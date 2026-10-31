package extract

import (
	"slices"
	"strings"
	"testing"
)

func TestOrderingScheme(t *testing.T) {
	var lines []Line
	for _, l := range strings.Split(`Ordering information
Example:
STM32 U 545 V E T 6 Q TR
Device family
STM32 = Arm based 32-bit microcontroller
Product type
U = ultra-low-power
Device subfamily
545 = STM32U545xx with USB
Pin count
C = 48 pins
R = 64 pins
V = 100 pins
Flash memory size
E = 512 Kbytes
Package
T = LQFP
I = UFBGA
U = UFQFPN
Temperature range
6 = Industrial temperature range
Dedicated pinout
Q = Dedicated pinout including SMPS
Packing
TR = tape and reel`, "\n") {
		lines = append(lines, Line{Text: l})
	}
	o, ok := OrderingScheme([][]Line{lines}, []Section{{Kind: KindOrdering, Page: 0, EndPage: 0}})
	if !ok || len(o.Fields) != 9 {
		t.Fatalf("scheme %+v", o)
	}
	if o.Pins("STM32U545CE") != 48 || o.Pins("STM32U545VET6") != 100 || o.Pins("STM32F103C8") != 0 {
		t.Errorf("pins %d %d %d", o.Pins("STM32U545CE"), o.Pins("STM32U545VET6"), o.Pins("STM32F103C8"))
	}
	// A base part number comes in every package; an orderable code in its own.
	if w := o.PackageWords("STM32U545CET6"); !slices.Equal(w, []string{"LQFP"}) {
		t.Errorf("CET6 packages %v", w)
	}
	pkgs := []Package{{Name: "LQFP48"}, {Name: "UFQFPN48"}, {Name: "LQFP64"}, {Name: "UFBGA64"}, {Name: "LQFP100"}, {Name: "SOT-23"}}
	partsOfPackages(pkgs, []string{"STM32U545CE", "STM32U545RE", "STM32U545VE"}, o)
	want := map[string][]string{"LQFP48": {"STM32U545CE"}, "UFQFPN48": {"STM32U545CE"}, "LQFP64": {"STM32U545RE"}, "UFBGA64": {"STM32U545RE"}, "LQFP100": {"STM32U545VE"}, "SOT-23": nil}
	for _, p := range pkgs {
		if !slices.Equal(p.Parts, want[p.Name]) {
			t.Errorf("%s parts %v, want %v", p.Name, p.Parts, want[p.Name])
		}
	}
}
