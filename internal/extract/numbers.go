package extract

import (
	"regexp"
	"strconv"
	"strings"
)

// numberRe matches a leading number with an optional sign, accepting the
// en dash and the Unicode minus that datasheets use for negatives.
var numberRe = regexp.MustCompile(`^\s*([±+\-–−]?)\s*(\d+(?:[.,]\d+)?|\.\d+)(?:\s*[eE]([-+]?\d+))?`)

// ParseNumber reads the number at the start of a cell ("–0.3", "1.5 (2)",
// "±2"). Footnote markers and trailing text are ignored.
func ParseNumber(s string) (float64, bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, false
	}
	m := numberRe.FindStringSubmatch(s)
	if m == nil {
		return 0, false
	}
	// A footnote marker alone, e.g. "(1)", isn't a value.
	if strings.HasPrefix(s, "(") {
		return 0, false
	}
	num := strings.Replace(m[2], ",", ".", 1)
	v, err := strconv.ParseFloat(num, 64)
	if err != nil {
		return 0, false
	}
	if m[3] != "" {
		e, _ := strconv.Atoi(m[3])
		v *= pow10(e)
	}
	if m[1] == "-" || m[1] == "–" || m[1] == "−" {
		v = -v
	}
	return v, true
}

// pow10 returns 10^e.
func pow10(e int) float64 {
	v := 1.0
	for ; e > 0; e-- {
		v *= 10
	}
	for ; e < 0; e++ {
		v /= 10
	}
	return v
}

// unitAliases normalizes common spellings.
var unitAliases = map[string]string{
	"uA": "µA", "μA": "µA", "uV": "µV", "μV": "µV", "us": "µs", "μs": "µs", "uF": "µF", "μF": "µF", "uH": "µH", "μH": "µH",
	"uW": "µW", "μW": "µW", "mohm": "mΩ", "mOhm": "mΩ", "ohm": "Ω", "Ohm": "Ω", "ohms": "Ω", "kohm": "kΩ", "kOhm": "kΩ", "Mohm": "MΩ",
	"°C/W": "°C/W", "ºC/W": "°C/W", "C/W": "°C/W", "K/W": "°C/W", "ºC": "°C", "C": "°C", "degC": "°C",
	"KHz": "kHz", "khz": "kHz", "mhz": "MHz", "Mhz": "MHz", "hz": "Hz",
}

// NormalizeUnit tidies a unit cell ("uA" → "µA", "ºC" → "°C").
func NormalizeUnit(u string) string {
	// The Ohm sign (U+2126) and micro sign variants look identical in
	// print; fold them to Ω (U+03A9) and µ (U+00B5).
	u = strings.NewReplacer("\u2126", "Ω", "\u03bc", "µ").Replace(strings.TrimSpace(u))
	if i := strings.Index(u, "("); i > 0 {
		u = strings.TrimSpace(u[:i]) // drop footnote markers
	}
	// Drop empty-cell placeholders ("–––") and spaces inside units ("m Ω").
	u = strings.Join(strings.Fields(strings.Trim(u, "–—-― ")), "")
	if a, ok := unitAliases[u]; ok {
		return a
	}
	return u
}

// prefixes are SI multipliers by prefix rune.
var prefixes = map[string]float64{"p": 1e-12, "n": 1e-9, "µ": 1e-6, "m": 1e-3, "k": 1e3, "K": 1e3, "M": 1e6, "G": 1e9}

// baseUnits are the units ToSI understands without a prefix.
var baseUnits = map[string]bool{"V": true, "A": true, "W": true, "Hz": true, "s": true, "F": true, "H": true, "Ω": true, "C": true}

// ToSI converts a value in unit to base SI units, returning the base unit
// ("mV" → V, "kHz" → Hz, "µs" → s, "%" stays "%"). Units it doesn't know
// are returned unchanged with ok false.
func ToSI(v float64, unit string) (float64, string, bool) {
	u := NormalizeUnit(unit)
	if baseUnits[u] {
		return v, u, true
	}
	for _, special := range []string{"°C", "%", "°C/W", "dB", "ppm", "ppm/°C"} {
		if u == special {
			return v, u, true
		}
	}
	r := []rune(u)
	if len(r) >= 2 {
		if m, ok := prefixes[string(r[0])]; ok && baseUnits[string(r[1:])] {
			return v * m, string(r[1:]), true
		}
	}
	return v, u, false
}
