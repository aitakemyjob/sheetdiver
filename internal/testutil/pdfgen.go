// Package testutil builds small synthetic PDFs for tests, so the whole
// import pipeline (PDF engine, extraction, library, MCP) is tested without
// shipping copyrighted manufacturer datasheets.
package testutil

import (
	"bytes"
	"fmt"
	"strings"
)

// Text is a run of text at a position (points, top-left origin).
type Text struct {
	X, Y float64
	Size float64
	Bold bool
	S    string
}

// Path is a stroked polyline, or a filled rectangle when Fill is set.
type Path struct {
	Points [][2]float64
	Width  float64
	Fill   bool
}

// PageSpec is one page's content.
type PageSpec struct {
	Texts []Text
	Paths []Path
}

// Outline is a bookmark to a page and height.
type Outline struct {
	Title string
	Page  int
	Y     float64
}

// PDF writes a minimal, valid PDF with Helvetica text, vector paths and an
// optional outline, on US Letter pages.
func PDF(title string, pages []PageSpec, outline []Outline) []byte {
	const w, h = 612.0, 792.0
	var objs []string // objs[i] is object i+1
	add := func(s string) int {
		objs = append(objs, s)
		return len(objs)
	}
	catalog := add("") // filled later
	pagesObj := add("")
	font := add("<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica /Encoding /WinAnsiEncoding >>")
	bold := add("<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica-Bold /Encoding /WinAnsiEncoding >>")
	var kids []string
	var pageIDs []int
	for _, p := range pages {
		var c strings.Builder
		for _, path := range p.Paths {
			if len(path.Points) == 0 {
				continue
			}
			fmt.Fprintf(&c, "%.2f w\n", max(path.Width, 0.5))
			for i, pt := range path.Points {
				op := "l"
				if i == 0 {
					op = "m"
				}
				fmt.Fprintf(&c, "%.2f %.2f %s\n", pt[0], h-pt[1], op)
			}
			if path.Fill {
				c.WriteString("h f\n")
			} else {
				c.WriteString("S\n")
			}
		}
		for _, t := range p.Texts {
			f := "F1"
			if t.Bold {
				f = "F2"
			}
			size := t.Size
			if size == 0 {
				size = 9
			}
			// Y is the top of the text; PDF positions the baseline.
			fmt.Fprintf(&c, "BT /%s %.1f Tf %.2f %.2f Td (%s) Tj ET\n", f, size, t.X, h-t.Y-size*0.8, escape(t.S))
		}
		content := add(fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", c.Len(), c.String()))
		id := add(fmt.Sprintf("<< /Type /Page /Parent %d 0 R /MediaBox [0 0 %.0f %.0f] /Resources << /Font << /F1 %d 0 R /F2 %d 0 R >> >> /Contents %d 0 R >>", pagesObj, w, h, font, bold, content))
		pageIDs = append(pageIDs, id)
		kids = append(kids, fmt.Sprintf("%d 0 R", id))
	}
	objs[pagesObj-1] = fmt.Sprintf("<< /Type /Pages /Kids [%s] /Count %d >>", strings.Join(kids, " "), len(kids))
	outlines := ""
	if len(outline) > 0 {
		root := add("")
		first := len(objs) + 1
		for i, o := range outline {
			id := first + i
			prev, next := "", ""
			if i > 0 {
				prev = fmt.Sprintf("/Prev %d 0 R ", id-1)
			}
			if i < len(outline)-1 {
				next = fmt.Sprintf("/Next %d 0 R ", id+1)
			}
			add(fmt.Sprintf("<< /Title (%s) /Parent %d 0 R %s%s/Dest [%d 0 R /XYZ 0 %.1f 0] >>", escape(o.Title), root, prev, next, pageIDs[o.Page], h-o.Y))
		}
		objs[root-1] = fmt.Sprintf("<< /Type /Outlines /First %d 0 R /Last %d 0 R /Count %d >>", first, first+len(outline)-1, len(outline))
		outlines = fmt.Sprintf(" /Outlines %d 0 R /PageMode /UseOutlines", root)
	}
	objs[catalog-1] = fmt.Sprintf("<< /Type /Catalog /Pages %d 0 R%s >>", pagesObj, outlines)
	info := add(fmt.Sprintf("<< /Title (%s) /Producer (SheetDiver tests) >>", escape(title)))

	var b bytes.Buffer
	b.WriteString("%PDF-1.4\n")
	offsets := make([]int, len(objs))
	for i, o := range objs {
		offsets[i] = b.Len()
		fmt.Fprintf(&b, "%d 0 obj\n%s\nendobj\n", i+1, o)
	}
	xref := b.Len()
	fmt.Fprintf(&b, "xref\n0 %d\n0000000000 65535 f \n", len(objs)+1)
	for _, off := range offsets {
		fmt.Fprintf(&b, "%010d 00000 n \n", off)
	}
	fmt.Fprintf(&b, "trailer\n<< /Size %d /Root %d 0 R /Info %d 0 R >>\nstartxref\n%d\n%%%%EOF\n", len(objs)+1, catalog, info, xref)
	return b.Bytes()
}

// escape escapes a PDF string literal (WinAnsi; non-ASCII is replaced).
func escape(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch {
		case r == '(' || r == ')' || r == '\\':
			b.WriteByte('\\')
			b.WriteRune(r)
		case r == '°':
			b.WriteString("\\260")
		case r == 'µ':
			b.WriteString("\\265")
		case r < 128:
			b.WriteRune(r)
		default:
			b.WriteByte('?')
		}
	}
	return b.String()
}

// Datasheet returns a synthetic three-page datasheet for "SD1234" and
// "SD1235" (family SD123x): a buck converter with an outline, a pin
// table, absolute-maximum, recommended and electrical tables, an
// efficiency graph and an ordering table with orderable codes.
func Datasheet() []byte {
	row := func(y float64, cells ...string) []Text {
		xs := []float64{60, 200, 340, 400, 450, 500}
		var out []Text
		for i, c := range cells {
			if c != "" && i < len(xs) {
				out = append(out, Text{X: xs[i], Y: y, Size: 8, S: c})
			}
		}
		return out
	}
	var p1 PageSpec
	p1.Texts = append(p1.Texts,
		Text{X: 60, Y: 30, Size: 9, S: "SD1234, SD1235"},
		Text{X: 60, Y: 80, Size: 18, Bold: true, S: "SD123x 4.5-V to 17-V Input, 3-A Synchronous Step-Down Converter"},
		Text{X: 60, Y: 120, Size: 12, Bold: true, S: "1 Features"},
		Text{X: 60, Y: 140, Size: 10, S: "4.5-V to 17-V input voltage range for the SD1234 and SD1235."},
		Text{X: 60, Y: 200, Size: 12, Bold: true, S: "5 Pin Configuration and Functions"},
		Text{X: 60, Y: 220, Size: 10, S: "Table 5-1. Pin Functions"},
		Text{X: 60, Y: 240, Size: 8, S: "NAME"}, Text{X: 120, Y: 240, Size: 8, S: "NO."}, Text{X: 160, Y: 240, Size: 8, S: "I/O"}, Text{X: 220, Y: 240, Size: 8, S: "DESCRIPTION"},
		Text{X: 60, Y: 256, Size: 8, S: "GND"}, Text{X: 120, Y: 256, Size: 8, S: "1"}, Text{X: 160, Y: 256, Size: 8, S: "G"}, Text{X: 220, Y: 256, Size: 8, S: "Ground pin."},
		Text{X: 60, Y: 270, Size: 8, S: "SW"}, Text{X: 120, Y: 270, Size: 8, S: "2"}, Text{X: 160, Y: 270, Size: 8, S: "O"}, Text{X: 220, Y: 270, Size: 8, S: "Switch node."},
		Text{X: 60, Y: 284, Size: 8, S: "VIN"}, Text{X: 120, Y: 284, Size: 8, S: "3"}, Text{X: 160, Y: 284, Size: 8, S: "P"}, Text{X: 220, Y: 284, Size: 8, S: "Input supply."},
		Text{X: 60, Y: 298, Size: 8, S: "VFB"}, Text{X: 120, Y: 298, Size: 8, S: "4"}, Text{X: 160, Y: 298, Size: 8, S: "I"}, Text{X: 220, Y: 298, Size: 8, S: "Feedback input."},
		Text{X: 60, Y: 360, Size: 12, Bold: true, S: "6 Specifications"},
		Text{X: 60, Y: 380, Size: 11, Bold: true, S: "6.1 Absolute Maximum Ratings"},
	)
	p1.Texts = append(p1.Texts, row(400, "PARAMETER", "", "MIN", "", "MAX", "UNIT")...)
	p1.Texts = append(p1.Texts, row(416, "Input voltage", "VIN", "-0.3", "", "20", "V")...)
	p1.Texts = append(p1.Texts, row(430, "Junction temperature", "TJ", "-40", "", "150", "°C")...)
	p1.Texts = append(p1.Texts, Text{X: 60, Y: 470, Size: 11, Bold: true, S: "6.3 Recommended Operating Conditions"})
	p1.Texts = append(p1.Texts, row(490, "PARAMETER", "", "MIN", "", "MAX", "UNIT")...)
	p1.Texts = append(p1.Texts, row(506, "VIN Supply input voltage", "", "4.5", "", "17", "V")...)
	p1.Texts = append(p1.Texts, row(520, "IOUT Output current", "", "0", "", "3", "A")...)

	var p2 PageSpec
	p2.Texts = append(p2.Texts,
		Text{X: 60, Y: 30, Size: 9, S: "SD1234, SD1235"},
		Text{X: 60, Y: 70, Size: 11, Bold: true, S: "6.5 Electrical Characteristics"},
	)
	p2.Texts = append(p2.Texts, row(90, "PARAMETER", "TEST CONDITIONS", "MIN", "TYP", "MAX", "UNIT")...)
	p2.Texts = append(p2.Texts, row(106, "VFB Feedback voltage", "TA = 25°C", "0.758", "0.765", "0.772", "V")...)
	p2.Texts = append(p2.Texts, row(120, "fSW Switching frequency", "VIN = 12 V", "", "650", "", "kHz")...)
	p2.Texts = append(p2.Texts, row(134, "ILIM Current limit", "SD1235", "3.5", "4.2", "5.3", "A")...)
	p2.Texts = append(p2.Texts, row(148, "tON(MIN) Minimum on time", "", "", "60", "90", "ns")...)
	p2.Texts = append(p2.Texts, Text{X: 60, Y: 200, Size: 11, Bold: true, S: "6.7 Typical Characteristics"})
	// An efficiency graph: frame, grid and a curve, with axis labels.
	frame := Path{Points: [][2]float64{{100, 230}, {300, 230}, {300, 380}, {100, 380}, {100, 230}}, Width: 1}
	curve := Path{Points: [][2]float64{{100, 360}, {130, 300}, {170, 270}, {220, 255}, {300, 262}}, Width: 1.5}
	grid := Path{Points: [][2]float64{{100, 305}, {300, 305}}, Width: 0.5}
	p2.Paths = append(p2.Paths, frame, curve, grid)
	p2.Texts = append(p2.Texts,
		Text{X: 160, Y: 386, Size: 7, S: "Output Current (A)"},
		Text{X: 70, Y: 300, Size: 7, S: "Eff (%)"},
		Text{X: 110, Y: 405, Size: 9, S: "Figure 6-1. Efficiency vs Output Current"},
	)
	p2.Texts = append(p2.Texts, row(162, "RSET setting #1", "", "-3%", "5.11", "+3%", "kOhm")...)
	var p3 PageSpec
	// A selection matrix (grouped rows: a label row, then a values row per
	// output voltage), a prose reference to it, and a recommended-inductor
	// table with a units row.
	p3.Texts = append(p3.Texts,
		Text{X: 60, Y: 180, Size: 10, S: "Table 7-1 summarizes the resistor values for each output voltage."},
		Text{X: 60, Y: 200, Size: 10, S: "Table 7-1. Output Voltage Settings"},
		Text{X: 60, Y: 216, Size: 8, S: "VOUT"}, Text{X: 200, Y: 216, Size: 8, S: "CURRENT LIMIT"},
	)
	for i, v := range []string{"1.8 V", "2.5 V", "3.3 V"} {
		y := 232 + float64(i)*30
		p3.Texts = append(p3.Texts,
			Text{X: 60, Y: y + 6, Size: 8, S: v}, Text{X: 110, Y: y, Size: 8, S: "RSET1"}, Text{X: 300, Y: y, Size: 8, S: []string{"0 Ohm", "511 Ohm", "1.15 kOhm"}[i]},
			Text{X: 110, Y: y + 12, Size: 8, S: "RSET2"}, Text{X: 170, Y: y + 12, Size: 8, S: "0 Ohm"}, Text{X: 230, Y: y + 12, Size: 8, S: "511 Ohm"},
			Text{X: 290, Y: y + 12, Size: 8, S: "1.15 kOhm"}, Text{X: 350, Y: y + 12, Size: 8, S: "1.87 kOhm"},
		)
	}
	p3.Texts = append(p3.Texts,
		Text{X: 60, Y: 360, Size: 10, S: "Table 8-1. List of Recommended Inductors"},
		Text{X: 60, Y: 376, Size: 8, S: "INDUCTOR"}, Text{X: 160, Y: 376, Size: 8, S: "SATURATION CURRENT"}, Text{X: 300, Y: 376, Size: 8, S: "PART NUMBER"}, Text{X: 420, Y: 376, Size: 8, S: "MANUFACTURER"},
		Text{X: 60, Y: 388, Size: 8, S: "VALUE [uH]"}, Text{X: 160, Y: 388, Size: 8, S: "[A]"},
		Text{X: 60, Y: 402, Size: 8, S: "2.2"}, Text{X: 160, Y: 402, Size: 8, S: "3.5"}, Text{X: 300, Y: 402, Size: 8, S: "XFL4020-222ME"}, Text{X: 420, Y: 402, Size: 8, S: "Coilcraft"},
		Text{X: 60, Y: 416, Size: 8, S: "1.5"}, Text{X: 160, Y: 416, Size: 8, S: "4.1"}, Text{X: 300, Y: 416, Size: 8, S: "XFL4020-152ME"}, Text{X: 420, Y: 416, Size: 8, S: "Coilcraft"},
	)
	p3.Texts = append(p3.Texts,
		Text{X: 60, Y: 30, Size: 9, S: "SD1234, SD1235"},
		Text{X: 60, Y: 70, Size: 12, Bold: true, S: "10 Mechanical, Packaging, and Orderable Information"},
		Text{X: 60, Y: 100, Size: 8, S: "Orderable part number"}, Text{X: 200, Y: 100, Size: 8, S: "Package"},
		Text{X: 60, Y: 116, Size: 8, S: "SD1234DDCR"}, Text{X: 200, Y: 116, Size: 8, S: "SOT-23 (DDC)"},
		Text{X: 60, Y: 130, Size: 8, S: "SD1235DDCR"}, Text{X: 200, Y: 130, Size: 8, S: "SOT-23 (DDC)"},
	)
	return PDF("SD123x 3-A Synchronous Step-Down Converter datasheet (Rev. A)", []PageSpec{p1, p2, p3}, []Outline{
		{Title: "1 Features", Page: 0, Y: 120},
		{Title: "5 Pin Configuration and Functions", Page: 0, Y: 200},
		{Title: "6 Specifications", Page: 0, Y: 360},
		{Title: "6.1 Absolute Maximum Ratings", Page: 0, Y: 380},
		{Title: "6.3 Recommended Operating Conditions", Page: 0, Y: 470},
		{Title: "6.5 Electrical Characteristics", Page: 1, Y: 70},
		{Title: "6.7 Typical Characteristics", Page: 1, Y: 200},
		{Title: "10 Mechanical, Packaging, and Orderable Information", Page: 2, Y: 70},
	})
}
