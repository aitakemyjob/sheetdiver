package pinrole

import "testing"

func TestOf(t *testing.T) {
	for _, tc := range []struct {
		name, typ, desc string
		want            Role
	}{
		// Ground, whatever the type column says (VSS is typed S or P in some tables).
		{"GND", "", "", Ground},
		{"VSS", "S", "", Ground},
		{"VSSA", "P", "", Ground},
		{"AGND", "", "Analog ground", Ground},
		{"PGND", "G", "", Ground},
		{"GROUND", "", "", Ground},
		// A pad is a pad, not ground, even when it goes there.
		{"Thermal Pad", "", "Connect this pin to ground for correct operation.", Pad},
		{"EP", "G", "", Pad},
		{"PowerPAD", "", "", Pad},
		// Supplies, by type or name.
		{"VIN", "", "Supply voltage", Power},
		{"VDD", "P", "", Power},
		{"VDDA", "", "", Power},
		{"VBAT", "P", "", Power},
		{"VS", "", "", Power},
		{"PVIN", "", "", Power},
		{"IN", "", "Power Input. Bypass to PGND.", Power},
		{"VBUS", "P", "", Power},
		{"VCC", "", "", Power},
		{"3V3", "P", "", Power}, // a connector's rail pin, typed
		// Outputs and inputs, by type then name, then description.
		{"SW", "O", "Switch node.", Output},
		{"VOUT", "", "Output voltage", Output},
		{"OUT", "", "", Output},
		{"POK", "DO", "", Output},
		{"VFB", "I", "Feedback input.", Input},
		{"EN", "", "Device enable input", Input},
		{"STDBY", "", "Open-drain output, low while charging", Output},
		{"EN", "", "Device enable. A high level applied to this pin enables the device.", Other},
		{"LX1", "", "Switching node of the buck stage", Other},
		{"CFG1", "I", "Configuration pin 1.", Input},
		// I/O and analog.
		{"PA0", "I/O", "", Bidir},
		{"GPIO8", "", "", Other},
		{"CC1", "A", "", Analog},
		{"SDA", "IO", "", Bidir},
		// Two-terminals' ends and NC.
		{"A", "", "Anode", Passive},
		{"K", "", "Cathode", Passive},
		{"1", "", "", Passive},
		{"+", "", "", Passive},
		{"NC", "", "No connection", NC},
		{"N.C.", "", "", NC},
		{"SBU1", "NC", "", NC},
	} {
		if got := Of(tc.name, tc.typ, tc.desc); got != tc.want {
			t.Errorf("Of(%q, %q, %q) = %s, want %s", tc.name, tc.typ, tc.desc, got, tc.want)
		}
	}
}

func TestNames(t *testing.T) {
	for name, want := range map[string]bool{"GND": true, "agnd": true, "VSS": true, "VSSA": true, "GROUND": true, "VIN": false, "SGND": true, "3V3": false} {
		if got := IsGroundName(name); got != want {
			t.Errorf("IsGroundName(%q) = %v", name, got)
		}
	}
	for name, want := range map[string]bool{"VIN": true, "PVIN": true, "VSYS": true, "VSUP": true, "VBUS": true, "VSS": false, "VOUT": false, "SUPPLY": true} {
		if got := IsPowerName(name); got != want {
			t.Errorf("IsPowerName(%q) = %v", name, got)
		}
	}
}
