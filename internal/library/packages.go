package library

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"strings"

	"github.com/aitakemyjob/sheetdiver/internal/pdf"
)

// Package dimensions: what a datasheet's package drawings and land
// pattern tables say each package measures. The extractor's rows (source
// auto) are replaced on re-extraction; what an agent or the user records
// is kept, one row per package, and wins over the extractor's for each
// dimension it gives.

// PackageDim is one dimension, in mm.
type PackageDim struct {
	Symbol      string   `json:"symbol"`
	Description string   `json:"description,omitempty"`
	Key         string   `json:"key,omitempty"`
	Min         *float64 `json:"min,omitempty"`
	Nom         *float64 `json:"nom,omitempty"`
	Max         *float64 `json:"max,omitempty"`
	// Unit is the unit the datasheet printed; values are in mm.
	Unit string `json:"unit,omitempty"`
	Note string `json:"note,omitempty"`
}

// PackageDims are a package's dimensions and land pattern from one
// datasheet.
type PackageDims struct {
	ID          int64        `json:"id"`
	DatasheetID int64        `json:"datasheetId"`
	Name        string       `json:"name"`
	Code        string       `json:"code,omitempty"`
	Reference   string       `json:"reference,omitempty"`
	Page        int          `json:"page"`
	Box         pdf.Rect     `json:"box"`
	Dims        []PackageDim `json:"dims"`
	Land        []PackageDim `json:"land"`
	Candidates  []string     `json:"candidates,omitempty"`
	Checks      []string     `json:"checks,omitempty"`
	// Parts are the datasheet's parts that come in the package (its
	// ordering scheme says); none means every part.
	Parts   []string `json:"parts,omitempty"`
	Notes   string   `json:"notes,omitempty"`
	Source  string   `json:"source"`
	Updated string   `json:"updated"`
}

// packageMigration adds the table.
const packageMigration = `
CREATE TABLE IF NOT EXISTS packages (
	id INTEGER PRIMARY KEY,
	datasheet_id INTEGER NOT NULL REFERENCES datasheets(id) ON DELETE CASCADE,
	name TEXT NOT NULL COLLATE NOCASE,
	code TEXT NOT NULL DEFAULT '',
	reference TEXT NOT NULL DEFAULT '',
	page INTEGER NOT NULL DEFAULT 0,
	box TEXT NOT NULL DEFAULT '{}',
	dims TEXT NOT NULL DEFAULT '[]',
	land TEXT NOT NULL DEFAULT '[]',
	candidates TEXT NOT NULL DEFAULT '[]',
	checks TEXT NOT NULL DEFAULT '[]',
	notes TEXT NOT NULL DEFAULT '',
	source TEXT NOT NULL DEFAULT 'auto',
	updated_at TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS packages_datasheet ON packages(datasheet_id);
`

// execer runs a statement, in a transaction or not.
type execer interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
}

// insertPackage adds a package's row.
func insertPackage(ctx context.Context, db execer, p PackageDims) error {
	box, _ := json.Marshal(p.Box)
	dims, _ := json.Marshal(nonNilDims(p.Dims))
	land, _ := json.Marshal(nonNilDims(p.Land))
	cands, _ := json.Marshal(nonNil(p.Candidates))
	checks, _ := json.Marshal(nonNil(p.Checks))
	_, err := db.ExecContext(ctx, `INSERT INTO packages (datasheet_id, name, code, reference, page, box, dims, land, candidates, checks, parts, notes, source, updated_at) VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
		p.DatasheetID, strings.TrimSpace(p.Name), p.Code, p.Reference, p.Page, string(box), string(dims), string(land), string(cands), string(checks), strings.Join(p.Parts, ","), p.Notes, p.Source, now())
	return err
}

// nonNilDims is a list, empty rather than nil.
func nonNilDims(d []PackageDim) []PackageDim {
	if d == nil {
		return []PackageDim{}
	}
	return d
}

// savePackages replaces a datasheet's extracted packages.
func savePackages(ctx context.Context, tx *sql.Tx, dsID int64, ps []PackageDims) error {
	if _, err := tx.ExecContext(ctx, `DELETE FROM packages WHERE datasheet_id = ? AND source = ?`, dsID, SourceAuto); err != nil {
		return err
	}
	for _, p := range ps {
		p.DatasheetID, p.Source = dsID, SourceAuto
		if err := insertPackage(ctx, tx, p); err != nil {
			return err
		}
	}
	return nil
}

// Packages are a datasheet's packages: the extractor's and recorded ones.
func (s *Store) Packages(ctx context.Context, dsID int64) ([]PackageDims, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, datasheet_id, name, code, reference, page, box, dims, land, candidates, checks, parts, notes, source, updated_at
		FROM packages WHERE datasheet_id = ? ORDER BY name, source = 'auto' DESC, id`, dsID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []PackageDims{}
	for rows.Next() {
		var p PackageDims
		var box, dims, land, cands, checks, parts string
		if err := rows.Scan(&p.ID, &p.DatasheetID, &p.Name, &p.Code, &p.Reference, &p.Page, &box, &dims, &land, &cands, &checks, &parts, &p.Notes, &p.Source, &p.Updated); err != nil {
			return nil, err
		}
		_ = json.Unmarshal([]byte(box), &p.Box)
		_ = json.Unmarshal([]byte(dims), &p.Dims)
		_ = json.Unmarshal([]byte(land), &p.Land)
		_ = json.Unmarshal([]byte(cands), &p.Candidates)
		_ = json.Unmarshal([]byte(checks), &p.Checks)
		p.Parts = splitList(parts)
		out = append(out, p)
	}
	return out, rows.Err()
}

// RecordPackage stores a package's dimensions as an agent or the user
// read them, replacing what that source recorded for the package before.
func (s *Store) RecordPackage(ctx context.Context, p PackageDims, source string) error {
	if strings.TrimSpace(p.Name) == "" {
		return errors.New("a package needs a name (SOT-23-6, its drawing code, …)")
	}
	if len(p.Dims) == 0 && len(p.Land) == 0 {
		return errors.New("give the package's dimensions, its land pattern, or both")
	}
	if source == "" || source == SourceAuto {
		source = SourceAgent
	}
	p.Source = source
	return s.tx(ctx, func(tx *sql.Tx) error {
		if _, err := tx.ExecContext(ctx, `DELETE FROM packages WHERE datasheet_id = ? AND name = ? AND source = ?`, p.DatasheetID, strings.TrimSpace(p.Name), source); err != nil {
			return err
		}
		return insertPackage(ctx, tx, p)
	})
}

// DeletePackage removes a package's row.
func (s *Store) DeletePackage(ctx context.Context, id int64) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM packages WHERE id = ?`, id)
	return err
}
