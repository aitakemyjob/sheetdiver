package kicad

import (
	"crypto/sha256"
	"fmt"
	"math"
	"slices"
	"sort"
	"strings"
)

// The sheet. Each group is a frame with its components inside, placed
// as placement.go says: each part with its passives wired to it, the
// nets that leave it labelled. Labels of the same name are the same
// net, so the netlist is exact however the clusters are arranged.

// fileVersion is the KiCad 7 schematic format; KiCad 8 and 9 open it.
const fileVersion = 20230121

// Export is a written schematic: the sheet, the library of the symbols
// generated for it, the library table that points KiCad at it, and a
// project file.
type Export struct {
	Schematic   string
	Symbols     string
	SymLibTable string
	Project     string
}

// SymbolLibrary is the name of the generated symbol library.
const SymbolLibrary = "SheetDiver"

// Files names the export's files for a project saved as base.
func (e Export) Files(base string) map[string]string {
	return map[string]string{
		base + ".kicad_sch":    e.Schematic,
		base + ".kicad_pro":    e.Project,
		"sheetdiver.kicad_sym": e.Symbols,
		"sym-lib-table":        e.SymLibTable,
	}
}

// Writer writes schematics with its symbol factory.
type Writer struct {
	Symbols *SymbolFactory
}

// NewWriter is a writer with the standard symbols.
func NewWriter() *Writer {
	return &Writer{Symbols: NewSymbolFactory()}
}

// Write draws the schematic.
func (w *Writer) Write(s Schematic) Export {
	ids := newIDs(s.Project + "/" + s.Title)
	root := ids.next("root")

	// The symbols, one per library ID.
	syms := map[string]LibSymbol{}
	var order []string
	pl := &placer{members: map[string]int{}, gnd: slices.Contains(s.GroundNets, "GND")}
	pl.symbol = func(c Component) LibSymbol {
		ls := w.Symbols.Build(c)
		if _, ok := syms[ls.ID]; !ok {
			syms[ls.ID] = ls
			order = append(order, ls.ID)
		}
		return syms[ls.ID]
	}
	for _, c := range s.Components {
		for _, p := range c.Pins {
			if p.Net != "" {
				pl.members[p.Net]++
			}
		}
	}

	// Components by group, in the groups' order, then those in none.
	byGroup := map[string][]Component{}
	for _, c := range s.Components {
		byGroup[c.Group] = append(byGroup[c.Group], c)
	}
	groups := append([]Group{}, s.Groups...)
	if len(byGroup[""]) > 0 {
		groups = append(groups, Group{Name: ""})
	}

	sch := list("kicad_sch", list("version", fileVersion), list("generator", sym("sheetdiver")), list("uuid", root))
	var items []item
	frames := []frame{}
	const margin, gap, maxWidth = 25.4, 12.7, 380.0
	const inset, titleH = 2 * grid, 5.08
	cx, cy, rowH := margin, margin, 0.0
	for _, g := range groups {
		if len(byGroup[g.Name]) == 0 {
			continue
		}
		drawn, width, height := pl.group(byGroup[g.Name])
		// A frame with its title above.
		fw, fh := width+2*inset, height+2*inset+titleH
		if cx+fw > maxWidth && cx > margin {
			cx, cy, rowH = margin, cy+rowH+gap, 0
		}
		items = append(items, moveAll(drawn, snap(cx+inset), snap(cy+inset+titleH))...)
		frames = append(frames, frame{x0: cx, y0: cy, x1: cx + fw, y1: cy + fh, title: g.Name, description: g.Description})
		cx += fw + gap
		rowH = math.Max(rowH, fh)
	}
	e := &emitter{ids: ids, root: root, project: s.Project}
	var body []*node
	for _, it := range items {
		body = append(body, it.write(e))
	}
	if e.usedGround {
		gnd := groundSymbol()
		syms[gnd.ID] = gnd
		order = append(order, gnd.ID)
	}

	// The page: the smallest that holds everything.
	w0, h0 := 0.0, 0.0
	for _, f := range frames {
		w0, h0 = math.Max(w0, f.x1), math.Max(h0, f.y1)
	}
	sch.add(paper(w0+margin, h0+margin))
	tb := list("title_block", list("title", s.Title))
	if s.Date != "" {
		tb.add(list("date", s.Date))
	}
	if s.Revision != "" {
		tb.add(list("rev", s.Revision))
	}
	tb.add(list("comment", 1, "Drawn by SheetDiver from the block diagram: each part with the passives that serve it."))
	tb.add(list("comment", 2, "Nets that leave a part's cluster end in labels: wire them as you like, the nets stay the same."))
	sch.add(tb)
	libs := list("lib_symbols")
	for _, id := range order {
		libs.add(syms[id].def(id))
	}
	sch.add(libs)
	for _, f := range frames {
		for _, n := range f.nodes(ids) {
			sch.add(n)
		}
	}
	for _, n := range body {
		sch.add(n)
	}
	sch.add(list("sheet_instances", list("path", "/", list("page", "1"))))

	symLib := list("kicad_symbol_lib", list("version", 20220914), list("generator", sym("sheetdiver")))
	for _, id := range order {
		if ls := syms[id]; ls.Library() == SymbolLibrary {
			symLib.add(ls.def(ls.Name()))
		}
	}
	table := list("sym_lib_table", list("lib", list("name", SymbolLibrary), list("type", "KiCad"), list("uri", "${KIPRJMOD}/sheetdiver.kicad_sym"), list("options", ""), list("descr", "Symbols SheetDiver generated from datasheet pin tables")))
	return Export{Schematic: sch.String(), Symbols: symLib.String(), SymLibTable: table.String(), Project: projectFile(s.Project)}
}

// step is a point d further from a pin, the way it leaves its symbol
// (sheet coordinates, Y down).
func step(x, y float64, out Side, d float64) (float64, float64) {
	switch out {
	case Left:
		return x - d, y
	case Right:
		return x + d, y
	case Top:
		return x, y - d
	}
	return x, y + d
}

// pinOf is the component's pin with a number on its symbol.
func pinOf(c Component, number string) Pin {
	for _, p := range c.Pins {
		if pinNumber(p) == number {
			return p
		}
	}
	return Pin{Number: number}
}

// sortedPins are a symbol's pin numbers, in order.
func sortedPins(s LibSymbol) []string {
	out := make([]string, 0, len(s.Pins))
	for n := range s.Pins {
		out = append(out, n)
	}
	sort.Strings(out)
	return out
}

// snap puts a coordinate on the grid.
func snap(v float64) float64 {
	return math.Round(v/grid) * grid
}

// frame is a group's frame with its name and description.
type frame struct {
	x0, y0, x1, y1     float64
	title, description string
}

// nodes draws the frame: a dashed rectangle with the title above it.
func (f frame) nodes(ids *idGen) []*node {
	if f.title == "" {
		return nil
	}
	rect := list("rectangle", list("start", f.x0, f.y0), list("end", f.x1, f.y1), list("stroke", list("width", 0), list("type", sym("dash"))), fill("none"), list("uuid", ids.next("frame")))
	title := list("text", f.title, at(f.x0+grid, f.y0+3.81, 0), list("effects", list("font", list("size", 2.0, 2.0), sym("bold")), list("justify", sym("left"), sym("bottom"))), list("uuid", ids.next("title")))
	out := []*node{rect, title}
	if f.description != "" {
		out = append(out, list("text", f.description, at(f.x0+grid, f.y0+6.35, 0), list("effects", font(), list("justify", sym("left"), sym("bottom"))), list("uuid", ids.next("desc"))))
	}
	return out
}

// paper is the smallest ISO sheet that holds w×h mm, landscape; a
// custom size beyond A0.
func paper(w, h float64) *node {
	for _, p := range []struct {
		name string
		w, h float64
	}{{"A4", 297, 210}, {"A3", 420, 297}, {"A2", 594, 420}, {"A1", 841, 594}, {"A0", 1189, 841}} {
		if w <= p.w && h <= p.h {
			return list("paper", p.name)
		}
	}
	return list("paper", "User", math.Ceil(w), math.Ceil(h))
}

// projectFile is a minimal KiCad project; KiCad fills in its defaults.
func projectFile(name string) string {
	return fmt.Sprintf("{\n  \"meta\": {\n    \"filename\": %q,\n    \"version\": 1\n  }\n}\n", safeFile(name)+".kicad_pro")
}

// safeFile is a name made safe for a file name.
func safeFile(name string) string {
	s := strings.Trim(unsafeName.ReplaceAllString(name, "_"), "_")
	if s == "" {
		return "schematic"
	}
	return s
}

// BaseName is the file name a project's schematic is saved under.
func BaseName(project string) string { return safeFile(project) }

// idGen makes the UUIDs KiCad wants for every item, from a seed, so the
// same design gives the same file.
type idGen struct {
	seed   string
	counts map[string]int
}

// newIDs makes UUIDs from a seed.
func newIDs(seed string) *idGen { return &idGen{seed: seed, counts: map[string]int{}} }

// count is how many times a key was counted, from 1.
func (g *idGen) count(key string) int {
	g.counts["#"+key]++
	return g.counts["#"+key]
}

// next is a fresh UUID for a key.
func (g *idGen) next(key string) string {
	g.counts[key]++
	h := sha256.Sum256([]byte(fmt.Sprintf("%s|%s|%d", g.seed, key, g.counts[key])))
	h[6] = h[6]&0x0f | 0x50
	h[8] = h[8]&0x3f | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", h[0:4], h[4:6], h[6:8], h[8:10], h[10:16])
}
