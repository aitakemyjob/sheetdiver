package catalog

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"math"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// Value is a parsed attribute value: a number or range in SI units
// ("4.5V~17V" is 4.5 to 17 V; "48mΩ@2.5V" is 0.048 Ω, the condition after
// @ is dropped; "±1%" is 1 %).
type Value struct {
	Lo, Hi float64
	Unit   string
}

// baseUnits are the units values are reduced to, longest first so "Hz"
// wins over "H" and "Vrms" over "V".
var baseUnits = func() []string {
	u := []string{"ppm/℃", "ppm/°C", "Vrms", "Hz", "dB", "V", "A", "Ω", "W", "F", "H", "s", "C", "%", "℃", "°C", "S", "ppm"}
	sort.SliceStable(u, func(i, j int) bool { return len(u[i]) > len(u[j]) })
	return u
}()

// prefixes are SI multipliers.
var prefixes = map[string]float64{"": 1, "p": 1e-12, "n": 1e-9, "u": 1e-6, "µ": 1e-6, "μ": 1e-6, "m": 1e-3, "k": 1e3, "K": 1e3, "M": 1e6, "G": 1e9}

// numUnitRe matches a number with an optional unit.
var numUnitRe = regexp.MustCompile(`^([+-]?\d+(?:\.\d+)?)\s*(\S*)$`)

// parseOne reads "4.7uF", "-55℃", "100", "62.5mW".
func parseOne(s string) (float64, string, bool) {
	m := numUnitRe.FindStringSubmatch(strings.TrimSpace(s))
	if m == nil {
		return 0, "", false
	}
	v, err := strconv.ParseFloat(m[1], 64)
	if err != nil {
		return 0, "", false
	}
	u := m[2]
	if u == "" {
		return v, "", true
	}
	for _, base := range baseUnits {
		if p, ok := strings.CutSuffix(u, base); ok {
			if mult, ok := prefixes[p]; ok {
				if base == "°C" {
					base = "℃"
				}
				return v * mult, base, true
			}
		}
	}
	return 0, "", false
}

// ParseValue reads an attribute value; ok is false for text.
func ParseValue(s string) (Value, bool) {
	s = strings.TrimSpace(s)
	if i := strings.Index(s, "@"); i > 0 {
		s = s[:i]
	}
	s = strings.TrimPrefix(strings.TrimPrefix(s, "±"), "+/-")
	if lo, hi, ok := strings.Cut(s, "~"); ok {
		a, ua, okA := parseOne(lo)
		b, ub, okB := parseOne(hi)
		if !okA || !okB || (ua != ub && ua != "" && ub != "") {
			return Value{}, false
		}
		return Value{Lo: math.Min(a, b), Hi: math.Max(a, b), Unit: firstNonEmpty(ua, ub)}, true
	}
	v, u, ok := parseOne(s)
	return Value{Lo: v, Hi: v, Unit: u}, ok
}

// firstNonEmpty returns the first non-empty string.
func firstNonEmpty(ss ...string) string {
	for _, s := range ss {
		if s != "" {
			return s
		}
	}
	return ""
}

// Param is one attribute of a set of parts, summarized for filtering:
// numeric (with its unit and range) or text (with its common values).
type Param struct {
	Key string `json:"key"`
	// Kind is number or text.
	Kind string `json:"kind"`
	// Count is how many of the parts have it.
	Count int     `json:"count"`
	Unit  string  `json:"unit,omitempty"`
	Min   float64 `json:"min,omitempty"`
	Max   float64 `json:"max,omitempty"`
	// Values are the commonest text values (multi-valued attributes such
	// as Features are split on commas).
	Values []ValueCount `json:"values,omitempty"`
}

// ValueCount is a text value and how many parts have it.
type ValueCount struct {
	Value string `json:"value"`
	Count int    `json:"count"`
}

// maxScan bounds how many parts a parameter summary or attribute filter
// reads.
const maxScan = 200000

// candidates runs a query's text and flag filters without a limit and
// returns the matching rows' attributes, in the query's order.
func (c *Catalog) candidates(ctx context.Context, q Query, cols string) (*sql.Rows, error) {
	where, args := q.where()
	if q.Subcategory == "" && strings.TrimSpace(q.Text) == "" {
		return nil, errors.New("pick a category (or search words) before filtering on parameters")
	}
	rows, err := c.db.QueryContext(ctx, `SELECT `+cols+` FROM jlc_components WHERE `+where+ //nolint:gosec // constant SQL fragments; values are placeholders
		` ORDER BY `+q.order()+` LIMIT ?`, append(args, maxScan)...)
	if err != nil {
		return nil, err
	}
	return rows, nil
}

// Params summarizes the attributes of the parts a query matches (usually
// one category), most common first; attributes on fewer than 3% of them
// are left out.
func (c *Catalog) Params(ctx context.Context, q Query) ([]Param, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.db == nil {
		return nil, ErrNotInstalled
	}
	rows, err := c.candidates(ctx, q, "attributes")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	type acc struct {
		count    int
		units    map[string]int
		min, max map[string]float64
		texts    map[string]int
	}
	all := map[string]*acc{}
	n := 0
	for rows.Next() {
		var s string
		if err := rows.Scan(&s); err != nil {
			return nil, err
		}
		n++
		for k, v := range parseAttrs(s) {
			a := all[k]
			if a == nil {
				a = &acc{units: map[string]int{}, min: map[string]float64{}, max: map[string]float64{}, texts: map[string]int{}}
				all[k] = a
			}
			a.count++
			if pv, ok := ParseValue(v); ok {
				if a.units[pv.Unit] == 0 || pv.Lo < a.min[pv.Unit] {
					a.min[pv.Unit] = pv.Lo
				}
				if a.units[pv.Unit] == 0 || pv.Hi > a.max[pv.Unit] {
					a.max[pv.Unit] = pv.Hi
				}
				a.units[pv.Unit]++
				continue
			}
			for _, t := range splitValues(v) {
				a.texts[t]++
			}
		}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	var out []Param
	for k, a := range all {
		if a.count*100 < n*3 || a.count < 2 {
			continue
		}
		unit, best := "", 0
		for u, c := range a.units {
			if c > best || c == best && u < unit {
				unit, best = u, c
			}
		}
		p := Param{Key: k, Count: a.count}
		if best*10 >= a.count*8 {
			p.Kind, p.Unit, p.Min, p.Max = "number", unit, a.min[unit], a.max[unit]
		} else {
			p.Kind = "text"
			for v, c := range a.texts {
				p.Values = append(p.Values, ValueCount{v, c})
			}
			sort.Slice(p.Values, func(i, j int) bool {
				if p.Values[i].Count != p.Values[j].Count {
					return p.Values[i].Count > p.Values[j].Count
				}
				return p.Values[i].Value < p.Values[j].Value
			})
			if len(p.Values) > 40 {
				p.Values = p.Values[:40]
			}
			if len(p.Values) < 2 && len(a.texts) < 2 {
				continue // one value for every part: nothing to filter
			}
		}
		out = append(out, p)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Count != out[j].Count {
			return out[i].Count > out[j].Count
		}
		return out[i].Key < out[j].Key
	})
	return out, nil
}

// splitValues splits a multi-valued text attribute ("Overcurrent
// Protection, Light load efficient mode").
func splitValues(v string) []string {
	var out []string
	for _, p := range strings.Split(v, ",") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

// AttrFilter is a requirement on one attribute. For numbers Op is >= or
// <= against Value (SI, in the attribute's unit): a part meets ">= x" when
// its value (or the top of its range) reaches x, "<= x" when its value
// (or the bottom of its range) is at most x, so "supply ≥ 24 V" means
// the part accepts 24 V. For text Op is "=" and Text a value (one of a
// multi-valued attribute counts).
type AttrFilter struct {
	Key   string  `json:"key"`
	Op    string  `json:"op"`
	Value float64 `json:"value,omitempty"`
	Text  string  `json:"text,omitempty"`
	// Input is the value as typed ("30V", "10mΩ", "4.7u"); when set it
	// is parsed into Value (for >= and <=) or used as Text (for =).
	Input string `json:"input,omitempty"`
}

// match reports whether an attribute value meets the filter.
func (f AttrFilter) match(v string) bool {
	switch f.Op {
	case "=":
		for _, t := range splitValues(v) {
			if strings.EqualFold(t, f.Text) {
				return true
			}
		}
		return strings.EqualFold(strings.TrimSpace(v), f.Text)
	case ">=", "<=":
		pv, ok := ParseValue(v)
		if !ok {
			return false
		}
		eps := 1e-9 * math.Max(math.Abs(f.Value), 1e-30)
		if f.Op == ">=" {
			return pv.Hi >= f.Value-eps
		}
		return pv.Lo <= f.Value+eps
	}
	return false
}

// checkFilters validates attribute filters and parses typed values.
func checkFilters(fs []AttrFilter) error {
	for i, f := range fs {
		if f.Key == "" {
			return errors.New("a filter needs an attribute key")
		}
		switch f.Op {
		case ">=", "<=":
			if f.Input != "" {
				v, err := ParseQuantity(f.Input)
				if err != nil {
					return fmt.Errorf("filter %q: %w", f.Key, err)
				}
				fs[i].Value = v
			}
		case "=":
			if f.Input != "" {
				fs[i].Text = f.Input
			}
		default:
			return fmt.Errorf("filter %q: op must be >=, <= or =", f.Key)
		}
	}
	return nil
}

// attrSort is the key of an attribute sort ("attr:RDS(on)"), else "".
func attrSort(s string) string {
	k, _ := strings.CutPrefix(s, "attr:")
	if k == s {
		return ""
	}
	return k
}

// searchAttrs runs a search with attribute filters or an attribute sort:
// the text and flag filters select candidates in SQL, attributes are
// checked here, then the page is cut.
func (c *Catalog) searchAttrs(ctx context.Context, q Query, limit int) ([]Item, error) {
	if err := checkFilters(q.Attrs); err != nil {
		return nil, err
	}
	rows, err := c.candidates(ctx, q, itemCols)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	sortKey := attrSort(q.Sort)
	type keyed struct {
		it  Item
		key float64
		has bool
	}
	var hits []keyed
	for rows.Next() {
		it, err := scanItem(rows)
		if err != nil {
			return nil, err
		}
		ok := true
		for _, f := range q.Attrs {
			if !f.match(it.Attributes[f.Key]) {
				ok = false
				break
			}
		}
		if !ok {
			continue
		}
		k := keyed{it: it}
		if sortKey != "" {
			if pv, ok := ParseValue(it.Attributes[sortKey]); ok {
				k.key, k.has = pv.Lo, true
				if q.Desc {
					k.key = pv.Hi
				}
			}
		}
		hits = append(hits, k)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if sortKey != "" {
		sort.SliceStable(hits, func(i, j int) bool {
			a, b := hits[i], hits[j]
			if a.has != b.has {
				return a.has
			}
			if q.Desc {
				return a.key > b.key
			}
			return a.key < b.key
		})
	}
	out := []Item{}
	for i := max(0, q.Offset); i < len(hits) && len(out) < limit; i++ {
		out = append(out, hits[i].it)
	}
	return out, nil
}

// bareMultRe matches a number with only a multiplier ("4.7u", "10k").
var bareMultRe = regexp.MustCompile(`^([+-]?\d+(?:\.\d+)?)([pnuµμmkKMG])$`)

// ParseQuantity reads a filter value typed by a person or agent: "30V",
// "10mΩ", "4.7u", "1.5nF", "24". It returns the SI number.
func ParseQuantity(s string) (float64, error) {
	s = strings.TrimSpace(strings.ReplaceAll(s, " ", ""))
	if v, ok := ParseValue(s); ok {
		return v.Lo, nil
	}
	// A bare multiplier: "4.7u", "10k".
	m := bareMultRe.FindStringSubmatch(s)
	if m != nil {
		v, _ := strconv.ParseFloat(m[1], 64)
		return v * prefixes[m[2]], nil
	}
	return 0, fmt.Errorf("can't read %q as a number (try 30V, 10mΩ, 4.7u)", s)
}
