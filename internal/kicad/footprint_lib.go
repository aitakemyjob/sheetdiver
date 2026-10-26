package kicad

import (
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"
)

// Library footprints, read from the KiCad libraries installed on this
// computer: the board embeds each one as KiCad does, and placement needs
// where its pads are and the area its courtyard keeps clear.

// libPad is a footprint's pad: its number and where it is on the
// footprint (millimetres, Y down).
type libPad struct {
	number string
	x, y   float64
}

// libFootprint is a footprint as read from its library.
type libFootprint struct {
	libID string
	tree  *node
	pads  []libPad
	// court is the courtyard: the area kept clear around the footprint.
	court Rect
}

// FootprintSource loads library footprints by ID ("Package_SO:SOIC-8_…").
type FootprintSource interface {
	Load(libID string) (*libFootprint, error)
}

// Load reads a footprint from the installed libraries.
func (d *DirIndex) Load(libID string) (*libFootprint, error) {
	if d == nil {
		return nil, fmt.Errorf("KiCad's footprints aren't installed")
	}
	lib, name, ok := strings.Cut(libID, ":")
	if !ok {
		return nil, fmt.Errorf("%q isn't a footprint ID", libID)
	}
	b, err := os.ReadFile(filepath.Join(d.Root, lib+".pretty", name+".kicad_mod"))
	if err != nil {
		return nil, fmt.Errorf("footprint %s isn't installed", libID)
	}
	return readFootprint(libID, string(b))
}

// readFootprint reads a .kicad_mod file's pads and courtyard.
func readFootprint(libID, text string) (*libFootprint, error) {
	tree, err := parse(text)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", libID, err)
	}
	if h := tree.head(); h != "footprint" && h != "module" {
		return nil, fmt.Errorf("%s isn't a footprint", libID)
	}
	fp := &libFootprint{libID: libID, tree: tree}
	court := Rect{math.Inf(1), math.Inf(1), math.Inf(-1), math.Inf(-1)}
	pads := court
	for _, it := range tree.items {
		c, ok := it.(*node)
		if !ok {
			continue
		}
		switch c.head() {
		case "pad":
			p := libPad{number: c.text(1)}
			if a := c.child("at"); a != nil {
				p.x, p.y = a.float(1), a.float(2)
			}
			w, h := 0.0, 0.0
			if s := c.child("size"); s != nil {
				w, h = s.float(1), s.float(2)
			}
			pads = grow(grow(pads, p.x-w/2, p.y-h/2), p.x+w/2, p.y+h/2)
			fp.pads = append(fp.pads, p)
		case "fp_line", "fp_rect", "fp_circle", "fp_poly", "fp_arc":
			if l := c.child("layer"); l == nil || l.text(1) != "F.CrtYd" {
				continue
			}
			for _, key := range []string{"start", "end", "mid"} {
				if pt := c.child(key); pt != nil {
					court = grow(court, pt.float(1), pt.float(2))
				}
			}
			if c.head() == "fp_circle" {
				if ctr, end := c.child("center"), c.child("end"); ctr != nil && end != nil {
					r := math.Hypot(end.float(1)-ctr.float(1), end.float(2)-ctr.float(2))
					court = grow(grow(court, ctr.float(1)-r, ctr.float(2)-r), ctr.float(1)+r, ctr.float(2)+r)
				}
			}
			if pts := c.child("pts"); pts != nil {
				for _, p := range pts.children("xy") {
					court = grow(court, p.float(1), p.float(2))
				}
			}
		}
	}
	switch {
	case !math.IsInf(court.X0, 1):
		fp.court = court
	case !math.IsInf(pads.X0, 1):
		fp.court = Rect{pads.X0 - 0.25, pads.Y0 - 0.25, pads.X1 + 0.25, pads.Y1 + 0.25}
	default:
		fp.court = Rect{-1, -1, 1, 1}
	}
	return fp, nil
}

// pad is the footprint's first pad with a number.
func (f *libFootprint) pad(number string) (libPad, bool) {
	for _, p := range f.pads {
		if p.number == number {
			return p, true
		}
	}
	return libPad{}, false
}
