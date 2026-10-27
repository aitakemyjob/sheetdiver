package library

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// Simulations of a project. A simulation is a test: one question an
// agent set out to answer ("Does 3V3 survive the radio waking?"), with
// its targets and the agent's write-up. A test is tried in variations:
// each a netlist of the circuit as it was then, or an alternative being
// tried, with a note of what changed; variations are added as the
// schematic changes, not replaced, so the test keeps its history. Each
// variation runs in its conditions (corners, temperatures, loads), and
// each run keeps its state, measurements, the plots ngspice wrote (their
// vectors; the values stay in the run's results file, in its folder
// under simulations/) and its log. Part SPICE models live with the parts.

// SpiceModel is a part's SPICE model: a .subckt or .model by name, in
// SPICE or PSpice dialect.
type SpiceModel struct {
	ID      int64  `json:"id"`
	PartID  int64  `json:"partId"`
	MPN     string `json:"mpn,omitempty"`
	Name    string `json:"name"`
	Dialect string `json:"dialect"`
	Text    string `json:"text"`
	Notes   string `json:"notes,omitempty"`
	Source  string `json:"source"`
	Updated string `json:"updated"`
}

// SimModel is a model a simulation includes: the part's, by name.
type SimModel struct {
	Part string `json:"part"`
	Name string `json:"name"`
}

// SimCondition is a condition a variation runs in: its parameter values
// and temperature ("hot", "10 µF", "corner ss").
type SimCondition struct {
	Name   string            `json:"name"`
	Params map[string]string `json:"params,omitempty"`
	Temp   *float64          `json:"temp,omitempty"`
}

// SimPlot is a chart the agent asked for: the analysis's plot (tran1,
// ac1…), the signals, which variations (none: the latest) and conditions
// (none: all) it overlays, the part of an AC signal (mag or phase), axes
// and a note.
type SimPlot struct {
	Title      string   `json:"title"`
	Plot       string   `json:"plot"`
	Signals    []string `json:"signals"`
	Variations []string `json:"variations,omitempty"`
	Conditions []string `json:"conditions,omitempty"`
	Part       string   `json:"part,omitempty"`
	XLog       bool     `json:"xLog,omitempty"`
	YLabel     string   `json:"yLabel,omitempty"`
	Note       string   `json:"note,omitempty"`
}

// SimCheck is a target a measurement should meet.
type SimCheck struct {
	Measure string   `json:"measure"`
	Min     *float64 `json:"min,omitempty"`
	Max     *float64 `json:"max,omitempty"`
	Unit    string   `json:"unit,omitempty"`
	Why     string   `json:"why,omitempty"`
}

// VariationNote is what the agent took from one variation's results.
type VariationNote struct {
	Variation string `json:"variation"`
	Text      string `json:"text"`
}

// Presentation is the agent's write-up of a test's results across its
// variations: a summary, the charts to show, the targets, a note per
// variation, its conclusions, and which parts were stand-ins rather than
// their real models.
type Presentation struct {
	Summary     string          `json:"summary"`
	Plots       []SimPlot       `json:"plots"`
	Checks      []SimCheck      `json:"checks"`
	Notes       []VariationNote `json:"notes,omitempty"`
	Conclusions string          `json:"conclusions,omitempty"`
	StandIns    []string        `json:"standIns,omitempty"`
	Updated     string          `json:"updated,omitempty"`
}

// Simulation is a project's test.
type Simulation struct {
	ID           int64         `json:"id"`
	ProjectID    int64         `json:"projectId"`
	Name         string        `json:"name"`
	Kind         string        `json:"kind,omitempty"`
	Purpose      string        `json:"purpose,omitempty"`
	Target       string        `json:"target,omitempty"`
	Presentation *Presentation `json:"presentation,omitempty"`
	Source       string        `json:"source"`
	Created      string        `json:"created"`
	Updated      string        `json:"updated"`
}

// Variation is a try of a test: the netlist, what changed from the one
// before, the part models it includes and the conditions it runs in.
type Variation struct {
	ID           int64          `json:"id"`
	SimulationID int64          `json:"simulationId"`
	Name         string         `json:"name"`
	Change       string         `json:"change,omitempty"`
	Netlist      string         `json:"netlist"`
	Models       []SimModel     `json:"models"`
	Conditions   []SimCondition `json:"conditions"`
	Source       string         `json:"source"`
	Created      string         `json:"created"`
	Updated      string         `json:"updated"`
}

// RunPlot is a plot a run wrote: its id and name, whether it's complex,
// its points and its vectors' names and kinds.
type RunPlot struct {
	ID      string      `json:"id"`
	Name    string      `json:"name"`
	Complex bool        `json:"complex,omitempty"`
	Points  int         `json:"points"`
	Vectors []RunVector `json:"vectors"`
}

// RunVector is a vector of a run's plot.
type RunVector struct {
	Name string `json:"name"`
	Kind string `json:"kind"`
}

// RunMeasure is a measurement's result in a run.
type RunMeasure struct {
	Name  string   `json:"name"`
	Value *float64 `json:"value,omitempty"`
	At    *float64 `json:"at,omitempty"`
	Error string   `json:"error,omitempty"`
}

// SimRun is a run of a variation in one of its conditions.
type SimRun struct {
	ID          int64        `json:"id"`
	VariationID int64        `json:"variationId"`
	Variation   string       `json:"variation"`
	Condition   string       `json:"condition"`
	State       string       `json:"state"`
	Ngspice     string       `json:"ngspice,omitempty"`
	DeckSHA     string       `json:"deckSha,omitempty"`
	Started     string       `json:"started,omitempty"`
	Finished    string       `json:"finished,omitempty"`
	TookMS      int64        `json:"tookMs"`
	Error       string       `json:"error,omitempty"`
	Problems    []string     `json:"problems"`
	Measures    []RunMeasure `json:"measures"`
	Plots       []RunPlot    `json:"plots"`
	Log         string       `json:"-"`
}

// simulationMigration adds the tables.
const simulationMigration = `
CREATE TABLE spice_models (
	id INTEGER PRIMARY KEY,
	part_id INTEGER NOT NULL REFERENCES parts(id) ON DELETE CASCADE,
	name TEXT NOT NULL COLLATE NOCASE,
	dialect TEXT NOT NULL DEFAULT 'spice',
	text TEXT NOT NULL,
	notes TEXT NOT NULL DEFAULT '',
	source TEXT NOT NULL DEFAULT 'agent',
	updated_at TEXT NOT NULL,
	UNIQUE(part_id, name)
);
CREATE TABLE simulations (
	id INTEGER PRIMARY KEY,
	project_id INTEGER NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
	name TEXT NOT NULL COLLATE NOCASE,
	kind TEXT NOT NULL DEFAULT '',
	purpose TEXT NOT NULL DEFAULT '',
	target TEXT NOT NULL DEFAULT '',
	presentation TEXT NOT NULL DEFAULT '',
	source TEXT NOT NULL DEFAULT 'agent',
	created_at TEXT NOT NULL,
	updated_at TEXT NOT NULL,
	UNIQUE(project_id, name)
);
CREATE TABLE simulation_variations (
	id INTEGER PRIMARY KEY,
	simulation_id INTEGER NOT NULL REFERENCES simulations(id) ON DELETE CASCADE,
	name TEXT NOT NULL COLLATE NOCASE,
	change TEXT NOT NULL DEFAULT '',
	netlist TEXT NOT NULL,
	models TEXT NOT NULL DEFAULT '[]',
	conditions TEXT NOT NULL DEFAULT '[]',
	source TEXT NOT NULL DEFAULT 'agent',
	created_at TEXT NOT NULL,
	updated_at TEXT NOT NULL,
	UNIQUE(simulation_id, name)
);
CREATE TABLE simulation_runs (
	id INTEGER PRIMARY KEY,
	variation_id INTEGER NOT NULL REFERENCES simulation_variations(id) ON DELETE CASCADE,
	condition TEXT NOT NULL DEFAULT '',
	state TEXT NOT NULL,
	ngspice TEXT NOT NULL DEFAULT '',
	deck_sha TEXT NOT NULL DEFAULT '',
	started_at TEXT NOT NULL DEFAULT '',
	finished_at TEXT NOT NULL DEFAULT '',
	took_ms INTEGER NOT NULL DEFAULT 0,
	error TEXT NOT NULL DEFAULT '',
	problems TEXT NOT NULL DEFAULT '[]',
	measures TEXT NOT NULL DEFAULT '[]',
	plots TEXT NOT NULL DEFAULT '[]',
	log TEXT NOT NULL DEFAULT ''
);
CREATE INDEX simulation_variations_sim ON simulation_variations(simulation_id);
CREATE INDEX simulation_runs_variation ON simulation_runs(variation_id);
`

// RunDir is the folder a run's deck, log and results are kept in.
func (s *Store) RunDir(runID int64) string {
	return filepath.Join(s.Dir, "simulations", strconv.FormatInt(runID, 10))
}

// toJSON writes a column's JSON.
func toJSON(v any) string {
	b, _ := json.Marshal(v)
	return string(b)
}

// SetSpiceModel adds or replaces a part's model of the same name.
func (s *Store) SetSpiceModel(ctx context.Context, m SpiceModel) (SpiceModel, error) {
	m.Name = strings.TrimSpace(m.Name)
	if m.Name == "" || strings.TrimSpace(m.Text) == "" {
		return m, errors.New("a model needs its name (the .subckt or .model name) and its text")
	}
	if m.Dialect == "" {
		m.Dialect = "spice"
	}
	if m.Source == "" {
		m.Source = SourceAgent
	}
	m.Updated = now()
	err := s.db.QueryRowContext(ctx, `INSERT INTO spice_models (part_id, name, dialect, text, notes, source, updated_at) VALUES (?,?,?,?,?,?,?)
		ON CONFLICT(part_id, name) DO UPDATE SET dialect = excluded.dialect, text = excluded.text, notes = excluded.notes, source = excluded.source, updated_at = excluded.updated_at
		RETURNING id`, m.PartID, m.Name, m.Dialect, m.Text, m.Notes, m.Source, m.Updated).Scan(&m.ID)
	return m, err
}

// SpiceModels lists a part's models (with their text).
func (s *Store) SpiceModels(ctx context.Context, partID int64) ([]SpiceModel, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT m.id, m.part_id, p.mpn, m.name, m.dialect, m.text, m.notes, m.source, m.updated_at
		FROM spice_models m JOIN parts p ON p.id = m.part_id WHERE m.part_id = ? ORDER BY m.name`, partID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []SpiceModel{}
	for rows.Next() {
		var m SpiceModel
		if err := rows.Scan(&m.ID, &m.PartID, &m.MPN, &m.Name, &m.Dialect, &m.Text, &m.Notes, &m.Source, &m.Updated); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// SaveSimulation adds a project's test or updates the one of the same
// name (its kind, purpose and target), keeping its variations and
// presentation.
func (s *Store) SaveSimulation(ctx context.Context, sim Simulation) (Simulation, error) {
	sim.Name = strings.TrimSpace(sim.Name)
	if sim.Name == "" {
		return sim, errors.New("a test needs a name, e.g. \"3V3 load step\"")
	}
	if sim.Source == "" {
		sim.Source = SourceAgent
	}
	t := now()
	if _, err := s.db.ExecContext(ctx, `INSERT INTO simulations (project_id, name, kind, purpose, target, source, created_at, updated_at) VALUES (?,?,?,?,?,?,?,?)
		ON CONFLICT(project_id, name) DO UPDATE SET kind = COALESCE(NULLIF(excluded.kind, ''), kind), purpose = COALESCE(NULLIF(excluded.purpose, ''), purpose),
		target = COALESCE(NULLIF(excluded.target, ''), target), updated_at = excluded.updated_at`,
		sim.ProjectID, sim.Name, sim.Kind, sim.Purpose, sim.Target, sim.Source, t, t); err != nil {
		return sim, err
	}
	return s.Simulation(ctx, sim.ProjectID, sim.Name)
}

// simCols are a test's columns.
const simCols = `id, project_id, name, kind, purpose, target, presentation, source, created_at, updated_at`

// scanSimulation reads a test row.
func scanSimulation(sc interface{ Scan(...any) error }) (Simulation, error) {
	var sim Simulation
	var pres string
	if err := sc.Scan(&sim.ID, &sim.ProjectID, &sim.Name, &sim.Kind, &sim.Purpose, &sim.Target, &pres, &sim.Source, &sim.Created, &sim.Updated); err != nil {
		return sim, err
	}
	if pres != "" {
		sim.Presentation = &Presentation{}
		_ = json.Unmarshal([]byte(pres), sim.Presentation)
	}
	return sim, nil
}

// Simulation finds a project's test by name (ignoring case).
func (s *Store) Simulation(ctx context.Context, projectID int64, name string) (Simulation, error) {
	sim, err := scanSimulation(s.db.QueryRowContext(ctx, `SELECT `+simCols+` FROM simulations WHERE project_id = ? AND name = ?`, projectID, strings.TrimSpace(name)))
	if errors.Is(err, sql.ErrNoRows) {
		return sim, fmt.Errorf("test %q: %w", name, ErrNotFound)
	}
	return sim, err
}

// Simulations lists a project's tests, the latest changed first.
func (s *Store) Simulations(ctx context.Context, projectID int64) ([]Simulation, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+simCols+` FROM simulations WHERE project_id = ? ORDER BY updated_at DESC, id DESC`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Simulation{}
	for rows.Next() {
		sim, err := scanSimulation(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, sim)
	}
	return out, rows.Err()
}

// SetPresentation saves the agent's write-up of a test.
func (s *Store) SetPresentation(ctx context.Context, simID int64, p Presentation) error {
	p.Updated = now()
	_, err := s.db.ExecContext(ctx, `UPDATE simulations SET presentation = ?, updated_at = ? WHERE id = ?`, toJSON(p), p.Updated, simID)
	return err
}

// DeleteSimulation removes a test, its variations, runs and their files.
func (s *Store) DeleteSimulation(ctx context.Context, simID int64) error {
	ids := s.runIDs(ctx, `SELECT r.id FROM simulation_runs r JOIN simulation_variations v ON v.id = r.variation_id WHERE v.simulation_id = ?`, simID)
	if _, err := s.db.ExecContext(ctx, `DELETE FROM simulations WHERE id = ?`, simID); err != nil {
		return err
	}
	s.removeRuns(ids)
	return nil
}

// SaveVariation adds a variation to a test, or replaces the one of the
// same name (a netlist fixed before it's run, say); its runs then go
// stale. Saving a variation makes the test the latest changed.
func (s *Store) SaveVariation(ctx context.Context, v Variation) (Variation, error) {
	v.Name = strings.TrimSpace(v.Name)
	if v.Name == "" {
		return v, errors.New("a variation needs a name, e.g. \"Rev A, 22 µF\"")
	}
	if v.Source == "" {
		v.Source = SourceAgent
	}
	if v.Models == nil {
		v.Models = []SimModel{}
	}
	if v.Conditions == nil {
		v.Conditions = []SimCondition{}
	}
	t := now()
	err := s.tx(ctx, func(tx *sql.Tx) error {
		if err := tx.QueryRowContext(ctx, `INSERT INTO simulation_variations (simulation_id, name, change, netlist, models, conditions, source, created_at, updated_at) VALUES (?,?,?,?,?,?,?,?,?)
			ON CONFLICT(simulation_id, name) DO UPDATE SET change = excluded.change, netlist = excluded.netlist, models = excluded.models,
			conditions = excluded.conditions, source = excluded.source, updated_at = excluded.updated_at
			RETURNING id`, v.SimulationID, v.Name, v.Change, v.Netlist, toJSON(v.Models), toJSON(v.Conditions), v.Source, t, t).Scan(&v.ID); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `UPDATE simulation_runs SET state = 'stale' WHERE variation_id = ? AND state IN ('done', 'failed')`, v.ID); err != nil {
			return err
		}
		_, err := tx.ExecContext(ctx, `UPDATE simulations SET updated_at = ? WHERE id = ?`, t, v.SimulationID)
		return err
	})
	if err != nil {
		return v, err
	}
	return s.Variation(ctx, v.SimulationID, v.Name)
}

// varCols are a variation's columns.
const varCols = `id, simulation_id, name, change, netlist, models, conditions, source, created_at, updated_at`

// scanVariation reads a variation row.
func scanVariation(sc interface{ Scan(...any) error }) (Variation, error) {
	var v Variation
	var models, conds string
	if err := sc.Scan(&v.ID, &v.SimulationID, &v.Name, &v.Change, &v.Netlist, &models, &conds, &v.Source, &v.Created, &v.Updated); err != nil {
		return v, err
	}
	_ = json.Unmarshal([]byte(models), &v.Models)
	_ = json.Unmarshal([]byte(conds), &v.Conditions)
	return v, nil
}

// Variation finds a test's variation by name (ignoring case).
func (s *Store) Variation(ctx context.Context, simID int64, name string) (Variation, error) {
	v, err := scanVariation(s.db.QueryRowContext(ctx, `SELECT `+varCols+` FROM simulation_variations WHERE simulation_id = ? AND name = ?`, simID, strings.TrimSpace(name)))
	if errors.Is(err, sql.ErrNoRows) {
		return v, fmt.Errorf("variation %q: %w", name, ErrNotFound)
	}
	return v, err
}

// Variations lists a test's variations in the order they were added.
func (s *Store) Variations(ctx context.Context, simID int64) ([]Variation, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+varCols+` FROM simulation_variations WHERE simulation_id = ? ORDER BY id`, simID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Variation{}
	for rows.Next() {
		v, err := scanVariation(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

// DeleteVariation removes a variation, its runs and their files.
func (s *Store) DeleteVariation(ctx context.Context, variationID int64) error {
	ids := s.runIDs(ctx, `SELECT id FROM simulation_runs WHERE variation_id = ?`, variationID)
	if _, err := s.db.ExecContext(ctx, `DELETE FROM simulation_variations WHERE id = ?`, variationID); err != nil {
		return err
	}
	s.removeRuns(ids)
	return nil
}

// AddRun starts a run of a variation in a condition, queued.
func (s *Store) AddRun(ctx context.Context, variationID int64, condition string) (SimRun, error) {
	r := SimRun{VariationID: variationID, Condition: condition, State: "queued", Problems: []string{}, Measures: []RunMeasure{}, Plots: []RunPlot{}}
	err := s.db.QueryRowContext(ctx, `INSERT INTO simulation_runs (variation_id, condition, state) VALUES (?,?,?) RETURNING id`, variationID, condition, r.State).Scan(&r.ID)
	return r, err
}

// SaveRun records a run's state and results.
func (s *Store) SaveRun(ctx context.Context, r SimRun) error {
	_, err := s.db.ExecContext(ctx, `UPDATE simulation_runs SET state = ?, ngspice = ?, deck_sha = ?, started_at = ?, finished_at = ?, took_ms = ?, error = ?, problems = ?, measures = ?, plots = ?, log = ? WHERE id = ?`,
		r.State, r.Ngspice, r.DeckSHA, r.Started, r.Finished, r.TookMS, r.Error, toJSON(r.Problems), toJSON(r.Measures), toJSON(r.Plots), r.Log, r.ID)
	return err
}

// runCols are a run's columns, with its variation's name.
const runCols = `r.id, r.variation_id, v.name, r.condition, r.state, r.ngspice, r.deck_sha, r.started_at, r.finished_at, r.took_ms, r.error, r.problems, r.measures, r.plots, r.log`

// runFrom joins a run to its variation.
const runFrom = ` FROM simulation_runs r JOIN simulation_variations v ON v.id = r.variation_id `

// scanRun reads a run row.
func scanRun(sc interface{ Scan(...any) error }) (SimRun, error) {
	var r SimRun
	var problems, measures, plots string
	if err := sc.Scan(&r.ID, &r.VariationID, &r.Variation, &r.Condition, &r.State, &r.Ngspice, &r.DeckSHA, &r.Started, &r.Finished, &r.TookMS, &r.Error, &problems, &measures, &plots, &r.Log); err != nil {
		return r, err
	}
	_ = json.Unmarshal([]byte(problems), &r.Problems)
	_ = json.Unmarshal([]byte(measures), &r.Measures)
	_ = json.Unmarshal([]byte(plots), &r.Plots)
	return r, nil
}

// Runs lists a test's runs, of every variation, newest first.
func (s *Store) Runs(ctx context.Context, simID int64) ([]SimRun, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+runCols+runFrom+`WHERE v.simulation_id = ? ORDER BY r.id DESC`, simID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []SimRun{}
	for rows.Next() {
		r, err := scanRun(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// Run finds a run by id.
func (s *Store) Run(ctx context.Context, id int64) (SimRun, error) {
	r, err := scanRun(s.db.QueryRowContext(ctx, `SELECT `+runCols+runFrom+`WHERE r.id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return r, fmt.Errorf("run %d: %w", id, ErrNotFound)
	}
	return r, err
}

// runIDs lists the runs a query selects.
func (s *Store) runIDs(ctx context.Context, query string, args ...any) []int64 {
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var out []int64
	for rows.Next() {
		var id int64
		if rows.Scan(&id) == nil {
			out = append(out, id)
		}
	}
	return out
}

// removeRuns deletes runs' folders.
func (s *Store) removeRuns(ids []int64) {
	for _, id := range ids {
		_ = os.RemoveAll(s.RunDir(id))
	}
}

// projectRunIDs lists the runs of a project's tests.
func (s *Store) projectRunIDs(ctx context.Context, projectID int64) []int64 {
	return s.runIDs(ctx, `SELECT r.id FROM simulation_runs r JOIN simulation_variations v ON v.id = r.variation_id JOIN simulations m ON m.id = v.simulation_id WHERE m.project_id = ?`, projectID)
}
