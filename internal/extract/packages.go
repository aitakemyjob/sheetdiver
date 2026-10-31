package extract

import (
	"fmt"
	"math"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/aitakemyjob/sheetdiver/internal/pdf"
)

// Package dimensions. Datasheets end with each package's outline drawing
// and, often, a recommended land pattern. Where they print a dimension
// table (symbol against min/nom/max, in millimetres and often inches),
// it is read column by column: the table sits beside the drawing, whose
// labels share its rows, so only the spans under the table's own columns
// count. Where the numbers are only on the drawing (TI's newer outlines),
// the dimension-like texts in it are offered as candidates for an agent
// to read against the drawing's image, its max-over-min pairs joined.

// PackageDim is one dimension: its symbol as printed (A, A1, b, D, E1, e,
// L…), what it measures (Key: height, standoff, leadWidth…), and its
// values in mm, with the unit the datasheet printed them in.
type PackageDim struct {
	Symbol      string   `json:"symbol"`
	Description string   `json:"description,omitempty"`
	Key         string   `json:"key,omitempty"`
	Min         *float64 `json:"min,omitempty"`
	Nom         *float64 `json:"nom,omitempty"`
	Max         *float64 `json:"max,omitempty"`
	// Unit is the unit printed (mm, inch or mil); values are in mm.
	Unit string `json:"unit"`
	// Note is a qualifier printed with the value: BSC, REF, TYP.
	Note string `json:"note,omitempty"`
}

// Package is a package's dimensions and land pattern as a datasheet
// gives them.
type Package struct {
	// Name is the package's name (SOT-23, SO-8, WSON); Code the drawing's
	// own code (TI's DDC0006A); Reference its JEDEC outline (MO-193).
	Name      string       `json:"name"`
	Code      string       `json:"code,omitempty"`
	Reference string       `json:"reference,omitempty"`
	Page      int          `json:"page"`
	Box       pdf.Rect     `json:"box"`
	Dims      []PackageDim `json:"dims,omitempty"`
	Land      []PackageDim `json:"land,omitempty"`
	// Candidates are dimension-like texts in the drawing ("2.55–3.05",
	// "6X (1.1)", "0.65 TYP"), for reading the drawing when there is no
	// table.
	Candidates []string `json:"candidates,omitempty"`
	// Checks are what doesn't add up (mm and inch columns disagreeing).
	Checks []string `json:"checks,omitempty"`
	// Parts are the datasheet's parts that come in it, by its ordering
	// scheme; none when it doesn't say (every part's).
	Parts []string `json:"parts,omitempty"`
}

var (
	// limitWords are the column heads of a dimension table's values.
	limitWords = map[string]string{"MIN": "min", "MIN.": "min", "NOM": "nom", "NOM.": "nom", "TYP": "nom", "TYP.": "nom", "MAX": "max", "MAX.": "max", "VALUE": "nom"}
	// symbolHeadRe is the head of a table's symbol column.
	symbolHeadRe = regexp.MustCompile(`(?i)^(dim\.?|symbol|symbols|dimension limits|dimensions|ref\.?|item)$`)
	// dimSymbolRe is a dimension symbol: one or two letters and a digit.
	dimSymbolRe = regexp.MustCompile(`^(?:[A-Za-z]{1,2}\d?|[θΘ]\d?)$`)
	// placesRe is Microchip's "(3 PLACES)" or a "(X3)" count after a symbol.
	placesRe = regexp.MustCompile(`(?i)\s*\((\d+\s*places|x\d+)\)`)
	// symbolNoteRe is a footnote mark after a symbol: A1 (12), b(9)(11).
	symbolNoteRe = regexp.MustCompile(`\s*(\(\d{1,2}\))+`)
	// valueRe is a value cell: a number, a qualifier, an inch value in
	// brackets after it.
	valueRe = regexp.MustCompile(`^\(?(-?\d*\.?\d+)\)?\s*(?:\((BSC|REF|TYP)\)|(BSC|REF|TYP))?\s*(?:\[[\d.]+\])?$`)
	// unitHeadRe finds a column group's unit.
	unitHeadRe = regexp.MustCompile(`(?i)^(millimet(er|re)s?|mm|inch(es)?|in\.?|mils?)$`)
	// pageUnitRe finds a page's stated unit.
	pageUnitRe = regexp.MustCompile(`(?i)(all )?dimensions (are )?(in|shown in) (mm|millimet|inch)|in mm\b|\(in mm\)|value \(in mm\)`)
	// packageWordRe finds a package page's headings.
	packageWordRe = regexp.MustCompile(`(?i)package (outline|dimensions|information|drawing)|mechanical (data|case outline|drawing)|land pattern|pad layout|board layout|recommended footprint`)
	// landHeadRe marks a land pattern's heading.
	landHeadRe = regexp.MustCompile(`(?i)land pattern|pad layout|board layout|footprint|recommended (pad|land|footprint)|solder ?pad`)
	// outlineHeadRe marks an outline's heading.
	outlineHeadRe = regexp.MustCompile(`(?i)package (outline|dimensions)|outline dimensions|case outline|mechanical`)
	// packageNameRe finds a package's name.
	packageNameRe = regexp.MustCompile(`(?i)\b((?:T|V|W|X)?SOT-?\d{2,3}(?:-\d{1,2})?|SC-?\d{2,3}(?:-\d)?|SO-?\d{1,2}|SOIC-?\d{1,2}|E?TSSOP-?\d{1,2}|V?MSOP-?\d{1,2}|VSSOP-?\d{1,2}|[UVWX]?[QD]FN-?\d{1,2}|UFQFPN-?\d{2}|[LTMV]?QFP-?\d{2,3}|WLCSP-?\d*|W?SON-?\d{1,2}|WSON|VSON|DSBGA-?\d*|(?:UF|TF|LF|F|W)?BGA-?\d*|TO-?\d{2,3}(?:-\d)?|SOD-?\d{3}F?|P?DIP-?\d{1,2}|SOT-?223|SOT-?89|DFN\d+X\d+-\d+)\b`)
	// heightNameRe is TI's "WSON - 0.8 mm max height" title.
	heightNameRe = regexp.MustCompile(`(?i)^\s*([A-Z][A-Z0-9-]*(?:\s?[A-Z0-9-]+)?)\s+-\s+[\d.]+\s*(mm\s+)?max height`)
	// drawingPageRe heads the pages of TI's package drawings.
	drawingPageRe = regexp.MustCompile(`(?i:package outline|board layout|stencil design)|MECHANICAL DATA`)
	// drawingCodeRe is TI's drawing code (DDC0006A, DSK0010A, PW0020A).
	drawingCodeRe = regexp.MustCompile(`^[A-Z]{2,4}\d{4}[A-Z]$`)
	// jedecRe finds a JEDEC outline reference.
	jedecRe = regexp.MustCompile(`(?i)\b(?:jedec|outline conforms to)(?:\s+(?:outline|registration|reference))?\s+([A-Z]{2,3}-?\d{3}[A-Z]?(?:\s?[A-Z]{2})?)`)
	// electricalRe marks a row of an electrical table.
	electricalRe = regexp.MustCompile(`(?i)voltage|current|resistance|capacitance|temperature|charge|V =|°C`)
	// candidateRe is a dimension-like text on a drawing.
	candidateRe = regexp.MustCompile(`^(?:\d+X\s+)?(?:\d*\.\d+|\(\d*\.\d+\))(?:\s*(?:±|\+/-)\s*\d*\.\d+)?(?:\s+(?:MAX|MIN|TYP|BSC|REF))?(?:\s+TYP)?$`)
)

// dimKeys are the keys of outline symbols, as JEDEC uses them.
var dimKeys = map[string]string{
	"A": "height", "A1": "standoff", "A2": "bodyThickness", "b": "leadWidth", "c": "leadThickness",
	"D": "bodyLength", "E": "span", "E1": "bodyWidth", "e": "pitch", "L": "leadLength",
	"D2": "padLength", "E2": "padWidth", "N": "pins",
}

// landKeys are the keys of land pattern symbols.
var landKeys = map[string]string{"C": "rowSpan", "C1": "rowSpan", "C2": "pitch", "X": "padWidth", "Y": "padLength", "G": "gap", "Z": "outerSpan", "E": "pitch", "Y1": "outerSpan", "X1": "outerWidth"}

// descKeys map words in a dimension's description to its key.
var descKeys = []struct {
	re  *regexp.Regexp
	key string
}{
	{regexp.MustCompile(`(?i)number of (pins|leads)`), "pins"},
	{regexp.MustCompile(`(?i)pitch`), "pitch"},
	{regexp.MustCompile(`(?i)overall height`), "height"},
	{regexp.MustCompile(`(?i)standoff`), "standoff"},
	{regexp.MustCompile(`(?i)overall width`), "span"},
	{regexp.MustCompile(`(?i)(molded )?package width`), "bodyWidth"},
	{regexp.MustCompile(`(?i)overall length`), "bodyLength"},
	{regexp.MustCompile(`(?i)foot length|terminal length|contact length`), "leadLength"},
	{regexp.MustCompile(`(?i)(lead|terminal|contact) width`), "leadWidth"},
	{regexp.MustCompile(`(?i)lead thickness`), "leadThickness"},
	{regexp.MustCompile(`(?i)exposed pad width`), "padWidth"},
	{regexp.MustCompile(`(?i)exposed pad length`), "padLength"},
	{regexp.MustCompile(`(?i)pad spacing`), "rowSpan"},
	{regexp.MustCompile(`(?i)(contact )?pad width`), "padWidth"},
	{regexp.MustCompile(`(?i)(contact )?pad length`), "padLength"},
}

// limitColumn is a value column of a dimension table.
type limitColumn struct {
	x     float64 // its centre
	limit string  // min, nom or max
	unit  string  // mm, inch or mil
}

// packagePages are the pages worth reading for packages: those in a
// package section, and any whose text says it is a package drawing.
func packagePages(doc *pdf.Document, secs []Section) []int {
	pages := map[int]bool{}
	for _, s := range secs {
		if s.Kind == KindPackage {
			for p := s.Page; p <= s.EndPage && p < len(doc.Pages); p++ {
				pages[p] = true
			}
		}
	}
	for i, p := range doc.Pages {
		var b strings.Builder
		for _, s := range p.Spans {
			b.WriteString(s.Text + " ")
		}
		if packageWordRe.MatchString(b.String()) {
			pages[i] = true
		}
	}
	var out []int
	for p := range pages {
		out = append(out, p)
	}
	sort.Ints(out)
	return out
}

// Packages reads the package dimension and land pattern tables, and the
// candidates on drawings without one.
func Packages(doc *pdf.Document, secs []Section) []Package {
	var out []Package
	for _, pi := range packagePages(doc, secs) {
		p := doc.Pages[pi]
		rows := Rows(p.Spans)
		var text strings.Builder
		for _, r := range rows {
			text.WriteString(rowText(r) + "\n")
		}
		pageText := text.String()
		pageUnit := ""
		if m := pageUnitRe.FindString(pageText); m != "" {
			pageUnit = "mm"
			if strings.Contains(strings.ToLower(m), "inch") {
				pageUnit = "inch"
			}
		}
		code, name := "", ""
		// A drawing's code heads its pages (a part number looks alike).
		drawingPage := drawingPageRe.MatchString(pageText)
		for _, r := range rows {
			for _, s := range r.Spans {
				if drawingPage && drawingCodeRe.MatchString(strings.TrimSpace(s.Text)) && code == "" {
					code = strings.TrimSpace(s.Text)
				}
				if m := heightNameRe.FindStringSubmatch(s.Text); m != nil && name == "" {
					name = strings.TrimSpace(m[1])
				}
			}
		}
		ref := ""
		if m := jedecRe.FindStringSubmatch(pageText); m != nil {
			ref = strings.ToUpper(strings.TrimSpace(m[1]))
		}
		found := false
		for i := 0; i < len(rows); i++ {
			cols, symX, ok := limitHeader(rows, i, pageUnit)
			if !ok {
				continue
			}
			dims, end, box := readDims(rows, i, cols, symX)
			if len(dims) < 2 {
				continue
			}
			found = true
			pk := Package{Name: firstNonBlank(nearestName(rows, i), name, pageName(pageText, ref), code), Code: code, Reference: ref, Page: pi, Box: box}
			if isLand(rows, i) {
				pk.Land = keyed(dims, landKeys, true, pk.Name)
			} else {
				pk.Dims = keyed(dims, dimKeys, jedecStyle(dims), pk.Name)
			}
			pk.Checks = unitChecks(dims)
			out = append(out, pk)
			i = end
		}
		if !found && (code != "" || name != "") {
			// A drawing without a table: its numbers, for an agent, and
			// what they say when it's one of TI's.
			pk := Package{Name: firstNonBlank(name, pageName(pageText, ref), code), Code: code, Reference: ref, Page: pi, Candidates: candidates(rows)}
			dims, land := tiPage(rows, p.Graphics, pageText, pk.Name, code)
			for _, d := range dims {
				pk.Dims = append(pk.Dims, d.dim)
			}
			for _, d := range land {
				pk.Land = append(pk.Land, d.dim)
			}
			if len(pk.Candidates)+len(pk.Dims)+len(pk.Land) > 0 {
				out = append(out, pk)
			}
		}
	}
	return mergePackages(out)
}

// textSpans are a row's spans less the sideways ones.
func textSpans(r Row) []pdf.Span {
	out := make([]pdf.Span, 0, len(r.Spans))
	for _, s := range r.Spans {
		if !s.Vertical {
			out = append(out, s)
		}
	}
	return out
}

// textBox is the box round a row's spans less the sideways ones.
func textBox(r Row) pdf.Rect {
	var box pdf.Rect
	for i, s := range textSpans(r) {
		if i == 0 {
			box = s.Box
		} else {
			box = box.Union(s.Box)
		}
	}
	if box.Empty() {
		return r.Box
	}
	return box
}

// namedPackage is the package a datasheet names on its first page
// without drawing it ("Industry standard outline SOT-23 surface mount
// package"): by name alone, so a standard outline's footprint can stand
// in for the drawing it lacks.
func namedPackage(text string) (Package, bool) {
	m := packageNameRe.FindString(text)
	if m == "" {
		return Package{}, false
	}
	name := commonName(m)
	return Package{Name: name, Checks: []string{fmt.Sprintf("named on the first page (%q) but not drawn: the footprint is the standard %s outline's", m, name)}}, true
}

// firstNonBlank is the first non-blank string.
func firstNonBlank(ss ...string) string {
	for _, s := range ss {
		if strings.TrimSpace(s) != "" {
			return strings.TrimSpace(s)
		}
	}
	return ""
}

// limitHeader reads a dimension table's header at row i: two or more
// MIN/NOM/MAX (or a VALUE) heads, the symbol column left of them, and
// the unit each column is in (from MILLIMETERS / INCHES above them).
func limitHeader(rows []Row, i int, pageUnit string) ([]limitColumn, float64, bool) {
	var cols []limitColumn
	values := false
	for _, s := range rows[i].Spans {
		t := strings.ToUpper(strings.TrimSpace(s.Text))
		if strings.Contains(t, "VALUE") {
			t = "VALUE"
			values = true
		}
		if l, ok := limitWords[t]; ok {
			cols = append(cols, limitColumn{x: (s.Box.X0 + s.Box.X1) / 2, limit: l})
		}
	}
	if len(cols) < 2 && !values {
		return nil, 0, false
	}
	// The symbol column: a "Dim"/"Symbol" head on this row or the two
	// above, left of the values.
	symX := -1.0
	for k := i; k >= 0 && k >= i-3; k-- {
		if rows[i].Box.Y0-rows[k].Box.Y1 > 30 {
			break
		}
		for _, s := range rows[k].Spans {
			if s.Box.X1 <= cols[0].x && symbolHeadRe.MatchString(strings.TrimSpace(s.Text)) {
				symX = s.Box.X0
			}
		}
	}
	if symX < 0 {
		return nil, 0, false
	}
	// Units: a column belongs to the group head closest above it.
	type group struct {
		x0, x1 float64
		unit   string
	}
	var groups []group
	for k := i; k >= 0 && k >= i-3; k-- {
		if rows[i].Box.Y0-rows[k].Box.Y1 > 30 {
			break
		}
		for _, s := range rows[k].Spans {
			for _, w := range strings.Fields(s.Text) {
				if m := unitHeadRe.FindString(strings.Trim(w, "()")); m != "" {
					u := "mm"
					switch {
					case strings.HasPrefix(strings.ToLower(m), "in"):
						u = "inch"
					case strings.HasPrefix(strings.ToLower(m), "mil") && !strings.HasPrefix(strings.ToLower(m), "milli"):
						u = "mil"
					}
					groups = append(groups, group{s.Box.X0, s.Box.X1, u})
				}
			}
		}
	}
	for c := range cols {
		cols[c].unit = pageUnit
		best := math.Inf(1)
		for _, g := range groups {
			if d := math.Abs((g.x0+g.x1)/2 - cols[c].x); d < best {
				best, cols[c].unit = d, g.unit
			}
		}
		if cols[c].unit == "" {
			cols[c].unit = "mm"
		}
	}
	return cols, symX, true
}

// rawDim is a table row as read: symbol, description and the value in
// each column.
type rawDim struct {
	symbol, desc string
	values       map[int]float64
	notes        map[int]string
}

// readDims reads the rows under a header until the table ends: spans
// under the symbol column name the row, spans under a value column are
// its values; everything else on the row is the drawing's.
func readDims(rows []Row, header int, cols []limitColumn, symX float64) ([]dimRow, int, pdf.Rect) {
	right := cols[len(cols)-1].x + 30
	box := rows[header].Box
	var out []dimRow
	misses := 0
	last := header
	for k := header + 1; k < len(rows) && misses < 3; k++ {
		r := rows[k]
		if r.Box.Y0-rows[last].Box.Y1 > 40 {
			break
		}
		// A row's parts set a little off its line (ST's "-" for no value,
		// a symbol beside values with a footnote) come as rows of their
		// own overlapping it: they're the same row. A sideways label of
		// the drawing (a tall vertical span) isn't a row's part and
		// mustn't drag the rows under it in: the row's own bottom, less
		// such spans, is the limit.
		r = Row{Y: r.Y, Box: textBox(r), Spans: textSpans(r)}
		bottom := r.Box.Y1
		for k+1 < len(rows) && rows[k+1].Box.Y0 < bottom-1 {
			k++
			next := textSpans(rows[k])
			if len(next) == 0 {
				continue
			}
			r = Row{Y: r.Y, Box: r.Box.Union(textBox(rows[k])), Spans: append(append([]pdf.Span(nil), r.Spans...), next...)}
		}
		sort.SliceStable(r.Spans, func(a, b int) bool { return r.Spans[a].Box.X0 < r.Spans[b].Box.X0 })
		var label []string
		var raw rawDim
		raw.values, raw.notes = map[int]float64{}, map[int]string{}
		for _, s := range r.Spans {
			cx := (s.Box.X0 + s.Box.X1) / 2
			t := strings.TrimSpace(s.Text)
			switch {
			case s.Box.X0 >= symX-12 && s.Box.X1 <= cols[0].x-6:
				label = append(label, t)
			case cx > cols[0].x-25 && cx < right:
				ci, best := -1, 26.0
				for c, col := range cols {
					if d := math.Abs(cx - col.x); d < best {
						ci, best = c, d
					}
				}
				if ci < 0 {
					continue
				}
				// "0.95 BSC" may come as one span or two.
				fields := strings.Fields(t)
				if len(fields) == 1 && (strings.EqualFold(t, "BSC") || strings.EqualFold(t, "REF") || strings.EqualFold(t, "TYP")) {
					for c := range cols {
						if _, ok := raw.values[c]; ok {
							raw.notes[c] = strings.ToUpper(t)
						}
					}
					continue
				}
				if m := valueRe.FindStringSubmatch(t); m != nil {
					v, err := strconv.ParseFloat(m[1], 64)
					if err == nil {
						raw.values[ci] = v
						raw.notes[ci] = strings.ToUpper(firstNonBlank(m[2], m[3]))
					}
				}
			}
		}
		raw.symbol, raw.desc = splitLabel(strings.Join(label, " "))
		if electricalRe.MatchString(raw.desc) {
			// An electrical characteristics table, not a package's.
			return nil, header, box
		}
		if raw.symbol == "" || len(raw.values) == 0 {
			misses++
			continue
		}
		misses = 0
		last = k
		box = box.Union(r.Box)
		out = append(out, toDim(raw, cols))
	}
	return out, last, box
}

// dimRow is a table row with its values per unit.
type dimRow struct {
	dim PackageDim
	// alt are the same dimension's values in the other unit's columns,
	// in mm, to check the two agree.
	alt *PackageDim
}

// splitLabel splits a row's label into its symbol and description:
// "A1", "X (3 PLACES)", "Contact Pitch E", "Overall Height A".
func splitLabel(s string) (string, string) {
	s = strings.TrimSpace(placesRe.ReplaceAllString(s, ""))
	s = strings.TrimSpace(symbolNoteRe.ReplaceAllString(s, " "))
	f := strings.Fields(s)
	switch {
	case len(f) == 0:
		return "", ""
	case len(f) == 1 && dimSymbolRe.MatchString(f[0]):
		return f[0], ""
	case dimSymbolRe.MatchString(f[len(f)-1]) && len(f) > 1:
		return f[len(f)-1], strings.Join(f[:len(f)-1], " ")
	case dimSymbolRe.MatchString(f[0]) && len(f) <= 3:
		// "B C B" stray drawing letters before a symbol aren't symbols.
		return "", ""
	}
	return "", ""
}

// toDim puts a row's values in mm: the mm columns when there are any,
// else the inch (or mil) ones converted; the other unit is kept to check.
func toDim(raw rawDim, cols []limitColumn) dimRow {
	byUnit := map[string]*PackageDim{}
	for c, v := range raw.values {
		u := cols[c].unit
		d, ok := byUnit[u]
		if !ok {
			d = &PackageDim{Symbol: raw.symbol, Description: raw.desc, Unit: u}
			byUnit[u] = d
		}
		mm := v
		switch u {
		case "inch":
			mm = v * 25.4
		case "mil":
			mm = v * 0.0254
		}
		mm = math.Round(mm*10000) / 10000
		switch cols[c].limit {
		case "min":
			d.Min = &mm
		case "max":
			d.Max = &mm
		default:
			d.Nom = &mm
		}
		if n := raw.notes[c]; n != "" && n != "TYP" {
			d.Note = n
		}
	}
	main := byUnit["mm"]
	var alt *PackageDim
	for _, u := range []string{"inch", "mil"} {
		if d, ok := byUnit[u]; ok {
			if main == nil {
				main = d
			} else {
				alt = d
			}
		}
	}
	for _, d := range []*PackageDim{main, alt} {
		// A lone basic or reference value is the nominal, whichever
		// column it was printed in.
		if d != nil && d.Note != "" && d.Nom == nil && (d.Min == nil) != (d.Max == nil) {
			if d.Min != nil {
				d.Nom, d.Min = d.Min, nil
			} else {
				d.Nom, d.Max = d.Max, nil
			}
		}
	}
	return dimRow{*main, alt}
}

// keyed gives each dimension its key: from its description when it has
// one, else its symbol. A gull-wing's E is its span when there is an E1
// (the body); otherwise E is the body's width.
func keyed(rows []dimRow, keys map[string]string, jedec bool, name string) []PackageDim {
	hasE1 := false
	for _, r := range rows {
		hasE1 = hasE1 || r.dim.Symbol == "E1"
	}
	var out []PackageDim
	for _, r := range rows {
		d := r.dim
		for _, dk := range descKeys {
			if d.Description != "" && dk.re.MatchString(d.Description) {
				d.Key = dk.key
				break
			}
		}
		if d.Key == "" && jedec {
			d.Key = keys[d.Symbol]
			if k, ok := shapeKeys[outlineShape(name)][d.Symbol]; ok {
				d.Key = k
			}
		}
		if d.Symbol == "E" && !hasE1 && d.Key == "span" {
			d.Key = "bodyWidth"
		}
		out = append(out, d)
	}
	return out
}

// outlineShape is what a package's name says its D and E measure:
// "array" for no-lead and ball packages, where they're the body (D1 and
// E1 the terminals' or balls' span); "quad" for quad flat packages,
// whose D, like E, is tip to tip across the leads.
func outlineShape(name string) string {
	n := strings.ToUpper(name)
	switch {
	case arrayNameRe.MatchString(n):
		return "array"
	case strings.Contains(n, "QFP"):
		return "quad"
	}
	return ""
}

// arrayNameRe names no-lead and ball packages (UFQFPN is a QFN).
var arrayNameRe = regexp.MustCompile(`QFPN|QFN|DFN|SON|BGA|CSP|LGA`)

// shapeKeys are the keys that differ from dimKeys by outline shape.
var shapeKeys = map[string]map[string]string{
	"array": {"E": "bodyWidth", "E1": "", "D1": ""},
	"quad":  {"D": "spanX", "D1": "bodyLength"},
}

// jedecStyle reports whether a table's symbols are JEDEC's (A1, b, e,
// E1…) rather than a maker's own lettering (A to N around the drawing),
// whose letters mean nothing by themselves.
func jedecStyle(rows []dimRow) bool {
	n := 0
	for _, r := range rows {
		switch r.dim.Symbol {
		case "A1", "A2", "b", "c", "e", "E1", "D1", "L1":
			n++
		}
	}
	return n >= 2
}

// pageName is the package's name anywhere on a page: a name, or one a
// JEDEC outline is known by.
func pageName(text, ref string) string {
	if m := packageNameRe.FindStringSubmatch(text); m != nil {
		return commonName(m[1])
	}
	if ref != "" && commonName(ref) != strings.ToUpper(ref) {
		return commonName(ref)
	}
	return ""
}

// commonName is the name a package is known by: SOT-23 for TO-236.
func commonName(name string) string {
	n := strings.ToUpper(strings.ReplaceAll(name, " ", ""))
	for prefix, common := range jedecNames {
		if strings.HasPrefix(n, prefix) {
			return common
		}
	}
	return strings.ToUpper(name)
}

// jedecNames are the common names of JEDEC outlines.
var jedecNames = map[string]string{"TO-236": "SOT-23", "TO-261": "SOT-223", "TO-243": "SOT-89", "MO-178": "SOT-23-5", "MS-012": "SO-8", "MO-153": "TSSOP", "MO-187": "MSOP", "TO-253": "SOT-143"}

// unitChecks flags rows whose mm and inch columns disagree by over 3%.
func unitChecks(rows []dimRow) []string {
	var out []string
	pairs := func(a, b *float64) bool {
		return a != nil && b != nil && math.Abs(*a-*b) > 0.03*math.Max(math.Abs(*a), 0.01)+0.005
	}
	for _, r := range rows {
		if r.alt == nil {
			continue
		}
		if pairs(r.dim.Min, r.alt.Min) || pairs(r.dim.Nom, r.alt.Nom) || pairs(r.dim.Max, r.alt.Max) {
			out = append(out, fmt.Sprintf("%s: the millimetre and inch columns disagree", r.dim.Symbol))
		}
	}
	return out
}

// isLand reports whether the heading nearest above a table is a land
// pattern's rather than an outline's.
func isLand(rows []Row, i int) bool {
	for k := i; k >= 0; k-- {
		t := rowText(rows[k])
		switch {
		case landHeadRe.MatchString(t):
			return true
		case outlineHeadRe.MatchString(t):
			return false
		}
	}
	return false
}

// nearestName is the package name nearest above row i.
func nearestName(rows []Row, i int) string {
	for k := i; k >= 0; k-- {
		if m := packageNameRe.FindStringSubmatch(rowText(rows[k])); m != nil {
			return commonName(m[1])
		}
	}
	return ""
}

// candidates are a drawing's dimension-like texts in reading order, a
// number stacked over another at the same place joined as min–max.
func candidates(rows []Row) []string {
	type num struct {
		text string
		box  pdf.Rect
		used bool
	}
	var ns []*num
	for _, r := range rows {
		for _, s := range r.Spans {
			t := strings.Join(strings.Fields(s.Text), " ")
			if candidateRe.MatchString(t) {
				ns = append(ns, &num{text: t, box: s.Box})
			}
		}
	}
	var out []string
	for i, n := range ns {
		if n.used {
			continue
		}
		text := n.text
		for _, m := range ns[i+1:] {
			if !m.used && math.Abs(m.box.X0-n.box.X0) < 3 && m.box.Y0-n.box.Y1 >= -1 && m.box.Y0-n.box.Y1 < 6 {
				a, errA := strconv.ParseFloat(n.text, 64)
				b, errB := strconv.ParseFloat(m.text, 64)
				if errA == nil && errB == nil {
					text = fmt.Sprintf("%s–%s", strconv.FormatFloat(math.Min(a, b), 'f', -1, 64), strconv.FormatFloat(math.Max(a, b), 'f', -1, 64))
					m.used = true
					break
				}
			}
		}
		out = append(out, text)
		if len(out) == 80 {
			break
		}
	}
	return out
}

// mergePackages joins the tables and drawings of one package (same name
// or drawing code) into one.
func mergePackages(ps []Package) []Package {
	var out []Package
	idx := map[string]int{}
	for _, p := range ps {
		key := strings.ToUpper(firstNonBlank(p.Code, p.Name))
		if key == "" {
			key = fmt.Sprintf("page %d", p.Page+1)
			p.Name = key
		}
		if i, ok := idx[key]; ok {
			q := &out[i]
			q.Dims = append(q.Dims, p.Dims...)
			q.Land = append(q.Land, p.Land...)
			q.Candidates = append(q.Candidates, p.Candidates...)
			q.Checks = append(q.Checks, p.Checks...)
			if q.Reference == "" {
				q.Reference = p.Reference
			}
			if len(q.Dims) == 0 && len(p.Dims) > 0 {
				q.Page, q.Box = p.Page, p.Box
			}
			continue
		}
		idx[key] = len(out)
		out = append(out, p)
	}
	return out
}
