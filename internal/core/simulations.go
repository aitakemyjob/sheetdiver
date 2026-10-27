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

// Simulations. An agent sets up simulations of a project (any number,
// testing whatever it judges worth testing: inrush, start-up, loop
// gain, a sweep of corners), each a netlist with its variants and the
// part models it uses; SheetDiver checks the netlist, runs each variant
// with the user's ngspice, keeps the results with the project, and
// shows the agent's presentation of them in the app.

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

// SimulationSpec is a simulation as an agent sets it up.
type SimulationSpec struct {
	Name     string               `json:"name"`
	Kind     string               `json:"kind,omitempty"`
	Purpose  string               `json:"purpose,omitempty"`
	Target   string               `json:"target,omitempty"`
	Netlist  string               `json:"netlist"`
	Models   []library.SimModel   `json:"models,omitempty"`
	Variants []library.SimVariant `json:"variants,omitempty"`
}

// SaveSimulation checks a simulation and saves it to its project: the
// netlist against what may run, its models against the library, its
// variants against the netlist.
func (s *Service) SaveSimulation(ctx context.Context, project string, spec SimulationSpec, source string) (library.Simulation, error) {
	p, err := s.Lib.Project(ctx, project)
	if err != nil {
		return library.Simulation{}, err
	}
	nl, err := spice.Check(spec.Netlist)
	if err != nil {
		return library.Simulation{}, fmt.Errorf("the netlist can't run:\n%w", err)
	}
	if _, err := s.models(ctx, spec.Models); err != nil {
		return library.Simulation{}, err
	}
	seen := map[string]bool{}
	for _, v := range spec.Variants {
		key := strings.ToLower(strings.TrimSpace(v.Name))
		if key == "" || seen[key] {
			return library.Simulation{}, fmt.Errorf("each variant needs a name of its own (%q)", v.Name)
		}
		seen[key] = true
		if _, err := spice.Deck(nl, spice.Variant{Name: v.Name, Params: v.Params, Temp: v.Temp}, nil); err != nil {
			return library.Simulation{}, err
		}
	}
	sim, err := s.Lib.SaveSimulation(ctx, library.Simulation{ProjectID: p.ID, Name: spec.Name, Kind: strings.TrimSpace(spec.Kind), Purpose: strings.TrimSpace(spec.Purpose),
		Target: strings.TrimSpace(spec.Target), Netlist: spec.Netlist, Models: spec.Models, Variants: spec.Variants, Source: source})
	if err == nil {
		s.ProjectChanged(p.Name, "simulation")
	}
	return sim, err
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

// RunSimulation runs a simulation's variants (all, or those named; the
// netlist as written when it has none), one after another, and returns
// the runs.
func (s *Service) RunSimulation(ctx context.Context, project, name string, only []string) ([]library.SimRun, error) {
	p, err := s.Lib.Project(ctx, project)
	if err != nil {
		return nil, err
	}
	sim, err := s.Lib.Simulation(ctx, p.ID, name)
	if err != nil {
		return nil, err
	}
	inst, err := s.Ngspice(ctx)
	if err != nil {
		return nil, err
	}
	nl, err := spice.Check(sim.Netlist)
	if err != nil {
		return nil, err
	}
	models, err := s.models(ctx, sim.Models)
	if err != nil {
		return nil, err
	}
	variants := sim.Variants
	if len(variants) == 0 {
		variants = []library.SimVariant{{Name: "default"}}
	}
	var todo []library.SimVariant
	for _, v := range variants {
		if len(only) == 0 || slices.ContainsFunc(only, func(o string) bool { return strings.EqualFold(o, v.Name) }) {
			todo = append(todo, v)
		}
	}
	if len(todo) == 0 {
		return nil, fmt.Errorf("%s has no variant %s", sim.Name, strings.Join(only, ", "))
	}
	runs := make([]library.SimRun, 0, len(todo))
	for _, v := range todo {
		r, err := s.Lib.AddRun(ctx, sim.ID, v.Name)
		if err != nil {
			return runs, err
		}
		runs = append(runs, r)
	}
	s.ProjectChanged(p.Name, "simulation")
	for i, v := range todo {
		runs[i] = s.runOne(ctx, inst, runs[i], nl, spice.Variant{Name: v.Name, Params: v.Params, Temp: v.Temp}, models)
		s.ProjectChanged(p.Name, "simulation")
	}
	return runs, nil
}

// runOne runs a queued run through its states and records what it made.
func (s *Service) runOne(ctx context.Context, inst spice.Install, r library.SimRun, nl spice.Netlist, v spice.Variant, models []spice.Model) library.SimRun {
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
	res := spice.Runner{Install: inst}.Run(ctx, spice.Job{Dir: dir, Netlist: nl, Variant: v, Models: models})
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

// PresentSimulation saves the agent's presentation of a simulation,
// checking its charts against what the runs wrote and its targets
// against the netlist's measurements.
func (s *Service) PresentSimulation(ctx context.Context, project, name string, pres library.Presentation) (SimulationDetail, error) {
	d, err := s.SimulationDetail(ctx, project, name)
	if err != nil {
		return d, err
	}
	nl, _ := spice.Check(d.Simulation.Netlist)
	ids := spice.PlotIDs(nl.Analyses)
	var problems []error
	for _, pl := range pres.Plots {
		if !slices.Contains(ids, pl.Plot) {
			problems = append(problems, fmt.Errorf("chart %q: no plot %q (the netlist makes %s)", pl.Title, pl.Plot, strings.Join(ids, ", ")))
			continue
		}
		if vecs := d.vectors(pl.Plot); len(vecs) > 0 {
			for _, sig := range pl.Signals {
				if !slices.ContainsFunc(vecs, func(v string) bool { return strings.EqualFold(v, sig) }) {
					problems = append(problems, fmt.Errorf("chart %q: %s has no %s (it has %s)", pl.Title, pl.Plot, sig, strings.Join(vecs, ", ")))
				}
			}
		}
	}
	for _, c := range pres.Checks {
		if !slices.Contains(nl.Measures, strings.ToLower(c.Measure)) {
			problems = append(problems, fmt.Errorf("target on %s: the netlist has no .meas of that name (it has %s)", c.Measure, strings.Join(nl.Measures, ", ")))
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

// CheckResult is a target checked against a variant's latest run.
type CheckResult struct {
	library.SimCheck
	Variant string   `json:"variant"`
	Value   *float64 `json:"value,omitempty"`
	// Pass is nil when the run has no value for it.
	Pass *bool `json:"pass,omitempty"`
}

// SimulationDetail is a simulation with its runs, latest per variant
// first, and its targets checked.
type SimulationDetail struct {
	Simulation library.Simulation `json:"simulation"`
	Analyses   []spice.Analysis   `json:"analyses"`
	Measures   []string           `json:"measures"`
	// Latest is each variant's latest run; Runs every run, newest first.
	Latest  []library.SimRun `json:"latest"`
	Runs    []library.SimRun `json:"runs"`
	Checks  []CheckResult    `json:"checks"`
	project string
}

// vectors are the vector names of a plot in the latest runs.
func (d SimulationDetail) vectors(plot string) []string {
	for _, r := range d.Latest {
		for _, p := range r.Plots {
			if p.ID == plot {
				var out []string
				for _, v := range p.Vectors {
					out = append(out, v.Name)
				}
				return out
			}
		}
	}
	return nil
}

// SimulationDetail reads a simulation with its runs and checks.
func (s *Service) SimulationDetail(ctx context.Context, project, name string) (SimulationDetail, error) {
	p, err := s.Lib.Project(ctx, project)
	if err != nil {
		return SimulationDetail{}, err
	}
	sim, err := s.Lib.Simulation(ctx, p.ID, name)
	if err != nil {
		return SimulationDetail{}, err
	}
	d := SimulationDetail{Simulation: sim, project: p.Name, Latest: []library.SimRun{}, Checks: []CheckResult{}}
	nl, _ := spice.Check(sim.Netlist)
	d.Analyses, d.Measures = nl.Analyses, nl.Measures
	if d.Runs, err = s.Lib.Runs(ctx, sim.ID); err != nil {
		return d, err
	}
	seen := map[string]bool{}
	for _, r := range d.Runs {
		if !seen[strings.ToLower(r.Variant)] {
			seen[strings.ToLower(r.Variant)] = true
			d.Latest = append(d.Latest, r)
		}
	}
	// In the variants' order, as the agent set them up.
	order := map[string]int{}
	for i, v := range sim.Variants {
		order[strings.ToLower(v.Name)] = i
	}
	slices.SortStableFunc(d.Latest, func(a, b library.SimRun) int {
		return order[strings.ToLower(a.Variant)] - order[strings.ToLower(b.Variant)]
	})
	if sim.Presentation != nil {
		for _, r := range d.Latest {
			for _, c := range sim.Presentation.Checks {
				cr := CheckResult{SimCheck: c, Variant: r.Variant}
				for _, m := range r.Measures {
					if strings.EqualFold(m.Name, c.Measure) && m.Value != nil {
						v := *m.Value
						pass := (c.Min == nil || v >= *c.Min) && (c.Max == nil || v <= *c.Max)
						cr.Value, cr.Pass = &v, &pass
					}
				}
				d.Checks = append(d.Checks, cr)
			}
		}
	}
	return d, nil
}

// SimulationSummary is a simulation in a project's list: what it is, its
// variants' latest states, and how its targets fare.
type SimulationSummary struct {
	Name    string            `json:"name"`
	Kind    string            `json:"kind,omitempty"`
	Purpose string            `json:"purpose,omitempty"`
	Target  string            `json:"target,omitempty"`
	Updated string            `json:"updated"`
	States  map[string]string `json:"states"`
	Pass    int               `json:"pass"`
	Fail    int               `json:"fail"`
	// Presented says the agent wrote up the results.
	Presented bool `json:"presented"`
}

// Simulations lists a project's simulations.
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
		sum := SimulationSummary{Name: sim.Name, Kind: sim.Kind, Purpose: sim.Purpose, Target: sim.Target, Updated: sim.Updated, States: map[string]string{}, Presented: sim.Presentation != nil}
		for _, r := range d.Latest {
			sum.States[r.Variant] = r.State
		}
		for _, c := range d.Checks {
			switch {
			case c.Pass == nil:
			case *c.Pass:
				sum.Pass++
			default:
				sum.Fail++
			}
		}
		out = append(out, sum)
	}
	return out, nil
}

// DeleteSimulation removes a simulation and its runs.
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

// SimulationData is a chart's data: the series of a run's plot, each
// against the plot's scale.
type SimulationData struct {
	Run     int64          `json:"run"`
	Variant string         `json:"variant"`
	Plot    string         `json:"plot"`
	XName   string         `json:"xName"`
	XUnit   string         `json:"xUnit"`
	Series  []spice.Series `json:"series"`
}

// SimulationData reads signals of a run's plot from its results file, at
// most max points each (a complex signal as its magnitude, or its phase
// when part is "phase").
func (s *Service) SimulationData(ctx context.Context, runID int64, plot string, signals []string, part string, max int) (SimulationData, error) {
	r, err := s.Lib.Run(ctx, runID)
	if err != nil {
		return SimulationData{}, err
	}
	out := SimulationData{Run: r.ID, Variant: r.Variant, Plot: plot, Series: []spice.Series{}}
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
