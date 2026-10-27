package spice

import (
	"context"
	"math"
	"os/exec"
	"strings"
	"testing"
)

// rc is an RC low-pass with a step, a frequency response, an operating
// point and two measurements, one of which can't be met.
const rc = `RC low-pass
.param rval=1k
V1 in 0 PULSE(0 3.3 0 1u 1u 1m 2m) AC 1
R1 in out {rval}
C1 out 0 1u
.tran 10u 3m
.ac dec 10 10 100k
.op
.meas tran vmax MAX v(out)
.meas tran never TRIG v(out) VAL=5 RISE=1 TARG v(out) VAL=6 RISE=1
.end
`

func TestCheck(t *testing.T) {
	nl, err := Check(rc)
	if err != nil {
		t.Fatal(err)
	}
	var kinds []string
	for _, a := range nl.Analyses {
		kinds = append(kinds, a.Kind)
	}
	if strings.Join(kinds, ",") != "tran,ac,op" || strings.Join(nl.Measures, ",") != "vmax,never" || nl.Title != "RC low-pass" {
		t.Errorf("analyses %q, measures %q, title %q", kinds, nl.Measures, nl.Title)
	}
	for _, bad := range []struct{ netlist, want string }{
		{"t\nR1 a 0 1k\n.op\n.control\nshell rm -rf /\n.endc\n", ".control blocks aren't allowed"},
		{"t\n.include /etc/passwd\n.op\n", ".include isn't allowed"},
		{"t\n.lib /models/x.lib typ\n.op\n", ".lib with a file"},
		{"t\nA1 %v([a]) filesrc\n.model filesrc filesource (file=\"/etc/passwd\")\n.op\n", "naming files"},
		{"t\nR1 a 0 1k\n", "no analysis"},
		{"t\nR1 a 0 1k\n.op\n.shell ls\n", ".shell isn't allowed"},
		{"t\nV1 a 0 1\n.tran 1p 1\n", "at most 2000000"},
	} {
		if _, err := Check(bad.netlist); err == nil || !strings.Contains(err.Error(), bad.want) {
			t.Errorf("%q: %v, want %q", bad.netlist, err, bad.want)
		}
	}
}

func TestParseNumber(t *testing.T) {
	for s, want := range map[string]float64{"10u": 10e-6, "2.2meg": 2.2e6, "1k": 1e3, "3m": 3e-3, "1e-3": 1e-3, "5": 5, "100nF": 100e-9} {
		if got, err := ParseNumber(s); err != nil || math.Abs(got-want) > want*1e-9 {
			t.Errorf("%s: %v %v, want %v", s, got, err, want)
		}
	}
}

func TestVariant(t *testing.T) {
	nl, _ := Check(rc)
	hot := 85.0
	deck, err := Deck(nl, Variant{Name: "10k hot", Params: map[string]string{"rval": "10k", "cload": "{2*1u}"}, Temp: &hot}, []string{"m1.lib"})
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{".param rval=10k", ".param cload={2*1u}", ".temp 85", `.include "m1.lib"`, "setplot tran1\nwrite results.raw", "setplot ac1", "setplot op1", ".endc\n.end\n"} {
		if !strings.Contains(deck, want) {
			t.Errorf("deck lacks %q:\n%s", want, deck)
		}
	}
	if _, err := Deck(nl, Variant{Params: map[string]string{"r": "1k\n.control"}}, nil); err == nil {
		t.Error("a value with a line break got in")
	}
	if got := PlotIDs([]Analysis{{Kind: "tran"}, {Kind: "noise"}, {Kind: "tran"}}); strings.Join(got, ",") != "tran1,noise1,noise2,tran2" {
		t.Errorf("plots %q", got)
	}
}

func TestParseMeasures(t *testing.T) {
	log := "vmax                =  2.368299e+00 at=  3.000000e-03\nError: measure  never  trig(TRIG) : out of interval\n .meas tran never trig v(out) val=5 rise=1 targ v(out) val=6 rise=1 failed!\ntrise = 1.5e-03 targ= 2e-3 trig= 5e-4\n"
	got := ParseMeasures(log, []string{"vmax", "never", "trise", "gone"})
	if got[0].Value == nil || *got[0].Value != 2.368299 || got[0].At == nil || *got[0].At != 3e-3 {
		t.Errorf("vmax: %+v", got[0])
	}
	if got[1].Value != nil || !strings.Contains(got[1].Error, "out of interval") {
		t.Errorf("never: %+v", got[1])
	}
	if got[2].Value == nil || *got[2].Value != 1.5e-3 {
		t.Errorf("trise: %+v", got[2])
	}
	if got[3].Error == "" {
		t.Errorf("gone: %+v", got[3])
	}
}

func TestStates(t *testing.T) {
	if s, err := Queued.To(Running); err != nil || s != Running {
		t.Errorf("queued → running: %v", err)
	}
	if _, err := Done.To(Running); err == nil {
		t.Error("a done run started again")
	}
	if !Failed.Finished() || Running.Finished() {
		t.Error("finished")
	}
}

func TestDownsample(t *testing.T) {
	x, y := make([]float64, 10000), make([]float64, 10000)
	for i := range x {
		x[i] = float64(i)
	}
	y[5000] = 7 // a spike survives
	ox, oy := Downsample(x, y, 200)
	if len(ox) != 200 || ox[0] != 0 || ox[199] != 9999 {
		t.Fatalf("%d points, %v…%v", len(ox), ox[0], ox[len(ox)-1])
	}
	max := 0.0
	for _, v := range oy {
		max = math.Max(max, v)
	}
	if max != 7 {
		t.Error("the spike was lost")
	}
}

// TestRunInNgspice runs the RC circuit in ngspice, twice, and reads the
// results back. It needs ngspice.
func TestRunInNgspice(t *testing.T) {
	if _, err := exec.LookPath("ngspice"); err != nil {
		t.Skip("ngspice isn't installed")
	}
	inst, err := Locate(context.Background(), "")
	if err != nil || inst.Version == "" {
		t.Fatalf("locate: %+v %v", inst, err)
	}
	nl, err := Check(rc)
	if err != nil {
		t.Fatal(err)
	}
	vmax := map[string]float64{}
	for _, v := range []Variant{{Name: "1k"}, {Name: "100R", Params: map[string]string{"rval": "100"}}} {
		res := Runner{Install: inst}.Run(context.Background(), Job{Dir: t.TempDir(), Netlist: nl, Variant: v})
		if res.Err != nil {
			t.Fatalf("%s: %v\n%s", v.Name, res.Err, res.Log)
		}
		var names []string
		for _, p := range res.Plots {
			names = append(names, p.Name)
		}
		if strings.Join(names, ",") != "Transient Analysis,AC Analysis,Operating Point" || res.Plots[1].ID != "ac1" {
			t.Errorf("plots %q", names)
		}
		if res.Measures[0].Value == nil || res.Measures[1].Error == "" {
			t.Errorf("measures %+v", res.Measures)
		} else {
			vmax[v.Name] = *res.Measures[0].Value
		}
		// The AC response: 0 dB at 10 Hz, -3 dB near 1/(2πRC).
		ac := res.Plots[1]
		s, err := ac.Series("v(out)", "mag", 1000)
		if err != nil || !ac.Complex || math.Abs(s.Y[0]) > 0.1 || s.Unit != "dB" {
			t.Errorf("ac: %v %+v", err, s.Y[:3])
		}
	}
	// A smaller resistor charges the capacitor further in a 1 ms pulse.
	if !(vmax["100R"] > vmax["1k"]) || vmax["100R"] < 3.2 {
		t.Errorf("vmax %v", vmax)
	}
}

func TestLocateConfigured(t *testing.T) {
	if _, err := Locate(context.Background(), "/nowhere/ngspice"); err == nil {
		t.Error("a missing configured path was accepted")
	}
	if fs := Finders(""); len(fs) != 2 {
		t.Errorf("finders %d", len(fs))
	}
}

func TestReadRawText(t *testing.T) {
	raw := "Title: t\nDate: x\nPlotname: AC Analysis\nFlags: complex\nNo. Variables: 2\nNo. Points: 2\nVariables:\n\t0\tfrequency\tfrequency\n\t1\tv(out)\tvoltage\nValues:\n0\t10,0\n\t1,0\n\n1\t100,0\n\t0.5,-0.5\n"
	plots, err := ReadRaw([]byte(raw))
	if err != nil || len(plots) != 1 {
		t.Fatalf("%v %d", err, len(plots))
	}
	p := plots[0]
	if !p.Complex || p.Vectors[0].Re[1] != 100 || p.Vectors[1].Re[1] != 0.5 || p.Vectors[1].Im[1] != -0.5 {
		t.Errorf("plot %+v", p.Vectors)
	}
}
