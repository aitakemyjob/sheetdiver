package pdf

import (
	"image"
	"math"
)

// InkDPI is the resolution of ink maps: enough to find a figure's edges
// to about a point, cheap enough to render every figure page.
const InkDPI = 72

// InkMap is a one-bit raster of where a page has visible marks. Object
// bounds in PDFs are often unreliable (form XObjects report their clip
// box, which can run off the page), so figure regions are trimmed to ink.
type InkMap struct {
	W, H  int
	Scale float64 // pixels per point
	Bits  []uint64
}

// newInkMap thresholds a rendered page: anything darker than near-white
// is ink.
func newInkMap(img image.Image, scale float64) *InkMap {
	b := img.Bounds()
	m := &InkMap{W: b.Dx(), H: b.Dy(), Scale: scale}
	m.Bits = make([]uint64, (m.W*m.H+63)/64)
	for y := 0; y < m.H; y++ {
		for x := 0; x < m.W; x++ {
			r, g, bl, a := img.At(b.Min.X+x, b.Min.Y+y).RGBA()
			if a < 0x8000 {
				continue
			}
			lum := (299*r + 587*g + 114*bl) / 1000
			if lum < 0xE800 {
				i := y*m.W + x
				m.Bits[i/64] |= 1 << (i % 64)
			}
		}
	}
	return m
}

// at reports whether pixel (x, y) has ink.
func (m *InkMap) at(x, y int) bool {
	if x < 0 || y < 0 || x >= m.W || y >= m.H {
		return false
	}
	i := y*m.W + x
	return m.Bits[i/64]&(1<<(i%64)) != 0
}

// Trim shrinks r to the smallest box containing all ink inside it. It
// returns r unchanged when the map is nil or r holds no ink.
func (m *InkMap) Trim(r Rect) Rect {
	if m == nil {
		return r
	}
	x0, y0 := int(math.Floor(r.X0*m.Scale)), int(math.Floor(r.Y0*m.Scale))
	x1, y1 := int(math.Ceil(r.X1*m.Scale)), int(math.Ceil(r.Y1*m.Scale))
	minX, minY, maxX, maxY := math.MaxInt, math.MaxInt, -1, -1
	for y := max(0, y0); y < min(m.H, y1); y++ {
		for x := max(0, x0); x < min(m.W, x1); x++ {
			if m.at(x, y) {
				minX, maxX = min(minX, x), max(maxX, x)
				minY, maxY = min(minY, y), max(maxY, y)
			}
		}
	}
	if maxX < 0 {
		return r
	}
	return Rect{float64(minX) / m.Scale, float64(minY) / m.Scale, float64(maxX+1) / m.Scale, float64(maxY+1) / m.Scale}
}
