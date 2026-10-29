package core

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/aitakemyjob/sheetdiver/internal/library"
	"github.com/aitakemyjob/sheetdiver/internal/testutil"
)

// exportFixture builds the Driver board with a little of everything a
// project holds besides its connections: a second entry of the same part
// with two designators, a calculation, a note, a SPICE model, a saved
// position and a test whose run has its folder.
func exportFixture(t *testing.T, s *Service) {
	t.Helper()
	ctx := context.Background()
	buildCircuit(t, s, circuit("Driver board"))
	p, err := s.Lib.Project(ctx, "Driver board")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.AddToProject(ctx, p.Name, "SD1234", library.ProjectPart{Designator: "U5, U6", Qty: 2, Role: "spare regulators"}); err != nil {
		t.Fatal(err)
	}
	part, err := s.Lib.Resolve(ctx, "SD1234")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Lib.SaveCalculation(ctx, library.Calculation{ProjectID: p.ID, PartID: part.Part.ID, Kind: "divider", Title: "VOUT divider", Inputs: json.RawMessage(`{"vout":3.3}`), Outputs: json.RawMessage(`{"r1":100000}`)}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Lib.AddNote(ctx, library.Note{TargetKind: library.TargetProject, TargetID: p.Name, Body: "check the motor's inrush", Author: library.SourceAgent}, 0); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Lib.SetSpiceModel(ctx, library.SpiceModel{PartID: part.Part.ID, Name: "SD1234", Dialect: "spice", Text: ".subckt SD1234 in out gnd\nR1 in out 1\n.ends\n", Source: library.SourceAgent}); err != nil {
		t.Fatal(err)
	}
	entries, err := s.Lib.ProjectParts(ctx, p.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Lib.SetPosition(ctx, p.ID, library.Position{Node: library.PartNode(entries[0].ID), X: 120, Y: 40}); err != nil {
		t.Fatal(err)
	}
	// A part on the test bench only: its model is in a variation, the
	// part isn't on the BOM.
	if _, err := s.Import(ctx, ImportRequest{Data: testutil.Mini("SD9001", "LDO"), Filename: "sd9001.pdf"}); err != nil {
		t.Fatal(err)
	}
	bench, err := s.Lib.Resolve(ctx, "SD9001")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Lib.SetSpiceModel(ctx, library.SpiceModel{PartID: bench.Part.ID, Name: "SD9001", Dialect: "spice", Text: ".subckt SD9001 a b\nR1 a b 1\n.ends\n", Source: library.SourceAgent}); err != nil {
		t.Fatal(err)
	}
	_, v, err := s.SaveVariation(ctx, p.Name, TestSpec{Name: "RC step", Kind: "step response", Purpose: "Does out settle?"}, VariationSpec{Name: "Rev A", Change: "first", Netlist: rcNetlist, Models: []library.SimModel{{Part: "SD9001", Name: "SD9001"}}, Conditions: []library.SimCondition{{Name: "room"}}}, library.SourceAgent)
	if err != nil {
		t.Fatal(err)
	}
	run, err := s.Lib.AddRun(ctx, v.ID, "room")
	if err != nil {
		t.Fatal(err)
	}
	run.State, run.Log = "done", "ngspice log"
	run.Measures = []library.RunMeasure{{Name: "vmax", Value: f64(3.29)}}
	if err := s.Lib.SaveRun(ctx, run); err != nil {
		t.Fatal(err)
	}
	dir := s.Lib.RunDir(run.ID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "results.raw"), []byte("raw data"), 0o644); err != nil {
		t.Fatal(err)
	}
}

// readProjectFile opens an exported project: project.json and the other
// files by name.
func readProjectFile(t *testing.T, data []byte) (ProjectFile, map[string]string) {
	t.Helper()
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatal(err)
	}
	files := map[string]string{}
	for _, f := range zr.File {
		rc, err := f.Open()
		if err != nil {
			t.Fatal(err)
		}
		b, _ := io.ReadAll(rc)
		rc.Close()
		files[f.Name] = string(b)
	}
	var pf ProjectFile
	if err := json.Unmarshal([]byte(files[projectJSON]), &pf); err != nil {
		t.Fatalf("project.json: %v", err)
	}
	return pf, files
}

func TestExportProject(t *testing.T) {
	s := newService(t)
	exportFixture(t, s)
	out, err := s.ExportProject(context.Background(), "driver board")
	if err != nil {
		t.Fatal(err)
	}
	if out.Filename != "Driver board.sdproj" {
		t.Errorf("filename = %q", out.Filename)
	}
	pf, files := readProjectFile(t, out.Data)
	if pf.Format != ProjectFileFormat || pf.Version != ProjectFileVersion || pf.Name != "Driver board" {
		t.Errorf("header: %+v", pf)
	}
	// Each part once, with where its datasheet came from: the BOM's, then
	// the test bench's.
	if len(pf.Parts) != 2 || pf.Parts[0].MPN != "SD1234" || pf.Parts[0].Datasheet == nil || len(pf.Parts[0].Datasheet.SHA256) != 64 || pf.Parts[0].Datasheet.Filename != "sd1234.pdf" || pf.Parts[1].MPN != "SD9001" {
		t.Fatalf("parts: %+v", pf.Parts)
	}
	// Entries by designator; connections, assignments and positions by
	// entry key and block name, never by library ID.
	keys := map[string]FileEntry{}
	for _, e := range pf.Entries {
		keys[e.Key] = e
	}
	if e, ok := keys["U5, U6"]; !ok || e.Qty != 2 || e.Part != "SD1234" {
		t.Errorf("the two-designator entry: %+v", pf.Entries)
	}
	if _, ok := keys["U1"]; !ok {
		t.Errorf("U1 by its designator: %+v", pf.Entries)
	}
	entryConn, blockConn := false, false
	for _, c := range pf.Connections {
		if c.Entry == "U1" && c.Pin != "" {
			entryConn = true
		}
		if c.Block != "" {
			blockConn = true
		}
		if c.Entry == "" && c.Block == "" {
			t.Errorf("a connection to nothing: %+v", c)
		}
	}
	if !entryConn || !blockConn || len(pf.Nets) == 0 || len(pf.Blocks) == 0 || len(pf.Rails) == 0 {
		t.Errorf("connections %d (entry %v, block %v), nets %d, blocks %d, rails %d", len(pf.Connections), entryConn, blockConn, len(pf.Nets), len(pf.Blocks), len(pf.Rails))
	}
	blockNames := map[string]bool{}
	for _, b := range pf.Blocks {
		blockNames[b.Name] = true
	}
	for _, a := range pf.Assignments {
		part, isPart := strings.CutPrefix(a.Node, "part:")
		block, isBlock := strings.CutPrefix(a.Node, "block:")
		key, _, _ := strings.Cut(part, "/")
		if isPart && keys[key].Key == "" || isBlock && !blockNames[block] || !isPart && !isBlock {
			t.Errorf("assignment node %q names no entry or block", a.Node)
		}
	}
	if len(pf.Positions) != 1 || pf.Positions[0].Node != "part:U1" || pf.Positions[0].X != 120 {
		t.Errorf("positions: %+v", pf.Positions)
	}
	var in bytes.Buffer
	if len(pf.Calculations) == 1 {
		_ = json.Compact(&in, pf.Calculations[0].Inputs)
	}
	if len(pf.Calculations) != 1 || pf.Calculations[0].Part != "SD1234" || in.String() != `{"vout":3.3}` {
		t.Errorf("calculations: %+v", pf.Calculations)
	}
	if len(pf.Notes) != 1 || pf.Notes[0].Body != "check the motor's inrush" {
		t.Errorf("notes: %+v", pf.Notes)
	}
	if len(pf.Models) != 2 || pf.Models[0].Part != "SD1234" || pf.Models[1].Part != "SD9001" || !strings.Contains(pf.Models[0].Text, ".subckt") {
		t.Errorf("models: %+v", pf.Models)
	}
	// The test with its variation, its run and the run's folder.
	if len(pf.Tests) != 1 || len(pf.Tests[0].Variations) != 1 || len(pf.Tests[0].Variations[0].Runs) != 1 {
		t.Fatalf("tests: %+v", pf.Tests)
	}
	r := pf.Tests[0].Variations[0].Runs[0]
	if r.State != "done" || r.Log != "ngspice log" || len(r.Measures) != 1 || r.Files != "runs/1" {
		t.Errorf("run: %+v", r)
	}
	if files["runs/1/results.raw"] != "raw data" {
		t.Errorf("the run's files: %v", keysOf(files))
	}
	// Nothing in the JSON is a library ID.
	if strings.Contains(files[projectJSON], `"id"`) || strings.Contains(files[projectJSON], `"projectId"`) {
		t.Error("project.json carries library IDs")
	}
	if _, err := s.ExportProject(context.Background(), "no such board"); err == nil {
		t.Error("a missing project exported")
	}
}

func TestEntryKeys(t *testing.T) {
	got := entryKeys([]library.ProjectPart{
		{ID: 1, MPN: "TPS63900", Designator: "U1"},
		{ID: 2, MPN: "TPS63900", Designator: ""},
		{ID: 3, MPN: "TPS63900", Designator: ""},
		{ID: 4, MPN: "LED", Designator: "D1"},
		{ID: 5, MPN: "R", Designator: "D1"},
		{ID: 6, MPN: "X", Designator: "R5, R6"},
	})
	want := map[int64]string{1: "U1", 2: "TPS63900#1", 3: "TPS63900#2", 4: "LED", 5: "R", 6: "R5, R6"}
	for id, k := range want {
		if got[id] != k {
			t.Errorf("entry %d: key %q, want %q", id, got[id], k)
		}
	}
}

// keysOf lists a map's keys, for messages.
func keysOf(m map[string]string) []string {
	var out []string
	for k := range m {
		out = append(out, k)
	}
	return out
}
