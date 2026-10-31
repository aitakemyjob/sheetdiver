package altium

import (
	"bytes"
	"compress/zlib"
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
	"unicode/utf16"

	"github.com/aitakemyjob/sheetdiver/internal/cad"
)

// rec builds a little-endian binary record.
type rec struct{ bytes.Buffer }

// u8 writes a byte.
func (r *rec) u8(v byte) { r.WriteByte(v) }

// u16 writes a 16-bit number.
func (r *rec) u16(v uint16) { r.Write(binary.LittleEndian.AppendUint16(nil, v)) }

// u32 writes a 32-bit number.
func (r *rec) u32(v uint32) { r.Write(binary.LittleEndian.AppendUint32(nil, v)) }

// f64 writes a double.
func (r *rec) f64(v float64) { r.Write(binary.LittleEndian.AppendUint64(nil, math.Float64bits(v))) }

// units is a length in mm as Altium's unit, a ten-thousandth of a mil,
// kept within 32 bits.
func units(mm float64) int32 {
	return int32(math.Max(math.MinInt32, math.Min(math.MaxInt32, math.Round(mm/0.0254*10000))))
}

// i32 writes a signed 32-bit number.
func (r *rec) i32(v int32) { _ = binary.Write(r, binary.LittleEndian, v) }

// coord writes a length in mm in Altium's unit.
func (r *rec) coord(mm float64) { r.i32(units(mm)) }

// pascal writes a short string: a length byte, then Windows-1252 bytes.
func (r *rec) pascal(s string) {
	b := latin1(s)
	if len(b) > 255 {
		b = b[:255]
	}
	r.u8(byte(len(b)))
	r.Write(b)
}

// block writes a length-prefixed block built by fn.
func (r *rec) block(fn func(*rec)) {
	var b rec
	fn(&b)
	r.u32(u32(b.Len()))
	r.Write(b.Bytes())
}

// stringBlock writes a block holding a short string.
func (r *rec) stringBlock(s string) { r.block(func(b *rec) { b.pascal(s) }) }

// params writes a block of |KEY=value parameters as a C string.
func (r *rec) params(kv ...string) {
	r.block(func(b *rec) {
		b.Write(latin1(paramText(kv...)))
		b.u8(0)
	})
}

// paramText joins keys and values as |KEY=value….
func paramText(kv ...string) string {
	var s strings.Builder
	for i := 0; i+1 < len(kv); i += 2 {
		s.WriteString("|" + kv[i] + "=" + strings.ReplaceAll(kv[i+1], "|", "/"))
	}
	return s.String()
}

// latin1 encodes text as Altium's single-byte strings do, ? for what
// doesn't fit.
func latin1(s string) []byte {
	var b []byte
	for _, r := range s {
		switch {
		case r < 256:
			b = append(b, byte(r))
		case r == '–' || r == '—':
			b = append(b, '-')
		case r == '×':
			b = append(b, 0xD7)
		default:
			b = append(b, '?')
		}
	}
	return b
}

// sectionKey is a footprint's storage name: its name cut to 31 characters,
// slashes made underscores.
func sectionKey(name string) string {
	if name == "" {
		return "_"
	}
	r := []rune(name)
	if len(r) > 31 {
		r = r[:31]
	}
	return strings.ReplaceAll(string(r), "/", "_")
}

// Layers.
const (
	layerTop        = 1
	layerTopOverlay = 33
	layerTopPaste   = 35
	layerMech13     = 69 // assembly outline
	layerMech15     = 71 // courtyard
	layerMulti      = 74
)

// v7Layer is the layer id Altium repeats in each primitive's tail.
func v7Layer(layer int) uint32 {
	switch {
	case layer >= 1 && layer <= 31:
		return 0x01000000 + u32(layer)
	case layer >= 57 && layer <= 72:
		return 0x01020000 + u32(layer-56)
	case layer == layerTopOverlay:
		return 0x01030006
	case layer == layerTopPaste:
		return 0x01030008
	}
	return 0x0103000F
}

// common writes the 13 bytes every primitive starts with: layer, flags
// (saved, unlocked), no net, polygon or component.
func (r *rec) common(layer int) {
	r.u8(byte(layer))
	r.u16(0x000C)
	r.u16(0xFFFF)
	r.u16(0xFFFF)
	r.u16(0xFFFF)
	r.u32(0xFFFFFFFF)
}

// padTail is the 141-byte tail of a pad's main record (offsets 61–201)
// as Altium writes it for a standard pad; a pad's own values go over it.
var padTail = []byte{
	0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0xA0, 0x86, 0x01, 0x00, 0x04, 0x00, 0xA0, 0x86, 0x01,
	0x00, 0x40, 0x0D, 0x03, 0x00, 0x40, 0x0D, 0x03, 0x00, 0x00, 0x00, 0x00, 0x00, 0x40, 0x9C, 0x00,
	0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x01, 0x01, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
	0x00, 0x00, 0x00, 0x00, 0x00, 0x0F, 0x00, 0x03, 0x01, 0x00, 0x00, 0x00, 0x40, 0x9C, 0x00, 0x00,
	0x00, 0x64, 0x9A, 0x92, 0x26, 0x10, 0xC7, 0xE4, 0x41, 0xA3, 0x2B, 0x29, 0x17, 0xA5, 0x35, 0x2E,
	0x67, 0x7F, 0xAB, 0x21, 0x20, 0xC3, 0x0B, 0x32, 0x47, 0xAD, 0xCE, 0x6C, 0xB7, 0xB8, 0xC9, 0x7E,
	0x68, 0x00, 0x00, 0x00, 0x00, 0xFF, 0xFF, 0xFF, 0x7F, 0xFF, 0xFF, 0xFF, 0x7F, 0x00, 0x01, 0x1A,
	0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x01, 0x01, 0x00, 0x00, 0x00,
	0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00,
}

// guid is a stable identity for a primitive, from what it is.
func guid(parts ...string) []byte {
	h := sha256.Sum256([]byte(strings.Join(parts, "\x00")))
	return h[:16]
}

// Pad shapes.
const (
	shapeRound = 1 // round, or an obround when its sides differ
	shapeRect  = 2
)

// pad writes a pad: on the top layer when surface-mount, on all layers
// with its hole when it goes through; a bare hole is an unplated pad the
// size of its hole. A pad whose paste is in windows of its own gets none.
func (r *rec) pad(fp string, i int, p cad.Pad) {
	r.u8(2)
	layer, hole, plated, w, h := layerTop, 0.0, true, p.W, p.H
	switch p.Kind {
	case cad.PadThru:
		layer, hole = layerMulti, p.Drill
	case cad.PadNPTH:
		layer, hole, plated, w, h = layerMulti, p.Drill, false, p.Drill, p.Drill
	}
	shape := byte(shapeRect)
	if p.Shape == "circle" || p.Shape == "oval" || p.Kind == cad.PadNPTH {
		shape = shapeRound
	}
	r.stringBlock(p.Number)
	r.stringBlock("")
	r.stringBlock("|&|0")
	r.block(func(b *rec) { b.u8(0) })
	r.block(func(b *rec) {
		b.common(layer)
		b.coord(p.X)
		b.coord(-p.Y)
		for k := 0; k < 3; k++ {
			b.coord(w)
			b.coord(h)
		}
		b.coord(hole)
		b.u8(shape)
		b.u8(shape)
		b.u8(shape)
		b.f64(0)
		if plated {
			b.u8(1)
		} else {
			b.u8(0)
		}
		tail := append([]byte{}, padTail...)
		put := func(off int, v uint32) { binary.LittleEndian.PutUint32(tail[off-61:], v) }
		if p.NoPaste {
			// Paste: manual, shrunk by half the pad so none is left.
			var e rec
			e.i32(units(-math.Max(w, h) / 2))
			copy(tail[86-61:], e.Bytes())
			tail[101-61] = 2
		}
		put(114, v7Layer(layer))
		copy(tail[126-61:], guid(fp, "pad", strconv.Itoa(i), "a"))
		copy(tail[142-61:], guid(fp, "pad", "b"))
		b.Write(tail)
	})
	r.u32(0) // no per-layer size and shape block
}

// track writes a line.
func (r *rec) track(layer int, a, b cad.Pt, w float64) {
	r.u8(4)
	r.block(func(t *rec) {
		t.common(layer)
		t.coord(a.X)
		t.coord(-a.Y)
		t.coord(b.X)
		t.coord(-b.Y)
		t.coord(w)
		t.u16(0)
		t.u32(0)
		t.u16(0)
		t.u32(v7Layer(layer))
		t.u8(0)
		t.Write([]byte{0, 0, 0})
	})
}

// fill writes a filled rectangle (a paste window), given by its corners
// in footprint coordinates; Altium's go low corner first, y up.
func (r *rec) fill(layer int, x0, y0, x1, y1 float64) {
	r.u8(6)
	r.block(func(f *rec) {
		f.common(layer)
		f.coord(math.Min(x0, x1))
		f.coord(math.Min(-y0, -y1))
		f.coord(math.Max(x0, x1))
		f.coord(math.Max(-y0, -y1))
		f.f64(0)
		f.u32(0)
		f.u8(0)
		f.u32(v7Layer(layer))
		f.u8(0)
		f.Write([]byte{0, 0, 0})
	})
}

// layerOf is the Altium layer a footprint drawing layer goes on.
var layerOf = map[string]int{cad.LayerSilk: layerTopOverlay, cad.LayerFab: layerMech13, cad.LayerCourtyard: layerMech15}

// footprintData is a footprint's Data stream and how many primitives it
// holds.
func footprintData(f cad.Footprint) ([]byte, int) {
	var r rec
	r.stringBlock(f.Name)
	n := 0
	for i, p := range f.Pads {
		if p.Kind == cad.PadPaste {
			continue
		}
		r.pad(f.Name, i, p)
		n++
	}
	for _, l := range f.Lines {
		r.track(layerOf[l.Layer], l.A, l.B, l.Width)
		n++
	}
	for _, p := range f.Polys {
		for i := range p.Pts {
			r.track(layerOf[p.Layer], p.Pts[i], p.Pts[(i+1)%len(p.Pts)], 0.12)
			n++
		}
	}
	for _, p := range f.Pads {
		if p.Kind == cad.PadPaste {
			r.fill(layerTopPaste, p.X-p.W/2, p.Y-p.H/2, p.X+p.W/2, p.Y+p.H/2)
			n++
		}
	}
	return r.Bytes(), n
}

// mils writes a length as Altium's mil-suffixed coordinate text.
func mils(mm float64) string {
	return strconv.FormatFloat(math.Round(mm/0.0254*1e4)/1e4, 'f', -1, 64) + "mil"
}

// header is a storage's Header stream: its record count.
func header(n int) []byte { return binary.LittleEndian.AppendUint32(nil, u32(n)) }

// Footprint is a footprint to write: drawn, or as the user left it in
// Altium (Raw), with its height and 3D model.
type Footprint struct {
	Footprint cad.Footprint
	Height    float64
	// Step is its 3D model, embedded in the library.
	Step []byte
	// Raw is the footprint as the user saved it, written back as it is.
	Raw *Raw
}

// Raw is a footprint or symbol as Altium saved it: its storage's streams
// by path ("Data", "UniqueIDPrimitiveInformation/Data"), and the
// embedded models its 3D bodies use.
type Raw struct {
	Streams map[string][]byte `json:"streams"`
	Models  []Model           `json:"models,omitempty"`
}

// Model is an embedded 3D model: its parameters and its compressed STEP.
type Model struct {
	ID     string `json:"id"`
	Params string `json:"params"`
	Data   []byte `json:"data"`
}

// stepChecksum is Altium's checksum of a model's STEP text: each byte
// weighted by its position (the first by one), summed modulo 2³².
func stepChecksum(b []byte) uint32 {
	var sum uint32
	for i, c := range b {
		w := uint32(1)
		if i > 0 {
			w = u32(i)
		}
		sum += uint32(c) * w
	}
	return sum
}

// modelID is a footprint's model's id, a braced GUID.
func modelID(name string) string {
	g := guid(name, "model")
	return fmt.Sprintf("{%X-%X-%X-%X-%X}", g[0:4], g[4:6], g[6:8], g[8:10], g[10:16])
}

// compress zlib-compresses a model.
func compress(b []byte) []byte {
	var out bytes.Buffer
	w := zlib.NewWriter(&out)
	_, _ = w.Write(b)
	_ = w.Close()
	return out.Bytes()
}

// body writes the 3D body that places an embedded model on a footprint:
// on Mechanical 1, over the footprint's body, as tall as the model.
func (r *rec) body(f cad.Footprint, height float64, id string, step []byte) {
	r.u8(12)
	r.block(func(b *rec) {
		b.common(57)
		b.u32(0)
		b.u8(0)
		kv := []string{"V7_LAYER", "MECHANICAL1", "NAME", "", "KIND", "0", "SUBPOLYINDEX", "-1", "UNIONINDEX", "0", "ARCRESOLUTION", "0.5mil",
			"ISSHAPEBASED", "FALSE", "CAVITYHEIGHT", "0mil", "STANDOFFHEIGHT", "0mil", "OVERALLHEIGHT", mils(height), "BODYPROJECTION", "0",
			"ARCRESOLUTION", "0.5mil", "BODYCOLOR3D", "14737632", "BODYOPACITY3D", "1.000", "IDENTIFIER", "", "TEXTURE", "",
			"TEXTURECENTERX", "0mil", "TEXTURECENTERY", "0mil", "TEXTURESIZEX", "0mil", "TEXTURESIZEY", "0mil",
			"TEXTUREROTATION", " 0.00000000000000E+0000", "MODELID", id, "MODEL.CHECKSUM", strconv.FormatUint(uint64(stepChecksum(step)), 10),
			"MODEL.EMBED", "TRUE", "MODEL.NAME", f.Name + ".step", "MODEL.2D.X", "0mil", "MODEL.2D.Y", "0mil", "MODEL.2D.ROTATION", "0.000",
			"MODEL.3D.ROTX", "0.000", "MODEL.3D.ROTY", "0.000", "MODEL.3D.ROTZ", "0.000", "MODEL.3D.DZ", "0mil", "MODEL.MODELTYPE", "1"}
		// Altium writes this list without a leading separator.
		b.block(func(c *rec) {
			c.Write(latin1(strings.TrimPrefix(paramText(kv...), "|")))
			c.u8(0)
		})
		// Its outline: the body's rectangle, y up, in Altium's units.
		o := f.Body
		pts := [][2]float64{{o.X0, -o.Y1}, {o.X1, -o.Y1}, {o.X1, -o.Y0}, {o.X0, -o.Y0}}
		b.u32(4)
		for _, p := range pts {
			b.f64(float64(units(p[0])))
			b.f64(float64(units(p[1])))
		}
	})
}

// sortedPaths are a storage's stream paths in order, so a library is
// written the same way each time.
func sortedPaths(m map[string][]byte) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// add puts a stream at a path below a storage, making storages on the way.
func (s *Storage) add(path string, data []byte) {
	parts := strings.Split(path, "/")
	st := s
	for _, dir := range parts[:len(parts)-1] {
		var next *Storage
		for _, c := range st.storages {
			if c.name == dir {
				next = c
			}
		}
		if next == nil {
			next = st.Storage(dir)
		}
		st = next
	}
	st.Stream(parts[len(parts)-1], data)
}

// PcbLib writes footprints as a PCB library, their 3D models embedded.
func PcbLib(name string, fps []Footprint) ([]byte, error) {
	root := NewStorage()
	var h rec
	const version = "PCB 6.0 Binary Library File"
	h.u32(u32(len(version)))
	h.pascal(version)
	h.f64(5.01)
	id := uniqueID(name, "pcblib")
	h.u32(u32(len(id)))
	h.pascal(id)
	root.Stream("FileHeader", h.Bytes())

	var keys rec
	nkeys := 0
	for _, f := range fps {
		if sectionKey(f.Footprint.Name) != f.Footprint.Name {
			keys.stringBlock(f.Footprint.Name)
			keys.stringBlock(sectionKey(f.Footprint.Name))
			nkeys++
		}
	}
	if nkeys > 0 {
		root.Stream("SectionKeys", append(header(nkeys), keys.Bytes()...))
	}

	// The models: the user's, as they saved them, and SheetDiver's.
	var models []Model
	seen := map[string]bool{}
	for _, f := range fps {
		switch {
		case f.Raw != nil:
			for _, m := range f.Raw.Models {
				if !seen[m.ID] {
					seen[m.ID] = true
					models = append(models, m)
				}
			}
		case len(f.Step) > 0:
			mid := modelID(f.Footprint.Name)
			if !seen[mid] {
				seen[mid] = true
				models = append(models, Model{ID: mid, Data: compress(f.Step), Params: fmt.Sprintf("EMBED=TRUE|MODELSOURCE=Undefined|ID=%s|ROTX=0.000|ROTY=0.000|ROTZ=0.000|DZ=0|CHECKSUM=%d|NAME=%s.step",
					mid, int32(stepChecksum(f.Step)), f.Footprint.Name)}) //nolint:gosec // Altium stores the checksum's bits as a signed number
			}
		}
	}

	lib := root.Storage("Library")
	lib.Stream("Header", header(1))
	var data rec
	data.params("HEADER", version, "WEIGHT", strconv.Itoa(len(fps)))
	data.u32(u32(len(fps)))
	var toc strings.Builder
	for _, f := range fps {
		data.stringBlock(f.Footprint.Name)
		pads := 0
		for _, p := range f.Footprint.Pads {
			if p.Terminal() {
				pads++
			}
		}
		fmt.Fprintf(&toc, "Name=%s|Pad Count=%d|Height=%s|Description=%s\r\n", f.Footprint.Name, pads, mils(f.Height), strings.ReplaceAll(f.Footprint.Description, "|", "/"))
	}
	lib.Stream("Data", data.Bytes())
	ms := lib.Storage("Models")
	ms.Stream("Header", header(len(models)))
	var md rec
	for i, m := range models {
		t := append(latin1(m.Params), 0)
		md.u32(u32(len(t)))
		md.Write(t)
		ms.Stream(strconv.Itoa(i), m.Data)
	}
	ms.Stream("Data", md.Bytes())
	lkm := lib.Storage("LayerKindMapping")
	lkm.Stream("Header", header(1))
	var l rec
	text := utf16.Encode([]rune("1.0\x00"))
	l.u32(u32(2 * len(text)))
	for _, c := range text {
		l.u16(c)
	}
	l.u32(0)
	l.u32(0)
	lkm.Stream("Data", l.Bytes())
	pvl := lib.Storage("PadViaLibrary")
	pvl.Stream("Header", header(0))
	g := guid(name, "padvialibrary")
	var p rec
	pt := latin1(fmt.Sprintf("|PADVIALIBRARY.LIBRARYID={%X-%X-%X-%X-%X}|PADVIALIBRARY.LIBRARYNAME=%s|PADVIALIBRARY.DISPLAYUNITS=1", g[0:4], g[4:6], g[6:8], g[8:10], g[10:16], name))
	p.u32(u32(len(pt) + 1))
	p.Write(pt)
	p.u8(0)
	pvl.Stream("Data", p.Bytes())
	tocStore := lib.Storage("ComponentParamsTOC")
	tocStore.Stream("Header", header(1))
	tb := append(latin1(toc.String()), 0)
	tocStore.Stream("Data", append(header(len(tb)), tb...))
	for _, empty := range []string{"Textures", "ModelsNoEmbed"} {
		s := lib.Storage(empty)
		s.Stream("Header", header(0))
		s.Stream("Data", nil)
	}

	for _, f := range fps {
		st := root.Storage(sectionKey(f.Footprint.Name))
		if f.Raw != nil {
			for _, path := range sortedPaths(f.Raw.Streams) {
				st.add(path, f.Raw.Streams[path])
			}
			continue
		}
		d, n := footprintData(f.Footprint)
		if len(f.Step) > 0 {
			var b rec
			b.body(f.Footprint, f.Height, modelID(f.Footprint.Name), f.Step)
			d = append(d, b.Bytes()...)
			n++
		}
		st.Stream("Header", header(n))
		var params rec
		params.params("PATTERN", f.Footprint.Name, "HEIGHT", mils(f.Height), "DESCRIPTION", f.Footprint.Description, "ITEMGUID", "", "REVISIONGUID", "")
		st.Stream("Parameters", params.Bytes())
		var ws rec
		ws.params()
		st.Stream("WideStrings", ws.Bytes())
		st.Stream("Data", d)
	}
	return root.Bytes()
}
