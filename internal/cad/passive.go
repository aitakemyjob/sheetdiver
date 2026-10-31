package cad

// Standard symbols for two-terminal parts whose datasheets give no pin
// table: a resistor, a capacitor, an inductor, a diode… drawn as the
// block diagram draws them (frontend/src/lib/symbols.ts), pin 1 on top
// and pin 2 underneath, a KiCad-sized body (5.08 mm between the pins'
// ends) and no box.

// passivePin is a terminal's distance from the centre and its lead.
const passivePin, passiveLead = 3.81, 1.27

// PassiveSymbol is the standard symbol for a two-terminal shape (the
// categories library.TwoTerminal names), or false for a shape it has
// none for.
func PassiveSymbol(name, shape string) (Symbol, bool) {
	line := func(ax, ay, bx, by float64) Graphic {
		return Graphic{Kind: GraphicLine, Layer: "body", Pts: []Pt{{ax, ay}, {bx, by}}, Width: 0.254}
	}
	arc := func(sx, sy, mx, my, ex, ey float64) Graphic {
		return Graphic{Kind: GraphicArc, Layer: "body", Pts: []Pt{{sx, sy}, {mx, my}, {ex, ey}}, Width: 0.254}
	}
	rect := func(x0, y0, x1, y1 float64) Graphic {
		return Graphic{Kind: GraphicRect, Layer: "body", Pts: []Pt{{x0, y0}, {x1, y1}}, Width: 0.254}
	}
	fill := func(pts ...Pt) Graphic {
		return Graphic{Kind: GraphicPoly, Layer: "body", Pts: pts, Width: 0.254, Fill: true}
	}
	lead := func(from, to float64) Graphic { return line(0, from, 0, to) }
	// The diodes: a triangle pointing at the cathode's bar, below.
	diode := func(bar ...Graphic) []Graphic {
		return append([]Graphic{lead(2.54, 1.185), lead(-1.185, -2.54), fill(Pt{-1.185, 1.185}, Pt{1.185, 1.185}, Pt{0, -1.185})}, bar...)
	}
	var g []Graphic
	ref, top, bottom := "", "1", "2"
	switch shape {
	case "resistor":
		ref = "R"
		zig := []Pt{{0, 2.54}, {0.85, 2.117}, {-0.85, 1.27}, {0.85, 0.423}, {-0.85, -0.423}, {0.85, -1.27}, {-0.85, -2.117}, {0, -2.54}}
		for i := range zig[:len(zig)-1] {
			g = append(g, line(zig[i].X, zig[i].Y, zig[i+1].X, zig[i+1].Y))
		}
	case "capacitor":
		ref = "C"
		g = []Graphic{lead(2.54, 0.508), line(-1.35, 0.508, 1.35, 0.508), line(-1.35, -0.508, 1.35, -0.508), lead(-0.508, -2.54)}
	case "capacitor-polarised":
		ref, top, bottom = "C", "+", "-"
		g = []Graphic{lead(2.54, 0.508), line(-1.35, 0.508, 1.35, 0.508), arc(-1.35, -1.35, 0, -0.836, 1.35, -1.35), lead(-0.677, -2.54),
			line(-2.54, 2.2, -1.52, 2.2), line(-2.03, 2.71, -2.03, 1.69)}
	case "inductor":
		ref = "L"
		for _, y := range []float64{1.905, 0.635, -0.635, -1.905} {
			g = append(g, arc(0, y+0.635, 0.635, y, 0, y-0.635))
		}
	case "ferrite":
		ref = "FB"
		g = []Graphic{lead(2.54, 2.03), lead(-2.03, -2.54), rect(-0.677, 2.03, 0.677, -2.03),
			line(-0.677, 0.85, 0.677, 1.52), line(-0.677, -0.34, 0.677, 0.34), line(-0.677, -1.52, 0.677, -0.85)}
	case "fuse":
		ref = "F"
		g = []Graphic{lead(2.54, -2.54), rect(-0.85, 2.03, 0.85, -2.03)}
	case "diode":
		ref, top, bottom = "D", "2", "1"
		g = diode(line(-1.185, -1.185, 1.185, -1.185))
	case "schottky":
		ref, top, bottom = "D", "2", "1"
		g = diode(line(-1.185, -1.69, -1.185, -1.185), line(-1.185, -1.185, 1.185, -1.185), line(1.185, -1.185, 1.185, -0.677))
	case "zener":
		ref, top, bottom = "D", "2", "1"
		g = diode(line(-1.52, -0.677, -1.185, -1.185), line(-1.185, -1.185, 1.185, -1.185), line(1.185, -1.185, 1.52, -1.69))
	case "led":
		ref, top, bottom = "D", "2", "1"
		g = diode(line(-1.185, -1.185, 1.185, -1.185),
			line(0.677, 0.17, 1.86, 1.35), line(1.86, 1.35, 1.185, 1.35), line(1.86, 1.35, 1.86, 0.677),
			line(1.185, -0.677, 2.37, 0.508), line(2.37, 0.508, 1.69, 0.508), line(2.37, 0.508, 2.37, -0.17))
	case "tvs":
		ref = "D"
		g = []Graphic{lead(2.54, 2.03), lead(-2.03, -2.54), line(-1.185, 0, 1.185, 0),
			fill(Pt{-1.185, 2.03}, Pt{1.185, 2.03}, Pt{0, 0}), fill(Pt{-1.185, -2.03}, Pt{1.185, -2.03}, Pt{0, 0})}
	case "crystal":
		ref = "Y"
		g = []Graphic{lead(2.54, 1.35), line(-1.35, 1.35, 1.35, 1.35), rect(-1.016, 0.677, 1.016, -0.677), line(-1.35, -1.35, 1.35, -1.35), lead(-1.35, -2.54)}
	default:
		return Symbol{}, false
	}
	pins := []SymbolPin{
		{Number: top, Name: "~", Type: "passive", Side: Top, X: 0, Y: passivePin, Length: passiveLead},
		{Number: bottom, Name: "~", Type: "passive", Side: Bottom, X: 0, Y: -passivePin, Length: passiveLead},
	}
	return Symbol{Name: name, Reference: ref, Value: name, Pins: pins, Graphics: g}, true
}
