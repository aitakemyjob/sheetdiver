package core

import (
	"context"
	"fmt"
	"math"
	"sort"
	"strings"

	"github.com/aitakemyjob/sheetdiver/internal/calc"
	"github.com/aitakemyjob/sheetdiver/internal/library"
)

// How a key decides whether a replacement is at least as good.
const (
	// dirHigher: more is better (current, voltage rating, current limit).
	dirHigher = "higher"
	// dirLower: less is better (quiescent current, on-resistance, charge).
	dirLower = "lower"
	// dirCover: the replacement's range must cover the original's
	// (input/output voltage range, junction temperature).
	dirCover = "cover"
	// dirClose: the value should match (reference voltage, frequency,
	// threshold), within closeTolerance.
	dirClose = "close"
)

// keyDirections says, per key, what makes a replacement at least as good.
var keyDirections = map[string]string{
	"vin": dirCover, "vout": dirCover, "tj": dirCover,
	"iout": dirHigher, "id": dirHigher, "vds": dirHigher, "vgs": dirHigher, "ilim": dirHigher, "ilim_in": dirHigher, "ichg": dirHigher,
	"iq": dirLower, "isd": dirLower, "idss": dirLower, "rdson": dirLower, "rdson_hs": dirLower, "rdson_ls": dirLower, "vdropout": dirLower,
	"qg": dirLower, "qgd": dirLower, "ciss": dirLower, "rth_ja": dirLower, "rth_jc": dirLower, "ton_min": dirLower, "toff_min": dirLower,
	"vref": dirClose, "fsw": dirClose, "vreg": dirClose, "vgs_th": dirClose, "vih": dirClose, "vil": dirClose,
}

// closeTolerance is how far a dirClose value may differ (relative).
const closeTolerance = 0.1

// relatedCategories lists the categories whose parts can replace a
// category's parts.
var relatedCategories = map[string][]string{
	"mosfet":     {"mosfet-n", "mosfet-p"},
	"mosfet-n":   {"mosfet"},
	"mosfet-p":   {"mosfet"},
	"buck":       {"buck-boost", "power"},
	"boost":      {"buck-boost", "power"},
	"power":      {"buck", "boost", "buck-boost"},
	"buck-boost": {"power"},
}

// Verdicts of a key comparison.
const (
	VerdictBetter  = "better"
	VerdictSame    = "same"
	VerdictWorse   = "worse"
	VerdictUnknown = "unknown"
)

// AltCheck compares one key between the original part and a candidate.
type AltCheck struct {
	Key       string `json:"key"`
	Direction string `json:"direction"`
	Original  string `json:"original"`
	Candidate string `json:"candidate"`
	Verdict   string `json:"verdict"`
	// Page of the candidate's value (for the link), -1 when unknown.
	Page int `json:"page"`
}

// Alternative is a candidate replacement with how it compares.
type Alternative struct {
	Part          library.Part `json:"part"`
	Checks        []AltCheck   `json:"checks"`
	Better        int          `json:"better"`
	Same          int          `json:"same"`
	Worse         int          `json:"worse"`
	Unknown       int          `json:"unknown"`
	SamePackage   bool         `json:"samePackage"`
	SameDatasheet bool         `json:"sameDatasheet"`
	// Drop-in: nothing worse and at most one key unknown.
	DropIn bool `json:"dropIn"`
	// Distance measures how far its values are from the original's
	// (0 = identical); used to rank candidates that are equally good.
	Distance float64 `json:"distance"`
}

// AltRequest asks for replacements of a part.
type AltRequest struct {
	Part string `json:"part"`
	// Keys limits the comparison to these keys (default: the type's
	// comparison keys the part has a value for).
	Keys []string `json:"keys,omitempty"`
	// Discouraged includes parts marked avoid, NRND or EOL.
	Discouraged bool `json:"discouraged,omitempty"`
	Limit       int  `json:"limit,omitempty"`
}

// AltResult lists a part's alternatives, best first.
type AltResult struct {
	Part         library.Part  `json:"part"`
	Category     string        `json:"category"`
	Keys         []string      `json:"keys"`
	Alternatives []Alternative `json:"alternatives"`
	// Considered is how many candidates of the right type were compared.
	Considered int `json:"considered"`
}

// Alternatives finds library parts that could replace a part: the same
// (or a related) component type, compared key by key in the direction
// that matters (more output current is better, less quiescent current is
// better, the input range must cover the original's, the reference
// voltage should match). Drop-ins (nothing worse) come first, then the
// fewest worse keys; ties go to the preferred status, the same package
// and the closest values.
func (s *Service) Alternatives(ctx context.Context, req AltRequest) (AltResult, error) {
	res, err := s.Lib.Resolve(ctx, req.Part)
	if err != nil {
		return AltResult{}, err
	}
	orig := res.Part
	out := AltResult{Part: orig, Category: orig.Category, Alternatives: []Alternative{}}
	if orig.Category == "" {
		return out, fmt.Errorf("%s has no component type; set its category (update_part) to find alternatives", orig.MPN)
	}
	ov, err := s.partValues(ctx, orig)
	if err != nil {
		return out, err
	}
	keys := req.Keys
	if len(keys) == 0 {
		for _, k := range categoryKeys[orig.Category] {
			if _, ok := ov[k]; ok && keyDirections[k] != "" {
				keys = append(keys, k)
			}
		}
	}
	if len(keys) == 0 {
		return out, fmt.Errorf("%s has no key parameters to compare; record its main ratings (record_parameters) first", orig.MPN)
	}
	out.Keys = keys
	cats := append([]string{orig.Category}, relatedCategories[orig.Category]...)
	for _, cat := range cats {
		parts, err := s.Lib.Parts(ctx, library.PartFilter{Category: cat})
		if err != nil {
			return out, err
		}
		for _, p := range parts {
			if p.ID == orig.ID || (!req.Discouraged && library.Discouraged(p.Status)) {
				continue
			}
			cv, err := s.partValues(ctx, p)
			if err != nil {
				return out, err
			}
			out.Considered++
			alt := Alternative{
				Part:          p,
				SameDatasheet: p.DatasheetID != 0 && p.DatasheetID == orig.DatasheetID,
				SamePackage:   orig.Package != "" && strings.EqualFold(orig.Package, p.Package),
			}
			for _, k := range keys {
				o, haveO := ov[k]
				v, haveC := cv[k]
				c, dist := compareKey(k, o, v, haveO, haveC)
				alt.Checks = append(alt.Checks, c)
				alt.Distance += dist
				switch c.Verdict {
				case VerdictBetter:
					alt.Better++
				case VerdictSame:
					alt.Same++
				case VerdictWorse:
					alt.Worse++
				default:
					alt.Unknown++
				}
			}
			// A candidate that shares nothing with the original isn't one.
			if alt.Better+alt.Same+alt.Worse == 0 {
				continue
			}
			alt.DropIn = alt.Worse == 0 && alt.Unknown <= 1
			out.Alternatives = append(out.Alternatives, alt)
		}
	}
	sort.SliceStable(out.Alternatives, func(i, j int) bool {
		a, b := out.Alternatives[i], out.Alternatives[j]
		switch {
		case a.DropIn != b.DropIn:
			return a.DropIn
		case a.Worse != b.Worse:
			return a.Worse < b.Worse
		case a.Unknown != b.Unknown:
			return a.Unknown < b.Unknown
		case statusRank(a.Part) != statusRank(b.Part):
			return statusRank(a.Part) < statusRank(b.Part)
		case a.SamePackage != b.SamePackage:
			return a.SamePackage
		}
		return a.Distance < b.Distance
	})
	if req.Limit > 0 && len(out.Alternatives) > req.Limit {
		out.Alternatives = out.Alternatives[:req.Limit]
	}
	return out, nil
}

// statusRank orders parts for suggestions: preferred, favorite,
// approved, then the rest.
func statusRank(p library.Part) int {
	switch {
	case p.Status == library.StatusPreferred:
		return 0
	case p.Favorite:
		return 1
	case p.Status == library.StatusApproved:
		return 2
	}
	return 3
}

// compareKey compares one key of a candidate against the original,
// returning the check and a distance (|log ratio| of the values).
func compareKey(key string, o, c CompareValue, haveO, haveC bool) (AltCheck, float64) {
	dir := keyDirections[key]
	ch := AltCheck{Key: key, Direction: dir, Verdict: VerdictUnknown, Page: -1, Original: rangeStr(o, haveO), Candidate: rangeStr(c, haveC)}
	if haveC {
		ch.Page = c.Page
	}
	if !haveO || !haveC {
		return ch, 0
	}
	switch dir {
	case dirHigher:
		x, okX := pick(o, ">=", "")
		y, okY := pick(c, ">=", "")
		if okX && okY {
			ch.Verdict = order(y, x, false)
			return ch, logDist(x, y)
		}
	case dirLower:
		x, okX := pick(o, "<=", "")
		y, okY := pick(c, "<=", "")
		if okX && okY {
			ch.Verdict = order(y, x, true)
			return ch, logDist(x, y)
		}
	case dirCover:
		oLo, oHi := bounds(o)
		cLo, cHi := bounds(c)
		if oLo == nil && oHi == nil {
			return ch, 0
		}
		worse, better := false, false
		if oLo != nil {
			if cLo == nil {
				return ch, 0
			}
			worse = worse || *cLo > *oLo*(1+1e-6)+1e-12
			better = better || *cLo < *oLo*(1-0.02)
		}
		if oHi != nil {
			if cHi == nil {
				return ch, 0
			}
			worse = worse || *cHi < *oHi*(1-1e-6)-1e-12
			better = better || *cHi > *oHi*(1+0.02)
		}
		switch {
		case worse:
			ch.Verdict = VerdictWorse
		case better:
			ch.Verdict = VerdictBetter
		default:
			ch.Verdict = VerdictSame
		}
		d := 0.0
		if oLo != nil && cLo != nil {
			d += absDiffRel(*oLo, *cLo)
		}
		if oHi != nil && cHi != nil {
			d += logDist(*oHi, *cHi)
		}
		return ch, d
	case dirClose:
		x, okX := natural(o)
		y, okY := natural(c)
		if okX && okY {
			ch.Verdict = VerdictSame
			if absDiffRel(x, y) > closeTolerance {
				ch.Verdict = VerdictWorse
			}
			return ch, absDiffRel(x, y)
		}
	}
	return ch, 0
}

// order is the verdict for candidate y against original x, where lower
// is better when lowerBetter is set. Within 2% counts as the same.
func order(y, x float64, lowerBetter bool) string {
	if absDiffRel(x, y) <= 0.02 {
		return VerdictSame
	}
	if (y > x) != lowerBetter {
		return VerdictBetter
	}
	return VerdictWorse
}

// bounds is a value's range: min (else typ) to max (else typ).
func bounds(v CompareValue) (lo, hi *float64) {
	lo, hi = v.Min, v.Max
	if lo == nil {
		lo = v.Typ
	}
	if hi == nil {
		hi = v.Typ
	}
	return lo, hi
}

// absDiffRel is |x−y| relative to the larger magnitude.
func absDiffRel(x, y float64) float64 {
	m := math.Max(math.Abs(x), math.Abs(y))
	if m == 0 {
		return 0
	}
	return math.Abs(x-y) / m
}

// logDist is |ln(y/x)| for positive values, else the relative difference.
func logDist(x, y float64) float64 {
	if x > 0 && y > 0 {
		return math.Abs(math.Log(y / x))
	}
	return absDiffRel(x, y)
}

// rangeStr renders a value as "min … max unit" (or typ).
func rangeStr(v CompareValue, ok bool) string {
	if !ok {
		return "–"
	}
	f := func(x *float64) string { return calc.Eng(*x, v.Unit) }
	switch {
	case v.Min != nil && v.Max != nil:
		return f(v.Min) + " … " + f(v.Max)
	case v.Typ != nil:
		return f(v.Typ) + " typ"
	case v.Max != nil:
		return "≤ " + f(v.Max)
	case v.Min != nil:
		return "≥ " + f(v.Min)
	}
	return "–"
}
