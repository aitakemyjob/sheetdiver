package library

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/aitakemyjob/sheetdiver/internal/pdf"
)

// ImportVariant links an orderable code to its base part by MPN.
type ImportVariant struct {
	MPN  string
	Base string
}

// Import is everything extracted from one datasheet, ready to store.
type Import struct {
	Datasheet Datasheet
	Parts     []Part
	Variants  []ImportVariant
	Pages     []Page
	Sections  []Section
	Figures   []Figure
	Tables    []Table
	Params    []Parameter
	Pins      []Pin
	// Recommendations read from regular recommended-component tables.
	Recommendations []Recommendation
}

// SaveImport stores an import. If the datasheet (by SHA-256) is already in
// the library its mechanical ("auto") data is replaced and everything the
// agent or user recorded is kept. Parts are matched by MPN; an existing
// part keeps its edited fields and is linked to this datasheet if it had
// none. It returns the datasheet ID.
func (s *Store) SaveImport(ctx context.Context, in *Import) (int64, error) {
	var dsID int64
	err := s.tx(ctx, func(tx *sql.Tx) error {
		d := in.Datasheet
		fam, _ := json.Marshal(nonNil(d.Families))
		meta, _ := json.Marshal(d.Meta)
		err := tx.QueryRowContext(ctx, `SELECT id FROM datasheets WHERE sha256 = ?`, d.SHA256).Scan(&dsID)
		switch {
		case errors.Is(err, sql.ErrNoRows):
			res, err := tx.ExecContext(ctx, `INSERT INTO datasheets (sha256, file, filename, title, description, manufacturer, pages, families, source_url, meta, extractor, imported_at)
				VALUES (?,?,?,?,?,?,?,?,?,?,?,?)`, d.SHA256, d.File, d.Filename, d.Title, d.Description, d.Manufacturer, d.Pages, string(fam), d.SourceURL, string(meta), d.Extractor, now())
			if err != nil {
				return err
			}
			dsID, _ = res.LastInsertId()
		case err != nil:
			return err
		default:
			if _, err := tx.ExecContext(ctx, `UPDATE datasheets SET pages = ?, meta = ?, extractor = ?,
				title = CASE WHEN title = '' THEN ? ELSE title END,
				description = CASE WHEN description = '' THEN ? ELSE description END,
				manufacturer = CASE WHEN manufacturer = '' THEN ? ELSE manufacturer END,
				families = CASE WHEN families = '[]' THEN ? ELSE families END
				WHERE id = ?`, d.Pages, string(meta), d.Extractor, d.Title, d.Description, d.Manufacturer, string(fam), dsID); err != nil {
				return err
			}
			for _, q := range []string{
				`DELETE FROM pages WHERE datasheet_id = ?`,
				`DELETE FROM sections WHERE datasheet_id = ?`,
				`DELETE FROM parameters WHERE datasheet_id = ? AND source = 'auto'`,
				`DELETE FROM pins WHERE datasheet_id = ? AND source = 'auto'`,
				`DELETE FROM variants WHERE source = 'auto' AND part_id IN (SELECT id FROM parts WHERE datasheet_id = ?)`,
			} {
				if _, err := tx.ExecContext(ctx, q, dsID); err != nil {
					return err
				}
			}
		}
		partIDs := map[string]int64{}
		for _, p := range in.Parts {
			id, err := upsertPart(ctx, tx, p, dsID)
			if err != nil {
				return err
			}
			partIDs[strings.ToUpper(p.MPN)] = id
		}
		for _, v := range in.Variants {
			pid, ok := partIDs[strings.ToUpper(v.Base)]
			if !ok {
				continue
			}
			if _, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO variants (mpn, part_id, source) VALUES (?,?,'auto')`, v.MPN, pid); err != nil {
				return err
			}
		}
		for _, p := range in.Pages {
			lines, _ := json.Marshal(nonNil(p.Lines))
			if _, err := tx.ExecContext(ctx, `INSERT INTO pages (datasheet_id, page, width, height, text, lines) VALUES (?,?,?,?,?,?)`,
				dsID, p.Page, p.Width, p.Height, p.Text, string(lines)); err != nil {
				return err
			}
		}
		for i, sec := range in.Sections {
			if _, err := tx.ExecContext(ctx, `INSERT INTO sections (datasheet_id, ord, num, title, level, kind, page, y, end_page, end_y, text) VALUES (?,?,?,?,?,?,?,?,?,?,?)`,
				dsID, i, sec.Num, sec.Title, sec.Level, sec.Kind, sec.Page, sec.Y, sec.EndPage, sec.EndY, sec.Text); err != nil {
				return err
			}
		}
		// Figures keep their IDs across re-extraction (notes and open views
		// refer to them): auto figures are updated in place by number, ones
		// the agent or user described are left alone, and auto figures that
		// no longer exist are removed.
		existing := map[string]struct {
			id     int64
			source string
		}{}
		rows, err := tx.QueryContext(ctx, `SELECT id, number, source FROM figures WHERE datasheet_id = ?`, dsID)
		if err != nil {
			return err
		}
		for rows.Next() {
			var id int64
			var n, src string
			if err := rows.Scan(&id, &n, &src); err != nil {
				rows.Close()
				return err
			}
			existing[n] = struct {
				id     int64
				source string
			}{id, src}
		}
		rows.Close()
		seen := map[int64]bool{}
		for _, f := range in.Figures {
			f.DatasheetID, f.Source = dsID, SourceAuto
			if e, ok := existing[f.Number]; ok && f.Number != "" {
				seen[e.id] = true
				if e.source != SourceAuto {
					continue // the agent or user already described this figure
				}
				if _, err := tx.ExecContext(ctx, `UPDATE figures SET page = ?, caption = ?, kind = ?, x0 = ?, y0 = ?, x1 = ?, y1 = ?, section = ?, x_axis = ?, y_axis = ?, applies_to = ?, updated_at = ? WHERE id = ?`,
					f.Page, f.Caption, f.Kind, f.Box.X0, f.Box.Y0, f.Box.X1, f.Box.Y1, f.Section, f.XAxis, f.YAxis, strings.Join(f.AppliesTo, ","), now(), e.id); err != nil {
					return err
				}
				continue
			}
			id, err := insertFigure(ctx, tx, f)
			if err != nil {
				return err
			}
			seen[id] = true
		}
		for _, e := range existing {
			if !seen[e.id] && e.source == SourceAuto {
				if _, err := tx.ExecContext(ctx, `DELETE FROM figures WHERE id = ?`, e.id); err != nil {
					return err
				}
			}
		}
		if err := saveTables(ctx, tx, dsID, in.Tables); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM recommendations WHERE datasheet_id = ? AND source = 'auto'`, dsID); err != nil {
			return err
		}
		for _, r := range in.Recommendations {
			if _, err := tx.ExecContext(ctx, `INSERT INTO recommendations (datasheet_id, applies_to, role, value, mpn, manufacturer, conditions, notes, page, source) VALUES (?,?,?,?,?,?,?,?,?,'auto')`,
				dsID, strings.Join(r.AppliesTo, ","), r.Role, r.Value, r.MPN, r.Manufacturer, r.Conditions, r.Notes, r.Page); err != nil {
				return err
			}
		}
		// Keys the agent or user recorded replace the extractor's rows.
		recorded := map[string]bool{}
		krows, err := tx.QueryContext(ctx, `SELECT key, section FROM parameters WHERE datasheet_id = ? AND source != 'auto' AND key != ''`, dsID)
		if err != nil {
			return err
		}
		for krows.Next() {
			var k, sec string
			if err := krows.Scan(&k, &sec); err != nil {
				krows.Close()
				return err
			}
			recorded[k+"|"+sec] = true
		}
		if err := krows.Close(); err != nil {
			return err
		}
		for _, p := range in.Params {
			if p.Key != "" && recorded[p.Key+"|"+p.Section] {
				continue
			}
			p.DatasheetID, p.Source = dsID, SourceAuto
			if _, err := insertParam(ctx, tx, p); err != nil {
				return err
			}
		}
		for _, p := range in.Pins {
			p.DatasheetID, p.Source = dsID, SourceAuto
			if _, err := insertPin(ctx, tx, p); err != nil {
				return err
			}
		}
		return reindexDatasheet(ctx, tx, dsID)
	})
	return dsID, err
}

// tableKey identifies a table across re-extractions: its number and page
// (a continued table has the same number on the next page), or its page
// and height when it has no caption.
func tableKey(number string, page int, y0 float64) string {
	if number != "" {
		return fmt.Sprintf("%s@%d", number, page)
	}
	return fmt.Sprintf("@%d:%.0f", page, y0/10)
}

// saveTables stores extracted tables, keeping IDs stable across
// re-extraction: automatic tables are updated in place by key, tables the
// agent or user transcribed are left alone, and automatic tables that are
// no longer found are removed.
func saveTables(ctx context.Context, tx *sql.Tx, dsID int64, tables []Table) error {
	type row struct {
		id     int64
		source string
	}
	existing := map[string]row{}
	rows, err := tx.QueryContext(ctx, `SELECT id, number, page, y0, source FROM doc_tables WHERE datasheet_id = ?`, dsID)
	if err != nil {
		return err
	}
	for rows.Next() {
		var r row
		var num string
		var page int
		var y0 float64
		if err := rows.Scan(&r.id, &num, &page, &y0, &r.source); err != nil {
			rows.Close()
			return err
		}
		existing[tableKey(num, page, y0)] = r
	}
	rows.Close()
	seen := map[int64]bool{}
	for _, t := range tables {
		h, _ := json.Marshal(nonNil(t.Header))
		r, _ := json.Marshal(nonNilRows(t.Rows))
		if e, ok := existing[tableKey(t.Number, t.Page, t.Box.Y0)]; ok && !seen[e.id] {
			seen[e.id] = true
			if e.source != SourceAuto {
				continue
			}
			if _, err := tx.ExecContext(ctx, `UPDATE doc_tables SET caption = ?, kind = ?, section = ?, x0 = ?, y0 = ?, x1 = ?, y1 = ?, header = ?, rows = ?, irregular = ?, updated_at = ? WHERE id = ?`,
				t.Caption, t.Kind, t.Section, t.Box.X0, t.Box.Y0, t.Box.X1, t.Box.Y1, string(h), string(r), t.Irregular, now(), e.id); err != nil {
				return err
			}
			continue
		}
		res, err := tx.ExecContext(ctx, `INSERT INTO doc_tables (datasheet_id, page, number, caption, kind, section, x0, y0, x1, y1, header, rows, irregular, source, updated_at) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,'auto',?)`,
			dsID, t.Page, t.Number, t.Caption, t.Kind, t.Section, t.Box.X0, t.Box.Y0, t.Box.X1, t.Box.Y1, string(h), string(r), t.Irregular, now())
		if err != nil {
			return err
		}
		id, _ := res.LastInsertId()
		seen[id] = true
	}
	for _, e := range existing {
		if !seen[e.id] && e.source == SourceAuto {
			if _, err := tx.ExecContext(ctx, `DELETE FROM doc_tables WHERE id = ?`, e.id); err != nil {
				return err
			}
		}
	}
	return nil
}

// upsertPart inserts a part, or links and fills in an existing one.
func upsertPart(ctx context.Context, tx *sql.Tx, p Part, dsID int64) (int64, error) {
	var id int64
	var cur sql.NullInt64
	err := tx.QueryRowContext(ctx, `SELECT id, datasheet_id FROM parts WHERE mpn = ?`, p.MPN).Scan(&id, &cur)
	tags, _ := json.Marshal(nonNil(p.Tags))
	switch {
	case errors.Is(err, sql.ErrNoRows):
		res, err := tx.ExecContext(ctx, `INSERT INTO parts (mpn, manufacturer, description, category, package, datasheet_id, tags, created_at, updated_at) VALUES (?,?,?,?,?,?,?,?,?)`,
			p.MPN, p.Manufacturer, p.Description, p.Category, p.Package, dsID, string(tags), now(), now())
		if err != nil {
			return 0, err
		}
		return res.LastInsertId()
	case err != nil:
		return 0, err
	}
	_, err = tx.ExecContext(ctx, `UPDATE parts SET
		datasheet_id = CASE WHEN datasheet_id IS NULL THEN ? ELSE datasheet_id END,
		manufacturer = CASE WHEN manufacturer = '' THEN ? ELSE manufacturer END,
		description = CASE WHEN description = '' THEN ? ELSE description END,
		category = CASE WHEN category = '' THEN ? ELSE category END,
		updated_at = ? WHERE id = ?`, dsID, p.Manufacturer, p.Description, p.Category, now(), id)
	return id, err
}

// nonNil turns a nil slice into an empty one for JSON.
func nonNil[T any](s []T) []T {
	if s == nil {
		return []T{}
	}
	return s
}

// nonNilRows is nonNil for table rows.
func nonNilRows(r [][]string) [][]string {
	if r == nil {
		return [][]string{}
	}
	return r
}

// insertFigure stores a figure.
func insertFigure(ctx context.Context, tx *sql.Tx, f Figure) (int64, error) {
	curves, _ := json.Marshal(nonNil(f.Curves))
	res, err := tx.ExecContext(ctx, `INSERT INTO figures (datasheet_id, page, number, caption, kind, x0, y0, x1, y1, section, title, x_axis, y_axis, conditions, description, curves, applies_to, source, updated_at)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		f.DatasheetID, f.Page, f.Number, f.Caption, f.Kind, f.Box.X0, f.Box.Y0, f.Box.X1, f.Box.Y1, f.Section, f.Title, f.XAxis, f.YAxis, f.Conditions, f.Description, string(curves), strings.Join(f.AppliesTo, ","), f.Source, now())
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

// insertParam stores a parameter.
func insertParam(ctx context.Context, tx *sql.Tx, p Parameter) (int64, error) {
	var x0, y0, x1, y1 any
	if p.Box != nil {
		x0, y0, x1, y1 = p.Box.X0, p.Box.Y0, p.Box.X1, p.Box.Y1
	}
	res, err := tx.ExecContext(ctx, `INSERT INTO parameters (datasheet_id, applies_to, key, symbol, name, conditions, min, typ, max, unit, section, page, x0, y0, x1, y1, raw, note, source, updated_at)
		VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		p.DatasheetID, strings.Join(p.AppliesTo, ","), p.Key, p.Symbol, p.Name, p.Conditions, nullFloat(p.Min), nullFloat(p.Typ), nullFloat(p.Max), p.Unit, p.Section, p.Page, x0, y0, x1, y1, p.Raw, p.Note, p.Source, now())
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

// insertPin stores a pin.
func insertPin(ctx context.Context, tx *sql.Tx, p Pin) (int64, error) {
	res, err := tx.ExecContext(ctx, `INSERT INTO pins (datasheet_id, package, number, name, type, description, page, source) VALUES (?,?,?,?,?,?,?,?)`,
		p.DatasheetID, p.Package, p.Number, p.Name, p.Type, p.Description, p.Page, p.Source)
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

// ---- datasheets -------------------------------------------------------------

// datasheetCols is the column list scanDatasheet reads.
const datasheetCols = `id, sha256, file, filename, title, description, manufacturer, pages, families, source_url, meta, extractor, imported_at`

// scanDatasheet reads a datasheet row.
func scanDatasheet(sc interface{ Scan(...any) error }) (Datasheet, error) {
	var d Datasheet
	var fam, meta, at string
	err := sc.Scan(&d.ID, &d.SHA256, &d.File, &d.Filename, &d.Title, &d.Description, &d.Manufacturer, &d.Pages, &fam, &d.SourceURL, &meta, &d.Extractor, &at)
	if err != nil {
		return d, err
	}
	_ = json.Unmarshal([]byte(fam), &d.Families)
	_ = json.Unmarshal([]byte(meta), &d.Meta)
	d.ImportedAt = parseTime(at)
	return d, nil
}

// Datasheet returns a datasheet by ID.
func (s *Store) Datasheet(ctx context.Context, id int64) (Datasheet, error) {
	d, err := scanDatasheet(s.db.QueryRowContext(ctx, `SELECT `+datasheetCols+` FROM datasheets WHERE id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return d, fmt.Errorf("datasheet %d: %w", id, ErrNotFound)
	}
	return d, err
}

// DatasheetBySHA returns a datasheet by content hash.
func (s *Store) DatasheetBySHA(ctx context.Context, sha string) (Datasheet, bool, error) {
	d, err := scanDatasheet(s.db.QueryRowContext(ctx, `SELECT `+datasheetCols+` FROM datasheets WHERE sha256 = ?`, sha))
	if errors.Is(err, sql.ErrNoRows) {
		return d, false, nil
	}
	return d, err == nil, err
}

// Datasheets lists every datasheet, newest first.
func (s *Store) Datasheets(ctx context.Context) ([]Datasheet, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+datasheetCols+` FROM datasheets ORDER BY imported_at DESC, id DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Datasheet{}
	for rows.Next() {
		d, err := scanDatasheet(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

// UpdateDatasheet saves the editable fields of a datasheet.
func (s *Store) UpdateDatasheet(ctx context.Context, d Datasheet) error {
	fam, _ := json.Marshal(nonNil(d.Families))
	res, err := s.db.ExecContext(ctx, `UPDATE datasheets SET title = ?, description = ?, manufacturer = ?, families = ?, source_url = ? WHERE id = ?`,
		d.Title, d.Description, d.Manufacturer, string(fam), d.SourceURL, d.ID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("datasheet %d: %w", d.ID, ErrNotFound)
	}
	return s.Reindex(ctx, d.ID)
}

// DeleteDatasheet removes a datasheet with everything extracted from it.
// Its parts stay in the library (and in projects) without a datasheet.
func (s *Store) DeleteDatasheet(ctx context.Context, id int64) (Datasheet, error) {
	d, err := s.Datasheet(ctx, id)
	if err != nil {
		return d, err
	}
	err = s.tx(ctx, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, `DELETE FROM fts WHERE datasheet = ?`, strconv.FormatInt(id, 10)); err != nil {
			return err
		}
		_, err := tx.ExecContext(ctx, `DELETE FROM datasheets WHERE id = ?`, id)
		return err
	})
	return d, err
}

// ---- pages -----------------------------------------------------------------

// Page returns one page with its lines.
func (s *Store) Page(ctx context.Context, dsID int64, page int) (Page, error) {
	p := Page{DatasheetID: dsID, Page: page}
	var lines string
	err := s.db.QueryRowContext(ctx, `SELECT width, height, text, lines FROM pages WHERE datasheet_id = ? AND page = ?`, dsID, page).
		Scan(&p.Width, &p.Height, &p.Text, &lines)
	if errors.Is(err, sql.ErrNoRows) {
		return p, fmt.Errorf("page %d of datasheet %d: %w", page+1, dsID, ErrNotFound)
	}
	_ = json.Unmarshal([]byte(lines), &p.Lines)
	return p, err
}

// PageSizes returns every page's size, in order.
func (s *Store) PageSizes(ctx context.Context, dsID int64) ([]Page, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT page, width, height FROM pages WHERE datasheet_id = ? ORDER BY page`, dsID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Page{}
	for rows.Next() {
		p := Page{DatasheetID: dsID}
		if err := rows.Scan(&p.Page, &p.Width, &p.Height); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// RegionText returns the text of the lines inside a box on a page.
func (s *Store) RegionText(ctx context.Context, dsID int64, page int, box pdf.Rect) (string, error) {
	p, err := s.Page(ctx, dsID, page)
	if err != nil {
		return "", err
	}
	var parts []string
	for _, l := range p.Lines {
		if box.Intersects(l.Box) {
			parts = append(parts, l.Text)
		}
	}
	return strings.Join(parts, "\n"), nil
}
