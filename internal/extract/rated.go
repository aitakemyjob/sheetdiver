package extract

import (
	"regexp"
	"strconv"
	"strings"
)

// ratedCategories are the regulator types whose output-current rating is
// read from page 1 when the spec tables lack one.
var ratedCategories = map[string]bool{"buck": true, "buck-boost": true, "ldo": true, "power": true, "boost": true}

// Output-current phrases on a datasheet's first page.
var (
	amps = `(\d+(?:\.\d+)?)\s*-?\s*(mA|A)\b`
	// "3-A continuous output current", "250 mA Output Current", "400-mA output current"
	ratedBeforeRe = regexp.MustCompile(`(?i)` + amps + `\s+(?:of\s+)?(?:continuous\s+|maximum\s+|max\.?\s+)?(?:output|load)\s+current`)
	// "Output Current: 600mA (Min.)", "output current up to 2 A"
	ratedAfterRe = regexp.MustCompile(`(?i)(?:output|load)\s+current\s*(?::|of|up\s+to|to)?\s*` + amps)
	// "up to 3-A load current" is covered above; "TPS563200 - 3-A converter"
	perPartRe = regexp.MustCompile(`^[^A-Za-z0-9]*([A-Z][A-Z0-9-]{3,}?)\s*[-–:]\s*` + amps + `\s+(?:converter|regulator|output|LDO|buck|step)`)
	// A current in a title: "3-A, Synchronous Buck", "200-mA, Low-Dropout"
	titleAmpsRe = regexp.MustCompile(`(?i)` + amps)
	// Currents that aren't output ratings.
	notRatingRe = regexp.MustCompile(`(?i)peak|switch|valley|limit|charge|quiescent|\bI\s*Q\b|shutdown|leakage|sink|source current|inrush`)
)

// RatedCurrent reads a regulator's output-current rating from its first
// page and title, for datasheets whose spec tables have no output-current
// row (common for TI converters). It returns one "iout" parameter per
// rating: per part when the page lists each part's current ("TPS563200 -
// 3-A converter"), else the lowest current the page states (conservative
// when it quotes several, such as 2 A in boost and 3 A in buck mode).
// Boost titles are skipped: they quote the switch current limit.
func RatedCurrent(category, title string, page0 []Line, parts []string) []Param {
	if !ratedCategories[category] {
		return nil
	}
	known := map[string]bool{}
	for _, p := range parts {
		known[strings.ToUpper(p)] = true
	}
	mk := func(v float64, unit, cond string, l Line) Param {
		return Param{Key: "iout", Name: "Output current (rated, from the first page)", Max: &v, Unit: unit, Conditions: cond, Section: KindFeatures, Page: 0, Box: l.Box, Raw: l.Text}
	}
	parse := func(num, unit string) (float64, string, bool) {
		v, err := strconv.ParseFloat(num, 64)
		if err != nil || v <= 0 {
			return 0, "", false
		}
		if unit == "mA" && v >= 1 && v <= 50000 || unit == "A" && v > 0 && v <= 100 {
			return v, unit, true
		}
		return 0, "", false
	}
	// Per-part ratings.
	var out []Param
	for _, l := range page0 {
		if m := perPartRe.FindStringSubmatch(strings.TrimSpace(l.Text)); m != nil && known[strings.ToUpper(m[1])] {
			if v, u, ok := parse(m[2], m[3]); ok {
				out = append(out, mk(v, u, m[1], l))
			}
		}
	}
	if len(out) > 0 {
		return out
	}
	// Phrases: the lowest stated rating.
	var best *Param
	consider := func(num, unit, text string, l Line) {
		if notRatingRe.MatchString(text) {
			return
		}
		v, u, ok := parse(num, unit)
		if !ok {
			return
		}
		si := v
		if u == "mA" {
			si = v / 1000
		}
		if best != nil {
			cur := *best.Max
			if best.Unit == "mA" {
				cur /= 1000
			}
			if cur <= si {
				return
			}
		}
		p := mk(v, u, "", l)
		best = &p
	}
	for _, l := range page0 {
		for _, re := range []*regexp.Regexp{ratedBeforeRe, ratedAfterRe} {
			for _, m := range re.FindAllStringSubmatchIndex(l.Text, -1) {
				// Look at the words just around the match for disqualifiers.
				lo, hi := max(0, m[0]-12), min(len(l.Text), m[1]+12)
				consider(l.Text[m[2]:m[3]], l.Text[m[4]:m[5]], l.Text[lo:hi], l)
			}
		}
	}
	if best != nil {
		return []Param{*best}
	}
	if category == "boost" {
		return nil
	}
	// The title, when it names a single current.
	ms := titleAmpsRe.FindAllStringSubmatchIndex(title, -1)
	if len(ms) == 1 {
		m := ms[0]
		ctx := title[m[1]:min(len(title), m[1]+20)]
		if !notRatingRe.MatchString(ctx) && !strings.Contains(strings.ToLower(ctx), "iq") {
			if v, u, ok := parse(title[m[2]:m[3]], title[m[4]:m[5]]); ok {
				l := Line{Text: title}
				if len(page0) > 0 {
					l = page0[0]
					for _, x := range page0 {
						if strings.Contains(title, strings.TrimSpace(x.Text)) && len(x.Text) > 10 {
							l = x
							break
						}
					}
				}
				p := mk(v, u, "", l)
				p.Raw = title
				return []Param{p}
			}
		}
	}
	return nil
}
