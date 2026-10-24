package core

import (
	"strings"
	"testing"

	"github.com/aitakemyjob/sheetdiver/internal/library"
	"github.com/aitakemyjob/sheetdiver/internal/pinrole"
)

func TestLessPinNumber(t *testing.T) {
	for _, tc := range [][2]string{{"2", "10"}, {"10", "A1"}, {"A2", "A10"}, {"A10", "B1"}, {"B1", "PAD"}, {"A1B12", "SH"}} {
		if !lessPinNumber(tc[0], tc[1]) || lessPinNumber(tc[1], tc[0]) {
			t.Errorf("%q should come before %q", tc[0], tc[1])
		}
	}
}

func TestSymbolSide(t *testing.T) {
	for _, tc := range []struct {
		role     pinrole.Role
		onGround bool
		want     string
	}{
		{pinrole.Power, false, SideTop}, {pinrole.Ground, false, SideBottom}, {pinrole.Pad, false, SideBottom},
		{pinrole.Output, false, SideRight}, {pinrole.Input, false, SideLeft}, {pinrole.Bidir, false, SideLeft},
		{pinrole.Other, false, SideLeft}, {pinrole.NC, false, SideLeft},
		// Whatever it is, a pin on a ground net goes along the bottom.
		{pinrole.Input, true, SideBottom}, {pinrole.Power, true, SideBottom},
	} {
		if got := symbolSide(tc.role, tc.onGround); got != tc.want {
			t.Errorf("symbolSide(%s, %v) = %s, want %s", tc.role, tc.onGround, got, tc.want)
		}
	}
}

func TestTiePins(t *testing.T) {
	pin := func(name, net, side, role string) DiagramPin {
		return DiagramPin{Name: name, Net: net, Side: side, Role: role, Connected: net != ""}
	}
	p := DiagramPart{Full: true, Pins: []DiagramPin{
		pin("VBAT", "3V3", SideTop, "power"), pin("VDDA", "3V3", SideTop, "power"), pin("VCAP", "CAP", SideTop, "power"), pin("VDD", "3V3", SideTop, "power"), pin("VDD", "3V3", SideTop, "power"),
		pin("EN", "3V3", SideLeft, "other"), pin("CFG", "", SideLeft, "other"), pin("SEL", "3V3", SideLeft, "other"),
		pin("FB", "VOUT", SideRight, "input"), pin("OUT", "VOUT", SideRight, "output"),
	}}
	tiePins(&p)
	var got []string
	for _, x := range p.Pins {
		got = append(got, x.Name+"→"+x.Tied)
	}
	// VDDA next to VBAT on top is tied to it; the VDDs past VCAP are a run
	// of their own, the second tied to the first; EN on another side is a
	// pin of its own, and so is SEL, past CFG; FB ties to OUT, the output.
	want := []string{"VBAT→", "VDDA→VBAT", "VCAP→", "VDD→", "VDD→VDD", "EN→", "CFG→", "SEL→", "FB→OUT", "OUT→"}
	if strings.Join(got, " ") != strings.Join(want, " ") {
		t.Errorf("tied:\n got %s\nwant %s", strings.Join(got, " "), strings.Join(want, " "))
	}
}

func TestSymbolKind(t *testing.T) {
	for in, want := range map[[2]string]string{
		// Block kinds as they are; categories by what they name.
		{"tvs", ""}: "tvs", {"ferrite", ""}: "ferrite", {"buck", ""}: "ic", {"", "block"}: "connector", {"module", "block"}: "module", {"led", "block"}: "led",
		{"Capacitor", ""}: "capacitor", {"MLCC", ""}: "capacitor", {"Tantalum capacitor", ""}: "capacitor-polarised", {"Chip resistor", ""}: "resistor", {"NTC thermistor", ""}: "resistor",
		{"LED", ""}: "led", {"Schottky diode", ""}: "schottky", {"Zener diode", ""}: "zener", {"ESD protection diode", ""}: "tvs", {"Rectifier diode", ""}: "diode",
		{"N-channel MOSFET", ""}: "mosfet-n", {"P-Channel MOSFET", ""}: "mosfet-p", {"NPN transistor", ""}: "npn", {"PNP", ""}: "pnp", {"Power inductor", ""}: "inductor", {"Ferrite bead", ""}: "ferrite",
		{"Crystal", ""}: "crystal", {"Tactile switch", ""}: "switch", {"Resettable fuse", ""}: "fuse", {"Coin cell holder", ""}: "battery", {"Buzzer", ""}: "other", {"Pin header", ""}: "connector",
		{"MCU / SoC", ""}: "ic", {"Buck-boost", ""}: "ic", {"Battery protection", ""}: "ic", {"Charger", ""}: "ic", {"LED driver", ""}: "ic", {"Switching regulator", ""}: "ic",
	} {
		if got := symbolKind(in[0], in[1] == "block"); got != want {
			t.Errorf("symbolKind(%q, %v) = %s, want %s", in[0], in[1] == "block", got, want)
		}
	}
	if !twoTerminal("Capacitor", false) || twoTerminal("Charger", false) || twoTerminal("mosfet-n", true) {
		t.Error("a part is a two-terminal by its category's symbol")
	}
}

func TestTerminalOf(t *testing.T) {
	for in, want := range map[[3]string]string{
		{"diode", "A", ""}: "anode", {"diode", "K", ""}: "cathode", {"led", "", "1"}: "anode", {"led", "", "2"}: "cathode", {"tvs", "Cathode", "1"}: "cathode", {"diode", "NC", ""}: "",
		{"capacitor-polarised", "+", ""}: "+", {"battery", "", "2"}: "-", {"capacitor", "", "1"}: "1", {"resistor", "B", ""}: "2", {"resistor", "3", ""}: "",
		{"mosfet-n", "G", ""}: "gate", {"mosfet-n", "Drain", "2"}: "drain", {"mosfet-p", "TAB", "4"}: "drain", {"mosfet-n", "S", ""}: "source",
		{"npn", "B", ""}: "base", {"npn", "C", ""}: "collector", {"pnp", "E", ""}: "emitter", {"npn", "", "1"}: "",
	} {
		if got := terminalOf(in[0], in[1], in[2]); got != want {
			t.Errorf("terminalOf(%q, %q, %q) = %q, want %q", in[0], in[1], in[2], got, want)
		}
	}
}

func TestPassiveEnds(t *testing.T) {
	row := func(net, pin, name string) ConnectionRow {
		return ConnectionRow{Connection: library.Connection{Net: net, BlockID: 1, Pin: pin}, PinName: name}
	}
	id := library.BlockNode(1)
	b := &diagramBuilder{kinds: map[string]string{"3v3": "power", "gnd": "ground", "led": "signal"}}
	for _, tc := range []struct {
		name  string
		shape string
		nets  []string
		rows  []ConnectionRow
		want  string
	}{
		{"a diode from a signal to ground conducts into ground", "led", []string{"LED"}, nil, "anode cathode"},
		{"an LED sinking from the rail has its cathode on the signal", "led", []string{"LED", "3V3"}, nil, "cathode anode"},
		{"a TVS clamps the rail, cathode on it", "tvs", []string{"3V3"}, nil, "cathode anode"},
		{"a polarised capacitor's + is on the rail", "capacitor-polarised", []string{"3V3"}, nil, "+ -"},
		{"a battery's + is on the rail it makes", "battery", []string{"GND", "3V3"}, nil, "- +"},
		{"a resistor's ends are 1 and 2", "resistor", []string{"LED", "3V3"}, nil, "1 2"},
		{"the connections' pin names decide", "diode", []string{"LED", "3V3"}, []ConnectionRow{row("LED", "1", "A"), row("3V3", "2", "K")}, "anode cathode"},
		{"so do their pin numbers", "diode", []string{"LED", "3V3"}, []ConnectionRow{row("LED", "2", ""), row("3V3", "1", "")}, "cathode anode"},
		{"a pin on the far net alone decides too", "capacitor-polarised", []string{"3V3", "GND"}, []ConnectionRow{row("GND", "", "+")}, "- +"},
	} {
		b.rows = tc.rows
		got := strings.Join(b.passiveEnds(id, DiagramPassive{Shape: tc.shape, Nets: tc.nets}), " ")
		if got != tc.want {
			t.Errorf("%s: ends %s, want %s", tc.name, got, tc.want)
		}
	}
}

func TestTransistorSymbol(t *testing.T) {
	pin := func(name, number, net string) DiagramPin {
		return DiagramPin{Name: name, Number: number, Net: net, Connected: net != "", Side: SideLeft}
	}
	q := DiagramPart{Kind: "mosfet-n", Shape: "ic", Full: true, Pins: []DiagramPin{pin("G", "1", "PWM"), pin("S", "2", "GND"), pin("D", "3", "LED_K"), pin("TAB", "4", "")}}
	transistorSymbol(&q)
	var got []string
	for _, p := range q.Pins {
		got = append(got, p.Name+"="+p.Terminal+":"+p.Side)
	}
	if q.Shape != "mosfet-n" || q.Full || strings.Join(got, " ") != "G=gate:left S=source:bottom D=drain:top" {
		t.Errorf("mosfet: shape %s full %v pins %s", q.Shape, q.Full, strings.Join(got, " "))
	}
	// A box drawn from its connections, with pins that aren't terminals, stays a box.
	q = DiagramPart{Kind: "npn", Shape: "ic", Pins: []DiagramPin{pin("IN", "", "A"), pin("OUT", "", "B")}}
	transistorSymbol(&q)
	if q.Shape != "ic" || len(q.Pins) != 2 {
		t.Errorf("unknown pins: shape %s, %d pins", q.Shape, len(q.Pins))
	}
	// Two pins on one terminal (D and TAB both connected): a box.
	q = DiagramPart{Kind: "mosfet-n", Shape: "ic", Pins: []DiagramPin{pin("D", "", "A"), pin("TAB", "", "A"), pin("G", "", "B")}}
	transistorSymbol(&q)
	if q.Shape != "ic" {
		t.Errorf("repeated terminal: shape %s", q.Shape)
	}
	// Not a transistor kind: untouched.
	q = DiagramPart{Kind: "ic", Shape: "ic", Pins: []DiagramPin{pin("G", "", "A")}}
	transistorSymbol(&q)
	if q.Shape != "ic" || q.Pins[0].Terminal != "" {
		t.Error("an IC isn't a transistor")
	}
}
