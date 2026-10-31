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
	Wires      []xWire `xml:"wire"`
	Rectangles []struct {
		X1    float64 `xml:"x1,attr"`
		Y1    float64 `xml:"y1,attr"`
		X2    float64 `xml:"x2,attr"`
		Y2    float64 `xml:"y2,attr"`
		Layer int     `xml:"layer,attr"`
	} `xml:"rectangle"`
	Polygons []struct {
		Layer    int `xml:"layer,attr"`
		Vertices []struct {
			X float64 `xml:"x,attr"`
			Y float64 `xml:"y,attr"`
		} `xml:"vertex"`
	} `xml:"polygon"`
}

// xWire is a wire.
type xWire struct {
	X1    float64 `xml:"x1,attr"`
	Y1    float64 `xml:"y1,attr"`
	X2    float64 `xml:"x2,attr"`
	Y2    float64 `xml:"y2,attr"`
	Width float64 `xml:"width,attr"`
	Layer int     `xml:"layer,attr"`
}

// xSymbol is a symbol element.
type xSymbol struct {
	Name  string  `xml:"name,attr"`
	Wires []xWire `xml:"wire"`
	Pins  []struct {
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
	layer := map[int]string{layerTPlace: cad.LayerSilk, layerTDocu: cad.LayerFab}
	for _, w := range x.Wires {
		if l, ok := layer[w.Layer]; ok {
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
	var box cad.Rect
	for i, w := range x.Wires {
		r := cad.Rect{X0: math.Min(w.X1, w.X2), Y0: math.Min(w.Y1, w.Y2), X1: math.Max(w.X1, w.X2), Y1: math.Max(w.Y1, w.Y2)}
		if i == 0 {
			box = r
			continue
		}
		box = cad.Rect{X0: math.Min(box.X0, r.X0), Y0: math.Min(box.Y0, r.Y0), X1: math.Max(box.X1, r.X1), Y1: math.Max(box.Y1, r.Y1)}
	}
	s.Body = box
	onEdge := func(w xWire) bool {
		return (w.X1 == w.X2 && (w.X1 == box.X0 || w.X1 == box.X1)) || (w.Y1 == w.Y2 && (w.Y1 == box.Y0 || w.Y1 == box.Y1))
	}
	for _, w := range x.Wires {
		if !onEdge(w) {
			s.Extra = append(s.Extra, cad.Line{Layer: "body", A: cad.Pt{X: w.X1, Y: w.Y1}, B: cad.Pt{X: w.X2, Y: w.Y2}, Width: w.Width})
		}
	}
	return s
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
