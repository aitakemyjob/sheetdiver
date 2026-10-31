package mcpserver

import (
	"context"
	"fmt"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/aitakemyjob/sheetdiver/internal/core"
	"github.com/aitakemyjob/sheetdiver/internal/library"
)

// Package dimension tools: what a part's datasheet says its packages
// measure, read from the dimension and land pattern tables, and recorded
// by an agent where the numbers are only on the drawing.

type getPackageArgs struct {
	Part    string `json:"part" jsonschema:"the part (MPN or orderable code)"`
	Package string `json:"package,omitempty" jsonschema:"only this package"`
	Image   bool   `json:"image,omitempty" jsonschema:"also return the page of each package's drawing"`
	Show    bool   `json:"show,omitempty" jsonschema:"open the part's Package tab in the app"`
}

type recordPackageArgs struct {
	Part      string               `json:"part"`
	Name      string               `json:"name" jsonschema:"the package's name as the pin table and datasheet call it (SOT-23-6, WSON-10)"`
	Code      string               `json:"code,omitempty" jsonschema:"the drawing's code (DDC0006A)"`
	Reference string               `json:"reference,omitempty" jsonschema:"its JEDEC outline (MO-193)"`
	Page      int                  `json:"page,omitempty" jsonschema:"the drawing's page (1-based)"`
	Dims      []library.PackageDim `json:"dims,omitempty" jsonschema:"the outline's dimensions in mm (convert inches): {symbol as printed (A, A1, b, c, D, E, E1, e, L, D2, E2), key: height, standoff, leadWidth, leadThickness, bodyLength (D, along the pin rows), bodyWidth (E1 on gull-wing, E on no-lead), span (E, lead tip to tip), pitch (e), leadLength (L), padWidth/padLength (exposed pad E2/D2), description, min, nom, max, note (BSC, REF)}"`
	Land      []library.PackageDim `json:"land,omitempty" jsonschema:"the recommended land pattern in mm: keys padWidth (X), padLength (Y), rowSpan (C: pad centre to centre across the rows), gap (G), outerSpan (Z), pitch, exposedPadWidth/exposedPadLength"`
	Notes     string               `json:"notes,omitempty" jsonschema:"where they came from and anything doubtful"`
}

// dimText is a dimension's line.
func dimText(d core.PackageDim) string {
	val := func(p *float64) string {
		if p == nil {
			return "–"
		}
		return fmt.Sprintf("%g", *p)
	}
	s := fmt.Sprintf("  %-4s %-14s min %s  nom %s  max %s mm", d.Symbol, d.Key, val(d.Min), val(d.Nom), val(d.Max))
	if d.Note != "" {
		s += " " + d.Note
	}
	if d.Unit != "" && d.Unit != "mm" {
		s += " (printed in " + d.Unit + ")"
	}
	if d.Description != "" {
		s += " — " + d.Description
	}
	if d.Source != library.SourceAuto {
		s += " [" + d.Source + "]"
	}
	return s + "\n"
}

// packagesText describes a part's packages for an agent.
func packagesText(pp core.PartPackages, only string) string {
	var b strings.Builder
	fmt.Fprintf(&b, "# %s packages\n", pp.Part.MPN)
	n := 0
	for _, p := range pp.Packages {
		if only != "" && !strings.EqualFold(p.Name, only) && !strings.EqualFold(p.Code, only) {
			continue
		}
		n++
		fmt.Fprintf(&b, "\n## %s", p.Name)
		if p.Code != "" {
			fmt.Fprintf(&b, " (%s)", p.Code)
		}
		if p.Reference != "" {
			fmt.Fprintf(&b, ", JEDEC %s", p.Reference)
		}
		fmt.Fprintf(&b, " — page %d, from %s", p.Page+1, strings.Join(p.Sources, " and "))
		if p.Pins > 0 {
			fmt.Fprintf(&b, "; %d pins in the pin table", p.Pins)
		}
		b.WriteString("\n")
		if len(p.Dims) > 0 {
			b.WriteString("outline:\n")
			for _, d := range p.Dims {
				b.WriteString(dimText(d))
			}
		}
		if len(p.Land) > 0 {
			b.WriteString("land pattern:\n")
			for _, d := range p.Land {
				b.WriteString(dimText(d))
			}
		}
		if len(p.Dims) == 0 && len(p.Land) == 0 && len(p.Candidates) > 0 {
			b.WriteString("No dimension table: the numbers are on the drawing. Read them against its image (image=true) and record_package them. Numbers found on it (stacked max/min joined):\n  " + strings.Join(p.Candidates, " | ") + "\n")
		}
		if p.Notes != "" {
			fmt.Fprintf(&b, "notes: %s\n", p.Notes)
		}
		for _, c := range p.Checks {
			fmt.Fprintf(&b, "check: %s\n", c)
		}
	}
	if n == 0 {
		b.WriteString("\nNo package dimensions were read from the datasheet. Find the package drawing (get_section kind package, list_figures, get_page_image), read it, and record_package it.\n")
	}
	return b.String()
}

// registerPackages adds the package dimension tools.
func (s *Server) registerPackages() {
	c := s.core
	showPkg := func(mpn string) { c.Show(core.Focus{Kind: "part", Part: mpn, Tab: "package"}) }

	addTool(s, &mcp.Tool{Name: "get_package", Description: "A part's package dimensions as its datasheet gives them: each package's outline (A, A1, b, c, D, E, E1, e, L, exposed pad… with min/nom/max in mm, inch tables converted) and recommended land pattern (X, Y, C, G, Z), its drawing code and JEDEC outline, and checks that they add up with the pin table. Where the datasheet has only a drawing (TI's), the numbers on it are listed for reading against its image. What an agent recorded wins over what was extracted. Footprints are drawn from these (get_cad).", Annotations: readOnly("Get package")},
		read, func(a getPackageArgs) string { return "package " + a.Part }, func(ctx context.Context, a getPackageArgs) (result, error) {
			pp, err := c.PartPackages(ctx, a.Part)
			if err != nil {
				return result{}, err
			}
			if a.Show {
				showPkg(pp.Part.MPN)
			}
			res := text(packagesText(pp, a.Package))
			if a.Image {
				seen := map[int]bool{}
				for _, p := range pp.Packages {
					if seen[p.Page] || a.Package != "" && !strings.EqualFold(p.Name, a.Package) && !strings.EqualFold(p.Code, a.Package) {
						continue
					}
					seen[p.Page] = true
					if png, err := c.RenderPage(ctx, pp.Part.DatasheetID, p.Page, nil, 0); err == nil {
						res.images = append(res.images, png)
					}
				}
			}
			return res, nil
		})

	addTool(s, &mcp.Tool{Name: "record_package", Description: "Record a package's dimensions as you read them from the datasheet's package drawing (get_package with image=true, get_page_image): its outline and its recommended land pattern, in mm. Kept when the datasheet is re-extracted, and wins over the extractor's values dimension by dimension. The part's footprint is redrawn from them unless an agent drew it (draw_footprint) or the user edited it.", Annotations: writes("Record package", true)},
		write, func(a recordPackageArgs) string { return "record package " + a.Name + " " + a.Part }, func(ctx context.Context, a recordPackageArgs) (result, error) {
			page := a.Page - 1
			if page < 0 {
				page = 0
			}
			pp, err := c.RecordPackage(ctx, a.Part, library.PackageDims{Name: a.Name, Code: a.Code, Reference: a.Reference, Page: page, Dims: a.Dims, Land: a.Land, Notes: a.Notes}, library.SourceAgent)
			if err != nil {
				return result{}, err
			}
			return text("recorded.\n" + packagesText(pp, a.Name)), nil
		})
}
