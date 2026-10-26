package kicad

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// Footprints. A component's footprint is found by a chain of rules, each
// reading what is known of its package (its package name, description,
// value, pin count) and either naming a footprint of KiCad's standard
// libraries or passing it on: a footprint named outright, a chip size
// (0603) for a resistor or capacitor, a package everyone makes the same
// (SOT-23-5, SOIC-8), a QFN or DFN of a given size in the libraries
// installed with KiCad. When none can, the component keeps no footprint
// and a hint of what it needs. A new rule is a FootprintRule added to
// the chain.

// FootprintQuery is what is known of a component's package: its kind,
// the texts that may name the package (its package field, description,
// value, the pin table's package label), and its pads (0 when unknown).
type FootprintQuery struct {
	Kind  Kind
	Texts []string
	Pads  int
}

// text is the query's texts as one, for searching.
func (q FootprintQuery) text() string {
	return strings.Join(q.Texts, " | ")
}

// FootprintRule names a footprint for a component, or says it can't.
type FootprintRule interface {
	Footprint(q FootprintQuery) (Footprint, bool)
}

// FootprintChain asks its rules in turn; the first to name a footprint
// wins.
type FootprintChain struct {
	rules []FootprintRule
}

// NewFootprintChain is a chain of the given rules.
func NewFootprintChain(rules ...FootprintRule) *FootprintChain {
	return &FootprintChain{rules: rules}
}

// DefaultFootprints is the standard chain: a footprint named outright, a
// chip size, a standard package, a manufacturer's package code, then a QFN/DFN found in index (nil
// when KiCad's footprints aren't installed).
func DefaultFootprints(index FootprintIndex) *FootprintChain {
	c := NewFootprintChain(namedRule{}, chipRule{}, packageRule{}, packageCodeRule{})
	if index != nil {
		c.Then(leadlessRule{index: index})
	}
	return c
}

// Then adds a rule at the end of the chain.
func (c *FootprintChain) Then(r FootprintRule) *FootprintChain {
	c.rules = append(c.rules, r)
	return c
}

// Footprint is the first footprint a rule names, else none with a hint
// of the package as known.
func (c *FootprintChain) Footprint(q FootprintQuery) Footprint {
	for _, r := range c.rules {
		if f, ok := r.Footprint(q); ok {
			return f
		}
	}
	return Footprint{Hint: unresolvedHint(q)}
}

// unresolvedHint says what is known of a package no rule could place.
func unresolvedHint(q FootprintQuery) string {
	var known []string
	for _, t := range q.Texts {
		if t = strings.Join(strings.Fields(t), " "); t != "" {
			if r := []rune(t); len(r) > 80 {
				t = string(r[:79]) + "…"
			}
			known = append(known, t)
		}
	}
	hint := "no footprint found"
	if len(known) > 0 {
		hint += " for " + strings.Join(known, "; ")
	}
	if q.Pads > 0 {
		hint += fmt.Sprintf(" (%d pads)", q.Pads)
	}
	return hint
}

// namedRule takes a footprint named outright in KiCad's form
// ("Package_SO:SOIC-8_3.9x4.9mm_P1.27mm"), say in a part's package field.
type namedRule struct{}

var namedRe = regexp.MustCompile(`\b((?:Package|Resistor|Capacitor|Inductor|Diode|LED|Connector|Crystal|Fuse|Button_Switch|Battery|Oscillator|Module|RF_Module)[A-Za-z0-9_]*:[A-Za-z0-9_.,+-]+)`)

// Footprint takes a footprint named in KiCad's form.
func (namedRule) Footprint(q FootprintQuery) (Footprint, bool) {
	if m := namedRe.FindStringSubmatch(q.text()); m != nil {
		return Footprint{LibID: m[1], Hint: "named in the part"}, true
	}
	return Footprint{}, false
}

// chipRule places a chip resistor, capacitor, inductor, ferrite, fuse or
// LED by its size code (0402, 0603…), as BOMs and descriptions give it.
type chipRule struct{}

// chipSizes are the imperial size codes with their metric names.
var chipSizes = map[string]string{
	"0201": "0201_0603Metric", "0402": "0402_1005Metric", "0603": "0603_1608Metric", "0805": "0805_2012Metric",
	"1206": "1206_3216Metric", "1210": "1210_3225Metric", "1812": "1812_4532Metric", "2512": "2512_6332Metric",
}

// chipLibraries are each kind's library and prefix for chip parts.
var chipLibraries = map[Kind]string{
	KindResistor: "Resistor_SMD:R_", KindCapacitor: "Capacitor_SMD:C_", KindInductor: "Inductor_SMD:L_",
	KindFerrite: "Inductor_SMD:L_", KindFuse: "Fuse:Fuse_", KindLED: "LED_SMD:LED_",
}

var chipRe = regexp.MustCompile(`(?:^|[^\d.])(0201|0402|0603|0805|1206|1210|1812|2512)(?:$|[^\d])`)

// Footprint places a chip part by its size code.
func (chipRule) Footprint(q FootprintQuery) (Footprint, bool) {
	prefix, ok := chipLibraries[q.Kind]
	if !ok {
		return Footprint{}, false
	}
	m := chipRe.FindStringSubmatch(q.text())
	if m == nil {
		return Footprint{}, false
	}
	return Footprint{LibID: prefix + chipSizes[m[1]], Hint: m[1] + " chip"}, true
}

// packageRule places the packages every maker draws alike, by name,
// when the pad count (if known) fits.
type packageRule struct{}

// standardPackage is a package name as datasheets write it, its
// footprint, and its pads.
type standardPackage struct {
	re    *regexp.Regexp
	libID string
	pads  int
	// sized are the variants by pad count, for a name that doesn't say
	// ("6-pin SOT-23").
	sized map[int]string
}

// standardPackages, more specific names first (SOT-23-6 before SOT-23).
var standardPackages = []standardPackage{
	{regexp.MustCompile(`(?i)\bT?SOT-?23-?6\b|\bSC-?74\b`), "Package_TO_SOT_SMD:SOT-23-6", 6, nil},
	{regexp.MustCompile(`(?i)\bT?SOT-?23-?5\b|\bSC-?74A\b`), "Package_TO_SOT_SMD:SOT-23-5", 5, nil},
	{regexp.MustCompile(`(?i)\bSOT-?23-?8\b`), "Package_TO_SOT_SMD:SOT-23-8", 8, nil},
	{regexp.MustCompile(`(?i)\bSOT-?223\b`), "Package_TO_SOT_SMD:SOT-223-3_TabPin2", 4, nil},
	{regexp.MustCompile(`(?i)\bSOT-?89\b`), "Package_TO_SOT_SMD:SOT-89-3", 3, nil},
	{regexp.MustCompile(`(?i)\bSOT-?363\b|\bSC-?70-?6\b|\bSC-?88\b`), "Package_TO_SOT_SMD:SOT-363_SC-70-6", 6, nil},
	{regexp.MustCompile(`(?i)\bSOT-?353\b|\bSC-?70-?5\b|\bSC-?88A\b`), "Package_TO_SOT_SMD:SOT-353_SC-70-5", 5, nil},
	{regexp.MustCompile(`(?i)\bSOT-?563\b`), "Package_TO_SOT_SMD:SOT-563", 6, nil},
	{regexp.MustCompile(`(?i)\bSOT-?323\b|\bSC-?70\b`), "Package_TO_SOT_SMD:SOT-323_SC-70", 3, map[int]string{5: "Package_TO_SOT_SMD:SOT-353_SC-70-5", 6: "Package_TO_SOT_SMD:SOT-363_SC-70-6"}},
	{regexp.MustCompile(`(?i)\bSOT-?23(?:-?3)?\b|\bTO-?236\b`), "Package_TO_SOT_SMD:SOT-23", 3, map[int]string{5: "Package_TO_SOT_SMD:SOT-23-5", 6: "Package_TO_SOT_SMD:SOT-23-6", 8: "Package_TO_SOT_SMD:SOT-23-8"}},
	{regexp.MustCompile(`(?i)\bTO-?252\b|\bD-?PAK\b`), "Package_TO_SOT_SMD:TO-252-2", 3, nil},
	{regexp.MustCompile(`(?i)\bTO-?263\b|\bD2-?PAK\b`), "Package_TO_SOT_SMD:TO-263-2", 3, nil},
	{regexp.MustCompile(`(?i)\bSOD-?123F\b`), "Diode_SMD:D_SOD-123F", 2, nil},
	{regexp.MustCompile(`(?i)\bSOD-?123\b`), "Diode_SMD:D_SOD-123", 2, nil},
	{regexp.MustCompile(`(?i)\bSOD-?323\b`), "Diode_SMD:D_SOD-323", 2, nil},
	{regexp.MustCompile(`(?i)\bSOD-?523\b`), "Diode_SMD:D_SOD-523", 2, nil},
	{regexp.MustCompile(`(?i)\bSOD-?923\b`), "Diode_SMD:D_SOD-923", 2, nil},
	{regexp.MustCompile(`(?i)\bSMA\b|\bDO-?214AC\b`), "Diode_SMD:D_SMA", 2, nil},
	{regexp.MustCompile(`(?i)\bSMB\b|\bDO-?214AA\b`), "Diode_SMD:D_SMB", 2, nil},
	{regexp.MustCompile(`(?i)\bSMC\b|\bDO-?214AB\b`), "Diode_SMD:D_SMC", 2, nil},
	{regexp.MustCompile(`(?i)\bSOIC-?8\b|\bSO-?8\b|\b8-(?:pin|lead) SOIC\b`), "Package_SO:SOIC-8_3.9x4.9mm_P1.27mm", 8, nil},
	{regexp.MustCompile(`(?i)\bSOIC-?14\b|\b14-(?:pin|lead) SOIC\b`), "Package_SO:SOIC-14_3.9x8.7mm_P1.27mm", 14, nil},
	{regexp.MustCompile(`(?i)\bSOIC-?16\b|\b16-(?:pin|lead) SOIC\b`), "Package_SO:SOIC-16_3.9x9.9mm_P1.27mm", 16, nil},
	{regexp.MustCompile(`(?i)\b(?:MSOP|VSSOP)-?8\b|\b8-(?:pin|lead) (?:MSOP|VSSOP)\b`), "Package_SO:MSOP-8_3x3mm_P0.65mm", 8, nil},
	{regexp.MustCompile(`(?i)\b(?:MSOP|VSSOP)-?10\b|\b10-(?:pin|lead) (?:MSOP|VSSOP)\b`), "Package_SO:MSOP-10_3x3mm_P0.5mm", 10, nil},
	{regexp.MustCompile(`(?i)\bTSSOP-?8\b`), "Package_SO:TSSOP-8_4.4x3mm_P0.65mm", 8, nil},
	{regexp.MustCompile(`(?i)\bTSSOP-?14\b`), "Package_SO:TSSOP-14_4.4x5mm_P0.65mm", 14, nil},
	{regexp.MustCompile(`(?i)\bTSSOP-?16\b`), "Package_SO:TSSOP-16_4.4x5mm_P0.65mm", 16, nil},
	{regexp.MustCompile(`(?i)\bTSSOP-?20\b`), "Package_SO:TSSOP-20_4.4x6.5mm_P0.65mm", 20, nil},
}

// diodes are the kinds a diode package fits.
var diodes = map[Kind]bool{KindDiode: true, KindSchottky: true, KindZener: true, KindTVS: true, KindLED: true}

// Footprint places a standard package named in the texts.
func (packageRule) Footprint(q FootprintQuery) (Footprint, bool) {
	text := q.text()
	for _, p := range standardPackages {
		if strings.HasPrefix(p.libID, "Diode_SMD:") && !diodes[q.Kind] {
			continue // an SMA is also a connector
		}
		m := p.re.FindString(text)
		switch {
		case m == "":
		case q.Pads == 0 || q.Pads == p.pads:
			return Footprint{LibID: p.libID, Hint: m}, true
		case p.sized[q.Pads] != "":
			return Footprint{LibID: p.sized[q.Pads], Hint: fmt.Sprintf("%d-pin %s", q.Pads, m)}, true
		}
	}
	return Footprint{}, false
}

// packageCodeRule places a part by its maker's package code, as pin
// tables and ordering numbers give it (TI's DBV is a SOT-23-5 or -6),
// by its pad count when the code covers several.
type packageCodeRule struct{}

// packageCodes are TI's codes for standard packages, by pad count.
var packageCodes = map[string]map[int]string{
	"DBV": {5: "Package_TO_SOT_SMD:SOT-23-5", 6: "Package_TO_SOT_SMD:SOT-23-6"},
	"DBZ": {3: "Package_TO_SOT_SMD:SOT-23"},
	"DCK": {3: "Package_TO_SOT_SMD:SOT-323_SC-70", 5: "Package_TO_SOT_SMD:SOT-353_SC-70-5", 6: "Package_TO_SOT_SMD:SOT-363_SC-70-6"},
	"DDC": {5: "Package_TO_SOT_SMD:TSOT-23-5", 6: "Package_TO_SOT_SMD:TSOT-23-6"},
	"DRL": {5: "Package_TO_SOT_SMD:SOT-553", 6: "Package_TO_SOT_SMD:SOT-563"},
	"DGK": {8: "Package_SO:MSOP-8_3x3mm_P0.65mm"},
	"DGS": {10: "Package_SO:MSOP-10_3x3mm_P0.5mm"},
	"PW":  {8: "Package_SO:TSSOP-8_4.4x3mm_P0.65mm", 14: "Package_SO:TSSOP-14_4.4x5mm_P0.65mm", 16: "Package_SO:TSSOP-16_4.4x5mm_P0.65mm", 20: "Package_SO:TSSOP-20_4.4x6.5mm_P0.65mm"},
}

var packageCodeRe = regexp.MustCompile(`\b(DBV|DBZ|DCK|DDC|DRL|DGK|DGS|PW)\b`)

// Footprint places a part by its package code and pad count.
func (packageCodeRule) Footprint(q FootprintQuery) (Footprint, bool) {
	for _, m := range packageCodeRe.FindAllString(q.text(), -1) {
		sizes := packageCodes[m]
		if id := sizes[q.Pads]; id != "" {
			return Footprint{LibID: id, Hint: fmt.Sprintf("package code %s, %d pins", m, q.Pads)}, true
		}
		if q.Pads == 0 && len(sizes) == 1 {
			for n, id := range sizes {
				return Footprint{LibID: id, Hint: fmt.Sprintf("package code %s, %d pins", m, n)}, true
			}
		}
	}
	return Footprint{}, false
}

// FootprintIndex lists the footprints of a KiCad library installed on
// this computer.
type FootprintIndex interface {
	Footprints(library string) []string
}

// DirIndex is the footprint libraries in a folder of .pretty folders.
type DirIndex struct {
	Root  string
	cache map[string][]string
}

// FindFootprints is the folder KiCad's standard footprints are installed
// in, from KiCad's own variables or where its installers put them; nil
// when there's none.
func FindFootprints() *DirIndex {
	candidates := []string{os.Getenv("KICAD9_FOOTPRINT_DIR"), os.Getenv("KICAD8_FOOTPRINT_DIR"), os.Getenv("KICAD7_FOOTPRINT_DIR"),
		"/usr/share/kicad/footprints", "/usr/local/share/kicad/footprints",
		"/Applications/KiCad/KiCad.app/Contents/SharedSupport/footprints"}
	for _, v := range []string{"9.0", "8.0", "7.0"} {
		candidates = append(candidates, filepath.Join(os.Getenv("ProgramFiles"), "KiCad", v, "share", "kicad", "footprints"))
	}
	for _, dir := range candidates {
		if dir == "" {
			continue
		}
		if st, err := os.Stat(filepath.Join(dir, "Package_DFN_QFN.pretty")); err == nil && st.IsDir() {
			return &DirIndex{Root: dir, cache: map[string][]string{}}
		}
	}
	return nil
}

// Footprints lists a library's footprints by name, sorted.
func (d *DirIndex) Footprints(library string) []string {
	if d == nil {
		return nil
	}
	if names, ok := d.cache[library]; ok {
		return names
	}
	entries, _ := os.ReadDir(filepath.Join(d.Root, library+".pretty"))
	var names []string
	for _, e := range entries {
		if name, ok := strings.CutSuffix(e.Name(), ".kicad_mod"); ok {
			names = append(names, name)
		}
	}
	sort.Strings(names)
	d.cache[library] = names
	return names
}

// leadlessRule places a QFN, DFN or SON by its family, pad count and
// body size ("VQFN-16 3x3mm"), choosing among the footprints installed
// with KiCad; the size must be known, as a family has many.
type leadlessRule struct {
	index FootprintIndex
}

var (
	leadlessRe = regexp.MustCompile(`(?i)\b([VWTU]?QFN|HVQFN|[TWU]?DFN|[VWU]?SON)(?:-HR)?[- ]?(\d{1,3})?\b`)
	countRe    = regexp.MustCompile(`(?i)\b(\d{1,3})[- ](?:pin|lead|pad)s?\b`)
	bodyRe     = regexp.MustCompile(`(?i)\b(\d(?:\.\d+)?)\s*(?:mm)?\s*[x×]\s*(\d(?:\.\d+)?)\s*mm\b`)
)

// Footprint finds a QFN, DFN or SON of the family, pad count and size among the installed footprints.
func (r leadlessRule) Footprint(q FootprintQuery) (Footprint, bool) {
	text := q.text()
	m := leadlessRe.FindStringSubmatch(text)
	body := bodyRe.FindStringSubmatch(text)
	if m == nil || body == nil {
		return Footprint{}, false
	}
	family := strings.ToUpper(m[1])
	pins, _ := strconv.Atoi(m[2])
	if pins == 0 {
		if c := countRe.FindStringSubmatch(text); c != nil {
			pins, _ = strconv.Atoi(c[1])
		}
	}
	if pins == 0 && q.Pads > 0 {
		pins = q.Pads - 1 // the exposed pad
	}
	if pins == 0 {
		return Footprint{}, false
	}
	// The libraries name sizes as written: "3x3mm", "1.5x2mm".
	size := trimZero(body[1]) + "x" + trimZero(body[2]) + "mm"
	var found []string
	for _, f := range []string{family, strings.TrimLeft(family, "VWTUH")} {
		prefix := fmt.Sprintf("%s-%d-", f, pins)
		for _, name := range r.index.Footprints("Package_DFN_QFN") {
			if strings.HasPrefix(name, prefix) && strings.Contains(name, "_"+size+"_") && !strings.HasSuffix(name, "_ThermalVias") {
				found = append(found, name)
			}
		}
		if len(found) > 0 {
			break
		}
	}
	if len(found) == 0 {
		return Footprint{}, false
	}
	hint := fmt.Sprintf("%s-%d %s", family, pins, size)
	if len(found) > 1 {
		hint += fmt.Sprintf(": %d footprints fit, check the exposed pad and pitch against the datasheet", len(found))
	}
	return Footprint{LibID: "Package_DFN_QFN:" + found[0], Hint: hint}, true
}

// trimZero writes a size as the libraries do: "3" for "3.0".
func trimZero(s string) string {
	if strings.Contains(s, ".") {
		s = strings.TrimRight(strings.TrimRight(s, "0"), ".")
	}
	return s
}
