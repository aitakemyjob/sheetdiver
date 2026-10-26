// Package layoutguide gives general PCB layout advice for a board from
// what is on it: its parts by category and pin, its nets and buses, and
// its rails' currents. The advice is common good practice, not any
// datasheet's: each rule looks for what it knows about (a switching
// regulator, an I2C bus, an exposed pad) and says how to lay it out.
// A new piece of advice is a new Rule added to the Guide.
package layoutguide

import (
	"fmt"
	"math"
	"regexp"
	"slices"
	"strings"
)

// Pin is a part's pin: its name, role (pinrole: power, ground, pad,
// output…) and net.
type Pin struct {
	Name string
	Role string
	Net  string
}

// Part is a part on the board.
type Part struct {
	Ref      string
	MPN      string
	Category string
	Pins     []Pin
}

// Net is a net with its kind (signal, power, ground) and bus.
type Net struct {
	Name string
	Kind string
	Bus  string
}

// Rail is a supply rail with the most current it carries, in amps.
type Rail struct {
	Name    string
	Voltage float64
	Current float64
}

// Board is what the advice is about.
type Board struct {
	Parts []Part
	Nets  []Net
	Rails []Rail
}

// Advice is one piece of advice: its topic, what to do, and the parts it
// is about (none for the whole board).
type Advice struct {
	Topic string
	Text  string
	Parts []string
}

// Rule gives advice on what it finds on a board.
type Rule interface {
	Advise(b Board) []Advice
}

// RuleFunc is a function as a Rule.
type RuleFunc func(b Board) []Advice

// Advise calls the function.
func (f RuleFunc) Advise(b Board) []Advice { return f(b) }

// Guide asks each of its rules in turn.
type Guide struct {
	rules []Rule
}

// New is a guide of the given rules.
func New(rules ...Rule) *Guide { return &Guide{rules: rules} }

// Default is the guide with every rule here.
func Default() *Guide {
	return New(RuleFunc(groundPlane), RuleFunc(railWidths), RuleFunc(switchers), RuleFunc(linearRegulators),
		RuleFunc(decoupling), RuleFunc(exposedPads), RuleFunc(i2c), RuleFunc(usb), RuleFunc(protection),
		RuleFunc(crystals), RuleFunc(batteries), RuleFunc(mosfets))
}

// With adds a rule.
func (g *Guide) With(r Rule) *Guide {
	g.rules = append(g.rules, r)
	return g
}

// Advise is every rule's advice, in the rules' order.
func (g *Guide) Advise(b Board) []Advice {
	var out []Advice
	for _, r := range g.rules {
		out = append(out, r.Advise(b)...)
	}
	return out
}

// ForPart is the advice about one part.
func ForPart(all []Advice, ref string) []Advice {
	var out []Advice
	for _, a := range all {
		if slices.Contains(a.Parts, ref) {
			out = append(out, a)
		}
	}
	return out
}

// BoardWide is the advice about no part in particular.
func BoardWide(all []Advice) []Advice {
	var out []Advice
	for _, a := range all {
		if len(a.Parts) == 0 {
			out = append(out, a)
		}
	}
	return out
}

// Helpers.

// category reports whether a part is of one of the categories.
func category(p Part, names ...string) bool {
	return slices.Contains(names, strings.ToLower(p.Category))
}

// pinNamed is a part's first pin whose name matches.
func pinNamed(p Part, re *regexp.Regexp) (Pin, bool) {
	for _, pin := range p.Pins {
		if re.MatchString(pin.Name) {
			return pin, true
		}
	}
	return Pin{}, false
}

// hasRole reports whether a part has a pin of a role.
func hasRole(p Part, role string) bool {
	return slices.ContainsFunc(p.Pins, func(pin Pin) bool { return pin.Role == role })
}

// netNamed lists the nets whose name or bus matches.
func netNamed(b Board, re *regexp.Regexp) []string {
	var out []string
	for _, n := range b.Nets {
		if re.MatchString(n.Name) || re.MatchString(n.Bus) {
			out = append(out, n.Name)
		}
	}
	return out
}

// The rules.

// groundPlane advises a solid ground plane for any board.
func groundPlane(b Board) []Advice {
	if len(b.Parts) == 0 {
		return nil
	}
	return []Advice{
		{Topic: "Ground", Text: "Give the board a solid ground plane on the layer next to the parts (layer 2 of a four-layer board) and don't split it under signal traces: a return current follows its trace on the plane, and a gap makes it detour into a loop."},
		{Topic: "Ground", Text: "Take each ground pin and decoupling capacitor to the plane with its own via, right at the pad, rather than sharing a long trace to one."},
	}
}

// railWidths sizes each rail's traces for its current (IPC-2221, outer
// layer, 1 oz copper, 10 °C rise).
func railWidths(b Board) []Advice {
	var out []Advice
	for _, r := range b.Rails {
		if r.Current <= 0 {
			continue
		}
		w := traceWidthMM(r.Current, 10, 1)
		out = append(out, Advice{Topic: "Power traces", Text: fmt.Sprintf("%s carries up to %s: make its traces at least %.2f mm wide on an outer layer of 1 oz copper for a 10 °C rise (IPC-2221), wider or a pour where you can, and use two or more vias where it changes layer.", r.Name, amps(r.Current), math.Max(w, 0.2))})
	}
	return out
}

// traceWidthMM is IPC-2221's external trace width for a current, a
// temperature rise and a copper weight in ounces.
func traceWidthMM(current, rise, oz float64) float64 {
	area := math.Pow(current/(0.048*math.Pow(rise, 0.44)), 1/0.725) // mil²
	mils := area / (1.378 * oz)
	return mils * 0.0254
}

// amps writes a current.
func amps(a float64) string {
	if a < 1 {
		return fmt.Sprintf("%.0f mA", a*1000)
	}
	return fmt.Sprintf("%.2g A", a)
}

var switchRe = regexp.MustCompile(`(?i)^(SW|LX|PH)\d?$`)
var feedbackRe = regexp.MustCompile(`(?i)^(FB|VFB|ADJ)$`)

// switchers advises on a switching regulator's loop, switch node and feedback.
func switchers(b Board) []Advice {
	var out []Advice
	for _, p := range b.Parts {
		_, sw := pinNamed(p, switchRe)
		if !sw && !category(p, "buck", "boost", "buck-boost") {
			continue
		}
		refs := []string{p.Ref}
		out = append(out,
			Advice{Topic: "Switching loop", Parts: refs, Text: "Place the input capacitor first, as close as it will go between the input and power-ground pins: the current it supplies switches on and off every cycle, and the area of that loop is what radiates and rings."},
			Advice{Topic: "Switching loop", Parts: refs, Text: "Keep the switch node (SW/LX) short and no wider than the current needs: it swings the full input voltage at the switching frequency. Put the inductor right next to it and keep sensitive traces off and away from it."},
			Advice{Topic: "Switching loop", Parts: refs, Text: "Return the input and output capacitors to the same ground copper at the part's power-ground pin, with vias to the plane there."},
		)
		if _, fb := pinNamed(p, feedbackRe); fb {
			out = append(out, Advice{Topic: "Feedback", Parts: refs, Text: "Put the feedback divider next to the FB pin, so the high-impedance node is short, and route its top from the output capacitor (where the voltage should be right), away from the inductor and switch node."})
		}
	}
	return out
}

// linearRegulators advises on an LDO's capacitors.
func linearRegulators(b Board) []Advice {
	var out []Advice
	for _, p := range b.Parts {
		if category(p, "ldo") {
			out = append(out, Advice{Topic: "Regulator capacitors", Parts: []string{p.Ref}, Text: "Put the input and output capacitors at the regulator's pins with short ground returns: an LDO's stability depends on its output capacitor, and trace inductance between them adds to the capacitor's ESR."})
		}
	}
	return out
}

// decoupling advises a capacitor at each supply pin of a part that isn't a regulator or a discrete.
func decoupling(b Board) []Advice {
	var out []Advice
	for _, p := range b.Parts {
		n := 0
		for _, pin := range p.Pins {
			if pin.Role == "power" && pin.Net != "" {
				n++
			}
		}
		if n == 0 || category(p, "buck", "boost", "buck-boost", "ldo", "mosfet", "tvs", "connector") {
			continue
		}
		where := "its supply pin"
		if n > 1 {
			where = fmt.Sprintf("each of its %d supply pins", n)
		}
		out = append(out, Advice{Topic: "Decoupling", Parts: []string{p.Ref}, Text: "Put a decoupling capacitor at " + where + ", between the pin and its via to ground, smallest value nearest the pin, with no via between the capacitor and the pin."})
	}
	return out
}

// exposedPads advises on soldering and cooling an exposed pad.
func exposedPads(b Board) []Advice {
	var out []Advice
	for _, p := range b.Parts {
		if hasRole(p, "pad") {
			out = append(out, Advice{Topic: "Exposed pad", Parts: []string{p.Ref}, Text: "Solder the exposed pad down and stitch it to the ground plane with an array of vias (0.3 mm drill at about 1 mm pitch, tented or filled so solder doesn't wick away); it is the part's main path for heat. Split the paste into windows covering about half the pad, so the part doesn't float."})
		}
	}
	return out
}

var i2cRe = regexp.MustCompile(`(?i)\bI2C|\bSDA\b|\bSCL\b|SMBUS`)

// i2c advises on I2C lines and their pull-ups.
func i2c(b Board) []Advice {
	if nets := netNamed(b, i2cRe); len(nets) > 0 {
		return []Advice{{Topic: "I2C", Text: fmt.Sprintf("Keep the I2C lines (%s) short and away from switching nodes, with one set of pull-ups per bus; on a long or busy bus, size them for its capacitance (the rise time limits the speed).", strings.Join(nets, ", "))}}
	}
	return nil
}

var usbRe = regexp.MustCompile(`(?i)\bUSB_?D[PMN+-]|^D[+-]$|^DP$|^DM$|\bUSB_?DP\b|\bUSB_?DM\b`)

// usb advises on routing USB data lines.
func usb(b Board) []Advice {
	if nets := netNamed(b, usbRe); len(nets) > 0 {
		return []Advice{{Topic: "USB", Text: fmt.Sprintf("Route the USB data lines (%s) as a 90 Ω differential pair over unbroken ground, matched in length, without stubs or test points hanging off them, and with the ESD protection between the connector and everything else.", strings.Join(nets, ", "))}}
	}
	return nil
}

// protection advises placing TVS and ESD parts at the connector.
func protection(b Board) []Advice {
	var out []Advice
	for _, p := range b.Parts {
		if category(p, "tvs", "esd") {
			out = append(out, Advice{Topic: "Protection", Parts: []string{p.Ref}, Text: "Place it at the connector, before anything else on the line, with the shortest possible path to ground: every millimetre of trace to it or its ground adds inductance that lets the spike through."})
		}
	}
	return out
}

var crystalRe = regexp.MustCompile(`(?i)^(X(TAL)?_?(IN|OUT)|OSC_?(IN|OUT)|XI|XO|XTAL\d?|XC(IN|OUT))$`)

// crystals advises on a crystal by an IC's oscillator pins.
func crystals(b Board) []Advice {
	var out []Advice
	for _, p := range b.Parts {
		if _, ok := pinNamed(p, crystalRe); ok {
			out = append(out, Advice{Topic: "Crystal", Parts: []string{p.Ref}, Text: "Place the crystal and its load capacitors right at the oscillator pins, ringed by ground, with no other signals routed under or beside them."})
		}
	}
	return out
}

// batteries advises on chargers' and gauges' sense lines and power path.
func batteries(b Board) []Advice {
	var out []Advice
	for _, p := range b.Parts {
		if category(p, "charger", "fuel-gauge", "battery-management") {
			out = append(out, Advice{Topic: "Battery", Parts: []string{p.Ref}, Text: "Connect any current-sense resistor with Kelvin traces (from its pads, not from the high-current copper), keep the battery's power path wide and short, and keep the part's thermal pad and the sense lines away from the charge current's path."})
		}
	}
	return out
}

// mosfets advises on a MOSFET's drain copper and gate resistor.
func mosfets(b Board) []Advice {
	var out []Advice
	for _, p := range b.Parts {
		if category(p, "mosfet") {
			out = append(out, Advice{Topic: "MOSFET", Parts: []string{p.Ref}, Text: "Give the drain (the tab, on most packages) copper area to spread its heat, and keep any gate resistor right at the gate."})
		}
	}
	return out
}
