package api

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/aitakemyjob/sheetdiver/internal/core"
	"github.com/aitakemyjob/sheetdiver/internal/library"
	"github.com/aitakemyjob/sheetdiver/internal/testutil"
)

// update rewrites the frontend's recorded API fixtures:
// go test ./internal/api -run TestFrontendFixtures -update
var update = flag.Bool("update", false, "rewrite frontend/src/test/fixtures.json")

// fixturesFile is where the frontend's view tests read recorded responses.
const fixturesFile = "../../frontend/src/test/fixtures.json"

// fixture is one recorded call: the frontend's fake backend answers a
// method called with these arguments (or, failing that, any call of the
// method) with this result.
type fixture struct {
	Method string          `json:"method"`
	Args   json.RawMessage `json:"args"`
	Result json.RawMessage `json:"result"`
}

// Volatile parts of responses: times and dates ("generated on
// 2026-10-19" in reports).
var (
	timeRe = regexp.MustCompile(`"\d{4}-\d\d-\d\dT\d\d:\d\d:\d\d(\.\d+)?(Z|[+-]\d\d:\d\d)"`)
	dateRe = regexp.MustCompile(`\b20\d\d-\d\d-\d\d\b`)
)

// TestFrontendFixtures records real responses for the calls the UI makes,
// over a small library (the synthetic datasheets and two projects), so
// the frontend's view tests run against what the backend actually sends
// (empty lists as [], not null). It fails when the recording is stale.
func TestFrontendFixtures(t *testing.T) {
	a, dsID := newAPI(t)
	ctx := context.Background()
	c := a.core
	if _, err := c.Import(ctx, core.ImportRequest{Data: testutil.Mini("TPX5430", "buck"), Filename: "tpx5430.pdf"}); err != nil {
		t.Fatal(err)
	}
	proj, err := c.Lib.CreateProject(ctx, "Sensor board", "Battery sensor node")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.Lib.CreateProject(ctx, "Empty board", ""); err != nil {
		t.Fatal(err)
	}
	v12, v33, im := 12.0, 3.3, 0.5
	for _, r := range []library.Rail{{ProjectID: proj.ID, Name: "VIN", VNom: &v12}, {ProjectID: proj.ID, Name: "3V3", VNom: &v33}} {
		if _, err := c.Lib.SetRail(ctx, r); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := c.AddToProject(ctx, "Sensor board", "TPX5430", library.ProjectPart{Designator: "U1", SupplyRail: "VIN", OutputRail: "3V3"}); err != nil {
		t.Fatal(err)
	}
	if _, err := c.AddToProject(ctx, "Sensor board", "SD1235", library.ProjectPart{Designator: "U2", SupplyRail: "3V3", IMax: &im}); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Calc(ctx, core.CalcRequest{Kind: "divider", Inputs: json.RawMessage(`{"vin": 3.3, "vout": 1.8}`), Project: "Sensor board", Save: true, Title: "ADC divider"}); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Lib.AddNote(ctx, library.Note{TargetKind: library.TargetPart, TargetID: "SD1234", Body: "Use the **SD1234** for the 3V3 rail."}, dsID); err != nil {
		t.Fatal(err)
	}
	var sections []library.Section
	var figures []library.Figure
	var tables []library.Table
	_ = invoke(t, a, "sections.list", map[string]any{"id": dsID}, &sections)
	_ = invoke(t, a, "figures.list", map[string]any{"datasheetId": dsID}, &figures)
	_ = invoke(t, a, "tables.list", map[string]any{"id": dsID}, &tables)
	if len(sections) == 0 || len(figures) == 0 {
		t.Fatal("the synthetic datasheet has sections and figures")
	}
	calls := []struct {
		method string
		args   any
	}{
		{"app.state", nil},
		{"activity.list", nil},
		{"imports.list", nil},
		{"catalog.status", nil},
		{"translate.status", nil},
		{"datasheets.list", nil},
		{"parts.list", map[string]any{}},
		{"parts.categories", nil},
		{"categories.list", nil},
		{"compare", map[string]any{"category": "buck"}},
		{"part.detail", map[string]any{"part": "SD1234"}},
		{"part.detail", map[string]any{"part": "TPX5430"}},
		{"part.alternatives", map[string]any{"part": "SD1234"}},
		{"datasheet.get", map[string]any{"id": dsID}},
		{"page.get", map[string]any{"datasheetId": dsID, "page": 0}},
		{"parameters.list", map[string]any{"datasheetId": dsID}},
		{"sections.list", map[string]any{"id": dsID}},
		{"section.get", map[string]any{"id": sections[0].ID}},
		{"figures.list", map[string]any{"datasheetId": dsID}},
		{"figure.get", map[string]any{"id": figures[0].ID}},
		{"tables.list", map[string]any{"id": dsID}},
		{"pins.list", map[string]any{"id": dsID}},
		{"registers.list", map[string]any{"id": dsID}},
		{"recommendations.list", map[string]any{"datasheetId": dsID}},
		{"search", map[string]any{"query": "switching frequency"}},
		{"notes.list", map[string]any{"kind": "part", "id": "SD1234"}},
		{"projects.list", nil},
		{"project.detail", map[string]any{"project": "Sensor board"}},
		{"project.detail", map[string]any{"project": "Empty board"}},
		{"project.report", map[string]any{"project": "Sensor board"}},
		{"calc.list", map[string]any{"projectId": 0}},
		{"delete.preview", map[string]any{"part": "SD1234"}},
	}
	kinds := make([]string, 0, len(core.CalcKinds))
	for k := range core.CalcKinds {
		kinds = append(kinds, k)
	}
	sort.Strings(kinds)
	for _, k := range kinds {
		calls = append(calls, struct {
			method string
			args   any
		}{"calc.fields", map[string]any{"kind": k}})
	}
	calls = append(calls, struct {
		method string
		args   any
	}{"calc.run", map[string]any{"kind": "divider", "inputs": map[string]any{"vin": 3.3, "vout": 1.8}}})
	var out []fixture
	for _, call := range calls {
		raw, _ := json.Marshal(call.args)
		if call.args == nil {
			raw = []byte("null")
		}
		res, err := a.Invoke(ctx, call.method, raw)
		if err != nil {
			t.Fatalf("%s: %v", call.method, err)
		}
		out = append(out, fixture{Method: call.method, Args: raw, Result: normalize(res, filepath.Dir(os.Getenv("SHEETDIVER_LIBRARY")))})
	}
	got, _ := json.MarshalIndent(out, "", " ")
	got = append(got, '\n')
	if *update {
		if err := os.MkdirAll(filepath.Dir(fixturesFile), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(fixturesFile, got, 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(fixturesFile)
	if err != nil || !bytes.Equal(want, got) {
		t.Fatalf("%s is stale (the API changed): run go test ./internal/api -run TestFrontendFixtures -update", fixturesFile)
	}
}

// normalize makes a response stable: fixed times, and the test's
// temporary folder as /tmp/sheetdiver.
func normalize(res json.RawMessage, dir string) json.RawMessage {
	s := timeRe.ReplaceAllString(string(res), `"2026-01-01T00:00:00Z"`)
	s = dateRe.ReplaceAllString(s, "2026-01-01")
	if dir != "" && dir != "." {
		s = strings.ReplaceAll(s, dir, "/tmp/sheetdiver")
	}
	return json.RawMessage(s)
}
