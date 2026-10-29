package extract

import (
	"slices"
	"testing"
)

func TestTitledFigures(t *testing.T) {
	// Linear's Typical Performance Characteristics: a grid of plots with a
	// bold title of one or two lines right above each, no figure numbers.
	// Two plots in a column touch and merge into one cluster; the title
	// inside it splits them. A plot without a title is nothing; a scope
	// shot is a waveform.
	s := newSynth(2).header("LTC3536").
		bold(0, 45, 84, 18, "Typical Performance Characteristics").
		bold(0, 367, 89, 10, "TA = 25°C, VIN = VOUT = 3.3 V unless otherwise noted.").
		bold(0, 50, 121, 10, "Efficiency vs Load Current").
		text(0, 40, 150, 7, "100").
		drawing(0, 48, 147, 183, 280).
		bold(0, 250, 121, 10, "No-Load Quiescent Current").
		bold(0, 250, 132, 10, "in Burst Mode (MODE = VIN)").
		drawing(0, 248, 147, 383, 280).
		drawing(0, 440, 147, 575, 280).
		bold(0, 50, 323, 10, "Load Step 0A to 1A").
		drawing(0, 48, 349, 183, 482).
		drawing(0, 48, 280, 183, 349). // the two left plots joined by a stray path
		bold(1, 46, 84, 18, "Pin Functions")
	r := Analyze(s.doc, "ltc.pdf")
	var got []string
	for _, f := range r.Figures {
		got = append(got, f.Kind+": "+f.Caption)
	}
	want := []string{
		"graph: Efficiency vs Load Current",
		"graph: No-Load Quiescent Current in Burst Mode (MODE = VIN)",
		"waveform: Load Step 0A to 1A",
	}
	if !slices.Equal(got, want) {
		t.Fatalf("figures = %q\nwant      %q", got, want)
	}
	if f := r.Figures[0]; f.XAxis != "Load Current" || f.YAxis != "Efficiency" || f.Box.Y0 > 121 || f.Box.Y1 < 280 || f.Box.Y1 > 325 {
		t.Errorf("the first graph: %+v", f)
	}
	if f := r.Figures[2]; f.Box.Y0 < 300 || f.Box.Y1 < 482 {
		t.Errorf("the split plot is the one under its title: %+v", f)
	}
}
