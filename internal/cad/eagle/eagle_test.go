package eagle

import (
	"encoding/xml"
	"os"
	"strings"
	"testing"

	"github.com/aitakemyjob/sheetdiver/internal/cad"
)

// testPart is a part with two GND pins and an exposed pad.
func testPart(t *testing.T) Part {
	t.Helper()
	q := cad.Package{Family: "dfn", Pins: 8, Pitch: 0.5, BodyLength: cad.Dim{Nom: 2}, BodyWidth: cad.Dim{Nom: 2}, LeadLength: cad.Dim{Min: 0.25, Max: 0.45},
		LeadWidth: cad.Dim{Min: 0.18, Max: 0.3}, Height: cad.Dim{Max: 0.8}, ExposedPad: &cad.Size{W: 0.9, H: 1.6}}
	fp, ps := cad.MakeFootprint("DFN-8_2x2", q)
	if err := cad.Err(ps); err != nil {
		t.Fatal(err)
	}
	pins := []cad.PinInfo{{Number: "1", Name: "VIN"}, {Number: "2", Name: "GND"}, {Number: "3", Name: "EN", Type: "I"}, {Number: "4", Name: "NC"},
		{Number: "5", Name: "FB", Type: "I"}, {Number: "6", Name: "VOUT", Type: "O"}, {Number: "7", Name: "PG", Description: "open-drain output"}, {Number: "8", Name: "GND"}, {Number: "9", Name: "EP", Description: "Exposed thermal pad"}}
	s, ps := cad.MakeSymbol("TLV757P", pins, cad.SymbolSpec{})
	if err := cad.Err(ps); err != nil {
		t.Fatal(err)
	}
	return Part{Symbol: s, Footprint: fp}
}

func TestLibrary(t *testing.T) {
	out := Library("SheetDiver", []Part{testPart(t)})
	if os.Getenv("SHEETDIVER_KEEP") != "" {
		_ = os.WriteFile(os.Getenv("SHEETDIVER_KEEP"), out, 0o644)
	}
	var doc struct {
		Packages []struct {
			Name string `xml:"name,attr"`
			SMDs []struct {
				Name  string `xml:"name,attr"`
				Cream string `xml:"cream,attr"`
			} `xml:"smd"`
		} `xml:"drawing>library>packages>package"`
		Symbols []struct {
			Pins []struct {
				Name      string `xml:"name,attr"`
				Direction string `xml:"direction,attr"`
			} `xml:"pin"`
		} `xml:"drawing>library>symbols>symbol"`
		Connects []struct {
			Pin string `xml:"pin,attr"`
			Pad string `xml:"pad,attr"`
		} `xml:"drawing>library>devicesets>deviceset>devices>device>connects>connect"`
	}
	if err := xml.Unmarshal(out, &doc); err != nil {
		t.Fatal(err)
	}
	if len(doc.Packages) != 1 || len(doc.Packages[0].SMDs) != 9 || doc.Packages[0].Name != "DFN-8_2X2" {
		t.Fatalf("packages: %+v", doc.Packages)
	}
	names := map[string]string{}
	for _, p := range doc.Symbols[0].Pins {
		names[p.Name] = p.Direction
	}
	if names["GND@1"] != "pwr" || names["GND@2"] != "pwr" || names["PG"] != "oc" || names["NC"] != "nc" {
		t.Errorf("pins: %v", names)
	}
	if len(doc.Connects) != 9 {
		t.Errorf("%d connects", len(doc.Connects))
	}
	// A pad this small (0.9 × 1.6 mm) keeps its own paste.
	if strings.Contains(string(out), `layer="31"`) || strings.Contains(string(out), `cream="no"`) {
		t.Error("paste windows on a small exposed pad")
	}
}

// What's written reads back the same, and an element edited in the file
// reads as the edit while the rest stay as they were.
func TestReadBack(t *testing.T) {
	p := testPart(t)
	lib := Library("SheetDiver", []Part{p})
	r, err := ReadLibrary(lib)
	if err != nil {
		t.Fatal(err)
	}
	pkg, ok := r.Packages["DFN-8_2X2"]
	if !ok || len(pkg.Footprint.Pads) != len(p.Footprint.Pads) || !strings.HasPrefix(pkg.Raw, "<package") {
		t.Fatalf("package read back: %+v", r.Packages)
	}
	sym, ok := r.Symbols["TLV757P"]
	if !ok || sym.Package != "DFN-8_2X2" || sym.Symbol.Reference != "U" || len(sym.Symbol.Pins) != 9 {
		t.Fatalf("symbol read back: %+v", r.Symbols)
	}
	nums := map[string]string{}
	for _, pin := range sym.Symbol.Pins {
		nums[pin.Number] = pin.Name
	}
	if nums["2"] != "GND" || nums["8"] != "GND" || nums["7"] != "PG" {
		t.Errorf("pins by number: %v", nums)
	}
	// Written again from what was read, raw, it's the same file.
	again := Library("SheetDiver", []Part{{Symbol: sym.Symbol, Footprint: pkg.Footprint, RawPackage: pkg.Raw, RawSymbol: sym.RawSymbol, RawDevice: sym.RawDevice}})
	r2, err := ReadLibrary(again)
	if err != nil {
		t.Fatal(err)
	}
	if cad.FootprintPrint(r2.Packages["DFN-8_2X2"].Footprint) != cad.FootprintPrint(pkg.Footprint) || cad.SymbolPrint(r2.Symbols["TLV757P"].Symbol) != cad.SymbolPrint(sym.Symbol) {
		t.Error("rewriting what was read changed it")
	}
	// An edit: pad 1 wider.
	before := `<smd name="1" x="-0.975" y="0.75" dx="0.75"`
	if !strings.Contains(string(lib), before) {
		t.Fatal("no pad 1 to edit")
	}
	r3, err := ReadLibrary([]byte(strings.Replace(string(lib), before, `<smd name="1" x="-0.975" y="0.75" dx="0.95"`, 1)))
	if err != nil {
		t.Fatal(err)
	}
	if cad.FootprintPrint(r3.Packages["DFN-8_2X2"].Footprint) == cad.FootprintPrint(pkg.Footprint) {
		t.Error("an edited pad kept the fingerprint")
	}
}
