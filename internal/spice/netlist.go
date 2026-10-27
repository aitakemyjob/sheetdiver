package spice

import (
	"errors"
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"
)

// Checking a netlist. An agent's netlist describes the circuit, its
// analyses and its measurements; SheetDiver adds how it's run. Each
// line goes through a chain of rules: one that knows the line says
// whether it may stay (and notes the analyses and measurements it
// finds); a dot command no rule knows is refused, so only what's on
// the allow list runs. Files are never read or written by the netlist:
// part models come in through SheetDiver, which includes them itself.

// Analysis is an analysis the netlist asks for: its kind (tran, ac, dc,
// op, noise, tf, pz, sens, disto) and its line.
type Analysis struct {
	Kind string `json:"kind"`
	Line string `json:"line"`
}

// Netlist is a checked netlist: its title, its lines (without .end),
// its analyses and the names of its measurements.
type Netlist struct {
	Title    string     `json:"title"`
	Lines    []string   `json:"-"`
	Analyses []Analysis `json:"analyses"`
	Measures []string   `json:"measures"`
}

// Limits on a netlist.
const (
	maxNetlistBytes = 1 << 20
	maxNetlistLines = 20000
	// maxTranPoints is how many output steps a transient may ask for.
	maxTranPoints = 2_000_000
)

// lineRule decides on the lines it knows: handled is false for a line it
// doesn't, err says why a line it knows can't run.
type lineRule interface {
	check(cmd string, fields []string, nl *Netlist) (handled bool, err error)
}

// refused are dot commands a netlist may not have, and why.
type refused struct{}

// check refuses control blocks and anything that reads files.
func (refused) check(cmd string, fields []string, _ *Netlist) (bool, error) {
	switch cmd {
	case ".control", ".endc":
		return true, errors.New(".control blocks aren't allowed: SheetDiver runs the analyses and writes the results itself; put measurements in .meas lines")
	case ".include", ".inc":
		return true, errors.New(".include isn't allowed: give the parts' models with models (record_spice_model first) and SheetDiver includes them")
	case ".lib":
		if len(fields) > 2 {
			return true, errors.New(".lib with a file isn't allowed: give the parts' models with models (record_spice_model first)")
		}
		return true, nil // the start of a library section written inline
	}
	return false, nil
}

// analyses notes the analyses, checking a transient's size.
type analyses struct{}

// analysisKinds are the analyses ngspice runs from a netlist.
var analysisKinds = map[string]string{".tran": "tran", ".ac": "ac", ".dc": "dc", ".op": "op", ".noise": "noise", ".tf": "tf", ".pz": "pz", ".sens": "sens", ".disto": "disto"}

// check notes an analysis line.
func (analyses) check(cmd string, fields []string, nl *Netlist) (bool, error) {
	kind, ok := analysisKinds[cmd]
	if !ok {
		return false, nil
	}
	if kind == "tran" && len(fields) >= 3 {
		step, err1 := ParseNumber(fields[1])
		stop, err2 := ParseNumber(fields[2])
		if err1 == nil && err2 == nil && step > 0 && stop/step > maxTranPoints {
			return true, fmt.Errorf("%s asks for %.0f steps: at most %d (a larger step, or a shorter time)", strings.Join(fields, " "), stop/step, maxTranPoints)
		}
	}
	nl.Analyses = append(nl.Analyses, Analysis{Kind: kind, Line: strings.Join(fields, " ")})
	return true, nil
}

// measures notes the names of .meas lines.
type measures struct{}

// check notes a measurement's name.
func (measures) check(cmd string, fields []string, nl *Netlist) (bool, error) {
	if cmd != ".meas" && cmd != ".measure" {
		return false, nil
	}
	if len(fields) < 3 {
		return true, fmt.Errorf("%q: a measurement is .meas ANALYSIS NAME …", strings.Join(fields, " "))
	}
	nl.Measures = append(nl.Measures, strings.ToLower(fields[2]))
	return true, nil
}

// allowed are the other dot commands a netlist may have.
type allowed struct{}

// allowedCommands describe the circuit, not how it's run.
var allowedCommands = map[string]bool{
	".param": true, ".func": true, ".model": true, ".subckt": true, ".ends": true, ".endl": true, ".global": true,
	".ic": true, ".nodeset": true, ".temp": true, ".option": true, ".options": true, ".save": true, ".print": true,
	".plot": true, ".four": true, ".csparam": true, ".title": true,
}

// check allows the commands on the allow list.
func (allowed) check(cmd string, _ []string, _ *Netlist) (bool, error) {
	return allowedCommands[cmd], nil
}

// fileParamRe is a device or model parameter that names a file (an
// XSPICE source reading its data).
var fileParamRe = regexp.MustCompile(`(?i)\b(file|input_file|infile|filename)\s*=`)

// rules are asked in order.
var rules = []lineRule{refused{}, analyses{}, measures{}, allowed{}}

// Check checks an agent's netlist, returning every problem it has.
func Check(text string) (Netlist, error) {
	var nl Netlist
	if len(text) > maxNetlistBytes {
		return nl, fmt.Errorf("the netlist is %d bytes: at most %d", len(text), maxNetlistBytes)
	}
	lines := strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), "\n")
	if len(lines) > maxNetlistLines {
		return nl, fmt.Errorf("the netlist has %d lines: at most %d", len(lines), maxNetlistLines)
	}
	var problems []error
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if i == 0 {
			// SPICE takes the first line as the title, whatever it says.
			nl.Title = trimmed
			continue
		}
		if fileParamRe.MatchString(stripComment(trimmed)) {
			problems = append(problems, fmt.Errorf("line %d: parameters naming files aren't allowed", i+1))
			continue
		}
		fields := strings.Fields(stripComment(trimmed))
		if len(fields) == 0 || !strings.HasPrefix(fields[0], ".") {
			nl.Lines = append(nl.Lines, line)
			continue
		}
		cmd := strings.ToLower(fields[0])
		if cmd == ".end" {
			continue // SheetDiver ends the deck after its control block
		}
		handled := false
		for _, r := range rules {
			ok, err := r.check(cmd, fields, &nl)
			if err != nil {
				problems = append(problems, fmt.Errorf("line %d: %w", i+1, err))
			}
			if ok {
				handled = true
				break
			}
		}
		if !handled {
			problems = append(problems, fmt.Errorf("line %d: %s isn't allowed", i+1, fields[0]))
		}
		nl.Lines = append(nl.Lines, line)
	}
	if len(nl.Analyses) == 0 && len(problems) == 0 {
		problems = append(problems, errors.New("the netlist has no analysis: add .tran, .ac, .dc, .op, .noise, .tf, .pz, .sens or .disto"))
	}
	return nl, errors.Join(problems...)
}

// stripComment drops a line's comment: a whole-line * or an inline ;
// or $ comment.
func stripComment(line string) string {
	if strings.HasPrefix(line, "*") {
		return ""
	}
	if i := strings.IndexAny(line, ";$"); i >= 0 {
		return line[:i]
	}
	return line
}

// numberRe is a SPICE number: a value and a scale suffix (1k, 10u,
// 2.2meg), any unit after it ignored.
var numberRe = regexp.MustCompile(`(?i)^([-+]?(?:\d+\.?\d*|\.\d+)(?:e[-+]?\d+)?)(meg|mil|[tgkmunpf])?`)

// scales are the SPICE scale suffixes.
var scales = map[string]float64{"t": 1e12, "g": 1e9, "meg": 1e6, "k": 1e3, "m": 1e-3, "mil": 25.4e-6, "u": 1e-6, "n": 1e-9, "p": 1e-12, "f": 1e-15}

// ParseNumber reads a SPICE number ("10u", "2.2meg", "1e-3").
func ParseNumber(s string) (float64, error) {
	m := numberRe.FindStringSubmatch(strings.TrimSpace(s))
	if m == nil {
		return math.NaN(), fmt.Errorf("%q isn't a number", s)
	}
	v, err := strconv.ParseFloat(m[1], 64)
	if err != nil {
		return math.NaN(), err
	}
	if m[2] != "" {
		v *= scales[strings.ToLower(m[2])]
	}
	return v, nil
}
