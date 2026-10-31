// Package cad holds a part's CAD data in no particular tool's format: its
// schematic symbol, its footprint (land pattern) and its 3D body. They
// are generated from what SheetDiver knows about the part (its pin table,
// its package's dimensions and the datasheet's recommended land pattern)
// and written out by the format packages (KiCad, Eagle, STEP) for the
// user's own CAD library.
//
// Lengths are millimetres. A footprint is seen from the top with x to the
// right and y down, pin 1 at the top left, as KiCad draws them; a symbol
// has y up; a 3D body has z up from the board.
package cad

import (
	"fmt"
	"math"
	"strings"
)

// Dim is a package dimension as a datasheet's table gives it: min, nominal
// and max, any of which may be missing (0).
type Dim struct {
	Min float64 `json:"min,omitempty"`
	Nom float64 `json:"nom,omitempty"`
	Max float64 `json:"max,omitempty"`
}

// Set reports whether any value is given.
func (d Dim) Set() bool { return d.Min > 0 || d.Nom > 0 || d.Max > 0 }

// Lo is the least value: min, else nominal, else max.
func (d Dim) Lo() float64 { return first(d.Min, d.Nom, d.Max) }

// Hi is the greatest value: max, else nominal, else min.
func (d Dim) Hi() float64 { return first(d.Max, d.Nom, d.Min) }

// Mid is the nominal value, else the middle of min and max.
func (d Dim) Mid() float64 {
	if d.Nom > 0 {
		return d.Nom
	}
	return (d.Lo() + d.Hi()) / 2
}

// Tol is the tolerance range, max minus min (0 when only one is given).
func (d Dim) Tol() float64 { return d.Hi() - d.Lo() }

// String writes the dimension as min/nom/max.
func (d Dim) String() string {
	switch {
	case d.Min > 0 && d.Max > 0 && d.Nom > 0:
		return fmt.Sprintf("%s/%s/%s", mm(d.Min), mm(d.Nom), mm(d.Max))
	case d.Min > 0 && d.Max > 0:
		return fmt.Sprintf("%s–%s", mm(d.Min), mm(d.Max))
	}
	return mm(d.Mid())
}

// first is the first value that is set.
func first(vs ...float64) float64 {
	for _, v := range vs {
		if v > 0 {
			return v
		}
	}
	return 0
}

// mm writes a length without trailing zeros.
func mm(v float64) string {
	return strings.TrimRight(strings.TrimRight(fmt.Sprintf("%.3f", v), "0"), ".")
}

// Package is a package's dimensions as its outline drawing gives them,
// with the land pattern the datasheet recommends when it has one.
type Package struct {
	// Family is how the package is built. Surface mount: gullwing (SOIC,
	// SOP, TSSOP, MSOP, SOT-23), qfp, dfn (DFN, SON, two rows of no-lead
	// terminals), qfn, chip (two-terminal 0402, 0603… resistors,
	// capacitors, SOD) or bga. Through hole: dip, header (pin headers and
	// SIP, one or two rows), inline (TO-92, TO-220: one row of leads under
	// a body), radial (capacitors, LEDs) or axial (resistors, diodes).
	// custom is any other: its pads listed one by one.
	Family string `json:"family"`
	// Name is the package's name, e.g. SOT-23-6, WSON-10, QFN-32.
	Name string `json:"name,omitempty"`
	// Pins is the number of terminals, an exposed pad not counted.
	Pins int `json:"pins"`
	// PinsX is, on a quad package, the pins on each of the top and bottom
	// sides (the rest are on the left and right); 0 shares them evenly.
	PinsX int `json:"pinsX,omitempty"`
	// Pitch is the terminal pitch (e).
	Pitch float64 `json:"pitch,omitempty"`
	// BodyLength (D) is the body along the rows of terminals; BodyWidth
	// (E1 on a gull-wing, E on a no-lead) is across them.
	BodyLength Dim `json:"bodyLength,omitempty"`
	BodyWidth  Dim `json:"bodyWidth,omitempty"`
	// Height (A) is the seated height; Standoff (A1) the body's gap above
	// the board.
	Height   Dim `json:"height,omitempty"`
	Standoff Dim `json:"standoff,omitempty"`
	// Span (E) is a gull-wing's tip-to-tip span across the leads; for a
	// no-lead or chip package it is the body.
	Span Dim `json:"span,omitempty"`
	// SpanX is a quad package's span along the top and bottom sides when
	// it differs from Span.
	SpanX Dim `json:"spanX,omitempty"`
	// LeadLength (L) is the length of the lead's foot, or of a no-lead
	// terminal, or of a chip's end cap.
	LeadLength Dim `json:"leadLength,omitempty"`
	// LeadWidth (b) is the lead's width; for a BGA, the ball diameter.
	LeadWidth Dim `json:"leadWidth,omitempty"`
	// LeadThickness (c) is a gull-wing lead's thickness.
	LeadThickness Dim `json:"leadThickness,omitempty"`
	// ExposedPad is the thermal pad on the underside (D2 × E2).
	ExposedPad *Size `json:"exposedPad,omitempty"`
	// ExposedPadNumber is the exposed pad's number in the pin table
	// (default: one more than the pins).
	ExposedPadNumber string `json:"exposedPadNumber,omitempty"`
	// Layout names the terminal at each position when the package skips
	// some or numbers them its own way: per side, top to bottom on the
	// left and right ("" is no terminal); e.g. SOT-23 is left [1, "", 2],
	// right ["", 3, ""]. Quad packages list left (top to bottom), bottom
	// (left to right), right (bottom to top), top (right to left). A BGA
	// lists its missing balls in Depopulated instead.
	Layout *Layout `json:"layout,omitempty"`
	// Depopulated are a BGA's missing balls (A1, C3).
	Depopulated []string `json:"depopulated,omitempty"`
	// Rows and Columns are a BGA's grid.
	Rows    int `json:"rows,omitempty"`
	Columns int `json:"columns,omitempty"`
	// RowSpacing is the distance between a DIP's two rows of leads, or a
	// two-row header's (default: the pitch), centre to centre.
	RowSpacing Dim `json:"rowSpacing,omitempty"`
	// BodyOffset moves an inline package's body off its row of leads,
	// towards the back (a TO-220's tab side), in mm.
	BodyOffset float64 `json:"bodyOffset,omitempty"`
	// LeadBelow is how far through-hole leads stick out under the board,
	// for the 3D model (default 3 mm).
	LeadBelow float64 `json:"leadBelow,omitempty"`
	// Pads are a custom package's pads, from its drawing.
	Pads []PadSpec `json:"pads,omitempty"`
	// Land is the land pattern the datasheet recommends; it wins over the
	// one calculated from the package.
	Land *Land `json:"land,omitempty"`
	// Page is the datasheet page the outline drawing is on.
	Page int `json:"page,omitempty"`
	// Color is the body's colour: black (default), tan, brown, blue, grey.
	Color string `json:"color,omitempty"`
}

// Size is a width (x) and height (y).
type Size struct {
	W float64 `json:"w"`
	H float64 `json:"h"`
}

// Layout names the terminal at each position, per side.
type Layout struct {
	Left   []string `json:"left,omitempty"`
	Right  []string `json:"right,omitempty"`
	Bottom []string `json:"bottom,omitempty"`
	Top    []string `json:"top,omitempty"`
}

// Land is a land pattern as a datasheet's recommendation gives it.
type Land struct {
	// PadLength is a pad's length across the row (along the lead); PadWidth
	// along the row.
	PadLength float64 `json:"padLength"`
	PadWidth  float64 `json:"padWidth"`
	// RowSpan is the distance between the centres of the two rows of
	// pads (C); RowSpanX the same between the top and bottom rows of a
	// quad package when it differs. A drawing that gives the outer span
	// (Z) or the gap between rows (G) gives C as (Z+G)/2 or Z-PadLength.
	RowSpan  float64 `json:"rowSpan"`
	RowSpanX float64 `json:"rowSpanX,omitempty"`
	// Pitch is the pad pitch when it differs from the package's.
	Pitch float64 `json:"pitch,omitempty"`
	// ExposedPad is the thermal pad's land when it differs from the pad.
	ExposedPad *Size `json:"exposedPad,omitempty"`
	// Paste is the fraction of the thermal pad covered by paste windows
	// (0.5 to 0.8; 0 for the default).
	Paste float64 `json:"paste,omitempty"`
	// Vias are thermal vias in the exposed pad: a grid of Columns × Rows
	// at Pitch with the given drill.
	Vias *Vias `json:"vias,omitempty"`
	// Round makes the pads round (a BGA's always are).
	Round bool `json:"round,omitempty"`
	// Drill is a through-hole pad's finished hole; its pad is then
	// padLength × padWidth (the same for round pads).
	Drill float64 `json:"drill,omitempty"`
	// Page is the datasheet page it is drawn on.
	Page int `json:"page,omitempty"`
}

// PadSpec is one pad of a custom package, as its drawing gives it, in
// footprint coordinates: x right, y down, from the package's centre.
type PadSpec struct {
	// Number is the pin it is; pads with the same number are one pin
	// (a shield's tabs). Empty for a mounting hole.
	Number string  `json:"number,omitempty"`
	X      float64 `json:"x"`
	Y      float64 `json:"y"`
	W      float64 `json:"w"`
	H      float64 `json:"h,omitempty"`
	// Shape is rect, roundrect, oval or circle (default rect, circle when
	// it has a hole and no height).
	Shape string `json:"shape,omitempty"`
	// Drill makes it a plated through hole; with Kind npth, a bare hole.
	Drill float64 `json:"drill,omitempty"`
	// Kind is smd, thru or npth (default: thru with a drill, else smd).
	Kind string `json:"kind,omitempty"`
}

// Vias is a grid of thermal vias.
type Vias struct {
	Columns int     `json:"columns"`
	Rows    int     `json:"rows"`
	Pitch   float64 `json:"pitch"`
	Drill   float64 `json:"drill"`
}

// Problem is something wrong or doubtful about a package or what was
// made from it.
type Problem struct {
	// Level is error (it can't be drawn as given) or warning.
	Level string `json:"level"`
	Text  string `json:"text"`
}

// Problem levels.
const (
	Error   = "error"
	Warning = "warning"
)

// problems collects problems.
type problems []Problem

// errf adds an error.
func (p *problems) errf(format string, a ...any) {
	*p = append(*p, Problem{Error, fmt.Sprintf(format, a...)})
}

// warnf adds a warning.
func (p *problems) warnf(format string, a ...any) {
	*p = append(*p, Problem{Warning, fmt.Sprintf(format, a...)})
}

// failed reports whether there is an error.
func (p problems) failed() bool {
	for _, x := range p {
		if x.Level == Error {
			return true
		}
	}
	return false
}

// Err is the first error as an error, or nil.
func Err(ps []Problem) error {
	var msgs []string
	for _, p := range ps {
		if p.Level == Error {
			msgs = append(msgs, p.Text)
		}
	}
	if len(msgs) == 0 {
		return nil
	}
	return fmt.Errorf("%s", strings.Join(msgs, "; "))
}

// ceilTo rounds up to a grid.
func ceilTo(v, grid float64) float64 { return math.Ceil(v/grid-1e-9) * grid }

// floorTo rounds down to a grid.
func floorTo(v, grid float64) float64 { return math.Floor(v/grid+1e-9) * grid }

// Number keeps lengths tidy: four decimals, as KiCad writes them.
func Number(v float64) float64 { return math.Round(v*1e4) / 1e4 }
