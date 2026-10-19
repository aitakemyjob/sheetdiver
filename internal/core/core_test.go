package core

import (
	"context"
	"encoding/json"
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
