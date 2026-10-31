package altium

import (
	"bytes"
	"compress/zlib"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math"
	"sort"
	"strconv"
	"strings"
	"unicode/utf16"

	"github.com/aitakemyjob/sheetdiver/internal/cad"
)

// Reading Altium libraries back: the compound file's streams, then each
// footprint's and symbol's records as SheetDiver's model, with the raw
// streams kept so an edit can be written back as the user left it.

// ReadCompound reads a compound file's streams by path ("Library/Data").
func ReadCompound(b []byte) (map[string][]byte, error) {
	le := binary.LittleEndian
	if len(b) < 512 || !bytes.Equal(b[:8], []byte{0xD0, 0xCF, 0x11, 0xE0, 0xA1, 0xB1, 0x1A, 0xE1}) {
		return nil, errors.New("not a compound file")
	}
	shift := le.Uint16(b[30:])
	if shift < 9 || shift > 12 {
		return nil, fmt.Errorf("unexpected sector size 2^%d", shift)
	}
	ssz := 1 << shift
	msz := 1 << le.Uint16(b[32:])
	cutoff := le.Uint32(b[56:])
	sector := func(id uint32) ([]byte, error) {
		off := (int(id) + 1) * ssz
		if id >= fatSect || off+ssz > len(b) {
			return nil, fmt.Errorf("sector %d out of the file", id)
		}
		return b[off : off+ssz], nil
	}
	// The FAT's sectors: the header's list, then the DIFAT chain's.
	var fatSectors []uint32
	for i := 0; i < 109; i++ {
		if v := le.Uint32(b[76+4*i:]); v < fatSect {
			fatSectors = append(fatSectors, v)
		}
	}
	for d, n := le.Uint32(b[68:]), 0; d < fatSect && n < 1<<16; n++ {
		s, err := sector(d)
		if err != nil {
			return nil, err
		}
		for i := 0; i < ssz/4-1; i++ {
			if v := le.Uint32(s[4*i:]); v < fatSect {
				fatSectors = append(fatSectors, v)
			}
		}
		d = le.Uint32(s[ssz-4:])
	}
	var fat []uint32
	for _, id := range fatSectors {
		s, err := sector(id)
		if err != nil {
			return nil, err
		}
		for i := 0; i < ssz; i += 4 {
			fat = append(fat, le.Uint32(s[i:]))
		}
	}
	chain := func(start uint32, table []uint32, read func(uint32) ([]byte, error)) ([]byte, error) {
		var out []byte
		for id, n := start, 0; id < fatSect; n++ {
			if n > len(table) || int(id) >= len(table) {
				return nil, errors.New("a broken sector chain")
			}
			s, err := read(id)
			if err != nil {
				return nil, err
			}
			out = append(out, s...)
			id = table[id]
		}
		return out, nil
	}
	dir, err := chain(le.Uint32(b[48:]), fat, sector)
	if err != nil {
		return nil, err
	}
	type dirEntry struct {
		name               string
		kind               byte
		left, right, child uint32
		start              uint32
		size               uint64
	}
	var entries []dirEntry
	for i := 0; i+128 <= len(dir); i += 128 {
		e := dir[i : i+128]
		n := int(le.Uint16(e[64:]))/2 - 1
		if n < 0 || n > 31 {
			n = 0
		}
		u := make([]uint16, n)
		for k := range u {
			u[k] = le.Uint16(e[2*k:])
		}
		entries = append(entries, dirEntry{string(utf16.Decode(u)), e[66], le.Uint32(e[68:]), le.Uint32(e[72:]), le.Uint32(e[76:]), le.Uint32(e[116:]), le.Uint64(e[120:])})
	}
	if len(entries) == 0 || entries[0].kind != 5 {
		return nil, errors.New("no root entry")
	}
	root := entries[0]
	mini, err := chain(root.start, fat, sector)
	if err != nil {
		return nil, err
	}
	miniFATBytes, err := chain(le.Uint32(b[60:]), fat, sector)
	if err != nil {
		return nil, err
	}
	miniFAT := make([]uint32, len(miniFATBytes)/4)
	for i := range miniFAT {
		miniFAT[i] = le.Uint32(miniFATBytes[4*i:])
	}
	miniSector := func(id uint32) ([]byte, error) {
		off := int(id) * msz
		if off+msz > len(mini) {
			return nil, fmt.Errorf("mini sector %d out of the mini stream", id)
		}
		return mini[off : off+msz], nil
	}
	out := map[string][]byte{}
	seen := map[uint32]bool{}
	var walk func(id uint32, prefix string) error
	walk = func(id uint32, prefix string) error {
		if id == noStream || int(id) >= len(entries) || seen[id] {
			return nil
		}
		seen[id] = true
		e := entries[id]
		if err := walk(e.left, prefix); err != nil {
			return err
		}
		switch e.kind {
		case 1:
			if err := walk(e.child, prefix+e.name+"/"); err != nil {
				return err
			}
		case 2:
			var data []byte
			var err error
			switch {
			case e.size == 0:
			case e.size < uint64(cutoff):
				data, err = chain(e.start, miniFAT, miniSector)
			default:
				data, err = chain(e.start, fat, sector)
			}
			if err != nil {
				return fmt.Errorf("%s%s: %w", prefix, e.name, err)
			}
			if uint64(len(data)) < e.size {
				return fmt.Errorf("%s%s is short", prefix, e.name)
			}
			out[prefix+e.name] = data[:e.size]
		}
		return walk(e.right, prefix)
	}
	if err := walk(root.child, ""); err != nil {
		return nil, err
	}
	return out, nil
}

// reader reads Altium's little-endian records.
type reader struct {
	b   []byte
	pos int
	err error
}

// take reads n bytes.
func (r *reader) take(n int) []byte {
	if r.err != nil || n < 0 || r.pos+n > len(r.b) {
		r.err = errors.New("a record runs past its stream")
		return make([]byte, max(n, 0))
	}
	s := r.b[r.pos : r.pos+n]
	r.pos += n
	return s
}

// u8 reads a byte.
func (r *reader) u8() byte { return r.take(1)[0] }

// u32 reads a 32-bit number.
func (r *reader) u32() uint32 { return binary.LittleEndian.Uint32(r.take(4)) }

// block reads a length-prefixed block.
func (r *reader) block() []byte { return r.take(int(r.u32() & 0xFFFFFF)) }

// pascal reads a short string from a block.
func pascal(b []byte) string {
	if len(b) == 0 || int(b[0]) > len(b)-1 {
		return ""
	}
	return fromLatin1(b[1 : 1+int(b[0])])
}

// fromLatin1 decodes single-byte text.
func fromLatin1(b []byte) string {
	r := make([]rune, len(b))
	for i, c := range b {
		r[i] = rune(c)
	}
	return string(r)
}

// parseParams reads |KEY=value… text, keys upper case.
func parseParams(b []byte) map[string]string {
	b = bytes.TrimRight(b, "\x00")
	out := map[string]string{}
	for _, part := range strings.Split(fromLatin1(b), "|") {
		if k, v, ok := strings.Cut(part, "="); ok {
			out[strings.ToUpper(strings.TrimSpace(k))] = v
		}
	}
	return out
}

// paramsBlock reads a length-prefixed parameter block.
func paramsBlock(b []byte) map[string]string {
	r := &reader{b: b}
	return parseParams(r.block())
}

// i32 is a little-endian signed number at an offset (0 past the end).
func i32(b []byte, off int) int32 {
	if off+4 > len(b) {
		return 0
	}
	return int32(binary.LittleEndian.Uint32(b[off:])) //nolint:gosec // the bits are a signed number
}

// mmAt is an Altium length at an offset, in mm.
func mmAt(b []byte, off int) float64 { return float64(i32(b, off)) * 0.0254 / 10000 }

// f64At is a double at an offset.
func f64At(b []byte, off int) float64 {
	if off+8 > len(b) {
		return 0
	}
	return math.Float64frombits(binary.LittleEndian.Uint64(b[off:]))
}

// layerName is the footprint drawing layer an Altium layer reads as ("" to
// leave out).
var layerName = map[byte]string{layerTopOverlay: cad.LayerSilk, layerMech13: cad.LayerFab, layerMech15: cad.LayerCourtyard}

// ReadFootprint is a footprint read from a PCB library.
type ReadFootprint struct {
	Footprint cad.Footprint
	Height    float64
	// Model is its embedded 3D model's STEP text, if it has one.
	Model []byte
	Raw   Raw
}

// ReadPcbLib reads a PCB library's footprints.
func ReadPcbLib(b []byte) ([]ReadFootprint, error) {
	streams, err := ReadCompound(b)
	if err != nil {
		return nil, err
	}
	// The embedded models, by id.
	models := map[string]Model{}
	if d, ok := streams["Library/Models/Data"]; ok {
		r := &reader{b: d}
		for i := 0; r.pos < len(d) && r.err == nil; i++ {
			text := r.take(int(r.u32()))
			p := parseParams(text)
			models[p["ID"]] = Model{ID: p["ID"], Params: strings.TrimRight(fromLatin1(text), "\x00"), Data: streams["Library/Models/"+strconv.Itoa(i)]}
		}
	}
	var dirs []string
	for path := range streams {
		if dir, ok := strings.CutSuffix(path, "/Parameters"); ok && !strings.Contains(dir, "/") {
			dirs = append(dirs, dir)
		}
	}
	sort.Strings(dirs)
	var out []ReadFootprint
	for _, dir := range dirs {
		params := paramsBlock(streams[dir+"/Parameters"])
		f, err := readFootprint(streams[dir+"/Data"])
		if err != nil {
			return nil, fmt.Errorf("footprint %s: %w", params["PATTERN"], err)
		}
		rf := ReadFootprint{Footprint: f.Footprint, Raw: Raw{Streams: map[string][]byte{}}}
		rf.Footprint.Name, rf.Footprint.Description = params["PATTERN"], params["DESCRIPTION"]
		if h, err := strconv.ParseFloat(strings.TrimSuffix(params["HEIGHT"], "mil"), 64); err == nil {
			rf.Height = cad.Number(h * 0.0254)
		}
		for path, s := range streams {
			if rest, ok := strings.CutPrefix(path, dir+"/"); ok {
				rf.Raw.Streams[rest] = s
			}
		}
		for _, id := range f.models {
			if m, ok := models[id]; ok {
				rf.Raw.Models = append(rf.Raw.Models, m)
				if z, err := zlib.NewReader(bytes.NewReader(m.Data)); err == nil {
					rf.Model, _ = io.ReadAll(z)
				}
			}
		}
		out = append(out, rf)
	}
	return out, nil
}

// readFP is a footprint read from its Data stream, with the ids of the
// models its 3D bodies place.
type readFP struct {
	cad.Footprint
	models []string
}

// readFootprint reads a footprint's records: pads, lines, arcs and fills
// on the layers SheetDiver draws, and 3D bodies.
func readFootprint(d []byte) (readFP, error) {
	var f readFP
	r := &reader{b: d}
	r.block() // its name
	add := func(layer byte, a, b cad.Pt, w float64) {
		if l, ok := layerName[layer]; ok {
			f.Lines = append(f.Lines, cad.Line{Layer: l, A: a, B: b, Width: w})
		}
	}
	for r.pos < len(d) && r.err == nil {
		switch kind := r.u8(); kind {
		case 2:
			name := pascal(r.block())
			r.block()
			r.block()
			r.block()
			m := r.block()
			r.block()
			f.Pads = append(f.Pads, readPad(name, m))
		case 4:
			t := r.block()
			add(t[0], cad.Pt{X: mmAt(t, 13), Y: -mmAt(t, 17)}, cad.Pt{X: mmAt(t, 21), Y: -mmAt(t, 25)}, mmAt(t, 29))
		case 1:
			a := r.block()
			cx, cy, rad := mmAt(a, 13), mmAt(a, 17), mmAt(a, 21)
			start, end := f64At(a, 25), f64At(a, 33)
			if end <= start {
				end += 360
			}
			n := max(4, int((end-start)/15))
			for i := 0; i < n; i++ {
				t0, t1 := (start+(end-start)*float64(i)/float64(n))*math.Pi/180, (start+(end-start)*float64(i+1)/float64(n))*math.Pi/180
				add(a[0], cad.Pt{X: cx + rad*math.Cos(t0), Y: -(cy + rad*math.Sin(t0))}, cad.Pt{X: cx + rad*math.Cos(t1), Y: -(cy + rad*math.Sin(t1))}, mmAt(a, 41))
			}
		case 6:
			b := r.block()
			x0, y0, x1, y1 := mmAt(b, 13), -mmAt(b, 17), mmAt(b, 21), -mmAt(b, 25)
			switch b[0] {
			case layerTopPaste:
				f.Pads = append(f.Pads, cad.Pad{Kind: cad.PadPaste, Shape: "roundrect", X: cad.Number((x0 + x1) / 2), Y: cad.Number((y0 + y1) / 2),
					W: cad.Number(math.Abs(x1 - x0)), H: cad.Number(math.Abs(y1 - y0))})
			case layerTopOverlay:
				f.Polys = append(f.Polys, cad.Poly{Layer: cad.LayerSilk, Pts: []cad.Pt{{X: x0, Y: y0}, {X: x1, Y: y0}, {X: x1, Y: y1}, {X: x0, Y: y1}}})
			}
		case 12:
			b := r.block()
			if len(b) > 18 {
				f.models = append(f.models, paramsBlock(b[18:])["MODELID"])
			}
		case 5:
			r.block()
			r.block()
		case 3, 11:
			r.block()
		default:
			return f, fmt.Errorf("a record of unknown kind %d", kind)
		}
	}
	if r.err != nil {
		return f, r.err
	}
	// Thermal vias are holes numbered like a surface pad.
	smd := map[string]bool{}
	for _, p := range f.Pads {
		if p.Kind == cad.PadSMD {
			smd[p.Number] = true
		}
	}
	var body, crt cad.Rect
	grow := func(r *cad.Rect, p cad.Pt) {
		if *r == (cad.Rect{}) {
			*r = cad.Rect{X0: p.X, Y0: p.Y, X1: p.X, Y1: p.Y}
			return
		}
		*r = cad.Rect{X0: math.Min(r.X0, p.X), Y0: math.Min(r.Y0, p.Y), X1: math.Max(r.X1, p.X), Y1: math.Max(r.Y1, p.Y)}
	}
	for i := range f.Pads {
		f.Pads[i].Via = f.Pads[i].Kind == cad.PadThru && smd[f.Pads[i].Number]
	}
	for _, l := range f.Lines {
		switch l.Layer {
		case cad.LayerFab:
			grow(&body, l.A)
			grow(&body, l.B)
		case cad.LayerCourtyard:
			grow(&crt, l.A)
			grow(&crt, l.B)
		}
	}
	f.Body, f.Courtyard = body, crt
	f.From = "your edit in Altium"
	f.Mount = cad.MountSMD
	for _, p := range f.Pads {
		if p.Kind == cad.PadThru && !p.Via {
			f.Mount = cad.MountThru
		}
	}
	return f, nil
}

// readPad reads a pad's main record.
func readPad(name string, m []byte) cad.Pad {
	p := cad.Pad{Number: name, X: cad.Number(mmAt(m, 13)), Y: cad.Number(-mmAt(m, 17)), W: cad.Number(mmAt(m, 21)), H: cad.Number(mmAt(m, 25))}
	if r := math.Mod(math.Abs(f64At(m, 52)), 180); r > 45 && r < 135 {
		p.W, p.H = p.H, p.W
	}
	hole := cad.Number(mmAt(m, 45))
	plated := len(m) > 60 && m[60] != 0
	switch {
	case hole > 0 && !plated:
		p.Kind, p.Number, p.Drill = cad.PadNPTH, "", hole
	case hole > 0:
		p.Kind, p.Drill = cad.PadThru, hole
	default:
		p.Kind = cad.PadSMD
	}
	switch {
	case len(m) > 49 && m[49] == shapeRound && p.W == p.H:
		p.Shape = "circle"
	case len(m) > 49 && m[49] == shapeRound:
		p.Shape = "oval"
	default:
		p.Shape = "rect"
	}
	// Paste taken off: manual, shrunk by at least half the pad.
	if p.Kind == cad.PadSMD && len(m) > 101 && m[101] == 2 && mmAt(m, 86) <= -math.Min(p.W, p.H)/2+1e-3 {
		p.NoPaste = true
	}
	return p
}

// ReadSymbol is a symbol read from a schematic library.
type ReadSymbol struct {
	Symbol    cad.Symbol
	Footprint string
	Raw       map[string][]byte
}

// electricalType is a pin's type from Altium's.
var electricalType = []string{cad.TypeInput, cad.TypeBidir, cad.TypeOutput, cad.TypeOpenDrain, cad.TypePassive, cad.TypeTriState, cad.TypeOutput, cad.TypePowerIn}

// sides are the sides a pin's direction puts it on.
var sides = []string{cad.Right, cad.Top, cad.Left, cad.Bottom}

// schAt is a schematic coordinate parameter in mm, with its fraction.
func schAt(p map[string]string, key string) float64 {
	n, _ := strconv.ParseFloat(p[key], 64)
	f, _ := strconv.ParseFloat(p[key+"_FRAC"], 64)
	return (n + f/1e5) * 0.254
}

// ReadSchLib reads a schematic library's symbols.
func ReadSchLib(b []byte) ([]ReadSymbol, error) {
	streams, err := ReadCompound(b)
	if err != nil {
		return nil, err
	}
	head := paramsBlock(streams["FileHeader"])
	keys := map[string]string{}
	if k, ok := streams["SectionKeys"]; ok {
		kp := paramsBlock(k)
		n, _ := strconv.Atoi(kp["KEYCOUNT"])
		for i := 0; i < n; i++ {
			keys[kp[fmt.Sprintf("LIBREF%d", i)]] = kp[fmt.Sprintf("SECTIONKEY%d", i)]
		}
	}
	n, _ := strconv.Atoi(head["COMPCOUNT"])
	var out []ReadSymbol
	for i := 0; i < n; i++ {
		name := head[fmt.Sprintf("LIBREF%d", i)]
		dir := keys[name]
		if dir == "" {
			dir = sectionKey(name)
		}
		d, ok := streams[dir+"/Data"]
		if !ok {
			return nil, fmt.Errorf("symbol %s has no data", name)
		}
		s, fp, err := readSymbol(d)
		if err != nil {
			return nil, fmt.Errorf("symbol %s: %w", name, err)
		}
		s.Name = name
		rs := ReadSymbol{Symbol: s, Footprint: fp, Raw: map[string][]byte{}}
		for path, st := range streams {
			if rest, ok := strings.CutPrefix(path, dir+"/"); ok {
				rs.Raw[rest] = st
			}
		}
		out = append(out, rs)
	}
	return out, nil
}

// readSymbol reads a symbol's records: its pins, body, lines, designator,
// comment and current footprint (part 1 of a multi-part symbol).
func readSymbol(d []byte) (cad.Symbol, string, error) {
	var s cad.Symbol
	var fp string
	haveBody := false
	r := &reader{b: d}
	line := func(a, b cad.Pt) { s.Extra = append(s.Extra, cad.Line{Layer: "body", A: a, B: b, Width: 0.254}) }
	for r.pos+4 <= len(d) && r.err == nil {
		h := r.u32()
		body := r.take(int(h & 0xFFFFFF))
		if h>>24 == 1 {
			if p, ok := readPin(body); ok {
				s.Pins = append(s.Pins, p)
			}
			continue
		}
		p := parseParams(body)
		if part := p["OWNERPARTID"]; part != "" && part != "1" && part != "-1" {
			continue
		}
		switch p["RECORD"] {
		case "1":
			s.Description = p["COMPONENTDESCRIPTION"]
		case "14":
			rect := cad.Rect{X0: schAt(p, "LOCATION.X"), Y0: schAt(p, "LOCATION.Y"), X1: schAt(p, "CORNER.X"), Y1: schAt(p, "CORNER.Y")}
			rect = cad.Rect{X0: math.Min(rect.X0, rect.X1), Y0: math.Min(rect.Y0, rect.Y1), X1: math.Max(rect.X0, rect.X1), Y1: math.Max(rect.Y0, rect.Y1)}
			if !haveBody {
				s.Body, haveBody = rect, true
				continue
			}
			c := []cad.Pt{{X: rect.X0, Y: rect.Y0}, {X: rect.X1, Y: rect.Y0}, {X: rect.X1, Y: rect.Y1}, {X: rect.X0, Y: rect.Y1}}
			for i := range c {
				line(c[i], c[(i+1)%4])
			}
		case "13":
			line(cad.Pt{X: schAt(p, "LOCATION.X"), Y: schAt(p, "LOCATION.Y")}, cad.Pt{X: schAt(p, "CORNER.X"), Y: schAt(p, "CORNER.Y")})
		case "6":
			n, _ := strconv.Atoi(p["LOCATIONCOUNT"])
			for i := 1; i < n; i++ {
				line(cad.Pt{X: schAt(p, fmt.Sprintf("X%d", i)), Y: schAt(p, fmt.Sprintf("Y%d", i))}, cad.Pt{X: schAt(p, fmt.Sprintf("X%d", i+1)), Y: schAt(p, fmt.Sprintf("Y%d", i+1))})
			}
		case "34":
			s.Reference = strings.TrimRight(p["TEXT"], "?")
		case "41":
			if strings.EqualFold(p["NAME"], "Comment") {
				s.Value = p["TEXT"]
			}
		case "45":
			if strings.EqualFold(p["MODELTYPE"], "PCBLIB") && (fp == "" || p["ISCURRENT"] == "T") {
				fp = p["MODELNAME"]
			}
		}
	}
	return s, fp, r.err
}

// readPin reads a binary pin record: its connection point is one length
// beyond its location, the way it points.
func readPin(b []byte) (cad.SymbolPin, bool) {
	r := &reader{b: b}
	r.take(4 + 1 + 2 + 1 + 4)
	r.take(int(r.u8())) // description
	r.u8()              // formal type
	et := int(r.u8())
	flags := r.u8()
	nums := r.take(6)
	r.take(4) // colour
	name := fromLatin1(r.take(int(r.u8())))
	number := fromLatin1(r.take(int(r.u8())))
	if r.err != nil {
		return cad.SymbolPin{}, false
	}
	at := func(i int) float64 {
		return float64(int16(binary.LittleEndian.Uint16(nums[2*i:]))) * 0.254 //nolint:gosec // the bits are a signed number
	}
	length, x, y := at(0), at(1), at(2)
	side := sides[flags&3]
	switch side {
	case cad.Right:
		x += length
	case cad.Top:
		y += length
	case cad.Left:
		x -= length
	default:
		y -= length
	}
	typ := cad.TypePassive
	if et < len(electricalType) {
		typ = electricalType[et]
	}
	return cad.SymbolPin{Number: number, Name: name, Type: typ, Side: side, X: cad.Number(x), Y: cad.Number(y), Length: cad.Number(length), Hidden: flags&4 != 0}, true
}

// DecodeFootprint is the footprint a raw footprint holds.
func DecodeFootprint(r Raw) (cad.Footprint, error) {
	f, err := readFootprint(r.Streams["Data"])
	if err != nil {
		return cad.Footprint{}, err
	}
	p := paramsBlock(r.Streams["Parameters"])
	f.Name, f.Description = p["PATTERN"], p["DESCRIPTION"]
	return f.Footprint, nil
}

// DecodeSymbol is the symbol a raw symbol's streams hold, and its
// footprint.
func DecodeSymbol(streams map[string][]byte) (cad.Symbol, string, error) {
	return readSymbol(streams["Data"])
}
