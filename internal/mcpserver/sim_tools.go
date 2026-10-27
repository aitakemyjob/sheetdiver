package mcpserver

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/aitakemyjob/sheetdiver/internal/core"
	"github.com/aitakemyjob/sheetdiver/internal/library"
)

// Simulation tools: the agent decides what to simulate (any number of
// simulations of a project, each with its variants), SheetDiver checks
// and runs them with the user's ngspice and keeps the results; the agent
// then presents them, and the app shows that presentation.

type spiceModelArgs struct {
	Part    string `json:"part" jsonschema:"the part the model is of (MPN or orderable code)"`
	Name    string `json:"name" jsonschema:"the .subckt or .model name the text defines"`
	Dialect string `json:"dialect,omitempty" jsonschema:"spice (default) or pspice: vendor PSpice models run in ngspice's PSpice compatibility mode"`
	Text    string `json:"text" jsonschema:"the model's text: the .subckt … .ends or .model lines"`
	Notes   string `json:"notes,omitempty" jsonschema:"where it's from (vendor download, written from the datasheet's figures) and what it leaves out"`
}

type getModelsArgs struct {
	Part string `json:"part"`
}

type simulateArgs struct {
	Project  string               `json:"project,omitempty"`
	Name     string               `json:"name" jsonschema:"what it checks, unique in the project: \"VBUS inrush on hot-plug\", \"3V3 load step\", \"buck loop gain\""`
	Kind     string               `json:"kind,omitempty" jsonschema:"what sort of test, in your words: inrush, start-up, load step, loop gain, PSRR, corners, Monte Carlo, thermal…"`
	Purpose  string               `json:"purpose,omitempty" jsonschema:"the question it answers and why it matters"`
	Target   string               `json:"target,omitempty" jsonschema:"the designator of the part it tests on its own (a component test bench); empty for the board"`
	Netlist  string               `json:"netlist" jsonschema:"the SPICE netlist: title line, circuit, .param, .model/.subckt, analyses (.tran .ac .dc .op .noise .tf .pz .sens .disto) and .meas lines. No .control, .include or file paths: SheetDiver runs it and includes models"`
	Models   []library.SimModel   `json:"models,omitempty" jsonschema:"part models to include, recorded with record_spice_model: {part, name}"`
	Variants []library.SimVariant `json:"variants,omitempty" jsonschema:"runs of it with other parameter values or temperature: {name, params: {rload: \"3.3\"}, temp: 85}; none runs the netlist as written"`
	Run      *bool                `json:"run,omitempty" jsonschema:"run it now (default true)"`
}

type runSimArgs struct {
	Project  string   `json:"project,omitempty"`
	Name     string   `json:"name"`
	Variants []string `json:"variants,omitempty" jsonschema:"which variants to run (default all)"`
}

type presentArgs struct {
	Project     string             `json:"project,omitempty"`
	Name        string             `json:"name" jsonschema:"the simulation"`
	Summary     string             `json:"summary" jsonschema:"what was simulated and what it showed (markdown)"`
	Plots       []library.SimPlot  `json:"plots,omitempty" jsonschema:"charts to show: {title, plot: tran1/ac1/dc1…, signals: [v(out), i(v1)], variants (default all, overlaid), part: mag or phase for AC, xLog, yLabel, note}"`
	Checks      []library.SimCheck `json:"checks,omitempty" jsonschema:"targets for .meas results: {measure, min, max, unit, why}; each variant passes or fails"`
	Conclusions string             `json:"conclusions,omitempty" jsonschema:"what to do about it (markdown)"`
	StandIns    []string           `json:"stand_ins,omitempty" jsonschema:"parts modelled by idealised stand-ins rather than their real models, and how"`
}

type simNameArgs struct {
	Project string `json:"project,omitempty"`
	Name    string `json:"name"`
}

type simListArgs struct {
	Project string `json:"project,omitempty"`
}

type simDataArgs struct {
	Run     int64    `json:"run" jsonschema:"the run's id (get_simulation lists them)"`
	Plot    string   `json:"plot" jsonschema:"tran1, ac1, dc1…"`
	Signals []string `json:"signals"`
	Part    string   `json:"part,omitempty" jsonschema:"for AC: mag (dB, default) or phase (degrees)"`
	Points  int      `json:"points,omitempty" jsonschema:"how many points per signal (default 40, at most 2000), keeping peaks and edges"`
}

// registerSimulation adds the simulation tools.
func (s *Server) registerSimulation() {
	c := s.core

	addTool(s, &mcp.Tool{Name: "ngspice_status", Description: "Whether simulations can run: the ngspice SheetDiver uses (from Settings, the PATH or its usual install place) and its version, or how to install it.", Annotations: readOnly("ngspice status")},
		read, func(struct{}) string { return "ngspice status" }, func(ctx context.Context, _ struct{}) (result, error) {
			inst, err := c.Ngspice(ctx)
			if err != nil {
				return text("Simulations can't run: " + err.Error()), nil
			}
			return text(fmt.Sprintf("ngspice %s at %s (found in %s). Simulations run in batch mode, one folder per run, with a time limit.", inst.Version, inst.Path, inst.From)), nil
		})

	addTool(s, &mcp.Tool{Name: "record_spice_model", Description: "Save a part's SPICE model in the library (a .subckt or .model, SPICE or PSpice dialect), so simulations can include it by {part, name}. Replaces the part's model of the same name.", Annotations: writes("Record SPICE model", true)},
		write, func(a spiceModelArgs) string { return "spice model " + a.Part + " " + a.Name }, func(ctx context.Context, a spiceModelArgs) (result, error) {
			m, err := c.RecordSpiceModel(ctx, a.Part, library.SpiceModel{Name: a.Name, Dialect: strings.ToLower(a.Dialect), Text: a.Text, Notes: a.Notes}, library.SourceAgent)
			if err != nil {
				return result{}, err
			}
			return text(fmt.Sprintf("saved %s's model %s (%s). Include it in a simulation with models: [{part: %q, name: %q}].", a.Part, m.Name, m.Dialect, a.Part, m.Name)), nil
		})

	addTool(s, &mcp.Tool{Name: "get_spice_models", Description: "A part's SPICE models in the library, with their text.", Annotations: readOnly("Get SPICE models")},
		read, func(a getModelsArgs) string { return "spice models " + a.Part }, func(ctx context.Context, a getModelsArgs) (result, error) {
			res, err := c.Lib.Resolve(ctx, a.Part)
			if err != nil {
				return result{}, err
			}
			ms, err := c.Lib.SpiceModels(ctx, res.Part.ID)
			if err != nil {
				return result{}, err
			}
			if len(ms) == 0 {
				return text(res.Part.MPN + " has no SPICE model: record one with record_spice_model, or use a stand-in and say so in the presentation."), nil
			}
			var b strings.Builder
			for _, m := range ms {
				fmt.Fprintf(&b, "## %s (%s, %s)\n%s\n```\n%s\n```\n", m.Name, m.Dialect, m.Source, m.Notes, m.Text)
			}
			return text(b.String()), nil
		})

	addTool(s, &mcp.Tool{Name: "simulate", Description: "Set up a simulation of a project and run it with ngspice: you decide what's worth testing (inrush, start-up, load steps, loop gain, PSRR, corners, temperature, one part on a test bench…) and write the netlist with its analyses and .meas measurements; variants run it again with other parameter values or temperatures. SheetDiver checks the netlist (no .control, .include or files), runs each variant in its own folder with a time limit, and keeps the results with the project. Returns each run's state, measurements and the plots it wrote (tran1, ac1…, with their signals) to present with present_simulation. Saving under an existing name replaces it and marks its runs stale.", Annotations: writes("Simulate", true)},
		write, func(a simulateArgs) string { return "simulate " + a.Name }, func(ctx context.Context, a simulateArgs) (result, error) {
			name, err := s.project(a.Project)
			if err != nil {
				return result{}, err
			}
			sim, err := c.SaveSimulation(ctx, name, core.SimulationSpec{Name: a.Name, Kind: a.Kind, Purpose: a.Purpose, Target: a.Target, Netlist: a.Netlist, Models: a.Models, Variants: a.Variants}, library.SourceAgent)
			if err != nil {
				return result{}, err
			}
			if a.Run != nil && !*a.Run {
				return text(fmt.Sprintf("saved simulation %q in %s with %d variants; run it with run_simulation.", sim.Name, name, len(sim.Variants))), nil
			}
			runs, err := c.RunSimulation(ctx, name, sim.Name, nil)
			if err != nil {
				return result{}, err
			}
			return text(runsText(sim.Name, runs)), nil
		})

	addTool(s, &mcp.Tool{Name: "run_simulation", Description: "Run a saved simulation again, all its variants or those named (after recording a model, or changing ngspice).", Annotations: writes("Run simulation", true)},
		write, func(a runSimArgs) string { return "run " + a.Name }, func(ctx context.Context, a runSimArgs) (result, error) {
			name, err := s.project(a.Project)
			if err != nil {
				return result{}, err
			}
			runs, err := c.RunSimulation(ctx, name, a.Name, a.Variants)
			if err != nil {
				return result{}, err
			}
			return text(runsText(a.Name, runs)), nil
		})

	addTool(s, &mcp.Tool{Name: "present_simulation", Description: "Write up a simulation's results for the user, shown in the project's Simulations tab: a summary, the charts that matter (signals of a plot, variants overlaid; AC as dB or phase), targets for the measurements (pass or fail per variant), conclusions, and which parts were stand-ins. Charts and targets are checked against what the runs wrote.", Annotations: writes("Present simulation", true)},
		write, func(a presentArgs) string { return "present " + a.Name }, func(ctx context.Context, a presentArgs) (result, error) {
			name, err := s.project(a.Project)
			if err != nil {
				return result{}, err
			}
			d, err := c.PresentSimulation(ctx, name, a.Name, library.Presentation{Summary: a.Summary, Plots: a.Plots, Checks: a.Checks, Conclusions: a.Conclusions, StandIns: a.StandIns})
			if err != nil {
				return result{}, err
			}
			return text(fmt.Sprintf("presented %q: %d charts.\n%s\nThe user sees it under the project's Simulations tab (show with tab: simulations).", a.Name, len(a.Plots), checksText(d.Checks))), nil
		})

	addTool(s, &mcp.Tool{Name: "list_simulations", Description: "A project's simulations: kind, purpose, each variant's latest run state, targets passed and failed, and whether they're presented.", Annotations: readOnly("List simulations")},
		read, func(a simListArgs) string { return "simulations " + a.Project }, func(ctx context.Context, a simListArgs) (result, error) {
			name, err := s.project(a.Project)
			if err != nil {
				return result{}, err
			}
			list, err := c.Simulations(ctx, name)
			if err != nil {
				return result{}, err
			}
			if len(list) == 0 {
				return text(name + " has no simulations yet: set one up with simulate."), nil
			}
			var b strings.Builder
			for _, sm := range list {
				var states []string
				for v, st := range sm.States {
					states = append(states, v+": "+st)
				}
				fmt.Fprintf(&b, "- %s [%s] %s — %s; targets %d pass, %d fail; presented %v\n", sm.Name, sm.Kind, sm.Purpose, strings.Join(states, ", "), sm.Pass, sm.Fail, sm.Presented)
			}
			return text(b.String()), nil
		})

	addTool(s, &mcp.Tool{Name: "get_simulation", Description: "A simulation: its netlist, variants and models, each variant's latest run (state, measurements, plots and their signals, ngspice's errors and warnings), the targets checked, and the presentation.", Annotations: readOnly("Get simulation")},
		read, func(a simNameArgs) string { return "simulation " + a.Name }, func(ctx context.Context, a simNameArgs) (result, error) {
			name, err := s.project(a.Project)
			if err != nil {
				return result{}, err
			}
			d, err := c.SimulationDetail(ctx, name, a.Name)
			if err != nil {
				return result{}, err
			}
			var b strings.Builder
			sim := d.Simulation
			fmt.Fprintf(&b, "# %s\nkind: %s; target: %s\n%s\n\n```spice\n%s\n```\n", sim.Name, sim.Kind, firstNonEmpty(sim.Target, "the board"), sim.Purpose, strings.TrimSpace(sim.Netlist))
			for _, v := range sim.Variants {
				fmt.Fprintf(&b, "- variant %s: %v", v.Name, v.Params)
				if v.Temp != nil {
					fmt.Fprintf(&b, " at %g °C", *v.Temp)
				}
				b.WriteString("\n")
			}
			b.WriteString("\n" + runsText(sim.Name, d.Latest))
			b.WriteString(checksText(d.Checks))
			if p := sim.Presentation; p != nil {
				fmt.Fprintf(&b, "\n## Presentation\n%s\n%d charts\n%s\n", p.Summary, len(p.Plots), p.Conclusions)
			}
			return text(b.String()), nil
		})

	addTool(s, &mcp.Tool{Name: "get_simulation_data", Description: "Values of a run's signals, thinned to a few points keeping peaks and edges, to read numbers off a result (AC as dB or phase).", Annotations: readOnly("Get simulation data")},
		read, func(a simDataArgs) string { return fmt.Sprintf("simulation data run %d %s", a.Run, a.Plot) }, func(ctx context.Context, a simDataArgs) (result, error) {
			n := a.Points
			if n <= 0 {
				n = 40
			}
			d, err := c.SimulationData(ctx, a.Run, a.Plot, a.Signals, a.Part, min(n, 2000))
			if err != nil {
				return result{}, err
			}
			var b strings.Builder
			fmt.Fprintf(&b, "run %d (%s), %s\n", d.Run, d.Variant, d.Plot)
			for _, ser := range d.Series {
				fmt.Fprintf(&b, "\n%s (%s) against %s (%s):\n", ser.Name, ser.Unit, d.XName, d.XUnit)
				for i := range ser.X {
					fmt.Fprintf(&b, "%s\t%s\n", strconv.FormatFloat(ser.X[i], 'g', 5, 64), strconv.FormatFloat(ser.Y[i], 'g', 5, 64))
				}
			}
			return text(b.String()), nil
		})

	addTool(s, &mcp.Tool{Name: "delete_simulation", Description: "Delete a simulation with its runs and their results. Not undoable.", Annotations: destructive("Delete simulation")},
		write, func(a simNameArgs) string { return "delete simulation " + a.Name }, func(ctx context.Context, a simNameArgs) (result, error) {
			name, err := s.project(a.Project)
			if err != nil {
				return result{}, err
			}
			if err := c.DeleteSimulation(ctx, name, a.Name); err != nil {
				return result{}, err
			}
			return text("deleted simulation " + a.Name), nil
		})
}

// runsText says how runs went: state, measurements, plots and problems.
func runsText(name string, runs []library.SimRun) string {
	var b strings.Builder
	for _, r := range runs {
		fmt.Fprintf(&b, "## %s / %s: %s (run %d, ngspice %s, %d ms)\n", name, r.Variant, r.State, r.ID, r.Ngspice, r.TookMS)
		if r.Error != "" {
			fmt.Fprintf(&b, "error: %s\n", r.Error)
		}
		for _, m := range r.Measures {
			switch {
			case m.Value != nil && m.At != nil:
				fmt.Fprintf(&b, "- %s = %g at %g\n", m.Name, *m.Value, *m.At)
			case m.Value != nil:
				fmt.Fprintf(&b, "- %s = %g\n", m.Name, *m.Value)
			default:
				fmt.Fprintf(&b, "- %s: %s\n", m.Name, m.Error)
			}
		}
		for _, p := range r.Plots {
			var vs []string
			for _, v := range p.Vectors[min(1, len(p.Vectors)):] {
				vs = append(vs, v.Name)
			}
			fmt.Fprintf(&b, "- plot %s (%s, %d points): %s\n", p.ID, p.Name, p.Points, strings.Join(vs, ", "))
		}
		if len(r.Problems) > 0 {
			fmt.Fprintf(&b, "- ngspice said: %s\n", strings.Join(r.Problems, "; "))
		}
	}
	if len(runs) > 0 {
		b.WriteString("\nPresent the results with present_simulation; read values with get_simulation_data.\n")
	}
	return b.String()
}

// checksText lists the targets per variant.
func checksText(checks []core.CheckResult) string {
	if len(checks) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("\ntargets:\n")
	for _, c := range checks {
		verdict := "no value"
		if c.Pass != nil {
			verdict = map[bool]string{true: "pass", false: "FAIL"}[*c.Pass]
		}
		val := "–"
		if c.Value != nil {
			val = strconv.FormatFloat(*c.Value, 'g', 4, 64)
		}
		fmt.Fprintf(&b, "- %s / %s = %s %s: %s\n", c.Variant, c.Measure, val, c.Unit, verdict)
	}
	return b.String()
}
