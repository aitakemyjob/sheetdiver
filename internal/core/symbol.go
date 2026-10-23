package core

import (
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/aitakemyjob/sheetdiver/internal/library"
	"github.com/aitakemyjob/sheetdiver/internal/pinrole"
)

// Schematic symbols. A part with a pin table in its datasheet is drawn
// as a generated symbol: a rectangle with every pin of the table, on a
// side by its role (supplies on top, grounds and pads along the bottom,
// outputs on the right, everything else on the left), in pin-number
// order within a side, connected or not. A block without a pin table is
// drawn with its connected pins only, each facing what it connects to.

// symbolSide is the side of a generated symbol a pin goes on, by its
// role; a pin on a ground net goes along the bottom whatever it is.
func symbolSide(role pinrole.Role, onGround bool) string {
	switch {
	case onGround, role == pinrole.Ground, role == pinrole.Pad:
		return SideBottom
	case role == pinrole.Power:
		return SideTop
	case role == pinrole.Output:
		return SideRight
	}
	return SideLeft
}

// pinNumberRe splits a pin number into its letters and digits ("A12").
var pinNumberRe = regexp.MustCompile(`^([A-Za-z]*)(\d+)$`)

// lessPinNumber orders pin numbers as a pin table lists them: integers
// numerically, then grid numbers by row letter and column (A2 before
// A10, before B1), then anything else as text.
func lessPinNumber(a, b string) bool {
	ia, erra := strconv.Atoi(a)
	ib, errb := strconv.Atoi(b)
	switch {
	case erra == nil && errb == nil:
		return ia < ib
	case erra == nil:
		return true
	case errb == nil:
		return false
	}
	ma, mb := pinNumberRe.FindStringSubmatch(a), pinNumberRe.FindStringSubmatch(b)
	switch {
	case ma != nil && mb != nil:
		if !strings.EqualFold(ma[1], mb[1]) {
			return strings.ToUpper(ma[1]) < strings.ToUpper(mb[1])
		}
		na, _ := strconv.Atoi(ma[2])
		nb, _ := strconv.Atoi(mb[2])
		return na < nb
	case ma != nil:
		return true
	case mb != nil:
		return false
	}
	return strings.ToUpper(a) < strings.ToUpper(b)
}

// sideOrder is the order the sides are listed in.
var sideOrder = map[string]int{SideTop: 0, SideLeft: 1, SideRight: 2, SideBottom: 3}

// fullSymbol builds the symbol of a BOM entry from its pin table: every
// pin, connected or not, on its side in number order, then any
// connection the table doesn't have (as the pin tables list them).
func (b *diagramBuilder) fullSymbol(id string, e *end, table []library.Pin, mine []ConnectionRow) DiagramPart {
	p := DiagramPart{ID: id, Title: e.title, MPN: e.mpn, Kind: e.kind, Shape: "ic", Full: true, Column: b.columns[id], Group: b.groupOf[id], Pins: []DiagramPin{}}
	used := map[int64]bool{}
	for _, pin := range table {
		dp := DiagramPin{Name: pin.Name, Number: pin.Number, Role: string(roleOf(pin))}
		for _, r := range mine {
			if !used[r.ID] && r.Pin != "" && strings.EqualFold(r.Pin, pinKey(pin)) {
				used[r.ID] = true
				dp.Net, dp.Connected = r.Net, true
				dp.Ground = b.ground(r.Net)
				break
			}
		}
		dp.Side = symbolSide(roleOf(pin), dp.Ground)
		p.Pins = append(p.Pins, dp)
	}
	sort.SliceStable(p.Pins, func(i, j int) bool {
		if p.Pins[i].Side != p.Pins[j].Side {
			return sideOrder[p.Pins[i].Side] < sideOrder[p.Pins[j].Side]
		}
		return lessPinNumber(p.Pins[i].Number, p.Pins[j].Number)
	})
	// Connections the table lacks (a pin named as the agent read it).
	for _, r := range mine {
		if used[r.ID] {
			continue
		}
		name := pinName(r)
		if b.ground(r.Net) {
			if r.Pin == "" && r.PinName == "" {
				name = "GND"
			}
			p.Pins = append(p.Pins, connectedPin(r, name, SideBottom, "", true))
			continue
		}
		p.Pins = append(p.Pins, connectedPin(r, name, symbolSide(rowRole(r), false), "", false))
	}
	return p
}

// tiePins marks the pins of a generated symbol that share a net with a
// neighbour: each run of pins on one net next to each other on a side
// (VDD 24, 36 and 48 along the top) gets one wire, to the run's main pin
// (a supply or output, else its first), and the others are tied to it,
// drawn as a jog to its wire. A pin on the net elsewhere on the symbol,
// or past a pin on another net, stays a pin of its own that the net's
// wire reaches. The pins must be sorted by side, as fullSymbol leaves them.
func tiePins(p *DiagramPart) {
	for i := 0; i < len(p.Pins); {
		pin := p.Pins[i]
		if !pin.Connected || pin.Ground || pin.Side == SideBottom {
			i++
			continue
		}
		j := i + 1
		for j < len(p.Pins) && sameTie(p.Pins[j], pin) {
			j++
		}
		main := i
		for k := i; k < j; k++ {
			if !isMainPin(p.Pins[main]) && isMainPin(p.Pins[k]) {
				main = k
			}
		}
		for k := i; k < j; k++ {
			if k != main {
				p.Pins[k].Tied = p.Pins[main].Name
			}
		}
		i = j
	}
}

// sameTie reports whether a neighbouring pin is on the main pin's net
// and side, so a run of them ties together.
func sameTie(a, m DiagramPin) bool {
	return a.Connected && !a.Ground && a.Side == m.Side && strings.EqualFold(a.Net, m.Net)
}

// isMainPin reports whether a pin is the one that gets the wire when
// several pins of a part share a net.
func isMainPin(p DiagramPin) bool {
	return p.Role == string(pinrole.Power) || p.Role == string(pinrole.Output)
}
