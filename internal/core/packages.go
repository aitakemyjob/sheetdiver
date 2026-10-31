package core

import (
	"context"
	"fmt"
	"math"
	"regexp"
	"sort"
	"strings"

	"github.com/aitakemyjob/sheetdiver/internal/cad"
	"github.com/aitakemyjob/sheetdiver/internal/library"
)

// Package dimensions as a part's datasheet gives them: the extractor's
// reading of its dimension tables and land patterns, with whatever an
// agent or the user recorded winning dimension by dimension, checked for
// sense, and turned into the footprint's package where they are enough.

// PackageDim is a dimension with where its value came from.
type PackageDim struct {
	library.PackageDim
	Source string `json:"source"`
}

// PartPackage is one of a part's packages, its rows merged.
type PartPackage struct {
	Name       string       `json:"name"`
	Code       string       `json:"code,omitempty"`
	Reference  string       `json:"reference,omitempty"`
	Page       int          `json:"page"`
	Dims       []PackageDim `json:"dims"`
	Land       []PackageDim `json:"land"`
	Candidates []string     `json:"candidates,omitempty"`
	// Checks are what doesn't add up: the extractor's and SheetDiver's.
	Checks  []string `json:"checks,omitempty"`
	Notes   string   `json:"notes,omitempty"`
	Sources []string `json:"sources"`
	// Pins is how many pins the part's pin table gives the package.
	Pins int `json:"pins,omitempty"`
	// Parts are the datasheet's parts that come in it, by its ordering
	// scheme; none when it doesn't say.
	Parts []string `json:"parts,omitempty"`
	// Rows are the stored rows it's merged from, for deleting one.
	Rows []library.PackageDims `json:"rows"`
}

// PartPackages is a part's packages.
type PartPackages struct {
	Part     library.Part  `json:"part"`
	Packages []PartPackage `json:"packages"`
}

// PartPackages are the package dimensions of a part's datasheet.
func (s *Service) PartPackages(ctx context.Context, ref string) (PartPackages, error) {
	r, err := s.Lib.Resolve(ctx, ref)
	if err != nil {
		return PartPackages{}, err
	}
	pkgs, err := s.partPackages(ctx, r.Part)
	return PartPackages{Part: r.Part, Packages: pkgs}, err
}

// partPackages are the packages a part comes in, from its datasheet's
// rows merged by name, each checked against its pin count.
func (s *Service) partPackages(ctx context.Context, p library.Part) ([]PartPackage, error) {
	out, _, err := s.ownPackages(ctx, p)
	if err != nil {
		return nil, err
	}
	pins, err := s.PartPins(ctx, p, "")
	if err != nil {
		return nil, err
	}
	for i := range out {
		out[i].Pins = pinCount(pins, out[i].Name, len(out) == 1)
		out[i].Checks = append(out[i].Checks, checkPackage(out[i])...)
	}
	return out, nil
}

// ownPackages are the packages of a part's datasheet that it comes in:
// those its ordering scheme gives it (claimed), else, when the scheme
// gives it none or there is no scheme, all of them.
func (s *Service) ownPackages(ctx context.Context, p library.Part) ([]PartPackage, bool, error) {
	if p.DatasheetID == 0 {
		return []PartPackage{}, false, nil
	}
	rows, err := s.Lib.Packages(ctx, p.DatasheetID)
	if err != nil {
		return nil, false, err
	}
	all := mergePackageRows(rows)
	var mine []PartPackage
	for _, pp := range all {
		if containsFold(pp.Parts, p.MPN) {
			mine = append(mine, pp)
		}
	}
	if len(mine) > 0 {
		return mine, true, nil
	}
	if all == nil {
		all = []PartPackage{}
	}
	return all, false, nil
}

// mergePackageRows joins rows of the same package: recorded ones first,
// so their dimensions win; the extractor's fill in what they left out.
func mergePackageRows(rows []library.PackageDims) []PartPackage {
	sort.SliceStable(rows, func(i, j int) bool {
		return rows[i].Source != library.SourceAuto && rows[j].Source == library.SourceAuto
	})
	var out []PartPackage
	idx := map[string]int{}
	for _, r := range rows {
		key := cadNormal(r.Name)
		i, ok := idx[key]
		if !ok {
			idx[key] = len(out)
			out = append(out, PartPackage{Name: r.Name, Code: r.Code, Reference: r.Reference, Page: r.Page, Dims: []PackageDim{}, Land: []PackageDim{}})
			i = len(out) - 1
		}
		pp := &out[i]
		pp.Dims = mergeDims(pp.Dims, r.Dims, r.Source)
		pp.Land = mergeDims(pp.Land, r.Land, r.Source)
		pp.Candidates = append(pp.Candidates, r.Candidates...)
		pp.Checks = append(pp.Checks, r.Checks...)
		pp.Code = firstOf(pp.Code, r.Code)
		pp.Reference = firstOf(pp.Reference, r.Reference)
		pp.Notes = firstOf(pp.Notes, r.Notes)
		for _, part := range r.Parts {
			if !containsFold(pp.Parts, part) {
				pp.Parts = append(pp.Parts, part)
			}
		}
		if !containsFold(pp.Sources, r.Source) {
			pp.Sources = append(pp.Sources, r.Source)
		}
		pp.Rows = append(pp.Rows, r)
	}
	return out
}

// mergeDims adds the dimensions not yet given, by symbol (or key).
func mergeDims(have []PackageDim, add []library.PackageDim, source string) []PackageDim {
	for _, d := range add {
		dup := false
		for _, h := range have {
			if h.Symbol == d.Symbol || (h.Key != "" && h.Key == d.Key) {
				dup = true
				break
			}
		}
		if !dup {
			have = append(have, PackageDim{d, source})
		}
	}
	return have
}

// firstOf is a if set, else b.
func firstOf(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

// cadNormal is a package name with only its letters and digits.
func cadNormal(name string) string {
	var b strings.Builder
	for _, r := range strings.ToUpper(name) {
		if r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// pinCount is how many pins the pin table gives a package, by number
// (pins without one each count); a lone package takes every pin.
func pinCount(pins []library.Pin, name string, only bool) int {
	numbers := map[string]bool{}
	for i, p := range pins {
		if only || p.Package == "" || packageMatches(p.Package, name) {
			key := strings.ToUpper(p.Number)
			if key == "" { // a pin read without its number counts by itself
				key = fmt.Sprintf("#%d", i)
			}
			numbers[key] = true
		}
	}
	return len(numbers)
}

// packageMatches reports whether two names are the same package:
// SOT-23-6 and SOT23-6, or SOT-23 (DDC) and SOT-23-THIN.
func packageMatches(a, b string) bool {
	na, nb := cadNormal(a), cadNormal(b)
	if na == "" || nb == "" {
		return false
	}
	return na == nb || strings.HasPrefix(na, nb) || strings.HasPrefix(nb, na) || strings.Contains(na, nb) || strings.Contains(nb, na)
}

// packageFor is the datasheet package a CAD package is drawn from: the
// one of its name, else the only one.
func packageFor(pkgs []PartPackage, name string) (PartPackage, bool) {
	for _, p := range pkgs {
		if cadNormal(p.Name) == cadNormal(name) {
			return p, true
		}
	}
	for _, p := range pkgs {
		if packageMatches(p.Name, name) {
			return p, true
		}
	}
	if len(pkgs) == 1 {
		return pkgs[0], true
	}
	return PartPackage{}, false
}

// dimOf is a package's dimension of a key.
func dimOf(ds []PackageDim, key string) (cad.Dim, bool) {
	for _, d := range ds {
		if d.Key == key {
			var out cad.Dim
			if d.Min != nil {
				out.Min = *d.Min
			}
			if d.Nom != nil {
				out.Nom = *d.Nom
			}
			if d.Max != nil {
				out.Max = *d.Max
			}
			return out, out.Set()
		}
	}
	return cad.Dim{}, false
}

// packageCheck is one of the checks a package's dimensions go through.
type packageCheck func(PartPackage) []string

// packageChecks are the checks, in order.
var packageChecks = []packageCheck{checkLimits, checkPitchFits, checkLandClearsBody, checkPadFitsExposed}

// checkPackage runs the checks.
func checkPackage(p PartPackage) []string {
	var out []string
	for _, c := range packageChecks {
		out = append(out, c(p)...)
	}
	return out
}

// checkLimits flags a minimum over a maximum, or a nominal outside them.
func checkLimits(p PartPackage) []string {
	var out []string
	for _, d := range append(append([]PackageDim{}, p.Dims...), p.Land...) {
		lo, hi := d.Min, d.Max
		if lo != nil && hi != nil && *lo > *hi {
			out = append(out, fmt.Sprintf("%s: minimum %g mm is over maximum %g mm", d.Symbol, *lo, *hi))
		}
		if d.Nom != nil && ((lo != nil && *d.Nom < *lo-1e-6) || (hi != nil && *d.Nom > *hi+1e-6)) {
			out = append(out, fmt.Sprintf("%s: nominal %g mm is outside its limits", d.Symbol, *d.Nom))
		}
	}
	return out
}

// checkPitchFits flags a row of pins longer than the body it is on, for
// a package with two rows.
func checkPitchFits(p PartPackage) []string {
	pitch, okP := dimOf(p.Dims, "pitch")
	body, okB := dimOf(p.Dims, "bodyLength")
	lead, _ := dimOf(p.Dims, "leadWidth")
	if !okP || !okB || p.Pins < 4 || familyOf(p.Name) != "gullwing" && familyOf(p.Name) != "dfn" {
		return nil
	}
	per := (p.Pins + 1) / 2
	row := pitch.Mid()*float64(per-1) + lead.Lo()
	if row > body.Hi()+0.3 {
		return []string{fmt.Sprintf("%d pins at %g mm pitch take %.2f mm, more than the %g mm body: the pitch, the body length or the pin count is wrong", p.Pins, pitch.Mid(), row, body.Hi())}
	}
	return nil
}

// checkLandClearsBody flags a land pattern whose pads don't reach past
// the body of a leaded package.
func checkLandClearsBody(p PartPackage) []string {
	span, okS := dimOf(p.Land, "rowSpan")
	padLen, okL := dimOf(p.Land, "padLength")
	body, okB := dimOf(p.Dims, "bodyWidth")
	if !okS || !okL || !okB || familyOf(p.Name) != "gullwing" {
		return nil
	}
	if outer := span.Mid() + padLen.Mid(); outer < body.Hi() {
		return []string{fmt.Sprintf("the land pattern's pads end %.2f mm apart, inside the %g mm body: check C and Y", outer, body.Hi())}
	}
	return nil
}

// checkPadFitsExposed flags a land's thermal pad larger than the exposed
// pad it solders.
func checkPadFitsExposed(p PartPackage) []string {
	ew, okW := dimOf(p.Dims, "padWidth")
	lw, okL := dimOf(p.Land, "exposedPadWidth")
	if okW && okL && lw.Mid() > ew.Hi()+0.5 {
		return []string{fmt.Sprintf("the land's thermal pad (%g mm) is much wider than the exposed pad (%g mm)", lw.Mid(), ew.Hi())}
	}
	return nil
}

// familyRules name a package's family from its name.
var familyRules = []struct {
	re     *regexp.Regexp
	family string
}{
	{regexp.MustCompile(`^(T|V|W)?SOT|^SC-?\d|^SOIC|^SO-?\d|^SOP|^E?TSSOP|^V?MSOP|^VSSOP|^SSOP|^HTSSOP`), "gullwing"},
	{regexp.MustCompile(`QFPN|QFN|^LGA`), "qfn"},
	{regexp.MustCompile(`QFP`), "qfp"},
	{regexp.MustCompile(`DFN|SON$|^W?SON|^X\d?SON|^VSON`), "dfn"},
	{regexp.MustCompile(`BGA|CSP`), "bga"},
}

// familyOf is the family a package's name says it is in, or "".
func familyOf(name string) string {
	n := strings.ToUpper(strings.TrimSpace(name))
	for _, r := range familyRules {
		if r.re.MatchString(n) {
			return r.family
		}
	}
	return ""
}

// datasheetPackage is the footprint's package from a datasheet's
// dimensions: the standard outline's (when the name is one) with the
// datasheet's own wherever it gives them, and its land pattern when it
// gives one. ok is false when there's too little to draw.
func datasheetPackage(pp PartPackage, name string, pins int) (cad.Package, bool) {
	base, std := cad.Standard(name, pins)
	if !std {
		base, std = cad.Standard(pp.Name, pins)
	}
	if !std {
		base = cad.Package{Family: familyOf(pp.Name), Name: pp.Name, Pins: pins}
		if base.Family == "" {
			base.Family = familyOf(name)
		}
	}
	if base.Family == "" || pins == 0 {
		return cad.Package{}, false
	}
	set := func(dst *cad.Dim, key string) bool {
		if d, ok := dimOf(pp.Dims, key); ok {
			*dst = d
			return true
		}
		return false
	}
	n := 0
	for _, f := range []struct {
		dst *cad.Dim
		key string
	}{
		{&base.BodyLength, "bodyLength"}, {&base.BodyWidth, "bodyWidth"}, {&base.Span, "span"}, {&base.SpanX, "spanX"}, {&base.Height, "height"},
		{&base.Standoff, "standoff"}, {&base.LeadLength, "leadLength"}, {&base.LeadWidth, "leadWidth"}, {&base.LeadThickness, "leadThickness"},
	} {
		if set(f.dst, f.key) {
			n++
		}
	}
	if d, ok := dimOf(pp.Dims, "pitch"); ok {
		base.Pitch, n = d.Mid(), n+1
	}
	if w, okW := dimOf(pp.Dims, "padWidth"); okW {
		if l, okL := dimOf(pp.Dims, "padLength"); okL {
			base.ExposedPad, n = &cad.Size{W: w.Mid(), H: l.Mid()}, n+1
		}
	}
	if base.Pins != pins && pins > 0 {
		// The pin table's count wins over the outline's (SOT-23 vs SOT-23-6).
		base.Pins, base.Layout = pins, nil
	}
	if land, ok := landOf(pp, base.Pitch); ok {
		base.Land, n = &land, n+1
	}
	if n == 0 {
		return cad.Package{}, false
	}
	base.Page = pp.Page + 1
	return base, true
}

// landOf is a datasheet's land pattern, when it gives the pads' size and
// where their rows are: C, or Z and G.
func landOf(pp PartPackage, pitch float64) (cad.Land, bool) {
	x, okX := dimOf(pp.Land, "padWidth")
	y, okY := dimOf(pp.Land, "padLength")
	if !okX || !okY {
		return cad.Land{}, false
	}
	land := cad.Land{PadWidth: x.Mid(), PadLength: y.Mid(), Pitch: pitch, Page: pp.Page + 1}
	if c, ok := dimOf(pp.Land, "rowSpan"); ok {
		land.RowSpan = c.Mid()
		// A land table's C is sometimes the pitch, not the rows' span.
		if pitch > 0 && math.Abs(land.RowSpan-pitch) < 0.01 {
			land.RowSpan = 0
		}
	}
	if land.RowSpan == 0 {
		z, okZ := dimOf(pp.Land, "outerSpan")
		g, okG := dimOf(pp.Land, "gap")
		switch {
		case okZ && okG:
			land.RowSpan = (z.Mid() + g.Mid()) / 2
		case okZ:
			land.RowSpan = z.Mid() - land.PadLength
		case okG:
			land.RowSpan = g.Mid() + land.PadLength
		}
	}
	if e, ok := dimOf(pp.Land, "pitch"); ok && land.Pitch == 0 {
		land.Pitch = e.Mid()
	}
	land.RowSpan = math.Round(land.RowSpan*10000) / 10000
	return land, land.RowSpan > 0
}

// RecordPackage stores a package's dimensions as an agent read them from
// the datasheet's drawing, and returns the part's packages as they now
// stand.
func (s *Service) RecordPackage(ctx context.Context, ref string, p library.PackageDims, source string) (PartPackages, error) {
	r, err := s.Lib.Resolve(ctx, ref)
	if err != nil {
		return PartPackages{}, err
	}
	if r.Part.DatasheetID == 0 {
		return PartPackages{}, fmt.Errorf("%s has no datasheet to record a package for", r.Part.MPN)
	}
	p.DatasheetID = r.Part.DatasheetID
	for _, ds := range [][]library.PackageDim{p.Dims, p.Land} {
		for i := range ds {
			if ds[i].Unit == "" {
				ds[i].Unit = "mm"
			}
		}
	}
	if err := s.Lib.RecordPackage(ctx, p, source); err != nil {
		return PartPackages{}, err
	}
	return s.PartPackages(ctx, ref)
}

// DeletePackage removes a recorded package row.
func (s *Service) DeletePackage(ctx context.Context, id int64) error {
	return s.Lib.DeletePackage(ctx, id)
}
