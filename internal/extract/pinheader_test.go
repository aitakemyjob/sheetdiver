package extract

import (
	"slices"
	"strings"
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

// pinsOf reads a pin table from its header rows and body rows.
func pinsOf(t *testing.T, rows []Row, headerRows int) []Pin {
	t.Helper()
	cols, last, ok := pinHeader(rows, 0)
	if !ok || last != headerRows-1 {
		t.Fatalf("header not found: %v, last %d", ok, last)
	}
	_, pins := pinTable(cols, rows[0], rows[headerRows:], 3, "Pin Functions")
	return pins
}

// pinList is "package number name" for each pin.
func pinList(pins []Pin) []string {
	var out []string
	for _, p := range pins {
		out = append(out, strings.Join(strings.Fields(p.Package+" "+p.Number+" "+p.Name), " "))
	}
	return out
}

func TestPinTableOnePackagePerPart(t *testing.T) {
	// The TPS2121's table: a column per part and package, under three
	// header rows; OUT's numbers set above and below its name (#75, #76).
	rows := []Row{
		hrow(493, 125.0, "PIN"),
		hrow(507, 115.0, "TPS2120", 163.0, "TPS2121", 226.0, "I/O", 379.0, "DESCRIPTION"),
		hrow(514, 59.0, "NAME"),
		hrow(521, 119.0, "WCSP", 163.0, "VQFN-HR"),
		hrow(535, 59.0, "IN1", 112.0, "B1, B2, C1", 179.0, "7", 230.0, "I", 259.0, "Power Input for Source 1"),
		hrow(563, 111.0, "C2, C3, D1,", 175.0, "1, 8"),
		hrow(568, 59.0, "OUT", 230.0, "I", 259.0, "Power Output"),
		hrow(573, 112.0, "D2, D3, D4"),
		hrow(586, 59.0, "ST", 127.0, "E1", 179.0, "9", 228.0, "O", 259.0, "Status output"),
		hrow(717, 59.0, "SEL", 126.0, "A4", 177.0, "—", 230.0, "I", 259.0, "TPS2120 only."),
	}
	got := pinList(pinsOf(t, rows, 4))
	want := []string{
		"TPS2120 WCSP B1 IN1", "TPS2120 WCSP B2 IN1", "TPS2120 WCSP C1 IN1", "TPS2121 VQFN-HR 7 IN1",
		"TPS2120 WCSP C2 OUT", "TPS2120 WCSP C3 OUT", "TPS2120 WCSP D1 OUT", "TPS2120 WCSP D2 OUT", "TPS2120 WCSP D3 OUT", "TPS2120 WCSP D4 OUT",
		"TPS2121 VQFN-HR 1 OUT", "TPS2121 VQFN-HR 8 OUT",
		"TPS2120 WCSP E1 ST", "TPS2121 VQFN-HR 9 ST",
		"TPS2120 WCSP A4 SEL",
	}
	if !slices.Equal(got, want) {
		t.Errorf("pins =\n%q\nwant\n%q", got, want)
	}
}

func TestPinTablePackageColumns(t *testing.T) {
	// The TPD2E2U06's: DRL and DCK columns; NC on two pins in DRL only.
	// The header row naming the packages isn't a pin.
	rows := []Row{
		hrow(419, 123.0, "PIN"),
		hrow(425, 225.0, "I/O", 380.0, "DESCRIPTION"),
		hrow(432, 57.0, "NAME", 122.0, "DRL", 172.0, "DCK"),
		hrow(445, 57.0, "IO1", 127.0, "3", 178.0, "1", 225.0, "I/O", 259.0, "ESD protected channel"),
		hrow(472, 57.0, "NC", 123.0, "1, 2", 176.0, "—", 259.0, "Not connected"),
		hrow(485, 57.0, "GND", 127.0, "4", 178.0, "3", 227.0, "G", 259.0, "Ground"),
	}
	got := pinList(pinsOf(t, rows, 3))
	want := []string{"DRL 3 IO1", "DCK 1 IO1", "DRL 1 NC", "DRL 2 NC", "DRL 4 GND", "DCK 3 GND"}
	if !slices.Equal(got, want) {
		t.Errorf("pins = %q, want %q", got, want)
	}
	// The MAX17048's: TDFN and WLP under PIN/BUMP; the exposed pad, a dash
	// in both, belongs to the package its description names.
	rows = []Row{
		hrow(411, 76.0, "PIN/BUMP"),
		hrow(417, 150.0, "NAME", 351.0, "FUNCTION"),
		hrow(424, 64.0, "TDFN", 108.0, "WLP"),
		hrow(437, 74.0, "1", 112.0, "A1", 154.0, "CTG", 192.0, "Connect to Ground"),
		hrow(618, 71.0, "—", 113.0, "—", 157.0, "EP", 192.0, "Exposed Pad (TDFN Only). Connect to GND."),
	}
	got = pinList(pinsOf(t, rows, 3))
	want = []string{"TDFN 1 CTG", "WLP A1 CTG", "TDFN EP"}
	if !slices.Equal(got, want) {
		t.Errorf("pins = %q, want %q", got, want)
	}
}

func TestPinNumbers(t *testing.T) {
	for cell, want := range map[string][]string{
		"4, 5, 6":                      {"4", "5", "6"},
		"4–6":                          {"4", "5", "6"},
		"B1, B2, C1":                   {"B1", "B2", "C1"},
		"1, 2, 3, exposed thermal pad": {"1", "2", "3", "PAD"},
		"PAD":                          {"PAD"},
		"—":                            nil,
		"":                             nil,
		"17":                           {"17"},
		"3,4":                          {"3", "4"},
	} {
		if got := pinNumbers(cell); !slices.Equal(got, want) {
			t.Errorf("pinNumbers(%q) = %q, want %q", cell, got, want)
		}
	}
	// The TVS0500's: pin ranges, and the pad's words wrapped onto a second line.
	rows := []Row{
		hrow(291, 127.0, "PIN"),
		hrow(298, 265.0, "TYPE", 422.0, "DESCRIPTION"),
		hrow(305, 71.0, "NAME", 155.0, "No."),
		hrow(318, 58.0, "IN", 150.0, "4, 5, 6", 275.0, "I", 343.0, "ESD and surge protected channel"),
		hrow(331, 118.0, "1, 2, 3, exposed thermal"),
		hrow(335, 57.0, "GND", 267.0, "GND", 342.0, "Ground"),
		hrow(340, 155.0, "pad"),
	}
	got := pinList(pinsOf(t, rows, 3))
	want := []string{"4 IN", "5 IN", "6 IN", "1 GND", "2 GND", "3 GND", "PAD GND"}
	if !slices.Equal(got, want) {
		t.Errorf("pins = %q, want %q", got, want)
	}
}
