package mcpserver

import (
	"context"
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
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
