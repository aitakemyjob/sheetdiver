package core

import (
	"sort"
	"strings"

	"github.com/aitakemyjob/sheetdiver/internal/library"
)

// Diagram is the project's block diagram: a node per BOM entry and
// block, power links along the rails (regulator to the parts it feeds)
// and signal links per bus or net (from the part with the most
// connections on it to the others). Ground isn't drawn.
type Diagram struct {
	Nodes []DiagramNode `json:"nodes"`
	Edges []DiagramEdge `json:"edges"`
}

// DiagramNode is a block on the diagram. ID is "part:<entry id>" or
// "block:<block id>"; X and Y are its saved position, if any.
type DiagramNode struct {
	ID       string   `json:"id"`
	Label    string   `json:"label"`
	MPN      string   `json:"mpn,omitempty"`
	Category string   `json:"category,omitempty"`
	Block    bool     `json:"block,omitempty"`
	X        *float64 `json:"x,omitempty"`
	Y        *float64 `json:"y,omitempty"`
}

// DiagramEdge links two nodes: Kind "power" (From feeds To on the rail in
// Label) or "signal" (Label is the bus or the nets).
type DiagramEdge struct {
	From  string   `json:"from"`
	To    string   `json:"to"`
	Label string   `json:"label"`
	Kind  string   `json:"kind"`
	Nets  []string `json:"nets"`
}

// diagram builds the block diagram.
func diagram(parts []library.ProjectPart, blocks []library.Block, nets []library.Net, rows []ConnectionRow, rails []RailStatus, layout []library.Position) Diagram {
	d := Diagram{Nodes: []DiagramNode{}, Edges: []DiagramEdge{}}
	pos := map[string]library.Position{}
	for _, p := range layout {
		pos[p.Node] = p
	}
	addNode := func(n DiagramNode) {
		if p, ok := pos[n.ID]; ok {
			x, y := p.X, p.Y
			n.X, n.Y = &x, &y
		}
		d.Nodes = append(d.Nodes, n)
	}
	// Passives aren't blocks of their own on a block diagram.
	skip := map[string]bool{}
	for _, pp := range parts {
		if library.TwoTerminal(pp.Category) {
			skip[library.PartNode(pp.ID)] = true
			continue
		}
		addNode(DiagramNode{ID: library.PartNode(pp.ID), Label: entryLabel(pp), MPN: pp.MPN, Category: pp.Category})
	}
	for _, b := range blocks {
		if library.TwoTerminal(b.Kind) {
			skip[library.BlockNode(b.ID)] = true
			continue
		}
		addNode(DiagramNode{ID: library.BlockNode(b.ID), Label: b.Name, Category: b.Kind, Block: true})
	}
	nodeOf := func(r ConnectionRow) string {
		if r.EntryID != 0 {
			return library.PartNode(r.EntryID)
		}
		return library.BlockNode(r.BlockID)
	}
	kinds := netKinds(nets, rails)
	onNet := map[string][]string{} // net (lower case) → nodes, in order
	pinsOn := map[string]map[string]int{}
	for _, r := range rows {
		k := strings.ToLower(r.Net)
		n := nodeOf(r)
		if skip[n] {
			continue
		}
		if !contains(onNet[k], n) {
			onNet[k] = append(onNet[k], n)
		}
		if pinsOn[k] == nil {
			pinsOn[k] = map[string]int{}
		}
		pinsOn[k][n]++
	}
	type edgeKey struct{ from, to, kind string }
	edges := map[edgeKey]*DiagramEdge{}
	var order []edgeKey
	link := func(from, to, label, kind, net string) {
		if from == to {
			return
		}
		k := edgeKey{from, to, kind}
		if _, ok := edges[edgeKey{to, from, kind}]; ok && kind == "signal" {
			k = edgeKey{to, from, kind} // signals have no direction: one link
		}
		e := edges[k]
		if e == nil {
			e = &DiagramEdge{From: k.from, To: k.to, Kind: kind, Nets: []string{}}
			edges[k] = e
			order = append(order, k)
		}
		if !contains(e.Nets, net) {
			e.Nets = append(e.Nets, net)
		}
		if e.Label == "" {
			e.Label = label
		} else if !contains(strings.Split(e.Label, ", "), label) {
			e.Label += ", " + label
		}
	}
	// Power: along each rail, from the part (or block) that makes it to
	// what's on it.
	railNames := map[string]string{}
	for _, r := range rails {
		railNames[strings.ToLower(r.Name)] = r.Name
	}
	for _, n := range nets {
		if kinds[strings.ToLower(n.Name)] == library.NetPower {
			if _, ok := railNames[strings.ToLower(n.Name)]; !ok {
				railNames[strings.ToLower(n.Name)] = n.Name
			}
		}
	}
	names := make([]string, 0, len(railNames))
	for k := range railNames {
		names = append(names, k)
	}
	sort.Strings(names)
	for _, k := range names {
		name := railNames[k]
		var src, users []string
		for _, pp := range parts {
			node := library.PartNode(pp.ID)
			if strings.EqualFold(pp.OutputRail, name) && !strings.EqualFold(pp.SupplyRail, name) {
				src = append(src, node)
			} else if strings.EqualFold(pp.SupplyRail, name) {
				users = append(users, node)
			}
		}
		for _, node := range onNet[k] {
			if !contains(src, node) && !contains(users, node) {
				if strings.HasPrefix(node, "block:") && len(src) == 0 {
					src = append(src, node) // a battery or connector feeding the rail
				} else {
					users = append(users, node)
				}
			}
		}
		for _, f := range src {
			for _, t := range users {
				link(f, t, name, "power", name)
			}
		}
	}
	// Signals: per bus (or net), from the node with the most pins on it.
	groups := map[string][]library.Net{}
	var groupOrder []string
	for _, n := range nets {
		if kinds[strings.ToLower(n.Name)] != library.NetSignal || len(onNet[strings.ToLower(n.Name)]) < 2 {
			continue
		}
		g := n.Bus
		if g == "" {
			g = n.Name
		}
		if _, ok := groups[g]; !ok {
			groupOrder = append(groupOrder, g)
		}
		groups[g] = append(groups[g], n)
	}
	for _, g := range groupOrder {
		count := map[string]int{}
		var members []string
		for _, n := range groups[g] {
			for _, node := range onNet[strings.ToLower(n.Name)] {
				if !contains(members, node) {
					members = append(members, node)
				}
				count[node] += pinsOn[strings.ToLower(n.Name)][node]
			}
		}
		hub := members[0]
		for _, m := range members {
			if count[m] > count[hub] {
				hub = m
			}
		}
		for _, n := range groups[g] {
			for _, node := range onNet[strings.ToLower(n.Name)] {
				if node == hub {
					continue
				}
				label := g
				if n.Bus == "" {
					label = n.Name
				}
				link(hub, node, label, "signal", n.Name)
			}
		}
	}
	for _, k := range order {
		d.Edges = append(d.Edges, *edges[k])
	}
	return d
}
