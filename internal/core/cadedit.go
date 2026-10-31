package core

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/aitakemyjob/sheetdiver/internal/cad"
	"github.com/aitakemyjob/sheetdiver/internal/cadlib"
	"github.com/aitakemyjob/sheetdiver/internal/library"
)

// Editing symbols, footprints and 3D models in SheetDiver: the app's
// editors and an agent's edit_ tools give the whole model as it should
// be. It's checked, kept as the user's edit (the newest edit wins over
// one made in a CAD tool, and every format's file is rewritten from it),
// and stays until it's reset. A 3D model's edit is kept beside the
// footprint's, so editing the footprint anywhere doesn't lose it.

// CadModel is a part's symbol, footprint and 3D model for one package,
// as data to edit.
type CadModel struct {
	Part      string         `json:"part"`
	Package   string         `json:"package"`
	Symbol    *cad.Symbol    `json:"symbol,omitempty"`
	Footprint *cad.Footprint `json:"footprint,omitempty"`
	Shapes    []cad.Shape3D  `json:"shapes,omitempty"`
}

// CadModelOf is a part's model for a package (its only one when pkg is
// "").
func (s *Service) CadModelOf(ctx context.Context, ref, pkg string) (CadModel, error) {
	pc, err := s.PartCad(ctx, ref)
	if err != nil {
		return CadModel{}, err
	}
	for _, p := range pc.Packages {
		if pkg != "" && !strings.EqualFold(p.Package, pkg) {
			continue
		}
		if pkg == "" && len(pc.Packages) > 1 {
			return CadModel{}, fmt.Errorf("%s comes in %s: say which package", pc.Part.MPN, packageNames(pc.Packages))
		}
		m := CadModel{Part: pc.Part.MPN, Package: p.Package}
		if p.Symbol != nil {
			m.Symbol = p.Symbol.Symbol
		}
		if p.Footprint != nil {
			m.Footprint, m.Shapes = p.Footprint.Footprint, p.Footprint.Shapes
		}
		return m, nil
	}
	return CadModel{}, fmt.Errorf("%s has no package %q", pc.Part.MPN, pkg)
}

// packageNames lists packages' names.
func packageNames(ps []CadPackage) string {
	var names []string
	for _, p := range ps {
		names = append(names, fmt.Sprintf("%q", p.Package))
	}
	return strings.Join(names, ", ")
}

// editCheck is one check an edit goes through before it's kept.
type editCheck func(part library.Part, pkg string, m CadModel) []cad.Problem

// symbolChecks and footprintChecks are the checks, in order.
var (
	symbolChecks = []editCheck{
		func(_ library.Part, _ string, m CadModel) []cad.Problem { return m.Symbol.Problems() },
		matchOther,
	}
	footprintChecks = []editCheck{
		func(_ library.Part, _ string, m CadModel) []cad.Problem { return m.Footprint.Problems() },
		matchOther,
	}
)

// matchOther checks an edited symbol against the package's footprint, or
// an edited footprint against its symbol: pins and pads by number.
func matchOther(_ library.Part, _ string, m CadModel) []cad.Problem {
	if m.Symbol == nil || m.Footprint == nil {
		return nil
	}
	ps := cad.Match(*m.Symbol, *m.Footprint)
	for i := range ps {
		ps[i].Level = cad.Warning
	}
	return ps
}

// runChecks runs checks on an edit.
func runChecks(checks []editCheck, part library.Part, pkg string, m CadModel) []cad.Problem {
	var out []cad.Problem
	for _, c := range checks {
		out = append(out, c(part, pkg, m)...)
	}
	return out
}

// keep saves an edit to an item, under the cad lock: the library is
// synced first (to take any edit made in a CAD tool meanwhile) and after
// (to write the edit to every format).
func (s *Service) keep(ctx context.Context, ref, pkg, kind string, change func(p library.Part, it *library.CadItem) ([]cad.Problem, error)) (PartCad, []cad.Problem, error) {
	s.cadMu.Lock()
	defer s.cadMu.Unlock()
	if _, err := s.syncCad(ctx); err != nil {
		return PartCad{}, nil, err
	}
	p, it, err := s.cadItem(ctx, ref, pkg, kind)
	if err != nil {
		return PartCad{}, nil, err
	}
	ps, err := change(p, &it)
	if err != nil {
		return PartCad{}, ps, err
	}
	if _, err := s.Lib.SaveCadItem(ctx, it); err != nil {
		return PartCad{}, ps, err
	}
	if _, err := s.syncCad(ctx); err != nil {
		return PartCad{}, ps, err
	}
	pc, err := s.PartCad(ctx, p.MPN)
	return pc, ps, err
}

// current is the part's model for an item's package, to check an edit
// of one half against the other.
func (s *Service) current(ctx context.Context, p library.Part, pkg string) CadModel {
	m, _ := s.CadModelOf(ctx, p.MPN, pkg)
	return m
}

// editedRaw are an item's kept edits with a new one made in SheetDiver.
func editedRaw(old map[string]string, model any) map[string]string {
	b, _ := json.Marshal(model)
	raw := map[string]string{cadlib.ModelKey: string(b)}
	if m, ok := old[cadlib.Model3DKey]; ok {
		raw[cadlib.Model3DKey] = m
	}
	return raw
}

// EditSymbol keeps a symbol as edited, by the user in the app or an agent
// (source).
func (s *Service) EditSymbol(ctx context.Context, ref, pkg string, sym cad.Symbol, notes, source string) (PartCad, []cad.Problem, error) {
	return s.keep(ctx, ref, pkg, library.CadSymbol, func(p library.Part, it *library.CadItem) ([]cad.Problem, error) {
		if err := cadlib.State(it.State).To(cadlib.Edited, p.MPN+" symbol"); err != nil {
			return nil, err
		}
		m := s.current(ctx, p, it.Package)
		m.Symbol = &sym
		ps := runChecks(symbolChecks, p, it.Package, m)
		if err := cad.Err(ps); err != nil {
			return ps, err
		}
		it.Raw, it.State, it.Source = editedRaw(it.Raw, sym), string(cadlib.Edited), source
		if notes != "" {
			it.Notes = notes
		}
		return ps, nil
	})
}

// EditFootprint keeps a footprint as edited: its pads and drawing. Its
// courtyard and body boxes and its mounting follow what's drawn.
func (s *Service) EditFootprint(ctx context.Context, ref, pkg string, fp cad.Footprint, notes, source string) (PartCad, []cad.Problem, error) {
	fp.Tidy()
	return s.keep(ctx, ref, pkg, library.CadFootprint, func(p library.Part, it *library.CadItem) ([]cad.Problem, error) {
		if err := cadlib.State(it.State).To(cadlib.Edited, p.MPN+" footprint"); err != nil {
			return nil, err
		}
		m := s.current(ctx, p, it.Package)
		m.Footprint = &fp
		ps := runChecks(footprintChecks, p, it.Package, m)
		if err := cad.Err(ps); err != nil {
			return ps, err
		}
		it.Raw, it.State, it.Source = editedRaw(it.Raw, fp), string(cadlib.Edited), source
		if notes != "" {
			it.Notes = notes
		}
		return ps, nil
	})
}

// EditModel3D keeps a footprint's 3D model as edited shapes, whatever
// state the footprint itself is in.
func (s *Service) EditModel3D(ctx context.Context, ref, pkg string, shapes []cad.Shape3D, notes string) (PartCad, []cad.Problem, error) {
	if len(shapes) == 0 {
		return PartCad{}, nil, fmt.Errorf("a 3D model needs at least one shape")
	}
	if _, err := cad.SolidsOf(shapes); err != nil {
		return PartCad{}, nil, err
	}
	return s.keep(ctx, ref, pkg, library.CadFootprint, func(p library.Part, it *library.CadItem) ([]cad.Problem, error) {
		b, _ := json.Marshal(shapes)
		if it.Raw == nil {
			it.Raw = map[string]string{}
		}
		it.Raw[cadlib.Model3DKey] = string(b)
		// A model the user put in the KiCad library by hand gives way.
		delete(it.Written, "kicad.step.user")
		if it.State == "" {
			it.State = string(cadlib.Auto)
		}
		// The footprint's notes stay (where its pads came from); the
		// model's follow them, in place of an earlier model's.
		if notes != "" {
			kept := it.Notes
			if i := strings.Index(kept, model3DNotes); i == 0 || i > 0 && kept[i-1] == '\n' {
				kept = kept[:i]
			}
			it.Notes = strings.TrimSpace(strings.TrimSpace(kept) + "\n" + model3DNotes + notes)
		}
		return nil, nil
	})
}

// model3DNotes starts a 3D model's notes, on a line after its footprint's.
const model3DNotes = "3D model: "

// ResetModel3D drops a 3D model's edit: it's built from the package again.
func (s *Service) ResetModel3D(ctx context.Context, ref, pkg string) (PartCad, error) {
	pc, _, err := s.keep(ctx, ref, pkg, library.CadFootprint, func(p library.Part, it *library.CadItem) ([]cad.Problem, error) {
		if _, ok := it.Raw[cadlib.Model3DKey]; !ok {
			return nil, fmt.Errorf("%s's 3D model wasn't edited", p.MPN)
		}
		delete(it.Raw, cadlib.Model3DKey)
		return nil, nil
	})
	return pc, err
}
