// Package preview writes a cad.Drawing as a one-page PDF, which the app's
// PDF engine renders to the picture an agent looks at.
package preview

import (
	"bytes"
	"fmt"
	"math"
	"strings"

	"github.com/aitakemyjob/sheetdiver/internal/cad"
)

// colors are each role's stroke and fill (RGB 0–1) on white.
var colors = map[string][3]float64{
	cad.RoleCopper:    {0.80, 0.33, 0.20},
	cad.RolePaste:     {0.55, 0.55, 0.60},
	cad.RoleDrill:     {1, 1, 1},
	cad.RoleSilk:      {0.15, 0.55, 0.85},
	cad.RoleFab:       {0.45, 0.45, 0.45},
	cad.RoleCourtyard: {0.85, 0.20, 0.75},
	cad.RoleBody:      {0.55, 0.10, 0.10},
	cad.RolePin:       {0.55, 0.10, 0.10},
	cad.RolePinName:   {0.10, 0.35, 0.35},
	cad.RolePinNumber: {0.55, 0.10, 0.10},
	cad.RoleText:      {0.12, 0.12, 0.12},
	cad.RoleDim:       {0.10, 0.45, 0.20},
}

// bodyFill is a symbol body's fill.
var bodyFill = [3]float64{1, 1, 0.84}

// PDF draws a drawing on a page widthPt points wide (its height follows).
func PDF(d cad.Drawing, title string, widthPt float64) []byte {
	v := d.View
	s := widthPt / math.Max(v.X1-v.X0, 1e-3)
	if s > 60 {
		s = 60 // don't blow a tiny part up past 60 pt/mm
	}
	head := 0.0
	if title != "" {
		head = 22
	}
	w, h := (v.X1-v.X0)*s, (v.Y1-v.Y0)*s+head
	px := func(p cad.Pt) (float64, float64) { return (p.X - v.X0) * s, (v.Y1 - p.Y) * s }
	var c bytes.Buffer
	c.WriteString("1 1 1 rg 0 0 " + f(w) + " " + f(h) + " re f\n1 J 1 j\n")
	for _, it := range d.Items {
		col := colors[it.Role]
		switch it.Kind {
		case cad.ShapeLine:
			dash := ""
			if it.Role == cad.RoleCourtyard {
				dash = f(0.25*s) + " " + f(0.15*s)
			}
			// A line, or a polyline (an arc drawn as one).
			fmt.Fprintf(&c, "%s RG %s w [%s] 0 d %sS\n", rgb(col), f(math.Max(it.Width*s, 0.5)), dash, path(it.Pts, px))
		case cad.ShapeRect:
			x0, y0 := px(it.Pts[0])
			x1, y1 := px(it.Pts[1])
			path := roundRect(math.Min(x0, x1), math.Min(y0, y1), math.Abs(x1-x0), math.Abs(y1-y0), it.R*s)
			if it.Role == cad.RoleBody {
				fmt.Fprintf(&c, "[] 0 d %s rg %s RG %s w %s B\n", rgb(bodyFill), rgb(col), f(math.Max(it.Width*s, 0.8)), path)
			} else {
				fmt.Fprintf(&c, "%s rg %s f\n", rgb(col), path)
			}
		case cad.ShapePoly:
			var b strings.Builder
			b.WriteString(path(it.Pts, px))
			if it.Color != "" {
				var r, g, bl int
				_, _ = fmt.Sscanf(it.Color, "#%02x%02x%02x", &r, &g, &bl)
				fc := [3]float64{float64(r) / 255, float64(g) / 255, float64(bl) / 255}
				fmt.Fprintf(&c, "%s rg %s RG 0.3 w [] 0 d %sh B\n", rgb(fc), rgb(fc), b.String())
			} else if it.Width > 0 && !it.Fill {
				// A drawn, open shape: its outline only.
				fmt.Fprintf(&c, "%s RG %s w [] 0 d %sh S\n", rgb(col), f(math.Max(it.Width*s, 0.5)), b.String())
			} else {
				fmt.Fprintf(&c, "%s rg %sh f\n", rgb(col), b.String())
			}
		case cad.ShapeCircle:
			x, y := px(it.Pts[0])
			if it.Width > 0 && !it.Fill && it.Role != cad.RoleCopper && it.Role != cad.RoleDrill {
				fmt.Fprintf(&c, "%s RG %s w [] 0 d %s S\n", rgb(col), f(math.Max(it.Width*s, 0.5)), circle(x, y, it.R*s))
				continue
			}
			fmt.Fprintf(&c, "%s rg %s f\n", rgb(col), circle(x, y, it.R*s))
		case cad.ShapeText:
			x, y := px(it.Pts[0])
			size := it.Size * s
			tw := textWidth(it.Text) * size
			shift := map[string]float64{"middle": tw / 2, "end": tw}[it.Anchor]
			if it.Vertical {
				fmt.Fprintf(&c, "BT %s rg /F1 %s Tf 0 1 -1 0 %s %s Tm (%s) Tj ET\n", rgb(col), f(size), f(x), f(y-shift), pdfText(it.Text))
			} else {
				fmt.Fprintf(&c, "BT %s rg /F1 %s Tf %s %s Td (%s) Tj ET\n", rgb(col), f(size), f(x-shift), f(y), pdfText(it.Text))
			}
		}
	}
	if title != "" {
		fmt.Fprintf(&c, "BT 0.1 0.1 0.1 rg /F1 12 Tf 6 %s Td (%s) Tj ET\n", f(h-15), pdfText(title))
	}
	return page(w, h, c.Bytes())
}

// path is points as a PDF path: moved to the first, lines to the rest.
func path(pts []cad.Pt, px func(cad.Pt) (float64, float64)) string {
	var b strings.Builder
	for i, p := range pts {
		x, y := px(p)
		op := "l"
		if i == 0 {
			op = "m"
		}
		fmt.Fprintf(&b, "%s %s %s ", f(x), f(y), op)
	}
	return b.String()
}

// page wraps a content stream as a one-page PDF with Helvetica.
func page(w, h float64, content []byte) []byte {
	objs := []string{
		"<< /Type /Catalog /Pages 2 0 R >>",
		"<< /Type /Pages /Kids [3 0 R] /Count 1 >>",
		fmt.Sprintf("<< /Type /Page /Parent 2 0 R /MediaBox [0 0 %s %s] /Contents 4 0 R /Resources << /Font << /F1 5 0 R >> >> >>", f(w), f(h)),
		fmt.Sprintf("<< /Length %d >>\nstream\n%s\nendstream", len(content), content),
		"<< /Type /Font /Subtype /Type1 /BaseFont /Helvetica /Encoding /WinAnsiEncoding >>",
	}
	var b bytes.Buffer
	b.WriteString("%PDF-1.4\n")
	offsets := make([]int, len(objs))
	for i, o := range objs {
		offsets[i] = b.Len()
		fmt.Fprintf(&b, "%d 0 obj\n%s\nendobj\n", i+1, o)
	}
	xref := b.Len()
	fmt.Fprintf(&b, "xref\n0 %d\n0000000000 65535 f \n", len(objs)+1)
	for _, o := range offsets {
		fmt.Fprintf(&b, "%010d 00000 n \n", o)
	}
	fmt.Fprintf(&b, "trailer\n<< /Size %d /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", len(objs)+1, xref)
	return b.Bytes()
}

// textWidth is a string's width in Helvetica, in ems: capitals and most
// lower case about two thirds, digits and underscores just over half,
// narrow letters and punctuation less.
func textWidth(s string) float64 {
	w := 0.0
	for _, r := range s {
		switch {
		case r == 'i' || r == 'j' || r == 'l' || r == 'I' || r == '.' || r == ',' || r == ':' || r == '/' || r == '\'' || r == ' ':
			w += 0.28
		case r == 'f' || r == 't' || r == 'r' || r == '-' || r == '(' || r == ')' || r == '[' || r == ']':
			w += 0.35
		case r == 'm' || r == 'w' || r == 'M' || r == 'W':
			w += 0.85
		case r >= '0' && r <= '9' || r == '_' || r == 'J' || r == 'L' || r == 'E' || r == 'F' || r == 'T' || r == 'Z':
			w += 0.58
		case r >= 'A' && r <= 'Z':
			w += 0.7
		default:
			w += 0.56
		}
	}
	return w
}

// f writes a number for PDF.
func f(v float64) string {
	return strings.TrimRight(strings.TrimRight(fmt.Sprintf("%.3f", v), "0"), ".")
}

// rgb writes a colour.
func rgb(c [3]float64) string { return f(c[0]) + " " + f(c[1]) + " " + f(c[2]) }

// k is the Bézier handle length for a quarter circle.
const k = 0.5523

// roundRect is a rectangle path with rounded corners.
func roundRect(x, y, w, h, r float64) string {
	r = math.Min(r, math.Min(w, h)/2)
	if r <= 0.01 {
		return fmt.Sprintf("%s %s %s %s re", f(x), f(y), f(w), f(h))
	}
	x1, y1 := x+w, y+h
	return fmt.Sprintf("%s %s m %s %s l %s %s %s %s %s %s c %s %s l %s %s %s %s %s %s c %s %s l %s %s %s %s %s %s c %s %s l %s %s %s %s %s %s c h",
		f(x+r), f(y), f(x1-r), f(y),
		f(x1-r+k*r), f(y), f(x1), f(y+r-k*r), f(x1), f(y+r),
		f(x1), f(y1-r),
		f(x1), f(y1-r+k*r), f(x1-r+k*r), f(y1), f(x1-r), f(y1),
		f(x+r), f(y1),
		f(x+r-k*r), f(y1), f(x), f(y1-r+k*r), f(x), f(y1-r),
		f(x), f(y+r),
		f(x), f(y+r-k*r), f(x+r-k*r), f(y), f(x+r), f(y))
}

// circle is a circle path.
func circle(x, y, r float64) string {
	return roundRect(x-r, y-r, 2*r, 2*r, r)
}

// pdfText escapes a string for a PDF literal in WinAnsi: Latin-1 as is,
// anything else as ?.
func pdfText(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch {
		case r == '(' || r == ')' || r == '\\':
			b.WriteByte('\\')
			b.WriteRune(r)
		case r == '·':
			b.WriteByte(0xB7)
		case r < 128:
			b.WriteRune(r)
		case r < 256:
			b.WriteByte(byte(r))
		case r == '–' || r == '—':
			b.WriteByte('-')
		default:
			b.WriteByte('?')
		}
	}
	return b.String()
}
