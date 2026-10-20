package extract

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/aitakemyjob/sheetdiver/internal/pdf"
	"github.com/aitakemyjob/sheetdiver/internal/testutil"
)

// analyzeSample runs the whole pipeline on the synthetic datasheet.
func analyzeSample(t *testing.T) *Result {
	t.Helper()
	e := pdf.NewEngine(pdf.DefaultCacheDir())
	t.Cleanup(func() { e.Close() })
	doc, err := e.Extract(context.Background(), testutil.Datasheet(), nil)
	if err != nil {
		t.Fatal(err)
	}
	return Analyze(doc, "sd1234.pdf")
}

func TestAnalyzeSyntheticDatasheet(t *testing.T) {
	r := analyzeSample(t)
	if !slices.Equal(r.PartNumbers, []string{"SD1234", "SD1235"}) {
		t.Errorf("parts = %v", r.PartNumbers)
	}
	if !slices.Equal(r.Families, []string{"SD123x"}) {
		t.Errorf("families = %v", r.Families)
	}
	bases := map[string]string{}
	for _, v := range r.Variants {
		bases[v.MPN] = v.Base
	}
	if bases["SD1234DDCR"] != "SD1234" || bases["SD1235DDCR"] != "SD1235" {
		t.Errorf("variants = %v", r.Variants)
	}
	if r.Category != "buck" {
		t.Errorf("category = %q", r.Category)
	}
	kinds := map[string]bool{}
	for _, s := range r.Sections {
		kinds[s.Kind] = true
	}
	for _, k := range []string{KindPins, KindAbsMax, KindRecommended, KindElectrical, KindTypical} {
		if !kinds[k] {
			t.Errorf("no %s section in %+v", k, r.Sections)
		}
	}
	if len(r.Figures) != 1 || r.Figures[0].Number != "6-1" || r.Figures[0].Kind != FigGraph || r.Figures[0].XAxis != "Output Current" {
		t.Fatalf("figures = %+v", r.Figures)
	}
	// The figure region covers the plot, not the heading above it.
	if b := r.Figures[0].Box; b.Y0 < 210 || b.Y1 < 400 || b.X0 > 101 {
		t.Errorf("figure box = %+v", b)
	}
	byKey := map[string]Param{}
	for _, p := range r.Params {
		if _, dup := byKey[p.Key+p.Section]; !dup {
			byKey[p.Key+p.Section] = p
		}
	}
	vref := byKey["vref"+KindElectrical]
	if vref.Typ == nil || *vref.Typ != 0.765 || vref.Unit != "V" || vref.Conditions != "TA = 25°C" {
		t.Errorf("vref = %+v", vref)
	}
	if p := byKey["vin"+KindAbsMax]; p.Max == nil || *p.Max != 20 {
		t.Errorf("abs max vin = %+v", p)
	}
	if p := byKey["vin"+KindRecommended]; p.Min == nil || *p.Min != 4.5 || *p.Max != 17 {
		t.Errorf("recommended vin = %+v", p)
	}
	if p := byKey["fsw"+KindElectrical]; p.Typ == nil || *p.Typ != 650 || p.Unit != "kHz" {
		t.Errorf("fsw = %+v", p)
	}
	if p := byKey["ton_min"+KindElectrical]; p.Max == nil || *p.Max != 90 {
		t.Errorf("ton_min = %+v", p)
	}
	if len(r.Pins) != 4 || r.Pins[2].Name != "VIN" || r.Pins[2].Number != "3" || r.Pins[2].Type != "P" {
		t.Errorf("pins = %+v", r.Pins)
	}
	// A "–3% / 5.11 / +3%" row is a tolerance around typ.
	for _, p := range r.Params {
		if strings.HasPrefix(p.Name, "RSET") && (p.Min == nil || *p.Min != 4.9567 || *p.Max != 5.2633) {
			t.Errorf("tolerance limits = %+v", p)
		}
	}
	tables := map[string]Table{}
	for _, tb := range r.Tables {
		tables[tb.Number] = tb
	}
	if tb := tables["7-1"]; tb.Caption != "Output Voltage Settings" || tb.Irregular == "" {
		t.Errorf("the selection matrix is irregular, and prose mentioning it is not its caption: %+v", tb)
	}
	if tb := tables["7-1"]; tb.Box.Y0 > 205 {
		t.Errorf("the table region includes its caption: %+v", tb.Box)
	}
	if tb := tables["8-1"]; tb.Irregular != "" || len(tb.Rows) != 3 {
		t.Errorf("the inductor list is a plain grid: %+v", tb)
	}
}

func TestParseNumber(t *testing.T) {
	cases := map[string]float64{"–0.3": -0.3, "−40": -40, "1.5 (2)": 1.5, "650": 650, ".5": 0.5, "2,5": 2.5, "1e3": 1000}
	for in, want := range cases {
		if v, ok := ParseNumber(in); !ok || v != want {
			t.Errorf("ParseNumber(%q) = %v, %v; want %v", in, v, ok, want)
		}
	}
	for _, in := range []string{"", "(1)", "–––", "V", "MIN"} {
		if _, ok := ParseNumber(in); ok {
			t.Errorf("ParseNumber(%q) should fail", in)
		}
	}
}

func TestUnits(t *testing.T) {
	cases := map[string]string{"uA": "µA", "m Ω": "mΩ", "––– V": "V", "ºC": "°C", "μs": "µs", "Ω": "Ω", "V (1)": "V"}
	for in, want := range cases {
		if got := NormalizeUnit(in); got != want {
			t.Errorf("NormalizeUnit(%q) = %q, want %q", in, got, want)
		}
	}
	if v, u, ok := ToSI(650, "kHz"); !ok || v != 650e3 || u != "Hz" {
		t.Errorf("ToSI kHz = %v %v %v", v, u, ok)
	}
	if v, u, ok := ToSI(68, "mΩ"); !ok || v != 0.068 || u != "Ω" {
		t.Errorf("ToSI mΩ = %v %v %v", v, u, ok)
	}
	if _, _, ok := ToSI(1, "furlong"); ok {
		t.Error("unknown unit should not convert")
	}
	if symbolFontOhms("mW", "R DS(ON) Static Drain-Source On-Resistance") != "mΩ" || symbolFontOhms("mW", "Power dissipation") != "mW" {
		t.Error("Symbol-font ohms repair")
	}
}

func TestGuessKey(t *testing.T) {
	cases := []struct{ text, unit, want string }{
		{"VFB Feedback voltage", "V", "vref"},
		{"fSW Switching frequency", "kHz", "fsw"},
		{"RDS(on) Drain-to-source on resistance", "mΩ", "rdson"},
		{"High-side switch resistance RDS(on)h", "mΩ", "rdson_hs"},
		{"VGS(th) Gate threshold voltage", "V", "vgs_th"},
		{"Qg Total gate charge", "nC", "qg"},
		{"Junction-to-ambient thermal resistance", "°C/W", "rth_ja"},
		{"tON(MIN) Minimum on time", "ns", "ton_min"},
		{"Input voltage", "V", "vin"},
		// A key needs a matching unit: a misread cell must not become vin.
		{"Input voltage", "", ""},
		{"Input voltage", "mA", ""},
		{"Peak current limit during Startup", "A", "ilim_startup"},
		{"Average input current limit", "mA", "ilim_in"},
		{"Peak current limit (Q1)", "A", "ilim"},
	}
	for _, c := range cases {
		if got := GuessKey(c.text, c.unit, ""); got != c.want {
			t.Errorf("GuessKey(%q, %q) = %q, want %q", c.text, c.unit, got, c.want)
		}
	}
}

func TestKinds(t *testing.T) {
	if SectionKind("6.1 Absolute Maximum Ratings") != KindAbsMax || SectionKind("8.2.1.2.3 Output Filter Selection") != KindComponents || SectionKind("7.6 Register Maps") != KindRegisters {
		t.Error("section kinds")
	}
	if FigureKind("Efficiency vs Output Current") != FigGraph || FigureKind("Typical Application Schematic") != FigSchematic || FigureKind("Output Voltage Ripple") != FigWaveform {
		t.Error("figure kinds")
	}
	if y, x := graphAxes("TPS563200 Efficiency vs Output Current, VIN = 12 V"); y != "Efficiency" || x != "Output Current" {
		t.Errorf("axes = %q, %q", y, x)
	}
	if Category("STM32U545xx Ultra-low-power Arm Cortex-M33 MCU", "", "integrated SMPS step-down converter") != "mcu" {
		t.Error("the title outranks the body text")
	}
}

func TestFamiliesAndVariants(t *testing.T) {
	re := FamilyRegex("STM32U545xx")
	if !re.MatchString("STM32U545RE") || re.MatchString("STM32U545RET6") {
		t.Error("family regex")
	}
	pages := []string{"TLV75733PDBVR TLV75712PDBVR LM317 TPS61023AUGUST", "BQ24072/3/4/5 TLV757P"}
	bases, vars := Expand(pages, []string{"TLV757P", "TPS61023"}, nil)
	got := map[string]string{}
	for _, v := range vars {
		got[v.MPN] = v.Base
	}
	if got["TLV75733PDBVR"] != "TLV757P" {
		t.Errorf("voltage-coded variant not found: %v", vars)
	}
	if _, bad := got["TPS61023AUGUST"]; bad {
		t.Error("a date glued to a part number is not a variant")
	}
	if !slices.Contains(bases, "TLV757P") {
		t.Errorf("bases = %v", bases)
	}
	if isPartToken("SLVSCB0E") || isPartToken("SOT-23") || !isPartToken("TPS563200") {
		t.Error("part token filter")
	}
}

func TestSpecHeaderRejectsDataRows(t *testing.T) {
	row := Row{Spans: []pdf.Span{{Text: "tON_min Minimum on time", Box: pdf.Rect{X0: 60, X1: 160}}, {Text: "40", Box: pdf.Rect{X0: 410, X1: 420}}, {Text: "96", Box: pdf.Rect{X0: 455, X1: 465}}}}
	if _, ok := specHeader(row); ok {
		t.Error("a row with values is not a header")
	}
	hdr := Row{Spans: []pdf.Span{{Text: "PARAMETER", Box: pdf.Rect{X0: 60, X1: 110}}, {Text: "MIN", Box: pdf.Rect{X0: 400, X1: 415}}, {Text: "MAX", Box: pdf.Rect{X0: 450, X1: 466}}, {Text: "Conditions", Box: pdf.Rect{X0: 520, X1: 560}}}}
	cols, ok := specHeader(hdr)
	if !ok || cols[len(cols)-1].role != "conditions" {
		t.Errorf("text columns right of the numbers: %+v", cols)
	}
}

func TestSpecTableBandsAndSplitRows(t *testing.T) {
	// TI layout: a parameter's name is centred across its condition rows,
	// rules crossing the name column separate parameters, and typ sits
	// half a line above min/max.
	span := func(x, y float64, s string) pdf.Span {
		return pdf.Span{Text: s, Size: 8, Box: pdf.Rect{X0: x, Y0: y - 3, X1: x + float64(len(s))*4, Y1: y + 3}}
	}
	row := func(y float64, spans ...pdf.Span) Row {
		r := Row{Y: y, Box: spans[0].Box, Spans: spans}
		for _, s := range spans {
			r.Box = r.Box.Union(s.Box)
		}
		return r
	}
	header := row(100, span(60, 100, "PARAMETER"), span(200, 100, "TEST CONDITIONS"), span(400, 100, "MIN"), span(450, 100, "TYP"), span(500, 100, "MAX"), span(540, 100, "UNIT"))
	cols, ok := specHeader(header)
	if !ok {
		t.Fatal("header")
	}
	body := []Row{
		row(115, span(60, 115, "Startup current limit"), span(400, 115, "0.35"), span(500, 115, "0.83"), span(540, 115, "A")),
		row(130, span(200, 130, "VI = 1.8 V"), span(450, 130, "1.45")),
		row(135, span(400, 135, "1.33"), span(500, 135, "1.6")),
		row(142, span(60, 142, "Peak current limit"), span(540, 142, "A")),
		row(150, span(200, 150, "100-mA setting"), span(450, 150, "0.29")),
		row(155, span(400, 155, "0.15"), span(500, 155, "0.51")),
	}
	rules := []pdf.Rect{{X0: 56, Y0: 122, X1: 556, Y1: 122.5}, {X0: 56, Y0: 160, X1: 556, Y1: 160.5}}
	_, ps := specTable(cols, header, body, 0, KindElectrical, "", rules)
	if len(ps) != 3 {
		t.Fatalf("params = %+v", ps)
	}
	if ps[1].Name != "Peak current limit" || *ps[1].Min != 1.33 || *ps[1].Typ != 1.45 || *ps[1].Max != 1.6 || ps[1].Unit != "A" || ps[1].Key != "ilim" {
		t.Errorf("merged row = %+v", ps[1])
	}
	if ps[0].Key != "ilim_startup" || ps[2].Conditions != "100-mA setting" {
		t.Errorf("rows = %+v", ps)
	}
}

// tspan builds a span for table tests (4 pt per character).
func tspan(x, y, size float64, s string) pdf.Span {
	return pdf.Span{Text: s, Size: size, Box: pdf.Rect{X0: x, Y0: y - size/2, X1: x + float64(len([]rune(s)))*size*0.5, Y1: y + size/2}}
}

// trow builds a row from spans.
func trow(spans ...pdf.Span) Row {
	r := Row{Y: centerY(spans[0].Box), Box: spans[0].Box, Spans: spans}
	for _, s := range spans {
		r.Box = r.Box.Union(s.Box)
	}
	return r
}

func TestMultiPackagePinTable(t *testing.T) {
	// Maxim layout: a larger "Pin Description" title, then a stacked
	// header with one number column per package, pins listed per package.
	rows := []Row{
		trow(tspan(55, 50, 12, "Pin Description")),
		trow(tspan(60, 70, 8, "20-BUMP"), tspan(120, 70, 8, "20-PIN")),
		trow(tspan(200, 76, 8, "NAME"), tspan(330, 76, 8, "FUNCTION")),
		trow(tspan(66, 82, 8, "WLP"), tspan(124, 82, 8, "TQFN")),
		trow(tspan(66, 96, 8, "A1"), tspan(126, 96, 8, "5"), tspan(200, 96, 8, "VSYS"), tspan(250, 96, 8, "System voltage input.")),
		trow(tspan(60, 110, 8, "A3, B3"), tspan(120, 110, 8, "8, 9"), tspan(200, 110, 8, "AGND"), tspan(250, 110, 8, "Analog ground.")),
		trow(tspan(60, 118, 8, ","), tspan(120, 118, 8, ",")),
		trow(tspan(66, 126, 8, "B1"), tspan(126, 126, 8, "4"), tspan(200, 126, 8, "FB"), tspan(250, 126, 8, "Output voltage sense.")),
	}
	if _, _, ok := pinHeader(rows, 0); ok {
		t.Fatal("a larger title above the table is not its header")
	}
	cols, last, ok := pinHeader(rows, 1)
	if !ok || last != 3 {
		t.Fatalf("header: ok %v last %d", ok, last)
	}
	var names []string
	for _, c := range cols {
		names = append(names, c.name)
	}
	if strings.Join(names, "|") != "20-BUMP WLP|20-PIN TQFN|NAME|DESCRIPTION" {
		t.Fatalf("columns = %v", names)
	}
	_, pins := pinTable(cols, rows[3], rows[4:], 9, "Pin Description")
	if len(pins) != 6 {
		t.Fatalf("one pin per package, no comma rows: %+v", pins)
	}
	want := []Pin{
		{Package: "20-BUMP WLP", Number: "A1", Name: "VSYS"},
		{Package: "20-PIN TQFN", Number: "5", Name: "VSYS"},
		{Package: "20-BUMP WLP", Number: "A3, B3", Name: "AGND"},
		{Package: "20-PIN TQFN", Number: "8, 9", Name: "AGND"},
	}
	for i, w := range want {
		p := pins[i]
		if p.Package != w.Package || p.Number != w.Number || p.Name != w.Name {
			t.Errorf("pin %d = %+v, want %+v", i, p, w)
		}
	}
	if pins[2].Description != "Analog ground." {
		t.Errorf("description = %q", pins[2].Description)
	}
}

func TestCellsJoinSubscripts(t *testing.T) {
	cols := []column{{role: "name", x0: -1, x1: 1e9}}
	base := pdf.Span{Text: "V", Size: 8, Box: pdf.Rect{X0: 60, Y0: 100, X1: 65, Y1: 108}}
	sub := pdf.Span{Text: "SYS", Size: 6, Box: pdf.Rect{X0: 65.3, Y0: 103, X1: 75, Y1: 109}}
	word := pdf.Span{Text: "input", Size: 8, Box: pdf.Rect{X0: 79, Y0: 100, X1: 95, Y1: 108}}
	if got := cells(cols, []pdf.Span{base, sub, word})[0]; got != "VSYS input" {
		t.Errorf("cells = %q", got)
	}
}

func TestIrregularityAndProse(t *testing.T) {
	header := trow(tspan(60, 10, 8, "VOUT"), tspan(200, 10, 8, "CURRENT LIMIT"))
	matrix := []Row{
		trow(tspan(110, 20, 8, "RCFG1"), tspan(300, 20, 8, "0 Ohm")),
		trow(tspan(110, 30, 8, "RCFG2"), tspan(170, 30, 8, "0"), tspan(230, 30, 8, "511"), tspan(290, 30, 8, "1.15k"), tspan(350, 30, 8, "1.87k")),
		trow(tspan(110, 40, 8, "RCFG1"), tspan(300, 40, 8, "511 Ohm")),
		trow(tspan(110, 50, 8, "RCFG2"), tspan(170, 50, 8, "0"), tspan(230, 50, 8, "511"), tspan(290, 50, 8, "1.15k"), tspan(350, 50, 8, "1.87k")),
	}
	if irregularity(header, matrix) == "" {
		t.Error("a selection matrix is irregular")
	}
	grid := []Row{
		trow(tspan(60, 20, 8, "2.2"), tspan(200, 20, 8, "XFL4020")),
		trow(tspan(60, 30, 8, "1.5"), tspan(200, 30, 8, "XFL4020-152")),
	}
	if r := irregularity(header, grid); r != "" {
		t.Errorf("a plain grid is regular: %s", r)
	}
	if irregularity(header, nil) == "" {
		t.Error("a table with no rows read is flagged")
	}
	prose := trow(pdf.Span{Text: "If the output is prebiased, the start-up behaves as follows in every case", Size: 9, Box: pdf.Rect{X0: 60, Y0: 0, X1: 400, Y1: 9}})
	if !isProse(prose) || isProse(grid[0]) {
		t.Error("prose detection")
	}
	if tidyConditions("V I = 3.6 V , , unlimited setting ,") != "V I = 3.6 V, unlimited setting" {
		t.Errorf("tidy = %q", tidyConditions("V I = 3.6 V , , unlimited setting ,"))
	}
}

func TestMOSFETCurrentKeys(t *testing.T) {
	cases := map[string]string{
		"IDSS Zero Gate Voltage Drain Current": "idss",
		"Drain-Source Leakage Current":         "idss",
		"ID(ON) On state drain current":        "id_on",
		"ID Continuous Drain Current":          "id",
		"Continuous drain current, TA = 25°C":  "id",
	}
	for text, want := range cases {
		if got := GuessKey(text, "A", ""); got != want {
			t.Errorf("%q = %q, want %q", text, got, want)
		}
	}
}

func TestRatedCurrent(t *testing.T) {
	lines := func(ts ...string) []Line {
		var out []Line
		for _, s := range ts {
			out = append(out, Line{Text: s})
		}
		return out
	}
	one := func(ps []Param) string {
		var s []string
		for _, p := range ps {
			s = append(s, strings.TrimSpace(fmt.Sprintf("%g %s %s", *p.Max, p.Unit, p.Conditions)))
		}
		return strings.Join(s, "; ")
	}
	cases := []struct {
		name, cat, title string
		page             []string
		parts            []string
		want             string
	}{
		{"per part", "buck", "TPS56x200 4.5-V to 17-V Input, 2-A, 3-A Synchronous Step-down", []string{"• TPS562200 - 2-A converter with integrated", "TPS563200 - 3-A converter with integrated 68-mΩ"}, []string{"TPS562200", "TPS563200"}, "2 A TPS562200; 3 A TPS563200"},
		{"phrase", "buck", "LMR51430 3-A Buck", []string{"3-A continuous output current", "Up to 5-A peak switch current"}, nil, "3 A"},
		{"lowest of several", "buck-boost", "MAX77801 2A", []string{"Up to 2A Output Current in Boost Mode", "Up to 3A Output Current in Buck Mode"}, nil, "2 A"},
		{"after the words", "ldo", "AP2112", []string{"Output Current: 600mA (Min.)"}, nil, "600 mA"},
		{"title", "ldo", "TPS7A02 Nanopower IQ, 25-nA, 200-mA, Low-Dropout Voltage Regulator", []string{"Some text"}, nil, "200 mA"},
		{"boost title is a switch limit", "boost", "TPS61023 3.7-A Boost Converter", []string{"3.7-A Valley switching current limit"}, nil, ""},
		{"not a regulator", "charger", "BQ24074 1.5-A Charger", []string{"1.5-A output current"}, nil, ""},
		{"limits are not ratings", "buck", "XYZ buck", []string{"1.45-A peak switching current limit output current"}, nil, ""},
	}
	for _, c := range cases {
		if got := one(RatedCurrent(c.cat, c.title, lines(c.page...), c.parts)); got != c.want {
			t.Errorf("%s: got %q, want %q", c.name, got, c.want)
		}
	}
}

func TestCurrentKeyRules(t *testing.T) {
	for text, want := range map[string]string{
		"IGND Ground current":            "iq",
		"I Output Short Circuit Current": "isc",
		"VIO SCL, SDA Input Current":     "",
		"IOUT Output current":            "iout",
		"IO Output current":              "iout",
	} {
		if got := GuessKey(text, "mA", ""); got != want {
			t.Errorf("%q = %q, want %q", text, got, want)
		}
	}
}

func TestRangeKeysAndPinRatings(t *testing.T) {
	cases := []struct{ text, unit, want string }{
		{"IN (with respect to VSS)", "V", "vin"},
		{"BAT (with respect to VSS) VI Input Voltage", "V", ""},
		{"TMR, ITERM, SYSOFF, TD (with respect to VSS)", "V", ""},
		{"SW to PGND", "V", ""},
		{"VIN, EN to GND", "V", "vin"},
		{"IN voltage range", "V", "vin"},
		{"IN operating voltage range ‘74", "V", "vin"},
		{"VIN(DT) Input power detection threshold", "mV", ""},
		{"Vhys Hysteresis on VIN(DT)", "mV", ""},
		{"VDO(IN-OUT) VIN – VOUT", "mV", "vdropout"},
		{"VO(SC1) Output short-circuit detection threshold", "V", ""},
		{"VIN Supply input voltage", "V", "vin"},
		{"VOUT Output voltage range", "V", "vout"},
		{"VIN_UVLO Undervoltage lockout threshold", "V", "uvlo"},
	}
	for _, c := range cases {
		if got := GuessKey(c.text, c.unit, ""); got != c.want {
			t.Errorf("%q [%s] = %q, want %q", c.text, c.unit, got, c.want)
		}
	}
}

func TestChineseHeadings(t *testing.T) {
	for in, want := range map[string]string{
		"极限参数 （注 1）":  KindAbsMax,
		"电气参数（注 2，3）": KindElectrical,
		"3. 电气特性":     KindElectrical,
		"二、管脚描述":      KindPins,
		"内部框图":        KindBlock,
		"典型应用电路":      KindTypicalApp,
		"封装尺寸":        KindPackage,
		"订购信息：":       KindOrdering,
	} {
		if !isPlainHeading(in) || SectionKind(in) != want {
			t.Errorf("%q: heading %v, kind %q, want %q", in, isPlainHeading(in), SectionKind(in), want)
		}
	}
	for _, in := range []string{"TP4056是一款充电器", "极限参数很重要", "管脚1"} {
		if isPlainHeading(in) {
			t.Errorf("%q isn't a heading", in)
		}
	}
}
