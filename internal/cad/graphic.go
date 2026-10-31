package cad

import (
	"fmt"
	"math"
)

// Graphics are what's drawn free-hand on a footprint or a symbol: lines,
// rectangles, circles, arcs, polygons and texts, on a footprint's silk,
// fab, courtyard or copper layer, or on a symbol's body. SheetDiver's
// own outlines are Lines and Polys; what the user draws in the app, and
// what other tools' drawings read back as beyond those, are Graphics.
// Every format writes them and reads them back.

// Graphic kinds.
const (
	GraphicLine   = "line"
	GraphicRect   = "rect"
	GraphicCircle = "circle"
	// An arc runs from its first point through its second to its third.
	GraphicArc  = "arc"
	GraphicPoly = "poly"
	GraphicText = "text"
)

// LayerCopper is a footprint's top copper, for drawn copper (not pads).
const LayerCopper = "copper"

// LayerBody is a symbol's only layer.
const LayerBody = "body"

// Graphic is a drawn shape. Pts are a line's ends, a rectangle's opposite
// corners, a circle's centre, an arc's start, a point on it and its end,
// a polygon's corners, or a text's anchor (its baseline's left end).
type Graphic struct {
	Kind  string `json:"kind"`
	Layer string `json:"layer"`
	Pts   []Pt   `json:"pts"`
	// R is a circle's radius.
	R float64 `json:"r,omitempty"`
	// Width is the stroke's width; 0 draws a filled shape with no stroke.
	Width float64 `json:"width,omitempty"`
	Fill  bool    `json:"fill,omitempty"`
	Text  string  `json:"text,omitempty"`
	// Size is a text's height.
	Size float64 `json:"size,omitempty"`
	// Angle is a text's rotation, degrees counter-clockwise as seen.
	Angle float64 `json:"angle,omitempty"`
}

// graphicPts are how many points each kind has (0: any, at least 3).
var graphicPts = map[string]int{GraphicLine: 2, GraphicRect: 2, GraphicCircle: 1, GraphicArc: 3, GraphicPoly: 0, GraphicText: 1}

// Check says what's wrong with a graphic, or nil.
func (g Graphic) Check() error {
	n, ok := graphicPts[g.Kind]
	switch {
	case !ok:
		return fmt.Errorf("unknown graphic kind %q", g.Kind)
	case n > 0 && len(g.Pts) != n:
		return fmt.Errorf("a %s has %d points, not %d", g.Kind, n, len(g.Pts))
	case n == 0 && len(g.Pts) < 3:
		return fmt.Errorf("a polygon needs at least 3 points")
	case g.Kind == GraphicCircle && g.R <= 0:
		return fmt.Errorf("a circle needs a radius")
	case g.Kind == GraphicText && (g.Text == "" || g.Size <= 0):
		return fmt.Errorf("a text needs its text and size")
	case g.Kind == GraphicArc:
		if _, _, ok := ArcCircle(g.Pts[0], g.Pts[1], g.Pts[2]); !ok {
			return fmt.Errorf("an arc's three points are in a line")
		}
	}
	return nil
}

// ArcCircle is the centre and radius of the circle through three points;
// ok is false when they're in a line.
func ArcCircle(a, b, c Pt) (Pt, float64, bool) {
	d := 2 * (a.X*(b.Y-c.Y) + b.X*(c.Y-a.Y) + c.X*(a.Y-b.Y))
	if math.Abs(d) < 1e-9 {
		return Pt{}, 0, false
	}
	a2, b2, c2 := a.X*a.X+a.Y*a.Y, b.X*b.X+b.Y*b.Y, c.X*c.X+c.Y*c.Y
	o := Pt{(a2*(b.Y-c.Y) + b2*(c.Y-a.Y) + c2*(a.Y-b.Y)) / d, (a2*(c.X-b.X) + b2*(a.X-c.X) + c2*(b.X-a.X)) / d}
	return o, math.Hypot(a.X-o.X, a.Y-o.Y), true
}

// ArcAngles are an arc's centre, radius and its start and end angles in
// degrees, measured from +x towards +y of its own coordinates, going
// from start to end the increasing way (so the arc passes its middle
// point when start < end, the angles normalised to that).
func ArcAngles(g Graphic) (centre Pt, r, start, end float64) {
	o, r, _ := ArcCircle(g.Pts[0], g.Pts[1], g.Pts[2])
	ang := func(p Pt) float64 { return math.Mod(math.Atan2(p.Y-o.Y, p.X-o.X)*180/math.Pi+360, 360) }
	a, m, b := ang(g.Pts[0]), ang(g.Pts[1]), ang(g.Pts[2])
	// Going up from a, the middle comes before the end: a to b. Else the
	// arc runs the other way: b to a.
	up := func(x float64) float64 { return math.Mod(x-a+360, 360) }
	if up(m) <= up(b) {
		start, end = a, b
	} else {
		start, end = b, a
	}
	if end <= start {
		end += 360
	}
	return o, r, start, end
}

// ArcFrom is an arc of a circle from one angle to another (degrees, the
// increasing way), as three points.
func ArcFrom(centre Pt, r, start, end float64) Graphic {
	for end <= start {
		end += 360
	}
	at := func(deg float64) Pt {
		t := deg * math.Pi / 180
		return Pt{Number(centre.X + r*math.Cos(t)), Number(centre.Y + r*math.Sin(t))}
	}
	return Graphic{Kind: GraphicArc, Pts: []Pt{at(start), at((start + end) / 2), at(end)}}
}

// Flip is a graphic with y negated: from a symbol's y up to y down.
func (g Graphic) Flip() Graphic {
	out := g
	out.Pts = make([]Pt, len(g.Pts))
	for i, p := range g.Pts {
		out.Pts[i] = Pt{p.X, -p.Y}
	}
	return out
}

// Bounds is the box round a graphic (a text's anchor only).
func (g Graphic) Bounds() Rect {
	var r Rect
	for i, p := range g.Pts {
		if i == 0 {
			r = Rect{p.X, p.Y, p.X, p.Y}
			continue
		}
		r = r.union(Rect{p.X, p.Y, p.X, p.Y})
	}
	switch g.Kind {
	case GraphicCircle:
		r = r.grow(g.R)
	case GraphicArc:
		// The arc's extremes where it crosses an axis.
		o, rad, a, b := ArcAngles(g)
		for _, q := range []float64{0, 90, 180, 270, 360, 450, 540, 630} {
			if q > a && q < b {
				t := q * math.Pi / 180
				r = r.union(Rect{o.X + rad*math.Cos(t), o.Y + rad*math.Sin(t), o.X + rad*math.Cos(t), o.Y + rad*math.Sin(t)})
			}
		}
	}
	return r
}

// graphicKeys are a graphic's fingerprint lines: a line, a rectangle's or
// an unfilled polygon's sides as lines (so a tool that keeps them as
// lines reads the same), the rest by their points.
func graphicKeys(g Graphic) []string {
	switch g.Kind {
	case GraphicLine:
		return []string{lineKey(g.Layer, g.Pts[0], g.Pts[1])}
	case GraphicRect:
		a, b := g.Pts[0], g.Pts[1]
		c := []Pt{a, {b.X, a.Y}, b, {a.X, b.Y}}
		if g.Fill {
			return []string{polyKey(g.Layer, c)}
		}
		var out []string
		for i := range c {
			out = append(out, lineKey(g.Layer, c[i], c[(i+1)%4]))
		}
		return out
	case GraphicPoly:
		if g.Fill {
			return []string{polyKey(g.Layer, g.Pts)}
		}
		var out []string
		for i := range g.Pts {
			out = append(out, lineKey(g.Layer, g.Pts[i], g.Pts[(i+1)%len(g.Pts)]))
		}
		return out
	case GraphicCircle:
		return []string{fmt.Sprintf("circle %s %s,%s %s %v", g.Layer, r3(g.Pts[0].X), r3(g.Pts[0].Y), r3(g.R), g.Fill)}
	case GraphicArc:
		o, r, a, b := ArcAngles(g)
		return []string{fmt.Sprintf("arc %s %s,%s %s %s %s", g.Layer, r3(o.X), r3(o.Y), r3(r), angleKey(a), angleKey(b))}
	case GraphicText:
		return []string{fmt.Sprintf("text %s %s,%s %s %s %q", g.Layer, r3(g.Pts[0].X), r3(g.Pts[0].Y), r3(g.Size), angleKey(g.Angle), g.Text)}
	}
	return nil
}

// angleKey writes an angle to a tenth of a degree, from 0 to 360.
func angleKey(v float64) string {
	v = math.Mod(math.Round(v*10)/10, 360)
	if v < 0 {
		v += 360
	}
	return fmt.Sprintf("%.1f", v)
}

// polyKey is a filled polygon's line: its corners from the least, in
// order either way round.
func polyKey(layer string, pts []Pt) string {
	if len(pts) > 1 && pts[0] == pts[len(pts)-1] {
		pts = pts[:len(pts)-1]
	}
	keys := make([]string, len(pts))
	least := 0
	for i, p := range pts {
		keys[i] = r3(p.X) + "," + r3(p.Y)
		if keys[i] < keys[least] {
			least = i
		}
	}
	n := len(keys)
	fwd, back := "", ""
	for i := 0; i < n; i++ {
		fwd += keys[(least+i)%n] + " "
		back += keys[(least-i+n)%n] + " "
	}
	if back < fwd {
		fwd = back
	}
	return "poly " + layer + " " + fwd
}

// drawGraphic adds a graphic to a drawing in a role.
func (d *Drawing) graphic(g Graphic, role string) {
	w := g.Width
	switch g.Kind {
	case GraphicLine:
		d.line(role, g.Pts[0], g.Pts[1], w)
	case GraphicRect:
		a, b := g.Pts[0], g.Pts[1]
		pts := []Pt{a, {b.X, a.Y}, b, {a.X, b.Y}}
		d.add(Shape{Kind: ShapePoly, Role: role, Pts: pts, Width: w, Fill: g.Fill})
	case GraphicPoly:
		d.add(Shape{Kind: ShapePoly, Role: role, Pts: append([]Pt(nil), g.Pts...), Width: w, Fill: g.Fill})
	case GraphicCircle:
		d.add(Shape{Kind: ShapeCircle, Role: role, Pts: []Pt{g.Pts[0]}, R: g.R, Width: w, Fill: g.Fill})
	case GraphicArc:
		// As a polyline: the drawer has no arcs.
		o, r, a, b := ArcAngles(g)
		n := int(math.Max(4, math.Ceil((b-a)/6)))
		pts := make([]Pt, 0, n+1)
		for i := 0; i <= n; i++ {
			t := (a + (b-a)*float64(i)/float64(n)) * math.Pi / 180
			pts = append(pts, Pt{o.X + r*math.Cos(t), o.Y + r*math.Sin(t)})
		}
		d.add(Shape{Kind: ShapeLine, Role: role, Pts: pts, Width: w})
	case GraphicText:
		d.add(Shape{Kind: ShapeText, Role: role, Text: g.Text, Pts: []Pt{g.Pts[0]}, Size: g.Size, Anchor: "start", Vertical: math.Abs(math.Mod(g.Angle+360, 360)-90) < 1})
	}
}

// Tidy sets what a footprint's drawing implies once it's edited: its
// mounting from its pads, its courtyard and body boxes from what's drawn
// on those layers (the courtyard round the pads when nothing is).
func (f *Footprint) Tidy() {
	f.Mount = mountOf(f.Pads)
	var crt, fab Rect
	grow := func(r *Rect, b Rect) { *r = r.union(b) }
	for _, l := range f.Lines {
		b := Rect{math.Min(l.A.X, l.B.X), math.Min(l.A.Y, l.B.Y), math.Max(l.A.X, l.B.X), math.Max(l.A.Y, l.B.Y)}
		switch l.Layer {
		case LayerCourtyard:
			grow(&crt, b)
		case LayerFab:
			grow(&fab, b)
		}
	}
	for _, g := range f.Graphics {
		if g.Check() != nil {
			continue
		}
		switch g.Layer {
		case LayerCourtyard:
			grow(&crt, g.Bounds())
		case LayerFab:
			grow(&fab, g.Bounds())
		}
	}
	if crt == (Rect{}) {
		for _, p := range f.Pads {
			grow(&crt, p.Rect().grow(0.25))
		}
	}
	f.Courtyard, f.Body = crt, fab
}

// Problems are what's wrong with a footprint as edited: pads that can't
// be made, graphics off any layer or misshapen.
func (f Footprint) Problems() []Problem {
	var ps problems
	shapes := map[string]bool{"rect": true, "roundrect": true, "circle": true, "oval": true}
	for i, p := range f.Pads {
		name := fmt.Sprintf("pad %d (%q)", i+1, p.Number)
		switch {
		case p.Kind != PadSMD && p.Kind != PadThru && p.Kind != PadNPTH && p.Kind != PadPaste:
			ps.errf("%s: kind %q isn't smd, thru, npth or paste", name, p.Kind)
		case p.Kind != PadNPTH && (p.W <= 0 || p.H <= 0):
			ps.errf("%s has no size", name)
		case (p.Kind == PadThru || p.Kind == PadNPTH) && p.Drill <= 0:
			ps.errf("%s is a hole with no drill", name)
		case p.Kind == PadThru && p.Drill >= math.Min(p.W, p.H):
			ps.errf("%s: its %s mm hole leaves no copper round it", name, mm(p.Drill))
		case p.Kind != PadNPTH && !shapes[p.Shape]:
			ps.errf("%s: shape %q isn't rect, roundrect, circle or oval", name, p.Shape)
		case p.Terminal() && p.Number == "":
			ps.warnf("%s has no number: no pin lands on it", name)
		}
	}
	for i, g := range f.Graphics {
		if _, ok := layerRoles[g.Layer]; !ok {
			ps.errf("graphic %d: layer %q isn't silk, fab, courtyard or copper", i+1, g.Layer)
		} else if err := g.Check(); err != nil {
			ps.errf("graphic %d: %v", i+1, err)
		}
	}
	return ps
}

// Problems are what's wrong with a symbol as edited: pins without a
// number, side or type, and misshapen graphics.
func (s Symbol) Problems() []Problem {
	var ps problems
	sides := map[string]bool{Left: true, Right: true, Top: true, Bottom: true}
	types := map[string]bool{TypeInput: true, TypeOutput: true, TypeBidir: true, TypeTriState: true, TypePassive: true, TypePowerIn: true,
		TypePowerOut: true, TypeOpenDrain: true, TypeNoConnect: true, TypeUnspecified: true, "free": true, "open_emitter": true}
	seen := map[string]bool{}
	for i, p := range s.Pins {
		name := fmt.Sprintf("pin %d (%q)", i+1, p.Number)
		switch {
		case p.Number == "":
			ps.errf("pin %d has no number", i+1)
		case !sides[p.Side]:
			ps.errf("%s: side %q isn't left, right, top or bottom (the side of the body it sticks out of)", name, p.Side)
		case !types[p.Type]:
			ps.errf("%s: type %q isn't one of KiCad's electrical types", name, p.Type)
		case p.Length < 0:
			ps.errf("%s has a negative length", name)
		case seen[p.Number]:
			ps.warnf("%s: the number is used twice (stacked pins)", name)
		}
		seen[p.Number] = true
	}
	for i, g := range s.Graphics {
		if err := g.Check(); err != nil {
			ps.errf("graphic %d: %v", i+1, err)
		}
	}
	return ps
}
