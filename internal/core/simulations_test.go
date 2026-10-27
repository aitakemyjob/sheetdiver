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
	// A netlist that would read files or run commands is refused.
	if _, err := s.SaveSimulation(ctx, "board", SimulationSpec{Name: "bad", Netlist: "t\n.include /etc/passwd\n.op\n"}, library.SourceAgent); err == nil || !strings.Contains(err.Error(), ".include isn't allowed") {
		t.Errorf("bad netlist: %v", err)
	}
	// A model the library doesn't have is named.
	if _, err := s.SaveSimulation(ctx, "board", SimulationSpec{Name: "m", Netlist: rcNetlist, Models: []library.SimModel{{Part: "NOPE", Name: "x"}}}, library.SourceAgent); err == nil {
		t.Error("a missing model was accepted")
	}
	sim, err := s.SaveSimulation(ctx, "board", SimulationSpec{Name: "RC step", Kind: "step response", Purpose: "How fast the filter settles", Netlist: rcNetlist,
		Variants: []library.SimVariant{{Name: "1k"}, {Name: "100R", Params: map[string]string{"rval": "100"}}}}, library.SourceAgent)
	if err != nil {
		t.Fatal(err)
	}
	if len(sim.Variants) != 2 {
		t.Fatalf("variants %+v", sim.Variants)
	}
	if _, err := exec.LookPath("ngspice"); err != nil {
		t.Skip("ngspice isn't installed")
	}
	runs, err := s.RunSimulation(ctx, "board", "rc step", nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range runs {
		if r.State != "done" || len(r.Plots) != 2 || r.Plots[0].ID != "tran1" || r.Measures[0].Value == nil {
			t.Fatalf("run %s: %+v", r.Variant, r)
		}
	}
	// The presentation is checked against what the runs wrote.
	min := 3.0
	pres := library.Presentation{Summary: "The 100 Ω version charges fully.", Plots: []library.SimPlot{{Title: "Step", Plot: "tran1", Signals: []string{"v(out)"}}}, Checks: []library.SimCheck{{Measure: "vmax", Min: &min, Unit: "V"}}}
	bad := pres
	bad.Plots = []library.SimPlot{{Title: "x", Plot: "tran1", Signals: []string{"v(nowhere)"}}}
	if _, err := s.PresentSimulation(ctx, "board", "RC step", bad); err == nil || !strings.Contains(err.Error(), "has no v(nowhere)") {
		t.Errorf("a chart of a missing signal: %v", err)
	}
	d, err := s.PresentSimulation(ctx, "board", "RC step", pres)
	if err != nil {
		t.Fatal(err)
	}
	pass := map[string]bool{}
	for _, c := range d.Checks {
		if c.Pass != nil {
			pass[c.Variant] = *c.Pass
		}
	}
	if pass["1k"] || !pass["100R"] {
		t.Errorf("checks %+v", d.Checks)
	}
	list, err := s.Simulations(ctx, "board")
	if err != nil || len(list) != 1 || list[0].Pass != 1 || list[0].Fail != 1 || !list[0].Presented || list[0].States["100R"] != "done" {
		t.Errorf("list %+v %v", list, err)
	}
	// Chart data: the step, thinned; the AC response in dB and phase.
	data, err := s.SimulationData(ctx, runs[0].ID, "tran1", []string{"v(out)", "v(in)"}, "", 100)
	if err != nil || len(data.Series) != 2 || len(data.Series[0].X) != 100 || data.XUnit != "s" {
		t.Errorf("tran data: %v %+v", err, data.XName)
	}
	ac, err := s.SimulationData(ctx, runs[0].ID, "ac1", []string{"v(out)"}, "phase", 0)
	if err != nil || ac.Series[0].Unit != "°" || ac.XUnit != "Hz" {
		t.Errorf("ac data: %v %+v", err, ac.Series)
	}
	// Changing the simulation makes its runs stale.
	if _, err := s.SaveSimulation(ctx, "board", SimulationSpec{Name: "RC step", Netlist: rcNetlist}, library.SourceAgent); err != nil {
		t.Fatal(err)
	}
	d, _ = s.SimulationDetail(ctx, "board", "RC step")
	if d.Latest[0].State != "stale" || d.Simulation.Presentation == nil {
		t.Errorf("after a change: %s, presentation kept %v", d.Latest[0].State, d.Simulation.Presentation != nil)
	}
	// Deleting it removes its runs' files.
	if err := s.DeleteSimulation(ctx, "board", "RC step"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(s.Lib.RunDir(runs[0].ID)); !os.IsNotExist(err) {
		t.Errorf("run folder left: %v", err)
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
