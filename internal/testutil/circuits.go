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
	// Groups are functional groups; Assign puts parts in them and says
	// what they support: {designator or block, group, supports}.
	Groups []CircuitGroup
	Assign [][3]string
}

// CircuitGroup is a functional group of a board.
type CircuitGroup struct {
	Name, Description, Notes string
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
// microcontroller board with I2C, USB data, pull-ups and an LED, and
// that board in functional groups with decoupling capacitors that each
// serve one part on the shared 3.3 V rail.
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
		{
			Name:  "Grouped MCU board",
			Rails: []CircuitRail{{Name: "VBUS", V: 5}, {Name: "3V3", V: 3.3}},
			Parts: []CircuitPart{
				{Name: "J1 USB-C", Kind: "connector"}, {Name: "U1", Ref: "SD1234", Supply: "VBUS", Output: "3V3"}, {Name: "U2 ESP32-C3", Kind: "ic"}, {Name: "U3 BME280", Kind: "ic"},
				{Name: "C1", Kind: "capacitor", Value: "1µF"}, {Name: "C2", Kind: "capacitor", Value: "1µF"}, {Name: "C3", Kind: "capacitor", Value: "1µF"},
				{Name: "C4", Kind: "capacitor", Value: "100nF"}, {Name: "C5", Kind: "capacitor", Value: "100nF"},
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
				{"3V3", "C4", ""}, {"3V3", "C5", ""},
			},
			Groups: []CircuitGroup{
				{Name: "Power", Description: "USB 5 V to 3.3 V", Notes: "Keep the buck's input loop short."},
				{Name: "MCU", Description: "ESP32-C3 with USB and status LED"},
				{Name: "Sensor", Description: "BME280 on I2C"},
			},
			Assign: [][3]string{
				{"J1 USB-C", "Power", ""}, {"U1", "Power", ""}, {"U2 ESP32-C3", "MCU", ""}, {"U3 BME280", "Sensor", ""},
				// Each decoupling capacitor serves one part on the 3.3 V rail.
				{"C2", "", "U1"}, {"C4", "", "U2 ESP32-C3"}, {"C5", "", "U3 BME280"},
				// The pull-ups sit with the sensor.
				{"R2", "Sensor", ""}, {"R3", "Sensor", ""},
			},
		},
		{
			// Shaped like a tester's STM32 board: a buck in a Power group
			// feeding an MCU group with VBAT, reset and boot switches, a
			// 32 kHz crystal, an SWD header, nine decoupling capacitors and
			// an LED behind a series resistor.
			Name:  "STM32 board",
			Rails: []CircuitRail{{Name: "VBUS", V: 5}, {Name: "3V3", V: 3.3}},
			Parts: []CircuitPart{
				{Name: "J1 USB-C", Kind: "connector"}, {Name: "U1 MAX77827", Kind: "ic"}, {Name: "L1", Kind: "inductor", Value: "1µH"},
				{Name: "C1", Kind: "capacitor", Value: "10µF"}, {Name: "C2", Kind: "capacitor", Value: "22µF"}, {Name: "C3", Kind: "capacitor", Value: "22µF"},
				{Name: "U2 STM32G0", Kind: "ic"}, {Name: "SW1 Reset", Kind: "ic"}, {Name: "SW2 Boot", Kind: "ic"}, {Name: "J2 SWD", Kind: "connector"}, {Name: "Y1 32k", Kind: "ic"},
				{Name: "C4", Kind: "capacitor", Value: "100nF"}, {Name: "C5", Kind: "capacitor", Value: "100nF"}, {Name: "C6", Kind: "capacitor", Value: "100nF"},
				{Name: "C7", Kind: "capacitor", Value: "100nF"}, {Name: "C8", Kind: "capacitor", Value: "100nF"}, {Name: "C9", Kind: "capacitor", Value: "100nF"},
				{Name: "C10", Kind: "capacitor", Value: "100nF"}, {Name: "C11", Kind: "capacitor", Value: "4.7µF"}, {Name: "C12", Kind: "capacitor", Value: "1µF"},
				{Name: "C13", Kind: "capacitor", Value: "10pF"}, {Name: "C14", Kind: "capacitor", Value: "10pF"},
				{Name: "R5", Kind: "resistor", Value: "1k"}, {Name: "R6", Kind: "resistor", Value: "10k"}, {Name: "D1 LED", Kind: "ic"},
			},
			Nets: []CircuitNet{{Name: "GND", Kind: "ground"}, {Name: "VBUS", Kind: "power"}, {Name: "3V3", Kind: "power"}, {Name: "SWDIO", Bus: "SWD"}, {Name: "SWCLK", Bus: "SWD"}},
			Pins: [][3]string{
				{"VBUS", "J1 USB-C", "VBUS"}, {"GND", "J1 USB-C", "GND"},
				{"VBUS", "U1 MAX77827", "IN"}, {"VBUS", "U1 MAX77827", "EN"}, {"3V3", "U1 MAX77827", "OUT"}, {"POK", "U1 MAX77827", "POK"},
				{"LX1", "U1 MAX77827", "LX1"}, {"LX2", "U1 MAX77827", "LX2"}, {"LX1", "L1", "1"}, {"LX2", "L1", "2"}, {"GND", "U1 MAX77827", "GND"},
				{"VBUS", "C1", ""}, {"3V3", "C2", ""}, {"3V3", "C3", ""},
				{"3V3", "U2 STM32G0", "VDD"}, {"3V3", "U2 STM32G0", "VBAT"}, {"3V3", "U2 STM32G0", "VDDA"}, {"GND", "U2 STM32G0", "VSS"}, {"GND", "U2 STM32G0", "VSSA"},
				{"NRST", "U2 STM32G0", "NRST"}, {"BOOT0", "U2 STM32G0", "PH3-BOOT0"}, {"OSC32_IN", "U2 STM32G0", "PC14-OSC32_IN"}, {"OSC32_OUT", "U2 STM32G0", "PC15-OSC32_OUT"},
				{"SWDIO", "U2 STM32G0", "PA13"}, {"SWCLK", "U2 STM32G0", "PA14"}, {"PA5", "U2 STM32G0", "PA5"}, {"POK", "U2 STM32G0", "PB0"},
				{"NRST", "SW1 Reset", "1"}, {"GND", "SW1 Reset", "2"}, {"3V3", "SW2 Boot", "1"}, {"BOOT0", "SW2 Boot", "2"}, {"BOOT0", "R6", "1"}, {"GND", "R6", "2"},
				{"3V3", "J2 SWD", "VTref"}, {"SWDIO", "J2 SWD", "SWDIO"}, {"SWCLK", "J2 SWD", "SWCLK"}, {"NRST", "J2 SWD", "nRESET"}, {"GND", "J2 SWD", "GND"},
				{"OSC32_IN", "Y1 32k", "1"}, {"OSC32_OUT", "Y1 32k", "2"}, {"OSC32_IN", "C13", ""}, {"OSC32_OUT", "C14", ""},
				{"3V3", "C4", ""}, {"3V3", "C5", ""}, {"3V3", "C6", ""}, {"3V3", "C7", ""}, {"3V3", "C8", ""}, {"3V3", "C9", ""}, {"3V3", "C10", ""}, {"3V3", "C11", ""}, {"3V3", "C12", ""},
				{"PA5", "R5", "1"}, {"LED_A", "R5", "2"}, {"LED_A", "D1 LED", "A"}, {"GND", "D1 LED", "K"},
			},
			Groups: []CircuitGroup{
				{Name: "Power", Description: "USB 5 V to 3.3 V"},
				{Name: "MCU", Description: "STM32 with reset, boot, crystal and SWD"},
			},
			Assign: [][3]string{
				{"J1 USB-C", "Power", ""}, {"U1 MAX77827", "Power", ""}, {"C1", "", "U1 MAX77827"}, {"C2", "", "U1 MAX77827"}, {"C3", "", "U1 MAX77827"},
				{"U2 STM32G0", "MCU", ""}, {"SW1 Reset", "MCU", ""}, {"SW2 Boot", "MCU", ""}, {"J2 SWD", "MCU", ""}, {"Y1 32k", "MCU", ""}, {"D1 LED", "MCU", ""},
				{"C4", "", "U2 STM32G0"}, {"C5", "", "U2 STM32G0"}, {"C6", "", "U2 STM32G0"}, {"C7", "", "U2 STM32G0"}, {"C8", "", "U2 STM32G0"},
				{"C9", "", "U2 STM32G0"}, {"C10", "", "U2 STM32G0"}, {"C11", "", "U2 STM32G0"}, {"C12", "", "U2 STM32G0"}, {"R5", "", "D1 LED"},
			},
		},
	}
}
