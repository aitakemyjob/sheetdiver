package core

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/aitakemyjob/sheetdiver/internal/calc"
	"github.com/aitakemyjob/sheetdiver/internal/library"
)

// Connections is a project's connection table and the block diagram
// drawn from it and the power rails.
type Connections struct {
	Nets        []library.Net   `json:"nets"`
	Blocks      []library.Block `json:"blocks"`
	Connections []ConnectionRow `json:"connections"`
	Diagram     Diagram         `json:"diagram"`
	// Pinouts is, for each part and block that isn't a passive, every
	// pin with what it's connected to: the reference table per part.
	Pinouts []Pinout `json:"pinouts"`
}

// Pinout is a part's (or block's) pins and what each is connected to.
type Pinout struct {
	Node  string      `json:"node"`
	Title string      `json:"title"`
	MPN   string      `json:"mpn,omitempty"`
	Pins  []PinoutRow `json:"pins"`
}

// PinoutRow is one pin: from the pin table when the part has one (all
// its pins, connected or not), else as connected. To lists the other
// pins and blocks on its net ("U2.VIN", "C1 10µF").
type PinoutRow struct {
	Number      string   `json:"number,omitempty"`
	Name        string   `json:"name,omitempty"`
	Type        string   `json:"type,omitempty"`
	Description string   `json:"description,omitempty"`
	Net         string   `json:"net,omitempty"`
	NetKind     string   `json:"netKind,omitempty"`
	To          []string `json:"to"`
}

// ConnectionRow is a connection with what it connects: the entry's
// designator (or part number) or the block's name, and the pin's name and
// type from the pin table.
type ConnectionRow struct {
	library.Connection
	Endpoint string `json:"endpoint"`
	PinName  string `json:"pinName,omitempty"`
	PinType  string `json:"pinType,omitempty"`
}

// Diagram is the project's block diagram: a node per BOM entry and
// block, power links along the rails (regulator to the parts it feeds)
// and signal links per bus or net (from the part with the most
// connections on it to the others). Ground isn't drawn.
type Diagram struct {
	Nodes []DiagramNode `json:"nodes"`
	Edges []DiagramEdge `json:"edges"`
}

// DiagramNode is a block on the diagram. ID is "part:<entry id>" or
// "block:<block id>"; X and Y are its saved position, if any.
type DiagramNode struct {
	ID       string   `json:"id"`
	Label    string   `json:"label"`
	MPN      string   `json:"mpn,omitempty"`
	Category string   `json:"category,omitempty"`
	Block    bool     `json:"block,omitempty"`
	X        *float64 `json:"x,omitempty"`
	Y        *float64 `json:"y,omitempty"`
}

// DiagramEdge links two nodes: Kind "power" (From feeds To on the rail in
// Label) or "signal" (Label is the bus or the nets).
type DiagramEdge struct {
	From  string   `json:"from"`
	To    string   `json:"to"`
	Label string   `json:"label"`
	Kind  string   `json:"kind"`
	Nets  []string `json:"nets"`
}

// connections loads a project's connection table, builds its diagram and
// returns the checks on it.
func (s *Service) connections(ctx context.Context, p library.Project, parts []library.ProjectPart, rails []RailStatus) (Connections, []ProjectCheck, error) {
	var out Connections
	var err error
	if out.Nets, err = s.Lib.Nets(ctx, p.ID); err != nil {
		return out, nil, err
	}
	if out.Blocks, err = s.Lib.Blocks(ctx, p.ID); err != nil {
		return out, nil, err
	}
	conns, err := s.Lib.Connections(ctx, p.ID)
	if err != nil {
		return out, nil, err
	}
	layout, err := s.Lib.Layout(ctx, p.ID)
	if err != nil {
		return out, nil, err
	}
	// Each net's kind as it's checked and drawn: a net named like a rail
	// is power, GND and the like are ground.
	kinds := netKinds(out.Nets, rails)
	for i := range out.Nets {
		out.Nets[i].Kind = kinds[strings.ToLower(out.Nets[i].Name)]
	}
	pins := s.entryPins(ctx, parts)
	out.Connections = make([]ConnectionRow, 0, len(conns))
	for _, c := range conns {
		row := ConnectionRow{Connection: c}
		if c.EntryID != 0 {
			if pp := entryByID(parts, c.EntryID); pp != nil {
				row.Endpoint = entryLabel(*pp)
			}
			if pin, ok := findPin(pins[c.EntryID], c.Pin); ok {
				row.PinName, row.PinType = pin.Name, pin.Type
			}
		} else {
			for _, b := range out.Blocks {
				if b.ID == c.BlockID {
					row.Endpoint = b.Name
				}
			}
		}
		out.Connections = append(out.Connections, row)
	}
	out.Diagram = diagram(parts, out.Blocks, out.Nets, out.Connections, rails, layout)
	out.Pinouts = pinouts(parts, out.Blocks, out.Nets, out.Connections, pins)
	return out, connectionChecks(parts, rails, out.Nets, out.Connections, pins), nil
}

// entryPins loads each BOM entry's pins (for its package, when the
// datasheet has several and the part's package is one of them).
func (s *Service) entryPins(ctx context.Context, parts []library.ProjectPart) map[int64][]library.Pin {
	out := map[int64][]library.Pin{}
	for _, pp := range parts {
		if pp.DatasheetID == 0 {
			continue
		}
		all, err := s.Lib.Pins(ctx, pp.DatasheetID, "")
		if err != nil || len(all) == 0 {
			continue
		}
		pins := all
		if part, err := s.Lib.PartByID(ctx, pp.PartID); err == nil && part.Package != "" {
			var mine []library.Pin
			for _, pin := range all {
				if pin.Package != "" && strings.Contains(strings.ToLower(part.Package), strings.ToLower(pin.Package)) {
					mine = append(mine, pin)
				}
			}
			if len(mine) > 0 {
				pins = mine
			}
		}
		out[pp.ID] = pins
	}
	return out
}

// findPin finds a pin by number, then by name (ignoring case).
func findPin(pins []library.Pin, ref string) (library.Pin, bool) {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return library.Pin{}, false
	}
	for _, p := range pins {
		if strings.EqualFold(p.Number, ref) {
			return p, true
		}
	}
	for _, p := range pins {
		if strings.EqualFold(p.Name, ref) {
			return p, true
		}
	}
	return library.Pin{}, false
}

// entryByID finds a BOM entry.
func entryByID(parts []library.ProjectPart, id int64) *library.ProjectPart {
	for i := range parts {
		if parts[i].ID == id {
			return &parts[i]
		}
	}
	return nil
}

// entryLabel names a BOM entry: its designator, else its part number.
func entryLabel(pp library.ProjectPart) string {
	if pp.Designator != "" {
		return pp.Designator
	}
	return pp.MPN
}

// Pin roles by name, for pin tables without a type column.
var (
	groundPinRe = regexp.MustCompile(`(?i)^(A|D|P|S)?(GND|VSS)\w*$`)
	supplyPinRe = regexp.MustCompile(`(?i)^(A|D)?V(DD|CC|IN|BAT|IO|S)\w*$|^VDDIO\w*$`)
)

// groundPin and supplyPin say what a pin is from its type, else its name.
func groundPin(p library.Pin) bool {
	t := strings.ToUpper(p.Type)
	return t == "G" || t == "GND" || groundPinRe.MatchString(p.Name)
}

// supplyPin reports whether a pin takes the part's supply.
func supplyPin(p library.Pin) bool {
	t := strings.ToUpper(p.Type)
	if t == "P" || t == "PWR" {
		return !groundPinRe.MatchString(p.Name)
	}
	return supplyPinRe.MatchString(p.Name)
}

// outputPin reports whether a pin drives its net.
func outputPin(p library.Pin) bool {
	switch strings.ToUpper(p.Type) {
	case "O", "DO", "AO":
		return true
	}
	return false
}

// netKinds says which nets are ground and which power (a net named like
// a rail is that rail).
func netKinds(nets []library.Net, rails []RailStatus) map[string]string {
	out := map[string]string{}
	for _, n := range nets {
		kind := n.Kind
		if kind == library.NetSignal && groundPinRe.MatchString(n.Name) {
			kind = library.NetGround
		}
		out[strings.ToLower(n.Name)] = kind
	}
	for _, r := range rails {
		if out[strings.ToLower(r.Name)] != library.NetGround {
			out[strings.ToLower(r.Name)] = library.NetPower
		}
	}
	return out
}

// diagram builds the block diagram.
func diagram(parts []library.ProjectPart, blocks []library.Block, nets []library.Net, rows []ConnectionRow, rails []RailStatus, layout []library.Position) Diagram {
	d := Diagram{Nodes: []DiagramNode{}, Edges: []DiagramEdge{}}
	pos := map[string]library.Position{}
	for _, p := range layout {
		pos[p.Node] = p
	}
	addNode := func(n DiagramNode) {
		if p, ok := pos[n.ID]; ok {
			x, y := p.X, p.Y
			n.X, n.Y = &x, &y
		}
		d.Nodes = append(d.Nodes, n)
	}
	// Passives aren't blocks of their own on a block diagram.
	skip := map[string]bool{}
	for _, pp := range parts {
		if library.TwoTerminal(pp.Category) {
			skip[library.PartNode(pp.ID)] = true
			continue
		}
		addNode(DiagramNode{ID: library.PartNode(pp.ID), Label: entryLabel(pp), MPN: pp.MPN, Category: pp.Category})
	}
	for _, b := range blocks {
		if library.TwoTerminal(b.Kind) {
			skip[library.BlockNode(b.ID)] = true
			continue
		}
		addNode(DiagramNode{ID: library.BlockNode(b.ID), Label: b.Name, Category: b.Kind, Block: true})
	}
	nodeOf := func(r ConnectionRow) string {
		if r.EntryID != 0 {
			return library.PartNode(r.EntryID)
		}
		return library.BlockNode(r.BlockID)
	}
	kinds := netKinds(nets, rails)
	onNet := map[string][]string{} // net (lower case) → nodes, in order
	pinsOn := map[string]map[string]int{}
	for _, r := range rows {
		k := strings.ToLower(r.Net)
		n := nodeOf(r)
		if skip[n] {
			continue
		}
		if !contains(onNet[k], n) {
			onNet[k] = append(onNet[k], n)
		}
		if pinsOn[k] == nil {
			pinsOn[k] = map[string]int{}
		}
		pinsOn[k][n]++
	}
	type edgeKey struct{ from, to, kind string }
	edges := map[edgeKey]*DiagramEdge{}
	var order []edgeKey
	link := func(from, to, label, kind, net string) {
		if from == to {
			return
		}
		k := edgeKey{from, to, kind}
		if _, ok := edges[edgeKey{to, from, kind}]; ok && kind == "signal" {
			k = edgeKey{to, from, kind} // signals have no direction: one link
		}
		e := edges[k]
		if e == nil {
			e = &DiagramEdge{From: k.from, To: k.to, Kind: kind, Nets: []string{}}
			edges[k] = e
			order = append(order, k)
		}
		if !contains(e.Nets, net) {
			e.Nets = append(e.Nets, net)
		}
		if e.Label == "" {
			e.Label = label
		} else if !contains(strings.Split(e.Label, ", "), label) {
			e.Label += ", " + label
		}
	}
	// Power: along each rail, from the part (or block) that makes it to
	// what's on it.
	railNames := map[string]string{}
	for _, r := range rails {
		railNames[strings.ToLower(r.Name)] = r.Name
	}
	for _, n := range nets {
		if kinds[strings.ToLower(n.Name)] == library.NetPower {
			if _, ok := railNames[strings.ToLower(n.Name)]; !ok {
				railNames[strings.ToLower(n.Name)] = n.Name
			}
		}
	}
	names := make([]string, 0, len(railNames))
	for k := range railNames {
		names = append(names, k)
	}
	sort.Strings(names)
	for _, k := range names {
		name := railNames[k]
		var src, users []string
		for _, pp := range parts {
			node := library.PartNode(pp.ID)
			if strings.EqualFold(pp.OutputRail, name) && !strings.EqualFold(pp.SupplyRail, name) {
				src = append(src, node)
			} else if strings.EqualFold(pp.SupplyRail, name) {
				users = append(users, node)
			}
		}
		for _, node := range onNet[k] {
			if !contains(src, node) && !contains(users, node) {
				if strings.HasPrefix(node, "block:") && len(src) == 0 {
					src = append(src, node) // a battery or connector feeding the rail
				} else {
					users = append(users, node)
				}
			}
		}
		for _, f := range src {
			for _, t := range users {
				link(f, t, name, "power", name)
			}
		}
	}
	// Signals: per bus (or net), from the node with the most pins on it.
	groups := map[string][]library.Net{}
	var groupOrder []string
	for _, n := range nets {
		if kinds[strings.ToLower(n.Name)] != library.NetSignal || len(onNet[strings.ToLower(n.Name)]) < 2 {
			continue
		}
		g := n.Bus
		if g == "" {
			g = n.Name
		}
		if _, ok := groups[g]; !ok {
			groupOrder = append(groupOrder, g)
		}
		groups[g] = append(groups[g], n)
	}
	for _, g := range groupOrder {
		count := map[string]int{}
		var members []string
		for _, n := range groups[g] {
			for _, node := range onNet[strings.ToLower(n.Name)] {
				if !contains(members, node) {
					members = append(members, node)
				}
				count[node] += pinsOn[strings.ToLower(n.Name)][node]
			}
		}
		hub := members[0]
		for _, m := range members {
			if count[m] > count[hub] {
				hub = m
			}
		}
		for _, n := range groups[g] {
			for _, node := range onNet[strings.ToLower(n.Name)] {
				if node == hub {
					continue
				}
				label := g
				if n.Bus == "" {
					label = n.Name
				}
				link(hub, node, label, "signal", n.Name)
			}
		}
	}
	for _, k := range order {
		d.Edges = append(d.Edges, *edges[k])
	}
	return d
}

// connectionChecks checks the connection table against the parts' pin
// tables and the rails.
func connectionChecks(parts []library.ProjectPart, rails []RailStatus, nets []library.Net, rows []ConnectionRow, pins map[int64][]library.Pin) []ProjectCheck {
	out := []ProjectCheck{}
	add := func(level, part string, format string, a ...any) {
		out = append(out, ProjectCheck{Level: level, Part: part, Area: "connections", Message: fmt.Sprintf(format, a...)})
	}
	kinds := netKinds(nets, rails)
	connected := map[int64]map[string]string{} // entry → pin number → net
	byNet := map[string][]ConnectionRow{}
	for _, r := range rows {
		byNet[strings.ToLower(r.Net)] = append(byNet[strings.ToLower(r.Net)], r)
		if r.EntryID == 0 {
			continue
		}
		pp := entryByID(parts, r.EntryID)
		if pp == nil {
			continue
		}
		who := entryLabel(*pp)
		list := pins[r.EntryID]
		pin, ok := findPin(list, r.Pin)
		if len(list) > 0 && r.Pin != "" && !ok {
			add(calc.Warn, who, "%s has no pin “%s” in its datasheet's pin table (%s)", who, r.Pin, r.Net)
			continue
		}
		if !ok {
			continue
		}
		if connected[r.EntryID] == nil {
			connected[r.EntryID] = map[string]string{}
		}
		connected[r.EntryID][pin.Number] = r.Net
		kind := kinds[strings.ToLower(r.Net)]
		switch {
		case groundPin(pin) && kind != library.NetGround:
			add(calc.Error, who, "%s pin %s (%s) is a ground pin but is on %s, which isn't ground", who, pin.Number, pin.Name, r.Net)
		case supplyPin(pin) && kind == library.NetGround:
			add(calc.Error, who, "%s pin %s (%s) is a supply pin but is on ground (%s)", who, pin.Number, pin.Name, r.Net)
		case supplyPin(pin) && kind == library.NetPower && pp.SupplyRail != "" && !strings.EqualFold(pp.SupplyRail, r.Net) && !strings.EqualFold(pp.OutputRail, r.Net):
			add(calc.Warn, who, "%s pin %s (%s) is on %s, but the BOM has %s powered from %s", who, pin.Number, pin.Name, r.Net, who, pp.SupplyRail)
		}
	}
	// Supply and ground pins left open, on parts whose pins are being
	// connected.
	for _, pp := range parts {
		got := connected[pp.ID]
		if len(got) == 0 {
			continue
		}
		var open []string
		for _, pin := range pins[pp.ID] {
			if _, ok := got[pin.Number]; !ok && (groundPin(pin) || supplyPin(pin)) {
				open = append(open, fmt.Sprintf("%s (%s)", pin.Name, pin.Number))
			}
		}
		if len(open) > 0 {
			add(calc.Warn, entryLabel(pp), "%s: supply or ground pins not connected: %s", entryLabel(pp), strings.Join(open, ", "))
		}
	}
	for _, n := range nets {
		rs := byNet[strings.ToLower(n.Name)]
		kind := kinds[strings.ToLower(n.Name)]
		if kind != library.NetSignal {
			continue
		}
		ends := map[string]bool{}
		var drivers []string
		for _, r := range rs {
			ends[r.Endpoint] = true
			if r.EntryID != 0 {
				if pin, ok := findPin(pins[r.EntryID], r.Pin); ok && outputPin(pin) {
					drivers = append(drivers, r.Endpoint+"."+pin.Name)
				}
			}
		}
		switch {
		case len(rs) == 1:
			add(calc.Warn, rs[0].Endpoint, "%s only goes to %s", n.Name, rs[0].Endpoint)
		case len(rs) > 1 && len(ends) == 1:
			add(calc.Warn, rs[0].Endpoint, "%s only connects pins of %s", n.Name, rs[0].Endpoint)
		}
		if len(drivers) > 1 {
			add(calc.Warn, "", "%s has more than one output on it: %s", n.Name, strings.Join(drivers, ", "))
		}
	}
	return out
}

// ConnectionsChange edits a project's connection table and block
// diagram. Blocks are applied first, then nets, connections and
// positions.
type ConnectionsChange struct {
	Blocks      []BlockChange    `json:"blocks,omitempty"`
	Nets        []NetChange      `json:"nets,omitempty"`
	Connect     []ConnectChange  `json:"connect,omitempty"`
	Positions   []PositionChange `json:"positions,omitempty"`
	ResetLayout bool             `json:"resetLayout,omitempty"`
}

// BlockChange adds, updates or removes an off-BOM block.
type BlockChange struct {
	Name   string `json:"name"`
	Kind   string `json:"kind,omitempty"`
	Value  string `json:"value,omitempty"`
	Notes  string `json:"notes,omitempty"`
	Remove bool   `json:"remove,omitempty"`
}

// NetChange sets a net's kind, bus, I/O voltage or notes (unset fields
// are kept), or removes it with its connections.
type NetChange struct {
	Name    string   `json:"name"`
	Kind    string   `json:"kind,omitempty"`
	Bus     *string  `json:"bus,omitempty"`
	Voltage *float64 `json:"voltage,omitempty"`
	Notes   *string  `json:"notes,omitempty"`
	Remove  bool     `json:"remove,omitempty"`
}

// ConnectChange puts a pin on a net, or takes it off. Part is a BOM
// designator, part number or block name; Pin a pin number or name ("U1.SDA"
// in Part works too).
type ConnectChange struct {
	Net    string `json:"net"`
	Part   string `json:"part"`
	Pin    string `json:"pin,omitempty"`
	Notes  string `json:"notes,omitempty"`
	Remove bool   `json:"remove,omitempty"`
}

// PositionChange places a node on the block diagram: a designator, part
// number or block name.
type PositionChange struct {
	Node string  `json:"node"`
	X    float64 `json:"x"`
	Y    float64 `json:"y"`
}

// errEndpoint means a connection names nothing in the project.
var errEndpoint = errors.New("not in the BOM or the blocks")

// ChangeConnections applies a change and returns what couldn't be done
// (an unknown part, a pin its pin table doesn't have), one line each; the
// rest is applied.
func (s *Service) ChangeConnections(ctx context.Context, project string, ch ConnectionsChange, source string) ([]string, error) {
	p, err := s.Lib.Project(ctx, project)
	if err != nil {
		return nil, err
	}
	problems := []string{}
	for _, b := range ch.Blocks {
		if b.Remove {
			err = s.Lib.DeleteBlock(ctx, p.ID, b.Name)
		} else {
			_, err = s.Lib.SetBlock(ctx, library.Block{ProjectID: p.ID, Name: b.Name, Kind: b.Kind, Value: b.Value, Notes: b.Notes, Source: source})
		}
		if err != nil {
			problems = append(problems, fmt.Sprintf("block %s: %v", b.Name, err))
		}
	}
	nets, err := s.Lib.Nets(ctx, p.ID)
	if err != nil {
		return nil, err
	}
	for _, nc := range ch.Nets {
		if nc.Remove {
			if err := s.Lib.DeleteNet(ctx, p.ID, nc.Name); err != nil {
				problems = append(problems, fmt.Sprintf("net %s: %v", nc.Name, err))
			}
			continue
		}
		n := library.Net{ProjectID: p.ID, Name: nc.Name, Source: source}
		for _, old := range nets {
			if strings.EqualFold(old.Name, strings.TrimSpace(nc.Name)) {
				n.Kind, n.Bus, n.Voltage, n.Notes = old.Kind, old.Bus, old.Voltage, old.Notes
			}
		}
		if nc.Kind != "" {
			n.Kind = nc.Kind
		}
		if nc.Bus != nil {
			n.Bus = *nc.Bus
		}
		if nc.Voltage != nil {
			n.Voltage = nc.Voltage
		}
		if nc.Notes != nil {
			n.Notes = *nc.Notes
		}
		if _, err := s.Lib.SetNet(ctx, n); err != nil {
			problems = append(problems, fmt.Sprintf("net %s: %v", nc.Name, err))
		}
	}
	parts, err := s.Lib.ProjectParts(ctx, p.ID)
	if err != nil {
		return nil, err
	}
	blocks, err := s.Lib.Blocks(ctx, p.ID)
	if err != nil {
		return nil, err
	}
	pins := s.entryPins(ctx, parts)
	for _, cc := range ch.Connect {
		ref, pinRef := strings.TrimSpace(cc.Part), strings.TrimSpace(cc.Pin)
		entry, block := endpoint(parts, blocks, ref)
		if entry == 0 && block == 0 && pinRef == "" {
			if i := strings.LastIndex(ref, "."); i > 0 {
				if e, b := endpoint(parts, blocks, ref[:i]); e != 0 || b != 0 {
					entry, block, ref, pinRef = e, b, ref[:i], ref[i+1:]
				}
			}
		}
		if entry == 0 && block == 0 {
			problems = append(problems, fmt.Sprintf("%s: %v", ref, errEndpoint))
			continue
		}
		pin := pinRef
		if entry != 0 && pinRef != "" {
			if found, ok := findPin(pins[entry], pinRef); ok {
				pin = found.Number
			} else if len(pins[entry]) > 0 {
				problems = append(problems, fmt.Sprintf("%s has no pin “%s” in its pin table (saved as given)", ref, pinRef))
			}
		}
		c := library.Connection{Net: cc.Net, EntryID: entry, BlockID: block, Pin: pin, Notes: cc.Notes, Source: source}
		if cc.Remove {
			err = s.Lib.Disconnect(ctx, p.ID, c)
		} else {
			_, err = s.Lib.Connect(ctx, p.ID, c)
		}
		if err != nil {
			problems = append(problems, fmt.Sprintf("%s %s on %s: %v", ref, pinRef, cc.Net, err))
		}
	}
	if ch.ResetLayout {
		if err := s.Lib.ClearLayout(ctx, p.ID); err != nil {
			return problems, err
		}
	}
	for _, pc := range ch.Positions {
		node := pc.Node
		if !strings.HasPrefix(node, "part:") && !strings.HasPrefix(node, "block:") {
			entry, block := endpoint(parts, blocks, node)
			switch {
			case entry != 0:
				node = library.PartNode(entry)
			case block != 0:
				node = library.BlockNode(block)
			default:
				problems = append(problems, fmt.Sprintf("%s: %v", pc.Node, errEndpoint))
				continue
			}
		}
		if err := s.Lib.SetPosition(ctx, p.ID, library.Position{Node: node, X: pc.X, Y: pc.Y}); err != nil {
			return problems, err
		}
	}
	s.ProjectChanged(p.Name, "connections")
	return problems, nil
}

// endpoint finds what a connection names: a BOM entry by designator, or
// by part number when only one entry has it, else a block by name.
func endpoint(parts []library.ProjectPart, blocks []library.Block, ref string) (entry, block int64) {
	for _, pp := range parts {
		if pp.Designator != "" && strings.EqualFold(pp.Designator, ref) {
			return pp.ID, 0
		}
	}
	var found []int64
	for _, pp := range parts {
		if strings.EqualFold(pp.MPN, ref) || (pp.Variant != "" && strings.EqualFold(pp.Variant, ref)) {
			found = append(found, pp.ID)
		}
	}
	if len(found) == 1 {
		return found[0], 0
	}
	for _, b := range blocks {
		if strings.EqualFold(b.Name, ref) {
			return 0, b.ID
		}
	}
	return 0, 0
}

// pinouts builds the reference table per part: every pin of each part
// that isn't a passive (from its pin table, in order), and each block's
// connected pins, with what else is on the pin's net.
func pinouts(parts []library.ProjectPart, blocks []library.Block, nets []library.Net, rows []ConnectionRow, pins map[int64][]library.Pin) []Pinout {
	kind := map[string]string{}
	for _, n := range nets {
		kind[strings.ToLower(n.Name)] = n.Kind
	}
	value := map[int64]string{}
	for _, b := range blocks {
		value[b.ID] = b.Value
	}
	// What a row is, seen from another pin: "U2.VIN", "C1 10µF".
	name := func(r ConnectionRow) string {
		if r.BlockID != 0 {
			if r.Pin != "" {
				return r.Endpoint + "." + r.Pin
			}
			return strings.TrimSpace(r.Endpoint + " " + value[r.BlockID])
		}
		return r.Endpoint + "." + firstNonEmpty(r.PinName, r.Pin)
	}
	to := func(self ConnectionRow) []string {
		out := []string{}
		if kind[strings.ToLower(self.Net)] == library.NetGround {
			return out // everything on ground would be noise
		}
		for _, r := range rows {
			if r.ID != self.ID && strings.EqualFold(r.Net, self.Net) {
				out = append(out, name(r))
			}
		}
		return out
	}
	row := func(r ConnectionRow) PinoutRow {
		return PinoutRow{Net: r.Net, NetKind: kind[strings.ToLower(r.Net)], To: to(r)}
	}
	out := []Pinout{}
	for _, pp := range parts {
		if library.TwoTerminal(pp.Category) {
			continue
		}
		mine := []ConnectionRow{}
		for _, r := range rows {
			if r.EntryID == pp.ID {
				mine = append(mine, r)
			}
		}
		po := Pinout{Node: library.PartNode(pp.ID), Title: strings.TrimSpace(pp.Designator + " " + pp.MPN), MPN: pp.MPN, Pins: []PinoutRow{}}
		used := map[int64]bool{}
		for _, pin := range pins[pp.ID] {
			pr := PinoutRow{Number: pin.Number, Name: pin.Name, Type: pin.Type, Description: pin.Description, To: []string{}}
			for _, r := range mine {
				if !used[r.ID] && r.Pin != "" && strings.EqualFold(r.Pin, pin.Number) {
					x := row(r)
					pr.Net, pr.NetKind, pr.To = x.Net, x.NetKind, x.To
					used[r.ID] = true
				}
			}
			po.Pins = append(po.Pins, pr)
		}
		// Connections the pin table doesn't have (or a part without one).
		for _, r := range mine {
			if !used[r.ID] {
				x := row(r)
				x.Number, x.Name, x.Type = r.Pin, r.PinName, r.PinType
				po.Pins = append(po.Pins, x)
			}
		}
		if len(po.Pins) > 0 {
			out = append(out, po)
		}
	}
	for _, b := range blocks {
		if library.TwoTerminal(b.Kind) {
			continue
		}
		po := Pinout{Node: library.BlockNode(b.ID), Title: b.Name, Pins: []PinoutRow{}}
		for _, r := range rows {
			if r.BlockID == b.ID {
				x := row(r)
				x.Name = r.Pin
				po.Pins = append(po.Pins, x)
			}
		}
		if len(po.Pins) > 0 {
			out = append(out, po)
		}
	}
	return out
}
