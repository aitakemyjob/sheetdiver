package extract

import (
	"slices"
	"testing"

	"github.com/aitakemyjob/sheetdiver/internal/pdf"
)

func TestRatingsAreNotParts(t *testing.T) {
	// MAX77827's title "1.8A/3.1A Buck-Boost…" made a part "1.8A/3.1A".
	for _, tok := range []string{"1.8A/3.1A", "3.3V/5V", "2.5V-5.5V", "500MA/1A"} {
		if isPartToken(tok) {
			t.Errorf("%q is taken for a part", tok)
		}
	}
	for _, tok := range []string{"MAX77827", "STM32G031K8T6", "LM2596S-5.0", "74HC595", "1N4148W", "TPS63900DSKR"} {
		if !isPartToken(tok) {
			t.Errorf("%q isn't taken for a part", tok)
		}
	}
}

func TestPartFromFilename(t *testing.T) {
	for name, want := range map[string]string{
		"2304140030_STMicroelectronics-STM32G031K8T6_C432211.pdf":             "STM32G031K8T6",
		"lcsc_datasheet_2304140030_Analog-Devices-MAX77827EFD+T_C2683375.pdf": "MAX77827EFD+T",
		"2304140030_ADI-MAX77827EFD+T_C2683375.pdf":                           "MAX77827EFD+T",
		"C432211.pdf":     "",
		"2304140030.pdf":  "",
		"tps63900.pdf":    "TPS63900",
		"LM2596S-5.0.pdf": "LM2596S-5.0",
	} {
		if got := fromFilename(name); got != want {
			t.Errorf("fromFilename(%q) = %q, want %q", name, got, want)
		}
	}
}

func TestSlashJoinedPartNumbers(t *testing.T) {
	// MAX17048's title names both parts with a slash; the library stored
	// one part "MAX17048/MAX17049", which no ordering number matched.
	for tok, want := range map[string][]string{
		"MAX17048/MAX17049": {"MAX17048", "MAX17049"},
		"MAX17048/9":        {"MAX17048", "MAX17049"},
		"TPS2120/21":        {"TPS2120", "TPS2121"},
		"LM317":             {"LM317"},
		"1.8A/3.1A":         {"1.8A", "3.1A"},
	} {
		if got := alternatives(tok); !slices.Equal(got, want) {
			t.Errorf("alternatives(%q) = %q, want %q", tok, got, want)
		}
	}
	doc := &pdf.Document{Meta: map[string]string{"Title": "MAX17048/MAX17049 - 3µA 1-Cell/2-Cell Fuel Gauge with ModelGauge"}}
	got := PartNumbers(doc, nil, "MAX17048-MAX17049.pdf")
	if !slices.Contains(got, "MAX17048") || !slices.Contains(got, "MAX17049") || slices.Contains(got, "MAX17048/MAX17049") {
		t.Errorf("part numbers = %q, want MAX17048 and MAX17049 apart", got)
	}
}
