package kicad

import (
	"crypto/sha256"
	"fmt"
	"math"
	"sort"
	"strings"
)

// The sheet. Each group is a frame with its components inside: the
// boxes in a row, the two-terminals in rows under them. Nothing is wired
// across the sheet: every pin gets a short wire ending in a label with
// its net's name (or a ground symbol), and labels of the same name are
// the same net, so the netlist is exact however the parts are arranged.
// Moving them into a readable circuit is left to the designer.

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

// placed is a component on the sheet: its symbol and where its origin is.
type placed struct {
	c      Component
	s      LibSymbol
	x, y   float64
	extent Rect // sheet coordinates (Y down), around its labels
}

// Write draws the schematic.
func (w *Writer) Write(s Schematic) Export {
	ids := newIDs(s.Project + "/" + s.Title)
	root := ids.next("root")
	ground := map[string]bool{}
	for _, g := range s.GroundNets {
		ground[g] = true
	}

	// The symbols, one per library ID.
	syms := map[string]LibSymbol{}
	var order []string
	symbolOf := func(c Component) LibSymbol {
		ls := w.Symbols.Build(c)
		if _, ok := syms[ls.ID]; !ok {
			syms[ls.ID] = ls
			order = append(order, ls.ID)
		}
		return syms[ls.ID]
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
	var body []*node
	frames := []frame{}
	const margin, gap, maxWidth = 25.4, 12.7, 380.0
	cx, cy, rowH := margin, margin, 0.0
	gnd := groundSymbol()
	usedGround := false
	for _, g := range groups {
		comps := byGroup[g.Name]
		if len(comps) == 0 {
			continue
		}
		var items []*placed
		for _, c := range comps {
			p := &placed{c: c, s: symbolOf(c)}
			p.extent = extentOf(p)
			items = append(items, p)
		}
		width, height := arrange(items)
		// A frame with its title above.
		fw, fh := width+2*grid*2, height+2*grid*2+5.08
		if cx+fw > maxWidth && cx > margin {
			cx, cy, rowH = margin, cy+rowH+gap, 0
		}
		fx, fy := cx, cy
		for _, p := range items {
			p.x, p.y = snap(fx+grid*2+p.x), snap(fy+grid*2+5.08+p.y)
		}
		frames = append(frames, frame{x0: fx, y0: fy, x1: fx + fw, y1: fy + fh, title: g.Name, description: g.Description})
		cx += fw + gap
		rowH = math.Max(rowH, fh)
		for _, p := range items {
			body = append(body, w.instance(p, root, s.Project, ids))
			for _, number := range sortedPins(p.s) {
				pin := pinOf(p.c, number)
				place := p.s.Pins[number]
				px, py := p.x+place.X, p.y-place.Y
				switch {
				case pin.Net == "":
					if _, two := twoTerminals[p.c.Kind]; !two {
						body = append(body, list("no_connect", list("at", px, py), list("uuid", ids.next("nc"))))
					}
				case pin.Net == "GND" && ground["GND"]:
					usedGround = true
					qx, qy := px, py
					if place.Out != Bottom {
						qx, qy = step(px, py, place.Out, grid)
						body = append(body, wire(px, py, qx, qy, ids))
					}
					body = append(body, groundInstance(gnd, qx, qy, place.Out == Top, root, s.Project, ids))
				default:
					qx, qy := step(px, py, place.Out, grid)
					body = append(body, wire(px, py, qx, qy, ids), label(pin.Net, qx, qy, place.Out, ids))
				}
			}
		}
	}
	if usedGround {
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
	tb.add(list("comment", 1, "Drawn by SheetDiver from the block diagram: every pin is labelled with its net."))
	tb.add(list("comment", 2, "Arrange the parts and replace labels with wires as you like; the nets stay the same."))
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

// arrange places a group's components relative to its top-left corner:
// boxes and transistors in a row, two-terminals in rows under them no
// wider than the boxes or six two-terminals. It returns the size used.
func arrange(items []*placed) (float64, float64) {
	const gap = 5.08
	var boxes, small []*placed
	for _, p := range items {
		if _, ok := twoTerminals[p.c.Kind]; ok {
			small = append(small, p)
		} else {
			boxes = append(boxes, p)
		}
	}
	x, rowH := 0.0, 0.0
	for _, p := range boxes {
		p.x, p.y = x-p.extent.X0, -p.extent.Y0
		x += p.extent.X1 - p.extent.X0 + gap
		rowH = math.Max(rowH, p.extent.Y1-p.extent.Y0)
	}
	width := math.Max(x-gap, 0)
	six := 0.0
	for i, p := range small {
		if i < 6 {
			six += p.extent.X1 - p.extent.X0 + gap
		}
	}
	limit := math.Max(width, six)
	y := rowH
	if len(boxes) > 0 {
		y += gap * 2
	}
	x, rowH = 0, 0
	for _, p := range small {
		w := p.extent.X1 - p.extent.X0
		if x > 0 && x+w > limit {
			x, y, rowH = 0, y+rowH+gap, 0
		}
		p.x, p.y = x-p.extent.X0, y-p.extent.Y0
		x += w + gap
		width = math.Max(width, x-gap)
		rowH = math.Max(rowH, p.extent.Y1-p.extent.Y0)
	}
	return width, y + rowH
}

// extentOf is the area a component covers with its pins' wires and
// labels, in sheet coordinates around its origin.
func extentOf(p *placed) Rect {
	b := p.s.Bounds
	r := Rect{b.X0, -b.Y1, b.X1, -b.Y0}
	for number, place := range p.s.Pins {
		x, y := place.X, -place.Y
		reach := grid + float64(len([]rune(pinOf(p.c, number).Net))+2)*charWidth
		switch place.Out {
		case Left:
			r.X0 = math.Min(r.X0, x-reach)
		case Right:
			r.X1 = math.Max(r.X1, x+reach)
		case Top:
			r.Y0 = math.Min(r.Y0, y-reach)
		case Bottom:
			r.Y1 = math.Max(r.Y1, y+reach)
		}
	}
	// Room for the reference and value, as wide as they're written.
	value := p.c.Value
	if value == "" {
		value = p.c.MPN
	}
	for _, f := range []struct {
		at   textPlace
		text string
	}{{p.s.Ref, p.c.Ref}, {p.s.Val, value}} {
		w := float64(len([]rune(f.text))) * charWidth
		x0, x1 := f.at.x-w/2, f.at.x+w/2
		if f.at.justify == "left" {
			x0, x1 = f.at.x, f.at.x+w
		}
		r = Rect{math.Min(r.X0, x0), math.Min(r.Y0, -f.at.y-grid), math.Max(r.X1, x1), math.Max(r.Y1, -f.at.y+grid)}
	}
	return r
}

// instance is a component placed on the sheet.
func (w *Writer) instance(p *placed, root, project string, ids *idGen) *node {
	ref := p.c.Ref
	n := list("symbol", list("lib_id", p.s.ID), at(p.x, p.y, 0), list("unit", 1),
		list("in_bom", sym("yes")), list("on_board", sym("yes")), list("dnp", sym("no")), list("uuid", ids.next("sym:"+ref)))
	field := func(name, value string, t textPlace, dy float64, hidden bool) *node {
		return list("property", name, value, at(p.x+t.x, p.y-t.y+dy, 0), t.effects(hidden))
	}
	value := p.c.Value
	if value == "" {
		value = p.c.MPN
	}
	n.add(field("Reference", ref, p.s.Ref, 0, false), field("Value", value, p.s.Val, 0, false))
	n.add(field("Footprint", p.c.Footprint.LibID, p.s.Val, 2.54, true), field("Datasheet", p.c.Datasheet, p.s.Val, 2.54, true))
	extra := []struct{ name, value string }{{"MPN", p.c.MPN}, {"Manufacturer", p.c.Manufacturer}, {"Description", p.c.Description}}
	if p.c.Footprint.LibID == "" && p.c.Footprint.Hint != "" {
		extra = append(extra, struct{ name, value string }{"Package", p.c.Footprint.Hint})
	}
	for _, e := range extra {
		if e.value != "" {
			n.add(field(e.name, e.value, p.s.Val, 2.54, true))
		}
	}
	for _, number := range sortedPins(p.s) {
		n.add(list("pin", number, list("uuid", ids.next("pin:"+ref+":"+number))))
	}
	return n.add(list("instances", list("project", project, list("path", "/"+root, list("reference", ref), list("unit", 1)))))
}

// groundInstance is a ground symbol at a point, upside down above a pin
// on top of its part.
func groundInstance(g LibSymbol, x, y float64, up bool, root, project string, ids *idGen) *node {
	ref := fmt.Sprintf("#PWR%03d", ids.count("#PWR"))
	angle, dy := 0.0, 1.0
	if up {
		angle, dy = 180, -1
	}
	return list("symbol", list("lib_id", g.ID), at(x, y, angle), list("unit", 1),
		list("in_bom", sym("yes")), list("on_board", sym("yes")), list("dnp", sym("no")), list("uuid", ids.next("gnd:"+ref)),
		list("property", "Reference", ref, at(x, y+6.35*dy, 0), effects(true)),
		// Its value is always GND: hidden, so the symbols on pins a step
		// apart don't print over each other.
		list("property", "Value", "GND", at(x, y+3.81*dy, 0), effects(true)),
		list("property", "Footprint", "", at(x, y, 0), effects(true)),
		list("property", "Datasheet", "", at(x, y, 0), effects(true)),
		list("pin", "1", list("uuid", ids.next("gndpin:"+ref))),
		list("instances", list("project", project, list("path", "/"+root, list("reference", ref), list("unit", 1)))))
}

// wire is a wire between two points.
func wire(x0, y0, x1, y1 float64, ids *idGen) *node {
	return list("wire", list("pts", xy(x0, y0), xy(x1, y1)), stroke(0), list("uuid", ids.next("wire")))
}

// label is a net label at the end of a pin's wire, reading away from it.
func label(net string, x, y float64, out Side, ids *idGen) *node {
	angle, justify := 0.0, "left"
	switch out {
	case Left:
		angle, justify = 180, "right"
	case Top:
		angle = 90
	case Bottom:
		angle, justify = 270, "right"
	}
	return list("label", net, at(x, y, angle), list("fields_autoplaced"), effects(false, justify, "bottom"), list("uuid", ids.next("label")))
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
