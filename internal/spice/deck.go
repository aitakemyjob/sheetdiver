package spice

import (
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// Condition is what a run changes from the netlist as written: its name
// and the parameter values and temperature it sets ("hot", "load 2 A",
// "corner ss"). The empty condition runs the netlist as written.
type Condition struct {
	Name   string            `json:"name"`
	Params map[string]string `json:"params,omitempty"`
	// Temp is the circuit temperature in °C, replacing any .temp.
	Temp *float64 `json:"temp,omitempty"`
}

// valueRe is a parameter value: a braced or quoted expression, or a word.
const valueRe = `(\{[^}]*\}|'[^']*'|[^\s,]+)`

// paramValueRe is a safe parameter value: a number, a name, or an
// expression of them, in braces or not; no quotes, braces inside or
// line breaks.
var paramValueRe = regexp.MustCompile(`^[\w.+\-*/()^ ]+$|^\{[\w.+\-*/()^ ]+\}$`)

// paramNameRe is a parameter's name.
var paramNameRe = regexp.MustCompile(`^[A-Za-z_]\w*$`)

// apply is the netlist's lines with a condition's values: each parameter
// set where the netlist defines it, else added; the temperature set.
func (v Condition) apply(lines []string) ([]string, error) {
	out := append([]string(nil), lines...)
	names := make([]string, 0, len(v.Params))
	for n := range v.Params {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, name := range names {
		value := strings.TrimSpace(v.Params[name])
		if !paramNameRe.MatchString(name) || !paramValueRe.MatchString(value) {
			return nil, fmt.Errorf("condition %q: %s=%s isn't a parameter value", v.Name, name, value)
		}
		re := regexp.MustCompile(`(?i)(^|\s)(` + regexp.QuoteMeta(name) + `)\s*=\s*` + valueRe)
		found := false
		for i, l := range out {
			if f := strings.Fields(l); len(f) > 0 && strings.EqualFold(f[0], ".param") && re.MatchString(l) {
				out[i] = re.ReplaceAllString(l, "${1}${2}="+value)
				found = true
			}
		}
		if !found {
			out = append(out, ".param "+name+"="+value)
		}
	}
	if v.Temp != nil {
		kept := out[:0]
		for _, l := range out {
			if f := strings.Fields(l); len(f) > 0 && strings.EqualFold(f[0], ".temp") {
				continue
			}
			kept = append(kept, l)
		}
		out = append(kept, ".temp "+strconv.FormatFloat(*v.Temp, 'g', -1, 64))
	}
	return out, nil
}

// PlotIDs are the plots ngspice makes for each analysis, in order:
// tran1, ac1, op1…; a noise analysis makes two (the spectrum and the
// totals).
func PlotIDs(as []Analysis) []string {
	count := map[string]int{}
	var out []string
	for _, a := range as {
		n := 1
		if a.Kind == "noise" {
			n = 2
		}
		for i := 0; i < n; i++ {
			count[a.Kind]++
			out = append(out, fmt.Sprintf("%s%d", a.Kind, count[a.Kind]))
		}
	}
	return out
}

// RawFile is the name of the results file in a run's folder.
const RawFile = "results.raw"

// Deck is the deck ngspice runs: the netlist with a condition's values,
// the models it includes (files in the run's folder), and SheetDiver's
// control block, which runs the analyses and writes every plot to the
// results file.
func Deck(nl Netlist, v Condition, includes []string) (string, error) {
	lines, err := v.apply(nl.Lines)
	if err != nil {
		return "", err
	}
	var b strings.Builder
	title := nl.Title
	if title == "" {
		title = "SheetDiver simulation"
	}
	b.WriteString(title + "\n")
	for _, inc := range includes {
		fmt.Fprintf(&b, ".include %q\n", inc)
	}
	for _, l := range lines {
		b.WriteString(l + "\n")
	}
	b.WriteString(".control\nset filetype=binary\nset appendwrite\nrun\n")
	for _, p := range PlotIDs(nl.Analyses) {
		fmt.Fprintf(&b, "setplot %s\nwrite %s\n", p, RawFile)
	}
	b.WriteString(".endc\n.end\n")
	return b.String(), nil
}
