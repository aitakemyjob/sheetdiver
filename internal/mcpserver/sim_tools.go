package mcpserver

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/aitakemyjob/sheetdiver/internal/core"
	"github.com/aitakemyjob/sheetdiver/internal/library"
)

// Simulation tools: the agent sets up tests of a project (any number,
// each one question) and tries each in variations as the schematic
// changes or alternatives come up; SheetDiver checks and runs them with
// the user's ngspice and keeps every variation's results with its test;
// the agent then writes up how the test fared across them, and the app
// shows that write-up.

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
	Project    string                 `json:"project,omitempty"`
	Test       string                 `json:"test" jsonschema:"the test: one question, unique in the project (\"3V3 load step\", \"VBUS inrush on hot-plug\", \"buck loop gain\"); a new name starts a test, an existing one adds this variation to it"`
	Kind       string                 `json:"kind,omitempty" jsonschema:"what sort of test, in your words: inrush, start-up, load step, loop gain, PSRR, corners, thermal…"`
	Purpose    string                 `json:"purpose,omitempty" jsonschema:"the question the test answers and why it matters"`
	Target     string                 `json:"target,omitempty" jsonschema:"the designator of the part the test exercises on its own (a component test bench); empty for the board"`
	Variation  string                 `json:"variation" jsonschema:"this try of the test, unique in it: \"Rev A, 22 µF\", \"47 µF polymer\", \"after adding the soft-start cap\". A new name adds a variation and keeps the earlier ones; the same name replaces that variation (to fix its netlist)"`
	Change     string                 `json:"change,omitempty" jsonschema:"what's different from the variation before: the schematic change or the alternative being tried"`
	Netlist    string                 `json:"netlist" jsonschema:"the SPICE netlist of the circuit as it is in this variation: title line, circuit, .param, .model/.subckt, analyses (.tran .ac .dc .op .noise .tf .pz .sens .disto) and .meas lines. No .control, .include or file paths: SheetDiver runs it and includes models"`
	Models     []library.SimModel     `json:"models,omitempty" jsonschema:"part models to include, recorded with record_spice_model: {part, name}"`
	Conditions []library.SimCondition `json:"conditions,omitempty" jsonschema:"the conditions this variation runs in: {name, params: {rload: \"3.3\"}, temp: 85} (corners, loads, temperatures); none runs the netlist as written"`
	Run        *bool                  `json:"run,omitempty" jsonschema:"run it now (default true)"`
}

type runSimArgs struct {
	Project    string   `json:"project,omitempty"`
	Test       string   `json:"test"`
	Variation  string   `json:"variation,omitempty" jsonschema:"which variation (default the latest)"`
	Conditions []string `json:"conditions,omitempty" jsonschema:"which conditions (default all)"`
}

type presentArgs struct {
	Project     string                  `json:"project,omitempty"`
	Test        string                  `json:"test"`
	Summary     string                  `json:"summary" jsonschema:"what the test checks and how it fared across its variations (markdown)"`
	Plots       []library.SimPlot       `json:"plots,omitempty" jsonschema:"charts: {title, plot: tran1/ac1/dc1…, signals: [v(out)], variations (default the latest; name several to compare them), conditions (default all), part: mag or phase for AC, xLog, yLabel, note}"`
	Checks      []library.SimCheck      `json:"checks,omitempty" jsonschema:"targets for .meas results: {measure, min, max, unit, why}; every variation passes or fails in each condition"`
	Notes       []library.VariationNote `json:"notes,omitempty" jsonschema:"what you took from each variation: {variation, text}"`
	Conclusions string                  `json:"conclusions,omitempty" jsonschema:"what to do about it (markdown)"`
	StandIns    []string                `json:"stand_ins,omitempty" jsonschema:"parts modelled by idealised stand-ins rather than their real models, and how"`
}

type simTestArgs struct {
	Project string `json:"project,omitempty"`
	Test    string `json:"test"`
}

type simVariationArgs struct {
	Project   string `json:"project,omitempty"`
	Test      string `json:"test"`
	Variation string `json:"variation"`
}

type simListArgs struct {
	Project string `json:"project,omitempty"`
}

type simDataArgs struct {
	Run     int64    `json:"run" jsonschema:"the run's id (simulate and get_simulation list them)"`
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

	addTool(s, &mcp.Tool{Name: "simulate", Description: "Add a variation to a test of a project and run it with ngspice. A test is one question you decided is worth checking (inrush, start-up, a load step, loop gain, PSRR, a part on its own test bench…); each variation is a try of it — the circuit as the schematic is now, or an alternative — with what changed, so the test keeps its history as the design moves and its results can be compared. Write the variation's netlist with its analyses and .meas measurements; conditions run it at other parameter values or temperatures (corners, loads). SheetDiver checks the netlist (no .control, .include or files), runs each condition in its own folder with a time limit, and keeps the results with the test. Returns each run's state, measurements and the plots it wrote (tran1, ac1…, with their signals) to write up with present_simulation.", Annotations: writes("Simulate", true)},
		write, func(a simulateArgs) string { return "simulate " + a.Test + " / " + a.Variation }, func(ctx context.Context, a simulateArgs) (result, error) {
			name, err := s.project(a.Project)
			if err != nil {
				return result{}, err
			}
			sim, v, err := c.SaveVariation(ctx, name, core.TestSpec{Name: a.Test, Kind: a.Kind, Purpose: a.Purpose, Target: a.Target},
				core.VariationSpec{Name: a.Variation, Change: a.Change, Netlist: a.Netlist, Models: a.Models, Conditions: a.Conditions}, library.SourceAgent)
			if err != nil {
				return result{}, err
			}
			if a.Run != nil && !*a.Run {
				return text(fmt.Sprintf("saved variation %q of test %q in %s with %d conditions; run it with run_simulation.", v.Name, sim.Name, name, len(v.Conditions))), nil
			}
			runs, err := c.RunVariation(ctx, name, sim.Name, v.Name, nil)
			if err != nil {
				return result{}, err
			}
			return text(runsText(sim.Name, runs)), nil
		})

	addTool(s, &mcp.Tool{Name: "run_simulation", Description: "Run a test's variation again (the latest when none is named), in all its conditions or those named: after recording a model, or changing ngspice.", Annotations: writes("Run simulation", true)},
		write, func(a runSimArgs) string { return "run " + a.Test + " / " + a.Variation }, func(ctx context.Context, a runSimArgs) (result, error) {
			name, err := s.project(a.Project)
			if err != nil {
				return result{}, err
			}
			runs, err := c.RunVariation(ctx, name, a.Test, a.Variation, a.Conditions)
			if err != nil {
				return result{}, err
			}
			return text(runsText(a.Test, runs)), nil
		})

	addTool(s, &mcp.Tool{Name: "present_simulation", Description: "Write up a test's results for the user, shown in the project's Simulations tab: a summary of how the test fared across its variations, the charts that matter (signals of a plot, for the variations and conditions named, overlaid to compare; AC as dB or phase), targets for the measurements (pass or fail for every variation in every condition, so the user sees how the design improved), a note per variation, conclusions, and which parts were stand-ins. Charts, targets and notes are checked against what the variations' runs wrote. Call it again as variations are added.", Annotations: writes("Present simulation", true)},
		write, func(a presentArgs) string { return "present " + a.Test }, func(ctx context.Context, a presentArgs) (result, error) {
			name, err := s.project(a.Project)
			if err != nil {
				return result{}, err
			}
			d, err := c.PresentSimulation(ctx, name, a.Test, library.Presentation{Summary: a.Summary, Plots: a.Plots, Checks: a.Checks, Notes: a.Notes, Conclusions: a.Conclusions, StandIns: a.StandIns})
			if err != nil {
				return result{}, err
			}
			return text(fmt.Sprintf("presented %q: %d charts.\n%s\nThe user sees it under the project's Simulations tab (show with tab: simulations).", a.Test, len(a.Plots), checksText(d))), nil
		})

	addTool(s, &mcp.Tool{Name: "list_simulations", Description: "A project's tests: kind, purpose, how many variations, the latest variation's state in each condition and its targets passed and failed, and whether written up.", Annotations: readOnly("List simulations")},
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
				return text(name + " has no tests yet: start one with simulate."), nil
			}
			var b strings.Builder
			for _, sm := range list {
				var states []string
				for cond, st := range sm.States {
					states = append(states, cond+": "+st)
				}
				sort.Strings(states)
				fmt.Fprintf(&b, "- %s [%s] %s — %d variations, latest %q: %s; targets %d pass, %d fail; written up %v\n", sm.Name, sm.Kind, sm.Purpose, sm.Variations, sm.Latest, strings.Join(states, ", "), sm.Pass, sm.Fail, sm.Presented)
			}
			return text(b.String()), nil
		})

	addTool(s, &mcp.Tool{Name: "get_simulation", Description: "A test with every variation, in the order they were added: what changed, its netlist, conditions and models, each condition's latest run (state, measurements, plots and their signals, ngspice's errors and warnings), the targets across variations and conditions, and the write-up.", Annotations: readOnly("Get simulation")},
		read, func(a simTestArgs) string { return "simulation " + a.Test }, func(ctx context.Context, a simTestArgs) (result, error) {
			name, err := s.project(a.Project)
			if err != nil {
				return result{}, err
			}
			d, err := c.SimulationDetail(ctx, name, a.Test)
			if err != nil {
				return result{}, err
			}
			var b strings.Builder
			sim := d.Simulation
			fmt.Fprintf(&b, "# %s\nkind: %s; target: %s\n%s\n", sim.Name, sim.Kind, firstNonEmpty(sim.Target, "the board"), sim.Purpose)
			for i, v := range d.Variations {
				fmt.Fprintf(&b, "\n## Variation %d: %s\n%s\n```spice\n%s\n```\n", i+1, v.Variation.Name, v.Variation.Change, strings.TrimSpace(v.Variation.Netlist))
				for _, cond := range v.Variation.Conditions {
					fmt.Fprintf(&b, "- condition %s: %v", cond.Name, cond.Params)
					if cond.Temp != nil {
						fmt.Fprintf(&b, " at %g °C", *cond.Temp)
					}
					b.WriteString("\n")
				}
				b.WriteString(runsText(sim.Name, v.Latest))
			}
			b.WriteString(checksText(d))
			if p := sim.Presentation; p != nil {
				fmt.Fprintf(&b, "\n## Write-up\n%s\n%d charts\n%s\n", p.Summary, len(p.Plots), p.Conclusions)
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
			fmt.Fprintf(&b, "run %d (%s, %s), %s\n", d.Run, d.Variation, d.Condition, d.Plot)
			for _, ser := range d.Series {
				fmt.Fprintf(&b, "\n%s (%s) against %s (%s):\n", ser.Name, ser.Unit, d.XName, d.XUnit)
				for i := range ser.X {
					fmt.Fprintf(&b, "%s\t%s\n", strconv.FormatFloat(ser.X[i], 'g', 5, 64), strconv.FormatFloat(ser.Y[i], 'g', 5, 64))
				}
			}
			return text(b.String()), nil
		})

	addTool(s, &mcp.Tool{Name: "delete_variation", Description: "Delete one variation of a test, with its runs and results (a try that was a mistake). The test and its other variations stay. Not undoable.", Annotations: destructive("Delete variation")},
		write, func(a simVariationArgs) string { return "delete variation " + a.Test + " / " + a.Variation }, func(ctx context.Context, a simVariationArgs) (result, error) {
			name, err := s.project(a.Project)
			if err != nil {
				return result{}, err
			}
			if err := c.DeleteVariation(ctx, name, a.Test, a.Variation); err != nil {
				return result{}, err
			}
			return text("deleted variation " + a.Variation + " of " + a.Test), nil
		})

	addTool(s, &mcp.Tool{Name: "delete_simulation", Description: "Delete a test with all its variations, runs and results. Not undoable.", Annotations: destructive("Delete simulation")},
		write, func(a simTestArgs) string { return "delete simulation " + a.Test }, func(ctx context.Context, a simTestArgs) (result, error) {
			name, err := s.project(a.Project)
			if err != nil {
				return result{}, err
			}
			if err := c.DeleteSimulation(ctx, name, a.Test); err != nil {
				return result{}, err
			}
			return text("deleted test " + a.Test), nil
		})
}

// runsText says how runs went: state, measurements, plots and problems.
func runsText(name string, runs []library.SimRun) string {
	var b strings.Builder
	for _, r := range runs {
		fmt.Fprintf(&b, "### %s / %s / %s: %s (run %d, ngspice %s, %d ms)\n", name, r.Variation, r.Condition, r.State, r.ID, r.Ngspice, r.TookMS)
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

// checksText lists the targets: each variation's result in each
// condition.
func checksText(d core.SimulationDetail) string {
	var b strings.Builder
	for _, v := range d.Variations {
		for _, c := range v.Checks {
			if b.Len() == 0 {
				b.WriteString("\ntargets:\n")
			}
			verdict := "no value"
			if c.Pass != nil {
				verdict = map[bool]string{true: "pass", false: "FAIL"}[*c.Pass]
			}
			val := "–"
			if c.Value != nil {
				val = strconv.FormatFloat(*c.Value, 'g', 4, 64)
			}
			fmt.Fprintf(&b, "- %s / %s / %s = %s %s: %s\n", c.Variation, c.Condition, c.Measure, val, c.Unit, verdict)
		}
	}
	return b.String()
}
