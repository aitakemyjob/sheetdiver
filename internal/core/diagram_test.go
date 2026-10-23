package core

import (
	"context"
	"fmt"
	"slices"
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
	for _, g := range c.Groups {
		desc, notes := g.Description, g.Notes
		ch.Groups = append(ch.Groups, GroupChange{Name: g.Name, Description: &desc, Notes: &notes})
	}
	for _, a := range c.Assign {
		group, supports := a[1], a[2]
		ch.Assign = append(ch.Assign, AssignChange{Part: a[0], Group: &group, Supports: &supports})
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

// pins says each pin's side ("VIN:left", "EN:left→VIN"), with its
// number ("VIN:top#3"), "*" bridged, "⏚" to ground, "?" unconnected.
func (d drawn) pins(title string) string {
	var out []string
	for _, p := range d.part(title).Pins {
		s := p.Name + ":" + p.Side
		if p.Number != "" {
			s += "#" + p.Number
		}
		if p.Tied != "" {
			s += "→" + p.Tied
		}
		if p.Bridged {
			s += "*"
		}
		if p.Ground {
			s += "⏚"
		}
		if !p.Connected {
			s += "?"
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
		"J1": "VBUS:right CC1:right CC2:right GND:bottom⏚",
		// IN gets the wire, EN is written tied to it; ILIM's resistor is below.
		"U1": "IN:left EN:left→IN OUT:right ILIM:bottom GND:bottom⏚",
		// SEL is tied to ground; LX1 and LX2 come last, bridged by L1.
		// Pins tied to ground go to a ground symbol like GND itself.
		"U2": "VIN:left EN:left→VIN VOUT:right CFG1:bottom CFG2:bottom CFG3:bottom LX1:right* LX2:right* SEL:bottom⏚ GND:bottom⏚",
	} {
		if got := d.pins(title); got != want {
			t.Errorf("%s pins = %s\n             want %s", title, got, want)
		}
	}
	for name, want := range map[string]string{"VBUS": "between", "5V_PROT": "between", "3V3": "out", "CC1": "out", "ILIM": "", "LX1": "", "CFG1": ""} {
		if got := d.net(name).Wire; got != want {
			t.Errorf("%s wire = %q, want %q", name, got, want)
		}
	}
	if !d.net("3V3").Arrow {
		t.Error("3V3 leaves the board as an output, with an arrow")
	}
	want := []string{
		"C1,C2 10µF wire U2 on 5V_PROT", // alike on one net: merged, between U1 and U2, by U2's VIN they feed
		"C3,C4 22µF out U2 on 3V3",
		"D1 TVS wire J1 on VBUS",
		"L1 2.2µH bridge U2 on LX1+LX2",
		"R3 150k below U1 on ILIM",
		"R4 36.5k below U2 on CFG1",
		"R5 16.2k below U2 on CFG3",
		"R6 0Ω below U2 on CFG2",
		"Rd1 5.1k out J1 on CC1", // hanging from a short stub, to ground
		"Rd2 5.1k out J1 on CC2",
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
		// The lane's pin first, so its riser crosses nothing.
		"U1": "BAT:left VCC:left CE:left→VCC PROG:bottom CHRG:bottom GND:bottom⏚ TEMP:bottom⏚",
		"U3": "VIN:left EN:left→VIN VOUT:right LX1:right* LX2:right* GND:bottom⏚",
	} {
		if got := d.pins(title); got != want {
			t.Errorf("%s pins = %s\n             want %s", title, got, want)
		}
	}
	for _, want := range []string{"C1 10µF wire U1 on 5V", "C2 10µF wire U3 on 3V3", "L1 2.2µH bridge U3 on LX1+LX2", "LED1 red below U1 on CHRG", "R1 2.2k below U1 on PROG"} {
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
		// A library part: a generated symbol from its pin table, every pin
		// numbered, by role: the supply on top, the output right, the
		// feedback input (unconnected: "?") left, ground along the bottom.
		"U1": "VIN:top#3 VFB:left#4? SW:right#2 GND:bottom#1⏚",
		// USB data from the connector on the left, I2C to the sensor on
		// the right; the LED pin's LED below; EN leaves with its pull-up
		// and capacitor.
		"U2": "3V3:left GPIO19:left GPIO18:left GPIO8:right GPIO9:right EN:left GPIO2:bottom GND:bottom⏚",
		"U3": "VDD:left CSB:left→VDD SDA:left SCL:left GND:bottom⏚",
	} {
		if got := d.pins(title); got != want {
			t.Errorf("%s pins = %s\n             want %s", title, got, want)
		}
	}
	if n := d.net("3V3"); n.Wire != WireLane || len(n.Parts) != 3 {
		t.Errorf("3V3 = %+v", n)
	}
	for _, want := range []string{
		"C1 1µF wire U1 on VBUS",
		"C2 1µF lane U1 on 3V3", // below the regulator's output
		"C3 1µF out U2 on EN",
		"LED1 green below U2 on LED",
		"R1 10k out U2 on EN+3V3", // a pull-up: from EN, its other end 3V3
		"R2 4.7k wire U2 on SDA+3V3",
		"R3 4.7k wire U2 on SCL+3V3",
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
	for in, want := range map[string]string{
		"10µF": "10µF", "10µF input bulk": "10µF", "0Ω": "0Ω", "3V3 LDO": "", "2.2µH, shielded": "2.2µH", "330": "330",
		// Anywhere in the role, with its multiplier or unit: not the 1 of U1 or the 5 of PA5.
		"U1 input 10µF": "10µF", "BOOT0 10k pull-down": "10k", "CC1/CC2 5.1k pull-downs": "5.1k", "1µF: C2 U1 BIAS": "1µF", "LSE load caps 6.8pF": "6.8pF", "32.768 kHz LSE": "32.768 kHz", "user LED (PA5)": "", "LED 330 series": "",
	} {
		if got := passiveValue(in); got != want {
			t.Errorf("passiveValue(%q) = %q, want %q", in, got, want)
		}
	}
	// A description's leading value only: a package or a family's example isn't the part's value.
	for in, want := range map[string]string{"100nF 50V X7R 0603 (LCSC C14663)": "100nF", "6.8pF 50V C0G 0603": "6.8pF", "1µH molded inductor, 35mΩ": "1µH", "Uniroyal 0603 1% thick film. 10k C25804": "", "0603 1% thick film": "", "Samsung CL series MLCC catalogue. CL10A105 1µF/50V": ""} {
		if got := descriptionValue(in); got != want {
			t.Errorf("descriptionValue(%q) = %q, want %q", in, got, want)
		}
	}
	if shortValue("input bulk for U2 VIN") != "input bulk …" || shortValue("TVS") != "TVS" || shortValue("10µF input bulk for U2") != "10µF" {
		t.Error("a long description is cut to its value, or 12 characters")
	}
}

func TestPassivesHangByThePartTheySupport(t *testing.T) {
	// Issue 71: decoupling capacitors on a shared rail used to hang
	// together at the regulator. Marked with the part each serves, they
	// hang beside it, and alike ones serving different parts stay apart.
	c := circuit("Grouped MCU board")
	unmarked := c
	unmarked.Groups, unmarked.Assign = nil, nil
	d := drawn{t, buildCircuit(t, newService(t), unmarked).Connections.Diagram}
	if got := d.passives(); !slices.Contains(got, "C4,C5 100nF lane U1 on 3V3") {
		t.Errorf("unmarked: %q", got)
	}

	marked := c
	marked.Name, marked.Groups, marked.Assign = "Marked", nil, nil
	for _, a := range c.Assign {
		if a[2] != "" {
			marked.Assign = append(marked.Assign, a)
		}
	}
	d = drawn{t, buildCircuit(t, newService(t), marked).Connections.Diagram}
	got := d.passives()
	for _, want := range []string{"C2 1µF lane U1 on 3V3", "C4 100nF lane U2 on 3V3", "C5 100nF lane U3 on 3V3"} {
		if !slices.Contains(got, want) {
			t.Errorf("marked: want %q in %q", want, got)
		}
	}
	if len(d.Groups) != 0 {
		t.Errorf("no groups were made: %+v", d.Groups)
	}
	for _, p := range d.Passives {
		if p.Label == "C4" && p.Supports != d.part("U2").ID {
			t.Errorf("C4 supports %q", p.Supports)
		}
	}

	// Unmarked, a passive whose nets reach one part serves it.
	usb := drawn{t, buildCircuit(t, newService(t), circuit("USB-C to 3V3")).Connections.Diagram}
	for _, p := range usb.Passives {
		if p.Label == "R4" && p.Supports != usb.part("U2").ID {
			t.Errorf("R4 (U2's CFG1 resistor) supports %q", p.Supports)
		}
		// C1 and C2 sit between U1's output and U2's VIN: they serve U2,
		// the IC there that doesn't drive the net; D1 guards the connector.
		if strings.HasPrefix(p.Label, "C1") && (p.Supports != usb.part("U2").ID || !p.Guessed) {
			t.Errorf("C1,C2 support %q (guessed %v), want U2", p.Supports, p.Guessed)
		}
		if p.Label == "D1" && p.Supports != usb.part("J1").ID {
			t.Errorf("D1 supports %q, want J1", p.Supports)
		}

	}
}

func TestDiagramInGroups(t *testing.T) {
	// Issue 72: parts in groups, wires only inside a group, and a net that
	// runs between groups drawn as a labelled stub in each.
	d := drawn{t, buildCircuit(t, newService(t), circuit("Grouped MCU board")).Connections.Diagram}
	var names []string
	for _, g := range d.Groups {
		names = append(names, g.Name+":"+strings.Join(g.Parts, ","))
	}
	j1, u1, u2, u3 := d.part("J1").ID, d.part("U1").ID, d.part("U2").ID, d.part("U3").ID
	want := []string{"Power:" + u1 + "," + j1, "MCU:" + u2, "Sensor:" + u3}
	if strings.Join(names, " ") != strings.Join(want, " ") {
		t.Errorf("groups = %q, want %q", names, want)
	}
	if g := d.Groups[0]; g.Description != "USB 5 V to 3.3 V" || g.Notes != "Keep the buck's input loop short." {
		t.Errorf("Power = %+v", g)
	}
	if d.part("U2").Group != "MCU" || d.part("J1").Group != "Power" {
		t.Errorf("parts' groups: U2 %q, J1 %q", d.part("U2").Group, d.part("J1").Group)
	}

	// VBUS stays inside Power: one wire, not labelled as going elsewhere.
	var vbus, rail []DiagramNet
	for _, n := range d.Nets {
		switch n.Name {
		case "VBUS":
			vbus = append(vbus, n)
		case "3V3":
			rail = append(rail, n)
		}
	}
	if len(vbus) != 1 || vbus[0].Group != "Power" || vbus[0].Wire != WireBetween || vbus[0].Offboard {
		t.Errorf("VBUS = %+v", vbus)
	}
	// 3V3 runs in all three groups: a labelled stub in each.
	if len(rail) != 3 {
		t.Fatalf("3V3 = %+v", rail)
	}
	for _, n := range rail {
		if n.Wire != WireOut || !n.Offboard || len(n.Parts) != 1 {
			t.Errorf("3V3 in %s = %+v", n.Group, n)
		}
	}

	// Passives are drawn in the group of the part they serve (or their own).
	got := d.passives()
	for _, w := range []string{"C2 1µF out U1 on 3V3", "C4 100nF out U2 on 3V3", "C5 100nF out U3 on 3V3", "R2 4.7k out U3 on SDA+3V3", "R1 10k out U2 on EN+3V3"} {
		if !slices.Contains(got, w) {
			t.Errorf("want %q in %q", w, got)
		}
	}
	for _, p := range d.Passives {
		owner := ""
		for _, part := range d.Parts {
			if part.ID == p.Part {
				owner = part.Group
			}
		}
		if p.Group != owner {
			t.Errorf("%s is drawn in %q but hangs from a part in %q", p.Label, p.Group, owner)
		}
	}
}

func TestAssignChanges(t *testing.T) {
	s := newService(t)
	ctx := context.Background()
	buildCircuit(t, s, circuit("USB-C to 3V3"))
	str := func(v string) *string { return &v }
	problems, err := s.ChangeConnections(ctx, "USB-C to 3V3", ConnectionsChange{
		Groups: []GroupChange{{Name: "Input", Description: str("USB-C with protection")}},
		Assign: []AssignChange{
			{Part: "J1 USB-C", Group: str("Input")},
			{Part: "U1 TPS25200", Group: str("input")},
			{Part: "C3", Supports: str("U2 TPS63900")},
			{Part: "U9", Group: str("Input")},
			{Part: "C4", Supports: str("U9")},
			// The wrong way round: refused, saying which way it goes.
			{Part: "U2 TPS63900", Supports: str("C4")},
		},
	}, library.SourceAgent)
	if err != nil {
		t.Fatal(err)
	}
	if len(problems) != 3 || !strings.Contains(problems[0], "U9") || !strings.Contains(problems[1], "C4 supports U9") || !strings.Contains(problems[2], "part C4, supports U2 TPS63900") {
		t.Errorf("problems = %q", problems)
	}
	d, _ := s.ProjectDetail(ctx, "USB-C to 3V3")
	c := d.Connections
	if len(c.Groups) != 1 || c.Groups[0].Description != "USB-C with protection" || c.Groups[0].Source != library.SourceAgent {
		t.Errorf("groups = %+v", c.Groups)
	}
	byName := map[string]AssignmentRow{}
	for _, a := range c.Assignments {
		byName[a.Endpoint] = a
	}
	if byName["J1 USB-C"].Group != "Input" || byName["C3"].SupportsEndpoint != "U2 TPS63900" || byName["C3"].Group != "" {
		t.Errorf("assignments = %+v", c.Assignments)
	}

	// Unset fields are kept; "" clears one.
	if _, err := s.ChangeConnections(ctx, "USB-C to 3V3", ConnectionsChange{Assign: []AssignChange{{Part: "C3", Group: str("Input")}, {Part: "J1 USB-C", Group: str("")}}}, library.SourceUser); err != nil {
		t.Fatal(err)
	}
	d, _ = s.ProjectDetail(ctx, "USB-C to 3V3")
	byName = map[string]AssignmentRow{}
	for _, a := range d.Connections.Assignments {
		byName[a.Endpoint] = a
	}
	if a := byName["C3"]; a.Group != "Input" || a.SupportsEndpoint != "U2 TPS63900" {
		t.Errorf("C3 = %+v", a)
	}
	if _, ok := byName["J1 USB-C"]; ok {
		t.Errorf("J1's assignment was cleared: %+v", byName["J1 USB-C"])
	}
	// A subgroup is kept with the rest, drawn on the part, and "" clears it.
	if _, err := s.ChangeConnections(ctx, "USB-C to 3V3", ConnectionsChange{Assign: []AssignChange{{Part: "C3", Cluster: str(" VIN ")}}}, library.SourceUser); err != nil {
		t.Fatal(err)
	}
	d, _ = s.ProjectDetail(ctx, "USB-C to 3V3")
	for _, a := range d.Connections.Assignments {
		if a.Endpoint == "C3" && (a.Cluster != "VIN" || a.Group != "Input" || a.SupportsEndpoint != "U2 TPS63900") {
			t.Errorf("C3 = %+v", a)
		}
	}
	if i := slices.IndexFunc(d.Connections.Diagram.Passives, func(p DiagramPassive) bool { return p.Label == "C3" }); i < 0 || d.Connections.Diagram.Passives[i].Cluster != "VIN" {
		t.Errorf("C3 drawn = %+v", d.Connections.Diagram.Passives)
	}
	if _, err := s.ChangeConnections(ctx, "USB-C to 3V3", ConnectionsChange{Assign: []AssignChange{{Part: "C3", Cluster: str("")}}}, library.SourceUser); err != nil {
		t.Fatal(err)
	}
	d, _ = s.ProjectDetail(ctx, "USB-C to 3V3")
	for _, a := range d.Connections.Assignments {
		if a.Endpoint == "C3" && (a.Cluster != "" || a.Group != "Input") {
			t.Errorf("C3 after clearing = %+v", a)
		}
	}
	// The group's description survives an update that doesn't set it.
	if _, err := s.ChangeConnections(ctx, "USB-C to 3V3", ConnectionsChange{Groups: []GroupChange{{Name: "Input", Notes: str("ESD first")}}}, library.SourceUser); err != nil {
		t.Fatal(err)
	}
	d, _ = s.ProjectDetail(ctx, "USB-C to 3V3")
	if g := d.Connections.Groups[0]; g.Description != "USB-C with protection" || g.Notes != "ESD first" {
		t.Errorf("group = %+v", g)
	}
}

func TestReportDrawsGroupsAsSubgraphs(t *testing.T) {
	s := newService(t)
	buildCircuit(t, s, circuit("Grouped MCU board"))
	md, err := s.ProjectReport(context.Background(), "Grouped MCU board")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`subgraph g0["Power"]`, `subgraph g1["MCU"]`, `subgraph g2["Sensor"]`, "  end\n"} {
		if !strings.Contains(md, want) {
			t.Errorf("report lacks %q:\n%s", want, md)
		}
	}
}

// debugBoard is an MCU board with an SWD header (its VTref on 3V3), a
// series resistor to an LED.
var debugBoard = testutil.Circuit{
	Name:  "Debug board",
	Rails: []testutil.CircuitRail{{Name: "VBUS", V: 5}, {Name: "3V3", V: 3.3}},
	Parts: []testutil.CircuitPart{
		{Name: "U1", Ref: "SD1234", Supply: "VBUS", Output: "3V3"}, {Name: "U2 STM32", Kind: "ic"}, {Name: "J2 SWD", Kind: "connector"},
		// An LED drawn as a box, as one whose category isn't "diode" is.
		{Name: "R5", Kind: "resistor", Value: "1k"}, {Name: "D1", Kind: "ic", Value: "red"},
	},
	Nets: []testutil.CircuitNet{{Name: "GND", Kind: "ground"}},
	Pins: [][3]string{
		{"VBUS", "U1", "VIN"}, {"GND", "U1", "GND"}, {"3V3", "U1", "SW"},
		{"3V3", "U2 STM32", "VDD"}, {"GND", "U2 STM32", "VSS"}, {"SWDIO", "U2 STM32", "PA13"}, {"NRST", "U2 STM32", "NRST"}, {"PA5", "U2 STM32", "PA5"},
		{"3V3", "J2 SWD", "VTref"}, {"SWDIO", "J2 SWD", "SWDIO"}, {"NRST", "J2 SWD", "nRESET"}, {"GND", "J2 SWD", "GND"},
		{"PA5", "R5", "1"}, {"LED_A", "R5", "2"}, {"LED_A", "D1", "A"}, {"GND", "D1", "K"},
	},
	Assign: [][3]string{{"R5", "", "D1"}},
}

func TestDebugHeaderAndSeriesResistor(t *testing.T) {
	d := drawn{t, buildCircuit(t, newService(t), debugBoard).Connections.Diagram}
	// The header's VTref senses 3V3: the regulator makes it.
	for _, l := range d.links() {
		if strings.HasPrefix(l, "J2→") && strings.Contains(l, "power") {
			t.Errorf("the header is drawn as a source: %q", d.links())
		}
	}
	if got := d.links(); !slices.Contains(got, "U1→J2 power 3V3") {
		t.Errorf("links = %q, want U1 feeding the header's VTref", got)
	}
	// R5 is set to support D1, so it hangs by D1, not by U2 on PA5.
	if got := d.passives(); !slices.Contains(got, "R5 1k out D1 on LED_A+PA5") {
		t.Errorf("passives = %q, want R5 by D1", got)
	}
}

func TestSymbolsOfTheDriverBoard(t *testing.T) {
	s := newService(t)
	d := drawn{t, buildCircuit(t, s, circuit("Driver board")).Connections.Diagram}
	// A library part with a pin table is a generated symbol; a block of a
	// transistor kind whose pins are its terminals is a transistor.
	if got := d.pins("U1"); got != "VIN:top#3 VFB:left#4? SW:right#2 GND:bottom#1⏚" {
		t.Errorf("U1 pins = %s", got)
	}
	for title, want := range map[string]string{"Q1": "mosfet-n G=gate:left D=drain:top S=source:bottom⏚", "Q2": "npn B=base:left C=collector:top E=emitter:bottom⏚"} {
		p := d.part(title)
		var pins []string
		for _, pin := range p.Pins {
			g := ""
			if pin.Ground {
				g = "⏚"
			}
			pins = append(pins, pin.Name+"="+pin.Terminal+":"+pin.Side+g)
		}
		if got := p.Shape + " " + strings.Join(pins, " "); got != want {
			t.Errorf("%s = %s, want %s", title, got, want)
		}
	}
	// Each two-terminal has its symbol and its terminals at its nets: as
	// its pins were connected (D3's cathode on VBUS), else by default
	// (D1 conducts into ground).
	var got []string
	for _, p := range d.Passives {
		got = append(got, fmt.Sprintf("%s %s %s/%s on %s", p.Label, p.Shape, p.Ends[0], p.Ends[1], strings.Join(p.Nets, "+")))
	}
	sort.Strings(got)
	want := []string{
		"C1 capacitor-polarised +/- on VBUS",
		"C2 capacitor 1/2 on 3V3",
		"C3 capacitor 1/2 on XIN",
		"C4 capacitor 1/2 on XOUT",
		"D1 led anode/cathode on LED",
		"D2 led anode/cathode on 3V3+LED2_K",
		"D3 schottky cathode/anode on VBUS+MOTOR",
		"F1 fuse 2/1 on VBUS+VBUS_IN",
		"R2 resistor 1/2 on BTN+3V3",
		"R3 resistor 1/2 on CTRL+BASE",
		"SW1 switch 1/2 on BTN",
		"Y1 crystal 1/2 on XIN+XOUT",
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("passives:\n%s", strings.Join(got, "\n"))
	}
}

func TestARailsSourceMakesItsNet(t *testing.T) {
	s := newService(t)
	ctx := context.Background()
	p, _ := s.Lib.CreateProject(ctx, "ldo", "")
	v := 3.3
	if _, err := s.Lib.SetRail(ctx, library.Rail{ProjectID: p.ID, Name: "3V3", VNom: &v, Source: "U1"}); err != nil {
		t.Fatal(err)
	}
	// U1's pin on 3V3 isn't typed or named as an output; the rail says U1
	// supplies it, so U1 drives it: a power link to the connector it
	// feeds, and the capacitor by U1.
	ch := ConnectionsChange{
		Blocks:  []BlockChange{{Name: "U1 LDO", Kind: "ic"}, {Name: "J2 OUT", Kind: "connector"}, {Name: "C2", Kind: "capacitor", Value: "10µF"}},
		Connect: []ConnectChange{{Net: "3V3", Part: "U1 LDO", Pin: "P3"}, {Net: "GND", Part: "U1 LDO", Pin: "GND"}, {Net: "3V3", Part: "J2 OUT", Pin: "1"}, {Net: "GND", Part: "J2 OUT", Pin: "2"}, {Net: "3V3", Part: "C2"}},
	}
	if problems, err := s.ChangeConnections(ctx, "ldo", ch, library.SourceUser); err != nil || len(problems) > 0 {
		t.Fatal(err, problems)
	}
	d, _ := s.ProjectDetail(ctx, "ldo")
	dg := d.Connections.Diagram
	var links []string
	for _, l := range dg.Links {
		links = append(links, l.From+" "+l.Kind+" "+l.To)
	}
	if strings.Join(links, "\n") != "block:1 power block:2" {
		t.Errorf("links = %q", links)
	}
	if len(dg.Passives) != 1 || dg.Passives[0].Part != "block:1" || dg.Passives[0].Supports != "block:1" {
		t.Errorf("C2 = %+v", dg.Passives)
	}
}

func TestAPartWithMoreThanTwoPinsIsABox(t *testing.T) {
	s := newService(t)
	ctx := context.Background()
	if _, err := s.Lib.CreateProject(ctx, "usb", ""); err != nil {
		t.Fatal(err)
	}
	pp, err := s.AddToProject(ctx, "usb", "SD1234", library.ProjectPart{Designator: "U3"})
	if err != nil {
		t.Fatal(err)
	}
	part, _ := s.Lib.PartByID(ctx, pp.PartID)
	cat := "ESD protection TVS diode array"
	if _, err := s.Lib.UpdatePart(ctx, part.ID, library.PartUpdate{Category: &cat}); err != nil {
		t.Fatal(err)
	}
	pins := []library.Pin{{Number: "1", Name: "I/O1"}, {Number: "2", Name: "GND", Type: "G"}, {Number: "3", Name: "I/O2"}, {Number: "4", Name: "I/O2"}, {Number: "5", Name: "VBUS", Type: "P"}, {Number: "6", Name: "I/O1"}}
	if err := s.Lib.RecordPins(ctx, part.DatasheetID, "SOT-23-6", pins, library.SourceUser); err != nil {
		t.Fatal(err)
	}
	ch := ConnectionsChange{
		Blocks:  []BlockChange{{Name: "J1 USB", Kind: "connector"}},
		Connect: []ConnectChange{{Net: "D+", Part: "J1 USB", Pin: "D+"}, {Net: "D-", Part: "J1 USB", Pin: "D-"}, {Net: "VBUS", Part: "J1 USB", Pin: "VBUS"}, {Net: "D+", Part: "U3", Pin: "1"}, {Net: "D-", Part: "U3", Pin: "3"}, {Net: "VBUS", Part: "U3", Pin: "5"}, {Net: "GND", Part: "U3", Pin: "2"}},
	}
	if problems, err := s.ChangeConnections(ctx, "usb", ch, library.SourceUser); err != nil || len(problems) > 0 {
		t.Fatal(err, problems)
	}
	d, _ := s.ProjectDetail(ctx, "usb")
	dg := d.Connections.Diagram
	if len(dg.Parts) != 2 || dg.Parts[0].Shape != "ic" || !dg.Parts[0].Full || len(dg.Parts[0].Pins) != 6 || len(dg.Passives) != 0 {
		t.Errorf("a six-pin TVS array is a box with its pins: parts %+v, passives %+v", dg.Parts, dg.Passives)
	}
}

func TestPassivesReachedOnlyThroughAnotherAreChained(t *testing.T) {
	s := newService(t)
	d := drawn{t, buildCircuit(t, s, circuit("Boost board")).Connections.Diagram}
	got := d.passives()
	var chained []string
	for _, p := range d.Passives {
		if p.Mount == MountChain {
			chained = append(chained, p.Label+" after "+p.Part+" on "+strings.Join(p.Nets, "+"))
		}
	}
	sort.Strings(chained)
	// C2 is on VOUT, which no part is on: it chains off L1, whose far net
	// it is. D1 chains off R5. R4 hangs from FB at U1, VOUT at its far end.
	if strings.Join(chained, "; ") != "C2 after block:3 on VOUT; D1 after block:6 on LEDK" {
		t.Errorf("chained = %q\nall: %q", chained, got)
	}
	if !slices.ContainsFunc(got, func(s string) bool { return strings.HasPrefix(s, "R4 100k ") && strings.HasSuffix(s, " U1 on FB+VOUT") }) {
		t.Errorf("R4 = %q", got)
	}
}
