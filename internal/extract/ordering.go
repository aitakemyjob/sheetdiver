package extract

import (
	"regexp"
	"strconv"
	"strings"
)

// Ordering schemes. A family datasheet explains its part numbers with an
// example cut into fields ("STM32 U 545 V E T 6 Q TR") and, under it, each
// field's name and what its codes mean ("Pin count: C = 48 pins …",
// "Package: T = LQFP …"), in the example's order. A part number then says
// how many pins it has and which packages it comes in, which is what
// tells a family's packages apart: the STM32U545CE is the LQFP48 and the
// UFQFPN48, the STM32U545VE the LQFP100 and the UFBGA100.

// OrderingField is a field of a part number: its name, where it sits and
// what its codes mean.
type OrderingField struct {
	Name string `json:"name"`
	// At is where the field starts in a part number, and Len how long it
	// is, from the example.
	At, Len int
	// Codes are what each code means ("C": "48 pins").
	Codes map[string]string `json:"codes"`
}

// Ordering is a datasheet's part numbering scheme.
type Ordering struct {
	// Example is the example part number, its fields run together.
	Example string          `json:"example"`
	Fields  []OrderingField `json:"fields"`
}

var (
	// exampleRe is the label before an ordering example.
	exampleRe = regexp.MustCompile(`(?i)^example:?$`)
	// codeRe is a field's code and its meaning: "C = 48 pins".
	codeRe = regexp.MustCompile(`^(\S{1,8})\s*=\s*(.+)$`)
	// pinCountFieldRe names the pin count field.
	pinCountFieldRe = regexp.MustCompile(`(?i)pin count|number of pins|^pins$`)
	// packageFieldRe names the package field.
	packageFieldRe = regexp.MustCompile(`(?i)^package( type)?$`)
	// pinsMeaningRe is a pin count's meaning: "48 pins", "100-pin".
	pinsMeaningRe = regexp.MustCompile(`(?i)^(\d{1,3})[ -]?(pins?|balls?|bumps?|leads?)\b`)
)

// OrderingScheme reads a datasheet's ordering example and its fields, on
// the pages of its ordering sections. ok is false when there's none, or
// its fields don't line up with the example's.
func OrderingScheme(pages [][]Line, secs []Section) (Ordering, bool) {
	for _, pi := range orderingPages(secs, len(pages)) {
		rows := pages[pi]
		for i, l := range rows {
			text := strings.TrimSpace(l.Text)
			label, rest, _ := strings.Cut(text, " ")
			if !exampleRe.MatchString(label) {
				continue
			}
			// The example on the label's line, or the next.
			next := i + 1
			if strings.TrimSpace(rest) == "" && next < len(rows) {
				rest, next = rows[next].Text, next+1
			}
			tokens := strings.Fields(rest)
			if len(tokens) < 3 {
				continue
			}
			if o, ok := orderingFields(tokens, rows[next:]); ok {
				return o, true
			}
		}
	}
	return Ordering{}, false
}

// orderingPages are the pages of the ordering sections.
func orderingPages(secs []Section, n int) []int {
	var out []int
	seen := map[int]bool{}
	for _, s := range secs {
		if s.Kind != KindOrdering {
			continue
		}
		for p := s.Page; p <= s.EndPage && p < n; p++ {
			if !seen[p] {
				seen[p] = true
				out = append(out, p)
			}
		}
	}
	return out
}

// orderingFields reads the fields under an example: each a name line
// followed by its code lines, one field per example token.
func orderingFields(tokens []string, lines []Line) (Ordering, bool) {
	var fields []OrderingField
	for _, l := range lines {
		text := strings.TrimSpace(l.Text)
		if m := codeRe.FindStringSubmatch(text); m != nil && len(fields) > 0 {
			fields[len(fields)-1].Codes[m[1]] = strings.TrimSpace(m[2])
			continue
		}
		if len(fields) == len(tokens) {
			break
		}
		if text != "" && len(text) < 40 {
			fields = append(fields, OrderingField{Name: text, Codes: map[string]string{}})
		}
	}
	if len(fields) != len(tokens) {
		return Ordering{}, false
	}
	at := 0
	for i := range fields {
		fields[i].At, fields[i].Len = at, len(tokens[i])
		at += len(tokens[i])
		if len(fields[i].Codes) == 0 {
			return Ordering{}, false
		}
	}
	return Ordering{Example: strings.Join(tokens, ""), Fields: fields}, true
}

// field is the scheme's field of a name, or nil.
func (o Ordering) field(re *regexp.Regexp) *OrderingField {
	for i := range o.Fields {
		if re.MatchString(o.Fields[i].Name) {
			return &o.Fields[i]
		}
	}
	return nil
}

// prefixFits reports whether a part number reaches a field and is the
// scheme's up to it: each field before it one of its codes.
func (o Ordering) prefixFits(mpn string, f *OrderingField) bool {
	mpn = strings.ToUpper(mpn)
	if len(mpn) < f.At+f.Len {
		return false
	}
	for _, g := range o.Fields {
		if g.At >= f.At {
			break
		}
		code := mpn[g.At : g.At+g.Len]
		known := false
		for c := range g.Codes {
			if strings.EqualFold(c, code) {
				known = true
				break
			}
		}
		if !known {
			return false
		}
	}
	return true
}

// Pins is how many pins a part number says its part has, or 0.
func (o Ordering) Pins(mpn string) int {
	f := o.field(pinCountFieldRe)
	if f == nil || !o.prefixFits(mpn, f) {
		return 0
	}
	code := strings.ToUpper(mpn)[f.At : f.At+f.Len]
	for c, meaning := range f.Codes {
		if strings.EqualFold(c, code) {
			if m := pinsMeaningRe.FindStringSubmatch(meaning); m != nil {
				n, _ := strconv.Atoi(m[1])
				return n
			}
		}
	}
	return 0
}

// PackageWords are the package types a part number allows: its own
// package code's, else (a base part number that stops before it) every
// one the scheme lists. Nil when the scheme has no package field.
func (o Ordering) PackageWords(mpn string) []string {
	f := o.field(packageFieldRe)
	if f == nil {
		return nil
	}
	if o.prefixFits(mpn, f) {
		code := strings.ToUpper(mpn)[f.At : f.At+f.Len]
		for c, meaning := range f.Codes {
			if strings.EqualFold(c, code) {
				return []string{packageWord(meaning)}
			}
		}
	}
	var out []string
	for _, meaning := range f.Codes {
		out = append(out, packageWord(meaning))
	}
	return out
}

// packageWord is a package code's meaning's first word: "LQFP".
func packageWord(meaning string) string {
	w, _, _ := strings.Cut(strings.TrimSpace(meaning), " ")
	return strings.ToUpper(strings.Trim(w, ",;."))
}

// packageCountRe is the pin count at the end of a package's name.
var packageCountRe = regexp.MustCompile(`[A-Z]-?(\d{1,3})$`)

// partsOfPackages names, on each package, the datasheet's parts that come
// in it by the ordering scheme: as many pins as its name says, and a
// package type the part number allows. A package no part claims, or a
// datasheet without a scheme, keeps no names (every part's).
func partsOfPackages(pkgs []Package, parts []string, o Ordering) {
	for i := range pkgs {
		name := strings.ToUpper(strings.TrimSpace(pkgs[i].Name))
		m := packageCountRe.FindStringSubmatch(name)
		if m == nil {
			continue
		}
		count, _ := strconv.Atoi(m[1])
		var mine []string
		for _, p := range parts {
			if o.Pins(p) != count {
				continue
			}
			words := o.PackageWords(p)
			fits := words == nil
			for _, w := range words {
				fits = fits || w != "" && strings.HasPrefix(name, w)
			}
			if fits {
				mine = append(mine, p)
			}
		}
		pkgs[i].Parts = mine
	}
}
