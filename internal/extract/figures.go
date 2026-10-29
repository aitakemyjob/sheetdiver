package extract

import (
	"math"
	"regexp"
	"sort"
	"strings"

	"github.com/aitakemyjob/sheetdiver/internal/pdf"
)

// Figure kinds.
const (
	FigGraph     = "graph"
	FigSchematic = "schematic"
	FigBlock     = "block_diagram"
	FigPinout    = "pinout"
	FigWaveform  = "waveform"
	FigTiming    = "timing"
	FigLayout    = "layout"
	FigPackage   = "package"
	FigOther     = "figure"
)

// captionRe matches "Figure 6-1. Title", "Fig. 3: Title", "Figure 12 Title".
var captionRe = regexp.MustCompile(`^(?i:figure|fig\.?)\s*(\d+(?:[-–.]\d+)*)\s*[.:]?\s*(.*)$`)

// tableCaptionRe matches "Table 6-1. Title".
var tableCaptionRe = regexp.MustCompile(`^(?i:table)\s+(\d+(?:[-–.]\d+)*)\s*[.:]?\s*(.*)$`)

// leaderRe matches table-of-contents dot leaders.
var leaderRe = regexp.MustCompile(`\.{4,}|…{2,}|(\. ){4,}`)

// caption is a figure caption line found on a page.
type caption struct {
	line   Line
	number string
	title  string
}

// Figures finds captioned figures and the graphics each caption labels.
// Graphics are clustered (vector paths, images and forms that touch),
// each caption is paired with the nearest cluster above or below it in
// its column, and the region grows to take in the cluster's own labels
// (axis titles, tick values). Which side captions sit on is decided per
// document, by majority.
func Figures(doc *pdf.Document, lines [][]Line, secs []Section, lay Layout) []Figure {
	type cand struct {
		cap          caption
		above, below *pdf.Rect
		gapA, gapB   float64
		clusters     []pdf.Rect
		col          [2]float64
	}
	var cands []cand
	votesAbove, votesBelow := 0, 0
	for pi, p := range doc.Pages {
		caps := captions(lines[pi])
		for ci, c := range caps {
			x0, x1 := captionColumn(caps, ci, p.Width, lay.columnBounds(pi, c.line.Box))
			clusters := clusterGraphics(p, x0, x1)
			cd := cand{cap: c, gapA: math.Inf(1), gapB: math.Inf(1), clusters: clusters, col: [2]float64{x0, x1}}
			for i := range clusters {
				g := clusters[i]
				g = p.Ink.Trim(g)
				if !sameColumn(g, c.line.Box) {
					continue
				}
				if gap := c.line.Box.Y0 - g.Y1; g.Y1 <= c.line.Box.Y0+3 && gap > -3 && gap < 70 && gap < cd.gapA {
					cd.above, cd.gapA = &clusters[i], gap
				}
				if gap := g.Y0 - c.line.Box.Y1; g.Y0 >= c.line.Box.Y1-3 && gap > -3 && gap < 70 && gap < cd.gapB {
					cd.below, cd.gapB = &clusters[i], gap
				}
			}
			switch {
			case cd.above != nil && (cd.below == nil || cd.gapA <= cd.gapB):
				votesAbove++
			case cd.below != nil:
				votesBelow++
			}
			cands = append(cands, cd)
		}
	}
	preferAbove := votesAbove >= votesBelow
	seen := map[string]int{}
	var out []Figure
	for _, cd := range cands {
		g, alt := cd.above, cd.below
		if !preferAbove {
			g, alt = alt, g
		}
		if g == nil {
			g = alt
		}
		if g == nil {
			continue // a reference to a figure in prose, or an undetectable figure
		}
		pi := cd.cap.line.Page
		x0, x1 := cd.col[0], cd.col[1]
		// A figure never reaches past the captions above and below it in
		// its column (form bounds often do).
		top, bottom := captionLimits(captions(lines[pi]), cd.cap.line.Box, cd.col)
		clipped := *g
		clipped.Y0, clipped.Y1 = math.Max(clipped.Y0, top), math.Min(clipped.Y1, bottom)
		if clipped.H() > 10 {
			g = &clipped
		}
		box := growWithLabels(doc.Pages[pi].Ink.Trim(*g), lines[pi], cd.cap.line.Box, cd.clusters, x0, x1, top, bottom)
		box = doc.Pages[pi].Ink.Trim(box)
		f := Figure{
			Page:    pi,
			Number:  cd.cap.number,
			Caption: cd.cap.title,
			Box:     clampY(clampPage(box.Union(cd.cap.line.Box).Inset(4, 4), doc.Pages[pi]), top, bottom),
			Kind:    FigureKind(cd.cap.title),
		}
		if s := SectionAt(secs, lay, pi, cd.cap.line.Box); s != nil {
			f.Section = joinNum(s.Num, s.Title)
			if f.Kind == FigOther {
				f.Kind = figureKindFromSection(s.Kind)
			}
		}
		if f.Kind == FigGraph {
			f.YAxis, f.XAxis = graphAxes(f.Caption)
		}
		if i, dup := seen[f.Number]; dup && f.Number != "" {
			// Keep the larger region; duplicates come from captions repeated
			// in running text.
			if out[i].Box.W()*out[i].Box.H() < f.Box.W()*f.Box.H() {
				out[i] = f
			}
			continue
		}
		seen[f.Number] = len(out)
		out = append(out, f)
	}
	return out
}

// captionLimits returns how far up and down a caption's figure may
// extend: to the nearest other captions in the same column.
func captionLimits(caps []caption, c pdf.Rect, col [2]float64) (float64, float64) {
	top, bottom := 0.0, 1e9
	for _, o := range caps {
		b := o.line.Box
		if b == c || centerX(b) < col[0] || centerX(b) > col[1] {
			continue
		}
		if b.Y1 <= c.Y0-2 {
			top = math.Max(top, b.Y1+2)
		}
		if b.Y0 >= c.Y1+2 {
			bottom = math.Min(bottom, b.Y0-1)
		}
	}
	return top, bottom
}

// captionColumn returns the horizontal extent a caption's figure can
// occupy: the page (or the caption's column of a two-column page, col),
// narrowed halfway towards any caption beside it (figures laid out side
// by side, two or three to a row).
func captionColumn(caps []caption, i int, pageW float64, col [2]float64) (float64, float64) {
	c := caps[i].line.Box
	x0, x1 := col[0], col[1]
	if x1 <= x0 {
		x0, x1 = 0, pageW
	}
	for j, o := range caps {
		ob := o.line.Box
		if j == i || math.Abs(centerY(ob)-centerY(c)) > 60 || (ob.X0 < c.X1 && c.X0 < ob.X1) {
			continue
		}
		mid := (centerX(ob) + centerX(c)) / 2
		if centerX(ob) < centerX(c) {
			x0 = math.Max(x0, mid)
		} else {
			x1 = math.Min(x1, mid)
		}
	}
	return x0, x1
}

// joinNum renders "6.1 Title", or just the title when unnumbered.
func joinNum(num, title string) string {
	if num == "" {
		return title
	}
	return num + " " + title
}

// captions finds figure caption lines on a page, joining a wrapped second
// line of caption text.
func captions(lines []Line) []caption {
	var out []caption
	for i, l := range lines {
		if leaderRe.MatchString(l.Text) {
			continue
		}
		m := captionRe.FindStringSubmatch(l.Text)
		if m == nil {
			continue
		}
		title := strings.TrimSpace(m[2])
		// Prose such as "Figure 8-1 shows…" starts lower-case after the number.
		if title != "" && isLower(title) {
			continue
		}
		c := caption{line: l, number: m[1], title: title}
		if j := i + 1; j < len(lines) {
			n := lines[j]
			if n.Box.Y0-l.Box.Y1 < l.Size*0.6 && math.Abs(n.Size-l.Size) < 0.6 && n.Box.X0 >= l.Box.X0-20 && n.Box.X1 <= l.Box.X1+60 &&
				!captionRe.MatchString(n.Text) && len(title) > 0 && !strings.HasSuffix(title, ".") {
				c.title += " " + n.Text
				c.line.Box = c.line.Box.Union(n.Box)
			}
		}
		out = append(out, c)
	}
	return out
}

// isLower reports whether s starts with a lower-case letter.
func isLower(s string) bool {
	r := []rune(s)
	return len(r) > 0 && r[0] >= 'a' && r[0] <= 'z'
}

// clusterGraphics merges the page's graphic objects into connected
// regions within a column, ignoring hairlines (table rules, separators), page-sized
// backgrounds and anything in the header or footer band.
func clusterGraphics(p pdf.Page, colX0, colX1 float64) []pdf.Rect {
	var boxes []pdf.Rect
	for _, g := range p.Graphics {
		// Form bounds can run far off the page (clip paths), so clip to the
		// page and to the caption's column first.
		b := g.Box
		b = pdf.Rect{X0: math.Max(b.X0, colX0), Y0: math.Max(b.Y0, 0), X1: math.Min(b.X1, colX1), Y1: math.Min(b.Y1, p.Height)}
		if b.W() < 1.5 || b.H() < 1.5 {
			continue
		}
		// Page-sized graphics are backgrounds, judged on the page before
		// the column clip; a form spanning most of the page holds the
		// page's content (Linear sets each page as one form): a container,
		// not a drawing.
		onPage := pdf.Rect{X0: math.Max(g.Box.X0, 0), Y0: math.Max(g.Box.Y0, 0), X1: math.Min(g.Box.X1, p.Width), Y1: math.Min(g.Box.Y1, p.Height)}
		if onPage.W() > p.Width*0.9 && onPage.H() > p.Height*0.8 {
			continue
		}
		if g.Kind == "form" && onPage.W() > p.Width*0.75 && onPage.H() > p.Height*0.75 {
			continue
		}
		if inMargin(b, p.Height) {
			continue
		}
		boxes = append(boxes, b)
	}
	// Repeatedly merge touching boxes until nothing changes.
	for merged := true; merged; {
		merged = false
		for i := 0; i < len(boxes); i++ {
			for j := i + 1; j < len(boxes); j++ {
				if boxes[i].Inset(3, 3).Intersects(boxes[j]) {
					boxes[i] = boxes[i].Union(boxes[j])
					boxes = append(boxes[:j], boxes[j+1:]...)
					merged = true
					j--
				}
			}
		}
	}
	out := boxes[:0]
	for _, b := range boxes {
		if b.W() >= 24 && b.H() >= 24 {
			out = append(out, b)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Y0 < out[j].Y0 })
	return out
}

// sameColumn reports whether a graphic and a caption share a column: they
// overlap horizontally by a good part of the narrower one, or the caption
// is centred over the graphic.
func sameColumn(g, c pdf.Rect) bool {
	ov := math.Min(g.X1, c.X1) - math.Max(g.X0, c.X0)
	if ov <= 0 {
		return false
	}
	cx := centerX(c)
	return ov >= 0.3*math.Min(g.W(), c.W()) || (cx >= g.X0 && cx <= g.X1)
}

// growWithLabels extends a graphic region with nearby text that belongs to
// it (axis titles, tick labels, legends), stopping at the caption and at
// other graphics.
func growWithLabels(g pdf.Rect, lines []Line, capBox pdf.Rect, clusters []pdf.Rect, colX0, colX1, top, bottom float64) pdf.Rect {
	box := g
	reach := g.Inset(22, 16)
	for _, l := range lines {
		if l.Box.Intersects(capBox) || !reach.Intersects(l.Box) || l.Box.X0 < colX0-1 || l.Box.X1 > colX1+1 || l.Box.Y0 < top || l.Box.Y1 > bottom {
			continue
		}
		if captionRe.MatchString(l.Text) || len(l.Text) > 80 {
			continue
		}
		other := false
		for _, c := range clusters {
			if c != g && c.Intersects(l.Box) {
				other = true
				break
			}
		}
		if !other {
			box = box.Union(l.Box)
		}
	}
	// Never swallow the caption itself.
	if box.Y1 > capBox.Y0 && g.Y1 <= capBox.Y0+3 {
		box.Y1 = math.Min(box.Y1, capBox.Y0-1)
	}
	if box.Y0 < capBox.Y1 && g.Y0 >= capBox.Y1-3 {
		box.Y0 = math.Max(box.Y0, capBox.Y1+1)
	}
	return box
}

// clampY keeps a rectangle between two heights.
func clampY(r pdf.Rect, top, bottom float64) pdf.Rect {
	r.Y0, r.Y1 = math.Max(r.Y0, top), math.Min(r.Y1, bottom)
	return r
}

// clampPage keeps a rectangle on the page.
func clampPage(r pdf.Rect, p pdf.Page) pdf.Rect {
	return pdf.Rect{X0: math.Max(0, r.X0), Y0: math.Max(0, r.Y0), X1: math.Min(p.Width, r.X1), Y1: math.Min(p.Height, r.Y1)}
}

// figureKinds maps caption patterns to kinds, most specific first.
var figureKinds = []struct {
	re   *regexp.Regexp
	kind string
}{
	{regexp.MustCompile(`(?i)block diagram`), FigBlock},
	{regexp.MustCompile(`(?i)\btiming\b|timing diagram|waveforms? definition`), FigTiming},
	{regexp.MustCompile(`(?i)\bvs\.?\b|\bversus\b|efficiency|regulation|derating|\bsoa\b|safe operating area|bode plot|\bplot\b`), FigGraph},
	{regexp.MustCompile(`(?i)typical application|application (circuit|schematic|diagram)|reference design|schematic|simplified|circuit|evaluation module|connection diagram`), FigSchematic},
	{regexp.MustCompile(`(?i)pin ?out|pin (configuration|assignment|diagram)|top view|bottom view|ball map`), FigPinout},
	{regexp.MustCompile(`(?i)layout|pcb|board`), FigLayout},
	{regexp.MustCompile(`(?i)package|outline|land pattern|footprint|dimensions|marking|tape|reel`), FigPackage},
	{regexp.MustCompile(`(?i)ripple|transient|start-?up|shut ?down|power-?up|power-?down|soft-?start|switching node|scope|waveform|response|enable|short circuit|overcurrent|hiccup|load step`), FigWaveform},
}

// FigureKind guesses what a figure shows from its caption.
func FigureKind(caption string) string {
	for _, k := range figureKinds {
		if k.re.MatchString(caption) {
			return k.kind
		}
	}
	return FigOther
}

// figureKindFromSection guesses a figure's kind from its section when the
// caption gives nothing away.
func figureKindFromSection(kind string) string {
	switch kind {
	case KindTypical:
		return FigGraph
	case KindTypicalApp, KindApplication:
		return FigSchematic
	case KindBlock:
		return FigBlock
	case KindPins:
		return FigPinout
	case KindLayout:
		return FigLayout
	case KindPackage:
		return FigPackage
	case KindTiming:
		return FigTiming
	}
	return FigOther
}

// vsRe splits "Efficiency vs Output Current" into its two quantities.
var vsRe = regexp.MustCompile(`(?i)^(.*?)\s+(?:vs\.?|versus)\s+(.*)$`)

// graphAxes returns the y and x quantities named by a "Y vs X" caption,
// stripping trailing conditions such as ", VIN = 12 V".
func graphAxes(caption string) (string, string) {
	m := vsRe.FindStringSubmatch(caption)
	if m == nil {
		return "", ""
	}
	y := strings.TrimSpace(m[1])
	x := strings.TrimSpace(m[2])
	if i := strings.IndexAny(x, ",;("); i > 0 {
		x = strings.TrimSpace(x[:i])
	}
	// "TPS563200 Efficiency vs …" names the part first; keep the quantity.
	if f := strings.Fields(y); len(f) > 1 && mpnRe.MatchString(f[0]) {
		y = strings.Join(f[1:], " ")
	}
	return y, x
}

// figureSections are the section kinds that are mostly a drawing, and
// pinTables the pin sections that are a table rather than a pinout.
var (
	figureSections = map[string]string{KindTypicalApp: FigSchematic, KindBlock: FigBlock, KindPins: FigPinout, KindPackage: FigPackage, KindTiming: FigTiming}
	pinTables      = regexp.MustCompile(`(?i)function|description|definition`)
	orderable      = regexp.MustCompile(`(?i)orderable|ordering`)
)

// SectionFigures finds uncaptioned figures: the drawing under a heading
// such as "Typical application circuit", "Pin configuration",
// "Functional block diagram" or "Package dimensions" (many datasheets,
// especially Chinese ones, caption none of their figures). The heading
// names the figure. Sections that already have a captioned figure are
// left alone.
func SectionFigures(doc *pdf.Document, lines [][]Line, secs []Section, lay Layout, have []Figure) []Figure {
	captioned := map[string]bool{}
	for _, f := range have {
		captioned[f.Section] = true
	}
	var out []Figure
	// Deepest sections first, so "1. Overcharge condition" names its
	// drawing rather than the "Timing Diagram" section around it.
	order := append([]Section(nil), secs...)
	sort.SliceStable(order, func(i, j int) bool { return order[i].Level > order[j].Level })
	for _, s := range order {
		kind, ok := figureSections[s.Kind]
		name := joinNum(s.Num, s.Title)
		if !ok || captioned[name] || (s.Kind == KindPins && pinTables.MatchString(s.Title)) || orderable.MatchString(s.Title) {
			continue
		}
		for pi := s.Page; pi <= s.EndPage && pi < len(doc.Pages) && pi <= s.Page+1; pi++ {
			p := doc.Pages[pi]
			var box pdf.Rect
			found := false
			for _, g := range clusterGraphics(p, 0, p.Width) {
				g = p.Ink.Trim(g)
				if k := lay.Key(pi, g); k < s.start-0.5 || k >= s.end-0.5 || g.W() < 40 || g.H() < 30 {
					continue
				}
				// A ruled table beside the drawing isn't part of it.
				if textCover(g, lines[pi]) >= 0.12 {
					continue
				}
				if found {
					box = box.Union(g)
				} else {
					box, found = g, true
				}
			}
			if !found {
				continue
			}
			top, bottom := 0.0, p.Height
			if pi == s.Page {
				top = s.Y + 4
			}
			if pi == s.EndPage && s.EndY > top {
				bottom = s.EndY
			}
			box = growWithLabels(box, lines[pi], pdf.Rect{}, nil, 0, p.Width, top, bottom)
			// A region dense with text is a table (pin lists, orderable
			// parts), not a drawing; a region already taken is a parent
			// section's.
			if textCover(box, lines[pi]) >= 0.12 || taken(out, pi, box) {
				break
			}
			out = append(out, Figure{Page: pi, Caption: s.Title, Section: name, Kind: kind, Box: clampY(clampPage(box.Inset(4, 4), p), top, bottom)})
			break
		}
	}
	return out
}

// textCover is the share of a region covered by text lines.
func textCover(r pdf.Rect, lines []Line) float64 {
	var a float64
	for _, l := range lines {
		if r.Inset(1, 1).Contains(l.Box) {
			a += l.Box.W() * l.Box.H()
		}
	}
	return a / math.Max(1, r.W()*r.H())
}

// taken reports whether a figure on the page already covers most of a
// region.
func taken(figs []Figure, page int, r pdf.Rect) bool {
	for _, f := range figs {
		if f.Page != page || !f.Box.Intersects(r) {
			continue
		}
		ov := pdf.Rect{X0: math.Max(f.Box.X0, r.X0), Y0: math.Max(f.Box.Y0, r.Y0), X1: math.Min(f.Box.X1, r.X1), Y1: math.Min(f.Box.Y1, r.Y1)}
		if ov.W()*ov.H() > 0.6*r.W()*r.H() {
			return true
		}
	}
	return false
}
