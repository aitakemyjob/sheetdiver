package mcpserver

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/aitakemyjob/sheetdiver/internal/core"
	"github.com/aitakemyjob/sheetdiver/internal/extract"
	"github.com/aitakemyjob/sheetdiver/internal/library"
	"github.com/aitakemyjob/sheetdiver/internal/pdf"
)

type noArgs struct{}

type searchArgs struct {
	Query string   `json:"query" jsonschema:"words to find: part numbers, parameter names, figure captions, pin names, register names, note text"`
	Kinds []string `json:"kinds,omitempty" jsonschema:"limit to: part, page, section, figure, parameter, pin, register, recommendation, note"`
	Part  string   `json:"part,omitempty" jsonschema:"search only this part's datasheet"`
	Limit int      `json:"limit,omitempty" jsonschema:"max results (default 30)"`
	Show  bool     `json:"show,omitempty" jsonschema:"also show the results in SheetDiver"`
}

type listPartsArgs struct {
	Query        string `json:"query,omitempty" jsonschema:"part number, variant or description fragment"`
	Category     string `json:"category,omitempty" jsonschema:"buck, boost, ldo, mosfet-n, mcu, charger, …"`
	Manufacturer string `json:"manufacturer,omitempty"`
	Limit        int    `json:"limit,omitempty" jsonschema:"max results (default 100)"`
}

type partArgs struct {
	Part string `json:"part" jsonschema:"part number (base MPN, orderable code or family member) or id"`
	Show bool   `json:"show,omitempty" jsonschema:"also open the part in SheetDiver"`
}

type paramArgs struct {
	Part    string `json:"part" jsonschema:"part number or id"`
	Key     string `json:"key,omitempty" jsonschema:"canonical key, e.g. vin, fsw, vref, ilim, rdson, qg (see list_calculators for all keys)"`
	Section string `json:"section,omitempty" jsonschema:"abs_max, recommended, electrical, timing, thermal, esd"`
	Query   string `json:"query,omitempty" jsonschema:"name/symbol/conditions fragment"`
	Show    bool   `json:"show,omitempty" jsonschema:"also show the parameter table in SheetDiver"`
}

type sectionArgs struct {
	Part     string `json:"part" jsonschema:"part number or id"`
	Section  string `json:"section,omitempty" jsonschema:"a kind (pins, registers, layout, design_procedure, component_selection, typical_application, detailed_description, features, ordering, …), a title fragment or a number like 8.2.1; empty lists the sections"`
	MaxChars int    `json:"max_chars,omitempty" jsonschema:"truncate the text (default 12000)"`
	Show     bool   `json:"show,omitempty" jsonschema:"also open the section's page in SheetDiver"`
}

type pagesArgs struct {
	Part     string `json:"part" jsonschema:"part number or id"`
	From     int    `json:"from" jsonschema:"first page (1-based)"`
	To       int    `json:"to,omitempty" jsonschema:"last page (default from; at most 10 pages per call)"`
	MaxChars int    `json:"max_chars,omitempty" jsonschema:"truncate the text (default 20000)"`
}

type regionArgs struct {
	X0 float64 `json:"x0"`
	Y0 float64 `json:"y0"`
	X1 float64 `json:"x1"`
	Y1 float64 `json:"y1"`
}

type pageImageArgs struct {
	Part   string      `json:"part" jsonschema:"part number or id"`
	Page   int         `json:"page" jsonschema:"page (1-based)"`
	Region *regionArgs `json:"region,omitempty" jsonschema:"crop to this box, in points from the top-left of the page"`
	DPI    int         `json:"dpi,omitempty" jsonschema:"resolution (default 110; up to 300 for small print)"`
	Show   bool        `json:"show,omitempty" jsonschema:"also open the page in SheetDiver"`
}

type figuresArgs struct {
	Part  string `json:"part" jsonschema:"part number or id"`
	Kind  string `json:"kind,omitempty" jsonschema:"graph, schematic, waveform, block_diagram, pinout, timing, layout, package, figure"`
	Query string `json:"query,omitempty" jsonschema:"caption or axis fragment, e.g. efficiency, ripple, RDS(on)"`
}

type figureArgs struct {
	Part   string `json:"part,omitempty" jsonschema:"part number (needed when figure is a number or caption)"`
	Figure string `json:"figure" jsonschema:"figure id, number (6-8) or caption fragment"`
	DPI    int    `json:"dpi,omitempty" jsonschema:"resolution (default 150)"`
	Show   bool   `json:"show,omitempty" jsonschema:"also open the figure in SheetDiver"`
}

type tablesArgs struct {
	Part  string `json:"part" jsonschema:"part number or id"`
	Table string `json:"table,omitempty" jsonschema:"table id, number (7-2) or caption fragment to read in full, with its image; omit to list tables"`
	DPI   int    `json:"dpi,omitempty" jsonschema:"image resolution (default 130)"`
	Show  bool   `json:"show,omitempty" jsonschema:"also show the table on its page in SheetDiver"`
}

type pinsArgs struct {
	Part    string `json:"part" jsonschema:"part number or id"`
	Package string `json:"package,omitempty" jsonschema:"only this package"`
}

type showArgs struct {
	Kind    string      `json:"kind" jsonschema:"part, page, figure, section, project, search"`
	Part    string      `json:"part,omitempty"`
	Page    int         `json:"page,omitempty" jsonschema:"1-based page"`
	Region  *regionArgs `json:"region,omitempty" jsonschema:"highlight this box on the page (points, top-left origin)"`
	Figure  string      `json:"figure,omitempty" jsonschema:"figure id or number"`
	Section string      `json:"section,omitempty"`
	Project string      `json:"project,omitempty"`
	Query   string      `json:"query,omitempty"`
	Message string      `json:"message,omitempty" jsonschema:"a short caption to show with it"`
}

// registerLibrary adds the read-only library tools.
func (s *Server) registerLibrary() {
	c := s.core

	addTool(s, &mcp.Tool{Name: "library_overview", Description: "Summarize the library: datasheets, parts by category, projects, and what the user is looking at in SheetDiver right now.", Annotations: readOnly("Library overview")},
		read, func(noArgs) string { return "overview" }, func(ctx context.Context, _ noArgs) (result, error) {
			ds, err := c.Lib.Datasheets(ctx)
			if err != nil {
				return result{}, err
			}
			parts, _ := c.Lib.Parts(ctx, library.PartFilter{})
			cats, _ := c.Lib.Categories(ctx)
			projs, _ := c.Lib.Projects(ctx)
			var b strings.Builder
			fmt.Fprintf(&b, "%d datasheets, %d parts\n", len(ds), len(parts))
			var cs []string
			for k, n := range cats {
				cs = append(cs, fmt.Sprintf("%s %d", firstNonEmpty(k, "uncategorized"), n))
			}
			sort.Strings(cs)
			if len(cs) > 0 {
				fmt.Fprintf(&b, "categories: %s\n", strings.Join(cs, ", "))
			}
			b.WriteString("\nparts:\n")
			for i, p := range parts {
				if i == 200 {
					fmt.Fprintf(&b, "… %d more (use list_parts)\n", len(parts)-200)
					break
				}
				fmt.Fprintf(&b, "  %s\n", partHeader(p))
			}
			if len(projs) > 0 {
				b.WriteString("\nprojects:\n")
				for _, p := range projs {
					fmt.Fprintf(&b, "  %s (%d parts, %d rails)\n", p.Name, p.Parts, p.Rails)
				}
			}
			if a := c.Config.Get().ActiveProject; a != "" {
				fmt.Fprintf(&b, "active project: %s\n", a)
			}
			b.WriteString("\n" + uiContextText(c.UIContext()))
			return text(b.String()), nil
		})

	addTool(s, &mcp.Tool{Name: "get_ui_context", Description: "What the user is looking at in SheetDiver: the part, page, figure or project open, and any selected text.", Annotations: readOnly("UI context")},
		read, func(noArgs) string { return "context" }, func(ctx context.Context, _ noArgs) (result, error) {
			return text(uiContextText(c.UIContext())), nil
		})

	addTool(s, &mcp.Tool{Name: "search", Description: "Full-text search across every datasheet: parts, page text, section titles, figure captions and descriptions, parameters, pins, registers, recommendations and notes. Results name the part and page.", Annotations: readOnly("Search")},
		read, func(a searchArgs) string { return "search " + a.Query }, func(ctx context.Context, a searchArgs) (result, error) {
			o := library.SearchOptions{Kinds: a.Kinds, Limit: a.Limit}
			if a.Part != "" {
				_, d, err := c.DatasheetOf(ctx, a.Part)
				if err != nil {
					return result{}, err
				}
				o.DatasheetID = d.ID
			}
			hits, err := c.Search(ctx, a.Query, o)
			if err != nil {
				return result{}, err
			}
			if a.Show {
				c.Show(core.Focus{Kind: "search", Query: a.Query})
			}
			if len(hits) == 0 {
				return text("no results for " + a.Query), nil
			}
			var b strings.Builder
			for _, h := range hits {
				where := strings.Join(h.Parts, ",")
				if h.Kind != "part" && h.Kind != "note" {
					where += fmt.Sprintf(" p%d", h.Page+1)
				}
				fmt.Fprintf(&b, "[%s %s] %s — %s | %s\n", h.Kind, h.Ref, where, h.Title, strings.Join(strings.Fields(h.Snippet), " "))
			}
			return text(b.String()), nil
		})

	addTool(s, &mcp.Tool{Name: "list_parts", Description: "List parts in the library, filtered by part number/variant/description, category or manufacturer.", Annotations: readOnly("List parts")},
		read, func(a listPartsArgs) string { return "list parts " + a.Query }, func(ctx context.Context, a listPartsArgs) (result, error) {
			if a.Limit == 0 {
				a.Limit = 100
			}
			ps, err := c.Lib.Parts(ctx, library.PartFilter{Query: a.Query, Category: a.Category, Manufacturer: a.Manufacturer, Limit: a.Limit})
			if err != nil {
				return result{}, err
			}
			if len(ps) == 0 {
				return text("no parts match; import a datasheet with import_datasheet"), nil
			}
			var b strings.Builder
			for _, p := range ps {
				fmt.Fprintf(&b, "%s — %s\n", partHeader(p), p.Description)
			}
			return text(b.String()), nil
		})

	addTool(s, &mcp.Tool{Name: "get_part", Description: "A part's overview: datasheet, sibling parts and orderable variants, the best value for each key parameter (with page), sections, figure counts, recommended components, projects and notes.", Annotations: readOnly("Get part")},
		read, func(a partArgs) string { return "part " + a.Part }, func(ctx context.Context, a partArgs) (result, error) {
			d, err := c.PartDetail(ctx, a.Part)
			if err != nil {
				return result{}, err
			}
			if a.Show {
				c.Show(core.Focus{Kind: "part", Part: d.Part.MPN})
			}
			return text(partDetail(d)), nil
		})

	addTool(s, &mcp.Tool{Name: "get_parameters", Description: "A part's spec parameters: min/typ/max, unit, conditions, section and page. Filter by canonical key, section or text. Auto-extracted rows can be wrong; verify against the page and fix them with record_parameters.", Annotations: readOnly("Get parameters")},
		read, func(a paramArgs) string { return "parameters " + a.Part + " " + a.Key + a.Query }, func(ctx context.Context, a paramArgs) (result, error) {
			r, ps, err := c.ParamsFor(ctx, a.Part, library.ParamFilter{Key: a.Key, Section: a.Section, Query: a.Query})
			if err != nil {
				return result{}, err
			}
			if a.Show {
				c.Show(core.Focus{Kind: "part", Part: r.Part.MPN, Tab: "parameters", Query: firstNonEmpty(a.Key, a.Query)})
			}
			if len(ps) == 0 {
				return text("no parameters match; read the spec pages with get_section (abs_max, recommended, electrical) and record what you find"), nil
			}
			var b strings.Builder
			fmt.Fprintf(&b, "%s: %d parameters\n", r.Part.MPN, len(ps))
			for _, p := range ps {
				b.WriteString(paramLine(p) + "\n")
			}
			return text(b.String()), nil
		})

	addTool(s, &mcp.Tool{Name: "get_section", Description: "Read a datasheet section's text by kind (pins, registers, layout, design_procedure, component_selection, typical_application, …), title or number. Without a section, lists the sections.", Annotations: readOnly("Get section")},
		read, func(a sectionArgs) string { return "section " + a.Part + " " + a.Section }, func(ctx context.Context, a sectionArgs) (result, error) {
			r, d, err := c.DatasheetOf(ctx, a.Part)
			if err != nil {
				return result{}, err
			}
			if strings.TrimSpace(a.Section) == "" {
				secs, err := c.Lib.Sections(ctx, d.ID, false)
				if err != nil {
					return result{}, err
				}
				var b strings.Builder
				for _, sc := range secs {
					fmt.Fprintf(&b, "%s%s %s [%s] p%d\n", strings.Repeat("  ", max(0, sc.Level-1)), sc.Num, sc.Title, sc.Kind, sc.Page+1)
				}
				return text(b.String()), nil
			}
			secs, err := c.Lib.FindSections(ctx, d.ID, a.Section)
			if err != nil {
				return result{}, err
			}
			if len(secs) == 0 {
				return text(fmt.Sprintf("no section matches %q on %s; call get_section without a section to list them, or search", a.Section, r.Part.MPN)), nil
			}
			if a.Show {
				c.Show(core.Focus{Kind: "page", Part: r.Part.MPN, DatasheetID: d.ID, Page: secs[0].Page, SectionID: secs[0].ID})
			}
			limit := a.MaxChars
			if limit == 0 {
				limit = 12000
			}
			var b strings.Builder
			for _, sc := range secs {
				fmt.Fprintf(&b, "## %s %s [%s] pages %d–%d\n%s\n\n", sc.Num, sc.Title, sc.Kind, sc.Page+1, sc.EndPage+1, sc.Text)
			}
			return text(truncate(b.String(), limit, "raise max_chars or read_pages")), nil
		})

	addTool(s, &mcp.Tool{Name: "read_pages", Description: "Plain text of datasheet pages (1-based, up to 10 at a time).", Annotations: readOnly("Read pages")},
		read, func(a pagesArgs) string { return fmt.Sprintf("pages %s %d-%d", a.Part, a.From, a.To) }, func(ctx context.Context, a pagesArgs) (result, error) {
			_, d, err := c.DatasheetOf(ctx, a.Part)
			if err != nil {
				return result{}, err
			}
			from, to := a.From, a.To
			if from < 1 {
				from = 1
			}
			if to < from {
				to = from
			}
			if to > d.Pages {
				to = d.Pages
			}
			if to-from > 9 {
				to = from + 9
			}
			var b strings.Builder
			for p := from; p <= to; p++ {
				pg, err := c.Lib.Page(ctx, d.ID, p-1)
				if err != nil {
					return result{}, err
				}
				fmt.Fprintf(&b, "=== page %d of %d ===\n%s\n", p, d.Pages, pg.Text)
			}
			limit := a.MaxChars
			if limit == 0 {
				limit = 20000
			}
			return text(truncate(b.String(), limit, "read fewer pages")), nil
		})

	addTool(s, &mcp.Tool{Name: "get_page_image", Description: "Render a datasheet page, or a region of it, as an image you can look at (schematics, tables that extracted poorly, small print). Coordinates are points from the top-left; pages are about 612×792.", Annotations: readOnly("Page image")},
		read, func(a pageImageArgs) string { return fmt.Sprintf("page image %s p%d", a.Part, a.Page) }, func(ctx context.Context, a pageImageArgs) (result, error) {
			r, d, err := c.DatasheetOf(ctx, a.Part)
			if err != nil {
				return result{}, err
			}
			var box *pdf.Rect
			if a.Region != nil {
				box = &pdf.Rect{X0: a.Region.X0, Y0: a.Region.Y0, X1: a.Region.X1, Y1: a.Region.Y1}
			}
			png, err := c.RenderPage(ctx, d.ID, a.Page-1, box, a.DPI)
			if err != nil {
				return result{}, err
			}
			if a.Show {
				c.Show(core.Focus{Kind: "page", Part: r.Part.MPN, DatasheetID: d.ID, Page: a.Page - 1, Box: box})
			}
			return result{text: fmt.Sprintf("%s page %d of %d", r.Part.MPN, a.Page, d.Pages), images: [][]byte{png}}, nil
		})

	addTool(s, &mcp.Tool{Name: "list_figures", Description: "List a part's figures: number, page, kind (graph, schematic, waveform, block_diagram, pinout, timing, layout, package), caption, axes and any description recorded.", Annotations: readOnly("List figures")},
		read, func(a figuresArgs) string { return "figures " + a.Part + " " + a.Kind + a.Query }, func(ctx context.Context, a figuresArgs) (result, error) {
			r, d, err := c.DatasheetOf(ctx, a.Part)
			if err != nil {
				return result{}, err
			}
			figs, err := c.Lib.Figures(ctx, d.ID, library.FigureFilter{Kind: a.Kind, Query: a.Query, MPN: r.Part.MPN})
			if err != nil {
				return result{}, err
			}
			if len(figs) == 0 {
				return text("no figures match"), nil
			}
			var b strings.Builder
			for _, f := range figs {
				b.WriteString(figureLine(f) + "\n")
			}
			return text(b.String()), nil
		})

	addTool(s, &mcp.Tool{Name: "get_figure", Description: "Get a figure as an image plus what is known about it. Look at it to read values off graphs or component values off reference schematics, then record them (describe_figure, record_parameters, record_recommendations).", Annotations: readOnly("Get figure")},
		read, func(a figureArgs) string { return "figure " + a.Part + " " + a.Figure }, func(ctx context.Context, a figureArgs) (result, error) {
			f, err := c.FindFigure(ctx, a.Part, a.Figure)
			if err != nil {
				return result{}, err
			}
			dpi := a.DPI
			if dpi == 0 {
				dpi = 150
			}
			f, png, err := c.RenderFigure(ctx, f.ID, dpi)
			if err != nil {
				return result{}, err
			}
			txt, _ := c.Lib.RegionText(ctx, f.DatasheetID, f.Page, f.Box)
			if a.Show {
				c.Show(core.Focus{Kind: "figure", DatasheetID: f.DatasheetID, FigureID: f.ID, Page: f.Page, Box: &f.Box})
			}
			return result{text: figureDetail(f) + "\ntext inside the figure:\n" + txt, images: [][]byte{png}}, nil
		})

	addTool(s, &mcp.Tool{Name: "get_tables", Description: "List a part's tables (spec, pin, recommended components, selection/configuration tables…), or read one: its image plus the cells as read. Tables marked IRREGULAR (matrices, grouped rows, multi-row headers) have unreliable cells: read the image and save a clean version with record_table.", Annotations: readOnly("Get tables")},
		read, func(a tablesArgs) string { return "tables " + a.Part + " " + a.Table }, func(ctx context.Context, a tablesArgs) (result, error) {
			r, d, err := c.DatasheetOf(ctx, a.Part)
			if err != nil {
				return result{}, err
			}
			var b strings.Builder
			if strings.TrimSpace(a.Table) != "" {
				t, err := c.FindTable(ctx, r.Part.MPN, a.Table)
				if err != nil {
					return result{}, err
				}
				if t.DatasheetID != d.ID {
					return result{}, fmt.Errorf("table %s is not on this datasheet", a.Table)
				}
				dpi := a.DPI
				if dpi == 0 {
					dpi = 130
				}
				_, png, err := c.RenderTable(ctx, t.ID, dpi)
				if err != nil {
					return result{}, err
				}
				if a.Show {
					c.Show(core.Focus{Kind: "page", Part: r.Part.MPN, DatasheetID: d.ID, Page: t.Page, Box: &t.Box})
				}
				b.WriteString(tableHeader(t) + "\n")
				switch {
				case t.Source != library.SourceAuto:
					fmt.Fprintf(&b, "transcribed by %s", t.Source)
					if t.Description != "" {
						b.WriteString(": " + t.Description)
					}
					b.WriteString("\n")
				case t.Irregular != "":
					fmt.Fprintf(&b, "IRREGULAR (%s): the cells below are scrambled. Read the image and record the table with record_table (one row per combination, header naming every column).\n", t.Irregular)
				}
				b.WriteString(strings.Join(t.Header, " | ") + "\n")
				for _, row := range t.Rows {
					b.WriteString(strings.Join(row, " | ") + "\n")
				}
				return result{text: b.String(), images: [][]byte{png}}, nil
			}
			ts, err := c.Lib.Tables(ctx, d.ID, false)
			if err != nil {
				return result{}, err
			}
			for _, t := range ts {
				b.WriteString(tableHeader(t) + " | columns: " + strings.Join(t.Header, ", ") + "\n")
			}
			if b.Len() == 0 {
				return text("no tables detected"), nil
			}
			return text(b.String()), nil
		})

	addTool(s, &mcp.Tool{Name: "get_pins", Description: "A part's pin table (number, name, type, description, package). If empty or wrong, read get_section with section=pins and record_pins.", Annotations: readOnly("Get pins")},
		read, func(a pinsArgs) string { return "pins " + a.Part }, func(ctx context.Context, a pinsArgs) (result, error) {
			r, d, err := c.DatasheetOf(ctx, a.Part)
			if err != nil {
				return result{}, err
			}
			pins, err := c.Lib.Pins(ctx, d.ID, a.Package)
			if err != nil {
				return result{}, err
			}
			if len(pins) == 0 {
				return text(fmt.Sprintf("no pins recorded for %s; read get_section(section=pins) and record them with record_pins", r.Part.MPN)), nil
			}
			var b strings.Builder
			for _, p := range pins {
				pkg := ""
				if p.Package != "" {
					pkg = "[" + p.Package + "] "
				}
				fmt.Fprintf(&b, "%s%s %s %s — %s (p%d, %s)\n", pkg, p.Number, p.Name, p.Type, p.Description, p.Page+1, p.Source)
			}
			return text(b.String()), nil
		})

	addTool(s, &mcp.Tool{Name: "get_registers", Description: "A part's registers: those recorded with record_registers, plus the register-map sections' text to read the rest from.", Annotations: readOnly("Get registers")},
		read, func(a partArgs) string { return "registers " + a.Part }, func(ctx context.Context, a partArgs) (result, error) {
			_, d, err := c.DatasheetOf(ctx, a.Part)
			if err != nil {
				return result{}, err
			}
			regs, err := c.Lib.Registers(ctx, d.ID)
			if err != nil {
				return result{}, err
			}
			var b strings.Builder
			for _, r := range regs {
				fmt.Fprintf(&b, "%s %s (%d-bit, reset %s, %s) — %s (p%d)\n", r.Address, r.Name, r.Width, r.Reset, r.Access, r.Description, r.Page+1)
				for _, f := range r.Fields {
					fmt.Fprintf(&b, "    [%s] %s %s reset %s — %s\n", f.Bits, f.Name, f.Access, f.Reset, f.Description)
				}
			}
			secs, _ := c.Lib.FindSections(ctx, d.ID, extract.KindRegisters)
			if len(secs) > 0 {
				b.WriteString("\nregister sections (get_section to read):\n")
				for _, sc := range secs {
					fmt.Fprintf(&b, "  %s %s p%d–%d (%d characters)\n", sc.Num, sc.Title, sc.Page+1, sc.EndPage+1, len(sc.Text))
				}
			}
			if b.Len() == 0 {
				return text("no registers recorded and no register sections found"), nil
			}
			return text(b.String()), nil
		})

	addTool(s, &mcp.Tool{Name: "show", Description: "Put something on the user's screen in SheetDiver: a part, a page (optionally highlighting a region), a figure, a section, a project or search results.", Annotations: &mcp.ToolAnnotations{Title: "Show", ReadOnlyHint: true, OpenWorldHint: boolPtr(false)}},
		read, func(a showArgs) string { return "show " + a.Kind + " " + a.Part + a.Project }, func(ctx context.Context, a showArgs) (result, error) {
			f := core.Focus{Kind: a.Kind, Message: a.Message, Query: a.Query, Project: a.Project}
			if a.Part != "" {
				r, d, err := c.DatasheetOf(ctx, a.Part)
				if err != nil {
					return result{}, err
				}
				f.Part, f.DatasheetID = r.Part.MPN, d.ID
			}
			if a.Page > 0 {
				f.Page = a.Page - 1
			}
			if a.Region != nil {
				f.Box = &pdf.Rect{X0: a.Region.X0, Y0: a.Region.Y0, X1: a.Region.X1, Y1: a.Region.Y1}
			}
			if a.Figure != "" {
				fig, err := c.FindFigure(ctx, a.Part, a.Figure)
				if err != nil {
					return result{}, err
				}
				f.FigureID, f.DatasheetID, f.Page, f.Box = fig.ID, fig.DatasheetID, fig.Page, &fig.Box
			}
			if a.Section != "" && f.DatasheetID != 0 {
				if secs, _ := c.Lib.FindSections(ctx, f.DatasheetID, a.Section); len(secs) > 0 {
					f.SectionID, f.Page = secs[0].ID, secs[0].Page
				}
			}
			c.Show(f)
			return text("shown"), nil
		})
}

// tableHeader renders a table's identity line with its flags.
func tableHeader(t library.Table) string {
	s := fmt.Sprintf("#%d p%d [%s]", t.ID, t.Page+1, t.Kind)
	if t.Number != "" {
		s += " Table " + t.Number
	}
	s += " " + firstNonEmpty(t.Title, t.Caption, t.Section)
	switch {
	case t.Source != library.SourceAuto:
		s += " (transcribed by " + t.Source + ")"
	case t.Irregular != "":
		s += " IRREGULAR: " + t.Irregular
	}
	return s
}

// uiContextText describes what the user is looking at.
func uiContextText(u core.UIContext) string {
	if u.View == "" {
		return "The user has not opened anything in SheetDiver yet (or the app window is closed)."
	}
	var b strings.Builder
	fmt.Fprintf(&b, "The user is looking at: %s", u.View)
	if u.Part != "" {
		fmt.Fprintf(&b, ", part %s", u.Part)
	}
	if u.DatasheetID != 0 {
		fmt.Fprintf(&b, ", page %d", u.Page+1)
	}
	if u.FigureID != 0 {
		fmt.Fprintf(&b, ", figure #%d", u.FigureID)
	}
	if u.Project != "" {
		fmt.Fprintf(&b, ", project %s", u.Project)
	}
	if u.Selection != "" {
		fmt.Fprintf(&b, "\nselected text: %s", u.Selection)
	}
	return b.String() + "\n"
}

// firstNonEmpty returns the first non-empty string.
func firstNonEmpty(ss ...string) string {
	for _, s := range ss {
		if strings.TrimSpace(s) != "" {
			return s
		}
	}
	return ""
}
