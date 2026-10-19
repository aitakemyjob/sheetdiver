package extract

import (
	"fmt"
	"math"
	"regexp"
	"sort"
	"strings"

	"github.com/aitakemyjob/sheetdiver/internal/pdf"
)

// Table kinds.
const (
	TableSpec    = "spec"
	TablePins    = "pins"
	TableGeneric = "table"
)

// word is one whitespace-separated token with an estimated position
// (spans are split proportionally by character count).
type word struct {
	text string
	box  pdf.Rect
	size float64
	bold bool
}

// words splits a row's spans into words.
func words(spans []pdf.Span) []word {
	var out []word
	for _, s := range spans {
		parts := strings.Fields(s.Text)
		total := len([]rune(s.Text))
		if total == 0 {
			continue
		}
		per := s.Box.W() / float64(total)
		pos := 0
		rs := []rune(s.Text)
		for _, p := range parts {
			// Find p's rune offset from pos.
			pr := []rune(p)
			for pos < len(rs) && rs[pos] == ' ' {
				pos++
			}
			x0 := s.Box.X0 + float64(pos)*per
			x1 := x0 + float64(len(pr))*per
			out = append(out, word{text: p, box: pdf.Rect{X0: x0, Y0: s.Box.Y0, X1: x1, Y1: s.Box.Y1}, size: s.Size, bold: s.Bold})
			pos += len(pr)
		}
	}
	return out
}

// column is a table column: its header and its horizontal extent.
type column struct {
	name   string
	role   string
	x0, x1 float64
}

// cells assigns each span of a row to a column by its horizontal centre,
// splitting spans that straddle columns into words first.
func cells(cols []column, spans []pdf.Span) []string {
	out := make([]string, len(cols))
	for _, w := range words(spans) {
		cx := centerX(w.box)
		best := -1
		for i, c := range cols {
			if cx >= c.x0 && cx < c.x1 {
				best = i
				break
			}
		}
		if best < 0 {
			continue
		}
		if out[best] != "" {
			out[best] += " "
		}
		out[best] += w.text
	}
	return out
}

// headerToken normalizes a header word for matching.
func headerToken(s string) string {
	return strings.Trim(strings.ToUpper(s), " .:()*")
}

// specRoles maps spec-table header words to column roles.
var specRoles = map[string]string{
	"MIN": "min", "MINIMUM": "min", "TYP": "typ", "TYPICAL": "typ", "NOM": "typ", "NOMINAL": "typ",
	"MAX": "max", "MAXIMUM": "max", "UNIT": "unit", "UNITS": "unit",
}

// textRoles maps the descriptive headers left of the numbers.
var textRoles = map[string]string{
	"PARAMETER": "name", "PARAMETERS": "name", "CHARACTERISTIC": "name", "CHARACTERISTICS": "name", "DESCRIPTION": "name", "RATING": "name",
	"SYMBOL": "symbol", "SYM": "symbol",
	"CONDITIONS": "conditions", "CONDITION": "conditions", "TEST": "conditions", "COMMENTS": "conditions", "NOTES": "conditions",
}

// specHeader recognises a MIN/TYP/MAX header row and returns its columns.
func specHeader(row Row) ([]column, bool) {
	ws := words(row.Spans)
	type hw struct {
		role string
		w    word
	}
	var nums, texts []hw
	limits := map[string]bool{}
	for _, w := range ws {
		// A header has no values: "tON_min Minimum on time 40 96 130 ns"
		// is a data row even though it says min twice.
		if _, isNum := ParseNumber(w.text); isNum {
			return nil, false
		}
		t := headerToken(w.text)
		if r, ok := specRoles[t]; ok {
			nums = append(nums, hw{r, w})
			if r != "unit" {
				limits[r] = true
			}
		} else if r, ok := textRoles[t]; ok {
			texts = append(texts, hw{r, w})
		}
	}
	if len(limits) < 2 || len(nums) > 5 {
		return nil, false
	}
	// Merge adjacent text header words with the same role ("TEST CONDITIONS").
	var merged []hw
	for _, t := range texts {
		if n := len(merged); n > 0 && merged[n-1].role == t.role {
			merged[n-1].w.box = merged[n-1].w.box.Union(t.w.box)
			continue
		}
		merged = append(merged, t)
	}
	sort.Slice(nums, func(i, j int) bool { return nums[i].w.box.X0 < nums[j].w.box.X0 })
	sort.Slice(merged, func(i, j int) bool { return merged[i].w.box.X0 < merged[j].w.box.X0 })
	spacing := 40.0
	if len(nums) > 1 {
		spacing = (centerX(nums[len(nums)-1].w.box) - centerX(nums[0].w.box)) / float64(len(nums)-1)
	}
	numLo := centerX(nums[0].w.box) - spacing/2
	numHi := centerX(nums[len(nums)-1].w.box) + spacing/2
	// Text columns sit left of the numbers (PARAMETER, TEST CONDITIONS)
	// or right of them (Infineon's Conditions after Units).
	var left, right []hw
	for _, t := range merged {
		if centerX(t.w.box) < centerX(nums[0].w.box) {
			left = append(left, t)
		} else {
			right = append(right, t)
		}
	}
	var cols []column
	if len(left) == 0 {
		cols = append(cols, column{name: "PARAMETER", role: "name", x0: -1, x1: numLo})
	}
	for i, t := range left {
		x0 := -1.0
		if i > 0 {
			x0 = (left[i-1].w.box.X1 + t.w.box.X0) / 2
		}
		x1 := numLo
		if i+1 < len(left) {
			x1 = (t.w.box.X1 + left[i+1].w.box.X0) / 2
		}
		cols = append(cols, column{name: strings.ToUpper(t.role), role: t.role, x0: x0, x1: x1})
	}
	for i, n := range nums {
		x0 := numLo
		if i > 0 {
			x0 = (centerX(nums[i-1].w.box) + centerX(n.w.box)) / 2
		}
		x1 := 1e9
		if len(right) > 0 {
			x1 = numHi
		}
		if i+1 < len(nums) {
			x1 = (centerX(n.w.box) + centerX(nums[i+1].w.box)) / 2
		}
		cols = append(cols, column{name: strings.ToUpper(n.role), role: n.role, x0: x0, x1: x1})
	}
	for i, t := range right {
		x0 := numHi
		if i > 0 {
			x0 = (right[i-1].w.box.X1 + t.w.box.X0) / 2
		}
		x1 := 1e9
		if i+1 < len(right) {
			x1 = (t.w.box.X1 + right[i+1].w.box.X0) / 2
		}
		cols = append(cols, column{name: strings.ToUpper(t.role), role: t.role, x0: x0, x1: x1})
	}
	return cols, true
}

// pinRoles maps pin-table header words to roles.
var pinRoles = map[string]string{
	"NO": "number", "NUMBER": "number", "#": "number", "PIN#": "number", "BALL": "number",
	"NAME": "name", "SIGNAL": "name", "MNEMONIC": "name", "SYMBOL": "name",
	"I/O": "type", "TYPE": "type", "DIRECTION": "type", "TYPE(1)": "type",
	"DESCRIPTION": "description", "FUNCTION": "description", "FUNCTIONS": "description",
	"PIN": "pin",
}

// pinHeader recognises a pin-function header at row i. TI and others set
// it over two or three rows ("PIN" above "NAME NO.", with "DESCRIPTION"
// centred between), so neighbouring rows made only of header words are
// merged in. It returns the columns and the last header row's index.
func pinHeader(rows []Row, i int) ([]column, int, bool) {
	isHeader := func(r Row) bool {
		ws := words(r.Spans)
		if len(ws) == 0 {
			return false
		}
		for _, w := range ws {
			if _, ok := pinRoles[headerToken(w.text)]; !ok {
				return false
			}
		}
		return true
	}
	if !isHeader(rows[i]) {
		return nil, i, false
	}
	lo, hi := i, i
	for lo > 0 && isHeader(rows[lo-1]) && rows[lo].Box.Y0-rows[lo-1].Box.Y1 < 12 {
		lo--
	}
	for hi+1 < len(rows) && isHeader(rows[hi+1]) && rows[hi+1].Box.Y0-rows[hi].Box.Y1 < 12 {
		hi++
	}
	var ws []word
	for k := lo; k <= hi; k++ {
		ws = append(ws, words(rows[k].Spans)...)
	}
	type hw struct {
		role string
		box  pdf.Rect
	}
	var hs []hw
	roles := map[string]bool{}
	for _, w := range ws {
		if r, ok := pinRoles[headerToken(w.text)]; ok {
			hs = append(hs, hw{r, w.box})
			roles[r] = true
		}
	}
	if !roles["description"] || (!roles["name"] && !roles["pin"]) || len(hs) > 7 {
		return nil, i, false
	}
	// "PIN" names the pin only when there is no separate NAME column;
	// otherwise it is the spanning label over NAME and NO.
	var keep []hw
	for _, h := range hs {
		if h.role == "pin" {
			if roles["name"] {
				continue
			}
			if roles["number"] {
				h.role = "name"
			} else {
				h.role = "number"
			}
		}
		keep = append(keep, h)
	}
	sort.Slice(keep, func(i, j int) bool { return keep[i].box.X0 < keep[j].box.X0 })
	var cols []column
	for i, h := range keep {
		x0 := -1.0
		if i > 0 {
			x0 = (centerX(keep[i-1].box) + centerX(h.box)) / 2
		}
		x1 := 1e9
		if i+1 < len(keep) {
			x1 = (centerX(h.box) + centerX(keep[i+1].box)) / 2
		}
		cols = append(cols, column{name: strings.ToUpper(h.role), role: h.role, x0: x0, x1: x1})
	}
	// Descriptions are long and left-aligned: start the column just after
	// the widest content of the column before it, not at the midpoint.
	for k := 1; k < len(cols); k++ {
		if cols[k].role == "description" {
			start := keep[k-1].box.X1 + 6
			cols[k].x0 = math.Min(cols[k].x0, start)
			cols[k-1].x1 = cols[k].x0
		}
	}
	return cols, hi, true
}

// footnoteRe matches table footnotes such as "(1) Stresses beyond…".
var footnoteRe = regexp.MustCompile(`^\(\d\)\s`)

// tableRows collects the rows of a table starting after header row i,
// stopping at headings, captions, footnotes, another header or a large
// vertical gap. It returns the rows and the index after the last one.
func tableRows(rows []Row, i int, pageH float64, headerSize float64) ([]Row, int) {
	var out []Row
	last := rows[i].Box.Y1
	j := i + 1
	for ; j < len(rows); j++ {
		r := rows[j]
		if inMargin(r.Box, pageH) {
			continue
		}
		text := rowText(r)
		if r.Box.Y0-last > 28 {
			break
		}
		if captionRe.MatchString(text) || tableCaptionRe.MatchString(text) || footnoteRe.MatchString(text) || headingRe.MatchString(text) && maxSize(r) > headerSize+1 {
			break
		}
		if _, ok := specHeader(r); ok {
			break
		}
		out = append(out, r)
		last = r.Box.Y1
	}
	return out, j
}

// isProse reports whether a row is running text rather than table cells:
// a sentence set as one wide run.
func isProse(r Row) bool {
	if len(r.Spans) > 2 {
		return false
	}
	for _, s := range r.Spans {
		if s.Box.W() > 280 && len(strings.Fields(s.Text)) >= 8 {
			return true
		}
	}
	return false
}

// rowText joins a row's spans.
func rowText(r Row) string {
	parts := make([]string, len(r.Spans))
	for i, s := range r.Spans {
		parts[i] = s.Text
	}
	return strings.Join(parts, " ")
}

// maxSize is the largest font size in a row.
func maxSize(r Row) float64 { return rowSize(r) }

// Tables finds spec tables (converted to parameters), pin tables and
// captioned tables on every page.
func Tables(doc *pdf.Document, secs []Section, lay Layout) ([]Table, []Param, []Pin) {
	var tables []Table
	var params []Param
	var pins []Pin
	for pi, p := range doc.Pages {
		rows := Rows(p.Spans)
		rules := tableRules(p)
		var caption, number string
		var capBox pdf.Rect
		for i := 0; i < len(rows); i++ {
			r := rows[i]
			if inMargin(r.Box, p.Height) {
				continue
			}
			text := rowText(r)
			// "Table 7-2 summarizes…" is prose referring to a table, not
			// its caption: captions continue with a capital or a period.
			if m := tableCaptionRe.FindStringSubmatch(text); m != nil && !leaderRe.MatchString(text) && !isLower(strings.TrimSpace(m[2])) {
				number, caption, capBox = m[1], strings.TrimSpace(m[2]), r.Box
				continue
			}
			sec := SectionAt(secs, lay, pi, r.Box)
			secKind, secName := KindOther, ""
			if sec != nil {
				secKind, secName = sec.Kind, joinNum(sec.Num, sec.Title)
			}
			if cols, ok := specHeader(r); ok {
				body, next := tableRows(rows, i, p.Height, maxSize(r))
				t, ps := specTable(cols, r, body, pi, secKind, secName, rules)
				t.Number, t.Caption = number, caption
				t.Box = t.Box.Union(capBox)
				if dimensionTable(rows, i) {
					t.Kind = "dimensions"
				} else if specSections[secKind] && !designSection(sec) {
					params = append(params, ps...)
				}
				tables = append(tables, t)
				number, caption, capBox = "", "", pdf.Rect{}
				i = next - 1
				continue
			}
			if secKind == KindPins || sec == nil || strings.Contains(strings.ToLower(caption), "pin") {
				if cols, last, ok := pinHeader(rows, i); ok {
					body, next := tableRows(rows, last, p.Height, maxSize(r))
					t, ps := pinTable(cols, r, body, pi, secName)
					t.Number, t.Caption = number, caption
					t.Box = t.Box.Union(capBox)
					if len(ps) > 0 {
						tables = append(tables, t)
						pins = append(pins, ps...)
					}
					number, caption, capBox = "", "", pdf.Rect{}
					i = next - 1
					continue
				}
			}
			if caption != "" && len(r.Spans) >= 2 {
				// A captioned table we have no specific reader for.
				cols := genericColumns(r)
				body, next := tableRows(rows, i, p.Height, maxSize(r))
				// Generic tables end where running text starts (pin and spec
				// tables legitimately have sentence-long cells).
				for k, b := range body {
					if isProse(b) {
						body = body[:k]
						break
					}
				}
				t := Table{Page: pi, Number: number, Caption: caption, Kind: TableGeneric, Section: secName, Box: r.Box.Union(capBox)}
				for _, c := range cols {
					t.Header = append(t.Header, c.name)
				}
				for _, b := range body {
					t.Rows = append(t.Rows, cells(cols, b.Spans))
					t.Box = t.Box.Union(b.Box)
				}
				t.Irregular = irregularity(r, body)
				tables = append(tables, t)
				number, caption, capBox = "", "", pdf.Rect{}
				i = next - 1
				continue
			}
			if secKind == KindThermal {
				if prm, ok := thermalRow(r, pi, secName); ok {
					params = append(params, prm)
				}
			}
		}
	}
	return tables, params, pins
}

// sig6 rounds to six significant figures, dropping float noise from
// computed limits.
func sig6(v float64) float64 {
	if v == 0 {
		return 0
	}
	p := math.Pow(10, 5-math.Floor(math.Log10(math.Abs(v))))
	return math.Round(v*p) / p
}

// irregularity says why a generic table's cells can't be trusted, or ""
// when the body is a plain grid under its header. Datasheets use matrix
// tables (resistor-select tables), grouped rows with label rows between
// them, and multi-row headers; reading those column by column scrambles
// them, so they are flagged and their image becomes the reference.
func irregularity(header Row, body []Row) string {
	if len(body) == 0 {
		return "no data rows read (multi-row header)"
	}
	cols := len(header.Spans)
	wider, single := 0, 0
	labels := map[string]int{}
	for _, b := range body {
		n := len(b.Spans)
		if n > cols+1 {
			wider++
		}
		if n == 1 {
			single++
		}
		if n > 0 {
			labels[strings.ToUpper(strings.TrimSpace(b.Spans[0].Text))]++
		}
	}
	switch {
	case cols < 2:
		return "multi-row header"
	case wider*4 > len(body):
		return "rows have more cells than the header (merged or multi-row header)"
	case single*3 > len(body):
		return "group label rows between data rows"
	}
	for l, n := range labels {
		if n >= 3 && n*3 >= len(body) && !numberRe.MatchString(l) {
			return "repeated row labels (" + l + "): a grouped or matrix table"
		}
	}
	return ""
}

// dimensionRe marks package-drawing tables (MIN/MAX in millimetres).
var dimensionRe = regexp.MustCompile(`(?i)millimet|\binches\b|dimension|\bDIM\b|\bmm\b`)

// dimensionTable reports whether the MIN/MAX header at row i belongs to a
// package dimension table, judging by its header and the rows above.
func dimensionTable(rows []Row, i int) bool {
	for k := i; k >= 0 && k >= i-2; k-- {
		if rows[i].Box.Y0-rows[k].Box.Y1 > 30 {
			break
		}
		if dimensionRe.MatchString(rowText(rows[k])) {
			return true
		}
	}
	return false
}

// designSection reports whether a section (or its title) is about an
// application design rather than the device's own ratings.
func designSection(s *Section) bool {
	if s == nil {
		return false
	}
	t := strings.ToLower(s.Title)
	return strings.Contains(t, "selection") || strings.Contains(t, "design") || strings.Contains(t, "application") || strings.Contains(t, "example")
}

// genericColumns treats each header span as a column, split at midpoints.
func genericColumns(r Row) []column {
	var cols []column
	for i, s := range r.Spans {
		x0, x1 := -1.0, 1e9
		if i > 0 {
			x0 = (r.Spans[i-1].Box.X1 + s.Box.X0) / 2
		}
		if i+1 < len(r.Spans) {
			x1 = (s.Box.X1 + r.Spans[i+1].Box.X0) / 2
		}
		cols = append(cols, column{name: s.Text, x0: x0, x1: x1})
	}
	return cols
}

// specRow is a body row of a spec table split into cells.
type specRow struct {
	row   Row
	cells []string
	value bool
}

// specTable turns spec-table rows into parameters. Rows with numbers are
// parameters; text-only rows (wrapped names, multi-line conditions) are
// attached to the nearest numbered row, and a numbered row with no name
// inherits the name of the row above (one parameter, several conditions).
func specTable(cols []column, header Row, body []Row, page int, secKind, secName string, rules []pdf.Rect) (Table, []Param) {
	t := Table{Page: page, Kind: TableSpec, Section: secName, Box: header.Box}
	for _, c := range cols {
		t.Header = append(t.Header, c.name)
	}
	idx := map[string]int{}
	for i, c := range cols {
		if _, dup := idx[c.role]; !dup {
			idx[c.role] = i
		}
	}
	var rows []specRow
	for _, b := range body {
		cs := cells(cols, b.Spans)
		v := false
		for _, role := range []string{"min", "typ", "max"} {
			if i, ok := idx[role]; ok {
				if _, ok := ParseNumber(cs[i]); ok {
					v = true
				}
			}
		}
		rows = append(rows, specRow{row: b, cells: cs, value: v})
		t.Box = t.Box.Union(b.Box)
	}
	band := bandsFor(rows, cols, idx, rules)
	rows, band = mergeSplitRows(rows, band, cols)
	// Attach text-only rows to the nearest value row. All-caps rows are
	// group labels inside the table ("SUPPLY CURRENT") and are skipped.
	extra := map[int][]specRow{}
	for i, r := range rows {
		if r.value || isGroupLabel(r.cells) {
			continue
		}
		best, dist := -1, math.Inf(1)
		for j, v := range rows {
			if !v.value || band[j] != band[i] {
				continue
			}
			if d := math.Abs(v.row.Y - r.row.Y); d < dist || (d == dist && j < i) {
				best, dist = j, d
			}
		}
		if best >= 0 && dist < 30 {
			extra[best] = append(extra[best], r)
		}
	}
	// A unit set once for several rows (a merged cell) applies to the rows
	// around it: fill blanks from the nearest row that has one.
	if ui, ok := idx["unit"]; ok {
		for i := range rows {
			if !rows[i].value || rows[i].cells[ui] != "" {
				continue
			}
			best, dist := "", math.Inf(1)
			for j, o := range rows {
				if band[j] != band[i] {
					continue
				}
				if o.cells[ui] != "" && math.Abs(o.row.Y-rows[i].row.Y) < dist {
					best, dist = o.cells[ui], math.Abs(o.row.Y-rows[i].row.Y)
				}
			}
			if dist < 60 {
				rows[i].cells[ui] = best
			}
		}
	}
	// The name of each ruled group: TI centres a parameter's name across
	// its condition rows, so the rows above the label belong to it too.
	bandName := map[int]string{}
	if ni, ok := idx["name"]; ok && len(rules) > 0 {
		for i, r := range rows {
			if c := strings.TrimSpace(r.cells[ni]); c != "" && !isGroupLabel(r.cells) {
				bandName[band[i]] = strings.TrimSpace(bandName[band[i]] + " " + c)
			}
		}
	}
	var params []Param
	prevName := ""
	for i, r := range rows {
		if !r.value {
			t.Rows = append(t.Rows, r.cells)
			continue
		}
		merged := append([]string(nil), r.cells...)
		box := r.row.Box
		group := append([]specRow{r}, extra[i]...)
		sort.Slice(group, func(a, b int) bool { return group[a].row.Y < group[b].row.Y })
		for k := range merged {
			if cols[k].role == "min" || cols[k].role == "typ" || cols[k].role == "max" || cols[k].role == "unit" {
				continue
			}
			var parts []string
			for _, g := range group {
				if g.cells[k] != "" {
					parts = append(parts, g.cells[k])
				}
				box = box.Union(g.row.Box)
			}
			merged[k] = strings.Join(parts, " ")
		}
		t.Rows = append(t.Rows, merged)
		p := Param{Section: secKind, Page: page, Box: box}
		get := func(role string) string {
			if i, ok := idx[role]; ok {
				return strings.TrimSpace(merged[i])
			}
			return ""
		}
		p.Name = get("name")
		p.Symbol = get("symbol")
		p.Conditions = tidyConditions(get("conditions"))
		if p.Name == "" && p.Symbol == "" {
			p.Name = prevName
			if n, ok := bandName[band[i]]; ok {
				p.Name = n
			}
		}
		if p.Symbol == "" {
			p.Symbol, p.Name = splitSymbol(p.Name)
		}
		prevName = get("name")
		if prevName == "" {
			prevName = p.Name
		}
		if v, ok := ParseNumber(get("min")); ok {
			p.Min = &v
		}
		if v, ok := ParseNumber(get("typ")); ok {
			p.Typ = &v
		}
		if v, ok := ParseNumber(get("max")); ok {
			p.Max = &v
		}
		p.Unit = NormalizeUnit(get("unit"))
		// "–3% / 0.511 / +3%": limits given as a tolerance around typ.
		if p.Typ != nil && p.Unit != "%" {
			if strings.HasSuffix(get("min"), "%") && p.Min != nil {
				v := sig6(*p.Typ * (1 + *p.Min/100))
				p.Conditions = strings.TrimSpace(p.Conditions + fmt.Sprintf(" (min %s%%)", get("min")[:len(get("min"))-1]))
				p.Min = &v
			}
			if strings.HasSuffix(get("max"), "%") && p.Max != nil {
				v := sig6(*p.Typ * (1 + *p.Max/100))
				p.Conditions = strings.TrimSpace(p.Conditions + fmt.Sprintf(" (max %s%%)", get("max")[:len(get("max"))-1]))
				p.Max = &v
			}
		}
		p.Unit = symbolFontOhms(p.Unit, p.Symbol+" "+p.Name)
		p.Raw = strings.Join(nonEmpty(merged), " | ")
		p.Key = GuessKey(p.Symbol+" "+p.Name, p.Unit, secKind)
		params = append(params, p)
	}
	return t, params
}

// resistanceRe recognises resistance parameters.
var resistanceRe = regexp.MustCompile(`(?i)resistance|R\s*DS|\bR\s*[A-Z(]`)

// symbolFontOhms repairs Ω set in the Symbol font, which extracts as "W"
// (AOS and other older datasheets print "mW" for milliohms).
func symbolFontOhms(unit, label string) string {
	switch unit {
	case "W", "mW", "kW", "MW":
		if resistanceRe.MatchString(label) {
			return strings.TrimSuffix(unit, "W") + "Ω"
		}
	}
	return unit
}

// isGroupLabel reports whether a text-only row is an all-caps group
// heading inside a table.
func isGroupLabel(cells []string) bool {
	text := strings.Join(nonEmpty(cells), " ")
	if len(text) < 4 {
		return false
	}
	letters := 0
	for _, r := range text {
		if r >= 'a' && r <= 'z' {
			return false
		}
		if r >= 'A' && r <= 'Z' {
			letters++
		}
	}
	return letters >= 4
}

// specSections are the section kinds whose MIN/TYP/MAX tables are device
// parameters (elsewhere such tables are design examples, such as
// recommended inductor values per output voltage).
var specSections = map[string]bool{
	KindAbsMax: true, KindESD: true, KindRecommended: true, KindThermal: true, KindElectrical: true, KindTiming: true, KindOther: true,
}

// strayCommas matches commas left over from subscripted symbols
// ("V I = 3.6 V , , unlimited").
var strayCommas = regexp.MustCompile(`(\s*,){2,}|\s+,`)

// tidyConditions cleans up a conditions cell.
func tidyConditions(c string) string {
	c = strayCommas.ReplaceAllString(c, ",")
	return strings.Trim(strings.Join(strings.Fields(c), " "), ", ")
}

// bandsFor numbers the ruled groups of a spec table: horizontal rules
// that cross the parameter-name column separate parameters, while rules
// that start right of it only separate conditions of one parameter.
// Without rules every row is in band 0.
func bandsFor(rows []specRow, cols []column, idx map[string]int, rules []pdf.Rect) []int {
	band := make([]int, len(rows))
	ni, ok := idx["name"]
	if !ok || len(rules) == 0 {
		return band
	}
	nameX := math.Inf(1)
	for _, r := range rows {
		for _, s := range r.row.Spans {
			if cx := centerX(s.Box); cx >= cols[ni].x0 && cx < cols[ni].x1 {
				nameX = math.Min(nameX, s.Box.X0)
			}
		}
	}
	if math.IsInf(nameX, 1) {
		return band
	}
	var ys []float64
	for _, r := range rules {
		if r.X0 <= nameX+2 && r.X1 >= nameX+12 {
			ys = append(ys, centerY(r))
		}
	}
	for i, r := range rows {
		for _, y := range ys {
			if y < r.row.Y {
				band[i]++
			}
		}
	}
	return band
}

// mergeSplitRows joins value rows printed half a line apart with
// complementary limits (TI sets typ on one baseline and min/max on the
// next), so one condition's min, typ and max stay together.
func mergeSplitRows(rows []specRow, band []int, cols []column) ([]specRow, []int) {
	limit := func(r specRow) map[int]bool {
		m := map[int]bool{}
		for k, c := range cols {
			if (c.role == "min" || c.role == "typ" || c.role == "max") && strings.TrimSpace(r.cells[k]) != "" {
				m[k] = true
			}
		}
		return m
	}
	var out []specRow
	var outBand []int
	for i := 0; i < len(rows); i++ {
		r := rows[i]
		// The previous value row, looking past stray text rows (", ,").
		n := len(out) - 1
		for n >= 0 && !out[n].value && math.Abs(r.row.Y-out[n].row.Y) < 8 {
			n--
		}
		if n >= 0 && r.value && out[n].value && outBand[n] == band[i] && math.Abs(r.row.Y-out[n].row.Y) < 0.9*math.Max(rowSize(r.row), 6) {
			a, b := limit(out[n]), limit(r)
			disjoint := true
			for k := range b {
				if a[k] {
					disjoint = false
				}
			}
			if disjoint {
				m := &out[n]
				for k := range m.cells {
					if c := strings.TrimSpace(r.cells[k]); c != "" {
						m.cells[k] = strings.TrimSpace(m.cells[k] + " " + c)
					}
				}
				m.row.Box = m.row.Box.Union(r.row.Box)
				continue
			}
		}
		out = append(out, r)
		outBand = append(outBand, band[i])
	}
	return out, outBand
}

// tableRules returns a page's thin horizontal lines (table rules).
func tableRules(p pdf.Page) []pdf.Rect {
	var out []pdf.Rect
	for _, g := range p.Graphics {
		if g.Box.H() < 1.6 && g.Box.W() > 15 {
			out = append(out, g.Box)
		}
	}
	return out
}

// nonEmpty drops empty strings.
func nonEmpty(ss []string) []string {
	var out []string
	for _, s := range ss {
		if s = strings.TrimSpace(s); s != "" {
			out = append(out, s)
		}
	}
	return out
}

// symbolRe matches a leading electrical symbol such as VIN, I(VIN),
// RDS(on), fSW or V_FB.
var symbolRe = regexp.MustCompile(`^([A-Za-zθΘη]{1,4}(?:[(_][A-Za-z0-9_,\-+ ]{1,14}\)?|[A-Z0-9]{1,8}(?:\([A-Za-z0-9]{1,6}\))?)?)\s+(\S.*)$`)

// splitSymbol separates a leading symbol from a parameter name when the
// rest still reads as a name.
func splitSymbol(name string) (string, string) {
	m := symbolRe.FindStringSubmatch(name)
	if m == nil {
		return "", name
	}
	sym, rest := m[1], m[2]
	// The symbol needs an upper-case letter or a subscript, and the rest
	// at least one more word, or "Input voltage" becomes symbol "Input".
	if !strings.ContainsAny(sym, "ABCDEFGHIJKLMNOPQRSTUVWXYZ(_θΘ") || len(strings.Fields(rest)) < 1 || len(sym) > 1 && sym[1:] == strings.ToLower(sym[1:]) && !strings.ContainsAny(sym, "(_") {
		return "", name
	}
	return sym, rest
}

// thermalValueRe matches "… 89.3 °C/W" at the end of a thermal row.
var thermalValueRe = regexp.MustCompile(`([-–−]?\d+(?:\.\d+)?)\s*(°C/W|ºC/W|C/W|K/W)?\s*$`)

// thermalRow reads a thermal-metric row such as
// "RθJA Junction-to-ambient thermal resistance 89.3 °C/W".
func thermalRow(r Row, page int, secName string) (Param, bool) {
	text := rowText(r)
	low := strings.ToLower(text)
	if !strings.Contains(low, "junction") && !strings.Contains(text, "θJ") && !strings.Contains(text, "ΘJ") && !strings.Contains(low, "rth") {
		return Param{}, false
	}
	m := thermalValueRe.FindStringSubmatchIndex(text)
	if m == nil {
		return Param{}, false
	}
	v, ok := ParseNumber(text[m[2]:m[3]])
	if !ok {
		return Param{}, false
	}
	label := strings.TrimSpace(text[:m[0]])
	sym, name := splitSymbol(label)
	p := Param{Symbol: sym, Name: name, Typ: &v, Unit: "°C/W", Section: KindThermal, Page: page, Box: r.Box, Raw: text}
	p.Key = GuessKey(label, p.Unit, KindThermal)
	_ = secName
	return p, true
}

// pinTypeRe matches a pin type cell.
var pinTypeRe = regexp.MustCompile(`^(?i:I|O|I/O|IO|P|G|PWR|GND|A|AI|AO|D|DI|DO|OD|NC|S|B|—|-|–)$`)

// pinTable reads a pin-function table. A row with a pin number or name
// starts a pin; text-only rows (wrapped or vertically centred
// descriptions) go to the nearest pin row.
func pinTable(cols []column, header Row, body []Row, page int, secName string) (Table, []Pin) {
	t := Table{Page: page, Kind: TablePins, Section: secName, Box: header.Box}
	for _, c := range cols {
		t.Header = append(t.Header, c.name)
	}
	role := func(cs []string, r string) string {
		for i, c := range cols {
			if c.role == r {
				return strings.TrimSpace(cs[i])
			}
		}
		return ""
	}
	type prow struct {
		y     float64
		cells []string
		pin   bool
	}
	var rows []prow
	for _, b := range body {
		cs := cells(cols, b.Spans)
		t.Box = t.Box.Union(b.Box)
		rows = append(rows, prow{y: b.Y, cells: cs, pin: role(cs, "number") != "" || role(cs, "name") != ""})
	}
	type extra struct {
		y    float64
		text string
	}
	attached := map[int][]extra{}
	for _, r := range rows {
		if r.pin {
			continue
		}
		d := role(r.cells, "description")
		if d == "" {
			d = strings.Join(nonEmpty(r.cells), " ")
		}
		best, dist := -1, math.Inf(1)
		for j, o := range rows {
			if o.pin && math.Abs(o.y-r.y) < dist {
				best, dist = j, math.Abs(o.y-r.y)
			}
		}
		if best >= 0 && dist < 24 {
			attached[best] = append(attached[best], extra{r.y, d})
		}
	}
	var pins []Pin
	for j, r := range rows {
		if !r.pin {
			continue
		}
		num, name, typ := role(r.cells, "number"), role(r.cells, "name"), role(r.cells, "type")
		parts := []extra{{r.y, role(r.cells, "description")}}
		parts = append(parts, attached[j]...)
		sort.SliceStable(parts, func(a, b int) bool { return parts[a].y < parts[b].y })
		var desc []string
		for _, p := range parts {
			if p.text != "" {
				desc = append(desc, p.text)
			}
		}
		d := strings.Join(desc, " ")
		if typ != "" && !pinTypeRe.MatchString(typ) {
			d = strings.TrimSpace(typ + " " + d)
			typ = ""
		}
		t.Rows = append(t.Rows, []string{num, name, typ, d})
		pins = append(pins, Pin{Number: num, Name: name, Type: typ, Description: d, Page: page})
	}
	return t, pins
}
