package extract

import (
	"context"
	"encoding/json"
	"math"
	"os"
	"path/filepath"
	"slices"
	"strings"
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
		// Pin names that must be extracted, and the packages pins list.
		PinNames      []string `json:"pinNames"`
		PinPackages   []string `json:"pinPackages"`
		RegularTables []string `json:"regularTables"`
		// Revision as rendered by Revision.String.
		Revision string `json:"revision"`
		// Language as detected (en when omitted).
		Language string `json:"language"`
		// Values of the first parameter with each key.
		Values map[string]struct {
			Min *float64 `json:"min"`
			Typ *float64 `json:"typ"`
			Max *float64 `json:"max"`
			// Unit, when set, must match (catches µ read as m).
			Unit string `json:"unit"`
		} `json:"values"`
		// Packages by name: dimensions by key as [min, max] (nom when
		// the table gives one value), the land by key, and how many
		// numbers a drawing without a table must offer.
		Packages map[string]corpusPackage `json:"packages"`
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
			if want := map[bool]string{true: x.Language, false: "en"}[x.Language != ""]; r.Language != want {
				t.Errorf("language = %q, want %q", r.Language, want)
			}
			if x.Revision != "" && r.Revision.String() != x.Revision {
				t.Errorf("revision = %q, want %q", r.Revision, x.Revision)
			}
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
			pinNames, pinPkgs := map[string]bool{}, map[string]bool{}
			for _, p := range r.Pins {
				pinNames[p.Name] = true
				pinPkgs[p.Package] = true
				if !hasAlnum(p.Number) && !hasAlnum(p.Name) {
					t.Errorf("junk pin row %+v", p)
				}
			}
			for _, n := range x.PinNames {
				if !pinNames[n] {
					t.Errorf("no pin named %s", n)
				}
			}
			for _, pk := range x.PinPackages {
				if !pinPkgs[pk] {
					t.Errorf("no pins for package %q", pk)
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
				} else if want.Unit != "" && got.Unit != want.Unit {
					t.Errorf("%s unit = %q, want %q", key, got.Unit, want.Unit)
				}
			}
			checkPackages(t, r.Packages, x.Packages)
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

// corpusPackage is a package a datasheet must give.
type corpusPackage struct {
	Code          string                `json:"code"`
	Dims          map[string][2]float64 `json:"dims"`
	Land          map[string]float64    `json:"land"`
	MinCandidates int                   `json:"minCandidates"`
	// Parts are the parts that come in it, by the ordering scheme.
	Parts []string `json:"parts"`
}

// checkPackages compares packages read with those expected.
func checkPackages(t *testing.T, got []Package, exp map[string]corpusPackage) {
	t.Helper()
	near := func(a *float64, b float64) bool { return a != nil && math.Abs(*a-b) < 0.006 }
	for name, w := range exp {
		var p *Package
		for i := range got {
			if strings.EqualFold(got[i].Name, name) {
				p = &got[i]
			}
		}
		if p == nil {
			t.Errorf("no package %s", name)
			continue
		}
		if w.Code != "" && p.Code != w.Code {
			t.Errorf("package %s code = %q, want %q", name, p.Code, w.Code)
		}
		for key, mm := range w.Dims {
			var d *PackageDim
			for i := range p.Dims {
				if p.Dims[i].Key == key {
					d = &p.Dims[i]
				}
			}
			switch {
			case d == nil:
				t.Errorf("package %s has no %s", name, key)
			case mm[0] == mm[1] && !near(d.Nom, mm[0]):
				t.Errorf("package %s %s = %+v, want %g", name, key, *d, mm[0])
			case mm[0] != mm[1] && (!near(d.Min, mm[0]) || !near(d.Max, mm[1])):
				t.Errorf("package %s %s = %+v, want %g–%g", name, key, *d, mm[0], mm[1])
			}
		}
		for key, mm := range w.Land {
			ok := false
			for _, d := range p.Land {
				ok = ok || d.Key == key && near(d.Nom, mm)
			}
			if !ok {
				t.Errorf("package %s land has no %s of %g mm", name, key, mm)
			}
		}
		if w.Parts != nil && !slices.Equal(p.Parts, w.Parts) {
			t.Errorf("package %s parts = %v, want %v", name, p.Parts, w.Parts)
		}
		if len(p.Candidates) < w.MinCandidates {
			t.Errorf("package %s: %d numbers off its drawing, want at least %d", name, len(p.Candidates), w.MinCandidates)
		}
	}
}
