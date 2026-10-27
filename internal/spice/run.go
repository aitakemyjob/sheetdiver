package spice

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// Measure is a .meas result: its value (and where it was taken, for
// a measurement at a point), or why it failed.
type Measure struct {
	Name  string   `json:"name"`
	Value *float64 `json:"value,omitempty"`
	At    *float64 `json:"at,omitempty"`
	Error string   `json:"error,omitempty"`
}

// measureLineRe is a measurement's result as ngspice logs it:
// "vmax = 2.368299e+00 at= 3.000000e-03", or "trise = 1.2e-03 targ= … trig= …".
var measureLineRe = regexp.MustCompile(`(?i)^\s*([\w.]+)\s*=\s*([-+]?[\d.]+(?:e[-+]?\d+)?)(?:\s+at\s*=\s*([-+]?[\d.]+(?:e[-+]?\d+)?))?`)

// failedRe is a measurement that failed: ngspice's message names it.
var failedRe = regexp.MustCompile(`(?i)^\s*\.meas\w*\s+\w+\s+([\w.]+)\b.*\bfailed!?`)

// ParseMeasures reads the measurements' results from ngspice's log, in
// the netlist's order; one with no result is marked missing.
func ParseMeasures(log string, names []string) []Measure {
	values := map[string]Measure{}
	lines := strings.Split(log, "\n")
	for i, line := range lines {
		if m := failedRe.FindStringSubmatch(line); m != nil {
			why := "failed"
			if i > 0 && strings.Contains(strings.ToLower(lines[i-1]), "error") {
				why = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(lines[i-1]), "Error:"))
			}
			values[strings.ToLower(m[1])] = Measure{Name: strings.ToLower(m[1]), Error: why}
			continue
		}
		m := measureLineRe.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		name := strings.ToLower(m[1])
		if _, seen := values[name]; seen {
			continue
		}
		v, err := strconv.ParseFloat(m[2], 64)
		if err != nil {
			continue
		}
		ms := Measure{Name: name, Value: &v}
		if m[3] != "" {
			if at, err := strconv.ParseFloat(m[3], 64); err == nil {
				ms.At = &at
			}
		}
		values[name] = ms
	}
	out := make([]Measure, 0, len(names))
	for _, n := range names {
		if m, ok := values[n]; ok {
			out = append(out, m)
		} else {
			out = append(out, Measure{Name: n, Error: "no result in ngspice's log"})
		}
	}
	return out
}

// Model is a part model a run includes: the file it's written to in the
// run's folder and its text.
type Model struct {
	File string
	Text string
	// PSpice says it's written for PSpice: ngspice reads it in its
	// PSpice compatibility mode.
	PSpice bool
}

// Job is a run to do: the folder it runs in (its own, empty), the
// netlist and variant, and the models it includes.
type Job struct {
	Dir       string
	Netlist   Netlist
	Condition Condition
	Models    []Model
}

// Result is what a run produced: the deck it ran, ngspice's log, the
// plots, the measurements, and why it failed if it did.
type Result struct {
	Deck     string
	Log      string
	Plots    []Plot
	Measures []Measure
	// Problems are the errors and warnings ngspice logged.
	Problems []string
	Err      error
	Took     time.Duration
}

// Runner runs jobs with an ngspice install, within limits.
type Runner struct {
	Install Install
	// Timeout is how long a run may take (default a minute).
	Timeout time.Duration
	// MaxRaw is how large the results file may get (default 200 MB).
	MaxRaw int64
}

// problemRe is a line of ngspice's log worth passing on.
var problemRe = regexp.MustCompile(`(?i)^\s*(error|warning)\b`)

// Run runs a job: writes its deck and models into its folder, runs
// ngspice there in batch mode, and reads back the results. A run that
// produced results succeeds even if ngspice also logged errors (a
// failed measurement); one that produced none fails with its log.
func (rn Runner) Run(ctx context.Context, job Job) Result {
	start := time.Now()
	var res Result
	timeout, maxRaw := rn.Timeout, rn.MaxRaw
	if timeout <= 0 {
		timeout = time.Minute
	}
	if maxRaw <= 0 {
		maxRaw = 200 << 20
	}
	includes := make([]string, 0, len(job.Models))
	pspice := false
	for _, m := range job.Models {
		if filepath.Base(m.File) != m.File || m.File == "" {
			res.Err = fmt.Errorf("model file %q must be a plain name", m.File)
			return res
		}
		if err := os.WriteFile(filepath.Join(job.Dir, m.File), []byte(m.Text), 0o600); err != nil {
			res.Err = err
			return res
		}
		includes = append(includes, m.File)
		pspice = pspice || m.PSpice
	}
	deck, err := Deck(job.Netlist, job.Condition, includes)
	if err != nil {
		res.Err = err
		return res
	}
	res.Deck = deck
	// ngspice reads .spiceinit from the folder it runs in: ours sets
	// PSpice compatibility for vendor models, nothing else.
	init := "* SheetDiver\n"
	if pspice {
		init += "set ngbehavior=ps\n"
	}
	for name, text := range map[string]string{"deck.cir": deck, ".spiceinit": init} {
		if err := os.WriteFile(filepath.Join(job.Dir, name), []byte(text), 0o600); err != nil {
			res.Err = err
			return res
		}
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, rn.Install.Path, "-b", "-o", "run.log", "deck.cir") // #nosec G204 -- the user's ngspice on SheetDiver's own deck
	cmd.Dir = job.Dir
	// Its own home: no user .spiceinit runs with the deck.
	cmd.Env = append(os.Environ(), "HOME="+job.Dir, "USERPROFILE="+job.Dir, "SPICE_ASCIIRAWFILE=0")
	out, runErr := cmd.CombinedOutput()
	res.Took = time.Since(start)
	logText, _ := os.ReadFile(filepath.Join(job.Dir, "run.log"))
	res.Log = string(logText)
	if len(res.Log) == 0 {
		res.Log = string(out)
	}
	for _, l := range strings.Split(res.Log, "\n") {
		if problemRe.MatchString(l) && len(res.Problems) < 50 {
			res.Problems = append(res.Problems, strings.TrimSpace(l))
		}
	}
	res.Measures = ParseMeasures(res.Log, job.Netlist.Measures)
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		res.Err = fmt.Errorf("ngspice took longer than %s: shorten the analysis or loosen its step", timeout)
		return res
	}
	if ctx.Err() != nil {
		res.Err = ctx.Err()
		return res
	}
	raw := filepath.Join(job.Dir, RawFile)
	st, err := os.Stat(raw)
	if err != nil {
		res.Err = fmt.Errorf("ngspice wrote no results (%v): %s", runErr, lastLines(res.Problems, res.Log))
		return res
	}
	if st.Size() > maxRaw {
		res.Err = fmt.Errorf("the results are %d MB: at most %d MB (fewer points or saved vectors)", st.Size()>>20, maxRaw>>20)
		return res
	}
	data, err := os.ReadFile(raw)
	if err != nil {
		res.Err = err
		return res
	}
	if res.Plots, err = ReadRaw(data); err != nil {
		res.Err = fmt.Errorf("reading the results: %w", err)
	}
	ids := PlotIDs(job.Netlist.Analyses)
	for i := range res.Plots {
		if i < len(ids) {
			res.Plots[i].ID = ids[i]
		}
	}
	if len(res.Plots) == 0 && res.Err == nil {
		res.Err = fmt.Errorf("ngspice wrote no plots: %s", lastLines(res.Problems, res.Log))
	}
	return res
}

// lastLines is what to say about a failed run: its errors, else the
// end of its log.
func lastLines(problems []string, log string) string {
	if len(problems) > 0 {
		return strings.Join(problems, "; ")
	}
	lines := strings.Split(strings.TrimSpace(log), "\n")
	if len(lines) > 8 {
		lines = lines[len(lines)-8:]
	}
	return strings.Join(lines, " | ")
}
