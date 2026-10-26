// Package docpdf writes simple PDF documents: headings, wrapped
// paragraphs, bullets and JPEG images flowing down A4 pages, in the
// standard Helvetica fonts every PDF reader has. It knows nothing of
// what the document says.
package docpdf

import (
	"bytes"
	"compress/zlib"
	"fmt"
	"strings"
)

// A4 in points, and the margins.
const (
	pageW, pageH = 595.0, 842.0
	margin       = 56.0
	bottom       = 60.0
	textW        = pageW - 2*margin
)

// Font is one of the standard fonts the document uses.
type Font int

// Fonts.
const (
	Regular Font = iota
	Bold
	Italic
)

var fontNames = []string{"Helvetica", "Helvetica-Bold", "Helvetica-Oblique"}

// Color is an RGB colour, 0–1 each.
type Color struct{ R, G, B float64 }

// Colours used.
var (
	Black = Color{0, 0, 0}
	Grey  = Color{0.4, 0.4, 0.4}
	Blue  = Color{0.1, 0.25, 0.55}
)

// picture is a JPEG drawn on a page.
type picture struct {
	data []byte
	w, h int
}

// page is a page's content stream and the images it draws.
type page struct {
	content bytes.Buffer
	images  []int
}

// Document is a PDF being written: pages added as content flows.
type Document struct {
	title  string
	pages  []*page
	images []picture
	y      float64 // where the next line goes, from the page's top
	footer string
}

// New starts a document; footer is printed at the bottom of each page
// with its number.
func New(title, footer string) *Document {
	d := &Document{title: title, footer: footer}
	d.newPage()
	return d
}

// newPage starts a page, writing from its top margin.
func (d *Document) newPage() {
	d.pages = append(d.pages, &page{})
	d.y = margin
}

// cur is the page being written.
func (d *Document) cur() *page { return d.pages[len(d.pages)-1] }

// room makes sure h points fit on the page, starting a new one if not.
func (d *Document) room(h float64) {
	if d.y+h > pageH-bottom && d.y > margin {
		d.newPage()
	}
}

// text writes one line at x (from the left) on the current line.
func (d *Document) text(x float64, s string, f Font, size float64, c Color) {
	fmt.Fprintf(&d.cur().content, "BT %.3f %.3f %.3f rg /F%d %.1f Tf %.2f %.2f Td (%s) Tj ET\n",
		c.R, c.G, c.B, int(f)+1, size, x, pageH-d.y-size, escape(winAnsi(s)))
}

// Heading writes a heading: level 1 large, 2 smaller, 3 bold body text.
func (d *Document) Heading(level int, s string) {
	size := map[int]float64{1: 18, 2: 14, 3: 11}[level]
	if size == 0 {
		size = 11
	}
	d.y += size * 0.6
	lines := wrap(s, Bold, size, textW)
	d.room(float64(len(lines))*size*1.25 + 3*12) // with a line or two of what follows
	for _, l := range lines {
		d.text(margin, l, Bold, size, Blue)
		d.y += size * 1.25
	}
	d.y += size * 0.25
}

// Paragraph writes wrapped text.
func (d *Document) Paragraph(s string) { d.Styled(s, Regular, 10, Black) }

// Small writes wrapped small grey text: sources, notes.
func (d *Document) Small(s string) { d.Styled(s, Italic, 8.5, Grey) }

// Styled writes wrapped text in a font, size and colour, keeping its
// line breaks.
func (d *Document) Styled(s string, f Font, size float64, c Color) {
	for _, para := range strings.Split(strings.TrimSpace(s), "\n") {
		for _, l := range wrap(para, f, size, textW) {
			d.room(size * 1.35)
			d.text(margin, l, f, size, c)
			d.y += size * 1.35
		}
	}
	d.y += size * 0.5
}

// Bullet writes a wrapped bullet point.
func (d *Document) Bullet(s string) {
	const size, indent = 10.0, 14.0
	for i, l := range wrap(s, Regular, size, textW-indent) {
		d.room(size * 1.35)
		if i == 0 {
			d.text(margin+3, "•", Regular, size, Black)
		}
		d.text(margin+indent, l, Regular, size, Black)
		d.y += size * 1.35
	}
	d.y += 2
}

// Image draws a JPEG (w×h pixels) as wide as the text, or smaller to fit
// half a page, with a caption under it.
func (d *Document) Image(jpeg []byte, w, h int, caption string) {
	if w <= 0 || h <= 0 {
		return
	}
	dw := min(textW, float64(w)*0.75) // 96 dpi at most
	dh := dw * float64(h) / float64(w)
	if limit := (pageH - margin - bottom) * 0.55; dh > limit {
		dh, dw = limit, limit*float64(w)/float64(h)
	}
	d.room(dh + 24)
	d.images = append(d.images, picture{data: jpeg, w: w, h: h})
	idx := len(d.images)
	d.cur().images = append(d.cur().images, idx)
	fmt.Fprintf(&d.cur().content, "q %.2f 0 0 %.2f %.2f %.2f cm /Im%d Do Q\n", dw, dh, margin, pageH-d.y-dh, idx)
	d.y += dh + 4
	if caption != "" {
		d.Small(caption)
	}
	d.y += 4
}

// Rule draws a thin line across the page.
func (d *Document) Rule() {
	d.room(10)
	fmt.Fprintf(&d.cur().content, "0.8 0.8 0.8 RG 0.5 w %.2f %.2f m %.2f %.2f l S\n", margin, pageH-d.y-4, pageW-margin, pageH-d.y-4)
	d.y += 10
}

// PageBreak starts a new page unless the current one is empty.
func (d *Document) PageBreak() {
	if d.y > margin {
		d.newPage()
	}
}

// Bytes is the finished PDF.
func (d *Document) Bytes() []byte {
	var out bytes.Buffer
	var offsets []int
	obj := func(body string) int {
		offsets = append(offsets, out.Len())
		n := len(offsets)
		fmt.Fprintf(&out, "%d 0 obj\n%s\nendobj\n", n, body)
		return n
	}
	stream := func(dict string, data []byte) int {
		offsets = append(offsets, out.Len())
		n := len(offsets)
		fmt.Fprintf(&out, "%d 0 obj\n<< %s /Length %d >>\nstream\n", n, dict, len(data))
		out.Write(data)
		out.WriteString("\nendstream\nendobj\n")
		return n
	}
	out.WriteString("%PDF-1.4\n%\xe2\xe3\xcf\xd3\n")
	// Objects 1 and 2 are the catalog and the page tree, written first
	// with the numbers the pages will have.
	nFonts, nImages := len(fontNames), len(d.images)
	first := 3 + nFonts + nImages + 1 // after the fonts, images and info
	var kids []string
	for i := range d.pages {
		kids = append(kids, fmt.Sprintf("%d 0 R", first+2*i))
	}
	obj("<< /Type /Catalog /Pages 2 0 R >>")
	obj(fmt.Sprintf("<< /Type /Pages /Count %d /Kids [%s] >>", len(d.pages), strings.Join(kids, " ")))
	var fontRefs []string
	for i, name := range fontNames {
		n := obj(fmt.Sprintf("<< /Type /Font /Subtype /Type1 /BaseFont /%s /Encoding /WinAnsiEncoding >>", name))
		fontRefs = append(fontRefs, fmt.Sprintf("/F%d %d 0 R", i+1, n))
	}
	imageObj := map[int]int{}
	for i, im := range d.images {
		imageObj[i+1] = stream(fmt.Sprintf("/Type /XObject /Subtype /Image /Width %d /Height %d /ColorSpace /DeviceRGB /BitsPerComponent 8 /Filter /DCTDecode", im.w, im.h), im.data)
	}
	obj(fmt.Sprintf("<< /Title (%s) /Producer (SheetDiver) >>", escape(winAnsi(d.title))))
	info := len(offsets)
	for i, p := range d.pages {
		var xobjs []string
		for _, idx := range p.images {
			xobjs = append(xobjs, fmt.Sprintf("/Im%d %d 0 R", idx, imageObj[idx]))
		}
		content := p.content.Bytes()
		if d.footer != "" {
			content = append(content, fmt.Sprintf("BT 0.5 0.5 0.5 rg /F1 8 Tf %.2f %.2f Td (%s) Tj ET\n", margin, 30.0, escape(winAnsi(fmt.Sprintf("%s  ·  page %d of %d", d.footer, i+1, len(d.pages)))))...)
		}
		pageNum := len(offsets) + 1
		obj(fmt.Sprintf("<< /Type /Page /Parent 2 0 R /MediaBox [0 0 %.0f %.0f] /Resources << /Font << %s >> /XObject << %s >> >> /Contents %d 0 R >>",
			pageW, pageH, strings.Join(fontRefs, " "), strings.Join(xobjs, " "), pageNum+1))
		var z bytes.Buffer
		zw := zlib.NewWriter(&z)
		_, _ = zw.Write(content)
		_ = zw.Close()
		stream("/Filter /FlateDecode", z.Bytes())
	}
	xref := out.Len()
	fmt.Fprintf(&out, "xref\n0 %d\n0000000000 65535 f \n", len(offsets)+1)
	for _, off := range offsets {
		fmt.Fprintf(&out, "%010d 00000 n \n", off)
	}
	fmt.Fprintf(&out, "trailer\n<< /Size %d /Root 1 0 R /Info %d 0 R >>\nstartxref\n%d\n%%%%EOF\n", len(offsets)+1, info, xref)
	return out.Bytes()
}

// escape escapes a PDF string's special characters.
func escape(s string) string {
	r := strings.NewReplacer(`\`, `\\`, `(`, `\(`, `)`, `\)`, "\r", "", "\n", " ")
	return r.Replace(s)
}

// winAnsiOf are the characters above ASCII the standard fonts have, as
// WinAnsiEncoding codes; others are written as their nearest ASCII.
var winAnsiOf = map[rune]byte{
	'€': 0x80, '‚': 0x82, '„': 0x84, '…': 0x85, '†': 0x86, '‰': 0x89, '‘': 0x91, '’': 0x92, '“': 0x93, '”': 0x94,
	'•': 0x95, '–': 0x96, '—': 0x97, '™': 0x99, '°': 0xB0, '±': 0xB1, '²': 0xB2, '³': 0xB3, 'µ': 0xB5, 'μ': 0xB5,
	'·': 0xB7, '×': 0xD7, '÷': 0xF7, '¼': 0xBC, '½': 0xBD, '©': 0xA9, '®': 0xAE, 'é': 0xE9, 'è': 0xE8, 'ü': 0xFC, 'ö': 0xF6, 'ä': 0xE4,
}

// asciiOf are characters the fonts lack, spelled out.
var asciiOf = map[rune]string{
	'Ω': "Ohm", '≤': "<=", '≥': ">=", '≈': "~", '≠': "!=", '−': "-", '→': "->", '←': "<-", '√': "sqrt", 'Δ': "delta", 'θ': "theta",
	'η': "eta", 'τ': "tau", 'π': "pi", '∞': "inf", '\t': " ", ' ': " ", 'ﬁ': "fi", 'ﬂ': "fl",
}

// winAnsi is a string in the fonts' encoding.
func winAnsi(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch {
		case r >= 32 && r < 127:
			b.WriteRune(r)
		case winAnsiOf[r] != 0:
			b.WriteByte(winAnsiOf[r])
		case asciiOf[r] != "":
			b.WriteString(asciiOf[r])
		case r >= 0xA0 && r <= 0xFF:
			b.WriteByte(byte(r))
		default:
			b.WriteByte('?')
		}
	}
	return b.String()
}

// wrap breaks text into lines no wider than width at a font size.
func wrap(s string, f Font, size, width float64) []string {
	words := strings.Fields(s)
	var lines []string
	line := ""
	for _, w := range words {
		try := w
		if line != "" {
			try = line + " " + w
		}
		if line != "" && textWidth(try, f, size) > width {
			lines = append(lines, line)
			line = w
			continue
		}
		line = try
	}
	if line != "" {
		lines = append(lines, line)
	}
	return lines
}

// textWidth is how wide a text is drawn, in points.
func textWidth(s string, f Font, size float64) float64 {
	widths := helvetica
	if f == Bold {
		widths = helveticaBold
	}
	total := 0
	for _, c := range []byte(winAnsi(s)) {
		if c >= 32 && c < 127 {
			total += widths[c-32]
		} else {
			total += 556
		}
	}
	return float64(total) * size / 1000
}

// Character widths of Helvetica and Helvetica-Bold, space to tilde, in
// thousandths of the font size (from Adobe's font metrics).
var (
	helvetica = [95]int{
		278, 278, 355, 556, 556, 889, 667, 191, 333, 333, 389, 584, 278, 333, 278, 278,
		556, 556, 556, 556, 556, 556, 556, 556, 556, 556, 278, 278, 584, 584, 584, 556,
		1015, 667, 667, 722, 722, 667, 611, 778, 722, 278, 500, 667, 556, 833, 722, 778,
		667, 778, 722, 667, 611, 722, 667, 944, 667, 667, 611, 278, 278, 278, 469, 556,
		333, 556, 556, 500, 556, 556, 278, 556, 556, 222, 222, 500, 222, 833, 556, 556,
		556, 556, 333, 500, 278, 556, 500, 722, 500, 500, 500, 334, 260, 334, 584,
	}
	helveticaBold = [95]int{
		278, 333, 474, 556, 556, 889, 722, 238, 333, 333, 389, 584, 278, 333, 278, 278,
		556, 556, 556, 556, 556, 556, 556, 556, 556, 556, 333, 333, 584, 584, 584, 611,
		975, 722, 722, 722, 722, 667, 611, 778, 722, 278, 556, 722, 611, 833, 722, 778,
		667, 778, 722, 667, 611, 722, 667, 944, 667, 667, 611, 333, 278, 333, 584, 556,
		333, 556, 611, 556, 611, 556, 333, 611, 611, 278, 278, 556, 278, 889, 611, 611,
		611, 611, 389, 556, 333, 611, 556, 778, 556, 556, 500, 389, 280, 389, 584,
	}
)
