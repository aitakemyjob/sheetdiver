// Package eagle writes a CAD library as an Eagle library (.lbr, XML):
// packages (footprints), symbols, and device sets joining them. Fusion
// Electronics reads it as is, and Altium's Import Wizard and most other
// tools import it.
package eagle

import (
	"encoding/xml"
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"

	"github.com/aitakemyjob/sheetdiver/internal/cad"
)

// Part is one library entry: a symbol on a footprint.
type Part struct {
	Symbol    cad.Symbol
	Footprint cad.Footprint
	// RawPackage, RawSymbol and RawDevice are the package, symbol and
	// device set as the user saved them in Eagle or Fusion, written back
	// as they are.
	RawPackage, RawSymbol, RawDevice string
}

// Eagle's layers used here.
const (
	layerTop     = 1
	layerTPlace  = 21
	layerTNames  = 25
	layerTValues = 27
	layerTCream  = 31
	layerTDocu   = 51
	layerTKeep   = 39
	layerSymbols = 94
	layerNames   = 95
	layerValues  = 96
)

// layers are the layer definitions a library needs.
var layers = []struct {
	n     int
	name  string
	color int
}{
	{1, "Top", 4}, {16, "Bottom", 1}, {17, "Pads", 2}, {18, "Vias", 2}, {20, "Dimension", 15}, {21, "tPlace", 7}, {25, "tNames", 7},
	{27, "tValues", 7}, {29, "tStop", 7}, {31, "tCream", 7}, {39, "tKeepout", 4}, {51, "tDocu", 7}, {94, "Symbols", 4}, {95, "Names", 7}, {96, "Values", 7},
}

// f writes a number.
func f(v float64) string {
	s := strconv.FormatFloat(v, 'f', 4, 64)
	s = strings.TrimRight(strings.TrimRight(s, "0"), ".")
	if s == "-0" || s == "" {
		return "0"
	}
	return s
}

// w writes an element with attributes in the order given.
func w(b *strings.Builder, indent int, name string, attrs ...string) {
	b.WriteString(strings.Repeat(" ", indent))
	b.WriteString("<" + name)
	for i := 0; i+1 < len(attrs); i += 2 {
		fmt.Fprintf(b, ` %s="%s"`, attrs[i], esc(attrs[i+1]))
	}
	b.WriteString("/>\n")
}

// esc escapes XML text.
func esc(s string) string {
	var b strings.Builder
	_ = xml.EscapeText(&b, []byte(s))
	return b.String()
}

// directions are Eagle's pin directions for the electrical types.
var directions = map[string]string{
	cad.TypeInput: "in", cad.TypeOutput: "out", cad.TypeBidir: "io", cad.TypeTriState: "hiz", cad.TypePassive: "pas",
	cad.TypePowerIn: "pwr", cad.TypePowerOut: "out", cad.TypeOpenDrain: "oc", cad.TypeNoConnect: "nc", cad.TypeUnspecified: "pas",
}

// rotations are a pin's rotation by side: it points into the body.
var rotations = map[string]string{cad.Left: "R0", cad.Right: "R180", cad.Top: "R270", cad.Bottom: "R90"}

// Name makes a name usable in Eagle: letters, digits and - _ . only,
// upper case as Eagle shows them.
func Name(s string) string {
	var b strings.Builder
	for _, r := range strings.ToUpper(strings.TrimSpace(s)) {
		switch {
		case r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_', r == '.':
			b.WriteRune(r)
		default:
			b.WriteRune('_')
		}
	}
	return b.String()
}

// pinNames are the symbol's pin names made unique as Eagle wants them:
// a name used more than once becomes NAME@1, NAME@2…
func pinNames(s cad.Symbol) map[string]string {
	count := map[string]int{}
	for _, p := range s.Pins {
		count[p.Name]++
	}
	seen := map[string]int{}
	out := map[string]string{}
	for _, p := range s.Pins {
		name := p.Name
		if name == "" {
			name = p.Number
		}
		if count[p.Name] > 1 {
			seen[p.Name]++
			name = fmt.Sprintf("%s@%d", name, seen[p.Name])
		}
		out[p.Number] = name
	}
	return out
}

// Library writes the parts as an Eagle library.
func Library(name string, parts []Part) []byte {
	sort.Slice(parts, func(i, j int) bool { return parts[i].Symbol.Name < parts[j].Symbol.Name })
	var b strings.Builder
	b.WriteString(`<?xml version="1.0" encoding="utf-8"?>` + "\n<!DOCTYPE eagle SYSTEM \"eagle.dtd\">\n<eagle version=\"9.6.2\">\n<drawing>\n")
	b.WriteString(" <settings>\n")
	w(&b, 2, "setting", "alwaysvectorfont", "no")
	b.WriteString(" </settings>\n")
	w(&b, 1, "grid", "distance", "0.1", "unitdist", "inch", "unit", "inch", "style", "lines", "multiple", "1", "display", "no", "altdistance", "0.01", "altunitdist", "inch", "altunit", "inch")
	b.WriteString(" <layers>\n")
	for _, l := range layers {
		w(&b, 2, "layer", "number", strconv.Itoa(l.n), "name", l.name, "color", strconv.Itoa(l.color), "fill", "1", "visible", "yes", "active", "yes")
	}
	b.WriteString(" </layers>\n <library>\n")
	fmt.Fprintf(&b, "  <description>%s, made by SheetDiver from datasheets.</description>\n", esc(name))
	b.WriteString("  <packages>\n")
	done := map[string]bool{}
	for _, p := range parts {
		if fp := Name(p.Footprint.Name); !done[fp] {
			done[fp] = true
			if p.RawPackage != "" {
				b.WriteString("   " + p.RawPackage + "\n")
				continue
			}
			writePackage(&b, p.Footprint)
		}
	}
	b.WriteString("  </packages>\n  <symbols>\n")
	for _, p := range parts {
		if p.RawSymbol != "" {
			b.WriteString("   " + p.RawSymbol + "\n")
			continue
		}
		writeSymbol(&b, p.Symbol)
	}
	b.WriteString("  </symbols>\n  <devicesets>\n")
	for _, p := range parts {
		if p.RawDevice != "" {
			b.WriteString("   " + p.RawDevice + "\n")
			continue
		}
		writeDevice(&b, p)
	}
	b.WriteString("  </devicesets>\n </library>\n</drawing>\n</eagle>\n")
	return []byte(b.String())
}

// padNames are a footprint's pads' names as Eagle wants them, unique: a
// number used by several pads (a shield's tabs) becomes 5, 5@2, 5@3…
func padNames(fp cad.Footprint) map[int]string {
	seen := map[string]int{}
	out := map[int]string{}
	for i, p := range fp.Pads {
		if !p.Terminal() {
			continue
		}
		seen[p.Number]++
		out[i] = p.Number
		if seen[p.Number] > 1 {
			out[i] = fmt.Sprintf("%s@%d", p.Number, seen[p.Number])
		}
	}
	return out
}

// writePackage writes a footprint, its y flipped (Eagle's is up).
func writePackage(b *strings.Builder, fp cad.Footprint) {
	fmt.Fprintf(b, "   <package name=\"%s\">\n    <description>%s</description>\n", esc(Name(fp.Name)), esc(fp.Description))
	names := padNames(fp)
	for i, p := range fp.Pads {
		switch {
		case p.Kind == cad.PadSMD:
			attrs := []string{"name", names[i], "x", f(p.X), "y", f(-p.Y), "dx", f(p.W), "dy", f(p.H), "layer", strconv.Itoa(layerTop)}
			switch p.Shape {
			case "roundrect":
				attrs = append(attrs, "roundness", "25")
			case "circle", "oval":
				attrs = append(attrs, "roundness", "100")
			}
			if p.NoPaste {
				attrs = append(attrs, "cream", "no")
			}
			w(b, 4, "smd", attrs...)
		case p.Kind == cad.PadThru && !p.Via:
			// Eagle's pads are round, square or long (twice as long as
			// wide, along x unless turned).
			shape, dia, rot := "round", math.Min(p.W, p.H), "R0"
			switch {
			case p.Shape == "rect":
				shape = "square"
			case p.W != p.H:
				shape = "long"
				if p.H > p.W {
					rot = "R90"
				}
			}
			w(b, 4, "pad", "name", names[i], "x", f(p.X), "y", f(-p.Y), "drill", f(p.Drill), "diameter", f(dia), "shape", shape, "rot", rot)
		case p.Kind == cad.PadNPTH:
			w(b, 4, "hole", "x", f(p.X), "y", f(-p.Y), "drill", f(p.Drill))
		case p.Kind == cad.PadPaste:
			w(b, 4, "rectangle", "x1", f(p.X-p.W/2), "y1", f(-p.Y-p.H/2), "x2", f(p.X+p.W/2), "y2", f(-p.Y+p.H/2), "layer", strconv.Itoa(layerTCream))
		}
		// Thermal vias: Eagle names pads uniquely and a via isn't a pad,
		// so they are left for the board (a via per hole, on the exposed
		// pad's net).
	}
	layerOf := map[string]int{cad.LayerSilk: layerTPlace, cad.LayerFab: layerTDocu, cad.LayerCourtyard: layerTKeep}
	for _, l := range fp.Lines {
		if l.Layer == cad.LayerCourtyard {
			continue
		}
		w(b, 4, "wire", "x1", f(l.A.X), "y1", f(-l.A.Y), "x2", f(l.B.X), "y2", f(-l.B.Y), "width", f(l.Width), "layer", strconv.Itoa(layerOf[l.Layer]))
	}
	for _, p := range fp.Polys {
		fmt.Fprintf(b, "    <polygon width=\"0.01\" layer=\"%d\">\n", layerOf[p.Layer])
		for _, pt := range p.Pts {
			w(b, 5, "vertex", "x", f(pt.X), "y", f(-pt.Y))
		}
		b.WriteString("    </polygon>\n")
	}
	for _, g := range fp.Graphics {
		if l, ok := graphicLayers[g.Layer]; ok {
			writeGraphic(b, g, l, -1)
		}
	}
	fmt.Fprintf(b, "    <text x=\"%s\" y=\"%s\" size=\"1\" layer=\"%d\" align=\"bottom-center\">&gt;NAME</text>\n", f(0), f(-fp.Courtyard.Y0+0.3), layerTNames)
	fmt.Fprintf(b, "    <text x=\"%s\" y=\"%s\" size=\"1\" layer=\"%d\" align=\"top-center\">&gt;VALUE</text>\n", f(0), f(-fp.Courtyard.Y1-0.3), layerTValues)
	b.WriteString("   </package>\n")
}

// graphicLayers are the Eagle layers a footprint's graphics are drawn on.
var graphicLayers = map[string]int{cad.LayerSilk: layerTPlace, cad.LayerFab: layerTDocu, cad.LayerCourtyard: layerTKeep, cad.LayerCopper: layerTop}

// hairline is the width of a stroke given none where Eagle needs one: a
// polygon's outline, an unfilled circle's (width 0 fills it).
const hairline = 0.01

// writeGraphic writes a drawn graphic on an Eagle layer, ys -1 to flip a
// footprint's y (down) to Eagle's (up), 1 for a symbol's. Rectangles and
// polygons with no fill are their sides as wires, an arc a curved wire.
func writeGraphic(b *strings.Builder, g cad.Graphic, layer int, ys float64) {
	if g.Check() != nil {
		return
	}
	l := strconv.Itoa(layer)
	wire := func(p, q cad.Pt, curve float64) {
		attrs := []string{"x1", f(p.X), "y1", f(ys * p.Y), "x2", f(q.X), "y2", f(ys * q.Y), "width", f(g.Width), "layer", l}
		if curve != 0 {
			attrs = append(attrs, "curve", f(curve))
		}
		w(b, 4, "wire", attrs...)
	}
	sides := func(pts []cad.Pt) {
		for i := range pts {
			wire(pts[i], pts[(i+1)%len(pts)], 0)
		}
	}
	switch g.Kind {
	case cad.GraphicLine:
		wire(g.Pts[0], g.Pts[1], 0)
	case cad.GraphicRect:
		p, q := g.Pts[0], g.Pts[1]
		if !g.Fill {
			sides([]cad.Pt{p, {X: q.X, Y: p.Y}, q, {X: p.X, Y: q.Y}})
			return
		}
		w(b, 4, "rectangle", "x1", f(math.Min(p.X, q.X)), "y1", f(math.Min(ys*p.Y, ys*q.Y)), "x2", f(math.Max(p.X, q.X)), "y2", f(math.Max(ys*p.Y, ys*q.Y)), "layer", l)
	case cad.GraphicPoly:
		if !g.Fill {
			sides(g.Pts)
			return
		}
		width := g.Width
		if width <= 0 {
			width = hairline
		}
		fmt.Fprintf(b, "    <polygon width=\"%s\" layer=\"%s\">\n", f(width), l)
		for _, pt := range g.Pts {
			w(b, 5, "vertex", "x", f(pt.X), "y", f(ys*pt.Y))
		}
		b.WriteString("    </polygon>\n")
	case cad.GraphicCircle:
		width := g.Width
		switch {
		case g.Fill:
			width = 0
		case width <= 0:
			width = hairline
		}
		w(b, 4, "circle", "x", f(g.Pts[0].X), "y", f(ys*g.Pts[0].Y), "radius", f(g.R), "width", f(width), "layer", l)
	case cad.GraphicArc:
		// Eagle's curve is the angle swept from the first end to the
		// second, counter-clockwise with y up: the increasing way from
		// start to end in a symbol, from end to start once a footprint's
		// y is flipped.
		o, r, a, e := cad.ArcAngles(g)
		start, end := g.Pts[0], g.Pts[2]
		at := cad.Pt{X: o.X + r*math.Cos(a*math.Pi/180), Y: o.Y + r*math.Sin(a*math.Pi/180)}
		if math.Hypot(at.X-start.X, at.Y-start.Y) > math.Hypot(at.X-end.X, at.Y-end.Y) {
			start, end = end, start
		}
		if ys < 0 {
			start, end = end, start
		}
		wire(start, end, e-a)
	case cad.GraphicText:
		fmt.Fprintf(b, "    <text x=\"%s\" y=\"%s\" size=\"%s\" layer=\"%s\"", f(g.Pts[0].X), f(ys*g.Pts[0].Y), f(g.Size), l)
		if r := textRot(g.Angle); r != "" {
			fmt.Fprintf(b, " rot=\"%s\"", r)
		}
		b.WriteString(">" + esc(g.Text) + "</text>\n")
	}
}

// textRot is Eagle's rotation for a text turned an angle as seen. It is
// spun (S) so Eagle draws it as turned, not turned round to read from
// below, and KiCad's importer keeps angles other than right angles.
func textRot(deg float64) string {
	deg = math.Mod(math.Round(deg*1e4)/1e4, 360)
	if deg < 0 {
		deg += 360
	}
	if deg == 0 {
		return ""
	}
	return "SR" + f(deg)
}

// writeSymbol writes a symbol.
func writeSymbol(b *strings.Builder, s cad.Symbol) {
	fmt.Fprintf(b, "   <symbol name=\"%s\">\n", esc(Name(s.Name)))
	r := s.Body
	for _, l := range [][4]float64{{r.X0, r.Y0, r.X1, r.Y0}, {r.X1, r.Y0, r.X1, r.Y1}, {r.X1, r.Y1, r.X0, r.Y1}, {r.X0, r.Y1, r.X0, r.Y0}} {
		w(b, 4, "wire", "x1", f(l[0]), "y1", f(l[1]), "x2", f(l[2]), "y2", f(l[3]), "width", "0.254", "layer", strconv.Itoa(layerSymbols))
	}
	for _, l := range s.Extra {
		w(b, 4, "wire", "x1", f(l.A.X), "y1", f(l.A.Y), "x2", f(l.B.X), "y2", f(l.B.Y), "width", "0.254", "layer", strconv.Itoa(layerSymbols))
	}
	for _, g := range s.Graphics {
		writeGraphic(b, g, layerSymbols, 1)
	}
	names := pinNames(s)
	for _, p := range s.Pins {
		length := "short"
		if p.Length > 4 {
			length = "middle"
		}
		attrs := []string{"name", names[p.Number], "x", f(p.X), "y", f(p.Y), "length", length, "direction", directions[p.Type], "rot", rotations[p.Side]}
		if p.Hidden {
			attrs = append(attrs, "visible", "off")
		}
		w(b, 4, "pin", attrs...)
	}
	fmt.Fprintf(b, "    <text x=\"%s\" y=\"%s\" size=\"1.778\" layer=\"%d\">&gt;NAME</text>\n", f(r.X0), f(r.Y1+1.27), layerNames)
	fmt.Fprintf(b, "    <text x=\"%s\" y=\"%s\" size=\"1.778\" layer=\"%d\" align=\"top-left\">&gt;VALUE</text>\n", f(r.X0), f(r.Y0-1.27), layerValues)
	b.WriteString("   </symbol>\n")
}

// writeDevice writes the device set joining a symbol's pins to the
// footprint's pads by number.
func writeDevice(b *strings.Builder, p Part) {
	fmt.Fprintf(b, "   <deviceset name=\"%s\" prefix=\"%s\">\n    <description>%s</description>\n    <gates>\n", esc(Name(p.Symbol.Name)), esc(p.Symbol.Reference), esc(p.Symbol.Description))
	w(b, 5, "gate", "name", "G$1", "symbol", Name(p.Symbol.Name), "x", "0", "y", "0")
	fmt.Fprintf(b, "    </gates>\n    <devices>\n     <device name=\"\" package=\"%s\">\n      <connects>\n", esc(Name(p.Footprint.Name)))
	names := pinNames(p.Symbol)
	padsOf := map[string][]string{}
	pn := padNames(p.Footprint)
	for i, pad := range p.Footprint.Pads {
		if n, ok := pn[i]; ok {
			padsOf[pad.Number] = append(padsOf[pad.Number], n)
		}
	}
	for _, pin := range p.Symbol.Pins {
		if pads := padsOf[pin.Number]; len(pads) > 0 {
			w(b, 7, "connect", "gate", "G$1", "pin", names[pin.Number], "pad", strings.Join(pads, " "))
		}
	}
	b.WriteString("      </connects>\n      <technologies>\n")
	w(b, 7, "technology", "name", "")
	b.WriteString("      </technologies>\n     </device>\n    </devices>\n   </deviceset>\n")
}
