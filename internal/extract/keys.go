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
	"isc":          "Output short-circuit current",
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
	"vocp":         "Overcharge protection (detection) voltage",
	"vodp":         "Overdischarge protection (detection) voltage",
	"ichg":         "Charge current",
	"iterm":        "Charge termination current",
	"iprechg":      "Pre-charge (trickle) current",
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
	{regexp.MustCompile(`(?i)soft[- ]?start|软启动|^\s*t\s*SS\b`), "tss"},
	{regexp.MustCompile(`(?i)switching frequency|oscillator frequency|operating frequency|f\s*\(?SW\)?\b|f\s*OSC|\bfs\b|开关频率|振荡频率`), "fsw"},
	{regexp.MustCompile(`(?i)feedback (voltage|reference|regulation)|reference voltage|V\s*\(?FB\)?\b|V\s*REF\b|regulated (feedback|fb)|基准电压|参考电压|反馈电压`), "vref"},
	{regexp.MustCompile(`(?i)high[- ]side.*(on[- ]?resistance|R\s*DS)|R\s*DS\s*\(?on\)?.*high|R\s*\(?HS`), "rdson_hs"},
	{regexp.MustCompile(`(?i)low[- ]side.*(on[- ]?resistance|R\s*DS)|R\s*DS\s*\(?on\)?.*low|R\s*\(?LS`), "rdson_ls"},
	{regexp.MustCompile(`(?i)drain[- ]to[- ]source on|on[- ]?state resistance|static drain|R\s*DS\s*\(?on|R\s*DS\(ON|\bon[- ]?resist|^\s*R\s*ON\b|导通电阻`), "rdson"},
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
	{regexp.MustCompile(`(?i)dropout|V\s*\(?DO\b|V\s*IN\s*[–-]\s*V\s*OUT`), "vdropout"},
	{regexp.MustCompile(`(?i)under-?voltage lock-?out|\bUVLO\b|^\s*V\s*UV\b|under-?voltage protection threshold|欠压锁定|欠压保护阈值|欠压保护门限`), "uvlo"},
	{regexp.MustCompile(`(?i)overcharge (protection|detection)( threshold)? voltage|^\s*V\s*OC[PU]?\b|过充(保护|检测)电压`), "vocp"},
	{regexp.MustCompile(`(?i)over-?discharge (protection|detection)( threshold)? voltage|^\s*V\s*OD[PL]?\b|过放(保护|检测)电压`), "vodp"},
	{regexp.MustCompile(`(?i)termination current|I\s*TERM\b|终止电流|截止电流`), "iterm"},
	{regexp.MustCompile(`(?i)pre-?charge current|trickle[- ]?charg\w* current|^\s*I\s*(TRKL|TRIKL|PRE|PRECHG|TRICKLE)\b|涓流充电电流|预充电流|预充电电流`), "iprechg"},
	{regexp.MustCompile(`(?i)current limit.*(start-?up|soft-?start)|(start-?up|soft-?start).*current limit`), "ilim_startup"},
	{regexp.MustCompile(`(?i)input current limit|I\s*\(?IN[_ ]?LIM`), "ilim_in"},
	{regexp.MustCompile(`(?i)current limit|over-?current|I\s*\(?LIM|I\s*\(?OCL|I\s*\(?LIMIT|I\s*CL\b`), "ilim"},
	{regexp.MustCompile(`(?i)shut-?down (supply )?current|I\s*\(?SD|I\s*\(?SHDN|standby current|关断电流|停机电流|power-?down current|^\s*I\s*PD\b`), "isd"},
	{regexp.MustCompile(`(?i)short[- ]circuit current|I\s*\(?SC\b|I\s*\(?SHORT`), "isc"},
	{regexp.MustCompile(`(?i)ground (pin )?current|\bI\s*\(?GND|静态电流|工作电流|电源电流`), "iq"},
	{regexp.MustCompile(`(?i)input current|\bI\s*I\b|\bI\s*IN\b`), ""},
	{regexp.MustCompile(`(?i)quiescent|operating.*supply current|supply current|I\s*Q\b|I\s*\(?VIN\)?|I\s*CC\b|I\s*DD\b|静态电流|工作电流|电源电流`), "iq"},
	{regexp.MustCompile(`(?i)enable threshold|EN threshold|V\s*\(?EN`), "ven"},
	{regexp.MustCompile(`(?i)input high|V\s*IH\b|high-level input`), "vih"},
	{regexp.MustCompile(`(?i)input low|V\s*IL\b|low-level input`), "vil"},
	{regexp.MustCompile(`(?i)charge current|I\s*CHG|I\s*BAT\b|programm?ed current|充电电流`), "ichg"},
	{regexp.MustCompile(`(?i)regulation voltage|charge voltage|float voltage|V\s*FLOAT|V\s*REG|V\s*BAT\b|浮充电压|充电电压|恒压电压`), "vreg"},
	{regexp.MustCompile(`(?i)junction temperature|operating temperature|T\s*J\b|结温|工作温度`), "tj"},
	{regexp.MustCompile(`(?i)efficiency|η|效率`), "efficiency"},
	{regexp.MustCompile(`(?i)output current|load current|\bI\s*\(?OUT|\bI\s*O\b|输出电流`), "iout"},
	{regexp.MustCompile(`(?i)output voltage|V\s*\(?OUT|V\s*O\b|输出电压`), "vout"},
	{regexp.MustCompile(`(?i)input voltage|supply voltage|V\s*\(?IN\)?\b|V\s*CC\b|V\s*DD\b|V\s*S\b|VIN, EN|^\s*IN\b[^,]*voltage|^\s*V\s*I\b|输入电压|电源电压`), "vin"},
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
	// A per-pin rating ("BAT (with respect to VSS)", "EN, MODE to GND")
	// is the device's input voltage only for a supply pin.
	if m := pinRatingRe.FindStringSubmatch(text); m != nil && u == "V" {
		if supplyPinRe.MatchString(m[1]) {
			return "vin"
		}
		return ""
	}
	for _, r := range keyRules {
		if !r.re.MatchString(text) {
			continue
		}
		if !unitFits(r.key, u) {
			continue
		}
		// Thresholds, hysteresis and detection levels are not operating
		// ranges.
		if (r.key == "vin" || r.key == "vout" || r.key == "iout") && notRangeRe.MatchString(text) {
			continue
		}
		// "VCC-VBAT lockout voltage" is a difference, not the battery voltage.
		if (r.key == "vreg" || r.key == "vin" || r.key == "vout") && differenceRe.MatchString(text) {
			continue
		}
		return r.key
	}
	return ""
}

// pinRatingRe matches a row naming pins and their reference: "IN (with
// respect to VSS)", "SW to PGND", "EN, MODE (with respect to GND)".
var pinRatingRe = regexp.MustCompile(`^([A-Z][A-Z0-9_]{0,11}(?:\s*,\s*[A-Z][A-Z0-9_]{0,11})*)\s*(?:\(\s*(?:with respect to|w\.?r\.?t\.?|referenced to|to)\s+[A-Z]|to\s+(?:P?GND|A?VSS|AGND|SGND)\b)`)

// supplyPinRe matches input supply pin names.
var supplyPinRe = regexp.MustCompile(`^(?:IN|VIN|PVIN|AVIN|VCC|VDD|VS|VBUS|VSUP|VSYS|SUPPLY)(?:\s*,|$)`)

// differenceRe matches a difference of two voltages ("VCC-VBAT", "VIN – VOUT").
var differenceRe = regexp.MustCompile(`V\s*[A-Z]+\s*[-–−]\s*V\s*[A-Z]+`)

// notRangeRe matches parameters that are levels, not operating ranges.
var notRangeRe = regexp.MustCompile(`(?i)threshold|hysteresis|detect|short[- ]circuit|under-?voltage|over-?voltage|lock-?out|power[- ]good|accuracy|regulation|ripple|阈值|门限|迟滞|滞回|欠压|过压|闭锁|检测|精度|纹波`)

// keyUnits lists the base units each key's values may have.
var keyUnits = map[string]string{
	"vin": "V", "vout": "V", "vref": "V", "uvlo": "V", "vocp": "V", "vodp": "V", "vds": "V", "vgs": "V", "vgs_th": "V", "vplateau": "V", "vf": "V",
	"vdropout": "V", "vreg": "V", "vih": "V", "vil": "V", "ven": "V",
	"iout": "A", "ilim": "A", "ilim_startup": "A", "ilim_in": "A", "iq": "A", "isd": "A", "iterm": "A", "iprechg": "A", "id": "A", "idss": "A", "id_on": "A", "isc": "A", "ichg": "A",
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
