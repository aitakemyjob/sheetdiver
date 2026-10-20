package extract

import (
	"math"
	"regexp"
	"sort"
	"strings"

	"github.com/aitakemyjob/sheetdiver/internal/pdf"
)

// Section kinds. The agent and calculators look sections up by kind, so
// "the pin description" or "the register map" is one call regardless of
// how a manufacturer titles it.
const (
	KindFeatures     = "features"
	KindApplications = "applications"
	KindDescription  = "description"
	KindPins         = "pins"
	KindAbsMax       = "abs_max"
	KindESD          = "esd"
	KindRecommended  = "recommended"
	KindThermal      = "thermal"
	KindElectrical   = "electrical"
	KindTiming       = "timing"
	KindTypical      = "typical_characteristics"
	KindDetailed     = "detailed_description"
	KindBlock        = "block_diagram"
	KindRegisters    = "registers"
	KindApplication  = "application"
	KindTypicalApp   = "typical_application"
	KindDesign       = "design_procedure"
	KindComponents   = "component_selection"
	KindPower        = "power_supply"
	KindLayout       = "layout"
	KindPackage      = "package"
	KindOrdering     = "ordering"
	KindRevision     = "revision_history"
	KindOther        = "other"
)

// sectionKinds maps title patterns to kinds; the first match wins, so
// specific patterns come before general ones.
var sectionKinds = []struct {
	re   *regexp.Regexp
	kind string
}{
	{regexp.MustCompile(`(?i)revision history|document history`), KindRevision},
	{regexp.MustCompile(`(?i)table of contents|^contents$|list of (figures|tables)`), KindOther},
	{regexp.MustCompile(`(?i)absolute maximum`), KindAbsMax},
	{regexp.MustCompile(`(?i)\bESD\b|electrostatic`), KindESD},
	{regexp.MustCompile(`(?i)recommended operating|operating conditions|general operating`), KindRecommended},
	{regexp.MustCompile(`(?i)thermal (information|characteristics|resistance|data)`), KindThermal},
	{regexp.MustCompile(`(?i)electrical (characteristics|specifications|parameters)|dc characteristics|static characteristics`), KindElectrical},
	{regexp.MustCompile(`(?i)timing|switching characteristics|ac characteristics|dynamic characteristics`), KindTiming},
	{regexp.MustCompile(`(?i)typical (performance )?characteristics|typical performance|performance curves|typical operating characteristics`), KindTypical},
	{regexp.MustCompile(`(?i)register|memory map|bit description`), KindRegisters},
	{regexp.MustCompile(`(?i)block diagram`), KindBlock},
	{regexp.MustCompile(`(?i)pin (configuration|description|function|assignment|out|definition)|pinout|pin ?map|terminal (configuration|functions)|ball (map|configuration)`), KindPins},
	{regexp.MustCompile(`(?i)detailed design|design procedure|design steps|design requirements|design parameters|calculat`), KindDesign},
	{regexp.MustCompile(`(?i)\bselection\b|selecting the|external components|choosing|component values`), KindComponents},
	{regexp.MustCompile(`(?i)application curves|performance curves|typical waveforms`), KindTypical},
	{regexp.MustCompile(`(?i)typical application|application circuit|reference design|typical circuit|application schematic`), KindTypicalApp},
	{regexp.MustCompile(`(?i)application|implementation`), KindApplication},
	{regexp.MustCompile(`(?i)power supply recommendations|power supply considerations|power supply`), KindPower},
	{regexp.MustCompile(`(?i)layout`), KindLayout},
	{regexp.MustCompile(`(?i)mechanical|package (information|dimensions|outline|option|drawing)|packaging|land pattern|footprint|marking`), KindPackage},
	{regexp.MustCompile(`(?i)ordering|device (information|options|comparison)|part number|orderable`), KindOrdering},
	{regexp.MustCompile(`(?i)^features$|key features|\bfeatures\b`), KindFeatures},
	{regexp.MustCompile(`(?i)^applications?$`), KindApplications},
	{regexp.MustCompile(`(?i)detailed description|functional description|feature description|device functional modes|overview|theory of operation|operation`), KindDetailed},
	{regexp.MustCompile(`(?i)^description$|general description|product description`), KindDescription},
	{regexp.MustCompile(`(?i)specifications?|characteristics|ratings`), KindElectrical},
}

// SectionKind classifies a heading title.
func SectionKind(title string) string {
	if en := zhHeading(title); en != "" {
		title = en
	}
	t := strings.TrimSpace(numPrefix.ReplaceAllString(title, ""))
	for _, k := range sectionKinds {
		if k.re.MatchString(t) {
			return k.kind
		}
	}
	return KindOther
}

// numPrefix matches a leading section number such as "6.1 " or "7.3.2. ".
var numPrefix = regexp.MustCompile(`^\s*(\d+(?:\.\d+){0,4})\.?\s+`)

// splitNum separates "6.1 Absolute Maximum Ratings" into "6.1" and the title.
func splitNum(title string) (string, string) {
	if m := numPrefix.FindStringSubmatch(title); m != nil {
		return m[1], strings.TrimSpace(title[len(m[0]):])
	}
	return "", strings.TrimSpace(title)
}

// headingRe matches a numbered heading line.
var headingRe = regexp.MustCompile(`^(\d{1,2}(?:\.\d{1,2}){0,3})\.?\s+([A-Z][A-Za-z0-9 ,/&()\-–'’:+]{2,90})$`)

// plainHeadings are unnumbered headings worth recognising when set in a
// larger or bold font.
var plainHeadings = regexp.MustCompile(`(?i)^(features|applications|description|general description|pin (configurations?|descriptions?|functions?|assignments?)( and functions)?|absolute maximum ratings|recommended operating conditions|electrical (characteristics|parameters)|thermal (information|characteristics)|typical (performance )?characteristics|typical applications?( circuit)?|block diagram|functional block diagram|application information|detailed description|layout( guidelines)?|package (information|dimensions)|ordering information|register (map|description)s?)( \(notes? [\d, ]+\))?$`)

// Sections builds the section tree, preferring the PDF outline (exact and
// already nested) and falling back to heading detection by font.
func Sections(doc *pdf.Document, lines [][]Line, body float64, lay Layout) []Section {
	var secs []Section
	stale := false
	if len(doc.Bookmarks) > 0 {
		secs = fromBookmarks(doc.Bookmarks, lines, 1, lay, KindOther, &stale)
	}
	switch {
	case len(secs) < 3:
		secs = fromHeadings(lines, body, lay)
	case stale:
		// An outline that doesn't match the pages (left over from an older
		// version of the document) misses headings: add those it lacks.
		have := map[string]bool{}
		for _, s := range secs {
			have[squash(s.Title)] = true
		}
		for _, h := range fromHeadings(lines, body+1.5, lay) {
			if h.Num == "" && !have[squash(h.Title)] {
				secs = append(secs, h)
			}
		}
	}
	fillSectionText(secs, lines, lay, body, nil)
	return secs
}

// fromBookmarks flattens the outline depth first, locating each heading
// on its page so the section starts at the right line. A heading that
// isn't on its bookmark's page is looked for on the others (outlines go
// stale when a document is revised); stale is set when one wasn't where
// its bookmark said, and bookmarks whose heading is nowhere and that
// have no destination are dropped. Untitled-kind sections under a
// diagram section ("Timing Diagram" › "1. Overcharge condition") take
// its kind.
func fromBookmarks(bms []pdf.Bookmark, lines [][]Line, level int, lay Layout, parent string, stale *bool) []Section {
	var out []Section
	for _, b := range bms {
		// Outlines often list every table and figure; those aren't sections.
		if captionRe.MatchString(b.Title) || tableCaptionRe.MatchString(b.Title) {
			out = append(out, fromBookmarks(b.Children, lines, level+1, lay, parent, stale)...)
			continue
		}
		kind := parent
		if b.Title != "" {
			page, box, found := b.Page, pdf.Rect{Y0: b.Y, Y1: b.Y + 1}, false
			title := b.Title
			if page >= 0 && page < len(lines) {
				var l Line
				if l, found = findHeading(lines[page], b.Title); found {
					// The page says it as printed ("1. Overcharge → Load"),
					// the outline sometimes garbled ("Overcharge (Load").
					box, title = l.Box, l.Text
				}
			}
			if !found {
				if p, l, ok := findHeadingAnywhere(lines, b.Title, page); ok {
					page, box, found, title = p, l.Box, true, l.Text
					*stale = true
				} else if page < 0 || page >= len(lines) || !b.HasY {
					*stale = true
					out = append(out, fromBookmarks(b.Children, lines, level+1, lay, parent, stale)...)
					continue
				}
			}
			num, t := splitNum(title)
			kind = SectionKind(t)
			if kind == KindOther && diagramKinds[parent] {
				kind = parent
			}
			out = append(out, Section{Num: num, Title: t, Level: level, Kind: kind, Page: page, Y: box.Y0, start: lay.Key(page, box)})
		}
		out = append(out, fromBookmarks(b.Children, lines, level+1, lay, kind, stale)...)
	}
	return out
}

// diagramKinds are section kinds whose sub-sections are diagrams too.
var diagramKinds = map[string]bool{KindTiming: true, KindTypical: true, KindTypicalApp: true, KindBlock: true}

// findHeadingAnywhere looks for a bookmark's heading on every page,
// nearest the bookmark's own page first.
func findHeadingAnywhere(lines [][]Line, title string, near int) (int, Line, bool) {
	best, bestLine, bestDist := -1, Line{}, len(lines)+1
	for p := range lines {
		if l, ok := findHeading(lines[p], title); ok {
			d := p - near
			if d < 0 {
				d = -d
			}
			if d < bestDist {
				best, bestLine, bestDist = p, l, d
			}
		}
	}
	return best, bestLine, best >= 0
}

// findHeading finds the line on a page that matches a bookmark title,
// tolerating the number and title being split or spaced differently.
func findHeading(lines []Line, title string) (Line, bool) {
	want := squash(title)
	if want == "" {
		return Line{}, false
	}
	for _, l := range lines {
		if squash(l.Text) == want {
			return l, true
		}
	}
	_, t := splitNum(title)
	want = squash(t)
	for _, l := range lines {
		_, lt := splitNum(l.Text)
		if len(want) > 3 && squash(lt) == want {
			return l, true
		}
	}
	return Line{}, false
}

// squash lowercases and drops everything but letters and digits.
func squash(s string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(s) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// fromHeadings detects headings as numbered or well-known lines set
// larger or bolder than body text.
func fromHeadings(lines [][]Line, body float64, lay Layout) []Section {
	var out []Section
	for p, ls := range lines {
		for _, l := range ls {
			if len(l.Text) > 90 || strings.Contains(l.Text, "....") {
				continue
			}
			emphasis := l.Size >= body+1 || (l.Bold && l.Size >= body-0.5)
			if !emphasis {
				continue
			}
			if m := headingRe.FindStringSubmatch(l.Text); m != nil {
				out = append(out, Section{Num: m[1], Title: strings.TrimSpace(m[2]), Level: strings.Count(m[1], ".") + 1, Kind: SectionKind(m[2]), Page: p, Y: l.Box.Y0, start: lay.Key(p, l.Box)})
			} else if isPlainHeading(l.Text) {
				out = append(out, Section{Title: strings.TrimSpace(l.Text), Level: 1, Kind: SectionKind(l.Text), Page: p, Y: l.Box.Y0, start: lay.Key(p, l.Box)})
			}
		}
	}
	return out
}

// fillSectionText sets each section's end (the next section at the same
// or a higher level, in reading order) and the text between.
func fillSectionText(secs []Section, lines [][]Line, lay Layout, bodySize float64, regions []region) {
	sort.SliceStable(secs, func(i, j int) bool { return secs[i].start < secs[j].start })
	running := runningLines(lines)
	letters := letterBullets(lines)
	for i := range secs {
		secs[i].end = math.Inf(1)
		secs[i].EndPage, secs[i].EndY = len(lines)-1, 1e9
		for j := i + 1; j < len(secs); j++ {
			if secs[j].Level <= secs[i].Level {
				secs[i].end = secs[j].start
				secs[i].EndPage, secs[i].EndY = secs[j].Page, secs[j].Y
				break
			}
		}
		var body []Line
		var keys []float64
		inRegion := map[int]bool{}
		for _, l := range linesBetween(lines, lay, secs[i].Page, secs[i].EndPage, secs[i].start, secs[i].end) {
			// Not the heading itself, nor running headers and footers.
			if (l.Page == secs[i].Page && math.Abs(lay.Key(l.Page, l.Box)-secs[i].start) < 0.5) || (running[runningKey(l.Text)] && headerBand(l)) {
				continue
			}
			// Text inside a figure or table is shown as that figure or
			// table, not as loose words.
			if k := regionOf(regions, l); k >= 0 {
				inRegion[k] = true
				continue
			}
			body = append(body, l)
			keys = append(keys, lay.Key(l.Page, l.Box))
		}
		for k, rg := range regions {
			key := lay.Key(rg.page, rg.box)
			if !inRegion[k] && (key < secs[i].start || key >= secs[i].end-0.5) {
				continue
			}
			// The figure or table goes where it sits in reading order.
			at := sort.SearchFloat64s(keys, key)
			marker := Line{Page: rg.page, Box: rg.box, Text: "\x00" + rg.text}
			body = append(body[:at], append([]Line{marker}, body[at:]...)...)
			keys = append(keys[:at], append([]float64{key}, keys[at:]...)...)
		}
		secs[i].Text = formatText(body, bodySize, letters)
	}
}

// region is a figure or table on a page and how section text shows it.
type region struct {
	page int
	box  pdf.Rect
	text string
}

// regionOf returns the region a line lies in, or -1.
func regionOf(regions []region, l Line) int {
	cx, cy := centerX(l.Box), centerY(l.Box)
	for k, r := range regions {
		if r.page == l.Page && cx >= r.box.X0 && cx <= r.box.X1 && cy >= r.box.Y0 && cy <= r.box.Y1 {
			return k
		}
	}
	return -1
}

// contentRegions lists figures and tables for section text: a figure as
// "[Figure: caption]", a table as a markdown table (or a pointer to the
// page image when its cells are unreliable).
func contentRegions(figs []Figure, tables []Table) []region {
	var out []region
	for _, t := range tables {
		out = append(out, region{page: t.Page, box: t.Box.Inset(1, 2), text: markdownTable(t)})
	}
	for _, f := range figs {
		name := strings.TrimSpace(strings.TrimSpace("Figure " + f.Number))
		if f.Caption != "" {
			name += ": " + f.Caption
		}
		out = append(out, region{page: f.Page, box: f.Box, text: "[" + name + "]"})
	}
	return out
}

// headerNames are readable table header names for column roles.
var headerNames = map[string]string{
	"NAME": "Parameter", "SYMBOL": "Symbol", "CONDITIONS": "Conditions", "MIN": "Min", "TYP": "Typ", "MAX": "Max",
	"UNIT": "Unit", "VALUE": "Value", "NUMBER": "Pin", "DESCRIPTION": "Description", "TYPE": "Type",
}

// markdownTable renders a table as a markdown table.
func markdownTable(t Table) string {
	label := "Table"
	if t.Number != "" {
		label += " " + t.Number
	}
	if t.Caption != "" {
		label += ": " + t.Caption
	}
	if t.Irregular != "" || len(t.Header) == 0 || len(t.Rows) == 0 {
		return "[" + label + " (see the page)]"
	}
	cell := func(s string) string {
		return strings.ReplaceAll(strings.Join(strings.Fields(s), " "), "|", "\\|")
	}
	var b strings.Builder
	b.WriteString("|")
	for _, h := range t.Header {
		name := headerNames[h]
		if name == "" {
			name = h
		}
		if t.Kind == TablePins && h == "NAME" {
			name = "Name"
		}
		b.WriteString(" " + cell(name) + " |")
	}
	b.WriteString("\n|")
	for range t.Header {
		b.WriteString(" --- |")
	}
	for _, r := range t.Rows {
		b.WriteString("\n|")
		for i := range t.Header {
			v := ""
			if i < len(r) {
				v = r[i]
			}
			b.WriteString(" " + cell(v) + " |")
		}
	}
	return b.String()
}

// runningLines finds running headers and footers: texts in the top or
// bottom eighth of at least half the pages (and three or more).
func runningLines(lines [][]Line) map[string]bool {
	pages := map[string]map[int]bool{}
	for p, ls := range lines {
		for _, l := range ls {
			if !headerBand(l) {
				continue
			}
			t := runningKey(l.Text)
			if pages[t] == nil {
				pages[t] = map[int]bool{}
			}
			pages[t][p] = true
		}
	}
	out := map[string]bool{}
	for t, ps := range pages {
		if len(ps) >= 3 && 2*len(ps) >= len(lines) {
			out[t] = true
		}
	}
	return out
}

// runningKey is a header or footer line without its page number
// ("Submit Document Feedback 13", "10 Submit Document Feedback").
func runningKey(text string) string {
	return strings.Join(strings.Fields(digitsRe.ReplaceAllString(text, "")), " ")
}

// digitsRe matches a run of digits.
var digitsRe = regexp.MustCompile(`\d+`)

// headerBand reports whether a line is in the top or bottom of its page,
// where running headers and footers are.
func headerBand(l Line) bool {
	return l.PageH > 0 && (l.Box.Y1 <= l.PageH*0.15 || l.Box.Y0 >= l.PageH*0.88)
}

// linesBetween returns the body lines whose reading-order key lies in
// [start, end), skipping page headers and footers.
func linesBetween(lines [][]Line, lay Layout, page, endPage int, start, end float64) []Line {
	var out []Line
	for p := page; p <= endPage && p < len(lines); p++ {
		for _, l := range lines[p] {
			k := lay.Key(p, l.Box)
			if k < start-0.5 || k >= end-0.5 {
				continue
			}
			if l.PageH > 0 && inMargin(l.Box, l.PageH) {
				continue
			}
			out = append(out, l)
		}
	}
	return out
}

// SectionAt returns the innermost section containing a box on a page, or
// nil.
func SectionAt(secs []Section, lay Layout, page int, box pdf.Rect) *Section {
	k := lay.Key(page, box)
	var best *Section
	for i := range secs {
		s := &secs[i]
		if k < s.start-0.5 || k >= s.end-0.5 {
			continue
		}
		if best == nil || s.Level > best.Level {
			best = s
		}
	}
	return best
}
