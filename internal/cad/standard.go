package cad

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// Standard packages: the JEDEC outlines most datasheets use unchanged, so
// a part whose package is one of them gets a footprint before anyone has
// read its drawing. The datasheet's own dimensions, once recorded, win.

// d is a dimension from min and max.
func d(lo, hi float64) Dim { return Dim{Min: lo, Max: hi} }

// soic is an SOIC (JEDEC MS-012, 3.9 mm body) with n pins.
func soic(n int, length float64) Package {
	return Package{Family: "gullwing", Name: fmt.Sprintf("SOIC-%d", n), Pins: n, Pitch: 1.27,
		BodyLength: d(length-0.1, length+0.1), BodyWidth: d(3.8, 4.0), Span: d(5.8, 6.2),
		LeadLength: d(0.4, 1.27), LeadWidth: d(0.31, 0.51), Height: d(1.35, 1.75), Standoff: d(0.1, 0.25), LeadThickness: d(0.17, 0.25)}
}

// tssop is a TSSOP (JEDEC MO-153, 4.4 mm body) with n pins.
func tssop(n int, length float64) Package {
	return Package{Family: "gullwing", Name: fmt.Sprintf("TSSOP-%d", n), Pins: n, Pitch: 0.65,
		BodyLength: d(length-0.1, length+0.1), BodyWidth: d(4.3, 4.5), Span: d(6.2, 6.6),
		LeadLength: d(0.45, 0.75), LeadWidth: d(0.19, 0.30), Height: Dim{Max: 1.2}, Standoff: d(0.05, 0.15), LeadThickness: d(0.09, 0.2)}
}

// sot23 is a SOT-23 family package (JEDEC MO-178) with n leads.
func sot23(n int) Package {
	p := Package{Family: "gullwing", Name: fmt.Sprintf("SOT-23-%d", n), Pins: n, Pitch: 0.95,
		BodyLength: d(2.8, 3.0), BodyWidth: d(1.5, 1.7), Span: d(2.6, 3.0),
		LeadLength: d(0.3, 0.6), LeadWidth: d(0.3, 0.5), Height: d(0.9, 1.45), Standoff: d(0, 0.15), LeadThickness: d(0.08, 0.22)}
	switch n {
	case 3:
		p.Name, p.BodyWidth, p.Span, p.Height = "SOT-23", d(1.2, 1.4), d(2.1, 2.64), d(0.89, 1.12)
		p.Layout = &Layout{Left: []string{"1", "", "2"}, Right: []string{"", "3", ""}}
	case 5:
		p.Layout = &Layout{Left: []string{"1", "2", "3"}, Right: []string{"5", "", "4"}}
	}
	return p
}

// msop is an MSOP (JEDEC MO-187, 3 mm body).
func msop(n int) Package {
	p := Package{Family: "gullwing", Name: fmt.Sprintf("MSOP-%d", n), Pins: n, Pitch: 0.65,
		BodyLength: d(2.9, 3.1), BodyWidth: d(2.9, 3.1), Span: d(4.75, 5.05),
		LeadLength: d(0.4, 0.7), LeadWidth: d(0.22, 0.38), Height: Dim{Max: 1.1}, Standoff: d(0.05, 0.15), LeadThickness: d(0.08, 0.23)}
	if n == 10 {
		p.Pitch, p.LeadWidth = 0.5, d(0.17, 0.27)
	}
	return p
}

// chipSize is a chip resistor or capacitor by its imperial code.
func chipSize(code string, l, w, t, h Dim) Package {
	return Package{Family: "chip", Name: code, Pins: 2, BodyLength: l, BodyWidth: w, LeadLength: t, Height: h}
}

// standard are the standard packages by their names, normalised.
var standard = map[string]Package{}

// init fills the table.
func init() {
	for _, p := range []Package{
		soic(8, 4.9), soic(14, 8.65), soic(16, 9.9),
		tssop(8, 3.0), tssop(14, 5.0), tssop(16, 5.0), tssop(20, 6.5), tssop(24, 7.8), tssop(28, 9.7),
		sot23(3), sot23(5), sot23(6), msop(8), msop(10),
		chipSize("0402", d(0.9, 1.1), d(0.45, 0.55), d(0.15, 0.35), d(0.3, 0.4)),
		chipSize("0603", d(1.45, 1.75), d(0.65, 0.95), d(0.15, 0.45), d(0.4, 0.5)),
		chipSize("0805", d(1.8, 2.2), d(1.05, 1.45), d(0.25, 0.75), d(0.45, 0.55)),
		chipSize("1206", d(2.9, 3.3), d(1.4, 1.8), d(0.25, 0.75), d(0.5, 0.6)),
	} {
		standard[normal(p.Name)] = p
	}
	for k, v := range map[string]string{"SOT23": "SOT23", "SOT233": "SOT23", "SO8": "SOIC8", "SOP8": "SOIC8", "SO14": "SOIC14", "SO16": "SOIC16",
		"SOT235": "SOT235", "SOT236": "SOT236", "SC746": "SOT236", "SC745": "SOT235", "MSOP8": "MSOP8", "VSSOP8": "MSOP8", "MSOP10": "MSOP10", "VSSOP10": "MSOP10",
		"1005": "0402", "1608": "0603", "2012": "0805", "3216": "1206"} {
		if p, ok := standard[v]; ok {
			standard[k] = p
		}
	}
}

// pinsSuffixRe finds a package name's pin count in brackets ("SOIC (8)").
var pinsSuffixRe = regexp.MustCompile(`\((\d+)\)`)

// normal is a package name with only its letters and digits, upper case.
func normal(name string) string {
	var b strings.Builder
	for _, r := range strings.ToUpper(name) {
		if r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// Standard is the standard package a datasheet's package name stands for,
// if it is one: "SOT-23-6", "SOIC (8)", "8-SOIC", "0603 (1608 Metric)".
func Standard(name string, pins int) (Package, bool) {
	name = strings.TrimSpace(name)
	try := []string{normal(name)}
	if m := pinsSuffixRe.FindStringSubmatch(name); m != nil {
		try = append(try, normal(pinsSuffixRe.ReplaceAllString(name, ""))+m[1])
	}
	if f := strings.Fields(strings.NewReplacer("-", " ", "_", " ").Replace(name)); len(f) == 2 {
		if _, err := strconv.Atoi(f[0]); err == nil {
			try = append(try, normal(f[1])+f[0])
		}
	}
	if f := strings.Fields(name); len(f) > 0 {
		try = append(try, normal(f[0]))
	}
	if pins > 0 {
		try = append(try, normal(name)+strconv.Itoa(pins))
	}
	for _, t := range try {
		if p, ok := standard[t]; ok && (pins == 0 || p.Pins == pins) {
			return p, true
		}
	}
	return Package{}, false
}
