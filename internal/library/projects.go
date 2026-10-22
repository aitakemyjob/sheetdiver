package library

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// Project groups the parts of one board or design.
type Project struct {
	ID          int64     `json:"id"`
	Name        string    `json:"name"`
	Description string    `json:"description"`
	CreatedAt   time.Time `json:"createdAt"`
	UpdatedAt   time.Time `json:"updatedAt"`
	// Counts, filled by Projects.
	Parts int `json:"parts"`
	Rails int `json:"rails"`
}

// Rail is a supply net on a board. Voltages are in volts and the current
// limit in amps.
type Rail struct {
	ID           int64    `json:"id"`
	ProjectID    int64    `json:"projectId"`
	Name         string   `json:"name"`
	VNom         *float64 `json:"vNom,omitempty"`
	VMin         *float64 `json:"vMin,omitempty"`
	VMax         *float64 `json:"vMax,omitempty"`
	Source       string   `json:"source,omitempty"`
	CurrentLimit *float64 `json:"currentLimit,omitempty"`
	// OtherTyp and OtherMax are loads on the rail that aren't in the BOM
	// (a module, "the sensors"), in amps.
	OtherTyp *float64 `json:"otherTyp,omitempty"`
	OtherMax *float64 `json:"otherMax,omitempty"`
	Notes    string   `json:"notes,omitempty"`
}

// ProjectPart is a part placed in a project: its designators, quantity,
// role and the rails it draws from and drives. ITyp and IMax are the
// current (amps) each instance draws from its supply rail.
type ProjectPart struct {
	ID        int64  `json:"id"`
	ProjectID int64  `json:"projectId"`
	PartID    int64  `json:"partId"`
	MPN       string `json:"mpn"`
	Category  string `json:"category"`
	Status    string `json:"status,omitempty"`
	// HasDatasheet is false for BOM placeholders not yet imported.
	HasDatasheet bool `json:"hasDatasheet"`
	// DatasheetID is the part's datasheet (its pin table), 0 without one.
	DatasheetID int64    `json:"datasheetId,omitempty"`
	Designator  string   `json:"designator,omitempty"`
	Qty         int      `json:"qty"`
	Role        string   `json:"role,omitempty"`
	SupplyRail  string   `json:"supplyRail,omitempty"`
	OutputRail  string   `json:"outputRail,omitempty"`
	ITyp        *float64 `json:"iTyp,omitempty"`
	IMax        *float64 `json:"iMax,omitempty"`
	Variant     string   `json:"variant,omitempty"`
	Notes       string   `json:"notes,omitempty"`
}

// Calculation is a saved calculator run.
type Calculation struct {
	ID        int64           `json:"id"`
	ProjectID int64           `json:"projectId,omitempty"`
	PartID    int64           `json:"partId,omitempty"`
	Kind      string          `json:"kind"`
	Title     string          `json:"title"`
	Inputs    json.RawMessage `json:"inputs"`
	Outputs   json.RawMessage `json:"outputs"`
	CreatedAt time.Time       `json:"createdAt"`
}

// CreateProject adds a project; names are unique (ignoring case).
func (s *Store) CreateProject(ctx context.Context, name, desc string) (Project, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return Project{}, errors.New("a project needs a name")
	}
	res, err := s.db.ExecContext(ctx, `INSERT INTO projects (name, description, created_at, updated_at) VALUES (?,?,?,?)`, name, desc, now(), now())
	if err != nil {
		if strings.Contains(err.Error(), "UNIQUE") {
			return Project{}, fmt.Errorf("a project named %q already exists", name)
		}
		return Project{}, err
	}
	id, _ := res.LastInsertId()
	return s.Project(ctx, strconv.FormatInt(id, 10))
}

// Project finds a project by ID or name.
func (s *Store) Project(ctx context.Context, ref string) (Project, error) {
	var p Project
	var c, u string
	q := `SELECT id, name, description, created_at, updated_at FROM projects WHERE name = ?`
	arg := any(strings.TrimSpace(ref))
	if id, err := strconv.ParseInt(strings.TrimPrefix(ref, "#"), 10, 64); err == nil {
		q, arg = `SELECT id, name, description, created_at, updated_at FROM projects WHERE id = ?`, id
	}
	err := s.db.QueryRowContext(ctx, q, arg).Scan(&p.ID, &p.Name, &p.Description, &c, &u)
	if errors.Is(err, sql.ErrNoRows) {
		names, _ := s.Projects(ctx)
		var list []string
		for _, n := range names {
			list = append(list, n.Name)
		}
		if len(list) == 0 {
			return p, fmt.Errorf("project %q: %w (there are no projects yet)", ref, ErrNotFound)
		}
		return p, fmt.Errorf("project %q: %w; projects: %s", ref, ErrNotFound, strings.Join(list, ", "))
	}
	p.CreatedAt, p.UpdatedAt = parseTime(c), parseTime(u)
	return p, err
}

// Projects lists projects with part and rail counts.
func (s *Store) Projects(ctx context.Context) ([]Project, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, name, description, created_at, updated_at,
		(SELECT count(*) FROM project_parts pp WHERE pp.project_id = projects.id),
		(SELECT count(*) FROM project_rails pr WHERE pr.project_id = projects.id)
		FROM projects ORDER BY updated_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Project{}
	for rows.Next() {
		var p Project
		var c, u string
		if err := rows.Scan(&p.ID, &p.Name, &p.Description, &c, &u, &p.Parts, &p.Rails); err != nil {
			return nil, err
		}
		p.CreatedAt, p.UpdatedAt = parseTime(c), parseTime(u)
		out = append(out, p)
	}
	return out, rows.Err()
}

// UpdateProject renames or redescribes a project.
func (s *Store) UpdateProject(ctx context.Context, id int64, name, desc string) error {
	if strings.TrimSpace(name) == "" {
		return errors.New("a project needs a name")
	}
	_, err := s.db.ExecContext(ctx, `UPDATE projects SET name = ?, description = ?, updated_at = ? WHERE id = ?`, strings.TrimSpace(name), desc, now(), id)
	return err
}

// DeleteProject removes a project (its parts stay in the library).
func (s *Store) DeleteProject(ctx context.Context, id int64) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM projects WHERE id = ?`, id)
	return err
}

// touch bumps a project's modification time.
func (s *Store) touch(ctx context.Context, id int64) {
	_, _ = s.db.ExecContext(ctx, `UPDATE projects SET updated_at = ? WHERE id = ?`, now(), id)
}

// Rails lists a project's rails by name.
func (s *Store) Rails(ctx context.Context, projectID int64) ([]Rail, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, name, v_nom, v_min, v_max, source, current_limit, other_typ, other_max, notes FROM project_rails WHERE project_id = ? ORDER BY name`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Rail{}
	for rows.Next() {
		r := Rail{ProjectID: projectID}
		var nom, mn, mx, lim, ot, om sql.NullFloat64
		if err := rows.Scan(&r.ID, &r.Name, &nom, &mn, &mx, &r.Source, &lim, &ot, &om, &r.Notes); err != nil {
			return nil, err
		}
		r.VNom, r.VMin, r.VMax, r.CurrentLimit = floatPtr(nom), floatPtr(mn), floatPtr(mx), positive(floatPtr(lim))
		r.OtherTyp, r.OtherMax = floatPtr(ot), floatPtr(om)
		out = append(out, r)
	}
	return out, rows.Err()
}

// positive returns v when it's above zero, else nil.
func positive(v *float64) *float64 {
	if v == nil || *v <= 0 {
		return nil
	}
	return v
}

// SetRail adds or replaces a rail by name.
func (s *Store) SetRail(ctx context.Context, r Rail) (Rail, error) {
	r.Name = strings.TrimSpace(r.Name)
	if r.Name == "" {
		return r, errors.New("a rail needs a name, e.g. 3V3 or VBUS")
	}
	// A current limit of zero (or less) means none: the regulator's
	// rating then sets the rail's capacity.
	r.CurrentLimit = positive(r.CurrentLimit)
	if r.VNom == nil && r.VMin != nil && r.VMax != nil {
		v := (*r.VMin + *r.VMax) / 2
		r.VNom = &v
	}
	_, err := s.db.ExecContext(ctx, `INSERT INTO project_rails (project_id, name, v_nom, v_min, v_max, source, current_limit, other_typ, other_max, notes) VALUES (?,?,?,?,?,?,?,?,?,?)
		ON CONFLICT(project_id, name) DO UPDATE SET v_nom = excluded.v_nom, v_min = excluded.v_min, v_max = excluded.v_max, source = excluded.source, current_limit = excluded.current_limit, other_typ = excluded.other_typ, other_max = excluded.other_max, notes = excluded.notes`,
		r.ProjectID, r.Name, nullFloat(r.VNom), nullFloat(r.VMin), nullFloat(r.VMax), r.Source, nullFloat(r.CurrentLimit), nullFloat(r.OtherTyp), nullFloat(r.OtherMax), r.Notes)
	if err != nil {
		return r, err
	}
	s.touch(ctx, r.ProjectID)
	rails, err := s.Rails(ctx, r.ProjectID)
	for _, x := range rails {
		if strings.EqualFold(x.Name, r.Name) {
			return x, err
		}
	}
	return r, err
}

// DeleteRail removes a rail by name.
func (s *Store) DeleteRail(ctx context.Context, projectID int64, name string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM project_rails WHERE project_id = ? AND name = ?`, projectID, name)
	s.touch(ctx, projectID)
	return err
}

// ProjectParts lists a project's parts (its BOM).
func (s *Store) ProjectParts(ctx context.Context, projectID int64) ([]ProjectPart, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT pp.id, pp.part_id, p.mpn, p.category, p.status, IFNULL(p.datasheet_id, 0), pp.designator, pp.qty, pp.role, pp.supply_rail, pp.output_rail, pp.i_typ, pp.i_max, pp.variant, pp.notes
		FROM project_parts pp JOIN parts p ON p.id = pp.part_id WHERE pp.project_id = ? ORDER BY pp.designator, p.mpn`, projectID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ProjectPart{}
	for rows.Next() {
		pp := ProjectPart{ProjectID: projectID}
		var it, im sql.NullFloat64
		if err := rows.Scan(&pp.ID, &pp.PartID, &pp.MPN, &pp.Category, &pp.Status, &pp.DatasheetID, &pp.Designator, &pp.Qty, &pp.Role, &pp.SupplyRail, &pp.OutputRail, &it, &im, &pp.Variant, &pp.Notes); err != nil {
			return nil, err
		}
		pp.ITyp, pp.IMax = floatPtr(it), floatPtr(im)
		pp.HasDatasheet = pp.DatasheetID != 0
		out = append(out, pp)
	}
	return out, rows.Err()
}

// AddProjectPart places a part in a project. If the same part with the
// same designator is already there, it is updated instead.
func (s *Store) AddProjectPart(ctx context.Context, pp ProjectPart) (ProjectPart, error) {
	if pp.Qty <= 0 {
		pp.Qty = 1
	}
	var id int64
	err := s.db.QueryRowContext(ctx, `SELECT id FROM project_parts WHERE project_id = ? AND part_id = ? AND designator = ?`, pp.ProjectID, pp.PartID, pp.Designator).Scan(&id)
	if err == nil {
		pp.ID = id
		return pp, s.UpdateProjectPart(ctx, pp)
	}
	res, err := s.db.ExecContext(ctx, `INSERT INTO project_parts (project_id, part_id, designator, qty, role, supply_rail, output_rail, i_typ, i_max, variant, notes) VALUES (?,?,?,?,?,?,?,?,?,?,?)`,
		pp.ProjectID, pp.PartID, pp.Designator, pp.Qty, pp.Role, pp.SupplyRail, pp.OutputRail, nullFloat(pp.ITyp), nullFloat(pp.IMax), pp.Variant, pp.Notes)
	if err != nil {
		return pp, err
	}
	pp.ID, _ = res.LastInsertId()
	s.touch(ctx, pp.ProjectID)
	return pp, nil
}

// UpdateProjectPart saves a project part's placement details.
func (s *Store) UpdateProjectPart(ctx context.Context, pp ProjectPart) error {
	if pp.Qty <= 0 {
		pp.Qty = 1
	}
	res, err := s.db.ExecContext(ctx, `UPDATE project_parts SET designator = ?, qty = ?, role = ?, supply_rail = ?, output_rail = ?, i_typ = ?, i_max = ?, variant = ?, notes = ? WHERE id = ? AND project_id = ?`,
		pp.Designator, pp.Qty, pp.Role, pp.SupplyRail, pp.OutputRail, nullFloat(pp.ITyp), nullFloat(pp.IMax), pp.Variant, pp.Notes, pp.ID, pp.ProjectID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("project part %d: %w", pp.ID, ErrNotFound)
	}
	s.touch(ctx, pp.ProjectID)
	return nil
}

// RemoveProjectPart takes a part out of a project.
func (s *Store) RemoveProjectPart(ctx context.Context, projectID, id int64) error {
	res, err := s.db.ExecContext(ctx, `DELETE FROM project_parts WHERE id = ? AND project_id = ?`, id, projectID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("project part %d: %w", id, ErrNotFound)
	}
	// Its connections go with it (cascade); so does its place on the
	// block diagram, which a later entry could otherwise inherit.
	if _, err := s.db.ExecContext(ctx, `DELETE FROM project_layout WHERE project_id = ? AND node = ?`, projectID, PartNode(id)); err != nil {
		return err
	}
	s.touch(ctx, projectID)
	return nil
}

// ProjectsOfPart lists the projects a part is used in.
func (s *Store) ProjectsOfPart(ctx context.Context, partID int64) ([]Project, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT DISTINCT pr.id, pr.name FROM projects pr JOIN project_parts pp ON pp.project_id = pr.id WHERE pp.part_id = ? ORDER BY pr.name`, partID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Project{}
	for rows.Next() {
		var p Project
		if err := rows.Scan(&p.ID, &p.Name); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// SaveCalculation stores a calculator run.
func (s *Store) SaveCalculation(ctx context.Context, c Calculation) (Calculation, error) {
	var proj, part any
	if c.ProjectID != 0 {
		proj = c.ProjectID
	}
	if c.PartID != 0 {
		part = c.PartID
	}
	if len(c.Inputs) == 0 {
		c.Inputs = json.RawMessage("{}")
	}
	if len(c.Outputs) == 0 {
		c.Outputs = json.RawMessage("{}")
	}
	res, err := s.db.ExecContext(ctx, `INSERT INTO calculations (project_id, part_id, kind, title, inputs, outputs, created_at) VALUES (?,?,?,?,?,?,?)`,
		proj, part, c.Kind, c.Title, string(c.Inputs), string(c.Outputs), now())
	if err != nil {
		return c, err
	}
	c.ID, _ = res.LastInsertId()
	c.CreatedAt = time.Now().UTC()
	if c.ProjectID != 0 {
		s.touch(ctx, c.ProjectID)
	}
	return c, nil
}

// Calculations lists saved calculations, newest first, for a project (or
// all when projectID is 0).
func (s *Store) Calculations(ctx context.Context, projectID int64) ([]Calculation, error) {
	q := `SELECT id, COALESCE(project_id, 0), COALESCE(part_id, 0), kind, title, inputs, outputs, created_at FROM calculations`
	var args []any
	if projectID != 0 {
		q += ` WHERE project_id = ?`
		args = append(args, projectID)
	}
	rows, err := s.db.QueryContext(ctx, q+` ORDER BY created_at DESC, id DESC`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Calculation{}
	for rows.Next() {
		var c Calculation
		var in, outp, at string
		if err := rows.Scan(&c.ID, &c.ProjectID, &c.PartID, &c.Kind, &c.Title, &in, &outp, &at); err != nil {
			return nil, err
		}
		c.Inputs, c.Outputs, c.CreatedAt = json.RawMessage(in), json.RawMessage(outp), parseTime(at)
		out = append(out, c)
	}
	return out, rows.Err()
}

// DeleteCalculation removes a saved calculation.
func (s *Store) DeleteCalculation(ctx context.Context, id int64) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM calculations WHERE id = ?`, id)
	return err
}
