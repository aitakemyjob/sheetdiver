package core

import (
	"context"
	"fmt"
	"math"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"

	"github.com/aitakemyjob/sheetdiver/internal/extract"
	"github.com/aitakemyjob/sheetdiver/internal/library"
	"github.com/aitakemyjob/sheetdiver/internal/pdf"
)

// categoryKeys are the parameters worth comparing for each component
// type, in column order. Types without an entry use whatever keys their
// parts have.
var categoryKeys = map[string][]string{
	"buck":               {"vin", "vout", "iout", "fsw", "ilim", "vref", "iq", "ton_min", "rdson_hs", "rdson_ls", "rth_ja"},
	"boost":              {"vin", "vout", "iout", "ilim", "fsw", "vref", "iq", "rdson_ls", "rth_ja"},
	"buck-boost":         {"vin", "vout", "iout", "ilim", "ilim_in", "fsw", "iq", "rth_ja"},
	"power":              {"vin", "vout", "iout", "fsw", "ilim", "iq", "rth_ja"},
	"ldo":                {"vin", "vout", "iout", "vdropout", "iq", "ilim", "rth_ja"},
	"charger":            {"vin", "ichg", "vreg", "iq", "isd", "rth_ja"},
	"mosfet-n":           {"vds", "id", "rdson", "vgs_th", "vgs", "qg", "qgd", "ciss", "rth_ja"},
	"mosfet-p":           {"vds", "id", "rdson", "vgs_th", "vgs", "qg", "qgd", "ciss", "rth_ja"},
	"mosfet":             {"vds", "id", "rdson", "vgs_th", "vgs", "qg", "ciss", "rth_ja"},
	"gate-driver":        {"vin", "iout", "iq", "vih", "vil", "rth_ja"},
	"load-switch":        {"vin", "iout", "rdson", "iq", "ilim", "rth_ja"},
	"mcu":                {"vin", "iq", "isd", "fsw", "vih", "vil", "tj"},
	"current-monitor":    {"vin", "iq", "isd", "tj"},
	"battery-protection": {"vocp", "vodp", "iq", "isd"},
}

// CategoryInfo is a component type with its parts count and comparison
// columns.
type CategoryInfo struct {
	Category string   `json:"category"`
	Count    int      `json:"count"`
	Keys     []string `json:"keys"`
}

// Categories lists the component types in the library, largest first.
func (s *Service) Categories(ctx context.Context) ([]CategoryInfo, error) {
	counts, err := s.Lib.Categories(ctx)
	if err != nil {
		return nil, err
	}
	out := []CategoryInfo{}
	for c, n := range counts {
		out = append(out, CategoryInfo{Category: c, Count: n, Keys: categoryKeys[c]})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Count != out[j].Count {
			return out[i].Count > out[j].Count
		}
		return out[i].Category < out[j].Category
	})
	return out, nil
}

// Filter is a requirement on one key, e.g. vin ≥ 24 V. Value is SI.
// Op is ">=", "<=", ">" or "<". Stat picks which limit to test: "min",
// "typ", "max", or "" for the natural one (≥ tests the highest the part
// can do — max, else typ, else min; ≤ tests typ, else max, else min).
type Filter struct {
	Key   string  `json:"key"`
	Op    string  `json:"op"`
	Value float64 `json:"value"`
	Stat  string  `json:"stat,omitempty"`
}

// CompareRequest asks for a comparison of the parts of one type.
type CompareRequest struct {
	Category string `json:"category"`
	// Parts compares these parts (part numbers) instead of a category's.
	Parts   []string `json:"parts,omitempty"`
	Keys    []string `json:"keys,omitempty"`
	Filters []Filter `json:"filters,omitempty"`
	// Query keeps parts whose number, manufacturer or description
	// contains it.
	Query string `json:"query,omitempty"`
	// Favorite keeps starred parts; Status keeps parts with that status.
	Favorite bool   `json:"favorite,omitempty"`
	Status   string `json:"status,omitempty"`
	// Sort is a key (by its natural value) or "mpn"; Desc reverses it.
	Sort string `json:"sort,omitempty"`
	Desc bool   `json:"desc,omitempty"`
}

// CompareValue is one part's value for one key, in SI units, with where
// it came from.
type CompareValue struct {
	Min        *float64  `json:"min,omitempty"`
	Typ        *float64  `json:"typ,omitempty"`
	Max        *float64  `json:"max,omitempty"`
	Unit       string    `json:"unit"`
	Rating     bool      `json:"rating,omitempty"`
	Conditions string    `json:"conditions,omitempty"`
	Page       int       `json:"page"`
	Box        *pdf.Rect `json:"box,omitempty"`
	ParamID    int64     `json:"paramId"`
	Source     string    `json:"source"`
}

// CompareRow is one part.
type CompareRow struct {
	Part   library.Part            `json:"part"`
	Values map[string]CompareValue `json:"values"`
}

// CompareColumn is one key, with how many of the type's parts have it.
type CompareColumn struct {
	Key   string `json:"key"`
	Label string `json:"label"`
	Unit  string `json:"unit"`
	Have  int    `json:"have"`
	// Direction says which values are better: higher, lower, close (to
	// a target) or cover (a range that includes the need).
	Direction string `json:"direction,omitempty"`
}

// CompareResult is the comparison table.
type CompareResult struct {
	Category string          `json:"category"`
	Columns  []CompareColumn `json:"columns"`
	Rows     []CompareRow    `json:"rows"`
	Total    int             `json:"total"`
	// Unknown counts, per filtered key, the parts excluded only because
	// they have no value for it.
	Unknown map[string]int `json:"unknown,omitempty"`
}

// Compare builds a table of a component type's parts by their key
// parameters, filtered and sorted. Values come from the same choice as a
// part's key parameters (recorded values first, operating conditions
// over ratings), converted to SI.
func (s *Service) Compare(ctx context.Context, req CompareRequest) (CompareResult, error) {
	out := CompareResult{Category: req.Category, Rows: []CompareRow{}, Unknown: map[string]int{}}
	if len(req.Parts) > 0 {
		return s.compareParts(ctx, req, out)
	}
	parts, err := s.Lib.Parts(ctx, library.PartFilter{Category: req.Category, Favorite: req.Favorite, Status: req.Status})
	if err != nil {
		return out, err
	}
	if req.Category == "" {
		return out, fmt.Errorf("give a category; one of: %s", strings.Join(s.categoryNames(ctx), ", "))
	}
	if len(parts) == 0 && (req.Favorite || req.Status != "") {
		return out, nil
	}
	if len(parts) == 0 {
		return out, fmt.Errorf("no parts of type %q; types in the library: %s", req.Category, strings.Join(s.categoryNames(ctx), ", "))
	}
	for _, f := range req.Filters {
		if _, ok := extract.Keys[f.Key]; !ok {
			return out, fmt.Errorf("unknown key %q in filter", f.Key)
		}
		switch f.Op {
		case ">=", "<=", ">", "<":
		default:
			return out, fmt.Errorf("filter op must be >=, <=, > or < (got %q)", f.Op)
		}
	}
	out.Total = len(parts)
	have := map[string]int{}
	units := map[string]string{}
	var all []CompareRow
	for _, p := range parts {
		vals, err := s.partValues(ctx, p)
		if err != nil {
			return out, err
		}
		for k, v := range vals {
			have[k]++
			units[k] = v.Unit
		}
		all = append(all, CompareRow{Part: p, Values: vals})
	}
	keys := req.Keys
	if len(keys) == 0 {
		keys = defaultKeys(req.Category, have)
	}
	for _, k := range keys {
		out.Columns = append(out.Columns, CompareColumn{Key: k, Label: extract.Keys[k], Unit: units[k], Have: have[k], Direction: keyDirections[k]})
	}
	q := strings.ToLower(strings.TrimSpace(req.Query))
rows:
	for _, r := range all {
		if q != "" && !strings.Contains(strings.ToLower(r.Part.MPN+" "+r.Part.Manufacturer+" "+r.Part.Description+" "+strings.Join(r.Part.Tags, " ")), q) {
			continue
		}
		for _, f := range req.Filters {
			v, ok := r.Values[f.Key]
			x, okStat := pick(v, f.Op, f.Stat)
			if !ok || !okStat {
				out.Unknown[f.Key]++
				continue rows
			}
			if !compare(x, f.Op, f.Value) {
				continue rows
			}
		}
		out.Rows = append(out.Rows, r)
	}
	sortRows(out.Rows, req.Sort, req.Desc)
	if len(out.Unknown) == 0 {
		out.Unknown = nil
	}
	return out, nil
}

// partValues is a part's key parameters in SI, keyed by canonical key.
func (s *Service) partValues(ctx context.Context, p library.Part) (map[string]CompareValue, error) {
	out := map[string]CompareValue{}
	if p.DatasheetID == 0 {
		return out, nil
	}
	params, err := s.Lib.Parameters(ctx, p.DatasheetID, library.ParamFilter{MPN: p.MPN})
	if err != nil {
		return nil, err
	}
	for _, kv := range keyValues(params) {
		if v, ok := compareValue(kv); ok {
			out[kv.Key] = v
		}
	}
	return out, nil
}

// categoryNames lists the categories in use.
func (s *Service) categoryNames(ctx context.Context) []string {
	cs, _ := s.Categories(ctx)
	var out []string
	for _, c := range cs {
		if c.Category != "" {
			out = append(out, c.Category)
		}
	}
	return out
}

// compareParts compares the given parts, in the order given, on the
// requested keys or the usual keys of their types.
func (s *Service) compareParts(ctx context.Context, req CompareRequest, out CompareResult) (CompareResult, error) {
	have := map[string]int{}
	units := map[string]string{}
	cats := map[string]bool{}
	for _, ref := range req.Parts {
		r, err := s.ResolvePart(ctx, ref)
		if err != nil {
			return out, err
		}
		vals, err := s.partValues(ctx, r.Part)
		if err != nil {
			return out, err
		}
		for k, v := range vals {
			have[k]++
			units[k] = v.Unit
		}
		cats[r.Part.Category] = true
		out.Rows = append(out.Rows, CompareRow{Part: r.Part, Values: vals})
	}
	keys := req.Keys
	if len(keys) == 0 {
		for c := range cats {
			for _, k := range defaultKeys(c, have) {
				if !slices.Contains(keys, k) {
					keys = append(keys, k)
				}
			}
		}
		sort.Strings(keys)
	}
	for _, k := range keys {
		if _, ok := extract.Keys[k]; !ok {
			return out, fmt.Errorf("unknown key %q", k)
		}
		out.Columns = append(out.Columns, CompareColumn{Key: k, Label: extract.Keys[k], Unit: units[k], Have: have[k], Direction: keyDirections[k]})
	}
	out.Total = len(out.Rows)
	out.Unknown = nil
	return out, nil
}

// defaultKeys picks a type's comparison columns: its preset keys that some
// part has, then other keys most of its parts have.
func defaultKeys(category string, have map[string]int) []string {
	var out []string
	seen := map[string]bool{}
	for _, k := range categoryKeys[category] {
		if have[k] > 0 {
			out = append(out, k)
			seen[k] = true
		}
	}
	var rest []string
	for k, n := range have {
		if !seen[k] && n >= 2 {
			rest = append(rest, k)
		}
	}
	sort.Slice(rest, func(i, j int) bool {
		if have[rest[i]] != have[rest[j]] {
			return have[rest[i]] > have[rest[j]]
		}
		return rest[i] < rest[j]
	})
	out = append(out, rest...)
	if len(out) > 14 {
		out = out[:14]
	}
	return out
}

// compareValue converts a key parameter to SI.
func compareValue(kv KeyValue) (CompareValue, bool) {
	p := kv.Param
	v := CompareValue{Rating: kv.Rating, Conditions: p.Conditions, Page: p.Page, Box: p.Box, ParamID: p.ID, Source: p.Source}
	conv := func(x *float64) *float64 {
		if x == nil {
			return nil
		}
		si, unit, ok := extract.ToSI(*x, p.Unit)
		if !ok {
			return nil
		}
		v.Unit = unit
		return &si
	}
	v.Min, v.Typ, v.Max = conv(p.Min), conv(p.Typ), conv(p.Max)
	if v.Min == nil && v.Typ == nil && v.Max == nil {
		return v, false
	}
	return v, true
}

// pick chooses the limit a filter tests.
func pick(v CompareValue, op, stat string) (float64, bool) {
	order := []*float64{v.Max, v.Typ, v.Min}
	switch {
	case stat == "min":
		order = []*float64{v.Min}
	case stat == "typ":
		order = []*float64{v.Typ}
	case stat == "max":
		order = []*float64{v.Max}
	case strings.HasPrefix(op, "<"):
		order = []*float64{v.Typ, v.Max, v.Min}
	}
	for _, x := range order {
		if x != nil {
			return *x, true
		}
	}
	return 0, false
}

// compare applies a filter operator with a small relative tolerance, so
// "≥ 3.3 V" accepts 3.3 V written as 3300 mV.
func compare(x float64, op string, y float64) bool {
	eps := 1e-9 * math.Max(math.Abs(x), math.Abs(y))
	switch op {
	case ">=":
		return x >= y-eps
	case "<=":
		return x <= y+eps
	case ">":
		return x > y+eps
	case "<":
		return x < y-eps
	}
	return false
}

// natural is the value a column sorts by: typ, else the middle of the
// range.
func natural(v CompareValue) (float64, bool) {
	switch {
	case v.Typ != nil:
		return *v.Typ, true
	case v.Min != nil && v.Max != nil:
		return (*v.Min + *v.Max) / 2, true
	case v.Max != nil:
		return *v.Max, true
	case v.Min != nil:
		return *v.Min, true
	}
	return 0, false
}

// sortRows orders rows by a key's natural value (parts without it last)
// or by part number.
func sortRows(rows []CompareRow, key string, desc bool) {
	sort.SliceStable(rows, func(i, j int) bool {
		if key == "" || key == "mpn" {
			if desc {
				return rows[i].Part.MPN > rows[j].Part.MPN
			}
			return rows[i].Part.MPN < rows[j].Part.MPN
		}
		a, okA := natural(rows[i].Values[key])
		b, okB := natural(rows[j].Values[key])
		switch {
		case okA != okB:
			return okA
		case !okA:
			return rows[i].Part.MPN < rows[j].Part.MPN
		case desc:
			return a > b
		}
		return a < b
	})
}

// quantityRe splits "24 V", "3A", "650 kHz", "1µ", "4.7u" into number and unit.
var quantityRe = regexp.MustCompile(`^\s*([-+]?\d*\.?\d+(?:[eE][-+]?\d+)?)\s*([a-zA-ZµμΩ°/%]*)\s*$`)

// ParseQuantity reads a value with an optional unit or SI prefix into SI:
// "24 V" → 24, "650k" → 650000, "1 µA" → 1e-6, "68 mΩ" → 0.068.
func ParseQuantity(s string) (float64, error) {
	m := quantityRe.FindStringSubmatch(strings.ReplaceAll(s, ",", "."))
	if m == nil {
		return 0, fmt.Errorf("%q is not a number with an optional unit", s)
	}
	v, err := strconv.ParseFloat(m[1], 64)
	if err != nil {
		return 0, err
	}
	unit := strings.ReplaceAll(m[2], "u", "µ")
	if unit == "" {
		return v, nil
	}
	if si, _, ok := extract.ToSI(v, unit); ok {
		return si, nil
	}
	// A bare prefix: "650k", "1µ".
	mult := map[string]float64{"p": 1e-12, "n": 1e-9, "µ": 1e-6, "m": 1e-3, "k": 1e3, "K": 1e3, "M": 1e6, "G": 1e9}
	if x, ok := mult[unit]; ok {
		return v * x, nil
	}
	return 0, fmt.Errorf("unknown unit %q in %q", m[2], s)
}
