package core

import (
	"reflect"
	"strings"
	"testing"
)

// A board for the subgroup rules: U1 on an undriven 3V3 rail in MCU
// with C1, C2 (its decoupling) and R1 (a pull-up, by U1 too); J1 in
// Output with R4 on 3V3 leading to D2 on LED_A, a net of passives only;
// in Power, L1 from LX to VOUT with C3 on VOUT, which reaches no part
// of Power.
func subgroupBoard() subgroupFacts {
	parts := []DiagramPart{{ID: "part:1", Group: "MCU"}, {ID: "block:1", Group: "Output"}, {ID: "block:9", Group: "Power"}}
	passives := []DiagramPassive{
		{ID: "block:2", Label: "C1", Mount: MountLane, Part: "part:1", Group: "MCU", Nets: []string{"3V3"}},
		{ID: "block:3", Label: "C2", Mount: MountLane, Part: "part:1", Group: "MCU", Nets: []string{"3V3"}},
		{ID: "block:4", Label: "R1", Mount: MountLane, Part: "part:1", Group: "MCU", Nets: []string{"SDA", "3V3"}},
		{ID: "block:10", Label: "D9", Mount: MountLane, Part: "part:1", Group: "MCU", Nets: []string{"3V3", "SDA"}},
		{ID: "block:5", Label: "R4", Mount: MountOut, Part: "block:1", Group: "Output", Nets: []string{"3V3", "LED_A"}},
		{ID: "block:6", Label: "D2", Mount: MountChain, Part: "block:5", Group: "Output", Nets: []string{"LED_A"}},
		{ID: "block:7", Label: "L1", Mount: MountWire, Part: "block:9", Group: "Power", Nets: []string{"LX", "VOUT"}},
		{ID: "block:8", Label: "C3", Mount: MountLane, Part: "block:9", Group: "Power", Nets: []string{"VOUT"}},
	}
	drawn := map[string]bool{"3v3|mcu": true, "sda|mcu": true, "3v3|output": true, "lx|power": true}
	return subgroupFacts{
		parts:    parts,
		passives: passives,
		undriven: func(net, group string) bool { return strings.EqualFold(net, "3V3") },
		drawn:    func(net, group string) bool { return drawn[strings.ToLower(net)+"|"+strings.ToLower(group)] },
	}
}

func members(sgs []DiagramSubgroup) []string {
	var out []string
	for _, sg := range sgs {
		out = append(out, sg.Kind+":"+sg.Name+"@"+sg.Group+"["+sg.Net+"] "+strings.Join(sg.Members, ","))
	}
	return out
}

func TestSubgroupRulesFindRailsAndChains(t *testing.T) {
	got := members(subgroups(subgroupBoard()))
	want := []string{
		// D2 chains off R4 at the output connector; C3 off L1, whose far net
		// VOUT reaches no part of Power.
		"chain:@Output[LED_A] block:5,block:6",
		"chain:@Power[VOUT] block:7,block:8",
		// U1's two capacitors on the undriven rail share a bus; its pull-up
		// hangs off SDA, not the rail, so it isn't one of them, nor is the
		// diode from the rail to SDA, which hangs off SDA too.
		"rail:@MCU[3V3] block:2,block:3",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("subgroups =\n  %s\nwant\n  %s", strings.Join(got, "\n  "), strings.Join(want, "\n  "))
	}
}

func TestNamedSubgroupsTakeTheirRailMatesAndChainLeads(t *testing.T) {
	f := subgroupBoard()
	// C1 named in brings C2 (same rail, same part); D2 named in brings R4;
	// a part named in is a member like any other.
	cluster := func(label, name string) {
		for i := range f.passives {
			if f.passives[i].Label == label {
				f.passives[i].Cluster = name
			}
		}
	}
	cluster("C1", "VDD")
	cluster("D2", "PWR LED")
	f.parts[1].Cluster = "Out"
	got := members(subgroups(f))
	want := []string{
		"named:Out@Output[] block:1",
		"named:VDD@MCU[] block:2,block:3",
		"named:PWR LED@Output[] block:6,block:5",
		"chain:@Power[VOUT] block:7,block:8",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("subgroups =\n  %s\nwant\n  %s", strings.Join(got, "\n  "), strings.Join(want, "\n  "))
	}
}

func TestARailWithOnePassiveIsNoSubgroup(t *testing.T) {
	f := subgroupBoard()
	f.passives = f.passives[:1] // C1 alone on 3V3
	if got := members(subgroups(f)); len(got) != 0 {
		t.Errorf("subgroups = %v", got)
	}
	// Nor a driven rail's passives.
	f = subgroupBoard()
	f.undriven = func(string, string) bool { return false }
	for _, sg := range subgroups(f) {
		if sg.Kind == SubgroupRail {
			t.Errorf("a driven rail made %+v", sg)
		}
	}
}
