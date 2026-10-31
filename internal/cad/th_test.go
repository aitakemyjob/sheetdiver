package cad

import (
	"strings"
	"testing"
)

// pad finds a footprint's pad by number and kind.
func pad(f Footprint, n, kind string) Pad {
	for _, p := range f.Pads {
		if p.Number == n && p.Kind == kind {
			return p
		}
	}
	return Pad{}
}

func TestThroughHoleAgreesWithKiCad(t *testing.T) {
	// KiCad's own footprints for the same outlines: pad 1's size and hole,
	// and where pad 2 is from pad 1.
	for _, c := range []struct {
		name         string
		w, h, drill  float64
		dx, dy       float64
		shape1, kind string
	}{
		{"DIP-8", 1.6, 1.6, 0.8, 0, 2.54, "rect", MountThru},
		{"TO-92", 1.05, 1.5, 0.75, 1.27, 0, "rect", MountThru},
		{"TO-220-3", 1.905, 2, 1.1, 2.54, 0, "rect", MountThru},
		{"PinHeader_2x05_P2.54mm_Vertical", 1.7, 1.7, 1.0, 2.54, 0, "rect", MountThru},
	} {
		p, ok := Standard(c.name, 0)
		if !ok {
			t.Fatalf("%s isn't standard", c.name)
		}
		f, ps := MakeFootprint(c.name, p)
		if err := Err(ps); err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		for _, x := range ps {
			t.Errorf("%s: %s", c.name, x.Text)
		}
		p1, p2 := pad(f, "1", PadThru), pad(f, "2", PadThru)
		if !near(p1.W, c.w, 0.2) || !near(p1.H, c.h, 0.2) || !near(p1.Drill, c.drill, 0.12) || p1.Shape != c.shape1 {
			t.Errorf("%s pad 1 %.2f × %.2f drill %.2f %s; KiCad's %.2f × %.2f drill %.2f", c.name, p1.W, p1.H, p1.Drill, p1.Shape, c.w, c.h, c.drill)
		}
		if !near(p2.X-p1.X, c.dx, 1e-6) || !near(p2.Y-p1.Y, c.dy, 1e-6) {
			t.Errorf("%s pad 2 at (%.3f, %.3f) from pad 1", c.name, p2.X-p1.X, p2.Y-p1.Y)
		}
		if f.Mount != c.kind {
			t.Errorf("%s mounts %s", c.name, f.Mount)
		}
	}
	// A DIP's rows: pin 4 faces pin 5, 7.62 mm apart.
	p, _ := Standard("PDIP (8)", 8)
	f, _ := MakeFootprint("DIP-8", p)
	if a, b := pad(f, "4", PadThru), pad(f, "5", PadThru); a.Y != b.Y || !near(b.X-a.X, 7.62, 1e-6) {
		t.Errorf("DIP rows %+v %+v", a, b)
	}
}

func TestRadialAxialAndCustom(t *testing.T) {
	// A 6.3 mm electrolytic on a 2.5 mm pitch: circles for its outline.
	cap := Package{Family: "radial", Pins: 2, Pitch: 2.5, BodyWidth: Dim{Nom: 6.3}, Height: Dim{Nom: 11}, LeadWidth: Dim{Nom: 0.5}}
	f, ps := MakeFootprint("CP_D6.3", cap)
	if err := Err(ps); err != nil {
		t.Fatal(err)
	}
	silk := 0
	for _, l := range f.Lines {
		if l.Layer == LayerSilk {
			silk++
		}
	}
	if silk != 48 || f.Courtyard.X1 < 3.4 {
		t.Errorf("radial outline: %d silk lines, courtyard %+v", silk, f.Courtyard)
	}
	// An axial resistor whose pitch doesn't clear its body is refused.
	res := Package{Family: "axial", Pins: 2, Pitch: 5.08, BodyLength: Dim{Nom: 6.3}, BodyWidth: Dim{Nom: 2.5}, LeadWidth: Dim{Nom: 0.6}}
	if _, ps := MakeFootprint("R", res); Err(ps) == nil {
		t.Error("a pitch shorter than the body")
	}
	res.Pitch = 10.16
	if _, ps := MakeFootprint("R", res); Err(ps) != nil {
		t.Error(Err(ps))
	}
	ss, _ := MakeSolids(res)
	lo, _ := Bounds(ss)
	if lo[2] != -3 {
		t.Errorf("the leads go %v below the board", lo[2])
	}
	// A USB-C receptacle: 12 signal pads, 4 shield tabs that are one pin,
	// two locating pegs.
	usb := Package{Family: "custom", Name: "USB-C 16P", BodyWidth: Dim{Nom: 8.94}, BodyLength: Dim{Nom: 7.3}, Height: Dim{Nom: 3.2}}
	for i, n := range []string{"A1", "A4", "B8", "A5", "B7", "A6", "A7", "B6", "A8", "B5", "B4", "A12"} {
		x := -2.75 + float64(i)*0.5
		if i >= 2 && i <= 9 {
			x = -1.75 + float64(i-2)*0.5
		}
		usb.Pads = append(usb.Pads, PadSpec{Number: n, X: x, Y: -3.9, W: 0.3, H: 1.15})
	}
	for _, x := range []float64{-4.32, 4.32} {
		usb.Pads = append(usb.Pads, PadSpec{Number: "S1", X: x, Y: -3.4, W: 1, H: 2.1, Shape: "oval", Drill: 0.6}, PadSpec{Number: "S1", X: x, Y: 0.78, W: 1, H: 1.6, Shape: "oval", Drill: 0.6})
	}
	usb.Pads = append(usb.Pads, PadSpec{X: -2.89, Y: -2.86, Drill: 0.65, Kind: "npth"}, PadSpec{X: 2.89, Y: -2.86, Drill: 0.65, Kind: "npth"})
	f, ps = MakeFootprint("USB_C", usb)
	if err := Err(ps); err != nil {
		t.Fatalf("%v %v", err, ps)
	}
	holes, shield := 0, 0
	for _, p := range f.Pads {
		switch {
		case p.Kind == PadNPTH:
			holes++
		case p.Number == "S1":
			shield++
		}
	}
	if holes != 2 || shield != 4 || f.Mount != MountThru || f.From != FromDatasheet {
		t.Errorf("USB-C: %d holes, %d shield pads, %s, %s", holes, shield, f.Mount, f.From)
	}
	if _, ps := MakeFootprint("x", Package{Family: "custom", Pads: []PadSpec{{Number: "1", X: 0, Y: 0, W: 1, Drill: 1.2}}}); Err(ps) == nil || !strings.Contains(Err(ps).Error(), "as big as the pad") {
		t.Errorf("a hole bigger than its pad: %v", Err(ps))
	}
}
