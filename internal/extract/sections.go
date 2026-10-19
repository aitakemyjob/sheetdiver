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
	{regexp.MustCompile(`(?i)electrical characteristics|electrical specifications|dc characteristics|static characteristics`), KindElectrical},
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
var plainHeadings = regexp.MustCompile(`(?i)^(features|applications|description|general description|pin (configuration|description|functions?|assignments?)( and functions)?|absolute maximum ratings|recommended operating conditions|electrical characteristics|thermal (information|characteristics)|typical (performance )?characteristics|typical applications?( circuit)?|block diagram|functional block diagram|application information|detailed description|layout( guidelines)?|package (information|dimensions)|ordering information|register (map|description)s?)$`)

// Sections builds the section tree, preferring the PDF outline (exact and
// already nested) and falling back to heading detection by font.
func Sections(doc *pdf.Document, lines [][]Line, body float64, lay Layout) []Section {
	var secs []Section
	if len(doc.Bookmarks) > 0 {
		secs = fromBookmarks(doc.Bookmarks, lines, 1, lay)
	}
	if len(secs) < 3 {
		secs = fromHeadings(lines, body, lay)
	}
	fillSectionText(secs, lines, lay)
	return secs
}

// fromBookmarks flattens the outline depth first, locating each heading
// on its page so the section starts at the right line.
func fromBookmarks(bms []pdf.Bookmark, lines [][]Line, level int, lay Layout) []Section {
	var out []Section
	for _, b := range bms {
		// Outlines often list every table and figure; those aren't sections.
		if captionRe.MatchString(b.Title) || tableCaptionRe.MatchString(b.Title) {
			out = append(out, fromBookmarks(b.Children, lines, level+1, lay)...)
			continue
		}
		if b.Page >= 0 && b.Page < len(lines) && b.Title != "" {
			num, title := splitNum(b.Title)
			box := pdf.Rect{Y0: b.Y, Y1: b.Y + 1}
			if l, ok := findHeading(lines[b.Page], b.Title); ok {
				box = l.Box
			} else if !b.HasY {
				box = pdf.Rect{}
			}
			out = append(out, Section{Num: num, Title: title, Level: level, Kind: SectionKind(title), Page: b.Page, Y: box.Y0, start: lay.Key(b.Page, box)})
		}
		out = append(out, fromBookmarks(b.Children, lines, level+1, lay)...)
	}
	return out
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
			} else if plainHeadings.MatchString(strings.TrimSpace(l.Text)) {
				out = append(out, Section{Title: strings.TrimSpace(l.Text), Level: 1, Kind: SectionKind(l.Text), Page: p, Y: l.Box.Y0, start: lay.Key(p, l.Box)})
			}
		}
	}
	return out
}

// fillSectionText sets each section's end (the next section at the same
// or a higher level, in reading order) and the text between.
func fillSectionText(secs []Section, lines [][]Line, lay Layout) {
	sort.SliceStable(secs, func(i, j int) bool { return secs[i].start < secs[j].start })
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
		secs[i].Text = textBetween(lines, lay, secs[i].Page, secs[i].EndPage, secs[i].start, secs[i].end)
	}
}

// textBetween returns the body text whose reading-order key lies in
// [start, end), skipping running headers and footers.
func textBetween(lines [][]Line, lay Layout, page, endPage int, start, end float64) string {
	var b strings.Builder
	for p := page; p <= endPage && p < len(lines); p++ {
		for _, l := range lines[p] {
			k := lay.Key(p, l.Box)
			if k < start-0.5 || k >= end-0.5 {
				continue
			}
			if l.PageH > 0 && inMargin(l.Box, l.PageH) {
				continue
			}
			if b.Len() > 0 {
				b.WriteByte('\n')
			}
			b.WriteString(l.Text)
		}
	}
	return b.String()
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
