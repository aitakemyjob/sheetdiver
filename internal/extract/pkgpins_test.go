package extract

import (
	"slices"
	"testing"

	"github.com/aitakemyjob/sheetdiver/internal/pdf"
)

func TestPackageColumnPins(t *testing.T) {
	span := func(x, y float64, text string) pdf.Span {
		return pdf.Span{Text: text, Box: pdf.Rect{X0: x - 4, Y0: y - 4, X1: x + 4, Y1: y + 4}, Size: 8}
	}
	head := func(x float64, text string) pdf.Span {
		s := span(x, 0, text)
		s.Box.Y0, s.Box.Y1, s.Vertical = 20, 80, true
		return s
	}
	// ST's: LQFP48 and UFQFPN48 share a column (same pinout); a name over
	// two lines with its function after reset under it in brackets; a
	// pin the 48-pin packages don't have.
	spans := []pdf.Span{
		head(98, "LQFP48"), head(108, "UFQFPN48"), head(140, "LQFP64"), span(200, 50, "Pin name"), span(260, 50, "Pin type"),
		span(102, 110, "3"), span(140, 110, "3"), span(200, 100, "PC14-"), span(200, 110, "OSC32_IN"), span(200, 120, "(PC14)"), span(260, 110, "I/O"),
		span(102, 150, "-"), span(140, 150, "4"), span(200, 150, "VSS"), span(260, 150, "S"),
	}
	got := packageColumnPage(Rows(spans), nil, 7)
	var list []string
	for _, p := range got {
		list = append(list, p.Package+" "+p.Number+" "+p.Name+" "+p.Type)
	}
	want := []string{"LQFP48 3 PC14-OSC32_IN I/O", "UFQFPN48 3 PC14-OSC32_IN I/O", "LQFP64 3 PC14-OSC32_IN I/O", "LQFP64 4 VSS G"}
	if !slices.Equal(list, want) {
		t.Errorf("pins %q, want %q", list, want)
	}
}
