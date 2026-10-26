package core

import (
	"bytes"
	"context"
	"fmt"
	"image"
	"image/draw"
	"image/jpeg"
	"image/png"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/aitakemyjob/sheetdiver/internal/docpdf"
	"github.com/aitakemyjob/sheetdiver/internal/extract"
	"github.com/aitakemyjob/sheetdiver/internal/kicad"
	"github.com/aitakemyjob/sheetdiver/internal/layoutguide"
	"github.com/aitakemyjob/sheetdiver/internal/library"
)

// The layout guide: for the board, general advice (ground, power trace
// widths, buses); for each part, what its datasheet says about layout
// (its layout sections, quoted with their pages, and its layout
// figures) and general advice for what it is; then the footprints the
// schematic uses. What the datasheets say and what is general practice
// are kept apart, so the reader knows which is which.

// maxLayoutFigures is how many layout figures of one datasheet go in.
const maxLayoutFigures = 4

// layoutGuide writes the guide as a PDF, with how many pieces of advice
// and datasheet sections it has.
func (s *Service) layoutGuide(ctx context.Context, detail ProjectDetail, info map[int64]entryInfo, sch kicad.Schematic) ([]byte, int) {
	name := detail.Project.Name
	doc := docpdf.New(name+" PCB layout guide", name+" layout guide · SheetDiver")
	doc.Heading(1, name+": PCB layout guide")
	doc.Small(fmt.Sprintf("Made by SheetDiver on %s with the KiCad schematic. \"From the datasheet\" quotes each part's datasheet, with its pages; \"General practice\" is common layout advice for what the part is, not from its datasheet. Where they differ, the datasheet wins.", time.Now().Format("2 January 2006")))
	advice := layoutguide.Default().Advise(boardOf(detail))
	count := len(advice)

	doc.Heading(2, "The whole board")
	for _, a := range layoutguide.BoardWide(advice) {
		doc.Bullet(a.Topic + ": " + a.Text)
	}

	nodes := diagramNodes(detail.Parts, detail.Connections.Blocks)
	seen := map[int64]string{} // datasheet → the part it was given for
	for _, p := range detail.Connections.Diagram.Parts {
		n := nodes[p.ID]
		ref := firstNonEmpty(n.ref, p.Title)
		doc.Heading(2, strings.TrimSpace(ref+"  "+p.MPN))
		if n.entry != nil && info[n.entry.ID].part.Description != "" {
			doc.Small(info[n.entry.ID].part.Description)
		}
		doc.Heading(3, "From the datasheet")
		switch {
		case n.entry == nil || n.entry.DatasheetID == 0:
			doc.Small("No datasheet in the library.")
		case seen[n.entry.DatasheetID] != "":
			doc.Small("Same datasheet as " + seen[n.entry.DatasheetID] + ": see there.")
		default:
			seen[n.entry.DatasheetID] = ref
			count += s.datasheetLayout(ctx, doc, n.entry.DatasheetID)
		}
		if mine := layoutguide.ForPart(advice, ref); len(mine) > 0 {
			doc.Heading(3, "General practice")
			for _, a := range mine {
				doc.Bullet(a.Topic + ": " + a.Text)
			}
		}
	}

	doc.Heading(2, "Footprints")
	doc.Small("As set in the schematic. Check each against its datasheet's land pattern before ordering boards; a part without one needs it chosen in KiCad.")
	for _, c := range sch.Components {
		if c.Footprint.LibID != "" {
			doc.Bullet(c.Ref + ": " + c.Footprint.LibID)
		} else {
			doc.Bullet(c.Ref + ": none (" + c.Footprint.Hint + ")")
		}
	}
	return doc.Bytes(), count
}

// datasheetLayout writes a datasheet's layout sections and figures, and
// says how many there were.
func (s *Service) datasheetLayout(ctx context.Context, doc *docpdf.Document, dsID int64) int {
	secs, _ := s.Lib.Sections(ctx, dsID, true)
	figs, _ := s.Lib.Figures(ctx, dsID, library.FigureFilter{Kind: extract.FigLayout})
	n := 0
	var printed []library.Section
	for _, sec := range secs {
		if sec.Kind != extract.KindLayout || strings.TrimSpace(sec.Text) == "" || slices.ContainsFunc(printed, func(p library.Section) bool { return within(sec, p) }) {
			continue
		}
		printed = append(printed, sec)
		n++
		doc.Styled(strings.TrimSpace(sec.Num+" "+sec.Title), docpdf.Bold, 10, docpdf.Black)
		for _, para := range strings.Split(reflow(sec.Text), "\n") {
			if heading, ok := strings.CutPrefix(para, "#"); ok {
				doc.Styled(strings.TrimLeft(heading, "# "), docpdf.Bold, 10, docpdf.Black)
				continue
			}
			doc.Paragraph(para)
		}
		doc.Small(pageRange(sec.Page, sec.EndPage))
	}
	for i, f := range figs {
		if i == maxLayoutFigures {
			doc.Small(fmt.Sprintf("%d more layout figures in the datasheet.", len(figs)-i))
			break
		}
		_, img, err := s.RenderFigure(ctx, f.ID, 110)
		if err != nil {
			continue
		}
		jpg, w, h, err := toJPEG(img)
		if err != nil {
			continue
		}
		n++
		doc.Image(jpg, w, h, strings.TrimSpace(fmt.Sprintf("Figure %s. %s (datasheet page %d)", f.Number, f.Caption, f.Page+1)))
	}
	if n == 0 {
		doc.Small("SheetDiver found no layout section or layout figure in this datasheet: look for layout advice in its application information.")
	}
	return n
}

// within reports whether a section is a subsection of another, whose
// text already has it.
func within(sec, parent library.Section) bool {
	starts := sec.Page > parent.Page || sec.Page == parent.Page && sec.Y >= parent.Y
	ends := sec.Page < parent.EndPage || sec.Page == parent.EndPage && sec.Y < parent.EndY
	return sec.Level > parent.Level && starts && ends
}

// pageRange names the datasheet pages a section is on (0-based in).
func pageRange(first, last int) string {
	if last <= first {
		return fmt.Sprintf("Datasheet page %d.", first+1)
	}
	return fmt.Sprintf("Datasheet pages %d–%d.", first+1, last+1)
}

// listItemRe starts a line that begins a new item: a bullet or a number.
var listItemRe = regexp.MustCompile(`^\s*(?:[•●▪◦\-–*]|\d+[.)]|[a-z][.)])\s`)

// reflow joins a section's lines back into paragraphs, one a line: a
// line goes on the one before unless that one ended a sentence or was a
// heading ("### 8.4.1 Layout Guidelines", as subsections are marked), or
// this one is a heading or starts a list item.
func reflow(text string) string {
	var out strings.Builder
	last := ""
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		switch {
		case last == "":
		case strings.HasPrefix(line, "#"), strings.HasPrefix(last, "#"), listItemRe.MatchString(line), strings.HasSuffix(last, "."), strings.HasSuffix(last, ":"):
			out.WriteString("\n")
		default:
			out.WriteString(" ")
		}
		out.WriteString(line)
		last = line
	}
	return out.String()
}

// toJPEG re-encodes a rendered PNG as a JPEG for the PDF.
func toJPEG(pngData []byte) ([]byte, int, int, error) {
	src, err := png.Decode(bytes.NewReader(pngData))
	if err != nil {
		return nil, 0, 0, err
	}
	b := src.Bounds()
	rgb := image.NewRGBA(b)
	draw.Draw(rgb, b, image.White, image.Point{}, draw.Src)
	draw.Draw(rgb, b, src, b.Min, draw.Over)
	var out bytes.Buffer
	if err := jpeg.Encode(&out, rgb, &jpeg.Options{Quality: 85}); err != nil {
		return nil, 0, 0, err
	}
	return out.Bytes(), b.Dx(), b.Dy(), nil
}

// boardOf is what the general advice is drawn from: the diagram's parts
// with their categories and pins, the nets, and the rails' currents.
func boardOf(detail ProjectDetail) layoutguide.Board {
	nodes := diagramNodes(detail.Parts, detail.Connections.Blocks)
	var b layoutguide.Board
	for _, p := range detail.Connections.Diagram.Parts {
		n := nodes[p.ID]
		part := layoutguide.Part{Ref: firstNonEmpty(n.ref, p.Title), MPN: p.MPN, Category: p.Kind}
		if n.entry != nil {
			part.Category = n.entry.Category
		}
		for _, pin := range p.Pins {
			part.Pins = append(part.Pins, layoutguide.Pin{Name: pin.Name, Role: pin.Role, Net: pin.Net})
		}
		b.Parts = append(b.Parts, part)
	}
	for _, q := range detail.Connections.Diagram.Passives {
		for _, label := range strings.Split(q.Label, ",") {
			b.Parts = append(b.Parts, layoutguide.Part{Ref: strings.TrimSpace(label), Category: q.Kind})
		}
	}
	for _, n := range detail.Connections.Nets {
		b.Nets = append(b.Nets, layoutguide.Net{Name: n.Name, Kind: n.Kind, Bus: n.Bus})
	}
	for _, r := range detail.Rails {
		b.Rails = append(b.Rails, layoutguide.Rail{Name: r.Name, Voltage: railNominal(r.Rail), Current: r.LoadMax})
	}
	return b
}
