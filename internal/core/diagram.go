package core

import (
	"regexp"
	"slices"
	"sort"
	"strings"

	"github.com/aitakemyjob/sheetdiver/internal/library"
)

// Diagram is a project's block diagram, drawn from its connection table:
// what is drawn and how it connects. The app decides where things go
// (frontend/src/lib/blockDiagram.ts); every rule about what a part, pin
// or passive is lives here.
//
// Parts and connectors are boxes with their pins on the sides: a pin
// faces what it connects to (inputs left of a part, outputs right),
// configuration pins with only a passive on them go along the bottom,
// a pin on the same net as another pin of its part is written inside
// the box ("EN→IN"), and ground pins are a label. Passives (resistors,
// capacitors, inductors, diodes) aren't boxes: they hang off the net or
// pin they serve. Ground isn't drawn.
type Diagram struct {
	Parts    []DiagramPart    `json:"parts"`
	Passives []DiagramPassive `json:"passives"`
	Nets     []DiagramNet     `json:"nets"`
	// Links join parts that share a net, for a diagram without pins (the
	// report's Mermaid chart) and for agents.
	Links []DiagramLink `json:"links"`
}

// DiagramPart is a box: a BOM entry or block that isn't a passive. ID is
// its node ("part:<entry id>", "block:<block id>"); Column orders the
// boxes along the power flow; X and Y are where it was dragged to.
type DiagramPart struct {
	ID     string       `json:"id"`
	Title  string       `json:"title"`
	MPN    string       `json:"mpn,omitempty"`
	Kind   string       `json:"kind"`
	Column int          `json:"column"`
	Pins   []DiagramPin `json:"pins"`
	Ground []string     `json:"ground"`
	X      *float64     `json:"x,omitempty"`
	Y      *float64     `json:"y,omitempty"`
}

// Pin sides on a box.
const (
	SideLeft   = "left"
	SideRight  = "right"
	SideBottom = "bottom"
	// SideInside is a pin written inside the box, tied to another pin.
	SideInside = "inside"
)

// DiagramPin is a connected pin of a box. Tied is the pin (or ground
// net) an inside pin is tied to; Bridged pins have a passive across
// them (LX1 and LX2 with the inductor) and go last on the right.
type DiagramPin struct {
	Name    string `json:"name"`
	Net     string `json:"net"`
	Side    string `json:"side"`
	Tied    string `json:"tied,omitempty"`
	Bridged bool   `json:"bridged,omitempty"`
}

// How a net is drawn.
const (
	// WireBetween joins two parts' pins.
	WireBetween = "between"
	// WireLane runs above the parts, for a net joining three or more.
	WireLane = "lane"
	// WireOut leaves the board from one part's pin (with an arrow when
	// it's an output).
	WireOut = "out"
	// WireNone: the net is a bottom or bridged pin, or a connector pin
	// with its passive beside it; drawn with the passive.
	WireNone = ""
)

// DiagramNet is a net that's drawn: the parts with a pin on it, in
// column order, and how its wire runs.
type DiagramNet struct {
	Name  string   `json:"name"`
	Kind  string   `json:"kind"`
	Parts []string `json:"parts"`
	Wire  string   `json:"wire"`
	Arrow bool     `json:"arrow,omitempty"`
}

// Where a passive hangs.
const (
	// MountWire: below the wire between the net's first two parts.
	MountWire = "wire"
	// MountLane: below the pin of Part on a lane.
	MountLane = "lane"
	// MountOut: below the wire leaving the board from Part.
	MountOut = "out"
	// MountBelow: below Part's bottom pin.
	MountBelow = "below"
	// MountBeside: beside a connector's pin.
	MountBeside = "beside"
	// MountBridge: across Part's two pins on Nets.
	MountBridge = "bridge"
)

// DiagramPassive is a passive (or several alike on one net: "C1,C2")
// and where it hangs. Its other end is ground, except for a bridge.
type DiagramPassive struct {
	ID    string   `json:"id"`
	Label string   `json:"label"`
	Value string   `json:"value,omitempty"`
	Kind  string   `json:"kind"`
	Mount string   `json:"mount"`
	Part  string   `json:"part"`
	Nets  []string `json:"nets"`
}

// DiagramLink says two parts share nets: Kind "power" (From feeds To) or
// "signal"; Label is the bus or the nets.
type DiagramLink struct {
	From  string   `json:"from"`
	To    string   `json:"to"`
	Label string   `json:"label"`
	Kind  string   `json:"kind"`
	Nets  []string `json:"nets"`
}

// Pin names that say what a pin is, for pin tables without types.
var (
	// groundName is a pin that is ground itself (not one tied to it).
	groundName = regexp.MustCompile(`(?i)^((A|D|P|S)?GND\w*|VSS\w*|EP|0V)$|thermal`)
	// mainName is the pin that gets the wire when several pins of a part
	// share a net (IN over EN).
	mainName = regexp.MustCompile(`(?i)^(V?IN|VCC|VDD|VBUS|VBAT|V?OUT)$`)
	// outputName is an output by name.
	outputName = regexp.MustCompile(`(?i)^V?OUT`)
)

// end is something in the connection table: a BOM entry or a block.
type end struct {
	id      string
	title   string
	mpn     string
	kind    string
	value   string
	passive bool
	entry   *library.ProjectPart
}

// diagramKind is how a part category or block kind is drawn.
func diagramKind(kind string, block bool) string {
	switch kind {
	case "resistor", "capacitor", "inductor", "diode", "connector", "module", "ic":
		return kind
	case "tvs":
		return "diode"
	case "ferrite", "fuse":
		return "other"
	}
	if block {
		return "connector"
	}
	return "ic"
}

// ends lists the BOM entries and blocks, keyed by node.
func ends(parts []library.ProjectPart, blocks []library.Block) (map[string]*end, []string) {
	out := map[string]*end{}
	var order []string
	for i := range parts {
		pp := &parts[i]
		id := library.PartNode(pp.ID)
		out[id] = &end{id: id, title: strings.TrimSpace(pp.Designator + " " + pp.MPN), mpn: pp.MPN, kind: diagramKind(pp.Category, false), value: passiveValue(pp.Role), passive: library.TwoTerminal(pp.Category), entry: pp}
		if pp.Designator == "" {
			out[id].title = pp.MPN
		}
		order = append(order, id)
	}
	for _, b := range blocks {
		id := library.BlockNode(b.ID)
		out[id] = &end{id: id, title: b.Name, kind: diagramKind(b.Kind, true), value: b.Value, passive: library.TwoTerminal(b.Kind)}
		order = append(order, id)
	}
	return out, order
}

// valueRe is a component value ("10µF", "36.5k", "2.2uH").
var valueRe = regexp.MustCompile(`^[\d.]+\s*[pnuµmkKMG]?(F|H|Ω|R|ohm)?\b`)

// passiveValue is a BOM entry's value, when its role gives one.
func passiveValue(role string) string {
	if valueRe.MatchString(role) {
		return role
	}
	return ""
}

// nodeOf is a connection's node.
func nodeOf(r ConnectionRow) string {
	if r.EntryID != 0 {
		return library.PartNode(r.EntryID)
	}
	return library.BlockNode(r.BlockID)
}

// diagramBuilder holds what building a diagram needs.
type diagramBuilder struct {
	ends     map[string]*end
	order    []string
	kinds    map[string]string          // net (lower case) → kind
	buses    map[string]string          // net (lower case) → bus
	rows     []ConnectionRow            // in table order
	onNet    map[string][]ConnectionRow // net (lower case) → its rows
	mains    []string                   // boxes, in BOM then block order
	passives map[string][]string        // passive → its non-ground nets
	columns  map[string]int
}

// diagram builds the block diagram of a project's connection table.
func diagram(parts []library.ProjectPart, blocks []library.Block, nets []library.Net, rows []ConnectionRow, layout []library.Position) Diagram {
	b := &diagramBuilder{kinds: map[string]string{}, buses: map[string]string{}, onNet: map[string][]ConnectionRow{}, passives: map[string][]string{}}
	b.ends, b.order = ends(parts, blocks)
	for _, n := range nets {
		b.kinds[strings.ToLower(n.Name)] = n.Kind
		b.buses[strings.ToLower(n.Name)] = n.Bus
	}
	// Pins and passives in the order they were entered (the table itself
	// is listed by net).
	rows = slices.Clone(rows)
	sort.SliceStable(rows, func(i, j int) bool { return rows[i].ID < rows[j].ID })
	connected := map[string]bool{}
	for _, r := range rows {
		if _, ok := b.ends[nodeOf(r)]; !ok {
			continue
		}
		b.rows = append(b.rows, r)
		b.onNet[strings.ToLower(r.Net)] = append(b.onNet[strings.ToLower(r.Net)], r)
		connected[nodeOf(r)] = true
	}
	for _, id := range b.order {
		if !connected[id] {
			continue // the diagram is of what's connected
		}
		if !b.ends[id].passive {
			b.mains = append(b.mains, id)
			continue
		}
		for _, r := range b.rows {
			if nodeOf(r) == id && !b.ground(r.Net) && !containsFold(b.passives[id], r.Net) {
				b.passives[id] = append(b.passives[id], r.Net)
			}
		}
	}
	links := b.links()
	b.columns = columnsOf(b.mains, links)
	d := Diagram{Parts: []DiagramPart{}, Passives: []DiagramPassive{}, Nets: []DiagramNet{}, Links: []DiagramLink{}}
	pos := map[string]library.Position{}
	for _, p := range layout {
		pos[p.Node] = p
	}
	for _, id := range b.mains {
		part := b.part(id)
		if p, ok := pos[id]; ok {
			x, y := p.X, p.Y
			part.X, part.Y = &x, &y
		}
		d.Parts = append(d.Parts, part)
	}
	d.Nets = b.nets(d.Parts)
	d.Passives = b.mount(d.Parts, d.Nets)
	d.Links = links
	return d
}

// drives reports whether a box drives a net: it makes the rail (an
// output rail in the BOM that isn't also its supply), it's a connector
// bringing the net in, or it has an output pin on it.
func (b *diagramBuilder) drives(id, net string) bool {
	e := b.ends[id]
	if e.entry != nil && strings.EqualFold(e.entry.OutputRail, net) && !strings.EqualFold(e.entry.SupplyRail, net) {
		return true
	}
	if e.kind == "connector" {
		return true
	}
	for _, r := range b.onNet[strings.ToLower(net)] {
		if nodeOf(r) == id && output(r) {
			return true
		}
	}
	return false
}

// ground reports whether a net is ground.
func (b *diagramBuilder) ground(net string) bool {
	return b.kinds[strings.ToLower(net)] == library.NetGround
}

// mainsOn lists the boxes with a pin on a net, in order.
func (b *diagramBuilder) mainsOn(net string) []string {
	var out []string
	for _, r := range b.onNet[strings.ToLower(net)] {
		id := nodeOf(r)
		if !b.ends[id].passive && !contains(out, id) {
			out = append(out, id)
		}
	}
	return out
}

// passivesOn lists the passives on a net.
func (b *diagramBuilder) passivesOn(net string) []string {
	var out []string
	for _, id := range b.order {
		if containsFold(b.passives[id], net) {
			out = append(out, id)
		}
	}
	return out
}

// links joins the boxes that share each net other than ground, from the
// one that feeds it; signal links are labelled by bus.
func (b *diagramBuilder) links() []DiagramLink {
	type key struct{ from, to, kind string }
	byKey := map[key]*DiagramLink{}
	var order []key
	var netNames []string
	for _, r := range b.rows {
		if !containsFold(netNames, r.Net) {
			netNames = append(netNames, r.Net)
		}
	}
	for _, net := range netNames {
		if b.ground(net) {
			continue
		}
		ids := b.mainsOn(net)
		kind := "signal"
		if b.kinds[strings.ToLower(net)] == library.NetPower {
			kind = "power"
		}
		label := net
		if bus := b.buses[strings.ToLower(net)]; bus != "" && kind == "signal" {
			label = bus
		}
		for i := 0; i < len(ids); i++ {
			for j := i + 1; j < len(ids); j++ {
				from, to := ids[i], ids[j]
				if b.drives(to, net) && !b.drives(from, net) {
					from, to = to, from
				}
				// Two parts that both draw from a rail aren't linked by it.
				if kind == "power" && !b.drives(from, net) {
					continue
				}
				k := key{from, to, kind}
				if _, ok := byKey[key{to, from, kind}]; ok && kind == "signal" {
					k = key{to, from, kind}
				}
				l := byKey[k]
				if l == nil {
					l = &DiagramLink{From: k.from, To: k.to, Kind: kind, Nets: []string{}}
					byKey[k] = l
					order = append(order, k)
				}
				if !contains(l.Nets, net) {
					l.Nets = append(l.Nets, net)
				}
				if l.Label == "" {
					l.Label = label
				} else if !contains(strings.Split(l.Label, ", "), label) {
					l.Label += ", " + label
				}
			}
		}
	}
	out := make([]DiagramLink, 0, len(order))
	for _, k := range order {
		out = append(out, *byKey[k])
	}
	return out
}

// columnsOf orders the boxes along the links: each is one column right
// of the furthest box linked into it. Unlinked boxes go last. Loops end
// after as many rounds as there are boxes.
func columnsOf(ids []string, links []DiagramLink) map[string]int {
	col := map[string]int{}
	for _, id := range ids {
		col[id] = 0
	}
	for round := 0; round < len(ids); round++ {
		changed := false
		for _, l := range links {
			if c := col[l.From] + 1; c > col[l.To] && c < len(ids) {
				col[l.To] = c
				changed = true
			}
		}
		if !changed {
			break
		}
	}
	linked := map[string]bool{}
	last := 0
	for _, l := range links {
		linked[l.From], linked[l.To] = true, true
		last = max(last, col[l.From], col[l.To])
	}
	for _, id := range ids {
		if !linked[id] && len(links) > 0 {
			col[id] = last + 1
		}
	}
	return col
}

// part builds a box: its pins on their sides.
func (b *diagramBuilder) part(id string) DiagramPart {
	e := b.ends[id]
	p := DiagramPart{ID: id, Title: e.title, MPN: e.mpn, Kind: e.kind, Column: b.columns[id], Pins: []DiagramPin{}, Ground: []string{}}
	var mine []ConnectionRow
	for _, r := range b.rows {
		if nodeOf(r) == id {
			mine = append(mine, r)
		}
	}
	// One pin per net gets the wire, the main one (IN over EN); the others
	// are written tied to it. Pins stay in the order they were entered.
	wired := map[string]string{}
	for _, r := range mine {
		key := strings.ToLower(r.Net)
		if b.ground(r.Net) {
			continue
		}
		if w, ok := wired[key]; !ok || !mainName.MatchString(w) && mainName.MatchString(pinName(r)) {
			wired[key] = pinName(r)
		}
	}
	for _, r := range mine {
		name := pinName(r)
		key := strings.ToLower(r.Net)
		switch {
		case b.ground(r.Net) && groundName.MatchString(name) || b.ground(r.Net) && r.Pin == "" && r.PinName == "":
			if !contains(p.Ground, name) {
				p.Ground = append(p.Ground, name)
			}
		case b.ground(r.Net):
			p.Pins = append(p.Pins, DiagramPin{Name: name, Net: r.Net, Side: SideInside, Tied: r.Net})
		case wired[key] != name:
			p.Pins = append(p.Pins, DiagramPin{Name: name, Net: r.Net, Side: SideInside, Tied: wired[key]})
		default:
			p.Pins = append(p.Pins, DiagramPin{Name: name, Net: r.Net, Side: b.side(e, r)})
		}
	}
	// Two pins with a passive across them and nothing else on their nets
	// (LX1, LX2 and the inductor) sit together, last on the right.
	for _, q := range b.order {
		nets := b.passives[q]
		if len(nets) != 2 || !b.only(id, nets[0]) || !b.only(id, nets[1]) {
			continue
		}
		var idx []int
		for i, pin := range p.Pins {
			if pin.Side != SideInside && containsFold(nets, pin.Net) {
				idx = append(idx, i)
			}
		}
		if len(idx) == 2 {
			for _, i := range idx {
				p.Pins[i].Side, p.Pins[i].Bridged = SideRight, true
			}
		}
	}
	sort.SliceStable(p.Pins, func(i, j int) bool { return !p.Pins[i].Bridged && p.Pins[j].Bridged })
	return p
}

// only reports whether a box is the only one with a pin on a net.
func (b *diagramBuilder) only(id, net string) bool {
	m := b.mainsOn(net)
	return len(m) == 1 && m[0] == id
}

// pinName is what a connected pin is called on the diagram.
func pinName(r ConnectionRow) string {
	return firstNonEmpty(r.PinName, r.Pin, r.Net)
}

// side says which side of its box a pin goes on: towards the other
// parts on its net (on a rail with parts on both sides, only what makes
// it faces right), else by what it is.
func (b *diagramBuilder) side(e *end, r ConnectionRow) string {
	net := r.Net
	var left, right bool
	for _, o := range b.mainsOn(net) {
		if o == e.id {
			continue
		}
		left = left || b.columns[o] < b.columns[e.id]
		right = right || b.columns[o] > b.columns[e.id]
	}
	switch {
	case left && right:
		if b.drives(e.id, net) {
			return SideRight
		}
		return SideLeft
	case right:
		return SideRight
	case left:
		return SideLeft
	}
	if e.entry != nil && strings.EqualFold(e.entry.SupplyRail, net) {
		return SideLeft
	}
	if e.entry != nil && strings.EqualFold(e.entry.OutputRail, net) || output(r) {
		return SideRight
	}
	if hang := b.passivesOn(net); len(hang) > 0 {
		single := true
		for _, q := range hang {
			single = single && len(b.passives[q]) == 1
		}
		if single {
			if e.kind == "connector" {
				return SideRight // the passive sits beside it
			}
			return SideBottom // a configuration pin, its resistor below
		}
	}
	if e.kind == "connector" {
		return SideRight
	}
	return SideLeft
}

// output reports whether a connected pin drives its net.
func output(r ConnectionRow) bool {
	switch strings.ToUpper(r.PinType) {
	case "O", "DO", "AO":
		return true
	}
	return outputName.MatchString(pinName(r))
}

// nets says how each net with a pin on a box is drawn.
func (b *diagramBuilder) nets(parts []DiagramPart) []DiagramNet {
	col := map[string]int{}
	for _, p := range parts {
		col[p.ID] = p.Column
	}
	type at struct {
		part string
		pin  DiagramPin
	}
	byNet := map[string][]at{}
	var names []string
	for _, p := range parts {
		for _, pin := range p.Pins {
			if pin.Side == SideInside {
				continue
			}
			k := strings.ToLower(pin.Net)
			if _, ok := byNet[k]; !ok {
				names = append(names, pin.Net)
			}
			byNet[k] = append(byNet[k], at{p.ID, pin})
		}
	}
	out := []DiagramNet{}
	for _, name := range names {
		pins := byNet[strings.ToLower(name)]
		sort.SliceStable(pins, func(i, j int) bool { return col[pins[i].part] < col[pins[j].part] })
		n := DiagramNet{Name: name, Kind: b.kinds[strings.ToLower(name)], Parts: []string{}}
		for _, p := range pins {
			n.Parts = append(n.Parts, p.part)
		}
		switch {
		case len(pins) >= 3:
			n.Wire = WireLane
		case len(pins) == 2:
			n.Wire = WireBetween
		case pins[0].pin.Side == SideBottom || pins[0].pin.Bridged:
			n.Wire = WireNone
		case b.ends[pins[0].part].kind == "connector" && pins[0].pin.Side == SideRight && len(b.passivesOn(name)) > 0 && n.Kind != library.NetPower:
			n.Wire = WireNone // its passive sits beside the pin
		default:
			n.Wire = WireOut
			e := b.ends[pins[0].part]
			n.Arrow = pins[0].pin.Side == SideRight && (n.Kind == library.NetPower || e.entry != nil && strings.EqualFold(e.entry.OutputRail, name) || outputName.MatchString(pins[0].pin.Name))
		}
		out = append(out, n)
	}
	return out
}

// mount says where each passive hangs, merging alike ones on one net.
func (b *diagramBuilder) mount(parts []DiagramPart, nets []DiagramNet) []DiagramPassive {
	byNet := map[string]DiagramNet{}
	for _, n := range nets {
		byNet[strings.ToLower(n.Name)] = n
	}
	pinOf := func(part, net string) (DiagramPin, bool) {
		for _, p := range parts {
			if p.ID != part {
				continue
			}
			for _, pin := range p.Pins {
				if pin.Side != SideInside && strings.EqualFold(pin.Net, net) {
					return pin, true
				}
			}
		}
		return DiagramPin{}, false
	}
	out := []DiagramPassive{}
	merged := map[string]int{} // net|value|kind → index in out
	for _, id := range b.order {
		nets := b.passives[id]
		if len(nets) == 0 {
			continue
		}
		e := b.ends[id]
		label := e.title
		if e.entry != nil && e.entry.Designator != "" {
			label = e.entry.Designator
		}
		p := DiagramPassive{ID: id, Label: label, Value: e.value, Kind: e.kind, Nets: nets}
		// Across two pins of one box.
		if len(nets) == 2 {
			for _, part := range parts {
				a, okA := pinOf(part.ID, nets[0])
				z, okZ := pinOf(part.ID, nets[1])
				if okA && okZ && a.Bridged && z.Bridged {
					p.Mount, p.Part = MountBridge, part.ID
				}
			}
			if p.Mount != "" {
				out = append(out, p)
				continue
			}
			p.Nets = nets[:1] // a series part hangs from its first net
		}
		n, ok := byNet[strings.ToLower(p.Nets[0])]
		if !ok {
			continue // between passives only: not drawn
		}
		switch n.Wire {
		case WireBetween:
			p.Mount, p.Part = MountWire, n.Parts[0]
		case WireLane:
			p.Mount, p.Part = MountLane, n.Parts[0]
			for _, part := range n.Parts {
				if pin, _ := pinOf(part, n.Name); pin.Side == SideRight {
					p.Mount, p.Part = MountLane, part
					break
				}
			}
		case WireOut:
			p.Mount, p.Part = MountOut, n.Parts[0]
		default:
			p.Part = n.Parts[0]
			if pin, _ := pinOf(p.Part, n.Name); pin.Side == SideBottom {
				p.Mount = MountBelow
			} else {
				p.Mount = MountBeside
			}
		}
		key := strings.ToLower(n.Name) + "|" + p.Value + "|" + p.Kind
		if i, ok := merged[key]; ok {
			out[i].ID += "," + p.ID
			out[i].Label += "," + p.Label
			continue
		}
		merged[key] = len(out)
		out = append(out, p)
	}
	return out
}

// containsFold reports whether list has s, ignoring case.
func containsFold(list []string, s string) bool {
	for _, x := range list {
		if strings.EqualFold(x, s) {
			return true
		}
	}
	return false
}
