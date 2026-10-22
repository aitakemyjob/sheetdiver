package core

import (
	"context"
	"sort"
	"strings"
	"testing"

	"github.com/aitakemyjob/sheetdiver/internal/library"
)

func TestConnectionsDiagramAndChecks(t *testing.T) {
	s := newService(t)
	ctx := context.Background()
	p, err := s.Lib.CreateProject(ctx, "board", "")
	if err != nil {
		t.Fatal(err)
	}
	v12, v33 := 12.0, 3.3
	for _, r := range []library.Rail{{ProjectID: p.ID, Name: "VIN", VNom: &v12}, {ProjectID: p.ID, Name: "3V3", VNom: &v33}} {
		if _, err := s.Lib.SetRail(ctx, r); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := s.AddToProject(ctx, "board", "SD1234", library.ProjectPart{Designator: "U1", SupplyRail: "VIN", OutputRail: "3V3"}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.AddToProject(ctx, "board", "SD1235", library.ProjectPart{Designator: "U2", SupplyRail: "3V3"}); err != nil {
		t.Fatal(err)
	}
	bus := "I2C1"
	problems, err := s.ChangeConnections(ctx, "board", ConnectionsChange{
		Blocks: []BlockChange{{Name: "Battery"}},
		Nets:   []NetChange{{Name: "GND", Kind: library.NetGround}, {Name: "SDA", Bus: &bus}},
		Connect: []ConnectChange{
			{Net: "VIN", Part: "Battery"},
			{Net: "VIN", Part: "U1.VIN"}, // part.pin, by pin name
			{Net: "GND", Part: "U1", Pin: "1"},
			{Net: "3V3", Part: "u2", Pin: "VIN"},
			{Net: "SDA", Part: "U2", Pin: "GND"}, // a ground pin on a signal
			{Net: "SDA", Part: "U1", Pin: "SW"},
			{Net: "SDA", Part: "SD1235", Pin: "2"}, // by part number; two outputs
			{Net: "INT", Part: "U2", Pin: "XYZ"},
			{Net: "X", Part: "U9", Pin: "1"},
		},
		Positions: []PositionChange{{Node: "U1", X: 10, Y: 20}, {Node: "nowhere"}},
	}, library.SourceAgent)
	if err != nil {
		t.Fatal(err)
	}
	if len(problems) != 3 || !strings.Contains(problems[0], "no pin “XYZ”") || !strings.Contains(problems[1], "U9: not in the BOM") || !strings.Contains(problems[2], "nowhere") {
		t.Errorf("problems = %q", problems)
	}

	d, err := s.ProjectDetail(ctx, "board")
	if err != nil {
		t.Fatal(err)
	}
	c := d.Connections
	var vin ConnectionRow
	for _, r := range c.Connections {
		if r.Endpoint == "U1" && r.Net == "VIN" {
			vin = r
		}
	}
	if vin.Pin != "3" || vin.PinName != "VIN" || vin.PinType != "P" || vin.Source != library.SourceAgent {
		t.Errorf("U1.VIN is stored as its pin number, with its name: %+v", vin)
	}
	// The links between parts, from the connection table only: the
	// battery feeds U1 on VIN and U1 and U2 share I2C1 (one link,
	// labelled by the bus). U1 makes 3V3 in the BOM, but has no pin on it
	// in the table, so there's no 3V3 link. Ground isn't linked. (The
	// drawing itself is tested in diagram_test.go.)
	title := map[string]string{}
	for _, p := range c.Diagram.Parts {
		title[p.ID] = p.Title
		if p.Title == "U1 SD1234" && (p.X == nil || *p.X != 10) {
			t.Errorf("U1 keeps its saved position: %+v", p)
		}
	}
	var got []string
	for _, l := range c.Diagram.Links {
		got = append(got, title[l.From]+"→"+title[l.To]+" "+l.Kind+" "+l.Label)
	}
	// A signal link runs from the part connected to it first.
	want := []string{"Battery→U1 SD1234 power VIN", "U2 SD1235→U1 SD1234 signal I2C1"}
	sort.Strings(got)
	if strings.Join(got, "; ") != strings.Join(want, "; ") {
		t.Errorf("links = %q, want %q", got, want)
	}

	checks := map[string]string{}
	for _, ch := range d.Checks {
		if ch.Area == "connections" {
			checks[ch.Message] = ch.Level
		}
	}
	for msg, level := range map[string]string{
		"U2 pin 1 (GND) is a ground pin but is on SDA, which isn't ground": "error",
		"SDA has more than one output on it: U1.SW, U2.SW":                 "warn",
		"U2 has no pin “XYZ” in its datasheet's pin table (INT)":           "warn",
		"INT only goes to U2": "warn",
	} {
		if checks[msg] != level {
			t.Errorf("missing %s check %q in %v", level, msg, checks)
		}
	}
	if d.Checks[0].Level != "error" {
		t.Error("errors come first")
	}

	// A supply pin moved to another rail, and a part left half connected.
	if _, err := s.ChangeConnections(ctx, "board", ConnectionsChange{Connect: []ConnectChange{{Net: "VIN", Part: "U2", Pin: "3"}, {Net: "GND", Part: "U1", Pin: "1", Remove: true}}}, library.SourceUser); err != nil {
		t.Fatal(err)
	}
	d, _ = s.ProjectDetail(ctx, "board")
	var msgs []string
	for _, ch := range d.Checks {
		msgs = append(msgs, ch.Message)
	}
	all := strings.Join(msgs, "\n")
	for _, want := range []string{"U2 pin 3 (VIN) is on VIN, but the BOM has U2 powered from 3V3", "U1: pins that must be connected aren't: GND (1)"} {
		if !strings.Contains(all, want) {
			t.Errorf("missing %q in\n%s", want, all)
		}
	}
}

func TestConnectionsInTheReport(t *testing.T) {
	s := newService(t)
	ctx := context.Background()
	if _, err := s.Lib.CreateProject(ctx, "board", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := s.AddToProject(ctx, "board", "SD1234", library.ProjectPart{Designator: "U1"}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ChangeConnections(ctx, "board", ConnectionsChange{
		Blocks:  []BlockChange{{Name: "USB-C"}},
		Connect: []ConnectChange{{Net: "VBUS", Part: "USB-C"}, {Net: "VBUS", Part: "U1", Pin: "VIN"}, {Net: "GND", Part: "U1", Pin: "1"}},
		Nets:    []NetChange{{Name: "VBUS", Kind: library.NetPower}},
	}, library.SourceUser); err != nil {
		t.Fatal(err)
	}
	md, err := s.ProjectReport(ctx, "board")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"## Block diagram", "```mermaid\nflowchart LR", `n0["U1 SD1234"]`, `n1(["USB-C"])`, `n1 ==>|"VBUS"| n0`, "## Connections", "| VBUS | power | U1.3 (VIN), USB-C |", "| GND | ground | U1.1 (GND) |"} {
		if !strings.Contains(md, want) {
			t.Errorf("report lacks %q:\n%s", want, md)
		}
	}
}

func TestPinoutsListEveryPinWithWhatItMeets(t *testing.T) {
	s := newService(t)
	ctx := context.Background()
	if _, err := s.Lib.CreateProject(ctx, "board", ""); err != nil {
		t.Fatal(err)
	}
	for _, pp := range []struct{ part, des string }{{"SD1234", "U1"}, {"SD1235", "U2"}} {
		if _, err := s.AddToProject(ctx, "board", pp.part, library.ProjectPart{Designator: pp.des}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := s.ChangeConnections(ctx, "board", ConnectionsChange{
		Blocks: []BlockChange{{Name: "J1", Kind: "connector"}, {Name: "C1", Kind: "capacitor", Value: "10µF"}},
		Connect: []ConnectChange{
			{Net: "VBUS", Part: "J1", Pin: "VBUS"}, {Net: "VBUS", Part: "U1", Pin: "VIN"}, {Net: "VBUS", Part: "C1"},
			{Net: "GND", Part: "U1", Pin: "GND"}, {Net: "GND", Part: "J1", Pin: "GND"},
			{Net: "FB", Part: "U1", Pin: "VFB"}, {Net: "FB", Part: "U2", Pin: "VFB"}, {Net: "SW", Part: "U1", Pin: "SW"}, {Net: "SW", Part: "U2"},
		},
	}, library.SourceUser); err != nil {
		t.Fatal(err)
	}
	d, err := s.ProjectDetail(ctx, "board")
	if err != nil {
		t.Fatal(err)
	}
	var titles []string
	byTitle := map[string]Pinout{}
	for _, po := range d.Connections.Pinouts {
		titles = append(titles, po.Title)
		byTitle[po.Title] = po
	}
	// Parts and connectors, not passives.
	if strings.Join(titles, "|") != "U1 SD1234|U2 SD1235|J1" {
		t.Fatalf("pinouts = %q", titles)
	}
	u1 := byTitle["U1 SD1234"]
	got := map[string]PinoutRow{}
	for _, p := range u1.Pins {
		got[p.Name] = p
	}
	// Every pin of the pin table, in order, connected or not.
	if len(u1.Pins) != 4 || u1.Pins[0].Name != "GND" || got["SW"].Description != "Switch node." {
		t.Errorf("U1 pins = %+v", u1.Pins)
	}
	if strings.Join(got["VIN"].To, ", ") != "J1.VBUS, C1 10µF" || got["VIN"].Net != "VBUS" {
		t.Errorf("VIN = %+v", got["VIN"])
	}
	if got["GND"].NetKind != "ground" || len(got["GND"].To) != 0 {
		t.Errorf("ground lists nothing else: %+v", got["GND"])
	}
	// A part connected without naming a pin is listed as itself.
	if strings.Join(got["SW"].To, ", ") != "U2" {
		t.Errorf("SW = %+v", got["SW"])
	}
	if strings.Join(got["VFB"].To, ", ") != "U2.VFB" {
		t.Errorf("VFB = %+v", got["VFB"])
	}
	md, _ := s.ProjectReport(ctx, "board")
	for _, want := range []string{"### U1 SD1234 pins", "| 3 | VIN | VBUS | J1.VBUS, C1 10µF |", "| 4 | VFB | FB | U2.VFB |"} {
		if !strings.Contains(md, want) {
			t.Errorf("report lacks %q", want)
		}
	}
}

func TestChangeConnectionsEdits(t *testing.T) {
	s := newService(t)
	ctx := context.Background()
	p, _ := s.Lib.CreateProject(ctx, "board", "")
	if _, err := s.AddToProject(ctx, "board", "SD1234", library.ProjectPart{Designator: "U1"}); err != nil {
		t.Fatal(err)
	}
	apply := func(ch ConnectionsChange) []string {
		t.Helper()
		problems, err := s.ChangeConnections(ctx, "board", ch, library.SourceUser)
		if err != nil {
			t.Fatal(err)
		}
		return problems
	}
	v := 1.8
	notes := "I/O"
	apply(ConnectionsChange{
		Blocks:  []BlockChange{{Name: "J1", Kind: "connector"}},
		Nets:    []NetChange{{Name: "SDA", Voltage: &v, Notes: &notes}},
		Connect: []ConnectChange{{Net: "SDA", Part: "U1", Pin: "VFB"}, {Net: "SDA", Part: "J1", Pin: "2"}, {Net: "GND", Part: "U1", Pin: "1"}},
		// Placed by block name and by designator.
		Positions: []PositionChange{{Node: "J1", X: 5, Y: 6}, {Node: "U1", X: 300, Y: 6}},
	})
	// A partial net change keeps what it doesn't set.
	apply(ConnectionsChange{Nets: []NetChange{{Name: "sda", Kind: library.NetSignal}}})
	nets, _ := s.Lib.Nets(ctx, p.ID)
	for _, n := range nets {
		if n.Name == "SDA" && (n.Voltage == nil || *n.Voltage != 1.8 || n.Notes != "I/O") {
			t.Errorf("SDA lost its voltage or notes: %+v", n)
		}
	}
	if l, _ := s.Lib.Layout(ctx, p.ID); len(l) != 2 {
		t.Errorf("layout = %+v", l)
	}
	// What can't be done is reported, one line each; the rest is applied.
	problems := apply(ConnectionsChange{
		Blocks:  []BlockChange{{Name: "X", Kind: "transistor"}, {Name: "nope", Remove: true}},
		Nets:    []NetChange{{Name: "nope", Remove: true}, {Name: " ", Kind: "power"}},
		Connect: []ConnectChange{{Net: "SDA", Part: "U1", Pin: "VFB", Remove: true}, {Net: "SDA", Part: "U1", Pin: "VFB", Remove: true}},
	})
	if len(problems) != 5 || !strings.Contains(problems[0], "kind is one of ic, connector") || !strings.Contains(problems[4], "U1 VFB on SDA") {
		t.Errorf("problems = %q", problems)
	}
	// Removing a block takes its connections and place; a net its pins.
	apply(ConnectionsChange{Blocks: []BlockChange{{Name: "J1", Remove: true}}, Nets: []NetChange{{Name: "GND", Remove: true}}, ResetLayout: true})
	if cs, _ := s.Lib.Connections(ctx, p.ID); len(cs) != 0 {
		t.Errorf("connections left = %+v", cs)
	}
	if l, _ := s.Lib.Layout(ctx, p.ID); len(l) != 0 {
		t.Errorf("layout after reset = %+v", l)
	}
	if _, err := s.ChangeConnections(ctx, "nope", ConnectionsChange{}, library.SourceUser); err == nil {
		t.Error("an unknown project is an error")
	}
}

func TestEntryPinsFollowThePartsPackage(t *testing.T) {
	s := newService(t)
	ctx := context.Background()
	if _, err := s.Lib.CreateProject(ctx, "board", ""); err != nil {
		t.Fatal(err)
	}
	pp, err := s.AddToProject(ctx, "board", "SD1234", library.ProjectPart{Designator: "U1"})
	if err != nil {
		t.Fatal(err)
	}
	part, _ := s.Lib.PartByID(ctx, pp.PartID)
	// Two packages with different numbering; the part is the QFN one.
	if err := s.Lib.RecordPins(ctx, part.DatasheetID, "SOT-23", []library.Pin{{Number: "1", Name: "GND"}, {Number: "2", Name: "VIN"}}, library.SourceUser); err != nil {
		t.Fatal(err)
	}
	if err := s.Lib.RecordPins(ctx, part.DatasheetID, "QFN", []library.Pin{{Number: "7", Name: "VIN"}, {Number: "8", Name: "GND"}}, library.SourceUser); err != nil {
		t.Fatal(err)
	}
	pkg := "QFN-16"
	if _, err := s.Lib.UpdatePart(ctx, part.ID, library.PartUpdate{Package: &pkg}); err != nil {
		t.Fatal(err)
	}
	parts, _ := s.Lib.ProjectParts(ctx, pp.ProjectID)
	pins := s.entryPins(ctx, parts)[pp.ID]
	if len(pins) != 2 || pins[0].Number != "7" {
		t.Errorf("pins = %+v, want the QFN's", pins)
	}
}

func TestPinsWithoutANumberAndPinsThatMustBeConnected(t *testing.T) {
	s := newService(t)
	ctx := context.Background()
	p, _ := s.Lib.CreateProject(ctx, "board", "")
	pp, err := s.AddToProject(ctx, "board", "SD1234", library.ProjectPart{Designator: "U2"})
	if err != nil {
		t.Fatal(err)
	}
	part, _ := s.Lib.PartByID(ctx, pp.PartID)
	// As the TPS63900's pin table: a thermal pad with no number, and pins
	// whose descriptions say they mustn't be left open.
	if err := s.Lib.RecordPins(ctx, part.DatasheetID, "", []library.Pin{
		{Number: "1", Name: "EN", Type: "I", Description: "Device enable. It must not be left open."},
		{Number: "6", Name: "VOUT"},
		{Number: "8", Name: "GND"},
		{Number: "10", Name: "VIN"},
		{Name: "Thermal Pad", Description: "Connect this pin to ground for correct operation."},
		{Number: "11", Name: "NC", Description: "No connection."},
	}, library.SourceUser); err != nil {
		t.Fatal(err)
	}
	connect := func(ch ...ConnectChange) {
		t.Helper()
		if problems, err := s.ChangeConnections(ctx, "board", ConnectionsChange{Nets: []NetChange{{Name: "GND", Kind: library.NetGround}}, Connect: ch}, library.SourceUser); err != nil || len(problems) > 0 {
			t.Fatalf("%v %q", err, problems)
		}
	}
	connect(ConnectChange{Net: "GND", Part: "U2", Pin: "Thermal Pad"}, ConnectChange{Net: "GND", Part: "U2", Pin: "8"}, ConnectChange{Net: "VBAT", Part: "U2", Pin: "VIN"})
	// The pad is stored by its name, not as a pinless connection.
	cs, _ := s.Lib.Connections(ctx, p.ID)
	var pad *library.Connection
	for i := range cs {
		if cs[i].Pin == "" {
			t.Errorf("a pinless connection was made: %+v", cs[i])
		}
		if cs[i].Pin == "Thermal Pad" {
			pad = &cs[i]
		}
	}
	if pad == nil {
		t.Fatalf("the thermal pad isn't connected: %+v", cs)
	}
	d, _ := s.ProjectDetail(ctx, "board")
	for _, po := range d.Connections.Pinouts {
		for _, row := range po.Pins {
			if row.Name == "Thermal Pad" && row.Net != "GND" {
				t.Errorf("the pin table shows the pad as %+v", row)
			}
		}
	}
	checks := func() string {
		d, _ := s.ProjectDetail(ctx, "board")
		var out []string
		for _, ch := range d.Checks {
			if ch.Area == "connections" {
				out = append(out, ch.Message)
			}
		}
		return strings.Join(out, "\n")
	}
	// EN must not be left open, VOUT can; NC is fine open.
	if got := checks(); !strings.Contains(got, "U2: pins that must be connected aren't: EN (1)") || strings.Contains(got, "Thermal Pad") || strings.Contains(got, "VOUT") || strings.Contains(got, "NC") {
		t.Errorf("checks:\n%s", got)
	}
	// Without the pad, it's flagged too.
	if _, err := s.ChangeConnections(ctx, "board", ConnectionsChange{Connect: []ConnectChange{{Net: "GND", Part: "U2", Pin: "Thermal Pad", Remove: true}}}, library.SourceUser); err != nil {
		t.Fatal(err)
	}
	if got := checks(); !strings.Contains(got, "U2: pins that must be connected aren't: Thermal Pad, EN (1)") {
		t.Errorf("checks:\n%s", got)
	}
}

func TestOneDesignatorOfAnEntryWithSeveral(t *testing.T) {
	s := newService(t)
	ctx := context.Background()
	if _, err := s.Lib.CreateProject(ctx, "board", ""); err != nil {
		t.Fatal(err)
	}
	// Two pull-downs in one BOM line, and two of the same IC.
	res, _ := s.Lib.Resolve(ctx, "SD1235")
	cat := "resistor"
	if _, err := s.Lib.UpdatePart(ctx, res.Part.ID, library.PartUpdate{Category: &cat}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.AddToProject(ctx, "board", "SD1235", library.ProjectPart{Designator: "R1,R2", Qty: 2, Role: "5.1k"}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.AddToProject(ctx, "board", "SD1234", library.ProjectPart{Designator: "U1 U2", Qty: 2}); err != nil {
		t.Fatal(err)
	}
	problems, err := s.ChangeConnections(ctx, "board", ConnectionsChange{
		Blocks: []BlockChange{{Name: "J1", Kind: "connector"}},
		Connect: []ConnectChange{
			{Net: "CC1", Part: "J1", Pin: "CC1"}, {Net: "CC2", Part: "J1", Pin: "CC2"},
			{Net: "CC1", Part: "R1"}, {Net: "CC2", Part: "r2"}, // each resistor on its own net
			{Net: "A", Part: "U1", Pin: "VIN"}, {Net: "B", Part: "U2.VIN"}, // each IC on its own
		},
		Positions: []PositionChange{{Node: "U2", X: 400, Y: 50}},
	}, library.SourceUser)
	if err != nil || len(problems) > 0 {
		t.Fatalf("%v %q", err, problems)
	}
	d, _ := s.ProjectDetail(ctx, "board")
	ends := map[string]string{}
	for _, r := range d.Connections.Connections {
		if r.EntryID != 0 {
			ends[r.Endpoint] = r.Net
		}
	}
	if ends["R1"] != "CC1" || ends["R2"] != "CC2" || ends["U1"] != "A" || ends["U2"] != "B" {
		t.Errorf("connections by designator = %v", ends)
	}
	// The diagram: two resistors, each on its own CC pin, and two
	// boxes, U2 where it was placed.
	dg := drawn{t, d.Connections.Diagram}
	got := dg.passives()
	if strings.Join(got, "; ") != "R1 5.1k out J1 on CC1; R2 5.1k out J1 on CC2" {
		t.Errorf("passives = %q", got)
	}
	if u2 := dg.part("U2"); u2.X == nil || *u2.X != 400 || dg.part("U1").ID == u2.ID {
		t.Errorf("U2 = %+v", u2)
	}
	// A pin table each, and none for the pair as a whole.
	var titles []string
	for _, po := range d.Connections.Pinouts {
		titles = append(titles, po.Title)
	}
	if strings.Join(titles, "|") != "U1 SD1234|U2 SD1234|J1" {
		t.Errorf("pin tables = %q", titles)
	}
	// Disconnecting one leaves the other.
	if _, err := s.ChangeConnections(ctx, "board", ConnectionsChange{Connect: []ConnectChange{{Net: "CC1", Part: "R1", Remove: true}}}, library.SourceUser); err != nil {
		t.Fatal(err)
	}
	d, _ = s.ProjectDetail(ctx, "board")
	n := 0
	for _, r := range d.Connections.Connections {
		if r.Endpoint == "R2" {
			n++
		}
		if r.Endpoint == "R1" {
			t.Errorf("R1 is still connected: %+v", r)
		}
	}
	if n != 1 {
		t.Error("R2 stays connected")
	}
}
