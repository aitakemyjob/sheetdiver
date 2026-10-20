package extract

import (
	"testing"

	"github.com/aitakemyjob/sheetdiver/internal/pdf"
)

// srow builds a spec row at a height with cells, marked as a value row
// when it has one.
func srow(y float64, value bool, cells ...string) specRow {
	return specRow{row: Row{Y: y, Box: pdf.Rect{X0: 50, Y0: y - 4, X1: 550, Y1: y + 4}}, cells: cells, value: value}
}

// specCols are the columns of a typical electrical table.
var specCols = []column{{role: "name"}, {role: "conditions"}, {role: "min"}, {role: "typ"}, {role: "max"}, {role: "unit"}}

func TestColumnIndex(t *testing.T) {
	idx := columnIndex([]column{{role: "name"}, {role: "min"}, {role: "name"}, {role: "unit"}})
	if idx["name"] != 0 || idx["min"] != 1 || idx["unit"] != 3 || len(idx) != 3 {
		t.Errorf("idx = %v (the first column of a role wins)", idx)
	}
}

func TestReadSpecRows(t *testing.T) {
	cols := []column{{role: "name", x0: -1, x1: 300}, {role: "value", x0: 300, x1: 450}, {role: "unit", x0: 450, x1: 1e9}}
	rows := readSpecRows(cols, columnIndex(cols), []Row{
		trow(tspan(60, 100, 10, "Input voltage"), tspan(320, 100, 10, "-0.3~7"), tspan(460, 100, 10, "V")),
		trow(tspan(60, 115, 10, "OC pin voltage"), tspan(320, 115, 10, "VDD-24"), tspan(460, 115, 10, "V")),
	})
	if len(rows) != 2 || !rows[0].value || rows[1].value || rows[1].cells[1] != "VDD-24" {
		t.Errorf("rows = %+v", rows)
	}
}

func TestAttachTextRows(t *testing.T) {
	rows := []specRow{
		srow(100, false, "0V battery charge starting", "", "", "", "", ""), // wrapped name: above its row
		srow(110, true, "voltage", "", "1.2", "", "", "V"),
		srow(125, false, "SUPPLY CURRENT", "", "", "", "", ""), // group label
		srow(140, true, "Supply current", "VCC = 3.6 V", "", "3", "6", "µA"),
		srow(152, false, "", "ICO = 10 µA", "", "", "", ""),           // more conditions
		srow(170, false, "OC pin voltage", "", "VDD-24", "", "", "V"), // its own parameter
		srow(260, false, "Far away", "", "", "", "", ""),              // too far from any value row
	}
	band := make([]int, len(rows))
	extra, attached := attachTextRows(rows, band, specCols)
	if len(extra[1]) != 1 || extra[1][0].cells[0] != "0V battery charge starting" || len(extra[3]) != 1 {
		t.Errorf("extra = %+v", extra)
	}
	for i, want := range []bool{true, false, false, false, true, false, false} {
		if attached[i] != want {
			t.Errorf("row %d attached = %v, want %v", i, attached[i], want)
		}
	}
	// Bands (ruled groups) keep rows apart.
	band[0] = 1
	if extra, _ := attachTextRows(rows, band, specCols); len(extra[1]) != 0 {
		t.Errorf("a row in another band isn't attached: %+v", extra)
	}
}

func TestFillMergedUnits(t *testing.T) {
	rows := []specRow{
		srow(100, true, "Overcharge delay", "", "", "110", "200", "ms"),
		srow(115, true, "Overdischarge delay", "", "", "55", "200", ""),
		srow(130, true, "Overcurrent delay", "", "", "7", "20", ""),
		srow(300, true, "Far", "", "", "1", "2", ""),
	}
	fillMergedUnits(rows, []int{0, 0, 0, 0}, 5)
	for i, want := range []string{"ms", "ms", "ms", ""} {
		if rows[i].cells[5] != want {
			t.Errorf("row %d unit = %q, want %q", i, rows[i].cells[5], want)
		}
	}
}

func TestBandNames(t *testing.T) {
	rows := []specRow{
		srow(100, true, "", "VIN = 5 V", "1", "", "", "A"),
		srow(110, false, "Current limit", "", "", "", "", ""),
		srow(120, true, "", "VIN = 12 V", "1.1", "", "", "A"),
		srow(140, false, "SUPPLY", "", "", "", "", ""),
	}
	idx := columnIndex(specCols)
	if names := bandNames(rows, []int{0, 0, 0, 1}, idx, nil); len(names) != 0 {
		t.Errorf("without rules there are no groups: %v", names)
	}
	names := bandNames(rows, []int{0, 0, 0, 1}, idx, []pdf.Rect{{}})
	if names[0] != "Current limit" || names[1] != "" {
		t.Errorf("names = %v", names)
	}
}

func TestMergeGroup(t *testing.T) {
	cols := specCols
	value := srow(110, true, "voltage", "", "1.2", "", "", "V")
	cells, box := mergeGroup(value, []specRow{srow(100, false, "0V battery charge starting", "", "9", "", "", "mV")}, cols)
	if cells[0] != "0V battery charge starting voltage" || cells[2] != "1.2" || cells[5] != "V" {
		t.Errorf("cells = %q (text joins top to bottom; values and units come from the value row)", cells)
	}
	if box.Y0 != 96 || box.Y1 != 114 {
		t.Errorf("box = %+v", box)
	}
}

func TestNameRow(t *testing.T) {
	prev := Param{}
	n, sym, prev := nameRow("VFB Feedback voltage", "", prev, nil, 0)
	if n != "Feedback voltage" || sym != "VFB" {
		t.Errorf("a symbol at the start of the name: %q %q", n, sym)
	}
	// Another condition of the same parameter.
	n, sym, prev = nameRow("", "", prev, nil, 0)
	if n != "Feedback voltage" || sym != "VFB" {
		t.Errorf("inherited: %q %q", n, sym)
	}
	// A ruled group's name wins.
	n, _, _ = nameRow("", "", prev, map[int]string{2: "Reference voltage"}, 2)
	if n != "Reference voltage" {
		t.Errorf("group name: %q", n)
	}
	n, sym, _ = nameRow("Quiescent current", "IQ", prev, nil, 0)
	if n != "Quiescent current" || sym != "IQ" {
		t.Errorf("own cells: %q %q", n, sym)
	}
}

func TestReadLimits(t *testing.T) {
	cells := func(m map[string]string) func(string) string { return func(r string) string { return m[r] } }
	f := func(v *float64) any {
		if v == nil {
			return nil
		}
		return *v
	}
	for _, tc := range []struct {
		name    string
		cells   map[string]string
		sec     string
		min     any
		typ     any
		max     any
		unit    string
		conds   string
		initial string
	}{
		{"min typ max", map[string]string{"min": "4.25", "typ": "4.30", "max": "4.35", "unit": "V"}, KindElectrical, 4.25, 4.3, 4.35, "V", "", ""},
		{"rating range", map[string]string{"value": "-0.3~7", "unit": "V"}, KindAbsMax, -0.3, nil, 7.0, "V", "", ""},
		{"lone rating is a maximum", map[string]string{"value": "150", "unit": "°C"}, KindAbsMax, nil, nil, 150.0, "°C", "", ""},
		{"lone value elsewhere is typical", map[string]string{"value": "500", "unit": "kHz"}, KindRecommended, nil, 500.0, nil, "kHz", "", ""},
		{"tolerance around typ", map[string]string{"min": "-3%", "typ": "0.5", "max": "+3%", "unit": "V"}, KindElectrical, 0.485, 0.5, 0.515, "V", "(min -3%) (max +3%)", ""},
		{"percent unit stays", map[string]string{"min": "-1%", "typ": "0", "max": "1%", "unit": "%"}, KindElectrical, -1.0, 0.0, 1.0, "%", "", ""},
		{"conditions kept", map[string]string{"min": "-2%", "typ": "1.2", "unit": "V"}, KindElectrical, 1.176, 1.2, nil, "V", "TA = 25°C (min -2%)", "TA = 25°C"},
	} {
		p := Param{Conditions: tc.initial}
		readLimits(&p, cells(tc.cells), tc.sec)
		if f(p.Min) != tc.min || f(p.Typ) != tc.typ || f(p.Max) != tc.max || p.Unit != tc.unit {
			t.Errorf("%s: %v/%v/%v %s, want %v/%v/%v %s", tc.name, f(p.Min), f(p.Typ), f(p.Max), p.Unit, tc.min, tc.typ, tc.max, tc.unit)
		}
		if tc.conds != "" && p.Conditions != tc.conds {
			t.Errorf("%s: conditions %q, want %q", tc.name, p.Conditions, tc.conds)
		}
	}
}
