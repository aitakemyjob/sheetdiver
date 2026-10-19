package extract

import (
	"math"
	"sort"
	"strings"

	"github.com/aitakemyjob/sheetdiver/internal/pdf"
)

// Row is every span whose vertical centre lies on the same baseline,
// left to right. Tables are read row by row; prose is read as Lines.
type Row struct {
	Y     float64
	Box   pdf.Rect
	Spans []pdf.Span
}

// Rows groups a page's spans into baseline rows, top to bottom. Rotated
// or very tall spans (vertical axis labels) end up in rows of their own.
func Rows(spans []pdf.Span) []Row {
	sorted := append([]pdf.Span(nil), spans...)
	sort.SliceStable(sorted, func(i, j int) bool { return centerY(sorted[i].Box) < centerY(sorted[j].Box) })
	var rows []Row
	for _, s := range sorted {
		c := centerY(s.Box)
		h := math.Max(1, math.Min(s.Box.H(), s.Size))
		if n := len(rows); n > 0 {
			last := &rows[n-1]
			lh := math.Max(1, last.Box.H())
			// Subscripts and superscripts are set smaller and shifted; they
			// belong to the row whose box they sit inside.
			script := s.Size < rowSize(*last)*0.88 && c >= last.Box.Y0-1 && c <= last.Box.Y1+2
			if script || math.Abs(c-last.Y) <= math.Min(h, lh)*0.45 {
				last.Spans = append(last.Spans, s)
				last.Box = last.Box.Union(s.Box)
				continue
			}
		}
		rows = append(rows, Row{Y: c, Box: s.Box, Spans: []pdf.Span{s}})
	}
	for i := range rows {
		sort.SliceStable(rows[i].Spans, func(a, b int) bool { return rows[i].Spans[a].Box.X0 < rows[i].Spans[b].Box.X0 })
	}
	return rows
}

// rowSize is the largest font size in a row.
func rowSize(r Row) float64 {
	m := 0.0
	for _, s := range r.Spans {
		m = math.Max(m, s.Size)
	}
	return m
}

// centerY is a rectangle's vertical centre.
func centerY(r pdf.Rect) float64 { return (r.Y0 + r.Y1) / 2 }

// centerX is a rectangle's horizontal centre.
func centerX(r pdf.Rect) float64 { return (r.X0 + r.X1) / 2 }

// Lines splits each row at gaps wider than about two and a half em, so
// side-by-side columns and captions stay separate, and joins the spans of
// each piece with single spaces.
func Lines(p pdf.Page) []Line {
	var out []Line
	for _, row := range Rows(p.Spans) {
		var cur *Line
		flush := func() {
			if cur != nil {
				cur.Text = strings.TrimSpace(cur.Text)
				if cur.Text != "" {
					out = append(out, *cur)
				}
				cur = nil
			}
		}
		for _, s := range row.Spans {
			size := math.Max(s.Size, 4)
			if cur != nil && (s.Box.X0-cur.Box.X1 > 2.5*size || startsCaption(s.Text)) {
				flush()
			}
			if cur == nil {
				cur = &Line{Page: p.Index, PageH: p.Height, Box: s.Box, Size: s.Size, Bold: s.Bold}
			} else {
				if needsSpace(cur.Text, s.Text, s.Box.X0-cur.Box.X1, size) {
					cur.Text += " "
				}
				cur.Box = cur.Box.Union(s.Box)
				cur.Size = math.Max(cur.Size, s.Size)
				cur.Bold = cur.Bold && s.Bold
			}
			cur.Text += s.Text
			cur.Spans = append(cur.Spans, s)
		}
		flush()
	}
	return out
}

// startsCaption reports whether a span begins a figure or table caption,
// which always starts a new line (captions side by side can be close).
func startsCaption(text string) bool {
	return captionRe.MatchString(text) || tableCaptionRe.MatchString(text)
}

// needsSpace decides whether two adjacent spans are separate words.
// Subscripts and split runs often touch, so a small gap means no space.
func needsSpace(prev, next string, gap, size float64) bool {
	if prev == "" || strings.HasSuffix(prev, " ") || strings.HasPrefix(next, " ") {
		return false
	}
	return gap > size*0.15
}

// pageText joins lines into plain text, one line per line.
func pageText(lines []Line) string {
	var b strings.Builder
	for i, l := range lines {
		if i > 0 {
			b.WriteByte('\n')
		}
		b.WriteString(l.Text)
	}
	return b.String()
}

// bodySize is the most common font size by character count, rounded to
// half points: the size of the running text.
func bodySize(doc *pdf.Document) float64 {
	counts := map[float64]int{}
	for _, p := range doc.Pages {
		for _, s := range p.Spans {
			counts[math.Round(s.Size*2)/2] += len(s.Text)
		}
	}
	best, n := 9.0, 0
	for sz, c := range counts {
		if c > n || (c == n && sz < best) {
			best, n = sz, c
		}
	}
	return best
}

// inMargin reports whether a box sits in the running header or footer
// band of a page (top 7% or bottom 6%).
func inMargin(b pdf.Rect, pageH float64) bool {
	return b.Y1 < pageH*0.07 || b.Y0 > pageH*0.94
}
