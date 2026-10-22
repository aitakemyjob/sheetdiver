package testutil

// Circuit is a small board as a connection table, for testing the block
// diagram (in core and, recorded, in the frontend): rails, parts from
// the library (Ref) or blocks off the BOM (Kind, Value), nets, and pins.
type Circuit struct {
	Name  string
	Rails []CircuitRail
	Parts []CircuitPart
	Nets  []CircuitNet
	// Pins puts pins on nets: {net, designator or block, pin}; an empty
	// pin connects a block (a passive) without one.
	Pins [][3]string
}

// CircuitRail is a supply rail and its voltage.
type CircuitRail struct {
	Name string
	V    float64
}

// CircuitPart is a library part in the BOM (Ref set) or a block.
type CircuitPart struct {
	Name           string // designator, or the block's name
	Ref            string // library part number
	Supply, Output string // rails of a BOM entry
	Kind, Value    string // a block's kind and value
}

// CircuitNet sets a net's kind or bus.
type CircuitNet struct {
	Name, Kind, Bus string
}

// Circuits are the boards the block diagram is tested on: the USB-C
// to 3.3 V sketch (a connector, an eFuse and a buck-boost with their
// passives), a battery board whose 4.2 V rail joins four parts, and a
// microcontroller board with I2C, USB data, pull-ups and an LED.
func Circuits() []Circuit {
	return []Circuit{
		{
			Name: "USB-C to 3V3",
			Parts: []CircuitPart{
				{Name: "J1 USB-C", Kind: "connector"}, {Name: "U1 TPS25200", Kind: "ic"}, {Name: "U2 TPS63900", Kind: "ic"},
				{Name: "D1", Kind: "diode", Value: "TVS"}, {Name: "Rd1", Kind: "resistor", Value: "5.1k"}, {Name: "Rd2", Kind: "resistor", Value: "5.1k"},
				{Name: "R3", Kind: "resistor", Value: "150k"}, {Name: "C1", Kind: "capacitor", Value: "10µF"}, {Name: "C2", Kind: "capacitor", Value: "10µF"},
				{Name: "C3", Kind: "capacitor", Value: "22µF"}, {Name: "C4", Kind: "capacitor", Value: "22µF"}, {Name: "L1", Kind: "inductor", Value: "2.2µH"},
				{Name: "R4", Kind: "resistor", Value: "36.5k"}, {Name: "R5", Kind: "resistor", Value: "16.2k"}, {Name: "R6", Kind: "resistor", Value: "0Ω"},
			},
			Nets: []CircuitNet{{Name: "GND", Kind: "ground"}, {Name: "VBUS", Kind: "power"}, {Name: "5V_PROT", Kind: "power"}, {Name: "3V3", Kind: "power"}},
			Pins: [][3]string{
				{"VBUS", "J1 USB-C", "VBUS"}, {"CC1", "J1 USB-C", "CC1"}, {"CC2", "J1 USB-C", "CC2"}, {"GND", "J1 USB-C", "GND"},
				{"CC1", "Rd1", ""}, {"CC2", "Rd2", ""}, {"VBUS", "D1", ""},
				{"VBUS", "U1 TPS25200", "IN"}, {"VBUS", "U1 TPS25200", "EN"}, {"5V_PROT", "U1 TPS25200", "OUT"}, {"ILIM", "U1 TPS25200", "ILIM"}, {"GND", "U1 TPS25200", "GND"},
				{"ILIM", "R3", ""}, {"5V_PROT", "C1", ""}, {"5V_PROT", "C2", ""},
				{"5V_PROT", "U2 TPS63900", "VIN"}, {"5V_PROT", "U2 TPS63900", "EN"}, {"GND", "U2 TPS63900", "SEL"}, {"GND", "U2 TPS63900", "GND"}, {"3V3", "U2 TPS63900", "VOUT"},
				{"LX1", "U2 TPS63900", "LX1"}, {"LX2", "U2 TPS63900", "LX2"}, {"LX1", "L1", "1"}, {"LX2", "L1", "2"},
				{"CFG1", "U2 TPS63900", "CFG1"}, {"CFG2", "U2 TPS63900", "CFG2"}, {"CFG3", "U2 TPS63900", "CFG3"}, {"CFG1", "R4", ""}, {"CFG2", "R6", ""}, {"CFG3", "R5", ""},
				{"3V3", "C3", ""}, {"3V3", "C4", ""},
			},
		},
		{
			Name: "Battery board",
			Parts: []CircuitPart{
				{Name: "J1 USB-C", Kind: "connector"}, {Name: "U1 TP4056", Kind: "ic"}, {Name: "BT1 Li-ion", Kind: "connector"}, {Name: "U2 DW01A", Kind: "ic"},
				{Name: "U3 TPS63900", Kind: "ic"}, {Name: "Q1 FS8205A", Kind: "module"}, {Name: "Sensors", Kind: "module"},
				{Name: "L1", Kind: "inductor", Value: "2.2µH"}, {Name: "R1", Kind: "resistor", Value: "2.2k"}, {Name: "C1", Kind: "capacitor", Value: "10µF"},
				{Name: "C2", Kind: "capacitor", Value: "10µF"}, {Name: "LED1", Kind: "diode", Value: "red"},
			},
			Nets: []CircuitNet{{Name: "GND", Kind: "ground"}, {Name: "5V", Kind: "power"}, {Name: "4.2V", Kind: "power"}, {Name: "3V3", Kind: "power"}},
			Pins: [][3]string{
				{"5V", "J1 USB-C", "VBUS"}, {"GND", "J1 USB-C", "GND"}, {"5V", "U1 TP4056", "VCC"}, {"5V", "U1 TP4056", "CE"}, {"GND", "U1 TP4056", "GND"}, {"GND", "U1 TP4056", "TEMP"},
				{"5V", "C1", ""}, {"4.2V", "U1 TP4056", "BAT"}, {"PROG", "U1 TP4056", "PROG"}, {"PROG", "R1", ""}, {"CHRG", "U1 TP4056", "CHRG"}, {"CHRG", "LED1", ""},
				{"4.2V", "BT1 Li-ion", "+"}, {"4.2V", "U2 DW01A", "VCC"}, {"GND", "U2 DW01A", "GND"},
				{"OD", "U2 DW01A", "OD"}, {"OD", "Q1 FS8205A", "G1"}, {"OC", "U2 DW01A", "OC"}, {"OC", "Q1 FS8205A", "G2"},
				{"4.2V", "U3 TPS63900", "VIN"}, {"4.2V", "U3 TPS63900", "EN"}, {"GND", "U3 TPS63900", "GND"}, {"3V3", "U3 TPS63900", "VOUT"},
				{"LX1", "U3 TPS63900", "LX1"}, {"LX1", "L1", "1"}, {"LX2", "U3 TPS63900", "LX2"}, {"LX2", "L1", "2"},
				{"3V3", "C2", ""}, {"3V3", "Sensors", "VDD"},
			},
		},
		{
			// From testing: ESD diodes and pull-downs on both CC pins, an
			// eFuse with its current-limit resistor, input capacitors with
			// a long description, and a buck-boost whose thermal pad and SEL
			// go to ground.
			Name: "Tester board",
			Parts: []CircuitPart{
				{Name: "J1 USB-C", Kind: "connector"}, {Name: "U1 TPS25200", Kind: "ic"}, {Name: "U2 TPS63900", Kind: "ic"},
				{Name: "D1", Kind: "diode", Value: "TVS"}, {Name: "D2", Kind: "diode", Value: "ESD"}, {Name: "D3", Kind: "diode", Value: "ESD"},
				{Name: "R1", Kind: "resistor", Value: "5.1k"}, {Name: "R2", Kind: "resistor", Value: "5.1k"}, {Name: "R3", Kind: "resistor", Value: "150k"},
				{Name: "C1", Kind: "capacitor", Value: "10µF input bulk for U2 VIN, X5R 10 V"}, {Name: "C2", Kind: "capacitor", Value: "10µF input bulk for U2 VIN, X5R 10 V"},
				{Name: "C3", Kind: "capacitor", Value: "22µF"}, {Name: "C4", Kind: "capacitor", Value: "22µF"}, {Name: "L1", Kind: "inductor", Value: "2.2µH"},
				{Name: "R4", Kind: "resistor", Value: "36.5k"}, {Name: "R5", Kind: "resistor", Value: "16.2k"},
			},
			Nets: []CircuitNet{{Name: "GND", Kind: "ground"}, {Name: "VBUS", Kind: "power"}, {Name: "5V_PROT", Kind: "power"}, {Name: "3V3", Kind: "power"}},
			Pins: [][3]string{
				{"VBUS", "J1 USB-C", "VBUS"}, {"CC1", "J1 USB-C", "CC1"}, {"CC2", "J1 USB-C", "CC2"}, {"GND", "J1 USB-C", "GND"},
				{"VBUS", "D1", ""}, {"CC1", "D2", ""}, {"CC2", "D3", ""}, {"CC1", "R1", ""}, {"CC2", "R2", ""},
				{"VBUS", "U1 TPS25200", "IN"}, {"VBUS", "U1 TPS25200", "EN"}, {"5V_PROT", "U1 TPS25200", "OUT"}, {"ILIM", "U1 TPS25200", "ILIM"},
				{"GND", "U1 TPS25200", "GND"}, {"GND", "U1 TPS25200", "PAD"}, {"ILIM", "R3", ""},
				{"5V_PROT", "C1", ""}, {"5V_PROT", "C2", ""},
				{"5V_PROT", "U2 TPS63900", "VIN"}, {"5V_PROT", "U2 TPS63900", "EN"}, {"GND", "U2 TPS63900", "SEL"}, {"GND", "U2 TPS63900", "GND"}, {"GND", "U2 TPS63900", "Thermal Pad"},
				{"3V3", "U2 TPS63900", "VOUT"}, {"LX1", "U2 TPS63900", "LX1"}, {"LX2", "U2 TPS63900", "LX2"}, {"LX1", "L1", "1"}, {"LX2", "L1", "2"},
				{"CFG1", "U2 TPS63900", "CFG1"}, {"CFG3", "U2 TPS63900", "CFG3"}, {"CFG1", "R4", ""}, {"CFG3", "R5", ""}, {"3V3", "C3", ""}, {"3V3", "C4", ""},
			},
		},
		{
			Name:  "MCU board",
			Rails: []CircuitRail{{Name: "VBUS", V: 5}, {Name: "3V3", V: 3.3}},
			Parts: []CircuitPart{
				{Name: "J1 USB-C", Kind: "connector"}, {Name: "U1", Ref: "SD1234", Supply: "VBUS", Output: "3V3"}, {Name: "U2 ESP32-C3", Kind: "ic"}, {Name: "U3 BME280", Kind: "ic"},
				{Name: "C1", Kind: "capacitor", Value: "1µF"}, {Name: "C2", Kind: "capacitor", Value: "1µF"}, {Name: "C3", Kind: "capacitor", Value: "1µF"},
				{Name: "R1", Kind: "resistor", Value: "10k"}, {Name: "R2", Kind: "resistor", Value: "4.7k"}, {Name: "R3", Kind: "resistor", Value: "4.7k"}, {Name: "LED1", Kind: "diode", Value: "green"},
			},
			Nets: []CircuitNet{{Name: "GND", Kind: "ground"}, {Name: "SDA", Bus: "I2C"}, {Name: "SCL", Bus: "I2C"}, {Name: "D+", Bus: "USB"}, {Name: "D-", Bus: "USB"}},
			Pins: [][3]string{
				{"VBUS", "J1 USB-C", "VBUS"}, {"D+", "J1 USB-C", "D+"}, {"D-", "J1 USB-C", "D-"}, {"GND", "J1 USB-C", "GND"},
				{"VBUS", "U1", "VIN"}, {"GND", "U1", "GND"}, {"3V3", "U1", "SW"}, {"VBUS", "C1", ""}, {"3V3", "C2", ""},
				{"3V3", "U2 ESP32-C3", "3V3"}, {"GND", "U2 ESP32-C3", "GND"}, {"EN", "U2 ESP32-C3", "EN"}, {"EN", "R1", "1"}, {"3V3", "R1", "2"}, {"EN", "C3", ""},
				{"D+", "U2 ESP32-C3", "GPIO19"}, {"D-", "U2 ESP32-C3", "GPIO18"}, {"SDA", "U2 ESP32-C3", "GPIO8"}, {"SCL", "U2 ESP32-C3", "GPIO9"}, {"LED", "U2 ESP32-C3", "GPIO2"}, {"LED", "LED1", ""},
				{"3V3", "U3 BME280", "VDD"}, {"3V3", "U3 BME280", "CSB"}, {"GND", "U3 BME280", "GND"}, {"SDA", "U3 BME280", "SDA"}, {"SCL", "U3 BME280", "SCL"},
				{"SDA", "R2", "1"}, {"3V3", "R2", "2"}, {"SCL", "R3", "1"}, {"3V3", "R3", "2"},
			},
		},
	}
}
