package library

import (
	"context"
	"database/sql"
	"fmt"
	"path/filepath"
	"slices"
	"testing"
)

// A library made before variations keeps its simulations: each becomes a
// test whose first variation holds the netlist, with the variants as its
// conditions and the runs, under the same IDs, moved to it.
func TestVariationMigration(t *testing.T) {
	ctx := context.Background()
	dir := t.TempDir()
	s, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	s.Close()
	// Back to the schema as it was, then the rows that build would have written.
	db, err := sql.Open("sqlite", "file:"+filepath.ToSlash(filepath.Join(dir, "library.db"))+"?_pragma=foreign_keys(1)")
	if err != nil {
		t.Fatal(err)
	}
	// The schema just before the variation migration: the migrations
	// after it undone too.
	before := slices.Index(migrations, variationMigration)
	for _, q := range []string{
		"DROP TABLE cad_items",
		"DROP TABLE simulation_runs", "DROP TABLE simulation_variations", "DROP TABLE simulations", "DROP TABLE spice_models",
		"ALTER TABLE pins DROP COLUMN part",
		migrations[before-1],
		fmt.Sprintf("PRAGMA user_version = %d", before),
		"INSERT INTO projects (name, created_at, updated_at) VALUES ('Sensor', 't0', 't0')",
		`INSERT INTO simulations (project_id, name, kind, netlist, models, variants, created_at, updated_at)
			VALUES (1, '3V3 start-up', 'tran', 'deck', '["LDO"]', '[{"name":"hot","temp":85}]', 't1', 't1')`,
		"INSERT INTO simulation_runs (id, simulation_id, variant, state, measures) VALUES (7, 1, 'hot', 'done', '[{\"name\":\"vfinal\",\"value\":3.3}]')",
	} {
		if _, err := db.ExecContext(ctx, q); err != nil {
			t.Fatalf("%s: %v", q, err)
		}
	}
	db.Close()

	s, err = Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	vs, err := s.Variations(ctx, 1)
	if err != nil || len(vs) != 1 {
		t.Fatalf("variations = %+v, %v", vs, err)
	}
	v := vs[0]
	if v.Name != "Rev A" || v.Netlist != "deck" || len(v.Models) != 1 || len(v.Conditions) != 1 || v.Conditions[0].Name != "hot" || *v.Conditions[0].Temp != 85 {
		t.Errorf("variation = %+v", v)
	}
	runs, err := s.Runs(ctx, 1)
	if err != nil || len(runs) != 1 || runs[0].ID != 7 || runs[0].VariationID != v.ID || runs[0].Condition != "hot" || runs[0].State != "done" || len(runs[0].Measures) != 1 {
		t.Fatalf("runs = %+v, %v", runs, err)
	}
}
