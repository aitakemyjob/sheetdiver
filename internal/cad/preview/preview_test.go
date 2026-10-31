package preview

import (
	"bytes"
	"image/png"
	"os"
	"path/filepath"
	"testing"

	"github.com/aitakemyjob/sheetdiver/internal/cad"
	"github.com/aitakemyjob/sheetdiver/internal/pdf"
)

// The drawings render through the app's PDF engine to pictures the size
// asked for.
func TestRender(t *testing.T) {
	p, _ := cad.Standard("TSSOP-16", 16)
	p.ExposedPad = &cad.Size{W: 3, H: 3}
	f, _ := cad.MakeFootprint("TSSOP-16-EP", p)
	var pins []cad.PinInfo
	for i, n := range []string{"VIN", "VIN", "EN", "SS", "RT", "SYNC", "PG", "GND", "FB", "COMP", "AGND", "BOOT", "SW", "SW", "PGND", "PGND", "PAD"} {
		pins = append(pins, cad.PinInfo{Number: itoa(i + 1), Name: n})
	}
	s, _ := cad.MakeSymbol("TPS54360-ish", pins, cad.SymbolSpec{})
	ss, _ := cad.MakeSolids(p)
	e := pdf.NewEngine(t.TempDir())
	defer e.Close()
	keep := os.Getenv("SHEETDIVER_KEEP")
	for name, d := range map[string]cad.Drawing{"footprint": cad.DrawFootprint(f), "symbol": cad.DrawSymbol(s), "3d": cad.Draw3D(ss)} {
		out, err := e.Render(PDF(d, name, 500), 0, nil, 110)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		img, err := png.Decode(bytes.NewReader(out))
		if err != nil {
			t.Fatal(err)
		}
		if w := img.Bounds().Dx(); w < 700 || w > 800 {
			t.Errorf("%s is %d px wide", name, w)
		}
		if keep != "" {
			_ = os.WriteFile(filepath.Join(keep, name+".png"), out, 0o644)
		}
	}
}

// itoa writes a small number.
func itoa(i int) string { return string(rune('0'+i/10)) + string(rune('0'+i%10)) }
