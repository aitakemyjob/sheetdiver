package cad

import (
	"math"
	"strconv"
	"strings"
)

// Through-hole packages: pins go through plated holes. A hole is sized
// from the lead (IPC-2222: the lead's diameter, or a square lead's
// diagonal, plus clearance), a pad from its hole plus an annular ring, and
// pin 1's pad is square. Their 3D models' leads go through the board.

// The through-hole families.
func init() {
	Register(dip{})
	Register(header{})
	Register(inline{})
	Register(radial{})
	Register(axial{})
	Register(custom{})
}

// thHole is a through-hole pad's hole and diameter: the datasheet's, else
// from the lead.
func thHole(p Package, family string, ps *problems) (drill, size float64, from string) {
	if l := p.Land; l != nil && l.Drill > 0 {
		size = l.PadLength
		if size <= 0 {
			size = Number(ceilTo(l.Drill+0.8, 0.05))
		}
		return l.Drill, size, FromDatasheet
	}
	lead := p.LeadWidth.Hi()
	if lead <= 0 {
		ps.errf("a %s package needs leadWidth (the lead's diameter, or width with leadThickness)", family)
		return 0, 0, ""
	}
	clearance := 0.2
	if t := p.LeadThickness.Hi(); t > 0 { // a rectangular lead fits by its diagonal
		lead, clearance = math.Hypot(lead, t), 0.1
	}
	drill = Number(ceilTo(lead+clearance, 0.05))
	return drill, Number(ceilTo(drill+0.8, 0.05)), FromCalculated
}

// place is a pin and where its hole is.
type place struct {
	num  string
	x, y float64
}

// placesOf puts slots at their points.
func placesOf(sl []slot, at func(slot) Pt) []place {
	out := make([]place, len(sl))
	for i, s := range sl {
		pt := at(s)
		out[i] = place{s.num, pt.X, pt.Y}
	}
	return out
}

// thPads are through-hole pads at places, pin 1 square, each w × h.
func thPads(pl []place, drill, w, h float64, ps *problems) []Pad {
	var out []Pad
	for _, s := range pl {
		shape := "circle"
		if w != h {
			shape = "oval"
		}
		if s.num == "1" {
			shape = "rect"
		}
		out = append(out, Pad{Number: s.num, Kind: PadThru, Shape: shape, X: Number(s.x), Y: Number(s.y), W: Number(w), H: Number(h), Drill: drill})
	}
	if ring := (math.Min(w, h) - drill) / 2; ring < 0.15 {
		ps.warnf("the annular ring is %s mm (under 0.15 mm): the pads are too close for their holes", mm(ring))
	}
	return out
}

// fit narrows a pad to leave 0.25 mm between neighbours at a pitch.
func fit(size, pitch float64) float64 {
	if pitch > 0 && size > pitch-0.25 {
		return Number(floorTo(pitch-0.25, 0.05))
	}
	return size
}

// below is how far leads stick out under the board.
func below(p Package) float64 {
	if p.LeadBelow > 0 {
		return p.LeadBelow
	}
	return 3
}

// or is v, else d.
func or(v, d float64) float64 {
	if v > 0 {
		return v
	}
	return d
}

// dip is a dual in-line package: two rows of leads, numbered down the
// left and up the right.
type dip struct{}

// Name is the family's name.
func (dip) Name() string { return "dip" }

// geometry is a DIP's pitch and row spacing.
func (dip) geometry(p Package) (pitch, rows float64) {
	return or(p.Pitch, 2.54), or(p.RowSpacing.Mid(), 7.62)
}

// Pads are the two rows of holes.
func (f dip) Pads(p Package, ps *problems) ([]Pad, Rect, string) {
	need(ps, "dip", map[string]bool{"pins": p.Pins > 0, "bodyLength": p.BodyLength.Set(), "bodyWidth": p.BodyWidth.Set()})
	counts := sideCounts(p, false, ps)
	drill, size, from := thHole(p, "dip", ps)
	if ps.failed() {
		return nil, Rect{}, ""
	}
	pitch, rows := f.geometry(p)
	size = fit(size, pitch)
	at := func(s slot) Pt {
		if s.side == sideLeft {
			return Pt{-rows / 2, s.at}
		}
		return Pt{rows / 2, s.at}
	}
	return thPads(placesOf(slots(numbering(p, counts), pitch), at), drill, size, size, ps), centred(0, 0, p.BodyWidth.Mid(), p.BodyLength.Mid()), from
}

// Solids are the body on its standoff, and each lead from the body's side
// down through the board.
func (f dip) Solids(p Package) []Solid {
	pitch, rows := f.geometry(p)
	a, a1 := height(p), or(p.Standoff.Mid(), 0.5)
	bw, bl := p.BodyWidth.Mid(), p.BodyLength.Mid()
	out := []Solid{box(PartBody, -bw/2, -bl/2, a1, bw/2, bl/2, a), pin1Dimple(bw, bl, a)}
	var ps problems
	w, t := or(p.LeadWidth.Mid(), 0.46), or(p.LeadThickness.Mid(), 0.25)
	for _, s := range slots(numbering(p, sideCounts(p, false, &ps)), pitch) {
		x := rows / 2
		if s.side == sideLeft {
			x = -x
		}
		edge := math.Copysign(bw/2, x)
		out = append(out,
			box(PartLead, x-t/2, s.at-w/2, -below(p), x+t/2, s.at+w/2, a1+0.6),
			box(PartLead, math.Min(edge, x), s.at-w*0.8, a1+0.4, math.Max(edge, x), s.at+w*0.8, a1+0.6))
	}
	return out
}

// header is a pin header or single in-line package: one or two rows of
// pins; two rows are numbered across (1 2 / 3 4 …), as connectors are.
type header struct{}

// Name is the family's name.
func (header) Name() string { return "header" }

// grid is a header's pitch, its rows, pins per row and row spacing.
func (header) grid(p Package) (pitch float64, rows, per int, spacing float64) {
	pitch = or(p.Pitch, 2.54)
	rows = max(p.Rows, 1)
	return pitch, rows, p.Pins / rows, or(p.RowSpacing.Mid(), pitch)
}

// places are the pins' places, top to bottom; across the rows first.
func (h header) places(p Package) []place {
	pitch, rows, per, spacing := h.grid(p)
	var out []place
	n := 1
	for i := 0; i < per; i++ {
		for r := 0; r < rows; r++ {
			out = append(out, place{strconv.Itoa(n), (float64(r) - float64(rows-1)/2) * spacing, (float64(i) - float64(per-1)/2) * pitch})
			n++
		}
	}
	return out
}

// body is the header's plastic: the given body, else the pins' extent.
func (h header) body(p Package) (w, l float64) {
	pitch, rows, per, spacing := h.grid(p)
	return or(p.BodyWidth.Mid(), float64(rows-1)*spacing+pitch), or(p.BodyLength.Mid(), float64(per)*pitch)
}

// Pads are the rows of holes.
func (h header) Pads(p Package, ps *problems) ([]Pad, Rect, string) {
	_, rows, _, _ := h.grid(p)
	need(ps, "header", map[string]bool{"pins": p.Pins > 0})
	if p.Pins%rows != 0 {
		ps.errf("%d pins don't make %d rows", p.Pins, rows)
	}
	drill, size, from := thHole(p, "header", ps)
	if ps.failed() {
		return nil, Rect{}, ""
	}
	pitch, _, _, _ := h.grid(p)
	size = fit(size, pitch)
	w, l := h.body(p)
	return thPads(h.places(p), drill, size, size, ps), centred(0, 0, w, l), from
}

// Solids are the plastic strip and square pins standing up through it.
func (h header) Solids(p Package) []Solid {
	w, l := h.body(p)
	a := height(p)
	out := []Solid{box(PartBody, -w/2, -l/2, 0, w/2, l/2, a)}
	side := or(p.LeadWidth.Mid(), 0.64)
	for _, s := range h.places(p) {
		out = append(out, box(PartLead, s.x-side/2, s.y-side/2, -below(p), s.x+side/2, s.y+side/2, a+6))
	}
	return out
}

// inline is one row of leads under a body: TO-92, TO-220, TO-247 standing
// up. The row runs left to right from pin 1; the body sits over it, or
// behind it by BodyOffset.
type inline struct{}

// Name is the family's name.
func (inline) Name() string { return "inline" }

// places are the leads, left to right.
func (inline) places(p Package) []place {
	pitch := or(p.Pitch, 2.54)
	var out []place
	for i := 0; i < p.Pins; i++ {
		n := strconv.Itoa(i + 1)
		if p.Layout != nil && i < len(p.Layout.Left) {
			n = p.Layout.Left[i]
		}
		if n != "" {
			out = append(out, place{n, (float64(i) - float64(p.Pins-1)/2) * pitch, 0})
		}
	}
	return out
}

// Pads are a row of holes; pads narrower than the pitch are ovals, tall
// across the row.
func (f inline) Pads(p Package, ps *problems) ([]Pad, Rect, string) {
	need(ps, "inline", map[string]bool{"pins": p.Pins > 0, "bodyLength": p.BodyLength.Set(), "bodyWidth": p.BodyWidth.Set()})
	drill, size, from := thHole(p, "inline", ps)
	if ps.failed() {
		return nil, Rect{}, ""
	}
	w := fit(size, or(p.Pitch, 2.54))
	return thPads(f.places(p), drill, w, size, ps), centred(0, -p.BodyOffset, p.BodyLength.Mid(), p.BodyWidth.Mid()), from
}

// Solids are the body standing over its leads.
func (f inline) Solids(p Package) []Solid {
	bl, bw := p.BodyLength.Mid(), p.BodyWidth.Mid()
	a, a1 := height(p), or(p.Standoff.Mid(), 1)
	out := []Solid{box(PartBody, -bl/2, -p.BodyOffset-bw/2, a1, bl/2, -p.BodyOffset+bw/2, a)}
	w, t := or(p.LeadWidth.Mid(), 0.5), or(p.LeadThickness.Mid(), 0.4)
	for _, s := range f.places(p) {
		out = append(out, box(PartLead, s.x-w/2, -t/2, -below(p), s.x+w/2, t/2, a1+0.5))
	}
	return out
}

// twoLeads are a two-lead part's leads, pitch apart: 1 and 2, or the
// layout's names (A and K, + and -).
func twoLeads(p Package) []place {
	pl := []place{{"1", -p.Pitch / 2, 0}, {"2", p.Pitch / 2, 0}}
	if p.Layout != nil && len(p.Layout.Left) == 2 {
		pl[0].num, pl[1].num = p.Layout.Left[0], p.Layout.Left[1]
	}
	return pl
}

// radial is a round body standing on two leads: electrolytic and other
// capacitors, LEDs.
type radial struct{}

// Name is the family's name.
func (radial) Name() string { return "radial" }

// diameter is the body's diameter.
func (radial) diameter(p Package) float64 { return or(p.BodyWidth.Mid(), p.BodyLength.Mid()) }

// Pads are the two holes, pitch apart.
func (r radial) Pads(p Package, ps *problems) ([]Pad, Rect, string) {
	need(ps, "radial", map[string]bool{"pitch": p.Pitch > 0, "bodyWidth": r.diameter(p) > 0})
	drill, size, from := thHole(p, "radial", ps)
	if ps.failed() {
		return nil, Rect{}, ""
	}
	d := r.diameter(p)
	return thPads(twoLeads(p), drill, fit(size, p.Pitch), fit(size, p.Pitch), ps), centred(0, 0, d, d), from
}

// Outline is the can as circles: the body on the fab layer, just outside
// it on the silkscreen.
func (r radial) Outline(p Package, body Rect) []Line {
	d := r.diameter(p)
	return append(circle(LayerFab, d/2, 0.1), circle(LayerSilk, d/2+0.12, 0.12)...)
}

// circle is a circle of radius r about the origin, as 48 lines.
func circle(layer string, r, w float64) []Line {
	var out []Line
	const n = 48
	for i := 0; i < n; i++ {
		a, b := 2*math.Pi*float64(i)/n, 2*math.Pi*float64(i+1)/n
		out = append(out, Line{layer, Pt{Number(r * math.Cos(a)), Number(r * math.Sin(a))}, Pt{Number(r * math.Cos(b)), Number(r * math.Sin(b))}, w})
	}
	return out
}

// Solids are the cylinder and its two leads.
func (r radial) Solids(p Package) []Solid {
	d, a, a1 := r.diameter(p), height(p), or(p.Standoff.Mid(), 0.5)
	lead := or(p.LeadWidth.Mid(), 0.6)
	return []Solid{
		prism(PartBody, 0, 0, d/2, a1, a, 32),
		prism(PartLead, -p.Pitch/2, 0, lead/2, -below(p), a1+0.1, 8),
		prism(PartLead, p.Pitch/2, 0, lead/2, -below(p), a1+0.1, 8),
	}
}

// axial is a cylinder lying on the board, its leads bent down at each
// end: resistors, diodes.
type axial struct{}

// Name is the family's name.
func (axial) Name() string { return "axial" }

// Pads are the two holes, pitch apart.
func (axial) Pads(p Package, ps *problems) ([]Pad, Rect, string) {
	need(ps, "axial", map[string]bool{"pitch": p.Pitch > 0, "bodyLength": p.BodyLength.Set(), "bodyWidth": p.BodyWidth.Set()})
	drill, size, from := thHole(p, "axial", ps)
	if ps.failed() {
		return nil, Rect{}, ""
	}
	if p.Pitch < p.BodyLength.Mid()+size {
		ps.errf("a pitch of %s mm doesn't clear a %s mm body: the leads need room to bend", mm(p.Pitch), mm(p.BodyLength.Mid()))
		return nil, Rect{}, ""
	}
	return thPads(twoLeads(p), drill, size, size, ps), centred(0, 0, p.BodyLength.Mid(), p.BodyWidth.Mid()), from
}

// Solids are the body and its leads.
func (axial) Solids(p Package) []Solid {
	l, d := p.BodyLength.Mid(), p.BodyWidth.Mid()
	lead := or(p.LeadWidth.Mid(), 0.6)
	z := or(p.Standoff.Mid(), 0) + d/2
	return []Solid{
		rod(PartBody, -l/2, l/2, 0, z, d/2, 24),
		rod(PartLead, -p.Pitch/2, -l/2, 0, z, lead/2, 8),
		rod(PartLead, l/2, p.Pitch/2, 0, z, lead/2, 8),
		prism(PartLead, -p.Pitch/2, 0, lead/2, -below(p), z, 8),
		prism(PartLead, p.Pitch/2, 0, lead/2, -below(p), z, 8),
	}
}

// rod is a cylinder lying along x from x0 to x1, centred on (y, z).
func rod(part string, x0, x1, y, z, r float64, n int) Solid {
	s := Solid{Part: part}
	for _, x := range []float64{x0, x1} {
		for i := 0; i < n; i++ {
			a := 2 * math.Pi * (float64(i) + 0.5) / float64(n)
			s.Verts = append(s.Verts, V3{Number(x), Number(-y + r*math.Cos(a)), Number(z + r*math.Sin(a))})
		}
	}
	start, end := make([]int, n), make([]int, n)
	for i := 0; i < n; i++ {
		start[i] = i
		end[i] = 2*n - 1 - i
		j := (i + 1) % n
		s.Faces = append(s.Faces, []int{i, n + i, n + j, j})
	}
	s.Faces = append([][]int{start, end}, s.Faces...)
	return s
}

// custom is any package drawn pad by pad from its drawing: connectors,
// modules, power packages with tabs, odd inductors.
type custom struct{}

// Name is the family's name.
func (custom) Name() string { return "custom" }

// Pads are the pads as listed.
func (custom) Pads(p Package, ps *problems) ([]Pad, Rect, string) {
	if len(p.Pads) == 0 {
		ps.errf("a custom package lists its pads: {number, x, y, w, h, shape, drill, kind}")
		return nil, Rect{}, ""
	}
	var pads []Pad
	var box Rect
	for i, s := range p.Pads {
		pad := Pad{Number: s.Number, X: s.X, Y: s.Y, W: s.W, H: or(s.H, s.W), Shape: s.Shape, Drill: s.Drill}
		switch strings.ToLower(s.Kind) {
		case PadNPTH:
			pad.Kind, pad.Number, pad.H = PadNPTH, "", or(s.H, s.Drill)
			pad.W = or(s.W, s.Drill)
		case PadThru:
			pad.Kind = PadThru
		case "", PadSMD:
			pad.Kind = PadSMD
			if s.Drill > 0 {
				pad.Kind = PadThru
			}
		default:
			ps.errf("pad %d: kind %q isn't smd, thru or npth", i+1, s.Kind)
		}
		if pad.W <= 0 || (pad.Kind != PadSMD && pad.Drill <= 0) {
			ps.errf("pad %d (%s): needs a size, and a drill if it's a hole", i+1, s.Number)
			continue
		}
		if pad.Kind == PadThru && pad.Drill >= math.Min(pad.W, pad.H) {
			ps.errf("pad %s: its hole (%s mm) is as big as the pad", s.Number, mm(pad.Drill))
		}
		if pad.Shape == "" {
			pad.Shape = "rect"
			if pad.Kind != PadSMD && pad.W == pad.H {
				pad.Shape = "circle"
			}
		}
		if pad.Kind == PadSMD && pad.Number == "" {
			ps.warnf("pad %d has no number: it won't connect to anything", i+1)
		}
		box = box.union(pad.Rect())
		pads = append(pads, pad)
	}
	body := box
	if p.BodyWidth.Set() && p.BodyLength.Set() {
		body = centred(0, 0, p.BodyWidth.Mid(), p.BodyLength.Mid())
	}
	return pads, body, FromDatasheet
}

// Solids are the body over its pads, pins down through the board for the
// holes and thin leads on the surface pads.
func (c custom) Solids(p Package) []Solid {
	var ps problems
	pads, body, _ := c.Pads(p, &ps)
	if ps.failed() {
		return nil
	}
	a, a1 := height(p), or(p.Standoff.Mid(), 0)
	out := []Solid{box(PartBody, body.X0, body.Y0, a1+0.05, body.X1, body.Y1, a)}
	for _, pad := range pads {
		switch pad.Kind {
		case PadThru:
			out = append(out, prism(PartLead, pad.X, pad.Y, pad.Drill*0.4, -below(p), a1+0.1, 8))
		case PadSMD:
			out = append(out, box(PartLead, pad.X-pad.W*0.4, pad.Y-pad.H*0.4, 0, pad.X+pad.W*0.4, pad.Y+pad.H*0.4, 0.05))
		}
	}
	return out
}
