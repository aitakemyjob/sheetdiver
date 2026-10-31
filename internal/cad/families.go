package cad

import (
	"math"
	"strconv"
)

// The package families.
func init() {
	Register(leaded{name: "gullwing"})
	Register(leaded{name: "qfp", quad: true})
	Register(noLead{name: "dfn"})
	Register(noLead{name: "qfn", quad: true})
	Register(chip{})
	Register(bga{})
}

// side maps a box given outward from the package's middle (d0 to d1) and
// along a side (a0 to a1) to footprint coordinates.
func sideBox(part string, side int, d0, d1, a0, a1, z0, z1 float64) Solid {
	switch side {
	case sideLeft:
		return box(part, -d0, a0, z0, -d1, a1, z1)
	case sideRight:
		return box(part, d0, a0, z0, d1, a1, z1)
	case sideBottom:
		return box(part, a0, d0, z0, a1, d1, z1)
	}
	return box(part, a0, -d0, z0, a1, -d1, z1)
}

// across is the package's size across a side's terminals: its width for
// the left and right sides, its length for the top and bottom.
func across(side int, w, l float64) float64 {
	if side == sideLeft || side == sideRight {
		return w
	}
	return l
}

// need errors for each dimension a family needs that isn't given.
func need(ps *problems, family string, dims map[string]bool) {
	for _, n := range []string{"pins", "pitch", "bodyLength", "bodyWidth", "span", "leadLength", "leadWidth", "rows", "columns"} {
		if ok, asked := dims[n]; asked && !ok {
			ps.errf("a %s package needs %s", family, n)
		}
	}
}

// pin1Dimple is the mark on top of the body by pin 1.
func pin1Dimple(bw, bl, a float64) Solid {
	r := math.Min(0.25, 0.1*math.Min(bw, bl))
	return prism(PartMark, -bw/2+0.25+r, -bl/2+0.25+r, r, a-0.02, a+0.01, 12)
}

// epSolid is the exposed pad under the body.
func epSolid(p Package, z float64) []Solid {
	if p.ExposedPad == nil {
		return nil
	}
	return []Solid{box(PartPad, -p.ExposedPad.W/2, -p.ExposedPad.H/2, 0, p.ExposedPad.W/2, p.ExposedPad.H/2, z)}
}

// leaded is a package with gull-wing leads on two sides (SOIC, SOP,
// TSSOP, MSOP, SOT-23) or four (QFP).
type leaded struct {
	name string
	quad bool
}

// Name is the family's name.
func (f leaded) Name() string { return f.name }

// Pads are the land pattern: the datasheet's, else IPC-7351 nominal.
func (f leaded) Pads(p Package, ps *problems) ([]Pad, Rect, string) {
	land := p.Land != nil && p.Land.PadLength > 0 && p.Land.PadWidth > 0 && p.Land.RowSpan > 0
	need(ps, f.name, map[string]bool{"pins": p.Pins > 0, "pitch": p.Pitch > 0 || land && p.Land.Pitch > 0,
		"bodyLength": p.BodyLength.Set(), "bodyWidth": p.BodyWidth.Set(),
		"span": land || p.Span.Set(), "leadLength": land || p.LeadLength.Set(), "leadWidth": land || p.LeadWidth.Set()})
	counts := sideCounts(p, f.quad, ps)
	if ps.failed() {
		return nil, Rect{}, ""
	}
	pitch := p.Pitch
	var padL, padW, spanY, spanX float64
	from := FromCalculated
	if land {
		from = FromDatasheet
		padL, padW, spanY, spanX = p.Land.PadLength, p.Land.PadWidth, p.Land.RowSpan, p.Land.RowSpanX
		if p.Land.Pitch > 0 {
			pitch = p.Land.Pitch
		}
	} else {
		j := fillets{toe: 0.35, heel: 0.35, side: 0.03}
		if pitch <= 0.625 {
			j.side = 0.01
		}
		padL, padW, spanY = ipcLand(p.Span, p.LeadLength, p.LeadWidth, j)
		if p.SpanX.Set() {
			_, _, spanX = ipcLand(p.SpanX, p.LeadLength, p.LeadWidth, j)
		}
		if padW > pitch-0.2 {
			padW = Number(floorTo(pitch-0.2, 0.01))
		}
	}
	if spanX == 0 {
		spanX = spanY
	}
	sl := slots(numbering(p, counts), pitch)
	pads := rowPads(sl, padL, padW, spanY, spanX, "roundrect")
	pads = exposedPad(p, pads)
	if spanY-padL < 0.2 {
		ps.errf("the two rows of pads meet in the middle (inner edges %s mm apart): check the span and lead length", mm(spanY-padL))
	}
	return pads, centred(0, 0, p.BodyWidth.Mid(), p.BodyLength.Mid()), from
}

// Solids are the body, its leads (shoulder, leg and foot) and pin 1's mark.
func (f leaded) Solids(p Package) []Solid {
	a, a1 := height(p), p.Standoff.Mid()
	if a1 <= 0 {
		a1 = 0.05
	}
	c := p.LeadThickness.Mid()
	if c <= 0 {
		c = 0.15
	}
	bw, bl := p.BodyWidth.Mid(), p.BodyLength.Mid()
	out := []Solid{box(PartBody, -bw/2, -bl/2, a1, bw/2, bl/2, a), pin1Dimple(bw, bl, a)}
	var ps problems
	counts := sideCounts(p, f.quad, &ps)
	sl := slots(numbering(p, counts), p.Pitch)
	t, b := p.LeadLength.Mid(), p.LeadWidth.Mid()
	zs := a1 + (a-a1)/2 - c/2
	for _, s := range sl {
		span := p.Span.Mid()
		if (s.side == sideTop || s.side == sideBottom) && p.SpanX.Set() {
			span = p.SpanX.Mid()
		}
		tip := span / 2
		edge := across(s.side, bw, bl) / 2
		knee := math.Max(tip-t, edge+c)
		a0, a1 := s.at-b/2, s.at+b/2
		out = append(out,
			sideBox(PartLead, s.side, knee, tip, a0, a1, 0, c),
			sideBox(PartLead, s.side, knee-c, knee, a0, a1, 0, zs+c),
			sideBox(PartLead, s.side, edge-0.05, knee, a0, a1, zs, zs+c),
		)
	}
	return append(out, epSolid(p, a1+0.01)...)
}

// noLead is a package with terminals flush with its underside: two rows
// (DFN, SON) or four (QFN).
type noLead struct {
	name string
	quad bool
}

// Name is the family's name.
func (f noLead) Name() string { return f.name }

// spans are the terminals' tip-to-tip spans across the left and right
// rows and across the top and bottom: the package's span, else its body.
func (p Package) noLeadSpans() (Dim, Dim) {
	y, x := p.BodyWidth, p.BodyLength
	if p.Span.Set() {
		y = p.Span
	}
	if p.SpanX.Set() {
		x = p.SpanX
	}
	return y, x
}

// Pads are the land pattern: the datasheet's, else IPC-7351 nominal.
func (f noLead) Pads(p Package, ps *problems) ([]Pad, Rect, string) {
	land := p.Land != nil && p.Land.PadLength > 0 && p.Land.PadWidth > 0 && p.Land.RowSpan > 0
	need(ps, f.name, map[string]bool{"pins": p.Pins > 0, "pitch": p.Pitch > 0 || land && p.Land.Pitch > 0,
		"bodyLength": p.BodyLength.Set(), "bodyWidth": p.BodyWidth.Set(),
		"leadLength": land || p.LeadLength.Set(), "leadWidth": land || p.LeadWidth.Set()})
	counts := sideCounts(p, f.quad, ps)
	if ps.failed() {
		return nil, Rect{}, ""
	}
	pitch := p.Pitch
	var padL, padW, spanY, spanX float64
	from := FromCalculated
	if land {
		from = FromDatasheet
		padL, padW, spanY, spanX = p.Land.PadLength, p.Land.PadWidth, p.Land.RowSpan, p.Land.RowSpanX
		if p.Land.Pitch > 0 {
			pitch = p.Land.Pitch
		}
	} else {
		j := fillets{toe: 0.3, heel: 0, side: 0}
		sy, sx := p.noLeadSpans()
		padL, padW, spanY = ipcLand(sy, p.LeadLength, p.LeadWidth, j)
		_, _, spanX = ipcLand(sx, p.LeadLength, p.LeadWidth, j)
		if padW > pitch-0.15 {
			padW = Number(floorTo(pitch-0.15, 0.01))
		}
	}
	if spanX == 0 {
		spanX = spanY
	}
	sl := slots(numbering(p, counts), pitch)
	pads := rowPads(sl, padL, padW, spanY, spanX, "roundrect")
	pads = exposedPad(p, pads)
	return pads, centred(0, 0, p.BodyWidth.Mid(), p.BodyLength.Mid()), from
}

// Solids are the body, its terminals showing at the edges and underside,
// the exposed pad and pin 1's mark.
func (f noLead) Solids(p Package) []Solid {
	a := height(p)
	bw, bl := p.BodyWidth.Mid(), p.BodyLength.Mid()
	out := []Solid{box(PartBody, -bw/2, -bl/2, 0.02, bw/2, bl/2, a), pin1Dimple(bw, bl, a)}
	var ps problems
	sl := slots(numbering(p, sideCounts(p, f.quad, &ps)), p.Pitch)
	t, b := p.LeadLength.Mid(), p.LeadWidth.Mid()
	for _, s := range sl {
		edge := across(s.side, bw, bl) / 2
		out = append(out, sideBox(PartLead, s.side, edge-t, edge+0.01, s.at-b/2, s.at+b/2, 0, 0.2))
	}
	return append(out, epSolid(p, 0.03)...)
}

// chip is a two-terminal part with end caps: chip resistors and
// capacitors, SOD-323F and similar flat-lead diodes.
type chip struct{}

// Name is the family's name.
func (chip) Name() string { return "chip" }

// length is the chip's length end to end.
func (chip) length(p Package) Dim {
	if p.Span.Set() {
		return p.Span
	}
	return p.BodyLength
}

// Pads are two pads, 1 on the left (or the layout's names).
func (c chip) Pads(p Package, ps *problems) ([]Pad, Rect, string) {
	land := p.Land != nil && p.Land.PadLength > 0 && p.Land.PadWidth > 0 && p.Land.RowSpan > 0
	need(ps, "chip", map[string]bool{"bodyLength": c.length(p).Set(), "bodyWidth": p.BodyWidth.Set(), "leadLength": land || p.LeadLength.Set()})
	if ps.failed() {
		return nil, Rect{}, ""
	}
	names := [][]string{{"1"}, {"2"}}
	if p.Layout != nil && len(p.Layout.Left) == 1 && len(p.Layout.Right) == 1 {
		names = [][]string{p.Layout.Left, p.Layout.Right}
	}
	var padL, padW, span float64
	from := FromCalculated
	if land {
		from = FromDatasheet
		padL, padW, span = p.Land.PadLength, p.Land.PadWidth, p.Land.RowSpan
	} else {
		j := fillets{toe: 0.35}
		if c.length(p).Mid() < 1.5 {
			j.toe = 0.2
		}
		width := p.LeadWidth
		if !width.Set() {
			width = p.BodyWidth
		}
		padL, padW, span = ipcLand(c.length(p), p.LeadLength, width, j)
	}
	pads := rowPads(slots(names, 0), padL, padW, span, span, "roundrect")
	return pads, centred(0, 0, c.length(p).Mid(), p.BodyWidth.Mid()), from
}

// Solids are the body between its two end caps.
func (c chip) Solids(p Package) []Solid {
	a := height(p)
	l, w, t := c.length(p).Mid(), p.BodyWidth.Mid(), p.LeadLength.Mid()
	return []Solid{
		box(PartBody, -l/2+t, -w/2, 0.01, l/2-t, w/2, a-0.01),
		box(PartLead, -l/2, -w/2, 0, -l/2+t, w/2, a),
		box(PartLead, l/2-t, -w/2, 0, l/2, w/2, a),
	}
}

// bga is a grid of balls.
type bga struct{}

// Name is the family's name.
func (bga) Name() string { return "bga" }

// rowLetters are a BGA's row names: A to Y without I, O, Q, S, X and Z,
// then AA, AB…
var rowLetters = []string{"A", "B", "C", "D", "E", "F", "G", "H", "J", "K", "L", "M", "N", "P", "R", "T", "U", "V", "W", "Y"}

// rowName is the name of the i'th row.
func rowName(i int) string {
	if i < len(rowLetters) {
		return rowLetters[i]
	}
	return rowLetters[i/len(rowLetters)-1] + rowLetters[i%len(rowLetters)]
}

// ball is a BGA ball and its place.
type ball struct {
	num  string
	x, y float64
}

// balls are the grid's balls with their places.
func (bga) balls(p Package) []ball {
	gone := map[string]bool{}
	for _, d := range p.Depopulated {
		gone[d] = true
	}
	var out []ball
	for r := 0; r < p.Rows; r++ {
		for c := 0; c < p.Columns; c++ {
			n := rowName(r) + strconv.Itoa(c+1)
			if gone[n] {
				continue
			}
			x := (float64(c) - float64(p.Columns-1)/2) * p.Pitch
			y := (float64(r) - float64(p.Rows-1)/2) * p.Pitch
			out = append(out, ball{n, x, y})
		}
	}
	return out
}

// Pads are round pads at the balls: the datasheet's size, else 80% of the
// ball (non-solder-mask-defined).
func (g bga) Pads(p Package, ps *problems) ([]Pad, Rect, string) {
	need(ps, "bga", map[string]bool{"pitch": p.Pitch > 0, "rows": p.Rows > 0, "columns": p.Columns > 0,
		"bodyLength": p.BodyLength.Set(), "bodyWidth": p.BodyWidth.Set(), "leadWidth": p.LeadWidth.Set() || p.Land != nil && p.Land.PadWidth > 0})
	if ps.failed() {
		return nil, Rect{}, ""
	}
	d, from := Number(ceilTo(0.8*p.LeadWidth.Mid(), 0.01)), FromCalculated
	if p.Land != nil && p.Land.PadWidth > 0 {
		d, from = p.Land.PadWidth, FromDatasheet
	}
	var pads []Pad
	for _, b := range g.balls(p) {
		pads = append(pads, Pad{Number: b.num, Kind: PadSMD, Shape: "circle", X: Number(b.x), Y: Number(b.y), W: d, H: d})
	}
	if n := p.Rows*p.Columns - len(p.Depopulated); p.Pins > 0 && n != p.Pins {
		ps.warnf("the grid has %d balls but the package %d pins: list the missing balls in depopulated", n, p.Pins)
	}
	return pads, centred(0, 0, p.BodyWidth.Mid(), p.BodyLength.Mid()), from
}

// Solids are the body on its balls.
func (g bga) Solids(p Package) []Solid {
	a, dia := height(p), p.LeadWidth.Mid()
	stand := 0.6 * dia
	bw, bl := p.BodyWidth.Mid(), p.BodyLength.Mid()
	out := []Solid{box(PartBody, -bw/2, -bl/2, stand, bw/2, bl/2, a), pin1Dimple(bw, bl, a)}
	for _, b := range g.balls(p) {
		out = append(out, prism(PartLead, b.x, b.y, dia/2, 0, stand+0.01, 8))
	}
	return out
}
