package cad

import "testing"

func TestPassiveSymbol(t *testing.T) {
	for _, shape := range []string{"resistor", "capacitor", "capacitor-polarised", "inductor", "ferrite", "fuse", "diode", "schottky", "zener", "led", "tvs", "crystal"} {
		s, ok := PassiveSymbol("X", shape)
		if !ok || len(s.Pins) != 2 || s.Body != (Rect{}) || len(s.Graphics) == 0 || s.Reference == "" {
			t.Fatalf("%s: %+v", shape, s)
		}
		if s.Pins[0].Y != passivePin || s.Pins[1].Y != -passivePin || s.Pins[0].Side != Top || s.Pins[1].Side != Bottom {
			t.Errorf("%s pins %+v", shape, s.Pins)
		}
		for _, g := range s.Graphics {
			if err := g.Check(); err != nil {
				t.Errorf("%s: %v", shape, err)
			}
		}
	}
	r, _ := PassiveSymbol("X", "resistor")
	if r.Pins[0].Number != "1" || r.Pins[1].Number != "2" || r.Reference != "R" {
		t.Errorf("resistor %+v", r.Pins)
	}
	d, _ := PassiveSymbol("X", "diode")
	if d.Pins[0].Number != "2" || d.Pins[1].Number != "1" {
		t.Errorf("diode anode on top should be pin 2, cathode pin 1: %+v", d.Pins)
	}
	if _, ok := PassiveSymbol("X", "battery"); ok {
		t.Error("a battery got a symbol")
	}
}
