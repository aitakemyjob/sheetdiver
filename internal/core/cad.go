package core

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/aitakemyjob/sheetdiver/internal/cad"
	"github.com/aitakemyjob/sheetdiver/internal/cad/kicad"
	"github.com/aitakemyjob/sheetdiver/internal/cad/preview"
	"github.com/aitakemyjob/sheetdiver/internal/cadlib"
	"github.com/aitakemyjob/sheetdiver/internal/library"
	"github.com/aitakemyjob/sheetdiver/internal/pinrole"
)

// The user's CAD library: each part's schematic symbol, footprint and 3D
// model, kept in a folder in the formats the user chose. SheetDiver draws
// them from the pin table and the package (a standard one, or the
// dimensions the agent read from the datasheet), the agent redraws them
// as the datasheet says, and the user edits them in their CAD tool:
// every sync reads those edits back first, so they're kept.

// CadLibrary is where and how the user's CAD library is kept.
type CadLibrary struct {
	Dir     string   `json:"dir"`
	Name    string   `json:"name"`
	Formats []string `json:"formats"`
	// Available are the formats it can be kept in.
	Available []cadlib.FormatInfo `json:"available"`
	KiCad     cadlib.KiCadPaths   `json:"kicad"`
	Eagle     string              `json:"eagle"`
	// Altium is the folder of the Altium libraries (.SchLib, .PcbLib).
	Altium string `json:"altium"`
}

// CadLibrary is the library's settings, with their defaults.
func (s *Service) CadLibrary() CadLibrary {
	st := s.Config.Get().Settings
	l := CadLibrary{Dir: st.CadLibraryDir, Name: kicad.SafeName(st.CadLibraryName), Formats: st.CadFormats, Available: cadlib.Formats()}
	if l.Dir == "" {
		l.Dir = filepath.Join(s.Lib.Dir, "cad-library")
	}
	if l.Name == "" {
		l.Name = "SheetDiver"
	}
	switch {
	case len(l.Formats) == 0:
		l.Formats = []string{"kicad"}
	case len(l.Formats) == 1 && l.Formats[0] == "none":
		l.Formats = nil
	}
	l.KiCad = cadlib.KiCad(&cadlib.Library{Dir: l.Dir, Name: l.Name})
	l.Eagle = filepath.Join(l.Dir, "eagle", l.Name+".lbr")
	l.Altium = filepath.Join(l.Dir, "altium")
	return l
}

// CadView is a symbol or footprint as the app and agents see it.
type CadView struct {
	Name string `json:"name"`
	// State is auto (made by SheetDiver), drawn (by the agent) or edited
	// (by the user in a CAD tool).
	State    string        `json:"state"`
	Source   string        `json:"source,omitempty"`
	Notes    string        `json:"notes,omitempty"`
	Updated  string        `json:"updated,omitempty"`
	Problems []cad.Problem `json:"problems,omitempty"`
	// Drawing is what it looks like.
	Drawing cad.Drawing `json:"drawing"`
	// Symbol and its spec, for a symbol.
	Symbol     *cad.Symbol     `json:"symbol,omitempty"`
	SymbolSpec *cad.SymbolSpec `json:"symbolSpec,omitempty"`
	// Footprint, the package it's drawn from and its 3D model, for a
	// footprint.
	Footprint *cad.Footprint `json:"footprint,omitempty"`
	Package   *cad.Package   `json:"package,omitempty"`
	Model     *cad.Drawing   `json:"model,omitempty"`
	// Standard says the package is a standard outline, not the
	// datasheet's own dimensions.
	Standard bool `json:"standard,omitempty"`
	// ModelKept says the user replaced the 3D model with their own.
	ModelKept bool `json:"modelKept,omitempty"`
}

// CadPackage is a part's symbol and footprint for one of its packages.
type CadPackage struct {
	Package   string   `json:"package"`
	Pins      int      `json:"pins"`
	Symbol    *CadView `json:"symbol,omitempty"`
	Footprint *CadView `json:"footprint,omitempty"`
	// Match is what's wrong between the symbol's pins and the pads.
	Match []cad.Problem `json:"match,omitempty"`
	// Missing says why there is no footprint yet.
	Missing string `json:"missing,omitempty"`
}

// PartCad is a part's CAD data and the library it's kept in.
type PartCad struct {
	Part     library.Part `json:"part"`
	Packages []CadPackage `json:"packages"`
	Library  CadLibrary   `json:"library"`
}

// cadPkg is a package of a part and its pins.
type cadPkg struct {
	name string
	pins []library.Pin
}

// cadPackages are a part's packages with their pins: the pin table's
// packages, else the part's (or its first variant's) package.
func (s *Service) cadPackages(ctx context.Context, p library.Part, stored []library.CadItem) ([]cadPkg, error) {
	var pins []library.Pin
	if p.DatasheetID != 0 {
		var err error
		if pins, err = s.Lib.Pins(ctx, p.DatasheetID, ""); err != nil {
			return nil, err
		}
	}
	var names []string
	seen := map[string]bool{}
	for _, pin := range pins {
		if k := strings.ToUpper(pin.Package); pin.Package != "" && !seen[k] {
			seen[k] = true
			names = append(names, pin.Package)
		}
	}
	if len(names) == 0 {
		name := p.Package
		if name == "" {
			vs, _ := s.Lib.Variants(ctx, p.ID)
			for _, v := range vs {
				if v.Package != "" {
					name = v.Package
					break
				}
			}
		}
		names = []string{name}
	}
	// A part whose package nobody named takes the one the agent drew
	// it in; any other package the agent drew stays besides the pin
	// table's.
	if len(names) == 1 && names[0] == "" {
		for _, it := range stored {
			if it.Package != "" {
				names[0] = it.Package
				seen[strings.ToUpper(it.Package)] = true
				break
			}
		}
	}
	for _, it := range stored {
		if k := strings.ToUpper(it.Package); !seen[k] && !strings.EqualFold(it.Package, names[0]) {
			seen[k] = true
			names = append(names, it.Package)
		}
	}
	var out []cadPkg
	for _, n := range names {
		var mine []library.Pin
		for _, pin := range pins {
			if pin.Package == "" || strings.EqualFold(pin.Package, n) {
				mine = append(mine, pin)
			}
		}
		out = append(out, cadPkg{name: n, pins: mine})
	}
	return out, nil
}

// cadMeta is what a library item is stored with.
type cadMeta struct {
	partID int64
	pkg    string
	spec   json.RawMessage
	notes  string
	source string
}

// cadPart is a part's items for the library and its views for people.
type cadPart struct {
	items    []*cadlib.Item
	meta     map[*cadlib.Item]cadMeta
	packages []CadPackage
	// retired are stored items now kept under their package's name.
	retired []int64
}

// timeOf is a stored time, else now.
func timeOf(s string) time.Time {
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t
	}
	return time.Now().UTC()
}

// cadOfPart draws a part's symbols and footprints: from the user's edit
// when they made one, else from the agent's spec or SheetDiver's own.
func (s *Service) cadOfPart(ctx context.Context, p library.Part) (cadPart, error) {
	stored, err := s.Lib.CadItems(ctx, p.ID)
	if err != nil {
		return cadPart{}, err
	}
	pkgs, err := s.cadPackages(ctx, p, stored)
	if err != nil {
		return cadPart{}, err
	}
	var url string
	if p.DatasheetID != 0 {
		if d, err := s.Lib.Datasheet(ctx, p.DatasheetID); err == nil {
			url = d.SourceURL
		}
	}
	out := cadPart{meta: map[*cadlib.Item]cadMeta{}}
	find := func(kind, pkg string) (library.CadItem, bool) {
		for _, it := range stored {
			if it.Kind == kind && strings.EqualFold(it.Package, pkg) {
				return it, true
			}
		}
		// What was kept before the package had a name carries over to it.
		for _, it := range stored {
			if it.Kind == kind && it.Package == "" && pkg != "" {
				out.retired = append(out.retired, it.ID)
				it.ID = 0
				return it, true
			}
		}
		return library.CadItem{}, false
	}
	for _, pk := range pkgs {
		view := CadPackage{Package: pk.name, Pins: len(pk.pins)}
		key := p.MPN + "|" + strings.ToUpper(pk.name)
		var infos []cad.PinInfo
		epNum, nonPad := "", 0
		for _, pin := range pk.pins {
			infos = append(infos, cad.PinInfo{Number: pin.Number, Name: pin.Name, Type: pin.Type, Description: pin.Description})
			if pinrole.Of(pin.Name, pin.Type, pin.Description) == pinrole.Pad {
				epNum = pin.Number
			} else {
				nonPad++
			}
		}
		symName := kicad.SafeName(p.MPN)
		if len(pkgs) > 1 {
			symName += "_" + kicad.SafeName(pk.name)
		}

		// The symbol.
		sym := &cadlib.Item{Kind: library.CadSymbol, Name: symName, Key: key, State: cadlib.Auto, Updated: p.UpdatedAt}
		sv := &CadView{Name: symName, State: string(cadlib.Auto), Source: library.SourceAuto}
		meta := cadMeta{partID: p.ID, pkg: pk.name, source: library.SourceAuto}
		var spec cad.SymbolSpec
		if st, ok := find(library.CadSymbol, pk.name); ok {
			_ = json.Unmarshal(st.Spec, &spec)
			sym.State, sym.Raw, sym.Written, sym.Updated = cadlib.State(st.State), st.Raw, st.Written, timeOf(st.Updated)
			sv.State, sv.Source, sv.Notes, sv.Updated = st.State, st.Source, st.Notes, st.Updated
			meta.spec, meta.notes, meta.source = st.Spec, st.Notes, st.Source
		}
		if raw, ok := sym.Raw["kicad"]; ok && sym.State == cadlib.Edited {
			if nd, err := kicad.Parse(raw); err == nil {
				got := kicad.ReadSymbol(nd)
				got.Name = symName
				sym.Symbol = &got
			}
		}
		if sym.Symbol == nil && len(infos) > 0 {
			got, ps := cad.MakeSymbol(symName, infos, spec)
			sv.Problems = append(sv.Problems, ps...)
			if cad.Err(ps) == nil {
				got.Value, got.Description, got.Datasheet = p.MPN, p.Description, url
				sym.Symbol = &got
			}
		}
		if sym.Symbol != nil {
			sv.Symbol, sv.Drawing = sym.Symbol, cad.DrawSymbol(*sym.Symbol)
			if len(spec.Pins) > 0 || spec.Reference != "" {
				sv.SymbolSpec = &spec
			}
			out.items = append(out.items, sym)
			out.meta[sym] = meta
			view.Symbol = sv
		} else if len(sv.Problems) > 0 {
			view.Symbol = sv
		}

		// The footprint and its 3D model.
		fpi := &cadlib.Item{Kind: library.CadFootprint, Key: key, State: cadlib.Auto, Updated: p.UpdatedAt}
		fv := &CadView{State: string(cadlib.Auto), Source: library.SourceAuto}
		fmeta := cadMeta{partID: p.ID, pkg: pk.name, source: library.SourceAuto}
		var pkgSpec *cad.Package
		if st, ok := find(library.CadFootprint, pk.name); ok {
			fpi.State, fpi.Raw, fpi.Written, fpi.Updated = cadlib.State(st.State), st.Raw, st.Written, timeOf(st.Updated)
			fv.State, fv.Source, fv.Notes, fv.Updated = st.State, st.Source, st.Notes, st.Updated
			fmeta.spec, fmeta.notes, fmeta.source = st.Spec, st.Notes, st.Source
			if len(st.Spec) > 0 {
				var sp cad.Package
				if json.Unmarshal(st.Spec, &sp) == nil && sp.Family != "" {
					pkgSpec = &sp
				}
			}
		}
		if pkgSpec == nil && fpi.State == cadlib.Auto {
			if sp, ok := cad.Standard(pk.name, nonPad); ok {
				pkgSpec, fv.Standard = &sp, true
			}
		}
		if pkgSpec != nil && pkgSpec.ExposedPad != nil && pkgSpec.ExposedPadNumber == "" {
			pkgSpec.ExposedPadNumber = epNum
		}
		name := pk.name
		if name == "" && pkgSpec != nil {
			name = pkgSpec.Name
			if name == "" {
				name = strings.ToUpper(pkgSpec.Family)
			}
		}
		fpi.Name = kicad.SafeName(name) + "_" + kicad.SafeName(p.MPN)
		if name == "" {
			fpi.Name = kicad.SafeName(p.MPN)
		}
		fv.Name = fpi.Name
		if raw, ok := fpi.Raw["kicad"]; ok && fpi.State == cadlib.Edited {
			if nd, err := kicad.Parse(raw); err == nil {
				got := kicad.ReadFootprint(nd)
				got.Name, got.From = fpi.Name, "your edit in KiCad"
				fpi.Footprint = &got
			}
		}
		if pkgSpec != nil {
			if fpi.Footprint == nil {
				got, ps := cad.MakeFootprint(fpi.Name, *pkgSpec)
				fv.Problems = append(fv.Problems, ps...)
				if cad.Err(ps) == nil {
					fpi.Footprint = &got
				}
			}
			solids, ps := cad.MakeSolids(*pkgSpec)
			fv.Problems = append(fv.Problems, ps...)
			fpi.Solids = solids
			fv.Package = pkgSpec
			if len(solids) > 0 {
				m := cad.Draw3D(solids)
				fv.Model = &m
			}
		}
		fv.ModelKept = fpi.Written["kicad.step.user"] != ""
		switch {
		case fpi.Footprint != nil:
			fv.Footprint, fv.Drawing = fpi.Footprint, cad.DrawFootprint(*fpi.Footprint)
			out.items = append(out.items, fpi)
			out.meta[fpi] = fmeta
			view.Footprint = fv
		case len(fv.Problems) > 0:
			view.Footprint = fv
		default:
			view.Missing = fmt.Sprintf("no dimensions for package %q yet: draw it from the datasheet's package drawing (draw_footprint)", pk.name)
			if pk.name == "" {
				view.Missing = "the part's package isn't known: say which it is and draw it (draw_footprint)"
			}
		}
		if sym.Symbol != nil && fpi.Footprint != nil {
			view.Match = cad.Match(*sym.Symbol, *fpi.Footprint)
		}
		out.packages = append(out.packages, view)
	}
	return out, nil
}

// PartCad is a part's symbols, footprints and 3D models, per package.
func (s *Service) PartCad(ctx context.Context, ref string) (PartCad, error) {
	r, err := s.Lib.Resolve(ctx, ref)
	if err != nil {
		return PartCad{}, err
	}
	cp, err := s.cadOfPart(ctx, r.Part)
	if err != nil {
		return PartCad{}, err
	}
	return PartCad{Part: r.Part, Packages: cp.packages, Library: s.CadLibrary()}, nil
}

// SyncCad brings the user's CAD library up to date: their edits in it
// are read back and kept, then every part's symbol, footprint and model
// is written in each format they keep.
func (s *Service) SyncCad(ctx context.Context) (cadlib.Report, error) {
	s.cadMu.Lock()
	defer s.cadMu.Unlock()
	return s.syncCad(ctx)
}

// syncCad syncs; the caller holds cadMu.
func (s *Service) syncCad(ctx context.Context) (cadlib.Report, error) {
	settings := s.CadLibrary()
	if len(settings.Formats) == 0 {
		return cadlib.Report{Dir: settings.Dir}, nil
	}
	parts, err := s.Lib.Parts(ctx, library.PartFilter{})
	if err != nil {
		return cadlib.Report{}, err
	}
	lib := &cadlib.Library{Dir: settings.Dir, Name: settings.Name}
	meta := map[*cadlib.Item]cadMeta{}
	var retired []int64
	for _, p := range parts {
		cp, err := s.cadOfPart(ctx, p)
		if err != nil {
			return cadlib.Report{}, err
		}
		lib.Items = append(lib.Items, cp.items...)
		for k, v := range cp.meta {
			meta[k] = v
			if len(cp.retired) > 0 {
				k.Changed = true // saved under the package's name
			}
		}
		retired = append(retired, cp.retired...)
	}
	rep, err := cadlib.Sync(lib, settings.Formats)
	// Whatever was written or adopted is saved, even when a later format
	// failed: the files say so now.
	for _, it := range lib.Items {
		if !it.Changed {
			continue
		}
		m := meta[it]
		if it.State == cadlib.Edited {
			m.source = library.SourceUser
		}
		if _, serr := s.Lib.SaveCadItem(ctx, library.CadItem{PartID: m.partID, Kind: it.Kind, Package: m.pkg, Spec: m.spec, State: string(it.State),
			Raw: it.Raw, Written: it.Written, Notes: m.notes, Source: m.source}); serr != nil && err == nil {
			err = serr
		}
	}
	if err == nil {
		for _, id := range retired {
			_ = s.Lib.DeleteCadItem(ctx, id)
		}
	}
	s.emit(EvLibrary, map[string]any{"action": "cad"})
	return rep, err
}

// cadItem is a part's stored item of a kind for a package, with the
// package resolved: the part's only package when none is named.
func (s *Service) cadItem(ctx context.Context, ref, pkg, kind string) (library.Part, library.CadItem, error) {
	r, err := s.Lib.Resolve(ctx, ref)
	if err != nil {
		return library.Part{}, library.CadItem{}, err
	}
	stored, err := s.Lib.CadItems(ctx, r.Part.ID)
	if err != nil {
		return r.Part, library.CadItem{}, err
	}
	pkgs, err := s.cadPackages(ctx, r.Part, stored)
	if err != nil {
		return r.Part, library.CadItem{}, err
	}
	pkg = strings.TrimSpace(pkg)
	if pkg == "" {
		if len(pkgs) > 1 {
			var names []string
			for _, p := range pkgs {
				names = append(names, p.name)
			}
			return r.Part, library.CadItem{}, fmt.Errorf("%s comes in %s: say which package", r.Part.MPN, strings.Join(names, ", "))
		}
		pkg = pkgs[0].name
	} else {
		for _, p := range pkgs {
			if strings.EqualFold(p.name, pkg) {
				pkg = p.name
			}
		}
	}
	it, err := s.Lib.CadItem(ctx, r.Part.ID, kind, pkg)
	if errors.Is(err, library.ErrNotFound) {
		return r.Part, library.CadItem{PartID: r.Part.ID, Kind: kind, Package: pkg, State: string(cadlib.Auto)}, nil
	}
	return r.Part, it, err
}

// redraw saves an agent's drawing of an item, unless the user edited it,
// and syncs the library before (to notice an edit) and after.
func (s *Service) redraw(ctx context.Context, ref, pkg, kind string, spec any, notes string, check func(library.Part, string) []cad.Problem) (PartCad, []cad.Problem, error) {
	s.cadMu.Lock()
	defer s.cadMu.Unlock()
	if _, err := s.syncCad(ctx); err != nil {
		return PartCad{}, nil, err
	}
	p, it, err := s.cadItem(ctx, ref, pkg, kind)
	if err != nil {
		return PartCad{}, nil, err
	}
	if err := cadlib.State(it.State).To(cadlib.Drawn, p.MPN+" "+kind); err != nil {
		return PartCad{}, nil, err
	}
	ps := check(p, it.Package)
	if err := cad.Err(ps); err != nil {
		return PartCad{}, ps, err
	}
	b, _ := json.Marshal(spec)
	it.Spec, it.State, it.Source, it.Notes = b, string(cadlib.Drawn), library.SourceAgent, notes
	if _, err := s.Lib.SaveCadItem(ctx, it); err != nil {
		return PartCad{}, nil, err
	}
	if _, err := s.syncCad(ctx); err != nil {
		return PartCad{}, ps, err
	}
	pc, err := s.PartCad(ctx, p.MPN)
	return pc, ps, err
}

// DrawSymbol saves the agent's drawing of a part's symbol: where each pin
// goes and what it is.
func (s *Service) DrawSymbol(ctx context.Context, ref, pkg string, spec cad.SymbolSpec, notes string) (PartCad, []cad.Problem, error) {
	return s.redraw(ctx, ref, pkg, library.CadSymbol, spec, notes, func(p library.Part, pkg string) []cad.Problem {
		pins, _ := s.Lib.Pins(ctx, p.DatasheetID, "")
		var infos []cad.PinInfo
		for _, pin := range pins {
			if pin.Package == "" || strings.EqualFold(pin.Package, pkg) {
				infos = append(infos, cad.PinInfo{Number: pin.Number, Name: pin.Name, Type: pin.Type, Description: pin.Description})
			}
		}
		_, ps := cad.MakeSymbol(p.MPN, infos, spec)
		return ps
	})
}

// DrawFootprint saves the package dimensions and land pattern the agent
// read from the datasheet, which the footprint and 3D model are drawn
// from.
func (s *Service) DrawFootprint(ctx context.Context, ref, pkg string, spec cad.Package, notes string) (PartCad, []cad.Problem, error) {
	return s.redraw(ctx, ref, pkg, library.CadFootprint, spec, notes, func(p library.Part, pkg string) []cad.Problem {
		_, ps := cad.MakeFootprint(p.MPN, spec)
		_, more := cad.MakeSolids(spec)
		return append(ps, more...)
	})
}

// ResetCad drops the newest work on a symbol or footprint: the user's
// edit (back to the agent's drawing, or SheetDiver's), or the agent's
// drawing (back to SheetDiver's).
func (s *Service) ResetCad(ctx context.Context, ref, pkg, kind string) (PartCad, error) {
	s.cadMu.Lock()
	defer s.cadMu.Unlock()
	if kind != library.CadSymbol && kind != library.CadFootprint {
		return PartCad{}, fmt.Errorf("kind %q isn't symbol or footprint", kind)
	}
	if _, err := s.syncCad(ctx); err != nil {
		return PartCad{}, err
	}
	p, it, err := s.cadItem(ctx, ref, pkg, kind)
	if err != nil {
		return PartCad{}, err
	}
	st := cadlib.State(it.State)
	to := st.Reset(len(it.Spec) > 0 && st == cadlib.Edited)
	if st == cadlib.Edited {
		delete(it.Raw, "kicad")
		delete(it.Written, "kicad.step.user")
	}
	if to == cadlib.Auto {
		it.Spec, it.Source = nil, library.SourceAuto
	}
	it.State = string(to)
	if it.ID != 0 {
		if _, err := s.Lib.SaveCadItem(ctx, it); err != nil {
			return PartCad{}, err
		}
	}
	if _, err := s.syncCad(ctx); err != nil {
		return PartCad{}, err
	}
	return s.PartCad(ctx, p.MPN)
}

// CadImage is a picture of a part's symbol, footprint or 3D model (what
// is symbol, footprint or 3d), for an agent to check its drawing.
func (s *Service) CadImage(ctx context.Context, ref, pkg, what string) ([]byte, CadPackage, error) {
	pc, err := s.PartCad(ctx, ref)
	if err != nil {
		return nil, CadPackage{}, err
	}
	var cp *CadPackage
	for i := range pc.Packages {
		if pkg == "" || strings.EqualFold(pc.Packages[i].Package, pkg) {
			cp = &pc.Packages[i]
			break
		}
	}
	if cp == nil {
		return nil, CadPackage{}, fmt.Errorf("%s has no package %q", pc.Part.MPN, pkg)
	}
	var d *cad.Drawing
	title := pc.Part.MPN + " " + cp.Package + " "
	switch what {
	case "symbol":
		if cp.Symbol != nil && cp.Symbol.Symbol != nil {
			d = &cp.Symbol.Drawing
		}
	case "footprint":
		if cp.Footprint != nil && cp.Footprint.Footprint != nil {
			d = &cp.Footprint.Drawing
		}
	case "3d":
		if cp.Footprint != nil {
			d = cp.Footprint.Model
		}
	default:
		return nil, *cp, fmt.Errorf("what %q isn't symbol, footprint or 3d", what)
	}
	if d == nil {
		return nil, *cp, fmt.Errorf("%s has no %s for %q yet", pc.Part.MPN, what, cp.Package)
	}
	png, err := s.Engine.Render(preview.PDF(*d, title+what, 560), 0, nil, 110)
	return png, *cp, err
}

// CadParts are the parts in the CAD library and the state of each one's
// symbol and footprint, for the library overview.
func (s *Service) CadParts(ctx context.Context) ([]library.CadItem, error) {
	items, err := s.Lib.CadItems(ctx, 0)
	if err != nil {
		return nil, err
	}
	sort.SliceStable(items, func(i, j int) bool { return items[i].MPN < items[j].MPN })
	for i := range items {
		items[i].Raw, items[i].Written, items[i].Spec = nil, nil, nil
	}
	return items, nil
}
