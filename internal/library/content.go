package library

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/aitakemyjob/sheetdiver/internal/pdf"
)

// ---- sections ----------------------------------------------------------------

// Sections lists a datasheet's sections in order. withText includes each
// section's text.
func (s *Store) Sections(ctx context.Context, dsID int64, withText bool) ([]Section, error) {
	text := `''`
	if withText {
		text = `text`
	}
	rows, err := s.db.QueryContext(ctx, `SELECT id, ord, num, title, level, kind, page, y, end_page, end_y, `+text+` FROM sections`+ //nolint:gosec // text is one of two constants
		` WHERE datasheet_id = ? ORDER BY ord`, dsID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Section{}
	for rows.Next() {
		sec := Section{DatasheetID: dsID}
		if err := rows.Scan(&sec.ID, &sec.Ord, &sec.Num, &sec.Title, &sec.Level, &sec.Kind, &sec.Page, &sec.Y, &sec.EndPage, &sec.EndY, &sec.Text); err != nil {
			return nil, err
		}
		out = append(out, sec)
	}
	return out, rows.Err()
}

// Section returns one section with its text.
func (s *Store) Section(ctx context.Context, id int64) (Section, error) {
	var sec Section
	err := s.db.QueryRowContext(ctx, `SELECT id, datasheet_id, ord, num, title, level, kind, page, y, end_page, end_y, text FROM sections WHERE id = ?`, id).
		Scan(&sec.ID, &sec.DatasheetID, &sec.Ord, &sec.Num, &sec.Title, &sec.Level, &sec.Kind, &sec.Page, &sec.Y, &sec.EndPage, &sec.EndY, &sec.Text)
	if errors.Is(err, sql.ErrNoRows) {
		return sec, fmt.Errorf("section %d: %w", id, ErrNotFound)
	}
	return sec, err
}

// FindSections returns a datasheet's sections matching a kind or a title
// (or number) substring, with text.
func (s *Store) FindSections(ctx context.Context, dsID int64, kindOrTitle string) ([]Section, error) {
	all, err := s.Sections(ctx, dsID, true)
	if err != nil {
		return nil, err
	}
	q := strings.ToLower(strings.TrimSpace(kindOrTitle))
	var out []Section
	for _, sec := range all {
		if sec.Kind == q {
			out = append(out, sec)
		}
	}
	if len(out) > 0 {
		return out, nil
	}
	for _, sec := range all {
		if strings.Contains(strings.ToLower(sec.Num+" "+sec.Title), q) {
			out = append(out, sec)
		}
	}
	return out, nil
}

// ---- figures -----------------------------------------------------------------

// figureCols is the column list scanFigure reads.
const figureCols = `id, datasheet_id, page, number, caption, kind, x0, y0, x1, y1, section, title, x_axis, y_axis, conditions, description, curves, applies_to, source, updated_at`

// scanFigure reads a figure row.
func scanFigure(sc interface{ Scan(...any) error }) (Figure, error) {
	var f Figure
	var curves, applies, at string
	err := sc.Scan(&f.ID, &f.DatasheetID, &f.Page, &f.Number, &f.Caption, &f.Kind, &f.Box.X0, &f.Box.Y0, &f.Box.X1, &f.Box.Y1,
		&f.Section, &f.Title, &f.XAxis, &f.YAxis, &f.Conditions, &f.Description, &curves, &applies, &f.Source, &at)
	if err != nil {
		return f, err
	}
	_ = json.Unmarshal([]byte(curves), &f.Curves)
	f.AppliesTo = splitList(applies)
	f.UpdatedAt = parseTime(at)
	return f, nil
}

// FigureFilter narrows Figures.
type FigureFilter struct {
	Kind  string `json:"kind,omitempty"`
	Query string `json:"query,omitempty"`
	Page  *int   `json:"page,omitempty"`
	// MPN keeps only figures that apply to this part.
	MPN string `json:"mpn,omitempty"`
}

// Figures lists a datasheet's figures in page order.
func (s *Store) Figures(ctx context.Context, dsID int64, f FigureFilter) ([]Figure, error) {
	q := `SELECT ` + figureCols + ` FROM figures WHERE datasheet_id = ?`
	args := []any{dsID}
	if f.Kind != "" {
		q += ` AND kind = ?`
		args = append(args, f.Kind)
	}
	if f.Page != nil {
		q += ` AND page = ?`
		args = append(args, *f.Page)
	}
	if f.Query != "" {
		like := "%" + f.Query + "%"
		q += ` AND (caption LIKE ? OR title LIKE ? OR description LIKE ? OR x_axis LIKE ? OR y_axis LIKE ? OR number = ?)`
		args = append(args, like, like, like, like, like, f.Query)
	}
	q += ` ORDER BY page, y0, x0`
	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Figure{}
	for rows.Next() {
		fig, err := scanFigure(rows)
		if err != nil {
			return nil, err
		}
		if appliesTo(fig.AppliesTo, f.MPN) {
			out = append(out, fig)
		}
	}
	return out, rows.Err()
}

// Figure returns a figure by ID.
func (s *Store) Figure(ctx context.Context, id int64) (Figure, error) {
	f, err := scanFigure(s.db.QueryRowContext(ctx, `SELECT `+figureCols+` FROM figures WHERE id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return f, fmt.Errorf("figure %d: %w", id, ErrNotFound)
	}
	return f, err
}

// FigureByNumber returns a datasheet's figure by its number ("6-8").
func (s *Store) FigureByNumber(ctx context.Context, dsID int64, number string) (Figure, error) {
	f, err := scanFigure(s.db.QueryRowContext(ctx, `SELECT `+figureCols+` FROM figures WHERE datasheet_id = ? AND number = ?`, dsID, strings.TrimSpace(number)))
	if errors.Is(err, sql.ErrNoRows) {
		return f, fmt.Errorf("figure %s: %w", number, ErrNotFound)
	}
	return f, err
}

// FigureUpdate describes what a figure shows; nil fields are unchanged.
type FigureUpdate struct {
	Kind        *string   `json:"kind,omitempty"`
	Title       *string   `json:"title,omitempty"`
	XAxis       *string   `json:"xAxis,omitempty"`
	YAxis       *string   `json:"yAxis,omitempty"`
	Conditions  *string   `json:"conditions,omitempty"`
	Description *string   `json:"description,omitempty"`
	Curves      *[]Curve  `json:"curves,omitempty"`
	AppliesTo   *[]string `json:"appliesTo,omitempty"`
	Box         *pdf.Rect `json:"box,omitempty"`
	Caption     *string   `json:"caption,omitempty"`
}

// UpdateFigure records what a figure shows. The figure's source becomes
// source, so re-extraction keeps it.
func (s *Store) UpdateFigure(ctx context.Context, id int64, u FigureUpdate, source string) (Figure, error) {
	f, err := s.Figure(ctx, id)
	if err != nil {
		return f, err
	}
	str := func(dst *string, v *string) {
		if v != nil {
			*dst = strings.TrimSpace(*v)
		}
	}
	str(&f.Kind, u.Kind)
	str(&f.Title, u.Title)
	str(&f.XAxis, u.XAxis)
	str(&f.YAxis, u.YAxis)
	str(&f.Conditions, u.Conditions)
	str(&f.Description, u.Description)
	str(&f.Caption, u.Caption)
	if u.Curves != nil {
		f.Curves = *u.Curves
	}
	if u.AppliesTo != nil {
		f.AppliesTo = *u.AppliesTo
	}
	if u.Box != nil && !u.Box.Empty() {
		f.Box = *u.Box
	}
	curves, _ := json.Marshal(nonNil(f.Curves))
	_, err = s.db.ExecContext(ctx, `UPDATE figures SET kind = ?, title = ?, x_axis = ?, y_axis = ?, conditions = ?, description = ?, curves = ?, applies_to = ?, caption = ?,
		x0 = ?, y0 = ?, x1 = ?, y1 = ?, source = ?, updated_at = ? WHERE id = ?`,
		f.Kind, f.Title, f.XAxis, f.YAxis, f.Conditions, f.Description, string(curves), strings.Join(f.AppliesTo, ","), f.Caption,
		f.Box.X0, f.Box.Y0, f.Box.X1, f.Box.Y1, source, now(), id)
	if err != nil {
		return f, err
	}
	f.Source = source
	return f, s.Reindex(ctx, f.DatasheetID)
}

// AddFigure stores a figure the extractor missed.
func (s *Store) AddFigure(ctx context.Context, f Figure) (Figure, error) {
	var id int64
	err := s.tx(ctx, func(tx *sql.Tx) error {
		var err error
		id, err = insertFigure(ctx, tx, f)
		return err
	})
	if err != nil {
		return f, err
	}
	if err := s.Reindex(ctx, f.DatasheetID); err != nil {
		return f, err
	}
	return s.Figure(ctx, id)
}

// FigureKinds counts a datasheet's figures by kind.
func (s *Store) FigureKinds(ctx context.Context, dsID int64) (map[string]int, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT kind, count(*) FROM figures WHERE datasheet_id = ? GROUP BY kind`, dsID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]int{}
	for rows.Next() {
		var k string
		var n int
		if err := rows.Scan(&k, &n); err != nil {
			return nil, err
		}
		out[k] = n
	}
	return out, rows.Err()
}

// ---- tables ------------------------------------------------------------------

// tableCols is the column list scanTable reads (rows last, so listings
// can leave it out).
const tableCols = `id, datasheet_id, page, number, caption, kind, section, x0, y0, x1, y1, header, irregular, title, description, source, updated_at`

// scanTable reads a table row; rows is the JSON of its cells.
func scanTable(sc scanner, withRows bool) (Table, error) {
	var t Table
	var h, r, at string
	dest := []any{&t.ID, &t.DatasheetID, &t.Page, &t.Number, &t.Caption, &t.Kind, &t.Section, &t.Box.X0, &t.Box.Y0, &t.Box.X1, &t.Box.Y1, &h, &t.Irregular, &t.Title, &t.Description, &t.Source, &at}
	if withRows {
		dest = append(dest, &r)
	}
	if err := sc.Scan(dest...); err != nil {
		return t, err
	}
	_ = json.Unmarshal([]byte(h), &t.Header)
	if withRows {
		_ = json.Unmarshal([]byte(r), &t.Rows)
	}
	t.UpdatedAt = parseTime(at)
	return t, nil
}

// Tables lists a datasheet's tables; withRows includes their cells.
func (s *Store) Tables(ctx context.Context, dsID int64, withRows bool) ([]Table, error) {
	q := `SELECT ` + tableCols + ` FROM doc_tables WHERE datasheet_id = ? ORDER BY page, y0`
	if withRows {
		q = `SELECT ` + tableCols + `, rows FROM doc_tables WHERE datasheet_id = ? ORDER BY page, y0`
	}
	rows, err := s.db.QueryContext(ctx, q, dsID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Table{}
	for rows.Next() {
		t, err := scanTable(rows, withRows)
		if err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// Table returns a table with its rows.
func (s *Store) Table(ctx context.Context, id int64) (Table, error) {
	t, err := scanTable(s.db.QueryRowContext(ctx, `SELECT `+tableCols+`, rows FROM doc_tables WHERE id = ?`, id), true)
	if errors.Is(err, sql.ErrNoRows) {
		return t, fmt.Errorf("table %d: %w", id, ErrNotFound)
	}
	return t, err
}

// TableByNumber finds a datasheet's table by number ("7-2"); a table
// continued over pages returns its first part.
func (s *Store) TableByNumber(ctx context.Context, dsID int64, number string) (Table, error) {
	t, err := scanTable(s.db.QueryRowContext(ctx, `SELECT `+tableCols+`, rows FROM doc_tables WHERE datasheet_id = ? AND number = ? ORDER BY page LIMIT 1`, dsID, strings.TrimSpace(number)), true)
	if errors.Is(err, sql.ErrNoRows) {
		return t, fmt.Errorf("table %s: %w", number, ErrNotFound)
	}
	return t, err
}

// TableUpdate is a transcription of a table; nil fields are unchanged.
type TableUpdate struct {
	Title       *string     `json:"title,omitempty"`
	Description *string     `json:"description,omitempty"`
	Header      *[]string   `json:"header,omitempty"`
	Rows        *[][]string `json:"rows,omitempty"`
	Box         *pdf.Rect   `json:"box,omitempty"`
	Kind        *string     `json:"kind,omitempty"`
}

// RecordTable stores a transcription of a table (by the agent, after
// reading its image, or by the user). The table is no longer irregular,
// and re-extraction leaves it alone.
func (s *Store) RecordTable(ctx context.Context, id int64, u TableUpdate, source string) (Table, error) {
	t, err := s.Table(ctx, id)
	if err != nil {
		return t, err
	}
	if u.Title != nil {
		t.Title = strings.TrimSpace(*u.Title)
	}
	if u.Description != nil {
		t.Description = strings.TrimSpace(*u.Description)
	}
	if u.Header != nil {
		t.Header = *u.Header
	}
	if u.Rows != nil {
		t.Rows = *u.Rows
	}
	if u.Box != nil && !u.Box.Empty() {
		t.Box = *u.Box
	}
	if u.Kind != nil {
		t.Kind = *u.Kind
	}
	if u.Header != nil || u.Rows != nil {
		t.Irregular = ""
	}
	h, _ := json.Marshal(nonNil(t.Header))
	r, _ := json.Marshal(nonNilRows(t.Rows))
	err = s.tx(ctx, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, `UPDATE doc_tables SET title = ?, description = ?, header = ?, rows = ?, kind = ?, x0 = ?, y0 = ?, x1 = ?, y1 = ?, irregular = ?, source = ?, updated_at = ? WHERE id = ?`,
			t.Title, t.Description, string(h), string(r), t.Kind, t.Box.X0, t.Box.Y0, t.Box.X1, t.Box.Y1, t.Irregular, source, now(), id); err != nil {
			return err
		}
		return reindexDatasheet(ctx, tx, t.DatasheetID)
	})
	if err != nil {
		return t, err
	}
	return s.Table(ctx, id)
}

// AddTable stores a table the extractor missed.
func (s *Store) AddTable(ctx context.Context, t Table, source string) (Table, error) {
	h, _ := json.Marshal(nonNil(t.Header))
	r, _ := json.Marshal(nonNilRows(t.Rows))
	var id int64
	err := s.tx(ctx, func(tx *sql.Tx) error {
		res, err := tx.ExecContext(ctx, `INSERT INTO doc_tables (datasheet_id, page, number, caption, kind, section, x0, y0, x1, y1, header, rows, title, description, source, updated_at) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
			t.DatasheetID, t.Page, t.Number, t.Caption, firstNonEmptyStr(t.Kind, "table"), t.Section, t.Box.X0, t.Box.Y0, t.Box.X1, t.Box.Y1, string(h), string(r), t.Title, t.Description, source, now())
		if err != nil {
			return err
		}
		id, _ = res.LastInsertId()
		return reindexDatasheet(ctx, tx, t.DatasheetID)
	})
	if err != nil {
		return t, err
	}
	return s.Table(ctx, id)
}

// firstNonEmptyStr returns a, or b when a is empty.
func firstNonEmptyStr(a, b string) string {
	if strings.TrimSpace(a) != "" {
		return a
	}
	return b
}

// ---- parameters --------------------------------------------------------------

// paramCols is the column list scanParam reads.
const paramCols = `id, datasheet_id, applies_to, key, symbol, name, conditions, min, typ, max, unit, section, page, x0, y0, x1, y1, raw, note, source, updated_at`

// scanParam reads a parameter row.
func scanParam(sc interface{ Scan(...any) error }) (Parameter, error) {
	var p Parameter
	var applies, at string
	var mn, ty, mx, x0, y0, x1, y1 sql.NullFloat64
	err := sc.Scan(&p.ID, &p.DatasheetID, &applies, &p.Key, &p.Symbol, &p.Name, &p.Conditions, &mn, &ty, &mx, &p.Unit, &p.Section, &p.Page, &x0, &y0, &x1, &y1, &p.Raw, &p.Note, &p.Source, &at)
	if err != nil {
		return p, err
	}
	p.AppliesTo = splitList(applies)
	p.Min, p.Typ, p.Max = floatPtr(mn), floatPtr(ty), floatPtr(mx)
	if x0.Valid && x1.Valid {
		p.Box = &pdf.Rect{X0: x0.Float64, Y0: y0.Float64, X1: x1.Float64, Y1: y1.Float64}
	}
	p.UpdatedAt = parseTime(at)
	return p, nil
}

// ParamFilter narrows Parameters.
type ParamFilter struct {
	Key     string `json:"key,omitempty"`
	Section string `json:"section,omitempty"`
	Query   string `json:"query,omitempty"`
	Source  string `json:"source,omitempty"`
	// MPN keeps only parameters that apply to this part.
	MPN string `json:"mpn,omitempty"`
}

// Parameters lists a datasheet's parameters. Agent- and user-recorded
// values come before automatic ones for the same key.
func (s *Store) Parameters(ctx context.Context, dsID int64, f ParamFilter) ([]Parameter, error) {
	q := `SELECT ` + paramCols + ` FROM parameters WHERE datasheet_id = ?`
	args := []any{dsID}
	if f.Key != "" {
		q += ` AND key = ?`
		args = append(args, f.Key)
	}
	if f.Section != "" {
		q += ` AND section = ?`
		args = append(args, f.Section)
	}
	if f.Source != "" {
		q += ` AND source = ?`
		args = append(args, f.Source)
	}
	if f.Query != "" {
		like := "%" + f.Query + "%"
		q += ` AND (name LIKE ? OR symbol LIKE ? OR conditions LIKE ? OR key = ?)`
		args = append(args, like, like, like, f.Query)
	}
	q += ` ORDER BY CASE source WHEN 'user' THEN 0 WHEN 'agent' THEN 1 ELSE 2 END, page, y0, id`
	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Parameter{}
	for rows.Next() {
		p, err := scanParam(rows)
		if err != nil {
			return nil, err
		}
		if appliesTo(p.AppliesTo, f.MPN) {
			out = append(out, p)
		}
	}
	return out, rows.Err()
}

// Parameter returns one parameter.
func (s *Store) Parameter(ctx context.Context, id int64) (Parameter, error) {
	p, err := scanParam(s.db.QueryRowContext(ctx, `SELECT `+paramCols+` FROM parameters WHERE id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return p, fmt.Errorf("parameter %d: %w", id, ErrNotFound)
	}
	return p, err
}

// RecordParameters stores parameters from the agent or user. A parameter
// with an ID updates that row (taking it over from the extractor);
// without one it is added. When replaceKeys is set, automatic rows with
// the same key and section are removed so the recorded values win.
func (s *Store) RecordParameters(ctx context.Context, dsID int64, ps []Parameter, source string, replaceKeys bool) ([]Parameter, error) {
	var ids []int64
	err := s.tx(ctx, func(tx *sql.Tx) error {
		for _, p := range ps {
			p.DatasheetID, p.Source = dsID, source
			if replaceKeys && p.Key != "" {
				if _, err := tx.ExecContext(ctx, `DELETE FROM parameters WHERE datasheet_id = ? AND key = ? AND section = ? AND source = 'auto' AND id != ?`, dsID, p.Key, p.Section, p.ID); err != nil {
					return err
				}
			}
			if p.ID != 0 {
				var x0, y0, x1, y1 any
				if p.Box != nil {
					x0, y0, x1, y1 = p.Box.X0, p.Box.Y0, p.Box.X1, p.Box.Y1
				}
				res, err := tx.ExecContext(ctx, `UPDATE parameters SET applies_to = ?, key = ?, symbol = ?, name = ?, conditions = ?, min = ?, typ = ?, max = ?, unit = ?, section = ?,
					page = CASE WHEN ? >= 0 THEN ? ELSE page END, x0 = COALESCE(?, x0), y0 = COALESCE(?, y0), x1 = COALESCE(?, x1), y1 = COALESCE(?, y1), note = ?, source = ?, updated_at = ?
					WHERE id = ? AND datasheet_id = ?`,
					strings.Join(p.AppliesTo, ","), p.Key, p.Symbol, p.Name, p.Conditions, nullFloat(p.Min), nullFloat(p.Typ), nullFloat(p.Max), p.Unit, p.Section,
					p.Page, p.Page, x0, y0, x1, y1, p.Note, source, now(), p.ID, dsID)
				if err != nil {
					return err
				}
				if n, _ := res.RowsAffected(); n == 0 {
					return fmt.Errorf("parameter %d is not on this datasheet", p.ID)
				}
				ids = append(ids, p.ID)
				continue
			}
			id, err := insertParam(ctx, tx, p)
			if err != nil {
				return err
			}
			ids = append(ids, id)
		}
		return reindexDatasheet(ctx, tx, dsID)
	})
	if err != nil {
		return nil, err
	}
	out := make([]Parameter, 0, len(ids))
	for _, id := range ids {
		p, err := s.Parameter(ctx, id)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, nil
}

// DeleteParameters removes parameters from a datasheet.
func (s *Store) DeleteParameters(ctx context.Context, dsID int64, ids []int64) error {
	return s.tx(ctx, func(tx *sql.Tx) error {
		for _, id := range ids {
			if _, err := tx.ExecContext(ctx, `DELETE FROM parameters WHERE id = ? AND datasheet_id = ?`, id, dsID); err != nil {
				return err
			}
		}
		return reindexDatasheet(ctx, tx, dsID)
	})
}

// ---- pins --------------------------------------------------------------------

// Pins lists a datasheet's pins, optionally for one package.
func (s *Store) Pins(ctx context.Context, dsID int64, pkg string) ([]Pin, error) {
	q := `SELECT id, part, package, number, name, type, description, page, source FROM pins WHERE datasheet_id = ?`
	args := []any{dsID}
	if pkg != "" {
		q += ` AND package LIKE ?`
		args = append(args, "%"+pkg+"%")
	}
	rows, err := s.db.QueryContext(ctx, q+` ORDER BY package, CAST(number AS INTEGER), number, id`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Pin{}
	for rows.Next() {
		p := Pin{DatasheetID: dsID}
		if err := rows.Scan(&p.ID, &p.Part, &p.Package, &p.Number, &p.Name, &p.Type, &p.Description, &p.Page, &p.Source); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// RecordPins replaces a datasheet's pins for a package (all automatic
// pins when the package is new) with the given list, for every part of
// the datasheet.
func (s *Store) RecordPins(ctx context.Context, dsID int64, pkg string, pins []Pin, source string) error {
	return s.RecordPartPins(ctx, dsID, "", pkg, pins, source)
}

// RecordPartPins replaces the pins recorded for one part of a datasheet
// ("" for all of them) in a package with the given list. A package's
// table also replaces the table recorded without a package for the same
// part, so the two are never mixed; recording for every part also
// replaces the automatic pins.
func (s *Store) RecordPartPins(ctx context.Context, dsID int64, part, pkg string, pins []Pin, source string) error {
	return s.tx(ctx, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, `DELETE FROM pins WHERE datasheet_id = ? AND (
			(part = ? COLLATE NOCASE AND (package = ? OR (? <> '' AND package = '' AND source <> 'auto')))
			OR (? = '' AND source = 'auto'))`, dsID, part, pkg, pkg, part); err != nil {
			return err
		}
		for _, p := range pins {
			p.DatasheetID, p.Part, p.Package, p.Source = dsID, part, pkg, source
			if _, err := insertPin(ctx, tx, p); err != nil {
				return err
			}
		}
		return reindexDatasheet(ctx, tx, dsID)
	})
}

// ---- registers ---------------------------------------------------------------

// Registers lists a datasheet's recorded registers.
func (s *Store) Registers(ctx context.Context, dsID int64) ([]Register, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, name, address, width, reset, access, description, fields, page, source FROM registers WHERE datasheet_id = ? ORDER BY address, name`, dsID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Register{}
	for rows.Next() {
		r := Register{DatasheetID: dsID}
		var fields string
		if err := rows.Scan(&r.ID, &r.Name, &r.Address, &r.Width, &r.Reset, &r.Access, &r.Description, &fields, &r.Page, &r.Source); err != nil {
			return nil, err
		}
		_ = json.Unmarshal([]byte(fields), &r.Fields)
		out = append(out, r)
	}
	return out, rows.Err()
}

// RecordRegisters adds registers, replacing any with the same name.
func (s *Store) RecordRegisters(ctx context.Context, dsID int64, regs []Register, source string) error {
	return s.tx(ctx, func(tx *sql.Tx) error {
		for _, r := range regs {
			if strings.TrimSpace(r.Name) == "" {
				return errors.New("every register needs a name")
			}
			if _, err := tx.ExecContext(ctx, `DELETE FROM registers WHERE datasheet_id = ? AND name = ?`, dsID, r.Name); err != nil {
				return err
			}
			fields, _ := json.Marshal(nonNil(r.Fields))
			if _, err := tx.ExecContext(ctx, `INSERT INTO registers (datasheet_id, name, address, width, reset, access, description, fields, page, source) VALUES (?,?,?,?,?,?,?,?,?,?)`,
				dsID, r.Name, r.Address, r.Width, r.Reset, r.Access, r.Description, string(fields), r.Page, source); err != nil {
				return err
			}
		}
		return reindexDatasheet(ctx, tx, dsID)
	})
}

// ---- recommendations ---------------------------------------------------------

// Recommendations lists a datasheet's recommended components, optionally
// only those applying to one part.
func (s *Store) Recommendations(ctx context.Context, dsID int64, mpn string) ([]Recommendation, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, applies_to, role, value, mpn, manufacturer, conditions, notes, page, source FROM recommendations WHERE datasheet_id = ? ORDER BY role, id`, dsID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Recommendation{}
	for rows.Next() {
		r := Recommendation{DatasheetID: dsID}
		var applies string
		if err := rows.Scan(&r.ID, &applies, &r.Role, &r.Value, &r.MPN, &r.Manufacturer, &r.Conditions, &r.Notes, &r.Page, &r.Source); err != nil {
			return nil, err
		}
		r.AppliesTo = splitList(applies)
		if appliesTo(r.AppliesTo, mpn) {
			out = append(out, r)
		}
	}
	return out, rows.Err()
}

// RecordRecommendations adds recommended components.
func (s *Store) RecordRecommendations(ctx context.Context, dsID int64, recs []Recommendation, source string) error {
	return s.tx(ctx, func(tx *sql.Tx) error {
		for _, r := range recs {
			if strings.TrimSpace(r.Role) == "" {
				return errors.New("every recommendation needs a role (inductor, output capacitor, …)")
			}
			if _, err := tx.ExecContext(ctx, `INSERT INTO recommendations (datasheet_id, applies_to, role, value, mpn, manufacturer, conditions, notes, page, source) VALUES (?,?,?,?,?,?,?,?,?,?)`,
				dsID, strings.Join(r.AppliesTo, ","), r.Role, r.Value, r.MPN, r.Manufacturer, r.Conditions, r.Notes, r.Page, source); err != nil {
				return err
			}
		}
		return reindexDatasheet(ctx, tx, dsID)
	})
}

// DeleteRecommendation removes one recommendation.
func (s *Store) DeleteRecommendation(ctx context.Context, id int64) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM recommendations WHERE id = ?`, id)
	return err
}
