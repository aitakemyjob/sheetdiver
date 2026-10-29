package extract

import (
	"sort"

	"github.com/aitakemyjob/sheetdiver/internal/pdf"
)

// pageLayout records whether a page is set in two columns and where the
// gutter is.
type pageLayout struct {
	twoCol bool
	mid    float64
}

// Layout gives every position in a document a reading-order key, so
// "which section is this in" works on two-column pages (a datasheet's
// first page usually has Features and Applications on the left and the
// Description on the right).
type Layout []pageLayout

// NewLayout detects two-column pages: many lines entirely on each side of
// the centre and few crossing it.
func NewLayout(doc *pdf.Document, lines [][]Line) Layout {
	lay := make(Layout, len(doc.Pages))
	for i, p := range doc.Pages {
		mid := p.Width / 2
		left, right, cross := 0, 0, 0
		for _, l := range lines[i] {
			if inMargin(l.Box, p.Height) || len(l.Text) < 12 {
				continue
			}
			switch {
			case l.Box.X1 < mid+4:
				left++
			case l.Box.X0 > mid-4:
				right++
			case l.Box.X0 < mid-20 && l.Box.X1 > mid+20:
				cross++
			}
		}
		lay[i] = pageLayout{twoCol: left >= 6 && right >= 6 && cross*4 < left+right, mid: mid}
	}
	return lay
}

// column is 1 for boxes in the right column of a two-column page, else 0.
func (lay Layout) column(page int, b pdf.Rect) int {
	if page < 0 || page >= len(lay) || !lay[page].twoCol {
		return 0
	}
	if b.X0 >= lay[page].mid-4 {
		return 1
	}
	return 0
}

// columnBounds is the horizontal extent of the column a box is in on a
// two-column page, or zero bounds when the page is set in one column or
// the box spans both.
func (lay Layout) columnBounds(page int, b pdf.Rect) [2]float64 {
	if page < 0 || page >= len(lay) || !lay[page].twoCol {
		return [2]float64{}
	}
	mid := lay[page].mid
	switch {
	case b.X1 < mid+4:
		return [2]float64{0, mid}
	case b.X0 > mid-4:
		return [2]float64{mid, 2 * mid}
	}
	return [2]float64{}
}

// Key orders positions: page, then column, then height on the page.
func (lay Layout) Key(page int, b pdf.Rect) float64 {
	return float64(page)*1e6 + float64(lay.column(page, b))*1e4 + b.Y0
}

// Sort puts a page's lines in reading order.
func (lay Layout) Sort(page int, lines []Line) {
	sort.SliceStable(lines, func(i, j int) bool {
		ki, kj := lay.Key(page, lines[i].Box), lay.Key(page, lines[j].Box)
		if d := ki - kj; d > 1 || d < -1 {
			return ki < kj
		}
		return lines[i].Box.X0 < lines[j].Box.X0
	})
}
