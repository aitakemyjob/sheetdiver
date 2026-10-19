// Command brand draws the SheetDiver logo (the same mark as
// frontend/src/components/Logo.tsx) and writes the app assets:
//
//	build/appicon.png             1024×1024 (Wails derives .icns/.ico from it)
//	frontend/public/logo.png      256×256
//	frontend/public/favicon.png   64×64
//	docs/assets/logo.png          512×512  (README)
//
// It rasterizes with 4×4 supersampling using only the standard library, so
// no image tools are needed.
//
//	go run ./scripts/brand
package main

import (
	"image"
	"image/color"
	"image/png"
	"log"
	"math"
	"os"
	"path/filepath"
)

// pt is a point in the logo's 64×64 design grid.
type pt struct{ x, y float64 }

// shape reports whether a design-grid point is covered.
type shape func(x, y float64) bool

// layer is a shape painted in one color.
type layer struct {
	fill color.NRGBA
	in   shape
}

// hex parses #rrggbb.
func hex(s string) color.NRGBA {
	var r, g, b uint8
	for i, p := range []*uint8{&r, &g, &b} {
		v := 0
		for _, c := range s[1+2*i : 3+2*i] {
			v *= 16
			switch {
			case c >= '0' && c <= '9':
				v += int(c - '0')
			case c >= 'a' && c <= 'f':
				v += int(c-'a') + 10
			}
		}
		*p = uint8(v)
	}
	return color.NRGBA{r, g, b, 255}
}

// roundRect covers a rectangle with rounded corners.
func roundRect(x0, y0, x1, y1, r float64) shape {
	return func(x, y float64) bool {
		cx := math.Max(x0+r, math.Min(x, x1-r))
		cy := math.Max(y0+r, math.Min(y, y1-r))
		return x >= x0 && x <= x1 && y >= y0 && y <= y1 && math.Hypot(x-cx, y-cy) <= r
	}
}

// polygon covers a polygon (even-odd rule).
func polygon(ps ...pt) shape {
	return func(x, y float64) bool {
		in := false
		for i, j := 0, len(ps)-1; i < len(ps); j, i = i, i+1 {
			a, b := ps[i], ps[j]
			if (a.y > y) != (b.y > y) && x < (b.x-a.x)*(y-a.y)/(b.y-a.y)+a.x {
				in = !in
			}
		}
		return in
	}
}

// arc appends points of a quarter arc around c from angle a0 to a1.
func arc(c pt, r, a0, a1 float64) []pt {
	var out []pt
	for i := 0; i <= 8; i++ {
		a := a0 + (a1-a0)*float64(i)/8
		out = append(out, pt{c.x + r*math.Cos(a), c.y + r*math.Sin(a)})
	}
	return out
}

// stroke covers a polyline drawn with round caps and joins.
func stroke(width float64, ps ...pt) shape {
	r := width / 2
	return func(x, y float64) bool {
		for i := 0; i+1 < len(ps); i++ {
			a, b := ps[i], ps[i+1]
			dx, dy := b.x-a.x, b.y-a.y
			t := ((x-a.x)*dx + (y-a.y)*dy) / (dx*dx + dy*dy)
			t = math.Max(0, math.Min(1, t))
			if math.Hypot(x-(a.x+t*dx), y-(a.y+t*dy)) <= r {
				return true
			}
		}
		return false
	}
}

// cubic samples a cubic Bézier segment.
func cubic(p0, c1, c2, p1 pt) []pt {
	var out []pt
	for i := 0; i <= 24; i++ {
		t := float64(i) / 24
		u := 1 - t
		out = append(out, pt{
			u*u*u*p0.x + 3*u*u*t*c1.x + 3*u*t*t*c2.x + t*t*t*p1.x,
			u*u*u*p0.y + 3*u*u*t*c1.y + 3*u*t*t*c2.y + t*t*t*p1.y,
		})
	}
	return out
}

// circle covers a disc.
func circle(c pt, r float64) shape {
	return func(x, y float64) bool { return math.Hypot(x-c.x, y-c.y) <= r }
}

// logo returns the mark's layers, back to front.
func logo() []layer {
	page := []pt{{20, 13}, {37, 13}, {46, 22}}
	page = append(page, arc(pt{43, 51}, 3, 0, math.Pi/2)...)
	page = append(page, arc(pt{20, 51}, 3, math.Pi/2, math.Pi)...)
	page = append(page, arc(pt{20, 16}, 3, math.Pi, 1.5*math.Pi)...)
	var curve []pt
	curve = append(curve, cubic(pt{12, 46}, pt{18, 46}, pt{20, 31}, pt{26, 31})...)
	curve = append(curve, cubic(pt{26, 31}, pt{32, 31}, pt{33, 40}, pt{38, 40})...)
	curve = append(curve, cubic(pt{38, 40}, pt{43, 40}, pt{44, 28}, pt{52, 26})...)
	grey := hex("#9aa9be")
	return []layer{
		{hex("#0e1a33"), roundRect(4, 4, 60, 60, 14)},
		{hex("#e4ecf5"), polygon(page...)},
		{grey, polygon(pt{37, 13}, pt{37, 20}, pt{39, 22}, pt{46, 22})},
		{grey, stroke(2.2, pt{22, 31}, pt{30, 31})},
		{grey, stroke(2.2, pt{22, 36}, pt{34, 36})},
		{grey, stroke(2.2, pt{22, 41}, pt{28, 41})},
		{hex("#4fc3f7"), stroke(4, curve...)},
		{hex("#f5d76e"), circle(pt{52, 26}, 4)},
	}
}

// render draws the logo at size×size with 4×4 supersampling.
func render(size int) *image.NRGBA {
	img := image.NewNRGBA(image.Rect(0, 0, size, size))
	layers := logo()
	const ss = 4
	scale := 64.0 / float64(size)
	for py := 0; py < size; py++ {
		for px := 0; px < size; px++ {
			var r, g, b, a float64
			for sy := 0; sy < ss; sy++ {
				for sx := 0; sx < ss; sx++ {
					x := (float64(px) + (float64(sx)+0.5)/ss) * scale
					y := (float64(py) + (float64(sy)+0.5)/ss) * scale
					var c *color.NRGBA
					for i := range layers {
						if layers[i].in(x, y) {
							c = &layers[i].fill
						}
					}
					if c != nil {
						r += float64(c.R)
						g += float64(c.G)
						b += float64(c.B)
						a++
					}
				}
			}
			if a > 0 {
				img.SetNRGBA(px, py, color.NRGBA{uint8(r / a), uint8(g / a), uint8(b / a), uint8(255 * a / (ss * ss))})
			}
		}
	}
	return img
}

// main writes every branded asset.
func main() {
	for _, out := range []struct {
		path string
		size int
	}{
		{"build/appicon.png", 1024},
		{"frontend/public/logo.png", 256},
		{"frontend/public/favicon.png", 64},
		{"docs/assets/logo.png", 512},
	} {
		if err := save(out.path, render(out.size)); err != nil {
			log.Fatal(err)
		}
		log.Printf("wrote %s (%d×%d)", out.path, out.size, out.size)
	}
}

// save writes a PNG, creating its directory.
func save(path string, img image.Image) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	return png.Encode(f, img)
}
