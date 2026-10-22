package core

import (
	"context"
	"sort"
	"strings"
	"testing"

	"github.com/aitakemyjob/sheetdiver/internal/library"
	"github.com/aitakemyjob/sheetdiver/internal/testutil"
)

// buildCircuit puts a test circuit in a project, as an agent would, and
// returns the project.
func buildCircuit(t *testing.T, s *Service, c testutil.Circuit) ProjectDetail {
	t.Helper()
	ctx := context.Background()
	p, err := s.Lib.CreateProject(ctx, c.Name, "")
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range c.Rails {
		v := r.V
		if _, err := s.Lib.SetRail(ctx, library.Rail{ProjectID: p.ID, Name: r.Name, VNom: &v}); err != nil {
			t.Fatal(err)
		}
	}
	var ch ConnectionsChange
	for _, part := range c.Parts {
		if part.Ref != "" {
			if _, err := s.AddToProject(ctx, c.Name, part.Ref, library.ProjectPart{Designator: part.Name, SupplyRail: part.Supply, OutputRail: part.Output}); err != nil {
				t.Fatal(err)
			}
			continue
		}
		ch.Blocks = append(ch.Blocks, BlockChange{Name: part.Name, Kind: part.Kind, Value: part.Value})
	}
	for _, n := range c.Nets {
		bus := n.Bus
		ch.Nets = append(ch.Nets, NetChange{Name: n.Name, Kind: n.Kind, Bus: &bus})
	}
	for _, pin := range c.Pins {
		ch.Connect = append(ch.Connect, ConnectChange{Net: pin[0], Part: pin[1], Pin: pin[2]})
	}
	problems, err := s.ChangeConnections(ctx, c.Name, ch, library.SourceUser)
	if err != nil || len(problems) > 0 {
		t.Fatalf("%s: %v %q", c.Name, err, problems)
	}
	d, err := s.ProjectDetail(ctx, c.Name)
	if err != nil {
		t.Fatal(err)
	}
	return d
}

// circuit finds a test circuit by name.
func circuit(name string) testutil.Circuit {
	for _, c := range testutil.Circuits() {
		if c.Name == name {
			return c
		}
	}
	panic(name)
}

// drawn is a diagram with lookups by title, for tests.
type drawn struct {
	t *testing.T
	Diagram
}

// part finds a box by its title (or its title's first word).
func (d drawn) part(title string) DiagramPart {
	d.t.Helper()
	for _, p := range d.Parts {
		if p.Title == title || strings.HasPrefix(p.Title, title+" ") {
			return p
		}
	}
	d.t.Fatalf("no part %q", title)
	return DiagramPart{}
}

// pins says each pin's side ("VIN:left", "EN:inside→VIN").
func (d drawn) pins(title string) string {
	var out []string
	for _, p := range d.part(title).Pins {
		s := p.Name + ":" + p.Side
		if p.Tied != "" {
			s += "→" + p.Tied
		}
		if p.Bridged {
			s += "*"
		}
		out = append(out, s)
	}
	return strings.Join(out, " ")
}

// net finds a drawn net.
func (d drawn) net(name string) DiagramNet {
	d.t.Helper()
	for _, n := range d.Nets {
		if n.Name == name {
			return n
		}
	}
	d.t.Fatalf("no net %q", name)
	return DiagramNet{}
}

// passives says where each passive hangs ("C1,C2 10µF wire U1 on 5V_PROT").
func (d drawn) passives() []string {
	title := map[string]string{}
	for _, p := range d.Parts {
		title[p.ID] = strings.Fields(p.Title)[0]
	}
	var out []string
	for _, p := range d.Passives {
		out = append(out, strings.TrimSpace(p.Label+" "+p.Value)+" "+p.Mount+" "+title[p.Part]+" on "+strings.Join(p.Nets, "+"))
	}
	sort.Strings(out)
	return out
}

// links says each link ("J1→U1 power VBUS").
func (d drawn) links() []string {
	title := map[string]string{}
	for _, p := range d.Parts {
		title[p.ID] = strings.Fields(p.Title)[0]
	}
	var out []string
	for _, l := range d.Links {
		out = append(out, title[l.From]+"→"+title[l.To]+" "+l.Kind+" "+l.Label)
	}
	sort.Strings(out)
	return out
}

// columns says each box's column ("J1:0 U1:1").
func (d drawn) columns() string {
	var out []string
	for _, p := range d.Parts {
		out = append(out, strings.Fields(p.Title)[0]+":"+string(rune('0'+p.Column)))
	}
	return strings.Join(out, " ")
}

func TestDiagramOfTheUSBSketch(t *testing.T) {
	s := newService(t)
	d := drawn{t, buildCircuit(t, s, circuit("USB-C to 3V3")).Connections.Diagram}
	if got := d.columns(); got != "J1:0 U1:1 U2:2" {
		t.Errorf("columns = %s", got)
	}
	for title, want := range map[string]string{
		// The connector's CC pins face right, their resistors beside them.
		"J1": "VBUS:right CC1:right CC2:right",
		// IN gets the wire, EN is written tied to it; ILIM's resistor is below.
		"U1": "IN:left EN:inside→IN OUT:right ILIM:bottom",
		// SEL is tied to ground; LX1 and LX2 come last, bridged by L1.
		"U2": "VIN:left EN:inside→VIN SEL:inside→GND VOUT:right CFG1:bottom CFG2:bottom CFG3:bottom LX1:right* LX2:right*",
	} {
		if got := d.pins(title); got != want {
			t.Errorf("%s pins = %s\n             want %s", title, got, want)
		}
	}
	if g := d.part("U2").Ground; len(g) != 1 || g[0] != "GND" {
		t.Errorf("U2 ground = %v", g)
	}
	for name, want := range map[string]string{"VBUS": "between", "5V_PROT": "between", "3V3": "out", "CC1": "", "ILIM": "", "LX1": "", "CFG1": ""} {
		if got := d.net(name).Wire; got != want {
			t.Errorf("%s wire = %q, want %q", name, got, want)
		}
	}
	if !d.net("3V3").Arrow {
		t.Error("3V3 leaves the board as an output, with an arrow")
	}
	want := []string{
		"C1,C2 10µF wire U1 on 5V_PROT", // alike on one net: merged, between U1 and U2
		"C3,C4 22µF out U2 on 3V3",
		"D1 TVS wire J1 on VBUS",
		"L1 2.2µH bridge U2 on LX1+LX2",
		"R3 150k below U1 on ILIM",
		"R4 36.5k below U2 on CFG1",
		"R5 16.2k below U2 on CFG3",
		"R6 0Ω below U2 on CFG2",
		"Rd1 5.1k beside J1 on CC1",
		"Rd2 5.1k beside J1 on CC2",
	}
	if got := d.passives(); strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("passives:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
	if got := d.links(); strings.Join(got, "; ") != "J1→U1 power VBUS; U1→U2 signal 5V_PROT" && strings.Join(got, "; ") != "J1→U1 power VBUS; U1→U2 power 5V_PROT" {
		t.Errorf("links = %q", got)
	}
}

func TestDiagramOfABatteryBoard(t *testing.T) {
	s := newService(t)
	d := drawn{t, buildCircuit(t, s, circuit("Battery board")).Connections.Diagram}
	// The 4.2 V rail joins four parts: a lane above them, not a chain
	// through their boxes.
	lane := d.net("4.2V")
	if lane.Wire != WireLane || len(lane.Parts) != 4 {
		t.Errorf("4.2V = %+v", lane)
	}
	for title, want := range map[string]string{
		"U1": "VCC:left CE:inside→VCC TEMP:inside→GND BAT:left PROG:bottom CHRG:bottom",
		"U3": "VIN:left EN:inside→VIN VOUT:right LX1:right* LX2:right*",
	} {
		if got := d.pins(title); got != want {
			t.Errorf("%s pins = %s\n             want %s", title, got, want)
		}
	}
	for _, want := range []string{"C1 10µF wire J1 on 5V", "C2 10µF wire U3 on 3V3", "L1 2.2µH bridge U3 on LX1+LX2", "LED1 red below U1 on CHRG", "R1 2.2k below U1 on PROG"} {
		if !contains(d.passives(), want) {
			t.Errorf("missing %q in %q", want, d.passives())
		}
	}
	for _, n := range []string{"OD", "OC"} {
		if w := d.net(n); w.Wire != WireBetween || len(w.Parts) != 2 {
			t.Errorf("%s = %+v", n, w)
		}
	}
}

func TestDiagramOfAnMCUBoard(t *testing.T) {
	s := newService(t)
	d := drawn{t, buildCircuit(t, s, circuit("MCU board")).Connections.Diagram}
	if got := d.columns(); got != "U1:1 J1:0 U2:2 U3:3" {
		t.Errorf("columns = %s", got)
	}
	for title, want := range map[string]string{
		// A library part: its pins by name from its pin table.
		"U1": "VIN:left SW:right",
		// USB data from the connector on the left, I2C to the sensor on
		// the right; the LED pin's LED below; EN leaves with its pull-up
		// and capacitor.
		"U2": "3V3:left EN:left GPIO19:left GPIO18:left GPIO8:right GPIO9:right GPIO2:bottom",
		"U3": "VDD:left CSB:inside→VDD SDA:left SCL:left",
	} {
		if got := d.pins(title); got != want {
			t.Errorf("%s pins = %s\n             want %s", title, got, want)
		}
	}
	if n := d.net("3V3"); n.Wire != WireLane || len(n.Parts) != 3 {
		t.Errorf("3V3 = %+v", n)
	}
	for _, want := range []string{
		"C1 1µF wire J1 on VBUS",
		"C2 1µF lane U1 on 3V3", // below the regulator's output
		"C3 1µF out U2 on EN",
		"LED1 green below U2 on LED",
		"R1 10k out U2 on EN", // a pull-up to 3V3 hangs from EN
		"R2 4.7k wire U2 on SDA",
		"R3 4.7k wire U2 on SCL",
	} {
		if !contains(d.passives(), want) {
			t.Errorf("missing %q in\n%s", want, strings.Join(d.passives(), "\n"))
		}
	}
	want := []string{"J1→U1 power VBUS", "J1→U2 signal USB", "U1→U2 power 3V3", "U1→U3 power 3V3", "U2→U3 signal I2C"}
	if got := d.links(); strings.Join(got, "; ") != strings.Join(want, "; ") {
		t.Errorf("links = %q\n want %q", got, want)
	}
}

func TestColumnsSurviveALoop(t *testing.T) {
	col := columnsOf([]string{"a", "b", "c"}, []DiagramLink{{From: "a", To: "b"}, {From: "b", To: "a"}})
	if col["a"] > 2 || col["b"] > 2 {
		t.Errorf("columns = %v", col)
	}
	// An unlinked part goes after the others.
	if col["c"] != max(col["a"], col["b"])+1 {
		t.Errorf("unlinked c = %d", col["c"])
	}
}

func TestDiagramKinds(t *testing.T) {
	for in, want := range map[[2]string]string{
		{"tvs", ""}: "diode", {"ferrite", ""}: "other", {"buck", ""}: "ic", {"", "block"}: "connector", {"module", "block"}: "module",
	} {
		if got := diagramKind(in[0], in[1] == "block"); got != want {
			t.Errorf("diagramKind(%q, %v) = %s, want %s", in[0], in[1] == "block", got, want)
		}
	}
	if passiveValue("10µF") != "10µF" || passiveValue("3V3 LDO") != "" {
		t.Error("a BOM role is a value only when it reads like one")
	}
}
