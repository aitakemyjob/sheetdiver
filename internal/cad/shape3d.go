package cad

import (
	"fmt"
	"math"
)

// A 3D model as shapes: what the app's 3D editor and an agent work with.
// A package's model is built of solids; to edit it, each becomes a shape
// that can be moved (a solid as built), or is replaced or joined by
// boxes and cylinders, each sized, placed and coloured. Coordinates are
// the footprint's (mm, y down), z up from the board.

// Shape kinds.
const (
	Shape3DBox      = "box"
	Shape3DCylinder = "cylinder"
	// Shape3DSolid is a solid as built, moved by its offset.
	Shape3DSolid = "solid"
)

// Shape3D is a shape of a 3D model.
type Shape3D struct {
	Kind string `json:"kind"`
	// Part is body, lead, pad or mark, which sets its colour unless
	// Color does: black, grey, tan, brown or blue.
	Part  string `json:"part"`
	Color string `json:"color,omitempty"`
	// A box spans Min to Max; a cylinder stands on its centre (X, Y) from
	// Min[2] to Max[2] with radius R; a solid is moved by Offset.
	Min    V3      `json:"min"`
	Max    V3      `json:"max"`
	X      float64 `json:"x,omitempty"`
	Y      float64 `json:"y,omitempty"`
	R      float64 `json:"r,omitempty"`
	Offset V3      `json:"offset,omitempty"`
	// Solid is a built solid's own geometry (y up, as built).
	Solid *Solid `json:"solid,omitempty"`
	// Name says what it is, for people: "body", "lead 3".
	Name string `json:"name,omitempty"`
}

// ShapesOf are a model's solids as shapes to edit: a box when it's one,
// else the solid as built.
func ShapesOf(ss []Solid) []Shape3D {
	out := make([]Shape3D, 0, len(ss))
	leads := 0
	groups := map[string]int{}
	for i := range ss {
		s := ss[i]
		if s.Group != "" {
			// A group's solids are one shape, its faces together.
			if gi, ok := groups[s.Group]; ok {
				m := out[gi].Solid
				off := len(m.Verts)
				m.Verts = append(m.Verts, s.Verts...)
				for _, f := range s.Faces {
					shifted := make([]int, len(f))
					for k, v := range f {
						shifted[k] = v + off
					}
					m.Faces = append(m.Faces, shifted)
				}
				continue
			}
			groups[s.Group] = len(out)
			merged := Solid{Part: s.Part, Color: s.Color, Verts: append([]V3(nil), s.Verts...), Faces: append([][]int(nil), s.Faces...)}
			out = append(out, Shape3D{Kind: Shape3DSolid, Part: s.Part, Color: s.Color, Name: s.Group, Solid: &merged})
			continue
		}
		name := s.Part
		if s.Part == PartLead || s.Part == PartPad {
			leads++
			name = fmt.Sprintf("%s %d", s.Part, leads)
		}
		if lo, hi, ok := isBox(s); ok {
			out = append(out, Shape3D{Kind: Shape3DBox, Part: s.Part, Color: s.Color, Name: name,
				Min: V3{lo[0], -hi[1], lo[2]}, Max: V3{hi[0], -lo[1], hi[2]}})
			continue
		}
		out = append(out, Shape3D{Kind: Shape3DSolid, Part: s.Part, Color: s.Color, Name: name, Solid: &s})
	}
	return out
}

// components splits a solid into the solids its faces make: faces that
// share a vertex belong together.
func components(s Solid) []Solid {
	parent := make([]int, len(s.Verts))
	for i := range parent {
		parent[i] = i
	}
	var find func(int) int
	find = func(i int) int {
		for parent[i] != i {
			parent[i] = parent[parent[i]]
			i = parent[i]
		}
		return i
	}
	for _, f := range s.Faces {
		for _, v := range f[1:] {
			parent[find(v)] = find(f[0])
		}
	}
	byRoot := map[int]*Solid{}
	var order []int
	index := map[int]map[int]int{}
	for _, f := range s.Faces {
		if len(f) == 0 {
			continue
		}
		r := find(f[0])
		c, ok := byRoot[r]
		if !ok {
			c = &Solid{Part: s.Part, Color: s.Color, Group: s.Group}
			byRoot[r], index[r] = c, map[int]int{}
			order = append(order, r)
		}
		face := make([]int, len(f))
		for k, v := range f {
			if _, ok := index[r][v]; !ok {
				index[r][v] = len(c.Verts)
				c.Verts = append(c.Verts, s.Verts[v])
			}
			face[k] = index[r][v]
		}
		c.Faces = append(c.Faces, face)
	}
	if len(order) <= 1 {
		return []Solid{s}
	}
	out := make([]Solid, 0, len(order))
	for _, r := range order {
		out = append(out, *byRoot[r])
	}
	return out
}

// isBox reports whether a solid is an axis-aligned box, and its corners.
func isBox(s Solid) (V3, V3, bool) {
	if len(s.Verts) != 8 || len(s.Faces) != 6 {
		return V3{}, V3{}, false
	}
	lo, hi := Bounds([]Solid{s})
	for _, v := range s.Verts {
		for k := 0; k < 3; k++ {
			if math.Abs(v[k]-lo[k]) > 1e-6 && math.Abs(v[k]-hi[k]) > 1e-6 {
				return V3{}, V3{}, false
			}
		}
	}
	return lo, hi, true
}

// Check says what's wrong with a shape, or nil.
func (sh Shape3D) Check() error {
	switch sh.Kind {
	case Shape3DBox:
		for k := 0; k < 3; k++ {
			if sh.Max[k]-sh.Min[k] <= 0 {
				return fmt.Errorf("box %q has no size along %c", sh.Name, "xyz"[k])
			}
		}
	case Shape3DCylinder:
		if sh.R <= 0 || sh.Max[2]-sh.Min[2] <= 0 {
			return fmt.Errorf("cylinder %q needs a radius r and a height (min.z to max.z)", sh.Name)
		}
	case Shape3DSolid:
		if sh.Solid == nil || len(sh.Solid.Verts) < 4 {
			return fmt.Errorf("solid %q has no geometry", sh.Name)
		}
	default:
		return fmt.Errorf("unknown 3D shape kind %q: box, cylinder or solid", sh.Kind)
	}
	switch sh.Part {
	case PartBody, PartLead, PartPad, PartMark:
	default:
		return fmt.Errorf("shape %q: part %q isn't body, lead, pad or mark", sh.Name, sh.Part)
	}
	return nil
}

// SolidsOf builds a model's solids from its shapes.
func SolidsOf(shapes []Shape3D) ([]Solid, error) {
	out := make([]Solid, 0, len(shapes))
	for _, sh := range shapes {
		if err := sh.Check(); err != nil {
			return nil, err
		}
		var s Solid
		switch sh.Kind {
		case Shape3DBox:
			s = box(sh.Part, sh.Min[0], sh.Min[1], sh.Min[2], sh.Max[0], sh.Max[1], sh.Max[2])
		case Shape3DCylinder:
			s = prism(sh.Part, sh.X, sh.Y, sh.R, sh.Min[2], sh.Max[2], 24)
		default:
			s = Solid{Part: sh.Part, Faces: sh.Solid.Faces}
			for _, v := range sh.Solid.Verts {
				// The offset is in footprint coordinates: y down.
				s.Verts = append(s.Verts, V3{Number(v[0] + sh.Offset[0]), Number(v[1] - sh.Offset[1]), Number(v[2] + sh.Offset[2])})
			}
		}
		s.Part, s.Color = sh.Part, sh.Color
		// A shape of several pieces (a marking) is a solid each, so each
		// is a closed shell in the STEP model.
		out = append(out, components(s)...)
	}
	return out, nil
}
