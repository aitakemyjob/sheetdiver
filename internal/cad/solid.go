package cad

import "math"

// V3 is a point in 3D: x right, y up (away from pin 1's row, as 3D
// tools see a footprint from above), z up from the board.
type V3 [3]float64

// Solid is a closed, convex polyhedron with flat faces: a package body, a
// lead, a pad. Faces list their vertices counter-clockwise seen from
// outside.
type Solid struct {
	// Part says what it is (body, lead, pad, mark), which sets its colour.
	Part  string  `json:"part"`
	Color string  `json:"color,omitempty"`
	Verts []V3    `json:"verts"`
	Faces [][]int `json:"faces"`
}

// What a solid is.
const (
	PartBody = "body"
	PartLead = "lead"
	PartPad  = "pad"
	PartMark = "mark"
)

// RGB is a solid's colour, from its part and the package's colour.
func (s Solid) RGB() [3]float64 {
	switch s.Part {
	case PartLead, PartPad:
		return [3]float64{0.82, 0.82, 0.84}
	case PartMark:
		return [3]float64{0.95, 0.95, 0.95}
	}
	switch s.Color {
	case "tan":
		return [3]float64{0.76, 0.66, 0.48}
	case "brown":
		return [3]float64{0.45, 0.30, 0.18}
	case "blue":
		return [3]float64{0.15, 0.25, 0.55}
	case "grey", "gray":
		return [3]float64{0.40, 0.40, 0.42}
	}
	return [3]float64{0.12, 0.12, 0.13}
}

// box is a solid between two corners given in footprint coordinates (y
// down), stored with y up.
func box(part string, x0, y0, z0, x1, y1, z1 float64) Solid {
	x0, x1 = math.Min(x0, x1), math.Max(x0, x1)
	ya, yb := math.Min(-y0, -y1), math.Max(-y0, -y1)
	z0, z1 = math.Min(z0, z1), math.Max(z0, z1)
	v := []V3{
		{x0, ya, z0}, {x1, ya, z0}, {x1, yb, z0}, {x0, yb, z0},
		{x0, ya, z1}, {x1, ya, z1}, {x1, yb, z1}, {x0, yb, z1},
	}
	for i := range v {
		for k := range v[i] {
			v[i][k] = Number(v[i][k])
		}
	}
	return Solid{Part: part, Verts: v, Faces: [][]int{
		{0, 3, 2, 1}, {4, 5, 6, 7}, // bottom, top
		{0, 1, 5, 4}, {1, 2, 6, 5}, {2, 3, 7, 6}, {3, 0, 4, 7}, // sides
	}}
}

// prism is an n-sided upright prism (a pin 1 dimple, a ball) centred on
// (x, y) in footprint coordinates.
func prism(part string, x, y, r, z0, z1 float64, n int) Solid {
	s := Solid{Part: part}
	for _, z := range []float64{z0, z1} {
		for i := 0; i < n; i++ {
			a := 2 * math.Pi * (float64(i) + 0.5) / float64(n)
			s.Verts = append(s.Verts, V3{Number(x + r*math.Cos(a)), Number(-y + r*math.Sin(a)), Number(z)})
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

// colored sets the package's colour on its body solids.
func colored(ss []Solid, color string) []Solid {
	for i := range ss {
		ss[i].Color = color
	}
	return ss
}

// MakeSolids builds a package's 3D body.
func MakeSolids(p Package) ([]Solid, []Problem) {
	var ps problems
	f, ok := FamilyOf(p.Family)
	if !ok {
		ps.errf("unknown package family %q", p.Family)
		return nil, ps
	}
	if !p.Height.Set() {
		ps.warnf("no height (A) given: the body is drawn %s mm tall", mm(defaultHeight(p)))
	}
	return colored(f.Solids(p), p.Color), ps
}

// defaultHeight is a family's usual height when none is given.
func defaultHeight(p Package) float64 {
	switch p.Family {
	case "dfn", "qfn":
		return 0.8
	case "chip":
		return 0.5
	case "bga":
		return 1.0
	case "dip", "inline":
		return 4.5
	case "header":
		return 2.5
	case "radial":
		return 7
	case "axial":
		return p.BodyWidth.Mid() + p.Standoff.Mid()
	case "custom":
		return 2
	}
	return 1.2
}

// height is the package's seated height.
func height(p Package) float64 {
	if p.Height.Set() {
		return p.Height.Mid()
	}
	return defaultHeight(p)
}

// Bounds is the box around solids: min and max corners.
func Bounds(ss []Solid) (lo, hi V3) {
	lo = V3{math.Inf(1), math.Inf(1), math.Inf(1)}
	hi = V3{math.Inf(-1), math.Inf(-1), math.Inf(-1)}
	for _, s := range ss {
		for _, v := range s.Verts {
			for k := 0; k < 3; k++ {
				lo[k] = math.Min(lo[k], v[k])
				hi[k] = math.Max(hi[k], v[k])
			}
		}
	}
	return lo, hi
}
