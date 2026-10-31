// Package step writes 3D package bodies as STEP (ISO 10303-21, AP214):
// each solid an exact boundary representation of flat faces, coloured,
// in millimetres. KiCad, FreeCAD, Altium and mechanical CAD tools read it.
package step

import (
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/aitakemyjob/sheetdiver/internal/cad"
)

// writer numbers and collects entities.
type writer struct {
	b    strings.Builder
	next int
}

// add writes an entity and returns its reference.
func (w *writer) add(format string, a ...any) string {
	w.next++
	ref := "#" + strconv.Itoa(w.next)
	fmt.Fprintf(&w.b, "%s=%s;\n", ref, fmt.Sprintf(format, a...))
	return ref
}

// realNum writes a number as STEP wants it: always with a decimal point.
func realNum(v float64) string {
	if math.Abs(v) < 1e-9 {
		return "0."
	}
	s := strconv.FormatFloat(v, 'f', 6, 64)
	s = strings.TrimRight(s, "0")
	return s
}

// vec writes a coordinate triple.
func vec(v cad.V3) string {
	return fmt.Sprintf("(%s,%s,%s)", realNum(v[0]), realNum(v[1]), realNum(v[2]))
}

// sub is a minus b.
func sub(a, b cad.V3) cad.V3 { return cad.V3{a[0] - b[0], a[1] - b[1], a[2] - b[2]} }

// norm is a vector's length.
func norm(v cad.V3) float64 { return math.Sqrt(v[0]*v[0] + v[1]*v[1] + v[2]*v[2]) }

// unit is a vector scaled to length 1.
func unit(v cad.V3) cad.V3 {
	l := norm(v)
	if l == 0 {
		return cad.V3{0, 0, 1}
	}
	return cad.V3{v[0] / l, v[1] / l, v[2] / l}
}

// normal is a face's outward normal by Newell's method (its vertices
// counter-clockwise seen from outside).
func normal(vs []cad.V3, face []int) cad.V3 {
	var nv cad.V3
	for i := range face {
		a, b := vs[face[i]], vs[face[(i+1)%len(face)]]
		nv[0] += (a[1] - b[1]) * (a[2] + b[2])
		nv[1] += (a[2] - b[2]) * (a[0] + b[0])
		nv[2] += (a[0] - b[0]) * (a[1] + b[1])
	}
	return unit(nv)
}

// Write is a STEP file holding the solids as one part named name.
func Write(name string, solids []cad.Solid, now time.Time) []byte {
	w := &writer{}
	app := w.add("APPLICATION_CONTEXT('core data for automotive mechanical design processes')")
	w.add("APPLICATION_PROTOCOL_DEFINITION('international standard','automotive_design',2000,%s)", app)
	pc := w.add("PRODUCT_CONTEXT('',%s,'mechanical')", app)
	prod := w.add("PRODUCT('%s','%s','',(%s))", esc(name), esc(name), pc)
	w.add("PRODUCT_RELATED_PRODUCT_CATEGORY('part',$,(%s))", prod)
	form := w.add("PRODUCT_DEFINITION_FORMATION('','',%s)", prod)
	pdc := w.add("PRODUCT_DEFINITION_CONTEXT('part definition',%s,'design')", app)
	pd := w.add("PRODUCT_DEFINITION('design','',%s,%s)", form, pdc)
	pds := w.add("PRODUCT_DEFINITION_SHAPE('','',%s)", pd)
	length := w.add("( LENGTH_UNIT() NAMED_UNIT(*) SI_UNIT(.MILLI.,.METRE.) )")
	angle := w.add("( NAMED_UNIT(*) PLANE_ANGLE_UNIT() SI_UNIT($,.RADIAN.) )")
	solidAngle := w.add("( NAMED_UNIT(*) SI_UNIT($,.STERADIAN.) SOLID_ANGLE_UNIT() )")
	unc := w.add("UNCERTAINTY_MEASURE_WITH_UNIT(LENGTH_MEASURE(1.E-07),%s,'distance_accuracy_value','confusion accuracy')", length)
	ctx := w.add("( GEOMETRIC_REPRESENTATION_CONTEXT(3) GLOBAL_UNCERTAINTY_ASSIGNED_CONTEXT((%s)) GLOBAL_UNIT_ASSIGNED_CONTEXT((%s,%s,%s)) REPRESENTATION_CONTEXT('Context #1','3D Context with UNIT and UNCERTAINTY') )", unc, length, angle, solidAngle)
	origin := w.add("CARTESIAN_POINT('',(0.,0.,0.))")
	dz := w.add("DIRECTION('',(0.,0.,1.))")
	dx := w.add("DIRECTION('',(1.,0.,0.))")
	axis := w.add("AXIS2_PLACEMENT_3D('',%s,%s,%s)", origin, dz, dx)

	var breps, styles []string
	for i, s := range solids {
		brep := w.solid(fmt.Sprintf("%s_%d", s.Part, i+1), s)
		breps = append(breps, brep)
		c := s.RGB()
		col := w.add("COLOUR_RGB('',%s,%s,%s)", realNum(c[0]), realNum(c[1]), realNum(c[2]))
		fasc := w.add("FILL_AREA_STYLE_COLOUR('',%s)", col)
		fas := w.add("FILL_AREA_STYLE('',(%s))", fasc)
		ssfa := w.add("SURFACE_STYLE_FILL_AREA(%s)", fas)
		sss := w.add("SURFACE_SIDE_STYLE('',(%s))", ssfa)
		ssu := w.add("SURFACE_STYLE_USAGE(.BOTH.,%s)", sss)
		psa := w.add("PRESENTATION_STYLE_ASSIGNMENT((%s))", ssu)
		styles = append(styles, w.add("STYLED_ITEM('color',(%s),%s)", psa, brep))
	}
	rep := w.add("ADVANCED_BREP_SHAPE_REPRESENTATION('%s',(%s),%s)", esc(name), strings.Join(append([]string{axis}, breps...), ","), ctx)
	w.add("SHAPE_DEFINITION_REPRESENTATION(%s,%s)", pds, rep)
	w.add("MECHANICAL_DESIGN_GEOMETRIC_PRESENTATION_REPRESENTATION('',(%s),%s)", strings.Join(styles, ","), ctx)

	var out strings.Builder
	out.WriteString("ISO-10303-21;\nHEADER;\n")
	fmt.Fprintf(&out, "FILE_DESCRIPTION(('%s package model'),'2;1');\n", esc(name))
	fmt.Fprintf(&out, "FILE_NAME('%s.step','%s',('SheetDiver'),('SheetDiver'),'SheetDiver','SheetDiver','');\n", esc(name), now.UTC().Format("2006-01-02T15:04:05"))
	out.WriteString("FILE_SCHEMA(('AUTOMOTIVE_DESIGN { 1 0 10303 214 1 1 1 1 }'));\nENDSEC;\nDATA;\n")
	out.WriteString(w.b.String())
	out.WriteString("ENDSEC;\nEND-ISO-10303-21;\n")
	return []byte(out.String())
}

// solid writes one solid as a manifold boundary representation: its
// vertices, each edge once (as a line), and each face a plane bounded by
// a loop of its edges.
func (w *writer) solid(name string, s cad.Solid) string {
	verts := make([]string, len(s.Verts))
	for i, v := range s.Verts {
		p := w.add("CARTESIAN_POINT('',%s)", vec(v))
		verts[i] = w.add("VERTEX_POINT('',%s)", p)
	}
	type key struct{ a, b int }
	edges := map[key]string{}
	edge := func(a, b int) (string, bool) {
		k, forward := key{a, b}, true
		if a > b {
			k, forward = key{b, a}, false
		}
		if e, ok := edges[k]; ok {
			return e, forward
		}
		from, to := s.Verts[k.a], s.Verts[k.b]
		d := sub(to, from)
		p := w.add("CARTESIAN_POINT('',%s)", vec(from))
		dir := w.add("DIRECTION('',%s)", vec(unit(d)))
		v := w.add("VECTOR('',%s,%s)", dir, realNum(norm(d)))
		line := w.add("LINE('',%s,%s)", p, v)
		e := w.add("EDGE_CURVE('',%s,%s,%s,.T.)", verts[k.a], verts[k.b], line)
		edges[k] = e
		return e, forward
	}
	var faces []string
	for _, f := range s.Faces {
		var oes []string
		for i := range f {
			e, forward := edge(f[i], f[(i+1)%len(f)])
			sense := ".T."
			if !forward {
				sense = ".F."
			}
			oes = append(oes, w.add("ORIENTED_EDGE('',*,*,%s,%s)", e, sense))
		}
		loop := w.add("EDGE_LOOP('',(%s))", strings.Join(oes, ","))
		bound := w.add("FACE_OUTER_BOUND('',%s,.T.)", loop)
		o := s.Verts[f[0]]
		po := w.add("CARTESIAN_POINT('',%s)", vec(o))
		pn := w.add("DIRECTION('',%s)", vec(normal(s.Verts, f)))
		pr := w.add("DIRECTION('',%s)", vec(unit(sub(s.Verts[f[1]], o))))
		ax := w.add("AXIS2_PLACEMENT_3D('',%s,%s,%s)", po, pn, pr)
		plane := w.add("PLANE('',%s)", ax)
		faces = append(faces, w.add("ADVANCED_FACE('',(%s),%s,.T.)", bound, plane))
	}
	shell := w.add("CLOSED_SHELL('',(%s))", strings.Join(faces, ","))
	return w.add("MANIFOLD_SOLID_BREP('%s',%s)", esc(name), shell)
}

// esc escapes a STEP string.
func esc(s string) string { return strings.ReplaceAll(s, "'", "''") }
