package kicad

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/aitakemyjob/sheetdiver/internal/cad"
)

// allGraphics are one graphic of each kind on a layer: arcs both ways
// round, a rotated text, filled and open shapes.
func allGraphics(layer string) []cad.Graphic {
	return []cad.Graphic{
		{Kind: cad.GraphicLine, Layer: layer, Pts: []cad.Pt{{X: -2, Y: -2}, {X: 2, Y: -2}}, Width: 0.12},
		{Kind: cad.GraphicRect, Layer: layer, Pts: []cad.Pt{{X: -3, Y: -1}, {X: -2, Y: 1}}, Width: 0.1},
		{Kind: cad.GraphicRect, Layer: layer, Pts: []cad.Pt{{X: 2, Y: -1}, {X: 3, Y: 1}}, Fill: true},
		{Kind: cad.GraphicCircle, Layer: layer, Pts: []cad.Pt{{X: 0, Y: 2}}, R: 0.5, Width: 0.1},
		withLayer(cad.ArcFrom(cad.Pt{X: 1, Y: 1}, 0.8, 30, 200), layer),
		{Kind: cad.GraphicArc, Layer: layer, Pts: []cad.Pt{{X: -1, Y: 3}, {X: -0.5, Y: 2.5}, {X: 0, Y: 3}}, Width: 0.1},
		{Kind: cad.GraphicPoly, Layer: layer, Pts: []cad.Pt{{X: 0, Y: 0}, {X: 1, Y: 0}, {X: 0.5, Y: 0.8}}, Width: 0.1},
		{Kind: cad.GraphicPoly, Layer: layer, Pts: []cad.Pt{{X: 0, Y: -1}, {X: 1, Y: -1}, {X: 0.5, Y: -0.2}}, Fill: true},
		{Kind: cad.GraphicText, Layer: layer, Pts: []cad.Pt{{X: -1, Y: -3}}, Text: "EN", Size: 1, Angle: 90},
	}
}

// withLayer is a graphic on a layer, drawn 0.1 mm wide.
func withLayer(g cad.Graphic, layer string) cad.Graphic {
	g.Layer, g.Width = layer, 0.1
	return g
}

func TestGraphicsRoundTrip(t *testing.T) {
	gs := allGraphics(cad.LayerSilk)
	gs[4].Layer, gs[4].Width = cad.LayerFab, 0.1
	gs = append(gs, cad.Graphic{Kind: cad.GraphicLine, Layer: cad.LayerCopper, Pts: []cad.Pt{{X: 0, Y: 0}, {X: 0, Y: 1}}, Width: 0.3})
	f := cad.Footprint{Name: "G", Pads: []cad.Pad{{Number: "1", Kind: cad.PadSMD, Shape: "rect", W: 1, H: 1}}, Graphics: gs}
	nd, err := Parse(FootprintNode(f, "").String())
	if err != nil {
		t.Fatal(err)
	}
	back := ReadFootprint(nd)
	if cad.FootprintPrint(back) != cad.FootprintPrint(f) {
		t.Errorf("footprint graphics changed on the way back:\n%+v", back.Graphics)
	}
	kinds := map[string]int{}
	for _, g := range back.Graphics {
		kinds[g.Kind]++
	}
	if kinds[cad.GraphicArc] != 2 || kinds[cad.GraphicCircle] != 1 || kinds[cad.GraphicText] != 1 || kinds[cad.GraphicRect] != 2 {
		t.Errorf("kinds read back %v", kinds)
	}

	sg := allGraphics(cad.LayerBody)
	s := cad.Symbol{Name: "G", Reference: "U", Value: "G", Body: cad.Rect{X0: -5, Y0: -5, X1: 5, Y1: 5}, Graphics: sg,
		Pins: []cad.SymbolPin{{Number: "1", Name: "A", Type: cad.TypeInput, Side: cad.Left, X: -7.62, Y: 0, Length: 2.54}}}
	lib, err := SymbolLibrary([]cad.Symbol{s}, nil)
	if err != nil {
		t.Fatal(err)
	}
	syms, err := Symbols(string(lib))
	if err != nil {
		t.Fatal(err)
	}
	sb := ReadSymbol(syms["G"])
	if cad.SymbolPrint(sb) != cad.SymbolPrint(s) {
		t.Errorf("symbol graphics changed on the way back:\n%+v\n%+v", sb.Graphics, sb.Extra)
	}
	// A text moved in KiCad is an edit.
	moved, _ := Parse(strings.Replace(syms["G"].String(), `(at -1 -3 900)`, `(at -1 -2 900)`, 1))
	if Fingerprint(moved) == Fingerprint(syms["G"]) {
		t.Error("a moved text kept the fingerprint")
	}

	// KiCad loads them: pcbnew reads the footprint's shapes and texts.
	py, err := exec.LookPath("python3")
	if err != nil || exec.Command(py, "-c", "import pcbnew").Run() != nil {
		t.Skip("pcbnew isn't installed")
	}
	dir := t.TempDir()
	pretty := filepath.Join(dir, "G.pretty")
	_ = os.MkdirAll(pretty, 0o755)
	if err := os.WriteFile(filepath.Join(pretty, "G.kicad_mod"), []byte(FootprintNode(f, "").String()), 0o644); err != nil {
		t.Fatal(err)
	}
	out, err := exec.Command(py, "-c", `import pcbnew,sys
fp = pcbnew.FootprintLoad(sys.argv[1], "G")
kinds = {}
for g in fp.GraphicalItems():
    k = g.GetClass() + (":" + g.ShowShape() if hasattr(g, "ShowShape") else "")
    kinds[k] = kinds.get(k, 0) + 1
print(sorted(kinds.items()))`, pretty).CombinedOutput()
	if err != nil {
		t.Fatalf("pcbnew: %v\n%s", err, out)
	}
	for _, want := range []string{"Arc", "Circle", "Rect", "Poly", "TEXT"} {
		if !strings.Contains(string(out), want) {
			t.Errorf("KiCad didn't load a %s: %s", want, out)
		}
	}
}
