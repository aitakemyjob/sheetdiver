package kicad

import "testing"

func TestCustomPadSize(t *testing.T) {
	// Espressif's chamfered ground pad: a 0.8 mm anchor and the pad's
	// outline drawn on it.
	nd, err := Parse(`(footprint "X" (pad "49" smd custom (at -1.975 0.725) (size 0.8 0.8) (layers "F.Cu" "F.Paste" "F.Mask")
		(options (clearance outline) (anchor rect))
		(primitives (gr_poly (pts (xy 0.725 0.725) (xy -0.725 0.725) (xy -0.725 -0.3) (xy -0.3 -0.725) (xy 0.725 -0.725)) (width 0) (fill yes)))))`)
	if err != nil {
		t.Fatal(err)
	}
	p := ReadFootprint(nd).Pads[0]
	if p.W != 1.45 || p.H != 1.45 || p.X != -1.975 {
		t.Errorf("pad %+v", p)
	}
}
