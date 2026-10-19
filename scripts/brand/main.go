// Command brand turns the source logo into app assets:
//
//	build/appicon.png             1024×1024 (Wails derives .icns/.ico from it)
//	frontend/public/logo.png      256×256  (title bar, welcome screen)
//	frontend/public/favicon.png   64×64
//	docs/assets/logo.png          512×512  (README)
//
// The source may have a fake "transparency" checkerboard baked in; every
// light, neutral pixel connected to the image border is treated as
// background and made transparent.
//
//	go run ./scripts/brand -src sheetdiver.png
package main

import (
	"flag"
	"image"
	"image/color"
	_ "image/jpeg"
	"image/png"
	"log"
	"os"
	"path/filepath"
)

// main generates every branded asset from the source logo.
func main() {
	src := flag.String("src", "sheetdiver.png", "source logo (square PNG/JPEG)")
	flag.Parse()
	img, err := load(*src)
	if err != nil {
		log.Fatal(err)
	}
	cut := removeBackground(img)
	for _, out := range []struct {
		path string
		size int
	}{
		{"build/appicon.png", 1024},
		{"frontend/public/logo.png", 256},
		{"frontend/public/favicon.png", 64},
		{"docs/assets/logo.png", 512},
	} {
		if err := save(out.path, resize(cut, out.size)); err != nil {
			log.Fatal(err)
		}
		log.Printf("wrote %s (%d×%d)", out.path, out.size, out.size)
	}
}

// load decodes an image into NRGBA (non-premultiplied) form.
func load(path string) (*image.NRGBA, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	img, _, err := image.Decode(f)
	if err != nil {
		return nil, err
	}
	b := img.Bounds()
	out := image.NewNRGBA(image.Rect(0, 0, b.Dx(), b.Dy()))
	for y := 0; y < b.Dy(); y++ {
		for x := 0; x < b.Dx(); x++ {
			out.Set(x, y, img.At(b.Min.X+x, b.Min.Y+y))
		}
	}
	return out, nil
}

// isBackground matches light, unsaturated pixels (checkerboard greys/white).
func isBackground(c color.NRGBA) bool {
	if c.A < 16 {
		return true
	}
	lo := min(c.R, c.G, c.B)
	hi := max(c.R, c.G, c.B)
	return lo > 165 && hi-lo < 28
}

// removeBackground flood-fills background from the border and clears it.
func removeBackground(img *image.NRGBA) *image.NRGBA {
	w, h := img.Bounds().Dx(), img.Bounds().Dy()
	out := image.NewNRGBA(img.Bounds())
	copy(out.Pix, img.Pix)
	seen := make([]bool, w*h)
	stack := make([]int, 0, 4*w)
	push := func(x, y int) {
		if x < 0 || y < 0 || x >= w || y >= h || seen[y*w+x] {
			return
		}
		if !isBackground(img.NRGBAAt(x, y)) {
			return
		}
		seen[y*w+x] = true
		stack = append(stack, y*w+x)
	}
	for x := 0; x < w; x++ {
		push(x, 0)
		push(x, h-1)
	}
	for y := 0; y < h; y++ {
		push(0, y)
		push(w-1, y)
	}
	for len(stack) > 0 {
		i := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		x, y := i%w, i/w
		out.SetNRGBA(x, y, color.NRGBA{})
		push(x+1, y)
		push(x-1, y)
		push(x, y+1)
		push(x, y-1)
	}
	// Feather the cut edge: kept pixels touching background get partial alpha.
	for y := 1; y < h-1; y++ {
		for x := 1; x < w-1; x++ {
			if seen[y*w+x] {
				continue
			}
			n := 0
			for _, d := range [][2]int{{1, 0}, {-1, 0}, {0, 1}, {0, -1}} {
				if seen[(y+d[1])*w+x+d[0]] {
					n++
				}
			}
			if n > 0 {
				c := out.NRGBAAt(x, y)
				c.A = uint8(int(c.A) * (4 - n) / 4)
				out.SetNRGBA(x, y, c)
			}
		}
	}
	return out
}

// resize does area-averaged downscaling with premultiplied alpha, which
// avoids dark fringes around transparent edges.
func resize(src *image.NRGBA, size int) *image.NRGBA {
	sw, sh := src.Bounds().Dx(), src.Bounds().Dy()
	dst := image.NewNRGBA(image.Rect(0, 0, size, size))
	for y := 0; y < size; y++ {
		y0, y1 := y*sh/size, max((y+1)*sh/size, y*sh/size+1)
		for x := 0; x < size; x++ {
			x0, x1 := x*sw/size, max((x+1)*sw/size, x*sw/size+1)
			var r, g, b, a, n float64
			for sy := y0; sy < y1; sy++ {
				for sx := x0; sx < x1; sx++ {
					c := src.NRGBAAt(sx, sy)
					al := float64(c.A) / 255
					r += float64(c.R) * al
					g += float64(c.G) * al
					b += float64(c.B) * al
					a += al
					n++
				}
			}
			if a == 0 {
				continue
			}
			dst.SetNRGBA(x, y, color.NRGBA{R: uint8(r / a), G: uint8(g / a), B: uint8(b / a), A: uint8(a / n * 255)})
		}
	}
	return dst
}

// save writes img as a maximally compressed PNG, creating directories.
func save(path string, img image.Image) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	return (&png.Encoder{CompressionLevel: png.BestCompression}).Encode(f, img)
}
