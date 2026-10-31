package kicad

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"math"
	"sort"
	"strings"

	"github.com/aitakemyjob/sheetdiver/internal/cad"
)

// layerNames are KiCad's layers for the model's.
var layerNames = map[string]string{cad.LayerSilk: "F.SilkS", cad.LayerFab: "F.Fab", cad.LayerCourtyard: "F.CrtYd"}

// stroke is a line's stroke.
func stroke(w float64) *Node { return n("stroke", n("width", num(w)), n("type", Atom("solid"))) }

// text is a footprint text.
func fpText(kind, value string, x, y float64, layer string, size float64) *Node {
	return n("fp_text", Atom(kind), Str(value), xy("at", x, y), n("layer", Str(layer)),
		n("effects", n("font", n("size", num(size), num(size)), n("thickness", num(size*0.15)))))
}

// FootprintNode is a footprint as a .kicad_mod file's list; model is the
// path of its 3D model ("" for none).
func FootprintNode(f cad.Footprint, model string) *Node {
	fp := n("footprint", Str(SafeName(f.Name)), n("version", Atom(footprintVersion)), n("generator", Atom(generator)),
		n("layer", Str("F.Cu")), n("descr", Str(f.Description)), n("attr", Atom("smd")))
	c := f.Courtyard
	fp.Args = append(fp.Args,
		fpText("reference", "REF**", 0, c.Y0-0.8, "F.SilkS", 1),
		fpText("value", SafeName(f.Name), 0, c.Y1+0.8, "F.Fab", 1),
		fpText("user", "${REFERENCE}", 0, 0, "F.Fab", math.Min(1, math.Max(0.4, (f.Body.X1-f.Body.X0)/5))))
	for _, l := range f.Lines {
		fp.Args = append(fp.Args, n("fp_line", xy("start", l.A.X, l.A.Y), xy("end", l.B.X, l.B.Y), stroke(l.Width), n("layer", Str(layerNames[l.Layer]))))
	}
	for _, p := range f.Polys {
		pts := n("pts")
		for _, pt := range p.Pts {
			pts.Args = append(pts.Args, xy("xy", pt.X, pt.Y))
		}
		fp.Args = append(fp.Args, n("fp_poly", pts, stroke(0.12), n("fill", Atom("solid")), n("layer", Str(layerNames[p.Layer]))))
	}
	for _, p := range f.Pads {
		fp.Args = append(fp.Args, padNode(p))
	}
	if model != "" {
		fp.Args = append(fp.Args, n("model", Str(model),
			n("offset", n("xyz", Atom("0"), Atom("0"), Atom("0"))),
			n("scale", n("xyz", Atom("1"), Atom("1"), Atom("1"))),
			n("rotate", n("xyz", Atom("0"), Atom("0"), Atom("0")))))
	}
	return fp
}

// padNode is a pad.
func padNode(p cad.Pad) *Node {
	kind, layers := "smd", []any{Str("F.Cu"), Str("F.Paste"), Str("F.Mask")}
	switch {
	case p.Kind == cad.PadThru:
		kind, layers = "thru_hole", []any{Str("*.Cu")}
	case p.Kind == cad.PadPaste:
		layers = []any{Str("F.Paste")}
	case p.NoPaste:
		layers = []any{Str("F.Cu"), Str("F.Mask")}
	}
	nd := n("pad", Str(p.Number), Atom(kind), Atom(p.Shape), xy("at", p.X, p.Y), xy("size", p.W, p.H))
	if p.Drill > 0 {
		nd.Args = append(nd.Args, n("drill", num(p.Drill)))
	}
	nd.Args = append(nd.Args, n("layers", layers...))
	if p.Shape == "roundrect" {
		nd.Args = append(nd.Args, n("roundrect_rratio", num(0.25)))
	}
	return nd
}

// ReadFootprint turns a .kicad_mod back into the model.
func ReadFootprint(nd *Node) cad.Footprint {
	f := cad.Footprint{Name: nd.Text(0)}
	if d := nd.Child("descr"); d != nil {
		f.Description = d.Text(0)
	}
	layerOf := map[string]string{"F.SilkS": cad.LayerSilk, "F.Silkscreen": cad.LayerSilk, "F.Fab": cad.LayerFab, "F.CrtYd": cad.LayerCourtyard, "F.Courtyard": cad.LayerCourtyard}
	var crt, fab cad.Rect
	add := func(r *cad.Rect, p cad.Pt) {
		if *r == (cad.Rect{}) {
			*r = cad.Rect{X0: p.X, Y0: p.Y, X1: p.X, Y1: p.Y}
			return
		}
		*r = cad.Rect{X0: math.Min(r.X0, p.X), Y0: math.Min(r.Y0, p.Y), X1: math.Max(r.X1, p.X), Y1: math.Max(r.Y1, p.Y)}
	}
	line := func(layer string, a, b cad.Pt, w float64) {
		l, ok := layerOf[layer]
		if !ok {
			return
		}
		f.Lines = append(f.Lines, cad.Line{Layer: l, A: a, B: b, Width: w})
		switch l {
		case cad.LayerCourtyard:
			add(&crt, a)
			add(&crt, b)
		case cad.LayerFab:
			add(&fab, a)
			add(&fab, b)
		}
	}
	width := func(x *Node) float64 {
		if s := x.Child("stroke"); s != nil {
			return s.Child("width").Float(0)
		}
		return x.Child("width").Float(0)
	}
	pt := func(x *Node) cad.Pt { return cad.Pt{X: x.Float(0), Y: x.Float(1)} }
	for _, a := range nd.Args {
		x, ok := a.(*Node)
		if !ok {
			continue
		}
		layer := x.Child("layer").Text(0)
		switch x.Name {
		case "fp_line":
			line(layer, pt(x.Child("start")), pt(x.Child("end")), width(x))
		case "fp_rect":
			s, e := pt(x.Child("start")), pt(x.Child("end"))
			for _, l := range rectEdges(cad.Rect{X0: s.X, Y0: s.Y, X1: e.X, Y1: e.Y}) {
				line(layer, l.A, l.B, width(x))
			}
		case "fp_poly":
			if l, ok := layerOf[layer]; ok {
				var pts []cad.Pt
				for _, p := range x.Child("pts").Children("xy") {
					pts = append(pts, pt(p))
				}
				f.Polys = append(f.Polys, cad.Poly{Layer: l, Pts: pts})
			}
		case "pad":
			f.Pads = append(f.Pads, readPad(x))
		}
	}
	f.Courtyard, f.Body = crt, fab
	return f
}

// readPad reads a pad.
func readPad(x *Node) cad.Pad {
	at, size := x.Child("at"), x.Child("size")
	p := cad.Pad{Number: x.Text(0), Shape: x.Text(2), X: at.Float(0), Y: at.Float(1), W: size.Float(0), H: size.Float(1)}
	if r := math.Mod(math.Abs(at.Float(2)), 180); r > 45 && r < 135 {
		p.W, p.H = p.H, p.W
	}
	if d := x.Child("drill"); d != nil {
		p.Drill = d.Float(0)
	}
	var layers []string
	if l := x.Child("layers"); l != nil {
		for i := range l.Args {
			layers = append(layers, l.Text(i))
		}
	}
	has := func(s string) bool {
		for _, l := range layers {
			if l == s {
				return true
			}
		}
		return false
	}
	switch {
	case x.Text(1) == "thru_hole" || x.Text(1) == "np_thru_hole":
		p.Kind = cad.PadThru
	case !has("F.Cu") && has("F.Paste"):
		p.Kind = cad.PadPaste
	default:
		p.Kind = cad.PadSMD
		p.NoPaste = !has("F.Paste")
	}
	if p.Shape == "oval" || p.Shape == "custom" || p.Shape == "trapezoid" {
		p.Shape = "roundrect"
	}
	return p
}

// Fingerprint is what a symbol or footprint is, whatever the formatting a
// tool saves it with: its pins or pads and its drawing, rounded. Two that
// are the same to the board have the same fingerprint, so a library KiCad
// merely re-saved isn't taken for one the user edited.
func Fingerprint(nd *Node) string {
	symbol := nd.Name == "symbol"
	var lines []string
	var walk func(x *Node, path string)
	walk = func(x *Node, path string) {
		switch x.Name {
		case "pin":
			at := x.Child("at")
			lines = append(lines, fmt.Sprintf("pin %s %s %s %s %s %s %s %v", x.Child("number").Text(0), x.Child("name").Text(0), x.Text(0),
				r4(at.Float(0)), r4(at.Float(1)), at.Text(2), r4(x.Child("length").Float(0)), Hidden(x)))
			return
		case "pad":
			at, size := x.Child("at"), x.Child("size")
			var layers []string
			if l := x.Child("layers"); l != nil {
				for i := range l.Args {
					layers = append(layers, l.Text(i))
				}
			}
			sort.Strings(layers)
			lines = append(lines, fmt.Sprintf("pad %s %s %s %s %s %s %s %s %s %s", x.Text(0), x.Text(1), x.Text(2), r4(at.Float(0)), r4(at.Float(1)), r4(at.Float(2)),
				r4(size.Float(0)), r4(size.Float(1)), r4(x.Child("drill").Float(0)), strings.Join(layers, ",")))
			return
		case "property":
			// A footprint's texts aren't what it is (KiCad 8 writes its
			// reference and value as properties, KiCad 7 as fp_text).
			if !symbol {
				return
			}
			switch x.Text(0) {
			case "Reference", "Value", "Footprint":
				lines = append(lines, "property "+x.Text(0)+" "+x.Text(1))
			}
			return
		case "rectangle", "polyline", "circle", "arc", "fp_line", "fp_rect", "fp_poly", "fp_circle", "fp_arc":
			var pts []string
			var coords func(y *Node)
			coords = func(y *Node) {
				switch y.Name {
				case "start", "end", "mid", "center", "xy":
					pts = append(pts, r4(y.Float(0))+","+r4(y.Float(1)))
				}
				for _, a := range y.Args {
					if c, ok := a.(*Node); ok {
						coords(c)
					}
				}
			}
			coords(x)
			lines = append(lines, fmt.Sprintf("%s %s %s", strings.TrimPrefix(x.Name, "fp_"), x.Child("layer").Text(0), strings.Join(pts, " ")))
			return
		case "model":
			lines = append(lines, "model "+x.Text(0))
			return
		}
		for _, a := range x.Args {
			if c, ok := a.(*Node); ok {
				walk(c, path+"/"+x.Name)
			}
		}
	}
	walk(nd, "")
	sort.Strings(lines)
	h := sha256.Sum256([]byte(strings.Join(lines, "\n")))
	return hex.EncodeToString(h[:12])
}

// r4 writes a number rounded to four decimals.
func r4(v float64) string { return string(num(v)) }

// Hidden reports whether a pin is hidden, as KiCad 7 (hide) or KiCad 8
// ((hide yes)) writes it.
func Hidden(x *Node) bool { return x.Has("hide") || x.Child("hide").Text(0) == "yes" }
