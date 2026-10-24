// Package pinrole says what a pin is (supply, ground, input, output…)
// from a datasheet's pin table: its type column when there is one, else
// its name, else its description. It is the one place for that rule: the
// connection checks, the pin tables and the schematic symbols all use it,
// and so does extraction, which can't import core.
package pinrole

import (
	"regexp"
	"strings"
)

// Role is what a pin does.
type Role string

// The roles, in the order they're decided: a thermal pad before ground
// (it is one), ground before power (VSS reads as both), and a name that
// says nothing is Other.
const (
	Power   Role = "power"   // takes the part's supply (VIN, VCC, VDD, VBUS…)
	Ground  Role = "ground"  // GND, VSS, AGND…
	Pad     Role = "pad"     // an exposed or thermal pad
	Input   Role = "input"   // a digital or analog input
	Output  Role = "output"  // drives its net (VOUT, SW, an open-drain flag)
	Bidir   Role = "bidir"   // an I/O
	Analog  Role = "analog"  // an analog pin with no direction given
	Passive Role = "passive" // a two-terminal's end: 1, 2, A, K, +, -
	NC      Role = "nc"      // no connection
	Other   Role = "other"   // anything else (EN, CFG1, LX1)
)

var (
	padRe = regexp.MustCompile(`(?i)thermal|exposed|power\s*pad|^(EP|PAD|TAB)$`)
	// regOutRe is a part's own internal regulator output, which only takes
	// a capacitor: an MCU's VCAP (typed S, supply, in STM32 tables), VCORE,
	// VREGOUT, VDDCORE when it's the LDO's output (it says so).
	regOutRe = regexp.MustCompile(`(?i)^(VCAP|VCORE|VREG(OUT)?|VLDO)\w*$`)
	groundRe = regexp.MustCompile(`(?i)^(A|D|P|S)?(GND|VSS)\w*$|^GROUND$`)
	powerRe  = regexp.MustCompile(`(?i)^(A|D|P)?V(DD|CC|IN|BAT|IO|S|SUP|SYS|BUS)\w*$|^VDDIO\w*$|^(IN|SUPPLY)$`)
	ncRe     = regexp.MustCompile(`(?i)^(NC|N\.C\.?|DNC)$`)
	outputRe = regexp.MustCompile(`(?i)^V?OUT`)
	// passiveRe is a two-terminal's end as pin tables name them.
	passiveRe = regexp.MustCompile(`^(1|2|A|K|C|\+|-)$`)
	// Descriptions that say a direction ("Device enable input", "Open-drain
	// output"), when the table gives no type; one that says both says nothing.
	descOutputRe = regexp.MustCompile(`(?i)\boutput\b`)
	descInputRe  = regexp.MustCompile(`(?i)\binput\b`)
)

// Of says what a pin is from its type column (I, O, I/O, P, G, A, NC…),
// else its name, else its description.
func Of(name, typ, description string) Role {
	name = strings.TrimSpace(name)
	switch t := strings.ToUpper(strings.TrimSpace(typ)); {
	case padRe.MatchString(name):
		return Pad
	case regOutRe.MatchString(name):
		return Output
	case t == "G" || t == "GND" || t == "GROUND" || groundRe.MatchString(name):
		return Ground
	case t == "P" || t == "PWR" || t == "S" || t == "SUPPLY" || t == "POWER" || powerRe.MatchString(name):
		return Power
	case t == "NC" || ncRe.MatchString(name):
		return NC
	case t == "O" || t == "DO" || t == "AO" || t == "PO" || t == "OUT" || t == "OUTPUT" || outputRe.MatchString(name):
		return Output
	case t == "I" || t == "DI" || t == "AI" || t == "IN" || t == "INPUT":
		return Input
	case t == "I/O" || t == "IO" || t == "B" || t == "DIO" || t == "BIDIR":
		return Bidir
	case t == "A" || t == "AN" || t == "ANALOG":
		return Analog
	case t == "PASSIVE" || t == "PAS" || passiveRe.MatchString(name):
		return Passive
	case t == "" && descOutputRe.MatchString(description) && !descInputRe.MatchString(description):
		return Output
	case t == "" && descInputRe.MatchString(description) && !descOutputRe.MatchString(description):
		return Input
	}
	return Other
}

// IsGroundName reports whether a pin or net name reads as ground (GND,
// AGND, VSS, GROUND).
func IsGroundName(name string) bool {
	return groundRe.MatchString(strings.TrimSpace(name))
}

// IsPowerName reports whether a pin name reads as a supply (VIN, VCC,
// VDD, VBUS, PVIN…).
func IsPowerName(name string) bool {
	return powerRe.MatchString(strings.TrimSpace(name)) && !groundRe.MatchString(strings.TrimSpace(name))
}
