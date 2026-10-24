package mcpserver

import (
	"context"
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/aitakemyjob/sheetdiver/internal/config"
	"github.com/aitakemyjob/sheetdiver/internal/core"
	"github.com/aitakemyjob/sheetdiver/internal/testutil"
)

// harness connects an in-memory MCP client to a server over a temporary
// library.
type harness struct {
	t    *testing.T
	core *core.Service
	cs   *mcp.ClientSession
}

// newHarness starts a server on an empty library.
func newHarness(t *testing.T) *harness {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("SHEETDIVER_LIBRARY", filepath.Join(dir, "lib"))
	store, err := config.Open(filepath.Join(dir, "config.json"))
	if err != nil {
		t.Fatal(err)
	}
	svc, err := core.New(store, "test")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { svc.Close() })
	ctx := context.Background()
	st, ct := mcp.NewInMemoryTransports()
	if _, err := New(svc).MCP().Connect(ctx, st, nil); err != nil {
		t.Fatal(err)
	}
	cs, err := mcp.NewClient(&mcp.Implementation{Name: "test-agent", Version: "1"}, nil).Connect(ctx, ct, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { cs.Close() })
	return &harness{t: t, core: svc, cs: cs}
}

// call runs a tool, returning its text, whether it had an image, and
// whether it failed.
func (h *harness) call(name string, args map[string]any) (string, bool, bool) {
	h.t.Helper()
	res, err := h.cs.CallTool(context.Background(), &mcp.CallToolParams{Name: name, Arguments: args})
	if err != nil {
		h.t.Fatalf("%s: protocol error: %v", name, err)
	}
	var sb strings.Builder
	img := false
	for _, c := range res.Content {
		switch c := c.(type) {
		case *mcp.TextContent:
			sb.WriteString(c.Text)
		case *mcp.ImageContent:
			img = len(c.Data) > 100 && c.MIMEType == "image/png"
		}
	}
	return sb.String(), img, res.IsError
}

// must runs a tool that must succeed and contain every want.
func (h *harness) must(name string, args map[string]any, wants ...string) string {
	h.t.Helper()
	out, _, isErr := h.call(name, args)
	if isErr {
		h.t.Fatalf("%s failed: %s", name, out)
	}
	for _, w := range wants {
		if !strings.Contains(out, w) {
			h.t.Errorf("%s output lacks %q:\n%s", name, w, out)
		}
	}
	return out
}

// importSample imports the synthetic datasheet the way an agent would,
// with bytes it downloaded itself.
func (h *harness) importSample() {
	h.must("import_datasheet", map[string]any{"data_base64": base64.StdEncoding.EncodeToString(testutil.Datasheet()), "filename": "sd123x.pdf"},
		"SD1234", "SD1235", "families: SD123x", "orderable variants: 2")
}

func TestAgentWorkflow(t *testing.T) {
	h := newHarness(t)
	h.importSample()
	h.must("get_part", map[string]any{"part": "SD1234DDCR"}, "SD1234", "by variant", "vref", "0.765", "same datasheet: SD1235")
	h.must("search", map[string]any{"query": "switching frequency"}, "fSW Switching frequency")
	h.must("get_parameters", map[string]any{"part": "SD1235", "key": "ilim"}, "[ilim]", "3.5", "5.3")
	h.must("get_section", map[string]any{"part": "SD1234", "section": "pins"}, "Pin Configuration")
	h.must("get_pins", map[string]any{"part": "SD1234"}, "VFB", "Feedback input")
	h.must("read_pages", map[string]any{"part": "SD1234", "from": 2}, "page 2 of 3", "Feedback voltage")
	h.must("list_figures", map[string]any{"part": "SD1234", "kind": "graph"}, "Figure 6-1", "Efficiency vs Output Current")

	out, img, isErr := h.call("get_figure", map[string]any{"part": "SD1234", "figure": "6-1"})
	if isErr || !img || !strings.Contains(out, "Output Current") {
		t.Fatalf("get_figure: image %v err %v %s", img, isErr, out)
	}
	if _, img, _ := h.call("get_page_image", map[string]any{"part": "SD1234", "page": 2, "region": map[string]any{"x0": 50, "y0": 60, "x1": 560, "y1": 160}}); !img {
		t.Error("get_page_image returns an image")
	}

	h.must("describe_figure", map[string]any{"part": "SD1234", "figure": "6-1", "title": "Efficiency vs load at VIN = 12 V", "conditions": "VOUT = 3.3 V",
		"curves": []map[string]any{{"label": "VIN 12 V", "summary": "peaks at 92% near 1 A"}}}, "described by agent")
	h.must("search", map[string]any{"query": "peaks"}, "Figure 6-1")
	h.must("record_parameters", map[string]any{"part": "SD1234", "replace_auto": true, "parameters": []map[string]any{
		{"key": "fsw", "name": "Switching frequency", "min": 520, "typ": 650, "max": 780, "unit": "kHz", "section": "electrical", "page": 2},
	}}, "recorded 1", "agent")
	if out, _, isErr := h.call("record_parameters", map[string]any{"part": "SD1234", "parameters": []map[string]any{{"key": "frequency", "name": "x"}}}); !isErr || !strings.Contains(out, "unknown key") {
		t.Errorf("unknown keys are rejected: %s", out)
	}
	h.must("record_recommendations", map[string]any{"part": "SD1234", "recommendations": []map[string]any{{"role": "inductor", "value": "2.2 µH", "mpn": "XAL4020-222", "manufacturer": "Coilcraft", "page": 2}}}, "recorded 1")
	h.must("record_registers", map[string]any{"part": "SD1234", "registers": []map[string]any{{"name": "CTRL", "address": "0x01", "width": 8, "fields": []map[string]any{{"name": "EN", "bits": "0", "access": "RW"}}}}}, "recorded 1")
	h.must("get_registers", map[string]any{"part": "SD1234"}, "0x01 CTRL", "[0] EN RW")
	h.must("add_note", map[string]any{"target": "part", "ref": "SD1234DDCR", "text": "Use 22 µF output caps"}, "note #")
	h.must("get_part", map[string]any{"part": "SD1234"}, "XAL4020-222", "Use 22 µF output caps", "(agent, ")

	out, img, isErr = h.call("get_tables", map[string]any{"part": "SD1234", "table": "7-1"})
	if isErr || !img || !strings.Contains(out, "IRREGULAR") {
		t.Fatalf("get_tables returns the image and flags the matrix: %v %s", img, out)
	}
	if out, _, isErr := h.call("record_table", map[string]any{"part": "SD1234", "table": "7-1", "header": []string{"VOUT", "RSET1"}, "rows": [][]string{{"3.3 V"}}}); !isErr || !strings.Contains(out, "row 1 has 1 cells") {
		t.Errorf("ragged rows are rejected: %s", out)
	}
	h.must("record_table", map[string]any{"part": "SD1234", "table": "7-1", "title": "VOUT by RSET1/RSET2",
		"header": []string{"VOUT", "RSET1", "RSET2"}, "rows": [][]string{{"1.8 V", "0 Ω", "0 Ω"}, {"3.3 V", "1.15 kΩ", "0 Ω"}}}, "recorded table", "2 rows")
	h.must("get_tables", map[string]any{"part": "SD1234"}, "VOUT by RSET1/RSET2 (transcribed by agent)", "[table] Table 8-1 List of Recommended Inductors")
	h.must("search", map[string]any{"query": "RSET1", "kinds": []string{"table"}}, "Table 7-1")
	h.must("get_part", map[string]any{"part": "SD1234"}, "XFL4020-222ME")

	h.must("list_categories", nil, "buck: 2 parts", "compares vin")
	h.must("compare_parts", map[string]any{"category": "buck", "filters": []map[string]any{{"key": "ilim", "op": ">=", "value": "4 A"}}},
		"1 of 2 parts match", "| SD1235 |", "excluded only because they have no ilim")
	if out, _, isErr := h.call("compare_parts", map[string]any{"category": "buck", "filters": []map[string]any{{"key": "vin", "op": ">=", "value": "lots"}}}); !isErr || !strings.Contains(out, "not a number") {
		t.Errorf("bad filter values are explained: %s", out)
	}

	h.must("create_project", map[string]any{"name": "Board A"}, "created project Board A")
	h.must("set_rail", map[string]any{"name": "VIN", "v_min": 10, "v_max": 14}, "rail VIN")
	h.must("set_rail", map[string]any{"name": "3V3", "v_nom": 3.3, "other_typ": 0.05, "other_max": 0.1}, "rail 3V3")
	h.must("add_to_project", map[string]any{"part": "SD1234", "designator": "U1", "supply_rail": "VIN", "output_rail": "3V3"}, "Board A")
	h.must("add_to_project", map[string]any{"part": "SD1235", "designator": "U9", "supply_rail": "3V3", "i_max": 0.5}, "U9")
	h.must("get_project", map[string]any{}, "3V3: 3.3 V", "load", "600 mA max", "U1", "other loads 50 mA typ / 100 mA max", "ripple budget")
	// Clearing a part's currents takes it out of the budget.
	h.must("update_project_part", map[string]any{"entry": "U9", "clear_currents": true}, "U9")
	h.must("get_project", map[string]any{}, "100 mA max", "U9 has no load current set")
	h.must("update_project_part", map[string]any{"entry": "U9", "remove": true}, "removed")

	calc := h.must("calculate", map[string]any{"kind": "buck", "part": "SD1234", "project": "Board A", "inputs": map[string]any{"iout": 2}, "save": true},
		"fsw = 650 kHz", "(agent)", "vinMax = 14 V", "vout = 3.3 V", "Chosen inductance", "saved as calculation")
	if !strings.Contains(calc, "Rtop") {
		t.Errorf("feedback divider from vref:\n%s", calc)
	}
	h.must("get_project", map[string]any{"project": "Board A"}, "Saved calculations", "SD1234 buck")
	h.must("list_calculators", nil, "gate_divider", "ton_min")
	h.must("library_overview", nil, "1 datasheets, 2 parts", "Board A")
}

func TestImportByPartNumberNeedsNetwork(t *testing.T) {
	if testing.Short() || os.Getenv("SHEETDIVER_NETWORK_TESTS") == "" {
		t.Skip("set SHEETDIVER_NETWORK_TESTS=1 to download from manufacturer sites")
	}
	h := newHarness(t)
	h.must("import_datasheet", map[string]any{"part_number": "TPS54331"}, "TPS54331", "downloaded from https://www.ti.com")
}

func TestAccessSwitches(t *testing.T) {
	h := newHarness(t)
	h.importSample()
	st := h.core.Config.Get().Settings
	st.AgentWrites = false
	h.core.UpdateSettings(st)
	if out, _, isErr := h.call("add_note", map[string]any{"target": "part", "ref": "SD1234", "text": "x"}); !isErr || !strings.Contains(out, "read-only") {
		t.Errorf("writes are refused when read-only: %s", out)
	}
	h.must("get_part", map[string]any{"part": "SD1234"}, "SD1234")
	st.AgentImport = false
	h.core.UpdateSettings(st)
	if out, _, isErr := h.call("import_datasheet", map[string]any{"part_number": "X"}); !isErr || !strings.Contains(out, "import") {
		t.Errorf("imports are refused: %s", out)
	}
	st.MCPEnabled = false
	h.core.UpdateSettings(st)
	if out, _, isErr := h.call("search", map[string]any{"query": "x"}); !isErr || !strings.Contains(out, "Settings") {
		t.Errorf("everything is refused when disabled: %s", out)
	}
	if acts := h.core.Activity(); len(acts) == 0 || acts[len(acts)-1].Error == "" {
		t.Error("refused calls show in the activity feed")
	}
}

// docs/MCP.md is the public tool reference; keep it complete.
func TestToolsAreDocumented(t *testing.T) {
	doc, err := os.ReadFile(filepath.Join("..", "..", "docs", "MCP.md"))
	if err != nil {
		t.Fatal(err)
	}
	h := newHarness(t)
	res, err := h.cs.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, tool := range res.Tools {
		if !strings.Contains(string(doc), "`"+tool.Name+"`") {
			t.Errorf("tool %s is missing from docs/MCP.md", tool.Name)
		}
	}
}

func TestBatchImportBOMAndFavorites(t *testing.T) {
	h := newHarness(t)
	h.importSample()
	dir := t.TempDir()
	for _, m := range []string{"ZX7001", "ZX7002"} {
		if err := os.WriteFile(filepath.Join(dir, strings.ToLower(m)+".pdf"), testutil.Mini(m, "Step-Down Converter"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	h.must("import_datasheets", map[string]any{"folder": dir, "paths": []string{filepath.Join(dir, "missing.pdf")}},
		"2 imported, 0 already in the library, 1 failed", "done "+filepath.Join(dir, "zx7001.pdf")+" → ZX7001", "FAILED "+filepath.Join(dir, "missing.pdf"))
	if _, _, isErr := h.call("import_datasheets", map[string]any{}); !isErr {
		t.Error("an empty batch is an error")
	}

	bom := "Designator,Manufacturer Part Number,Quantity\nU1,ZX7001,1\nU2,SD1235DDCR,1\nR1,,1\n"
	h.must("import_bom", map[string]any{"csv": bom, "project": "board", "fetch": false},
		"project board: 2 in library, 0 imported, 0 without datasheet, 1 skipped", "U2 ×1 SD1235DDCR → SD1235 [in library]")
	h.must("get_project", map[string]any{"project": "board"}, "ZX7001", "SD1235")

	h.must("update_part", map[string]any{"part": "ZX7002", "favorite": true, "status": "preferred"}, "★ ZX7002 [PREFERRED]")
	if out, _, isErr := h.call("update_part", map[string]any{"part": "ZX7002", "status": "best"}); !isErr {
		t.Errorf("bad status accepted: %s", out)
	}
	out := h.must("list_parts", map[string]any{"favorite": true}, "ZX7002")
	if strings.Contains(out, "ZX7001") {
		t.Errorf("favorite filter: %s", out)
	}
	h.must("compare_parts", map[string]any{"category": "buck", "status": "preferred"}, "| ZX7002 |", "★ preferred")
}

func TestAlternativesReportAndUpdates(t *testing.T) {
	h := newHarness(t)
	h.importSample()
	ctx := context.Background()
	for _, m := range []string{"ZX7001", "ZX7002"} {
		if _, err := h.core.Import(ctx, core.ImportRequest{Data: testutil.MiniWith(m, "Step-Down Converter", "4.5", map[string]string{"ZX7001": "28", "ZX7002": "36"}[m], "2", ""), Filename: m + ".pdf"}); err != nil {
			t.Fatal(err)
		}
	}
	h.must("find_alternatives", map[string]any{"part": "ZX7001"}, "alternatives to ZX7001 (buck), compared on vin, iout", "| ZX7002 | drop-in | ▲ 4.5 V … 36 V | = 0 A … 2 A |", "ZX7001's values: vin 4.5 V … 28 V")
	h.must("create_project", map[string]any{"name": "b"})
	h.must("add_to_project", map[string]any{"project": "b", "part": "ZX7001", "designator": "U1"})
	h.must("export_project_report", map[string]any{"project": "b"}, "# b — design review", "| U1 | 1 | ZX7001 |")
	// A project is deleted by name only, and its parts stay.
	if out, _, isErr := h.call("delete_project", map[string]any{"project": " "}); !isErr || !strings.Contains(out, "name the project") {
		t.Errorf("delete_project without a name: %s", out)
	}
	h.must("delete_project", map[string]any{"project": "b"}, "deleted project b", "parts stay")
	if out := h.must("list_projects", map[string]any{}); strings.Contains(out, "b:") {
		t.Errorf("b still listed: %s", out)
	}
	h.must("get_part", map[string]any{"part": "ZX7001"}, "ZX7001")
	if out, _, isErr := h.call("delete_project", map[string]any{"project": "b"}); !isErr {
		t.Errorf("deleting twice: %s", out)
	}
	// The sample came as bytes (no URL) and lookups are offline here.
	out, _, _ := h.call("check_datasheet_updates", map[string]any{"part": "nope"})
	if !strings.Contains(out, "nope") {
		t.Errorf("unknown part: %s", out)
	}
	if _, _, isErr := h.call("apply_datasheet_update", map[string]any{"part": "ZX7001"}); !isErr {
		t.Error("nothing to apply without a check")
	}
}

func TestCatalogueTools(t *testing.T) {
	h := newHarness(t)
	ctx := context.Background()
	if out, _, isErr := h.call("search_catalog", map[string]any{"query": "ao3400"}); !isErr || !strings.Contains(out, "Settings") {
		t.Errorf("before download: %v %s", isErr, out)
	}
	pdfSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write(testutil.Mini("ZX7001", "Step-Down Converter"))
	}))
	defer pdfSrv.Close()
	db, err := testutil.JLCDatabase(t.TempDir(), "source-db-v2", [][]any{
		{1, 1, "Power Management (PMIC)", "DC-DC Converters", "ZX7001DBVR", "SOT-23-6", "Zed Semi", "base", 0, "2A 4.5V~28V Buck", pdfSrv.URL + "/zx7001.pdf", 1200, "1-:0.31", `{"Output Current":"2A"}`},
		{2, 1, "Power Management (PMIC)", "DC-DC Converters", "ZX9000", "SOT-23-6", "Zed Semi", "expand", 0, "1A 5V~12V Buck", "", 0, "", `{"Output Current":"1A"}`},
	})
	if err != nil {
		t.Fatal(err)
	}
	files, err := testutil.SplitZip(db, 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	pub := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, ok := files[strings.TrimPrefix(r.URL.Path, "/")]
		if !ok {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Last-Modified", time.Now().UTC().Format(http.TimeFormat))
		w.Write(b)
	}))
	defer pub.Close()
	h.core.Catalog.Source = pub.URL + "/"
	if err := h.core.Catalog.Download(ctx, nil); err != nil {
		t.Fatal(err)
	}
	h.must("search_catalog", map[string]any{"query": "buck 2A", "in_stock": true}, "1 parts", "| C1 | ZX7001DBVR | Zed Semi | SOT-23-6 | basic | 1200 | 0.31 | – |")
	if out := h.must("search_catalog", map[string]any{"query": "buck"}); !strings.Contains(out, "ZX9000") {
		t.Errorf("out-of-stock parts are found unless filtered: %s", out)
	}
	h.must("catalog_parameters", map[string]any{}, "Power Management (PMIC) › DC-DC Converters: 2 (1)")
	h.must("catalog_parameters", map[string]any{"subcategory": "DC-DC Converters"}, "- Output Current [number, A] 2 parts, 1 A … 2 A; common: ")
	if out := h.must("search_catalog", map[string]any{"subcategory": "DC-DC Converters", "filters": []map[string]any{{"parameter": "Output Current", "op": "=", "value": "1A"}}}, "ZX9000"); strings.Contains(out, "ZX7001DBVR") {
		t.Errorf("output current = 1 A: %s", out)
	}
	out := h.must("search_catalog", map[string]any{"subcategory": "DC-DC Converters", "filters": []map[string]any{{"parameter": "Output Current", "op": ">=", "value": "1.5A"}}},
		"| Output Current |", "| ZX7001DBVR |", "| 2A |")
	if strings.Contains(out, "ZX9000") {
		t.Errorf("filtered out: %s", out)
	}
	h.must("search_catalog", map[string]any{"subcategory": "DC-DC Converters", "sort": "Output Current"}, "ZX9000")
	h.must("catalog_parameters", map[string]any{"subcategory": "DC-DC Converters"}, "- Package [text] 2 parts: SOT-23-6 (2)")
	h.must("search_catalog", map[string]any{"subcategory": "DC-DC Converters", "filters": []map[string]any{{"parameter": "Package", "op": "=", "value": "SOT-23-6|SOT-23"}}}, "ZX7001DBVR", "ZX9000")
	if out := h.must("search_catalog", map[string]any{"subcategory": "DC-DC Converters", "filters": []map[string]any{{"parameter": "Package", "op": "=", "value": "TO-252"}}}); strings.Contains(out, "ZX7001DBVR") {
		t.Errorf("package filter: %s", out)
	}
	if out, _, isErr := h.call("search_catalog", map[string]any{"subcategory": "DC-DC Converters", "filters": []map[string]any{{"parameter": "Output Current", "op": ">=", "value": "lots"}}}); !isErr || !strings.Contains(out, "can't read") {
		t.Errorf("bad value: %s", out)
	}
	h.must("import_datasheets", map[string]any{"lcsc": []string{"C1"}}, "1 imported", "ZX7001")
	h.must("search_catalog", map[string]any{"query": "ZX7001"}, "| ZX7001 |")
	h.must("get_part", map[string]any{"part": "ZX7001DBVR"}, "ZX7001")
}

// shown runs a show tool and returns the focus it sent to the UI.
func (h *harness) shown(name string, args map[string]any) core.Focus {
	h.t.Helper()
	events, stop := h.core.Bus.Subscribe()
	defer stop()
	h.must(name, args)
	for {
		select {
		case ev := <-events:
			if ev.Type == core.EvFocus {
				return ev.Data.(core.Focus)
			}
		case <-time.After(5 * time.Second):
			h.t.Fatalf("%s sent nothing to the UI", name)
		}
	}
}

func TestShowWhileDiscussing(t *testing.T) {
	h := newHarness(t)
	h.importSample()
	// Parts side by side, with notes and a recommendation.
	f := h.shown("show", map[string]any{"kind": "compare", "parts": []string{"sd1234", "SD1235"}, "keys": []string{"vin", "iout"},
		"notes": map[string]string{"SD1234": "cheaper"}, "highlight": "SD1234", "explain": "SD1234 does it: see [p2](#page/SD1234/2)."})
	if f.Kind != "compare" || !slices.Equal(f.Parts, []string{"SD1234", "SD1235"}) || f.Notes["SD1234"] != "cheaper" || f.Highlight != "SD1234" || !strings.Contains(f.Explain, "#page/SD1234/2") {
		t.Errorf("compare = %+v", f)
	}
	for _, bad := range []map[string]any{
		{"kind": "compare", "parts": []string{"SD1234"}},
		{"kind": "compare", "parts": []string{"SD1234", "NOPE9"}},
		{"kind": "compare", "parts": []string{"SD1234", "SD1235"}, "keys": []string{"speed"}},
		{"kind": "catalog"},
		{"kind": "hologram"},
	} {
		if out, _, isErr := h.call("show", bad); !isErr {
			t.Errorf("%v should fail: %s", bad, out)
		}
	}
	// Catalogue candidates.
	f = h.shown("show", map[string]any{"kind": "catalog", "lcsc": []string{"C1", "C2"}, "highlight": "C2"})
	if f.Kind != "catalog" || len(f.LCSC) != 2 || f.Highlight != "C2" {
		t.Errorf("catalog = %+v", f)
	}
	f = h.shown("show", map[string]any{"kind": "catalog", "subcategory": "MOSFETs", "filters": []map[string]any{{"parameter": "Drain to Source Voltage", "op": ">=", "value": "30V"}}})
	if f.Subcategory != "MOSFETs" || len(f.Filters) != 1 || f.Filters[0].Input != "30V" {
		t.Errorf("catalog search = %+v", f)
	}
	// A calculator filled in from the part.
	f = h.shown("show", map[string]any{"kind": "calc", "calculator": "divider", "inputs": map[string]any{"vin": 3.3, "vout": 1.8}})
	if f.Kind != "calc" || f.Calc == nil {
		t.Errorf("calc = %+v", f)
	}
	// A project with a rail highlighted.
	h.must("create_project", map[string]any{"name": "Board"}, "created")
	f = h.shown("show", map[string]any{"kind": "project", "project": "board", "highlight": "3V3"})
	if f.Project != "Board" || f.Highlight != "3V3" {
		t.Errorf("project = %+v", f)
	}
	// A walkthrough of several steps.
	f = h.shown("show_walkthrough", map[string]any{"title": "Why SD1234", "steps": []map[string]any{
		{"kind": "part", "part": "SD1234", "title": "The part", "explain": "A 3 A buck."},
		{"kind": "page", "part": "SD1234", "page": 2, "title": "Its limits"},
		{"kind": "compare", "parts": []string{"SD1234", "SD1235"}, "title": "Against SD1235"},
	}})
	if f.Kind != "walkthrough" || f.Title != "Why SD1234" || len(f.Steps) != 3 || f.Steps[1].Page != 1 || f.Steps[1].DatasheetID == 0 || f.Steps[0].Explain != "A 3 A buck." {
		t.Errorf("walkthrough = %+v", f)
	}
	if out, _, isErr := h.call("show_walkthrough", map[string]any{"title": "x", "steps": []map[string]any{{"kind": "part", "part": "NOPE9"}}}); !isErr || !strings.Contains(out, "step 1") {
		t.Errorf("a bad step names itself: %s", out)
	}
}

func TestConnectionsTools(t *testing.T) {
	h := newHarness(t)
	h.importSample()
	h.must("create_project", map[string]any{"name": "board", "activate": true}, "created")
	h.must("set_rail", map[string]any{"name": "3V3", "v_nom": 3.3}, "rail 3V3")
	h.must("add_to_project", map[string]any{"part": "SD1234", "designator": "U1", "output_rail": "3V3"}, "added")
	h.must("add_to_project", map[string]any{"part": "SD1235", "designator": "U2", "supply_rail": "3V3"}, "added")
	out := h.must("set_connections", map[string]any{
		"blocks": []map[string]any{{"name": "Battery"}},
		"nets":   []map[string]any{{"name": "SDA", "bus": "I2C1"}},
		"connect": []map[string]any{
			{"net": "VBAT", "part": "Battery"},
			{"net": "VBAT", "part": "U1", "pin": "VIN"},
			{"net": "3V3", "part": "U2.VIN"},
			{"net": "SDA", "part": "U1", "pin": "VFB"},
			{"net": "SDA", "part": "U2", "pin": "4"},
			{"net": "SCL", "part": "U3", "pin": "1"},
		},
	}, "problems (what couldn't be applied", "U3: not in the BOM", "VBAT [signal]: U1.3(VIN) Battery", "SDA [signal, bus I2C1]: U1.4(VFB) U2.4(VFB)",
		"U1 SD1234 — U2 SD1235: I2C1 (signal)", "[warn] U1: pins that must be connected aren't: GND (1)")
	// Links come from the connection table: U1 makes 3V3 in the BOM but
	// has no pin on it here.
	if strings.Contains(out, ": 3V3 (power)") {
		t.Errorf("no 3V3 link without a pin on it:\n%s", out)
	}
	if strings.Contains(out, "Battery →") {
		t.Errorf("VBAT isn't a power net, so the battery isn't drawn feeding U1:\n%s", out)
	}
	h.must("set_connections", map[string]any{"nets": []map[string]any{{"name": "VBAT", "kind": "power"}}}, "Battery → U1 SD1234: VBAT (power)")
	h.must("get_connections", map[string]any{}, "blocks off the BOM: Battery", "3V3 [power]: U2.3(VIN)")
	// Passives are listed with where they hang, not only part-to-part links.
	h.must("set_connections", map[string]any{
		"blocks":  []map[string]any{{"name": "C9", "kind": "capacitor", "value": "10µF"}, {"name": "L9", "kind": "inductor", "value": "2.2µH"}, {"name": "R9", "kind": "resistor", "value": "36.5k"}, {"name": "U9 TPS63900", "kind": "ic"}},
		"connect": []map[string]any{{"net": "3V3", "part": "C9"}, {"net": "LX1", "part": "U9 TPS63900", "pin": "LX1"}, {"net": "LX2", "part": "U9 TPS63900", "pin": "LX2"}, {"net": "LX1", "part": "L9", "pin": "1"}, {"net": "LX2", "part": "L9", "pin": "2"}, {"net": "FB", "part": "U2", "pin": "VFB"}, {"net": "FB", "part": "R9"}},
	}, "C9 10µF (supports U2 SD1235, guessed) on 3V3 (by U2 SD1235), to ground", "L9 2.2µH (supports U9 TPS63900, guessed) across U9 TPS63900 LX1/LX2")
	h.must("get_project", map[string]any{}, "## Connections", "pin connections, 5 blocks off the BOM")
	// Groups, and what a passive supports.
	h.must("set_connections", map[string]any{
		"groups": []map[string]any{{"name": "Power", "description": "Battery to 3V3", "notes": "keep the loop small"}},
		"assign": []map[string]any{{"part": "U1", "group": "Power"}, {"part": "Battery", "group": "Power"}, {"part": "U2", "group": "Load"}, {"part": "C9", "supports": "U1"}, {"part": "U7", "group": "Power"}},
	}, "problems (what couldn't be applied", "U7: not in the BOM", "group Power: U1 SD1234, Battery", "— Battery to 3V3 (notes: keep the loop small)", "group Load: U2 SD1235", "C9 10µF (supports U1 SD1234) on 3V3 (by U1 SD1234)",
		// Each assignment's result is listed back.
		"assignments now:", "C9: group Load (its part's); supports U1 SD1234; hangs by U1 SD1234", "U1: group Power")
	// Clearing a support says what it's guessed to be now; the wrong way
	// round is refused with the right way.
	h.must("set_connections", map[string]any{"assign": []map[string]any{{"part": "C9", "supports": ""}, {"part": "U1", "supports": "C9"}}},
		"C9: group Load (its part's); supports U2 SD1235, guessed; hangs by U2 SD1235", "U1 supports C9: the other way round? A passive supports the part it serves: part C9, supports U1")
	// Subgroups set through the tool are stored, echoed and listed per
	// group; set again with another field, they're kept.
	h.must("set_connections", map[string]any{"assign": []map[string]any{{"part": "C9", "cluster": "Decoupling"}, {"part": "R9", "cluster": "Feedback"}}},
		"C9: group Load (its part's); subgroup Decoupling; supports U2 SD1235", "R9: group Load (its part's); subgroup Feedback;", "group Load: U2 SD1235, C9, R9; subgroups: Decoupling (C9), Feedback (R9)")
	h.must("set_connections", map[string]any{"assign": []map[string]any{{"part": "C9", "supports": "U2"}}}, "C9: group Load (its part's); subgroup Decoupling; supports U2 SD1235;")
	h.must("get_connections", map[string]any{}, "subgroups: Decoupling (C9), Feedback (R9)")
	h.must("set_connections", map[string]any{"assign": []map[string]any{{"part": "R9", "cluster": ""}}}, "R9: group Load (its part's); supports")
	// Groups stack: Load is drawn below Power, said in the group's line;
	// a loop is refused for the group that would close it.
	h.must("set_connections", map[string]any{"groups": []map[string]any{{"name": "Load", "below": "Power"}}}, "group Load: U2 SD1235, C9, R9", "[drawn below Power]")
	h.must("set_connections", map[string]any{"groups": []map[string]any{{"name": "Power", "below": "Load"}}}, "group Power: below Load, which is (through others) below it; it's drawn in a column of its own", "[drawn below Power]")
	h.must("get_connections", map[string]any{}, "subgroups: Decoupling (C9) [drawn below Power]")
	// A part asked for a column and row: echoed, shown as not drawn yet,
	// then, once the app reports where it drew things, column by column
	// with whether the place was kept.
	h.must("set_connections", map[string]any{"assign": []map[string]any{{"part": "U2", "column": 1, "row": 0}, {"part": "R9", "column": 0}}},
		"U2: group Load; asked for column 1, row 0", "R9: group Load (its part's); asked for column 0;")
	h.must("get_connections", map[string]any{}, "## Placement", "not drawn since the last change", "U2 SD1235 asked for column 1, row 0\n", "R9 asked for column 0\n")
	ids := map[string]string{}
	d, _ := h.core.ProjectDetail(context.Background(), "board")
	for _, p := range d.Connections.Diagram.Parts {
		ids[strings.Fields(p.Title)[0]] = p.ID
	}
	for _, p := range d.Connections.Diagram.Passives {
		ids[p.Label] = p.ID
	}
	drawn := core.Drawn{Project: "board", Cells: []core.DrawnCell{
		{Node: ids["U1"], Group: "Power", Column: 0, Row: 0}, {Node: ids["Battery"], Group: "Power", Column: 0, Row: 1},
		{Node: ids["U2"], Group: "Load", Column: 1, Row: 0}, {Node: ids["C9"], Group: "Load", Column: 1, Row: 1}, {Node: ids["R9"], Group: "Load", Column: 2, Row: 0},
	}, Problems: []core.LayoutProblem{{Check: "wire round a part", Group: "Load", Text: "I2C1_SDA's wire leaves U2 SD1235's SDA on its left and runs round U2 SD1235 to its right"}}}
	h.core.SetDrawn(drawn)
	h.must("get_connections", map[string]any{}, "Power: column 0: U1 SD1234, Battery\n", "Load: column 0:  | column 1: U2 SD1235, C9 | column 2: R9\n",
		"U2 SD1235 asked for column 1, row 0: drawn there", "R9 asked for column 0: drawn in column 2, row 0 (the wiring didn't allow it)",
		"## Layout problems\nLoad: I2C1_SDA's wire leaves U2 SD1235's SDA on its left and runs round U2 SD1235 to its right (wire round a part)\n")
	// A picture of the diagram, from the app showing it: this plays the
	// app, drawing again when it's asked to show the diagram.
	events, stop := h.core.Bus.Subscribe()
	defer stop()
	opened := make(chan core.Focus, 4)
	go func() {
		for ev := range events {
			switch ev.Type {
			case core.EvSnapshot:
				req := ev.Data.(core.SnapshotRequest)
				answer := core.Snapshot{ID: req.ID, PNG: append([]byte("\x89PNG\r\n\x1a\n"), make([]byte, 200)...), Width: 640, Height: 480}
				if req.Project != "board" || req.Group == "Clock" {
					answer = core.Snapshot{ID: req.ID, Problem: `no group "Clock" on the diagram; its groups are Power, Load`}
				}
				_ = h.core.DeliverSnapshot(answer)
			case core.EvFocus:
				opened <- ev.Data.(core.Focus)
				h.core.SetDrawn(drawn)
			}
		}
	}()
	out, img, isErr := h.call("get_diagram_image", map[string]any{"group": "Load"})
	if isErr || !img || !strings.Contains(out, "group Load of board's block diagram as SheetDiver draws it (640×480 px).") ||
		!strings.Contains(out, "## Layout problems\nI2C1_SDA's wire leaves") {
		t.Errorf("Load's picture (image %v):\n%s", img, out)
	}
	h.must("get_diagram_image", map[string]any{"group": "Power"}, "## Layout problems\nnone found\n")
	h.must("get_diagram_image", map[string]any{}, "the block diagram of board as SheetDiver draws it", "Ask for one group", "Load: I2C1_SDA's wire")
	if out, _, isErr := h.call("get_diagram_image", map[string]any{"group": "Clock"}); !isErr || !strings.Contains(out, `no group "Clock"`) {
		t.Errorf("an unknown group: %s", out)
	}
	if _, img, isErr := h.call("get_diagram_image", map[string]any{"show": true}); isErr || !img {
		t.Error("show then picture failed")
	}
	if f := <-opened; f.Kind != "project" || f.Tab != "diagram" || f.Project != "board" {
		t.Errorf("show opened %+v", f)
	}
	stop()

	// A change makes the drawing out of date until the app draws again.
	h.must("set_connections", map[string]any{"assign": []map[string]any{{"part": "R9", "column": -1}}}, "R9: group Load (its part's); supports")
	h.must("get_connections", map[string]any{}, "not drawn since the last change")

	f := h.shown("show", map[string]any{"kind": "project", "project": "board", "tab": "diagram", "highlight": "I2C1"})
	if f.Tab != "diagram" || f.Highlight != "I2C1" {
		t.Errorf("show = %+v", f)
	}
	if _, _, isErr := h.call("show", map[string]any{"kind": "project", "project": "board", "tab": "registers"}); !isErr {
		t.Error("a project has no registers tab")
	}
}
