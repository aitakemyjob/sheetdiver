package library

import (
	"context"
	"database/sql"
	"fmt"
	"path/filepath"
	"testing"
)

// A library that ran 0.2.2's migrations (pins.part where this build has
// the CAD library next) and one that ran the CAD library's preview
// migration in that place both open with every table and column.
func TestMigrationOrders(t *testing.T) {
	ctx := context.Background()
	for _, c := range []struct {
		name string
		undo string // what the other order didn't make
	}{
		{"0.2.2 first", "DROP TABLE cad_items"},
		{"the preview first", "ALTER TABLE pins DROP COLUMN part"},
	} {
		dir := t.TempDir()
		s, err := Open(dir)
		if err != nil {
			t.Fatal(err)
		}
		s.Close()
		db, err := sql.Open("sqlite", "file:"+filepath.ToSlash(filepath.Join(dir, "library.db")))
		if err != nil {
			t.Fatal(err)
		}
		// Both histories counted one migration past the simulations'.
		for _, q := range []string{c.undo, fmt.Sprintf("PRAGMA user_version = %d", len(migrations)-1)} {
			if _, err := db.ExecContext(ctx, q); err != nil {
				t.Fatalf("%s: %s: %v", c.name, q, err)
			}
		}
		if c.name == "the preview first" {
			_, _ = db.ExecContext(ctx, fmt.Sprintf("PRAGMA user_version = %d", len(migrations)))
		}
		db.Close()
		s, err = Open(dir)
		if err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		if _, err := s.CadItems(ctx, 0); err != nil {
			t.Errorf("%s: cad_items: %v", c.name, err)
		}
		var n int
		_ = s.db.QueryRowContext(ctx, `SELECT count(*) FROM pragma_table_info('pins') WHERE name = 'part'`).Scan(&n)
		if n != 1 {
			t.Errorf("%s: pins.part missing", c.name)
		}
		s.Close()
	}
}
