package api

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
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
	// The library folder and platform differ by OS (Windows paths, macOS's
	// symlinked temporary folder).
	libraryDirRe = regexp.MustCompile(`"libraryDir":"[^"]+"`)
	platformRe   = regexp.MustCompile(`"platform":"[^"]*"`)
	// The test binary's path.
	commandRe = regexp.MustCompile(`"command":"[^"]*"`)
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
	v12, v33, im, oth := 12.0, 3.3, 0.5, 0.05
	for _, r := range []library.Rail{{ProjectID: proj.ID, Name: "VIN", VNom: &v12}, {ProjectID: proj.ID, Name: "3V3", VNom: &v33, OtherTyp: &oth, OtherMax: &oth}} {
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
	bus := "I2C1"
	if _, err := c.ChangeConnections(ctx, "Sensor board", core.ConnectionsChange{
		Blocks:  []core.BlockChange{{Name: "Battery"}},
		Nets:    []core.NetChange{{Name: "SDA", Bus: &bus}, {Name: "VIN", Kind: library.NetPower}},
		Connect: []core.ConnectChange{{Net: "VIN", Part: "Battery"}, {Net: "3V3", Part: "U2", Pin: "VIN"}, {Net: "GND", Part: "U2", Pin: "GND"}, {Net: "SDA", Part: "U2", Pin: "VFB"}, {Net: "SDA", Part: "U1"}},
	}, library.SourceUser); err != nil {
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
		{"compare", map[string]any{"parts": []string{"SD1234", "TPX5430"}}},
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
	// The block diagram's test circuits, for the layout tests: built after
	// the calls above, so they aren't in projects.list (whose order by
	// update time would then depend on how fast the machine is).
	for _, cir := range testutil.Circuits() {
		buildCircuit(t, c, cir)
		raw, _ := json.Marshal(map[string]any{"project": cir.Name})
		res, err := a.Invoke(ctx, "project.detail", raw)
		if err != nil {
			t.Fatalf("%s: %v", cir.Name, err)
		}
		out = append(out, fixture{Method: "project.detail", Args: raw, Result: normalize(res, filepath.Dir(os.Getenv("SHEETDIVER_LIBRARY")))})
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
		t.Fatalf("%s is stale (the API changed): run go test ./internal/api -run TestFrontendFixtures -update\n%s", fixturesFile, firstDiff(string(want), string(got)))
	}
}

// normalize makes a response the same everywhere: fixed times and dates,
// the test's temporary folder as /tmp/sheetdiver, the platform as linux
// and the executable as /opt/sheetdiver/sheetdiver.
func normalize(res json.RawMessage, dir string) json.RawMessage {
	s := timeRe.ReplaceAllString(string(res), `"2026-01-01T00:00:00Z"`)
	s = dateRe.ReplaceAllString(s, "2026-01-01")
	s = libraryDirRe.ReplaceAllString(s, `"libraryDir":"/tmp/sheetdiver/lib"`)
	s = platformRe.ReplaceAllString(s, `"platform":"linux"`)
	s = commandRe.ReplaceAllString(s, `"command":"/opt/sheetdiver/sheetdiver"`)
	if dir != "" && dir != "." {
		s = strings.ReplaceAll(s, dir, "/tmp/sheetdiver")
	}
	return json.RawMessage(s)
}

// firstDiff shows the first line where two texts differ.
func firstDiff(want, got string) string {
	w, g := strings.Split(want, "\n"), strings.Split(got, "\n")
	for i := 0; i < len(w) || i < len(g); i++ {
		var a, b string
		if i < len(w) {
			a = w[i]
		}
		if i < len(g) {
			b = g[i]
		}
		if a != b {
			return fmt.Sprintf("line %d:\n  recorded: %s\n  now:      %s", i+1, a, b)
		}
	}
	return ""
}

// buildCircuit puts a test circuit in a project, as an agent would.
func buildCircuit(t *testing.T, c *core.Service, cir testutil.Circuit) {
	t.Helper()
	ctx := context.Background()
	p, err := c.Lib.CreateProject(ctx, cir.Name, "")
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range cir.Rails {
		v := r.V
		if _, err := c.Lib.SetRail(ctx, library.Rail{ProjectID: p.ID, Name: r.Name, VNom: &v, Source: r.Source}); err != nil {
			t.Fatal(err)
		}
	}
	var ch core.ConnectionsChange
	for _, part := range cir.Parts {
		if part.Ref != "" {
			if _, err := c.AddToProject(ctx, cir.Name, part.Ref, library.ProjectPart{Designator: part.Name, SupplyRail: part.Supply, OutputRail: part.Output}); err != nil {
				t.Fatal(err)
			}
			continue
		}
		ch.Blocks = append(ch.Blocks, core.BlockChange{Name: part.Name, Kind: part.Kind, Value: part.Value})
	}
	for _, n := range cir.Nets {
		bus := n.Bus
		ch.Nets = append(ch.Nets, core.NetChange{Name: n.Name, Kind: n.Kind, Bus: &bus})
	}
	for _, pin := range cir.Pins {
		ch.Connect = append(ch.Connect, core.ConnectChange{Net: pin[0], Part: pin[1], Pin: pin[2]})
	}
	for _, g := range cir.Groups {
		desc, notes := g.Description, g.Notes
		ch.Groups = append(ch.Groups, core.GroupChange{Name: g.Name, Description: &desc, Notes: &notes})
	}
	for _, a := range cir.Assign {
		group, supports := a[1], a[2]
		ch.Assign = append(ch.Assign, core.AssignChange{Part: a[0], Group: &group, Supports: &supports})
	}
	for _, c := range cir.Cluster {
		cluster := c[1]
		ch.Assign = append(ch.Assign, core.AssignChange{Part: c[0], Cluster: &cluster})
	}
	if problems, err := c.ChangeConnections(ctx, cir.Name, ch, library.SourceUser); err != nil || len(problems) > 0 {
		t.Fatalf("%s: %v %q", cir.Name, err, problems)
	}
}
