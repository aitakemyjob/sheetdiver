package core

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/aitakemyjob/sheetdiver/internal/cad"
	"github.com/aitakemyjob/sheetdiver/internal/cad/kicad"
)

// The footprint and symbol corpus (testdata/corpus/cad.json): what
// SheetDiver makes of a corpus datasheet, or of a drawing an agent
// recorded, checked against maker's and KiCad's library parts. The
// references are downloaded with the datasheets (scripts/corpus.sh).

type cadCorpus struct {
	Footprints []struct {
		Name      string  `json:"name"`
		Reference string  `json:"reference"`
		Tolerance float64 `json:"tolerance"`
		Sizes     bool    `json:"sizes"`
		// SizeTolerance checks sizes to a tolerance of their own.
		SizeTolerance float64         `json:"sizeTolerance"`
		Drawn         *cad.Package    `json:"drawn"`
		Datasheet     *corpusCadSheet `json:"datasheet"`
		// Standard is one of SheetDiver's standard outlines (a passive's
		// 0603), as a part of that package gets it.
		Standard string `json:"standard"`
		// PadNames are our pads' numbers as the reference numbers them
		// (an exposed pad the pin table calls "PAD", the reference 9).
		PadNames map[string]string `json:"padNames"`
	} `json:"footprints"`
	Symbols []struct {
		Name      string `json:"name"`
		Reference struct {
			File   string `json:"file"`
			Symbol string `json:"symbol"`
		} `json:"reference"`
		Datasheet corpusCadSheet    `json:"datasheet"`
		Aliases   map[string]string `json:"aliases"`
		Missing   []string          `json:"missing"`
	} `json:"symbols"`
}

// corpusCadSheet is a part's package in a corpus datasheet.
type corpusCadSheet struct {
	File    string `json:"file"`
	Part    string `json:"part"`
	Package string `json:"package"`
}

// cadCorpusDir is where the corpus is downloaded.
func cadCorpusDir() string {
	if d := os.Getenv("SHEETDIVER_CORPUS"); d != "" {
		return d
	}
	if d, err := os.UserCacheDir(); err == nil {
		return filepath.Join(d, "sheetdiver", "corpus")
	}
	return filepath.Join(os.TempDir(), "sheetdiver-corpus")
}

// readRef parses a reference library file, or skips the test when it
// isn't downloaded.
func readRef(t *testing.T, file string) *kicad.Node {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(cadCorpusDir(), "cad", file))
	if err != nil {
		t.Skipf("not downloaded (run make corpus): %v", err)
	}
	nd, err := kicad.Parse(string(b))
	if err != nil {
		t.Fatal(err)
	}
	return nd
}

func TestCadCorpus(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("..", "..", "testdata", "corpus", "cad.json"))
	if err != nil {
		t.Fatal(err)
	}
	var m cadCorpus
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	// Each datasheet is imported once, into a library of its own that
	// lasts the whole test.
	root := t
	services := map[string]*Service{}
	partCad := func(t *testing.T, ds corpusCadSheet) CadPackage {
		t.Helper()
		s, ok := services[ds.File]
		if !ok {
			path := filepath.Join(cadCorpusDir(), ds.File)
			if _, err := os.Stat(path); err != nil {
				t.Skipf("not downloaded (run make corpus): %v", err)
			}
			s = newService(root)
			if _, err := s.Import(ctx, ImportRequest{Path: path}); err != nil {
				t.Fatal(err)
			}
			services[ds.File] = s
		}
		pc, err := s.PartCad(ctx, ds.Part)
		if err != nil {
			t.Fatal(err)
		}
		var names []string
		for _, p := range pc.Packages {
			if ds.Package == "" || strings.EqualFold(p.Package, ds.Package) {
				return p
			}
			names = append(names, p.Package)
		}
		t.Fatalf("%s has no package %q, only %q", ds.Part, ds.Package, names)
		return CadPackage{}
	}

	for _, e := range m.Footprints {
		t.Run("footprint/"+e.Name, func(t *testing.T) {
			ref := kicad.ReadFootprint(readRef(t, e.Reference))
			var ours cad.Footprint
			switch {
			case e.Drawn != nil:
				f, ps := cad.MakeFootprint(e.Name, *e.Drawn)
				if err := cad.Err(ps); err != nil {
					t.Fatal(err)
				}
				ours = f
			case e.Standard != "":
				p, ok := cad.Standard(e.Standard, 0)
				if !ok {
					t.Fatalf("%s isn't a standard outline", e.Standard)
				}
				f, ps := cad.MakeFootprint(e.Name, p)
				if err := cad.Err(ps); err != nil {
					t.Fatal(err)
				}
				ours = f
			case e.Datasheet != nil:
				pk := partCad(t, *e.Datasheet)
				if pk.Footprint == nil || pk.Footprint.Footprint == nil {
					t.Fatalf("no footprint: %s", pk.Missing)
				}
				ours = *pk.Footprint.Footprint
			}
			for i, p := range ours.Pads {
				if n, ok := e.PadNames[p.Number]; ok {
					ours.Pads[i].Number = n
				}
			}
			tol := e.Tolerance
			if tol == 0 {
				tol = 0.05
			}
			d := cad.CompareFootprints(ours, ref, tol)
			switch {
			case e.SizeTolerance > 0:
				d.Resized = cad.CompareFootprints(ours, ref, e.SizeTolerance).Resized
			case !e.Sizes:
				d.Resized = nil
			}
			if !d.OK() {
				t.Errorf("differs from %s:\n%s", e.Reference, d)
			}
		})
	}

	for _, e := range m.Symbols {
		t.Run("symbol/"+e.Name, func(t *testing.T) {
			lib := readRef(t, e.Reference.File)
			var refSym *kicad.Node
			for _, nd := range lib.Children("symbol") {
				if nd.Text(0) == e.Reference.Symbol {
					refSym = nd
				}
			}
			if refSym == nil {
				t.Fatalf("%s has no symbol %s", e.Reference.File, e.Reference.Symbol)
			}
			pk := partCad(t, e.Datasheet)
			if pk.Symbol == nil || pk.Symbol.Symbol == nil {
				t.Fatalf("no symbol for %s %s", e.Datasheet.Part, e.Datasheet.Package)
			}
			d := cad.CompareSymbols(*pk.Symbol.Symbol, kicad.ReadSymbol(refSym), e.Aliases)
			if slices.Equal(d.Missing, e.Missing) {
				d.Missing = nil
			}
			if !d.OK() {
				t.Errorf("differs from %s's %s:\n%s", e.Reference.File, e.Reference.Symbol, d)
			}
		})
	}
}
