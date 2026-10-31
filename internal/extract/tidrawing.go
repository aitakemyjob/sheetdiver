package extract

import (
	"math"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/aitakemyjob/sheetdiver/internal/pdf"
)

// TI's package drawings. TI gives a package as drawings with no table: a
// package outline page and an example board layout page, titled with the
// drawing's code (DSG0008A: its digits the pin count) and "WSON - 0.8 mm
// max height". The numbers on them follow a pattern: a dimension's
// limits stacked (2.1 over 1.9), a count before the ones repeated per pin
// ("8X" over 0.32 and 0.18), a basic pitch after its count ("6X 0.5"), the
// exposed pad's sides with a tolerance (1.6±0.1), "MAX" on a height, "TYP"
// on a gull-wing's span; the board layout's in brackets ("10X (0.6)",
// "(2.3)"). Read by those, they give the outline and land pattern a
// footprint is drawn from.

// tiDim is a number on a drawing: a value or a pair of limits, with its
// count and marks.
type tiDim struct {
	min, max float64
	mult     int
	paren    bool // in brackets: reference (a board layout's sizes)
	typ      bool
	maxOnly  bool // "1.2 MAX"
	pm       bool // "1.6±0.1"
	x, y     float64
	box      pdf.Rect
	// across and down: the dimension's line runs across the page or
	// down it (beside the number), when that's seen.
	across, down bool
	used         bool
}

// nom is the middle of a number's limits.
func (d *tiDim) nom() float64 { return (d.min + d.max) / 2 }

var (
	tiNumRe   = regexp.MustCompile(`^(\d+)X$`)
	tiValueRe = regexp.MustCompile(`^(?:(\d+)X\s+)?(\()?(\d*\.\d+|\d+)(\))?(?:\s*±\s*(\d*\.\d+|\d+))?(?:\s+(TYP|MAX|MIN))?$`)
	// tiHeightRe is a title's height: "WSON - 0.8 mm max height".
	tiHeightRe = regexp.MustCompile(`(?i)-\s*([\d.]+)\s*(?:mm\s+)?max\s+height`)
	// tiCodeRe is a drawing code with its pin count: DSG0008A.
	tiCodeRe = regexp.MustCompile(`^[A-Z]{2,4}(\d{4})[A-Z]$`)
	// noLeadNameRe names a package with terminals under its body.
	noLeadNameRe = regexp.MustCompile(`(?i)SON|QFN|DFN|LGA`)
	// gullwingNameRe names a package with gull-wing leads.
	gullwingNameRe = regexp.MustCompile(`(?i)SOIC|SOP|SSOP|MSOP|SOT|^SO\b|SC-?\d`)
)

// tiDims reads the numbers on a drawing page: stacked limits joined, a
// count beside or above a number given to it, TYP beside a pair marking
// it.
func tiDims(rows []Row, graphics []pdf.Graphic) []*tiDim {
	type tok struct {
		text           string
		x0, x1, y0, y1 float64
	}
	var toks []tok
	for _, r := range rows {
		for _, s := range r.Spans {
			if s.Vertical {
				continue
			}
			toks = append(toks, tok{strings.Join(strings.Fields(s.Text), " "), s.Box.X0, s.Box.X1, s.Box.Y0, s.Box.Y1})
		}
	}
	// A tolerance whose ± sign was lost (a glyph of a symbol font):
	// "1.2" then "0.1" a sign's width along the line.
	for i := range toks {
		for j := range toks {
			a, b := toks[i], toks[j]
			if i == j || math.Abs((a.y0+a.y1)/2-(b.y0+b.y1)/2) > 2 || b.x0-a.x1 < 3 || b.x0-a.x1 > 12 {
				continue
			}
			va, errA := strconv.ParseFloat(a.text, 64)
			vb, errB := strconv.ParseFloat(b.text, 64)
			if errA == nil && errB == nil && vb <= 0.2 && va > vb {
				toks[i].text, toks[i].x1 = a.text+"±"+b.text, b.x1
				toks[j].text = ""
			}
		}
	}
	var out []*tiDim
	usedTok := make([]bool, len(toks))
	var counts, typs []int
	for i, t := range toks {
		switch {
		case tiNumRe.MatchString(t.text):
			counts = append(counts, i)
			continue
		case t.text == "TYP":
			typs = append(typs, i)
			continue
		}
		m := tiValueRe.FindStringSubmatch(t.text)
		if m == nil || usedTok[i] || strings.Count(m[3], ".") > 0 && len(m[3])-strings.Index(m[3], ".") > 3 {
			continue // not a number, or a scale's (15.000)
		}
		v, _ := strconv.ParseFloat(m[3], 64)
		d := &tiDim{min: v, max: v, paren: m[2] != "", x: (t.x0 + t.x1) / 2, y: (t.y0 + t.y1) / 2, box: pdf.Rect{X0: t.x0, Y0: t.y0, X1: t.x1, Y1: t.y1}}
		if m[1] != "" {
			d.mult, _ = strconv.Atoi(m[1])
		}
		if m[5] != "" {
			tol, _ := strconv.ParseFloat(m[5], 64)
			d.min, d.max, d.pm = v-tol, v+tol, true
		}
		switch m[6] {
		case "TYP":
			d.typ = true
		case "MAX":
			d.maxOnly = true
		case "MIN":
			continue // a solder mask's clearance
		}
		// The lower limit stacked under it.
		if !d.pm && !d.paren && d.mult == 0 && m[6] == "" {
			for j, u := range toks {
				if j == i || usedTok[j] || math.Abs((u.x0+u.x1)/2-d.x) > 4 || u.y0-t.y1 < -1 || u.y0-t.y1 > 6 {
					continue
				}
				// Millimetres have a point; a lead's angles (0° over 8°,
				// the signs lost) don't.
				if w, err := strconv.ParseFloat(u.text, 64); err == nil && strings.Contains(t.text+u.text, ".") {
					d.min, d.max = math.Min(v, w), math.Max(v, w)
					d.y = (t.y0 + u.y1) / 2
					d.box.Y1 = u.y1
					usedTok[j] = true
					break
				}
			}
		}
		usedTok[i] = true
		out = append(out, d)
	}
	// A count beside a pair ("20X" between 0.30 and 0.19) or above a
	// number ("2X" over 5.85) is theirs; TYP beside a pair marks it.
	for _, ci := range counts {
		c := toks[ci]
		n, _ := strconv.Atoi(strings.TrimSuffix(c.text, "X"))
		var best *tiDim
		bd := 30.0
		for _, d := range out {
			if d.mult != 0 {
				continue
			}
			cy := (c.y0 + c.y1) / 2
			beside := d.x > c.x1 && d.x-c.x1 < 25 && math.Abs(d.y-cy) < 8
			above := math.Abs(d.x-(c.x0+c.x1)/2) < 12 && d.y > c.y1 && d.y-c.y1 < 18
			if dist := math.Hypot(d.x-c.x1, d.y-cy); (beside || above) && dist < bd {
				best, bd = d, dist
			}
		}
		if best != nil {
			best.mult = n
		}
	}
	// Which way a dimension runs: its line across the page at the
	// number's height, or down it in line with the number, broken round
	// it.
	for _, d := range out {
		for _, g := range graphics {
			b := g.Box
			switch {
			case b.H() < 1.5 && b.W() > 8 && math.Abs((b.Y0+b.Y1)/2-d.y) < 4 && (math.Abs(b.X1-d.box.X0) < 12 || math.Abs(b.X0-d.box.X1) < 12):
				d.across = true
			case b.W() < 1.5 && b.H() > 8 && math.Abs((b.X0+b.X1)/2-d.x) < 4 && (math.Abs(b.Y1-d.box.Y0) < 12 || math.Abs(b.Y0-d.box.Y1) < 12):
				d.down = true
			}
		}
	}
	for _, ti := range typs {
		t := toks[ti]
		for _, d := range out {
			if d.x < t.x0 && t.x0-d.x < 25 && math.Abs(d.y-(t.y0+t.y1)/2) < 8 {
				d.typ = true
			}
		}
	}
	return out
}

// tiPage is what a page of a TI drawing gives: outline dimensions, or a
// land pattern, or nothing (the stencil).
func tiPage(rows []Row, graphics []pdf.Graphic, pageText, name, code string) (dims, land []dimRow) {
	m := tiCodeRe.FindStringSubmatch(code)
	if m == nil {
		return nil, nil
	}
	pins, _ := strconv.Atoi(m[1])
	if pins < 2 {
		return nil, nil
	}
	upper := strings.ToUpper(pageText)
	ds := tiDims(rows, graphics)
	switch {
	case strings.Contains(upper, "STENCIL") || strings.Contains(upper, "SOLDER PASTE"):
		return nil, nil
	case strings.Contains(upper, "BOARD LAYOUT") || strings.Contains(upper, "LAND PATTERN"):
		return nil, tiLand(ds, pins)
	}
	height := 0.0
	if h := tiHeightRe.FindStringSubmatch(pageText); h != nil {
		height, _ = strconv.ParseFloat(h[1], 64)
	}
	switch {
	case noLeadNameRe.MatchString(name):
		return tiNoLead(ds, pins, height), nil
	case gullwingNameRe.MatchString(name):
		return tiGullwing(ds, pins, height), nil
	}
	return nil, nil
}

// tiRow makes a row of a dimension, its values rounded to 0.1 µm.
func tiRow(symbol, key string, d *tiDim, note string) dimRow {
	d.used = true
	pd := PackageDim{Symbol: symbol, Key: key, Unit: "mm", Note: note}
	r := func(v float64) float64 { return math.Round(v*1e4) / 1e4 }
	lo, hi, nom := r(d.min), r(d.max), r(d.nom())
	switch {
	case d.maxOnly:
		pd.Max = &hi
	case lo == hi:
		pd.Nom = &nom
	default:
		pd.Min, pd.Max = &lo, &hi
		if d.pm {
			pd.Nom = &nom
		}
	}
	return dimRow{dim: pd}
}

// pick is the first unused number a test accepts, in order.
func pick(ds []*tiDim, ok func(*tiDim) bool) *tiDim {
	for _, d := range ds {
		if !d.used && ok(d) {
			return d
		}
	}
	return nil
}

// common are the outline's height, standoff and pitch, and the
// terminals' pairs (given per pin).
func tiCommon(ds []*tiDim, pins int, height float64) (out []dimRow, pitch float64, terms []*tiDim) {
	if a := pick(ds, func(d *tiDim) bool {
		return height > 0 && math.Abs(d.max-height) < 0.005 && !d.paren && d.mult == 0 && (d.maxOnly || d.min > 0.1)
	}); a != nil {
		out = append(out, tiRow("A", "height", a, ""))
	}
	if a1 := pick(ds, func(d *tiDim) bool {
		return !d.paren && d.mult == 0 && d.min < d.max && d.min <= 0.1 && d.max <= 0.25 && !d.pm
	}); a1 != nil {
		out = append(out, tiRow("A1", "standoff", a1, ""))
	}
	if e := pick(ds, func(d *tiDim) bool {
		return !d.paren && d.mult > 1 && d.mult < pins && d.min == d.max && d.max <= 1.27
	}); e != nil {
		pitch = e.max
		out = append(out, tiRow("e", "pitch", e, "BSC"))
	}
	for _, d := range ds {
		if !d.used && !d.paren && d.mult == pins && d.min < d.max {
			terms = append(terms, d)
		}
	}
	return out, pitch, terms
}

// tiNoLead reads a no-lead outline (WSON, SON, QFN): the body's sides
// (the largest pairs), the exposed pad's (with a tolerance), the
// terminals' width and length (given per pin: the narrower the width).
func tiNoLead(ds []*tiDim, pins int, height float64) []dimRow {
	out, pitch, terms := tiCommon(ds, pins, height)
	sort.SliceStable(terms, func(i, j int) bool { return terms[i].nom() < terms[j].nom() })
	if len(terms) >= 2 {
		out = append(out, tiRow("b", "leadWidth", terms[0], ""), tiRow("L", "leadLength", terms[1], ""))
	}
	var body []*tiDim
	for _, d := range ds {
		if !d.used && !d.paren && !d.pm && d.mult == 0 && d.min < d.max && d.nom() >= 1 {
			body = append(body, d)
		}
	}
	sort.SliceStable(body, func(i, j int) bool { return body[i].nom() > body[j].nom() })
	if len(body) >= 2 {
		// D runs along the rows (drawn down the page): the side drawn
		// so, else the side the pins need, else the longer.
		dI, eI := 0, 1
		switch {
		case body[1].down && !body[0].down || body[0].across && !body[1].across:
			dI, eI = 1, 0
		case body[0].down || body[1].across:
		default:
			if need := float64(pins/2) * pitch; pitch > 0 && body[0].nom() < need && body[1].nom() >= need {
				dI, eI = 1, 0
			}
		}
		out = append(out, tiRow("D", "bodyLength", body[dI], ""), tiRow("E", "bodyWidth", body[eI], ""))
	} else if len(body) == 1 {
		out = append(out, tiRow("D", "bodyLength", body[0], ""))
		out = append(out, tiRow("E", "bodyWidth", &tiDim{min: body[0].min, max: body[0].max}, ""))
	}
	var pad []*tiDim
	for _, d := range ds {
		if !d.used && d.pm && !d.paren {
			pad = append(pad, d)
		}
	}
	if len(pad) >= 2 {
		// D2 along the rows: drawn down the page, else the longer.
		sort.SliceStable(pad, func(i, j int) bool { return pad[i].nom() > pad[j].nom() })
		if pad[1].down && !pad[0].down || pad[0].across && !pad[1].across {
			pad[0], pad[1] = pad[1], pad[0]
		}
		out = append(out, tiRow("D2", "padLength", pad[0], ""), tiRow("E2", "padWidth", pad[1], ""))
	}
	return out
}

// tiGullwing reads a gull-wing outline (TSSOP, SOIC, SOT): the span tip
// to tip (marked TYP), the body's length (the pins' side) and width, the
// lead's width (given per pin) and foot length.
func tiGullwing(ds []*tiDim, pins int, height float64) []dimRow {
	out, pitch, terms := tiCommon(ds, pins, height)
	if len(terms) >= 1 {
		out = append(out, tiRow("b", "leadWidth", terms[0], ""))
	}
	var body []*tiDim
	for _, d := range ds {
		if !d.used && !d.paren && !d.pm && d.mult == 0 && d.min < d.max && d.nom() >= 1 {
			body = append(body, d)
		}
	}
	sort.SliceStable(body, func(i, j int) bool { return body[i].nom() > body[j].nom() })
	// TI draws the leads left and right: the span and the body's width
	// run across the page, its length down. Without the lines, the span
	// is marked TYP (else the largest) and the length the side the pins
	// need.
	var span *tiDim
	for _, d := range body {
		if d.typ {
			span = d
		}
	}
	if span == nil {
		span = pick(body, func(d *tiDim) bool { return d.across })
	}
	if span == nil && len(body) > 0 {
		span = body[0]
	}
	if span != nil {
		out = append(out, tiRow("E", "span", span, ""))
		need := float64(pins/2-1) * pitch
		d := pick(body, func(d *tiDim) bool { return d.down })
		if d == nil {
			d = pick(body, func(d *tiDim) bool { return d.nom() >= need && !d.across })
		}
		if d == nil {
			d = pick(body, func(d *tiDim) bool { return d.nom() >= need })
		}
		if d != nil {
			out = append(out, tiRow("D", "bodyLength", d, ""))
		}
		// The body's width: narrower than the span, drawn across like it
		// and under it (not a detail's), the nearest such.
		var e1 *tiDim
		for _, c := range body {
			if c.used || c.nom() >= span.nom() || c.down && !c.across {
				continue
			}
			if e1 == nil || c.across && !e1.across || c.across == e1.across && math.Abs(c.x-span.x) < math.Abs(e1.x-span.x) {
				e1 = c
			}
		}
		if e1 != nil {
			out = append(out, tiRow("E1", "bodyWidth", e1, ""))
		}
	}
	if l := pick(ds, func(d *tiDim) bool {
		return !d.paren && d.mult == 0 && d.min < d.max && d.min >= 0.2 && d.max <= 1.3 && !d.pm
	}); l != nil {
		out = append(out, tiRow("L", "leadLength", l, ""))
	}
	return out
}

// tiLand reads a board layout: the pads (given per pin: the longer side
// along the row's span), the pitch, the pads' rows' span (the largest
// other bracketed size) and the thermal pad's sides.
func tiLand(ds []*tiDim, pins int) []dimRow {
	var out []dimRow
	var pad []*tiDim
	for _, d := range ds {
		if d.paren && d.mult == pins {
			pad = append(pad, d)
		}
	}
	if len(pad) < 2 {
		return nil
	}
	sort.SliceStable(pad, func(i, j int) bool { return pad[i].nom() > pad[j].nom() })
	out = append(out, tiRow("Y", "padLength", pad[0], "REF"), tiRow("X", "padWidth", pad[1], "REF"))
	if e := pick(ds, func(d *tiDim) bool { return d.paren && d.mult > 1 && d.mult < pins && d.max <= 1.27 }); e != nil {
		out = append(out, tiRow("e", "pitch", e, "REF"))
	}
	var rest []*tiDim
	for _, d := range ds {
		if !d.used && d.paren && d.mult == 0 && !d.typ {
			rest = append(rest, d)
		}
	}
	sort.SliceStable(rest, func(i, j int) bool { return rest[i].nom() > rest[j].nom() })
	if len(rest) > 0 {
		out = append(out, tiRow("C", "rowSpan", rest[0], "REF"))
	}
	return out
}
