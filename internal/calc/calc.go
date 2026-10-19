// Package calc holds SheetDiver's design calculators: buck and boost
// power stages, MOSFET gate drive and gate dividers, resistor dividers
// and LDO dissipation. They are plain, deterministic functions over SI
// values; internal/core fills their inputs from datasheet parameters
// (with page provenance) before calling them, and agents call them over
// MCP instead of doing the arithmetic themselves.
//
// Every result lists its outputs with the formula used, plus checks
// against the device's limits, so a reviewer can follow the working.
package calc

import (
	"fmt"
	"math"
	"sort"
	"strings"
)

// Output is one computed quantity.
type Output struct {
	Name    string  `json:"name"`
	Symbol  string  `json:"symbol,omitempty"`
	Value   float64 `json:"value"`
	Unit    string  `json:"unit"`
	Formula string  `json:"formula,omitempty"`
	Note    string  `json:"note,omitempty"`
}

// Check levels.
const (
	OK    = "ok"
	Warn  = "warn"
	Error = "error"
)

// Check is a pass/warn/fail finding.
type Check struct {
	Level   string `json:"level"`
	Message string `json:"message"`
}

// Result is a calculator's answer.
type Result struct {
	Kind        string   `json:"kind"`
	Summary     string   `json:"summary"`
	Outputs     []Output `json:"outputs"`
	Checks      []Check  `json:"checks"`
	Suggestions []string `json:"suggestions,omitempty"`
}

// add appends an output.
func (r *Result) add(name, sym string, v float64, unit, formula string) {
	r.Outputs = append(r.Outputs, Output{Name: name, Symbol: sym, Value: v, Unit: unit, Formula: formula})
}

// check appends a finding.
func (r *Result) check(level, format string, a ...any) {
	r.Checks = append(r.Checks, Check{Level: level, Message: fmt.Sprintf(format, a...)})
}

// suggest appends a suggestion.
func (r *Result) suggest(format string, a ...any) {
	r.Suggestions = append(r.Suggestions, fmt.Sprintf(format, a...))
}

// Get returns an output's value by symbol or name.
func (r Result) Get(key string) (float64, bool) {
	for _, o := range r.Outputs {
		if o.Symbol == key || o.Name == key {
			return o.Value, true
		}
	}
	return 0, false
}

// Worst returns the most severe check level.
func (r Result) Worst() string {
	w := OK
	for _, c := range r.Checks {
		if c.Level == Error {
			return Error
		}
		if c.Level == Warn {
			w = Warn
		}
	}
	return w
}

// ---- E-series ---------------------------------------------------------------

// Series are the IEC 60063 preferred-number series.
var Series = map[string][]float64{
	"E6":  {1.0, 1.5, 2.2, 3.3, 4.7, 6.8},
	"E12": {1.0, 1.2, 1.5, 1.8, 2.2, 2.7, 3.3, 3.9, 4.7, 5.6, 6.8, 8.2},
	"E24": {1.0, 1.1, 1.2, 1.3, 1.5, 1.6, 1.8, 2.0, 2.2, 2.4, 2.7, 3.0, 3.3, 3.6, 3.9, 4.3, 4.7, 5.1, 5.6, 6.2, 6.8, 7.5, 8.2, 9.1},
	"E48": {1.00, 1.05, 1.10, 1.15, 1.21, 1.27, 1.33, 1.40, 1.47, 1.54, 1.62, 1.69, 1.78, 1.87, 1.96, 2.05, 2.15, 2.26, 2.37, 2.49, 2.61, 2.74, 2.87, 3.01,
		3.16, 3.32, 3.48, 3.65, 3.83, 4.02, 4.22, 4.42, 4.64, 4.87, 5.11, 5.36, 5.62, 5.90, 6.19, 6.49, 6.81, 7.15, 7.50, 7.87, 8.25, 8.66, 9.09, 9.53},
	"E96": {1.00, 1.02, 1.05, 1.07, 1.10, 1.13, 1.15, 1.18, 1.21, 1.24, 1.27, 1.30, 1.33, 1.37, 1.40, 1.43, 1.47, 1.50, 1.54, 1.58, 1.62, 1.65, 1.69, 1.74,
		1.78, 1.82, 1.87, 1.91, 1.96, 2.00, 2.05, 2.10, 2.15, 2.21, 2.26, 2.32, 2.37, 2.43, 2.49, 2.55, 2.61, 2.67, 2.74, 2.80, 2.87, 2.94, 3.01, 3.09,
		3.16, 3.24, 3.32, 3.40, 3.48, 3.57, 3.65, 3.74, 3.83, 3.92, 4.02, 4.12, 4.22, 4.32, 4.42, 4.53, 4.64, 4.75, 4.87, 4.99, 5.11, 5.23, 5.36, 5.49,
		5.62, 5.76, 5.90, 6.04, 6.19, 6.34, 6.49, 6.65, 6.81, 6.98, 7.15, 7.32, 7.50, 7.68, 7.87, 8.06, 8.25, 8.45, 8.66, 8.87, 9.09, 9.31, 9.53, 9.76},
}

// seriesValues lists a series' values across decades lo..hi (inclusive).
func seriesValues(series string, lo, hi int) []float64 {
	base, ok := Series[strings.ToUpper(series)]
	if !ok {
		base = Series["E24"]
	}
	var out []float64
	for d := lo; d <= hi; d++ {
		m := math.Pow(10, float64(d))
		for _, b := range base {
			out = append(out, round3(b*m))
		}
	}
	return out
}

// round3 rounds to three significant figures (removing float noise).
func round3(v float64) float64 {
	if v == 0 {
		return 0
	}
	p := math.Pow(10, 2-math.Floor(math.Log10(math.Abs(v))))
	return math.Round(v*p) / p
}

// Nearest returns the series value closest to v (in ratio).
func Nearest(v float64, series string) float64 {
	if v <= 0 {
		return 0
	}
	d := int(math.Floor(math.Log10(v)))
	best, bestErr := v, math.Inf(1)
	for _, c := range seriesValues(series, d-1, d+1) {
		if e := math.Abs(math.Log(c / v)); e < bestErr {
			best, bestErr = c, e
		}
	}
	return best
}

// NextUp returns the smallest series value at or above v.
func NextUp(v float64, series string) float64 {
	if v <= 0 {
		return 0
	}
	d := int(math.Floor(math.Log10(v)))
	vals := seriesValues(series, d, d+1)
	for _, c := range vals {
		if c >= v*0.999 {
			return c
		}
	}
	return vals[len(vals)-1]
}

// ---- formatting -------------------------------------------------------------

// prefixes for engineering notation, largest first.
var engPrefixes = []struct {
	exp int
	p   string
}{{9, "G"}, {6, "M"}, {3, "k"}, {0, ""}, {-3, "m"}, {-6, "µ"}, {-9, "n"}, {-12, "p"}}

// Eng formats a value with an SI prefix: Eng(0.0000047, "H") = "4.7 µH".
func Eng(v float64, unit string) string {
	if unit == "%" || unit == "°C" || unit == "°C/W" || unit == "" {
		return trimNum(v) + suffix(unit)
	}
	if v == 0 || math.IsInf(v, 0) || math.IsNaN(v) {
		return trimNum(v) + suffix(unit)
	}
	a := math.Abs(v)
	for _, p := range engPrefixes {
		if a >= math.Pow(10, float64(p.exp))*0.9995 {
			return trimNum(v/math.Pow(10, float64(p.exp))) + " " + p.p + unit
		}
	}
	return trimNum(v/1e-12) + " p" + unit
}

// suffix renders a unit after a number.
func suffix(unit string) string {
	switch unit {
	case "":
		return ""
	case "%":
		return "%"
	}
	return " " + unit
}

// trimNum prints three significant figures without trailing zeros.
func trimNum(v float64) string {
	s := fmt.Sprintf("%.3g", v)
	if strings.Contains(s, "e") {
		return fmt.Sprintf("%g", v)
	}
	return s
}

// sortChecks orders checks errors first.
func sortChecks(cs []Check) {
	rank := map[string]int{Error: 0, Warn: 1, OK: 2}
	sort.SliceStable(cs, func(i, j int) bool { return rank[cs[i].Level] < rank[cs[j].Level] })
}

// orDefault returns v, or d when v is zero.
func orDefault(v, d float64) float64 {
	if v == 0 {
		return d
	}
	return v
}
