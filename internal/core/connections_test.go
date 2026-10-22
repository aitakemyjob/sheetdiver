package core

import (
	"context"
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
	// The diagram: the battery feeds U1 on VIN, U1 feeds U2 on 3V3, and
	// U1 and U2 share I2C1: one link, labelled by the bus, from U2, which
	// has more pins on it. Ground isn't drawn. Rails go in name order.
	label := map[string]string{}
	for _, n := range c.Diagram.Nodes {
		label[n.ID] = n.Label
		if n.Label == "U1" && (n.X == nil || *n.X != 10) {
			t.Errorf("U1 keeps its saved position: %+v", n)
		}
	}
	var got []string
	for _, e := range c.Diagram.Edges {
		got = append(got, label[e.From]+"→"+label[e.To]+" "+e.Kind+" "+e.Label)
	}
	want := []string{"U1→U2 power 3V3", "Battery→U1 power VIN", "U2→U1 signal I2C1"}
	if strings.Join(got, "; ") != strings.Join(want, "; ") {
		t.Errorf("edges = %q, want %q", got, want)
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
	for _, want := range []string{"U2 pin 3 (VIN) is on VIN, but the BOM has U2 powered from 3V3", "U1: supply or ground pins not connected: GND (1)"} {
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
	for _, want := range []string{"## Block diagram", "```mermaid\nflowchart LR", `n0["U1<br/>SD1234"]`, `n1(["USB-C"])`, `n1 ==>|"VBUS"| n0`, "## Connections", "| VBUS | power | U1.3 (VIN), USB-C |", "| GND | ground | U1.1 (GND) |"} {
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
			{Net: "FB", Part: "U1", Pin: "VFB"}, {Net: "FB", Part: "U2", Pin: "VFB"},
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
	if len(u1.Pins) != 4 || u1.Pins[0].Name != "GND" || got["SW"].Net != "" || got["SW"].Description != "Switch node." {
		t.Errorf("U1 pins = %+v", u1.Pins)
	}
	if strings.Join(got["VIN"].To, ", ") != "J1.VBUS, C1 10µF" || got["VIN"].Net != "VBUS" {
		t.Errorf("VIN = %+v", got["VIN"])
	}
	if got["GND"].NetKind != "ground" || len(got["GND"].To) != 0 {
		t.Errorf("ground lists nothing else: %+v", got["GND"])
	}
	if strings.Join(got["VFB"].To, ", ") != "U2.VFB" {
		t.Errorf("VFB = %+v", got["VFB"])
	}
	md, _ := s.ProjectReport(ctx, "board")
	for _, want := range []string{"### U1 SD1234 pins", "| 3 | VIN | VBUS | J1.VBUS, C1 10µF |", "| 2 | SW | not connected |  |"} {
		if !strings.Contains(md, want) {
			t.Errorf("report lacks %q", want)
		}
	}
}
