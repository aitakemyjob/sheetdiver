package extract

import (
	"math"
	"slices"
	"strings"
	"unicode"

	"github.com/aitakemyjob/sheetdiver/internal/pdf"
)

// Graphs that carry a title but no figure number, as Linear Technology
// lays out its Typical Performance Characteristics: a grid of plots, each
// with one or two bold lines right above it ("Efficiency 3.3V vs Load
// Current"), and nothing to pair a caption rule with.

// titleGap is how far above a plot its title may sit, in points.
const titleGap = 40

// TitledFigures finds the untitled-by-number graphs of the
// typical-characteristics sections: every drawing of plot size with
// little text inside and a title right above it is a figure, named by
// the title and read as a graph. Regions a captioned figure already
// covers are left alone.
func TitledFigures(doc *pdf.Document, lines [][]Line, secs []Section, lay Layout, have []Figure) []Figure {
	var out []Figure
	for _, s := range secs {
		if s.Kind != KindTypical {
			continue
		}
		for pi := s.Page; pi <= s.EndPage && pi < len(doc.Pages); pi++ {
			p := doc.Pages[pi]
			clusters := clusterGraphics(p, 0, p.Width)
			var plots []pdf.Rect
			for _, g := range clusters {
				plots = append(plots, splitAtTitles(g, lines[pi])...)
			}
			for _, g := range plots {
				g = p.Ink.Trim(g)
				if g.W() < 60 || g.H() < 60 || textCover(g, lines[pi]) >= 0.12 {
					continue
				}
				if k := lay.Key(pi, g); k < s.start-0.5 || k >= s.end-0.5 {
					continue
				}
				if taken(have, pi, g) || taken(out, pi, g) {
					continue
				}
				title, tbox := titleAbove(g, lines[pi])
				if title == "" {
					continue
				}
				box := growWithLabels(g, lines[pi], tbox, clusters, 0, p.Width, tbox.Y0, p.Height)
				f := Figure{
					Page:    pi,
					Caption: title,
					Box:     clampPage(box.Union(tbox).Inset(4, 4), p),
					Kind:    FigureKind(title),
					Section: joinNum(s.Num, s.Title),
				}
				if f.Kind == FigOther {
					f.Kind = FigGraph
				}
				if f.Kind == FigGraph {
					f.YAxis, f.XAxis = graphAxes(title)
				}
				out = append(out, f)
			}
		}
	}
	// A grid reads row by row.
	slices.SortStableFunc(out, func(a, b Figure) int {
		if a.Page != b.Page {
			return a.Page - b.Page
		}
		if math.Abs(a.Box.Y0-b.Box.Y0) > 30 {
			return int(a.Box.Y0 - b.Box.Y0)
		}
		return int(a.Box.X0 - b.Box.X0)
	})
	return out
}

// splitAtTitles cuts a cluster of plots stacked so close that they merged
// into one, at the bold titles standing inside it: each title's run ends
// the plot above and heads the one below.
func splitAtTitles(g pdf.Rect, lines []Line) []pdf.Rect {
	var cuts []Line
	for _, l := range lines {
		if l.Bold && l.Size >= 8 && hasWord(l.Text) && sameColumn(g, l.Box) && l.Box.Y0 > g.Y0+20 && l.Box.Y1 < g.Y1-20 {
			cuts = append(cuts, l)
		}
	}
	if len(cuts) == 0 {
		return []pdf.Rect{g}
	}
	slices.SortFunc(cuts, func(a, b Line) int { return int(a.Box.Y0 - b.Box.Y0) })
	var out []pdf.Rect
	top := g.Y0
	for i, l := range cuts {
		// A title's wrapped lines are one cut: the plot above ends at the
		// first, the plot below starts under the last.
		if i > 0 && l.Box.Y0-cuts[i-1].Box.Y1 <= l.Size*0.8 {
			top = l.Box.Y1 + 1
			continue
		}
		piece := g
		piece.Y0, piece.Y1 = top, l.Box.Y0-1
		if piece.H() >= 24 {
			out = append(out, piece)
		}
		top = l.Box.Y1 + 1
	}
	last := g
	last.Y0 = top
	return append(out, last)
}

// titleAbove is the title right above a plot: the run of text lines
// ending within titleGap of its top, over it, in a readable size and
// made of words (not a tick value), top line first; or nothing.
func titleAbove(g pdf.Rect, lines []Line) (string, pdf.Rect) {
	var cands []Line
	for _, l := range lines {
		gap := g.Y0 - l.Box.Y1
		if gap < -2 || gap > titleGap || l.Size < 8 || !sameColumn(g, l.Box) || captionRe.MatchString(l.Text) || !hasWord(l.Text) {
			continue
		}
		cands = append(cands, l)
	}
	if len(cands) == 0 {
		return "", pdf.Rect{}
	}
	slices.SortFunc(cands, func(a, b Line) int { return int(b.Box.Y0 - a.Box.Y0) })
	run := []Line{cands[0]}
	for _, l := range cands[1:] {
		last := run[len(run)-1]
		if last.Box.Y0-l.Box.Y1 > last.Size*0.8 || math.Abs(l.Size-last.Size) > 0.6 {
			break
		}
		run = append(run, l)
	}
	box := run[0].Box
	var parts []string
	for i := len(run) - 1; i >= 0; i-- {
		parts = append(parts, strings.TrimSpace(run[i].Text))
		box = box.Union(run[i].Box)
	}
	return strings.Join(parts, " "), box
}

// hasWord reports whether text has a run of three letters: a title, not
// a tick value such as "100" or "µA".
func hasWord(text string) bool {
	n := 0
	for _, r := range text {
		if unicode.IsLetter(r) {
			if n++; n >= 3 {
				return true
			}
		} else {
			n = 0
		}
	}
	return false
}
