package core

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/aitakemyjob/sheetdiver/internal/library"
	"github.com/aitakemyjob/sheetdiver/internal/spice"
)

// Simulations. An agent sets up tests of a project (any number, each a
// question: inrush, start-up, loop gain, a load step) and tries each in
// variations as the schematic changes or alternatives come up: a
// netlist with what changed, the part models it uses and the conditions
// it runs in (corners, temperatures, loads). SheetDiver checks each
// netlist, runs each condition with the user's ngspice, keeps every
// variation's results with the test, and shows the agent's write-up of
// how the test fared across them.

// simSlots bounds how many runs go at once across the app.
var simSlots = make(chan struct{}, 2)

// Ngspice finds the ngspice simulations run with: the path in Settings,
// or where it's installed.
func (s *Service) Ngspice(ctx context.Context) (spice.Install, error) {
	return spice.Locate(ctx, s.Config.Get().Settings.NgspicePath)
}

// RecordSpiceModel saves a part's SPICE model, checked to define what
// its name says.
func (s *Service) RecordSpiceModel(ctx context.Context, part string, m library.SpiceModel, source string) (library.SpiceModel, error) {
	res, err := s.Lib.Resolve(ctx, part)
	if err != nil {
		return m, err
	}
	defines := regexp.MustCompile(`(?im)^\s*\.(subckt|model)\s+` + regexp.QuoteMeta(strings.TrimSpace(m.Name)) + `\b`)
	if !defines.MatchString(m.Text) {
		return m, fmt.Errorf("the model's text has no .subckt or .model named %s", m.Name)
	}
	if strings.Contains(strings.ToLower(m.Text), ".control") {
		return m, errors.New("a model can't have a .control block")
	}
	m.PartID, m.Source = res.Part.ID, source
	m, err = s.Lib.SetSpiceModel(ctx, m)
	if err == nil {
		s.emit(EvLibrary, map[string]any{"action": "model", "part": res.Part.MPN})
	}
	return m, err
}

// TestSpec is a test as an agent names it: what it checks.
type TestSpec struct {
	Name    string `json:"name"`
	Kind    string `json:"kind,omitempty"`
	Purpose string `json:"purpose,omitempty"`
	Target  string `json:"target,omitempty"`
}

// VariationSpec is a try of a test: its netlist, what changed, the
// models it includes and the conditions it runs in.
type VariationSpec struct {
	Name       string                 `json:"name"`
	Change     string                 `json:"change,omitempty"`
	Netlist    string                 `json:"netlist"`
	Models     []library.SimModel     `json:"models,omitempty"`
	Conditions []library.SimCondition `json:"conditions,omitempty"`
}

// SaveVariation checks a variation and adds it to its test, creating the
// test if it's new: the netlist against what may run, its models against
// the library, its conditions against the netlist.
func (s *Service) SaveVariation(ctx context.Context, project string, test TestSpec, v VariationSpec, source string) (library.Simulation, library.Variation, error) {
	p, err := s.Lib.Project(ctx, project)
	if err != nil {
		return library.Simulation{}, library.Variation{}, err
	}
	nl, err := spice.Check(v.Netlist)
	if err != nil {
		return library.Simulation{}, library.Variation{}, fmt.Errorf("the netlist can't run:\n%w", err)
	}
	if _, err := s.models(ctx, v.Models); err != nil {
		return library.Simulation{}, library.Variation{}, err
	}
	seen := map[string]bool{}
	for _, c := range v.Conditions {
		key := strings.ToLower(strings.TrimSpace(c.Name))
		if key == "" || seen[key] {
			return library.Simulation{}, library.Variation{}, fmt.Errorf("each condition needs a name of its own (%q)", c.Name)
		}
		seen[key] = true
		if _, err := spice.Deck(nl, spice.Condition{Name: c.Name, Params: c.Params, Temp: c.Temp}, nil); err != nil {
			return library.Simulation{}, library.Variation{}, err
		}
	}
	sim, err := s.Lib.SaveSimulation(ctx, library.Simulation{ProjectID: p.ID, Name: test.Name, Kind: strings.TrimSpace(test.Kind), Purpose: strings.TrimSpace(test.Purpose), Target: strings.TrimSpace(test.Target), Source: source})
	if err != nil {
		return sim, library.Variation{}, err
	}
	saved, err := s.Lib.SaveVariation(ctx, library.Variation{SimulationID: sim.ID, Name: v.Name, Change: strings.TrimSpace(v.Change), Netlist: v.Netlist, Models: v.Models, Conditions: v.Conditions, Source: source})
	if err == nil {
		s.ProjectChanged(p.Name, "simulation")
	}
	return sim, saved, err
}

// unsafeFile is what a model's file name can't hold.
var unsafeFile = regexp.MustCompile(`[^A-Za-z0-9_.-]+`)

// models are the model files a simulation includes, from the library.
func (s *Service) models(ctx context.Context, want []library.SimModel) ([]spice.Model, error) {
	var out []spice.Model
	for i, m := range want {
		res, err := s.Lib.Resolve(ctx, m.Part)
		if err != nil {
			return nil, fmt.Errorf("model of %s: %w", m.Part, err)
		}
		all, err := s.Lib.SpiceModels(ctx, res.Part.ID)
		if err != nil {
			return nil, err
		}
		found := false
		for _, sm := range all {
			if strings.EqualFold(sm.Name, m.Name) {
				out = append(out, spice.Model{File: fmt.Sprintf("model%d_%s.lib", i+1, unsafeFile.ReplaceAllString(sm.Name, "_")), Text: sm.Text, PSpice: sm.Dialect == "pspice"})
				found = true
			}
		}
		if !found {
			var names []string
			for _, sm := range all {
				names = append(names, sm.Name)
			}
			return nil, fmt.Errorf("%s has no model %s (it has: %s); record it with record_spice_model", res.Part.MPN, m.Name, firstNonEmpty(strings.Join(names, ", "), "none"))
		}
	}
	return out, nil
}

// RunVariation runs a test's variation (its latest when none is named)
// in its conditions (all, or those named; the netlist as written when it
// has none), one after another, and returns the runs.
func (s *Service) RunVariation(ctx context.Context, project, test, variation string, only []string) ([]library.SimRun, error) {
	p, err := s.Lib.Project(ctx, project)
	if err != nil {
		return nil, err
	}
	sim, err := s.Lib.Simulation(ctx, p.ID, test)
	if err != nil {
		return nil, err
	}
	v, err := s.variation(ctx, sim, variation)
	if err != nil {
		return nil, err
	}
	inst, err := s.Ngspice(ctx)
	if err != nil {
		return nil, err
	}
	nl, err := spice.Check(v.Netlist)
	if err != nil {
		return nil, err
	}
	models, err := s.models(ctx, v.Models)
	if err != nil {
		return nil, err
	}
	conds := conditionsOf(v)
	var todo []library.SimCondition
	for _, c := range conds {
		if len(only) == 0 || slices.ContainsFunc(only, func(o string) bool { return strings.EqualFold(o, c.Name) }) {
			todo = append(todo, c)
		}
	}
	if len(todo) == 0 {
		return nil, fmt.Errorf("%s has no condition %s", v.Name, strings.Join(only, ", "))
	}
	runs := make([]library.SimRun, 0, len(todo))
	for _, c := range todo {
		r, err := s.Lib.AddRun(ctx, v.ID, c.Name)
		if err != nil {
			return runs, err
		}
		r.Variation = v.Name
		runs = append(runs, r)
	}
	s.ProjectChanged(p.Name, "simulation")
	for i, c := range todo {
		runs[i] = s.runOne(ctx, inst, runs[i], nl, spice.Condition{Name: c.Name, Params: c.Params, Temp: c.Temp}, models)
		s.ProjectChanged(p.Name, "simulation")
	}
	return runs, nil
}

// variation finds a test's variation by name, or its latest.
func (s *Service) variation(ctx context.Context, sim library.Simulation, name string) (library.Variation, error) {
	if strings.TrimSpace(name) != "" {
		return s.Lib.Variation(ctx, sim.ID, name)
	}
	vs, err := s.Lib.Variations(ctx, sim.ID)
	if err != nil {
		return library.Variation{}, err
	}
	if len(vs) == 0 {
		return library.Variation{}, fmt.Errorf("%s has no variation yet: add one with simulate", sim.Name)
	}
	return vs[len(vs)-1], nil
}

// conditionsOf are the conditions a variation runs in: its own, or the
// netlist as written.
func conditionsOf(v library.Variation) []library.SimCondition {
	if len(v.Conditions) == 0 {
		return []library.SimCondition{{Name: "as written"}}
	}
	return v.Conditions
}

// runOne runs a queued run through its states and records what it made.
func (s *Service) runOne(ctx context.Context, inst spice.Install, r library.SimRun, nl spice.Netlist, v spice.Condition, models []spice.Model) library.SimRun {
	move := func(next spice.State) bool {
		st, err := spice.State(r.State).To(next)
		if err != nil {
			r.Error = err.Error()
			return false
		}
		r.State = string(st)
		return true
	}
	select {
	case simSlots <- struct{}{}:
		defer func() { <-simSlots }()
	case <-ctx.Done():
		move(spice.Cancelled)
		_ = s.Lib.SaveRun(context.WithoutCancel(ctx), r)
		return r
	}
	move(spice.Running)
	r.Ngspice, r.Started = inst.Version, time.Now().UTC().Format(time.RFC3339)
	_ = s.Lib.SaveRun(ctx, r)
	dir := s.Lib.RunDir(r.ID)
	_ = os.RemoveAll(dir)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		r.Error = err.Error()
		move(spice.Failed)
		_ = s.Lib.SaveRun(context.WithoutCancel(ctx), r)
		return r
	}
	res := spice.Runner{Install: inst}.Run(ctx, spice.Job{Dir: dir, Netlist: nl, Condition: v, Models: models})
	sum := sha256.Sum256([]byte(res.Deck))
	r.DeckSHA, r.TookMS, r.Log, r.Problems = hex.EncodeToString(sum[:8]), res.Took.Milliseconds(), truncate(res.Log, 64<<10), res.Problems
	if r.Problems == nil {
		r.Problems = []string{}
	}
	r.Finished = time.Now().UTC().Format(time.RFC3339)
	for _, m := range res.Measures {
		r.Measures = append(r.Measures, library.RunMeasure{Name: m.Name, Value: m.Value, At: m.At, Error: m.Error})
	}
	for _, p := range res.Plots {
		rp := library.RunPlot{ID: p.ID, Name: p.Name, Complex: p.Complex, Points: p.Points}
		for _, v := range p.Vectors {
			rp.Vectors = append(rp.Vectors, library.RunVector{Name: v.Name, Kind: v.Kind})
		}
		r.Plots = append(r.Plots, rp)
	}
	switch {
	case errors.Is(res.Err, context.Canceled):
		move(spice.Cancelled)
	case res.Err != nil:
		r.Error = res.Err.Error()
		move(spice.Failed)
	default:
		move(spice.Done)
	}
	_ = s.Lib.SaveRun(context.WithoutCancel(ctx), r)
	return r
}

// truncate keeps the end of a long text.
func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return "…" + s[len(s)-max:]
}

// PresentSimulation saves the agent's write-up of a test, checking its
// charts against what the variations' runs wrote, its targets against
// their measurements, and its notes and charts against the variations
// and conditions they name.
func (s *Service) PresentSimulation(ctx context.Context, project, name string, pres library.Presentation) (SimulationDetail, error) {
	d, err := s.SimulationDetail(ctx, project, name)
	if err != nil {
		return d, err
	}
	var ids, measures, variations, conditions []string
	vectors := map[string][]string{}
	for _, v := range d.Variations {
		variations = append(variations, v.Variation.Name)
		ids = appendNew(ids, spice.PlotIDs(v.Analyses)...)
		measures = appendNew(measures, v.Measures...)
		for _, c := range conditionsOf(v.Variation) {
			conditions = appendNew(conditions, c.Name)
		}
		for _, r := range v.Latest {
			for _, pl := range r.Plots {
				for _, vec := range pl.Vectors {
					vectors[pl.ID] = appendNew(vectors[pl.ID], vec.Name)
				}
			}
		}
	}
	has := func(list []string, x string) bool {
		return slices.ContainsFunc(list, func(y string) bool { return strings.EqualFold(x, y) })
	}
	var problems []error
	for _, pl := range pres.Plots {
		if !slices.Contains(ids, pl.Plot) {
			problems = append(problems, fmt.Errorf("chart %q: no plot %q (the variations make %s)", pl.Title, pl.Plot, strings.Join(ids, ", ")))
			continue
		}
		for _, sig := range pl.Signals {
			if len(vectors[pl.Plot]) > 0 && !has(vectors[pl.Plot], sig) {
				problems = append(problems, fmt.Errorf("chart %q: %s has no %s (it has %s)", pl.Title, pl.Plot, sig, strings.Join(vectors[pl.Plot], ", ")))
			}
		}
		for _, v := range pl.Variations {
			if !has(variations, v) {
				problems = append(problems, fmt.Errorf("chart %q: no variation %q (there are %s)", pl.Title, v, strings.Join(variations, ", ")))
			}
		}
		for _, c := range pl.Conditions {
			if !has(conditions, c) {
				problems = append(problems, fmt.Errorf("chart %q: no condition %q (there are %s)", pl.Title, c, strings.Join(conditions, ", ")))
			}
		}
	}
	for _, c := range pres.Checks {
		if !has(measures, c.Measure) {
			problems = append(problems, fmt.Errorf("target on %s: no variation has a .meas of that name (they have %s)", c.Measure, strings.Join(measures, ", ")))
		}
	}
	for _, n := range pres.Notes {
		if !has(variations, n.Variation) {
			problems = append(problems, fmt.Errorf("note on %q: no such variation (there are %s)", n.Variation, strings.Join(variations, ", ")))
		}
	}
	if err := errors.Join(problems...); err != nil {
		return d, err
	}
	if err := s.Lib.SetPresentation(ctx, d.Simulation.ID, pres); err != nil {
		return d, err
	}
	s.ProjectChanged(d.project, "simulation")
	return s.SimulationDetail(ctx, project, name)
}

// appendNew adds what a list doesn't have yet (ignoring case).
func appendNew(list []string, xs ...string) []string {
	for _, x := range xs {
		if !slices.ContainsFunc(list, func(y string) bool { return strings.EqualFold(x, y) }) {
			list = append(list, x)
		}
	}
	return list
}

// CheckResult is a target checked against a variation's latest run in a
// condition.
type CheckResult struct {
	library.SimCheck
	Variation string   `json:"variation"`
	Condition string   `json:"condition"`
	Value     *float64 `json:"value,omitempty"`
	// Pass is nil when the run has no value for it.
	Pass *bool `json:"pass,omitempty"`
}

// VariationDetail is a variation with its analyses, measurements, each
// condition's latest run and its targets checked.
type VariationDetail struct {
	Variation library.Variation `json:"variation"`
	Analyses  []spice.Analysis  `json:"analyses"`
	Measures  []string          `json:"measures"`
	Latest    []library.SimRun  `json:"latest"`
	Checks    []CheckResult     `json:"checks"`
}

// SimulationDetail is a test with its variations, in the order they
// were added, and every run, newest first.
type SimulationDetail struct {
	Simulation library.Simulation `json:"simulation"`
	Variations []VariationDetail  `json:"variations"`
	Runs       []library.SimRun   `json:"runs"`
	project    string
}

// SimulationDetail reads a test with its variations, runs and checks.
func (s *Service) SimulationDetail(ctx context.Context, project, name string) (SimulationDetail, error) {
	p, err := s.Lib.Project(ctx, project)
	if err != nil {
		return SimulationDetail{}, err
	}
	sim, err := s.Lib.Simulation(ctx, p.ID, name)
	if err != nil {
		return SimulationDetail{}, err
	}
	d := SimulationDetail{Simulation: sim, project: p.Name, Variations: []VariationDetail{}}
	if d.Runs, err = s.Lib.Runs(ctx, sim.ID); err != nil {
		return d, err
	}
	vs, err := s.Lib.Variations(ctx, sim.ID)
	if err != nil {
		return d, err
	}
	for _, v := range vs {
		vd := VariationDetail{Variation: v, Latest: []library.SimRun{}, Checks: []CheckResult{}}
		nl, _ := spice.Check(v.Netlist)
		vd.Analyses, vd.Measures = nl.Analyses, nl.Measures
		// Each condition's latest run, in the conditions' order.
		for _, c := range conditionsOf(v) {
			for _, r := range d.Runs {
				if r.VariationID == v.ID && strings.EqualFold(r.Condition, c.Name) {
					vd.Latest = append(vd.Latest, r)
					break
				}
			}
		}
		if sim.Presentation != nil {
			for _, r := range vd.Latest {
				for _, c := range sim.Presentation.Checks {
					vd.Checks = append(vd.Checks, check(c, v.Name, r))
				}
			}
		}
		d.Variations = append(d.Variations, vd)
	}
	return d, nil
}

// check is a target checked against a run.
func check(c library.SimCheck, variation string, r library.SimRun) CheckResult {
	cr := CheckResult{SimCheck: c, Variation: variation, Condition: r.Condition}
	for _, m := range r.Measures {
		if strings.EqualFold(m.Name, c.Measure) && m.Value != nil {
			v := *m.Value
			pass := (c.Min == nil || v >= *c.Min) && (c.Max == nil || v <= *c.Max)
			cr.Value, cr.Pass = &v, &pass
		}
	}
	return cr
}

// SimulationSummary is a test in a project's list: what it is, how many
// variations it has, the latest one's states by condition, and how its
// targets fare there.
type SimulationSummary struct {
	Name       string            `json:"name"`
	Kind       string            `json:"kind,omitempty"`
	Purpose    string            `json:"purpose,omitempty"`
	Target     string            `json:"target,omitempty"`
	Updated    string            `json:"updated"`
	Variations int               `json:"variations"`
	Latest     string            `json:"latest,omitempty"`
	States     map[string]string `json:"states"`
	Pass       int               `json:"pass"`
	Fail       int               `json:"fail"`
	// Presented says the agent wrote up the results.
	Presented bool `json:"presented"`
}

// Simulations lists a project's tests.
func (s *Service) Simulations(ctx context.Context, project string) ([]SimulationSummary, error) {
	p, err := s.Lib.Project(ctx, project)
	if err != nil {
		return nil, err
	}
	sims, err := s.Lib.Simulations(ctx, p.ID)
	if err != nil {
		return nil, err
	}
	out := []SimulationSummary{}
	for _, sim := range sims {
		d, err := s.SimulationDetail(ctx, p.Name, sim.Name)
		if err != nil {
			return nil, err
		}
		sum := SimulationSummary{Name: sim.Name, Kind: sim.Kind, Purpose: sim.Purpose, Target: sim.Target, Updated: sim.Updated, Variations: len(d.Variations), States: map[string]string{}, Presented: sim.Presentation != nil}
		if n := len(d.Variations); n > 0 {
			last := d.Variations[n-1]
			sum.Latest = last.Variation.Name
			for _, r := range last.Latest {
				sum.States[r.Condition] = r.State
			}
			for _, c := range last.Checks {
				switch {
				case c.Pass == nil:
				case *c.Pass:
					sum.Pass++
				default:
					sum.Fail++
				}
			}
		}
		out = append(out, sum)
	}
	return out, nil
}

// DeleteSimulation removes a test with its variations and runs.
func (s *Service) DeleteSimulation(ctx context.Context, project, name string) error {
	p, err := s.Lib.Project(ctx, project)
	if err != nil {
		return err
	}
	sim, err := s.Lib.Simulation(ctx, p.ID, name)
	if err != nil {
		return err
	}
	if err := s.Lib.DeleteSimulation(ctx, sim.ID); err != nil {
		return err
	}
	s.ProjectChanged(p.Name, "simulation")
	return nil
}

// DeleteVariation removes one variation of a test with its runs.
func (s *Service) DeleteVariation(ctx context.Context, project, test, variation string) error {
	p, err := s.Lib.Project(ctx, project)
	if err != nil {
		return err
	}
	sim, err := s.Lib.Simulation(ctx, p.ID, test)
	if err != nil {
		return err
	}
	v, err := s.Lib.Variation(ctx, sim.ID, variation)
	if err != nil {
		return err
	}
	if err := s.Lib.DeleteVariation(ctx, v.ID); err != nil {
		return err
	}
	s.ProjectChanged(p.Name, "simulation")
	return nil
}

// SimulationData is a chart's data: the series of a run's plot, each
// against the plot's scale.
type SimulationData struct {
	Run       int64          `json:"run"`
	Variation string         `json:"variation"`
	Condition string         `json:"condition"`
	Plot      string         `json:"plot"`
	XName     string         `json:"xName"`
	XUnit     string         `json:"xUnit"`
	Series    []spice.Series `json:"series"`
}

// SimulationData reads signals of a run's plot from its results file, at
// most max points each (a complex signal as its magnitude, or its phase
// when part is "phase").
func (s *Service) SimulationData(ctx context.Context, runID int64, plot string, signals []string, part string, max int) (SimulationData, error) {
	r, err := s.Lib.Run(ctx, runID)
	if err != nil {
		return SimulationData{}, err
	}
	out := SimulationData{Run: r.ID, Variation: r.Variation, Condition: r.Condition, Plot: plot, Series: []spice.Series{}}
	data, err := os.ReadFile(filepath.Join(s.Lib.RunDir(r.ID), spice.RawFile))
	if err != nil {
		return out, fmt.Errorf("run %d has no results file (state %s)", r.ID, r.State)
	}
	plots, err := spice.ReadRaw(data)
	if err != nil {
		return out, err
	}
	for i, p := range plots {
		if i < len(r.Plots) {
			p.ID = r.Plots[i].ID
		}
		if p.ID != plot {
			continue
		}
		scale := p.Scale()
		out.XName = scale.Name
		out.XUnit = map[string]string{"time": "s", "frequency": "Hz", "voltage": "V", "current": "A"}[scale.Kind]
		if max <= 0 {
			max = 2000
		}
		for _, sig := range signals {
			ser, err := p.Series(sig, part, max)
			if err != nil {
				return out, err
			}
			out.Series = append(out.Series, ser)
		}
		return out, nil
	}
	return out, fmt.Errorf("run %d has no plot %s", r.ID, plot)
}
