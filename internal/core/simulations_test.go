package core

import (
	"context"
	"os"
	"os/exec"
	"strings"
	"testing"

	"github.com/aitakemyjob/sheetdiver/internal/library"
)

// rcNetlist is an RC low-pass with a step and its frequency response.
const rcNetlist = `RC low-pass
.param rval=1k
V1 in 0 PULSE(0 3.3 0 1u 1u 1m 2m) AC 1
R1 in out {rval}
C1 out 0 1u
.tran 10u 3m
.ac dec 10 10 100k
.meas tran vmax MAX v(out)
.end
`

func TestSimulations(t *testing.T) {
	s := newService(t)
	ctx := context.Background()
	if _, err := s.Lib.CreateProject(ctx, "board", ""); err != nil {
		t.Fatal(err)
	}
	test := TestSpec{Name: "RC step", Kind: "step response", Purpose: "Does out reach 3 V within the pulse?"}
	// A netlist that would read files or run commands is refused.
	if _, _, err := s.SaveVariation(ctx, "board", test, VariationSpec{Name: "bad", Netlist: "t\n.include /etc/passwd\n.op\n"}, library.SourceAgent); err == nil || !strings.Contains(err.Error(), ".include isn't allowed") {
		t.Errorf("bad netlist: %v", err)
	}
	// A model the library doesn't have is named.
	if _, _, err := s.SaveVariation(ctx, "board", test, VariationSpec{Name: "m", Netlist: rcNetlist, Models: []library.SimModel{{Part: "NOPE", Name: "x"}}}, library.SourceAgent); err == nil {
		t.Error("a missing model was accepted")
	}
	hot := 85.0
	conds := []library.SimCondition{{Name: "room"}, {Name: "hot", Temp: &hot}}
	sim, revA, err := s.SaveVariation(ctx, "board", test, VariationSpec{Name: "Rev A", Change: "first draft: 1 kΩ", Netlist: rcNetlist, Conditions: conds}, library.SourceAgent)
	if err != nil {
		t.Fatal(err)
	}
	if sim.Kind != "step response" || len(revA.Conditions) != 2 {
		t.Fatalf("test %+v, variation %+v", sim, revA)
	}
	// The schematic changes: a second variation of the same test.
	revB := strings.Replace(rcNetlist, ".param rval=1k", ".param rval=100", 1)
	if _, _, err := s.SaveVariation(ctx, "board", TestSpec{Name: "rc step"}, VariationSpec{Name: "Rev B", Change: "R1 down to 100 Ω", Netlist: revB, Conditions: conds}, library.SourceAgent); err != nil {
		t.Fatal(err)
	}
	if _, err := exec.LookPath("ngspice"); err != nil {
		t.Skip("ngspice isn't installed")
	}
	var runs []library.SimRun
	for _, v := range []string{"Rev A", "Rev B"} {
		rs, err := s.RunVariation(ctx, "board", "RC step", v, nil)
		if err != nil {
			t.Fatal(err)
		}
		for _, r := range rs {
			if r.State != "done" || r.Variation != v || len(r.Plots) != 2 || r.Plots[0].ID != "tran1" || r.Measures[0].Value == nil {
				t.Fatalf("run %s/%s: %+v", v, r.Condition, r)
			}
		}
		runs = append(runs, rs...)
	}
	// The write-up is checked against what the variations' runs wrote.
	min := 3.0
	pres := library.Presentation{Summary: "Rev B charges fully.", Plots: []library.SimPlot{{Title: "Step", Plot: "tran1", Signals: []string{"v(out)"}, Variations: []string{"Rev A", "Rev B"}, Conditions: []string{"room"}}},
		Checks: []library.SimCheck{{Measure: "vmax", Min: &min, Unit: "V"}}, Notes: []library.VariationNote{{Variation: "Rev A", Text: "too slow"}}}
	for _, bad := range []library.Presentation{
		{Plots: []library.SimPlot{{Title: "x", Plot: "tran1", Signals: []string{"v(nowhere)"}}}},
		{Plots: []library.SimPlot{{Title: "x", Plot: "tran1", Signals: []string{"v(out)"}, Variations: []string{"Rev Z"}}}},
		{Notes: []library.VariationNote{{Variation: "Rev Z", Text: "?"}}},
		{Checks: []library.SimCheck{{Measure: "nope"}}},
	} {
		if _, err := s.PresentSimulation(ctx, "board", "RC step", bad); err == nil {
			t.Errorf("a write-up naming what isn't there was saved: %+v", bad)
		}
	}
	d, err := s.PresentSimulation(ctx, "board", "RC step", pres)
	if err != nil {
		t.Fatal(err)
	}
	// The targets read as a matrix: Rev A fails in both conditions, Rev B passes.
	got := map[string]bool{}
	for _, v := range d.Variations {
		for _, c := range v.Checks {
			if c.Pass != nil {
				got[c.Variation+"/"+c.Condition] = *c.Pass
			}
		}
	}
	if len(got) != 4 || got["Rev A/room"] || got["Rev A/hot"] || !got["Rev B/room"] || !got["Rev B/hot"] {
		t.Errorf("checks %v", got)
	}
	list, err := s.Simulations(ctx, "board")
	if err != nil || len(list) != 1 || list[0].Variations != 2 || list[0].Latest != "Rev B" || list[0].Pass != 2 || list[0].Fail != 0 || !list[0].Presented || list[0].States["hot"] != "done" {
		t.Errorf("list %+v %v", list, err)
	}
	// Chart data, by run: the step thinned, the AC response as phase.
	data, err := s.SimulationData(ctx, runs[0].ID, "tran1", []string{"v(out)", "v(in)"}, "", 100)
	if err != nil || len(data.Series) != 2 || len(data.Series[0].X) != 100 || data.XUnit != "s" || data.Variation != "Rev A" || data.Condition != "room" {
		t.Errorf("tran data: %v %+v", err, data)
	}
	ac, err := s.SimulationData(ctx, runs[0].ID, "ac1", []string{"v(out)"}, "phase", 0)
	if err != nil || ac.Series[0].Unit != "°" || ac.XUnit != "Hz" {
		t.Errorf("ac data: %v %+v", err, ac.Series)
	}
	// Fixing a variation's netlist makes its runs stale; the other
	// variation and the write-up stay.
	if _, _, err := s.SaveVariation(ctx, "board", test, VariationSpec{Name: "Rev A", Netlist: rcNetlist, Conditions: conds}, library.SourceAgent); err != nil {
		t.Fatal(err)
	}
	d, _ = s.SimulationDetail(ctx, "board", "RC step")
	if d.Variations[0].Latest[0].State != "stale" || d.Variations[1].Latest[0].State != "done" || d.Simulation.Presentation == nil {
		t.Errorf("after fixing Rev A: %s, %s, write-up kept %v", d.Variations[0].Latest[0].State, d.Variations[1].Latest[0].State, d.Simulation.Presentation != nil)
	}
	// Deleting a variation removes its runs' files; deleting the test, the rest.
	if err := s.DeleteVariation(ctx, "board", "RC step", "Rev A"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(s.Lib.RunDir(runs[0].ID)); !os.IsNotExist(err) {
		t.Errorf("Rev A's run folder left: %v", err)
	}
	if err := s.DeleteSimulation(ctx, "board", "RC step"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(s.Lib.RunDir(runs[3].ID)); !os.IsNotExist(err) {
		t.Errorf("Rev B's run folder left: %v", err)
	}
}

func TestSpiceModels(t *testing.T) {
	s := newService(t)
	ctx := context.Background()
	if _, err := s.Lib.AddPart(ctx, library.Part{MPN: "AO3400A", Category: "mosfet"}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.RecordSpiceModel(ctx, "AO3400A", library.SpiceModel{Name: "AO3400A", Text: ".model other NMOS"}, library.SourceAgent); err == nil {
		t.Error("a model not defining its name was saved")
	}
	m, err := s.RecordSpiceModel(ctx, "AO3400A", library.SpiceModel{Name: "AO3400A", Dialect: "pspice", Text: ".SUBCKT AO3400A D G S\nM1 D G S S NM\n.MODEL NM NMOS (VTO=1.2)\n.ENDS"}, library.SourceAgent)
	if err != nil || m.ID == 0 {
		t.Fatal(err)
	}
	got, err := s.models(ctx, []library.SimModel{{Part: "ao3400a", Name: "ao3400a"}})
	if err != nil || len(got) != 1 || !got[0].PSpice || !strings.HasSuffix(got[0].File, ".lib") {
		t.Errorf("models %+v %v", got, err)
	}
}
