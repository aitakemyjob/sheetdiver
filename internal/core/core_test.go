package core

import (
	"context"
	"encoding/json"
	"math"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/aitakemyjob/sheetdiver/internal/calc"
	"github.com/aitakemyjob/sheetdiver/internal/config"
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
