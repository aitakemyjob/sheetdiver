package extract

import (
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/aitakemyjob/sheetdiver/internal/pdf"
)

// mpnRe matches a manufacturer-part-number-shaped token: letters and
// digits, starting with a letter or digit, at least one of each.
var mpnRe = regexp.MustCompile(`^[A-Z0-9][A-Z0-9\-/+#.]{3,24}$`)

// notPart rejects tokens that look like part numbers but are packages,
// standards, document numbers or units.
var notPart = regexp.MustCompile(`(?i)^(S[LBCNP][A-Z]{2}\d{3}[A-Z]?$|SL[A-Z]{2}[A-Z0-9]{4}$|SOT|SOIC|TSSOP|MSOP|QFN|DFN|VQFN|WQFN|WSON|SON|LGA|BGA|QFP|LQFP|UFQFPN|WLCSP|TO-?\d|DPAK|D2PAK|SC-?\d|SOD|DO-?\d|SMA|SMB|SMC|PDIP|AEC|ISO|IEC|JEDEC|JESD|EIA|UL|RoHS|USB|I2C|SPI|UART|CAN|PCB|ESD|HBM|CDM|LED|ADC|DAC|PWM|MOSFET|IGBT|GPIO|DDR|LDO|PMIC|SL[A-Z]{2}\d|DS\d|AN\d|UM\d|RM\d|PM\d|REV|PAGE|FIGURE|TABLE|NOTE|\d+[A-Z]?$|[\d.]+-?[VAW]$|\d+(MHZ|KHZ|HZ|MA|UA|MV|NS|US|MS|PF|NF|UF|NH|UH|MW)$)`)

// PartNumbers proposes the part numbers a datasheet covers, best first:
// tokens in the PDF title, in large type on the first page and in the
// running page header score highest. The filename is the fallback.
func PartNumbers(doc *pdf.Document, lines [][]Line, filename string) []string {
	score := map[string]float64{}
	add := func(text string, w float64) {
		for _, tok := range tokenize(text) {
			if isPartToken(tok) {
				score[tok] += w
			}
		}
	}
	add(doc.Meta["Title"], 4)
	add(doc.Meta["Subject"], 1)
	add(doc.Meta["Keywords"], 0.5)
	body := bodySize(doc)
	for pi, ls := range lines {
		if pi > 2 {
			break
		}
		for _, l := range ls {
			switch {
			case pi == 0 && l.Size >= body+3:
				add(l.Text, 3)
			case l.Box.Y1 < l.PageH*0.09:
				add(l.Text, 1.5) // running header
			case pi == 0:
				add(l.Text, 0.3)
			}
		}
	}
	// Running headers repeat on every page; count them across the document.
	for pi, ls := range lines {
		if pi < 3 {
			continue
		}
		for _, l := range ls {
			if l.Box.Y1 < l.PageH*0.09 {
				add(l.Text, 0.2)
			}
		}
	}
	type kv struct {
		k string
		v float64
	}
	var all []kv
	for k, v := range score {
		all = append(all, kv{k, v})
	}
	sort.Slice(all, func(i, j int) bool {
		if all[i].v != all[j].v {
			return all[i].v > all[j].v
		}
		return all[i].k < all[j].k
	})
	var out []string
	for _, e := range all {
		if e.v < 3 || len(out) >= 8 {
			break
		}
		out = append(out, e.k)
	}
	if len(out) == 0 {
		stem := strings.ToUpper(strings.TrimSuffix(filepath.Base(filename), filepath.Ext(filename)))
		stem = strings.TrimPrefix(stem, "DS_")
		if tok := strings.FieldsFunc(stem, func(r rune) bool { return r == ' ' || r == '_' }); len(tok) > 0 {
			out = append(out, tok[0])
		}
	}
	return out
}

// tokenize splits text on spaces and list punctuation, keeping hyphens and
// slashes that are part of part numbers.
func tokenize(text string) []string {
	return strings.FieldsFunc(text, func(r rune) bool {
		return r == ' ' || r == ',' || r == ';' || r == '(' || r == ')' || r == '[' || r == ']' || r == '|' || r == '\t' || r == '\n' || r == '“' || r == '”' || r == '"'
	})
}

// IsPartNumber reports whether a token looks like a manufacturer part
// number (letters and digits, not a package, unit or document number).
func IsPartNumber(tok string) bool { return isPartToken(tok) }

// isPartToken reports whether tok looks like a part number.
func isPartToken(tok string) bool {
	tok = strings.TrimRight(tok, ".:")
	if !mpnRe.MatchString(tok) || notPart.MatchString(tok) {
		return false
	}
	letters, digits := 0, 0
	for _, r := range tok {
		switch {
		case r >= 'A' && r <= 'Z':
			letters++
		case r >= '0' && r <= '9':
			digits++
		}
	}
	return letters >= 2 && digits >= 2
}

// manufacturers lists names to look for, with the canonical name first.
var manufacturers = [][]string{
	{"Texas Instruments", "texas instruments", "ti.com"},
	{"Analog Devices", "analog devices", "analog.com", "linear technology", "maxim integrated"},
	{"STMicroelectronics", "stmicroelectronics", "st.com"},
	{"NXP Semiconductors", "nxp semiconductors", "nxp.com", "freescale"},
	{"Microchip Technology", "microchip technology", "microchip.com", "atmel"},
	{"Infineon Technologies", "infineon", "international rectifier", "cypress semiconductor"},
	{"onsemi", "onsemi", "on semiconductor", "fairchild"},
	{"Renesas Electronics", "renesas", "intersil", "dialog semiconductor", "idt"},
	{"Nexperia", "nexperia"},
	{"Vishay", "vishay", "siliconix"},
	{"Diodes Incorporated", "diodes incorporated", "diodes.com"},
	{"ROHM Semiconductor", "rohm"},
	{"Toshiba", "toshiba"},
	{"Monolithic Power Systems", "monolithic power", "monolithicpower.com", "mps"},
	{"Richtek", "richtek"},
	{"Silergy", "silergy"},
	{"NanJing Top Power", "top power", "nanjing top power"},
	{"Espressif Systems", "espressif"},
	{"Nordic Semiconductor", "nordic semiconductor"},
	{"Silicon Labs", "silicon labs", "silicon laboratories"},
	{"Allegro MicroSystems", "allegro microsystems"},
	{"Alpha and Omega Semiconductor", "alpha and omega", "alpha & omega", "aosmd"},
	{"Torex Semiconductor", "torex"},
	{"ABLIC", "ablic", "seiko instruments"},
	{"Semtech", "semtech"},
	{"Power Integrations", "power integrations"},
	{"Littelfuse", "littelfuse"},
	{"Murata", "murata"},
	{"TDK", "tdk corporation", "tdk"},
	{"Würth Elektronik", "würth", "wurth", "we-online"},
	{"Coilcraft", "coilcraft"},
	{"Bourns", "bourns"},
	{"Panasonic", "panasonic"},
	{"KEMET", "kemet"},
	{"Samsung Electro-Mechanics", "samsung"},
	{"GigaDevice", "gigadevice"},
	{"WCH", "nanjing qinheng", "wch.cn"},
	{"Broadcom", "broadcom", "avago"},
	{"Skyworks", "skyworks"},
	{"Qorvo", "qorvo"},
	{"Lattice Semiconductor", "lattice semiconductor"},
	{"AMD", "xilinx", "amd.com"},
	{"Melexis", "melexis"},
	{"ams OSRAM", "ams osram", "ams ag", "osram"},
	{"Bosch Sensortec", "bosch sensortec"},
	{"TDK InvenSense", "invensense"},
	{"Sensirion", "sensirion"},
	{"EPC", "efficient power conversion", "epc-co.com"},
	{"Navitas", "navitas"},
	{"Wolfspeed", "wolfspeed", "cree"},
}

// Manufacturer guesses who made the part from metadata and the text of
// the first pages and the last page (where ordering and legal text lives).
func Manufacturer(doc *pdf.Document, pageText []string) string {
	var hay strings.Builder
	for _, k := range []string{"Author", "Creator", "Title", "Subject", "Keywords"} {
		hay.WriteString(strings.ToLower(doc.Meta[k]) + " \n")
	}
	meta := hay.String()
	for i, t := range pageText {
		if i < 3 || i == len(pageText)-1 {
			hay.WriteString(strings.ToLower(t) + "\n")
		}
	}
	text := hay.String()
	best, bestN := "", 0
	for _, m := range manufacturers {
		n := 0
		for _, alias := range m[1:] {
			if len(alias) <= 4 {
				// Short aliases must be whole words.
				re := regexp.MustCompile(`\b` + regexp.QuoteMeta(alias) + `\b`)
				n += len(re.FindAllStringIndex(text, -1))
				n += 3 * len(re.FindAllStringIndex(meta, -1))
				continue
			}
			n += strings.Count(text, alias) + 3*strings.Count(meta, alias)
		}
		if n > bestN {
			best, bestN = m[0], n
		}
	}
	return best
}

// categoryRules map description keywords to device categories.
var categoryRules = []struct {
	re  *regexp.Regexp
	cat string
}{
	{regexp.MustCompile(`(?i)microcontroller|\bMCU\b|arm.*cortex|risc-v|\bSoC\b|wi-?fi.*bluetooth`), "mcu"},
	{regexp.MustCompile(`(?i)n[- ]channel.*(mosfet|power|fet)|nexfet|n-ch`), "mosfet-n"},
	{regexp.MustCompile(`(?i)p[- ]channel.*(mosfet|power|fet)|p-ch`), "mosfet-p"},
	{regexp.MustCompile(`(?i)\bmosfet|\bhexfet|power fet`), "mosfet"},
	{regexp.MustCompile(`(?i)current.{0,12}monitor|current sense|power monitor`), "current-monitor"},
	{regexp.MustCompile(`(?i)charger|charge management|battery charg|充电`), "charger"},
	{regexp.MustCompile(`(?i)\bLDO\b|low[- ]dropout|linear regulator|adjustable regulator|3-pin.*regulator|positive voltage regulator`), "ldo"},
	{regexp.MustCompile(`(?i)buck[- ]boost|inverting`), "buck-boost"},
	{regexp.MustCompile(`(?i)step[- ]?down|buck`), "buck"},
	{regexp.MustCompile(`(?i)step[- ]?up|boost`), "boost"},
	{regexp.MustCompile(`(?i)gate driver|half[- ]bridge driver|mosfet driver`), "gate-driver"},
	{regexp.MustCompile(`(?i)load switch|e-?fuse|hot[- ]swap|ideal diode`), "load-switch"},
	{regexp.MustCompile(`(?i)op(erational)? ?amp|amplifier`), "amplifier"},
	{regexp.MustCompile(`(?i)\bADC\b|analog[- ]to[- ]digital`), "adc"},
	{regexp.MustCompile(`(?i)\bDAC\b|digital[- ]to[- ]analog`), "dac"},
	{regexp.MustCompile(`(?i)voltage reference|shunt reference`), "reference"},
	{regexp.MustCompile(`(?i)schottky|rectifier|diode`), "diode"},
	{regexp.MustCompile(`(?i)inductor`), "inductor"},
	{regexp.MustCompile(`(?i)capacitor`), "capacitor"},
	{regexp.MustCompile(`(?i)transceiver|phy\b`), "interface"},
	{regexp.MustCompile(`(?i)sensor`), "sensor"},
	{regexp.MustCompile(`(?i)regulator|converter|controller`), "power"},
}

// Category guesses the device category, trusting the title over the
// description and the description over the first page's text (a MOSFET
// datasheet mentions buck converters; an MCU datasheet its internal SMPS).
func Category(texts ...string) string {
	for _, text := range texts {
		for _, r := range categoryRules {
			if r.re.MatchString(text) {
				return r.cat
			}
		}
	}
	return ""
}

// titleSuffix removes publisher boilerplate from PDF titles.
var titleSuffix = regexp.MustCompile(`(?i)\s*(datasheet|data sheet)\s*(\(rev\.?\s*\w+\))?\s*$`)

// tagRe matches markup some publishers leave in PDF titles (<Sup>®</Sup>).
var tagRe = regexp.MustCompile(`</?[A-Za-z]+>`)

// titleAndDescription returns a short title and a one-line description
// from the metadata title or the first page's largest text.
func titleAndDescription(doc *pdf.Document, lines [][]Line) (string, string) {
	title := strings.TrimSpace(titleSuffix.ReplaceAllString(tagRe.ReplaceAllString(doc.Meta["Title"], ""), ""))
	var big []Line
	if len(lines) > 0 {
		maxSize := 0.0
		for _, l := range lines[0] {
			if l.Box.Y1 > l.PageH*0.09 && l.Size > maxSize {
				maxSize = l.Size
			}
		}
		for _, l := range lines[0] {
			if l.Size >= maxSize-0.6 && l.Box.Y1 > l.PageH*0.09 {
				big = append(big, l)
			}
		}
	}
	var heading []string
	for _, l := range big {
		heading = append(heading, l.Text)
	}
	h := strings.Join(heading, " ")
	if title == "" || strings.EqualFold(title, "untitled") || len(title) < 4 {
		title = h
	}
	desc := h
	if desc == "" || desc == title {
		desc = title
	}
	if len(title) > 160 {
		title = title[:160]
	}
	return strings.TrimSpace(title), strings.TrimSpace(desc)
}

// Variant is a part number found in a datasheet that belongs to one of its
// base parts: an orderable code (package, packing, voltage option).
type Variant struct {
	MPN   string `json:"mpn"`
	Base  string `json:"base"`
	Count int    `json:"count"`
}

// familyRe matches a family pattern: a part number with a run of
// lower-case x wildcards, such as STM32U545xx, TPS56x200 or BQ2407x.
var familyRe = regexp.MustCompile(`^[A-Z0-9][A-Z0-9-]{2,}x{1,3}[A-Z0-9-]{0,6}$`)

// FamilyRegex compiles a family pattern: each x stands for one letter or
// digit. Matching is case-insensitive.
func FamilyRegex(family string) *regexp.Regexp {
	var b strings.Builder
	b.WriteString("^(?i:")
	for _, r := range family {
		if r == 'x' {
			b.WriteString("[A-Z0-9]")
		} else {
			b.WriteString(regexp.QuoteMeta(string(r)))
		}
	}
	b.WriteString(")$")
	return regexp.MustCompile(b.String())
}

// isFamily reports whether tok is a plausible family pattern.
func isFamily(tok string) bool {
	if !familyRe.MatchString(tok) {
		return false
	}
	head := tok[:strings.IndexByte(tok, 'x')]
	digits := 0
	for _, r := range head {
		if r >= '0' && r <= '9' {
			digits++
		}
	}
	return digits >= 2
}

// docToken is a part-like token's occurrences in the document.
type docToken struct {
	count int
	pages map[int]bool
	early bool
}

// partTokens counts part-number-like tokens over every page. TI's
// material suffixes (".A", ".B") are dropped.
func partTokens(pageText []string) map[string]*docToken {
	out := map[string]*docToken{}
	for p, text := range pageText {
		for _, tok := range tokenize(text) {
			tok = strings.TrimRight(tok, ".:,;®™*")
			if i := len(tok) - 2; i > 0 && tok[i] == '.' && tok[i+1] >= 'A' && tok[i+1] <= 'Z' {
				tok = tok[:i]
			}
			if len(tok) < 4 || len(tok) > 28 {
				continue
			}
			if !isFamily(tok) && !isPartToken(tok) {
				continue
			}
			t := out[tok]
			if t == nil {
				t = &docToken{pages: map[int]bool{}}
				out[tok] = t
			}
			t.count++
			t.pages[p] = true
			if p < 3 {
				t.early = true
			}
		}
	}
	return out
}

// Families returns the family patterns a datasheet uses for itself: those
// in its title or first pages, or used throughout.
func Families(doc *pdf.Document, pageText []string) []string {
	toks := partTokens(pageText)
	title := map[string]bool{}
	for _, t := range tokenize(doc.Meta["Title"]) {
		title[t] = true
	}
	var out []string
	for tok, t := range toks {
		if !isFamily(tok) {
			continue
		}
		if title[tok] || (t.early && t.count >= 1) || t.count >= 5 {
			out = append(out, tok)
		}
	}
	sort.Strings(out)
	return out
}

// Expand completes the part list: bases are the scored part numbers plus
// every token matching a family pattern; orderable codes are tokens that
// extend a base (longest base wins) and appear on the first pages, in the
// back half (ordering and packaging pages) or repeatedly.
func Expand(pageText []string, bases, families []string) ([]string, []Variant) {
	toks := partTokens(pageText)
	set := map[string]bool{}
	var out []string
	add := func(b string) {
		if !set[b] {
			set[b] = true
			out = append(out, b)
		}
	}
	for _, b := range bases {
		add(b)
	}
	var members []string
	for _, f := range families {
		re := FamilyRegex(f)
		for tok := range toks {
			if !isFamily(tok) && re.MatchString(tok) && !strings.EqualFold(tok, f) {
				members = append(members, tok)
			}
		}
	}
	sort.Strings(members)
	for _, m := range members {
		add(m)
	}
	// Drop bases that are another base plus a word ("LMR51430SIMPLE").
	var clean []string
	for _, b := range out {
		bad := false
		for _, o := range out {
			if o != b && strings.HasPrefix(b, o) && isLetters(b[len(o):]) && len(b)-len(o) >= 4 {
				bad = true
			}
		}
		if !bad {
			clean = append(clean, b)
		}
	}
	out = clean
	sort.SliceStable(out, func(i, j int) bool { return len(out[i]) > len(out[j]) })
	backHalf := len(pageText) / 2
	var vars []Variant
	for tok, t := range toks {
		if set[tok] || isFamily(tok) {
			continue
		}
		base, suffix := "", ""
		for _, b := range out {
			if sfx, ok := extends(tok, b); ok {
				base, suffix = b, sfx
				break
			}
		}
		if base == "" || len(suffix) > 14 || badSuffix.MatchString(suffix) {
			continue
		}
		late := false
		for p := range t.pages {
			if p >= backHalf {
				late = true
			}
		}
		if t.early || late || t.count >= 3 {
			vars = append(vars, Variant{MPN: tok, Base: base, Count: t.count})
		}
	}
	sort.Slice(vars, func(i, j int) bool { return vars[i].MPN < vars[j].MPN })
	if len(vars) > 400 {
		vars = vars[:400]
	}
	sort.Strings(out)
	return out, vars
}

// badSuffix rejects suffixes that aren't ordering codes: list fragments
// ("/3/4/5"), trailing slashes and words or dates glued on by the text
// layer ("AUGUST").
var badSuffix = regexp.MustCompile(`(?i)/$|/\d|^/|^-$|^(JANUARY|FEBRUARY|MARCH|APRIL|MAY|JUNE|JULY|AUGUST|SEPTEMBER|OCTOBER|NOVEMBER|DECEMBER|SIMPLE|SERIES|FAMILY|DEVICES?|DATASHEET|EVALUATION|MODULE|BOARD|REVISED?|REVISION|PRODUCT|SWITCHER)$`)

// extends reports whether tok is an orderable code of base, returning the
// suffix. Besides plain extension (TPS563200 → TPS563200DDCR) it accepts
// a voltage code inserted before a trailing letter (TLV757P → TLV75733PDBVR).
func extends(tok, base string) (string, bool) {
	t, b := strings.ToUpper(tok), strings.ToUpper(base)
	if len(t) > len(b) && strings.HasPrefix(t, b) {
		return tok[len(base):], true
	}
	n := len(b)
	if n >= 4 && b[n-1] >= 'A' && b[n-1] <= 'Z' && b[n-2] >= '0' && b[n-2] <= '9' {
		re := regexp.MustCompile(`^` + regexp.QuoteMeta(b[:n-1]) + `\d{1,3}` + regexp.QuoteMeta(b[n-1:]))
		if loc := re.FindStringIndex(t); loc != nil {
			return tok[loc[1]:], true
		}
	}
	return "", false
}

// isLetters reports whether s is all ASCII letters.
func isLetters(s string) bool {
	for _, r := range s {
		if (r < 'A' || r > 'Z') && (r < 'a' || r > 'z') {
			return false
		}
	}
	return s != ""
}
