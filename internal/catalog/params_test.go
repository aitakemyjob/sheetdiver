package catalog

import (
	"context"
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
	if err := checkFilters(fs); err != nil || fs[0].Value != 0.01 || fs[1].Text != "Buck" {
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
