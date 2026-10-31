package eagle

import (
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/aitakemyjob/sheetdiver/internal/cad"
)

// drawn are one of each graphic kind on a layer: arcs in each quadrant,
// either way round and past a half turn, and texts turned every way.
func drawn(layer string) []cad.Graphic {
	arc := func(o cad.Pt, r, a, b float64, rev bool) cad.Graphic {
		g := cad.ArcFrom(o, r, a, b)
		if rev {
			g.Pts[0], g.Pts[2] = g.Pts[2], g.Pts[0]
		}
		g.Width = 0.12
		return g
	}
	gs := []cad.Graphic{
		{Kind: cad.GraphicLine, Pts: []cad.Pt{{X: -3, Y: -3}, {X: 3, Y: -3.5}}, Width: 0.12},
		{Kind: cad.GraphicRect, Pts: []cad.Pt{{X: -4, Y: -4}, {X: 4, Y: 5}}, Width: 0.1},
		{Kind: cad.GraphicRect, Pts: []cad.Pt{{X: 1, Y: 1}, {X: 0.5, Y: 2}}, Fill: true},
		{Kind: cad.GraphicCircle, Pts: []cad.Pt{{X: 2, Y: 2}}, R: 0.5, Width: 0.1},
		{Kind: cad.GraphicCircle, Pts: []cad.Pt{{X: -2, Y: 2}}, R: 0.25, Fill: true},
		arc(cad.Pt{X: 0, Y: 0}, 1, 0, 90, false),
		arc(cad.Pt{X: 0, Y: 0}, 1.5, 90, 180, true),
		arc(cad.Pt{X: 1, Y: -1}, 2, 180, 270, false),
		arc(cad.Pt{X: -1, Y: 1}, 0.75, 270, 360, true),
		arc(cad.Pt{X: 0.5, Y: 0.5}, 2.5, 30, 300, false),
		{Kind: cad.GraphicPoly, Pts: []cad.Pt{{X: 0, Y: 3}, {X: 1, Y: 3}, {X: 0.5, Y: 4}}, Width: 0.1},
		{Kind: cad.GraphicPoly, Pts: []cad.Pt{{X: -1, Y: 3}, {X: -2, Y: 3}, {X: -1.5, Y: 4}, {X: -1, Y: 3.5}}, Fill: true},
		{Kind: cad.GraphicText, Pts: []cad.Pt{{X: -2, Y: -2}}, Text: "A&B", Size: 1},
		{Kind: cad.GraphicText, Pts: []cad.Pt{{X: 2, Y: -2}}, Text: "UP", Size: 0.8, Angle: 90},
		{Kind: cad.GraphicText, Pts: []cad.Pt{{X: 2, Y: -1}}, Text: "DOWN", Size: 0.8, Angle: 180},
		{Kind: cad.GraphicText, Pts: []cad.Pt{{X: 2, Y: 0}}, Text: "SLANT", Size: 0.8, Angle: 30},
		{Kind: cad.GraphicText, Pts: []cad.Pt{{X: 2, Y: 1}}, Text: "BACK", Size: 0.8, Angle: 250},
	}
	for i := range gs {
		gs[i].Layer = layer
	}
	return gs
}

// kinds counts graphics by kind.
func kinds(gs []cad.Graphic) map[string]int {
	out := map[string]int{}
	for _, g := range gs {
		out[g.Kind]++
	}
	return out
}

// sameCounts says whether two counts are the same.
func sameCounts(a, b map[string]int) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		if b[k] != v {
			return false
		}
	}
	return true
}

// graphicPart is the test part with graphics on every footprint layer and
// on the symbol, and no courtyard lines (Eagle's file leaves them out).
func graphicPart(t *testing.T) Part {
	t.Helper()
	p := testPart(t)
	var lines []cad.Line
	for _, l := range p.Footprint.Lines {
		if l.Layer != cad.LayerCourtyard {
			lines = append(lines, l)
		}
	}
	p.Footprint.Lines = lines
	for _, l := range []string{cad.LayerSilk, cad.LayerFab, cad.LayerCourtyard, cad.LayerCopper} {
		p.Footprint.Graphics = append(p.Footprint.Graphics, drawn(l)...)
	}
	// The symbol as Eagle reads it (a power output pin reads as an output).
	r, err := ReadLibrary(Library("SheetDiver", []Part{p}))
	if err != nil {
		t.Fatal(err)
	}
	p.Symbol = r.Symbols["TLV757P"].Symbol
	p.Symbol.Graphics = drawn(cad.LayerBody)
	return p
}

// Every graphic written reads back the same, as the same kinds.
func TestGraphicsReadBack(t *testing.T) {
	p := graphicPart(t)
	lib := Library("SheetDiver", []Part{p})
	r, err := ReadLibrary(lib)
	if err != nil {
		t.Fatal(err)
	}
	pkg, sym := r.Packages["DFN-8_2X2"], r.Symbols["TLV757P"]
	if got, want := cad.FootprintPrint(pkg.Footprint), cad.FootprintPrint(p.Footprint); got != want {
		t.Errorf("footprint print %s, wrote %s", got, want)
	}
	if got, want := cad.SymbolPrint(sym.Symbol), cad.SymbolPrint(p.Symbol); got != want {
		t.Errorf("symbol print %s, wrote %s", got, want)
	}
	// Decoded from the elements alone, the same.
	fp, err := DecodePackage(pkg.Raw)
	if err != nil || cad.FootprintPrint(fp) != cad.FootprintPrint(p.Footprint) {
		t.Errorf("decoded package differs: %v", err)
	}
	s, err := DecodeSymbol(sym.RawSymbol, sym.RawDevice)
	if err != nil || cad.SymbolPrint(s) != cad.SymbolPrint(p.Symbol) {
		t.Errorf("decoded symbol differs: %v", err)
	}
	// Arcs, circles, filled rectangles and texts come back as graphics;
	// straight wires and polygons on silk and fab are lines and marks as
	// before, elsewhere graphics.
	byLayer := map[string][]cad.Graphic{}
	for _, g := range fp.Graphics {
		byLayer[g.Layer] = append(byLayer[g.Layer], g)
	}
	silk := map[string]int{cad.GraphicArc: 5, cad.GraphicCircle: 2, cad.GraphicRect: 1, cad.GraphicText: 5}
	other := map[string]int{cad.GraphicLine: 8, cad.GraphicArc: 5, cad.GraphicCircle: 2, cad.GraphicRect: 1, cad.GraphicPoly: 1, cad.GraphicText: 5}
	for l, want := range map[string]map[string]int{cad.LayerSilk: silk, cad.LayerFab: silk, cad.LayerCourtyard: other, cad.LayerCopper: other} {
		if got := kinds(byLayer[l]); !sameCounts(got, want) {
			t.Errorf("%s graphics %v, want %v", l, got, want)
		}
	}
	body := map[string]int{cad.GraphicArc: 5, cad.GraphicCircle: 2, cad.GraphicRect: 1, cad.GraphicPoly: 1, cad.GraphicText: 5}
	if got := kinds(s.Graphics); !sameCounts(got, body) {
		t.Errorf("symbol graphics %v, want %v", got, body)
	}
	// A drawn rectangle bigger than the body leaves the body as it was.
	if s.Body != p.Symbol.Body || len(s.Extra) != 8 {
		t.Errorf("symbol body %v (wrote %v), %d extra lines", s.Body, p.Symbol.Body, len(s.Extra))
	}
	// Written again from what was read, it reads the same.
	r2, err := ReadLibrary(Library("SheetDiver", []Part{{Symbol: sym.Symbol, Footprint: pkg.Footprint}}))
	if err != nil {
		t.Fatal(err)
	}
	if cad.FootprintPrint(r2.Packages["DFN-8_2X2"].Footprint) != cad.FootprintPrint(p.Footprint) || cad.SymbolPrint(r2.Symbols["TLV757P"].Symbol) != cad.SymbolPrint(p.Symbol) {
		t.Error("rewriting what was read changed it")
	}
}

// Eagle's own placement of texts: not spun, turned round to read from
// below; aligned other than bottom left.
func TestTextAnchor(t *testing.T) {
	for _, c := range []struct {
		t     xText
		at    cad.Pt
		angle float64
	}{
		{xText{X: 1, Y: 2, Size: 1, Rot: "SR180"}, cad.Pt{X: 1, Y: 2}, 180},
		{xText{X: 1, Y: 2, Size: 1, Rot: "R90"}, cad.Pt{X: 1, Y: 2}, 90},
		{xText{X: 1, Y: 2, Size: 1, Rot: "R180", Align: "top-right"}, cad.Pt{X: 1, Y: 2}, 0},
		{xText{X: 1, Y: 2, Size: 1, Align: "top-left"}, cad.Pt{X: 1, Y: 1}, 0},
		{xText{X: 1, Y: 2, Size: 1, Rot: "R270", Align: "top-right"}, cad.Pt{X: 1, Y: 2}, 90},
		{xText{X: 1, Y: 2, Size: 1, Rot: "R180"}, cad.Pt{X: 1 - 1.6, Y: 1}, 0},
	} {
		at, angle := textAnchor(c.t, "AB")
		if cad.Number(at.X) != cad.Number(c.at.X) || cad.Number(at.Y) != c.at.Y || angle != c.angle {
			t.Errorf("%+v: %v %g, want %v %g", c.t, at, angle, c.at, c.angle)
		}
	}
}

// A rectangle turned in Eagle reads as turned.
func TestTurnedRect(t *testing.T) {
	pt := func(x, y float64) cad.Pt { return cad.Pt{X: x, Y: y} }
	if g := rectGraphic(xRect{X1: 0, Y1: 0, X2: 4, Y2: 2, Rot: "R90"}, cad.LayerSilk, pt); g.Kind != cad.GraphicRect || g.Pts[0] != (cad.Pt{X: 1, Y: -1}) || g.Pts[1] != (cad.Pt{X: 3, Y: 3}) {
		t.Errorf("R90: %+v", g)
	}
	if g := rectGraphic(xRect{X1: -1, Y1: -1, X2: 1, Y2: 1, Rot: "R45"}, cad.LayerSilk, pt); g.Kind != cad.GraphicPoly || len(g.Pts) != 4 || cad.Number(g.Pts[0].Y) != cad.Number(-math.Sqrt2) {
		t.Errorf("R45: %+v", g)
	}
}

// KiCad's Eagle importer reads the package's arcs, circles and texts.
func TestKiCadImportsGraphics(t *testing.T) {
	py, err := exec.LookPath("python3")
	if err != nil || exec.Command(py, "-c", "import pcbnew").Run() != nil {
		t.Skip("KiCad's pcbnew Python module isn't installed")
	}
	lbr := filepath.Join(t.TempDir(), "sd.lbr")
	if keep := os.Getenv("SHEETDIVER_KEEP"); keep != "" {
		lbr = keep
	}
	if err := os.WriteFile(lbr, Library("SheetDiver", []Part{graphicPart(t)}), 0o644); err != nil {
		t.Fatal(err)
	}
	// Arcs by centre and radius, circles by centre and radius, texts by
	// anchor and angle, in mm (KiCad's y is down, as the model's).
	script := `
import sys, pcbnew
mgr = getattr(pcbnew, "PCB_IO_MGR", None) or pcbnew.IO_MGR
fp = mgr.PluginFind(mgr.EAGLE).FootprintLoad(sys.argv[1], "DFN-8_2X2")
names = {pcbnew.F_SilkS: "silk", pcbnew.F_Fab: "fab", pcbnew.F_CrtYd: "courtyard", pcbnew.F_Cu: "copper"}
mm = lambda v: "%.2f" % (round(v / 1e6, 2) + 0)
for g in fp.GraphicalItems():
    layer = names.get(g.GetLayer(), g.GetLayerName())
    if g.GetClass() in ("FP_TEXT", "PCB_TEXT"):
        p = g.GetTextPos()
        print("text", layer, g.GetText(), mm(p.x), mm(p.y), round(g.GetTextAngleDegrees()) % 360)
    elif g.GetShapeStr() == "Arc":
        print("arc", layer, mm(g.GetCenter().x), mm(g.GetCenter().y), mm(g.GetRadius()))
    elif g.GetShapeStr() == "Circle":
        print("circle", layer, mm(g.GetCenter().x), mm(g.GetCenter().y))
`
	out, err := exec.Command(py, "-c", script, lbr).CombinedOutput()
	if err != nil {
		t.Fatalf("%v: %s", err, out)
	}
	got := map[string]bool{}
	for _, l := range strings.Split(string(out), "\n") {
		got[strings.TrimSpace(l)] = true
	}
	mm := func(v float64) string { return strconv.FormatFloat(math.Round(v*100)/100+0, 'f', 2, 64) }
	for _, l := range []string{cad.LayerSilk, cad.LayerFab, cad.LayerCourtyard, cad.LayerCopper} {
		for _, g := range drawn(l) {
			want := ""
			switch g.Kind {
			case cad.GraphicArc:
				o, r, _, _ := cad.ArcAngles(g)
				want = strings.Join([]string{"arc", l, mm(o.X), mm(o.Y), mm(r)}, " ")
			case cad.GraphicCircle:
				// A filled circle is a ring round half its radius.
				want = strings.Join([]string{"circle", l, mm(g.Pts[0].X), mm(g.Pts[0].Y)}, " ")
			case cad.GraphicText:
				want = strings.Join([]string{"text", l, g.Text, mm(g.Pts[0].X), mm(g.Pts[0].Y), strconv.Itoa(int(g.Angle))}, " ")
			default:
				continue
			}
			if !got[want] {
				t.Errorf("KiCad read no %q", want)
			}
		}
	}
	if t.Failed() {
		t.Logf("KiCad read:\n%s", out)
	}
}
