package kicad

import (
	"testing"

	"github.com/aitakemyjob/sheetdiver/internal/cad"
)

func TestPassiveSymbolRoundTrip(t *testing.T) {
	s, _ := cad.PassiveSymbol("RC0603", "resistor")
	lib, err := SymbolLibrary([]cad.Symbol{s}, nil)
	if err != nil {
		t.Fatal(err)
	}
	syms, err := Symbols(string(lib))
	if err != nil {
		t.Fatal(err)
	}
	got := ReadSymbol(syms["RC0603"])
	if len(got.Pins) != 2 || got.Pins[0].Number != "1" || got.Pins[0].Name != "~" {
		t.Errorf("pins %+v", got.Pins)
	}
}
