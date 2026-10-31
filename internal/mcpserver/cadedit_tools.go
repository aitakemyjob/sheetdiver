package mcpserver

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/aitakemyjob/sheetdiver/internal/cad"
	"github.com/aitakemyjob/sheetdiver/internal/core"
	"github.com/aitakemyjob/sheetdiver/internal/library"
)

// CAD editing tools: an agent reads a part's symbol, footprint and 3D
// model exactly (get_cad_model), changes what it needs and gives the
// whole thing back. What it gives is kept as an edit, like one the user
// makes in the app's editors, and written to every library format.

type cadModelArgs struct {
	Part    string `json:"part"`
	Package string `json:"package,omitempty" jsonschema:"which of its packages, when it comes in several"`
	What    string `json:"what,omitempty" jsonschema:"symbol, footprint or 3d; all three when empty"`
}

type editSymbolArgs struct {
	Part    string     `json:"part"`
	Package string     `json:"package,omitempty"`
	Symbol  cad.Symbol `json:"symbol" jsonschema:"the whole symbol as it should be, in mm with y up (from get_cad_model): body {x0,y0,x1,y1} (the box; all zero for none), pins [{number, name, type (KiCad's: input output bidirectional tri_state passive power_in power_out open_collector no_connect unspecified), side (left/right/top/bottom: the body side it sticks out of), x, y (where a wire connects, on the 2.54 mm grid), length, hidden}], graphics [{kind line|rect|circle|arc|poly|text, pts, r, width, fill, text, size, angle}] (line: 2 points; rect: 2 corners; circle: centre and r; arc: start, a point on it, end; poly: corners; text: baseline start, size, angle degrees), reference, value"`
	Notes   string     `json:"notes,omitempty"`
	Show    bool       `json:"show,omitempty"`
}

type editFootprintArgs struct {
	Part      string        `json:"part"`
	Package   string        `json:"package,omitempty"`
	Footprint cad.Footprint `json:"footprint" jsonschema:"the whole footprint as it should be, in mm with y down, from get_cad_model: pads [{number, kind smd|thru|npth|paste, shape rect|roundrect|circle|oval, x, y (centre), w, h, drill, noPaste, via}], lines [{layer silk|fab|courtyard, a, b, width}] and polys (SheetDiver's own outlines: keep, change or drop them), graphics [{kind line|rect|circle|arc|poly|text, layer silk|fab|courtyard|copper, pts, r, width, fill, text, size, angle}] (line: 2 points; rect: 2 corners; circle: centre and r; arc: start, a point on it, end; poly: corners; text: baseline start). The courtyard and body boxes and the mounting follow from what's drawn"`
	Notes     string        `json:"notes,omitempty"`
	Show      bool          `json:"show,omitempty"`
}

type edit3DArgs struct {
	Part    string        `json:"part"`
	Package string        `json:"package,omitempty"`
	Shapes  []cad.Shape3D `json:"shapes" jsonschema:"the whole 3D model as shapes, in mm in the footprint's coordinates (y down) with z up from the board (from get_cad_model): {kind box|cylinder|solid, part body|lead|pad|mark, color black|grey|tan|brown|blue, name, min {0:x,1:y,2:z} and max (a box's corners; a cylinder's z range), x, y, r (a cylinder's centre and radius), offset (moves a solid as built; keep its solid as given)}"`
	Notes   string        `json:"notes,omitempty"`
	Show    bool          `json:"show,omitempty"`
}

// registerCadEdit adds the CAD editing tools.
func (s *Server) registerCadEdit() {
	c := s.core
	showCad := func(mpn string) { c.Show(core.Focus{Kind: "part", Part: mpn, Tab: "cad"}) }
	done := func(pc core.PartCad, ps []cad.Problem, show bool) result {
		if show {
			showCad(pc.Part.MPN)
		}
		return text("kept as an edit; the library is rewritten.\n" + problemsText(ps) + cadText(pc))
	}

	addTool(s, &mcp.Tool{Name: "get_cad_model", Description: "A part's symbol, footprint and 3D model exactly, as JSON to change and give back to edit_symbol, edit_footprint or edit_3d_model: every pin, pad, line and drawn graphic with its coordinates, and the 3D model's shapes.", Annotations: readOnly("Get CAD model")},
		read, func(a cadModelArgs) string { return "cad model " + a.Part }, func(ctx context.Context, a cadModelArgs) (result, error) {
			m, err := c.CadModelOf(ctx, a.Part, a.Package)
			if err != nil {
				return result{}, err
			}
			switch a.What {
			case "symbol":
				m.Footprint, m.Shapes = nil, nil
			case "footprint":
				m.Symbol, m.Shapes = nil, nil
			case "3d":
				m.Symbol, m.Footprint = nil, nil
			case "":
			default:
				return result{}, fmt.Errorf("what %q isn't symbol, footprint or 3d", a.What)
			}
			b, _ := json.MarshalIndent(m, "", " ")
			return text(string(b)), nil
		})

	addTool(s, &mcp.Tool{Name: "edit_symbol", Description: "Change a part's symbol freely: move, add or remove pins, resize the body, draw lines, rectangles, circles, arcs, polygons and texts on it. Give the whole symbol (get_cad_model, change it, give it back). It's checked (pins need a number, a side and a type; shapes must be whole) and against the footprint's pads, kept as an edit (as if the user made it: it isn't redrawn until reset_cad), and written to every library format. When the user edited it themselves, change only what they asked for.", Annotations: writes("Edit symbol", true)},
		write, func(a editSymbolArgs) string { return "edit symbol " + a.Part }, func(ctx context.Context, a editSymbolArgs) (result, error) {
			pc, ps, err := c.EditSymbol(ctx, a.Part, a.Package, a.Symbol, a.Notes, library.SourceAgent)
			if err != nil {
				return result{}, fmt.Errorf("%w\n%s", err, problemsText(ps))
			}
			return done(pc, ps, a.Show), nil
		})

	addTool(s, &mcp.Tool{Name: "edit_footprint", Description: "Change a part's footprint freely: move, resize, add or remove pads (surface, plated or bare holes, paste windows), and draw on silkscreen, fab, courtyard or copper. Give the whole footprint (get_cad_model, change it, give it back). It's checked (pads need a size, holes a drill; shapes must be whole) and against the symbol's pins, kept as an edit (not redrawn until reset_cad), and written to every library format. Look at it after with get_cad_image. When the user edited it themselves, change only what they asked for.", Annotations: writes("Edit footprint", true)},
		write, func(a editFootprintArgs) string { return "edit footprint " + a.Part }, func(ctx context.Context, a editFootprintArgs) (result, error) {
			pc, ps, err := c.EditFootprint(ctx, a.Part, a.Package, a.Footprint, a.Notes, library.SourceAgent)
			if err != nil {
				return result{}, fmt.Errorf("%w\n%s", err, problemsText(ps))
			}
			return done(pc, ps, a.Show), nil
		})

	addTool(s, &mcp.Tool{Name: "edit_3d_model", Description: "Change a part's 3D model: resize or move the body and leads, add boxes and cylinders (a heatsink tab, a lens, a polarity mark), remove shapes. Give every shape (get_cad_model with what=3d, change, give back). Kept beside the footprint, whoever edits the footprint, until reset_cad with kind 3d; written as STEP to KiCad and embedded in Altium. Look at it after with get_cad_image what=3d.", Annotations: writes("Edit 3D model", true)},
		write, func(a edit3DArgs) string { return "edit 3d " + a.Part }, func(ctx context.Context, a edit3DArgs) (result, error) {
			pc, ps, err := c.EditModel3D(ctx, a.Part, a.Package, a.Shapes, a.Notes)
			if err != nil {
				return result{}, err
			}
			return done(pc, ps, a.Show), nil
		})
}
