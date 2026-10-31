package cad

import (
	"strings"
	"testing"
)

func TestCompareFootprints(t *testing.T) {
	p, _ := Standard("SOIC-8", 8)
	ours, _ := MakeFootprint("SOIC-8", p)
	// The reference: the same pads turned a quarter, its origin at pin 1,
	// pin 3 0.1 mm out, and a pad stacked twice (a library's slip).
	ref := Footprint{}
	p1 := ours.Pads[0]
	for _, pd := range ours.Pads {
		pd.X, pd.Y = pd.X-p1.X, pd.Y-p1.Y
		pd = turned(pd, 3)
		if pd.Number == "3" {
			pd.X += 0.1
		}
		ref.Pads = append(ref.Pads, pd)
	}
	ref.Pads = append(ref.Pads, ref.Pads[0])
	d := CompareFootprints(ours, ref, 0.05)
	if d.Turn != 90 || len(d.Missing)+len(d.Extra) != 0 || len(d.Moved) == 0 || !strings.Contains(d.String(), "pad 3 ") {
		t.Errorf("diff %+v\n%s", d, d)
	}
	if d := CompareFootprints(ours, ours, 0.01); !d.OK() || d.Worst != 0 {
		t.Errorf("itself: %s", d)
	}
}

func TestCompareSymbols(t *testing.T) {
	ours := Symbol{Pins: []SymbolPin{{Number: "3", Name: "PC14-OSC32_IN"}, {Number: "4", Name: "IO4"}, {Number: "36", Name: "RXD0"}, {Number: "99", Name: "X"}}}
	ref := Symbol{Pins: []SymbolPin{{Number: "3", Name: "PC14"}, {Number: "4", Name: "GPIO4/TOUCH4/ADC1_CH3"}, {Number: "36", Name: "U0RXD/GPIO44"}, {Number: "5", Name: "VDD"}}}
	d := CompareSymbols(ours, ref, nil)
	if strings.Join(d.Renamed, ";") != "36 RXD0 / U0RXD/GPIO44" || strings.Join(d.Missing, ",") != "5" || strings.Join(d.Extra, ",") != "99" {
		t.Errorf("diff %+v", d)
	}
	if d := CompareSymbols(ours, ref, map[string]string{"RXD0": "U0RXD"}); len(d.Renamed) != 0 {
		t.Errorf("with the alias: %+v", d)
	}
}
