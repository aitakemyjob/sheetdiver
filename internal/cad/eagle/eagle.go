// Package eagle writes a CAD library as an Eagle library (.lbr, XML):
// packages (footprints), symbols, and device sets joining them. Fusion
// Electronics reads it as is, and Altium's Import Wizard and most other
// tools import it.
package eagle

import (
	"encoding/xml"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/aitakemyjob/sheetdiver/internal/cad"
)

// Part is one library entry: a symbol on a footprint.
type Part struct {
	Symbol    cad.Symbol
	Footprint cad.Footprint
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
			writePackage(&b, p.Footprint)
		}
	}
	b.WriteString("  </packages>\n  <symbols>\n")
	for _, p := range parts {
		writeSymbol(&b, p.Symbol)
	}
	b.WriteString("  </symbols>\n  <devicesets>\n")
	for _, p := range parts {
		writeDevice(&b, p)
	}
	b.WriteString("  </devicesets>\n </library>\n</drawing>\n</eagle>\n")
	return []byte(b.String())
}

// writePackage writes a footprint, its y flipped (Eagle's is up).
func writePackage(b *strings.Builder, fp cad.Footprint) {
	fmt.Fprintf(b, "   <package name=\"%s\">\n    <description>%s</description>\n", esc(Name(fp.Name)), esc(fp.Description))
	for _, p := range fp.Pads {
		switch p.Kind {
		case cad.PadSMD:
			attrs := []string{"name", p.Number, "x", f(p.X), "y", f(-p.Y), "dx", f(p.W), "dy", f(p.H), "layer", strconv.Itoa(layerTop)}
			switch p.Shape {
			case "roundrect":
				attrs = append(attrs, "roundness", "25")
			case "circle":
				attrs = append(attrs, "roundness", "100")
			}
			if p.NoPaste {
				attrs = append(attrs, "cream", "no")
			}
			w(b, 4, "smd", attrs...)
		case cad.PadPaste:
			w(b, 4, "rectangle", "x1", f(p.X-p.W/2), "y1", f(-p.Y-p.H/2), "x2", f(p.X+p.W/2), "y2", f(-p.Y+p.H/2), "layer", strconv.Itoa(layerTCream))
		}
		// Thermal vias: Eagle names pads uniquely, so they are left for
		// the board (a via per hole, on the exposed pad's net).
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
	fmt.Fprintf(b, "    <text x=\"%s\" y=\"%s\" size=\"1\" layer=\"%d\" align=\"bottom-center\">&gt;NAME</text>\n", f(0), f(-fp.Courtyard.Y0+0.3), layerTNames)
	fmt.Fprintf(b, "    <text x=\"%s\" y=\"%s\" size=\"1\" layer=\"%d\" align=\"top-center\">&gt;VALUE</text>\n", f(0), f(-fp.Courtyard.Y1-0.3), layerTValues)
	b.WriteString("   </package>\n")
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
	pads := map[string]bool{}
	for _, pad := range p.Footprint.Pads {
		if pad.Kind == cad.PadSMD {
			pads[pad.Number] = true
		}
	}
	for _, pin := range p.Symbol.Pins {
		if pads[pin.Number] {
			w(b, 7, "connect", "gate", "G$1", "pin", names[pin.Number], "pad", pin.Number)
		}
	}
	b.WriteString("      </connects>\n      <technologies>\n")
	w(b, 7, "technology", "name", "")
	b.WriteString("      </technologies>\n     </device>\n    </devices>\n   </deviceset>\n")
}
