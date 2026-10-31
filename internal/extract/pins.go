package extract

import (
	"cmp"
	"fmt"
	"maps"
	"math"
	"regexp"
	"slices"
	"strconv"
	"strings"
)

// Checks on the pins read from a datasheet: an exposed pad the table
// doesn't number gets the next number, and pins missing against what the
// datasheet says its package has are reported, so an import with an empty
// or partial pin table says so instead of passing for complete.

// numberPads numbers each package's unnumbered pad after its last pin
// (N+1, as footprints usually number it) when the package's pins are
// numbered 1…N; its description says the number is ours.
func numberPads(pins []Pin) []Pin {
	last := map[string]int{}
	numeric := map[string]bool{}
	for _, p := range pins {
		if p.Number == "" {
			continue
		}
		n, err := strconv.Atoi(p.Number)
		if _, seen := numeric[p.Package]; !seen {
			numeric[p.Package] = true
		}
		if err != nil {
			numeric[p.Package] = false
			continue
		}
		last[p.Package] = max(last[p.Package], n)
	}
	for i, p := range pins {
		if p.Number != "" || !numeric[p.Package] || !padRe.MatchString(p.Name+" "+p.Description) {
			continue
		}
		last[p.Package]++
		pins[i].Number = strconv.Itoa(last[p.Package])
		pins[i].Description = strings.TrimSpace(p.Description + fmt.Sprintf(" (Numbered %d here, after the last pin: the datasheet gives it no number.)", last[p.Package]))
	}
	return pins
}

// Pin counts as datasheets write them: "12-Pin VQFN-HR", "8-pin TDFN",
// "20-ball" (countRe); "WSON-8", "QFN32" (packageDashRe, in capitals);
// "SOT-23-6", or a SOT-23 with three (sot23Re).
var (
	countRe       = regexp.MustCompile(`(?i)\b(\d{1,3})[- ](?:pins?|leads?|balls?|bumps?)\b(?:\s+\(?[A-Za-z][A-Za-z0-9-]{1,12})?`)
	packageDashRe = regexp.MustCompile(`\b[A-Z]{0,2}(?:SON|QFN|DFN|SOIC|SOP|QFP|DIP)-?(\d{1,3})\b`)
	sot23Re       = regexp.MustCompile(`\bSOT-?23(?:-(\d))?\b`)
)

// packageCount is a pin count with the text it was found in.
type packageCount struct {
	n    int
	text string
}

// packagePinCounts finds the pin counts a datasheet gives its packages,
// in its pin-configuration pages and captions first, then its first page.
func packagePinCounts(pinTexts []string, firstPage string) []packageCount {
	var out []packageCount
	read := func(text string) {
		for _, re := range []*regexp.Regexp{countRe, packageDashRe, sot23Re} {
			for _, m := range re.FindAllStringSubmatch(text, -1) {
				n, _ := strconv.Atoi(m[1])
				if re == sot23Re && m[1] == "" {
					n = 3
				}
				if n > 0 && n <= 400 {
					out = append(out, packageCount{n, m[0]})
				}
			}
		}
	}
	for _, t := range pinTexts {
		read(t)
	}
	if len(out) == 0 {
		read(firstPage)
	}
	return out
}

// pinWarnings compares the pins read with the counts the datasheet gives
// its packages: none read, or fewer than a package has (its exposed pad
// aside), is reported with what to do about it.
func pinWarnings(pins []Pin, counts []packageCount) []string {
	if len(counts) == 0 {
		return nil
	}
	if len(pins) == 0 {
		n := slices.MaxFunc(counts, func(a, b packageCount) int { return a.n - b.n })
		return []string{fmt.Sprintf("no pins were read, though the datasheet describes a %s package: record them with record_pins", n.text)}
	}
	numbers, named := map[string]map[string]bool{}, map[string]int{}
	var pkgs []string
	for _, p := range pins {
		if numbers[p.Package] == nil {
			numbers[p.Package] = map[string]bool{}
			pkgs = append(pkgs, p.Package)
		}
		named[p.Package]++
		if p.Number != "" && !padRe.MatchString(p.Name+" "+p.Description) {
			numbers[p.Package][p.Number] = true
		}
	}
	var out []string
	for _, pkg := range pkgs {
		want := expectedCount(pkg, counts, len(pkgs) == 1)
		got := len(numbers[pkg])
		label := ""
		if pkg != "" {
			label = " for " + pkg
		}
		switch {
		case want > 0 && got == 0:
			out = append(out, fmt.Sprintf("%d pins were read%s but the datasheet gives them no numbers: record them with record_pins", named[pkg], label))
		case want > 0 && got < want:
			out = append(out, fmt.Sprintf("only %d of %d pins were read%s: check the pin table and record the rest with record_pins", got, want, label))
		}
	}
	return out
}

// expectedCount is a package's pin count: the count given with a word of
// its label ("20-Pin WCSP" for "TPS2120 WCSP"), or, with one package, the
// largest count given.
func expectedCount(pkg string, counts []packageCount, only bool) int {
	best := 0
	for _, c := range counts {
		text := strings.ToUpper(c.text)
		for _, w := range strings.Fields(strings.ToUpper(pkg)) {
			if len(w) >= 3 && strings.Contains(text, w) {
				best = max(best, c.n)
			}
		}
	}
	if best == 0 && only {
		for _, c := range counts {
			best = max(best, c.n)
		}
	}
	return best
}

// pinTexts are where a datasheet gives its packages' pin counts: its
// pin-configuration sections and the captions of figures in them.
func pinTexts(secs []Section, figs []Figure) []string {
	var out []string
	pages := map[int]bool{}
	for _, s := range secs {
		if s.Kind == KindPins {
			out = append(out, s.Title+"\n"+s.Text)
			for p := s.Page; p <= s.EndPage; p++ {
				pages[p] = true
			}
		}
	}
	for _, f := range figs {
		if pages[f.Page] {
			out = append(out, f.Caption)
		}
	}
	return out
}

// terminalRe is a discrete part's terminal as its pinout drawing names it.
var terminalRe = regexp.MustCompile(`^(?i:gate|drain|source|base|collector|emitter|anode|cathode)$`)

// pinDigitRe is a pin number on a pinout drawing.
var pinDigitRe = regexp.MustCompile(`^\d{1,2}$`)

// terminalLetters are the letters a drawing labels a discrete part's
// terminals with when it gives no numbers at all, by category: an old
// datasheet's package drawing with just "D", "S" and "G" beside the
// leads (the NDS331N's Rev. 5). The whole set must be there.
var terminalLetters = map[string]map[string]string{
	"mosfet":   {"G": "Gate", "D": "Drain", "S": "Source"},
	"mosfet-n": {"G": "Gate", "D": "Drain", "S": "Source"},
	"mosfet-p": {"G": "Gate", "D": "Drain", "S": "Source"},
	"diode":    {"A": "Anode", "K": "Cathode"},
	"tvs":      {"A": "Anode", "K": "Cathode"},
}

// drawingPins reads the pins of a part without a pin table from its
// pinout drawing on the first pages: each terminal name with the pin
// number nearest it ("1 Gate", "2 Source", "Drain 3" around a SOT-23).
// Two terminals at least, each with its own number, or none. A drawing
// that only letters its leads ("D", "S", "G"), with no numbers to pair
// them with, gives the category's terminals unnumbered.
func drawingPins(lines [][]Line, category string) []Pin {
	type place struct {
		w    word
		page int
	}
	for pi := 0; pi < min(2, len(lines)); pi++ {
		var terms, digits, letters []place
		for _, l := range lines[pi] {
			for _, w := range words(l.Spans) {
				switch {
				case terminalRe.MatchString(w.text):
					terms = append(terms, place{w, pi})
				case pinDigitRe.MatchString(w.text):
					digits = append(digits, place{w, pi})
				case terminalLetters[category][w.text] != "":
					letters = append(letters, place{w, pi})
				}
			}
		}
		// Nearest pairs first, each terminal and number used once.
		type pair struct {
			t, d int
			dist float64
		}
		var pairs []pair
		for i, t := range terms {
			for j, d := range digits {
				dx := (t.w.box.X0+t.w.box.X1)/2 - (d.w.box.X0+d.w.box.X1)/2
				dy := (t.w.box.Y0+t.w.box.Y1)/2 - (d.w.box.Y0+d.w.box.Y1)/2
				if dist := math.Hypot(dx, dy); dist <= 30 {
					pairs = append(pairs, pair{i, j, dist})
				}
			}
		}
		slices.SortFunc(pairs, func(a, b pair) int { return cmp.Compare(a.dist, b.dist) })
		usedT, usedD, names := map[int]bool{}, map[int]bool{}, map[string]bool{}
		var pins []Pin
		for _, p := range pairs {
			name := strings.ToUpper(terms[p.t].w.text[:1]) + strings.ToLower(terms[p.t].w.text[1:])
			if usedT[p.t] || usedD[p.d] || names[name] {
				continue
			}
			usedT[p.t], usedD[p.d], names[name] = true, true, true
			pins = append(pins, Pin{Number: digits[p.d].w.text, Name: name, Description: "From the pinout drawing.", Page: pi})
		}
		if len(pins) >= 2 {
			slices.SortFunc(pins, func(a, b Pin) int { return cmp.Compare(a.Number, b.Number) })
			return pins
		}
		var ws []word
		for _, l := range letters {
			ws = append(ws, l.w)
		}
		if pins := letteredPins(ws, terminalLetters[category], pi); len(pins) > 0 {
			return pins
		}
	}
	return nil
}

// leadConventions number a package's lettered leads the way its makers
// do when a drawing only letters them: a SOT-23 MOSFET's gate, source
// and drain are 1, 2 and 3 (KiCad's Q_NMOS_GSD; onsemi's, Nexperia's
// and Diodes' SOT-23 FETs alike).
var leadConventions = map[string]map[string]string{
	"SOT23": {"Gate": "1", "Source": "2", "Drain": "3"},
}

// conventionCategories are the categories a convention applies to.
var conventionCategories = map[string]bool{"mosfet": true, "mosfet-n": true, "mosfet-p": true}

// numberByConvention numbers pins read unnumbered off a drawing by the
// convention for the package, when the datasheet names one with a
// convention for the part's category and the pins are exactly its leads.
// It says so in each pin's description and returns a note for the
// import.
func numberByConvention(pins []Pin, pkgs []Package, category string) (string, bool) {
	if len(pins) == 0 || !conventionCategories[category] {
		return "", false
	}
	for _, p := range pins {
		if p.Number != "" {
			return "", false
		}
	}
	var conv map[string]string
	pkgName := ""
	for _, pk := range pkgs {
		if c, ok := leadConventions[normalPackage(pk.Name)]; ok {
			conv, pkgName = c, pk.Name
			break
		}
	}
	if conv == nil || len(pins) != len(conv) {
		return "", false
	}
	for _, p := range pins {
		if conv[p.Name] == "" {
			return "", false
		}
	}
	for i := range pins {
		pins[i].Number = conv[pins[i].Name]
		pins[i].Description = strings.TrimSpace(pins[i].Description + fmt.Sprintf(" Numbered %s by the %s convention (gate 1, source 2, drain 3): the drawing doesn't number its leads; check it against the package drawing.", pins[i].Number, pkgName))
	}
	slices.SortFunc(pins, func(a, b Pin) int { return cmp.Compare(a.Number, b.Number) })
	return fmt.Sprintf("the pins are numbered by the %s convention (gate 1, source 2, drain 3), not by the datasheet: check them", pkgName), true
}

// normalPackage is a package name with only its letters and digits,
// upper case, a trailing lead count dropped (SOT-23-3 is SOT-23).
func normalPackage(name string) string {
	var b strings.Builder
	for _, r := range strings.TrimSuffix(strings.ToUpper(strings.TrimSpace(name)), "-3") {
		if r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// letterSpread is how far apart (in points) the letters on one package
// drawing can be: a SOT-23 drawn an inch or two across.
const letterSpread = 120

// letteredPins reads a drawing that letters its leads without numbering
// them: the tightest cluster with every letter of the set once, each
// letter the pin it names, numbers left for the user. Letters scattered
// over the page (a table's subscripts) are no drawing.
func letteredPins(ws []word, set map[string]string, page int) []Pin {
	keys := slices.Sorted(maps.Keys(set))
	byLetter := make([][]word, len(keys))
	for i, k := range keys {
		for _, w := range ws {
			if w.text == k {
				byLetter[i] = append(byLetter[i], w)
			}
		}
		if len(byLetter[i]) == 0 {
			return nil
		}
	}
	centre := func(w word) (float64, float64) { return (w.box.X0 + w.box.X1) / 2, (w.box.Y0 + w.box.Y1) / 2 }
	best, bestSpread := []word(nil), math.Inf(1)
	var pick []word
	var walk func(i int)
	walk = func(i int) {
		if i == len(keys) {
			spread := 0.0
			for a := range pick {
				for b := a + 1; b < len(pick); b++ {
					ax, ay := centre(pick[a])
					bx, by := centre(pick[b])
					spread = max(spread, math.Hypot(ax-bx, ay-by))
				}
			}
			if spread < bestSpread {
				best, bestSpread = slices.Clone(pick), spread
			}
			return
		}
		for _, w := range byLetter[i] {
			pick = append(pick, w)
			walk(i + 1)
			pick = pick[:len(pick)-1]
		}
	}
	walk(0)
	if bestSpread > letterSpread {
		return nil
	}
	var pins []Pin
	for i, w := range best {
		pins = append(pins, Pin{Name: set[keys[i]], Description: "From the pinout drawing, which letters the leads (" + w.text + ") and gives no pin numbers.", Page: page})
	}
	slices.SortFunc(pins, func(a, b Pin) int { return cmp.Compare(a.Name, b.Name) })
	return pins
}
