package cad

import (
	"math"
	"strings"
	"testing"
)

// near reports whether two lengths agree to within tol.
func near(a, b, tol float64) bool { return math.Abs(a-b) <= tol }

// padByNumber finds a pad.
func padByNumber(f Footprint, n string) Pad {
	for _, p := range f.Pads {
		if p.Number == n && p.Kind == PadSMD {
			return p
		}
	}
	return Pad{}
}

func TestStandardFootprintsAgreeWithKiCad(t *testing.T) {
	// KiCad's own generated footprints (IPC-7351 nominal) for the same
	// JEDEC outlines: pad 1's centre and size.
	for _, c := range []struct {
		name       string
		x, y, w, h float64
	}{
		{"SOT-23-6", -1.1375, -0.95, 1.325, 0.6},
		{"SOIC-8", -2.475, -1.905, 1.95, 0.6},
		{"TSSOP-16", -2.8625, -2.275, 1.475, 0.4},
		{"0603", -0.775, 0, 0.9, 0.95},
	} {
		p, ok := Standard(c.name, 0)
		if !ok {
			t.Fatalf("%s isn't a standard package", c.name)
		}
		f, ps := MakeFootprint(c.name, p)
		if err := Err(ps); err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		p1 := padByNumber(f, "1")
		// Within a quarter millimetre of KiCad's: both are IPC nominal but
		// round and pick tolerances differently.
		if !near(p1.X, c.x, 0.25) || !near(p1.Y, c.y, 0.01) || !near(p1.W, c.w, 0.3) || !near(p1.H, c.h, 0.1) {
			t.Errorf("%s pad 1 = (%.4f, %.4f) %.3f × %.3f, KiCad's (%.4f, %.4f) %.3f × %.3f", c.name, p1.X, p1.Y, p1.W, p1.H, c.x, c.y, c.w, c.h)
		}
		if f.From != FromCalculated {
			t.Errorf("%s from %q", c.name, f.From)
		}
	}
}

func TestPinNumbering(t *testing.T) {
	// SOIC-8: 1–4 down the left, 5–8 up the right.
	p, _ := Standard("SOIC (8)", 8)
	f, _ := MakeFootprint("SOIC-8", p)
	if a, b := padByNumber(f, "4"), padByNumber(f, "5"); a.X >= 0 || b.X <= 0 || a.Y != b.Y || a.Y <= 0 {
		t.Errorf("pads 4 %+v and 5 %+v should face each other at the bottom", a, b)
	}
	if a := padByNumber(f, "8"); a.X <= 0 || a.Y >= 0 {
		t.Errorf("pad 8 should be top right: %+v", a)
	}
	// SOT-23: 1 and 2 on the left, 3 alone on the right in the middle.
	p, _ = Standard("SOT-23", 3)
	f, _ = MakeFootprint("SOT-23", p)
	if a, b, c := padByNumber(f, "1"), padByNumber(f, "2"), padByNumber(f, "3"); a.X >= 0 || b.X >= 0 || c.X <= 0 || c.Y != 0 || !near(b.Y-a.Y, 1.9, 1e-9) {
		t.Errorf("SOT-23 pads: %+v %+v %+v", a, b, c)
	}
	// A QFN-16 with an exposed pad: counter-clockwise from the top left;
	// the pad is 17 with paste windows of its own.
	q := Package{Family: "qfn", Pins: 16, Pitch: 0.5, BodyLength: d(2.9, 3.1), BodyWidth: d(2.9, 3.1), LeadLength: d(0.3, 0.5), LeadWidth: d(0.18, 0.3), Height: Dim{Max: 0.8},
		ExposedPad: &Size{1.7, 1.7}}
	f, ps := MakeFootprint("QFN-16", q)
	if err := Err(ps); err != nil {
		t.Fatal(err)
	}
	if a, b, c, e := padByNumber(f, "1"), padByNumber(f, "5"), padByNumber(f, "9"), padByNumber(f, "13"); a.X >= 0 || b.Y <= 0 || c.X <= 0 || e.Y >= 0 {
		t.Errorf("QFN sides: 1 %+v 5 %+v 9 %+v 13 %+v", a, b, c, e)
	}
	if b := padByNumber(f, "5"); b.X >= padByNumber(f, "8").X {
		t.Error("the bottom row runs left to right")
	}
	ep := padByNumber(f, "17")
	windows := 0
	for _, p := range f.Pads {
		if p.Kind == PadPaste {
			windows++
		}
	}
	if ep.W != 1.7 || !ep.NoPaste || windows != 4 {
		t.Errorf("exposed pad %+v with %d paste windows", ep, windows)
	}
}

func TestDatasheetLandWins(t *testing.T) {
	p, _ := Standard("SOT-23-6", 6)
	p.Land = &Land{PadLength: 1.1, PadWidth: 0.6, RowSpan: 2.4}
	f, _ := MakeFootprint("x", p)
	if p1 := padByNumber(f, "1"); p1.X != -1.2 || p1.W != 1.1 || f.From != FromDatasheet {
		t.Errorf("pad 1 %+v from %s", p1, f.From)
	}
	if !strings.Contains(f.Description, "datasheet land pattern") {
		t.Error(f.Description)
	}
}

func TestFootprintProblems(t *testing.T) {
	_, ps := MakeFootprint("x", Package{Family: "gullwing", Pins: 8})
	if err := Err(ps); err == nil || !strings.Contains(err.Error(), "needs pitch") {
		t.Errorf("missing dimensions: %v", err)
	}
	_, ps = MakeFootprint("x", Package{Family: "teacup"})
	if Err(ps) == nil {
		t.Error("unknown family")
	}
	// Pads too wide for their pitch overlap.
	p, _ := Standard("TSSOP-16", 16)
	p.Land = &Land{PadLength: 1.5, PadWidth: 0.7, RowSpan: 5.8}
	if _, ps := MakeFootprint("x", p); Err(ps) == nil {
		t.Error("overlapping pads should be an error")
	}
}

func TestSilkKeepsClearOfPads(t *testing.T) {
	p, _ := Standard("SOIC-8", 8)
	f, _ := MakeFootprint("x", p)
	n := 0
	for _, l := range f.Lines {
		if l.Layer != LayerSilk {
			continue
		}
		n++
		for _, pad := range f.Pads {
			r := pad.Rect().grow(0.2)
			mid := Pt{(l.A.X + l.B.X) / 2, (l.A.Y + l.B.Y) / 2}
			if mid.X > r.X0 && mid.X < r.X1 && mid.Y > r.Y0 && mid.Y < r.Y1 {
				t.Errorf("silk %+v over pad %s", l, pad.Number)
			}
		}
	}
	if n == 0 {
		t.Error("no silkscreen")
	}
	if !(f.Courtyard.X0 < -3.5 && f.Courtyard.Y0 < -2.5) {
		t.Errorf("courtyard %+v", f.Courtyard)
	}
}

func TestSymbol(t *testing.T) {
	pins := []PinInfo{
		{Number: "1", Name: "GND"}, {Number: "2", Name: "SW", Type: "O"}, {Number: "3", Name: "VIN", Type: "P"},
		{Number: "4", Name: "VFB", Type: "I"}, {Number: "5", Name: "EN", Type: "I"}, {Number: "6", Name: "VBST", Type: "I"},
		{Number: "7", Name: "PGOOD", Description: "Open-drain power-good output"}, {Number: "8", Name: "NC"},
	}
	s, ps := MakeSymbol("TPS563200", pins, SymbolSpec{Pins: []PinPlace{{Number: "6", Side: Right, Position: 1}}})
	if err := Err(ps); err != nil {
		t.Fatal(err)
	}
	by := map[string]SymbolPin{}
	for _, p := range s.Pins {
		by[p.Number] = p
		if math.Mod(math.Abs(p.X)+1e-6, Grid) > 1e-3 || math.Mod(math.Abs(p.Y)+1e-6, Grid) > 1e-3 {
			t.Errorf("pin %s off the grid at (%v, %v)", p.Number, p.X, p.Y)
		}
	}
	if by["3"].Side != Top || by["3"].Type != TypePowerIn || by["1"].Side != Bottom || by["2"].Side != Right || by["4"].Side != Left {
		t.Errorf("sides: %+v", s.Pins)
	}
	if by["7"].Type != TypeOpenDrain || by["8"].Type != TypeNoConnect {
		t.Errorf("types: PGOOD %s NC %s", by["7"].Type, by["8"].Type)
	}
	// VBST was put first on the right, above SW.
	if by["6"].Side != Right || by["6"].Y <= by["2"].Y {
		t.Errorf("VBST %+v SW %+v", by["6"], by["2"])
	}
	// Pins touch the body.
	if by["4"].X+by["4"].Length != s.Body.X0 || by["3"].Y-by["3"].Length != s.Body.Y1 {
		t.Errorf("pins don't reach the body %+v", s.Body)
	}
	if _, ps := MakeSymbol("x", pins, SymbolSpec{Pins: []PinPlace{{Number: "99", Side: Left}}}); Err(ps) == nil {
		t.Error("a pin not in the table")
	}
	// Symbol against footprint: 8 pins, an SOIC-8 matches; a SOT-23-6 doesn't.
	p, _ := Standard("SOIC-8", 8)
	f, _ := MakeFootprint("SOIC-8", p)
	if ps := Match(s, f); len(ps) != 0 {
		t.Errorf("match: %v", ps)
	}
	p, _ = Standard("SOT-23-6", 6)
	f, _ = MakeFootprint("SOT-23-6", p)
	if err := Err(Match(s, f)); err == nil || !strings.Contains(err.Error(), "7, 8") {
		t.Errorf("mismatch: %v", err)
	}
}

func TestSolids(t *testing.T) {
	p, _ := Standard("SOIC-8", 8)
	ss, _ := MakeSolids(p)
	leads := 0
	for _, s := range ss {
		if s.Part == PartLead {
			leads++
		}
		// Every face is a closed loop of distinct vertices.
		for _, f := range s.Faces {
			if len(f) < 3 {
				t.Fatalf("face %v", f)
			}
		}
	}
	if leads != 8*3 {
		t.Errorf("%d lead pieces", leads)
	}
	lo, hi := Bounds(ss)
	if !near(hi[0]-lo[0], 6.0, 0.01) || !near(hi[2], 1.55, 0.02) || lo[2] != 0 {
		t.Errorf("bounds %v %v", lo, hi)
	}
}
