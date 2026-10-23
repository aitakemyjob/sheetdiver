package core

import (
	"regexp"
	"slices"
	"sort"
	"strings"

	"github.com/aitakemyjob/sheetdiver/internal/library"
	"github.com/aitakemyjob/sheetdiver/internal/pinrole"
)

// Diagram is a project's block diagram, drawn from its connection table:
// what is drawn and how it connects. The app decides where things go
// (frontend/src/lib/blockDiagram.ts); every rule about what a part, pin
// or passive is lives here.
//
// A part with a pin table is a generated symbol (symbol.go): a rectangle
// with every pin of the table, supplies on top, grounds and pads along
// the bottom, outputs on the right and the rest on the left, in number
// order, unconnected pins dimmed. A block without one shows its
// connected pins, each facing what it connects to (inputs left,
// outputs right, a configuration pin with only a passive on it along
// the bottom). A pin on the same net as the pin beside it is tied to
// it (a jog to its wire); a ground pin goes to a ground symbol. Passives (resistors,
// capacitors, inductors, diodes) aren't boxes: they hang off the net or
// pin they serve. Ground isn't drawn.
//
// Parts can be put in groups (Power, MCU), drawn framed with their
// description and notes. Wires run only inside a group: a net that also
// runs in another group is drawn as a labelled stub there instead, so
// the drawing stays readable however many parts share a rail. A passive
// hangs by the part it supports (set, or the only part its nets reach)
// and is drawn in that part's group.
type Diagram struct {
	Parts    []DiagramPart    `json:"parts"`
	Passives []DiagramPassive `json:"passives"`
	Nets     []DiagramNet     `json:"nets"`
	// Groups are the groups with something drawn in them, in power-flow
	// order. Parts in no group are drawn outside any frame.
	Groups []DiagramGroup `json:"groups"`
	// Links join parts that share a net, for a diagram without pins (the
	// report's Mermaid chart) and for agents.
	Links []DiagramLink `json:"links"`
}

// DiagramGroup is a functional group: a frame around its Parts (and the
// passives hanging from them) with its name, description and notes.
type DiagramGroup struct {
	Name        string   `json:"name"`
	Description string   `json:"description,omitempty"`
	Notes       string   `json:"notes,omitempty"`
	Parts       []string `json:"parts"`
}

// DiagramPart is a box: a BOM entry or block that isn't a passive. ID is
// its node ("part:<entry id>", "block:<block id>"); Column orders the
// boxes along the power flow; Group is the group it's drawn in ("" for
// none); X and Y are where it was dragged to.
type DiagramPart struct {
	ID    string `json:"id"`
	Title string `json:"title"`
	MPN   string `json:"mpn,omitempty"`
	Kind  string `json:"kind"`
	// Shape is the symbol's outline: "ic" (a rectangle with its pins), or
	// a transistor ("mosfet-n", "mosfet-p", "npn", "pnp").
	Shape string `json:"shape"`
	// Full means Pins is the whole pin table, connected or not.
	Full   bool   `json:"full,omitempty"`
	Column int    `json:"column"`
	Group  string `json:"group,omitempty"`
	// Cluster is the subgroup it was put in: the parts of a group with
	// the same cluster are laid out and wired together as one unit.
	Cluster string       `json:"cluster,omitempty"`
	Pins    []DiagramPin `json:"pins"`
	X       *float64     `json:"x,omitempty"`
	Y       *float64     `json:"y,omitempty"`
}

// Pin sides on a symbol.
const (
	SideLeft   = "left"
	SideRight  = "right"
	SideTop    = "top"
	SideBottom = "bottom"
)

// DiagramPin is a pin of a symbol: its Number from the pin table (""
// for a block's pin), its Net ("" when unconnected), its Side, and its
// Role (pinrole). Tied is the pin an inside pin is tied to; Bridged pins
// have a passive across them (LX1 and LX2 with the inductor) and go last
// on the right; Ground pins (GND, a thermal pad, a pin tied to ground)
// go along the bottom, each to a ground symbol; Terminal is a transistor
// pin's terminal (gate, drain, source; base, collector, emitter).
type DiagramPin struct {
	Name      string `json:"name"`
	Number    string `json:"number,omitempty"`
	Net       string `json:"net,omitempty"`
	Side      string `json:"side"`
	Role      string `json:"role"`
	Connected bool   `json:"connected"`
	Terminal  string `json:"terminal,omitempty"`
	Tied      string `json:"tied,omitempty"`
	Bridged   bool   `json:"bridged,omitempty"`
	Ground    bool   `json:"ground,omitempty"`
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
	// WireNone: the net is a bottom or bridged pin; drawn with its passive.
	WireNone = ""
)

// DiagramNet is a net's wire within one group: the parts there with a
// pin on it, in column order, and how the wire runs. A net in several
// groups has one per group, each Offboard: it also runs elsewhere, and
// its name says where.
type DiagramNet struct {
	Name     string   `json:"name"`
	Kind     string   `json:"kind"`
	Group    string   `json:"group,omitempty"`
	Parts    []string `json:"parts"`
	Wire     string   `json:"wire"`
	Arrow    bool     `json:"arrow,omitempty"`
	Offboard bool     `json:"offboard,omitempty"`
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
	// MountBridge: across Part's two pins on Nets.
	MountBridge = "bridge"
	// MountChain: after the passive Part names, on the net they share
	// (Nets[0]), which reaches no part but through it (C2 on VOUT after
	// the inductor; an LED after its series resistor).
	MountChain = "chain"
)

// DiagramPassive is a passive (or several alike on one net, serving the
// same part: "C1,C2") and where it hangs, from Nets[0], by Part. Its
// other end is ground, or Nets[1] (a pull-up's rail; for a bridge, the
// other pin's net). Supports is the part it serves, when known: set, or
// Guessed from its nets; Group the group it's drawn in. X and Y are where
// it was placed, if it was.
type DiagramPassive struct {
	ID       string   `json:"id"`
	Label    string   `json:"label"`
	Value    string   `json:"value,omitempty"`
	Kind     string   `json:"kind"`
	Mount    string   `json:"mount"`
	Part     string   `json:"part"`
	Nets     []string `json:"nets"`
	Supports string   `json:"supports,omitempty"`
	Guessed  bool     `json:"guessed,omitempty"`
	Group    string   `json:"group,omitempty"`
	// Cluster is the subgroup it was put in (see DiagramPart.Cluster).
	Cluster string `json:"cluster,omitempty"`
	// Shape is the standard symbol it's drawn with (resistor, capacitor,
	// led…); Ends are its terminals at Nets[0]'s end and the far end
	// (["anode", "cathode"], ["1", "2"], ["+", "-"]).
	Shape string   `json:"shape"`
	Ends  []string `json:"ends"`
	X     *float64 `json:"x,omitempty"`
	Y     *float64 `json:"y,omitempty"`
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

// Kinds as datasheets and BOMs name them.
var (
	// connectorKindRe and otherKindRe are categories as datasheets and
	// BOMs name them ("Pin header", "Buzzer"); the two-terminals and
	// transistors among them are in symbolKinds.
	connectorKindRe = regexp.MustCompile(`(?i)connector|header|socket|receptacle|jack|terminal`)
	otherKindRe     = regexp.MustCompile(`(?i)buzzer|speaker|test ?point|jumper`)
	// refPinRe is a connector pin that senses a rail rather than supplying
	// it: a debug header's target reference.
	refPinRe = regexp.MustCompile(`(?i)^V_?T(ARGET|_?REF|G)\b|^VREF|^VTREF|^V?SENSE`)
)

// end is something in the connection table: a BOM entry or a block.
type end struct {
	id    string
	title string
	mpn   string
	kind  string
	value string
	// label names it as a passive: its designator(s), or its name.
	label   string
	passive bool
	entry   *library.ProjectPart
}

// ends lists the BOM entries and blocks, keyed by node. A passive's
// value is what its role says ("U1 input 10µF"), else what its part's
// description starts with ("100nF 50V X7R 0603"). A part whose pin
// table has more than two pins is a box with them whatever its category
// says (a USBLC6 ESD array is a TVS with six pins), unless the entry is
// several designators of it (R1,R2 of a resistor array).
func ends(parts []library.ProjectPart, blocks []library.Block, descriptions map[int64]string, pins map[int64][]library.Pin) (map[string]*end, []string) {
	out := map[string]*end{}
	var order []string
	for i := range parts {
		pp := &parts[i]
		id := library.PartNode(pp.ID)
		value := passiveValue(pp.Role)
		if value == "" {
			value = descriptionValue(descriptions[pp.ID])
		}
		kind, passive := symbolKind(pp.Category, false), twoTerminal(pp.Category, false)
		if passive && len(pins[pp.ID]) > 2 && !strings.ContainsAny(strings.TrimSpace(pp.Designator), ", ") {
			kind, passive = "ic", false
		}
		out[id] = &end{id: id, title: strings.TrimSpace(pp.Designator + " " + pp.MPN), mpn: pp.MPN, kind: kind, value: value, label: firstNonEmpty(pp.Designator, pp.MPN), passive: passive, entry: pp}
		if pp.Designator == "" {
			out[id].title = pp.MPN
		}
		order = append(order, id)
	}
	for _, b := range blocks {
		id := library.BlockNode(b.ID)
		out[id] = &end{id: id, title: b.Name, kind: symbolKind(b.Kind, true), value: shortValue(b.Value), label: b.Name, passive: twoTerminal(b.Kind, true)}
		order = append(order, id)
	}
	return out, order
}

// A component value ("10µF", "36.5k", "2.2uH", "0Ω", "32.768 kHz") as a
// word of its own: leadingRe at the start of a text, where a bare number
// will do ("330" for a resistor), valueRe anywhere in it, where it needs
// its multiplier or unit ("10k" of "BOOT0 10k pull-down", not the 1 of
// "U1"). It must end there ("3V3" isn't a value), which \b can't say
// after "Ω".
var (
	leadingRe = regexp.MustCompile(`^([\d.]+\s*[pnuµmkKMG]?(?:F|H|Ω|R|ohm|Hz)?)(?:$|[\s,;:/(])`)
	valueRe   = regexp.MustCompile(`(?:^|[\s,;:/(])(\d+(?:\.\d+)?\s*(?:[pnuµmkKMG](?:F|H|Ω|R|ohm|Hz)?|F|H|Ω|R|ohm|Hz))(?:$|[\s,;:/)])`)
)

// passiveValue is the value a text starts with ("10µF" of "10µF input
// bulk"), else the first value in it ("10k" of "BOOT0 10k pull-down"),
// else nothing.
func passiveValue(text string) string {
	text = strings.TrimSpace(text)
	if m := leadingRe.FindStringSubmatch(text); m != nil {
		return m[1]
	}
	if m := valueRe.FindStringSubmatch(text); m != nil {
		return strings.TrimSpace(m[1])
	}
	return ""
}

// descriptionValue is the value a part's description starts with ("100nF"
// of "100nF 50V X7R 0603"), with its unit: a bare number there is a
// package or a count, and a value later in it may be another part's (a
// family's catalogue names one of them).
func descriptionValue(description string) string {
	if m := valueRe.FindStringSubmatch(strings.TrimSpace(description)); m != nil && m[0] == strings.TrimSpace(description)[:len(m[0])] {
		return strings.TrimSpace(m[1])
	}
	return ""
}

// shortValue is a block's value as printed: the value it starts with, or
// at most 12 characters, so a long description doesn't stretch its box.
func shortValue(v string) string {
	if m := passiveValue(v); m != "" {
		return m
	}
	if r := []rune(strings.TrimSpace(v)); len(r) > 12 {
		return string(r[:11]) + "…"
	}
	return strings.TrimSpace(v)
}

// nodeOf is a connection's node: an entry's, or one of its designators'.
func nodeOf(r ConnectionRow) string {
	if r.EntryID != 0 {
		return library.RefNode(r.EntryID, r.Ref)
	}
	return library.BlockNode(r.BlockID)
}

// diagramInput is what a block diagram is drawn from: the BOM, the
// blocks, the connection table, the saved layout, and the groups and
// assignments.
type diagramInput struct {
	parts   []library.ProjectPart
	blocks  []library.Block
	nets    []library.Net
	rows    []ConnectionRow
	layout  []library.Position
	groups  []library.Group
	assigns []library.Assignment
	// rails are the project's rails: one's source makes its net.
	rails []library.Rail
	// pins is each BOM entry's pin table (for its package), by entry id.
	pins map[int64][]library.Pin
	// descriptions is each BOM entry's part description, by entry id: a
	// passive's value when its role doesn't say.
	descriptions map[int64]string
}

// diagramBuilder holds what building a diagram needs.
type diagramBuilder struct {
	ends     map[string]*end
	order    []string
	kinds    map[string]string          // net (lower case) → kind
	sources  map[string]string          // rail (lower case) → what supplies it
	buses    map[string]string          // net (lower case) → bus
	rows     []ConnectionRow            // in table order
	onNet    map[string][]ConnectionRow // net (lower case) → its rows
	mains    []string                   // boxes, in BOM then block order
	passives map[string][]string        // passive → its non-ground nets
	columns  map[string]int
	pins     map[int64][]library.Pin       // entry → its pin table
	assigned map[string]library.Assignment // node → its assignment
	groupOf  map[string]string             // box → its group
	supports map[string]string             // passive → the box it serves
	guessed  map[string]bool               // passive → its support was guessed
}

// diagram builds the block diagram of a project's connection table.
func diagram(in diagramInput) Diagram {
	b := &diagramBuilder{kinds: map[string]string{}, sources: map[string]string{}, buses: map[string]string{}, onNet: map[string][]ConnectionRow{}, passives: map[string][]string{},
		assigned: map[string]library.Assignment{}, groupOf: map[string]string{}, supports: map[string]string{}, guessed: map[string]bool{}, pins: in.pins}
	b.ends, b.order = ends(in.parts, in.blocks, in.descriptions, in.pins)
	for _, r := range in.rails {
		b.sources[strings.ToLower(r.Name)] = strings.TrimSpace(r.Source)
	}
	for _, a := range in.assigns {
		b.assigned[a.Node] = a
	}
	nets, rows, layout := in.nets, in.rows, in.layout
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
		b.splitEnd(r)
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
	b.assign()
	d := Diagram{Parts: []DiagramPart{}, Passives: []DiagramPassive{}, Nets: []DiagramNet{}, Groups: []DiagramGroup{}, Links: []DiagramLink{}}
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
	orderPins(d.Parts, d.Nets)
	d.Passives = b.mount(d.Parts, d.Nets)
	for i, q := range d.Passives {
		if p, ok := pos[strings.Split(q.ID, ",")[0]]; ok {
			x, y := p.X, p.Y
			d.Passives[i].X, d.Passives[i].Y = &x, &y
		}
	}
	d.Groups = groups(in.groups, d.Parts)
	d.Links = links
	return d
}

// assignment is a node's assignment; a designator of an entry with
// several ("part:3/R1") falls back to its entry's.
func (b *diagramBuilder) assignment(id string) library.Assignment {
	if a, ok := b.assigned[id]; ok {
		return a
	}
	if i := strings.Index(id, "/"); i > 0 {
		return b.assigned[id[:i]]
	}
	return library.Assignment{}
}

// box is the drawn box a node names: itself, or the entry of one of its
// designators ("part:3" for "part:3/R1" drawn whole, and the reverse).
func (b *diagramBuilder) box(node string) (string, bool) {
	if node == "" {
		return "", false
	}
	if contains(b.mains, node) {
		return node, true
	}
	for _, m := range b.mains {
		if strings.HasPrefix(m, node+"/") || strings.HasPrefix(node, m+"/") {
			return m, true
		}
	}
	return "", false
}

// assign decides each box's group and each passive's part and group: a
// box is in its group, or the group of the box it supports; a passive
// serves the box it's set to support, else the one it's guessed to (see
// guessSupport), and is drawn in its group, else in that box's.
func (b *diagramBuilder) assign() {
	for _, id := range b.mains {
		if a := b.assignment(id); a.Group != "" {
			b.groupOf[id] = a.Group
		}
	}
	for _, id := range b.mains {
		if _, ok := b.groupOf[id]; ok {
			continue
		}
		if to, ok := b.box(b.assignment(id).Supports); ok && to != id {
			if g := b.groupOf[to]; g != "" {
				b.groupOf[id] = g
			}
		}
	}
	for _, id := range b.order {
		if len(b.passives[id]) == 0 {
			continue
		}
		if to, ok := b.box(b.assignment(id).Supports); ok {
			b.supports[id] = to
			continue
		}
		if to := b.guessSupport(id); to != "" {
			b.supports[id], b.guessed[id] = to, true
		}
	}
}

// guessSupport is the box a passive serves when nothing says so, from
// the boxes on its nets: a protection diode serves the connector it
// guards; anything else serves the IC there (over connectors and
// modules, which bring a net in or take it off the board), and of
// several ICs, the one that doesn't drive the net (a bulk capacitor
// between an eFuse's output and a regulator's VIN serves the regulator).
// "" when that isn't one box.
func (b *diagramBuilder) guessSupport(id string) string {
	var reach []string
	for _, net := range b.passives[id] {
		for _, m := range b.mainsOn(net) {
			if !contains(reach, m) {
				reach = append(reach, m)
			}
		}
	}
	ofKind := func(ids []string, kind string) []string {
		var out []string
		for _, m := range ids {
			if b.ends[m].kind == kind {
				out = append(out, m)
			}
		}
		return out
	}
	if b.ends[id].kind == "diode" {
		if c := ofKind(reach, "connector"); len(c) == 1 {
			return c[0]
		}
	}
	pool := ofKind(reach, "ic")
	if len(pool) == 0 {
		pool = reach
	}
	if len(pool) == 1 {
		return pool[0]
	}
	var uses []string
	for _, m := range pool {
		drives := false
		for _, net := range b.passives[id] {
			drives = drives || b.drives(m, net)
		}
		if !drives {
			uses = append(uses, m)
		}
	}
	if len(uses) == 1 {
		return uses[0]
	}
	return ""
}

// passiveGroup is the group a passive belongs in: its own, else its
// part's.
func (b *diagramBuilder) passiveGroup(id string) string {
	if g := b.assignment(id).Group; g != "" {
		return g
	}
	return b.groupOf[b.supports[id]]
}

// groups lists the groups with a box drawn in them, ordered along the
// power flow (by their first box's column), then by name.
func groups(defined []library.Group, parts []DiagramPart) []DiagramGroup {
	byName := map[string]*DiagramGroup{}
	first := map[string]int{}
	var order []string
	for _, p := range parts {
		if p.Group == "" {
			continue
		}
		k := strings.ToLower(p.Group)
		g := byName[k]
		if g == nil {
			g = &DiagramGroup{Name: p.Group, Parts: []string{}}
			for _, dg := range defined {
				if strings.EqualFold(dg.Name, p.Group) {
					g.Name, g.Description, g.Notes = dg.Name, dg.Description, dg.Notes
				}
			}
			byName[k], first[k] = g, p.Column
			order = append(order, k)
		}
		g.Parts = append(g.Parts, p.ID)
		first[k] = min(first[k], p.Column)
	}
	sort.SliceStable(order, func(i, j int) bool {
		if first[order[i]] != first[order[j]] {
			return first[order[i]] < first[order[j]]
		}
		return order[i] < order[j]
	})
	out := make([]DiagramGroup, 0, len(order))
	for _, k := range order {
		out = append(out, *byName[k])
	}
	return out
}

// drives reports whether a box drives a net: it makes the rail (an
// output rail in the BOM that isn't also its supply), it has an output
// pin on it, or it's a connector bringing the net in. A connector doesn't
// bring in a net that a part on the board makes, nor one it only senses
// (a debug header's VTref on 3V3 is a load).
func (b *diagramBuilder) drives(id, net string) bool {
	if b.makes(id, net) {
		return true
	}
	if b.ends[id].kind != "connector" {
		return false
	}
	for _, r := range b.onNet[strings.ToLower(net)] {
		o := nodeOf(r)
		if o == id && refPinRe.MatchString(pinName(r)) {
			return false
		}
		if o != id && !b.ends[o].passive && b.ends[o].kind != "connector" && b.makes(o, net) {
			return false
		}
	}
	return true
}

// makes reports whether a part makes a net: it's the part's output rail
// (and not also its supply), the part is the rail's source, or the part
// has an output pin on it.
func (b *diagramBuilder) makes(id, net string) bool {
	e := b.ends[id]
	if e.entry != nil && strings.EqualFold(e.entry.OutputRail, net) && !strings.EqualFold(e.entry.SupplyRail, net) {
		return true
	}
	if src := b.sources[strings.ToLower(net)]; src != "" && !e.passive && (e.entry == nil || !strings.EqualFold(e.entry.SupplyRail, net)) {
		if strings.EqualFold(src, e.label) || strings.EqualFold(src, e.mpn) || strings.EqualFold(src, strings.Fields(e.title + " ")[0]) {
			return true
		}
	}
	for _, r := range b.onNet[strings.ToLower(net)] {
		if nodeOf(r) == id && output(r) {
			return true
		}
	}
	return false
}

// splitEnd adds the end of one designator of a BOM entry with several
// ("R1" of "R1,R2"), drawn on its own, after its entry.
func (b *diagramBuilder) splitEnd(r ConnectionRow) {
	if r.Ref == "" || r.EntryID == 0 {
		return
	}
	id := nodeOf(r)
	parent, ok := b.ends[library.PartNode(r.EntryID)]
	if _, done := b.ends[id]; done || !ok {
		return
	}
	e := *parent
	e.id, e.label, e.title = id, r.Ref, strings.TrimSpace(r.Ref+" "+parent.mpn)
	b.ends[id] = &e
	i := slices.Index(b.order, parent.id)
	b.order = slices.Insert(b.order, i+1, id)
}

// ground reports whether a net is ground.
func (b *diagramBuilder) ground(net string) bool {
	return b.kinds[strings.ToLower(net)] == library.NetGround
}

// throughPassives lists the boxes on a net, and those on the far side
// of a series passive on it (an LED behind its resistor; not a pull-up,
// whose rail goes everywhere), in order.
func (b *diagramBuilder) throughPassives(net string) []string {
	out := b.mainsOn(net)
	if b.kinds[strings.ToLower(net)] == library.NetPower {
		return out
	}
	for _, q := range b.passivesOn(net) {
		if nets := b.passives[q]; len(nets) == 2 {
			for _, n := range nets {
				if strings.EqualFold(n, net) || b.kinds[strings.ToLower(n)] == library.NetPower {
					continue
				}
				for _, o := range b.mainsOn(n) {
					if !contains(out, o) {
						out = append(out, o)
					}
				}
			}
		}
	}
	return out
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
	// Two boxes joined through a series passive (an MCU pin, a resistor,
	// an LED) are linked too, from the one that drives the passive's net.
	// Not through a pull-up: its rail goes everywhere.
	for _, q := range b.order {
		nets := b.passives[q]
		if len(nets) != 2 || b.kinds[strings.ToLower(nets[0])] == library.NetPower || b.kinds[strings.ToLower(nets[1])] == library.NetPower {
			continue
		}
		for _, from := range b.mainsOn(nets[0]) {
			for _, to := range b.mainsOn(nets[1]) {
				if from == to || !b.drives(from, nets[0]) && b.drives(to, nets[1]) {
					continue
				}
				k := key{from, to, "signal"}
				if _, ok := byKey[k]; ok {
					continue
				}
				if _, ok := byKey[key{to, from, "signal"}]; ok {
					continue
				}
				byKey[k] = &DiagramLink{From: from, To: to, Kind: "signal", Nets: []string{nets[0], nets[1]}, Label: nets[0]}
				order = append(order, k)
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

// part builds a symbol: from the entry's pin table when it has one,
// else from its connected pins; then marks the pins a passive bridges.
func (b *diagramBuilder) part(id string) DiagramPart {
	e := b.ends[id]
	var mine []ConnectionRow
	for _, r := range b.rows {
		if nodeOf(r) == id {
			mine = append(mine, r)
		}
	}
	var p DiagramPart
	if e.entry != nil && len(b.pins[e.entry.ID]) > 0 {
		p = b.fullSymbol(id, e, b.pins[e.entry.ID], mine)
		tiePins(&p)
	} else {
		p = b.connectedSymbol(id, e, mine)
	}
	// Two pins with a passive across them and nothing else on their nets
	// (LX1, LX2 and the inductor) are bridged: on a generated symbol
	// where they are, on a box together, last on the right.
	for _, q := range b.order {
		nets := b.passives[q]
		if len(nets) != 2 || !b.only(id, nets[0]) || !b.only(id, nets[1]) {
			continue
		}
		var idx []int
		for i, pin := range p.Pins {
			if pin.Connected && pin.Tied == "" && containsFold(nets, pin.Net) {
				idx = append(idx, i)
			}
		}
		if len(idx) != 2 || p.Full && p.Pins[idx[0]].Side != p.Pins[idx[1]].Side {
			continue
		}
		for _, i := range idx {
			p.Pins[i].Bridged = true
			if !p.Full {
				p.Pins[i].Side = SideRight
			}
		}
	}
	if !p.Full {
		sort.SliceStable(p.Pins, func(i, j int) bool { return !p.Pins[i].Bridged && p.Pins[j].Bridged })
	}
	transistorSymbol(&p)
	return p
}

// connectedSymbol builds the symbol of a block, or of a part without a
// pin table: its connected pins only, each on the side it faces.
func (b *diagramBuilder) connectedSymbol(id string, e *end, mine []ConnectionRow) DiagramPart {
	p := DiagramPart{ID: id, Title: e.title, MPN: e.mpn, Kind: e.kind, Shape: "ic", Column: b.columns[id], Group: b.groupOf[id], Cluster: b.assignment(id).Cluster, Pins: []DiagramPin{}}
	// One pin per net gets the wire, the main one (IN over EN); the others
	// are written tied to it. Pins stay in the order they were entered.
	type wiredPin struct {
		name string
		main bool
	}
	wired := map[string]wiredPin{}
	for _, r := range mine {
		key := strings.ToLower(r.Net)
		if b.ground(r.Net) {
			continue
		}
		if w, ok := wired[key]; !ok || !w.main && mainPin(r) {
			wired[key] = wiredPin{pinName(r), mainPin(r)}
		}
	}
	drawn := map[string]bool{} // nets whose wired pin is placed
	for _, r := range mine {
		name := pinName(r)
		key := strings.ToLower(r.Net)
		switch {
		case b.ground(r.Net):
			// Each ground connection goes to a ground symbol below the box.
			if r.Pin == "" && r.PinName == "" {
				name = "GND"
			}
			if !slices.ContainsFunc(p.Pins, func(x DiagramPin) bool { return x.Ground && x.Name == name }) {
				p.Pins = append(p.Pins, connectedPin(r, name, SideBottom, "", true))
			}
		case wired[key].name != name || drawn[key]:
			// A second pin of the same name on the net (a connector's two
			// VBUS pins) is tied to the first, like any other.
			p.Pins = append(p.Pins, connectedPin(r, name, b.side(e, r), wired[key].name, false))
		default:
			drawn[key] = true
			p.Pins = append(p.Pins, connectedPin(r, name, b.side(e, r), "", false))
		}
	}
	// A tied pin sits on its main pin's side, next to it (the drawing puts
	// it right after); one whose main pin is along the bottom stays a pin
	// of its own.
	for i := range p.Pins {
		pin := &p.Pins[i]
		if pin.Tied == "" {
			continue
		}
		for _, m := range p.Pins {
			if m.Tied == "" && m.Name == pin.Tied && strings.EqualFold(m.Net, pin.Net) {
				if m.Side == SideLeft || m.Side == SideRight {
					pin.Side = m.Side
				} else {
					pin.Tied = ""
				}
				break
			}
		}
	}
	return p
}

// connectedPin is a drawn pin for a connection row: numbered when the
// row names a pin of the pin table.
func connectedPin(r ConnectionRow, name, side, tied string, ground bool) DiagramPin {
	number := ""
	if r.PinName != "" {
		number = r.Pin
	}
	return DiagramPin{Name: name, Number: number, Net: r.Net, Side: side, Role: string(rowRole(r)), Connected: true, Tied: tied, Ground: ground}
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
// parts on its net in its group (a part in another group is reached by a
// label, not a wire; on a rail with parts on both sides, only what makes
// it faces right), else by what it is.
func (b *diagramBuilder) side(e *end, r ConnectionRow) string {
	net := r.Net
	var left, right, offboard bool
	for _, o := range b.throughPassives(net) {
		if o == e.id {
			continue
		}
		if !strings.EqualFold(b.groupOf[o], b.groupOf[e.id]) {
			offboard = true
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
	case offboard:
		// Reached by its name in another group: a labelled stub on a
		// side, never a bottom pin (a crystal's pins to the MCU).
		if e.kind == "connector" || b.drives(e.id, net) {
			return SideRight
		}
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

// rowRole is what a connected pin is: from the pin table's type and
// name when it has one, else the name as entered.
func rowRole(r ConnectionRow) pinrole.Role {
	return pinrole.Of(pinName(r), r.PinType, "")
}

// output reports whether a connected pin drives its net.
func output(r ConnectionRow) bool {
	return rowRole(r) == pinrole.Output
}

// mainPin reports whether a pin is the one that gets the wire when
// several pins of a part share a net (VIN over EN, OUT over OUTS).
func mainPin(r ConnectionRow) bool {
	switch rowRole(r) {
	case pinrole.Power, pinrole.Output:
		return true
	}
	return false
}

// nets says how each net with a pin on a box is drawn: one wire per
// group the net has pins in, each Offboard when there are several.
func (b *diagramBuilder) nets(parts []DiagramPart) []DiagramNet {
	col := map[string]int{}
	for _, p := range parts {
		col[p.ID] = p.Column
	}
	type at struct {
		part string
		pin  DiagramPin
	}
	type piece struct {
		net, group string
	}
	byPiece := map[piece][]at{}
	var order []piece
	groupsOf := map[string]int{} // net (lower case) → how many groups it's in
	for _, p := range parts {
		for _, pin := range p.Pins {
			if !pin.Connected || pin.Ground || pin.Tied != "" {
				continue
			}
			k := piece{strings.ToLower(pin.Net), strings.ToLower(p.Group)}
			if _, ok := byPiece[k]; !ok {
				order = append(order, k)
				groupsOf[k.net]++
			}
			byPiece[k] = append(byPiece[k], at{p.ID, pin})
		}
	}
	// A net's pieces together, in the order the net first appears.
	first := map[string]int{}
	for i, k := range order {
		if _, ok := first[k.net]; !ok {
			first[k.net] = i
		}
	}
	sort.SliceStable(order, func(i, j int) bool { return first[order[i].net] < first[order[j].net] })
	out := []DiagramNet{}
	for _, k := range order {
		pins := byPiece[k]
		sort.SliceStable(pins, func(i, j int) bool { return col[pins[i].part] < col[pins[j].part] })
		name := pins[0].pin.Net
		n := DiagramNet{Name: name, Kind: b.kinds[k.net], Group: b.groupOf[pins[0].part], Parts: []string{}, Offboard: groupsOf[k.net] > 1}
		for _, p := range pins {
			n.Parts = append(n.Parts, p.part)
		}
		switch {
		case len(pins) >= 3:
			n.Wire = WireLane
		case len(pins) == 2:
			n.Wire = WireBetween
		case !n.Offboard && (pins[0].pin.Side == SideBottom || pins[0].pin.Bridged):
			n.Wire = WireNone
		default:
			// A stub leaving the box, labelled: off the board, or to the
			// same net in another group.
			n.Wire = WireOut
			e := b.ends[pins[0].part]
			n.Arrow = pins[0].pin.Side == SideRight && (n.Kind == library.NetPower || e.entry != nil && strings.EqualFold(e.entry.OutputRail, name) || pins[0].pin.Role == string(pinrole.Output))
		}
		out = append(out, n)
	}
	return out
}

// mount says where each passive hangs: from its net's wire in the group
// of the part it serves (beside that part on a lane), merging alike
// passives that serve the same part.
func (b *diagramBuilder) mount(parts []DiagramPart, nets []DiagramNet) []DiagramPassive {
	byNet := map[string][]DiagramNet{} // net (lower case) → its wires, one per group
	for _, n := range nets {
		k := strings.ToLower(n.Name)
		byNet[k] = append(byNet[k], n)
	}
	pinOf := func(part, net string) (DiagramPin, bool) {
		for _, p := range parts {
			if p.ID != part {
				continue
			}
			for _, pin := range p.Pins {
				if pin.Connected && pin.Tied == "" && strings.EqualFold(pin.Net, net) {
					return pin, true
				}
			}
		}
		return DiagramPin{}, false
	}
	groupOf := map[string]string{}
	for _, p := range parts {
		groupOf[p.ID] = p.Group
	}
	out := []DiagramPassive{}
	var pending []DiagramPassive // on no part's net: chains, or not drawn
	merged := map[string]int{}   // nets|value|kind|part → index in out
	for _, id := range b.order {
		nets := b.passives[id]
		if len(nets) == 0 {
			continue
		}
		e := b.ends[id]
		serves := b.supports[id]
		p := DiagramPassive{ID: id, Label: e.label, Value: e.value, Kind: e.kind, Shape: passiveShape(e.kind), Nets: nets, Supports: serves, Guessed: b.guessed[id], Cluster: b.assignment(id).Cluster}
		// Across two pins of one box.
		if len(nets) == 2 {
			for _, part := range parts {
				a, okA := pinOf(part.ID, nets[0])
				z, okZ := pinOf(part.ID, nets[1])
				if okA && okZ && a.Bridged && z.Bridged {
					p.Mount, p.Part, p.Group = MountBridge, part.ID, part.Group
				}
			}
			if p.Mount != "" {
				p.Ends = b.passiveEnds(id, p)
				out = append(out, p)
				continue
			}
			// Between two nets that aren't a box's pair (a pull-up from
			// SDA to 3V3): it hangs from the first one drawn, and its other
			// end is shown by name.
			if _, drawn := byNet[strings.ToLower(nets[0])]; !drawn {
				p.Nets = []string{nets[1], nets[0]}
			}
		}
		// In series between two parts (PA5 → R5 → LED), it hangs from the
		// net that reaches the part it serves.
		if len(p.Nets) == 2 && serves != "" && !reaches(byNet[strings.ToLower(p.Nets[0])], serves) && reaches(byNet[strings.ToLower(p.Nets[1])], serves) {
			p.Nets = []string{p.Nets[1], p.Nets[0]}
		}
		wires, ok := byNet[strings.ToLower(p.Nets[0])]
		if !ok {
			pending = append(pending, p) // between passives only: a chain, if one leads there
			continue
		}
		n := pickWire(wires, serves, b.passiveGroup(id))
		p.Group = n.Group
		switch n.Wire {
		case WireBetween:
			p.Mount, p.Part = MountWire, n.Parts[0]
			if contains(n.Parts, serves) {
				p.Part = serves // by the part it serves, at its end of the wire
			}
		case WireLane:
			p.Mount, p.Part = MountLane, n.Parts[0]
			if contains(n.Parts, serves) {
				// Beside the part it serves, not with every other passive on
				// the rail.
				p.Part = serves
				break
			}
			for _, part := range n.Parts {
				if pin, _ := pinOf(part, n.Name); pin.Side == SideRight {
					p.Mount, p.Part = MountLane, part
					break
				}
			}
		case WireOut:
			p.Mount, p.Part = MountOut, n.Parts[0]
		default:
			p.Mount, p.Part = MountBelow, n.Parts[0]
		}
		p.Ends = b.passiveEnds(id, p)
		key := strings.ToLower(strings.Join(p.Nets, "|")) + "|" + p.Value + "|" + p.Kind + "|" + p.Part + "|" + p.Supports + "|" + strings.ToLower(p.Cluster) + "|" + strings.Join(p.Ends, "|")
		if i, ok := merged[key]; ok {
			out[i].ID += "," + p.ID
			out[i].Label += "," + p.Label
			continue
		}
		merged[key] = len(out)
		out = append(out, p)
	}
	// Reached only through another passive (C2 on VOUT after L1; an LED
	// after its series resistor): a chain off that passive, from the net
	// they share, and chains go on (the next off C2's far net).
	for progress := true; progress && len(pending) > 0; {
		progress = false
		rest := pending[:0]
		for _, p := range pending {
			lead, net, ok := chainLead(out, p)
			if !ok {
				rest = append(rest, p)
				continue
			}
			if !strings.EqualFold(p.Nets[0], net) {
				p.Nets = []string{p.Nets[1], p.Nets[0]}
			}
			p.Mount, p.Part, p.Group = MountChain, lead.ID, lead.Group
			p.Ends = b.passiveEnds(p.ID, p)
			out = append(out, p)
			progress = true
		}
		pending = rest
	}
	return out
}

// chainLead finds the drawn passive whose far net a passive is on, and
// that net.
func chainLead(drawn []DiagramPassive, p DiagramPassive) (DiagramPassive, string, bool) {
	for _, q := range drawn {
		if q.Mount == MountBridge || len(q.Nets) != 2 {
			continue
		}
		for _, n := range p.Nets {
			if strings.EqualFold(n, q.Nets[1]) {
				return q, n, true
			}
		}
	}
	return DiagramPassive{}, "", false
}

// reaches reports whether one of a net's wires reaches a part.
func reaches(wires []DiagramNet, part string) bool {
	for _, n := range wires {
		if contains(n.Parts, part) {
			return true
		}
	}
	return false
}

// pickWire chooses which of a net's wires (one per group) a passive
// hangs from: the one reaching the part it serves, else the one in its
// group, else the first.
func pickWire(wires []DiagramNet, serves, group string) DiagramNet {
	for _, n := range wires {
		if serves != "" && contains(n.Parts, serves) {
			return n
		}
	}
	for _, n := range wires {
		if strings.EqualFold(n.Group, group) {
			return n
		}
	}
	return wires[0]
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

// orderPins orders each side of each box so wires leave it without
// crossing: nets running along a lane first (their riser goes up past
// nothing), then wires to other parts, then wires leaving the board
// (whose passives drop past nothing below them), then bridged pins.
// Along the bottom, ground pins come last.
func orderPins(parts []DiagramPart, nets []DiagramNet) {
	wire := map[string]string{}
	for _, n := range nets {
		wire[strings.ToLower(n.Name)] = n.Wire
	}
	rank := func(p DiagramPin) int {
		switch {
		case p.Ground:
			return 5
		case p.Bridged:
			return 4
		}
		switch wire[strings.ToLower(p.Net)] {
		case WireLane:
			return 0
		case WireBetween:
			return 1
		}
		return 2
	}
	for i := range parts {
		if parts[i].Full || parts[i].Shape != "ic" {
			continue // a generated symbol keeps its number order, a transistor its terminals'
		}
		sort.SliceStable(parts[i].Pins, func(a, b int) bool { return rank(parts[i].Pins[a]) < rank(parts[i].Pins[b]) })
	}
}
