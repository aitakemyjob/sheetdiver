package core

import "testing"

// A board for the support rules: J1 (connector), U1 (ic, drives VBUS's
// 3V3 output), U2 (ic), M1 (connector).
func supportBoard(kind string, nets, reach []string) supportFacts {
	kinds := map[string]string{"J1": "connector", "U1": "ic", "U2": "ic", "M1": "connector", "Q1": "mosfet-n"}
	drives := map[string]bool{"U1|3V3": true, "J1|VBUS": true}
	return supportFacts{
		kind:   kind,
		nets:   nets,
		reach:  reach,
		kindOf: func(id string) string { return kinds[id] },
		drives: func(id, net string) bool { return drives[id+"|"+net] },
	}
}

func TestSupportRules(t *testing.T) {
	for _, tc := range []struct {
		name  string
		f     supportFacts
		want  string
		which string
	}{
		{"a TVS goes by the one connector it protects", supportBoard("tvs", []string{"VBUS"}, []string{"J1", "U1"}), "J1", "protection"},
		{"a Schottky too", supportBoard("schottky", []string{"MOTOR"}, []string{"Q1", "M1"}), "M1", "protection"},
		{"a capacitor goes by the one IC, over the connector", supportBoard("capacitor", []string{"VBUS"}, []string{"J1", "U1"}), "U1", "only IC"},
		{"a capacitor goes by the one part when no IC is on its nets", supportBoard("capacitor", []string{"VBUS"}, []string{"J1"}), "J1", "only part"},
		{"of two ICs, the one it feeds rather than the one driving the net", supportBoard("capacitor", []string{"3V3"}, []string{"U1", "U2"}), "U2", "feeds"},
		{"two ICs it can't tell apart: no guess", supportBoard("capacitor", []string{"SDA"}, []string{"U1", "U2"}), "", ""},
		{"a diode between two connectors goes by the one that doesn't drive the net", supportBoard("diode", []string{"VBUS"}, []string{"J1", "M1"}), "M1", "feeds"},
	} {
		if got := guessSupport(tc.f); got != tc.want {
			t.Errorf("%s: got %q, want %q", tc.name, got, tc.want)
		}
	}
}

func TestPlacedBy(t *testing.T) {
	if placedBy("", []string{"U1"}, nil, true) {
		t.Error("nothing to support: not placed by it")
	}
	if !placedBy("U1", []string{"J1", "U1"}, nil, false) {
		t.Error("on the net: placed by it")
	}
	if !placedBy("U1", []string{"J2"}, []string{"J2", "U1"}, false) {
		t.Error("reached through the inductor: placed by it")
	}
	if !placedBy("U1", []string{"J2"}, []string{"J2"}, true) {
		t.Error("the rail's source: placed by it")
	}
	if placedBy("U1", []string{"J2"}, []string{"J2"}, false) {
		t.Error("not on the net, not through a passive, not the source: by whoever is on the net")
	}
}
