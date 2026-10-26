package extract

import (
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/aitakemyjob/sheetdiver/internal/pdf"
)

func TestNumberPads(t *testing.T) {
	// The TPS63900's "Thermal Pad" has no number; the MAX17048's EP has none
	// in its TDFN, and its WLP bumps aren't numbered 1…N (#75).
	pins := numberPads([]Pin{
		{Number: "1", Name: "EN"}, {Number: "10", Name: "VIN"}, {Name: "Thermal Pad", Description: "Connect this pin to ground."},
		{Package: "TDFN", Number: "8", Name: "SDA"}, {Package: "TDFN", Name: "EP", Description: "Exposed Pad (TDFN Only)."},
		{Package: "WLP", Number: "B1", Name: "SDA"}, {Package: "WLP", Name: "EP"},
		// Named by the datasheet: kept.
		{Package: "DSG", Number: "8", Name: "GND"}, {Package: "DSG", Number: "PAD", Name: "GND", Description: "The exposed pad"},
	})
	got := []string{}
	for _, p := range pins {
		got = append(got, p.Package+":"+p.Number+" "+p.Name)
	}
	want := []string{":1 EN", ":10 VIN", ":11 Thermal Pad", "TDFN:8 SDA", "TDFN:9 EP", "WLP:B1 SDA", "WLP: EP", "DSG:8 GND", "DSG:PAD GND"}
	if !slices.Equal(got, want) {
		t.Errorf("pins = %q, want %q", got, want)
	}
	if !strings.Contains(pins[2].Description, "Numbered 11 here") {
		t.Errorf("the pad's description says its number is ours: %q", pins[2].Description)
	}
}

func TestPinWarnings(t *testing.T) {
	counts := func(texts ...string) []packageCount { return packagePinCounts(texts, "") }
	// The TPS2121's two packages, each with its count in a caption.
	mux := counts("Figure 6-1. TPS2120 (YFP) Package 20-Pin WCSP Bottom View", "Figure 6-2. TPS2121 (RUX) Package 12-Pin VQFN-HR Bottom View")
	var pins []Pin
	for i := 1; i <= 12; i++ {
		pins = append(pins, Pin{Package: "TPS2121 VQFN-HR", Number: strconv.Itoa(i)})
	}
	pins = append(pins, Pin{Package: "TPS2120 WCSP", Number: "A1"}, Pin{Package: "TPS2120 WCSP", Number: "A2"})
	if got := pinWarnings(pins, mux); len(got) != 1 || !strings.Contains(got[0], "only 2 of 20 pins were read for TPS2120 WCSP") {
		t.Errorf("warnings = %q", got)
	}
	// None read, where the datasheet gives a package.
	if got := pinWarnings(nil, counts("SOT−23/SUPERSOT−23, 3 LEAD, 1.4x2.9")); len(got) != 1 || !strings.Contains(got[0], "no pins were read") {
		t.Errorf("no pins: %q", got)
	}
	// All there, the exposed pad aside: nothing to say.
	full := []Pin{{Number: "1"}, {Number: "2"}, {Number: "3"}, {Number: "4"}, {Number: "5"}, {Number: "6"}, {Number: "7"}, {Number: "8"}, {Number: "9", Name: "EP", Description: "Exposed pad"}}
	if got := pinWarnings(full, counts("8-pin TDFN package")); len(got) != 0 {
		t.Errorf("complete: %q", got)
	}
	// Nothing said about the package: nothing to compare with.
	if got := pinWarnings(nil, nil); len(got) != 0 {
		t.Errorf("no counts: %q", got)
	}
}

func TestPackagePinCounts(t *testing.T) {
	for text, want := range map[string]int{
		"DSK Package 10-Pin WSON (Top View)": 10,
		"in an 8-pin TDFN package":           8,
		"QFN32 (5×5 mm)":                     32,
		"SOT-23-6":                           6,
		"SOT-23":                             3,
		"WSON-8":                             8,
		"20-bump WLP":                        20,
	} {
		got := packagePinCounts([]string{text}, "")
		if len(got) == 0 || got[0].n != want {
			t.Errorf("%q: %+v, want %d", text, got, want)
		}
	}
	// Not packages: a person, a version.
	for _, text := range []string{"PERSON-1 approved", "Revision 2"} {
		if got := packagePinCounts([]string{text}, ""); len(got) != 0 {
			t.Errorf("%q: %+v", text, got)
		}
	}
	// The pin pages first; the first page only when they say nothing.
	if got := packagePinCounts([]string{"Pin Functions"}, "available in a 8-pin TDFN"); len(got) != 1 || got[0].n != 8 {
		t.Errorf("first page: %+v", got)
	}
}

func TestDrawingPins(t *testing.T) {
	// The NDS331N's marking diagram: "Drain" over "3", "1" and "2" over
	// "Gate Source", and a symbol's lone letters with no numbers (#75).
	line := func(x, y float64, s string) Line {
		sp := tspan(x, y, 8, s)
		return Line{Text: s, Box: sp.Box, Size: 8, Spans: []pdf.Span{sp}}
	}
	page := []Line{
		line(472, 92, "D"), line(430, 117, "G"), line(448, 126, "S"),
		line(444, 200, "Drain"), line(462, 212, "3"),
		line(443, 231, "331M"),
		line(422, 250, "1"), line(482, 250, "2"),
		line(426, 261, "Gate Source"),
	}
	var got []string
	for _, p := range drawingPins([][]Line{page}) {
		got = append(got, p.Number+" "+p.Name)
	}
	if want := []string{"1 Gate", "2 Source", "3 Drain"}; !slices.Equal(got, want) {
		t.Errorf("pins = %q, want %q", got, want)
	}
	// Terminal names without numbers near them (a description): none.
	if pins := drawingPins([][]Line{{line(60, 200, "Drain"), line(60, 400, "Source"), line(300, 600, "7")}}); len(pins) != 0 {
		t.Errorf("pins from prose: %+v", pins)
	}
}
