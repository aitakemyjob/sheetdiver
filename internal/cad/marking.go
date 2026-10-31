package cad

import (
	"math"
	"strconv"
	"strings"
)

// Marking: the part number printed on top of the body, as the maker
// marks it, in a stroke font. Each stroke is a thin white slab on the
// body's top face, so it shows in the 3D view, the editor and the STEP
// model alike. The strokes share the group "marking", which the editor
// shows as one shape.

// glyphs are the font's letters on a grid 4 wide and 6 high (y up), each
// a list of polylines ("x,y x,y …", separated by ";"). Characters not
// here are left out.
var glyphs = map[rune]string{
	'0': "0,0 4,0 4,6 0,6 0,0", '1': "1,5 2,6 2,0;1,0 3,0", '2': "0,6 4,6 4,3 0,3 0,0 4,0", '3': "0,6 4,6 4,0 0,0;0,3 4,3",
	'4': "0,6 0,3 4,3;4,6 4,0", '5': "4,6 0,6 0,3 4,3 4,0 0,0", '6': "4,6 0,6 0,0 4,0 4,3 0,3", '7': "0,6 4,6 1,0",
	'8': "0,0 4,0 4,6 0,6 0,0;0,3 4,3", '9': "0,0 4,0 4,6 0,6 0,3 4,3",
	'A': "0,0 0,4 2,6 4,4 4,0;0,3 4,3", 'B': "0,0 0,6 3,6 4,5 4,4 3,3 0,3;3,3 4,2 4,1 3,0 0,0", 'C': "4,5 3,6 1,6 0,5 0,1 1,0 3,0 4,1",
	'D': "0,0 0,6 3,6 4,5 4,1 3,0 0,0", 'E': "4,0 0,0 0,6 4,6;0,3 3,3", 'F': "0,0 0,6 4,6;0,3 3,3", 'G': "4,5 3,6 1,6 0,5 0,1 1,0 3,0 4,1 4,3 2,3",
	'H': "0,0 0,6;4,0 4,6;0,3 4,3", 'I': "1,0 3,0;2,0 2,6;1,6 3,6", 'J': "4,6 4,1 3,0 1,0 0,1", 'K': "0,0 0,6;4,6 0,2;1,3 4,0",
	'L': "0,6 0,0 4,0", 'M': "0,0 0,6 2,3 4,6 4,0", 'N': "0,0 0,6 4,0 4,6", 'O': "1,0 3,0 4,1 4,5 3,6 1,6 0,5 0,1 1,0",
	'P': "0,0 0,6 3,6 4,5 4,4 3,3 0,3", 'Q': "1,0 3,0 4,1 4,5 3,6 1,6 0,5 0,1 1,0;2,2 4,0", 'R': "0,0 0,6 3,6 4,5 4,4 3,3 0,3;1,3 4,0",
	'S': "4,5 3,6 1,6 0,5 0,4 1,3 3,3 4,2 4,1 3,0 1,0 0,1", 'T': "0,6 4,6;2,6 2,0", 'U': "0,6 0,1 1,0 3,0 4,1 4,6", 'V': "0,6 2,0 4,6",
	'W': "0,6 1,0 2,4 3,0 4,6", 'X': "0,0 4,6;0,6 4,0", 'Y': "0,6 2,3 4,6;2,3 2,0", 'Z': "0,6 4,6 0,0 4,0",
	'-': "1,3 3,3", '_': "0,0 4,0", '.': "2,0 2,0.6", '/': "0,0 4,6", '+': "2,1 2,5;0,3 4,3", ' ': "",
}

// The font's grid: a glyph 4 wide and 6 high, 6 from one to the next
// (two clear, so strokes as thick as a tenth of the height don't touch).
const (
	glyphW, glyphH, glyphAdvance = 4.0, 6.0, 6.0
	// markingMaxHeight is the tallest the marking is drawn, mm.
	markingMaxHeight = 1.2
	// markingMinHeight is the smallest marking worth drawing, mm.
	markingMinHeight = 0.35
	// markingGroup is the strokes' group, one shape in the editor.
	markingGroup = "marking"
	// markingDepth is how far the strokes sink into the body, and
	// markingProud how far they stand above its top, mm.
	markingDepth, markingProud = 0.015, 0.005
)

// markingBody is the top face a marking goes on: the body's rectangle
// (a custom package's from its pads) and ok when the family has a flat
// top to print on.
func markingBody(p Package) (Rect, bool) {
	switch p.Family {
	case "gullwing", "qfp", "dfn", "qfn", "bga", "dip":
		w, l := p.BodyWidth.Mid(), p.BodyLength.Mid()
		return centred(0, 0, w, l), w > 0 && l > 0
	case "custom":
		var ps problems
		_, body, _ := custom{}.Pads(p, &ps)
		return body, !ps.failed() && body.X1-body.X0 > 0 && body.Y1-body.Y0 > 0
	}
	return Rect{}, false
}

// Marking is a part number printed on the top of a package's body, as
// white strokes: along the body's longer side, centred, on the fewest
// lines that make it readable, clear of the pin 1 mark's corner. Nothing
// for packages with no flat top (TO-220s, radials) or too small to read
// it on (an 0402, a SOT-23).
func Marking(text string, p Package) []Solid {
	body, ok := markingBody(p)
	text = strings.ToUpper(strings.TrimSpace(text))
	if !ok || text == "" {
		return nil
	}
	runes := []rune(text)
	w, l := body.X1-body.X0, body.Y1-body.Y0
	mx, my := math.Max(0.3, 0.08*w), math.Max(0.3, 0.08*l)
	if l > w {
		mx, my = math.Max(0.3, 0.1*w), math.Max(0.3, 0.08*l)
	} else {
		mx, my = math.Max(0.3, 0.08*w), math.Max(0.3, 0.1*l)
	}
	areas := []Rect{{body.X0 + mx, body.Y0 + my, body.X1 - mx, body.Y1 - my}}
	if p.Family != "custom" {
		// The pin 1 mark sits in the top left corner: keep off it by
		// cutting the area under it or to its right, whichever leaves
		// the bigger letters.
		r := math.Min(0.25, 0.1*math.Min(w, l))
		clear := 0.25 + 2*r + 0.1
		under, right := areas[0], areas[0]
		under.Y0 = math.Max(under.Y0, body.Y0+clear)
		right.X0 = math.Max(right.X0, body.X0+clear)
		areas = []Rect{under, right}
	}
	var best markingLayout
	for _, a := range areas {
		if lay := layoutMarking(runes, a); lay.u > best.u {
			best = lay
		}
	}
	h := glyphH * best.u
	if h < markingMinHeight {
		return nil
	}
	u, lines, vertical := best.u, best.lines, best.vertical
	cx, cy := (best.area.X0+best.area.X1)/2, (best.area.Y0+best.area.Y1)/2
	z := height(p)
	stroke := math.Max(0.05, 0.09*h)
	blockH := ((glyphH+2)*float64(len(lines)) - 2) * u
	// at maps a point (x right, y up, from the text block's bottom left)
	// to the footprint (y down) about the area's centre: the text along
	// x or, turned to read up the longer side, along y.
	at := func(gx, gy float64) Pt {
		if vertical {
			return Pt{cx + blockH/2 - gy*u, cy - gx*u}
		}
		return Pt{cx + gx*u, cy + blockH/2 - gy*u}
	}
	var out []Solid
	for li, line := range lines {
		n := float64(len([]rune(line)))
		x0 := -(glyphAdvance*n - 1) / 2
		y0 := (glyphH + 2) * float64(len(lines)-1-li)
		for i, r := range line {
			for _, poly := range strokesOf(r) {
				for k := 0; k+1 < len(poly); k++ {
					a := at(x0+float64(i)*glyphAdvance+poly[k][0], y0+poly[k][1])
					b := at(x0+float64(i)*glyphAdvance+poly[k+1][0], y0+poly[k+1][1])
					// Sunk into the body but for a hair, as printed rather
					// than raised.
					s := slab(PartMark, a, b, stroke, z-markingDepth, z+markingProud)
					s.Group = markingGroup
					out = append(out, s)
				}
			}
		}
	}
	return out
}

// markingLayout is a marking's lines and letter size in an area.
type markingLayout struct {
	area     Rect
	lines    []string
	u        float64 // a grid unit, mm: the letters are glyphH of them
	vertical bool
}

// markingTargetHeight is the letter height a marking is readable at:
// the fewest lines reaching it are used, else the lines that come
// nearest.
const markingTargetHeight = 0.55

// layoutMarking lays a part number out in an area along its longer side,
// on one line, two or three.
func layoutMarking(runes []rune, area Rect) markingLayout {
	along, cross, vertical := area.X1-area.X0, area.Y1-area.Y0, false
	if cross > along {
		along, cross, vertical = cross, along, true
	}
	best := markingLayout{area: area, vertical: vertical}
	for k := 1; k <= 3 && k <= len(runes); k++ {
		lines := wrap(runes, k)
		longest := 0
		for _, ln := range lines {
			longest = max(longest, len([]rune(ln)))
		}
		// k lines of glyphH with 2 between across; the longest along.
		u := math.Min(cross/float64((glyphH+2)*k-2), along/(glyphAdvance*float64(longest)-1))
		u = math.Min(u, markingMaxHeight/glyphH)
		if u > best.u {
			best.lines, best.u = lines, u
		}
		if glyphH*u >= markingTargetHeight {
			break
		}
	}
	return best
}

// wrap cuts a part number into k lines of about equal length, after a
// "-" or "_" near the cut when there is one.
func wrap(runes []rune, k int) []string {
	var out []string
	rest := runes
	for i := k; i > 0; i-- {
		if i == 1 {
			out = append(out, string(rest))
			break
		}
		cut := (len(rest) + i - 1) / i
		for d := 0; d <= 1; d++ {
			for _, c := range []int{cut + d, cut - d} {
				if c > 0 && c < len(rest) && (rest[c-1] == '-' || rest[c-1] == '_') {
					cut = c
					d = 2
					break
				}
			}
		}
		out = append(out, string(rest[:cut]))
		rest = rest[cut:]
	}
	return out
}

// strokesOf are a glyph's polylines, in grid units.
func strokesOf(r rune) [][][2]float64 {
	var out [][][2]float64
	for _, line := range strings.Split(glyphs[r], ";") {
		var pts [][2]float64
		for _, pt := range strings.Fields(line) {
			xs, ys, _ := strings.Cut(pt, ",")
			x, _ := strconv.ParseFloat(xs, 64)
			y, _ := strconv.ParseFloat(ys, 64)
			pts = append(pts, [2]float64{x, y})
		}
		if len(pts) > 1 {
			out = append(out, pts)
		}
	}
	return out
}

// slab is a stroke from a to b (footprint coordinates, y down), w wide
// with square ends, between z0 and z1.
func slab(part string, a, b Pt, w, z0, z1 float64) Solid {
	dx, dy := b.X-a.X, b.Y-a.Y
	d := math.Hypot(dx, dy)
	if d == 0 {
		d, dx = 1, 1
	}
	dx, dy = dx/d*w/2, dy/d*w/2
	a, b = Pt{a.X - dx, a.Y - dy}, Pt{b.X + dx, b.Y + dy}
	nx, ny := -dy, dx
	return polySolid(part, []Pt{{a.X + nx, a.Y + ny}, {b.X + nx, b.Y + ny}, {b.X - nx, b.Y - ny}, {a.X - nx, a.Y - ny}}, z0, z1)
}

// polySolid is a convex polygon (footprint coordinates, y down) extruded
// from z0 to z1, its faces wound as prism's.
func polySolid(part string, pts []Pt, z0, z1 float64) Solid {
	n := len(pts)
	// Stored y up, the ring counter-clockwise as seen from above.
	area := 0.0
	for i := range pts {
		j := (i + 1) % n
		area += pts[i].X*(-pts[j].Y) - pts[j].X*(-pts[i].Y)
	}
	if area < 0 {
		for i, j := 0, n-1; i < j; i, j = i+1, j-1 {
			pts[i], pts[j] = pts[j], pts[i]
		}
	}
	s := Solid{Part: part}
	for _, z := range []float64{z0, z1} {
		for _, p := range pts {
			s.Verts = append(s.Verts, V3{Number(p.X), Number(-p.Y), Number(z)})
		}
	}
	bottom, top := make([]int, n), make([]int, n)
	for i := 0; i < n; i++ {
		bottom[i] = n - 1 - i
		top[i] = n + i
		j := (i + 1) % n
		s.Faces = append(s.Faces, []int{i, j, n + j, n + i})
	}
	s.Faces = append([][]int{bottom, top}, s.Faces...)
	return s
}
