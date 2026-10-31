package mcpserver

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/aitakemyjob/sheetdiver/internal/cad"
	"github.com/aitakemyjob/sheetdiver/internal/core"
)

// CAD library tools: SheetDiver keeps the user's CAD library (symbols,
// footprints and 3D models of their parts, in KiCad and Eagle formats).
// It draws what it can from the pin table and standard packages; the
// agent reads the datasheet's package drawing and pin table and draws
// the rest (draw_footprint, draw_symbol), checking its work with
// get_cad_image; the user edits in their CAD tool and SheetDiver keeps
// their edits.

type cadPartArgs struct {
	Part    string `json:"part" jsonschema:"the part (MPN or orderable code)"`
	Package string `json:"package,omitempty" jsonschema:"which of its packages, when it comes in several"`
	Show    bool   `json:"show,omitempty" jsonschema:"open the part's CAD tab in the app"`
}

type cadImageArgs struct {
	Part    string `json:"part"`
	Package string `json:"package,omitempty"`
	What    string `json:"what" jsonschema:"symbol, footprint or 3d"`
	Show    bool   `json:"show,omitempty"`
}

type drawSymbolArgs struct {
	Part      string         `json:"part"`
	Package   string         `json:"package,omitempty"`
	Reference string         `json:"reference,omitempty" jsonschema:"the designator prefix: U (default), Q, D, J…"`
	Pins      []cad.PinPlace `json:"pins" jsonschema:"pins to place, by number: {number, side: left/right/top/bottom, position: slot on that side from the top or left counting from 1 (leave gaps between groups), type: input/output/bidirectional/tri_state/passive/power_in/power_out/open_collector/no_connect, name: to rename, hidden}. Pins not listed keep their place by role (supplies top, grounds bottom, outputs right, the rest left)"`
	Notes     string         `json:"notes,omitempty" jsonschema:"why it's drawn this way"`
	Show      bool           `json:"show,omitempty"`
}

type drawFootprintArgs struct {
	Part    string      `json:"part"`
	Package string      `json:"package,omitempty" jsonschema:"which of the part's packages this is (the pin table's package name)"`
	Outline cad.Package `json:"outline" jsonschema:"the package as its datasheet drawing gives it, in mm (convert inch tables). family — surface mount: gullwing (SOIC SOP TSSOP MSOP SOT-23), qfp, dfn (DFN SON), qfn, chip (0402… SOD), bga; through hole: dip, header (pin headers and SIP; rows 1 or 2, numbered across), inline (TO-92, TO-220: one row of leads, bodyOffset moves the body back), radial (capacitors, LEDs: bodyWidth is the diameter), axial (resistors, diodes: pitch is the lead span); custom: anything else, pad by pad in pads [{number (shared by pads of one pin, empty for a bare hole), x, y (from the centre, y down), w, h, shape rect/roundrect/oval/circle, drill, kind smd/thru/npth}]. Then name, pins, pinsX (quad: pins on each of the top and bottom sides), pitch (e), rowSpacing (a DIP's rows), bodyLength (D, along the rows) and bodyWidth (E1 on gull-wing, E on no-lead) and height (A), standoff (A1), span (E: tip to tip across a gull-wing's leads), leadLength (L), leadWidth (b; a round lead's or BGA ball's diameter), leadThickness (c; with leadWidth, a rectangular through-hole lead) — each {min, nom, max} as the table gives them — exposedPad {w, h} (D2 × E2), exposedPadNumber, layout (positions per side when pins are skipped or numbered oddly: left/right top to bottom, quad: left, bottom, right, top counter-clockwise; \"\" is no pin; for inline, radial and axial, left lists the leads left to right), rows/columns/depopulated (BGA), color, page; and land: the datasheet's recommended land pattern when it has one {padLength, padWidth, rowSpan (pad centre to centre across the rows), rowSpanX, pitch, drill (a through-hole pad's hole; padLength is then its diameter), exposedPad {w, h}, paste (fraction of the thermal pad), vias {columns, rows, pitch, drill}, page}. With no land pattern the pads are calculated (IPC-7351 nominal; holes per IPC-2222 from the lead)"`
	Notes   string      `json:"notes,omitempty" jsonschema:"where the numbers came from (pages, which table) and anything doubtful"`
	Show    bool        `json:"show,omitempty"`
}

type resetCadArgs struct {
	Part    string `json:"part"`
	Package string `json:"package,omitempty"`
	Kind    string `json:"kind" jsonschema:"symbol or footprint"`
}

// problemsText lists problems.
func problemsText(ps []cad.Problem) string {
	var b strings.Builder
	for _, p := range ps {
		fmt.Fprintf(&b, "  - %s: %s\n", p.Level, p.Text)
	}
	return b.String()
}

// cadText describes a part's CAD data for an agent.
func cadText(pc core.PartCad) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# %s CAD\n", pc.Part.MPN)
	for _, p := range pc.Packages {
		fmt.Fprintf(&b, "\n## package %q (%d pins in the pin table)\n", p.Package, p.Pins)
		if s := p.Symbol; s != nil {
			fmt.Fprintf(&b, "symbol %s: %s", s.Name, s.State)
			if s.Notes != "" {
				fmt.Fprintf(&b, " — %s", s.Notes)
			}
			b.WriteString("\n")
			if s.Symbol != nil {
				sides := map[string][]string{}
				for _, pin := range s.Symbol.Pins {
					sides[pin.Side] = append(sides[pin.Side], fmt.Sprintf("%s %s (%s)", pin.Number, pin.Name, pin.Type))
				}
				for _, side := range []string{cad.Left, cad.Right, cad.Top, cad.Bottom} {
					if len(sides[side]) > 0 {
						fmt.Fprintf(&b, "  %s: %s\n", side, strings.Join(sides[side], ", "))
					}
				}
			}
			b.WriteString(problemsText(s.Problems))
		} else {
			b.WriteString("no symbol: the part has no pin table (record_pins)\n")
		}
		if f := p.Footprint; f != nil {
			fmt.Fprintf(&b, "footprint %s: %s", f.Name, f.State)
			if f.Standard {
				b.WriteString(", from the standard outline (not yet checked against the datasheet's drawing)")
			}
			if f.DatasheetPage > 0 {
				fmt.Fprintf(&b, ", from the datasheet's package dimensions (page %d, get_package)", f.DatasheetPage)
			}
			if f.Notes != "" {
				fmt.Fprintf(&b, " — %s", f.Notes)
			}
			b.WriteString("\n")
			if fp := f.Footprint; fp != nil {
				fmt.Fprintf(&b, "  %s\n  %d pads; courtyard %.2f × %.2f mm\n", fp.Description, countCopper(fp.Pads), fp.Courtyard.X1-fp.Courtyard.X0, fp.Courtyard.Y1-fp.Courtyard.Y0)
			}
			if f.ModelKept {
				b.WriteString("  3D model: the user's own (kept)\n")
			}
			b.WriteString(problemsText(f.Problems))
		} else if p.Missing != "" {
			fmt.Fprintf(&b, "no footprint: %s\n", p.Missing)
		}
		if len(p.Match) > 0 {
			b.WriteString("symbol against footprint:\n" + problemsText(p.Match))
		}
	}
	l := pc.Library
	fmt.Fprintf(&b, "\nKept in %s (%s) as library %q.\n", l.Dir, strings.Join(l.Formats, ", "), l.Name)
	return b.String()
}

// countCopper counts the pads with copper of their own.
func countCopper(pads []cad.Pad) int {
	n := 0
	for _, p := range pads {
		if p.Terminal() {
			n++
		}
	}
	return n
}

// registerCad adds the CAD library tools.
func (s *Server) registerCad() {
	c := s.core
	showCad := func(mpn string) { c.Show(core.Focus{Kind: "part", Part: mpn, Tab: "cad"}) }

	addTool(s, &mcp.Tool{Name: "get_cad", Description: "A part's CAD data, per package: its schematic symbol (pins by side and type), its footprint (where its pads come from: a standard outline, the datasheet's land pattern or calculated from the drawing) and 3D model, each with its state — auto (SheetDiver's own), drawn (by an agent) or edited (by the user in their CAD tool: kept as they left it) — what doesn't match between pins and pads, and what's missing. Use get_cad_image to look at them.", Annotations: readOnly("Get CAD")},
		read, func(a cadPartArgs) string { return "cad " + a.Part }, func(ctx context.Context, a cadPartArgs) (result, error) {
			pc, err := c.PartCad(ctx, a.Part)
			if err != nil {
				return result{}, err
			}
			if a.Show {
				showCad(pc.Part.MPN)
			}
			return text(cadText(pc)), nil
		})

	addTool(s, &mcp.Tool{Name: "get_cad_image", Description: "A picture of a part's symbol, footprint (pads numbered, pitch, row span and pad size dimensioned, courtyard dashed) or 3D model, to check a drawing against the datasheet's pin diagram and package drawing (get_figure).", Annotations: readOnly("Get CAD image")},
		read, func(a cadImageArgs) string { return "cad image " + a.Part + " " + a.What }, func(ctx context.Context, a cadImageArgs) (result, error) {
			png, cp, err := c.CadImage(ctx, a.Part, a.Package, strings.ToLower(a.What))
			if err != nil {
				return result{}, err
			}
			if a.Show {
				showCad(a.Part)
			}
			return result{text: fmt.Sprintf("%s %s of %s, package %q.", a.Part, a.What, a.Part, cp.Package), images: [][]byte{png}}, nil
		})

	addTool(s, &mcp.Tool{Name: "draw_symbol", Description: "Draw a part's schematic symbol the way an engineer would read it: put pins on sides and in groups (inputs left, outputs right, supplies top, grounds bottom; related pins together with gaps between groups), give them their electrical types for ERC, rename or hide them. The pins come from the pin table (record_pins first); unplaced ones keep their place by role. Refused when the user edited the symbol in their CAD tool (reset_cad first, if they agree). The library is rewritten.", Annotations: writes("Draw symbol", true)},
		write, func(a drawSymbolArgs) string { return "draw symbol " + a.Part }, func(ctx context.Context, a drawSymbolArgs) (result, error) {
			pc, ps, err := c.DrawSymbol(ctx, a.Part, a.Package, cad.SymbolSpec{Reference: a.Reference, Pins: a.Pins}, a.Notes)
			if err != nil {
				return result{}, fmt.Errorf("%w\n%s", err, problemsText(ps))
			}
			if a.Show {
				showCad(pc.Part.MPN)
			}
			return text("drawn.\n" + problemsText(ps) + cadText(pc)), nil
		})

	addTool(s, &mcp.Tool{Name: "draw_footprint", Description: "Draw a part's footprint and 3D model from its datasheet: read the package outline drawing and its dimension table (get_section with the package section, list_figures and get_figure, get_page_image) and the recommended land pattern when there is one, and give them here in mm. SheetDiver lays out the pads (the datasheet's land pattern, else IPC-7351 nominal), silkscreen, courtyard and fabrication outline, builds the 3D body (STEP) and checks them against the symbol's pins. Then look at it with get_cad_image and compare with the drawing. Refused when the user edited the footprint in their CAD tool (reset_cad first, if they agree). The library is rewritten.", Annotations: writes("Draw footprint", true)},
		write, func(a drawFootprintArgs) string { return "draw footprint " + a.Part }, func(ctx context.Context, a drawFootprintArgs) (result, error) {
			pc, ps, err := c.DrawFootprint(ctx, a.Part, a.Package, a.Outline, a.Notes)
			if err != nil {
				return result{}, fmt.Errorf("%w\n%s", err, problemsText(ps))
			}
			if a.Show {
				showCad(pc.Part.MPN)
			}
			return text("drawn.\n" + problemsText(ps) + cadText(pc)), nil
		})

	addTool(s, &mcp.Tool{Name: "reset_cad", Description: "Drop the newest work on a part's symbol or footprint: the user's edit from their CAD tool (back to the agent's drawing, or SheetDiver's), or an agent's drawing (back to SheetDiver's). Only when the user asks: their edits are theirs.", Annotations: writes("Reset CAD", false)},
		write, func(a resetCadArgs) string { return "reset " + a.Kind + " " + a.Part }, func(ctx context.Context, a resetCadArgs) (result, error) {
			pc, err := c.ResetCad(ctx, a.Part, a.Package, strings.ToLower(a.Kind))
			if err != nil {
				return result{}, err
			}
			return text("reset.\n" + cadText(pc)), nil
		})

	addTool(s, &mcp.Tool{Name: "sync_cad_library", Description: "Bring the user's CAD library up to date: read back what they changed in their CAD tools (KiCad, Altium, Eagle: kept from then on, and carried into the others), then write every part's symbol, footprint and 3D model in each format they keep (Settings → CAD library). Says where the library is and how to add it to the tool.", Annotations: writes("Sync CAD library", true)},
		write, func(struct{}) string { return "sync cad library" }, func(ctx context.Context, _ struct{}) (result, error) {
			rep, err := c.SyncCad(ctx)
			if err != nil {
				return result{}, err
			}
			l := c.CadLibrary()
			var b strings.Builder
			if len(l.Formats) == 0 {
				return text("The CAD library is turned off in Settings."), nil
			}
			fmt.Fprintf(&b, "%d symbols and %d footprints in %s.\n", rep.Symbols, rep.Footprints, l.Dir)
			var formats []string
			for f, where := range rep.Written {
				formats = append(formats, fmt.Sprintf("- %s: %s", f, where))
			}
			sort.Strings(formats)
			b.WriteString(strings.Join(formats, "\n") + "\n")
			if len(rep.Adopted) > 0 {
				fmt.Fprintf(&b, "Kept the user's edits: %s.\n", strings.Join(rep.Adopted, ", "))
			}
			fmt.Fprintf(&b, "In KiCad: add %s (symbols) and %s (footprints) as libraries named %s.\n", l.KiCad.Symbols, l.KiCad.Footprints, l.Name)
			return text(b.String()), nil
		})
}
