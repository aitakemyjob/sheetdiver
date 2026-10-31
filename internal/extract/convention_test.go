package extract

import (
	"strings"
	"testing"
)

func TestNamedPackage(t *testing.T) {
	p, ok := namedPackage("Industry standard outline SOT-23 surface mount package using proprietary SuperSOT-3 design")
	if !ok || p.Name != "SOT-23" || len(p.Checks) != 1 {
		t.Fatalf("package %+v, %v", p, ok)
	}
	if _, ok := namedPackage("a 1.3 A, 20 V transistor"); ok {
		t.Error("a page naming no package gave one")
	}
}

func TestNumberByConvention(t *testing.T) {
	lettered := func() []Pin {
		return []Pin{{Name: "Drain", Description: "From the drawing."}, {Name: "Gate"}, {Name: "Source"}}
	}
	pins := lettered()
	note, ok := numberByConvention(pins, []Package{{Name: "SOT-23"}}, "mosfet-n")
	if !ok || !strings.Contains(note, "convention") {
		t.Fatalf("not numbered: %q", note)
	}
	if got := pins[0].Number + pins[0].Name + " " + pins[1].Number + pins[1].Name + " " + pins[2].Number + pins[2].Name; got != "1Gate 2Source 3Drain" {
		t.Errorf("pins %q", got)
	}
	if !strings.Contains(pins[2].Description, "From the drawing. Numbered 3 by the SOT-23 convention") {
		t.Errorf("description %q", pins[2].Description)
	}
	for _, c := range []struct {
		pins     []Pin
		pkgs     []Package
		category string
	}{
		{lettered(), []Package{{Name: "SOT-23"}}, "bjt-npn"},                        // no convention for the category
		{lettered(), []Package{{Name: "TO-220"}}, "mosfet-n"},                       // none for the package
		{lettered(), nil, "mosfet-n"},                                               // no package
		{[]Pin{{Number: "1", Name: "Gate"}}, []Package{{Name: "SOT-23"}}, "mosfet"}, // numbered already
		{lettered()[:2], []Package{{Name: "SOT-23"}}, "mosfet"},                     // not all the leads
	} {
		if _, ok := numberByConvention(c.pins, c.pkgs, c.category); ok {
			t.Errorf("numbered %+v in %v as %s", c.pins, c.pkgs, c.category)
		}
	}
	if normalPackage("SOT-23-3") != "SOT23" || normalPackage("sot23") != "SOT23" {
		t.Errorf("normalPackage %q %q", normalPackage("SOT-23-3"), normalPackage("sot23"))
	}
}
