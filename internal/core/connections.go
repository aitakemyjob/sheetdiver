package core

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"sort"
	"strings"

	"github.com/aitakemyjob/sheetdiver/internal/library"
)

// Connections is a project's connection table and the block diagram
// drawn from it and the power rails.
type Connections struct {
	Nets        []library.Net   `json:"nets"`
	Blocks      []library.Block `json:"blocks"`
	Connections []ConnectionRow `json:"connections"`
	// Groups are the project's functional groups; Assignments put parts
	// and blocks in them and say what they support.
	Groups      []library.Group `json:"groups"`
	Assignments []AssignmentRow `json:"assignments"`
	Diagram     Diagram         `json:"diagram"`
	// Pinouts is, for each part and block that isn't a passive, every
	// pin with what it's connected to: the reference table per part.
	Pinouts []Pinout `json:"pinouts"`
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

// AssignmentRow is an assignment with the names of what it's about: the
// part or block (Endpoint) and the one it supports (SupportsEndpoint).
type AssignmentRow struct {
	library.Assignment
	Endpoint         string `json:"endpoint"`
	SupportsEndpoint string `json:"supportsEndpoint,omitempty"`
}

// assignmentRows names each assignment's nodes.
func assignmentRows(parts []library.ProjectPart, blocks []library.Block, as []library.Assignment) []AssignmentRow {
	out := make([]AssignmentRow, 0, len(as))
	for _, a := range as {
		out = append(out, AssignmentRow{Assignment: a, Endpoint: nodeLabel(parts, blocks, a.Node), SupportsEndpoint: nodeLabel(parts, blocks, a.Supports)})
	}
	return out
}

// nodeLabel names a node: a BOM entry's designator (or part number), one
// of its designators, or a block's name.
func nodeLabel(parts []library.ProjectPart, blocks []library.Block, node string) string {
	for _, pp := range parts {
		if node == library.PartNode(pp.ID) {
			return entryLabel(pp)
		}
		if strings.HasPrefix(node, library.PartNode(pp.ID)+"/") {
			return strings.TrimPrefix(node, library.PartNode(pp.ID)+"/")
		}
	}
	for _, b := range blocks {
		if node == library.BlockNode(b.ID) {
			return b.Name
		}
	}
	return node
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
				row.Endpoint = firstNonEmpty(c.Ref, entryLabel(*pp))
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
	if out.Groups, err = s.Lib.Groups(ctx, p.ID); err != nil {
		return out, nil, err
	}
	assigns, err := s.Lib.Assignments(ctx, p.ID)
	if err != nil {
		return out, nil, err
	}
	out.Assignments = assignmentRows(parts, out.Blocks, assigns)
	out.Diagram = diagram(diagramInput{parts: parts, blocks: out.Blocks, nets: out.Nets, rows: out.Connections, layout: layout, groups: out.Groups, assigns: assigns, rails: plainRails(rails), pins: pins, descriptions: s.entryDescriptions(ctx, parts)})
	out.Pinouts = pinouts(parts, out.Blocks, out.Nets, out.Connections, pins)
	return out, connectionChecks(parts, rails, out.Nets, out.Connections, pins), nil
}

// entryDescriptions loads each BOM entry's part description, by entry
// id: a passive's value comes from it when its role doesn't say.
func (s *Service) entryDescriptions(ctx context.Context, parts []library.ProjectPart) map[int64]string {
	out := map[int64]string{}
	for _, pp := range parts {
		if !twoTerminal(pp.Category, false) {
			continue
		}
		if part, err := s.Lib.PartByID(ctx, pp.PartID); err == nil {
			out[pp.ID] = part.Description
		}
	}
	return out
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
		part, _ := s.Lib.PartByID(ctx, pp.PartID)
		out[pp.ID] = packagePins(all, part)
	}
	return out
}

// packagePins are a part's pins when its datasheet lists several
// packages: those of the package named like the part's package, or
// labelled with its part number ("TPS2121 VQFN-HR" for the TPS2121, when
// one table serves the TPS2120 and the TPS2121); else the first package
// listed, not every package's pins mixed. A table of one package is all
// the part's.
func packagePins(all []library.Pin, part library.Part) []library.Pin {
	var pkgs []string
	for _, pin := range all {
		if pin.Package != "" && !slices.Contains(pkgs, pin.Package) {
			pkgs = append(pkgs, pin.Package)
		}
	}
	if len(pkgs) < 2 {
		return all
	}
	mpn, own := strings.ToUpper(part.MPN), strings.ToUpper(part.Package)
	choose := pkgs[0]
	for _, pkg := range pkgs {
		label := strings.ToUpper(pkg)
		named := own != "" && (strings.Contains(own, label) || slices.ContainsFunc(strings.Fields(label), func(w string) bool { return len(w) >= 3 && strings.Contains(own, w) }))
		mine := mpn != "" && slices.ContainsFunc(strings.Fields(label), func(w string) bool { return w == mpn || strings.HasPrefix(mpn, w) && len(w) >= 6 })
		if named || mine {
			choose = pkg
			break
		}
	}
	var out []library.Pin
	for _, pin := range all {
		if pin.Package == choose || pin.Package == "" {
			out = append(out, pin)
		}
	}
	return out
}

// pinKey is how a connection names a pin: its number, or its name when
// the pin table gives it none (a thermal pad).
func pinKey(p library.Pin) string {
	return firstNonEmpty(p.Number, p.Name)
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

// ConnectionsChange edits a project's connection table and block
// diagram. Blocks are applied first, then groups, nets, connections,
// assignments and positions.
type ConnectionsChange struct {
	Blocks      []BlockChange    `json:"blocks,omitempty"`
	Groups      []GroupChange    `json:"groups,omitempty"`
	Nets        []NetChange      `json:"nets,omitempty"`
	Connect     []ConnectChange  `json:"connect,omitempty"`
	Assign      []AssignChange   `json:"assign,omitempty"`
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

// GroupChange adds or updates a functional group (unset fields are
// kept), or removes it, leaving its parts ungrouped.
type GroupChange struct {
	Name        string  `json:"name"`
	Description *string `json:"description,omitempty"`
	Notes       *string `json:"notes,omitempty"`
	// Below names the group to draw it under; "" puts it back in a column
	// of its own.
	Below  *string `json:"below,omitempty"`
	Remove bool    `json:"remove,omitempty"`
}

// AssignChange puts a part or block (a designator, part number or block
// name) in a group, says which part it supports and/or names the
// subgroup it's wired up in with the other parts of that name; unset
// fields are kept, "" clears one. A group that doesn't exist is created.
type AssignChange struct {
	Part     string  `json:"part"`
	Group    *string `json:"group,omitempty"`
	Supports *string `json:"supports,omitempty"`
	Cluster  *string `json:"cluster,omitempty"`
	// Column and Row ask for a place in its group: the column from the
	// left and the place in it from the top, from 0; negative clears.
	Column *int `json:"column,omitempty"`
	Row    *int `json:"row,omitempty"`
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
	// What the app drew is out of date until it draws again.
	s.forgetDrawn(p.Name)
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
	problems = append(problems, s.changeGroups(ctx, p.ID, ch.Groups, source)...)
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
		to := endpoint(parts, blocks, ref)
		if !to.found() && pinRef == "" {
			if i := strings.LastIndex(ref, "."); i > 0 {
				if t := endpoint(parts, blocks, ref[:i]); t.found() {
					to, ref, pinRef = t, ref[:i], ref[i+1:]
				}
			}
		}
		entry, block := to.entry, to.block
		if !to.found() {
			problems = append(problems, fmt.Sprintf("%s: %v", ref, errEndpoint))
			continue
		}
		pin := pinRef
		if entry != 0 && pinRef != "" {
			if found, ok := findPin(pins[entry], pinRef); ok {
				pin = pinKey(found)
			} else if len(pins[entry]) > 0 {
				problems = append(problems, fmt.Sprintf("%s has no pin “%s” in its pin table (saved as given)", ref, pinRef))
			}
		}
		c := library.Connection{Net: cc.Net, EntryID: entry, BlockID: block, Ref: to.ref, Pin: pin, Notes: cc.Notes, Source: source}
		if cc.Remove {
			err = s.Lib.Disconnect(ctx, p.ID, c)
		} else {
			_, err = s.Lib.Connect(ctx, p.ID, c)
		}
		if err != nil {
			problems = append(problems, fmt.Sprintf("%s %s on %s: %v", ref, pinRef, cc.Net, err))
		}
	}
	more, err := s.assign(ctx, p.ID, parts, blocks, ch.Assign, source)
	if err != nil {
		return problems, err
	}
	problems = append(problems, more...)
	if ch.ResetLayout {
		if err := s.Lib.ClearLayout(ctx, p.ID); err != nil {
			return problems, err
		}
	}
	for _, pc := range ch.Positions {
		node := pc.Node
		if !strings.HasPrefix(node, "part:") && !strings.HasPrefix(node, "block:") {
			var ok bool
			if node, ok = nodeOfName(parts, blocks, node); !ok {
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

// changeGroups applies group changes, returning what couldn't be done.
func (s *Service) changeGroups(ctx context.Context, projectID int64, changes []GroupChange, source string) []string {
	var problems []string
	if len(changes) == 0 {
		return problems
	}
	groups, err := s.Lib.Groups(ctx, projectID)
	if err != nil {
		return []string{err.Error()}
	}
	for _, gc := range changes {
		if gc.Remove {
			if err := s.Lib.DeleteGroup(ctx, projectID, gc.Name); err != nil {
				problems = append(problems, fmt.Sprintf("group %s: %v", gc.Name, err))
			}
			continue
		}
		g := library.Group{ProjectID: projectID, Name: gc.Name, Source: source}
		for _, old := range groups {
			if strings.EqualFold(old.Name, strings.TrimSpace(gc.Name)) {
				g.Description, g.Notes, g.Below = old.Description, old.Notes, old.Below
			}
		}
		if gc.Description != nil {
			g.Description = *gc.Description
		}
		if gc.Notes != nil {
			g.Notes = *gc.Notes
		}
		if gc.Below != nil {
			g.Below = strings.TrimSpace(*gc.Below)
		}
		if _, err := s.Lib.SetGroup(ctx, g); err != nil {
			problems = append(problems, fmt.Sprintf("group %s: %v", gc.Name, err))
		}
	}
	// Where each group is drawn: one changed here to go below a group that
	// doesn't exist, or into a loop of groups below each other, starts a
	// column of its own (the others in the loop keep theirs).
	changed := map[string]bool{}
	for _, gc := range changes {
		changed[strings.ToLower(strings.TrimSpace(gc.Name))] = gc.Below != nil
	}
	if after, err := s.Lib.Groups(ctx, projectID); err == nil {
		for name, why := range placementProblems(after) {
			if !changed[strings.ToLower(name)] {
				continue
			}
			for _, g := range after {
				if g.Name == name {
					g.Below = ""
					_, _ = s.Lib.SetGroup(ctx, g)
				}
			}
			problems = append(problems, fmt.Sprintf("group %s: %s; it's drawn in a column of its own", name, why))
		}
		sort.Strings(problems)
	}
	return problems
}

// placementProblems finds the groups that can't be drawn below the group
// they name: it doesn't exist, or following "below" from it comes back to
// it. Each is reported once, by name.
func placementProblems(groups []library.Group) map[string]string {
	below := map[string]string{}
	names := map[string]string{}
	for _, g := range groups {
		names[strings.ToLower(g.Name)] = g.Name
		below[strings.ToLower(g.Name)] = strings.ToLower(g.Below)
	}
	out := map[string]string{}
	for _, g := range groups {
		k := strings.ToLower(g.Name)
		to := below[k]
		if to == "" {
			continue
		}
		if _, ok := names[to]; !ok {
			out[g.Name] = fmt.Sprintf("below %s, which isn't a group", g.Below)
			continue
		}
		// In a loop when following "below" comes back to it; one below a
		// loop it isn't part of is drawn in the column the loop gets.
		seen := map[string]bool{}
		for at := to; at != "" && !seen[at]; at = below[at] {
			if at == k {
				out[g.Name] = fmt.Sprintf("below %s, which is (through others) below it", g.Below)
				break
			}
			seen[at] = true
		}
	}
	return out
}

// assign applies assignment changes, returning what couldn't be done.
func (s *Service) assign(ctx context.Context, projectID int64, parts []library.ProjectPart, blocks []library.Block, changes []AssignChange, source string) ([]string, error) {
	var problems []string
	if len(changes) == 0 {
		return problems, nil
	}
	existing, err := s.Lib.Assignments(ctx, projectID)
	if err != nil {
		return nil, err
	}
	conns, err := s.Lib.Connections(ctx, projectID)
	if err != nil {
		return nil, err
	}
	rails, err := s.Lib.Rails(ctx, projectID)
	if err != nil {
		return nil, err
	}
	netsOf := func(node string) []string {
		var out []string
		for _, c := range conns {
			if connectionNode(c) == node && !contains(out, c.Net) {
				out = append(out, c.Net)
			}
		}
		// A part is on the rail it makes, wired to it or not: its output
		// rail, or one that names it as the source.
		for _, pp := range parts {
			if node == library.PartNode(pp.ID) && pp.OutputRail != "" {
				out = append(out, pp.OutputRail)
			}
		}
		label := nodeLabel(parts, blocks, node)
		for _, r := range rails {
			if r.Source != "" && (strings.EqualFold(r.Source, label) || strings.EqualFold(r.Source, strings.Fields(label + " ")[0])) {
				out = append(out, r.Name)
			}
		}
		return out
	}
	for _, ac := range changes {
		node, ok := nodeOfName(parts, blocks, ac.Part)
		if !ok {
			problems = append(problems, fmt.Sprintf("%s: %v", ac.Part, errEndpoint))
			continue
		}
		a := library.Assignment{Node: node, Source: source}
		for _, old := range existing {
			if old.Node == node {
				a.Group, a.Supports, a.Cluster, a.Column, a.Row = old.Group, old.Supports, old.Cluster, old.Column, old.Row
			}
		}
		if ac.Group != nil {
			a.Group = *ac.Group
		}
		if ac.Cluster != nil {
			a.Cluster = strings.TrimSpace(*ac.Cluster)
		}
		if ac.Column != nil {
			a.Column = ac.Column
		}
		if ac.Row != nil {
			a.Row = ac.Row
		}
		// A support that's refused keeps the one it had; the rest of the
		// change (its group, subgroup, cell) is still saved (#82).
		if ac.Supports != nil {
			to, problem := supportOf(parts, blocks, node, ac.Part, strings.TrimSpace(*ac.Supports), netsOf)
			if problem != "" {
				problems = append(problems, problem)
			}
			if to != "" || strings.TrimSpace(*ac.Supports) == "" {
				a.Supports = to
			}
		}
		if err := s.Lib.Assign(ctx, projectID, a); err != nil {
			problems = append(problems, fmt.Sprintf("%s: %v", ac.Part, err))
			continue
		}
		existing = slices.DeleteFunc(existing, func(x library.Assignment) bool { return x.Node == node })
		existing = append(existing, a)
	}
	return problems, nil
}

// supportOf resolves what a part is said to support ("C5 supports U3"):
// the node it names, or "" with the reason it's refused. A support said
// but not seen (the part isn't on the passive's nets) is applied, with a
// note.
func supportOf(parts []library.ProjectPart, blocks []library.Block, node, part, name string, netsOf func(string) []string) (string, string) {
	if name == "" {
		return "", ""
	}
	to, ok := nodeOfName(parts, blocks, name)
	if !ok {
		return "", fmt.Sprintf("%s supports %s: %v", part, name, errEndpoint)
	}
	// The passive is the one that supports: "C5 supports U3".
	if !passiveNode(parts, blocks, node) && passiveNode(parts, blocks, to) {
		return "", fmt.Sprintf("%s supports %s: the other way round? A passive supports the part it serves: part %s, supports %s", part, name, name, part)
	}
	if passiveNode(parts, blocks, node) && passiveNode(parts, blocks, to) {
		return "", fmt.Sprintf("%s supports %s: a passive supports the IC or connector it serves, not another passive", part, name)
	}
	// Said, but not seen: still drawn by it when a net reaches it through
	// a passive or is a rail it's the source of; the agent is told either
	// way.
	mine, theirs := netsOf(node), netsOf(to)
	if len(mine) > 0 && !slices.ContainsFunc(mine, func(n string) bool { return containsFold(theirs, n) }) {
		return to, fmt.Sprintf("%s supports %s, which isn't on %s's nets (%s): drawn by it only if a net reaches it through a passive or is a rail it's the source of", part, name, part, strings.Join(mine, ", "))
	}
	return to, ""
}

// plainRails is the rails without their status.
func plainRails(rs []RailStatus) []library.Rail {
	out := make([]library.Rail, len(rs))
	for i, r := range rs {
		out[i] = r.Rail
	}
	return out
}

// connectionNode is the block-diagram node a connection is on.
func connectionNode(c library.Connection) string {
	if c.EntryID != 0 {
		return library.RefNode(c.EntryID, c.Ref)
	}
	return library.BlockNode(c.BlockID)
}

// passiveNode reports whether a node is drawn as a passive (a resistor,
// capacitor, inductor or diode).
func passiveNode(parts []library.ProjectPart, blocks []library.Block, node string) bool {
	for _, pp := range parts {
		if node == library.PartNode(pp.ID) || strings.HasPrefix(node, library.PartNode(pp.ID)+"/") {
			return twoTerminal(pp.Category, false)
		}
	}
	for _, b := range blocks {
		if node == library.BlockNode(b.ID) {
			return twoTerminal(b.Kind, true)
		}
	}
	return false
}

// nodeOfName finds the block-diagram node a designator, part number or
// block name names.
func nodeOfName(parts []library.ProjectPart, blocks []library.Block, name string) (string, bool) {
	to := endpoint(parts, blocks, strings.TrimSpace(name))
	switch {
	case to.entry != 0:
		return library.RefNode(to.entry, to.ref), true
	case to.block != 0:
		return library.BlockNode(to.block), true
	}
	return "", false
}

// target is what a connection names: a BOM entry (with Ref, one of its
// designators when it has several), or a block.
type target struct {
	entry, block int64
	ref          string
}

// found reports whether the target names anything.
func (t target) found() bool { return t.entry != 0 || t.block != 0 }

// designatorSplit separates the designators of one BOM entry ("R1,R2",
// "C3 C4").
var designatorSplit = regexp.MustCompile(`[,;\s]+`)

// endpoint finds what a connection names: a BOM entry by designator (or
// by one of its designators, "R1" of "R1,R2"), or by part number when
// only one entry has it, else a block by name.
func endpoint(parts []library.ProjectPart, blocks []library.Block, ref string) target {
	for _, pp := range parts {
		if pp.Designator != "" && strings.EqualFold(pp.Designator, ref) {
			return target{entry: pp.ID}
		}
	}
	for _, pp := range parts {
		if refs := designatorSplit.Split(strings.TrimSpace(pp.Designator), -1); len(refs) > 1 {
			for _, d := range refs {
				if strings.EqualFold(d, ref) {
					return target{entry: pp.ID, ref: d}
				}
			}
		}
	}
	var found []int64
	for _, pp := range parts {
		if strings.EqualFold(pp.MPN, ref) || (pp.Variant != "" && strings.EqualFold(pp.Variant, ref)) {
			found = append(found, pp.ID)
		}
	}
	if len(found) == 1 {
		return target{entry: found[0]}
	}
	for _, b := range blocks {
		if strings.EqualFold(b.Name, ref) {
			return target{block: b.ID}
		}
	}
	return target{}
}

// RecordPins records a datasheet's pin table for a package and keeps the
// connections made to its pins: one connected by name before the table
// had numbers, or under a name since changed, is moved to the pin's key
// now (its number, else its name), matched by number, else by place in
// a table of the same length, else by name.
func (s *Service) RecordPins(ctx context.Context, dsID int64, pkg string, pins []library.Pin, source string) error {
	old, _ := s.Lib.Pins(ctx, dsID, pkg)
	if err := s.Lib.RecordPins(ctx, dsID, pkg, pins, source); err != nil {
		return err
	}
	return s.Lib.RekeyPins(ctx, dsID, rekeys(old, pins))
}

// rekeys maps each old pin's number and name, and each new pin's name,
// to the key of the pin as it is now.
func rekeys(old, now []library.Pin) map[string]string {
	keys := map[string]string{}
	add := func(alias string, p library.Pin) {
		if alias != "" && !strings.EqualFold(alias, pinKey(p)) {
			keys[alias] = pinKey(p)
		}
	}
	for _, p := range now {
		add(p.Name, p)
	}
	for i, o := range old {
		var match *library.Pin
		for j := range now {
			if o.Number != "" && strings.EqualFold(now[j].Number, o.Number) {
				match = &now[j]
				break
			}
		}
		if match == nil && len(old) == len(now) && (o.Number == "" || now[i].Number == "") {
			match = &now[i]
		}
		if match == nil {
			for j := range now {
				if o.Name != "" && strings.EqualFold(now[j].Name, o.Name) {
					match = &now[j]
					break
				}
			}
		}
		if match == nil {
			continue
		}
		add(o.Number, *match)
		add(o.Name, *match)
	}
	return keys
}
