package core

import (
	"context"
	"errors"
	"fmt"
	"strings"

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

// ConnectionRow is a connection with what it connects: the entry's
// designator (or part number) or the block's name, and the pin's name and
// type from the pin table.
type ConnectionRow struct {
	library.Connection
	Endpoint string `json:"endpoint"`
	PinName  string `json:"pinName,omitempty"`
	PinType  string `json:"pinType,omitempty"`
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
	out.Diagram = diagram(parts, out.Blocks, out.Nets, out.Connections, layout)
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
				pin = pinKey(found)
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
