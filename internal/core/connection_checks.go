package core

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/aitakemyjob/sheetdiver/internal/calc"
	"github.com/aitakemyjob/sheetdiver/internal/library"
	"github.com/aitakemyjob/sheetdiver/internal/pinrole"
)

// mustRe is a pin description that says the pin can't be left open.
var mustRe = regexp.MustCompile(`(?i)connect (this pin |it |the pad )?to (ground|gnd)|must (not|never) be left (open|floating|unconnected)|must not float|do not leave (it )?(open|floating|unconnected)|must be connected`)

// roleOf is what a pin of a pin table is (pinrole.Of on its columns).
func roleOf(p library.Pin) pinrole.Role {
	return pinrole.Of(p.Name, p.Type, p.Description)
}

// mustConnect reports whether a pin can't be left unconnected: supply and
// ground pins, exposed pads, and pins whose description says so.
func mustConnect(p library.Pin) bool {
	switch roleOf(p) {
	case pinrole.Ground, pinrole.Power, pinrole.Pad:
		return true
	}
	return mustRe.MatchString(p.Description)
}

// groundPin reports whether a pin is ground (a pad counts: it goes there).
func groundPin(p library.Pin) bool {
	r := roleOf(p)
	return r == pinrole.Ground || r == pinrole.Pad
}

// supplyPin reports whether a pin takes the part's supply.
func supplyPin(p library.Pin) bool {
	return roleOf(p) == pinrole.Power
}

// outputPin reports whether a pin drives its net.
func outputPin(p library.Pin) bool {
	return roleOf(p) == pinrole.Output
}

// netKinds says which nets are ground and which power (a net named like
// a rail is that rail).
func netKinds(nets []library.Net, rails []RailStatus) map[string]string {
	out := map[string]string{}
	for _, n := range nets {
		kind := n.Kind
		if kind == library.NetSignal && pinrole.IsGroundName(n.Name) {
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
		connected[r.EntryID][pinKey(pin)] = r.Net
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
	// Pins that must be connected but aren't (supply, ground, exposed
	// pads, and pins whose description says so), on parts whose pins are
	// being connected.
	for _, pp := range parts {
		got := connected[pp.ID]
		if len(got) == 0 {
			continue
		}
		var open []string
		for _, pin := range pins[pp.ID] {
			if _, ok := got[pinKey(pin)]; !ok && mustConnect(pin) {
				if pin.Number == "" || pin.Number == pin.Name {
					open = append(open, pinKey(pin))
				} else {
					open = append(open, fmt.Sprintf("%s (%s)", pin.Name, pin.Number))
				}
			}
		}
		if len(open) > 0 {
			add(calc.Warn, entryLabel(pp), "%s: pins that must be connected aren't: %s", entryLabel(pp), strings.Join(open, ", "))
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
