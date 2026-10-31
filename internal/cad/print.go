package cad

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"math"
	"sort"
	"strings"
)

// Fingerprints say what a symbol or footprint is, to the thousandth of a
// millimetre, whatever order or tool wrote it: two that are the same on
// the board have the same fingerprint. A library format compares the
// fingerprint of what it reads from its file with that of what it last
// wrote, read back the same way, to tell the user's edit from a re-save.

// r3 writes a length to the thousandth of a millimetre.
func r3(v float64) string {
	s := fmt.Sprintf("%.3f", math.Round(v*1000)/1000)
	if s == "-0.000" {
		s = "0.000"
	}
	return s
}

// digest hashes sorted lines.
func digest(lines []string) string {
	sort.Strings(lines)
	h := sha256.Sum256([]byte(strings.Join(lines, "\n")))
	return hex.EncodeToString(h[:12])
}

// lineKey is a line's text, its ends in a fixed order.
func lineKey(layer string, a, b Pt) string {
	p, q := r3(a.X)+","+r3(a.Y), r3(b.X)+","+r3(b.Y)
	if q < p {
		p, q = q, p
	}
	return "line " + layer + " " + p + " " + q
}

// FootprintPrint is a footprint's fingerprint: its pads, its lines by
// layer and its pin 1 marks.
func FootprintPrint(f Footprint) string {
	var lines []string
	for _, p := range f.Pads {
		round := p.Shape == "circle" || p.Shape == "oval"
		lines = append(lines, fmt.Sprintf("pad %s %s %v %s %s %s %s %s %v", p.Number, p.Kind, round, r3(p.X), r3(p.Y), r3(p.W), r3(p.H), r3(p.Drill), p.NoPaste))
	}
	for _, l := range f.Lines {
		lines = append(lines, lineKey(l.Layer, l.A, l.B))
	}
	for _, p := range f.Polys {
		lines = append(lines, polyKey(p.Layer, p.Pts))
	}
	for _, g := range f.Graphics {
		if g.Check() == nil {
			lines = append(lines, graphicKeys(g)...)
		}
	}
	return digest(lines)
}

// SymbolPrint is a symbol's fingerprint: its pins, body and lines, and the
// reference and value it's placed with.
func SymbolPrint(s Symbol) string {
	lines := []string{"body " + r3(s.Body.X0) + " " + r3(s.Body.Y0) + " " + r3(s.Body.X1) + " " + r3(s.Body.Y1), "ref " + s.Reference, "value " + s.Value}
	for _, p := range s.Pins {
		lines = append(lines, fmt.Sprintf("pin %s %s %s %s %s %s %s %v", p.Number, p.Name, p.Type, p.Side, r3(p.X), r3(p.Y), r3(p.Length), p.Hidden))
	}
	for _, l := range s.Extra {
		lines = append(lines, lineKey(LayerBody, l.A, l.B))
	}
	for _, g := range s.Graphics {
		if g.Check() == nil {
			g.Layer = LayerBody
			lines = append(lines, graphicKeys(g)...)
		}
	}
	return digest(lines)
}
