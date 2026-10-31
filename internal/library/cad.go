package library

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

// CAD library items: a part's schematic symbol and its footprint (with its
// 3D body), per package, as kept for the user's own CAD library. Each
// holds what it's drawn from (spec: how the agent placed the pins, or the
// package's dimensions and land pattern), its state (made by SheetDiver,
// drawn by the agent, or edited by the user in a CAD tool), what the user
// left in each tool's file when they edited it (raw, by format), and the
// fingerprint of what SheetDiver last wrote there (written, by format),
// which is how an edit is noticed.

// CadItem is a symbol or footprint of a part.
type CadItem struct {
	ID      int64  `json:"id"`
	PartID  int64  `json:"partId"`
	MPN     string `json:"mpn,omitempty"`
	Kind    string `json:"kind"`
	Package string `json:"package"`
	// Spec is what it's drawn from, as JSON (the cad package's types).
	Spec json.RawMessage `json:"spec,omitempty"`
	// State is auto, drawn or edited.
	State string `json:"state"`
	// Raw is what the user left in each format's file, by format.
	Raw map[string]string `json:"raw,omitempty"`
	// Written is the fingerprint of what was last written, by file.
	Written map[string]string `json:"written,omitempty"`
	Notes   string            `json:"notes,omitempty"`
	Source  string            `json:"source"`
	Updated string            `json:"updated"`
}

// CAD item kinds.
const (
	CadSymbol    = "symbol"
	CadFootprint = "footprint"
)

// cadMigration adds the table.
const cadMigration = `
CREATE TABLE cad_items (
	id INTEGER PRIMARY KEY,
	part_id INTEGER NOT NULL REFERENCES parts(id) ON DELETE CASCADE,
	kind TEXT NOT NULL,
	package TEXT NOT NULL DEFAULT '' COLLATE NOCASE,
	spec TEXT NOT NULL DEFAULT '',
	state TEXT NOT NULL,
	raw TEXT NOT NULL DEFAULT '{}',
	written TEXT NOT NULL DEFAULT '{}',
	notes TEXT NOT NULL DEFAULT '',
	source TEXT NOT NULL DEFAULT 'auto',
	updated_at TEXT NOT NULL,
	UNIQUE(part_id, kind, package)
);
CREATE INDEX cad_items_part ON cad_items(part_id);
`

// cadCols are an item's columns, with its part's number.
const cadCols = `c.id, c.part_id, p.mpn, c.kind, c.package, c.spec, c.state, c.raw, c.written, c.notes, c.source, c.updated_at`

// scanCad reads an item row.
func scanCad(sc interface{ Scan(...any) error }) (CadItem, error) {
	var it CadItem
	var spec, raw, written string
	if err := sc.Scan(&it.ID, &it.PartID, &it.MPN, &it.Kind, &it.Package, &spec, &it.State, &raw, &written, &it.Notes, &it.Source, &it.Updated); err != nil {
		return it, err
	}
	if spec != "" {
		it.Spec = json.RawMessage(spec)
	}
	_ = json.Unmarshal([]byte(raw), &it.Raw)
	_ = json.Unmarshal([]byte(written), &it.Written)
	return it, nil
}

// CadItems are the items of a part, or of every part when partID is 0,
// by part number, kind and package.
func (s *Store) CadItems(ctx context.Context, partID int64) ([]CadItem, error) {
	q := `SELECT ` + cadCols + ` FROM cad_items c JOIN parts p ON p.id = c.part_id`
	var args []any
	if partID != 0 {
		q += ` WHERE c.part_id = ?`
		args = append(args, partID)
	}
	rows, err := s.db.QueryContext(ctx, q+` ORDER BY p.mpn, c.kind DESC, c.package`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []CadItem
	for rows.Next() {
		it, err := scanCad(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, it)
	}
	return out, rows.Err()
}

// CadItem is a part's item of a kind for a package.
func (s *Store) CadItem(ctx context.Context, partID int64, kind, pkg string) (CadItem, error) {
	it, err := scanCad(s.db.QueryRowContext(ctx, `SELECT `+cadCols+` FROM cad_items c JOIN parts p ON p.id = c.part_id WHERE c.part_id = ? AND c.kind = ? AND c.package = ?`,
		partID, kind, strings.TrimSpace(pkg)))
	if errors.Is(err, sql.ErrNoRows) {
		return it, fmt.Errorf("%s %s: %w", kind, pkg, ErrNotFound)
	}
	return it, err
}

// SaveCadItem adds or replaces a part's item of a kind for a package.
func (s *Store) SaveCadItem(ctx context.Context, it CadItem) (CadItem, error) {
	if it.Kind != CadSymbol && it.Kind != CadFootprint {
		return it, fmt.Errorf("kind %q isn't symbol or footprint", it.Kind)
	}
	raw, _ := json.Marshal(emptyMap(it.Raw))
	written, _ := json.Marshal(emptyMap(it.Written))
	if it.Source == "" {
		it.Source = SourceAuto
	}
	if _, err := s.db.ExecContext(ctx, `INSERT INTO cad_items (part_id, kind, package, spec, state, raw, written, notes, source, updated_at) VALUES (?,?,?,?,?,?,?,?,?,?)
		ON CONFLICT(part_id, kind, package) DO UPDATE SET spec = excluded.spec, state = excluded.state, raw = excluded.raw, written = excluded.written,
		notes = excluded.notes, source = excluded.source, updated_at = excluded.updated_at`,
		it.PartID, it.Kind, strings.TrimSpace(it.Package), string(it.Spec), it.State, string(raw), string(written), it.Notes, it.Source, now()); err != nil {
		return it, err
	}
	return s.CadItem(ctx, it.PartID, it.Kind, it.Package)
}

// DeleteCadItem removes an item.
func (s *Store) DeleteCadItem(ctx context.Context, id int64) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM cad_items WHERE id = ?`, id)
	return err
}

// emptyMap is a map, empty rather than nil.
func emptyMap(m map[string]string) map[string]string {
	if m == nil {
		return map[string]string{}
	}
	return m
}
