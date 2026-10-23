package core

import (
	"slices"
	"strings"
)

// DiagramSubgroup is a set of a group's parts laid out and wired up as
// one unit, in a faint frame: named by someone, or found by a rule.
type DiagramSubgroup struct {
	// Name is as named; "" for one a rule found.
	Name string `json:"name,omitempty"`
	// Group is the group it's inside ("" for none).
	Group string `json:"group,omitempty"`
	// Kind is "named"; "rail" for two or more passives hanging off a rail
	// nothing in the group drives, by one part; "chain" for a passive
	// reached only through another, with that passive leading.
	Kind string `json:"kind"`
	// Net is the rail, or the net a chain hangs on.
	Net string `json:"net,omitempty"`
	// Members are node ids: parts and passives; a chain's lead first.
	Members []string `json:"members"`
}

const (
	SubgroupNamed = "named"
	SubgroupRail  = "rail"
	SubgroupChain = "chain"
)

// subgroupFacts is what the subgroup rules decide on: the drawn parts and
// passives with their groups, and which nets are rails nothing in their
// group drives.
type subgroupFacts struct {
	parts    []DiagramPart
	passives []DiagramPassive
	// undriven reports whether a net is a rail nothing in a group drives.
	undriven func(net, group string) bool
	// drawn reports whether a net reaches a part in a group.
	drawn func(net, group string) bool
}

// subgroupRule finds subgroups among what no earlier rule has taken.
type subgroupRule func(f subgroupFacts, taken map[string]bool) []DiagramSubgroup

// subgroupRules are tried in this order; a member goes to the first
// rule that takes it.
var subgroupRules = []subgroupRule{namedSubgroups, chainSubgroups, railSubgroups}

// subgroups finds a diagram's subgroups.
func subgroups(f subgroupFacts) []DiagramSubgroup {
	out := []DiagramSubgroup{}
	taken := map[string]bool{}
	for _, rule := range subgroupRules {
		for _, sg := range rule(f, taken) {
			for _, id := range sg.Members {
				taken[id] = true
			}
			out = append(out, sg)
		}
	}
	return out
}

// namedSubgroups groups the parts and passives given one name, within
// their group. A passive named in pulls the rest of its rail's passives
// by its part along, and a chained one the passive it chains off.
func namedSubgroups(f subgroupFacts, taken map[string]bool) []DiagramSubgroup {
	type key struct{ name, group string }
	byKey := map[key]*DiagramSubgroup{}
	var order []key
	add := func(name, group, id string) {
		if name == "" || taken[id] {
			return
		}
		k := key{strings.ToLower(name), strings.ToLower(group)}
		sg := byKey[k]
		if sg == nil {
			sg = &DiagramSubgroup{Name: name, Group: group, Kind: SubgroupNamed, Members: []string{}}
			byKey[k] = sg
			order = append(order, k)
		}
		if !slices.Contains(sg.Members, id) {
			sg.Members = append(sg.Members, id)
		}
	}
	for _, p := range f.parts {
		add(p.Cluster, p.Group, p.ID)
	}
	for _, q := range f.passives {
		add(q.Cluster, q.Group, q.ID)
	}
	for _, k := range order {
		sg := byKey[k]
		for _, id := range slices.Clone(sg.Members) {
			q, ok := passiveByID(f.passives, id)
			if !ok {
				continue
			}
			if onRail(f, q) {
				for _, x := range railMates(f, q) {
					add(sg.Name, sg.Group, x.ID)
				}
			}
			if lead, ok := chainLeadOf(f, q); ok {
				add(sg.Name, sg.Group, lead.ID)
			}
		}
	}
	out := make([]DiagramSubgroup, 0, len(order))
	for _, k := range order {
		out = append(out, *byKey[k])
	}
	return out
}

// chainSubgroups puts each passive reached only through another with
// that passive, which leads: the ones mounted as chains, and in a group,
// those whose net reaches no part of the group but through it.
func chainSubgroups(f subgroupFacts, taken map[string]bool) []DiagramSubgroup {
	byLead := map[string]*DiagramSubgroup{}
	var order []string
	for _, q := range f.passives {
		if taken[q.ID] {
			continue
		}
		lead, ok := chainLeadOf(f, q)
		if !ok || taken[lead.ID] {
			continue
		}
		sg := byLead[lead.ID]
		if sg == nil {
			sg = &DiagramSubgroup{Group: q.Group, Kind: SubgroupChain, Net: q.Nets[0], Members: []string{lead.ID}}
			byLead[lead.ID] = sg
			order = append(order, lead.ID)
		}
		sg.Members = append(sg.Members, q.ID)
	}
	out := make([]DiagramSubgroup, 0, len(order))
	for _, id := range order {
		out = append(out, *byLead[id])
	}
	return out
}

// railSubgroups puts two or more passives hanging off one rail nothing
// in their group drives, by one part, on one bus.
func railSubgroups(f subgroupFacts, taken map[string]bool) []DiagramSubgroup {
	type key struct{ net, part, group string }
	byKey := map[key]*DiagramSubgroup{}
	var order []key
	for _, q := range f.passives {
		if taken[q.ID] || !onRail(f, q) {
			continue
		}
		k := key{strings.ToLower(q.Nets[0]), q.Part, strings.ToLower(q.Group)}
		sg := byKey[k]
		if sg == nil {
			sg = &DiagramSubgroup{Group: q.Group, Kind: SubgroupRail, Net: q.Nets[0], Members: []string{}}
			byKey[k] = sg
			order = append(order, k)
		}
		sg.Members = append(sg.Members, q.ID)
	}
	out := []DiagramSubgroup{}
	for _, k := range order {
		if sg := byKey[k]; len(sg.Members) >= 2 {
			out = append(out, *sg)
		}
	}
	return out
}

// onRail reports whether a passive hangs off a rail nothing in its group
// drives, from a part's pin: not one between the rail and a signal that
// reaches a part here (a pull-up, a diode to a transistor), which hangs
// off the signal.
func onRail(f subgroupFacts, q DiagramPassive) bool {
	if q.Mount == MountBridge || q.Mount == MountChain || q.Mount == MountBelow || len(q.Nets) == 0 {
		return false
	}
	if len(q.Nets) == 2 && f.drawn(q.Nets[1], q.Group) {
		return false
	}
	return f.undriven(q.Nets[0], q.Group) && f.drawn(q.Nets[0], q.Group)
}

// railMates lists the other passives on the same rail by the same part
// in the same group.
func railMates(f subgroupFacts, q DiagramPassive) []DiagramPassive {
	var out []DiagramPassive
	for _, x := range f.passives {
		if x.ID != q.ID && x.Part == q.Part && strings.EqualFold(x.Group, q.Group) && onRail(f, x) && strings.EqualFold(x.Nets[0], q.Nets[0]) {
			out = append(out, x)
		}
	}
	return out
}

// chainLeadOf finds the passive a passive chains off: the one it's
// mounted after, or, when its net reaches no part of its group, the
// passive of the group whose far net it is.
func chainLeadOf(f subgroupFacts, q DiagramPassive) (DiagramPassive, bool) {
	if q.Mount == MountChain {
		return passiveByID(f.passives, q.Part)
	}
	if q.Mount == MountBridge || q.Mount == MountBelow || len(q.Nets) == 0 || f.drawn(q.Nets[0], q.Group) {
		return DiagramPassive{}, false
	}
	for _, x := range f.passives {
		if x.ID != q.ID && strings.EqualFold(x.Group, q.Group) && x.Mount != MountBridge && len(x.Nets) == 2 && strings.EqualFold(x.Nets[1], q.Nets[0]) && f.drawn(x.Nets[0], x.Group) {
			return x, true
		}
	}
	return DiagramPassive{}, false
}

// passiveByID finds a drawn passive by its id.
func passiveByID(passives []DiagramPassive, id string) (DiagramPassive, bool) {
	for _, q := range passives {
		if q.ID == id {
			return q, true
		}
	}
	return DiagramPassive{}, false
}
