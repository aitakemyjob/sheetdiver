package extract

import (
	"slices"
	"testing"
)

// hrow is a header row of 8-point words at a height.
func hrow(y float64, words ...any) Row {
	var r Row
	for i := 0; i+1 < len(words); i += 2 {
		sp := tspan(words[i].(float64), y, 8, words[i+1].(string))
		if r.Spans == nil {
			r = trow(sp)
		} else {
			r.Spans = append(r.Spans, sp)
			r.Box = r.Box.Union(sp.Box)
		}
	}
	return r
}

func TestIsPinHeaderRow(t *testing.T) {
	for _, tc := range []struct {
		row  Row
		want bool
	}{
		{hrow(100, 60.0, "PIN", 120.0, "NAME", 300.0, "DESCRIPTION"), true},
		{hrow(100, 60.0, "No.", 120.0, "Symbol", 300.0, "Function"), true},
		{hrow(100, 60.0, "20-PIN", 120.0, "TQFN"), true},
		{hrow(100, 60.0, "1", 120.0, "VIN", 300.0, "Input supply"), false},
		{Row{}, false},
	} {
		if got := isPinHeaderRow(tc.row); got != tc.want {
			t.Errorf("%v: %v, want %v", rowText(tc.row), got, tc.want)
		}
	}
}

func TestPinHeaderRows(t *testing.T) {
	rows := []Row{
		trow(tspan(60, 80, 12, "Pin Description")), // a larger title above
		hrow(95, 100.0, "PIN"),
		hrow(105, 60.0, "NAME", 120.0, "NO.", 300.0, "DESCRIPTION"),
		hrow(130, 60.0, "1", 120.0, "VIN"), // the first data row
	}
	if lo, hi, ok := pinHeaderRows(rows, 1); !ok || lo != 1 || hi != 2 {
		t.Errorf("stacked header = %d..%d %v, want 1..2", lo, hi, ok)
	}
	if _, _, ok := pinHeaderRows(rows, 3); ok {
		t.Error("a data row isn't a header")
	}
	// A title in a larger size directly above a header row isn't part of it.
	big := []Row{trow(tspan(60, 80, 12, "Pin")), hrow(92, 60.0, "NAME", 300.0, "DESCRIPTION")}
	if _, _, ok := pinHeaderRows(big, 0); ok {
		t.Error("a larger title isn't a header row")
	}
}

func TestStackPinWords(t *testing.T) {
	ws := append(headerWords(hrow(95, 70.0, "PIN")), headerWords(hrow(105, 60.0, "NAME", 120.0, "NO.", 300.0, "DESCRIPTION"))...)
	groups := stackPinWords(ws, true)
	var roles []string
	for _, g := range groups {
		roles = append(roles, g.role)
	}
	if !slices.Equal(roles, []string{"name", "number", "description"}) {
		t.Errorf("roles = %v (PIN spans NAME and NO.)", roles)
	}
	// Without a NAME column, PIN is a column of its own.
	groups = stackPinWords(headerWords(hrow(100, 60.0, "PIN", 300.0, "DESCRIPTION")), false)
	if len(groups) != 2 || groups[0].role != "pin" {
		t.Errorf("groups = %+v", groups)
	}
}

func TestResolvePinLabels(t *testing.T) {
	g := []*pinGroup{{role: "pin"}, {role: "description"}}
	resolvePinLabels(g, true)
	if g[0].role != "name" {
		t.Errorf("PIN beside a number column is the name: %q", g[0].role)
	}
	g = []*pinGroup{{role: "pin"}, {role: "name"}}
	resolvePinLabels(g, false)
	if g[0].role != "number" {
		t.Errorf("PIN without a number column is the number: %q", g[0].role)
	}
}

func TestPinColumns(t *testing.T) {
	ws := append(headerWords(hrow(95, 60.0, "20-PIN", 120.0, "20-BUMP")), headerWords(hrow(105, 60.0, "TQFN", 120.0, "WLP", 180.0, "NAME", 300.0, "DESCRIPTION"))...)
	cols := pinColumns(stackPinWords(ws, true))
	var names []string
	for _, c := range cols {
		names = append(names, c.name)
	}
	if !slices.Equal(names, []string{"20-PIN TQFN", "20-BUMP WLP", "NAME", "DESCRIPTION"}) {
		t.Errorf("names = %v (package columns are named by their stacked labels)", names)
	}
	if cols[0].x0 != -1 || cols[3].x1 != 1e9 || cols[0].x1 != cols[1].x0 {
		t.Errorf("columns = %+v", cols)
	}
	// The description starts just after the name column, not halfway.
	if cols[3].x0 > 210 || cols[2].x1 != cols[3].x0 {
		t.Errorf("description starts at %v", cols[3].x0)
	}
	// One number column keeps its role name.
	cols = pinColumns(stackPinWords(headerWords(hrow(100, 60.0, "NO.", 120.0, "NAME", 300.0, "DESCRIPTION")), true))
	if cols[0].name != "NUMBER" {
		t.Errorf("single number column = %q", cols[0].name)
	}
}

func TestPinHeaderRejects(t *testing.T) {
	// No description column: not a pin table.
	if _, _, ok := pinHeader([]Row{hrow(100, 60.0, "NAME", 120.0, "NO.")}, 0); ok {
		t.Error("a header without a description")
	}
	// Too many columns: a package matrix, not a pin list.
	var words []any
	for i := 0; i < 9; i++ {
		words = append(words, 40.0+float64(i)*50, "WLP")
	}
	words = append(words, 490.0, "NAME", 540.0, "DESCRIPTION")
	if _, _, ok := pinHeader([]Row{hrow(100, words...)}, 0); ok {
		t.Error("more than eight columns")
	}
	// Eight columns are fine.
	if _, _, ok := pinHeader([]Row{hrow(100, append(words[:12:12], 490.0, "NAME", 540.0, "DESCRIPTION")...)}, 0); !ok {
		t.Error("eight columns")
	}
}
