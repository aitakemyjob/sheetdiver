package kicad

import (
	"cmp"
	"math"
	"slices"
)

// Placement. Each part is drawn as a cluster with the passives that
// serve it wired to their pins, the way a designer draws one: a
// decoupling capacitor hanging from the supply pin's wire to ground, a
// pull-up from a signal's wire to its rail, an inductor across the two
// pins it bridges. Passives on the same pin hang side by side along its
// wire; pins lower on a side keep their passives nearer the part, so
// the passives of the pins above hang beyond them and nothing crosses.
// A net that leaves the cluster ends in a label, so the netlist is the
// connection table's however the clusters are arranged. The clusters go
// in their group's grid where the app drew them (column, then row), and
// passives that serve no one part go in rows under them, labelled.

// clusterGap is the space between clusters, and between a cluster and
// the passives under the grid.
const clusterGap = 7.62

// placer lays out a schematic's components.
type placer struct {
	// symbol builds a component's symbol, recording it for the library.
	symbol func(Component) LibSymbol
	// gnd draws the GND net with ground symbols.
	gnd bool
	// members is how many pins each net has in the whole design.
	members map[string]int
}

// pinEnd is what goes at the end of a pin that isn't wired to a passive:
// a ground symbol (on a short wire unless the pin points down), a stub
// with the net's label, a no-connect flag when the pin is unconnected
// and flag is set, or nothing.
func (pl *placer) pinEnd(x, y float64, out Side, net string, flag bool) []item {
	switch {
	case net == "":
		if flag {
			return []item{markItem{x: x, y: y}}
		}
		return nil
	case net == "GND" && pl.gnd:
		if out == Bottom {
			return []item{groundItem{x: x, y: y}}
		}
		qx, qy := step(x, y, out, grid)
		return []item{wireItem{x, y, qx, qy}, groundItem{x: qx, y: qy, up: out == Top}}
	}
	qx, qy := step(x, y, out, grid)
	return []item{wireItem{x, y, qx, qy}, labelItem{net: net, x: qx, y: qy, out: out}}
}

// free draws a component on its own, every pin ending in its net's
// label or ground.
func (pl *placer) free(c Component) []item {
	s := symbolItem{c: c, s: pl.symbol(c)}
	items := []item{s}
	_, two := twoTerminals[c.Kind]
	for _, number := range sortedPins(s.s) {
		x, y, out := s.pin(number)
		items = append(items, pl.pinEnd(x, y, out, pinOf(c, number).Net, !two)...)
	}
	return items
}

// hangAt draws a passive hanging from a point on a wire: turned so its
// terminal on net is at the top, there, and its other end below it.
func (pl *placer) hangAt(h Component, net string, x, y float64) []item {
	s := symbolItem{c: h, s: pl.symbol(h)}
	anchor := ""
	for _, p := range h.Pins {
		if p.Net == net {
			anchor = pinNumber(p)
			break
		}
	}
	place := s.s.Pins[anchor]
	for _, angle := range []float64{0, 90, 180, 270} {
		if rx, ry := rotate(place.X, place.Y, angle); math.Abs(rx) < 1e-9 && ry > 0 {
			s.angle = angle
			break
		}
	}
	rx, ry := rotate(place.X, place.Y, s.angle)
	s.x, s.y = x-rx, y+ry
	items := []item{s}
	for _, number := range sortedPins(s.s) {
		if number == anchor {
			continue
		}
		px, py, out := s.pin(number)
		items = append(items, pl.pinEnd(px, py, out, pinOf(h, number).Net, false)...)
	}
	return items
}

// textRoom is the width of a passive's reference and value, written to
// its right.
func textRoom(c Component) float64 {
	v := c.Value
	if v == "" {
		v = c.MPN
	}
	return 3.048 + max(textWidth(c.Ref), textWidth(v)) + 1.27
}

// sidePin is a pin on the left or right of a part, where it ends.
type sidePin struct {
	number string
	x, y   float64
	net    string
}

// cluster draws a part with the passives that hang from its pins. The
// passives it can't hang (from a pin on its top or bottom, or a pin it
// doesn't have) are returned to be drawn on their own.
func (pl *placer) cluster(main Component, hangs []Component) ([]item, []Component) {
	ms := symbolItem{c: main, s: pl.symbol(main)}
	items := []item{ms}
	var leftovers []Component
	byPin := map[string][]Component{}
	var bridges []Component
	for _, h := range hangs {
		switch {
		case len(h.Anchor.Pins) == 2:
			bridges = append(bridges, h)
		case len(h.Anchor.Pins) == 1 && hasPin(ms.s, h.Anchor.Pins[0]):
			byPin[h.Anchor.Pins[0]] = append(byPin[h.Anchor.Pins[0]], h)
		default:
			leftovers = append(leftovers, h)
		}
	}
	_, two := twoTerminals[main.Kind]
	sides := map[Side][]sidePin{}
	for _, number := range sortedPins(ms.s) {
		x, y, out := ms.pin(number)
		net := pinOf(main, number).Net
		if out == Left || out == Right {
			sides[out] = append(sides[out], sidePin{number, x, y, net})
			continue
		}
		items = append(items, pl.pinEnd(x, y, out, net, !two)...)
		leftovers = append(leftovers, byPin[number]...)
	}
	for _, side := range []Side{Left, Right} {
		pins := sides[side]
		// Bottom first: their passives stay nearest the part.
		slices.SortFunc(pins, func(a, b sidePin) int { return cmp.Compare(b.y, a.y) })
		dir := 1.0
		if side == Left {
			dir = -1
		}
		occupied := 0.0
		done := map[string]bool{}
		for _, b := range bridges {
			if bi, ok := pl.bridge(b, pins, dir, &occupied); ok {
				items = append(items, bi...)
				done[b.Anchor.Pins[0]], done[b.Anchor.Pins[1]] = true, true
			}
		}
		for _, p := range pins {
			if done[p.number] {
				continue
			}
			mine := byPin[p.number]
			if len(mine) == 0 || p.net == "" {
				end := pl.pinEnd(p.x, p.y, side, p.net, !two)
				items = append(items, end...)
				if len(end) > 0 {
					occupied = math.Max(occupied, (boundsReach(end, p.x, dir)))
				}
				continue
			}
			items = append(items, pl.tapped(p, mine, side, dir, &occupied)...)
		}
	}
	// Bridges that found no place hang from their first pin.
	for _, b := range bridges {
		if !slices.ContainsFunc(items, func(it item) bool { s, ok := it.(symbolItem); return ok && s.c.Ref == b.Ref }) {
			leftovers = append(leftovers, b)
		}
	}
	return items, leftovers
}

// boundsReach is how far from a pin's end, outward, a set of items
// reaches.
func boundsReach(items []item, x, dir float64) float64 {
	b := boundsOf(items)
	if dir < 0 {
		return x - b.X0
	}
	return b.X1 - x
}

// tapped draws a pin's wire with its passives hanging from it, beyond
// what the pins below took (occupied, outward from the pin's end), and
// the net's label at its end unless every pin on the net is on it.
func (pl *placer) tapped(p sidePin, hangs []Component, side Side, dir float64, occupied *float64) []item {
	var items []item
	px, prev := p.x, p.x
	u := 0.0
	for i, h := range hangs {
		room := textRoom(h)
		// The text is on the passive's right: toward the part on the left.
		want := *occupied + 2.54
		if dir < 0 {
			want = *occupied + room
		}
		u = snapUp(math.Max(want, 2*grid), grid)
		x := px + dir*u
		items = append(items, wireItem{prev, p.y, x, p.y})
		items = append(items, pl.hangAt(h, p.net, x, p.y)...)
		if i > 0 {
			items = append(items, markItem{x: prev, y: p.y, junction: true})
		}
		prev = x
		if dir < 0 {
			*occupied = u + 2.54
		} else {
			*occupied = u + room
		}
	}
	if pl.members[p.net] > 1+len(hangs) {
		qx := prev + dir*grid
		items = append(items, wireItem{prev, p.y, qx, p.y}, labelItem{net: p.net, x: qx, y: p.y, out: side})
		if len(hangs) > 0 {
			items = append(items, markItem{x: prev, y: p.y, junction: true})
		}
		*occupied = math.Max(*occupied, u+grid+textWidth(p.net)+1.27)
	}
	return items
}

// bridge draws a passive across two neighbouring pins at the bottom of
// a side (an inductor across LX1 and LX2): standing beside them, its top
// wired straight to the upper pin and its bottom round to the lower one.
// Both nets must be just the two pins and the passive's, as the
// passive's ends are drawn without labels.
func (pl *placer) bridge(b Component, pins []sidePin, dir float64, occupied *float64) ([]item, bool) {
	if len(pins) < 2 {
		return nil, false
	}
	// The two pins at the bottom of the side (pins are bottom first).
	lower, upper := pins[0], pins[1]
	want := map[string]bool{b.Anchor.Pins[0]: true, b.Anchor.Pins[1]: true}
	if !want[lower.number] || !want[upper.number] || math.Abs(lower.y-upper.y-grid) > 1e-6 ||
		pl.members[lower.net] != 2 || pl.members[upper.net] != 2 || lower.net == upper.net {
		return nil, false
	}
	room := textRoom(b)
	u := 2 * grid
	if dir < 0 {
		u = snapUp(grid+room, grid)
	}
	x := upper.x + dir*u
	items := pl.hangAt(b, upper.net, x, upper.y)
	// The other end, below.
	var oy float64
	hs := items[0].(symbolItem)
	for _, n := range sortedPins(hs.s) {
		if pinOf(b, n).Net == lower.net {
			_, oy, _ = hs.pin(n)
		}
	}
	items = items[:1] // its ends are wired here, not labelled
	corner := lower.x + dir*grid
	items = append(items, wireItem{upper.x, upper.y, x, upper.y})
	if math.Abs(oy-lower.y) < 1e-6 {
		items = append(items, wireItem{lower.x, lower.y, x, lower.y})
	} else {
		items = append(items, wireItem{lower.x, lower.y, corner, lower.y}, wireItem{corner, lower.y, corner, oy}, wireItem{corner, oy, x, oy})
	}
	if dir < 0 {
		*occupied = u + 2.54
	} else {
		*occupied = u + room
	}
	return items, true
}

// hasPin reports whether a symbol has a pin.
func hasPin(s LibSymbol, number string) bool {
	_, ok := s.Pins[number]
	return ok
}

// group lays out a group's components from its top-left corner: the
// parts' clusters on the grid by cell, then the passives that serve no
// one part in rows under them. It returns the items and their size.
func (pl *placer) group(comps []Component) ([]item, float64, float64) {
	refs := map[string]bool{}
	for _, c := range comps {
		if c.Anchor == nil {
			if _, two := twoTerminals[c.Kind]; !two {
				refs[c.Ref] = true
			}
		}
	}
	hangs := map[string][]Component{}
	var mains, free []Component
	for _, c := range comps {
		switch {
		case refs[c.Ref]:
			mains = append(mains, c)
		case c.Anchor != nil && refs[c.Anchor.Ref]:
			hangs[c.Anchor.Ref] = append(hangs[c.Anchor.Ref], c)
		default:
			free = append(free, c)
		}
	}
	type placedCluster struct {
		cell  Cell
		items []item
		box   Rect
	}
	var clusters []placedCluster
	for i, m := range mains {
		items, rest := pl.cluster(m, hangs[m.Ref])
		free = append(free, rest...)
		cell := Cell{Column: i}
		if m.Cell != nil {
			cell = *m.Cell
		}
		clusters = append(clusters, placedCluster{cell, items, boundsOf(items)})
	}
	slices.SortStableFunc(clusters, func(a, b placedCluster) int {
		return cmp.Or(cmp.Compare(a.cell.Column, b.cell.Column), cmp.Compare(a.cell.Row, b.cell.Row))
	})
	var out []item
	x, width, height := 0.0, 0.0, 0.0
	for i := 0; i < len(clusters); {
		col := clusters[i].cell.Column
		y, colW := 0.0, 0.0
		for ; i < len(clusters) && clusters[i].cell.Column == col; i++ {
			c := clusters[i]
			out = append(out, moveAll(c.items, snap(x-c.box.X0), snap(y-c.box.Y0))...)
			y += c.box.Y1 - c.box.Y0 + clusterGap
			colW = math.Max(colW, c.box.X1-c.box.X0)
		}
		x += colW + clusterGap
		width, height = math.Max(width, x-clusterGap), math.Max(height, y-clusterGap)
	}
	// The rest in rows under the grid, as wide as it or six of them.
	y := height
	if len(clusters) > 0 {
		y += clusterGap
	}
	limit := width
	var placedFree [][]item
	six := 0.0
	for i, c := range free {
		items := pl.free(c)
		placedFree = append(placedFree, items)
		if i < 6 {
			b := boundsOf(items)
			six += b.X1 - b.X0 + clusterGap
		}
	}
	limit = math.Max(limit, six)
	x, rowH := 0.0, 0.0
	for _, items := range placedFree {
		b := boundsOf(items)
		w := b.X1 - b.X0
		if x > 0 && x+w > limit {
			x, y, rowH = 0, y+rowH+clusterGap, 0
		}
		out = append(out, moveAll(items, snap(x-b.X0), snap(y-b.Y0))...)
		x += w + clusterGap
		width = math.Max(width, x-clusterGap)
		rowH = math.Max(rowH, b.Y1-b.Y0)
		height = math.Max(height, y+rowH)
	}
	return out, width, height
}
