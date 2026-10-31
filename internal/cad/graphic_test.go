package cad

import (
	"math"
	"testing"
)

func TestArcs(t *testing.T) {
	// A quarter circle from (1,0) through 45° to (0,1).
	s2 := math.Sqrt2 / 2
	g := Graphic{Kind: GraphicArc, Pts: []Pt{{1, 0}, {s2, s2}, {0, 1}}}
	o, r, a, b := ArcAngles(g)
	if math.Abs(o.X) > 1e-9 || math.Abs(r-1) > 1e-9 || math.Abs(a) > 1e-9 || math.Abs(b-90) > 1e-9 {
		t.Errorf("arc %v %g %g %g", o, r, a, b)
	}
	// The same arc given the other way round is the same arc.
	rev := Graphic{Kind: GraphicArc, Pts: []Pt{{0, 1}, {s2, s2}, {1, 0}}}
	if graphicKeys(g)[0] != graphicKeys(rev)[0] {
		t.Errorf("%v != %v", graphicKeys(g), graphicKeys(rev))
	}
	// Three quarters, through the bottom.
	big := Graphic{Kind: GraphicArc, Pts: []Pt{{1, 0}, {0, -1}, {0, 1}}}
	if _, _, a, b := ArcAngles(big); math.Abs(a-90) > 1e-9 || math.Abs(b-360) > 1e-9 {
		t.Errorf("big arc %g %g", a, b)
	}
	back := ArcFrom(Pt{}, 1, 90, 360)
	if graphicKeys(back)[0] != graphicKeys(big)[0] {
		t.Errorf("ArcFrom %v %v %v", back.Pts, graphicKeys(back), graphicKeys(big))
	}
	if b := big.Bounds(); math.Abs(b.X0+1) > 1e-9 || math.Abs(b.Y0+1) > 1e-9 {
		t.Errorf("bounds %v", b)
	}
	if (Graphic{Kind: GraphicArc, Pts: []Pt{{0, 0}, {1, 1}, {2, 2}}}).Check() == nil {
		t.Error("a straight arc passed")
	}
}

func TestGraphicPrints(t *testing.T) {
	// A rectangle drawn as a rect or as four lines is the same footprint.
	rect := Footprint{Graphics: []Graphic{{Kind: GraphicRect, Layer: LayerSilk, Pts: []Pt{{0, 0}, {2, 1}}, Width: 0.12}}}
	lines := Footprint{Lines: []Line{
		{Layer: LayerSilk, A: Pt{0, 0}, B: Pt{2, 0}}, {Layer: LayerSilk, A: Pt{2, 0}, B: Pt{2, 1}},
		{Layer: LayerSilk, A: Pt{2, 1}, B: Pt{0, 1}}, {Layer: LayerSilk, A: Pt{0, 1}, B: Pt{0, 0}},
	}}
	if FootprintPrint(rect) != FootprintPrint(lines) {
		t.Error("rect and lines differ")
	}
	// A filled polygon is the pin 1 mark whichever way round.
	mark := Footprint{Polys: []Poly{{Layer: LayerSilk, Pts: []Pt{{0, 0}, {1, 0}, {0, 1}}}}}
	drawn := Footprint{Graphics: []Graphic{{Kind: GraphicPoly, Layer: LayerSilk, Fill: true, Pts: []Pt{{1, 0}, {0, 0}, {0, 1}}}}}
	if FootprintPrint(mark) != FootprintPrint(drawn) {
		t.Error("poly and filled graphic differ")
	}
	// A moved text is an edit.
	a := Symbol{Graphics: []Graphic{{Kind: GraphicText, Pts: []Pt{{0, 0}}, Text: "EN", Size: 1.27}}}
	b := Symbol{Graphics: []Graphic{{Kind: GraphicText, Pts: []Pt{{0, 1}}, Text: "EN", Size: 1.27}}}
	if SymbolPrint(a) == SymbolPrint(b) {
		t.Error("moved text not noticed")
	}
	d := DrawFootprint(Footprint{Graphics: []Graphic{ArcFrom(Pt{}, 1, 0, 180), {Kind: GraphicCircle, Layer: LayerFab, Pts: []Pt{{3, 3}}, R: 0.5}}})
	if len(d.Items) != 2 || len(d.Items[0].Pts) < 5 {
		t.Errorf("drawing %+v", d.Items)
	}
}
