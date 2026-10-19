package library

import (
	"context"
	"database/sql"
	"fmt"
	"strconv"
	"strings"
	"unicode"
)

// Hit is one search result.
type Hit struct {
	// Kind is part, page, section, figure, parameter, pin, register,
	// recommendation or note.
	Kind        string  `json:"kind"`
	Ref         string  `json:"ref"`
	DatasheetID int64   `json:"datasheetId,omitempty"`
	Page        int     `json:"page"`
	Title       string  `json:"title"`
	Snippet     string  `json:"snippet"`
	Score       float64 `json:"score"`
	// Parts are the part numbers on the hit's datasheet.
	Parts []string `json:"parts,omitempty"`
}

// Reindex rebuilds a datasheet's search entries.
func (s *Store) Reindex(ctx context.Context, dsID int64) error {
	return s.tx(ctx, func(tx *sql.Tx) error { return reindexDatasheet(ctx, tx, dsID) })
}

// ftsEntry is one row of the search index.
type ftsEntry struct {
	kind, ref   string
	page        int
	title, body string
}

// scanner is the Scan method shared by *sql.Row and *sql.Rows.
type scanner interface{ Scan(...any) error }

// indexQueries produce a datasheet's search entries, one query per kind.
var indexQueries = []struct {
	q     string
	entry func(sc scanner, ctx string) (ftsEntry, error)
}{
	{`SELECT id, mpn, description, category, manufacturer, COALESCE((SELECT group_concat(mpn, ' ') FROM variants v WHERE v.part_id = parts.id), '') FROM parts WHERE datasheet_id = ?`,
		func(sc scanner, dsText string) (ftsEntry, error) {
			var id int64
			var mpn, d, c, m, vars string
			err := sc.Scan(&id, &mpn, &d, &c, &m, &vars)
			return ftsEntry{"part", fmt.Sprint(id), 0, mpn, strings.Join([]string{d, c, m, dsText, vars}, " ")}, err
		}},
	{`SELECT page, text FROM pages WHERE datasheet_id = ?`,
		func(sc scanner, _ string) (ftsEntry, error) {
			var p int
			var text string
			err := sc.Scan(&p, &text)
			return ftsEntry{"page", fmt.Sprint(p), p, fmt.Sprintf("Page %d", p+1), text}, err
		}},
	{`SELECT id, num, title, kind, page FROM sections WHERE datasheet_id = ?`,
		func(sc scanner, _ string) (ftsEntry, error) {
			var id int64
			var num, t, kind string
			var p int
			err := sc.Scan(&id, &num, &t, &kind, &p)
			return ftsEntry{"section", fmt.Sprint(id), p, strings.TrimSpace(num + " " + t), strings.ReplaceAll(kind, "_", " ")}, err
		}},
	{`SELECT id, number, caption, kind, title, x_axis, y_axis, conditions, description, curves, page FROM figures WHERE datasheet_id = ?`,
		func(sc scanner, _ string) (ftsEntry, error) {
			var id int64
			var num, cpt, kind, t, x, y, cond, d, curves string
			var p int
			err := sc.Scan(&id, &num, &cpt, &kind, &t, &x, &y, &cond, &d, &curves, &p)
			return ftsEntry{"figure", fmt.Sprint(id), p, strings.TrimSpace("Figure " + num + " " + cpt), strings.Join([]string{strings.ReplaceAll(kind, "_", " "), t, y, x, cond, d, curves}, " ")}, err
		}},
	{`SELECT id, number, caption, title, description, kind, header, rows, page FROM doc_tables WHERE datasheet_id = ? AND (number != '' OR source != 'auto')`,
		func(sc scanner, _ string) (ftsEntry, error) {
			var id int64
			var num, cpt, title, desc, kind, header, rows string
			var p int
			err := sc.Scan(&id, &num, &cpt, &title, &desc, &kind, &header, &rows, &p)
			t := strings.TrimSpace("Table " + num + " " + firstNonEmptyStr(title, cpt))
			// Cells make tables searchable by their contents ("3.3 V",
			// a resistor value); JSON punctuation is harmless to FTS.
			if len(rows) > 20000 {
				rows = rows[:20000]
			}
			return ftsEntry{"table", fmt.Sprint(id), p, t, strings.Join([]string{cpt, desc, kind, header, rows}, " ")}, err
		}},
	{`SELECT id, key, symbol, name, conditions, unit, section, page FROM parameters WHERE datasheet_id = ?`,
		func(sc scanner, _ string) (ftsEntry, error) {
			var id int64
			var key, sym, name, cond, unit, sec string
			var p int
			err := sc.Scan(&id, &key, &sym, &name, &cond, &unit, &sec, &p)
			return ftsEntry{"parameter", fmt.Sprint(id), p, strings.TrimSpace(sym + " " + name), strings.Join([]string{key, cond, unit, strings.ReplaceAll(sec, "_", " ")}, " ")}, err
		}},
	{`SELECT id, number, name, type, description, page FROM pins WHERE datasheet_id = ?`,
		func(sc scanner, _ string) (ftsEntry, error) {
			var id int64
			var num, name, typ, d string
			var p int
			err := sc.Scan(&id, &num, &name, &typ, &d, &p)
			return ftsEntry{"pin", fmt.Sprint(id), p, "Pin " + num + " " + name, typ + " " + d}, err
		}},
	{`SELECT id, name, address, description, fields, page FROM registers WHERE datasheet_id = ?`,
		func(sc scanner, _ string) (ftsEntry, error) {
			var id int64
			var name, addr, d, fields string
			var p int
			err := sc.Scan(&id, &name, &addr, &d, &fields, &p)
			return ftsEntry{"register", fmt.Sprint(id), p, strings.TrimSpace(name + " " + addr), d + " " + fields}, err
		}},
	{`SELECT id, role, value, mpn, manufacturer, conditions, notes, page FROM recommendations WHERE datasheet_id = ?`,
		func(sc scanner, _ string) (ftsEntry, error) {
			var id int64
			var role, val, mpn, m, cond, notes string
			var p int
			err := sc.Scan(&id, &role, &val, &mpn, &m, &cond, &notes, &p)
			return ftsEntry{"recommendation", fmt.Sprint(id), p, strings.TrimSpace(role + " " + val), strings.Join([]string{mpn, m, cond, notes}, " ")}, err
		}},
}

// reindexDatasheet replaces every search entry of a datasheet (notes are
// indexed separately and kept).
func reindexDatasheet(ctx context.Context, tx *sql.Tx, dsID int64) error {
	ds := strconv.FormatInt(dsID, 10)
	if _, err := tx.ExecContext(ctx, `DELETE FROM fts WHERE datasheet = ? AND kind != 'note'`, ds); err != nil {
		return err
	}
	var title, desc, mfr, fam string
	if err := tx.QueryRowContext(ctx, `SELECT title, description, manufacturer, families FROM datasheets WHERE id = ?`, dsID).Scan(&title, &desc, &mfr, &fam); err != nil {
		return err
	}
	dsText := strings.Join([]string{title, desc, mfr, strings.Trim(fam, "[]")}, " ")
	var entries []ftsEntry
	for _, iq := range indexQueries {
		rows, err := tx.QueryContext(ctx, iq.q, dsID)
		if err != nil {
			return err
		}
		for rows.Next() {
			e, err := iq.entry(rows, dsText)
			if err != nil {
				rows.Close()
				return err
			}
			entries = append(entries, e)
		}
		if err := rows.Close(); err != nil {
			return err
		}
	}
	stmt, err := tx.PrepareContext(ctx, `INSERT INTO fts (kind, ref, datasheet, page, title, body) VALUES (?,?,?,?,?,?)`)
	if err != nil {
		return err
	}
	defer stmt.Close()
	for _, e := range entries {
		if _, err := stmt.ExecContext(ctx, e.kind, e.ref, ds, e.page, e.title, e.body); err != nil {
			return err
		}
	}
	return nil
}

// ftsQuery turns free text into an FTS5 query: each word is quoted (so
// part numbers with dashes work) and the last one is a prefix match.
func ftsQuery(q string) string {
	words := strings.FieldsFunc(q, func(r rune) bool {
		return unicode.IsSpace(r) || r == '"' || r == '(' || r == ')' || r == ',' || r == ';'
	})
	if len(words) == 0 {
		return ""
	}
	for i, w := range words {
		words[i] = `"` + strings.ReplaceAll(w, `"`, ``) + `"`
		if i == len(words)-1 {
			words[i] += "*"
		}
	}
	return strings.Join(words, " ")
}

// SearchOptions narrows Search.
type SearchOptions struct {
	Kinds       []string `json:"kinds,omitempty"`
	DatasheetID int64    `json:"datasheetId,omitempty"`
	Limit       int      `json:"limit,omitempty"`
}

// Search runs a full-text query over the whole library (or one
// datasheet). Part numbers and titles weigh more than body text.
func (s *Store) Search(ctx context.Context, q string, o SearchOptions) ([]Hit, error) {
	fq := ftsQuery(q)
	if fq == "" {
		return []Hit{}, nil
	}
	if o.Limit <= 0 || o.Limit > 200 {
		o.Limit = 30
	}
	sqlq := `SELECT kind, ref, datasheet, page, title, snippet(fts, 5, '«', '»', ' … ', 14), bm25(fts, 0, 0, 0, 0, 4.0, 1.0) AS score FROM fts WHERE fts MATCH ?`
	args := []any{fq}
	if o.DatasheetID != 0 {
		sqlq += ` AND datasheet = ?`
		args = append(args, strconv.FormatInt(o.DatasheetID, 10))
	}
	if len(o.Kinds) > 0 {
		sqlq += ` AND kind IN (` + strings.TrimSuffix(strings.Repeat("?,", len(o.Kinds)), ",") + `)`
		for _, k := range o.Kinds {
			args = append(args, k)
		}
	}
	// Part hits first, then by relevance.
	sqlq += ` ORDER BY CASE kind WHEN 'part' THEN 0 ELSE 1 END, score LIMIT ?`
	args = append(args, o.Limit)
	rows, err := s.db.QueryContext(ctx, sqlq, args...)
	if err != nil {
		return nil, fmt.Errorf("search %q: %w", q, err)
	}
	defer rows.Close()
	out := []Hit{}
	for rows.Next() {
		var h Hit
		var ds string
		if err := rows.Scan(&h.Kind, &h.Ref, &ds, &h.Page, &h.Title, &h.Snippet, &h.Score); err != nil {
			return nil, err
		}
		h.DatasheetID, _ = strconv.ParseInt(ds, 10, 64)
		h.Score = -h.Score
		out = append(out, h)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	rows.Close()
	names := map[int64][]string{}
	for i := range out {
		id := out[i].DatasheetID
		if id == 0 {
			continue
		}
		if _, ok := names[id]; !ok {
			ps, _ := s.PartsOfDatasheet(ctx, id)
			for _, p := range ps {
				names[id] = append(names[id], p.MPN)
			}
		}
		out[i].Parts = names[id]
	}
	return out, nil
}
