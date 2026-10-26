package kicad

import (
	"fmt"
	"math"
)

// What's drawn on the sheet: symbols, wires, net labels, ground symbols,
// no-connect flags and junction dots. Placement builds them around a
// part in its own coordinates, moves them into place, then writes them.

// item is something drawn on the sheet, in sheet coordinates (millimetres,
// Y down).
type item interface {
	// bounds is the area it covers, with its texts.
	bounds() Rect
	// moved is the item moved by dx, dy.
	moved(dx, dy float64) item
	// write is the item as KiCad writes it.
	write(e *emitter) *node
}

// emitter is what writing items needs: the UUIDs, the sheet's path and
// project, and whether a ground symbol was used.
type emitter struct {
	ids        *idGen
	root       string
	project    string
	usedGround bool
}

// rotate turns a library point by a symbol's angle (counter-clockwise,
// library coordinates, Y up).
func rotate(x, y, angle float64) (float64, float64) {
	switch int(angle) {
	case 90:
		return -y, x
	case 180:
		return -x, -y
	case 270:
		return y, -x
	}
	return x, y
}

// rotateSide is the side a pin leaves from once its symbol is turned.
func rotateSide(s Side, angle float64) Side {
	order := []Side{Right, Top, Left, Bottom} // counter-clockwise
	for i, o := range order {
		if o == s {
			return order[(i+int(angle)/90)%4]
		}
	}
	return s
}

// symbolItem is a component's symbol placed at x, y and turned by angle.
type symbolItem struct {
	c     Component
	s     LibSymbol
	x, y  float64
	angle float64
}

// pin is where a pin of the symbol ends on the sheet and which way it
// leaves.
func (p symbolItem) pin(number string) (float64, float64, Side) {
	place := p.s.Pins[number]
	x, y := rotate(place.X, place.Y, p.angle)
	return p.x + x, p.y - y, rotateSide(place.Out, p.angle)
}

// value is the text the symbol's value field shows.
func (p symbolItem) value() string {
	if p.c.Value != "" {
		return p.c.Value
	}
	return p.c.MPN
}

// fields are where the reference and value go on the sheet: as the
// symbol places them, or to the right of a turned two-terminal.
func (p symbolItem) fields() (ref, val textPlace) {
	if p.angle == 0 {
		return textPlace{p.x + p.s.Ref.x, p.y - p.s.Ref.y, p.s.Ref.justify}, textPlace{p.x + p.s.Val.x, p.y - p.s.Val.y, p.s.Val.justify}
	}
	return textPlace{p.x + 3.048, p.y - 1.27, "left"}, textPlace{p.x + 3.048, p.y + 1.27, "left"}
}

// bounds is the area the symbol covers.
func (p symbolItem) bounds() Rect {
	b := p.s.Bounds
	r := Rect{math.Inf(1), math.Inf(1), math.Inf(-1), math.Inf(-1)}
	for _, c := range [][2]float64{{b.X0, b.Y0}, {b.X0, b.Y1}, {b.X1, b.Y0}, {b.X1, b.Y1}} {
		x, y := rotate(c[0], c[1], p.angle)
		r = grow(r, p.x+x, p.y-y)
	}
	ref, val := p.fields()
	for _, f := range []struct {
		at   textPlace
		text string
	}{{ref, p.c.Ref}, {val, p.value()}} {
		w := textWidth(f.text)
		x0, x1 := f.at.x-w/2, f.at.x+w/2
		if f.at.justify == "left" {
			x0, x1 = f.at.x, f.at.x+w
		}
		r = grow(grow(r, x0, f.at.y-1.27), x1, f.at.y+1.27)
	}
	return r
}

// moved is the symbol moved.
func (p symbolItem) moved(dx, dy float64) item {
	p.x, p.y = p.x+dx, p.y+dy
	return p
}

// write is the symbol as KiCad writes it.
func (p symbolItem) write(e *emitter) *node {
	ref := p.c.Ref
	n := list("symbol", list("lib_id", p.s.ID), at(p.x, p.y, p.angle), list("unit", 1),
		list("in_bom", sym("yes")), list("on_board", sym("yes")), list("dnp", sym("no")), list("uuid", e.ids.next("sym:"+ref)))
	refAt, valAt := p.fields()
	// A field's angle and justification are KiCad's after the symbol's
	// turn: the turn is given back so the text reads level, and on a
	// quarter turn KiCad mirrors the justification, so it's mirrored here.
	field := func(name, value string, t textPlace, dy float64, hidden bool) *node {
		if p.angle == 90 || p.angle == 270 {
			t.justify = map[string]string{"left": "right", "right": "left"}[t.justify]
		}
		return list("property", name, value, at(t.x, t.y+dy, p.angle), t.effects(hidden))
	}
	n.add(field("Reference", ref, refAt, 0, false), field("Value", p.value(), valAt, 0, false))
	n.add(field("Footprint", p.c.Footprint.LibID, valAt, 2.54, true), field("Datasheet", p.c.Datasheet, valAt, 2.54, true))
	extra := []struct{ name, value string }{{"MPN", p.c.MPN}, {"Manufacturer", p.c.Manufacturer}, {"Description", p.c.Description}}
	if p.c.Footprint.LibID == "" && p.c.Footprint.Hint != "" {
		extra = append(extra, struct{ name, value string }{"Package", p.c.Footprint.Hint})
	}
	for _, x := range extra {
		if x.value != "" {
			n.add(field(x.name, x.value, valAt, 2.54, true))
		}
	}
	for _, number := range sortedPins(p.s) {
		n.add(list("pin", number, list("uuid", e.ids.next("pin:"+ref+":"+number))))
	}
	return n.add(list("instances", list("project", e.project, list("path", "/"+e.root, list("reference", ref), list("unit", 1)))))
}

// wireItem is a wire between two points.
type wireItem struct {
	x0, y0, x1, y1 float64
}

// bounds is the area the wire covers.
func (w wireItem) bounds() Rect {
	return Rect{math.Min(w.x0, w.x1), math.Min(w.y0, w.y1), math.Max(w.x0, w.x1), math.Max(w.y0, w.y1)}
}

// moved is the wire moved.
func (w wireItem) moved(dx, dy float64) item {
	return wireItem{w.x0 + dx, w.y0 + dy, w.x1 + dx, w.y1 + dy}
}

// write is the wire as KiCad writes it.
func (w wireItem) write(e *emitter) *node {
	return list("wire", list("pts", xy(w.x0, w.y0), xy(w.x1, w.y1)), stroke(0), list("uuid", e.ids.next("wire")))
}

// labelItem is a net label at the end of a wire, reading away from it.
type labelItem struct {
	net  string
	x, y float64
	out  Side
}

// textWidth is about how wide a text is drawn at the default size.
func textWidth(s string) float64 {
	return float64(len([]rune(s))) * charWidth
}

// bounds is the area the label covers.
func (l labelItem) bounds() Rect {
	w := textWidth(l.net) + 1.27
	switch l.out {
	case Left:
		return Rect{l.x - w, l.y - 1.27, l.x, l.y + 0.5}
	case Top:
		return Rect{l.x - 1.27, l.y - w, l.x + 0.5, l.y}
	case Bottom:
		return Rect{l.x - 1.27, l.y, l.x + 0.5, l.y + w}
	}
	return Rect{l.x, l.y - 1.27, l.x + w, l.y + 0.5}
}

// moved is the label moved.
func (l labelItem) moved(dx, dy float64) item {
	l.x, l.y = l.x+dx, l.y+dy
	return l
}

// write is the label as KiCad writes it.
func (l labelItem) write(e *emitter) *node {
	angle, justify := 0.0, "left"
	switch l.out {
	case Left:
		angle, justify = 180, "right"
	case Top:
		angle = 90
	case Bottom:
		angle, justify = 270, "right"
	}
	return list("label", l.net, at(l.x, l.y, angle), list("fields_autoplaced"), effects(false, justify, "bottom"), list("uuid", e.ids.next("label")))
}

// groundItem is a ground symbol whose pin is at x, y: hanging below it,
// or upside down above it.
type groundItem struct {
	x, y float64
	up   bool
}

// bounds is the area the ground symbol covers.
func (g groundItem) bounds() Rect {
	if g.up {
		return Rect{g.x - 1.27, g.y - 2.54, g.x + 1.27, g.y}
	}
	return Rect{g.x - 1.27, g.y, g.x + 1.27, g.y + 2.54}
}

// moved is the ground symbol moved.
func (g groundItem) moved(dx, dy float64) item {
	g.x, g.y = g.x+dx, g.y+dy
	return g
}

// write is the ground symbol as KiCad writes it.
func (g groundItem) write(e *emitter) *node {
	e.usedGround = true
	ref := fmt.Sprintf("#PWR%03d", e.ids.count("#PWR"))
	angle, dy := 0.0, 1.0
	if g.up {
		angle, dy = 180, -1
	}
	return list("symbol", list("lib_id", "power:GND"), at(g.x, g.y, angle), list("unit", 1),
		list("in_bom", sym("yes")), list("on_board", sym("yes")), list("dnp", sym("no")), list("uuid", e.ids.next("gnd:"+ref)),
		list("property", "Reference", ref, at(g.x, g.y+6.35*dy, 0), effects(true)),
		// Its value is always GND: hidden, so the symbols on pins a step
		// apart don't print over each other.
		list("property", "Value", "GND", at(g.x, g.y+3.81*dy, 0), effects(true)),
		list("property", "Footprint", "", at(g.x, g.y, 0), effects(true)),
		list("property", "Datasheet", "", at(g.x, g.y, 0), effects(true)),
		list("pin", "1", list("uuid", e.ids.next("gndpin:"+ref))),
		list("instances", list("project", e.project, list("path", "/"+e.root, list("reference", ref), list("unit", 1)))))
}

// markItem is a no-connect flag or a junction dot at a point.
type markItem struct {
	x, y     float64
	junction bool
}

// bounds is the area the mark covers.
func (m markItem) bounds() Rect { return Rect{m.x - 0.8, m.y - 0.8, m.x + 0.8, m.y + 0.8} }

// moved is the mark moved.
func (m markItem) moved(dx, dy float64) item {
	m.x, m.y = m.x+dx, m.y+dy
	return m
}

// write is the mark as KiCad writes it.
func (m markItem) write(e *emitter) *node {
	if m.junction {
		return list("junction", list("at", m.x, m.y), list("diameter", 0), list("color", 0, 0, 0, 0), list("uuid", e.ids.next("junction")))
	}
	return list("no_connect", list("at", m.x, m.y), list("uuid", e.ids.next("nc")))
}

// boundsOf is the area a set of items covers.
func boundsOf(items []item) Rect {
	r := Rect{math.Inf(1), math.Inf(1), math.Inf(-1), math.Inf(-1)}
	for _, it := range items {
		b := it.bounds()
		r = grow(grow(r, b.X0, b.Y0), b.X1, b.Y1)
	}
	return r
}

// moveAll moves a set of items.
func moveAll(items []item, dx, dy float64) []item {
	out := make([]item, len(items))
	for i, it := range items {
		out[i] = it.moved(dx, dy)
	}
	return out
}
