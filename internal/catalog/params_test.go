package catalog

import (
	"context"
	"fmt"
	"math"
	"strings"
	"testing"
)

func TestParseValue(t *testing.T) {
	near := func(a, b float64) bool { return math.Abs(a-b) <= 1e-9*math.Max(math.Abs(a), math.Abs(b)) }
	cases := []struct {
		in     string
		lo, hi float64
		unit   string
	}{
		{"30V", 30, 30, "V"},
		{"48mΩ@2.5V", 0.048, 0.048, "Ω"},
		{"4.5V~17V", 4.5, 17, "V"},
		{"600mV~5.5V", 0.6, 5.5, "V"},
		{"±1%", 1, 1, "%"},
		{"±20V", 20, 20, "V"},
		{"1.5nF", 1.5e-9, 1.5e-9, "F"},
		{"190uA", 190e-6, 190e-6, "A"},
		{"650kHz", 650e3, 650e3, "Hz"},
		{"-55℃~+150℃", -55, 150, "℃"},
		{"±100ppm/℃", 100, 100, "ppm/℃"},
		{"15nC@10V", 15e-9, 15e-9, "C"},
		{"74dB@(1kHz)", 74, 74, "dB"},
		{"50uVrms", 50e-6, 50e-6, "Vrms"},
		{"10kΩ", 1e4, 1e4, "Ω"},
		{"2", 2, 2, ""},
	}
	for _, c := range cases {
		v, ok := ParseValue(c.in)
		if !ok || !near(v.Lo, c.lo) || !near(v.Hi, c.hi) || v.Unit != c.unit {
			t.Errorf("%q = %+v %v, want %g~%g %s", c.in, v, ok, c.lo, c.hi, c.unit)
		}
	}
	for _, s := range []string{"N-Channel", "X7R", "Built-in", "1 N-channel", "SOT-23", "5V~3A"} {
		if v, ok := ParseValue(s); ok {
			t.Errorf("%q is text, got %+v", s, v)
		}
	}
	for in, want := range map[string]float64{"30V": 30, "10mΩ": 0.01, "4.7u": 4.7e-6, "24": 24, "1.5 nF": 1.5e-9, "10k": 1e4} {
		if got, err := ParseQuantity(in); err != nil || !near(got, want) {
			t.Errorf("ParseQuantity(%q) = %g %v", in, got, err)
		}
	}
	if _, err := ParseQuantity("lots"); err == nil {
		t.Error("text isn't a quantity")
	}
}

func TestAttrFilterMatch(t *testing.T) {
	cases := []struct {
		f    AttrFilter
		v    string
		want bool
	}{
		{AttrFilter{Op: ">=", Value: 24}, "4.5V~40V", true},  // accepts 24 V
		{AttrFilter{Op: ">=", Value: 24}, "4.5V~17V", false}, // doesn't
		{AttrFilter{Op: "<=", Value: 3}, "2.5V~5.5V", true},  // works at 3 V
		{AttrFilter{Op: "<=", Value: 0.01}, "5mΩ@10V", true},
		{AttrFilter{Op: "<=", Value: 0.01}, "25mΩ@10V", false},
		{AttrFilter{Op: ">=", Value: 30}, "30V", true}, // inclusive
		{AttrFilter{Op: ">=", Value: 30}, "N-Channel", false},
		{AttrFilter{Op: "=", Text: "n-channel"}, "N-Channel", true},
		{AttrFilter{Op: "=", Text: "Hiccup current limiting"}, "Overcurrent Protection, Hiccup current limiting", true},
		{AttrFilter{Op: "=", Text: "Buck"}, "Buck-Boost", false},
		// Numbers compare by value; ranges contain; a typed unit must match.
		{AttrFilter{Op: "=", Text: "10k"}, "10kΩ", true},
		{AttrFilter{Op: "=", Text: "10kΩ"}, "10.0kΩ", true},
		{AttrFilter{Op: "=", Text: "10000"}, "10kΩ", true},
		{AttrFilter{Op: "=", Text: "10k"}, "10.2kΩ", false},
		{AttrFilter{Op: "=", Text: "100n"}, "100nF", true},
		{AttrFilter{Op: "=", Text: "3.3V"}, "600mV~5.5V", true},
		{AttrFilter{Op: "=", Text: "6V"}, "600mV~5.5V", false},
		{AttrFilter{Op: "=", Text: "10V"}, "10Ω", false},
		{AttrFilter{Op: "=", Values: []string{"4.7k", "10k"}}, "4.7kΩ", true},
		{AttrFilter{Op: "=", Text: "1"}, "1 N-channel", false},
	}
	for _, c := range cases {
		if got := c.f.match(c.v); got != c.want {
			t.Errorf("%+v on %q = %v", c.f, c.v, got)
		}
	}
	if err := checkFilters([]AttrFilter{{Key: "x", Op: "~"}}); err == nil {
		t.Error("bad op")
	}
	fs := []AttrFilter{{Key: "a", Op: ">=", Input: "10mΩ"}, {Key: "b", Op: "=", Input: "Buck"}}
	if err := checkFilters(fs); err != nil || fs[0].Value != 0.01 || len(fs[1].Values) != 1 || fs[1].Values[0] != "Buck" {
		t.Errorf("typed values = %+v %v", fs, err)
	}
	if err := checkFilters([]AttrFilter{{Key: "a", Op: "<=", Input: "lots"}}); err == nil {
		t.Error("unreadable values are reported")
	}
}

func TestParamsAndAttributeSearch(t *testing.T) {
	ctx := context.Background()
	rows := [][]any{
		{1, 1, "T", "MOSFETs", "N30A", "SOT-23", "M", "base", 0, "", "", 100, "1-:0.05", `{"Drain to Source Voltage":"30V","RDS(on)":"48mΩ@2.5V","Type":"N-Channel"}`},
		{2, 1, "T", "MOSFETs", "N60B", "SOT-23", "M", "expand", 0, "", "", 50, "1-:0.08", `{"Drain to Source Voltage":"60V","RDS(on)":"25mΩ@10V","Type":"N-Channel"}`},
		{3, 1, "T", "MOSFETs", "P20C", "SOT-23", "M", "expand", 0, "", "", 70, "1-:0.06", `{"Drain to Source Voltage":"20V","RDS(on)":"80mΩ@4.5V","Type":"P-Channel"}`},
		{4, 1, "T", "MOSFETs", "N100D", "TO-252", "M", "expand", 0, "", "", 0, "1-:0.30", `{"Drain to Source Voltage":"100V","RDS(on)":"9mΩ@10V","Type":"N-Channel"}`},
		{5, 1, "P", "DC-DC Converters", "BUCK1", "SOT-23-6", "M", "expand", 0, "", "", 9, "", `{"Voltage - Supply":"4.5V~40V","Topology":"Buck"}`},
	}
	c := installed(t, rows)
	ps, err := c.Params(ctx, Query{Subcategory: "MOSFETs"})
	if err != nil {
		t.Fatal(err)
	}
	byKey := map[string]Param{}
	for _, p := range ps {
		byKey[p.Key] = p
	}
	if p := byKey["Drain to Source Voltage"]; p.Kind != "number" || p.Unit != "V" || p.Min != 20 || p.Max != 100 || p.Count != 4 {
		t.Errorf("vds = %+v", p)
	}
	if p := byKey["RDS(on)"]; p.Kind != "number" || p.Unit != "Ω" || math.Abs(p.Min-0.009) > 1e-12 {
		t.Errorf("rdson = %+v", p)
	}
	if p := byKey["Type"]; p.Kind != "text" || len(p.Values) != 2 || p.Values[0] != (ValueCount{"N-Channel", 3}) {
		t.Errorf("type = %+v", p)
	}
	if _, ok := byKey["Topology"]; ok {
		t.Error("parameters are per category")
	}
	if ps, _ := c.Params(ctx, Query{Subcategory: "MOSFETs", InStock: true}); len(ps) == 0 || ps[0].Count != 3 {
		t.Errorf("params follow the flags: %+v", ps)
	}
	if _, err := c.Params(ctx, Query{}); err == nil {
		t.Error("parameters need a category or words")
	}

	mpns := func(items []Item) string {
		var s []string
		for _, it := range items {
			s = append(s, it.MPN)
		}
		return strings.Join(s, " ")
	}
	q := Query{Subcategory: "MOSFETs", Attrs: []AttrFilter{{Key: "Drain to Source Voltage", Op: ">=", Value: 30}, {Key: "Type", Op: "=", Text: "N-Channel"}}}
	if items, err := c.Search(ctx, q); err != nil || mpns(items) != "N30A N60B N100D" {
		t.Errorf("vds ≥ 30 V, N-channel = %q %v", mpns(items), err)
	}
	q.Sort = "attr:RDS(on)"
	if items, _ := c.Search(ctx, q); mpns(items) != "N100D N60B N30A" {
		t.Errorf("by RDS(on) = %q", mpns(items))
	}
	q.Desc, q.Limit, q.Offset = true, 1, 1
	if items, _ := c.Search(ctx, q); mpns(items) != "N60B" {
		t.Errorf("page 2 of RDS(on) desc = %q", mpns(items))
	}
	if items, _ := c.Search(ctx, Query{Subcategory: "MOSFETs", InStock: true, Attrs: []AttrFilter{{Key: "RDS(on)", Op: "<=", Value: 0.03}}}); mpns(items) != "N60B" {
		t.Errorf("in stock and RDS(on) ≤ 30 mΩ = %q", mpns(items))
	}
	if items, _ := c.Search(ctx, Query{Subcategory: "DC-DC Converters", Attrs: []AttrFilter{{Key: "Voltage - Supply", Op: ">=", Value: 24}}}); mpns(items) != "BUCK1" {
		t.Errorf("supply range accepts 24 V = %q", mpns(items))
	}
	if _, err := c.Search(ctx, Query{Attrs: []AttrFilter{{Key: "x", Op: ">=", Value: 1}}}); err == nil {
		t.Error("attribute filters need a category")
	}
}

func TestPackageFilterAndMultipleValues(t *testing.T) {
	ctx := context.Background()
	rows := [][]any{
		{1, 1, "T", "MOSFETs", "N30A", "SOT-23", "M", "base", 0, "", "", 100, "1-:0.05", `{"Drain to Source Voltage":"30V","Type":"N-Channel"}`},
		{2, 1, "T", "MOSFETs", "N60B", "SOT-23-3", "M", "expand", 0, "", "", 50, "1-:0.08", `{"Drain to Source Voltage":"60V","Type":"N-Channel"}`},
		{3, 1, "T", "MOSFETs", "P20C", "SOT-23", "M", "expand", 0, "", "", 70, "1-:0.06", `{"Drain to Source Voltage":"20V","Type":"P-Channel"}`},
		{4, 1, "T", "MOSFETs", "N100D", "TO-252", "M", "expand", 0, "", "", 9, "1-:0.30", `{"Drain to Source Voltage":"100V","Type":"N-Channel + P-Channel"}`},
	}
	c := installed(t, rows)
	ps, err := c.Params(ctx, Query{Subcategory: "MOSFETs"})
	if err != nil || ps[0].Key != PackageKey || ps[0].Kind != "text" || ps[0].Values[0] != (ValueCount{"SOT-23", 2}) || len(ps[0].Values) != 3 {
		t.Fatalf("package param first = %+v %v", ps[0], err)
	}
	mpns := func(q Query) string {
		t.Helper()
		items, err := c.Search(ctx, q)
		if err != nil {
			t.Fatal(err)
		}
		var s []string
		for _, it := range items {
			s = append(s, it.MPN)
		}
		return strings.Join(s, " ")
	}
	pkg := func(input string) AttrFilter { return AttrFilter{Key: PackageKey, Op: "=", Input: input} }
	// One package, several packages (any of), case-insensitive.
	if got := mpns(Query{Subcategory: "MOSFETs", Attrs: []AttrFilter{pkg("sot-23")}}); got != "N30A P20C" {
		t.Errorf("SOT-23 = %q", got)
	}
	if got := mpns(Query{Subcategory: "MOSFETs", Attrs: []AttrFilter{pkg("SOT-23|SOT-23-3")}}); got != "N30A P20C N60B" {
		t.Errorf("SOT-23 or SOT-23-3 = %q", got)
	}
	// Package with a parameter filter, and a multi-value text filter.
	q := Query{Subcategory: "MOSFETs", Attrs: []AttrFilter{pkg("SOT-23|SOT-23-3"), {Key: "Drain to Source Voltage", Op: ">=", Input: "30V"}}}
	if got := mpns(q); got != "N30A N60B" {
		t.Errorf("SOT-23(-3) and VDS ≥ 30 V = %q", got)
	}
	if got := mpns(Query{Subcategory: "MOSFETs", Attrs: []AttrFilter{{Key: "Type", Op: "=", Values: []string{"P-Channel", "N-Channel + P-Channel"}}}}); got != "P20C N100D" {
		t.Errorf("type is P or complementary = %q", got)
	}
	// A package filter works across categories too (plain SQL).
	if got := mpns(Query{Attrs: []AttrFilter{pkg("TO-252")}}); got != "N100D" {
		t.Errorf("TO-252 anywhere = %q", got)
	}
	if _, err := c.Search(ctx, Query{Subcategory: "MOSFETs", Attrs: []AttrFilter{{Key: PackageKey, Op: ">=", Input: "3"}}}); err == nil {
		t.Error("the package filter only takes =")
	}
	if _, err := c.Search(ctx, Query{Subcategory: "MOSFETs", Attrs: []AttrFilter{{Key: "Type", Op: "="}}}); err == nil {
		t.Error("a text filter needs a value")
	}
	// Filters are checked twice (Search, then searchAttrs) without doubling values.
	fs := []AttrFilter{pkg("A|B")}
	checkFilters(fs)
	checkFilters(fs)
	if len(fs[0].Values) != 2 {
		t.Errorf("values = %v", fs[0].Values)
	}
}

func TestParamsAreFacetedByTheOtherFilters(t *testing.T) {
	ctx := context.Background()
	rows := [][]any{
		{1, 1, "T", "MOSFETs", "N30A", "SOT-23", "M", "base", 0, "", "", 100, "1-:0.05", `{"Drain to Source Voltage":"30V","Type":"N-Channel"}`},
		{2, 1, "T", "MOSFETs", "N60B", "SOT-23-3", "M", "expand", 0, "", "", 50, "1-:0.08", `{"Drain to Source Voltage":"60V","Type":"N-Channel"}`},
		{3, 1, "T", "MOSFETs", "P20C", "SOT-23", "M", "expand", 0, "", "", 70, "1-:0.06", `{"Drain to Source Voltage":"20V","Type":"P-Channel"}`},
		{4, 1, "T", "MOSFETs", "N100D", "TO-252", "M", "expand", 0, "", "", 9, "1-:0.30", `{"Drain to Source Voltage":"100V","Type":"N-Channel"}`},
	}
	c := installed(t, rows)
	facet := func(attrs []AttrFilter, key string) Param {
		t.Helper()
		ps, err := c.Params(ctx, Query{Subcategory: "MOSFETs", Attrs: attrs})
		if err != nil {
			t.Fatal(err)
		}
		for _, p := range ps {
			if p.Key == key {
				return p
			}
		}
		return Param{}
	}
	values := func(p Param) string {
		var s []string
		for _, v := range p.Values {
			s = append(s, fmt.Sprintf("%s:%d", v.Value, v.Count))
		}
		return strings.Join(s, " ")
	}
	vds30 := AttrFilter{Key: "Drain to Source Voltage", Op: ">=", Input: "30V"}
	// After VDS ≥ 30 V only the packages of N30A, N60B and N100D remain.
	if got := values(facet([]AttrFilter{vds30}, PackageKey)); got != "SOT-23:1 SOT-23-3:1 TO-252:1" {
		t.Errorf("packages after VDS ≥ 30 V = %q", got)
	}
	// Picking a package keeps its alternatives listed (its own filter is
	// left out), while the other parameters narrow to the picked package.
	sot := AttrFilter{Key: PackageKey, Op: "=", Input: "SOT-23"}
	if got := values(facet([]AttrFilter{vds30, sot}, PackageKey)); got != "SOT-23:1 SOT-23-3:1 TO-252:1" {
		t.Errorf("packages with SOT-23 picked = %q", got)
	}
	if p := facet([]AttrFilter{sot}, "Drain to Source Voltage"); p.Min != 20 || p.Max != 30 || p.Count != 2 {
		t.Errorf("VDS range in SOT-23 = %+v", p)
	}
	if got := values(facet([]AttrFilter{sot}, "Type")); got != "N-Channel:1 P-Channel:1" {
		t.Errorf("types in SOT-23 = %q", got)
	}
}

func TestNumericEqualsAndCommonValues(t *testing.T) {
	ctx := context.Background()
	rows := [][]any{
		{1, 1, "R", "Chip Resistor - Surface Mount", "R10K", "0402", "U", "base", 0, "", "", 100, "1-:0.001", `{"Resistance":"10kΩ","Tolerance":"±1%"}`},
		{2, 1, "R", "Chip Resistor - Surface Mount", "R10K0", "0603", "U", "base", 0, "", "", 90, "1-:0.001", `{"Resistance":"10.0kΩ","Tolerance":"±0.1%"}`},
		{3, 1, "R", "Chip Resistor - Surface Mount", "R4K7", "0402", "U", "base", 0, "", "", 80, "1-:0.001", `{"Resistance":"4.7kΩ","Tolerance":"±1%"}`},
		{4, 1, "R", "Chip Resistor - Surface Mount", "R10K2", "0402", "U", "base", 0, "", "", 70, "1-:0.001", `{"Resistance":"10.2kΩ","Tolerance":"±1%"}`},
	}
	c := installed(t, rows)
	items, err := c.Search(ctx, Query{Subcategory: "Chip Resistor - Surface Mount", Attrs: []AttrFilter{
		{Key: "Resistance", Op: "=", Input: "10k"},
		{Key: "Tolerance", Op: "<=", Input: "1%"},
	}})
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, it := range items {
		got = append(got, it.MPN)
	}
	if strings.Join(got, " ") != "R10K R10K0" {
		t.Errorf("10k ±≤1%% = %v", got)
	}
	ps, _ := c.Params(ctx, Query{Subcategory: "Chip Resistor - Surface Mount"})
	for _, p := range ps {
		if p.Key == "Resistance" && (p.Kind != "number" || len(p.Values) != 4) {
			t.Errorf("resistance suggests its values: %+v", p)
		}
	}
}
