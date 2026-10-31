package kicad

import (
	"fmt"
	"math"
	"sort"
	"strings"

	"github.com/aitakemyjob/sheetdiver/internal/cad"
)

// Versions of the file formats written: KiCad 7's, which KiCad 7 and
// later read.
const (
	symbolVersion    = "20220914"
	footprintVersion = "20221018"
	generator        = "sheetdiver"
)

// SafeName makes a name usable as a KiCad symbol or footprint name: no
// path separators, colons or spaces.
func SafeName(s string) string {
	r := strings.NewReplacer("/", "_", "\\", "_", ":", "_", " ", "_", "\"", "", "(", "", ")", "")
	return r.Replace(strings.TrimSpace(s))
}

// font is the standard text effects.
func font(hidden bool) *Node {
	e := n("effects", n("font", n("size", num(1.27), num(1.27))))
	if hidden {
		e.Args = append(e.Args, Atom("hide"))
	}
	return e
}

// property is a symbol property.
func property(key, value string, x, y float64, hidden bool) *Node {
	return n("property", Str(key), Str(value), n("at", num(x), num(y), Atom("0")), font(hidden))
}

// angles are the directions a pin points from its end into the body, by
// side.
var angles = map[string]string{cad.Left: "0", cad.Right: "180", cad.Top: "270", cad.Bottom: "90"}

// SymbolNode is a symbol as a library's (symbol …) list.
func SymbolNode(s cad.Symbol) *Node {
	name := SafeName(s.Name)
	b := s.Body
	// The reference above the body and the value below, clear of any
	// pins on top or underneath.
	top, bottom := b.Y1+1.27, b.Y0-1.27
	for _, p := range s.Pins {
		top = math.Max(top, p.Y+1.27)
		bottom = math.Min(bottom, p.Y-1.27)
	}
	sym := n("symbol", Str(name), n("in_bom", Atom("yes")), n("on_board", Atom("yes")),
		property("Reference", s.Reference, b.X0, top, false),
		property("Value", s.Value, b.X0, bottom, false),
		property("Footprint", s.Footprint, 0, 0, true),
		property("Datasheet", s.Datasheet, 0, 0, true),
		property("ki_description", s.Description, 0, 0, true),
	)
	// Pin names sit inside the body, pin numbers on the pins.
	sym.Args = append(sym.Args[:1], append([]any{n("pin_names", n("offset", num(1.016)))}, sym.Args[1:]...)...)
	body := n("symbol", Str(name+"_0_1"))
	if b != (cad.Rect{}) {
		body.Args = append(body.Args, n("rectangle", xy("start", b.X0, b.Y1), xy("end", b.X1, b.Y0),
			n("stroke", n("width", num(0.254)), n("type", Atom("default"))),
			n("fill", n("type", Atom("background")))))
	}
	for _, l := range s.Extra {
		body.Args = append(body.Args, n("polyline", n("pts", xy("xy", l.A.X, l.A.Y), xy("xy", l.B.X, l.B.Y)),
			n("stroke", n("width", num(0.254)), n("type", Atom("default"))), n("fill", n("type", Atom("none")))))
	}
	for _, g := range s.Graphics {
		if g.Check() == nil {
			body.Args = append(body.Args, symbolGraphic(g))
		}
	}
	pins := n("symbol", Str(name+"_1_1"))
	for _, p := range s.Pins {
		pin := n("pin", Atom(p.Type), Atom("line"), n("at", num(p.X), num(p.Y), Atom(angles[p.Side])), n("length", num(p.Length)))
		if p.Hidden {
			pin.Args = append(pin.Args, Atom("hide"))
		}
		name := p.Name
		if name == "" {
			name = "~" // KiCad's "no name"
		}
		pin.Args = append(pin.Args, n("name", Str(name), font(false)), n("number", Str(p.Number), font(false)))
		pins.Args = append(pins.Args, pin)
	}
	sym.Args = append(sym.Args, body, pins)
	return sym
}

// symStroke is a symbol shape's stroke; 0 is KiCad's default width.
func symStroke(w float64) *Node {
	return n("stroke", n("width", num(w)), n("type", Atom("default")))
}

// symFill is a symbol shape's fill: in its outline's colour, or none.
func symFill(on bool) *Node {
	if on {
		return n("fill", n("type", Atom("outline")))
	}
	return n("fill", n("type", Atom("none")))
}

// symbolGraphic is a drawn graphic on a symbol's body.
func symbolGraphic(g cad.Graphic) *Node {
	p := g.Pts
	switch g.Kind {
	case cad.GraphicLine:
		return n("polyline", n("pts", xy("xy", p[0].X, p[0].Y), xy("xy", p[1].X, p[1].Y)), symStroke(g.Width), symFill(false))
	case cad.GraphicRect:
		return n("rectangle", xy("start", p[0].X, p[0].Y), xy("end", p[1].X, p[1].Y), symStroke(g.Width), symFill(g.Fill))
	case cad.GraphicCircle:
		return n("circle", xy("center", p[0].X, p[0].Y), n("radius", num(g.R)), symStroke(g.Width), symFill(g.Fill))
	case cad.GraphicArc:
		return n("arc", xy("start", p[0].X, p[0].Y), xy("mid", p[1].X, p[1].Y), xy("end", p[2].X, p[2].Y), symStroke(g.Width), symFill(g.Fill))
	case cad.GraphicPoly:
		pts := n("pts")
		for _, pt := range append(append([]cad.Pt(nil), p...), p[0]) { // closed
			pts.Args = append(pts.Args, xy("xy", pt.X, pt.Y))
		}
		return n("polyline", pts, symStroke(g.Width), symFill(g.Fill))
	}
	// A text: a symbol's text angle is in tenths of a degree.
	return n("text", Str(g.Text), n("at", num(p[0].X), num(p[0].Y), num(g.Angle*10)),
		n("effects", n("font", n("size", num(g.Size), num(g.Size))), n("justify", Atom("left"), Atom("bottom"))))
}

// SymbolLibrary is a .kicad_sym file holding the symbols, each written
// from its model or, when the user has changed it in KiCad, as they left
// it (raw).
func SymbolLibrary(symbols []cad.Symbol, raw map[string]string) ([]byte, error) {
	lib := n("kicad_symbol_lib", n("version", Atom(symbolVersion)), n("generator", Atom(generator)))
	var names []string
	byName := map[string]*Node{}
	for _, s := range symbols {
		name := SafeName(s.Name)
		if r, ok := raw[name]; ok {
			nd, err := Parse(r)
			if err != nil {
				return nil, fmt.Errorf("symbol %s as edited: %w", name, err)
			}
			byName[name] = nd
		} else {
			byName[name] = SymbolNode(s)
		}
		names = append(names, name)
	}
	sort.Strings(names)
	for _, nm := range names {
		lib.Args = append(lib.Args, byName[nm])
	}
	return []byte(lib.String()), nil
}

// Symbols reads a .kicad_sym file's symbols by name.
func Symbols(src string) (map[string]*Node, error) {
	nd, err := Parse(src)
	if err != nil {
		return nil, err
	}
	if nd.Name != "kicad_symbol_lib" {
		return nil, fmt.Errorf("not a KiCad symbol library (%s)", nd.Name)
	}
	out := map[string]*Node{}
	for _, s := range nd.Children("symbol") {
		out[s.Text(0)] = s
	}
	return out, nil
}

// sides are the sides pins are on, by the angle they point at.
var sides = map[string]string{"0": cad.Left, "180": cad.Right, "270": cad.Top, "90": cad.Bottom}

// ReadSymbol turns a library's symbol back into the model: its pins, its
// body (the first rectangle, else the box round its graphics) and its
// other lines.
func ReadSymbol(nd *Node) cad.Symbol {
	s := cad.Symbol{Name: nd.Text(0)}
	for _, p := range nd.Children("property") {
		switch p.Text(0) {
		case "Reference":
			s.Reference = p.Text(1)
		case "Value":
			s.Value = p.Text(1)
		case "Footprint":
			s.Footprint = p.Text(1)
		case "Datasheet":
			s.Datasheet = p.Text(1)
		case "ki_description", "Description":
			s.Description = p.Text(1)
		}
	}
	var box cad.Rect
	haveBody := false
	grow := func(x, y float64) {
		if !haveBody && box == (cad.Rect{}) {
			box = cad.Rect{X0: x, Y0: y, X1: x, Y1: y}
			return
		}
		box = cad.Rect{X0: math.Min(box.X0, x), Y0: math.Min(box.Y0, y), X1: math.Max(box.X1, x), Y1: math.Max(box.Y1, y)}
	}
	for _, unit := range nd.Children("symbol") {
		for _, r := range unit.Children("rectangle") {
			a, b := r.Child("start"), r.Child("end")
			rect := cad.Rect{X0: math.Min(a.Float(0), b.Float(0)), Y0: math.Min(a.Float(1), b.Float(1)), X1: math.Max(a.Float(0), b.Float(0)), Y1: math.Max(a.Float(1), b.Float(1))}
			if !haveBody && !filled(r) {
				s.Body, haveBody = rect, true
				continue
			}
			s.Graphics = append(s.Graphics, cad.Graphic{Kind: cad.GraphicRect, Layer: cad.LayerBody, Pts: []cad.Pt{{X: a.Float(0), Y: a.Float(1)}, {X: b.Float(0), Y: b.Float(1)}},
				Width: r.Child("stroke").Child("width").Float(0), Fill: filled(r)})
		}
		for _, pl := range unit.Children("polyline") {
			pts := pl.Child("pts").Children("xy")
			var ps []cad.Pt
			for _, x := range pts {
				ps = append(ps, cad.Pt{X: x.Float(0), Y: x.Float(1)})
				grow(x.Float(0), x.Float(1))
			}
			if filled(pl) && len(ps) >= 4 && ps[0] == ps[len(ps)-1] {
				s.Graphics = append(s.Graphics, cad.Graphic{Kind: cad.GraphicPoly, Layer: cad.LayerBody, Pts: ps[:len(ps)-1], Width: pl.Child("stroke").Child("width").Float(0), Fill: true})
				continue
			}
			for i := 1; i < len(ps); i++ {
				s.Extra = append(s.Extra, cad.Line{Layer: cad.LayerBody, A: ps[i-1], B: ps[i], Width: 0.254})
			}
		}
		for _, c := range unit.Children("circle") {
			at := c.Child("center")
			s.Graphics = append(s.Graphics, cad.Graphic{Kind: cad.GraphicCircle, Layer: cad.LayerBody, Pts: []cad.Pt{{X: at.Float(0), Y: at.Float(1)}}, R: c.Child("radius").Float(0),
				Width: c.Child("stroke").Child("width").Float(0), Fill: filled(c)})
		}
		for _, a := range unit.Children("arc") {
			if a.Child("mid") == nil {
				continue
			}
			pt := func(x *Node) cad.Pt { return cad.Pt{X: x.Float(0), Y: x.Float(1)} }
			g := cad.Graphic{Kind: cad.GraphicArc, Layer: cad.LayerBody, Pts: []cad.Pt{pt(a.Child("start")), pt(a.Child("mid")), pt(a.Child("end"))},
				Width: a.Child("stroke").Child("width").Float(0), Fill: filled(a)}
			if g.Check() == nil {
				s.Graphics = append(s.Graphics, g)
			}
		}
		for _, t := range unit.Children("text") {
			// A symbol's text angle is in tenths of a degree.
			s.Graphics = append(s.Graphics, readText(t, t.Text(0), cad.LayerBody, false, 10))
		}
		for _, p := range unit.Children("pin") {
			at := p.Child("at")
			sp := cad.SymbolPin{Number: p.Child("number").Text(0), Name: p.Child("name").Text(0), Type: p.Text(0),
				Side: sides[at.Text(2)], X: at.Float(0), Y: at.Float(1), Length: p.Child("length").Float(0), Hidden: Hidden(p)}
			if sp.Side == "" {
				sp.Side = cad.Left
			}
			s.Pins = append(s.Pins, sp)
		}
	}
	if !haveBody {
		s.Body = box
	}
	return s
}
