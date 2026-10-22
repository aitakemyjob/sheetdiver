package core

import (
	"strings"

	"github.com/aitakemyjob/sheetdiver/internal/library"
)

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
		if pin := firstNonEmpty(r.PinName, r.Pin); pin != "" {
			return r.Endpoint + "." + pin
		}
		return r.Endpoint // connected without naming a pin
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
				if !used[r.ID] && r.Pin != "" && strings.EqualFold(r.Pin, pinKey(pin)) {
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
