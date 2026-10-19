package api

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/aitakemyjob/sheetdiver/internal/config"
	"github.com/aitakemyjob/sheetdiver/internal/core"
	"github.com/aitakemyjob/sheetdiver/internal/library"
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

func TestImportQueueAndBOM(t *testing.T) {
	a, _ := newAPI(t)
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "zx7001.pdf"), testutil.Mini("ZX7001", "Step-Down Converter"), 0o644); err != nil {
		t.Fatal(err)
	}
	var jobs []core.ImportJob
	if err := invoke(t, a, "imports.start", map[string]any{"folder": dir}, &jobs); err != nil || len(jobs) != 1 {
		t.Fatalf("start = %+v %v", jobs, err)
	}
	if err := invoke(t, a, "imports.start", map[string]any{"folder": t.TempDir()}, nil); err == nil {
		t.Error("an empty folder is reported")
	}
	// Wait for the background import.
	deadline := time.Now().Add(30 * time.Second)
	for {
		var list []core.ImportJob
		if err := invoke(t, a, "imports.list", nil, &list); err != nil {
			t.Fatal(err)
		}
		if len(list) > 0 && list[0].Status == core.JobDone {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("import didn't finish: %+v", list)
		}
		time.Sleep(20 * time.Millisecond)
	}
	var bom core.BOMResult
	if err := invoke(t, a, "bom.import", map[string]any{"text": "Ref,MPN\nU1,ZX7001\nU2,NOPE123\n", "filename": "b.csv", "fetch": false}, &bom); err != nil {
		t.Fatal(err)
	}
	if bom.Project != "b" || bom.Counts[core.BOMInLibrary] != 1 || bom.Counts[core.BOMNoDatasheet] != 1 {
		t.Errorf("bom = %+v", bom)
	}
	var p library.Part
	if err := invoke(t, a, "part.update", map[string]any{"id": jobsPart(t, a, "ZX7001"), "favorite": true, "status": "approved"}, &p); err != nil || !p.Favorite || p.Status != "approved" {
		t.Errorf("update = %+v %v", p, err)
	}
	var st AppState
	if err := invoke(t, a, "app.state", nil, &st); err != nil || len(st.Statuses) != 5 {
		t.Errorf("statuses = %v %v", st.Statuses, err)
	}
	if err := invoke(t, a, "imports.clear", nil, nil); err != nil {
		t.Fatal(err)
	}
}

// jobsPart returns a part's id.
func jobsPart(t *testing.T, a *API, mpn string) int64 {
	t.Helper()
	var d core.PartDetail
	if err := invoke(t, a, "part.detail", map[string]any{"part": mpn}, &d); err != nil {
		t.Fatal(err)
	}
	return d.Part.ID
}
