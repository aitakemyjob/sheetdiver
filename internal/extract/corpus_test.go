package extract

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/aitakemyjob/sheetdiver/internal/pdf"
)

// corpusEntry is one datasheet in testdata/corpus/manifest.json.
type corpusEntry struct {
	File   string `json:"file"`
	Expect struct {
		Manufacturer string            `json:"manufacturer"`
		Category     string            `json:"category"`
		PartNumbers  []string          `json:"partNumbers"`
		Families     []string          `json:"families"`
		Variants     map[string]string `json:"variants"`
		MinVariants  int               `json:"minVariants"`
		MinSections  int               `json:"minSections"`
		MinFigures   int               `json:"minFigures"`
		Pins         int               `json:"pins"`
		Keys         []string          `json:"keys"`
		SectionKinds []string          `json:"sectionKinds"`
		Figures      map[string]string `json:"figures"`
		// Tables by number that must (or must not) be flagged irregular.
		IrregularTables []string `json:"irregularTables"`
		RegularTables   []string `json:"regularTables"`
		// Values of the first parameter with each key.
		Values map[string]struct {
			Min *float64 `json:"min"`
			Typ *float64 `json:"typ"`
			Max *float64 `json:"max"`
		} `json:"values"`
	} `json:"expect"`
}

// num formats an optional expected value.
func num(v *float64) any {
	if v == nil {
		return "–"
	}
	return *v
}

// corpusDir is where `make corpus` puts the datasheets.
func corpusDir() string {
	if d := os.Getenv("SHEETDIVER_CORPUS"); d != "" {
		return d
	}
	if d, err := os.UserCacheDir(); err == nil {
		return filepath.Join(d, "sheetdiver", "corpus")
	}
	return ""
}

// TestCorpus checks extraction against real datasheets. It runs only when
// the corpus has been downloaded (make corpus), and each missing file is
// skipped, so it never fails offline.
func TestCorpus(t *testing.T) {
	if testing.Short() {
		t.Skip("corpus test skipped in -short mode")
	}
	raw, err := os.ReadFile("../../testdata/corpus/manifest.json")
	if err != nil {
		t.Fatal(err)
	}
	var m struct {
		Datasheets []corpusEntry `json:"datasheets"`
	}
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatal(err)
	}
	dir := corpusDir()
	engine := pdf.NewEngine(pdf.DefaultCacheDir())
	defer engine.Close()
	for _, e := range m.Datasheets {
		t.Run(e.File, func(t *testing.T) {
			data, err := os.ReadFile(filepath.Join(dir, e.File))
			if err != nil {
				t.Skipf("not downloaded (run make corpus): %v", err)
			}
			doc, err := engine.Extract(context.Background(), data, nil)
			if err != nil {
				t.Fatal(err)
			}
			r := Analyze(doc, e.File)
			x := e.Expect
			if x.Manufacturer != "" && r.Manufacturer != x.Manufacturer {
				t.Errorf("manufacturer = %q, want %q", r.Manufacturer, x.Manufacturer)
			}
			if x.Category != "" && r.Category != x.Category {
				t.Errorf("category = %q, want %q", r.Category, x.Category)
			}
			for _, p := range x.PartNumbers {
				if !slices.Contains(r.PartNumbers, p) {
					t.Errorf("part numbers %v lack %s", r.PartNumbers, p)
				}
			}
			for _, f := range x.Families {
				if !slices.Contains(r.Families, f) {
					t.Errorf("families %v lack %s", r.Families, f)
				}
			}
			bases := map[string]string{}
			for _, v := range r.Variants {
				bases[v.MPN] = v.Base
			}
			for mpn, base := range x.Variants {
				if bases[mpn] != base {
					t.Errorf("variant %s has base %q, want %q", mpn, bases[mpn], base)
				}
			}
			if len(r.Variants) < x.MinVariants {
				t.Errorf("%d variants, want at least %d", len(r.Variants), x.MinVariants)
			}
			if len(r.Sections) < x.MinSections {
				t.Errorf("%d sections, want at least %d", len(r.Sections), x.MinSections)
			}
			if len(r.Figures) < x.MinFigures {
				t.Errorf("%d figures, want at least %d", len(r.Figures), x.MinFigures)
			}
			if len(r.Pins) < x.Pins {
				t.Errorf("%d pins, want at least %d", len(r.Pins), x.Pins)
			}
			keys := map[string]bool{}
			for _, p := range r.Params {
				keys[p.Key] = true
			}
			for _, k := range x.Keys {
				if !keys[k] {
					t.Errorf("no parameter with key %s", k)
				}
			}
			kinds := map[string]bool{}
			for _, s := range r.Sections {
				kinds[s.Kind] = true
			}
			for _, k := range x.SectionKinds {
				if !kinds[k] {
					t.Errorf("no section of kind %s", k)
				}
			}
			irregular := map[string]bool{}
			for _, tb := range r.Tables {
				if tb.Number != "" && tb.Irregular != "" {
					irregular[tb.Number] = true
				}
			}
			for _, n := range x.IrregularTables {
				if !irregular[n] {
					t.Errorf("table %s should be irregular", n)
				}
			}
			for _, n := range x.RegularTables {
				if irregular[n] {
					t.Errorf("table %s should be a plain grid", n)
				}
			}
			for key, want := range x.Values {
				var got *Param
				for i := range r.Params {
					if r.Params[i].Key == key {
						got = &r.Params[i]
						break
					}
				}
				same := func(a, b *float64) bool { return b == nil || (a != nil && *a == *b) }
				if got == nil || !same(got.Min, want.Min) || !same(got.Typ, want.Typ) || !same(got.Max, want.Max) {
					t.Errorf("%s = %+v, want %v/%v/%v", key, got, num(want.Min), num(want.Typ), num(want.Max))
				}
			}
			figs := map[string]string{}
			for _, f := range r.Figures {
				figs[f.Number] = f.Kind
			}
			for n, k := range x.Figures {
				if figs[n] != k {
					t.Errorf("figure %s kind = %q, want %q", n, figs[n], k)
				}
			}
		})
	}
}
