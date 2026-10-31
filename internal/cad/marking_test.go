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
	if lo[2] < a-1e-9 || hi[2] > a+0.03 {
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
	// A long number shrinks to fit, never below readable.
	q, _ := Standard("SOT-23-6", 6)
	if ss := Marking("TPS563200DDCR", q); len(ss) != 0 {
		lo, hi := Bounds(ss)
		if hi[1]-lo[1] > q.BodyLength.Mid() {
			t.Errorf("SOT-23 marking %.2f long on a %.2f body", hi[1]-lo[1], q.BodyLength.Mid())
		}
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
