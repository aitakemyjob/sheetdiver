package api

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/aitakemyjob/sheetdiver/internal/config"
	"github.com/aitakemyjob/sheetdiver/internal/core"
	"github.com/aitakemyjob/sheetdiver/internal/testutil"
)

// newAPI builds the method table over a library holding the synthetic
// datasheet.
func newAPI(t *testing.T) (*API, int64) {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("SHEETDIVER_LIBRARY", filepath.Join(dir, "lib"))
	store, err := config.Open(filepath.Join(dir, "c.json"))
	if err != nil {
		t.Fatal(err)
	}
	svc, err := core.New(store, "test")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { svc.Close() })
	res, err := svc.Import(context.Background(), core.ImportRequest{Data: testutil.Datasheet(), Filename: "sd.pdf"})
	if err != nil {
		t.Fatal(err)
	}
	return New(svc), res.Datasheet.ID
}

// invoke calls a method and decodes its result.
func invoke(t *testing.T, a *API, method string, args any, out any) error {
	t.Helper()
	raw, _ := json.Marshal(args)
	res, err := a.Invoke(context.Background(), method, raw)
	if err == nil && out != nil {
		if err := json.Unmarshal(res, out); err != nil {
			t.Fatal(err)
		}
	}
	return err
}

func TestOpenDatasheet(t *testing.T) {
	a, ds := newAPI(t)
	var st AppState
	invoke(t, a, "app.state", nil, &st)
	if st.CanOpenFile {
		t.Error("browser mode cannot open files")
	}
	if err := invoke(t, a, "datasheet.open", map[string]any{"id": ds}, nil); err == nil || !strings.Contains(err.Error(), "desktop") {
		t.Errorf("browser mode explains itself: %v", err)
	}
	var opened string
	a.OpenFile = func(p string) error { opened = p; return nil }
	invoke(t, a, "app.state", nil, &st)
	if !st.CanOpenFile {
		t.Error("desktop mode can open files")
	}
	var out map[string]string
	if err := invoke(t, a, "datasheet.open", map[string]any{"id": ds}, &out); err != nil {
		t.Fatal(err)
	}
	if opened == "" || opened != out["path"] || !strings.HasSuffix(opened, "-sd.pdf") {
		t.Errorf("opened %q (result %v)", opened, out)
	}
	if err := invoke(t, a, "datasheet.open", map[string]any{"id": 999}, nil); err == nil {
		t.Error("unknown datasheets are an error")
	}
}

func TestCompareAndCategories(t *testing.T) {
	a, _ := newAPI(t)
	var cats []core.CategoryInfo
	if err := invoke(t, a, "categories.list", nil, &cats); err != nil || len(cats) != 1 || cats[0].Category != "buck" {
		t.Fatalf("categories = %+v %v", cats, err)
	}
	var res core.CompareResult
	if err := invoke(t, a, "compare", map[string]any{"category": "buck", "filters": []map[string]any{{"key": "vin", "op": ">=", "value": 12}}}, &res); err != nil || len(res.Rows) != 2 {
		t.Fatalf("compare = %+v %v", res, err)
	}
}

func TestRecordTableFromUI(t *testing.T) {
	a, ds := newAPI(t)
	var tables []struct {
		ID        int64  `json:"id"`
		Number    string `json:"number"`
		Irregular string `json:"irregular"`
	}
	invoke(t, a, "tables.list", map[string]any{"id": ds}, &tables)
	var id int64
	for _, tb := range tables {
		if tb.Number == "7-1" && tb.Irregular != "" {
			id = tb.ID
		}
	}
	if id == 0 {
		t.Fatalf("irregular table 7-1 not listed: %+v", tables)
	}
	var saved struct {
		Source    string `json:"source"`
		Irregular string `json:"irregular"`
	}
	if err := invoke(t, a, "table.record", map[string]any{"id": id, "header": []string{"VOUT", "RSET1"}, "rows": [][]string{{"3.3 V", "1.15 kΩ"}}}, &saved); err != nil {
		t.Fatal(err)
	}
	if saved.Source != "user" || saved.Irregular != "" {
		t.Errorf("saved = %+v", saved)
	}
}
