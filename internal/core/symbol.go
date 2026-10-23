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

// Standard symbols. A two-terminal (a resistor, a diode, a crystal…) is
// drawn with its standard symbol hanging off a net, its terminals at
// each end; a transistor with its symbol, its pins by terminal.

// symbolKinds maps categories as datasheets and BOMs name them ("LED",
// "Schottky diode", "N-channel MOSFET") to the symbol they're drawn with,
// first match wins.
var symbolKinds = []struct {
	re   *regexp.Regexp
	kind string
}{
	{regexp.MustCompile(`(?i)\btvs\b|\besd\b|transient`), "tvs"},
	// An IC named after what it drives or protects (LED driver, battery protection).
	{regexp.MustCompile(`(?i)driver|protection|charger|management|monitor|gauge|controller|regulator|converter|supervisor|sensor|amplifier|\bmcu\b|\bsoc\b`), "ic"},
	{regexp.MustCompile(`(?i)schottky`), "schottky"},
	{regexp.MustCompile(`(?i)zener`), "zener"},
	{regexp.MustCompile(`(?i)\bled\b|light[- ]emitting`), "led"},
	{regexp.MustCompile(`(?i)diode|rectifier`), "diode"},
	{regexp.MustCompile(`(?i)p[- ]?(channel|ch\b)|pmos`), "mosfet-p"},
	{regexp.MustCompile(`(?i)mosfet|n[- ]?(channel|ch\b)|nmos|\bfet\b`), "mosfet-n"},
	{regexp.MustCompile(`(?i)\bpnp\b`), "pnp"},
	{regexp.MustCompile(`(?i)\bnpn\b|\bbjt\b|transistor`), "npn"},
	{regexp.MustCompile(`(?i)electrolytic|tantalum|polymer|polari[sz]ed`), "capacitor-polarised"},
	{regexp.MustCompile(`(?i)capacitor|\bmlcc\b`), "capacitor"},
	{regexp.MustCompile(`(?i)resistor|potentiometer|thermistor|\bntc\b|\bptc\b`), "resistor"},
	{regexp.MustCompile(`(?i)ferrite|bead`), "ferrite"},
	{regexp.MustCompile(`(?i)inductor|choke|coil`), "inductor"},
	{regexp.MustCompile(`(?i)fuse|polyfuse`), "fuse"},
	{regexp.MustCompile(`(?i)crystal|resonator|oscillator`), "crystal"},
	{regexp.MustCompile(`(?i)battery|\bcell\b|coin`), "battery"},
	{regexp.MustCompile(`(?i)switch|button|relay`), "switch"},
}

// symbolKind is how a part category or block kind is drawn: a block
// kind as it is; a category by what it names; else a connector for a
// block, an IC for a part.
func symbolKind(kind string, block bool) string {
	k := strings.ToLower(strings.TrimSpace(kind))
	if k == "connector" || k == "module" || k == "ic" || library.TwoTerminal(k) || library.Transistor(k) {
		return k
	}
	for _, m := range symbolKinds {
		if m.re.MatchString(k) {
			return m.kind
		}
	}
	switch {
	case connectorKindRe.MatchString(k):
		return "connector"
	case otherKindRe.MatchString(k):
		// Neither makes a net nor is an IC to hang passives by.
		return "other"
	case block:
		return "connector"
	}
	return "ic"
}

// twoTerminal reports whether a part category or block kind is drawn as
// a two-terminal symbol hanging off a net, rather than as a box with pins.
func twoTerminal(kind string, block bool) bool {
	return library.TwoTerminal(symbolKind(kind, block))
}

// passiveShape is the standard symbol a two-terminal kind is drawn with.
func passiveShape(kind string) string {
	if library.TwoTerminal(kind) {
		return kind
	}
	return "resistor"
}

// Terminals of the standard symbols, as pin tables and connections
// name them.
var (
	anodeRe     = regexp.MustCompile(`(?i)^(a|an|anode|\+|1|p|pos)$`)
	cathodeRe   = regexp.MustCompile(`(?i)^(k|c|cathode|-|2|n|neg)$`)
	gateRe      = regexp.MustCompile(`(?i)^(g|gate|b|base)$`)
	drainRe     = regexp.MustCompile(`(?i)^(d|drain|c|collector|tab)$`)
	sourceRe    = regexp.MustCompile(`(?i)^(s|source|e|emitter)$`)
	terminal1Re = regexp.MustCompile(`(?i)^(1|a)$`)
	terminal2Re = regexp.MustCompile(`(?i)^(2|b)$`)
)

// terminalOf names the terminal of a standard symbol a pin is, from its
// name (as the pin table or the connection gives it) or its number:
// anode/cathode for the diodes, +/- for a polarised capacitor or a
// battery, gate/drain/source or base/collector/emitter for a transistor,
// 1/2 for the rest; "" when it says nothing.
func terminalOf(shape, name, number string) string {
	try := func(re *regexp.Regexp) bool {
		return re.MatchString(strings.TrimSpace(name)) || name == "" && re.MatchString(strings.TrimSpace(number))
	}
	switch shape {
	case "diode", "schottky", "zener", "led", "tvs":
		switch {
		case try(anodeRe):
			return "anode"
		case try(cathodeRe):
			return "cathode"
		}
	case "capacitor-polarised", "battery":
		switch {
		case try(anodeRe):
			return "+"
		case try(cathodeRe):
			return "-"
		}
	case "mosfet-n", "mosfet-p":
		switch {
		case try(gateRe):
			return "gate"
		case try(drainRe):
			return "drain"
		case try(sourceRe):
			return "source"
		}
	case "npn", "pnp":
		switch {
		case try(gateRe):
			return "base"
		case try(drainRe):
			return "collector"
		case try(sourceRe):
			return "emitter"
		}
	default:
		switch {
		case try(terminal1Re):
			return "1"
		case try(terminal2Re):
			return "2"
		}
	}
	return ""
}

// terminals are a symbol's two terminals, at the net it hangs from and
// at its far end, as drawn by default: a diode conducts from the net
// into ground or the load; a Zener or TVS clamps the net, its cathode
// on it; a polarised capacitor's + is on the net.
func terminals(shape string) [2]string {
	switch shape {
	case "diode", "schottky", "led":
		return [2]string{"anode", "cathode"}
	case "zener", "tvs":
		return [2]string{"cathode", "anode"}
	case "capacitor-polarised", "battery":
		return [2]string{"+", "-"}
	}
	return [2]string{"1", "2"}
}

// passiveEnds says which terminal of a passive is at each of its nets:
// as its connections name its pins (D1 pin K on 3V3), else by default
// for its shape, the other way round when its far end is a supply rail
// (an LED sinking from 3V3 has its cathode on the signal).
func (b *diagramBuilder) passiveEnds(id string, p DiagramPassive) []string {
	t := terminals(p.Shape)
	at := map[string]string{} // net (lower case) → terminal
	for _, r := range b.rows {
		if nodeOf(r) != id {
			continue
		}
		if term := terminalOf(p.Shape, r.PinName, r.Pin); term != "" && (term == t[0] || term == t[1]) {
			at[strings.ToLower(r.Net)] = term
		}
	}
	first, hasFirst := at[strings.ToLower(p.Nets[0])]
	second := ""
	if len(p.Nets) == 2 {
		second = at[strings.ToLower(p.Nets[1])]
	}
	switch {
	case hasFirst:
		return []string{first, other(t, first)}
	case second != "":
		return []string{other(t, second), second}
	case t[0] != "1" && len(p.Nets) == 2 && b.kinds[strings.ToLower(p.Nets[1])] == "power" && b.kinds[strings.ToLower(p.Nets[0])] != "power":
		return []string{t[1], t[0]}
	}
	return []string{t[0], t[1]}
}

// other is the terminal of a pair that isn't the given one.
func other(t [2]string, term string) string {
	if term == t[0] {
		return t[1]
	}
	return t[0]
}

// transistorSide is the side of a transistor symbol a terminal is on:
// the gate or base on the left, the drain or collector on top, the
// source or emitter at the bottom.
func transistorSide(terminal string) string {
	switch terminal {
	case "drain", "collector":
		return SideTop
	case "source", "emitter":
		return SideBottom
	}
	return SideLeft
}

// transistorSymbol draws a part of a transistor kind as a transistor
// when each of its connected pins is a different terminal (a tab is the
// drain): the pins get their terminal and side, and unconnected ones go.
// Otherwise it stays a box with its pins.
func transistorSymbol(p *DiagramPart) {
	if !library.Transistor(p.Kind) {
		return
	}
	var pins []DiagramPin
	seen := map[string]bool{}
	for _, pin := range p.Pins {
		if !pin.Connected {
			continue
		}
		term := terminalOf(p.Kind, pin.Name, pin.Number)
		if term == "" || seen[term] {
			return
		}
		seen[term] = true
		pin.Terminal, pin.Side, pin.Tied, pin.Bridged = term, transistorSide(term), "", false
		pins = append(pins, pin)
	}
	if len(pins) == 0 {
		return
	}
	p.Shape, p.Full, p.Pins = p.Kind, false, pins
}
