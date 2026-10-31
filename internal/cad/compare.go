package cad

import (
	"fmt"
	"math"
	"slices"
	"sort"
	"strings"
)

// Checking a footprint or symbol against a reference: a maker's own
// library part, or KiCad's. Footprints are compared pad by pad once lined
// up (the two may put their origins, and turn the part, differently);
// symbols pin by pin, by number and name.

// PadDelta is how a pad differs from the reference's.
type PadDelta struct {
	Number string  `json:"number"`
	Ours   Pad     `json:"ours"`
	Ref    Pad     `json:"ref"`
	Dist   float64 `json:"dist"` // centre to centre, mm
}

// FootprintDiff is how a footprint differs from a reference.
type FootprintDiff struct {
	// Turn is how far (degrees, counter-clockwise) the reference was
	// turned, and Offset how far moved, to lie on ours.
	Turn    int      `json:"turn"`
	Offset  Pt       `json:"offset"`
	Missing []string `json:"missing,omitempty"` // the reference's pads we don't have
	Extra   []string `json:"extra,omitempty"`   // ours it doesn't have
	// Moved are pads further than the tolerance from the reference's;
	// Resized ones a different size by more than it.
	Moved   []PadDelta `json:"moved,omitempty"`
	Resized []PadDelta `json:"resized,omitempty"`
	// Worst is the largest distance between matched pads.
	Worst float64 `json:"worst"`
}

// OK reports whether the footprints agree.
func (d FootprintDiff) OK() bool {
	return len(d.Missing)+len(d.Extra)+len(d.Moved)+len(d.Resized) == 0
}

// String says how they differ, one line each.
func (d FootprintDiff) String() string {
	var b strings.Builder
	fmt.Fprintf(&b, "lined up turned %d°, moved (%.3f, %.3f); worst pad %.3f mm off\n", d.Turn, d.Offset.X, d.Offset.Y, d.Worst)
	if len(d.Missing) > 0 {
		fmt.Fprintf(&b, "missing pads: %s\n", strings.Join(d.Missing, ", "))
	}
	if len(d.Extra) > 0 {
		fmt.Fprintf(&b, "extra pads: %s\n", strings.Join(d.Extra, ", "))
	}
	for _, m := range d.Moved {
		fmt.Fprintf(&b, "pad %s at (%.3f, %.3f), the reference's at (%.3f, %.3f): %.3f mm off\n", m.Number, m.Ours.X, m.Ours.Y, m.Ref.X, m.Ref.Y, m.Dist)
	}
	for _, m := range d.Resized {
		fmt.Fprintf(&b, "pad %s is %.3f × %.3f, the reference's %.3f × %.3f\n", m.Number, m.Ours.W, m.Ours.H, m.Ref.W, m.Ref.H)
	}
	return b.String()
}

// copperPads are a footprint's pads with copper (not paste windows or
// bare holes), by number; a pad stacked on another just like it (a
// library's slip) counts once.
func copperPads(f Footprint) map[string][]Pad {
	out := map[string][]Pad{}
	for _, p := range f.Pads {
		if p.Kind == PadPaste || p.Kind == PadNPTH {
			continue
		}
		same := func(q Pad) bool {
			return math.Abs(q.X-p.X) < 1e-3 && math.Abs(q.Y-p.Y) < 1e-3 && math.Abs(q.W-p.W) < 1e-3 && math.Abs(q.H-p.H) < 1e-3
		}
		if slices.ContainsFunc(out[p.Number], same) {
			continue
		}
		out[p.Number] = append(out[p.Number], p)
	}
	return out
}

// turned is a pad turned about the origin by quarter turns (counter-
// clockwise as drawn, y down).
func turned(p Pad, quarters int) Pad {
	for i := 0; i < quarters; i++ {
		p.X, p.Y = p.Y, -p.X
		p.W, p.H = p.H, p.W
	}
	return p
}

// pairs matches each of our pads of a number to the nearest of the
// reference's, moved by off.
func pairs(ours, ref []Pad, off Pt) ([][2]Pad, []Pad, []Pad) {
	used := make([]bool, len(ref))
	var out [][2]Pad
	var extra []Pad
	for _, o := range ours {
		best, bd := -1, math.Inf(1)
		for j, r := range ref {
			if d := math.Hypot(o.X-(r.X+off.X), o.Y-(r.Y+off.Y)); !used[j] && d < bd {
				best, bd = j, d
			}
		}
		if best < 0 {
			extra = append(extra, o)
			continue
		}
		used[best] = true
		r := ref[best]
		r.X, r.Y = r.X+off.X, r.Y+off.Y
		out = append(out, [2]Pad{o, r})
	}
	var missing []Pad
	for j, r := range ref {
		if !used[j] {
			missing = append(missing, r)
		}
	}
	return out, missing, extra
}

// CompareFootprints checks a footprint against a reference, pads within
// tol mm of each other (and of each other's size) agreeing.
func CompareFootprints(ours, ref Footprint, tol float64) FootprintDiff {
	op, rp := copperPads(ours), copperPads(ref)
	var best FootprintDiff
	bestErr := math.Inf(1)
	for q := 0; q < 4; q++ {
		turnedRef := map[string][]Pad{}
		for n, ps := range rp {
			for _, p := range ps {
				turnedRef[n] = append(turnedRef[n], turned(p, q))
			}
		}
		// The offset: the mean of the pads' centres, number by number.
		var dx, dy float64
		n := 0
		for num, ps := range op {
			rs, ok := turnedRef[num]
			if !ok {
				continue
			}
			ox, oy := centre(ps)
			rx, ry := centre(rs)
			dx, dy, n = dx+ox-rx, dy+oy-ry, n+1
		}
		if n == 0 {
			continue
		}
		off := Pt{Number(dx / float64(n)), Number(dy / float64(n))}
		d := FootprintDiff{Turn: q * 90, Offset: off}
		// The fit: the pads left unmatched, then how far apart the rest are.
		score := 0.0
		for num, ps := range op {
			rs, ok := turnedRef[num]
			if !ok {
				d.Extra = append(d.Extra, num)
				continue
			}
			matched, missing, extra := pairs(ps, rs, off)
			score += 1e3 * float64(len(missing)+len(extra))
			if len(missing) > 0 {
				d.Missing = append(d.Missing, fmt.Sprintf("%s (%d more)", num, len(missing)))
			}
			if len(extra) > 0 {
				d.Extra = append(d.Extra, fmt.Sprintf("%s (%d more)", num, len(extra)))
			}
			for _, m := range matched {
				dist := Number(math.Hypot(m[0].X-m[1].X, m[0].Y-m[1].Y))
				d.Worst = math.Max(d.Worst, dist)
				score += dist
				delta := PadDelta{Number: num, Ours: m[0], Ref: m[1], Dist: dist}
				if dist > tol {
					d.Moved = append(d.Moved, delta)
				}
				if math.Abs(m[0].W-m[1].W) > tol || math.Abs(m[0].H-m[1].H) > tol {
					d.Resized = append(d.Resized, delta)
				}
			}
		}
		for num := range turnedRef {
			if _, ok := op[num]; !ok {
				d.Missing = append(d.Missing, num)
				score += 1e3
			}
		}
		if score < bestErr {
			best, bestErr = d, score
		}
	}
	for _, s := range [][]string{best.Missing, best.Extra} {
		sort.Slice(s, func(i, j int) bool { return padOrder(s[i], s[j]) })
	}
	for _, s := range [][]PadDelta{best.Moved, best.Resized} {
		sort.Slice(s, func(i, j int) bool { return padOrder(s[i].Number, s[j].Number) })
	}
	return best
}

// centre is the middle of pads.
func centre(ps []Pad) (float64, float64) {
	var x, y float64
	for _, p := range ps {
		x, y = x+p.X, y+p.Y
	}
	return x / float64(len(ps)), y / float64(len(ps))
}

// padOrder orders pad numbers as people count them: 2 before 10.
func padOrder(a, b string) bool {
	var na, nb int
	_, ea := fmt.Sscan(a, &na)
	_, eb := fmt.Sscan(b, &nb)
	if ea == nil && eb == nil && na != nb {
		return na < nb
	}
	return a < b
}

// SymbolDiff is how a symbol's pins differ from a reference's.
type SymbolDiff struct {
	Missing []string `json:"missing,omitempty"` // the reference's pin numbers we don't have
	Extra   []string `json:"extra,omitempty"`   // ours it doesn't have
	// Renamed are pins of a number named otherwise: "12 IO0 / GPIO0".
	Renamed []string `json:"renamed,omitempty"`
}

// OK reports whether the symbols' pins agree.
func (d SymbolDiff) OK() bool { return len(d.Missing)+len(d.Extra)+len(d.Renamed) == 0 }

// String says how they differ.
func (d SymbolDiff) String() string {
	var b strings.Builder
	if len(d.Missing) > 0 {
		fmt.Fprintf(&b, "missing pins: %s\n", strings.Join(d.Missing, ", "))
	}
	if len(d.Extra) > 0 {
		fmt.Fprintf(&b, "extra pins: %s\n", strings.Join(d.Extra, ", "))
	}
	for _, r := range d.Renamed {
		fmt.Fprintf(&b, "pin %s\n", r)
	}
	return b.String()
}

// CompareSymbols checks a symbol's pins against a reference's: the same
// numbers, each named alike. Names are alike when they share a part,
// case and punctuation aside, one library's "PC14-OSC32_IN" being
// another's "PC14", "IO4" "GPIO4/TOUCH4/ADC1_CH3"; aliases are names of
// ours taken as the reference's ("RXD0" for "U0RXD").
func CompareSymbols(ours, ref Symbol, aliases map[string]string) SymbolDiff {
	names := func(s Symbol) map[string]string {
		out := map[string]string{}
		for _, p := range s.Pins {
			out[p.Number] = p.Name
		}
		return out
	}
	on, rn := names(ours), names(ref)
	var d SymbolDiff
	for num, name := range on {
		r, ok := rn[num]
		switch {
		case !ok:
			d.Extra = append(d.Extra, num)
		case !namesAlike(name, r) && !namesAlike(aliases[name], r):
			d.Renamed = append(d.Renamed, fmt.Sprintf("%s %s / %s", num, name, r))
		}
	}
	for num := range rn {
		if _, ok := on[num]; !ok {
			d.Missing = append(d.Missing, num)
		}
	}
	sort.Slice(d.Missing, func(i, j int) bool { return padOrder(d.Missing[i], d.Missing[j]) })
	sort.Slice(d.Extra, func(i, j int) bool { return padOrder(d.Extra[i], d.Extra[j]) })
	sort.Slice(d.Renamed, func(i, j int) bool { return padOrder(d.Renamed[i], d.Renamed[j]) })
	return d
}

// namesAlike reports whether two pin names share a part (split at "/"
// and "-"), letters and digits only, a GPIO and its IO being one.
func namesAlike(a, b string) bool {
	parts := func(s string) map[string]bool {
		out := map[string]bool{}
		for _, f := range strings.FieldsFunc(s, func(r rune) bool { return r == '/' || r == '-' || r == ' ' }) {
			var k strings.Builder
			for _, r := range strings.ToUpper(f) {
				if r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' {
					k.WriteRune(r)
				}
			}
			key := k.String()
			if key == "" {
				continue
			}
			out[key] = true
			if rest, ok := strings.CutPrefix(key, "GPIO"); ok {
				out["IO"+rest] = true
			}
		}
		return out
	}
	pa := parts(a)
	for k := range parts(b) {
		if pa[k] {
			return true
		}
	}
	return false
}
