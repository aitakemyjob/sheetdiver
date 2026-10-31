package core

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strconv"
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
	// DatasheetPage is the page of the datasheet's package drawing its
	// dimensions came from (1-based), when they did.
	DatasheetPage int `json:"datasheetPage,omitempty"`
	// ModelKept says the user replaced the 3D model with their own.
	ModelKept bool `json:"modelKept,omitempty"`
	// EditedIn is where an edit was made: kicad, altium, eagle, or
	// sheetdiver (the app's editor or an agent's edit_ tools).
	EditedIn string `json:"editedIn,omitempty"`
	// Shapes are the 3D model as shapes to edit; Model3DEdited says they
	// were edited rather than built from the package.
	Shapes        []cad.Shape3D `json:"shapes,omitempty"`
	Model3DEdited bool          `json:"model3dEdited,omitempty"`
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
	// PinsFrom is the package whose pin table this one's symbol and
	// pads take, when the datasheet draws it but gives it no pins.
	PinsFrom string `json:"pinsFrom,omitempty"`
}

// PartCad is a part's CAD data and the library it's kept in.
type PartCad struct {
	Part     library.Part `json:"part"`
	Packages []CadPackage `json:"packages"`
	Library  CadLibrary   `json:"library"`
}

// cadPkg is a package of a part and its pins, and the package they're
// taken from when it has none of its own.
type cadPkg struct {
	name     string
	pins     []library.Pin
	pinsFrom string
}

// cadPackages are a part's packages with their pins: the pin table's
// packages, else the part's (or its first variant's) package; then the
// other packages its datasheet draws with as many pins.
func (s *Service) cadPackages(ctx context.Context, p library.Part, stored []library.CadItem, own ownPkgs) ([]cadPkg, error) {
	pins, err := s.PartPins(ctx, p, "")
	if err != nil {
		return nil, err
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
	// A part whose package nobody named, and no agent drew, takes its datasheet's, when the
	// datasheet draws only one.
	if len(names) == 1 && names[0] == "" {
		if pkgs, err := s.partPackages(ctx, p); err == nil && len(pkgs) == 1 {
			names[0] = pkgs[0].Name
			seen[strings.ToUpper(names[0])] = true
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
	return append(out, drawnPackages(own, out, seen)...), nil
}

// ownPkgs are the packages a part comes in (ownPackages), looked up once
// for everything drawing the part.
type ownPkgs struct {
	mine    []PartPackage
	claimed bool
}

// drawnPackages are the packages a part comes in by its datasheet's
// ordering scheme (the STM32U545CE's LQFP48 and UFQFPN48, not its
// siblings' LQFP100) besides those of its pin table. One the pin table
// gives no pins takes those of another of the part's packages with as
// many, when both are numbered round their edges (1 to N); a ball
// grid's are its own.
func drawnPackages(own ownPkgs, have []cadPkg, seen map[string]bool) []cadPkg {
	if !own.claimed {
		return nil
	}
	var out []cadPkg
	for _, dp := range own.mine {
		// A pin table's package covers its outline: the WLCSP72 is drawn
		// as the "WLCSP72 SMPS" the pin table names.
		if seen[strings.ToUpper(dp.Name)] || slices.ContainsFunc(have, func(h cadPkg) bool { return packageMatches(h.name, dp.Name) }) {
			continue
		}
		seen[strings.ToUpper(dp.Name)] = true
		pk := cadPkg{name: dp.Name}
		n := namedPins(dp)
		for i := range have {
			if c := pinCount(have[i].pins, have[i].name, true); len(have[i].pins) > 0 && (c == n || c == n+1 && hasPad(have[i].pins)) &&
				edgeNumbered(have[i].name) && edgeNumbered(dp.Name) {
				pk.pins, pk.pinsFrom = have[i].pins, have[i].name
				break
			}
		}
		out = append(out, pk)
	}
	return out
}

// gridOf lays a ball grid package's balls out from its pin table's ball
// names (A1 to M12, those not named left out), when that grid fits its
// body at its pitch. A staggered map (a WLCSP's, its columns half the
// pitch apart and its rows closer) is spread over the spans the drawing
// gives its balls instead (staggered).
func gridOf(sp *cad.Package, pp PartPackage, pins []library.Pin, problems *[]cad.Problem) {
	var names []string
	for _, p := range pins {
		if p.Number != "" {
			names = append(names, p.Number)
		}
	}
	rows, cols, missing, ok := cad.BallGrid(names)
	if !ok || sp.Pitch <= 0 {
		return
	}
	if float64(cols-1)*sp.Pitch > sp.BodyWidth.Mid() || float64(rows-1)*sp.Pitch > sp.BodyLength.Mid() {
		if !staggered(sp, pp, names, rows, cols) {
			*problems = append(*problems, cad.Problem{Level: "warning", Text: fmt.Sprintf("its balls (%d rows, %d columns at %g mm) don't fit a grid on its %g mm body, and the drawing's ball spans don't place them a pitch apart: draw it ball by ball (draw_footprint)", rows, cols, sp.Pitch, sp.BodyWidth.Mid())})
		}
		return
	}
	sp.Rows, sp.Columns, sp.Depopulated = rows, cols, missing
}

// staggered places a staggered ball map: its columns spread evenly over
// one of the spans the drawing gives (e1, e2: first ball to last) and its
// rows over the other, whichever way puts the nearest balls a pitch
// apart; without spans, columns half the pitch apart and rows the
// height of a triangle of them (hexagonal packing).
func staggered(sp *cad.Package, pp PartPackage, names []string, rows, cols int) bool {
	if rows < 2 || cols < 2 {
		return false
	}
	spans := [][2]float64{{float64(cols-1) * sp.Pitch / 2, float64(rows-1) * sp.Pitch * math.Sqrt(3) / 2}}
	e1, ok1 := symbolDim(pp.Dims, "e1")
	e2, ok2 := symbolDim(pp.Dims, "e2")
	if ok1 && ok2 {
		spans = [][2]float64{{e1, e2}, {e2, e1}}
	}
	place := func(w, h float64) []cad.PadSpec {
		var out []cad.PadSpec
		for _, n := range names {
			r, c, ok := cad.BallAt(n)
			if !ok {
				return nil
			}
			out = append(out, cad.PadSpec{Number: n, X: cad.Number(-w/2 + float64(c)*w/float64(cols-1)), Y: cad.Number(-h/2 + float64(r)*h/float64(rows-1))})
		}
		return out
	}
	var best []cad.PadSpec
	bestOff := math.Inf(1)
	for _, s := range spans {
		balls := place(s[0], s[1])
		near := math.Inf(1)
		for i := range balls {
			for j := i + 1; j < len(balls); j++ {
				near = math.Min(near, math.Hypot(balls[i].X-balls[j].X, balls[i].Y-balls[j].Y))
			}
		}
		if off := math.Abs(near - sp.Pitch); off < bestOff {
			best, bestOff = balls, off
		}
	}
	// Nearest balls a pitch apart (to 5%), or it isn't this layout.
	if best == nil || bestOff > 0.05*sp.Pitch {
		return false
	}
	sp.Pads, sp.Rows, sp.Columns = best, rows, cols
	return true
}

// symbolDim is a package's dimension of a symbol as printed (e1).
func symbolDim(ds []PackageDim, symbol string) (float64, bool) {
	for _, d := range ds {
		if d.Symbol == symbol {
			if v := (cad.Dim{Min: deref(d.Min), Nom: deref(d.Nom), Max: deref(d.Max)}).Mid(); v > 0 {
				return v, true
			}
		}
	}
	return 0, false
}

// deref is a value, or 0.
func deref(v *float64) float64 {
	if v == nil {
		return 0
	}
	return *v
}

// ownPkgsOf looks a part's packages up once.
func (s *Service) ownPkgsOf(ctx context.Context, p library.Part) (ownPkgs, error) {
	mine, claimed, err := s.ownPackages(ctx, p)
	return ownPkgs{mine: mine, claimed: claimed}, err
}

// padNumberRe is an exposed pad's number in a pin table.
var padNumberRe = regexp.MustCompile(`(?i)^(E?PAD|EP|TP|PowerPAD|thermal pad)$`)

// ownStored are a part's stored items but SheetDiver's own for packages
// its ordering scheme says it doesn't come in (made before the scheme
// was read: a family's 100-pin part given its 48-pin sibling's LQFP48),
// which are stale. What an agent drew or the user edited is kept.
func ownStored(own ownPkgs, stored []library.CadItem) ([]library.CadItem, []int64) {
	if !own.claimed {
		return stored, nil
	}
	mine := own.mine
	var kept []library.CadItem
	var stale []int64
	for _, it := range stored {
		if it.Package != "" && it.State == string(cadlib.Auto) && it.Source == library.SourceAuto &&
			!slices.ContainsFunc(mine, func(pp PartPackage) bool { return packageMatches(pp.Name, it.Package) }) {
			stale = append(stale, it.ID)
			continue
		}
		kept = append(kept, it)
	}
	return kept, stale
}

// namedPins is how many pins a datasheet package has: its N, else the
// count in its name (LQFP48, QFN-16), but not a name's number that isn't
// one (SOT-23, TO-220).
func namedPins(pp PartPackage) int {
	if d, ok := dimOf(pp.Dims, "pins"); ok && d.Mid() > 0 {
		return int(d.Mid())
	}
	n := strings.ToUpper(strings.TrimSpace(pp.Name))
	if outlineNumberRe.MatchString(n) {
		return 0
	}
	if m := namedCountRe.FindStringSubmatch(n); m != nil {
		v, _ := strconv.Atoi(m[1])
		return v
	}
	return 0
}

var (
	// outlineNumberRe is a name whose number is its outline's, not a count.
	outlineNumberRe = regexp.MustCompile(`^(SOT|SC|TO|SOD)-?\d+[A-Z]*$`)
	// namedCountRe is the pin count at the end of a package's name.
	namedCountRe = regexp.MustCompile(`[A-Z]-?(\d{1,3})[A-Z]?$`)
)

// edgeNumbered reports whether a package's pins are numbered 1 to N round
// its edges, so two such packages of a count share a pin table's numbers.
func edgeNumbered(name string) bool {
	f := familyOf(name)
	return f != "" && f != "bga"
}

// hasPad reports whether a pin table has an exposed pad.
func hasPad(pins []library.Pin) bool {
	for _, p := range pins {
		if pinrole.Of(p.Name, p.Type, p.Description) == pinrole.Pad {
			return true
		}
	}
	return false
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
	own, err := s.ownPkgsOf(ctx, p)
	if err != nil {
		return cadPart{}, err
	}
	stored, stale := ownStored(own, stored)
	pkgs, err := s.cadPackages(ctx, p, stored, own)
	if err != nil {
		return cadPart{}, err
	}
	var url string
	if p.DatasheetID != 0 {
		if d, err := s.Lib.Datasheet(ctx, p.DatasheetID); err == nil {
			url = d.SourceURL
		}
	}
	dsPkgs, err := s.partPackages(ctx, p)
	if err != nil {
		return cadPart{}, err
	}
	out := cadPart{meta: map[*cadlib.Item]cadMeta{}, retired: stale}
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
		view := CadPackage{Package: pk.name, Pins: len(pk.pins), PinsFrom: pk.pinsFrom}
		key := p.MPN + "|" + strings.ToUpper(pk.name)
		var infos []cad.PinInfo
		epNum, nonPad := "", 0
		for _, pin := range pk.pins {
			infos = append(infos, cad.PinInfo{Number: pin.Number, Name: pin.Name, Type: pin.Type, Description: pin.Description})
			// The exposed pad: by its name, or its number when it's named
			// for its net (TI's "PAD" row, GND).
			if pinrole.Of(pin.Name, pin.Type, pin.Description) == pinrole.Pad || padNumberRe.MatchString(pin.Number) {
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
		sv.EditedIn = cadlib.EditedIn(sym.Raw)
		if sym.State == cadlib.Edited {
			if got, _ := cadlib.Decode(library.CadSymbol, sym.Raw); got != nil {
				got.Name = symName
				sym.Symbol = got
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
		// A two-terminal with no pin table (a resistor's datasheet has
		// none): its standard symbol, pins 1 and 2.
		if sym.Symbol == nil && len(infos) == 0 && library.TwoTerminal(p.Category) {
			if got, ok := cad.PassiveSymbol(symName, p.Category); ok {
				got.Value, got.Description, got.Datasheet = p.MPN, p.Description, url
				sym.Symbol = &got
				sv.Problems = append(sv.Problems, cad.Problem{Level: "warning", Text: fmt.Sprintf("the standard %s symbol, pins 1 and 2: the datasheet has no pin table", p.Category)})
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
		// The package's outline, which an edited footprint keeps for its
		// 3D model.
		if pkgSpec == nil {
			// The datasheet's own dimensions, when they make a footprint.
			if pp, ok := packageFor(dsPkgs, pk.name); ok {
				pads := nonPad
				// Without a pin table, a package numbered round its edges
				// still has its pads 1 to N; a ball grid's need its map.
				if len(pk.pins) == 0 && edgeNumbered(pk.name) {
					pads = namedPins(pp)
				}
				if sp, ok := datasheetPackage(pp, pk.name, pads); ok {
					if sp.Family == "bga" && sp.Rows == 0 {
						gridOf(&sp, pp, pk.pins, &fv.Problems)
					}
					if _, ps := cad.MakeFootprint("check", sp); cad.Err(ps) == nil {
						pkgSpec, fv.DatasheetPage = &sp, sp.Page
					}
				}
			}
		}
		if pkgSpec == nil {
			if sp, ok := cad.Standard(pk.name, nonPad); ok {
				pkgSpec, fv.Standard = &sp, true
			}
		}
		if fpi.State == cadlib.Edited {
			fv.Standard, fv.DatasheetPage = false, 0
		}
		fv.EditedIn = cadlib.EditedIn(fpi.Raw)
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
		if fpi.State == cadlib.Edited {
			if _, got := cadlib.Decode(library.CadFootprint, fpi.Raw); got != nil {
				got.Name = fpi.Name
				fpi.Footprint = got
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
			fpi.Solids = append(solids, cad.Marking(p.MPN, *pkgSpec)...)
			fv.Package = pkgSpec
		}
		// The 3D model as the user or an agent edited it.
		if raw, ok := fpi.Raw[cadlib.Model3DKey]; ok {
			var shapes []cad.Shape3D
			if json.Unmarshal([]byte(raw), &shapes) == nil {
				if solids, err := cad.SolidsOf(shapes); err == nil {
					fpi.Solids, fv.Model3DEdited = solids, true
				}
			}
		}
		if len(fpi.Solids) > 0 {
			m := cad.Draw3D(fpi.Solids)
			fv.Model, fv.Shapes = &m, cad.ShapesOf(fpi.Solids)
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
			view.Missing = fmt.Sprintf("no dimensions for package %q yet: read them (get_package) and record them (record_package), or draw it from the datasheet's package drawing (draw_footprint)", pk.name)
			if len(pk.pins) == 0 {
				switch {
				case edgeNumbered(pk.name):
					view.Missing = fmt.Sprintf("the datasheet draws package %q but its dimensions weren't read: record them (record_package) or draw it from its drawing (draw_footprint); its pin table wasn't read either (record_pins with package %q)", pk.name, pk.name)
				default:
					view.Missing = fmt.Sprintf("the datasheet draws package %q but its pin table wasn't read for it, and its balls can't be laid out without it: record its pins (record_pins with package %q)", pk.name, pk.name)
				}
			}
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
		if it.State == cadlib.Edited && cadlib.EditedIn(it.Raw) != cadlib.ModelKey {
			m.source = library.SourceUser // adopted from their CAD tool
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
	own, err := s.ownPkgsOf(ctx, r.Part)
	if err != nil {
		return r.Part, library.CadItem{}, err
	}
	stored, _ = ownStored(own, stored)
	pkgs, err := s.cadPackages(ctx, r.Part, stored, own)
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
		pins, _ := s.PartPins(ctx, p, "")
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
// drawing (back to SheetDiver's); or a 3D model's edit (kind 3d).
func (s *Service) ResetCad(ctx context.Context, ref, pkg, kind string) (PartCad, error) {
	if kind == "3d" || kind == "model" {
		return s.ResetModel3D(ctx, ref, pkg)
	}
	s.cadMu.Lock()
	defer s.cadMu.Unlock()
	if kind != library.CadSymbol && kind != library.CadFootprint {
		return PartCad{}, fmt.Errorf("kind %q isn't symbol, footprint or 3d", kind)
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
		// The edit goes; a 3D model's edit stays (it's reset by itself).
		m3d, ok := it.Raw[cadlib.Model3DKey]
		it.Raw = nil
		if ok {
			it.Raw = map[string]string{cadlib.Model3DKey: m3d}
		}
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
