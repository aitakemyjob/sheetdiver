package extract

import (
	"regexp"
	"strings"
)

// Keys are canonical parameter identifiers. They let calculators and
// project checks find "the switching frequency" or "the gate threshold"
// whatever a datasheet calls it. The agent can set or correct a key when
// it records parameters; Keys documents them for MCP.
var Keys = map[string]string{
	"vin":          "Input / supply voltage (VIN, VCC, VDD)",
	"vout":         "Output voltage, or adjustable output range",
	"iout":         "Output / load current",
	"fsw":          "Switching or oscillator frequency",
	"vref":         "Feedback / reference voltage (VFB, VREF)",
	"ton_min":      "Minimum on-time",
	"toff_min":     "Minimum off-time",
	"dmax":         "Maximum duty cycle",
	"ilim":         "Current limit in normal operation (peak/valley switch or output)",
	"ilim_startup": "Current limit during start-up",
	"ilim_in":      "Input current limit (average)",
	"iq":           "Quiescent / operating supply current",
	"isd":          "Shutdown current",
	"uvlo":         "Under-voltage lockout threshold",
	"rdson_hs":     "High-side switch on-resistance",
	"rdson_ls":     "Low-side switch on-resistance",
	"rdson":        "MOSFET on-resistance RDS(on)",
	"vds":          "Drain-source voltage rating",
	"vgs":          "Gate-source voltage rating",
	"vgs_th":       "Gate threshold voltage VGS(th)",
	"id":           "Continuous drain current",
	"idss":         "Zero-gate-voltage drain (leakage) current",
	"id_on":        "On-state drain current",
	"qg":           "Total gate charge",
	"qgs":          "Gate-source charge",
	"qgd":          "Gate-drain (Miller) charge",
	"ciss":         "Input capacitance",
	"coss":         "Output capacitance",
	"rg":           "Internal gate resistance",
	"vplateau":     "Gate plateau voltage",
	"vf":           "Diode forward voltage",
	"vdropout":     "Dropout voltage",
	"ichg":         "Charge current",
	"vreg":         "Charge regulation voltage",
	"tj":           "Junction temperature",
	"rth_ja":       "Junction-to-ambient thermal resistance",
	"rth_jc":       "Junction-to-case thermal resistance",
	"vih":          "Logic input high threshold",
	"vil":          "Logic input low threshold",
	"ven":          "Enable threshold",
	"tss":          "Soft-start time",
	"efficiency":   "Efficiency",
}

// keyRules map name/symbol patterns to keys, most specific first.
var keyRules = []struct {
	re  *regexp.Regexp
	key string
}{
	{regexp.MustCompile(`(?i)junction[- ]to[- ]ambient|θ\s*JA|Θ\s*JA|R\s*th\s*\(?j-?a|RθJA|R\(?θ?JA`), "rth_ja"},
	{regexp.MustCompile(`(?i)junction[- ]to[- ]case|θ\s*JC|RθJC|R\s*th\s*\(?j-?c`), "rth_jc"},
	{regexp.MustCompile(`(?i)minimum on[- ]?time|t\s*\(?ON[_\s(]*MIN|tON\(min\)`), "ton_min"},
	{regexp.MustCompile(`(?i)minimum off[- ]?time|t\s*\(?OFF[_\s(]*MIN|tOFF\(min\)`), "toff_min"},
	{regexp.MustCompile(`(?i)maximum duty|duty cycle|D\s*\(?MAX`), "dmax"},
	{regexp.MustCompile(`(?i)soft[- ]?start`), "tss"},
	{regexp.MustCompile(`(?i)switching frequency|oscillator frequency|operating frequency|f\s*\(?SW\)?\b|f\s*OSC|\bfs\b`), "fsw"},
	{regexp.MustCompile(`(?i)feedback (voltage|reference|regulation)|reference voltage|V\s*\(?FB\)?\b|V\s*REF\b|regulated (feedback|fb)`), "vref"},
	{regexp.MustCompile(`(?i)high[- ]side.*(on[- ]?resistance|R\s*DS)|R\s*DS\s*\(?on\)?.*high|R\s*\(?HS`), "rdson_hs"},
	{regexp.MustCompile(`(?i)low[- ]side.*(on[- ]?resistance|R\s*DS)|R\s*DS\s*\(?on\)?.*low|R\s*\(?LS`), "rdson_ls"},
	{regexp.MustCompile(`(?i)drain[- ]to[- ]source on|on[- ]?state resistance|static drain|R\s*DS\s*\(?on|R\s*DS\(ON`), "rdson"},
	{regexp.MustCompile(`(?i)gate threshold|threshold voltage.*gate|V\s*GS\s*\(?th|V\s*\(?th\)?\b`), "vgs_th"},
	{regexp.MustCompile(`(?i)gate plateau|plateau voltage|V\s*\(?plateau|V\s*GP\b`), "vplateau"},
	{regexp.MustCompile(`(?i)total gate charge|^Q\s*g\b|Q\s*G\s*\(?tot|\bQg\b`), "qg"},
	{regexp.MustCompile(`(?i)gate[- ]to[- ]drain charge|gate[- ]drain charge|miller|Q\s*gd\b`), "qgd"},
	{regexp.MustCompile(`(?i)gate[- ]to[- ]source charge|gate[- ]source charge|Q\s*gs\b`), "qgs"},
	{regexp.MustCompile(`(?i)input capacitance|C\s*iss\b`), "ciss"},
	{regexp.MustCompile(`(?i)output capacitance|C\s*oss\b`), "coss"},
	{regexp.MustCompile(`(?i)gate resistance|R\s*G\b`), "rg"},
	{regexp.MustCompile(`(?i)drain[- ]to[- ]source (breakdown )?voltage|V\s*\(?BR\)?\s*DSS|V\s*DS\b|V\s*DSS\b`), "vds"},
	{regexp.MustCompile(`(?i)gate[- ]to[- ]source voltage|V\s*GS\b|V\s*GSS\b`), "vgs"},
	{regexp.MustCompile(`(?i)zero[- ]gate|gate[- ]voltage drain current|drain[- ](source )?leakage|I\s*DSS\b`), "idss"},
	{regexp.MustCompile(`(?i)on[- ]state drain current|I\s*D\s*\(\s*ON\s*\)`), "id_on"},
	{regexp.MustCompile(`(?i)continuous drain current|drain current|I\s*D\b`), "id"},
	{regexp.MustCompile(`(?i)forward voltage|V\s*F\b`), "vf"},
	{regexp.MustCompile(`(?i)dropout`), "vdropout"},
	{regexp.MustCompile(`(?i)under-?voltage lock-?out|\bUVLO\b`), "uvlo"},
	{regexp.MustCompile(`(?i)current limit.*(start-?up|soft-?start)|(start-?up|soft-?start).*current limit`), "ilim_startup"},
	{regexp.MustCompile(`(?i)input current limit|I\s*\(?IN[_ ]?LIM`), "ilim_in"},
	{regexp.MustCompile(`(?i)current limit|over-?current|I\s*\(?LIM|I\s*\(?OCL|I\s*\(?LIMIT|I\s*CL\b`), "ilim"},
	{regexp.MustCompile(`(?i)shut-?down (supply )?current|I\s*\(?SD|I\s*\(?SHDN|standby current`), "isd"},
	{regexp.MustCompile(`(?i)quiescent|operating.*supply current|supply current|I\s*Q\b|I\s*\(?VIN\)?|I\s*CC\b|I\s*DD\b`), "iq"},
	{regexp.MustCompile(`(?i)enable threshold|EN threshold|V\s*\(?EN`), "ven"},
	{regexp.MustCompile(`(?i)input high|V\s*IH\b|high-level input`), "vih"},
	{regexp.MustCompile(`(?i)input low|V\s*IL\b|low-level input`), "vil"},
	{regexp.MustCompile(`(?i)charge current|I\s*CHG|I\s*BAT\b|programm?ed current`), "ichg"},
	{regexp.MustCompile(`(?i)regulation voltage|charge voltage|float voltage|V\s*FLOAT|V\s*REG|V\s*BAT\b`), "vreg"},
	{regexp.MustCompile(`(?i)junction temperature|operating temperature|T\s*J\b`), "tj"},
	{regexp.MustCompile(`(?i)efficiency|η`), "efficiency"},
	{regexp.MustCompile(`(?i)output current|load current|I\s*\(?OUT|I\s*O\b`), "iout"},
	{regexp.MustCompile(`(?i)output voltage|V\s*\(?OUT|V\s*O\b`), "vout"},
	{regexp.MustCompile(`(?i)input voltage|supply voltage|V\s*\(?IN\)?\b|V\s*CC\b|V\s*DD\b|V\s*S\b|VIN, EN`), "vin"},
}

// GuessKey assigns a canonical key from a parameter's symbol and name,
// checking that the unit is plausible for the key. Section matters for
// a few keys: an input-voltage row under absolute maximum ratings is
// the rating, which calculators must not mistake for the operating range.
func GuessKey(text, unit, _ string) string {
	text = strings.TrimSpace(text)
	if text == "" {
		return ""
	}
	u := NormalizeUnit(unit)
	for _, r := range keyRules {
		if !r.re.MatchString(text) {
			continue
		}
		if !unitFits(r.key, u) {
			continue
		}
		return r.key
	}
	return ""
}

// keyUnits lists the base units each key's values may have.
var keyUnits = map[string]string{
	"vin": "V", "vout": "V", "vref": "V", "uvlo": "V", "vds": "V", "vgs": "V", "vgs_th": "V", "vplateau": "V", "vf": "V",
	"vdropout": "V", "vreg": "V", "vih": "V", "vil": "V", "ven": "V",
	"iout": "A", "ilim": "A", "ilim_startup": "A", "ilim_in": "A", "iq": "A", "isd": "A", "id": "A", "idss": "A", "id_on": "A", "ichg": "A",
	"fsw": "Hz", "ton_min": "s", "toff_min": "s", "tss": "s",
	"rdson": "Ω", "rdson_hs": "Ω", "rdson_ls": "Ω", "rg": "Ω",
	"qg": "C", "qgs": "C", "qgd": "C", "ciss": "F", "coss": "F",
	"tj": "°C", "rth_ja": "°C/W", "rth_jc": "°C/W", "dmax": "%", "efficiency": "%",
}

// unitFits reports whether unit is right for key. Keys with a physical
// unit need a recognised, matching unit: a wrong key is worse than none,
// since calculators trust keys, and dense tables often misplace cells.
func unitFits(key, unit string) bool {
	want, ok := keyUnits[key]
	if !ok {
		return true
	}
	_, base, known := ToSI(1, unit)
	return known && base == want
}
