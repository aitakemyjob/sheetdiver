package core

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/aitakemyjob/sheetdiver/internal/extract"
	"github.com/aitakemyjob/sheetdiver/internal/kicad"
	"github.com/aitakemyjob/sheetdiver/internal/library"
	"github.com/aitakemyjob/sheetdiver/internal/pinrole"
)

// A project's KiCad export: its block diagram as a KiCad schematic (every
// part with its pins on their nets, symbols generated from the pin
// tables, footprints where the package says which) and a PDF of layout
// advice from its datasheets and general practice.

// KiCadExport is what an export wrote.
type KiCadExport struct {
	Dir   string   `json:"dir"`
	Files []string `json:"files"`
	// Components is how many symbols the schematic has; Footprints how
	// many of them got a footprint.
	Components int `json:"components"`
	Footprints int `json:"footprints"`
	// Unplaced are the components without a footprint, with what is known
	// of their package.
	Unplaced []string `json:"unplaced"`
	// Guide is the layout guide's file, Advice how many pieces of advice
	// and datasheet sections it has.
	Guide  string `json:"guide"`
	Advice int    `json:"advice"`
	// Board is the board's file, "" when KiCad's footprints aren't
	// installed to place; OffBoard the parts and pins left off it.
	Board    string   `json:"board,omitempty"`
	OffBoard []string `json:"offBoard"`
}

// entryInfo is what the export needs of a BOM entry's part.
type entryInfo struct {
	part      library.Part
	pins      []library.Pin
	datasheet string
	// packages are the datasheet's package section titles and package
	// drawing captions ("SOT23 Package Outline").
	packages []string
	// firstPage is the datasheet's first page, where a discrete part's
	// one package is usually named.
	firstPage string
}

// ExportKiCad writes a project's schematic and layout guide into dir
// (the library's exports folder when empty).
func (s *Service) ExportKiCad(ctx context.Context, project, dir string) (KiCadExport, error) {
	detail, err := s.ProjectDetail(ctx, project)
	if err != nil {
		return KiCadExport{}, err
	}
	if len(detail.Connections.Diagram.Parts)+len(detail.Connections.Diagram.Passives) == 0 {
		return KiCadExport{}, fmt.Errorf("%s has no connections to draw: set_connections first", detail.Project.Name)
	}
	base := kicad.BaseName(detail.Project.Name)
	if dir == "" {
		dir = filepath.Join(s.Lib.Dir, "exports", base)
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return KiCadExport{}, err
	}
	info := s.entryInfo(ctx, detail.Parts)
	installed := kicad.FindFootprints()
	var index kicad.FootprintIndex
	if installed != nil {
		index = installed
	}
	sch := schematicOf(detail, info, s.drawnCells(detail.Project.Name), kicad.DefaultFootprints(index))
	sch.Date = time.Now().Format("2006-01-02")
	out := KiCadExport{Dir: dir, Components: len(sch.Components), Unplaced: []string{}, OffBoard: []string{}}
	for _, c := range sch.Components {
		if c.Footprint.LibID != "" {
			out.Footprints++
		} else {
			out.Unplaced = append(out.Unplaced, c.Ref+": "+c.Footprint.Hint)
		}
	}
	files := kicad.NewWriter().Write(sch).Files(base)
	var board *kicad.BoardResult
	if installed != nil {
		b := (&kicad.BoardWriter{Footprints: installed}).Write(sch)
		board = &b
		out.Board = base + ".kicad_pcb"
		files[out.Board] = b.Board
		out.OffBoard = append(append(out.OffBoard, b.Missing...), b.Unmatched...)
	}
	guide, advice := s.layoutGuide(ctx, detail, info, sch, board)
	out.Guide, out.Advice = base+"-layout-guide.pdf", advice
	names := slices.Sorted(func(yield func(string) bool) {
		for name := range files {
			if !yield(name) {
				return
			}
		}
	})
	for _, name := range names {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(files[name]), 0o644); err != nil {
			return out, err
		}
		out.Files = append(out.Files, name)
	}
	if err := os.WriteFile(filepath.Join(dir, out.Guide), guide, 0o644); err != nil {
		return out, err
	}
	out.Files = append(out.Files, out.Guide)
	return out, nil
}

// entryInfo loads each BOM entry's part, pin table and datasheet link.
func (s *Service) entryInfo(ctx context.Context, parts []library.ProjectPart) map[int64]entryInfo {
	pins := s.entryPins(ctx, parts)
	out := map[int64]entryInfo{}
	for _, pp := range parts {
		e := entryInfo{pins: pins[pp.ID]}
		e.part, _ = s.Lib.PartByID(ctx, pp.PartID)
		if pp.DatasheetID != 0 {
			if d, err := s.Lib.Datasheet(ctx, pp.DatasheetID); err == nil {
				e.datasheet = d.SourceURL
			}
			secs, _ := s.Lib.Sections(ctx, pp.DatasheetID, false)
			for _, sec := range secs {
				if sec.Kind == extract.KindPackage {
					e.packages = append(e.packages, sec.Title)
				}
			}
			figs, _ := s.Lib.Figures(ctx, pp.DatasheetID, library.FigureFilter{Kind: extract.FigPackage})
			for _, f := range figs {
				e.packages = append(e.packages, f.Caption)
			}
			if page, err := s.Lib.Page(ctx, pp.DatasheetID, 0); err == nil {
				e.firstPage = page.Text
			}
		}
		out[pp.ID] = e
	}
	return out
}

// diagramNode is what a diagram node stands for: a BOM entry (and which
// of its designators), or a block.
type diagramNode struct {
	entry *library.ProjectPart
	ref   string
	block *library.Block
}

// diagramNodes maps each node a diagram can name to its entry or block.
func diagramNodes(parts []library.ProjectPart, blocks []library.Block) map[string]diagramNode {
	out := map[string]diagramNode{}
	for i := range parts {
		pp := &parts[i]
		refs := splitDesignators(pp.Designator)
		first := ""
		if len(refs) > 0 {
			first = refs[0]
		}
		out[library.PartNode(pp.ID)] = diagramNode{entry: pp, ref: first}
		for _, r := range refs {
			out[library.RefNode(pp.ID, r)] = diagramNode{entry: pp, ref: r}
		}
	}
	for i := range blocks {
		ref, _ := blockRef(blocks[i].Name)
		out[library.BlockNode(blocks[i].ID)] = diagramNode{block: &blocks[i], ref: ref}
	}
	return out
}

// designatedRe is a block named with its designator first ("U2 TPS63900").
var designatedRe = regexp.MustCompile(`^([A-Z]{1,3}\d+[A-Z]?)\s+(.+)$`)

// blockRef is a block's designator and the rest of its name: "U2" and
// "TPS63900" of "U2 TPS63900"; its whole name when it doesn't start
// with one.
func blockRef(name string) (string, string) {
	if m := designatedRe.FindStringSubmatch(strings.TrimSpace(name)); m != nil {
		return m[1], m[2]
	}
	return strings.TrimSpace(name), ""
}

// schematicOf turns a project's block diagram into a KiCad schematic:
// each box with its pins, each passive (one per designator) with its
// ends on its nets, in the diagram's groups.
func schematicOf(detail ProjectDetail, info map[int64]entryInfo, drawn map[string]kicad.Cell, footprints *kicad.FootprintChain) kicad.Schematic {
	conns := detail.Connections
	d := conns.Diagram
	nodes := diagramNodes(detail.Parts, conns.Blocks)
	ground, kinds := "GND", map[string]string{}
	for _, n := range conns.Nets {
		kinds[strings.ToLower(n.Name)] = n.Kind
	}
	for _, n := range conns.Nets {
		if n.Kind == library.NetGround {
			ground = n.Name
			break
		}
	}
	sch := kicad.Schematic{Title: detail.Project.Name, Project: kicad.BaseName(detail.Project.Name), GroundNets: []string{ground}}
	for _, g := range d.Groups {
		sch.Groups = append(sch.Groups, kicad.Group{Name: g.Name, Description: g.Description})
	}
	refs := map[string]int{}
	unique := func(ref string) string {
		refs[ref]++
		if refs[ref] == 1 {
			return ref
		}
		return fmt.Sprintf("%s_%d", ref, refs[ref])
	}
	placed := map[string]kicad.Component{} // diagram part → its component
	cells := partCells(d.Parts, drawn)
	for _, p := range d.Parts {
		n := nodes[p.ID]
		c := kicad.Component{Ref: unique(firstNonEmpty(n.ref, p.Title)), MPN: p.MPN, Group: p.Group, Kind: componentKind(p.Kind, p.Shape)}
		if cell, ok := cells[p.ID]; ok {
			c.Cell = &cell
		}
		var texts []string
		if n.entry != nil {
			e := info[n.entry.ID]
			c.Manufacturer, c.Description, c.Datasheet = e.part.Manufacturer, e.part.Description, e.datasheet
			texts = append(texts, e.part.Package, e.part.Description, n.entry.Variant)
			if len(e.pins) > 0 {
				texts = append(texts, e.pins[0].Package)
			}
			texts = append(texts, e.packages...)
			if library.Transistor(p.Shape) {
				// A discrete's datasheet is of one package, named up front.
				texts = append(texts, e.firstPage)
			}
		}
		if n.block != nil {
			_, rest := blockRef(n.block.Name)
			c.Value = firstNonEmpty(n.block.Value, rest)
			texts = append(texts, n.block.Value, n.block.Notes)
		}
		transistor := library.Transistor(p.Shape)
		for i, pin := range p.Pins {
			kp := kicad.Pin{Number: pin.Number, Name: pin.Name, Net: pin.Net, Side: kicad.Side(pin.Side), Type: electricalType(pin, kinds)}
			if transistor {
				kp.Name = pin.Terminal
				if kp.Number == "" {
					kp.Number = transistorNumber(pin.Terminal, i)
				}
			}
			c.Pins = append(c.Pins, kp)
		}
		if !transistor {
			c.Pins = schematicSides(c.Pins, func(net string) bool { return kinds[strings.ToLower(net)] == library.NetGround })
		}
		c.Footprint = footprints.Footprint(kicad.FootprintQuery{Kind: c.Kind, Texts: texts, Pads: padCount(c.Pins, p.Full)})
		sch.Components = append(sch.Components, c)
		placed[p.ID] = c
	}
	for _, q := range d.Passives {
		ids, labels := strings.Split(q.ID, ","), strings.Split(q.Label, ",")
		kind := kicad.Kind(q.Shape)
		far := ground
		if len(q.Nets) > 1 {
			far = q.Nets[1]
		}
		for i, label := range labels {
			n := nodes[ids[min(i, len(ids)-1)]]
			c := kicad.Component{Ref: unique(strings.TrimSpace(label)), Value: q.Value, Kind: kind, Group: q.Group}
			var texts []string
			if n.entry != nil {
				e := info[n.entry.ID]
				c.MPN, c.Manufacturer, c.Description, c.Datasheet = n.entry.MPN, e.part.Manufacturer, e.part.Description, e.datasheet
				texts = append(texts, e.part.Package, e.part.Description, n.entry.Role)
			}
			if n.block != nil {
				texts = append(texts, n.block.Value, n.block.Notes)
			}
			texts = append(texts, q.Value)
			c.Pins = []kicad.Pin{
				{Number: kicad.TerminalPin(kind, q.Ends[0]), Net: q.Nets[0], Type: kicad.Passive},
				{Number: kicad.TerminalPin(kind, q.Ends[1]), Net: far, Type: kicad.Passive},
			}
			c.Footprint = footprints.Footprint(kicad.FootprintQuery{Kind: kind, Texts: texts, Pads: 2})
			c.Anchor = anchorOf(q, placed)
			sch.Components = append(sch.Components, c)
		}
	}
	return sch
}

// drawnCells is where the app last drew each part of a project, by node:
// the column and row in its group.
func (s *Service) drawnCells(project string) map[string]kicad.Cell {
	out := map[string]kicad.Cell{}
	if d, ok := s.DrawnLayout(project); ok {
		for _, c := range d.Cells {
			out[c.Node] = kicad.Cell{Column: c.Column, Row: c.Row}
		}
	}
	return out
}

// partCells is where each part goes in its group: where the app drew it,
// else where it's asked to be, else its column along the power flow, in
// diagram order down the column.
func partCells(parts []DiagramPart, drawn map[string]kicad.Cell) map[string]kicad.Cell {
	out := map[string]kicad.Cell{}
	rows := map[string]int{} // group and column → parts placed there
	for _, p := range parts {
		if c, ok := drawn[p.ID]; ok {
			out[p.ID] = c
			continue
		}
		c := kicad.Cell{Column: p.Column, Row: -1}
		if p.Want != nil && p.Want.Column != nil {
			c.Column = *p.Want.Column
		}
		if p.Want != nil && p.Want.Row != nil {
			c.Row = *p.Want.Row
		}
		key := fmt.Sprintf("%s|%d", p.Group, c.Column)
		if c.Row < 0 {
			c.Row = rows[key]
		}
		rows[key]++
		out[p.ID] = c
	}
	return out
}

// schematicSides puts a box's pins where schematics usually have them:
// supplies on the left, at the top, ground at the bottom, so decoupling
// capacitors hang from the supply's wire to ground. A pin the block
// diagram puts along the bottom for its passive (a configuration pin)
// goes on the left, last.
func schematicSides(pins []kicad.Pin, ground func(net string) bool) []kicad.Pin {
	var top, rest, last []kicad.Pin
	for _, p := range pins {
		switch {
		case p.Side == kicad.Top:
			p.Side = kicad.Left
			top = append(top, p)
		case p.Side == kicad.Bottom && p.Net != "" && !ground(p.Net):
			p.Side = kicad.Left
			last = append(last, p)
		default:
			rest = append(rest, p)
		}
	}
	return append(append(top, rest...), last...)
}

// anchorOf is the part and pins a passive is wired to on the schematic:
// the pins of the part it hangs by on its nets, both for one across two
// pins (an inductor across LX1 and LX2), the one on its first net else.
// A passive after another (C2 after L1) or between parts has none.
func anchorOf(q DiagramPassive, placed map[string]kicad.Component) *kicad.Anchor {
	part, ok := placed[q.Part]
	if !ok {
		return nil
	}
	pinOn := func(net string) string {
		for _, p := range part.Pins {
			if strings.EqualFold(p.Net, net) {
				return firstNonEmpty(p.Number, p.Name)
			}
		}
		return ""
	}
	switch q.Mount {
	case MountBridge:
		if len(q.Nets) == 2 {
			if a, b := pinOn(q.Nets[0]), pinOn(q.Nets[1]); a != "" && b != "" {
				return &kicad.Anchor{Ref: part.Ref, Pins: []string{a, b}}
			}
		}
	case MountBelow, MountLane, MountOut, MountWire:
		if a := pinOn(q.Nets[0]); a != "" {
			return &kicad.Anchor{Ref: part.Ref, Pins: []string{a}}
		}
	}
	return nil
}

// componentKind is how a diagram part is drawn in KiCad: a transistor by
// its shape, a connector by its kind, else a box.
func componentKind(kind, shape string) kicad.Kind {
	switch {
	case library.Transistor(shape):
		return kicad.Kind(shape)
	case kind == "connector":
		return kicad.KindConnector
	}
	return kicad.KindIC
}

// transistorNumber numbers a transistor block's pin, which has none, as
// KiCad's Q_NMOS_GSD and Q_NPN_BEC do.
func transistorNumber(terminal string, i int) string {
	switch terminal {
	case "gate", "base":
		return "1"
	case "source", "emitter":
		return "2"
	case "drain", "collector":
		return "3"
	}
	return fmt.Sprint(i + 1)
}

// electricalType is a pin's type for KiCad's ERC, by its role: an output
// on a power net is a supply's output.
func electricalType(p DiagramPin, kinds map[string]string) kicad.ElectricalType {
	switch pinrole.Role(p.Role) {
	case pinrole.Power, pinrole.Ground:
		return kicad.PowerIn
	case pinrole.Output:
		if kinds[strings.ToLower(p.Net)] == library.NetPower {
			return kicad.PowerOut
		}
		return kicad.Output
	case pinrole.Input:
		return kicad.Input
	case pinrole.Bidir:
		return kicad.Bidirectional
	case pinrole.NC:
		return kicad.NoConnect
	}
	return kicad.Passive
}

// padNumberRe is a pad number a package count includes: a pin's, or an
// exposed pad's.
var padNumberRe = regexp.MustCompile(`(?i)^(\d+|PAD|EP)$`)

// padCount is how many pads a part has, from its whole pin table: its
// distinct numbers; 0 when unknown (not the whole table, or a ball grid).
func padCount(pins []kicad.Pin, full bool) int {
	if !full {
		return 0
	}
	seen := map[string]bool{}
	for _, p := range pins {
		if !padNumberRe.MatchString(p.Number) {
			return 0
		}
		seen[strings.ToUpper(p.Number)] = true
	}
	return len(seen)
}
