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
	markingMaxHeight = 2.0
	// markingMinHeight is the smallest marking worth drawing, mm.
	markingMinHeight = 0.45
	// markingGroup is the strokes' group, one shape in the editor.
	markingGroup = "marking"
)

// Marking is a part number printed on the top of a package's body, as
// white strokes: along the body's longer side, centred, as large as the
// body allows up to 2 mm high. Nothing for packages with no flat top
// (TO-220s, radials) or too small to read it on (an 0402).
func Marking(text string, p Package) []Solid {
	switch p.Family {
	case "gullwing", "qfp", "dfn", "qfn", "bga", "dip":
	default:
		return nil
	}
	text = strings.ToUpper(strings.TrimSpace(text))
	w, l := p.BodyWidth.Mid(), p.BodyLength.Mid()
	if text == "" || w <= 0 || l <= 0 {
		return nil
	}
	n := float64(len([]rune(text)))
	along, cross, vertical := w, l, false
	if l > w {
		along, cross, vertical = l, w, true
	}
	along -= 2 * math.Max(0.3, 0.08*along)
	cross -= 2 * math.Max(0.3, 0.1*cross)
	u := math.Min(cross/2/glyphH, along/(glyphAdvance*n-1))
	h := glyphH * u
	if h > markingMaxHeight {
		h = markingMaxHeight
		u = h / glyphH
	}
	if h < markingMinHeight {
		return nil
	}
	z := height(p)
	stroke := math.Max(0.06, 0.12*h)
	x0 := -(glyphAdvance*n - 1) * u / 2
	// at maps a glyph point (x right, y up, from the text's bottom left)
	// to the footprint (y down), the text along x or, turned to read
	// up the longer side, along y.
	at := func(gx, gy float64) Pt {
		if vertical {
			return Pt{h/2 - gy*u, -(x0 + gx*u)}
		}
		return Pt{x0 + gx*u, h/2 - gy*u}
	}
	var out []Solid
	for i, r := range text {
		for _, poly := range strokesOf(r) {
			for k := 0; k+1 < len(poly); k++ {
				a := at(float64(i)*glyphAdvance+poly[k][0], poly[k][1])
				b := at(float64(i)*glyphAdvance+poly[k+1][0], poly[k+1][1])
				s := slab(PartMark, a, b, stroke, z, z+0.02)
				s.Group = markingGroup
				out = append(out, s)
			}
		}
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
