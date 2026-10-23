// Package library is SheetDiver's persistent store: datasheets and the
// parts they cover, everything extracted from them (pages, sections,
// figures, tables, parameters, pins, registers, recommended components),
// notes, and projects (board BOMs, rails and saved calculations). It is
// one SQLite database with FTS5 full-text search, next to the PDFs it
// was built from, so a library folder can be copied or synced as a unit.
//
// Every extracted row carries a source: "auto" (the mechanical
// extractor), "agent" (written over MCP) or "user" (edited in the UI).
// Re-extracting a datasheet replaces only "auto" rows.
package library

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	_ "modernc.org/sqlite" // registers the "sqlite" driver
)

// Sources of extracted data.
const (
	SourceAuto  = "auto"
	SourceAgent = "agent"
	SourceUser  = "user"
)

// ErrNotFound is returned when a referenced row doesn't exist.
var ErrNotFound = errors.New("not found")

// Store is an open library.
type Store struct {
	db *sql.DB
	// Dir is the library folder: library.db, datasheets/ and cache/.
	Dir string
}

// Open opens (creating if needed) the library in dir.
func Open(dir string) (*Store, error) {
	for _, d := range []string{dir, filepath.Join(dir, "datasheets"), filepath.Join(dir, "cache")} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			return nil, err
		}
	}
	dsn := "file:" + filepath.ToSlash(filepath.Join(dir, "library.db")) + "?_pragma=foreign_keys(1)&_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)&_pragma=synchronous(NORMAL)"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	// One connection: SQLite serializes writers anyway, and this keeps
	// transactions and pragmas simple.
	db.SetMaxOpenConns(1)
	s := &Store{db: db, Dir: dir}
	if err := s.migrate(context.Background()); err != nil {
		db.Close()
		return nil, fmt.Errorf("open library %s: %w", dir, err)
	}
	return s, nil
}

// Close closes the database.
func (s *Store) Close() error { return s.db.Close() }

// DatasheetPath is where a stored datasheet's PDF lives.
func (s *Store) DatasheetPath(file string) string {
	return filepath.Join(s.Dir, "datasheets", filepath.Base(file))
}

// CachePath is a path inside the render cache.
func (s *Store) CachePath(name string) string {
	return filepath.Join(s.Dir, "cache", filepath.Base(name))
}

// migrations are applied in order; user_version records how many ran.
var migrations = []string{`
CREATE TABLE datasheets (
	id INTEGER PRIMARY KEY,
	sha256 TEXT NOT NULL UNIQUE,
	file TEXT NOT NULL,
	filename TEXT NOT NULL,
	title TEXT NOT NULL DEFAULT '',
	description TEXT NOT NULL DEFAULT '',
	manufacturer TEXT NOT NULL DEFAULT '',
	pages INTEGER NOT NULL DEFAULT 0,
	families TEXT NOT NULL DEFAULT '[]',
	source_url TEXT NOT NULL DEFAULT '',
	meta TEXT NOT NULL DEFAULT '{}',
	extractor TEXT NOT NULL DEFAULT '',
	imported_at TEXT NOT NULL
);
CREATE TABLE parts (
	id INTEGER PRIMARY KEY,
	mpn TEXT NOT NULL UNIQUE COLLATE NOCASE,
	manufacturer TEXT NOT NULL DEFAULT '',
	description TEXT NOT NULL DEFAULT '',
	category TEXT NOT NULL DEFAULT '',
	package TEXT NOT NULL DEFAULT '',
	datasheet_id INTEGER REFERENCES datasheets(id) ON DELETE SET NULL,
	tags TEXT NOT NULL DEFAULT '[]',
	created_at TEXT NOT NULL,
	updated_at TEXT NOT NULL
);
CREATE INDEX parts_datasheet ON parts(datasheet_id);
CREATE TABLE variants (
	mpn TEXT PRIMARY KEY COLLATE NOCASE,
	part_id INTEGER NOT NULL REFERENCES parts(id) ON DELETE CASCADE,
	package TEXT NOT NULL DEFAULT '',
	notes TEXT NOT NULL DEFAULT '',
	source TEXT NOT NULL DEFAULT 'auto'
);
CREATE INDEX variants_part ON variants(part_id);
CREATE TABLE pages (
	datasheet_id INTEGER NOT NULL REFERENCES datasheets(id) ON DELETE CASCADE,
	page INTEGER NOT NULL,
	width REAL NOT NULL,
	height REAL NOT NULL,
	text TEXT NOT NULL DEFAULT '',
	lines TEXT NOT NULL DEFAULT '[]',
	PRIMARY KEY (datasheet_id, page)
);
CREATE TABLE sections (
	id INTEGER PRIMARY KEY,
	datasheet_id INTEGER NOT NULL REFERENCES datasheets(id) ON DELETE CASCADE,
	ord INTEGER NOT NULL,
	num TEXT NOT NULL DEFAULT '',
	title TEXT NOT NULL,
	level INTEGER NOT NULL,
	kind TEXT NOT NULL,
	page INTEGER NOT NULL,
	y REAL NOT NULL,
	end_page INTEGER NOT NULL,
	end_y REAL NOT NULL,
	text TEXT NOT NULL DEFAULT ''
);
CREATE INDEX sections_ds ON sections(datasheet_id, ord);
CREATE TABLE figures (
	id INTEGER PRIMARY KEY,
	datasheet_id INTEGER NOT NULL REFERENCES datasheets(id) ON DELETE CASCADE,
	page INTEGER NOT NULL,
	number TEXT NOT NULL DEFAULT '',
	caption TEXT NOT NULL DEFAULT '',
	kind TEXT NOT NULL DEFAULT 'figure',
	x0 REAL, y0 REAL, x1 REAL, y1 REAL,
	section TEXT NOT NULL DEFAULT '',
	title TEXT NOT NULL DEFAULT '',
	x_axis TEXT NOT NULL DEFAULT '',
	y_axis TEXT NOT NULL DEFAULT '',
	conditions TEXT NOT NULL DEFAULT '',
	description TEXT NOT NULL DEFAULT '',
	curves TEXT NOT NULL DEFAULT '[]',
	applies_to TEXT NOT NULL DEFAULT '',
	source TEXT NOT NULL DEFAULT 'auto',
	updated_at TEXT NOT NULL
);
CREATE INDEX figures_ds ON figures(datasheet_id, page);
CREATE TABLE doc_tables (
	id INTEGER PRIMARY KEY,
	datasheet_id INTEGER NOT NULL REFERENCES datasheets(id) ON DELETE CASCADE,
	page INTEGER NOT NULL,
	number TEXT NOT NULL DEFAULT '',
	caption TEXT NOT NULL DEFAULT '',
	kind TEXT NOT NULL DEFAULT 'table',
	section TEXT NOT NULL DEFAULT '',
	x0 REAL, y0 REAL, x1 REAL, y1 REAL,
	header TEXT NOT NULL DEFAULT '[]',
	rows TEXT NOT NULL DEFAULT '[]'
);
CREATE INDEX tables_ds ON doc_tables(datasheet_id, page);
CREATE TABLE parameters (
	id INTEGER PRIMARY KEY,
	datasheet_id INTEGER NOT NULL REFERENCES datasheets(id) ON DELETE CASCADE,
	applies_to TEXT NOT NULL DEFAULT '',
	key TEXT NOT NULL DEFAULT '',
	symbol TEXT NOT NULL DEFAULT '',
	name TEXT NOT NULL DEFAULT '',
	conditions TEXT NOT NULL DEFAULT '',
	min REAL, typ REAL, max REAL,
	unit TEXT NOT NULL DEFAULT '',
	section TEXT NOT NULL DEFAULT '',
	page INTEGER NOT NULL DEFAULT -1,
	x0 REAL, y0 REAL, x1 REAL, y1 REAL,
	raw TEXT NOT NULL DEFAULT '',
	note TEXT NOT NULL DEFAULT '',
	source TEXT NOT NULL DEFAULT 'auto',
	updated_at TEXT NOT NULL
);
CREATE INDEX parameters_ds ON parameters(datasheet_id, key);
CREATE TABLE pins (
	id INTEGER PRIMARY KEY,
	datasheet_id INTEGER NOT NULL REFERENCES datasheets(id) ON DELETE CASCADE,
	package TEXT NOT NULL DEFAULT '',
	number TEXT NOT NULL DEFAULT '',
	name TEXT NOT NULL DEFAULT '',
	type TEXT NOT NULL DEFAULT '',
	description TEXT NOT NULL DEFAULT '',
	page INTEGER NOT NULL DEFAULT -1,
	source TEXT NOT NULL DEFAULT 'auto'
);
CREATE INDEX pins_ds ON pins(datasheet_id);
CREATE TABLE registers (
	id INTEGER PRIMARY KEY,
	datasheet_id INTEGER NOT NULL REFERENCES datasheets(id) ON DELETE CASCADE,
	name TEXT NOT NULL,
	address TEXT NOT NULL DEFAULT '',
	width INTEGER NOT NULL DEFAULT 0,
	reset TEXT NOT NULL DEFAULT '',
	access TEXT NOT NULL DEFAULT '',
	description TEXT NOT NULL DEFAULT '',
	fields TEXT NOT NULL DEFAULT '[]',
	page INTEGER NOT NULL DEFAULT -1,
	source TEXT NOT NULL DEFAULT 'agent'
);
CREATE INDEX registers_ds ON registers(datasheet_id);
CREATE TABLE recommendations (
	id INTEGER PRIMARY KEY,
	datasheet_id INTEGER NOT NULL REFERENCES datasheets(id) ON DELETE CASCADE,
	applies_to TEXT NOT NULL DEFAULT '',
	role TEXT NOT NULL,
	value TEXT NOT NULL DEFAULT '',
	mpn TEXT NOT NULL DEFAULT '',
	manufacturer TEXT NOT NULL DEFAULT '',
	conditions TEXT NOT NULL DEFAULT '',
	notes TEXT NOT NULL DEFAULT '',
	page INTEGER NOT NULL DEFAULT -1,
	source TEXT NOT NULL DEFAULT 'agent'
);
CREATE INDEX recommendations_ds ON recommendations(datasheet_id);
CREATE TABLE notes (
	id INTEGER PRIMARY KEY,
	target_kind TEXT NOT NULL,
	target_id TEXT NOT NULL,
	body TEXT NOT NULL,
	author TEXT NOT NULL DEFAULT 'user',
	created_at TEXT NOT NULL,
	updated_at TEXT NOT NULL
);
CREATE INDEX notes_target ON notes(target_kind, target_id);
CREATE TABLE projects (
	id INTEGER PRIMARY KEY,
	name TEXT NOT NULL UNIQUE COLLATE NOCASE,
	description TEXT NOT NULL DEFAULT '',
	created_at TEXT NOT NULL,
	updated_at TEXT NOT NULL
);
CREATE TABLE project_rails (
	id INTEGER PRIMARY KEY,
	project_id INTEGER NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
	name TEXT NOT NULL COLLATE NOCASE,
	v_nom REAL, v_min REAL, v_max REAL,
	source TEXT NOT NULL DEFAULT '',
	current_limit REAL,
	notes TEXT NOT NULL DEFAULT '',
	UNIQUE (project_id, name)
);
CREATE TABLE project_parts (
	id INTEGER PRIMARY KEY,
	project_id INTEGER NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
	part_id INTEGER NOT NULL REFERENCES parts(id) ON DELETE CASCADE,
	designator TEXT NOT NULL DEFAULT '',
	qty INTEGER NOT NULL DEFAULT 1,
	role TEXT NOT NULL DEFAULT '',
	supply_rail TEXT NOT NULL DEFAULT '',
	output_rail TEXT NOT NULL DEFAULT '',
	i_typ REAL, i_max REAL,
	variant TEXT NOT NULL DEFAULT '',
	notes TEXT NOT NULL DEFAULT ''
);
CREATE INDEX project_parts_project ON project_parts(project_id);
CREATE TABLE calculations (
	id INTEGER PRIMARY KEY,
	project_id INTEGER REFERENCES projects(id) ON DELETE CASCADE,
	part_id INTEGER REFERENCES parts(id) ON DELETE SET NULL,
	kind TEXT NOT NULL,
	title TEXT NOT NULL DEFAULT '',
	inputs TEXT NOT NULL DEFAULT '{}',
	outputs TEXT NOT NULL DEFAULT '{}',
	created_at TEXT NOT NULL
);
CREATE INDEX calculations_project ON calculations(project_id);
CREATE VIRTUAL TABLE fts USING fts5(
	kind UNINDEXED, ref UNINDEXED, datasheet UNINDEXED, page UNINDEXED,
	title, body,
	tokenize = 'unicode61 remove_diacritics 2'
);
`, `
ALTER TABLE doc_tables ADD COLUMN irregular TEXT NOT NULL DEFAULT '';
ALTER TABLE doc_tables ADD COLUMN source TEXT NOT NULL DEFAULT 'auto';
ALTER TABLE doc_tables ADD COLUMN title TEXT NOT NULL DEFAULT '';
ALTER TABLE doc_tables ADD COLUMN description TEXT NOT NULL DEFAULT '';
ALTER TABLE doc_tables ADD COLUMN updated_at TEXT NOT NULL DEFAULT '';
`, `
ALTER TABLE parts ADD COLUMN favorite INTEGER NOT NULL DEFAULT 0;
ALTER TABLE parts ADD COLUMN status TEXT NOT NULL DEFAULT '';
`, `
CREATE TABLE translations (
	lang TEXT NOT NULL,
	source TEXT NOT NULL,
	text TEXT NOT NULL,
	created_at TEXT NOT NULL,
	PRIMARY KEY (lang, source)
);
`, `
ALTER TABLE project_rails ADD COLUMN other_typ REAL;
ALTER TABLE project_rails ADD COLUMN other_max REAL;
`, `
CREATE TABLE project_nets (
	id INTEGER PRIMARY KEY,
	project_id INTEGER NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
	name TEXT NOT NULL COLLATE NOCASE,
	kind TEXT NOT NULL DEFAULT 'signal',
	bus TEXT NOT NULL DEFAULT '',
	voltage REAL,
	notes TEXT NOT NULL DEFAULT '',
	source TEXT NOT NULL DEFAULT 'user',
	UNIQUE(project_id, name)
);
CREATE TABLE project_blocks (
	id INTEGER PRIMARY KEY,
	project_id INTEGER NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
	name TEXT NOT NULL COLLATE NOCASE,
	kind TEXT NOT NULL DEFAULT '',
	value TEXT NOT NULL DEFAULT '',
	notes TEXT NOT NULL DEFAULT '',
	source TEXT NOT NULL DEFAULT 'user',
	UNIQUE(project_id, name)
);
CREATE TABLE project_connections (
	id INTEGER PRIMARY KEY,
	net_id INTEGER NOT NULL REFERENCES project_nets(id) ON DELETE CASCADE,
	entry_id INTEGER REFERENCES project_parts(id) ON DELETE CASCADE,
	block_id INTEGER REFERENCES project_blocks(id) ON DELETE CASCADE,
	pin TEXT NOT NULL DEFAULT '',
	notes TEXT NOT NULL DEFAULT '',
	source TEXT NOT NULL DEFAULT 'user'
);
CREATE INDEX project_connections_net ON project_connections(net_id);
CREATE TABLE project_layout (
	project_id INTEGER NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
	node TEXT NOT NULL,
	x REAL NOT NULL,
	y REAL NOT NULL,
	PRIMARY KEY (project_id, node)
);
`, `
ALTER TABLE project_connections ADD COLUMN ref TEXT NOT NULL DEFAULT '';
`, `
CREATE TABLE project_groups (
	id INTEGER PRIMARY KEY,
	project_id INTEGER NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
	name TEXT NOT NULL COLLATE NOCASE,
	description TEXT NOT NULL DEFAULT '',
	notes TEXT NOT NULL DEFAULT '',
	source TEXT NOT NULL DEFAULT 'user',
	UNIQUE(project_id, name)
);
CREATE TABLE project_assignments (
	project_id INTEGER NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
	node TEXT NOT NULL,
	group_id INTEGER REFERENCES project_groups(id) ON DELETE SET NULL,
	supports TEXT NOT NULL DEFAULT '',
	source TEXT NOT NULL DEFAULT 'user',
	PRIMARY KEY (project_id, node)
);
`, `
ALTER TABLE project_assignments ADD COLUMN cluster TEXT NOT NULL DEFAULT '';
`}

// migrate applies pending schema migrations.
func (s *Store) migrate(ctx context.Context) error {
	var v int
	if err := s.db.QueryRowContext(ctx, "PRAGMA user_version").Scan(&v); err != nil {
		return err
	}
	for i := v; i < len(migrations); i++ {
		tx, err := s.db.BeginTx(ctx, nil)
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, migrations[i]); err != nil {
			tx.Rollback()
			return fmt.Errorf("migration %d: %w", i+1, err)
		}
		if _, err := tx.ExecContext(ctx, fmt.Sprintf("PRAGMA user_version = %d", i+1)); err != nil {
			tx.Rollback()
			return err
		}
		if err := tx.Commit(); err != nil {
			return err
		}
	}
	return nil
}

// now is the timestamp format stored in the database.
func now() string { return time.Now().UTC().Format(time.RFC3339) }

// parseTime reads a stored timestamp (zero on failure).
func parseTime(s string) time.Time {
	t, _ := time.Parse(time.RFC3339, s)
	return t
}

// tx runs fn in a transaction, committing on success.
func (s *Store) tx(ctx context.Context, fn func(*sql.Tx) error) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	if err := fn(tx); err != nil {
		tx.Rollback()
		return err
	}
	return tx.Commit()
}

// splitList reads a comma-separated column.
func splitList(s string) []string {
	var out []string
	for _, p := range strings.Split(s, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

// nullFloat converts an optional value for storage.
func nullFloat(v *float64) any {
	if v == nil {
		return nil
	}
	return *v
}

// floatPtr converts a nullable column.
func floatPtr(n sql.NullFloat64) *float64 {
	if !n.Valid {
		return nil
	}
	v := n.Float64
	return &v
}
