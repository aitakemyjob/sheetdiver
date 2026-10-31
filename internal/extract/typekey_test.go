package extract

import "testing"

func TestTypeKeyRe(t *testing.T) {
	for text, want := range map[string]bool{
		"a P: power supply; I: input; O: output; T: high impedance.": true,
		"(1) P: power supply; I: input; O: output.":                  true,
		"P: power supply; I: input; O: output; T: high impedance.":   true,
		"7 EN I EN: input; active high enables the regulator":        false,
		"2 EN/UVLO Analog input Active high enable":                  false,
	} {
		if got := typeKeyRe.MatchString(text); got != want {
			t.Errorf("%q: %v, want %v", text, got, want)
		}
	}
}
