package cad

import (
	"fmt"
	"math"
	"sort"
)

// A Drawing is a symbol, footprint or 3D body as shapes to draw, in
// millimetres with y down: what the app shows as SVG and the agent sees
// as a picture. Each shape has a role (copper, silk, body…) that the
// drawer colours by its theme; 3D faces carry their own shaded colour.
type Drawing struct {
	View  Rect    `json:"view"`
	Items []Shape `json:"items"`
}

// Shape kinds.
const (
	ShapeLine   = "line"
	ShapeRect   = "rect"
	ShapePoly   = "poly"
	ShapeCircle = "circle"
	ShapeText   = "text"
)

// Roles a shape is drawn in.
const (
	RoleCopper    = "copper"
	RolePaste     = "paste"
	RoleDrill     = "drill"
	RoleSilk      = "silk"
	RoleFab       = "fab"
	RoleCourtyard = "courtyard"
	RoleBody      = "body"
	RolePin       = "pin"
	RolePinName   = "pinName"
	RolePinNumber = "pinNumber"
	RoleText      = "text"
	RoleDim       = "dim"
	RoleFace      = "face"
)

// Shape is one thing drawn.
type Shape struct {
	Kind string `json:"kind"`
	Role string `json:"role"`
	Pts  []Pt   `json:"pts,omitempty"`
	// R is a circle's radius, or a rect's corner radius.
	R     float64 `json:"r,omitempty"`
	Width float64 `json:"width,omitempty"`
	Text  string  `json:"text,omitempty"`
	Size  float64 `json:"size,omitempty"`
	// Anchor is start, middle or end; Vertical text reads bottom to top.
	Anchor   string `json:"anchor,omitempty"`
	Vertical bool   `json:"vertical,omitempty"`
	// Fill fills a drawn rect, poly or circle.
	Fill bool `json:"fill,omitempty"`
	// Color is a 3D face's shaded colour (#rrggbb).
	Color string `json:"color,omitempty"`
}

// bounds grows a view around points.
func (d *Drawing) grow(pts ...Pt) {
	for _, p := range pts {
		d.View = d.View.union(Rect{p.X, p.Y, p.X, p.Y})
	}
}

// add adds a shape, growing the view.
func (d *Drawing) add(s Shape) {
	for i := range s.Pts {
		s.Pts[i] = Pt{Number(s.Pts[i].X), Number(s.Pts[i].Y)}
	}
	s.R, s.Width, s.Size = Number(s.R), Number(s.Width), Number(s.Size)
	d.Items = append(d.Items, s)
	switch s.Kind {
	case ShapeCircle:
		d.grow(Pt{s.Pts[0].X - s.R, s.Pts[0].Y - s.R}, Pt{s.Pts[0].X + s.R, s.Pts[0].Y + s.R})
	case ShapeText:
		w := float64(len([]rune(s.Text))) * s.Size * 0.6
		a := map[string]float64{"middle": w / 2, "end": w}[s.Anchor]
		p := s.Pts[0]
		if s.Vertical { // reads bottom to top: its start is at the bottom
			d.grow(Pt{p.X - s.Size, p.Y + a}, Pt{p.X, p.Y + a - w})
		} else {
			d.grow(Pt{p.X - a, p.Y - s.Size}, Pt{p.X - a + w, p.Y})
		}
	default:
		d.grow(s.Pts...)
	}
}

// line adds a line.
func (d *Drawing) line(role string, a, b Pt, w float64) {
	d.add(Shape{Kind: ShapeLine, Role: role, Pts: []Pt{a, b}, Width: w})
}

// text adds a text.
func (d *Drawing) text(role, s string, at Pt, size float64, anchor string, vertical bool) {
	d.add(Shape{Kind: ShapeText, Role: role, Text: s, Pts: []Pt{at}, Size: size, Anchor: anchor, Vertical: vertical})
}

// pad adds a margin round the view.
func (d *Drawing) pad(m float64) {
	v := d.View.grow(m)
	d.View = Rect{Number(v.X0), Number(v.Y0), Number(v.X1), Number(v.Y1)}
}

// DrawSymbol draws a symbol: its body, its pins with their numbers along
// them and their names inside the body (y flipped to point down).
func DrawSymbol(s Symbol) Drawing {
	var d Drawing
	b := s.Body
	if b != (Rect{}) {
		d.add(Shape{Kind: ShapeRect, Role: RoleBody, Pts: []Pt{{b.X0, -b.Y1}, {b.X1, -b.Y0}}, Width: 0.254})
	}
	for _, l := range s.Extra {
		d.line(RoleBody, Pt{l.A.X, -l.A.Y}, Pt{l.B.X, -l.B.Y}, 0.254)
	}
	for _, g := range s.Graphics {
		if g.Check() == nil {
			d.graphic(g.Flip(), RoleBody)
		}
	}
	const size = 1.27
	for _, p := range s.Pins {
		if p.Hidden {
			continue
		}
		// "~" is KiCad's "no name".
		if p.Name == "~" {
			p.Name = ""
		}
		end := Pt{p.X, -p.Y}
		var in Pt
		switch p.Side {
		case Left:
			in = Pt{p.X + p.Length, -p.Y}
		case Right:
			in = Pt{p.X - p.Length, -p.Y}
		case Top:
			in = Pt{p.X, -p.Y + p.Length}
		default:
			in = Pt{p.X, -p.Y - p.Length}
		}
		d.line(RolePin, end, in, 0.2)
		mid := Pt{(end.X + in.X) / 2, (end.Y + in.Y) / 2}
		switch p.Side {
		case Left:
			d.text(RolePinNumber, p.Number, Pt{mid.X, mid.Y - 0.35}, size*0.8, "middle", false)
			d.text(RolePinName, p.Name, Pt{in.X + 0.6, in.Y + 0.45}, size, "start", false)
		case Right:
			d.text(RolePinNumber, p.Number, Pt{mid.X, mid.Y - 0.35}, size*0.8, "middle", false)
			d.text(RolePinName, p.Name, Pt{in.X - 0.6, in.Y + 0.45}, size, "end", false)
		case Top:
			d.text(RolePinNumber, p.Number, Pt{mid.X - 0.35, mid.Y}, size*0.8, "middle", true)
			d.text(RolePinName, p.Name, Pt{in.X + 0.45, in.Y + 0.6}, size, "end", true)
		default:
			d.text(RolePinNumber, p.Number, Pt{mid.X - 0.35, mid.Y}, size*0.8, "middle", true)
			d.text(RolePinName, p.Name, Pt{in.X + 0.45, in.Y - 0.6}, size, "start", true)
		}
	}
	top, bottom := -b.Y1, -b.Y0
	for _, p := range s.Pins {
		top, bottom = math.Min(top, -p.Y), math.Max(bottom, -p.Y)
	}
	d.text(RoleText, s.Reference+"?", Pt{b.X0, top - 1}, size, "start", false)
	d.text(RoleText, s.Value, Pt{b.X0, bottom + 2}, size, "start", false)
	d.pad(1)
	return d
}

// layerRoles are the roles footprint layers are drawn in.
var layerRoles = map[string]string{LayerSilk: RoleSilk, LayerFab: RoleFab, LayerCourtyard: RoleCourtyard, LayerCopper: RoleCopper}

// DrawFootprint draws a footprint from the top: courtyard, fabrication
// outline, silkscreen, pads with their numbers and paste, and the pitch,
// row span and pad size dimensioned so they can be checked against the
// datasheet's drawing.
func DrawFootprint(f Footprint) Drawing {
	var d Drawing
	for _, l := range f.Lines {
		d.line(layerRoles[l.Layer], l.A, l.B, l.Width)
	}
	for _, p := range f.Polys {
		d.add(Shape{Kind: ShapePoly, Role: RoleSilk, Pts: p.Pts})
	}
	for _, g := range f.Graphics {
		if g.Check() == nil {
			d.graphic(g, layerRoles[g.Layer])
		}
	}
	for _, p := range f.Pads {
		r := p.Rect()
		rr := 0.0
		switch p.Shape {
		case "roundrect":
			rr = 0.25 * math.Min(p.W, p.H)
		case "oval", "circle":
			rr = math.Min(p.W, p.H) / 2
		}
		switch {
		case p.Kind == PadNPTH:
			d.add(Shape{Kind: ShapeCircle, Role: RoleDrill, Pts: []Pt{{p.X, p.Y}}, R: p.Drill / 2})
			d.add(Shape{Kind: ShapeCircle, Role: RoleFab, Pts: []Pt{{p.X, p.Y}}, R: p.Drill / 2, Width: 0.05})
		case p.Kind == PadPaste:
			d.add(Shape{Kind: ShapeRect, Role: RolePaste, Pts: []Pt{{r.X0, r.Y0}, {r.X1, r.Y1}}, R: 0.05})
		case p.Shape == "circle":
			d.add(Shape{Kind: ShapeCircle, Role: RoleCopper, Pts: []Pt{{p.X, p.Y}}, R: p.W / 2})
		default:
			d.add(Shape{Kind: ShapeRect, Role: RoleCopper, Pts: []Pt{{r.X0, r.Y0}, {r.X1, r.Y1}}, R: rr})
		}
		if p.Kind == PadThru {
			d.add(Shape{Kind: ShapeCircle, Role: RoleDrill, Pts: []Pt{{p.X, p.Y}}, R: p.Drill / 2})
		}
	}
	for _, p := range f.Pads {
		if p.Terminal() {
			size := math.Min(0.8, 0.7*math.Min(p.W, p.H))
			if len(f.Pads) > 6 && p.W > 2 && p.H > 1 { // an exposed pad
				size = 0.8
			}
			d.text(RoleText, p.Number, Pt{p.X, p.Y + size*0.35}, size, "middle", false)
		}
	}
	dimension(&d, f)
	d.pad(0.6)
	return d
}

// dimension adds the pitch, the row span and the size of pad 1.
func dimension(d *Drawing, f Footprint) {
	var p1, p2, across *Pad
	for i := range f.Pads {
		p := &f.Pads[i]
		if !p.Terminal() {
			continue
		}
		if p1 == nil {
			p1 = p
			continue
		}
		if p2 == nil && p.X == p1.X && p.W == p1.W {
			p2 = p
		}
		if across == nil && p.Y == p1.Y && p.X != p1.X && p.H == p1.H {
			across = p
		}
	}
	if p1 == nil {
		return
	}
	const size = 0.5
	left := f.Courtyard.X0 - 0.4
	if p2 != nil {
		y0, y1 := p1.Y, p2.Y
		d.line(RoleDim, Pt{left, y0}, Pt{left, y1}, 0.05)
		d.line(RoleDim, Pt{left - 0.2, y0}, Pt{p1.X - p1.W/2 - 0.1, y0}, 0.03)
		d.line(RoleDim, Pt{left - 0.2, y1}, Pt{p2.X - p2.W/2 - 0.1, y1}, 0.03)
		d.text(RoleDim, "pitch "+mm(math.Abs(y1-y0)), Pt{left - 0.25, (y0 + y1) / 2}, size, "middle", true)
	}
	top := f.Courtyard.Y0 - 0.4
	if across != nil {
		x0, x1 := p1.X, across.X
		d.line(RoleDim, Pt{x0, top}, Pt{x1, top}, 0.05)
		d.line(RoleDim, Pt{x0, top - 0.2}, Pt{x0, p1.Y - p1.H/2 - 0.1}, 0.03)
		d.line(RoleDim, Pt{x1, top - 0.2}, Pt{x1, across.Y - across.H/2 - 0.1}, 0.03)
		d.text(RoleDim, "rows "+mm(math.Abs(x1-x0))+" c-c", Pt{(x0 + x1) / 2, top - 0.25}, size, "middle", false)
	}
	bottom := f.Courtyard.Y1 + 0.8
	d.text(RoleDim, fmt.Sprintf("pad %s × %s  ·  %s", mm(p1.W), mm(p1.H), f.From), Pt{(f.Courtyard.X0 + f.Courtyard.X1) / 2, bottom}, size, "middle", false)
}

// face is a 3D face projected, with its depth and shade.
type face struct {
	pts   []Pt
	depth float64
	// rank puts it under (-1) or over (1) the body before anything else,
	// then solid is its solid's depth.
	rank  int
	solid float64
	color string
}

// onBody is -1 when a solid sits under a body (out past a face of it the
// viewer doesn't see), 1 when over it (out past one they see), 0 when it
// touches no body.
func onBody(s Solid, bodies []Solid, seen func(V3) bool) int {
	lo, hi := Bounds([]Solid{s})
	rank := 0
	for _, b := range bodies {
		blo, bhi := Bounds([]Solid{b})
		if !overlaps(lo, hi, blo, bhi, 0.05) {
			continue
		}
		for _, f := range b.Faces {
			nv, o := newell(b.Verts, f), b.Verts[f[0]]
			out := false
			for _, v := range s.Verts {
				if dot3(nv, V3{v[0] - o[0], v[1] - o[1], v[2] - o[2]}) > 1e-6 {
					out = true
					break
				}
			}
			if !out {
				continue
			}
			if seen(nv) {
				return 1
			}
			rank = -1
		}
	}
	return rank
}

// overlaps reports whether two boxes meet, within tol.
func overlaps(lo, hi, blo, bhi V3, tol float64) bool {
	for k := 0; k < 3; k++ {
		if lo[k] > bhi[k]+tol || blo[k] > hi[k]+tol {
			return false
		}
	}
	return true
}

// insideAny reports whether a point is inside one of the (convex) bodies.
func insideAny(p V3, bodies []Solid) bool {
	for _, b := range bodies {
		in := true
		for _, f := range b.Faces {
			nv, o := newell(b.Verts, f), b.Verts[f[0]]
			if dot3(nv, V3{p[0] - o[0], p[1] - o[1], p[2] - o[2]}) > -1e-6 {
				in = false
				break
			}
		}
		if in {
			return true
		}
	}
	return false
}

// Draw3D draws solids as an isometric view from above the front right,
// shaded, far faces first.
func Draw3D(ss []Solid) Drawing {
	const yaw, tilt = -35 * math.Pi / 180, 55 * math.Pi / 180
	ca, sa, cb, sb := math.Cos(yaw), math.Sin(yaw), math.Cos(tilt), math.Sin(tilt)
	light := unit3(V3{-0.4, -0.6, 1})
	project := func(v V3) (Pt, float64) {
		x1 := v[0]*ca - v[1]*sa
		y1 := v[0]*sa + v[1]*ca
		up := y1*sb + v[2]*cb
		depth := y1*cb - v[2]*sb
		return Pt{x1, -up}, depth
	}
	seen := func(nv V3) bool {
		return (nv[0]*sa+nv[1]*ca)*cb-nv[2]*sb < -1e-9
	}
	var bodies []Solid
	for _, s := range ss {
		if s.Part == PartBody {
			bodies = append(bodies, s)
		}
	}
	var faces []face
	for _, s := range ss {
		rgb := s.RGB()
		// Solids are drawn far to near by their middles, and each one's
		// faces far to near: comparing single faces of different solids
		// puts a tall pin's far side over the body in front of it.
		var solidDepth float64
		for _, v := range s.Verts {
			_, dp := project(v)
			solidDepth += dp / float64(len(s.Verts))
		}
		// A lead, pad or mark goes over the body it sits on when it
		// sticks out of a face of it the viewer sees, under it otherwise:
		// by their middles alone, a no-lead part's terminals down its far
		// end go under the body they show on.
		rank := 0
		if s.Part != PartBody {
			rank = onBody(s, bodies, seen)
		}
		for _, f := range s.Faces {
			nv := newell(s.Verts, f)
			ny1 := nv[0]*sa + nv[1]*ca
			if ny1*cb-nv[2]*sb >= -1e-9 { // facing away
				continue
			}
			var fc face
			var mid V3
			for _, i := range f {
				p, dp := project(s.Verts[i])
				fc.pts = append(fc.pts, p)
				fc.depth += dp
				for k := range mid {
					mid[k] += s.Verts[i][k] / float64(len(f))
				}
			}
			// A terminal's faces inside the body are hidden by it.
			if rank != 0 && insideAny(mid, bodies) {
				continue
			}
			fc.depth /= float64(len(f))
			fc.solid = solidDepth
			fc.rank = rank
			k := 0.5 + 0.5*math.Max(0, dot3(nv, light))
			fc.color = fmt.Sprintf("#%02x%02x%02x", int(255*rgb[0]*k), int(255*rgb[1]*k), int(255*rgb[2]*k))
			faces = append(faces, fc)
		}
	}
	sort.SliceStable(faces, func(i, j int) bool {
		if faces[i].rank != faces[j].rank {
			return faces[i].rank < faces[j].rank
		}
		if faces[i].solid != faces[j].solid {
			return faces[i].solid > faces[j].solid
		}
		return faces[i].depth > faces[j].depth
	})
	var d Drawing
	for _, f := range faces {
		d.add(Shape{Kind: ShapePoly, Role: RoleFace, Pts: f.pts, Color: f.color})
	}
	d.pad(0.3)
	return d
}

// newell is a face's unit normal.
func newell(vs []V3, f []int) V3 {
	var nv V3
	for i := range f {
		a, b := vs[f[i]], vs[f[(i+1)%len(f)]]
		nv[0] += (a[1] - b[1]) * (a[2] + b[2])
		nv[1] += (a[2] - b[2]) * (a[0] + b[0])
		nv[2] += (a[0] - b[0]) * (a[1] + b[1])
	}
	return unit3(nv)
}

// unit3 is a vector of length 1.
func unit3(v V3) V3 {
	l := math.Sqrt(dot3(v, v))
	if l == 0 {
		return v
	}
	return V3{v[0] / l, v[1] / l, v[2] / l}
}

// dot3 is the dot product.
func dot3(a, b V3) float64 { return a[0]*b[0] + a[1]*b[1] + a[2]*b[2] }
