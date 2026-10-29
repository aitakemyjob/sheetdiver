package extract

import (
	"testing"

	"github.com/aitakemyjob/sheetdiver/internal/pdf"
)

func TestFiguresInsideAPageForm(t *testing.T) {
	// Linear sets each page as one form spanning most of it, with the
	// columns' drawings inside; a caption's figure is the drawing in its
	// own column, not the form and not the other column's drawing.
	s := newSynth(2).header("LTC3536").
		bold(0, 46, 84, 18, "Applications Information").
		bold(1, 46, 84, 18, "Package Description")
	for i := 0; i < 8; i++ {
		s.text(0, 46, 330+float64(i)*14, 10, "left column body text about the converter's loop and its compensation")
		s.text(0, 315, 430+float64(i)*14, 10, "right column body text about the converter's loop and its compensation")
	}
	s.drawing(0, 60, 120, 280, 300).
		bold(0, 90, 312, 10, "Figure 1. Power Stage Schematic").
		drawing(0, 330, 120, 560, 400).
		bold(0, 380, 412, 10, "Figure 2. Buck-Boost Converter Bode Plot")
	s.doc.Pages[0].Graphics = append(s.doc.Pages[0].Graphics, pdf.Graphic{Box: pdf.Rect{X0: 44, Y0: 51, X1: 567, Y1: 775}, Kind: "form"})
	r := Analyze(s.doc, "ltc.pdf")
	figs := map[string]Figure{}
	for _, f := range r.Figures {
		figs[f.Number] = f
	}
	if len(figs) != 2 {
		t.Fatalf("figures: %+v", r.Figures)
	}
	if f := figs["1"]; f.Box.X1 > 306 || f.Box.Y0 < 100 || f.Box.Y1 > 330 || f.Kind != FigSchematic {
		t.Errorf("figure 1 stays in its column by its drawing: %+v", f)
	}
	if f := figs["2"]; f.Box.X0 < 306 || f.Box.Y1 > 430 || f.Kind != FigGraph {
		t.Errorf("figure 2 (a Bode plot, a graph) stays in its column: %+v", f)
	}
}
