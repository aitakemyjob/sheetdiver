package mcpserver

import (
	"context"
	"encoding/base64"
	"fmt"
	"strconv"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/aitakemyjob/sheetdiver/internal/core"
	"github.com/aitakemyjob/sheetdiver/internal/extract"
	"github.com/aitakemyjob/sheetdiver/internal/library"
	"github.com/aitakemyjob/sheetdiver/internal/pdf"
)

type importArgs struct {
	Path         string   `json:"path,omitempty" jsonschema:"local PDF path"`
	URL          string   `json:"url,omitempty" jsonschema:"http(s) URL of a PDF datasheet, or of a product page that links to it"`
	PartNumber   string   `json:"part_number,omitempty" jsonschema:"find the datasheet by part number at well-known manufacturer URLs (TI, ST, ADI, onsemi, Nexperia, Diodes)"`
	DataBase64   string   `json:"data_base64,omitempty" jsonschema:"the PDF itself, base64-encoded, when you downloaded it yourself (sites that block scripted downloads)"`
	Filename     string   `json:"filename,omitempty" jsonschema:"file name to store with data_base64"`
	MPNs         []string `json:"mpns,omitempty" jsonschema:"part numbers the datasheet covers, overriding detection (base numbers, e.g. TPS563200, STM32U545RE)"`
	Manufacturer string   `json:"manufacturer,omitempty"`
	Category     string   `json:"category,omitempty" jsonschema:"buck, boost, buck-boost, ldo, charger, mosfet-n, mosfet-p, gate-driver, mcu, current-monitor, …"`
	Reextract    bool     `json:"reextract,omitempty" jsonschema:"redo extraction of an already-imported datasheet (recorded data is kept)"`
	Show         bool     `json:"show,omitempty"`
}

type updatePartArgs struct {
	Part         string   `json:"part" jsonschema:"part number or id"`
	MPN          *string  `json:"mpn,omitempty" jsonschema:"rename the part number"`
	Manufacturer *string  `json:"manufacturer,omitempty"`
	Description  *string  `json:"description,omitempty"`
	Category     *string  `json:"category,omitempty"`
	Package      *string  `json:"package,omitempty"`
	Tags         []string `json:"tags,omitempty"`
	AddVariants  []string `json:"add_variants,omitempty" jsonschema:"orderable codes of this part to add (e.g. TPS563200DDCR)"`
	AddParts     []string `json:"add_parts,omitempty" jsonschema:"more base part numbers covered by the same datasheet"`
	Families     []string `json:"families,omitempty" jsonschema:"set the datasheet's family patterns, x = any character (STM32U545xx, TPS56x200)"`
}

type paramInput struct {
	ID         int64       `json:"id,omitempty" jsonschema:"update this existing parameter (from get_parameters) instead of adding"`
	Key        string      `json:"key,omitempty" jsonschema:"canonical key (vin, fsw, vref, ilim, ton_min, rdson, vgs_th, qg, …); needed for calculators"`
	Symbol     string      `json:"symbol,omitempty"`
	Name       string      `json:"name"`
	Conditions string      `json:"conditions,omitempty"`
	Min        *float64    `json:"min,omitempty"`
	Typ        *float64    `json:"typ,omitempty"`
	Max        *float64    `json:"max,omitempty"`
	Unit       string      `json:"unit,omitempty" jsonschema:"as printed: V, mV, µA, kHz, ns, mΩ, nC, pF, °C/W, %"`
	Section    string      `json:"section,omitempty" jsonschema:"abs_max, recommended, electrical, timing, thermal, esd (default electrical)"`
	Page       int         `json:"page,omitempty" jsonschema:"1-based page it is printed on"`
	AppliesTo  []string    `json:"applies_to,omitempty" jsonschema:"only these part numbers of a multi-part datasheet"`
	Note       string      `json:"note,omitempty"`
	Region     *regionArgs `json:"region,omitempty" jsonschema:"where on the page (points, top-left origin)"`
}

type recordParamsArgs struct {
	Part        string       `json:"part" jsonschema:"part number or id"`
	Parameters  []paramInput `json:"parameters"`
	ReplaceAuto bool         `json:"replace_auto,omitempty" jsonschema:"remove auto-extracted rows with the same key and section"`
	Delete      []int64      `json:"delete,omitempty" jsonschema:"parameter ids to delete (wrong auto-extracted rows)"`
}

type curveInput struct {
	Label      string `json:"label"`
	Conditions string `json:"conditions,omitempty"`
	Summary    string `json:"summary,omitempty" jsonschema:"what this trace shows, with values read off the graph"`
}

type describeFigureArgs struct {
	Part        string       `json:"part,omitempty"`
	Figure      string       `json:"figure" jsonschema:"figure id or number"`
	Kind        string       `json:"kind,omitempty" jsonschema:"graph, schematic, waveform, block_diagram, pinout, timing, layout, package, figure"`
	Title       string       `json:"title,omitempty" jsonschema:"a clear name, e.g. 'Efficiency vs output current, VIN = 12 V'"`
	XAxis       string       `json:"x_axis,omitempty" jsonschema:"quantity and unit, with range: 'Output current (A), 0.01–3'"`
	YAxis       string       `json:"y_axis,omitempty"`
	Conditions  string       `json:"conditions,omitempty" jsonschema:"test conditions printed on the figure"`
	Description string       `json:"description,omitempty" jsonschema:"what the figure shows and the key takeaways, with values"`
	Curves      []curveInput `json:"curves,omitempty"`
	AppliesTo   []string     `json:"applies_to,omitempty" jsonschema:"part numbers it applies to"`
	Region      *regionArgs  `json:"region,omitempty" jsonschema:"correct the figure's box (points, top-left)"`
}

type pinInput struct {
	Number      string `json:"number"`
	Name        string `json:"name"`
	Type        string `json:"type,omitempty" jsonschema:"I, O, I/O, P (power), G (ground), A (analog), OD, NC"`
	Description string `json:"description,omitempty"`
	Page        int    `json:"page,omitempty" jsonschema:"1-based"`
}

type recordPinsArgs struct {
	Part    string     `json:"part"`
	Package string     `json:"package,omitempty" jsonschema:"package these pins are for (replaces that package's pins)"`
	Pins    []pinInput `json:"pins"`
}

type fieldInput struct {
	Name        string `json:"name"`
	Bits        string `json:"bits" jsonschema:"e.g. 15:12 or 3"`
	Access      string `json:"access,omitempty" jsonschema:"R, W, RW, RC, W1C…"`
	Reset       string `json:"reset,omitempty"`
	Description string `json:"description,omitempty"`
}

type registerInput struct {
	Name        string       `json:"name"`
	Address     string       `json:"address,omitempty" jsonschema:"e.g. 0x00"`
	Width       int          `json:"width,omitempty" jsonschema:"bits"`
	Reset       string       `json:"reset,omitempty"`
	Access      string       `json:"access,omitempty"`
	Description string       `json:"description,omitempty"`
	Fields      []fieldInput `json:"fields,omitempty"`
	Page        int          `json:"page,omitempty" jsonschema:"1-based"`
}

type recordRegistersArgs struct {
	Part      string          `json:"part"`
	Registers []registerInput `json:"registers"`
}

type recInput struct {
	Role         string   `json:"role" jsonschema:"inductor, output capacitor, input capacitor, feedback resistor, bootstrap capacitor, diode, MOSFET, …"`
	Value        string   `json:"value,omitempty" jsonschema:"e.g. 2.2 µH, 22 µF 10 V X5R"`
	MPN          string   `json:"mpn,omitempty" jsonschema:"a specific suggested part number"`
	Manufacturer string   `json:"manufacturer,omitempty"`
	Conditions   string   `json:"conditions,omitempty" jsonschema:"when it applies, e.g. Vout = 1.05 V, fsw = 650 kHz"`
	Notes        string   `json:"notes,omitempty"`
	Page         int      `json:"page,omitempty" jsonschema:"1-based"`
	AppliesTo    []string `json:"applies_to,omitempty"`
}

type recordRecsArgs struct {
	Part            string     `json:"part"`
	Recommendations []recInput `json:"recommendations"`
	Delete          []int64    `json:"delete,omitempty" jsonschema:"recommendation ids to remove"`
}

type recordTableArgs struct {
	Part        string      `json:"part" jsonschema:"part number or id"`
	Table       string      `json:"table,omitempty" jsonschema:"table id, number (7-2) or caption fragment; omit (and give page and region) for a table the extractor missed"`
	Title       string      `json:"title,omitempty" jsonschema:"what the table is for, e.g. 'VOUT and input current limit by RCFG1/RCFG2'"`
	Description string      `json:"description,omitempty" jsonschema:"how to use it, assumptions, footnotes that matter"`
	Header      []string    `json:"header" jsonschema:"one name per column, with units, e.g. [\"RCFG1\", \"RCFG2\", \"VOUT (V)\", \"Input current limit\"]"`
	Rows        [][]string  `json:"rows" jsonschema:"the data, one array per row; flatten matrices to one row per combination"`
	Page        int         `json:"page,omitempty" jsonschema:"1-based page, for a new table"`
	Region      *regionArgs `json:"region,omitempty" jsonschema:"where it is on the page (points, top-left), for a new table"`
}

type noteArgs struct {
	Target string `json:"target" jsonschema:"part, figure, parameter, datasheet or project"`
	Ref    string `json:"ref" jsonschema:"the part number, figure id, parameter id, datasheet id or project name"`
	Text   string `json:"text" jsonschema:"the note (markdown)"`
}

type listNotesArgs struct {
	Target string `json:"target,omitempty" jsonschema:"part, figure, parameter, datasheet or project; empty lists all notes"`
	Ref    string `json:"ref,omitempty"`
}

// registerWrite adds the tools that change the library.
func (s *Server) registerWrite() {
	c := s.core

	addTool(s, &mcp.Tool{Name: "import_datasheet", Description: "Download and import a datasheet: by part number (SheetDiver tries the manufacturers' datasheet URLs), from a URL (a PDF or a product page linking to one), a local PDF path, or PDF bytes you downloaded yourself (data_base64). Extraction finds part numbers (incl. family patterns and orderable variants), sections, figures, spec tables and pins. Review with get_part and correct it. If a site blocks the download, fetch the PDF with your own tools and pass path or data_base64.", Annotations: &mcp.ToolAnnotations{Title: "Import datasheet", DestructiveHint: boolPtr(false), IdempotentHint: true, OpenWorldHint: boolPtr(true)}},
		importing, func(a importArgs) string { return "import " + firstNonEmpty(a.Path, a.URL, a.PartNumber, a.Filename) }, func(ctx context.Context, a importArgs) (result, error) {
			req := core.ImportRequest{Path: a.Path, URL: a.URL, Lookup: a.PartNumber, MPNs: a.MPNs, Manufacturer: a.Manufacturer, Category: a.Category, Force: a.Reextract, Filename: a.Filename}
			if a.DataBase64 != "" {
				data, err := base64.StdEncoding.DecodeString(strings.TrimSpace(a.DataBase64))
				if err != nil {
					return result{}, fmt.Errorf("data_base64: %w", err)
				}
				req.Data = data
				if req.Filename == "" {
					req.Filename = firstNonEmpty(a.PartNumber, "datasheet") + ".pdf"
				}
			}
			res, err := c.Import(ctx, req)
			if err != nil {
				return result{}, err
			}
			var b strings.Builder
			if res.Existing {
				b.WriteString("already in the library (pass reextract=true to redo extraction)\n")
			}
			fmt.Fprintf(&b, "datasheet #%d %s: %s\nmanufacturer: %s\n", res.Datasheet.ID, res.Datasheet.Filename, res.Datasheet.Title, res.Datasheet.Manufacturer)
			var mpns []string
			for _, p := range res.Parts {
				mpns = append(mpns, p.MPN+" ("+firstNonEmpty(p.Category, "?")+")")
			}
			fmt.Fprintf(&b, "parts: %s\n", strings.Join(mpns, ", "))
			if len(res.Families) > 0 {
				fmt.Fprintf(&b, "families: %s\n", strings.Join(res.Families, ", "))
			}
			if res.Variants > 0 {
				fmt.Fprintf(&b, "orderable variants: %d\n", res.Variants)
			}
			if res.Datasheet.SourceURL != "" {
				fmt.Fprintf(&b, "downloaded from %s\n", res.Datasheet.SourceURL)
			}
			if res.Counts != nil {
				fmt.Fprintf(&b, "extracted: %d pages, %d sections, %d figures, %d tables, %d parameters, %d pins in %s\n",
					res.Counts["pages"], res.Counts["sections"], res.Counts["figures"], res.Counts["tables"], res.Counts["parameters"], res.Counts["pins"], res.Took)
			}
			b.WriteString("Next: get_part to review; fix part numbers with update_part and key values with record_parameters.")
			if a.Show && len(res.Parts) > 0 {
				c.Show(core.Focus{Kind: "part", Part: res.Parts[0].MPN})
			}
			return text(b.String()), nil
		})

	addTool(s, &mcp.Tool{Name: "update_part", Description: "Correct a part: number, manufacturer, description, category, package, tags; add orderable variants or more part numbers covered by the same datasheet; set the datasheet's family patterns.", Annotations: writes("Update part", true)},
		write, func(a updatePartArgs) string { return "update part " + a.Part }, func(ctx context.Context, a updatePartArgs) (result, error) {
			r, err := c.ResolvePart(ctx, a.Part)
			if err != nil {
				return result{}, err
			}
			u := library.PartUpdate{MPN: a.MPN, Manufacturer: a.Manufacturer, Description: a.Description, Category: a.Category, Package: a.Package}
			if a.Tags != nil {
				u.Tags = &a.Tags
			}
			p, err := c.Lib.UpdatePart(ctx, r.Part.ID, u)
			if err != nil {
				return result{}, err
			}
			if len(a.AddVariants) > 0 {
				var vs []library.Variant
				for _, v := range a.AddVariants {
					vs = append(vs, library.Variant{MPN: v})
				}
				if err := c.Lib.SetVariants(ctx, p.ID, vs, library.SourceAgent); err != nil {
					return result{}, err
				}
			}
			for _, m := range a.AddParts {
				if _, err := c.Lib.AddPart(ctx, library.Part{MPN: m, Manufacturer: p.Manufacturer, Description: p.Description, Category: p.Category, DatasheetID: p.DatasheetID}); err != nil {
					return result{}, err
				}
			}
			if a.Families != nil && p.DatasheetID != 0 {
				d, err := c.Lib.Datasheet(ctx, p.DatasheetID)
				if err != nil {
					return result{}, err
				}
				d.Families = a.Families
				if err := c.Lib.UpdateDatasheet(ctx, d); err != nil {
					return result{}, err
				}
			}
			c.Changed("part", p.DatasheetID)
			d, err := c.PartDetail(ctx, strconv.FormatInt(p.ID, 10))
			if err != nil {
				return result{}, err
			}
			return text("updated\n" + partDetail(d)), nil
		})

	addTool(s, &mcp.Tool{Name: "record_parameters", Description: "Record verified spec values for a part's datasheet (add, or update by id), with key, section and page. Recorded values take precedence over auto-extracted ones and survive re-extraction; calculators and project checks use them. Also deletes wrong rows.", Annotations: writes("Record parameters", false)},
		write, func(a recordParamsArgs) string {
			return fmt.Sprintf("record %d parameters for %s", len(a.Parameters), a.Part)
		}, func(ctx context.Context, a recordParamsArgs) (result, error) {
			r, d, err := c.DatasheetOf(ctx, a.Part)
			if err != nil {
				return result{}, err
			}
			if len(a.Delete) > 0 {
				if err := c.Lib.DeleteParameters(ctx, d.ID, a.Delete); err != nil {
					return result{}, err
				}
			}
			var ps []library.Parameter
			for _, in := range a.Parameters {
				if strings.TrimSpace(in.Name) == "" && in.Key == "" {
					return result{}, fmt.Errorf("every parameter needs a name or key")
				}
				if in.Key != "" {
					if _, ok := extract.Keys[in.Key]; !ok {
						return result{}, fmt.Errorf("unknown key %q; known keys: %s", in.Key, strings.Join(sortedKeys(), ", "))
					}
				}
				p := library.Parameter{ID: in.ID, Key: in.Key, Symbol: in.Symbol, Name: firstNonEmpty(in.Name, extract.Keys[in.Key]), Conditions: in.Conditions,
					Min: in.Min, Typ: in.Typ, Max: in.Max, Unit: extract.NormalizeUnit(in.Unit), Section: firstNonEmpty(in.Section, extract.KindElectrical),
					Page: in.Page - 1, AppliesTo: in.AppliesTo, Note: in.Note}
				if in.Region != nil {
					p.Box = &pdf.Rect{X0: in.Region.X0, Y0: in.Region.Y0, X1: in.Region.X1, Y1: in.Region.Y1}
				}
				ps = append(ps, p)
			}
			out, err := c.Lib.RecordParameters(ctx, d.ID, ps, library.SourceAgent, a.ReplaceAuto)
			if err != nil {
				return result{}, err
			}
			c.Changed("parameters", d.ID)
			var b strings.Builder
			fmt.Fprintf(&b, "%s: recorded %d, deleted %d\n", r.Part.MPN, len(out), len(a.Delete))
			for _, p := range out {
				b.WriteString(paramLine(p) + "\n")
			}
			return text(b.String()), nil
		})

	addTool(s, &mcp.Tool{Name: "describe_figure", Description: "Record what a figure shows after looking at it: kind, a clear title, axes with ranges, conditions, a description with the values that matter, and one entry per curve. This makes graphs searchable by what they show.", Annotations: writes("Describe figure", true)},
		write, func(a describeFigureArgs) string { return "describe figure " + a.Part + " " + a.Figure }, func(ctx context.Context, a describeFigureArgs) (result, error) {
			f, err := c.FindFigure(ctx, a.Part, a.Figure)
			if err != nil {
				return result{}, err
			}
			u := library.FigureUpdate{}
			set := func(dst **string, v string) {
				if v != "" {
					*dst = &v
				}
			}
			set(&u.Kind, a.Kind)
			set(&u.Title, a.Title)
			set(&u.XAxis, a.XAxis)
			set(&u.YAxis, a.YAxis)
			set(&u.Conditions, a.Conditions)
			set(&u.Description, a.Description)
			if a.Curves != nil {
				cs := make([]library.Curve, len(a.Curves))
				for i, cv := range a.Curves {
					cs[i] = library.Curve{Label: cv.Label, Conditions: cv.Conditions, Summary: cv.Summary}
				}
				u.Curves = &cs
			}
			if a.AppliesTo != nil {
				u.AppliesTo = &a.AppliesTo
			}
			if a.Region != nil {
				u.Box = &pdf.Rect{X0: a.Region.X0, Y0: a.Region.Y0, X1: a.Region.X1, Y1: a.Region.Y1}
			}
			f, err = c.Lib.UpdateFigure(ctx, f.ID, u, library.SourceAgent)
			if err != nil {
				return result{}, err
			}
			c.Changed("figure", f.DatasheetID)
			return text("recorded\n" + figureDetail(f)), nil
		})

	addTool(s, &mcp.Tool{Name: "record_pins", Description: "Record a part's pin table (replacing auto-extracted pins, or one package's pins).", Annotations: writes("Record pins", true)},
		write, func(a recordPinsArgs) string { return fmt.Sprintf("record %d pins for %s", len(a.Pins), a.Part) }, func(ctx context.Context, a recordPinsArgs) (result, error) {
			_, d, err := c.DatasheetOf(ctx, a.Part)
			if err != nil {
				return result{}, err
			}
			pins := make([]library.Pin, len(a.Pins))
			for i, p := range a.Pins {
				pins[i] = library.Pin{Number: p.Number, Name: p.Name, Type: p.Type, Description: p.Description, Page: p.Page - 1}
			}
			if err := c.Lib.RecordPins(ctx, d.ID, a.Package, pins, library.SourceAgent); err != nil {
				return result{}, err
			}
			c.Changed("pins", d.ID)
			return text(fmt.Sprintf("recorded %d pins", len(pins))), nil
		})

	addTool(s, &mcp.Tool{Name: "record_registers", Description: "Record registers read from a register map (name, address, width, reset, access, description, bit fields). Replaces registers with the same name.", Annotations: writes("Record registers", true)},
		write, func(a recordRegistersArgs) string {
			return fmt.Sprintf("record %d registers for %s", len(a.Registers), a.Part)
		}, func(ctx context.Context, a recordRegistersArgs) (result, error) {
			_, d, err := c.DatasheetOf(ctx, a.Part)
			if err != nil {
				return result{}, err
			}
			regs := make([]library.Register, len(a.Registers))
			for i, r := range a.Registers {
				fs := make([]library.RegisterField, len(r.Fields))
				for j, f := range r.Fields {
					fs[j] = library.RegisterField{Name: f.Name, Bits: f.Bits, Access: f.Access, Reset: f.Reset, Description: f.Description}
				}
				regs[i] = library.Register{Name: r.Name, Address: r.Address, Width: r.Width, Reset: r.Reset, Access: r.Access, Description: r.Description, Fields: fs, Page: r.Page - 1}
			}
			if err := c.Lib.RecordRegisters(ctx, d.ID, regs, library.SourceAgent); err != nil {
				return result{}, err
			}
			c.Changed("registers", d.ID)
			return text(fmt.Sprintf("recorded %d registers", len(regs))), nil
		})

	addTool(s, &mcp.Tool{Name: "record_recommendations", Description: "Record components the datasheet recommends: inductor values and part numbers, capacitors, resistor values per output voltage, diodes. Calculators and project reviews show them next to computed values.", Annotations: writes("Record recommendations", false)},
		write, func(a recordRecsArgs) string {
			return fmt.Sprintf("record %d recommendations for %s", len(a.Recommendations), a.Part)
		}, func(ctx context.Context, a recordRecsArgs) (result, error) {
			_, d, err := c.DatasheetOf(ctx, a.Part)
			if err != nil {
				return result{}, err
			}
			for _, id := range a.Delete {
				if err := c.Lib.DeleteRecommendation(ctx, id); err != nil {
					return result{}, err
				}
			}
			recs := make([]library.Recommendation, len(a.Recommendations))
			for i, r := range a.Recommendations {
				recs[i] = library.Recommendation{Role: r.Role, Value: r.Value, MPN: r.MPN, Manufacturer: r.Manufacturer, Conditions: r.Conditions, Notes: r.Notes, Page: r.Page - 1, AppliesTo: r.AppliesTo}
			}
			if err := c.Lib.RecordRecommendations(ctx, d.ID, recs, library.SourceAgent); err != nil {
				return result{}, err
			}
			c.Changed("recommendations", d.ID)
			return text(fmt.Sprintf("recorded %d, deleted %d", len(recs), len(a.Delete))), nil
		})

	addTool(s, &mcp.Tool{Name: "record_table", Description: "Save a clean transcription of a table after reading its image (get_tables): header with units and one row per entry. Flatten matrix/selection tables to one row per combination so they can be searched (e.g. the resistor pair for a given output voltage and current limit). Replaces the automatically read cells and survives re-extraction.", Annotations: writes("Record table", true)},
		write, func(a recordTableArgs) string { return "record table " + a.Part + " " + a.Table }, func(ctx context.Context, a recordTableArgs) (result, error) {
			r, d, err := c.DatasheetOf(ctx, a.Part)
			if err != nil {
				return result{}, err
			}
			if len(a.Header) == 0 || len(a.Rows) == 0 {
				return result{}, fmt.Errorf("give the header and the rows")
			}
			for i, row := range a.Rows {
				if len(row) != len(a.Header) {
					return result{}, fmt.Errorf("row %d has %d cells but the header has %d columns", i+1, len(row), len(a.Header))
				}
			}
			var t library.Table
			if strings.TrimSpace(a.Table) != "" {
				found, err := c.FindTable(ctx, r.Part.MPN, a.Table)
				if err != nil {
					return result{}, err
				}
				u := library.TableUpdate{Header: &a.Header, Rows: &a.Rows}
				if a.Title != "" {
					u.Title = &a.Title
				}
				if a.Description != "" {
					u.Description = &a.Description
				}
				if t, err = c.Lib.RecordTable(ctx, found.ID, u, library.SourceAgent); err != nil {
					return result{}, err
				}
			} else {
				if a.Page < 1 || a.Region == nil {
					return result{}, fmt.Errorf("for a table the extractor missed, give page and region")
				}
				t, err = c.Lib.AddTable(ctx, library.Table{DatasheetID: d.ID, Page: a.Page - 1, Box: pdf.Rect{X0: a.Region.X0, Y0: a.Region.Y0, X1: a.Region.X1, Y1: a.Region.Y1},
					Title: a.Title, Description: a.Description, Header: a.Header, Rows: a.Rows}, library.SourceAgent)
				if err != nil {
					return result{}, err
				}
			}
			c.Changed("table", d.ID)
			return text(fmt.Sprintf("recorded table #%d (%d rows): %s", t.ID, len(t.Rows), firstNonEmpty(t.Title, t.Caption))), nil
		})

	addTool(s, &mcp.Tool{Name: "add_note", Description: "Attach a note to a part, figure, parameter, datasheet or project: design decisions, errata, gotchas, test results. Notes are searchable and shown to the user.", Annotations: writes("Add note", false)},
		write, func(a noteArgs) string { return "note on " + a.Target + " " + a.Ref }, func(ctx context.Context, a noteArgs) (result, error) {
			kind, id, dsID, err := s.noteTarget(ctx, a.Target, a.Ref)
			if err != nil {
				return result{}, err
			}
			n, err := c.Lib.AddNote(ctx, library.Note{TargetKind: kind, TargetID: id, Body: a.Text, Author: library.SourceAgent}, dsID)
			if err != nil {
				return result{}, err
			}
			c.Changed("note", dsID)
			if kind == library.TargetProject {
				c.ProjectChanged(id, "note")
			}
			return text(fmt.Sprintf("note #%d added to %s %s", n.ID, kind, id)), nil
		})

	addTool(s, &mcp.Tool{Name: "list_notes", Description: "List notes on a target, or every note.", Annotations: readOnly("List notes")},
		read, func(a listNotesArgs) string { return "notes " + a.Target + " " + a.Ref }, func(ctx context.Context, a listNotesArgs) (result, error) {
			kind, id := "", ""
			if a.Target != "" {
				var err error
				kind, id, _, err = s.noteTarget(ctx, a.Target, a.Ref)
				if err != nil {
					return result{}, err
				}
			}
			ns, err := c.Lib.Notes(ctx, kind, id)
			if err != nil {
				return result{}, err
			}
			if len(ns) == 0 {
				return text("no notes"), nil
			}
			var b strings.Builder
			for _, n := range ns {
				fmt.Fprintf(&b, "#%d %s %s (%s, %s): %s\n", n.ID, n.TargetKind, n.TargetID, n.Author, n.CreatedAt.Format("2006-01-02"), n.Body)
			}
			return text(b.String()), nil
		})
}

// noteTarget normalizes a note target, resolving parts to their MPN and
// returning the datasheet the note relates to (for scoped search).
func (s *Server) noteTarget(ctx context.Context, target, ref string) (string, string, int64, error) {
	c := s.core
	switch strings.ToLower(strings.TrimSpace(target)) {
	case library.TargetPart:
		r, err := c.ResolvePart(ctx, ref)
		if err != nil {
			return "", "", 0, err
		}
		return library.TargetPart, r.Part.MPN, r.Part.DatasheetID, nil
	case library.TargetFigure:
		id, err := strconv.ParseInt(strings.TrimPrefix(ref, "#"), 10, 64)
		if err != nil {
			return "", "", 0, fmt.Errorf("figure notes need the figure id (from list_figures)")
		}
		f, err := c.Lib.Figure(ctx, id)
		if err != nil {
			return "", "", 0, err
		}
		return library.TargetFigure, strconv.FormatInt(f.ID, 10), f.DatasheetID, nil
	case library.TargetParameter:
		id, err := strconv.ParseInt(strings.TrimPrefix(ref, "#"), 10, 64)
		if err != nil {
			return "", "", 0, fmt.Errorf("parameter notes need the parameter id (from get_parameters)")
		}
		p, err := c.Lib.Parameter(ctx, id)
		if err != nil {
			return "", "", 0, err
		}
		return library.TargetParameter, strconv.FormatInt(p.ID, 10), p.DatasheetID, nil
	case library.TargetDatasheet:
		id, err := strconv.ParseInt(strings.TrimPrefix(ref, "#"), 10, 64)
		if err != nil {
			return "", "", 0, fmt.Errorf("datasheet notes need the datasheet id")
		}
		if _, err := c.Lib.Datasheet(ctx, id); err != nil {
			return "", "", 0, err
		}
		return library.TargetDatasheet, strconv.FormatInt(id, 10), id, nil
	case library.TargetProject:
		p, err := c.Lib.Project(ctx, ref)
		if err != nil {
			return "", "", 0, err
		}
		return library.TargetProject, p.Name, 0, nil
	}
	return "", "", 0, fmt.Errorf("target must be part, figure, parameter, datasheet or project (got %q)", target)
}

// sortedKeys lists canonical parameter keys.
func sortedKeys() []string {
	out := make([]string, 0, len(extract.Keys))
	for k := range extract.Keys {
		out = append(out, k)
	}
	sortStrings(out)
	return out
}
