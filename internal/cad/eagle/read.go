package eagle

import (
	"bytes"
	"encoding/xml"
	"errors"
	"io"
	"math"
	"strconv"
	"strings"

	"github.com/aitakemyjob/sheetdiver/internal/cad"
)

// Reading an Eagle library back: each package, symbol and device set as
// SheetDiver's model, with its element's text kept, so an edit made in
// Eagle or Fusion can be written back as the user left it.

// xPackage is a package element.
type xPackage struct {
	Name        string `xml:"name,attr"`
	Description string `xml:"description"`
	SMDs        []struct {
		Name      string  `xml:"name,attr"`
		X         float64 `xml:"x,attr"`
		Y         float64 `xml:"y,attr"`
		DX        float64 `xml:"dx,attr"`
		DY        float64 `xml:"dy,attr"`
		Roundness int     `xml:"roundness,attr"`
		Rot       string  `xml:"rot,attr"`
		Cream     string  `xml:"cream,attr"`
	} `xml:"smd"`
	Pads []struct {
		Name     string  `xml:"name,attr"`
		X        float64 `xml:"x,attr"`
		Y        float64 `xml:"y,attr"`
		Drill    float64 `xml:"drill,attr"`
		Diameter float64 `xml:"diameter,attr"`
		Shape    string  `xml:"shape,attr"`
		Rot      string  `xml:"rot,attr"`
	} `xml:"pad"`
	Holes []struct {
		X     float64 `xml:"x,attr"`
		Y     float64 `xml:"y,attr"`
		Drill float64 `xml:"drill,attr"`
	} `xml:"hole"`
	xDrawing
}

// xDrawing are the drawing elements of a package or a symbol.
type xDrawing struct {
	Wires      []xWire    `xml:"wire"`
	Rectangles []xRect    `xml:"rectangle"`
	Polygons   []xPolygon `xml:"polygon"`
	Circles    []xCircle  `xml:"circle"`
	Texts      []xText    `xml:"text"`
}

// xWire is a wire, an arc when curved.
type xWire struct {
	X1    float64 `xml:"x1,attr"`
	Y1    float64 `xml:"y1,attr"`
	X2    float64 `xml:"x2,attr"`
	Y2    float64 `xml:"y2,attr"`
	Width float64 `xml:"width,attr"`
	Layer int     `xml:"layer,attr"`
	Curve float64 `xml:"curve,attr"`
}

// xRect is a filled rectangle.
type xRect struct {
	X1    float64 `xml:"x1,attr"`
	Y1    float64 `xml:"y1,attr"`
	X2    float64 `xml:"x2,attr"`
	Y2    float64 `xml:"y2,attr"`
	Layer int     `xml:"layer,attr"`
	Rot   string  `xml:"rot,attr"`
}

// xPolygon is a filled polygon.
type xPolygon struct {
	Width    float64 `xml:"width,attr"`
	Layer    int     `xml:"layer,attr"`
	Vertices []struct {
		X float64 `xml:"x,attr"`
		Y float64 `xml:"y,attr"`
	} `xml:"vertex"`
}

// xCircle is a circle, filled when its width is 0.
type xCircle struct {
	X      float64 `xml:"x,attr"`
	Y      float64 `xml:"y,attr"`
	Radius float64 `xml:"radius,attr"`
	Width  float64 `xml:"width,attr"`
	Layer  int     `xml:"layer,attr"`
}

// xText is a text.
type xText struct {
	X     float64 `xml:"x,attr"`
	Y     float64 `xml:"y,attr"`
	Size  float64 `xml:"size,attr"`
	Layer int     `xml:"layer,attr"`
	Rot   string  `xml:"rot,attr"`
	Align string  `xml:"align,attr"`
	Text  string  `xml:",chardata"`
}

// xSymbol is a symbol element.
type xSymbol struct {
	Name string `xml:"name,attr"`
	xDrawing
	Pins []struct {
		Name      string  `xml:"name,attr"`
		X         float64 `xml:"x,attr"`
		Y         float64 `xml:"y,attr"`
		Length    string  `xml:"length,attr"`
		Direction string  `xml:"direction,attr"`
		Rot       string  `xml:"rot,attr"`
		Visible   string  `xml:"visible,attr"`
	} `xml:"pin"`
}

// xDevice is a device set element.
type xDevice struct {
	Name        string `xml:"name,attr"`
	Prefix      string `xml:"prefix,attr"`
	Description string `xml:"description"`
	Gates       []struct {
		Symbol string `xml:"symbol,attr"`
	} `xml:"gates>gate"`
	Devices []struct {
		Package  string `xml:"package,attr"`
		Connects []struct {
			Pin string `xml:"pin,attr"`
			Pad string `xml:"pad,attr"`
		} `xml:"connects>connect"`
	} `xml:"devices>device"`
}

// ReadPackage is a footprint read from an Eagle library.
type ReadPackage struct {
	Footprint cad.Footprint
	Raw       string
}

// ReadSymbol is a symbol read from an Eagle library: numbered by its
// device set's connections to its package.
type ReadSymbol struct {
	Symbol    cad.Symbol
	Package   string
	RawSymbol string
	RawDevice string
}

// Read is an Eagle library's packages and symbols, by name.
type Read struct {
	Packages map[string]ReadPackage
	Symbols  map[string]ReadSymbol
}

// elements are a library's package, symbol and device set elements'
// texts, by kind and name.
func elements(b []byte) (map[string]map[string]string, error) {
	out := map[string]map[string]string{"package": {}, "symbol": {}, "deviceset": {}}
	d := xml.NewDecoder(bytes.NewReader(b))
	d.Strict = false
	depth := 0
	for {
		off := d.InputOffset()
		tok, err := d.Token()
		if errors.Is(err, io.EOF) {
			return out, nil
		}
		if err != nil {
			return nil, err
		}
		switch t := tok.(type) {
		case xml.StartElement:
			if m, ok := out[t.Name.Local]; ok && depth >= 3 {
				name := ""
				for _, a := range t.Attr {
					if a.Name.Local == "name" {
						name = a.Value
					}
				}
				if err := d.Skip(); err != nil {
					return nil, err
				}
				m[name] = string(b[off:d.InputOffset()])
				continue
			}
			depth++
		case xml.EndElement:
			depth--
		}
	}
}

// ReadLibrary reads an Eagle library.
func ReadLibrary(b []byte) (Read, error) {
	els, err := elements(b)
	if err != nil {
		return Read{}, err
	}
	out := Read{Packages: map[string]ReadPackage{}, Symbols: map[string]ReadSymbol{}}
	for name, raw := range els["package"] {
		var x xPackage
		if err := xml.Unmarshal([]byte(raw), &x); err != nil {
			return out, err
		}
		out.Packages[name] = ReadPackage{Footprint: readPackage(x), Raw: raw}
	}
	for _, raw := range els["deviceset"] {
		var dev xDevice
		if err := xml.Unmarshal([]byte(raw), &dev); err != nil {
			return out, err
		}
		if len(dev.Gates) == 0 {
			continue
		}
		symRaw, ok := els["symbol"][dev.Gates[0].Symbol]
		if !ok {
			continue
		}
		var x xSymbol
		if err := xml.Unmarshal([]byte(symRaw), &x); err != nil {
			return out, err
		}
		pads, pkg := map[string]string{}, ""
		if len(dev.Devices) > 0 {
			pkg = dev.Devices[0].Package
			for _, c := range dev.Devices[0].Connects {
				pads[c.Pin] = unsuffix(strings.Fields(c.Pad + " ")[0])
			}
		}
		s := readSymbol(x, pads)
		s.Name, s.Reference, s.Value, s.Description = x.Name, dev.Prefix, dev.Name, strings.TrimSpace(dev.Description)
		out.Symbols[x.Name] = ReadSymbol{Symbol: s, Package: pkg, RawSymbol: symRaw, RawDevice: raw}
	}
	return out, nil
}

// unsuffix drops Eagle's @n from a duplicated name.
func unsuffix(s string) string {
	if i := strings.LastIndex(s, "@"); i > 0 {
		return s[:i]
	}
	return s
}

// turned reports whether a rotation is a quarter turn.
func turned(rot string) bool {
	r, _ := strconv.ParseFloat(strings.TrimLeft(rot, "MSR"), 64)
	return math.Mod(math.Abs(r), 180) == 90
}

// readPackage turns a package into a footprint, its y flipped.
func readPackage(x xPackage) cad.Footprint {
	f := cad.Footprint{Name: x.Name, Description: strings.TrimSpace(x.Description), From: "your edit in Eagle"}
	for _, s := range x.SMDs {
		p := cad.Pad{Number: unsuffix(s.Name), Kind: cad.PadSMD, X: s.X, Y: -s.Y, W: s.DX, H: s.DY, NoPaste: s.Cream == "no"}
		if turned(s.Rot) {
			p.W, p.H = p.H, p.W
		}
		switch {
		case s.Roundness >= 100 && p.W == p.H:
			p.Shape = "circle"
		case s.Roundness >= 100:
			p.Shape = "oval"
		case s.Roundness > 0:
			p.Shape = "roundrect"
		default:
			p.Shape = "rect"
		}
		f.Pads = append(f.Pads, p)
	}
	for _, t := range x.Pads {
		d := t.Diameter
		if d <= 0 {
			d = t.Drill * 1.6
		}
		p := cad.Pad{Number: unsuffix(t.Name), Kind: cad.PadThru, X: t.X, Y: -t.Y, W: d, H: d, Drill: t.Drill, Shape: "circle"}
		switch t.Shape {
		case "square":
			p.Shape = "rect"
		case "long":
			p.Shape, p.W = "oval", 2*d
			if turned(t.Rot) {
				p.W, p.H = p.H, p.W
			}
		}
		f.Pads = append(f.Pads, p)
	}
	for _, h := range x.Holes {
		f.Pads = append(f.Pads, cad.Pad{Kind: cad.PadNPTH, Shape: "circle", X: h.X, Y: -h.Y, W: h.Drill, H: h.Drill, Drill: h.Drill})
	}
	for _, r := range x.Rectangles {
		if r.Layer == layerTCream {
			f.Pads = append(f.Pads, cad.Pad{Kind: cad.PadPaste, Shape: "roundrect", X: (r.X1 + r.X2) / 2, Y: -(r.Y1 + r.Y2) / 2, W: math.Abs(r.X2 - r.X1), H: math.Abs(r.Y2 - r.Y1)})
		}
	}
	// Straight wires on silk and fab, and their polygons, are SheetDiver's
	// lines and marks; the rest drawn on the layers it knows is graphics.
	layer := map[int]string{layerTPlace: cad.LayerSilk, layerTDocu: cad.LayerFab}
	for _, w := range x.Wires {
		if l, ok := layer[w.Layer]; ok && w.Curve == 0 {
			f.Lines = append(f.Lines, cad.Line{Layer: l, A: cad.Pt{X: w.X1, Y: -w.Y1}, B: cad.Pt{X: w.X2, Y: -w.Y2}, Width: w.Width})
		}
	}
	for _, p := range x.Polygons {
		if l, ok := layer[p.Layer]; ok {
			var pts []cad.Pt
			for _, v := range p.Vertices {
				pts = append(pts, cad.Pt{X: v.X, Y: -v.Y})
			}
			f.Polys = append(f.Polys, cad.Poly{Layer: l, Pts: pts})
		}
	}
	rest := x.xDrawing
	rest.Wires, rest.Polygons = nil, nil
	for _, w := range x.Wires {
		if _, ok := layer[w.Layer]; !ok || w.Curve != 0 {
			rest.Wires = append(rest.Wires, w)
		}
	}
	for _, p := range x.Polygons {
		if _, ok := layer[p.Layer]; !ok {
			rest.Polygons = append(rest.Polygons, p)
		}
	}
	drawn := map[int]string{}
	for l, n := range graphicLayers {
		drawn[n] = l
	}
	f.Graphics = rest.graphics(drawn, -1)
	var body cad.Rect
	for _, l := range f.Lines {
		if l.Layer == cad.LayerFab {
			for _, p := range []cad.Pt{l.A, l.B} {
				if body == (cad.Rect{}) {
					body = cad.Rect{X0: p.X, Y0: p.Y, X1: p.X, Y1: p.Y}
				}
				body = cad.Rect{X0: math.Min(body.X0, p.X), Y0: math.Min(body.Y0, p.Y), X1: math.Max(body.X1, p.X), Y1: math.Max(body.Y1, p.Y)}
			}
		}
	}
	f.Body, f.Courtyard = body, cad.Courtyard(f.Pads, body)
	f.Mount = cad.MountSMD
	for _, p := range f.Pads {
		if p.Kind == cad.PadThru {
			f.Mount = cad.MountThru
		}
	}
	return f
}

// pinTypes are the electrical types Eagle's directions read as.
var pinTypes = map[string]string{"in": cad.TypeInput, "out": cad.TypeOutput, "io": cad.TypeBidir, "hiz": cad.TypeTriState, "pas": cad.TypePassive,
	"pwr": cad.TypePowerIn, "sup": cad.TypePowerOut, "oc": cad.TypeOpenDrain, "nc": cad.TypeNoConnect}

// pinLengths are Eagle's pin lengths in mm.
var pinLengths = map[string]float64{"point": 0, "short": 2.54, "middle": 5.08, "long": 7.62}

// readSymbol turns a symbol into the model: pins numbered by the device's
// connections, the body the box round its lines, other lines kept.
func readSymbol(x xSymbol, pads map[string]string) cad.Symbol {
	var s cad.Symbol
	sides := map[string]string{"R0": cad.Left, "R180": cad.Right, "R270": cad.Top, "R90": cad.Bottom, "": cad.Left}
	for _, p := range x.Pins {
		length, ok := pinLengths[p.Length]
		if !ok {
			length = 7.62
		}
		typ := pinTypes[p.Direction]
		if typ == "" {
			typ = cad.TypeBidir
		}
		number := pads[p.Name]
		if number == "" {
			number = unsuffix(p.Name)
		}
		s.Pins = append(s.Pins, cad.SymbolPin{Number: number, Name: unsuffix(p.Name), Type: typ, Side: sides[p.Rot], X: p.X, Y: p.Y, Length: length,
			Hidden: p.Visible == "off"})
	}
	var straight []xWire
	for _, w := range x.Wires {
		if w.Curve == 0 {
			straight = append(straight, w)
		}
	}
	// The body is the first four wires in a row that make a rectangle (as
	// written), else the box round all the wires.
	box, at := cad.Rect{}, -1
	for i := 0; i+4 <= len(straight) && at < 0; i++ {
		if r, ok := rectOf(straight[i : i+4]); ok {
			box, at = r, i
		}
	}
	if at < 0 {
		for i, w := range straight {
			r := cad.Rect{X0: math.Min(w.X1, w.X2), Y0: math.Min(w.Y1, w.Y2), X1: math.Max(w.X1, w.X2), Y1: math.Max(w.Y1, w.Y2)}
			if i == 0 {
				box = r
				continue
			}
			box = cad.Rect{X0: math.Min(box.X0, r.X0), Y0: math.Min(box.Y0, r.Y0), X1: math.Max(box.X1, r.X1), Y1: math.Max(box.Y1, r.Y1)}
		}
	}
	s.Body = box
	onEdge := func(i int, w xWire) bool {
		if at >= 0 {
			return i >= at && i < at+4
		}
		return (w.X1 == w.X2 && (w.X1 == box.X0 || w.X1 == box.X1)) || (w.Y1 == w.Y2 && (w.Y1 == box.Y0 || w.Y1 == box.Y1))
	}
	for i, w := range straight {
		if !onEdge(i, w) {
			s.Extra = append(s.Extra, cad.Line{Layer: "body", A: cad.Pt{X: w.X1, Y: w.Y1}, B: cad.Pt{X: w.X2, Y: w.Y2}, Width: w.Width})
		}
	}
	for _, g := range x.graphics(nil, 1) {
		if g.Kind != cad.GraphicLine {
			s.Graphics = append(s.Graphics, g)
		}
	}
	return s
}

// rectOf is the rectangle four wires are the sides of, if they are.
func rectOf(ws []xWire) (cad.Rect, bool) {
	r := cad.Rect{X0: math.Inf(1), Y0: math.Inf(1), X1: math.Inf(-1), Y1: math.Inf(-1)}
	for _, w := range ws {
		r = cad.Rect{X0: math.Min(r.X0, math.Min(w.X1, w.X2)), Y0: math.Min(r.Y0, math.Min(w.Y1, w.Y2)), X1: math.Max(r.X1, math.Max(w.X1, w.X2)), Y1: math.Max(r.Y1, math.Max(w.Y1, w.Y2))}
	}
	if r.X1-r.X0 < 1e-6 || r.Y1-r.Y0 < 1e-6 {
		return r, false
	}
	near := func(a, b float64) bool { return math.Abs(a-b) < 1e-6 }
	// Each wire one side, from corner to corner: bottom, right, top, left.
	seen := [4]bool{}
	for _, w := range ws {
		lo, hi := cad.Pt{X: math.Min(w.X1, w.X2), Y: math.Min(w.Y1, w.Y2)}, cad.Pt{X: math.Max(w.X1, w.X2), Y: math.Max(w.Y1, w.Y2)}
		horiz := near(lo.Y, hi.Y) && near(lo.X, r.X0) && near(hi.X, r.X1)
		vert := near(lo.X, hi.X) && near(lo.Y, r.Y0) && near(hi.Y, r.Y1)
		side := -1
		switch {
		case horiz && near(lo.Y, r.Y0):
			side = 0
		case vert && near(lo.X, r.X1):
			side = 1
		case horiz && near(lo.Y, r.Y1):
			side = 2
		case vert && near(lo.X, r.X0):
			side = 3
		}
		if side < 0 || seen[side] {
			return r, false
		}
		seen[side] = true
	}
	return r, true
}

// graphics are a drawing's shapes as the model's graphics: on the layers
// given by Eagle layer (a symbol's, nil, all on its body), y flipped when
// ys is -1. Straight wires are lines, curved ones arcs; rectangles,
// polygons and circles of width 0 are filled; texts other than Eagle's
// >NAME, >VALUE… placeholders are texts.
func (x xDrawing) graphics(layers map[int]string, ys float64) []cad.Graphic {
	layerOf := func(n int) (string, bool) {
		if layers == nil {
			return cad.LayerBody, true
		}
		l, ok := layers[n]
		return l, ok
	}
	pt := func(px, py float64) cad.Pt { return cad.Pt{X: px, Y: ys * py} }
	var out []cad.Graphic
	add := func(g cad.Graphic) {
		if g.Check() == nil {
			out = append(out, g)
		}
	}
	for _, w := range x.Wires {
		l, ok := layerOf(w.Layer)
		if !ok {
			continue
		}
		g := cad.Graphic{Kind: cad.GraphicLine, Layer: l, Pts: []cad.Pt{pt(w.X1, w.Y1), pt(w.X2, w.Y2)}, Width: w.Width}
		if w.Curve != 0 {
			g.Kind, g.Pts = cad.GraphicArc, arcPts(w)
			for i, p := range g.Pts {
				g.Pts[i] = pt(p.X, p.Y)
			}
		}
		add(g)
	}
	for _, r := range x.Rectangles {
		if l, ok := layerOf(r.Layer); ok {
			add(rectGraphic(r, l, pt))
		}
	}
	for _, p := range x.Polygons {
		if l, ok := layerOf(p.Layer); ok {
			g := cad.Graphic{Kind: cad.GraphicPoly, Layer: l, Width: p.Width, Fill: true}
			for _, v := range p.Vertices {
				g.Pts = append(g.Pts, pt(v.X, v.Y))
			}
			add(g)
		}
	}
	for _, c := range x.Circles {
		if l, ok := layerOf(c.Layer); ok {
			add(cad.Graphic{Kind: cad.GraphicCircle, Layer: l, Pts: []cad.Pt{pt(c.X, c.Y)}, R: c.Radius, Width: c.Width, Fill: c.Width == 0})
		}
	}
	for _, t := range x.Texts {
		l, ok := layerOf(t.Layer)
		text := strings.TrimSpace(t.Text)
		if !ok || strings.HasPrefix(text, ">") {
			continue
		}
		at, angle := textAnchor(t, text)
		add(cad.Graphic{Kind: cad.GraphicText, Layer: l, Pts: []cad.Pt{pt(at.X, at.Y)}, Text: text, Size: t.Size, Angle: angle})
	}
	return out
}

// arcPts are a curved wire's start, the point midway round and its end
// (Eagle's frame): the centre is on the chord's left for a curve
// counter-clockwise, at half the chord over the tangent of half the curve.
func arcPts(w xWire) []cad.Pt {
	c := w.Curve * math.Pi / 180
	dx, dy := w.X2-w.X1, w.Y2-w.Y1
	k := 0.5 / math.Tan(c/2)
	o := cad.Pt{X: (w.X1+w.X2)/2 - dy*k, Y: (w.Y1+w.Y2)/2 + dx*k}
	r := math.Hypot(w.X1-o.X, w.Y1-o.Y)
	t := math.Atan2(w.Y1-o.Y, w.X1-o.X) + c/2
	return []cad.Pt{{X: w.X1, Y: w.Y1}, {X: o.X + r*math.Cos(t), Y: o.Y + r*math.Sin(t)}, {X: w.X2, Y: w.Y2}}
}

// rotation is the angle in degrees of an Eagle rotation (R90, SR180,
// MR45…) and whether it is spun (drawn as turned, even upside down).
func rotation(rot string) (float64, bool) {
	deg, _ := strconv.ParseFloat(strings.TrimLeft(rot, "MSR"), 64)
	return deg, strings.Contains(rot, "S")
}

// rectGraphic is a rectangle as a filled rect, or a filled polygon when
// turned other than a quarter turn (Eagle turns it about its centre).
func rectGraphic(r xRect, layer string, pt func(x, y float64) cad.Pt) cad.Graphic {
	deg, _ := rotation(r.Rot)
	cx, cy := (r.X1+r.X2)/2, (r.Y1+r.Y2)/2
	hw, hh := math.Abs(r.X2-r.X1)/2, math.Abs(r.Y2-r.Y1)/2
	switch math.Mod(math.Abs(deg), 180) {
	case 0:
		return cad.Graphic{Kind: cad.GraphicRect, Layer: layer, Fill: true, Pts: []cad.Pt{pt(cx-hw, cy-hh), pt(cx+hw, cy+hh)}}
	case 90:
		return cad.Graphic{Kind: cad.GraphicRect, Layer: layer, Fill: true, Pts: []cad.Pt{pt(cx-hh, cy-hw), pt(cx+hh, cy+hw)}}
	}
	t := deg * math.Pi / 180
	g := cad.Graphic{Kind: cad.GraphicPoly, Layer: layer, Fill: true}
	for _, c := range [][2]float64{{-hw, -hh}, {hw, -hh}, {hw, hh}, {-hw, hh}} {
		g.Pts = append(g.Pts, pt(cx+c[0]*math.Cos(t)-c[1]*math.Sin(t), cy+c[0]*math.Sin(t)+c[1]*math.Cos(t)))
	}
	return g
}

// charWidth is about how wide a character of Eagle's vector font is, by
// its height: for placing a text aligned other than at its start.
const charWidth = 0.8

// textAnchor is where a text's baseline starts (Eagle's frame) and its
// angle as seen. Eagle turns a text not spun round when it would read
// from below (more than 90°, up to 270°), its alignment with it; the
// anchor then and for other alignments is placed by its estimated width.
func textAnchor(t xText, text string) (cad.Pt, float64) {
	deg, spin := rotation(t.Rot)
	deg = math.Mod(deg, 360)
	if deg < 0 {
		deg += 360
	}
	// How far along and up the text its anchor is, 0 to 1.
	hx, vy := 0.0, 0.0
	parts := strings.Split(t.Align, "-")
	switch parts[0] {
	case "center":
		vy = 0.5
	case "top":
		vy = 1
	}
	if parts[0] == "center" && len(parts) == 1 {
		hx = 0.5
	}
	if len(parts) == 2 {
		switch parts[1] {
		case "center":
			hx = 0.5
		case "right":
			hx = 1
		}
	}
	if !spin && deg > 90 && deg <= 270 {
		deg -= 180
		hx, vy = 1-hx, 1-vy
	}
	if hx == 0 && vy == 0 {
		return cad.Pt{X: t.X, Y: t.Y}, deg
	}
	dx, dy := -hx*charWidth*t.Size*float64(len([]rune(text))), -vy*t.Size
	a := deg * math.Pi / 180
	return cad.Pt{X: t.X + dx*math.Cos(a) - dy*math.Sin(a), Y: t.Y + dx*math.Sin(a) + dy*math.Cos(a)}, deg
}

// DecodePackage is the footprint a package element holds.
func DecodePackage(raw string) (cad.Footprint, error) {
	var x xPackage
	if err := xml.Unmarshal([]byte(raw), &x); err != nil {
		return cad.Footprint{}, err
	}
	return readPackage(x), nil
}

// DecodeSymbol is the symbol a symbol element holds, numbered by its
// device set's connections.
func DecodeSymbol(rawSymbol, rawDevice string) (cad.Symbol, error) {
	var x xSymbol
	var dev xDevice
	if err := xml.Unmarshal([]byte(rawSymbol), &x); err != nil {
		return cad.Symbol{}, err
	}
	if err := xml.Unmarshal([]byte(rawDevice), &dev); err != nil {
		return cad.Symbol{}, err
	}
	pads := map[string]string{}
	if len(dev.Devices) > 0 {
		for _, c := range dev.Devices[0].Connects {
			pads[c.Pin] = unsuffix(strings.Fields(c.Pad + " ")[0])
		}
	}
	s := readSymbol(x, pads)
	s.Name, s.Reference, s.Value, s.Description = x.Name, dev.Prefix, dev.Name, strings.TrimSpace(dev.Description)
	return s, nil
}
