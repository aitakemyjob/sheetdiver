package extract

import (
	"cmp"
	"fmt"
	"math"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
	"unicode"

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
	last := make([]*word, len(cols))
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
		// A subscript or superscript touching the previous word belongs to
		// it: V + SYS is VSYS, I + 2 + C is I2C.
		if p := last[best]; out[best] != "" && !(p != nil && w.box.X0-p.box.X1 < math.Max(0.8, 0.15*p.size) && math.Abs(w.size-p.size) > 0.5) {
			out[best] += " "
		}
		out[best] += w.text
		w := w
		last[best] = &w
	}
	return out
}

// gapBoundaries moves the boundary between two text columns (symbol,
// name, conditions) to where the right-hand column starts. Headers are
// often centred over left-aligned cells ("Symbol | Parameter"), so
// halfway between the header words can cut into a column's first words.
// The new boundary is a gap no row's text crosses, chosen where the most
// rows have a word starting just after it (a left-aligned column).
func gapBoundaries(cols []column, body []Row) []column {
	var rows [][]pdf.Rect
	for _, b := range body {
		// Only rows with a value: notes and labels under the table span
		// every column.
		value := false
		for i, c := range cells(cols, b.Spans) {
			if _, ok := ParseNumber(c); ok && limitRole(cols[i].role) {
				value = true
			}
		}
		if !value {
			continue
		}
		var ws []pdf.Rect
		for _, w := range words(b.Spans) {
			ws = append(ws, w.box)
		}
		rows = append(rows, ws)
	}
	crossed := func(x float64) bool {
		for _, ws := range rows {
			for _, w := range ws {
				if w.X0 < x && w.X1 > x {
					return true
				}
			}
		}
		return false
	}
	// starts counts the rows with a word starting just right of x.
	starts := func(x float64) int {
		n := 0
		for _, ws := range rows {
			for _, w := range ws {
				if w.X0 >= x-0.5 && w.X0 <= x+6 {
					n++
					break
				}
			}
		}
		return n
	}
	out := append([]column(nil), cols...)
	for i := 0; i+1 < len(out); i++ {
		a, b := out[i], out[i+1]
		if limitRole(a.role) || limitRole(b.role) || a.role == "unit" || b.role == "unit" {
			continue
		}
		at := a.x1
		bestX, bestN := 0.0, 0
		lo, hi := math.Max(at-60, a.x0+1), math.Min(at+60, b.x1-1)
		runLo := math.NaN()
		for x := lo; x <= hi+1; x++ {
			free := x <= hi && !crossed(x)
			switch {
			case free && math.IsNaN(runLo):
				runLo = x
			case !free && !math.IsNaN(runLo):
				// A run of at least 3 points with words on its left (not
				// the margin before the table).
				if x-runLo >= 3 && wordsLeftOf(rows, runLo, a.x0) {
					if n := starts(x); n > bestN {
						bestX, bestN = (runLo+x)/2, n
					}
				}
				runLo = math.NaN()
			}
		}
		// Most rows must start there: a column, not a gap between words.
		if bestN*2 > len(rows) {
			out[i].x1 = bestX
			out[i+1].x0 = bestX
		}
	}
	return out
}

// wordsLeftOf reports whether some row has a word between x0 and x.
func wordsLeftOf(rows [][]pdf.Rect, x, x0 float64) bool {
	for _, ws := range rows {
		for _, w := range ws {
			if w.X1 <= x && w.X0 >= x0 {
				return true
			}
		}
	}
	return false
}

// headerToken normalizes a header word for matching.
func headerToken(s string) string {
	return strings.Trim(strings.ToUpper(s), " .:()*")
}

// specRoles maps spec-table header words to column roles.
var specRoles = map[string]string{
	"MIN": "min", "MINIMUM": "min", "TYP": "typ", "TYPICAL": "typ", "NOM": "typ", "NOMINAL": "typ",
	"MAX": "max", "MAXIMUM": "max", "UNIT": "unit", "UNITS": "unit",
	// Chinese datasheets.
	"最小": "min", "最小值": "min", "典型": "typ", "典型值": "typ", "最大": "max", "最大值": "max", "单位": "unit",
}

// textRoles maps the descriptive headers left of the numbers.
var textRoles = map[string]string{
	"PARAMETER": "name", "PARAMETERS": "name", "ITEM": "name", "ITEMS": "name", "CHARACTERISTIC": "name", "CHARACTERISTICS": "name", "DESCRIPTION": "name", "RATING": "name",
	"SYMBOL": "symbol", "SYM": "symbol",
	"CONDITIONS": "conditions", "CONDITION": "conditions", "TEST": "conditions", "COMMENTS": "conditions", "NOTES": "conditions",
	"参数": "name", "参数名称": "name", "项目": "name", "符号": "symbol", "测试条件": "conditions", "条件": "conditions",
}

// valueHeaders are the header words of a single value column.
var valueHeaders = map[string]bool{"VALUE": true, "VALUES": true, "RATING": true, "RATINGS": true, "RATED": true, "LIMIT": true, "LIMITS": true, "额定值": true, "数值": true, "极限值": true}

// textValue reports whether a row has text in a value column, other
// than the dashes tables use for "none".
func textValue(cells []string, cols []column) bool {
	for i, c := range cols {
		if v := strings.Trim(cells[i], " –—-"); limitRole(c.role) && v != "" {
			return true
		}
	}
	return false
}

// conditionsOnly reports whether a row has text only in conditions
// columns.
func conditionsOnly(cells []string, cols []column) bool {
	any := false
	for i, c := range cols {
		if strings.TrimSpace(cells[i]) == "" {
			continue
		}
		if c.role != "conditions" {
			return false
		}
		any = true
	}
	return any
}

// limitRole reports whether a column holds values.
func limitRole(role string) bool {
	return role == "min" || role == "typ" || role == "max" || role == "value"
}

// rangeRe matches a range in one cell, unit optional: "-0.3~7", "−40 to
// 85", "-65～150", "-0.3 to 6V".
var rangeRe = regexp.MustCompile(`^\s*([+\-–−]?\d+(?:[.,]\d+)?)\s*(?:~|～|to|\.\.\.?|…)\s*([+\-–−]?\d+(?:[.,]\d+)?)\s*(?:[a-zA-Zµμ°℃%Ω]{1,3})?\s*$`)

// parseRange reads a value cell: two numbers for a range, one for a
// single value; n says how many it found.
func parseRange(s string) (lo, hi float64, n int) {
	if m := rangeRe.FindStringSubmatch(s); m != nil {
		a, ok1 := ParseNumber(m[1])
		b, ok2 := ParseNumber(m[2])
		if ok1 && ok2 {
			return a, b, 2
		}
	}
	if v, ok := ParseNumber(s); ok {
		return v, v, 1
	}
	return 0, 0, 0
}

// specHeader recognises a MIN/TYP/MAX header row and returns its columns.
func specHeader(row Row) ([]column, bool) {
	ws := words(row.Spans)
	type hw struct {
		role string
		w    word
	}
	var nums, texts, values []hw
	limits := map[string]bool{}
	for _, w := range ws {
		// A header has no values: "tON_min Minimum on time 40 96 130 ns"
		// is a data row even though it says min twice.
		if _, isNum := ParseNumber(w.text); isNum {
			return nil, false
		}
		t := headerToken(w.text)
		if valueHeaders[t] {
			values = append(values, hw{"value", w})
		}
		if r, ok := specRoles[t]; ok {
			nums = append(nums, hw{r, w})
			if r != "unit" {
				limits[r] = true
			}
		} else if r, ok := textRoles[t]; ok {
			texts = append(texts, hw{r, w})
		}
	}
	// A single value column ("Symbol | Parameter | Rating | Unit"), as in
	// many absolute-maximum tables, stands for the limits.
	if len(limits) == 0 && len(nums) == 1 && len(values) > 0 {
		v := values[0]
		for _, c := range values {
			if !strings.HasPrefix(headerToken(c.w.text), "RAT") {
				v = c // VALUE beats RATING, which then names the rows
			}
		}
		kept := texts[:0]
		for _, t := range texts {
			if t.w != v.w {
				kept = append(kept, t)
			}
		}
		texts = kept
		if len(texts) > 0 {
			nums = append(nums, v)
			limits["value"] = true
			limits["unit"] = true
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
	"PIN": "pin", "PIN/BUMP": "pin", "PIN/BALL": "pin", "PINS": "pin",
}

// packageHeaderRe matches package column headers in pin tables:
// "20-PIN", "20-BUMP", "48-BALL", "WLP", "TQFN", "SOT-23", "DDC"…
var packageHeaderRe = regexp.MustCompile(`^(\d+-?(PIN|PINS|BUMP|BUMPS|BALL|BALLS|LEAD|LEADS)|WLP|WLCSP|WCSP|CSP|DSBGA|BGA|UFBGA|[A-Z]{0,3}(QFN|DFN|SON|SOP|SOIC|QFP)(-[A-Z0-9]{1,4})?|SOIC|TSSOP|MSOP|HTSSOP|SOT-?\d*|SC-?70|TO-?\d+|DPAK|D2PAK|DDC|DBV|DCK|DRL|DRV|DGK|DGN|DSG|DSK|RGT|RTE|RUX|YCH|DQN|YFF|YFP|YZF|PDIP|SO-?\d+)$`)

// pinRole returns a pin-table header word's role; package names are
// headerWordSpans are the spans of a row in the size of its first span.
func headerWordSpans(r Row) []pdf.Span {
	var out []pdf.Span
	for _, sp := range r.Spans {
		if len(r.Spans) > 0 && math.Abs(sp.Size-r.Spans[0].Size) <= 1.5 {
			out = append(out, sp)
		}
	}
	return out
}

// headerWords are a row's words in the size of its first word: a drawing
// beside the table can put its labels ("6 5 4" over a SOT-23-6 pinout)
// on the header's line.
func headerWords(r Row) []word {
	ws := words(r.Spans)
	if len(ws) == 0 {
		return nil
	}
	out := ws[:0:0]
	for _, w := range ws {
		if math.Abs(w.size-ws[0].size) <= 1.5 {
			out = append(out, w)
		}
	}
	return out
}

// pin-number columns.
func pinRole(tok string) (string, bool) {
	if r, ok := pinRoles[tok]; ok {
		return r, true
	}
	if packageHeaderRe.MatchString(tok) {
		return "number", true
	}
	// A part number over a package's column, when one table serves
	// several parts ("TPS2120" over "WCSP", "TPS2121" over "VQFN-HR").
	if len(tok) >= 6 && isPartToken(tok) {
		return "number", true
	}
	return "", false
}

// pinHeader recognises a pin-function header at row i. Headers are often
// stacked over two or three rows ("PIN" above "NAME NO." with
// "DESCRIPTION" centred between; Maxim's "20-BUMP / WLP" and
// "20-PIN / TQFN" package columns), so neighbouring rows made only of
// header words are merged and words stacked in one column form its label.
// It returns the columns and the last header row's index.
func pinHeader(rows []Row, i int) ([]column, int, bool) {
	lo, hi, ok := pinHeaderRows(rows, i)
	if !ok {
		return nil, i, false
	}
	var ws []word
	for k := lo; k <= hi; k++ {
		ws = append(ws, headerWords(rows[k])...)
	}
	roles := map[string]bool{}
	for _, w := range ws {
		r, _ := pinRole(headerToken(w.text))
		roles[r] = true
	}
	if !roles["description"] || (!roles["name"] && !roles["pin"]) {
		return nil, i, false
	}
	groups := stackPinWords(ws, roles["name"])
	if len(groups) > 8 {
		return nil, i, false
	}
	resolvePinLabels(groups, roles["number"])
	return pinColumns(groups), hi, true
}

// isPinHeaderRow reports whether a row is made only of pin-table header
// words.
func isPinHeaderRow(r Row) bool {
	ws := headerWords(r)
	if len(ws) == 0 {
		return false
	}
	for _, w := range ws {
		if _, ok := pinRole(headerToken(w.text)); !ok {
			return false
		}
	}
	return true
}

// pinHeaderRows finds the rows of a header starting at row i: the header
// rows around it in the same type size and less than 12 points apart. A
// larger title just above the header ("Pin Description") isn't a header
// row itself.
func pinHeaderRows(rows []Row, i int) (lo, hi int, ok bool) {
	if !isPinHeaderRow(rows[i]) {
		return i, i, false
	}
	if i+1 < len(rows) && isPinHeaderRow(rows[i+1]) && rowSize(rows[i]) > rowSize(rows[i+1])+1.5 {
		return i, i, false
	}
	// Header rows share the type size of row i.
	fits := func(r Row) bool { return isPinHeaderRow(r) && math.Abs(rowSize(r)-rowSize(rows[i])) <= 1.5 }
	lo, hi = i, i
	for lo > 0 && fits(rows[lo-1]) && rows[lo].Box.Y0-rows[lo-1].Box.Y1 < 12 {
		lo--
	}
	for hi+1 < len(rows) && fits(rows[hi+1]) && rows[hi+1].Box.Y0-rows[hi].Box.Y1 < 12 {
		hi++
	}
	return lo, hi, true
}

// pinGroup is a pin-table column being built from the header words
// stacked in it.
type pinGroup struct {
	box   pdf.Rect
	role  string
	words []word
}

// pinRank orders roles for a column with words of several: a column with
// "PIN" and "NAME" is the name column.
var pinRank = map[string]int{"description": 4, "name": 3, "type": 2, "number": 1, "pin": 0}

// stackPinWords groups header words into columns by horizontal overlap,
// left to right; a column takes the highest-ranked role of its words. A
// lone "PIN" is the label spanning NAME and NO. when there is a NAME
// column, so it's left out then.
func stackPinWords(ws []word, hasName bool) []*pinGroup {
	var groups []*pinGroup
	for _, w := range ws {
		r, _ := pinRole(headerToken(w.text))
		if r == "pin" && hasName {
			continue
		}
		var g *pinGroup
		for _, x := range groups {
			if w.box.X0 < x.box.X1 && x.box.X0 < w.box.X1 {
				g = x
				break
			}
		}
		if g == nil {
			g = &pinGroup{box: w.box, role: r}
			groups = append(groups, g)
		}
		g.box = g.box.Union(w.box)
		g.words = append(g.words, w)
		if pinRank[r] > pinRank[g.role] {
			g.role = r
		}
	}
	sort.Slice(groups, func(a, b int) bool { return groups[a].box.X0 < groups[b].box.X0 })
	return groups
}

// resolvePinLabels gives a "PIN" column its meaning: the name when the
// table has a number column, else the number.
func resolvePinLabels(groups []*pinGroup, hasNumber bool) {
	for _, g := range groups {
		if g.role != "pin" {
			continue
		}
		if hasNumber {
			g.role = "name"
		} else {
			g.role = "number"
		}
	}
}

// pinColumns turns the groups into columns split halfway between their
// centres. With several number columns (one per package), each is named
// by its stacked label ("20-PIN TQFN"). The description column starts
// just after the column before it: descriptions are long and
// left-aligned.
func pinColumns(groups []*pinGroup) []column {
	numbers := 0
	for _, g := range groups {
		if g.role == "number" {
			numbers++
		}
	}
	var cols []column
	for k, g := range groups {
		x0, x1 := -1.0, 1e9
		if k > 0 {
			x0 = (centerX(groups[k-1].box) + centerX(g.box)) / 2
		}
		if k+1 < len(groups) {
			x1 = (centerX(g.box) + centerX(groups[k+1].box)) / 2
		}
		name := strings.ToUpper(g.role)
		if g.role == "number" && numbers > 1 {
			name = stackedLabel(g.words)
		}
		cols = append(cols, column{name: name, role: g.role, x0: x0, x1: x1})
	}
	for k := 1; k < len(cols); k++ {
		if cols[k].role == "description" {
			cols[k].x0 = math.Min(cols[k].x0, groups[k-1].box.X1+6)
			cols[k-1].x1 = cols[k].x0
		}
	}
	return cols
}

// stackedLabel reads a column's header words top to bottom ("20-PIN" over
// "TQFN").
func stackedLabel(ws []word) string {
	ws = append([]word(nil), ws...)
	sort.SliceStable(ws, func(a, b int) bool { return ws[a].box.Y0 < ws[b].box.Y0 })
	label := make([]string, 0, len(ws))
	for _, w := range ws {
		label = append(label, w.text)
	}
	return strings.Join(label, " ")
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
		if captionRe.MatchString(text) || tableCaptionRe.MatchString(text) || footnoteRe.MatchString(text) || (headingRe.MatchString(text) || isPlainHeading(text)) && maxSize(r) > headerSize+1 {
			break
		}
		// The table's notes: its key ("P: power supply; I: input") or a
		// note marked in superscript.
		if typeKeyRe.MatchString(text) || noteMarked(r) {
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

// footnoteMark reports whether a span is a footnote's mark by a cell
// (IO47ᶜ): one small letter or star in a smaller size. A subscript is a
// size smaller too, but sits low and belongs to its text.
func footnoteMark(sp pdf.Span, size float64) bool {
	return isMark(sp.Text) && sp.Size < size-1.5
}

// isMark reports whether a text is a footnote's mark: one small letter
// or a star.
func isMark(t string) bool {
	t = strings.TrimSpace(t)
	return len([]rune(t)) == 1 && strings.ContainsAny(t, "abcdefghijklmnopqrstuvwxyz*†")
}

// typeKeyRe is a pin table's key to its type letters, starting its row
// after its footnote mark, if any (a description's "EN: input; …" in a
// cell doesn't).
var typeKeyRe = regexp.MustCompile(`(?i)^\s*(?:\(?[a-z\d*]\)?\s+)?[A-Z]{1,2}\s*:\s*(power|input|output|ground)\b[^;]*;`)

// noteMarked reports whether a row starts as a note does: a superscript
// mark (a letter, digit or star, smaller and raised) before its text.
func noteMarked(r Row) bool {
	if len(r.Spans) == 0 {
		return false
	}
	first, rest := r.Spans[0], r.Spans[1:]
	t := strings.TrimSpace(first.Text)
	if len(rest) == 0 || len([]rune(t)) > 1 || !strings.ContainsAny(strings.ToLower(t), "abcdefghijklmnopqrstuvwxyz0123456789*†") {
		return false
	}
	next := rest[0]
	return first.Box.Y0 < next.Box.Y0-1 && len(strings.Fields(next.Text)) >= 3
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
	idx := columnIndex(cols)
	cols = gapBoundaries(cols, body)
	rows := readSpecRows(cols, idx, body)
	for _, r := range rows {
		t.Box = t.Box.Union(r.row.Box)
	}
	band := bandsFor(rows, cols, idx, rules)
	rows, band = mergeSplitRows(rows, band, cols)
	extra, attached := attachTextRows(rows, band, cols)
	if ui, ok := idx["unit"]; ok {
		fillMergedUnits(rows, band, ui)
	}
	names := bandNames(rows, band, idx, rules)
	var params []Param
	prev := Param{}
	for i, r := range rows {
		if !r.value {
			// Rows merged into a parameter's row are already in it.
			if !attached[i] {
				t.Rows = append(t.Rows, r.cells)
			}
			continue
		}
		cells, box := mergeGroup(r, extra[i], cols)
		t.Rows = append(t.Rows, cells)
		get := func(role string) string {
			if k, ok := idx[role]; ok {
				return strings.TrimSpace(cells[k])
			}
			return ""
		}
		p := Param{Section: secKind, Page: page, Box: box, Conditions: tidyConditions(get("conditions"))}
		p.Name, p.Symbol, prev = nameRow(get("name"), get("symbol"), prev, names, band[i])
		readLimits(&p, get, secKind)
		p.Unit = timeUnit(symbolFontOhms(p.Unit, p.Symbol+" "+p.Name), p.Symbol+" "+p.Name)
		p.Raw = strings.Join(nonEmpty(cells), " | ")
		p.Key = modeKey(GuessKey(p.Symbol+" "+p.Name, p.Unit, secKind), p.Conditions)
		params = append(params, p)
	}
	return t, params
}

// columnIndex maps each role to its first column.
func columnIndex(cols []column) map[string]int {
	idx := map[string]int{}
	for i, c := range cols {
		if _, dup := idx[c.role]; !dup {
			idx[c.role] = i
		}
	}
	return idx
}

// readSpecRows splits the body rows into cells and marks the rows that
// hold a value in a min, typ, max or value column.
func readSpecRows(cols []column, idx map[string]int, body []Row) []specRow {
	rows := make([]specRow, 0, len(body))
	for _, b := range body {
		cs := cells(cols, b.Spans)
		v := false
		for _, role := range []string{"min", "typ", "max", "value"} {
			if i, ok := idx[role]; ok {
				if _, ok := ParseNumber(cs[i]); ok {
					v = true
				}
			}
		}
		rows = append(rows, specRow{row: b, cells: cs, value: v})
	}
	return rows
}

// attachTextRows assigns each text-only row (a wrapped name or another
// line of conditions) to the nearest value row in its band, within 30
// points. All-caps group labels ("SUPPLY CURRENT") and rows with text in
// a value column ("VDD-24 to VDD+0.3", a parameter of its own) stay
// apart; a row with only conditions is never a label. It returns the rows attached to each value row, and which rows
// were attached.
func attachTextRows(rows []specRow, band []int, cols []column) (map[int][]specRow, map[int]bool) {
	extra := map[int][]specRow{}
	attached := map[int]bool{}
	for i, r := range rows {
		// A row with only conditions ("VCC = 2 V", "IOUT = 10 mA") is more
		// conditions even in capitals; other all-caps rows are labels.
		if r.value || (isGroupLabel(r.cells) && !conditionsOnly(r.cells, cols)) || textValue(r.cells, cols) {
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
			attached[i] = true
		}
	}
	return extra, attached
}

// fillMergedUnits gives value rows without a unit the unit of the
// nearest row in their band that has one (within 60 points): a unit set
// once for several rows is a merged cell.
func fillMergedUnits(rows []specRow, band []int, ui int) {
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

// bandNames names each ruled group of rows by its name cells: TI centres
// a parameter's name across its condition rows, so the rows above the
// name belong to it too. Without rules there are no groups.
func bandNames(rows []specRow, band []int, idx map[string]int, rules []pdf.Rect) map[int]string {
	names := map[int]string{}
	ni, ok := idx["name"]
	if !ok || len(rules) == 0 {
		return names
	}
	for i, r := range rows {
		if c := strings.TrimSpace(r.cells[ni]); c != "" && !isGroupLabel(r.cells) {
			names[band[i]] = strings.TrimSpace(names[band[i]] + " " + c)
		}
	}
	return names
}

// mergeGroup joins a value row with the text rows attached to it, top to
// bottom, column by column (values and units come from the value row
// alone), and returns the cells and the region they cover.
func mergeGroup(r specRow, extra []specRow, cols []column) ([]string, pdf.Rect) {
	merged := append([]string(nil), r.cells...)
	box := r.row.Box
	group := append([]specRow{r}, extra...)
	sort.Slice(group, func(a, b int) bool { return group[a].row.Y < group[b].row.Y })
	for k := range merged {
		if limitRole(cols[k].role) || cols[k].role == "unit" {
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
	return merged, box
}

// nameRow names a parameter row from its name and symbol cells. A row
// with neither is another condition of the parameter above (or of its
// ruled group, when the group has a name); a name without a symbol may
// start with one ("VFB Feedback voltage"). It returns the name, the
// symbol and what the next row inherits (the same).
func nameRow(name, symbol string, prev Param, names map[int]string, band int) (string, string, Param) {
	n, sym := name, symbol
	if n == "" && sym == "" {
		n, sym = prev.Name, prev.Symbol
		if g, ok := names[band]; ok {
			n = g
		}
	}
	if sym == "" {
		sym, n = splitSymbol(n)
	}
	return n, sym, Param{Name: n, Symbol: sym}
}

// readLimits reads a row's limits and unit: min, typ and max columns, or
// a single value column (a range "-0.3~7" is min and max; a lone number
// is the max of an absolute-maximum rating, else typical). Limits given
// as a tolerance around typ ("–3% / 0.511 / +3%") become values, with the
// tolerance kept in the conditions.
func readLimits(p *Param, get func(string) string, secKind string) {
	if lo, hi, n := parseRange(get("value")); n == 2 {
		p.Min, p.Max = &lo, &hi
	} else if n == 1 && secKind == KindAbsMax {
		p.Max = &lo
	} else if n == 1 {
		p.Typ = &lo
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
	if p.Typ == nil || p.Unit == "%" {
		return
	}
	for _, lim := range []struct {
		role string
		v    **float64
	}{{"min", &p.Min}, {"max", &p.Max}} {
		cell := get(lim.role)
		if !strings.HasSuffix(cell, "%") || *lim.v == nil {
			continue
		}
		v := sig6(*p.Typ * (1 + **lim.v/100))
		p.Conditions = strings.TrimSpace(p.Conditions + fmt.Sprintf(" (%s %s%%)", lim.role, cell[:len(cell)-1]))
		*lim.v = &v
	}
}

// lowPowerModeRe matches conditions that put a part in a low-power mode.
var lowPowerModeRe = regexp.MustCompile(`(?i)\b(standby|shut-?down|sleep|stop) mode\b|\bmode of sleep\b|待机|停机|停止|关断|睡眠|休眠`)

// shutdownRe matches a shutdown condition.
var shutdownRe = regexp.MustCompile(`(?i)shut-?down|停机|关断`)

// modeKey corrects a current's key for a row measured in a low-power
// mode: the supply current in shutdown is the shutdown current, and
// neither a standby supply current nor a battery current outside charging
// is the quiescent or charge current.
func modeKey(key, conditions string) string {
	if (key != "iq" && key != "ichg") || !lowPowerModeRe.MatchString(conditions) {
		return key
	}
	if key == "iq" && shutdownRe.MatchString(conditions) {
		return "isd"
	}
	return ""
}

// timeRe recognises time parameters.
var timeRe = regexp.MustCompile(`(?i)\btime\b|delay|period|duration|^\s*t\s*[A-Z]`)

// timeUnit repairs seconds written with a capital S ("uS", "mS"), which
// strictly are siemens, on time parameters.
func timeUnit(unit, label string) string {
	switch unit {
	case "uS", "µS", "mS", "nS", "S":
		if timeRe.MatchString(label) {
			return NormalizeUnit(strings.TrimSuffix(unit, "S") + "s")
		}
	}
	return unit
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
			if limitRole(c.role) && strings.TrimSpace(r.cells[k]) != "" {
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
var pinTypeRe = regexp.MustCompile(`^(?i:I|O|I/O|IO|P|G|PWR|GND|A|AI|AO|D|DI|DO|OD|NC|S|B|I/O/T|I/T|O/T|—|-|–)$`)

// hasAlnum reports whether s contains a letter or digit.
func hasAlnum(s string) bool {
	for _, r := range s {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			return true
		}
	}
	return false
}

// pinTable reads a pin-function table. A row with a pin number or name
// starts a pin; text-only rows (wrapped or vertically centred
// descriptions) go to the nearest pin row. With several package columns
// (WLP and TQFN), each pin is recorded once per package.
func pinTable(cols []column, header Row, body []Row, page int, secName string) (Table, []Pin) {
	t := Table{Page: page, Kind: TablePins, Section: secName, Box: header.Box}
	// Labels of a pinout drawing beside the table ("SOT-23-6", "TOP
	// VIEW") are set in another size; subscripts are too, but touch the
	// text they belong to.
	size := rowSize(Row{Spans: headerWordSpans(header)})
	kept := make([]Row, 0, len(body))
	for _, b := range body {
		var spans []pdf.Span
		for _, sp := range b.Spans {
			n := len(spans)
			if footnoteMark(sp, size) {
				continue
			}
			if math.Abs(sp.Size-size) <= 1.5 || (n > 0 && sp.Box.X0-spans[n-1].Box.X1 < 2) {
				spans = append(spans, sp)
			}
		}
		// A footnote's mark raised over a name sets a line of its own.
		if len(spans) == 1 && isMark(spans[0].Text) {
			continue
		}
		if len(spans) > 0 {
			b.Spans = spans
			kept = append(kept, b)
		}
	}
	body = kept
	for _, c := range cols {
		t.Header = append(t.Header, c.name)
	}
	var numCols []int
	for k, c := range cols {
		if c.role == "number" {
			numCols = append(numCols, k)
		}
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
		pin := hasAlnum(role(cs, "name"))
		for _, k := range numCols {
			if hasAlnum(cs[k]) {
				pin = true
			}
		}
		rows = append(rows, prow{y: b.Y, cells: cs, pin: pin})
	}
	type extra struct {
		y    float64
		text string
	}
	// With a name column, a row without a name is part of the nearest
	// named row: numbers set on the line above or below the name (the
	// TPS2121's OUT, "1, 8" above it and a second line of ball numbers
	// below), not a pin of its own.
	if slices.ContainsFunc(cols, func(c column) bool { return c.role == "name" }) {
		frags := map[[2]int][]extra{}
		for j, r := range rows {
			rows[j].pin = hasAlnum(role(r.cells, "name"))
		}
		// A long list runs over more lines than the name ("4, 7, 9, 10,"
		// over three more lines round one NC): a line next to one taken
		// is taken too.
		type line struct {
			y    float64
			cont bool // ends with a comma: the list goes on below
		}
		near := map[int][]line{}
		for j, o := range rows {
			if o.pin {
				near[j] = []line{{o.y, slices.ContainsFunc(numCols, func(k int) bool { return strings.HasSuffix(strings.TrimSpace(o.cells[k]), ",") })}}
			}
		}
		for taken := true; taken; {
			taken = false
			for _, r := range rows {
				if r.pin || !slices.ContainsFunc(numCols, func(k int) bool { return hasAlnum(r.cells[k]) }) {
					continue
				}
				cont := slices.ContainsFunc(numCols, func(k int) bool { return strings.HasSuffix(strings.TrimSpace(r.cells[k]), ",") })
				best, dist := -1, math.Inf(1)
				for j, ls := range near {
					for _, l := range ls {
						d := math.Abs(l.y - r.y)
						// The line a comma runs on into, either way.
						if d < 18 && (l.y < r.y && l.cont || l.y > r.y && cont) {
							d = 0
						}
						if d < dist {
							best, dist = j, d
						}
					}
				}
				if best < 0 || dist >= 12 {
					continue
				}
				for _, k := range numCols {
					if hasAlnum(r.cells[k]) {
						frags[[2]int{best, k}] = append(frags[[2]int{best, k}], extra{r.y, r.cells[k]})
						r.cells[k] = ""
					}
				}
				near[best] = append(near[best], line{r.y, cont})
				taken = true
			}
		}
		for jk, fs := range frags {
			j, k := jk[0], jk[1]
			fs = append(fs, extra{rows[j].y, rows[j].cells[k]})
			sort.SliceStable(fs, func(a, b int) bool { return fs[a].y < fs[b].y })
			// A line without a digit continues the one above ("exposed
			// thermal" over "pad"); others list more numbers.
			cell := ""
			for _, f := range fs {
				t := strings.Trim(strings.TrimSpace(f.text), ",")
				switch {
				case !hasAlnum(t):
				case cell == "":
					cell = t
				case !strings.ContainsAny(t, "0123456789"):
					cell += " " + t
				default:
					cell += ", " + t
				}
			}
			rows[j].cells[k] = cell
		}
	}
	// One number for two names set one over the other (a pin that's
	// EN/UVLO on some parts and EN/OVLO on others): the name with an empty
	// number cell shares the number, and the type, of the one beside it.
	// A dash isn't empty: the pin isn't in that package.
	if len(numCols) == 1 {
		k := numCols[0]
		for j := range rows {
			// An exposed pad's empty cell is its own: it's numbered
			// after the pins (numberPads).
			if !rows[j].pin || strings.TrimSpace(rows[j].cells[k]) != "" || padRe.MatchString(role(rows[j].cells, "name")) {
				continue
			}
			// The number sits between the names, read with the lower: the
			// numbered row is below. One above is a pin whose name wrapped
			// onto this line, which stays its own.
			best, dist := -1, 25.0
			for i, o := range rows {
				if i != j && o.pin && hasAlnum(o.cells[k]) && o.y > rows[j].y && o.y-rows[j].y < dist {
					best, dist = i, o.y-rows[j].y
				}
			}
			if best < 0 {
				continue
			}
			rows[j].cells[k] = rows[best].cells[k]
			for c, col := range cols {
				if col.role == "type" && strings.TrimSpace(rows[j].cells[c]) == "" {
					rows[j].cells[c] = rows[best].cells[c]
				}
			}
		}
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
		if !hasAlnum(d) {
			continue
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
		name, typ := role(r.cells, "name"), role(r.cells, "type")
		// A footnote's mark run into the name: "IO47 c".
		if i := strings.LastIndexByte(name, ' '); i > 0 && isMark(name[i+1:]) {
			name = strings.TrimSpace(name[:i])
		}
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
		row := append([]string(nil), r.cells...)
		t.Rows = append(t.Rows, row)
		if len(numCols) <= 1 {
			num := ""
			if len(numCols) == 1 {
				num = strings.TrimSpace(r.cells[numCols[0]])
			}
			nums := pinNumbers(num)
			if len(nums) == 0 {
				nums = []string{""}
			}
			for _, n := range nums {
				pins = append(pins, Pin{Number: n, Name: name, Type: typ, Description: d, Page: page})
			}
			continue
		}
		before := len(pins)
		for _, k := range numCols {
			for _, n := range pinNumbers(r.cells[k]) {
				pins = append(pins, Pin{Package: cols[k].name, Number: n, Name: name, Type: typ, Description: d, Page: page})
			}
		}
		// A pad with a dash for a number in every package ("EP … Exposed
		// Pad (TDFN Only)"): the pad of the package its description names,
		// else of the first.
		if len(pins) == before && padRe.MatchString(name) {
			k := numCols[0]
			for _, c := range numCols {
				if strings.Contains(strings.ToUpper(d), strings.ToUpper(cols[c].name)) {
					k = c
					break
				}
			}
			pins = append(pins, Pin{Package: cols[k].name, Name: name, Type: typ, Description: d, Page: page})
		}
	}
	return t, pins
}

// padRe is an exposed or thermal pad in a pin-number cell.
var padRe = regexp.MustCompile(`(?i)thermal|exposed|\bpad\b|^EP$|^PAD$`)

// pinRangeRe is a run of pin numbers ("4-6", "4–6", "4 to 6").
var pinRangeRe = regexp.MustCompile(`^(\d{1,3})\s*(?:-|–|—|to)\s*(\d{1,3})$`)

// pinNumbers splits a pin-number cell into the pins it lists: "4, 5, 6",
// "B1, B2, C1", "4–6", and "1, 2, 3, exposed thermal pad" (the pad as
// PAD). A dash or an empty cell (not in this package) lists none.
func pinNumbers(cell string) []string {
	var out []string
	for _, tok := range strings.FieldsFunc(cell, func(r rune) bool { return r == ',' || r == ';' }) {
		tok = strings.TrimSpace(tok)
		switch m := pinRangeRe.FindStringSubmatch(tok); {
		case !hasAlnum(tok):
		case padRe.MatchString(tok):
			out = append(out, "PAD")
		case m != nil:
			lo, _ := strconv.Atoi(m[1])
			hi, _ := strconv.Atoi(m[2])
			if hi <= lo || hi-lo > 64 {
				out = append(out, tok)
				continue
			}
			for n := lo; n <= hi; n++ {
				out = append(out, strconv.Itoa(n))
			}
		default:
			out = append(out, tok)
		}
	}
	return slices.Compact(out)
}

// prosePin matches a pin described in prose, a heading per pin, wherever
// it stands in a line (two columns can share a baseline): "TEMP(PIN1):
// Battery temperature detection input", "V CC (PIN4): …", or Linear's
// "VIN (Pin 7/Pins 8, 9): Power Input" and "PGND (Exposed Pad Pin 11/Pin
// 5, Exposed Pad Pin 13):", one slash-separated part per package. The
// colon is what makes it a heading: a drawing's "EXPOSED PAD (PIN 11) IS
// PGND" is none.
var prosePin = regexp.MustCompile(`(?:^|\s)([A-Z]{1,4} [A-Z]{1,4}|[A-Z][A-Za-z0-9_/]{0,11})\s*[(（]\s*(` + prosePinPart + `(?:\s*/\s*` + prosePinPart + `)*)\s*[)）]\s*[:：]\s*`)

// prosePinPart is one package's numbers in a prose pin heading: "Pin 7",
// "Pins 8, 9", "Exposed Pad Pin 11", "Pin 5, Exposed Pad Pin 13".
const prosePinPart = `(?i:exposed\s+pad\s+)?(?i:pins?)\s*\d{1,3}(?:\s*,\s*(?i:exposed\s+pad\s+)?(?i:pins?\s*)?\d{1,3})*`

// prosePinPackages is the package list a prose pin section gives its
// slash-separated numbers in: "(DFN/MSOP)" under "Pin Functions", or in
// the heading itself.
var prosePinPackages = regexp.MustCompile(`^(?:(?i:pin (?:functions?|descriptions?))\s*)?\(([A-Z0-9][A-Za-z0-9 .-]{0,15}(?:/[A-Z0-9][A-Za-z0-9 .-]{0,15})+)\)$`)

// ProsePins reads pins described in prose under a pin heading (common in
// Chinese datasheets, and Linear's), a "NAME (Pin n): function" heading
// per pin. A heading with numbers for several packages ("Pin 7/Pins 8, 9")
// gives each package its pins, named by the section's package list and
// sorted by package and number; an exposed pad is described as one.
func ProsePins(lines [][]Line, secs []Section, lay Layout) []Pin {
	var out []Pin
	var packages []string
	seen := map[string]bool{}
	pkgOrder := map[string]int{}
	for pi, ls := range lines {
		for _, l := range ls {
			text := strings.TrimSpace(l.Text)
			ms := prosePin.FindAllStringSubmatchIndex(text, -1)
			if ms == nil && !prosePinPackages.MatchString(text) {
				continue
			}
			if s := SectionAt(secs, lay, pi, l.Box); s == nil || s.Kind != KindPins {
				continue
			}
			if m := prosePinPackages.FindStringSubmatch(text); m != nil {
				packages = strings.Split(m[1], "/")
				continue
			}
			for i, m := range ms {
				name := strings.ReplaceAll(text[m[2]:m[3]], " ", "")
				end := len(text)
				if i+1 < len(ms) {
					end = ms[i+1][0]
				}
				desc := strings.TrimSpace(text[m[1]:end])
				parts := strings.Split(text[m[4]:m[5]], "/")
				for j, part := range parts {
					pkg := ""
					if len(parts) > 1 {
						pkg = fmt.Sprintf("package %d", j+1)
						if j < len(packages) {
							pkg = strings.TrimSpace(packages[j])
						}
					}
					if _, ok := pkgOrder[pkg]; !ok {
						pkgOrder[pkg] = len(pkgOrder)
					}
					// "Pin 5, Exposed Pad Pin 13": each number says for
					// itself whether it is the pad.
					for _, seg := range strings.Split(part, ",") {
						for _, n := range digitsRe.FindAllString(seg, -1) {
							if seen[pkg+"#"+n] {
								continue
							}
							seen[pkg+"#"+n] = true
							d := desc
							if strings.Contains(strings.ToLower(seg), "exposed pad") {
								d = strings.TrimSpace("Exposed pad. " + d)
							}
							out = append(out, Pin{Package: pkg, Number: n, Name: name, Description: d, Page: pi})
						}
					}
				}
			}
		}
	}
	if len(out) < 2 {
		return nil
	}
	slices.SortStableFunc(out, func(a, b Pin) int {
		if c := cmp.Compare(pkgOrder[a.Package], pkgOrder[b.Package]); c != 0 {
			return c
		}
		na, _ := strconv.Atoi(a.Number)
		nb, _ := strconv.Atoi(b.Number)
		return cmp.Compare(na, nb)
	})
	return out
}
