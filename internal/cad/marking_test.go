package cad

import (
	"math"
	"testing"
)

func TestMarking(t *testing.T) {
	p, _ := Standard("SOIC-8", 8)
	ss := Marking("OPA2333", p)
	if len(ss) == 0 {
		t.Fatal("no marking on an SOIC-8")
	}
	a := height(p)
	lo, hi := Bounds(ss)
	if lo[2] < a-markingDepth-1e-9 || hi[2] > a+markingProud+1e-9 {
		t.Errorf("marking z %.3f–%.3f, the body's top is %.3f", lo[2], hi[2], a)
	}
	if hi[0]-lo[0] > p.BodyWidth.Mid() || hi[1]-lo[1] > p.BodyLength.Mid() {
		t.Errorf("marking %.2f × %.2f spills off a %.2f × %.2f body", hi[0]-lo[0], hi[1]-lo[1], p.BodyWidth.Mid(), p.BodyLength.Mid())
	}
	// Along the longer side: an SOIC-8 is longer along its rows (y).
	if hi[1]-lo[1] < hi[0]-lo[0] {
		t.Errorf("marking runs across the body (%.2f wide, %.2f tall)", hi[0]-lo[0], hi[1]-lo[1])
	}
	for _, s := range ss {
		if s.Part != PartMark || s.Group != markingGroup || len(s.Faces) != 6 {
			t.Fatalf("stroke %+v", s)
		}
	}
	// Too small to read on, or no flat top: nothing.
	for _, name := range []string{"0402", "TO-220-3"} {
		q, _ := Standard(name, 0)
		if ss := Marking("ABCDEFGH", q); len(ss) != 0 {
			t.Errorf("%s got a marking", name)
		}
	}
	// A long number wraps onto lines: on a 3 mm QFN it's two, across the
	// body, each a readable height; never off the body.
	d := func(n float64) Dim { return Dim{Nom: n} }
	qfn := Package{Family: "qfn", Pins: 16, Pitch: 0.5, BodyLength: d(3), BodyWidth: d(3), Height: d(0.9), LeadLength: d(0.4), LeadWidth: d(0.25)}
	ss = Marking("TPS2121RUXR", qfn)
	if len(ss) == 0 {
		t.Fatal("no marking on a 3 mm QFN")
	}
	lo, hi = Bounds(ss)
	if hi[1]-lo[1] < 0.9 || hi[0]-lo[0] > 2.4+1e-6 || hi[1]-lo[1] > 2.4+1e-6 {
		t.Errorf("QFN marking %.2f × %.2f: two lines of 0.4 mm or more wanted", hi[0]-lo[0], hi[1]-lo[1])
	}
	// A SOT-23-6 takes a nine-character number on two lines, not more.
	q, _ := Standard("SOT-23-6", 6)
	if ss := Marking("TPS563200", q); len(ss) == 0 {
		t.Error("no marking on a SOT-23-6")
	}
	if ss := Marking("TPS563200DDCR", q); len(ss) != 0 {
		t.Error("a 13-character number on a SOT-23-6 isn't readable")
	}
	// An agent-drawn module: on the body its pads give, off centre.
	mod := Package{Family: "custom", Pins: 4, Height: d(3), Pads: []PadSpec{{Number: "1", X: 2, Y: 2, W: 1, H: 1}, {Number: "2", X: 12, Y: 2, W: 1, H: 1}, {Number: "3", X: 2, Y: 8, W: 1, H: 1}, {Number: "4", X: 12, Y: 8, W: 1, H: 1}}}
	ss = Marking("ESP32-C3-MINI-1", mod)
	if len(ss) == 0 {
		t.Fatal("no marking on a module")
	}
	lo, hi = Bounds(ss)
	if (lo[0]+hi[0])/2 < 5 || (lo[0]+hi[0])/2 > 9 || hi[2] < 3 {
		t.Errorf("module marking centred at x %.2f, z from %.2f", (lo[0]+hi[0])/2, lo[2])
	}
	if got := wrap([]rune("STM32U545VE"), 2); got[0] != "STM32U" || got[1] != "545VE" {
		t.Errorf("wrap %q", got)
	}
	if got := wrap([]rune("ESP32-C3-MINI-1"), 2); got[0] != "ESP32-C3-" || got[1] != "MINI-1" {
		t.Errorf("wrap at a dash %q", got)
	}
}

func TestMarkingIsOneShapeAndManySolids(t *testing.T) {
	p, _ := Standard("SOIC-8", 8)
	solids, _ := MakeSolids(p)
	marks := Marking("OPA2333", p)
	shapes := ShapesOf(append(solids, marks...))
	var marking *Shape3D
	for i := range shapes {
		if shapes[i].Name == markingGroup {
			marking = &shapes[i]
		}
	}
	if marking == nil || marking.Kind != Shape3DSolid || len(marking.Solid.Faces) != 6*len(marks) {
		t.Fatalf("marking shape %+v", marking)
	}
	back, err := SolidsOf(shapes)
	if err != nil {
		t.Fatal(err)
	}
	if len(back) != len(solids)+len(marks) {
		t.Errorf("%d solids back, want %d", len(back), len(solids)+len(marks))
	}
	strokes := 0
	for _, s := range back {
		if s.Part == PartMark && len(s.Faces) == 6 {
			strokes++
		}
	}
	if strokes != len(marks) {
		t.Errorf("%d strokes back as solids of their own, want %d", strokes, len(marks))
	}
	// A stroke's ends are where its glyph puts them.
	s := slab(PartMark, Pt{0, 0}, Pt{1, 0}, 0.1, 0, 0.02)
	lo, hi := Bounds([]Solid{s})
	if math.Abs(lo[0]+0.05) > 1e-6 || math.Abs(hi[0]-1.05) > 1e-6 || math.Abs(hi[1]-lo[1]-0.1) > 1e-6 {
		t.Errorf("slab bounds %v %v", lo, hi)
	}
}
