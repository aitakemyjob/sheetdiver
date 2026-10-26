package kicad

import (
	"encoding/json"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// buckBoard is a small design with every kind of symbol: a regulator
// with its inductor, capacitors and feedback divider, a connector, an
// LED behind its resistor and a MOSFET.
func buckBoard() Schematic {
	return Schematic{
		Title: "Buck board", Project: "buck", Date: "2026-10-26",
		Groups:     []Group{{Name: "Power", Description: "5 V in, 3.3 V out"}, {Name: "IO"}},
		GroundNets: []string{"GND"},
		Components: []Component{
			{Ref: "U1", MPN: "TPS62160", Kind: KindIC, Group: "Power", Pins: []Pin{
				{Number: "1", Name: "SW", Net: "SW", Side: Right, Type: Output},
				{Number: "2", Name: "VIN", Net: "VIN", Side: Left, Type: PowerIn},
				{Number: "3", Name: "EN", Net: "VIN", Side: Left, Type: Input},
				{Number: "4", Name: "FB", Net: "FB", Side: Left, Type: Input},
				{Number: "5", Name: "NC", Side: Left, Type: NoConnect},
				{Number: "6", Name: "GND", Net: "GND", Side: Bottom, Type: PowerIn},
				{Number: "7", Name: "VOS", Net: "3V3", Side: Right, Type: Input},
				{Number: "8", Name: "LX1", Net: "LX1", Side: Right, Type: Output},
				{Number: "9", Name: "LX2", Net: "LX2", Side: Right, Type: Output},
			}},
			{Ref: "L2", Value: "1uH", Kind: KindInductor, Group: "Power", Anchor: &Anchor{Ref: "U1", Pins: []string{"8", "9"}}, Pins: []Pin{{Number: "1", Net: "LX1"}, {Number: "2", Net: "LX2"}}},
			{Ref: "L1", Value: "2.2uH", Kind: KindInductor, Group: "Power", Pins: []Pin{{Number: "1", Net: "SW"}, {Number: "2", Net: "3V3"}}},
			{Ref: "C1", Value: "10uF", Kind: KindCapacitor, Group: "Power", Anchor: &Anchor{Ref: "U1", Pins: []string{"2"}}, Pins: []Pin{{Number: "1", Net: "VIN"}, {Number: "2", Net: "GND"}}},
			{Ref: "C2", Value: "22uF", Kind: KindCapacitor, Group: "Power", Anchor: &Anchor{Ref: "U1", Pins: []string{"7"}}, Pins: []Pin{{Number: "1", Net: "3V3"}, {Number: "2", Net: "GND"}}},
			{Ref: "R1", Value: "100k", Kind: KindResistor, Group: "Power", Anchor: &Anchor{Ref: "U1", Pins: []string{"4"}}, Pins: []Pin{{Number: "1", Net: "3V3"}, {Number: "2", Net: "FB"}}},
			{Ref: "R2", Value: "22k", Kind: KindResistor, Group: "Power", Anchor: &Anchor{Ref: "U1", Pins: []string{"4"}}, Pins: []Pin{{Number: "1", Net: "FB"}, {Number: "2", Net: "GND"}}},
			{Ref: "J1", Value: "Power in", Kind: KindConnector, Group: "IO", Pins: []Pin{
				{Name: "VBUS", Net: "VIN", Side: Right, Type: Passive}, {Name: "GND", Net: "GND", Side: Right, Type: Passive},
			}},
			{Ref: "D1", Value: "green", Kind: KindLED, Group: "IO", Anchor: &Anchor{Ref: "Q1", Pins: []string{"3"}}, Pins: []Pin{{Number: TerminalPin(KindLED, "anode"), Net: "LED_A"}, {Number: TerminalPin(KindLED, "cathode"), Net: "LED_K"}}},
			{Ref: "R3", Value: "1k", Kind: KindResistor, Group: "IO", Pins: []Pin{{Number: "1", Net: "3V3"}, {Number: "2", Net: "LED_A"}}},
			{Ref: "Q1", MPN: "AO3400A", Kind: KindMOSFETN, Group: "IO", Pins: []Pin{{Number: "1", Name: "gate", Net: "LED_EN"}, {Number: "2", Name: "source", Net: "GND"}, {Number: "3", Name: "drain", Net: "LED_K"}}},
		},
	}
}

// wantNets is each pin's net as KiCad's netlist names it.
var wantNets = map[string]string{
	"U1.1": "/SW", "U1.2": "/VIN", "U1.3": "/VIN", "U1.6": "GND", "U1.7": "/3V3",
	"L1.1": "/SW", "L1.2": "/3V3", "C1.1": "/VIN", "C1.2": "GND", "C2.1": "/3V3", "C2.2": "GND",
	"R1.1": "/3V3", "R2.2": "GND",
	"J1.VBUS": "/VIN", "J1.GND": "GND",
	"D1.2": "/LED_A", "D1.1": "/LED_K", "R3.1": "/3V3", "R3.2": "/LED_A",
	"Q1.1": "/LED_EN", "Q1.2": "GND", "Q1.3": "/LED_K",
}

// sameNets are pins wired together without a label: on one net, whatever
// KiCad names it.
var sameNets = [][2]string{{"U1.8", "L2.1"}, {"U1.9", "L2.2"}, {"U1.4", "R1.2"}, {"U1.4", "R2.1"}}

func TestWriteSchematic(t *testing.T) {
	out := NewWriter().Write(buckBoard())
	for _, want := range []string{
		`(lib_id "SheetDiver:TPS62160")`, `(lib_id "Device:R")`, `(lib_id "Device:LED")`, `(lib_id "SheetDiver:AO3400A")`, `(lib_id "power:GND")`,
		`(label "VIN"`, `(no_connect`, `(text "Power"`, `(junction`,
	} {
		if !strings.Contains(out.Schematic, want) {
			t.Errorf("schematic lacks %s", want)
		}
	}
	// Generated symbols go in the project's library; KiCad's own don't.
	if !strings.Contains(out.Symbols, `(symbol "TPS62160"`) || strings.Contains(out.Symbols, `"R"`) {
		t.Errorf("symbol library:\n%s", out.Symbols)
	}
	if out2 := NewWriter().Write(buckBoard()); out2.Schematic != out.Schematic {
		t.Error("the same design gives a different file")
	}
	// Balanced parentheses, outside strings.
	depth := 0
	for _, s := range []string{out.Schematic, out.Symbols, out.SymLibTable} {
		inString := false
		for i := 0; i < len(s); i++ {
			switch {
			case s[i] == '\\' && inString:
				i++
			case s[i] == '"':
				inString = !inString
			case !inString && s[i] == '(':
				depth++
			case !inString && s[i] == ')':
				depth--
			}
		}
		if depth != 0 {
			t.Fatalf("unbalanced: %d", depth)
		}
	}
}

// TestNetlistInKiCad has KiCad read the schematic back and checks every
// pin is on its net. It needs kicad-cli (KiCad 7 or later).
func TestNetlistInKiCad(t *testing.T) {
	cli, err := exec.LookPath("kicad-cli")
	if err != nil {
		t.Skip("kicad-cli isn't installed")
	}
	dir := t.TempDir()
	for name, text := range NewWriter().Write(buckBoard()).Files("buck") {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	netlist := filepath.Join(dir, "buck.net")
	cmd := exec.Command(cli, "sch", "export", "netlist", "-o", netlist, filepath.Join(dir, "buck.kicad_sch"))
	cmd.Env = append(os.Environ(), "HOME="+dir)
	if b, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("kicad-cli: %v\n%s", err, b)
	}
	b, err := os.ReadFile(netlist)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]string{}
	netRe := regexp.MustCompile(`\(net \(code "?\d+"?\) \(name "([^"]*)"\)((?:\s*\(node [^\n]*\))*)`)
	nodeRe := regexp.MustCompile(`\(node \(ref "([^"]+)"\) \(pin "([^"]+)"\)`)
	for _, m := range netRe.FindAllStringSubmatch(string(b), -1) {
		for _, n := range nodeRe.FindAllStringSubmatch(m[2], -1) {
			got[n[1]+"."+n[2]] = m[1]
		}
	}
	for pin, net := range wantNets {
		if got[pin] != net {
			t.Errorf("%s on %q, want %q", pin, got[pin], net)
		}
	}
	for _, pair := range sameNets {
		if got[pair[0]] == "" || got[pair[0]] != got[pair[1]] || strings.HasPrefix(got[pair[0]], "unconnected") {
			t.Errorf("%s on %q and %s on %q: want them wired together", pair[0], got[pair[0]], pair[1], got[pair[1]])
		}
	}
	if got["U1.8"] == got["U1.9"] {
		t.Errorf("LX1 and LX2 joined: %q", got["U1.8"])
	}
	if !strings.HasPrefix(got["U1.5"], "unconnected") {
		t.Errorf("U1 pin 5 on %q, want it unconnected", got["U1.5"])
	}
}

func TestFootprints(t *testing.T) {
	chain := DefaultFootprints(FindFootprints())
	for _, c := range []struct {
		q    FootprintQuery
		want string
	}{
		{FootprintQuery{Kind: KindCapacitor, Texts: []string{"100nF 50V X7R 0603"}}, "Capacitor_SMD:C_0603_1608Metric"},
		{FootprintQuery{Kind: KindResistor, Texts: []string{"", "10k 1% 0402"}}, "Resistor_SMD:R_0402_1005Metric"},
		{FootprintQuery{Kind: KindIC, Texts: []string{"SOT-23-5"}, Pads: 5}, "Package_TO_SOT_SMD:SOT-23-5"},
		{FootprintQuery{Kind: KindMOSFETN, Texts: []string{"SOT23"}, Pads: 3}, "Package_TO_SOT_SMD:SOT-23"},
		{FootprintQuery{Kind: KindSchottky, Texts: []string{"SOD-123F"}}, "Diode_SMD:D_SOD-123F"},
		{FootprintQuery{Kind: KindIC, Texts: []string{"Package_SO:SOIC-8_3.9x4.9mm_P1.27mm"}}, "Package_SO:SOIC-8_3.9x4.9mm_P1.27mm"},
		// A SOT-23 with six pins is a SOT-23-6; one with seven isn't any.
		{FootprintQuery{Kind: KindIC, Texts: []string{"Regulator in 6-Pin SOT-23"}, Pads: 6}, "Package_TO_SOT_SMD:SOT-23-6"},
		{FootprintQuery{Kind: KindIC, Texts: []string{"SOT-23"}, Pads: 7}, ""},
		// TI's package codes.
		{FootprintQuery{Kind: KindIC, Texts: []string{"ESD Protection Device", "DCK"}, Pads: 3}, "Package_TO_SOT_SMD:SOT-323_SC-70"},
		{FootprintQuery{Kind: KindIC, Texts: []string{"TPS2121 DBV"}, Pads: 6}, "Package_TO_SOT_SMD:SOT-23-6"},
		{FootprintQuery{Kind: KindIC, Texts: []string{"DBV"}}, ""},
		// An SMA connector isn't a diode.
		{FootprintQuery{Kind: KindConnector, Texts: []string{"SMA edge mount"}}, ""},
		// A QFN without its size could be any of dozens.
		{FootprintQuery{Kind: KindIC, Texts: []string{"VQFN-16"}}, ""},
	} {
		if got := chain.Footprint(c.q); got.LibID != c.want {
			t.Errorf("%+v: %q (%s), want %q", c.q, got.LibID, got.Hint, c.want)
		}
	}
	if FindFootprints() == nil {
		return
	}
	got := chain.Footprint(FootprintQuery{Kind: KindIC, Texts: []string{"VQFN-12 3x3mm"}})
	if !strings.HasPrefix(got.LibID, "Package_DFN_QFN:QFN-12-1EP_3x3mm") {
		t.Errorf("VQFN-12 3x3mm: %+v", got)
	}
	if got := chain.Footprint(FootprintQuery{Kind: KindIC, Texts: []string{"WCSP"}, Pads: 20}); got.LibID != "" || !strings.Contains(got.Hint, "WCSP") {
		t.Errorf("unresolved: %+v", got)
	}
}

// boardFootprints are footprints for the test board, from KiCad's
// libraries.
var boardFootprints = map[string]string{
	"U1": "Package_SO:MSOP-10_3x3mm_P0.5mm", "L1": "Inductor_SMD:L_1210_3225Metric", "L2": "Inductor_SMD:L_1210_3225Metric",
	"C1": "Capacitor_SMD:C_0603_1608Metric", "C2": "Capacitor_SMD:C_0805_2012Metric", "R1": "Resistor_SMD:R_0402_1005Metric",
	"R2": "Resistor_SMD:R_0402_1005Metric", "R3": "Resistor_SMD:R_0402_1005Metric", "D1": "LED_SMD:LED_0603_1608Metric",
	"Q1": "Package_TO_SOT_SMD:SOT-23", "J1": "Connector_PinHeader_2.54mm:PinHeader_1x02_P2.54mm_Vertical",
}

// TestBoardInKiCad has KiCad load the board and checks each pad's net,
// that no courtyards overlap, and that the passives sit at their pins.
// It needs KiCad's footprints and its Python module.
func TestBoardInKiCad(t *testing.T) {
	src := FindFootprints()
	if src == nil {
		t.Skip("KiCad's footprints aren't installed")
	}
	if exec.Command("python3", "-c", "import pcbnew").Run() != nil {
		t.Skip("KiCad's Python module isn't installed")
	}
	s := buckBoard()
	for i := range s.Components {
		s.Components[i].Footprint.LibID = boardFootprints[s.Components[i].Ref]
	}
	res := (&BoardWriter{Footprints: src}).Write(s)
	if len(res.Missing) != 0 {
		t.Errorf("missing: %q", res.Missing)
	}
	// The header's pins are named, not numbered: said, not guessed.
	if len(res.Unmatched) != 2 || !strings.Contains(res.Unmatched[0], "J1 pin VBUS") {
		t.Errorf("unmatched: %q", res.Unmatched)
	}
	path := filepath.Join(t.TempDir(), "buck.kicad_pcb")
	if err := os.WriteFile(path, []byte(res.Board), 0o644); err != nil {
		t.Fatal(err)
	}
	out, err := exec.Command("python3", "testdata/board_report.py", path).Output()
	if err != nil {
		t.Fatalf("KiCad can't load the board: %v", err)
	}
	var report struct {
		Footprints []struct {
			Ref  string
			Pads []struct {
				Number string
				Net    string
				X, Y   float64
			}
		}
		DRC []string
	}
	if err := json.Unmarshal(out, &report); err != nil {
		t.Fatalf("%v: %s", err, out)
	}
	type pad struct {
		net  string
		x, y float64
	}
	pads := map[string]pad{}
	for _, f := range report.Footprints {
		for _, p := range f.Pads {
			pads[f.Ref+"."+p.Number] = pad{p.Net, p.X, p.Y}
		}
	}
	if len(report.Footprints) != len(s.Components) {
		t.Errorf("%d footprints on the board, want %d", len(report.Footprints), len(s.Components))
	}
	for pin, net := range wantNets {
		if strings.HasPrefix(pin, "J1.") {
			continue
		}
		if got := pads[pin].net; "/"+got != net && got != net {
			t.Errorf("pad %s on %q, want %q", pin, got, net)
		}
	}
	for _, d := range report.DRC {
		if strings.Contains(d, "courtyard") || strings.Contains(d, "clearance") || strings.Contains(d, "short") {
			t.Errorf("DRC: %s", d)
		}
	}
	// Each passive's pad on the shared net close to the pin it serves.
	for _, c := range [][2]string{{"C1.1", "U1.2"}, {"R1.2", "U1.4"}, {"L2.1", "U1.8"}, {"L2.2", "U1.9"}} {
		a, b := pads[c[0]], pads[c[1]]
		if d := math.Hypot(a.x-b.x, a.y-b.y); d > 3.5 {
			t.Errorf("%s is %.1f mm from %s", c[0], d, c[1])
		}
	}
}
