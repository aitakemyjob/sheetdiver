package kicad

import (
	"cmp"
	"fmt"
	"math"
	"regexp"
	"slices"
)

// The board. Each part goes on a four-layer board with its footprint
// from KiCad's libraries and its pads on their nets, and the passives
// that serve it right at the pins they serve: the pad on the shared net
// facing the part's pad, as close as the courtyards allow, the passive
// pointing away from the part (a decoupling capacitor's ground pad
// outward, to its via), side by side along the edge when a pin has
// several; a passive across two pins (an inductor across LX1 and LX2)
// lies along the edge beside both. Parts are packed by group, in the
// order the schematic has them, connectors first; passives that serve
// no one part go by a part they share a net with. Nothing is routed: the
// airwires show what's left, over a ground plane on the first inner
// layer. Each decision is noted, for the layout guide.

// boardClearance is the gap kept between courtyards, and around a part
// for its passives.
const boardClearance = 0.25

// boardPart is a footprint placed on the board: X, Y its origin and
// Angle its turn (degrees, counter-clockwise as seen from the top).
type boardPart struct {
	c     Component
	fp    *libFootprint
	x, y  float64
	angle float64
}

// toBoard turns a point on the footprint into board coordinates.
func (b boardPart) toBoard(px, py float64) (float64, float64) {
	s, c := math.Sincos(b.angle * math.Pi / 180)
	return b.x + px*c + py*s, b.y - px*s + py*c
}

// padAt is where a pad of the part is on the board.
func (b boardPart) padAt(number string) (float64, float64, bool) {
	p, ok := b.fp.pad(number)
	if !ok {
		return 0, 0, false
	}
	x, y := b.toBoard(p.x, p.y)
	return x, y, true
}

// courtyard is the area the part keeps clear on the board.
func (b boardPart) courtyard() Rect {
	r := Rect{math.Inf(1), math.Inf(1), math.Inf(-1), math.Inf(-1)}
	c := b.fp.court
	for _, pt := range [][2]float64{{c.X0, c.Y0}, {c.X0, c.Y1}, {c.X1, c.Y0}, {c.X1, c.Y1}} {
		x, y := b.toBoard(pt[0], pt[1])
		r = grow(r, x, y)
	}
	return r
}

// overlaps reports whether two areas overlap, with a gap between them.
func overlaps(a, b Rect, gap float64) bool {
	return a.X0 < b.X1+gap && b.X0 < a.X1+gap && a.Y0 < b.Y1+gap && b.Y0 < a.Y1+gap
}

// PlacementNote says where a part went and why.
type PlacementNote struct {
	Ref  string
	Text string
}

// BoardResult is a board written: its file, what was placed and why,
// and what couldn't be.
type BoardResult struct {
	Board string
	Notes []PlacementNote
	// Missing are the parts left off the board: no footprint.
	Missing []string
	// Unmatched are pins with no pad of the same number on their
	// footprint (a connector's pins by name): their nets aren't on it.
	Unmatched []string
}

// BoardWriter writes boards with footprints from its source.
type BoardWriter struct {
	Footprints FootprintSource
}

// boardPlacer holds a board being placed.
type boardPlacer struct {
	parts []*boardPart
	byRef map[string]*boardPart
	notes []PlacementNote
}

// free reports whether a part's courtyard at a place clears every part
// placed so far.
func (bp *boardPlacer) free(b boardPart) bool {
	r := b.courtyard()
	for _, p := range bp.parts {
		if overlaps(r, p.courtyard(), boardClearance) {
			return false
		}
	}
	return true
}

// add puts a part on the board.
func (bp *boardPlacer) add(b boardPart) *boardPart {
	p := &b
	bp.parts = append(bp.parts, p)
	bp.byRef[b.c.Ref] = p
	return p
}

// faceOut is the way a pad faces out of its part: along the axis on
// which it's furthest from the part's centre.
func faceOut(part *boardPart, x, y float64) (float64, float64) {
	r := part.courtyard()
	cx, cy := (r.X0+r.X1)/2, (r.Y0+r.Y1)/2
	dx, dy := (x-cx)/math.Max(r.X1-r.X0, 0.1), (y-cy)/math.Max(r.Y1-r.Y0, 0.1)
	if math.Abs(dx) >= math.Abs(dy) {
		return math.Copysign(1, dx), 0
	}
	return 0, math.Copysign(1, dy)
}

// facing is the turn that points a two-pad footprint's pad `toward`
// (from its other pad) in direction dx, dy.
func facing(fp *libFootprint, toward, other string, dx, dy float64) float64 {
	a, okA := fp.pad(toward)
	o, okO := fp.pad(other)
	if !okA || !okO {
		return 0
	}
	vx, vy := a.x-o.x, a.y-o.y
	best, bestDot := 0.0, math.Inf(-1)
	for _, angle := range []float64{0, 90, 180, 270} {
		s, c := math.Sincos(angle * math.Pi / 180)
		rx, ry := vx*c+vy*s, -vx*s+vy*c
		if dot := rx*dx + ry*dy; dot > bestDot {
			best, bestDot = angle, dot
		}
	}
	return best
}

// nearest places a part as near a point as it fits: its pad `pad` on
// the point, moved out along dx, dy until its courtyard clears the part
// it serves, then along the edge and further out until it clears the
// rest.
func (bp *boardPlacer) nearest(b boardPart, pad string, x, y, dx, dy float64) (boardPart, bool) {
	p, ok := b.fp.pad(pad)
	if !ok {
		p = libPad{}
	}
	place := func(out, along float64) boardPart {
		px, py := x+dx*out-dy*along, y+dy*out+dx*along
		c := b
		c.x, c.y = 0, 0
		ox, oy := c.toBoard(p.x, p.y)
		c.x, c.y = px-ox, py-oy
		return c
	}
	for _, c := range searchOrder() {
		if placed := place(c[0], c[1]); bp.free(placed) {
			return placed, true
		}
	}
	return b, false
}

// exposedPadRe is a pin that is a part's exposed pad.
var exposedPadRe = regexp.MustCompile(`(?i)^(EP|PAD|E?PAD|THERMAL[ _]?PAD|EXPOSED[ _]?PAD|PGND[ _]?PAD)$`)

// searchSteps are the places nearest tries, as distances out from the
// pin and along the edge, nearest first: a step along the edge costs
// twice a step out, so passives line up beside each other before
// they're pushed away from the part.
var searchSteps [][2]float64

// searchOrder is searchSteps, worked out once.
func searchOrder() [][2]float64 {
	if searchSteps == nil {
		for out := 0.0; out <= 20; out += 0.25 {
			for along := -10.0; along <= 10; along += 0.25 {
				searchSteps = append(searchSteps, [2]float64{out, along})
			}
		}
		slices.SortStableFunc(searchSteps, func(a, b [2]float64) int {
			return cmp.Compare(a[0]+2*math.Abs(a[1]), b[0]+2*math.Abs(b[1]))
		})
	}
	return searchSteps
}

// pinPad is the pad a component's pin is on: the pad of the same
// number, else an exposed pad for a pin named as one ("EP", "PAD").
func pinPad(fp *libFootprint, pin Pin) (string, bool) {
	number := pinNumber(pin)
	if _, ok := fp.pad(number); ok {
		return number, true
	}
	if exposedPadRe.MatchString(pin.Name) || exposedPadRe.MatchString(number) {
		// The exposed pad is the footprint's last pad number.
		last := ""
		for _, p := range fp.pads {
			if p.number != "" {
				last = p.number
			}
		}
		if last != "" {
			return last, true
		}
	}
	return "", false
}

// anchorPad is the pad of a passive on a net.
func anchorPad(c Component, fp *libFootprint, net string) (string, string) {
	var on, other string
	for _, p := range c.Pins {
		pad, ok := pinPad(fp, p)
		if !ok {
			continue
		}
		if p.Net == net && on == "" {
			on = pad
		} else {
			other = pad
		}
	}
	return on, other
}

// Write places the schematic's parts on a board and writes it.
func (w *BoardWriter) Write(s Schematic) BoardResult {
	var res BoardResult
	ids := newIDs(s.Project + "/board")
	fps := map[string]*libFootprint{}
	for _, c := range s.Components {
		if c.Footprint.LibID == "" {
			res.Missing = append(res.Missing, c.Ref+" (no footprint)")
			continue
		}
		fp, err := w.Footprints.Load(c.Footprint.LibID)
		if err != nil {
			res.Missing = append(res.Missing, c.Ref+" ("+err.Error()+")")
			continue
		}
		fps[c.Ref] = fp
	}
	bp := &boardPlacer{byRef: map[string]*boardPart{}}

	// The parts that passives hang from, connectors first, then by group
	// and cell as the schematic has them.
	groupOrder := map[string]int{}
	for i, g := range s.Groups {
		groupOrder[g.Name] = i
	}
	var mains, rest []Component
	for _, c := range s.Components {
		if fps[c.Ref] == nil {
			continue
		}
		if _, two := twoTerminals[c.Kind]; !two && c.Anchor == nil {
			mains = append(mains, c)
		} else {
			rest = append(rest, c)
		}
	}
	cellOf := func(c Component) Cell {
		if c.Cell != nil {
			return *c.Cell
		}
		return Cell{}
	}
	slices.SortStableFunc(mains, func(a, b Component) int {
		return cmp.Or(
			cmp.Compare(boolInt(a.Kind != KindConnector), boolInt(b.Kind != KindConnector)),
			cmp.Compare(groupOrder[a.Group], groupOrder[b.Group]),
			cmp.Compare(cellOf(a).Column, cellOf(b).Column), cmp.Compare(cellOf(a).Row, cellOf(b).Row))
	})

	// Each part with its passives, built around it, then packed.
	hangs := map[string][]Component{}
	var loose []Component
	for _, c := range rest {
		if c.Anchor != nil && slices.ContainsFunc(mains, func(m Component) bool { return m.Ref == c.Anchor.Ref }) {
			hangs[c.Anchor.Ref] = append(hangs[c.Anchor.Ref], c)
		} else {
			loose = append(loose, c)
		}
	}
	const origin, gap = 30.0, 4.0
	x, y, rowH := origin, origin, 0.0
	for _, m := range mains {
		cluster := &boardPlacer{byRef: map[string]*boardPart{}}
		main := cluster.add(boardPart{c: m, fp: fps[m.Ref]})
		for _, h := range hangs[m.Ref] {
			if !cluster.hang(main, h, fps[h.Ref]) {
				loose = append(loose, h)
			}
		}
		box := Rect{math.Inf(1), math.Inf(1), math.Inf(-1), math.Inf(-1)}
		for _, p := range cluster.parts {
			r := p.courtyard()
			box = grow(grow(box, r.X0, r.Y0), r.X1, r.Y1)
		}
		if x > origin && x+box.X1-box.X0 > origin+120 {
			x, y, rowH = origin, y+rowH+gap, 0
		}
		dx, dy := x-box.X0, y-box.Y0
		for _, p := range cluster.parts {
			p.x, p.y = p.x+dx, p.y+dy
			bp.parts = append(bp.parts, p)
			bp.byRef[p.c.Ref] = p
		}
		bp.notes = append(bp.notes, cluster.notes...)
		x += box.X1 - box.X0 + gap
		rowH = math.Max(rowH, box.Y1-box.Y0)
	}
	// The rest by a part they share a net with, or in a row under all.
	for _, c := range loose {
		if fps[c.Ref] == nil {
			continue
		}
		bp.loose(c, fps[c.Ref], s.Components)
	}

	// Nets, numbered as KiCad does: 0 is none.
	nets := map[string]int{}
	var names []string
	for _, c := range s.Components {
		for _, p := range c.Pins {
			if p.Net != "" && nets[p.Net] == 0 {
				names = append(names, p.Net)
				nets[p.Net] = len(names)
			}
		}
	}
	board := boardHeader()
	board.add(list("net", 0, ""))
	for i, n := range names {
		board.add(list("net", i+1, n))
	}
	outline := Rect{math.Inf(1), math.Inf(1), math.Inf(-1), math.Inf(-1)}
	for _, p := range bp.parts {
		node, unmatched := p.write(nets, ids)
		board.add(node)
		res.Unmatched = append(res.Unmatched, unmatched...)
		r := p.courtyard()
		outline = grow(grow(outline, r.X0, r.Y0), r.X1, r.Y1)
	}
	if len(bp.parts) > 0 {
		const margin = 2.0
		outline = Rect{math.Floor(outline.X0 - margin), math.Floor(outline.Y0 - margin), math.Ceil(outline.X1 + margin), math.Ceil(outline.Y1 + margin)}
		board.add(list("gr_rect", list("start", outline.X0, outline.Y0), list("end", outline.X1, outline.Y1), stroke(0.1), list("fill", sym("none")), list("layer", "Edge.Cuts"), list("tstamp", ids.next("edge"))))
		if n, ok := nets["GND"]; ok {
			board.add(groundZone(n, outline, ids))
			bp.notes = append(bp.notes, PlacementNote{Text: fmt.Sprintf("A ground plane fills the first inner layer (In1.Cu) under the whole board, %.0f × %.0f mm: press B in KiCad to fill it.", outline.X1-outline.X0, outline.Y1-outline.Y0)})
		}
	}
	res.Board = board.String()
	res.Notes = bp.notes
	return res
}

// boolInt is 1 for true.
func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

// hang places a passive at the part it serves; false when it found no
// place.
func (bp *boardPlacer) hang(main *boardPart, h Component, fp *libFootprint) bool {
	if fp == nil {
		return false
	}
	pinNet := func(number string) (string, string) {
		for _, p := range main.c.Pins {
			if pinNumber(p) == number {
				pad, _ := pinPad(main.fp, p)
				return p.Net, pad
			}
		}
		return "", ""
	}
	switch len(h.Anchor.Pins) {
	case 1:
		net, pad := pinNet(h.Anchor.Pins[0])
		px, py, ok := main.padAt(pad)
		if !ok || net == "" {
			return false
		}
		on, other := anchorPad(h, fp, net)
		dx, dy := faceOut(main, px, py)
		b := boardPart{c: h, fp: fp, angle: facing(fp, on, other, -dx, -dy)}
		placed, ok := bp.nearest(b, on, px, py, dx, dy)
		if !ok {
			return false
		}
		bp.add(placed)
		qx, qy, _ := placed.padAt(on)
		bp.notes = append(bp.notes, PlacementNote{Ref: h.Ref, Text: fmt.Sprintf("%s at %s pin %s (%s): its %s pad %.1f mm from the pin, pointing away from %s.", h.Ref, main.c.Ref, h.Anchor.Pins[0], net, net, math.Hypot(qx-px, qy-py), main.c.Ref)})
		return true
	case 2:
		net1, pad1 := pinNet(h.Anchor.Pins[0])
		net2, pad2 := pinNet(h.Anchor.Pins[1])
		x1, y1, ok1 := main.padAt(pad1)
		x2, y2, ok2 := main.padAt(pad2)
		if !ok1 || !ok2 {
			return false
		}
		on1, _ := anchorPad(h, fp, net1)
		on2, _ := anchorPad(h, fp, net2)
		mx, my := (x1+x2)/2, (y1+y2)/2
		dx, dy := faceOut(main, mx, my)
		// Along the edge: its pad on the first pin's net toward that pin.
		ex, ey := x1-x2, y1-y2
		if dx != 0 {
			ex, ey = 0, math.Copysign(1, ey)
		} else {
			ex, ey = math.Copysign(1, ex), 0
		}
		b := boardPart{c: h, fp: fp, angle: facing(fp, on1, on2, ex, ey)}
		// Centred on the two pins: its middle where they are.
		p1, _ := fp.pad(on1)
		p2, _ := fp.pad(on2)
		b.fp = fp
		mid := libPad{number: "", x: (p1.x + p2.x) / 2, y: (p1.y + p2.y) / 2}
		withMid := *fp
		withMid.pads = append(slices.Clone(fp.pads), libPad{number: "\x00mid", x: mid.x, y: mid.y})
		b.fp = &withMid
		placed, ok := bp.nearest(b, "\x00mid", mx, my, dx, dy)
		if !ok {
			return false
		}
		placed.fp = fp
		bp.add(placed)
		bp.notes = append(bp.notes, PlacementNote{Ref: h.Ref, Text: fmt.Sprintf("%s beside %s pins %s and %s (%s, %s), lying along the edge so both connections are short.", h.Ref, main.c.Ref, h.Anchor.Pins[0], h.Anchor.Pins[1], net1, net2)})
		return true
	}
	return false
}

// loose places a passive that serves no one part next to a placed part
// it shares a net with, else under everything.
func (bp *boardPlacer) loose(c Component, fp *libFootprint, all []Component) {
	for _, p := range c.Pins {
		if p.Net == "" || p.Net == "GND" {
			continue
		}
		for _, other := range bp.parts {
			for _, op := range other.c.Pins {
				if op.Net != p.Net {
					continue
				}
				pad, ok := pinPad(other.fp, op)
				if !ok {
					continue
				}
				x, y, _ := other.padAt(pad)
				on, oth := anchorPad(c, fp, p.Net)
				dx, dy := faceOut(other, x, y)
				b := boardPart{c: c, fp: fp, angle: facing(fp, on, oth, -dx, -dy)}
				if placed, ok := bp.nearest(b, on, x, y, dx, dy); ok {
					bp.add(placed)
					bp.notes = append(bp.notes, PlacementNote{Ref: c.Ref, Text: fmt.Sprintf("%s by %s pin %s, on %s with it.", c.Ref, other.c.Ref, pinNumber(op), p.Net)})
					return
				}
			}
		}
	}
	// Nothing shares a net: under everything.
	box := Rect{0, 30, 30, 30}
	for _, p := range bp.parts {
		r := p.courtyard()
		box = grow(grow(box, r.X0, r.Y0), r.X1, r.Y1)
	}
	b := boardPart{c: c, fp: fp}
	if placed, ok := bp.nearest(b, "", box.X0+2, box.Y1+3, 0, 1); ok {
		bp.add(placed)
	}
}

// write is the part as the board file has it: the library footprint
// with its reference, value, place and the nets of its pads; and the
// pins that found no pad.
func (b boardPart) write(nets map[string]int, ids *idGen) (*node, []string) {
	padNet := map[string]string{}
	var unmatched []string
	for _, p := range b.c.Pins {
		if pad, ok := pinPad(b.fp, p); ok {
			if p.Net != "" {
				padNet[pad] = p.Net
			}
		} else if p.Net != "" {
			unmatched = append(unmatched, fmt.Sprintf("%s pin %s (%s): no such pad on %s", b.c.Ref, pinNumber(p), p.Net, b.fp.libID))
		}
	}
	value := b.c.Value
	if value == "" {
		value = b.c.MPN
	}
	out := list("footprint", b.fp.libID)
	for _, it := range b.fp.tree.items[2:] {
		c, ok := it.(*node)
		if !ok {
			out.add(it)
			continue
		}
		switch c.head() {
		case "version", "generator", "tedit", "tstamp", "uuid":
			continue
		case "layer":
			out.add(c.clone(), list("tstamp", ids.next("fp:"+b.c.Ref)), at(b.x, b.y, b.angle))
			continue
		case "fp_text", "property":
			t := c.clone()
			switch t.text(1) {
			case "reference", "Reference":
				t.items[2] = b.c.Ref
			case "value", "Value":
				t.items[2] = value
			}
			turn(t, b.angle)
			out.add(t)
			continue
		case "pad":
			p := c.clone()
			turn(p, b.angle)
			if net, ok := padNet[p.text(1)]; ok {
				p.add(list("net", nets[net], net))
			}
			out.add(p)
			continue
		}
		out.add(c.clone())
	}
	return out, unmatched
}

// clone copies a list and the lists in it.
func (n *node) clone() *node {
	out := &node{items: make([]any, len(n.items))}
	for i, it := range n.items {
		if c, ok := it.(*node); ok {
			out.items[i] = c.clone()
		} else {
			out.items[i] = it
		}
	}
	return out
}

// turn adds a footprint's turn to a pad's or text's angle, which the
// board file gives as on the board.
func turn(n *node, angle float64) {
	if angle == 0 {
		return
	}
	a := n.child("at")
	if a == nil {
		return
	}
	base := 0.0
	if len(a.items) > 3 {
		base = a.float(3)
		a.items = a.items[:3]
	}
	a.items = append(a.items, math.Mod(base+angle, 360))
}

// groundZone is a ground plane on the first inner layer over the board.
func groundZone(net int, r Rect, ids *idGen) *node {
	return list("zone", list("net", net), list("net_name", "GND"), list("layer", "In1.Cu"), list("tstamp", ids.next("zone")),
		list("hatch", sym("edge"), 0.5), list("connect_pads", list("clearance", 0.3)), list("min_thickness", 0.25),
		list("filled_areas_thickness", sym("no")), list("fill", list("thermal_gap", 0.5), list("thermal_bridge_width", 0.5)),
		list("polygon", list("pts", xy(r.X0, r.Y0), xy(r.X1, r.Y0), xy(r.X1, r.Y1), xy(r.X0, r.Y1))))
}

// boardLayers are a four-layer board's layers, as KiCad 7 writes them.
var boardLayers = [][]any{
	{0, "F.Cu", sym("signal")}, {1, "In1.Cu", sym("signal")}, {2, "In2.Cu", sym("signal")}, {31, "B.Cu", sym("signal")},
	{32, "B.Adhes", sym("user"), "B.Adhesive"}, {33, "F.Adhes", sym("user"), "F.Adhesive"},
	{34, "B.Paste", sym("user")}, {35, "F.Paste", sym("user")},
	{36, "B.SilkS", sym("user"), "B.Silkscreen"}, {37, "F.SilkS", sym("user"), "F.Silkscreen"},
	{38, "B.Mask", sym("user")}, {39, "F.Mask", sym("user")},
	{40, "Dwgs.User", sym("user"), "User.Drawings"}, {41, "Cmts.User", sym("user"), "User.Comments"},
	{42, "Eco1.User", sym("user"), "User.Eco1"}, {43, "Eco2.User", sym("user"), "User.Eco2"},
	{44, "Edge.Cuts", sym("user")}, {45, "Margin", sym("user")},
	{46, "B.CrtYd", sym("user"), "B.Courtyard"}, {47, "F.CrtYd", sym("user"), "F.Courtyard"},
	{48, "B.Fab", sym("user")}, {49, "F.Fab", sym("user")},
}

// boardHeader starts a board file: its version, thickness, page, layers
// and setup.
func boardHeader() *node {
	layers := list("layers")
	for _, l := range boardLayers {
		n := &node{}
		for _, it := range l {
			if i, ok := it.(int); ok {
				n.items = append(n.items, sym(fmt.Sprint(i)))
			} else {
				n.items = append(n.items, it)
			}
		}
		layers.add(n)
	}
	return list("kicad_pcb", list("version", 20221018), list("generator", sym("sheetdiver")),
		list("general", list("thickness", 1.6)), list("paper", "A4"), layers,
		list("setup", list("pad_to_mask_clearance", 0)))
}
