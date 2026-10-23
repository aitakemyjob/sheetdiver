package core

import (
	"testing"

	"github.com/aitakemyjob/sheetdiver/internal/library"
)

func TestGroundPinsAreNotSupplyPins(t *testing.T) {
	// VSS reads as V+S+S to the supply pattern; tagged ground or not, it
	// isn't a supply pin, so isn't flagged for being on ground.
	for _, p := range []library.Pin{{Name: "VSS"}, {Name: "VSSA"}, {Name: "VSS", Type: "G"}, {Name: "VSSA", Type: "S"}, {Name: "GND", Type: "P"}} {
		if supplyPin(p) || !groundPin(p) {
			t.Errorf("%+v: supply %v, ground %v", p, supplyPin(p), groundPin(p))
		}
	}
	for _, p := range []library.Pin{{Name: "VDD"}, {Name: "VDDA"}, {Name: "VBAT"}, {Name: "VS"}, {Name: "PVIN", Type: "P"}, {Name: "IN"}} {
		if !supplyPin(p) {
			t.Errorf("%+v isn't a supply pin", p)
		}
	}
}

func TestUnpoweredParts(t *testing.T) {
	for _, c := range []struct {
		cat, ref string
		want     bool
	}{
		{"LED", "D1", true}, {"Tactile switch", "SW2", true}, {"Crystal", "Y1", true}, {"", "SW1", true}, {"", "Y1", true},
		{"Pin header", "J2", true}, {"resistor", "R1", true}, {"mosfet-n", "Q1", true},
		{"Microcontroller", "U2", false}, {"", "U1", false}, {"Oscillator", "Y2", false}, {"ldo", "U3", false},
	} {
		if got := unpowered(library.ProjectPart{Category: c.cat, Designator: c.ref}); got != c.want {
			t.Errorf("unpowered(%q %q) = %v", c.cat, c.ref, got)
		}
	}
}
