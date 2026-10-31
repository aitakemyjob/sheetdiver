package altium

import (
	"encoding/binary"
	"fmt"
	"math"
	"strconv"
	"strings"

	"github.com/aitakemyjob/sheetdiver/internal/cad"
)

// A schematic library: a FileHeader listing the symbols, and a storage
// per symbol whose Data stream is its records: the component, its pins
// (binary records), its body, its designator and comment, and the
// footprint it's drawn on. Schematic lengths are in tens of mils; an
// Altium pin's location is its body end, its electrical end one pin
// length further in its direction.

// Symbol is a schematic symbol and the footprint it's drawn on, or the
// symbol as the user saved it in Altium (Raw: its storage's streams by
// path), written back as it is.
type Symbol struct {
	Symbol    cad.Symbol
	Footprint string
	Raw       map[string][]byte
}

// sch is a length in mm as schematic units (10 mil), with the remainder
// in hundred-thousandths of one.
func sch(mm float64) (int, int) {
	u := mm / 0.254
	whole := math.Trunc(u)
	return int(whole), int(math.Round((u - whole) * 1e5))
}

// coordParams are a coordinate's parameters, as Altium leaves zeros out.
func coordParams(name string, mm float64) []string {
	n, frac := sch(mm)
	var out []string
	if n != 0 {
		out = append(out, name, strconv.Itoa(n))
	}
	if frac != 0 {
		out = append(out, name+"_Frac", strconv.Itoa(frac))
	}
	return out
}

// Pin electrical types, Altium's.
var electrical = map[string]byte{
	cad.TypeInput: 0, cad.TypeBidir: 1, cad.TypeOutput: 2, cad.TypeOpenDrain: 3, cad.TypePassive: 4,
	cad.TypeTriState: 5, cad.TypePowerIn: 7, cad.TypePowerOut: 7, cad.TypeNoConnect: 4, cad.TypeUnspecified: 4,
}

// Pin orientations: which way a pin points from its body end.
var orientation = map[string]byte{cad.Right: 0, cad.Top: 1, cad.Left: 2, cad.Bottom: 3}

// pinRecord is a pin's binary record.
func (r *rec) pinRecord(p cad.SymbolPin) {
	// The body end: one pin length back from the connection.
	x, y := p.X, p.Y
	switch p.Side {
	case cad.Left:
		x += p.Length
	case cad.Right:
		x -= p.Length
	case cad.Top:
		y -= p.Length
	case cad.Bottom:
		y += p.Length
	}
	var b rec
	b.u32(2)
	b.u8(0)
	b.u16(1) // part 1
	b.u8(0)  // display mode
	b.u8(0)  // inner edge symbol
	b.u8(0)  // outer edge symbol
	b.u8(0)  // inside symbol
	b.u8(0)  // outside symbol
	b.pascal("")
	b.u8(0) // formal type
	b.u8(electrical[p.Type])
	flags := orientation[p.Side] | 0x08 | 0x10 // name and designator shown
	if p.Hidden {
		flags |= 0x04
	}
	b.u8(flags)
	for _, v := range []float64{p.Length, x, y} {
		_ = binary.Write(&b, binary.LittleEndian, int16(math.Max(math.MinInt16, math.Min(math.MaxInt16, math.Trunc(v/0.254)))))
	}
	b.u32(0) // colour: black
	b.pascal(p.Name)
	b.pascal(p.Number)
	b.pascal("")
	b.pascal("")
	b.pascal("")
	r.u32(u32(b.Len()) | 1<<24) // flagged as a binary pin record
	r.Write(b.Bytes())
}

// textRecord is a parameter record as a C string, flagged as text.
func (r *rec) textRecord(kv ...string) {
	t := append(latin1(paramText(kv...)), 0)
	r.u32(u32(len(t)))
	r.Write(t)
}

// schWidths are Altium's schematic line widths in mm: smallest, small,
// medium and large.
var schWidths = []float64{0, 0.254, 0.762, 1.27}

// lineWidth is the nearest of Altium's line widths to one in mm.
func lineWidth(mm float64) string {
	best := 0
	for i, w := range schWidths {
		if math.Abs(w-mm) < math.Abs(schWidths[best]-mm) {
			best = i
		}
	}
	return strconv.Itoa(best)
}

// fontSize is a text height in mm as a font size: a font's height is
// half its size in schematic units.
func fontSize(mm float64) int { return max(1, int(math.Round(mm/0.127))) }

// angleText writes an angle as Altium does, to the thousandth.
func angleText(v float64) string { return strconv.FormatFloat(v, 'f', 3, 64) }

// pointParams are a polyline's or polygon's points' parameters.
func pointParams(pts []cad.Pt) []string {
	out := []string{"LocationCount", strconv.Itoa(len(pts))}
	for i, p := range pts {
		out = append(out, coordParams(fmt.Sprintf("X%d", i+1), p.X)...)
		out = append(out, coordParams(fmt.Sprintf("Y%d", i+1), p.Y)...)
	}
	return out
}

// graphicRecord is a drawn shape's record on a symbol's body: a line, a
// rectangle, a circle as an ellipse of equal radii, an arc, a filled
// polygon as a polygon and an unfilled one as a closed polyline, and a
// text as a label in the font of its size, turned to the nearest
// quarter.
func (r *rec) graphicRecord(g cad.Graphic, fonts map[int]int) {
	kv := []string{"OwnerIndex", "0", "OwnerPartId", "1"}
	solid := "F"
	if g.Fill {
		solid = "T"
	}
	switch g.Kind {
	case cad.GraphicLine, cad.GraphicRect:
		kind := "13"
		a, b := g.Pts[0], g.Pts[1]
		if g.Kind == cad.GraphicRect {
			kind = "14"
			a, b = cad.Pt{X: math.Min(a.X, b.X), Y: math.Min(a.Y, b.Y)}, cad.Pt{X: math.Max(a.X, b.X), Y: math.Max(a.Y, b.Y)}
		}
		kv = append([]string{"RECORD", kind}, kv...)
		kv = append(kv, coordParams("Location.X", a.X)...)
		kv = append(kv, coordParams("Location.Y", a.Y)...)
		kv = append(kv, coordParams("Corner.X", b.X)...)
		kv = append(kv, coordParams("Corner.Y", b.Y)...)
		kv = append(kv, "LineWidth", lineWidth(g.Width), "Color", "128")
		if g.Kind == cad.GraphicRect {
			kv = append(kv, "AreaColor", "11599871", "IsSolid", solid)
		}
	case cad.GraphicCircle:
		kv = append([]string{"RECORD", "8"}, kv...)
		kv = append(kv, coordParams("Location.X", g.Pts[0].X)...)
		kv = append(kv, coordParams("Location.Y", g.Pts[0].Y)...)
		kv = append(kv, coordParams("Radius", g.R)...)
		kv = append(kv, coordParams("SecondaryRadius", g.R)...)
		kv = append(kv, "LineWidth", lineWidth(g.Width), "Color", "128", "AreaColor", "11599871", "IsSolid", solid)
	case cad.GraphicArc:
		o, rad, a, b := cad.ArcAngles(g)
		kv = append([]string{"RECORD", "12"}, kv...)
		kv = append(kv, coordParams("Location.X", o.X)...)
		kv = append(kv, coordParams("Location.Y", o.Y)...)
		kv = append(kv, coordParams("Radius", rad)...)
		kv = append(kv, "StartAngle", angleText(degrees(a)), "EndAngle", angleText(degrees(b)), "LineWidth", lineWidth(g.Width), "Color", "128")
	case cad.GraphicPoly:
		if g.Fill {
			kv = append([]string{"RECORD", "7"}, kv...)
			kv = append(kv, pointParams(g.Pts)...)
			kv = append(kv, "LineWidth", lineWidth(g.Width), "Color", "128", "AreaColor", "11599871", "IsSolid", "T")
		} else {
			kv = append([]string{"RECORD", "6"}, kv...)
			kv = append(kv, pointParams(append(append([]cad.Pt(nil), g.Pts...), g.Pts[0]))...)
			kv = append(kv, "LineWidth", lineWidth(g.Width), "Color", "128")
		}
	case cad.GraphicText:
		quarter := int(math.Round(degrees(g.Angle)/90)) % 4
		kv = append([]string{"RECORD", "4"}, kv...)
		kv = append(kv, coordParams("Location.X", g.Pts[0].X)...)
		kv = append(kv, coordParams("Location.Y", g.Pts[0].Y)...)
		if quarter != 0 {
			kv = append(kv, "Orientation", strconv.Itoa(quarter))
		}
		kv = append(kv, "Color", "128", "FontID", strconv.Itoa(fonts[fontSize(g.Size)]), "Text", g.Text)
	default:
		return
	}
	r.textRecord(kv...)
}

// symbolData is a symbol's Data stream; fonts are the library's font ids
// by size.
func symbolData(s Symbol, id string, fonts map[int]int) []byte {
	var r rec
	sy := s.Symbol
	r.textRecord("RECORD", "1", "LibReference", sy.Name, "ComponentDescription", sy.Description, "PartCount", "2", "DisplayModeCount", "1",
		"IndexInSheet", "-1", "OwnerPartId", "-1", "CurrentPartId", "1", "LibraryPath", "*", "SourceLibraryName", "*",
		"SheetPartFileName", "*", "TargetFileName", "*", "UniqueID", id, "AreaColor", "11599871", "Color", "128",
		"PartIDLocked", "T", "AllPinCount", strconv.Itoa(len(sy.Pins)))
	for _, p := range sy.Pins {
		r.pinRecord(p)
	}
	b := sy.Body
	rect := []string{"RECORD", "14", "OwnerIndex", "0", "OwnerPartId", "1"}
	rect = append(rect, coordParams("Location.X", b.X0)...)
	rect = append(rect, coordParams("Location.Y", b.Y0)...)
	rect = append(rect, coordParams("Corner.X", b.X1)...)
	rect = append(rect, coordParams("Corner.Y", b.Y1)...)
	rect = append(rect, "LineWidth", "1", "Color", "128", "AreaColor", "11599871", "IsSolid", "T")
	r.textRecord(rect...)
	for _, l := range sy.Extra {
		line := []string{"RECORD", "13", "OwnerIndex", "0", "OwnerPartId", "1"}
		line = append(line, coordParams("Location.X", l.A.X)...)
		line = append(line, coordParams("Location.Y", l.A.Y)...)
		line = append(line, coordParams("Corner.X", l.B.X)...)
		line = append(line, coordParams("Corner.Y", l.B.Y)...)
		r.textRecord(append(line, "LineWidth", "1", "Color", "128")...)
	}
	for _, g := range sy.Graphics {
		if g.Check() == nil {
			r.graphicRecord(g, fonts)
		}
	}
	top, bottom := b.Y1+1.27, b.Y0-2.54
	for _, p := range sy.Pins {
		top, bottom = math.Max(top, p.Y+1.27), math.Min(bottom, p.Y-2.54)
	}
	des := []string{"RECORD", "34", "OwnerIndex", "0", "OwnerPartId", "-1"}
	des = append(des, coordParams("Location.X", b.X0)...)
	des = append(des, coordParams("Location.Y", top)...)
	r.textRecord(append(des, "Color", "8388608", "FontID", "1", "Text", sy.Reference+"?", "Name", "Designator")...)
	com := []string{"RECORD", "41", "OwnerIndex", "0", "OwnerPartId", "-1"}
	com = append(com, coordParams("Location.X", b.X0)...)
	com = append(com, coordParams("Location.Y", bottom)...)
	r.textRecord(append(com, "Color", "8388608", "FontID", "1", "Text", sy.Value, "Name", "Comment")...)
	r.textRecord("RECORD", "44")
	if s.Footprint != "" {
		r.textRecord("RECORD", "45", "OwnerIndex", "0", "DESCRIPTION", "Footprint", "MODELNAME", s.Footprint, "MODELTYPE", "PCBLIB",
			"DATAFILECOUNT", "1", "MODELDATAFILEKIND1", "PCBLib", "MODELDATAFILEENTITY1", s.Footprint, "ISCURRENT", "T")
		r.textRecord("RECORD", "46")
		r.textRecord("RECORD", "48")
	}
	return r.Bytes()
}

// SchLib writes symbols as a schematic library.
func SchLib(name string, symbols []Symbol) ([]byte, error) {
	root := NewStorage()
	// The fonts: the designator's and comment's, then one for each size
	// of text drawn.
	fonts, sizes := map[int]int{10: 1}, []int{10}
	for _, s := range symbols {
		for _, g := range s.Symbol.Graphics {
			if g.Kind == cad.GraphicText && g.Check() == nil && fonts[fontSize(g.Size)] == 0 {
				sizes = append(sizes, fontSize(g.Size))
				fonts[fontSize(g.Size)] = len(sizes)
			}
		}
	}
	head := []string{"HEADER", "Protel for Windows - Schematic Library Editor Binary File Version 5.0", "Weight", strconv.Itoa(len(symbols)),
		"MinorVersion", "2", "UniqueID", uniqueID(name, "schlib"), "FontIdCount", strconv.Itoa(len(sizes))}
	for i, sz := range sizes {
		head = append(head, fmt.Sprintf("Size%d", i+1), strconv.Itoa(sz), fmt.Sprintf("FontName%d", i+1), "Times New Roman")
	}
	head = append(head, "UseMBCS", "T", "IsBOC", "T", "SheetStyle", "9", "BorderOn", "T", "SheetNumberSpaceSize", "12", "AreaColor", "16317695",
		"SnapGridOn", "T", "SnapGridSize", "10", "VisibleGridOn", "T", "VisibleGridSize", "10", "CustomX", "18000", "CustomY", "18000",
		"UseCustomSheet", "T", "ReferenceZonesOn", "T", "Display_Unit", "0", "CompCount", strconv.Itoa(len(symbols)))
	var keys []string
	for i, s := range symbols {
		head = append(head, fmt.Sprintf("LibRef%d", i), s.Symbol.Name, fmt.Sprintf("CompDescr%d", i), s.Symbol.Description, fmt.Sprintf("PartCount%d", i), "2")
		if k := sectionKey(s.Symbol.Name); k != s.Symbol.Name {
			keys = append(keys, s.Symbol.Name, k)
		}
	}
	var h rec
	h.params(head...)
	root.Stream("FileHeader", h.Bytes())
	if len(keys) > 0 {
		kv := []string{"KeyCount", strconv.Itoa(len(keys) / 2)}
		for i := 0; i < len(keys); i += 2 {
			kv = append(kv, fmt.Sprintf("LibRef%d", i/2), keys[i], fmt.Sprintf("SectionKey%d", i/2), keys[i+1])
		}
		var k rec
		k.params(kv...)
		root.Stream("SectionKeys", k.Bytes())
	}
	for _, s := range symbols {
		st := root.Storage(sectionKey(s.Symbol.Name))
		if s.Raw != nil {
			for _, path := range sortedPaths(s.Raw) {
				st.add(path, s.Raw[path])
			}
			continue
		}
		st.Stream("Data", symbolData(s, uniqueID(name, s.Symbol.Name), fonts))
	}
	var stor rec
	stor.params("HEADER", "Icon storage")
	root.Stream("Storage", stor.Bytes())
	return root.Bytes()
}

// uniqueID is a stable eight-letter id, as Altium gives each library and
// component.
func uniqueID(parts ...string) string {
	g := guid(parts...)
	var b strings.Builder
	for _, c := range g[:8] {
		b.WriteByte('A' + c%26)
	}
	return b.String()
}
