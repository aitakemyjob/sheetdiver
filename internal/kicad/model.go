// Package kicad writes a KiCad schematic of a design: a .kicad_sch
// sheet with a symbol per component, every pin labelled with its net,
// plus a symbol library of the symbols it generated. What a component
// is and what its pins connect to comes from the caller; this package
// only decides how it's drawn and written.
package kicad

// Side is the side of a symbol a pin is on.
type Side string

// Pin sides.
const (
	Left   Side = "left"
	Right  Side = "right"
	Top    Side = "top"
	Bottom Side = "bottom"
)

// ElectricalType is a pin's electrical type, as KiCad's ERC knows it.
type ElectricalType string

// Electrical types.
const (
	PowerIn       ElectricalType = "power_in"
	PowerOut      ElectricalType = "power_out"
	Input         ElectricalType = "input"
	Output        ElectricalType = "output"
	Bidirectional ElectricalType = "bidirectional"
	Passive       ElectricalType = "passive"
	NoConnect     ElectricalType = "no_connect"
	Unspecified   ElectricalType = "unspecified"
)

// Kind is what a component is drawn as.
type Kind string

// Component kinds: a box with its pins, a standard two-terminal symbol,
// or a transistor.
const (
	KindIC                 Kind = "ic"
	KindConnector          Kind = "connector"
	KindResistor           Kind = "resistor"
	KindCapacitor          Kind = "capacitor"
	KindCapacitorPolarised Kind = "capacitor-polarised"
	KindInductor           Kind = "inductor"
	KindDiode              Kind = "diode"
	KindSchottky           Kind = "schottky"
	KindZener              Kind = "zener"
	KindLED                Kind = "led"
	KindTVS                Kind = "tvs"
	KindFuse               Kind = "fuse"
	KindFerrite            Kind = "ferrite"
	KindCrystal            Kind = "crystal"
	KindBattery            Kind = "battery"
	KindSwitch             Kind = "switch"
	KindMOSFETN            Kind = "mosfet-n"
	KindMOSFETP            Kind = "mosfet-p"
	KindNPN                Kind = "npn"
	KindPNP                Kind = "pnp"
)

// Pin is a component's pin: its Number (the footprint pad), Name, the
// Net it's on ("" when unconnected), the Side of a box it goes on, and
// its electrical Type. A two-terminal's or transistor's pins are named
// by terminal (TerminalPin gives their numbers).
type Pin struct {
	Number string
	Name   string
	Net    string
	Side   Side
	Type   ElectricalType
}

// Footprint is a component's footprint: LibID in KiCad's standard
// libraries ("Resistor_SMD:R_0603_1608Metric") when one was found, and
// Hint, what is known of the package when it wasn't (or why this one was
// picked).
type Footprint struct {
	LibID string
	Hint  string
}

// Cell is where in its group a part is drawn: the column from the left
// and the place in it from the top.
type Cell struct {
	Column, Row int
}

// Anchor is the part a passive serves and the pins it's wired to there:
// one (a capacitor from VIN to ground, a pull-up on SDA) or two (an
// inductor across LX1 and LX2), by number on that part's symbol.
type Anchor struct {
	Ref  string
	Pins []string
}

// Component is a part on the sheet. Symbol names the symbol generated
// for a box or transistor (its part number), shared by components with
// the same one; Group is the functional group it's drawn in, Cell where
// in it (a part), Anchor what it hangs from (a passive).
type Component struct {
	Ref          string
	Value        string
	MPN          string
	Manufacturer string
	Description  string
	Datasheet    string
	Kind         Kind
	Symbol       string
	Pins         []Pin
	Footprint    Footprint
	Group        string
	Cell         *Cell
	Anchor       *Anchor
}

// Group is a functional group, drawn as a frame around its components.
type Group struct {
	Name        string
	Description string
}

// Schematic is what's drawn: the components, their groups in order, and
// the nets that are ground (drawn with a ground symbol instead of a
// label).
type Schematic struct {
	Title      string
	Project    string
	Date       string
	Revision   string
	Groups     []Group
	Components []Component
	GroundNets []string
}
