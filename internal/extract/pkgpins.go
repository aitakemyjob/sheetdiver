package extract

import (
	"math"
	"regexp"
	"sort"
	"strings"

	"github.com/aitakemyjob/sheetdiver/internal/pdf"
)

// Pin tables with a column per package. An MCU family's datasheet gives
// one table for all its packages: a pin number (or ball) column headed by
// each package's name, often set sideways ("LQFP48", "UFBGA100 SMPS"),
// then the pin's name, type, I/O structure, notes and functions. A pin a
// package doesn't have is a dash. Names run over lines: "PC14-" over
// "OSC32_IN", with its function after reset "(PC14)" under it.

// pkgColumn is a column of the table: pin numbers, for the packages
// headed over it (two packages with the same pinout share one: "LQFP48"
// over "UFQFPN48"), or a heading's (name, type, functions…).
type pkgColumn struct {
	label    string
	packages []string
	role     string // package, name, type, alternate, additional, other
	x        float64
}

var (
	// pinCellRe is a pin number or a ball: 12, A1, AB10.
	pinCellRe = regexp.MustCompile(`^(?:[A-Z]{1,2}\d{1,2}|\d{1,3})$`)
	// pkgHeadRoles name a table's other headings.
	pkgHeadRoles = []struct {
		re   *regexp.Regexp
		role string
	}{
		{regexp.MustCompile(`(?i)^pin name`), "name"},
		{regexp.MustCompile(`(?i)^pin type`), "type"},
		{regexp.MustCompile(`(?i)^alternate functions?`), "alternate"},
		{regexp.MustCompile(`(?i)^additional( functions?)?$`), "additional"},
		{regexp.MustCompile(`(?i)^(i/o structure|notes)`), "other"},
	}
)

// PackageColumnPins reads the pin tables with a column per package on
// the pages of the pin sections.
func PackageColumnPins(doc *pdf.Document, secs []Section) []Pin {
	var out []Pin
	seen := map[int]bool{}
	for _, s := range secs {
		if s.Kind != KindPins {
			continue
		}
		for pi := s.Page; pi <= s.EndPage && pi < len(doc.Pages); pi++ {
			if seen[pi] {
				continue
			}
			seen[pi] = true
			out = append(out, packageColumnPage(Rows(doc.Pages[pi].Spans), doc.Pages[pi].Graphics, pi+1)...)
		}
	}
	return dedupePins(out)
}

// packageColumnPage reads one page's table, when it has one; its rules
// across the page are its rows' edges.
func packageColumnPage(rows []Row, graphics []pdf.Graphic, page int) []Pin {
	heads, bottom := packageColumns(rows)
	nameX := math.Inf(1)
	var pkgHeads []pkgColumn
	var cols []pkgColumn
	for _, c := range heads {
		if c.role == "package" {
			pkgHeads = append(pkgHeads, c)
			continue
		}
		if c.role == "name" {
			nameX = math.Min(nameX, c.x)
		}
		cols = append(cols, c)
	}
	if len(pkgHeads) < 2 || math.IsInf(nameX, 1) {
		return nil
	}
	cols = append(dataColumns(rows, bottom, nameX-25, pkgHeads), cols...)
	colOf := func(s pdf.Span) *pkgColumn {
		cx := (s.Box.X0 + s.Box.X1) / 2
		var best *pkgColumn
		bd := math.Inf(1)
		for i := range cols {
			if d := math.Abs(cols[i].x - cx); d < bd {
				best, bd = &cols[i], d
			}
		}
		// Under the package heads, only a package column will do: a
		// number whose column has no head isn't the name's.
		if best == nil || best.role == "package" && bd > 8 || bd > 90 || cx < nameX-25 && best.role != "package" {
			return nil
		}
		return best
	}
	// The table's text under its headings, span by span (a tall span
	// down the page's edge throws a row's box), and the pins' rows: a pin
	// number or ball under a package.
	midY := func(s pdf.Span) float64 { return (s.Box.Y0 + s.Box.Y1) / 2 }
	var body []pdf.Span
	var at []float64
	for _, r := range rows {
		pinRow := false
		for _, s := range r.Spans {
			if s.Vertical || midY(s) <= bottom {
				continue
			}
			body = append(body, s)
			if c := colOf(s); !pinRow && c != nil && c.role == "package" && pinCellRe.MatchString(strings.TrimSpace(s.Text)) {
				at, pinRow = append(at, midY(s)), true
			}
		}
	}
	var rules []float64
	for _, g := range graphics {
		if g.Box.H() < 2 && g.Box.W() > 200 && g.Box.Y1 > bottom-12 {
			rules = append(rules, (g.Box.Y0+g.Box.Y1)/2)
		}
	}
	sort.Float64s(rules)
	var out []Pin
	for k, y := range at {
		lo, hi := bottom, math.Inf(1)
		if k > 0 {
			lo = (at[k-1] + y) / 2
		}
		if k+1 < len(at) {
			hi = (y + at[k+1]) / 2
		}
		// The last row's band is as deep under it as over it: the
		// table's notes follow.
		if k+1 == len(at) && k > 0 {
			hi = y + (y - lo)
		}
		// The rules round the row, when the table has them.
		if i := sort.SearchFloat64s(rules, y); i > 0 && i < len(rules) {
			lo, hi = rules[i-1], rules[i]
		}
		cells := map[*pkgColumn][]pdf.Span{}
		for _, s := range body {
			if cy := midY(s); cy < lo || cy >= hi {
				continue
			}
			if c := colOf(s); c != nil {
				cells[c] = append(cells[c], s)
			}
		}
		name, typ, desc := "", "", []string{}
		for i := range cols {
			c := &cols[i]
			spans := cells[c]
			sort.SliceStable(spans, func(a, b int) bool { return spans[a].Box.Y0 < spans[b].Box.Y0 })
			switch c.role {
			case "name":
				// The name's lines; after it, in brackets, its function
				// after reset.
				var parts []string
				bracket := false
				for _, s := range spans {
					t := strings.TrimSpace(s.Text)
					if strings.HasPrefix(t, "(") && len(parts) > 0 {
						bracket = true
					}
					if bracket {
						bracket = !strings.Contains(t, ")")
						continue
					}
					parts = append(parts, t)
				}
				name = strings.Join(parts, "")
			case "type":
				typ = joinSpans(spans)
			case "alternate", "additional":
				if t := joinSpans(spans); t != "" && t != "-" {
					desc = append(desc, c.label+": "+t)
				}
			}
		}
		if name == "" {
			continue
		}
		typ = stPinType(typ, name)
		for i := range cols {
			c := &cols[i]
			if c.role != "package" {
				continue
			}
			for _, s := range cells[c] {
				if n := strings.TrimSpace(s.Text); pinCellRe.MatchString(n) && math.Abs((s.Box.Y0+s.Box.Y1)/2-y) < 6 {
					for _, pk := range c.packages {
						out = append(out, Pin{Package: pk, Number: n, Name: name, Type: typ, Description: strings.Join(desc, ". "), Page: page})
					}
				}
			}
		}
	}
	return out
}

// dataColumns are the pin number columns where the numbers sit, left of
// the name column, each with the packages headed nearest over it.
func dataColumns(rows []Row, bottom, right float64, heads []pkgColumn) []pkgColumn {
	var xs []float64
	for _, r := range rows {
		if r.Box.Y0 < bottom {
			continue
		}
		for _, s := range r.Spans {
			t := strings.TrimSpace(s.Text)
			if cx := (s.Box.X0 + s.Box.X1) / 2; cx < right && !s.Vertical && (t == "-" || pinCellRe.MatchString(t)) {
				xs = append(xs, cx)
			}
		}
	}
	sort.Float64s(xs)
	var cols []pkgColumn
	n := 0
	for i, x := range xs {
		if i > 0 && x-xs[i-1] > 6 {
			cols[len(cols)-1].x /= float64(n)
			n = 0
		}
		if n == 0 {
			cols = append(cols, pkgColumn{role: "package"})
		}
		cols[len(cols)-1].x += x
		n++
	}
	if n > 0 {
		cols[len(cols)-1].x /= float64(n)
	}
	for _, h := range heads {
		best, bd := -1, 25.0
		for i := range cols {
			if d := math.Abs(cols[i].x - h.x); d < bd {
				best, bd = i, d
			}
		}
		if best >= 0 {
			cols[best].packages = append(cols[best].packages, h.label)
		}
	}
	var out []pkgColumn
	for _, c := range cols {
		if len(c.packages) > 0 {
			c.label = strings.Join(c.packages, ", ")
			out = append(out, c)
		}
	}
	return out
}

// packageColumns finds a page's headings: package names over the number
// columns and the pin's other columns, and where the headings end.
func packageColumns(rows []Row) ([]pkgColumn, float64) {
	var cols []pkgColumn
	bottom := 0.0
	for _, r := range rows {
		for _, s := range r.Spans {
			t := strings.Join(strings.Fields(s.Text), " ")
			x := (s.Box.X0 + s.Box.X1) / 2
			if m := packageNameRe.FindString(t); m != "" && len(t) <= 20 && len(m) >= len(strings.Fields(t)[0])-1 {
				cols = append(cols, pkgColumn{label: strings.ToUpper(t), role: "package", x: x})
				bottom = math.Max(bottom, s.Box.Y1)
				continue
			}
			for _, h := range pkgHeadRoles {
				if h.re.MatchString(t) {
					cols = append(cols, pkgColumn{label: t, role: h.role, x: x})
					bottom = math.Max(bottom, s.Box.Y1)
					break
				}
			}
		}
		// The headings are at the top: stop at the first pin row under them.
		if bottom > 0 && r.Box.Y0 > bottom+40 {
			break
		}
	}
	// A heading over two columns' worth of lines ("Pin name (function
	// after reset)") counts once, by its first line.
	sort.SliceStable(cols, func(a, b int) bool { return cols[a].x < cols[b].x })
	var out []pkgColumn
	for _, c := range cols {
		if n := len(out); n > 0 && out[n-1].role == c.role && c.role != "package" && math.Abs(out[n-1].x-c.x) < 20 {
			continue
		}
		out = append(out, c)
	}
	for i := range out {
		if out[i].role == "alternate" {
			out[i].label = "Alternate functions"
		}
		if out[i].role == "additional" {
			out[i].label = "Additional functions"
		}
	}
	return out, bottom
}

// joinSpans joins a cell's lines, top to bottom.
func joinSpans(ss []pdf.Span) string {
	var parts []string
	for _, s := range ss {
		if t := strings.TrimSpace(s.Text); t != "" {
			parts = append(parts, t)
		}
	}
	return strings.TrimSuffix(strings.Join(parts, " "), ",")
}

// stPinType maps a pin type letter to SheetDiver's: S (supply) is power,
// or ground for a VSS.
func stPinType(t, name string) string {
	switch strings.ToUpper(strings.TrimSpace(t)) {
	case "S":
		if strings.HasPrefix(strings.ToUpper(name), "VSS") {
			return "G"
		}
		return "P"
	case "I/O":
		return "I/O"
	case "I":
		return "I"
	case "O":
		return "O"
	}
	return ""
}

// dedupePins drops a pin read twice (a table's page read again).
func dedupePins(ps []Pin) []Pin {
	seen := map[string]bool{}
	var out []Pin
	for _, p := range ps {
		k := p.Package + "|" + p.Number
		if seen[k] {
			continue
		}
		seen[k] = true
		out = append(out, p)
	}
	return out
}
