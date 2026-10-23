package core

import (
	"strings"
	"testing"

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
