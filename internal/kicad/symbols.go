package kicad

import (
	"math"
	"regexp"
	"slices"
	"strings"
)

// Symbols. A box (an IC, a connector) gets a symbol generated from its
// pin table; a transistor one generated with its own pin numbers in the
// standard transistor's shape; a two-terminal (resistor, capacitor,
// diode…) uses the symbol of KiCad's Device library, drawn here with the
// same pins in the same places, so updating it from the library keeps
// every wire. Which builder draws which kind is the SymbolFactory's;
// a new kind is a new builder registered with it.

// grid is KiCad's schematic grid (50 mil): pins end on it so wires
// and labels connect.
const grid = 2.54

// PinPlace is where a symbol's pin connects (library coordinates, Y up)
// and the side it leaves the symbol from.
type PinPlace struct {
	X, Y float64
	Out  Side
}

// Rect is an area in library coordinates.
type Rect struct {
	X0, Y0, X1, Y1 float64
}

// LibSymbol is a symbol a component is drawn with: its library ID, its
// pins by number, the area it covers with its pins, the reference prefix
// it's annotated with, and how to write its definition under a name.
type LibSymbol struct {
	ID     string
	Pins   map[string]PinPlace
	Bounds Rect
	Prefix string
	// Ref and Val are where the reference and value are written.
	Ref, Val textPlace
	def      func(name string) *node
}

// textPlace is where a symbol's field is written (library coordinates)
// and which way it's justified.
type textPlace struct {
	x, y    float64
	justify string
}

// at is the field's place in the library.
func (t textPlace) node() *node { return at(t.x, t.y, 0) }

// effects is how the field is drawn.
func (t textPlace) effects(hidden bool) *node {
	if t.justify == "" {
		return effects(hidden)
	}
	return effects(hidden, t.justify)
}

// Name is the symbol's name in its library ("R" of "Device:R").
func (s LibSymbol) Name() string {
	_, name, _ := strings.Cut(s.ID, ":")
	return name
}

// Library is the library the symbol is in ("Device" of "Device:R").
func (s LibSymbol) Library() string {
	lib, _, _ := strings.Cut(s.ID, ":")
	return lib
}

// SymbolBuilder draws the symbol of a component.
type SymbolBuilder interface {
	Build(c Component) LibSymbol
}

// SymbolFactory picks the builder for a component's kind.
type SymbolFactory struct {
	builders map[Kind]SymbolBuilder
	fallback SymbolBuilder
}

// NewSymbolFactory has a builder for every Kind: KiCad's Device symbols
// for the two-terminals, generated transistors, and generated boxes for
// the rest (and any kind it doesn't know).
func NewSymbolFactory() *SymbolFactory {
	f := &SymbolFactory{builders: map[Kind]SymbolBuilder{}, fallback: boxBuilder{prefix: "U"}}
	f.Register(KindConnector, boxBuilder{prefix: "J"})
	for kind, spec := range twoTerminals {
		f.Register(kind, spec)
	}
	for _, kind := range []Kind{KindMOSFETN, KindMOSFETP, KindNPN, KindPNP} {
		f.Register(kind, transistorBuilder{kind: kind})
	}
	return f
}

// Register sets the builder for a kind, replacing any before it.
func (f *SymbolFactory) Register(kind Kind, b SymbolBuilder) {
	f.builders[kind] = b
}

// Build draws a component's symbol with its kind's builder.
func (f *SymbolFactory) Build(c Component) LibSymbol {
	if b, ok := f.builders[c.Kind]; ok {
		return b.Build(c)
	}
	return f.fallback.Build(c)
}

// TerminalPin is the pin number a two-terminal's or transistor's
// terminal has on its symbol ("anode" of a diode is "2", as in KiCad's
// Device:D); for a transistor the number comes from the part, so it's
// "" here.
func TerminalPin(kind Kind, terminal string) string {
	if spec, ok := twoTerminals[kind]; ok {
		return spec.terminals[strings.ToLower(terminal)]
	}
	return ""
}

// Library symbol definitions.

// symbolDef starts a symbol definition with its properties.
func symbolDef(name, prefix, value string, hidePinNumbers, hidePinNames bool, power bool, ref, val textPlace) *node {
	n := list("symbol", name)
	if power {
		n.add(list("power"))
	}
	if hidePinNumbers {
		n.add(list("pin_numbers", sym("hide")))
	}
	pn := list("pin_names", list("offset", 0.508))
	if hidePinNames {
		pn.add(sym("hide"))
	}
	n.add(pn, list("in_bom", yesNo(!power)), list("on_board", yesNo(!power)))
	n.add(list("property", "Reference", prefix, ref.node(), ref.effects(power)))
	n.add(list("property", "Value", value, val.node(), val.effects(false)))
	n.add(list("property", "Footprint", "", at(0, 0, 0), effects(true)))
	n.add(list("property", "Datasheet", "~", at(0, 0, 0), effects(true)))
	return n
}

// unit is a symbol's graphics or pins sub-symbol ("R_0_1", "R_1_1").
func unit(name string, style int, items ...*node) *node {
	n := list("symbol", name+"_"+string(rune('0'+style/10))+"_"+string(rune('0'+style%10)))
	for _, it := range items {
		n.add(it)
	}
	return n
}

// libPin is a pin in a symbol definition.
func libPin(t ElectricalType, number, name string, x, y, angle, length float64, hidden bool) *node {
	n := list("pin", sym(t), sym("line"), at(x, y, angle), list("length", length))
	if hidden {
		n.add(sym("hide"))
	}
	if name == "" {
		name = "~"
	}
	return n.add(list("name", name, effects(false)), list("number", number, effects(false)))
}

// polyline is a line through points.
func polyline(width float64, filled string, pts ...[2]float64) *node {
	p := list("pts")
	for _, pt := range pts {
		p.add(xy(pt[0], pt[1]))
	}
	return list("polyline", p, stroke(width), fill(filled))
}

// rectangle is a rectangle between two corners.
func rectangle(x0, y0, x1, y1, width float64, filled string) *node {
	return list("rectangle", list("start", x0, y0), list("end", x1, y1), stroke(width), fill(filled))
}

// circle is a circle.
func circle(x, y, r, width float64, filled string) *node {
	return list("circle", list("center", x, y), list("radius", r), stroke(width), fill(filled))
}

// arc is an arc through three points.
func arc(sx, sy, mx, my, ex, ey float64) *node {
	return list("arc", list("start", sx, sy), list("mid", mx, my), list("end", ex, ey), stroke(0), fill("none"))
}

// Two-terminals.

// twoTerminalSpec is a KiCad Device symbol: its pins (number, name,
// where), which terminal each is, and its graphics.
type twoTerminalSpec struct {
	id        string
	prefix    string
	pins      [2]specPin
	terminals map[string]string
	graphics  func() []*node
	names     bool
}

// specPin is a pin of a standard symbol.
type specPin struct {
	number, name     string
	x, y, angle, len float64
}

// Build draws the Device symbol; a two-terminal's value is its own.
func (s twoTerminalSpec) Build(Component) LibSymbol {
	out := LibSymbol{ID: s.id, Prefix: s.prefix, Pins: map[string]PinPlace{}}
	for _, p := range s.pins {
		out.Pins[p.number] = PinPlace{X: p.x, Y: p.y, Out: outward(p.angle)}
		out.Bounds = grow(out.Bounds, p.x, p.y)
	}
	out.Bounds = grow(grow(out.Bounds, -2.54, -2.54), 2.54, 2.54)
	out.Ref, out.Val = textPlace{3.048, 1.27, "left"}, textPlace{3.048, -1.27, "left"}
	if s.pins[0].x != 0 { // horizontal: above and below
		out.Ref, out.Val = textPlace{0, 3.81, ""}, textPlace{0, -3.81, ""}
	}
	out.def = func(name string) *node {
		base := name
		if _, after, ok := strings.Cut(name, ":"); ok {
			base = after
		}
		n := symbolDef(name, s.prefix, s.prefix, true, !s.names, false, out.Ref, out.Val)
		n.add(unit(base, 1, s.graphics()...))
		pins := unit(base, 11)
		for _, p := range s.pins {
			pins.add(libPin(Passive, p.number, p.name, p.x, p.y, p.angle, p.len, false))
		}
		return n.add(pins)
	}
	return out
}

// outward is the side a pin leaves its symbol from, by its angle (the
// direction from its end into the symbol).
func outward(angle float64) Side {
	switch int(angle) {
	case 0:
		return Left
	case 180:
		return Right
	case 270:
		return Top
	}
	return Bottom
}

// grow extends an area to take in a point.
func grow(r Rect, x, y float64) Rect {
	return Rect{math.Min(r.X0, x), math.Min(r.Y0, y), math.Max(r.X1, x), math.Max(r.Y1, y)}
}

// Pins of the Device symbols: vertical ones from 1 at the top to 2 at
// the bottom, diodes from the cathode on the left to the anode.
var (
	vertical1270 = [2]specPin{{"1", "~", 0, 3.81, 270, 1.27}, {"2", "~", 0, -3.81, 90, 1.27}}
	diodePins    = [2]specPin{{"1", "K", -3.81, 0, 0, 2.54}, {"2", "A", 3.81, 0, 180, 2.54}}
	plain        = map[string]string{"1": "1", "2": "2"}
	diodeEnds    = map[string]string{"cathode": "1", "anode": "2"}
)

// diodeBody is a diode's triangle and bar, pointing at its cathode on
// the left.
func diodeBody() []*node {
	return []*node{
		polyline(0.254, "none", [2]float64{1.27, 1.27}, [2]float64{1.27, -1.27}, [2]float64{-1.27, 0}, [2]float64{1.27, 1.27}),
		polyline(0.254, "none", [2]float64{-1.27, 1.27}, [2]float64{-1.27, -1.27}),
		polyline(0, "none", [2]float64{1.27, 0}, [2]float64{-1.27, 0}),
	}
}

// twoTerminals are the Device symbols, by kind.
var twoTerminals = map[Kind]twoTerminalSpec{
	KindResistor: {id: "Device:R", prefix: "R", pins: vertical1270, terminals: plain, graphics: func() []*node {
		return []*node{rectangle(-1.016, -2.54, 1.016, 2.54, 0.254, "none")}
	}},
	KindCapacitor: {id: "Device:C", prefix: "C", pins: [2]specPin{{"1", "~", 0, 3.81, 270, 2.794}, {"2", "~", 0, -3.81, 90, 2.794}}, terminals: plain, graphics: func() []*node {
		return []*node{
			polyline(0.508, "none", [2]float64{-2.032, 0.762}, [2]float64{2.032, 0.762}),
			polyline(0.508, "none", [2]float64{-2.032, -0.762}, [2]float64{2.032, -0.762}),
		}
	}},
	KindCapacitorPolarised: {id: "Device:C_Polarized", prefix: "C", pins: [2]specPin{{"1", "~", 0, 3.81, 270, 2.794}, {"2", "~", 0, -3.81, 90, 2.794}}, terminals: map[string]string{"+": "1", "-": "2", "1": "1", "2": "2"}, graphics: func() []*node {
		return []*node{
			rectangle(-2.286, 0.508, 2.286, 1.016, 0.254, "none"),
			rectangle(-2.286, -0.508, 2.286, -1.016, 0.254, "outline"),
			polyline(0, "none", [2]float64{-1.778, 2.286}, [2]float64{-0.762, 2.286}),
			polyline(0, "none", [2]float64{-1.27, 2.794}, [2]float64{-1.27, 1.778}),
		}
	}},
	KindInductor: {id: "Device:L", prefix: "L", pins: vertical1270, terminals: plain, graphics: func() []*node {
		var out []*node
		for i := 0; i < 4; i++ {
			y := 2.54 - 1.27*float64(i)
			out = append(out, arc(0, y, 0.635, y-0.635, 0, y-1.27))
		}
		return out
	}},
	KindDiode: {id: "Device:D", prefix: "D", pins: diodePins, terminals: diodeEnds, graphics: diodeBody},
	KindSchottky: {id: "Device:D_Schottky", prefix: "D", pins: diodePins, terminals: diodeEnds, graphics: func() []*node {
		return append(diodeBody(), polyline(0.254, "none", [2]float64{-1.905, 0.635}, [2]float64{-1.905, 1.27}, [2]float64{-1.27, 1.27}), polyline(0.254, "none", [2]float64{-1.27, -1.27}, [2]float64{-0.635, -1.27}, [2]float64{-0.635, -0.635}))
	}},
	KindZener: {id: "Device:D_Zener", prefix: "D", pins: diodePins, terminals: diodeEnds, graphics: func() []*node {
		return append(diodeBody(), polyline(0.254, "none", [2]float64{-1.27, -1.27}, [2]float64{-0.762, -1.27}))
	}},
	KindTVS: {id: "Device:D_TVS", prefix: "D", pins: [2]specPin{{"1", "A1", -3.81, 0, 0, 2.54}, {"2", "A2", 3.81, 0, 180, 2.54}}, terminals: map[string]string{"cathode": "1", "anode": "2", "1": "1", "2": "2"}, graphics: func() []*node {
		return []*node{
			polyline(0.254, "none", [2]float64{-2.54, 1.27}, [2]float64{0, 0}, [2]float64{-2.54, -1.27}, [2]float64{-2.54, 1.27}),
			polyline(0.254, "none", [2]float64{2.54, 1.27}, [2]float64{0, 0}, [2]float64{2.54, -1.27}, [2]float64{2.54, 1.27}),
			polyline(0.254, "none", [2]float64{0.508, 1.27}, [2]float64{0, 1.27}, [2]float64{0, -1.27}, [2]float64{-0.508, -1.27}),
		}
	}},
	KindLED: {id: "Device:LED", prefix: "D", pins: diodePins, terminals: diodeEnds, graphics: func() []*node {
		return append(diodeBody(),
			polyline(0, "none", [2]float64{-0.508, 1.524}, [2]float64{0.508, 2.54}),
			polyline(0, "none", [2]float64{0.508, 1.524}, [2]float64{1.524, 2.54}))
	}},
	KindFuse: {id: "Device:Fuse", prefix: "F", pins: vertical1270, terminals: plain, graphics: func() []*node {
		return []*node{rectangle(-0.762, -2.54, 0.762, 2.54, 0.254, "none"), polyline(0, "none", [2]float64{0, 2.54}, [2]float64{0, -2.54})}
	}},
	KindFerrite: {id: "Device:FerriteBead", prefix: "FB", pins: [2]specPin{{"1", "~", 0, 3.81, 270, 2.54}, {"2", "~", 0, -3.81, 90, 2.54}}, terminals: plain, graphics: func() []*node {
		return []*node{polyline(0.254, "none", [2]float64{-2.032, 0.254}, [2]float64{-0.508, 1.778}, [2]float64{2.032, -0.762}, [2]float64{0.508, -2.286}, [2]float64{-2.032, 0.254})}
	}},
	KindCrystal: {id: "Device:Crystal", prefix: "Y", pins: [2]specPin{{"1", "1", -3.81, 0, 0, 1.27}, {"2", "2", 3.81, 0, 180, 1.27}}, terminals: plain, graphics: func() []*node {
		return []*node{
			rectangle(-1.143, -1.905, 1.143, 1.905, 0.254, "none"),
			polyline(0.508, "none", [2]float64{-2.54, -1.27}, [2]float64{-2.54, 1.27}),
			polyline(0.508, "none", [2]float64{2.54, -1.27}, [2]float64{2.54, 1.27}),
		}
	}},
	KindBattery: {id: "Device:Battery_Cell", prefix: "BT", pins: [2]specPin{{"1", "+", 0, 5.08, 270, 2.54}, {"2", "-", 0, -2.54, 90, 2.54}}, terminals: map[string]string{"+": "1", "-": "2", "1": "1", "2": "2"}, graphics: func() []*node {
		return []*node{
			polyline(0.254, "none", [2]float64{-2.032, 1.778}, [2]float64{2.032, 1.778}),
			polyline(0.508, "none", [2]float64{-1.27, 1.016}, [2]float64{1.27, 1.016}),
			polyline(0, "none", [2]float64{0, 2.54}, [2]float64{0, 1.778}),
			polyline(0, "none", [2]float64{0, 1.016}, [2]float64{0, 0}),
		}
	}},
	KindSwitch: {id: "Switch:SW_Push", prefix: "SW", pins: [2]specPin{{"1", "1", -5.08, 0, 0, 2.54}, {"2", "2", 5.08, 0, 180, 2.54}}, terminals: plain, graphics: func() []*node {
		return []*node{
			circle(-2.032, 0, 0.508, 0, "none"),
			circle(2.032, 0, 0.508, 0, "none"),
			polyline(0, "none", [2]float64{-2.54, 1.27}, [2]float64{2.54, 1.27}),
			polyline(0, "none", [2]float64{0, 1.27}, [2]float64{0, 3.048}),
		}
	}},
}

// Transistors.

// transistorBuilder draws a transistor in the shape of KiCad's
// Q_NMOS_GSD and Q_NPN_BEC (gate or base on the left, drain or collector
// on top, source or emitter at the bottom), with the part's own pin
// numbers, so one symbol fits whichever order its package has.
type transistorBuilder struct {
	kind Kind
}

// Build draws the transistor of a component whose pins are named by
// terminal.
func (t transistorBuilder) Build(c Component) LibSymbol {
	type terminal struct {
		name     string
		x, y, an float64
		typ      ElectricalType
	}
	mos := t.kind == KindMOSFETN || t.kind == KindMOSFETP
	terms := map[string]terminal{
		"gate": {"G", -5.08, 0, 0, Input}, "drain": {"D", 2.54, 5.08, 270, Passive}, "source": {"S", 2.54, -5.08, 90, Passive},
		"base": {"B", -5.08, 0, 0, Input}, "collector": {"C", 2.54, 5.08, 270, Passive}, "emitter": {"E", 2.54, -5.08, 90, Passive},
	}
	out := LibSymbol{ID: "SheetDiver:" + symbolName(c), Prefix: "Q", Pins: map[string]PinPlace{}, Bounds: Rect{-5.08, -5.08, 3.81, 5.08},
		Ref: textPlace{5.08, 1.27, "left"}, Val: textPlace{5.08, -1.27, "left"}}
	var pins []*node
	for _, p := range c.Pins {
		term, ok := terms[strings.ToLower(p.Name)]
		if !ok || p.Number == "" {
			continue
		}
		out.Pins[p.Number] = PinPlace{X: term.x, Y: term.y, Out: outward(term.an)}
		pins = append(pins, libPin(term.typ, p.Number, term.name, term.x, term.y, term.an, 2.54, false))
	}
	out.def = func(name string) *node {
		base := name
		if _, after, ok := strings.Cut(name, ":"); ok {
			base = after
		}
		n := symbolDef(name, "Q", c.Value, false, true, false, out.Ref, out.Val)
		var body []*node
		if mos {
			body = []*node{
				circle(0.762, 0, 2.794, 0.254, "none"),
				polyline(0.254, "none", [2]float64{-1.778, 1.905}, [2]float64{-1.778, -1.905}),
				polyline(0, "none", [2]float64{-2.54, 0}, [2]float64{-1.778, 0}),
				polyline(0.254, "none", [2]float64{-1.27, 2.286}, [2]float64{-1.27, -2.286}),
				polyline(0, "none", [2]float64{-1.27, 1.778}, [2]float64{2.54, 1.778}, [2]float64{2.54, 2.54}),
				polyline(0, "none", [2]float64{-1.27, -1.778}, [2]float64{2.54, -1.778}, [2]float64{2.54, -2.54}),
				polyline(0, "none", [2]float64{-1.27, 0}, [2]float64{2.54, 0}, [2]float64{2.54, -1.778}),
			}
			if t.kind == KindMOSFETN {
				body = append(body, polyline(0, "outline", [2]float64{-1.016, 0}, [2]float64{0, 0.508}, [2]float64{0, -0.508}, [2]float64{-1.016, 0}))
			} else {
				body = append(body, polyline(0, "outline", [2]float64{0.508, 0}, [2]float64{-0.508, 0.508}, [2]float64{-0.508, -0.508}, [2]float64{0.508, 0}))
			}
		} else {
			body = []*node{
				circle(0.762, 0, 2.794, 0.254, "none"),
				polyline(0.508, "none", [2]float64{-0.635, 1.905}, [2]float64{-0.635, -1.905}),
				polyline(0, "none", [2]float64{-2.54, 0}, [2]float64{-0.635, 0}),
				polyline(0, "none", [2]float64{-0.635, 0.635}, [2]float64{2.54, 2.54}),
				polyline(0, "none", [2]float64{-0.635, -0.635}, [2]float64{2.54, -2.54}),
			}
			if t.kind == KindNPN {
				body = append(body, polyline(0, "outline", [2]float64{1.778, -1.778}, [2]float64{1.27, -0.762}, [2]float64{0.762, -1.27}, [2]float64{1.778, -1.778}))
			} else {
				body = append(body, polyline(0, "outline", [2]float64{0.127, -1.016}, [2]float64{0.635, -2.032}, [2]float64{1.143, -1.524}, [2]float64{0.127, -1.016}))
			}
		}
		n.add(unit(base, 1, body...))
		return n.add(unit(base, 11, pins...))
	}
	return out
}

// Boxes.

// boxBuilder generates a rectangle with the component's pins: each on
// its side in the order given, top and bottom pins from the left.
type boxBuilder struct {
	prefix string
}

// charWidth is about how wide a character of a pin name is drawn.
const charWidth = 1.05

// Build lays out the box around the pins' names.
func (b boxBuilder) Build(c Component) LibSymbol {
	sides := map[Side][]Pin{}
	longest := map[Side]int{}
	for _, p := range c.Pins {
		side := p.Side
		if side == "" {
			side = Left
		}
		sides[side] = append(sides[side], p)
		longest[side] = max(longest[side], len([]rune(p.Name)))
	}
	value := c.Value
	if value == "" {
		value = c.MPN
	}
	// Top and bottom pins' names read down into the box: the side pins
	// start below them. The top and bottom pins go from the right, so
	// the reference above the box and the value under it, at the left,
	// stay clear of their labels.
	zone := func(side Side) float64 {
		if len(sides[side]) == 0 {
			return 0
		}
		return snapUp(float64(longest[side])*charWidth+1.27, grid)
	}
	top, bottom := zone(Top), zone(Bottom)
	rows := max(len(sides[Left]), len(sides[Right]), 1)
	cols := max(len(sides[Top]), len(sides[Bottom]))
	text := float64(max(len([]rune(value)), len(c.Ref), 4)) * charWidth
	width := snapUp(max(grid*float64(cols)+text+grid, float64(longest[Left]+longest[Right])*charWidth+2*grid), 2*grid)
	height := top + grid*float64(rows+1) + bottom
	hw := width / 2

	out := LibSymbol{ID: "SheetDiver:" + symbolName(c), Prefix: b.prefix, Pins: map[string]PinPlace{}, Bounds: Rect{-hw, -height, hw, 0}}
	var pins []*node
	place := func(p Pin, x, y, angle float64) {
		number := pinNumber(p)
		out.Pins[number] = PinPlace{X: x, Y: y, Out: outward(angle)}
		out.Bounds = grow(out.Bounds, x, y)
		t := p.Type
		if t == "" {
			t = Passive
		}
		pins = append(pins, libPin(t, number, p.Name, x, y, angle, grid, false))
	}
	for i, p := range sides[Left] {
		place(p, -hw-grid, -top-grid*float64(i+1), 0)
	}
	for i, p := range sides[Right] {
		place(p, hw+grid, -top-grid*float64(i+1), 180)
	}
	for i, p := range sides[Top] {
		place(p, hw-grid*float64(i+1), grid, 270)
	}
	for i, p := range sides[Bottom] {
		place(p, hw-grid*float64(i+1), -height-grid, 90)
	}
	out.Ref, out.Val = textPlace{-hw, 1.27, "left"}, textPlace{-hw, -height - 1.27, "left"}
	out.def = func(name string) *node {
		base := name
		if _, after, ok := strings.Cut(name, ":"); ok {
			base = after
		}
		// A block's pins have names only: its numbers would repeat them.
		numbered := slices.ContainsFunc(c.Pins, func(p Pin) bool { return p.Number != "" })
		n := symbolDef(name, b.prefix, value, !numbered, false, false, out.Ref, out.Val)
		n.add(unit(base, 1, rectangle(-hw, 0, hw, -height, 0.254, "background")))
		return n.add(unit(base, 11, pins...))
	}
	return out
}

// pinNumber is a pin's number on its symbol: its pad, else its name (a
// block's pins have only names).
func pinNumber(p Pin) string {
	if p.Number != "" {
		return p.Number
	}
	return p.Name
}

// snapUp rounds a length up to a multiple of step.
func snapUp(v, step float64) float64 {
	return math.Ceil(v/step-1e-9) * step
}

// unsafeName is what a symbol name can't hold.
var unsafeName = regexp.MustCompile(`[^A-Za-z0-9_.+-]+`)

// symbolName is the name a generated symbol is saved under: the part
// number, else the reference.
func symbolName(c Component) string {
	name := c.Symbol
	if name == "" {
		name = c.MPN
	}
	if name == "" {
		name = c.Ref
	}
	return strings.Trim(unsafeName.ReplaceAllString(name, "_"), "_")
}

// groundSymbol is KiCad's power:GND, drawn where a pin meets ground.
func groundSymbol() LibSymbol {
	return LibSymbol{ID: "power:GND", Prefix: "#PWR", Pins: map[string]PinPlace{"1": {0, 0, Top}}, Bounds: Rect{-1.27, -2.54, 1.27, 0},
		Ref: textPlace{0, -6.35, ""}, Val: textPlace{0, -3.81, ""}, def: func(name string) *node {
			n := symbolDef(name, "#PWR", "GND", false, false, true, textPlace{0, -6.35, ""}, textPlace{0, -3.81, ""})
			n.add(unit("GND", 1, polyline(0, "none", [2]float64{0, 0}, [2]float64{0, -1.27}, [2]float64{1.27, -1.27}, [2]float64{0, -2.54}, [2]float64{-1.27, -1.27}, [2]float64{0, -1.27})))
			return n.add(unit("GND", 11, list("pin", sym(PowerIn), sym("line"), at(0, 0, 270), list("length", 0), sym("hide"), list("name", "GND", effects(false)), list("number", "1", effects(false)))))
		}}
}
