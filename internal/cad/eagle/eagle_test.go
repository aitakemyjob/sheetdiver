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
