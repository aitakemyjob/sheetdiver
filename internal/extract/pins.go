package extract

import (
	"cmp"
	"fmt"
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
	numbers := map[string]map[string]bool{}
	var pkgs []string
	for _, p := range pins {
		if numbers[p.Package] == nil {
			numbers[p.Package] = map[string]bool{}
			pkgs = append(pkgs, p.Package)
		}
		if p.Number != "" && !padRe.MatchString(p.Name+" "+p.Description) {
			numbers[p.Package][p.Number] = true
		}
	}
	var out []string
	for _, pkg := range pkgs {
		want := expectedCount(pkg, counts, len(pkgs) == 1)
		if got := len(numbers[pkg]); want > 0 && got < want {
			label := ""
			if pkg != "" {
				label = " for " + pkg
			}
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

// drawingPins reads the pins of a part without a pin table from its
// pinout drawing on the first pages: each terminal name with the pin
// number nearest it ("1 Gate", "2 Source", "Drain 3" around a SOT-23).
// Two terminals at least, each with its own number, or none.
func drawingPins(lines [][]Line) []Pin {
	type place struct {
		w    word
		page int
	}
	for pi := 0; pi < min(2, len(lines)); pi++ {
		var terms, digits []place
		for _, l := range lines[pi] {
			for _, w := range words(l.Spans) {
				switch {
				case terminalRe.MatchString(w.text):
					terms = append(terms, place{w, pi})
				case pinDigitRe.MatchString(w.text):
					digits = append(digits, place{w, pi})
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
	}
	return nil
}
