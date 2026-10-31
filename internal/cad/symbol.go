package cad

import (
	"fmt"
	"math"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/aitakemyjob/sheetdiver/internal/pinrole"
)

// Grid is the schematic grid symbols are drawn on (100 mil): pins end on
// it so wires snap to them.
const Grid = 2.54

// PinLength is a symbol pin's length.
const PinLength = 2.54

// Sides of a symbol.
const (
	Left   = "left"
	Right  = "right"
	Top    = "top"
	Bottom = "bottom"
)

// Pin electrical types, as KiCad's ERC knows them (Eagle's are mapped
// from them).
const (
	TypeInput       = "input"
	TypeOutput      = "output"
	TypeBidir       = "bidirectional"
	TypeTriState    = "tri_state"
	TypePassive     = "passive"
	TypePowerIn     = "power_in"
	TypePowerOut    = "power_out"
	TypeOpenDrain   = "open_collector"
	TypeNoConnect   = "no_connect"
	TypeUnspecified = "unspecified"
)

// pinTypes are the types a pin may be given.
var pinTypes = map[string]bool{TypeInput: true, TypeOutput: true, TypeBidir: true, TypeTriState: true, TypePassive: true,
	TypePowerIn: true, TypePowerOut: true, TypeOpenDrain: true, TypeNoConnect: true, TypeUnspecified: true}

// PinInfo is a pin as the datasheet's pin table gives it.
type PinInfo struct {
	Number      string `json:"number"`
	Name        string `json:"name"`
	Type        string `json:"type,omitempty"`
	Description string `json:"description,omitempty"`
}

// PinPlace is where a symbol's pin goes and what it is, when the agent or
// user says so rather than leaving it to its role.
type PinPlace struct {
	Number string `json:"number"`
	// Side is left, right, top or bottom.
	Side string `json:"side,omitempty"`
	// Position is the pin's slot on its side, from the top (left and
	// right) or the left (top and bottom), counting from 1; slots left
	// free are gaps. 0 leaves it to pin-number order after the placed ones.
	Position int `json:"position,omitempty"`
	// Type is the pin's electrical type.
	Type string `json:"type,omitempty"`
	// Name renames the pin on the symbol.
	Name   string `json:"name,omitempty"`
	Hidden bool   `json:"hidden,omitempty"`
}

// SymbolSpec is how a symbol is drawn beyond what the pin table says.
type SymbolSpec struct {
	// Reference is the designator prefix (U, Q, D…).
	Reference string     `json:"reference,omitempty"`
	Pins      []PinPlace `json:"pins,omitempty"`
}

// SymbolPin is a pin as drawn: X and Y are where a wire connects.
type SymbolPin struct {
	Number string  `json:"number"`
	Name   string  `json:"name"`
	Type   string  `json:"type"`
	Side   string  `json:"side"`
	X      float64 `json:"x"`
	Y      float64 `json:"y"`
	Length float64 `json:"length"`
	Hidden bool    `json:"hidden,omitempty"`
}

// Symbol is a schematic symbol: a box with its pins around it, y up.
type Symbol struct {
	Name        string      `json:"name"`
	Reference   string      `json:"reference"`
	Value       string      `json:"value"`
	Footprint   string      `json:"footprint,omitempty"`
	Datasheet   string      `json:"datasheet,omitempty"`
	Description string      `json:"description,omitempty"`
	Body        Rect        `json:"body"`
	Pins        []SymbolPin `json:"pins"`
	// Extra are lines drawn besides the body: what a symbol edited in a
	// CAD tool has beyond its box.
	Extra []Line `json:"extra,omitempty"`
	// Graphics are drawn free-hand on the body, y up.
	Graphics []Graphic `json:"graphics,omitempty"`
}

// sideOf is the side a pin goes on by its role: supplies on top, grounds
// and pads along the bottom, outputs and unconnected pins on the right,
// everything else on the left.
func sideOf(r pinrole.Role) string {
	switch r {
	case pinrole.Ground, pinrole.Pad:
		return Bottom
	case pinrole.Power:
		return Top
	case pinrole.Output, pinrole.NC:
		return Right
	}
	return Left
}

var (
	openDrainRe = regexp.MustCompile(`(?i)open[- ]?(drain|collector)`)
	powerOutRe  = regexp.MustCompile(`(?i)^(V?OUT\w*|VREG\w*|VCAP\w*|VCORE\w*|VLDO\w*|REGOUT)$`)
)

// typeOf is a pin's electrical type from its role, name and description.
func typeOf(p PinInfo, r pinrole.Role) string {
	switch r {
	case pinrole.Power, pinrole.Ground:
		return TypePowerIn
	case pinrole.Output:
		switch {
		case openDrainRe.MatchString(p.Description):
			return TypeOpenDrain
		case powerOutRe.MatchString(p.Name) && !strings.Contains(strings.ToLower(p.Description), "signal"):
			return TypePowerOut
		}
		return TypeOutput
	case pinrole.Input:
		return TypeInput
	case pinrole.Bidir:
		return TypeBidir
	case pinrole.NC:
		return TypeNoConnect
	}
	return TypePassive
}

// pinNumberRe splits a pin number into its letters and digits ("A12").
var pinNumberRe = regexp.MustCompile(`^([A-Za-z]*)(\d+)$`)

// LessPinNumber orders pin numbers as a pin table lists them: integers
// numerically, then grid numbers by row letter and column (A2 before A10,
// before B1), then anything else as text.
func LessPinNumber(a, b string) bool {
	ia, erra := strconv.Atoi(a)
	ib, errb := strconv.Atoi(b)
	switch {
	case erra == nil && errb == nil:
		return ia < ib
	case erra == nil:
		return true
	case errb == nil:
		return false
	}
	ma, mb := pinNumberRe.FindStringSubmatch(a), pinNumberRe.FindStringSubmatch(b)
	switch {
	case ma != nil && mb != nil:
		if !strings.EqualFold(ma[1], mb[1]) {
			return len(ma[1]) < len(mb[1]) || len(ma[1]) == len(mb[1]) && strings.ToUpper(ma[1]) < strings.ToUpper(mb[1])
		}
		na, _ := strconv.Atoi(ma[2])
		nb, _ := strconv.Atoi(mb[2])
		return na < nb
	case ma != nil:
		return true
	case mb != nil:
		return false
	}
	return strings.ToUpper(a) < strings.ToUpper(b)
}

// placed is a pin on its way to the symbol.
type placed struct {
	SymbolPin
	pos int // the slot asked for, from 1; 0 is any
}

// MakeSymbol draws a part's symbol from its pin table and the spec.
func MakeSymbol(name string, pins []PinInfo, spec SymbolSpec) (Symbol, []Problem) {
	var ps problems
	if len(pins) == 0 {
		ps.errf("no pins: record the pin table first (record_pins)")
		return Symbol{Name: name}, ps
	}
	places := map[string]PinPlace{}
	for _, pl := range spec.Pins {
		if pl.Side != "" && pl.Side != Left && pl.Side != Right && pl.Side != Top && pl.Side != Bottom {
			ps.errf("pin %s: side %q isn't left, right, top or bottom", pl.Number, pl.Side)
		}
		if pl.Type != "" && !pinTypes[pl.Type] {
			ps.errf("pin %s: type %q isn't one of %s", pl.Number, pl.Type, strings.Join(sortedKeys(pinTypes), ", "))
		}
		places[strings.ToUpper(pl.Number)] = pl
	}
	seen := map[string]bool{}
	var all []placed
	for _, p := range pins {
		key := strings.ToUpper(p.Number)
		if seen[key] {
			ps.errf("pin %s is in the pin table twice", p.Number)
			continue
		}
		seen[key] = true
		r := pinrole.Of(p.Name, p.Type, p.Description)
		sp := placed{SymbolPin: SymbolPin{Number: p.Number, Name: p.Name, Type: typeOf(p, r), Side: sideOf(r), Length: PinLength}}
		if pl, ok := places[key]; ok {
			if pl.Side != "" {
				sp.Side = pl.Side
			}
			if pl.Type != "" {
				sp.Type = pl.Type
			}
			if pl.Name != "" {
				sp.Name = pl.Name
			}
			sp.Hidden, sp.pos = pl.Hidden, pl.Position
			delete(places, key)
		}
		all = append(all, sp)
	}
	for n := range places {
		ps.errf("pin %s isn't in the pin table", n)
	}
	if ps.failed() {
		return Symbol{Name: name}, ps
	}
	balance(all)
	sides := map[string][]*placed{}
	for i := range all {
		sides[all[i].Side] = append(sides[all[i].Side], &all[i])
	}
	slotsOf := map[string]int{}
	for side, list := range sides {
		slotsOf[side] = assignSlots(list, &ps)
	}
	// The body: tall enough for the left and right pins, wide enough for
	// the top and bottom ones and for the names facing each other.
	rows := max(slotsOf[Left], slotsOf[Right])
	cols := max(slotsOf[Top], slotsOf[Bottom])
	halfH := math.Max(Grid*math.Ceil(float64(rows+1)/2), 2*Grid)
	nameW := func(side string) float64 {
		w := 0.0
		for _, p := range sides[side] {
			w = math.Max(w, textWidth(p.Name, 1.27))
		}
		return w
	}
	inner := nameW(Left) + nameW(Right) + Grid
	// Top and bottom pins' names run down and up into the body: leave
	// them room to pass each other, and to clear the first and last of
	// the left and right pins' names, which start at the body's edge.
	halfH = math.Max(halfH, Grid*math.Ceil((nameW(Top)+nameW(Bottom)+Grid)/2/Grid))
	if rows > 0 {
		columnTop := Grid * math.Floor(float64(rows-1)/2+0.5)
		for _, w := range []float64{nameW(Top), nameW(Bottom)} {
			if w > 0 {
				halfH = math.Max(halfH, Grid*math.Ceil((columnTop+w+Grid)/Grid))
			}
		}
	}
	halfW := math.Max(Grid*math.Ceil((inner/2)/Grid), Grid*math.Ceil(float64(cols+1)/2))
	halfW = math.Max(halfW, 2*Grid)
	s := Symbol{Name: name, Reference: spec.Reference, Value: name, Body: Rect{-halfW, -halfH, halfW, halfH}}
	if s.Reference == "" {
		s.Reference = "U"
	}
	for _, side := range []string{Left, Right, Top, Bottom} {
		list := sides[side]
		free := slotsOf[side]
		for _, p := range list {
			i := float64(p.pos - 1)
			switch side {
			case Left, Right:
				// Centre the column of slots on the body, on the grid.
				top := Grid * math.Floor(float64(free-1)/2+0.5)
				p.Y = top - i*Grid
				p.X = -halfW - PinLength
				if side == Right {
					p.X = halfW + PinLength
				}
			default:
				left := -Grid * math.Floor(float64(free-1)/2+0.5)
				p.X = left + i*Grid
				p.Y = halfH + PinLength
				if side == Bottom {
					p.Y = -halfH - PinLength
				}
			}
			p.X, p.Y = Number(p.X), Number(p.Y)
			s.Pins = append(s.Pins, p.SymbolPin)
		}
	}
	return s, ps
}

// balance moves the later half of a crowded left side to the right: a
// part whose pins are mostly general I/O (an MCU, a port expander) would
// otherwise be one tall column. Pins placed by hand stay.
func balance(all []placed) {
	var left, right []*placed
	for i := range all {
		switch all[i].Side {
		case Left:
			left = append(left, &all[i])
		case Right:
			right = append(right, &all[i])
		}
	}
	if len(left) <= 12 || len(left) <= 2*len(right) {
		return
	}
	var free []*placed
	for _, p := range left {
		if p.pos == 0 {
			free = append(free, p)
		}
	}
	sort.SliceStable(free, func(i, j int) bool { return LessPinNumber(free[i].Number, free[j].Number) })
	move := (len(left) - len(right)) / 2
	if move > len(free) {
		move = len(free)
	}
	for _, p := range free[len(free)-move:] {
		p.Side = Right
	}
}

// assignSlots gives each pin of a side its slot: placed ones where asked,
// the rest in pin-number order in the free slots. It returns the slots
// used.
func assignSlots(list []*placed, ps *problems) int {
	sort.SliceStable(list, func(i, j int) bool { return LessPinNumber(list[i].Number, list[j].Number) })
	taken := map[int]string{}
	for _, p := range list {
		if p.pos > 0 {
			if other, ok := taken[p.pos]; ok {
				ps.warnf("pins %s and %s both asked for position %d on the %s: %s moved", other, p.Number, p.pos, p.Side, p.Number)
				p.pos = 0
				continue
			}
			taken[p.pos] = p.Number
		}
	}
	next, used := 1, 0
	for _, p := range list {
		if p.pos == 0 {
			for taken[next] != "" {
				next++
			}
			p.pos = next
			taken[next] = p.Number
		}
		used = max(used, p.pos)
	}
	sort.SliceStable(list, func(i, j int) bool { return list[i].pos < list[j].pos })
	return used
}

// textWidth is about how wide a pin name is drawn at a text size.
func textWidth(s string, size float64) float64 { return float64(len([]rune(s))) * size * 0.75 }

// sortedKeys are a set's keys in order.
func sortedKeys(m map[string]bool) []string {
	var out []string
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// Match checks a symbol's pins against a footprint's pads: every pin needs
// a pad and every pad a pin, else the board can't be wired.
func Match(s Symbol, f Footprint) []Problem {
	var ps problems
	pads := map[string]bool{}
	for _, p := range f.Pads {
		if p.Number != "" {
			pads[strings.ToUpper(p.Number)] = true
		}
	}
	pins := map[string]bool{}
	var noPad []string
	for _, p := range s.Pins {
		pins[strings.ToUpper(p.Number)] = true
		if !pads[strings.ToUpper(p.Number)] {
			noPad = append(noPad, p.Number)
		}
	}
	var noPin []string
	for n := range pads {
		if !pins[n] {
			noPin = append(noPin, n)
		}
	}
	sort.Slice(noPad, func(i, j int) bool { return LessPinNumber(noPad[i], noPad[j]) })
	sort.Slice(noPin, func(i, j int) bool { return LessPinNumber(noPin[i], noPin[j]) })
	if len(noPad) > 0 {
		ps.errf("symbol pins with no pad on %s: %s", f.Name, strings.Join(noPad, ", "))
	}
	if len(noPin) > 0 {
		ps.warnf("pads of %s with no symbol pin (left unconnected): %s", f.Name, strings.Join(noPin, ", "))
	}
	return ps
}

// String names a symbol pin for messages.
func (p SymbolPin) String() string { return fmt.Sprintf("%s (%s)", p.Number, p.Name) }
