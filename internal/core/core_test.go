package core

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/aitakemyjob/sheetdiver/internal/calc"
	"github.com/aitakemyjob/sheetdiver/internal/catalog"
	"github.com/aitakemyjob/sheetdiver/internal/config"
	"github.com/aitakemyjob/sheetdiver/internal/extract"
	"github.com/aitakemyjob/sheetdiver/internal/library"
	"github.com/aitakemyjob/sheetdiver/internal/testutil"
)

// newService opens a service on a temporary library with the synthetic
// datasheet imported.
func newService(t *testing.T) *Service {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("SHEETDIVER_LIBRARY", filepath.Join(dir, "lib"))
	store, err := config.Open(filepath.Join(dir, "config.json"))
	if err != nil {
		t.Fatal(err)
	}
	s, err := New(store, "test")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	if _, err := s.Import(context.Background(), ImportRequest{Data: testutil.Datasheet(), Filename: "sd1234.pdf"}); err != nil {
		t.Fatal(err)
	}
	return s
}

func TestImportAndResolve(t *testing.T) {
	s := newService(t)
	ctx := context.Background()
	again, err := s.Import(ctx, ImportRequest{Data: testutil.Datasheet(), Filename: "copy.pdf"})
	if err != nil || !again.Existing {
		t.Fatalf("re-import should find the existing datasheet: %v %+v", err, again)
	}
	for ref, want := range map[string]string{"SD1234": "mpn", "sd1235ddcr": "variant", "SD1234DDCT": "prefix", "SD123x": "family"} {
		r, err := s.ResolvePart(ctx, ref)
		if err != nil || r.Via != want {
			t.Errorf("resolve %s: %+v %v, want via %s", ref, r, err, want)
		}
	}
	if _, err := s.ResolvePart(ctx, "NOPE99"); err == nil {
		t.Error("unknown parts are an error")
	}
	d, err := s.PartDetail(ctx, "SD1234")
	if err != nil {
		t.Fatal(err)
	}
	if d.Datasheet == nil || len(d.Siblings) != 1 || d.Counts["figures"] != 1 || d.Counts["pins"] != 4 {
		t.Fatalf("detail = %+v", d)
	}
	var vref *KeyValue
	for i := range d.Key {
		if d.Key[i].Key == "vref" {
			vref = &d.Key[i]
		}
	}
	if vref == nil || *vref.Param.Typ != 0.765 {
		t.Fatalf("vref key = %+v", vref)
	}
	// ILIM is specified for the SD1235 only.
	d2, _ := s.PartDetail(ctx, "SD1234")
	for _, k := range d2.Key {
		if k.Key == "ilim" {
			t.Error("SD1235-only row shown for SD1234")
		}
	}
	hits, err := s.Search(ctx, "feedback", library.SearchOptions{})
	if err != nil || len(hits) == 0 {
		t.Fatalf("search: %v %v", hits, err)
	}
}

func TestRecordedValuesSurviveReextraction(t *testing.T) {
	s := newService(t)
	ctx := context.Background()
	_, d, err := s.DatasheetOf(ctx, "SD1234")
	if err != nil {
		t.Fatal(err)
	}
	v := 1.0
	if _, err := s.Lib.RecordParameters(ctx, d.ID, []library.Parameter{{Key: "fsw", Name: "Switching frequency", Typ: &v, Unit: "MHz", Section: "electrical", Page: 1}}, library.SourceAgent, true); err != nil {
		t.Fatal(err)
	}
	f, err := s.FindFigure(ctx, "SD1234", "6-1")
	if err != nil {
		t.Fatal(err)
	}
	title := "Efficiency vs load, VIN = 12 V"
	if _, err := s.Lib.UpdateFigure(ctx, f.ID, library.FigureUpdate{Title: &title}, library.SourceAgent); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Reextract(ctx, d.ID); err != nil {
		t.Fatal(err)
	}
	ps, _ := s.Lib.Parameters(ctx, d.ID, library.ParamFilter{Key: "fsw"})
	if len(ps) != 1 || ps[0].Source != library.SourceAgent || *ps[0].Typ != 1 {
		t.Errorf("fsw after re-extraction = %+v", ps)
	}
	f2, err := s.Lib.Figure(ctx, f.ID)
	if err != nil || f2.Title != title {
		t.Errorf("figure lost its id or description: %+v %v", f2, err)
	}
}

func TestBuckCalcFillsFromDatasheet(t *testing.T) {
	s := newService(t)
	resp, err := s.Calc(context.Background(), CalcRequest{Kind: "buck", Part: "SD1235", Inputs: json.RawMessage(`{"vinMin":10,"vinMax":14,"vout":3.3,"iout":2}`)})
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]float64{}
	for _, f := range resp.Filled {
		got[f.Field] = f.Value
		if !strings.Contains(f.From, "SD1235 p") {
			t.Errorf("provenance %q", f.From)
		}
	}
	if got["fsw"] != 650e3 || got["vref"] != 0.765 || got["ilim"] != 3.5 || got["vinLimit"] != 17 {
		t.Errorf("filled = %v", got)
	}
	if got["tonMin"] < 89e-9 || got["tonMin"] > 91e-9 {
		t.Errorf("tonMin = %v", got["tonMin"])
	}
	if l, _ := resp.Result.Get("L"); l <= 0 {
		t.Error("no inductor")
	}
	if _, err := s.Calc(context.Background(), CalcRequest{Kind: "buck", Inputs: json.RawMessage(`{"vin":12}`)}); err == nil || !strings.Contains(err.Error(), "fields") {
		t.Errorf("misspelled inputs must be rejected: %v", err)
	}
}

func TestProjectBudgetAndChecks(t *testing.T) {
	s := newService(t)
	ctx := context.Background()
	p, err := s.Lib.CreateProject(ctx, "board", "")
	if err != nil {
		t.Fatal(err)
	}
	f := func(v float64) *float64 { return &v }
	s.Lib.SetRail(ctx, library.Rail{ProjectID: p.ID, Name: "VIN", VMin: f(9), VMax: f(24)})
	s.Lib.SetRail(ctx, library.Rail{ProjectID: p.ID, Name: "3V3", VNom: f(3.3)})
	if _, err := s.AddToProject(ctx, "board", "SD1234DDCR", library.ProjectPart{Designator: "U1", SupplyRail: "VIN", OutputRail: "3V3"}); err != nil {
		t.Fatal(err)
	}
	// A load on the regulator's output, and the MCU it powers.
	mcu, _ := s.Lib.AddPart(ctx, library.Part{MPN: "MCU1", Category: "mcu"})
	if _, err := s.AddToProject(ctx, "board", mcu.MPN, library.ProjectPart{Designator: "U2", SupplyRail: "3V3", IMax: f(3.2), ITyp: f(1)}); err != nil {
		t.Fatal(err)
	}
	d, err := s.ProjectDetail(ctx, "board")
	if err != nil {
		t.Fatal(err)
	}
	rails := map[string]RailStatus{}
	for _, r := range d.Rails {
		rails[r.Name] = r
	}
	if r := rails["3V3"]; r.LoadMax != 3.2 || r.Capacity != 3 {
		t.Errorf("3V3 = %+v", r)
	}
	// The buck's input current derives from its output load.
	want := 3.3 * 3.2 / (16.5 * 0.9)
	if r := rails["VIN"]; r.LoadMax < want*0.99 || r.LoadMax > want*1.01 {
		t.Errorf("VIN load %g, want %g", r.LoadMax, want)
	}
	text := ""
	for _, c := range d.Checks {
		text += c.Level + ": " + c.Message + "\n"
	}
	for _, w := range []string{"error: U1: VIN reaches 24 V, above the absolute maximum 20 V", "error: 3V3 load 3.2 A exceeds its capacity 3 A"} {
		if !strings.Contains(text, w) {
			t.Errorf("missing check %q in\n%s", w, text)
		}
	}
	if d.Parts[0].Variant != "SD1234DDCR" {
		t.Errorf("the orderable code is kept as the variant: %+v", d.Parts[0])
	}
	// Project rails fill calculator inputs.
	resp, err := s.Calc(ctx, CalcRequest{Kind: "buck", Part: "SD1234", Project: "board", Save: true})
	if err != nil {
		t.Fatal(err)
	}
	filled := map[string]float64{}
	for _, fl := range resp.Filled {
		filled[fl.Field] = fl.Value
	}
	if filled["vinMin"] != 9 || filled["vinMax"] != 24 || filled["vout"] != 3.3 || filled["iout"] != 3.2 || resp.SavedID == 0 {
		t.Errorf("filled from project = %v (saved %d)", filled, resp.SavedID)
	}
	if resp.Result.Worst() != calc.Error {
		t.Error("VinMax above the device limit must fail the buck checks")
	}
}

func TestAgentSwitches(t *testing.T) {
	s := newService(t)
	st := s.Config.Get().Settings
	st.AgentWrites = false
	s.UpdateSettings(st)
	if s.AgentRead() != nil || s.AgentWrite() != ErrAgentReadOnly {
		t.Error("read-only mode")
	}
	st.MCPEnabled = false
	s.UpdateSettings(st)
	if s.AgentRead() != ErrAgentDisabled {
		t.Error("agents disabled")
	}
}

func TestTablesRecommendationsAndTranscription(t *testing.T) {
	s := newService(t)
	ctx := context.Background()
	_, d, err := s.DatasheetOf(ctx, "SD1234")
	if err != nil {
		t.Fatal(err)
	}
	recs, _ := s.Lib.Recommendations(ctx, d.ID, "")
	if len(recs) != 2 || recs[0].Role != "inductor" || recs[0].Value != "2.2 µH" || recs[0].MPN != "XFL4020-222ME" || recs[0].Manufacturer != "Coilcraft" ||
		!strings.Contains(recs[0].Conditions, "SATURATION CURRENT [A]: 3.5") {
		t.Fatalf("recommendations from Table 8-1 = %+v", recs)
	}
	tb, err := s.FindTable(ctx, "SD1234", "7-1")
	if err != nil || tb.Irregular == "" {
		t.Fatalf("table 7-1: %+v %v", tb, err)
	}
	if _, png, err := s.RenderTable(ctx, tb.ID, 72); err != nil || len(png) < 100 {
		t.Fatalf("render: %v", err)
	}
	header := []string{"VOUT", "RSET1", "RSET2", "Current limit"}
	rows := [][]string{{"3.3 V", "1.15 kΩ", "511 Ω", "100 mA"}}
	if _, err := s.Lib.RecordTable(ctx, tb.ID, library.TableUpdate{Header: &header, Rows: &rows}, library.SourceAgent); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Reextract(ctx, d.ID); err != nil {
		t.Fatal(err)
	}
	again, err := s.Lib.Table(ctx, tb.ID)
	if err != nil || again.Source != library.SourceAgent || again.Irregular != "" || len(again.Rows) != 1 {
		t.Fatalf("a transcribed table keeps its id and content: %+v %v", again, err)
	}
	if recs, _ := s.Lib.Recommendations(ctx, d.ID, ""); len(recs) != 2 {
		t.Errorf("re-extraction replaces automatic recommendations: %d", len(recs))
	}
	hits, _ := s.Search(ctx, "511", library.SearchOptions{Kinds: []string{"table"}})
	if len(hits) == 0 {
		t.Error("transcribed cells are searchable")
	}
}

func TestCompareCategory(t *testing.T) {
	s := newService(t)
	ctx := context.Background()
	cs, err := s.Categories(ctx)
	if err != nil || len(cs) != 1 || cs[0].Category != "buck" || cs[0].Count != 2 {
		t.Fatalf("categories = %+v %v", cs, err)
	}
	res, err := s.Compare(ctx, CompareRequest{Category: "buck"})
	if err != nil {
		t.Fatal(err)
	}
	cols := map[string]bool{}
	for _, c := range res.Columns {
		cols[c.Key] = true
	}
	for _, k := range []string{"vin", "iout", "fsw", "vref", "ilim"} {
		if !cols[k] {
			t.Errorf("no %s column in %+v", k, res.Columns)
		}
	}
	if len(res.Rows) != 2 || res.Total != 2 {
		t.Fatalf("rows = %+v", res.Rows)
	}
	if v := res.Rows[0].Values["fsw"]; v.Typ == nil || *v.Typ != 650e3 || v.Unit != "Hz" {
		t.Errorf("fsw in SI = %+v", v)
	}
	// ILIM is only given for the SD1235; a filter on it excludes the
	// SD1234 as unknown, not as failing.
	res, _ = s.Compare(ctx, CompareRequest{Category: "buck", Filters: []Filter{{Key: "ilim", Op: ">=", Value: 4}}})
	if len(res.Rows) != 1 || res.Rows[0].Part.MPN != "SD1235" || res.Unknown["ilim"] != 1 {
		t.Errorf("ilim >= 4 A (max 5.3) = %+v unknown %v", res.Rows, res.Unknown)
	}
	// >= tests the highest the part can do: VIN 4.5–17 V reaches 12 V but not 24 V.
	if res, _ := s.Compare(ctx, CompareRequest{Category: "buck", Filters: []Filter{{Key: "vin", Op: ">=", Value: 12}}}); len(res.Rows) != 2 {
		t.Errorf("vin >= 12 V: %d rows", len(res.Rows))
	}
	if res, _ := s.Compare(ctx, CompareRequest{Category: "buck", Filters: []Filter{{Key: "vin", Op: ">=", Value: 24}}}); len(res.Rows) != 0 {
		t.Errorf("vin >= 24 V: %d rows", len(res.Rows))
	}
	if res, _ := s.Compare(ctx, CompareRequest{Category: "buck", Query: "sd1235"}); len(res.Rows) != 1 {
		t.Errorf("query: %d rows", len(res.Rows))
	}
	res, _ = s.Compare(ctx, CompareRequest{Category: "buck", Sort: "ilim", Desc: true})
	if res.Rows[0].Part.MPN != "SD1235" {
		t.Errorf("parts without a value sort last")
	}
	if _, err := s.Compare(ctx, CompareRequest{Category: "ldo"}); err == nil || !strings.Contains(err.Error(), "buck") {
		t.Errorf("unknown categories list the known ones: %v", err)
	}
	if _, err := s.Compare(ctx, CompareRequest{Category: "buck", Filters: []Filter{{Key: "vin", Op: "=="}}}); err == nil {
		t.Error("bad operators are rejected")
	}
}

func TestParseQuantity(t *testing.T) {
	cases := map[string]float64{"24 V": 24, "3A": 3, "650k": 650e3, "650 kHz": 650e3, "1 µA": 1e-6, "1uA": 1e-6, "68 mΩ": 0.068, "30m": 0.03, "2.2u": 2.2e-6, "12": 12}
	for in, want := range cases {
		got, err := ParseQuantity(in)
		if err != nil || math.Abs(got-want) > want*1e-9 {
			t.Errorf("ParseQuantity(%q) = %v, %v; want %v", in, got, err, want)
		}
	}
	if _, err := ParseQuantity("lots"); err == nil {
		t.Error("garbage is rejected")
	}
}

func TestBatchAndFolderImport(t *testing.T) {
	s := newService(t)
	ctx := context.Background()
	dir := t.TempDir()
	for _, m := range []string{"ZX7001", "ZX7002", "ZX7003"} {
		if err := os.WriteFile(filepath.Join(dir, strings.ToLower(m)+".pdf"), testutil.Mini(m, "Step-Down Converter"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	os.MkdirAll(filepath.Join(dir, "sub"), 0o755)
	os.WriteFile(filepath.Join(dir, "sub", "zx7004.pdf"), testutil.Mini("ZX7004", "Step-Down Converter"), 0o644)
	os.WriteFile(filepath.Join(dir, "notes.txt"), []byte("not a pdf"), 0o644)
	flat, err := FolderRequests(dir, false)
	if err != nil || len(flat) != 3 {
		t.Fatalf("folder = %d %v", len(flat), err)
	}
	all, _ := FolderRequests(dir, true)
	if len(all) != 4 {
		t.Fatalf("recursive folder = %d", len(all))
	}
	// One duplicate and one broken file in the batch.
	reqs := append(all, ImportRequest{Data: testutil.Datasheet(), Filename: "dup.pdf"}, ImportRequest{Path: filepath.Join(dir, "notes.txt")})
	out := s.ImportBatch(ctx, reqs)
	status := map[string]int{}
	for _, o := range out {
		status[o.Job.Status]++
	}
	if status[JobDone] != 4 || status[JobExisting] != 1 || status[JobFailed] != 1 {
		t.Fatalf("statuses = %v", status)
	}
	for _, m := range []string{"ZX7001", "ZX7004"} {
		if r, err := s.ResolvePart(ctx, m); err != nil || r.Part.Category != "buck" {
			t.Errorf("%s: %+v %v", m, r.Part, err)
		}
	}
	jobs := s.ImportJobs()
	var failed ImportJob
	for _, j := range jobs {
		if j.Status == JobFailed {
			failed = j
		}
	}
	if failed.ID == 0 || !strings.Contains(failed.Error, "not a PDF") {
		t.Fatalf("failed job = %+v", failed)
	}
	for _, j := range jobs {
		if j.Status == JobDone {
			if _, err := s.RetryImport(j.ID); err == nil {
				t.Error("only failed jobs can be retried")
			}
			break
		}
	}
	s.ClearImports()
	if left := s.ImportJobs(); len(left) != 0 {
		t.Errorf("cleared queue still has %d jobs", len(left))
	}
}

func TestParseBOM(t *testing.T) {
	kicad := "\"Reference\",\"Value\",\"Footprint\",\"Qty\",\"MPN\",\"Manufacturer\"\n" +
		"\"C1,C2\",\"10uF\",\"C_0805\",\"2\",\"\",\"\"\n" +
		"\"U1\",\"TPS563200\",\"SOT-23-6\",\"1\",\"TPS563200DDCR\",\"Texas Instruments\"\n" +
		"\"U2\",\"ZX7001\",\"SOT-23-6\",\"1\",\"\",\"\"\n" +
		"\"R1-R4\",\"10k\",\"R_0402\",\"\",\"\",\"\"\n"
	lines, err := ParseBOM([]byte(kicad))
	if err != nil || len(lines) != 4 {
		t.Fatalf("lines = %+v %v", lines, err)
	}
	if l := lines[0]; l.Qty != 2 || strings.Join(l.Designators, " ") != "C1 C2" || l.MPN != "" {
		t.Errorf("passives have no part number: %+v", l)
	}
	if l := lines[1]; l.MPN != "TPS563200DDCR" || l.Manufacturer != "Texas Instruments" {
		t.Errorf("MPN column: %+v", l)
	}
	if l := lines[2]; l.MPN != "ZX7001" {
		t.Errorf("a part-number Value stands in for the MPN: %+v", l)
	}
	if l := lines[3]; l.Qty != 4 || l.Designators[3] != "R4" {
		t.Errorf("ranges expand: %+v", l)
	}
	semi := "Designator;Manufacturer Part Number;Quantity\nU1;ZX7002;1\n"
	if lines, err := ParseBOM([]byte(semi)); err != nil || lines[0].MPN != "ZX7002" {
		t.Errorf("semicolons: %+v %v", lines, err)
	}
	if _, err := ParseBOM([]byte("a,b\n1,2\n")); err == nil {
		t.Error("a CSV without BOM columns is rejected")
	}
}

func TestImportBOMFetchesByPartNumber(t *testing.T) {
	s := newService(t)
	ctx := context.Background()
	// A stand-in manufacturer site serving datasheets by lower-case part
	// number, like ti.com's symlink URLs.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/zx7001.pdf":
			w.Write(testutil.Mini("ZX7001", "Step-Down Converter"))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	saved := datasheetURLs
	datasheetURLs = []struct {
		pattern string
		upper   bool
	}{{srv.URL + "/%s.pdf", false}}
	defer func() { datasheetURLs = saved }()

	bom := "Reference,Value,MPN,Qty\nC1,10uF,,1\nU1,,SD1234DDCR,1\nU2,,ZX7001TR,1\nU3,,QQ9999,2\n"
	res, err := s.ImportBOM(ctx, BOMRequest{Data: []byte(bom), Filename: "board-a.csv", Fetch: true})
	if err != nil {
		t.Fatal(err)
	}
	if res.Project != "board-a" {
		t.Errorf("project = %q", res.Project)
	}
	got := map[string]BOMRow{}
	for _, r := range res.Rows {
		got[strings.Join(r.Designators, ",")] = r
	}
	if r := got["U1"]; r.Status != BOMInLibrary || r.Part != "SD1234" {
		t.Errorf("U1 = %+v", r)
	}
	if r := got["U2"]; r.Status != BOMImported || r.Part != "ZX7001" {
		t.Errorf("U2 fetched by its base number = %+v", r)
	}
	if r := got["U3"]; r.Status != BOMNoDatasheet || !strings.Contains(r.Note, "no datasheet for QQ9999") {
		t.Errorf("U3 = %+v", r)
	}
	if r := got["C1"]; r.Status != BOMSkipped {
		t.Errorf("C1 = %+v", r)
	}
	d, err := s.ProjectDetail(ctx, "board-a")
	if err != nil || len(d.Parts) != 3 {
		t.Fatalf("project parts = %+v %v", d.Parts, err)
	}
	var u1 library.ProjectPart
	for _, p := range d.Parts {
		if p.Designator == "U1" {
			u1 = p
		}
		if p.Designator == "U3" && (p.HasDatasheet || p.Qty != 2) {
			t.Errorf("placeholder = %+v", p)
		}
	}
	if u1.Variant != "SD1234DDCR" {
		t.Errorf("the BOM's orderable code is kept: %+v", u1)
	}
	warned := false
	for _, c := range d.Checks {
		if strings.Contains(c.Message, "QQ9999) has no datasheet") {
			warned = true
		}
	}
	if !warned {
		t.Errorf("checks = %+v", d.Checks)
	}
	// Importing the same BOM again doesn't duplicate project entries.
	if _, err := s.ImportBOM(ctx, BOMRequest{Data: []byte(bom), Project: "board-a", Fetch: true}); err != nil {
		t.Fatal(err)
	}
	if d, _ := s.ProjectDetail(ctx, "board-a"); len(d.Parts) != 3 {
		t.Errorf("re-import duplicated entries: %d", len(d.Parts))
	}
}

func TestStatusesInProjectsAndComparisons(t *testing.T) {
	s := newService(t)
	ctx := context.Background()
	r, _ := s.ResolvePart(ctx, "SD1235")
	eol, yes := "eol", true
	if _, err := s.Lib.UpdatePart(ctx, r.Part.ID, library.PartUpdate{Status: &eol, Favorite: &yes}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Lib.CreateProject(ctx, "p", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := s.AddToProject(ctx, "p", "SD1235", library.ProjectPart{Designator: "U1"}); err != nil {
		t.Fatal(err)
	}
	d, _ := s.ProjectDetail(ctx, "p")
	found := false
	for _, c := range d.Checks {
		found = found || (c.Level == calc.Warn && strings.Contains(c.Message, "marked EOL"))
	}
	if !found {
		t.Errorf("an end-of-life part is flagged: %+v", d.Checks)
	}
	res, _ := s.Compare(ctx, CompareRequest{Category: "buck", Favorite: true})
	if len(res.Rows) != 1 || res.Rows[0].Part.MPN != "SD1235" || res.Rows[0].Part.Status != "eol" {
		t.Errorf("favorites in comparisons = %+v", res.Rows)
	}
}

func TestLookupCandidates(t *testing.T) {
	got := lookupCandidates("max77801ewp+t")
	if strings.Join(got, " ") != "MAX77801EWP+T MAX77801EWP MAX77801" {
		t.Errorf("candidates = %v", got)
	}
	if u := orderedURLs("MAX77801"); !strings.Contains(u[0].pattern, "analog.com") {
		t.Errorf("Maxim parts try ADI first: %v", u[0])
	}
	if u := orderedURLs("TPS563200"); !strings.Contains(u[0].pattern, "ti.com") {
		t.Errorf("TI first: %v", u[0])
	}
}

func TestCurrentLimitIsACeilingNotACapacity(t *testing.T) {
	v := 3.3
	rail := func(load float64) []RailStatus {
		return []RailStatus{{Rail: library.Rail{Name: "3V3", VNom: &v}, LoadMax: load, Ceiling: 1.33, CeilingFrom: "TPS63900 current limit, min (p8)", SourcePart: "U1"}}
	}
	find := func(cs []ProjectCheck, level, text string) bool {
		for _, c := range cs {
			if c.Level == level && strings.Contains(c.Message, text) {
				return true
			}
		}
		return false
	}
	cs := projectChecks(rail(0.4), nil, nil)
	if !find(cs, calc.Warn, "capacity is unknown") || find(cs, calc.OK, "% of") {
		t.Errorf("under the limit, capacity is unknown, not OK: %+v", cs)
	}
	if cs := projectChecks(rail(1.5), nil, nil); !find(cs, calc.Error, "exceeds the regulator's current limit 1.33 A") {
		t.Errorf("over the limit is an error: %+v", cs)
	}
	if cs := projectChecks(rail(0), nil, nil); find(cs, calc.Warn, "capacity is unknown") {
		t.Errorf("an unloaded rail needs no warning: %+v", cs)
	}
	ps := []library.Parameter{{Key: "ilim", Min: f64(1.33), Unit: "A", Page: 7}}
	if c, _ := regulatorCapacity(ps); c != 0 {
		t.Errorf("ilim alone is no capacity: %v", c)
	}
	if c, from := regulatorCeiling(ps); c != 1.33 || !strings.Contains(from, "p8") {
		t.Errorf("ceiling = %v %q", c, from)
	}
}

// f64 returns a pointer to v.
func f64(v float64) *float64 { return &v }

func TestAlternatives(t *testing.T) {
	s := newService(t)
	ctx := context.Background()
	add := func(mpn, lo, hi, iout, iq string) {
		t.Helper()
		if _, err := s.Import(ctx, ImportRequest{Data: testutil.MiniWith(mpn, "Step-Down Converter", lo, hi, iout, iq), Filename: mpn + ".pdf"}); err != nil {
			t.Fatal(err)
		}
	}
	add("ZX7001", "4.5", "28", "2", "40") // the original
	add("ZX7002", "3", "36", "3", "20")   // better everywhere
	add("ZX7003", "4.5", "17", "2", "40") // narrower input range
	add("ZX7004", "4.5", "28", "2", "40") // identical, but end of life
	add("ZX7005", "4.5", "28", "2", "")   // no IQ known
	r, _ := s.ResolvePart(ctx, "ZX7004")
	eol := "eol"
	s.Lib.UpdatePart(ctx, r.Part.ID, library.PartUpdate{Status: &eol})

	res, err := s.Alternatives(ctx, AltRequest{Part: "ZX7001"})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(res.Keys, ",") != "vin,iout,iq" {
		t.Fatalf("keys = %v", res.Keys)
	}
	var got []string
	for _, a := range res.Alternatives {
		got = append(got, fmt.Sprintf("%s:%v/%d/%d", a.Part.MPN, a.DropIn, a.Worse, a.Unknown))
	}
	// ZX7002 is better on every key; ZX7005 matches but lacks IQ; ZX7003
	// can't take 28 V; ZX7004 (EOL) and SD1234/SD1235 (no IOUT) follow
	// only as far as they share keys.
	want := "ZX7002:true/0/0 ZX7005:true/0/1 ZX7003:false/1/0"
	if g := strings.Join(got, " "); !strings.HasPrefix(g, want) || strings.Contains(g, "ZX7004") {
		t.Errorf("alternatives = %s", g)
	}
	for _, c := range res.Alternatives[0].Checks {
		if c.Verdict != VerdictBetter {
			t.Errorf("ZX7002 %s = %+v", c.Key, c)
		}
	}
	all, _ := s.Alternatives(ctx, AltRequest{Part: "ZX7001", Discouraged: true})
	// On request discouraged parts are included; an identical part is the
	// closest (safest) drop-in.
	if all.Alternatives[0].Part.MPN != "ZX7004" {
		t.Errorf("discouraged parts on request: %+v", all.Alternatives[0].Part)
	}
	// Among drop-ins the closest wins, unless another is preferred.
	if res, _ := s.Alternatives(ctx, AltRequest{Part: "ZX7001", Keys: []string{"vin"}}); res.Alternatives[0].Part.MPN != "ZX7005" {
		t.Errorf("closest first: %+v", res.Alternatives[0].Part)
	}
	r2, _ := s.ResolvePart(ctx, "ZX7002")
	pref := "preferred"
	s.Lib.UpdatePart(ctx, r2.Part.ID, library.PartUpdate{Status: &pref})
	if res, _ := s.Alternatives(ctx, AltRequest{Part: "ZX7001", Keys: []string{"vin"}}); res.Alternatives[0].Part.MPN != "ZX7002" {
		t.Errorf("preferred first among drop-ins: %+v", res.Alternatives[0].Part)
	}
	if _, err := s.Alternatives(ctx, AltRequest{Part: "nope"}); err == nil {
		t.Error("unknown parts are an error")
	}
}

func TestCompareKeyDirections(t *testing.T) {
	v := func(min, typ, max *float64) CompareValue {
		return CompareValue{Min: min, Typ: typ, Max: max, Unit: "V"}
	}
	cases := []struct {
		key  string
		o, c CompareValue
		want string
	}{
		{"vin", v(f64(4.5), nil, f64(28)), v(f64(3), nil, f64(36)), VerdictBetter},
		{"vin", v(f64(4.5), nil, f64(28)), v(f64(4.5), nil, f64(28)), VerdictSame},
		{"vin", v(f64(4.5), nil, f64(28)), v(f64(5), nil, f64(36)), VerdictWorse},
		{"iout", v(nil, nil, f64(2)), v(nil, nil, f64(1.99)), VerdictSame},
		{"iout", v(nil, nil, f64(2)), v(nil, nil, f64(1.5)), VerdictWorse},
		{"iq", v(nil, f64(40e-6), nil), v(nil, f64(20e-6), nil), VerdictBetter},
		{"rdson", v(nil, f64(0.02), nil), v(nil, f64(0.03), nil), VerdictWorse},
		{"vref", v(nil, f64(0.6), nil), v(nil, f64(0.62), nil), VerdictSame},
		{"vref", v(nil, f64(0.6), nil), v(nil, f64(0.8), nil), VerdictWorse},
	}
	for _, c := range cases {
		if got, _ := compareKey(c.key, c.o, c.c, true, true); got.Verdict != c.want {
			t.Errorf("%s %s vs %s = %s, want %s", c.key, rangeStr(c.o, true), rangeStr(c.c, true), got.Verdict, c.want)
		}
	}
	if got, _ := compareKey("vin", v(f64(1), nil, f64(2)), CompareValue{}, true, false); got.Verdict != VerdictUnknown || got.Candidate != "–" {
		t.Errorf("missing value = %+v", got)
	}
}

func TestProjectReport(t *testing.T) {
	s := newService(t)
	ctx := context.Background()
	proj, err := s.Lib.CreateProject(ctx, "Sensor | board", "Battery sensor node")
	if err != nil {
		t.Fatal(err)
	}
	v12, v33 := 12.0, 3.3
	for _, r := range []library.Rail{{ProjectID: proj.ID, Name: "VIN", VNom: &v12}, {ProjectID: proj.ID, Name: "3V3", VNom: &v33}} {
		if _, err := s.Lib.SetRail(ctx, r); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := s.AddToProject(ctx, "Sensor | board", "SD1234", library.ProjectPart{Designator: "U1", SupplyRail: "VIN", OutputRail: "3V3"}); err != nil {
		t.Fatal(err)
	}
	im := 0.5
	r, _ := s.ResolvePart(ctx, "SD1235")
	eol := "eol"
	s.Lib.UpdatePart(ctx, r.Part.ID, library.PartUpdate{Status: &eol})
	if _, err := s.AddToProject(ctx, "Sensor | board", "SD1235", library.ProjectPart{Designator: "U2", Qty: 2, SupplyRail: "3V3", IMax: &im}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Calc(ctx, CalcRequest{Kind: "divider", Inputs: json.RawMessage(`{"vin": 3.3, "vout": 1.8}`), Project: "Sensor | board", Save: true, Title: "ADC divider"}); err != nil {
		t.Fatal(err)
	}
	md, err := s.ProjectReport(ctx, "Sensor | board")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"# Sensor | board — design review", "Battery sensor node", "2 BOM lines (3 parts), 2 rails",
		"## Findings", "marked EOL", "## Rails", "| 3V3 | 3.3 V | U1 SD1234 |",
		"## Parts", "| U2 | 2 | SD1235 |", "eol", "vin 4.5 V … 17 V",
		"## Calculations", "### ADC divider",
	} {
		if !strings.Contains(md, want) {
			t.Errorf("report lacks %q:\n%s", want, md)
		}
	}
	if _, err := s.ProjectReport(ctx, "nope"); err == nil {
		t.Error("unknown project")
	}
}

func TestDatasheetUpdates(t *testing.T) {
	s := newService(t)
	ctx := context.Background()
	rev := func(r, date string) testutil.Text {
		return testutil.Text{X: 60, Y: 760, Size: 7, S: "SLVSZZ1" + r + " - JANUARY 2020 - REVISED " + date}
	}
	var mu sync.Mutex
	current := testutil.MiniWith("ZX7001", "Step-Down Converter", "4.5", "28", "2", "40", rev("A", "MAY 2021"))
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		w.Write(current)
	}))
	defer srv.Close()
	// Part-number lookups (for datasheets without a URL) stay offline.
	saved := datasheetURLs
	datasheetURLs = nil
	defer func() { datasheetURLs = saved }()
	res, err := s.Import(ctx, ImportRequest{URL: srv.URL + "/zx7001.pdf"})
	if err != nil {
		t.Fatal(err)
	}
	id := res.Datasheet.ID
	if res.Datasheet.Meta["revision"] != "SLVSZZ1 rev A (MAY 2021)" {
		t.Errorf("revision = %q", res.Datasheet.Meta["revision"])
	}
	c, err := s.CheckDatasheetUpdate(ctx, id)
	if err != nil || !c.UpToDate {
		t.Fatalf("same PDF = %+v %v", c, err)
	}
	if _, err := s.ApplyDatasheetUpdate(ctx, id); err == nil {
		t.Error("nothing to apply when up to date")
	}
	// A user-recorded value and a project survive the update.
	part, _ := s.ResolvePart(ctx, "ZX7001")
	if _, err := s.Lib.RecordParameters(ctx, id, []library.Parameter{{Key: "fsw", Name: "Switching frequency", Typ: f64(600), Unit: "kHz", Section: "Electrical Characteristics"}}, library.SourceUser, false); err != nil {
		t.Fatal(err)
	}
	s.Lib.CreateProject(ctx, "p", "")
	s.AddToProject(ctx, "p", "ZX7001", library.ProjectPart{Designator: "U1"})

	mu.Lock()
	current = testutil.MiniWith("ZX7001", "Step-Down Converter", "4.5", "36", "2", "", rev("B", "JUNE 2024"))
	mu.Unlock()
	c, err = s.CheckDatasheetUpdate(ctx, id)
	if err != nil || c.UpToDate {
		t.Fatalf("new revision = %+v %v", c, err)
	}
	if c.Current != "SLVSZZ1 rev A (MAY 2021)" || c.Latest != "SLVSZZ1 rev B (JUNE 2024)" || c.Recorded != 1 {
		t.Errorf("check = %+v", c)
	}
	if c.Counts[ChangeChanged] != 1 || c.Counts[ChangeRemoved] != 1 || c.Changes[0].Key != "vin" || !strings.Contains(c.Changes[0].After, "36") {
		t.Errorf("changes = %+v %v", c.Changes, c.Counts)
	}
	up, err := s.ApplyDatasheetUpdate(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if up.Datasheet.ID != id || up.Datasheet.Meta["revision"] != "SLVSZZ1 rev B (JUNE 2024)" || up.Datasheet.Meta["previousRevision"] != "SLVSZZ1 rev A (MAY 2021)" {
		t.Errorf("updated = %+v", up.Datasheet)
	}
	vals, _ := s.partValues(ctx, part.Part)
	if v := vals["vin"]; v.Max == nil || *v.Max != 36 {
		t.Errorf("new values in use: %+v", v)
	}
	if v := vals["fsw"]; v.Source != library.SourceUser {
		t.Errorf("recorded values kept: %+v", v)
	}
	if d, _ := s.ProjectDetail(ctx, "p"); len(d.Parts) != 1 {
		t.Error("project entry kept")
	}
	if all, err := s.CheckAllUpdates(ctx); err != nil || len(all) != 2 {
		t.Errorf("check all = %+v %v", all, err)
	}
}

func TestDiffParams(t *testing.T) {
	old := []library.Parameter{
		{Key: "vin", Name: "Input voltage", Section: "Recommended", Min: f64(4.5), Max: f64(28), Unit: "V"},
		{Name: "Line regulation", Section: "Electrical", Typ: f64(0.1), Unit: "%"},
		{Key: "iq", Name: "Quiescent current", Section: "Electrical", Typ: f64(40), Unit: "µA"},
	}
	fresh := []extract.Param{
		{Key: "vin", Name: "Input  voltage", Section: "Recommended", Min: f64(4500), Max: f64(28000), Unit: "mV"},
		{Name: "Line regulation", Section: "Electrical", Typ: f64(0.2), Unit: "%"},
		{Key: "fsw", Name: "Switching frequency", Section: "Electrical", Typ: f64(1.2), Unit: "MHz"},
	}
	ch, counts := diffParams(old, fresh)
	if counts[ChangeChanged] != 1 || counts[ChangeAdded] != 1 || counts[ChangeRemoved] != 1 || len(ch) != 3 {
		t.Fatalf("changes = %+v", ch)
	}
	// Keyed rows first; equal values in other units are no change.
	if ch[0].Key != "iq" || ch[0].Kind != ChangeRemoved || ch[1].Key != "fsw" || ch[2].Name != "Line regulation" {
		t.Errorf("order = %+v", ch)
	}
}

func TestReextractOutdated(t *testing.T) {
	s := newService(t)
	ctx := context.Background()
	if jobs, err := s.ReextractOutdated(ctx); err != nil || len(jobs) != 0 {
		t.Fatalf("a current library needs nothing: %v %v", jobs, err)
	}
	r, _ := s.ResolvePart(ctx, "SD1234")
	if err := s.Lib.SetExtractor(ctx, r.Part.DatasheetID, "0"); err != nil {
		t.Fatal(err)
	}
	jobs, err := s.ReextractOutdated(ctx)
	if err != nil || len(jobs) != 1 || jobs[0].Name != "sd1234.pdf" {
		t.Fatalf("jobs = %+v %v", jobs, err)
	}
	deadline := time.Now().Add(30 * time.Second)
	for {
		if d, _ := s.Lib.Datasheet(ctx, r.Part.DatasheetID); d.Extractor == extract.Version {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("re-extraction didn't finish")
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func TestCatalogue(t *testing.T) {
	s := newService(t)
	ctx := context.Background()
	saved := datasheetURLs
	datasheetURLs = nil // manufacturer lookups stay offline
	defer func() { datasheetURLs = saved }()

	var files map[string][]byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/zx7001.pdf":
			w.Write(testutil.Mini("ZX7001", "Step-Down Converter"))
		case r.URL.Path == "/zx7003.pdf":
			w.Write(testutil.Mini("ZX7003", "Step-Down Converter"))
		case strings.HasPrefix(r.URL.Path, "/data/"):
			b, ok := files[strings.TrimPrefix(r.URL.Path, "/data/")]
			if !ok {
				http.NotFound(w, r)
				return
			}
			w.Header().Set("Last-Modified", "Thu, 01 Jan 2026 03:00:00 GMT")
			w.Header().Set("Content-Length", fmt.Sprint(len(b)))
			if r.Method == http.MethodGet {
				w.Write(b)
			}
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	rows := [][]any{
		{1, 1, "Power Management (PMIC)", "DC-DC Converters", "ZX7001DBVR", "SOT-23-6", "Zed Semi", "base", 0, "2A 4.5V~28V Buck", srv.URL + "/zx7001.pdf", 1200, "1-:0.31", `{"Output Current":"2A"}`},
		{3, 1, "Power Management (PMIC)", "DC-DC Converters", "ZX7003DR", "SOIC-8", "Zed Semi", "expand", 0, "2A Buck", srv.URL + "/zx7003.pdf", 0, "", `{}`},
	}
	db, err := testutil.JLCDatabase(t.TempDir(), "source-db-v2", rows)
	if err != nil {
		t.Fatal(err)
	}
	if files, err = testutil.SplitZip(db, 700); err != nil {
		t.Fatal(err)
	}
	s.Catalog.Source = srv.URL + "/data/"

	if _, err := s.CatalogSearch(ctx, catalog.Query{Text: "zx"}); err == nil {
		t.Error("searching needs a download")
	}
	if err := s.StartCatalogDownload(); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(30 * time.Second)
	for st := s.CatalogStatus(ctx); !st.Installed || st.Updating; st = s.CatalogStatus(ctx) {
		if st.Error != "" || time.Now().After(deadline) {
			t.Fatalf("download: %+v", st)
		}
		time.Sleep(20 * time.Millisecond)
	}
	st := s.CatalogStatus(ctx)
	if st.Parts != 2 || st.InStock != 1 || !st.Stale || st.RemindDays != 14 || st.AgeDays < 200 {
		t.Errorf("status = %+v", st)
	}
	never := s.Config.Get().Settings
	never.CatalogRemindDays = -1
	s.Config.UpdateSettings(never)
	if st := s.CatalogStatus(ctx); st.Stale || st.RemindDays != 0 {
		t.Errorf("reminders off: %+v", st)
	}

	// Pick a part and import its datasheet; the orderable code resolves.
	jobs, err := s.CatalogImport(ctx, []string{"C1"})
	if err != nil || len(jobs) != 1 {
		t.Fatalf("import = %+v %v", jobs, err)
	}
	waitJobs(t, s)
	if r, err := s.ResolvePart(ctx, "ZX7001DBVR"); err != nil || r.Part.MPN != "ZX7001" {
		t.Errorf("orderable code = %+v %v", r.Part, err)
	}
	items, err := s.CatalogSearch(ctx, catalog.Query{Text: "zx70 buck"})
	if err != nil || len(items) != 2 || items[0].InLibrary != "ZX7001" || items[1].InLibrary != "" {
		t.Errorf("search = %+v %v", items, err)
	}

	// Part-number lookups fall back to the catalogue's datasheet link,
	// and a JLCPCB BOM with only LCSC codes resolves through it.
	res, err := s.ImportBOM(ctx, BOMRequest{Data: []byte("Designator,Comment,LCSC Part #\nU1,buck,C3\nU2,buck,C1\nR1,10k,\n"), Filename: "jlc.csv", Fetch: true})
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]BOMRow{}
	for _, r := range res.Rows {
		got[strings.Join(r.Designators, ",")] = r
	}
	if r := got["U1"]; r.MPN != "ZX7003DR" || r.Status != BOMImported || r.Part != "ZX7003" || r.JLC == nil || r.JLC.Library != "extended" {
		t.Errorf("U1 = %+v jlc=%+v", r, r.JLC)
	}
	if r := got["U2"]; r.Status != BOMInLibrary || r.JLC == nil || r.JLC.LCSC != "C1" || r.JLC.Stock != 1200 {
		t.Errorf("U2 = %+v", r)
	}
	if r := got["R1"]; r.Status != BOMSkipped {
		t.Errorf("R1 = %+v", r)
	}
}

// waitJobs waits for the import queue to drain.
func waitJobs(t *testing.T, s *Service) {
	t.Helper()
	deadline := time.Now().Add(30 * time.Second)
	for {
		busy := false
		for _, j := range s.ImportJobs() {
			busy = busy || j.Status == JobQueued || j.Status == JobRunning
		}
		if !busy {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("imports didn't finish: %+v", s.ImportJobs())
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func TestSameDocumentFromAnotherURL(t *testing.T) {
	s := newService(t)
	ctx := context.Background()
	rev := func(r string, extra string) []byte {
		return testutil.MiniWith("ZX7001", "Step-Down Converter", "4.5", "28", "2", "", testutil.Text{X: 60, Y: 760, Size: 7, S: "SLVSZZ1" + r + " - JANUARY 2020 - REVISED MAY 2021"}, testutil.Text{X: 60, Y: 780, Size: 6, S: extra})
	}
	first, err := s.Import(ctx, ImportRequest{Data: rev("A", "www.ti.com"), Filename: "zx7001.pdf"})
	if err != nil {
		t.Fatal(err)
	}
	// The same revision with a different footer (another site's copy).
	again, err := s.Import(ctx, ImportRequest{Data: rev("A", "www.ti.com.cn"), Filename: "zx7001-cn.pdf", Orderable: []string{"ZX7001DBVR"}})
	if err != nil || !again.Existing || again.Datasheet.ID != first.Datasheet.ID || len(again.Parts) != 1 {
		t.Fatalf("same document = %+v %v", again, err)
	}
	if r, err := s.ResolvePart(ctx, "ZX7001DBVR"); err != nil || r.Part.MPN != "ZX7001" {
		t.Errorf("orderable kept: %+v %v", r, err)
	}
	// Another revision updates the datasheet in place.
	newer, err := s.Import(ctx, ImportRequest{Data: rev("B", "x"), Filename: "zx7001b.pdf"})
	if err != nil || newer.Existing || newer.Datasheet.ID != first.Datasheet.ID || newer.Datasheet.Meta["revisionRev"] != "B" || newer.Datasheet.Meta["previousRevision"] == "" {
		t.Fatalf("new revision = %+v %v", newer.Datasheet, err)
	}
	if ds, _ := s.Lib.Datasheets(ctx); len(ds) != 2 {
		t.Errorf("datasheets = %d (the sample plus ZX7001)", len(ds))
	}
}

func TestReextractDatasheetWithDocumentNumber(t *testing.T) {
	s := newService(t)
	ctx := context.Background()
	data := testutil.MiniWith("ZX7001", "Step-Down Converter", "4.5", "28", "2", "", testutil.Text{X: 60, Y: 760, Size: 7, S: "SLVSZZ1A - JANUARY 2020 - REVISED MAY 2021"})
	res, err := s.Import(ctx, ImportRequest{Data: data, Filename: "zx7001.pdf"})
	if err != nil || res.Datasheet.Meta["revisionDoc"] != "SLVSZZ1" {
		t.Fatalf("import = %+v %v", res.Datasheet.Meta, err)
	}
	id := res.Datasheet.ID
	if err := s.Lib.SetExtractor(ctx, id, "0"); err != nil {
		t.Fatal(err)
	}
	again, err := s.Reextract(ctx, id)
	if err != nil || again.Existing || again.Datasheet.ID != id {
		t.Fatalf("re-extract was skipped as a duplicate: %+v %v", again, err)
	}
	if d, _ := s.Lib.Datasheet(ctx, id); d.Extractor != extract.Version {
		t.Errorf("extractor = %q", d.Extractor)
	}
}

func TestDeletePartWithItsDatasheet(t *testing.T) {
	s := newService(t)
	ctx := context.Background()
	r, _ := s.ResolvePart(ctx, "SD1234")
	dsID := r.Part.DatasheetID
	d, _ := s.Lib.Datasheet(ctx, dsID)
	pdfPath := s.Lib.DatasheetPath(d.File)
	if _, err := os.Stat(pdfPath); err != nil {
		t.Fatal(err)
	}
	// Recorded data, notes and a project use.
	if _, err := s.Lib.RecordParameters(ctx, dsID, []library.Parameter{{Key: "fsw", Name: "Switching frequency", Typ: f64(600), Unit: "kHz", Section: "Electrical Characteristics"}}, library.SourceUser, false); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Lib.AddNote(ctx, library.Note{TargetKind: library.TargetPart, TargetID: "SD1235", Body: "check thermal"}, dsID); err != nil {
		t.Fatal(err)
	}
	s.Lib.CreateProject(ctx, "board", "")
	s.AddToProject(ctx, "board", "SD1235", library.ProjectPart{Designator: "U1"})

	plan, err := s.DeletePreview(ctx, "SD1234DDCR")
	if err != nil {
		t.Fatal(err)
	}
	if plan.Datasheet == nil || len(plan.Parts) != 2 || plan.Recorded["parameters"] != 1 || plan.Notes != 1 || !plan.InProjects {
		t.Fatalf("plan = %+v", plan)
	}
	for _, u := range plan.Parts {
		if u.Part.MPN == "SD1235" && (len(u.Projects) != 1 || u.Projects[0] != "board") {
			t.Errorf("SD1235 projects = %v", u.Projects)
		}
	}

	// Keep the project's part: it stays without a datasheet.
	res, err := s.DeletePart(ctx, "SD1234", true)
	if err != nil {
		t.Fatal(err)
	}
	if res.Datasheet == "" || strings.Join(res.Deleted, ",") != "SD1234" || strings.Join(res.Kept, ",") != "SD1235" {
		t.Errorf("result = %+v", res)
	}
	if _, err := s.Lib.Datasheet(ctx, dsID); err == nil {
		t.Error("the datasheet is gone")
	}
	if _, err := os.Stat(pdfPath); !os.IsNotExist(err) {
		t.Error("the PDF is removed")
	}
	if _, err := s.ResolvePart(ctx, "SD1234"); err == nil {
		t.Error("SD1234 is gone")
	}
	kept, err := s.ResolvePart(ctx, "SD1235")
	if err != nil || kept.Part.DatasheetID != 0 {
		t.Fatalf("SD1235 kept as a placeholder: %+v %v", kept.Part, err)
	}
	if d, _ := s.ProjectDetail(ctx, "board"); len(d.Parts) != 1 || d.Parts[0].HasDatasheet {
		t.Errorf("project keeps it without a datasheet: %+v", d.Parts)
	}

	// Deleting the placeholder removes it from the project and its notes.
	plan, _ = s.DeletePreview(ctx, "SD1235")
	if plan.Datasheet != nil || len(plan.Parts) != 1 || plan.Notes != 1 {
		t.Errorf("placeholder plan = %+v", plan)
	}
	if _, err := s.DeletePart(ctx, "SD1235", false); err != nil {
		t.Fatal(err)
	}
	if d, _ := s.ProjectDetail(ctx, "board"); len(d.Parts) != 0 {
		t.Errorf("removed from the project: %+v", d.Parts)
	}
	if n, _ := s.Lib.CountNotesOf(ctx, library.TargetPart, "SD1235"); n != 0 {
		t.Errorf("notes left: %d", n)
	}
	if _, err := s.DeletePart(ctx, "nope", false); err == nil {
		t.Error("unknown parts are an error")
	}
}
