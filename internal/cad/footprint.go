package cad

import (
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
)

// Pt is a point.
type Pt struct {
	X float64 `json:"x"`
	Y float64 `json:"y"`
}

// Rect is an axis-aligned rectangle by its corners.
type Rect struct {
	X0 float64 `json:"x0"`
	Y0 float64 `json:"y0"`
	X1 float64 `json:"x1"`
	Y1 float64 `json:"y1"`
}

// centred is a rectangle of the given size centred on (x, y).
func centred(x, y, w, h float64) Rect { return Rect{x - w/2, y - h/2, x + w/2, y + h/2} }

// union is the smallest rectangle holding both; an empty r is replaced.
func (r Rect) union(o Rect) Rect {
	if r == (Rect{}) {
		return o
	}
	return Rect{math.Min(r.X0, o.X0), math.Min(r.Y0, o.Y0), math.Max(r.X1, o.X1), math.Max(r.Y1, o.Y1)}
}

// grow pushes every side out by d.
func (r Rect) grow(d float64) Rect { return Rect{r.X0 - d, r.Y0 - d, r.X1 + d, r.Y1 + d} }

// overlaps reports whether two rectangles share area.
func (r Rect) overlaps(o Rect) bool { return r.X0 < o.X1 && o.X0 < r.X1 && r.Y0 < o.Y1 && o.Y0 < r.Y1 }

// Pad kinds.
const (
	PadSMD   = "smd"   // a surface-mount pad on the top copper, mask and paste
	PadThru  = "thru"  // a plated hole: a thermal via in an exposed pad
	PadPaste = "paste" // a paste window with no copper of its own
)

// Pad is a footprint pad.
type Pad struct {
	Number string `json:"number"`
	Kind   string `json:"kind"`
	// Shape is rect, roundrect or circle.
	Shape string  `json:"shape"`
	X     float64 `json:"x"`
	Y     float64 `json:"y"`
	W     float64 `json:"w"`
	H     float64 `json:"h"`
	Drill float64 `json:"drill,omitempty"`
	// NoPaste leaves paste off the pad (its paste is in windows of its own).
	NoPaste bool `json:"noPaste,omitempty"`
}

// Rect is the pad's copper.
func (p Pad) Rect() Rect { return centred(p.X, p.Y, p.W, p.H) }

// Layers of a footprint's drawing.
const (
	LayerSilk      = "silk"
	LayerFab       = "fab"
	LayerCourtyard = "courtyard"
)

// Line is a straight line on a layer.
type Line struct {
	Layer string  `json:"layer"`
	A     Pt      `json:"a"`
	B     Pt      `json:"b"`
	Width float64 `json:"width"`
}

// Poly is a closed, filled polygon on a layer (pin 1's mark).
type Poly struct {
	Layer string `json:"layer"`
	Pts   []Pt   `json:"pts"`
}

// Footprint is a land pattern with its silkscreen, fabrication outline and
// courtyard.
type Footprint struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Pads        []Pad  `json:"pads"`
	Lines       []Line `json:"lines"`
	Polys       []Poly `json:"polys,omitempty"`
	// Body is the package body's outline (the fab layer).
	Body Rect `json:"body"`
	// Courtyard is the keep-out around pads and body.
	Courtyard Rect `json:"courtyard"`
	// From says where the pads came from: the datasheet's recommended
	// land pattern, or calculated from the package (IPC-7351 nominal).
	From string `json:"from"`
}

// Where pads came from.
const (
	FromDatasheet  = "datasheet land pattern"
	FromCalculated = "calculated (IPC-7351 nominal)"
)

// Family makes the pads and 3D body of one kind of package. New package
// kinds are new families; the footprint's silkscreen, courtyard and
// checks are the same for all.
type Family interface {
	// Name is the family's name in a Package.
	Name() string
	// Pads are the package's pads and its body's outline.
	Pads(p Package, ps *problems) (pads []Pad, body Rect, from string)
	// Solids are its 3D body.
	Solids(p Package) []Solid
}

// families are the known package families by name.
var families = map[string]Family{}

// Register adds a family.
func Register(f Family) { families[f.Name()] = f }

// FamilyOf is the named family.
func FamilyOf(name string) (Family, bool) {
	f, ok := families[strings.ToLower(strings.TrimSpace(name))]
	return f, ok
}

// Families are the families' names.
func Families() []string {
	var out []string
	for n := range families {
		out = append(out, n)
	}
	sort.Strings(out)
	return out
}

// MakeFootprint draws a package's footprint, with what is wrong or
// doubtful about it.
func MakeFootprint(name string, p Package) (Footprint, []Problem) {
	var ps problems
	f, ok := FamilyOf(p.Family)
	if !ok {
		ps.errf("unknown package family %q (one of %s)", p.Family, strings.Join(Families(), ", "))
		return Footprint{Name: name}, ps
	}
	pads, body, from := f.Pads(p, &ps)
	if ps.failed() {
		return Footprint{Name: name}, ps
	}
	fp := Footprint{Name: name, Pads: pads, Body: body, From: from}
	fp.Description = describe(p, from)
	fp.Courtyard = courtyard(pads, body)
	fp.Lines = append(fp.Lines, fabOutline(body)...)
	fp.Lines = append(fp.Lines, silkOutline(body, pads)...)
	fp.Lines = append(fp.Lines, rectLines(LayerCourtyard, fp.Courtyard, 0.05)...)
	if mark, ok := pin1Mark(pads, body); ok {
		fp.Polys = append(fp.Polys, mark)
	}
	checkPads(pads, &ps)
	return fp, ps
}

// describe says what a footprint is.
func describe(p Package, from string) string {
	var b strings.Builder
	b.WriteString(strings.ToUpper(p.Family))
	if p.Name != "" {
		b.Reset()
		b.WriteString(p.Name)
	}
	fmt.Fprintf(&b, ", %d pins", p.Pins)
	if p.Pitch > 0 {
		fmt.Fprintf(&b, ", pitch %s mm", mm(p.Pitch))
	}
	if p.BodyLength.Set() && p.BodyWidth.Set() {
		fmt.Fprintf(&b, ", body %s × %s mm", mm(p.BodyLength.Mid()), mm(p.BodyWidth.Mid()))
	}
	if p.ExposedPad != nil {
		fmt.Fprintf(&b, ", exposed pad %s × %s mm", mm(p.ExposedPad.W), mm(p.ExposedPad.H))
	}
	fmt.Fprintf(&b, "; pads %s", from)
	if p.Page > 0 {
		fmt.Fprintf(&b, "; outline on datasheet page %d", p.Page)
	}
	b.WriteString(". Made by SheetDiver.")
	return b.String()
}

// courtyard is the pads and body grown by the IPC nominal excess
// (0.25 mm), on a 0.05 mm grid.
func courtyard(pads []Pad, body Rect) Rect {
	r := body
	for _, p := range pads {
		r = r.union(p.Rect())
	}
	r = r.grow(0.25)
	return Rect{Number(floorTo(r.X0, 0.05)), Number(floorTo(r.Y0, 0.05)), Number(ceilTo(r.X1, 0.05)), Number(ceilTo(r.Y1, 0.05))}
}

// rectLines are a rectangle's four sides.
func rectLines(layer string, r Rect, w float64) []Line {
	return []Line{
		{layer, Pt{r.X0, r.Y0}, Pt{r.X1, r.Y0}, w},
		{layer, Pt{r.X1, r.Y0}, Pt{r.X1, r.Y1}, w},
		{layer, Pt{r.X1, r.Y1}, Pt{r.X0, r.Y1}, w},
		{layer, Pt{r.X0, r.Y1}, Pt{r.X0, r.Y0}, w},
	}
}

// fabOutline is the body with pin 1's corner cut.
func fabOutline(r Rect) []Line {
	c := math.Min(1, 0.25*math.Min(r.X1-r.X0, r.Y1-r.Y0))
	pts := []Pt{{r.X0 + c, r.Y0}, {r.X1, r.Y0}, {r.X1, r.Y1}, {r.X0, r.Y1}, {r.X0, r.Y0 + c}}
	var out []Line
	for i := range pts {
		out = append(out, Line{LayerFab, pts[i], pts[(i+1)%len(pts)], 0.1})
	}
	return out
}

// silkOutline is the body's outline just outside it, broken where it would
// come within 0.2 mm of a pad's copper.
func silkOutline(body Rect, pads []Pad) []Line {
	const w = 0.12
	r := body.grow(0.11)
	var keep []Rect
	for _, p := range pads {
		if p.Kind == PadSMD {
			keep = append(keep, p.Rect().grow(0.2+w/2))
		}
	}
	var out []Line
	for _, l := range rectLines(LayerSilk, r, w) {
		out = append(out, clip(l, keep)...)
	}
	return out
}

// clip removes the parts of an axis-aligned line inside any of the
// rectangles, dropping pieces under 0.2 mm.
func clip(l Line, keep []Rect) []Line {
	horizontal := l.A.Y == l.B.Y
	lo, hi, at := math.Min(l.A.X, l.B.X), math.Max(l.A.X, l.B.X), l.A.Y
	if !horizontal {
		lo, hi, at = math.Min(l.A.Y, l.B.Y), math.Max(l.A.Y, l.B.Y), l.A.X
	}
	spans := [][2]float64{{lo, hi}}
	for _, k := range keep {
		a, b, in := k.X0, k.X1, at > k.Y0 && at < k.Y1
		if !horizontal {
			a, b, in = k.Y0, k.Y1, at > k.X0 && at < k.X1
		}
		if !in {
			continue
		}
		var next [][2]float64
		for _, s := range spans {
			if b <= s[0] || a >= s[1] {
				next = append(next, s)
				continue
			}
			if a > s[0] {
				next = append(next, [2]float64{s[0], a})
			}
			if b < s[1] {
				next = append(next, [2]float64{b, s[1]})
			}
		}
		spans = next
	}
	var out []Line
	for _, s := range spans {
		if s[1]-s[0] < 0.2 {
			continue
		}
		if horizontal {
			out = append(out, Line{l.Layer, Pt{s[0], at}, Pt{s[1], at}, l.Width})
		} else {
			out = append(out, Line{l.Layer, Pt{at, s[0]}, Pt{at, s[1]}, l.Width})
		}
	}
	return out
}

// pin1Mark is a small triangle pointing at pin 1 from outside the body, on
// the side away from the other pads.
func pin1Mark(pads []Pad, body Rect) (Poly, bool) {
	var p1 *Pad
	for i := range pads {
		if pads[i].Number == "1" || pads[i].Number == "A1" {
			p1 = &pads[i]
			break
		}
	}
	if p1 == nil || len(pads) <= 2 {
		return Poly{}, false
	}
	const s = 0.3
	r := p1.Rect()
	switch {
	case r.X1 < body.X0+0.5 && r.X0 < body.X0: // a pad on the left side: point right, above it
		x, y := r.X0+s, math.Min(r.Y0, body.Y0)-0.25
		return Poly{LayerSilk, []Pt{{x, y}, {x - s, y - s*1.2}, {x + s, y - s*1.2}}}, true
	default: // under the body (BGA): point at the corner from outside it
		x, y := body.X0-0.25, body.Y0-0.25
		return Poly{LayerSilk, []Pt{{x, y}, {x - s*1.4, y}, {x, y - s*1.4}}}, true
	}
}

// checkPads warns where pads come too close to each other for a solder
// mask web (0.1 mm) and errors where they overlap.
func checkPads(pads []Pad, ps *problems) {
	for i := range pads {
		for j := i + 1; j < len(pads); j++ {
			a, b := pads[i], pads[j]
			if a.Kind != PadSMD || b.Kind != PadSMD || a.Number == b.Number {
				continue
			}
			ra, rb := a.Rect(), b.Rect()
			if ra.overlaps(rb) {
				ps.errf("pads %s and %s overlap", a.Number, b.Number)
				return
			}
			if ra.grow(0.05).overlaps(rb.grow(0.05)) {
				ps.warnf("pads %s and %s are under 0.1 mm apart: no room for a solder mask web", a.Number, b.Number)
				return
			}
		}
	}
}

// fillets are IPC-7351 solder fillet goals: toe, heel and side.
type fillets struct{ toe, heel, side float64 }

// Fabrication and placement tolerances the IPC equations add (RMS).
const (
	fabTol   = 0.05
	placeTol = 0.05
)

// ipcLand is the IPC-7351 pad for a row of terminals: span is tip to tip
// across both rows, lead the terminal's length, width its width. It
// returns the pad's length (across the row), width (along it) and the
// distance between the rows' pad centres.
func ipcLand(span, lead, width Dim, j fillets) (padL, padW, rowSpan float64) {
	f := fabTol*fabTol + placeTol*placeTol
	z := span.Lo() + 2*j.toe + math.Sqrt(span.Tol()*span.Tol()+f)
	sMax := span.Hi() - 2*lead.Lo()
	sTol := math.Sqrt(span.Tol()*span.Tol() + 2*lead.Tol()*lead.Tol())
	g := sMax - 2*j.heel - math.Sqrt(sTol*sTol+f)
	x := width.Lo() + 2*j.side + math.Sqrt(width.Tol()*width.Tol()+f)
	z, g, x = ceilTo(z, 0.05), floorTo(g, 0.05), ceilTo(x, 0.01)
	if g < 0 {
		g = 0
	}
	return Number((z - g) / 2), Number(x), Number((z + g) / 2)
}

// sideCounts are the pins per side of a dual (left, right) or quad (left,
// bottom, right, top) package.
func sideCounts(p Package, quad bool, ps *problems) []int {
	if !quad {
		if p.Pins%2 != 0 && p.Layout == nil {
			ps.errf("%d pins don't split into two rows: give a layout", p.Pins)
		}
		return []int{p.Pins / 2, p.Pins / 2}
	}
	x := p.PinsX
	if x == 0 {
		x = p.Pins / 4
	}
	y := (p.Pins - 2*x) / 2
	if 2*x+2*y != p.Pins && p.Layout == nil {
		ps.errf("%d pins don't split into four sides (pinsX %d): give a layout", p.Pins, x)
	}
	return []int{y, x, y, x}
}

// numbering names the terminals at each position of each side: the
// layout's, else counter-clockwise from pin 1 at the top left.
func numbering(p Package, counts []int) [][]string {
	if l := p.Layout; l != nil {
		if len(counts) == 2 {
			return [][]string{l.Left, l.Right}
		}
		return [][]string{l.Left, l.Bottom, l.Right, l.Top}
	}
	out := make([][]string, len(counts))
	n := 1
	for s, c := range counts {
		out[s] = make([]string, c)
		for i := 0; i < c; i++ {
			out[s][i] = strconv.Itoa(n)
			n++
		}
	}
	if len(counts) == 2 { // the right side is numbered bottom to top
		reverse(out[1])
	}
	return out
}

// reverse reverses a slice in place.
func reverse(s []string) {
	for i, j := 0, len(s)-1; i < j; i, j = i+1, j-1 {
		s[i], s[j] = s[j], s[i]
	}
}

// Sides of a package, as slots name them.
const (
	sideLeft = iota
	sideBottom
	sideRight
	sideTop
)

// slot is a terminal's place: its side and its distance along it from
// the side's middle, in footprint coordinates (x right, y down).
type slot struct {
	num  string
	side int
	at   float64
}

// slots places the terminals of each side. Sides are left and right
// (dual) or left, bottom, right, top (quad); on the left and right of a
// dual they are listed top to bottom, on a quad counter-clockwise: the
// left top to bottom, the bottom left to right, the right bottom to top
// and the top right to left.
func slots(names [][]string, pitch float64) []slot {
	var out []slot
	for s, side := range names {
		k := float64(len(side))
		for i, n := range side {
			if n == "" {
				continue
			}
			at := (float64(i) - (k-1)/2) * pitch
			sl := slot{num: n, side: s, at: at}
			switch {
			case len(names) == 2 && s == 1:
				sl.side = sideRight
			case s == sideRight:
				sl.at = -at
			case s == sideTop:
				sl.at = -at
			}
			out = append(out, sl)
		}
	}
	return out
}

// rowPads places a pad at each slot: padL across the side, padW along it,
// the left and right rows' centres spanY apart and the top and bottom
// rows' spanX.
func rowPads(sl []slot, padL, padW, spanY, spanX float64, shape string) []Pad {
	var out []Pad
	for _, s := range sl {
		var p Pad
		switch s.side {
		case sideLeft:
			p = Pad{X: -spanY / 2, Y: s.at, W: padL, H: padW}
		case sideRight:
			p = Pad{X: spanY / 2, Y: s.at, W: padL, H: padW}
		case sideBottom:
			p = Pad{X: s.at, Y: spanX / 2, W: padW, H: padL}
		default:
			p = Pad{X: s.at, Y: -spanX / 2, W: padW, H: padL}
		}
		p.Number, p.Kind, p.Shape = s.num, PadSMD, shape
		p.X, p.Y = Number(p.X), Number(p.Y)
		out = append(out, p)
	}
	return out
}

// exposedPad adds the thermal pad with its paste windows and vias.
func exposedPad(p Package, pads []Pad) []Pad {
	ep := p.ExposedPad
	land := p.Land
	if land != nil && land.ExposedPad != nil {
		ep = land.ExposedPad
	}
	if ep == nil || ep.W <= 0 || ep.H <= 0 {
		return pads
	}
	num := p.ExposedPadNumber
	if num == "" {
		num = strconv.Itoa(p.Pins + 1)
	}
	pad := Pad{Number: num, Kind: PadSMD, Shape: "rect", W: Number(ep.W), H: Number(ep.H)}
	// Paste in windows covering about 65% of a pad over 1.6 mm a side, so
	// the part doesn't float on it.
	frac := 0.65
	if land != nil && land.Paste > 0 {
		frac = land.Paste
	}
	nx, ny := int(math.Ceil(ep.W/1.6)), int(math.Ceil(ep.H/1.6))
	var windows []Pad
	if nx*ny > 1 {
		pad.NoPaste = true
		k := math.Sqrt(frac)
		w, h := ep.W/float64(nx), ep.H/float64(ny)
		for i := 0; i < nx; i++ {
			for j := 0; j < ny; j++ {
				x, y := -ep.W/2+(float64(i)+0.5)*w, -ep.H/2+(float64(j)+0.5)*h
				windows = append(windows, Pad{Kind: PadPaste, Shape: "roundrect", X: Number(x), Y: Number(y), W: Number(w * k), H: Number(h * k)})
			}
		}
	}
	pads = append(pads, pad)
	pads = append(pads, windows...)
	if land != nil && land.Vias != nil && land.Vias.Columns > 0 && land.Vias.Rows > 0 {
		v := land.Vias
		drill := v.Drill
		if drill <= 0 {
			drill = 0.3
		}
		for i := 0; i < v.Columns; i++ {
			for j := 0; j < v.Rows; j++ {
				x := (float64(i) - float64(v.Columns-1)/2) * v.Pitch
				y := (float64(j) - float64(v.Rows-1)/2) * v.Pitch
				pads = append(pads, Pad{Number: num, Kind: PadThru, Shape: "circle", X: Number(x), Y: Number(y), W: Number(drill + 0.2), H: Number(drill + 0.2), Drill: drill})
			}
		}
	}
	return pads
}
