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
	// Read from a drawing that gives no numbers (the NDS331N's Rev. 5).
	lettered := []Pin{{Name: "Drain"}, {Name: "Gate"}, {Name: "Source"}}
	if got := pinWarnings(lettered, counts("SOT-23 surface mount package")); len(got) != 1 || !strings.Contains(got[0], "3 pins were read but the datasheet gives them no numbers") {
		t.Errorf("unnumbered: %q", got)
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
	for _, p := range drawingPins([][]Line{page}, "mosfet-n") {
		got = append(got, p.Number+" "+p.Name)
	}
	if want := []string{"1 Gate", "2 Source", "3 Drain"}; !slices.Equal(got, want) {
		t.Errorf("pins = %q, want %q", got, want)
	}
	// Terminal names without numbers near them (a description): none.
	if pins := drawingPins([][]Line{{line(60, 200, "Drain"), line(60, 400, "Source"), line(300, 600, "7")}}, "mosfet-n"); len(pins) != 0 {
		t.Errorf("pins from prose: %+v", pins)
	}
	// The NDS331N's Rev. 5: the package drawing letters its leads "D", "S"
	// and "G" and numbers nothing, and the ratings table has subscript
	// "D"s of its own far below. The letters are the pins, unnumbered.
	rev5 := []Line{
		line(405, 369, "D"), line(437, 441, "S"), line(373, 441, "G"),
		line(77, 555, "D"), line(80, 582, "D"),
	}
	got = nil
	for _, p := range drawingPins([][]Line{rev5}, "mosfet-n") {
		got = append(got, p.Number+"|"+p.Name+"|"+strconv.Itoa(p.Page))
	}
	if want := []string{"|Drain|0", "|Gate|0", "|Source|0"}; !slices.Equal(got, want) {
		t.Errorf("lettered pins = %q, want %q", got, want)
	}
	// Not for an IC, whose lone letters are subscripts; not when the set
	// is incomplete or scattered over the page.
	for name, tc := range map[string]struct {
		page     []Line
		category string
	}{
		"ic":         {rev5, "buck"},
		"incomplete": {[]Line{line(405, 369, "D"), line(437, 441, "S")}, "mosfet-n"},
		"scattered":  {[]Line{line(405, 369, "D"), line(437, 441, "S"), line(60, 700, "G")}, "mosfet-n"},
	} {
		if pins := drawingPins([][]Line{tc.page}, tc.category); len(pins) != 0 {
			t.Errorf("%s: pins = %+v", name, pins)
		}
	}
	// A diode's drawing: "A" and "K".
	got = nil
	for _, p := range drawingPins([][]Line{{line(100, 100, "A"), line(160, 100, "K")}}, "diode") {
		got = append(got, p.Name)
	}
	if want := []string{"Anode", "Cathode"}; !slices.Equal(got, want) {
		t.Errorf("diode pins = %q, want %q", got, want)
	}
}

func TestProsePinsPerPackage(t *testing.T) {
	// The LTC3536's Pin Functions: a heading per pin with the DFN and MSOP
	// numbers slash-separated, the package list on its own line under the
	// heading, two columns sharing baselines, one pin on two MSOP pins and
	// the exposed pad named as such.
	s := newSynth(3).header("LTC3536").
		bold(0, 46, 84, 18, "Description").
		text(0, 46, 108, 10, "The LTC3536 is a 1A buck-boost converter that regulates from inputs above, below or equal to the output.").
		text(0, 46, 122, 10, "The device operates from 1.8V to 5.5V and is available in DFN and MSOP packages.").
		bold(1, 46, 84, 18, "Pin Functions").
		text(1, 46, 98, 8, "(DFN/MSOP)").
		text(1, 46, 108, 10, "RT (Pin 1/Pin 1): Oscillator Frequency Programming VOUT (Pin 6/Pin 7): Output Voltage. This pin is the power").
		text(1, 46, 122, 10, "Input. Connect a resistor from RT to GND to program the output for the regulator.").
		text(1, 315, 184, 10, "VIN (Pin 7/Pins 8, 9): Power Input for the Converter. A low").
		text(1, 46, 224, 10, "SGND (Pin 2/Pin 2): Ground Connection for the LTC3536. placed as close to this pin as possible.").
		text(1, 46, 300, 10, "MODE/SYNC (Pin 3/Pin 3): Pulse Width Modulation/Burst").
		text(1, 45, 528, 10, "switches A and B. Connect one side of the buck-boost PGND (Exposed Pad Pin 11/Pin 5, Exposed Pad Pin 13):").
		text(1, 46, 560, 10, "Power Ground. The exposed pad must be soldered to the PCB.").
		bold(2, 46, 84, 18, "Block Diagram").
		drawing(2, 60, 100, 500, 600)
	r := Analyze(s.doc, "ltc3536.pdf")
	var got []string
	for _, p := range r.Pins {
		got = append(got, p.Package+":"+p.Number+" "+p.Name)
	}
	want := []string{"DFN:1 RT", "DFN:2 SGND", "DFN:3 MODE/SYNC", "DFN:6 VOUT", "DFN:7 VIN", "DFN:11 PGND", "MSOP:1 RT", "MSOP:2 SGND", "MSOP:3 MODE/SYNC", "MSOP:5 PGND", "MSOP:7 VOUT", "MSOP:8 VIN", "MSOP:9 VIN", "MSOP:13 PGND"}
	if !slices.Equal(got, want) {
		t.Errorf("pins = %q\nwant   %q", got, want)
	}
	for _, p := range r.Pins {
		switch p.Name {
		case "RT":
			if p.Description != "Oscillator Frequency Programming" {
				t.Errorf("RT's description stops at the next heading: %q", p.Description)
			}
		case "VOUT":
			if !strings.HasPrefix(p.Description, "Output Voltage.") {
				t.Errorf("VOUT's description: %q", p.Description)
			}
		case "PGND":
			if p.Description != "Exposed pad." && p.Number != "5" || p.Number == "5" && p.Description != "" {
				t.Errorf("PGND %s/%s: %q", p.Package, p.Number, p.Description)
			}
		}
	}
	// A drawing's note about the pad is no heading: it has no colon.
	s = newSynth(2).header("LTC3536").
		bold(0, 46, 84, 18, "Description").
		text(0, 46, 108, 10, "The LTC3536 is a 1A buck-boost converter that regulates from inputs above, below or equal to the output.").
		bold(1, 46, 84, 18, "Pin Configuration").
		text(1, 100, 300, 8, "EXPOSED PAD (PIN 11) IS PGND, MUST BE SOLDERED TO PCB").
		text(1, 100, 320, 8, "EXPOSED PAD (PIN 13) IS PGND MUST BE SOLDERED TO PCB")
	if pins := Analyze(s.doc, "x.pdf").Pins; len(pins) != 0 {
		t.Errorf("pins from a drawing's note: %+v", pins)
	}
	// One package, no list: no package label, as before.
	s = newSynth(2).header("TP9999").
		bold(0, 46, 84, 18, "Description").
		text(0, 46, 108, 10, "The TP9999 is a single-cell lithium-ion battery charger in an SOP8 package.").
		bold(1, 46, 84, 18, "Pin Functions").
		text(1, 46, 108, 10, "TEMP(PIN1): Battery temperature detection input").
		text(1, 46, 122, 10, "V CC (PIN4): Supply")
	got = nil
	for _, p := range Analyze(s.doc, "x.pdf").Pins {
		got = append(got, p.Package+":"+p.Number+" "+p.Name)
	}
	if want := []string{":1 TEMP", ":4 VCC"}; !slices.Equal(got, want) {
		t.Errorf("single package pins = %q, want %q", got, want)
	}
}
