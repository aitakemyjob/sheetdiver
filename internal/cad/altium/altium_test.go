package altium

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/aitakemyjob/sheetdiver/internal/cad"
)

// testLibrary is an SOIC, a QFN with an exposed pad, paste windows and
// vias and a long name, a DIP, and a connector with a bare hole; and a
// symbol for each, one with a long name.
func testLibrary(t *testing.T) ([]cad.Footprint, []Symbol) {
	t.Helper()
	var fps []cad.Footprint
	mk := func(name string, p cad.Package) {
		f, ps := cad.MakeFootprint(name, p)
		if err := cad.Err(ps); err != nil {
			t.Fatal(err)
		}
		fps = append(fps, f)
	}
	so, _ := cad.Standard("SOIC-8", 8)
	mk("SOIC-8_TEST", so)
	mk("QFN-16_WITH_A_VERY_LONG_NAME_INDEED_X", cad.Package{Family: "qfn", Pins: 16, Pitch: 0.5, BodyLength: cad.Dim{Min: 2.9, Max: 3.1}, BodyWidth: cad.Dim{Min: 2.9, Max: 3.1},
		LeadLength: cad.Dim{Min: 0.3, Max: 0.5}, LeadWidth: cad.Dim{Min: 0.18, Max: 0.3}, ExposedPad: &cad.Size{W: 1.7, H: 1.7},
		Land: &cad.Land{PadLength: 0.8, PadWidth: 0.28, RowSpan: 2.9, Vias: &cad.Vias{Columns: 2, Rows: 2, Pitch: 1, Drill: 0.3}}})
	dip, _ := cad.Standard("DIP-8", 8)
	mk("DIP-8_TEST", dip)
	mk("CONN", cad.Package{Family: "custom", BodyWidth: cad.Dim{Nom: 9}, BodyLength: cad.Dim{Nom: 7}, Pads: []cad.PadSpec{
		{Number: "1", X: -1, Y: -3, W: 0.6, H: 1.2}, {Number: "S", X: -4.3, Y: -2.5, W: 1, H: 2, Shape: "oval", Drill: 0.6}, {X: 0, Y: 1, Drill: 0.65, Kind: "npth"}}})
	fps = append(fps, drawnFootprint())
	var syms []Symbol
	for _, n := range []string{"TPS563200", "INA219_SOIC_WITH_A_LONG_NAME_X_Y_Z"} {
		pins := []cad.PinInfo{{Number: "1", Name: "GND"}, {Number: "2", Name: "SW", Type: "O"}, {Number: "3", Name: "VIN", Type: "P"},
			{Number: "4", Name: "VFB", Type: "I"}, {Number: "5", Name: "EN", Type: "I"}, {Number: "6", Name: "VBST", Type: "I"}}
		s, ps := cad.MakeSymbol(n, pins, cad.SymbolSpec{})
		if err := cad.Err(ps); err != nil {
			t.Fatal(err)
		}
		s.Value, s.Description = n, "test part"
		syms = append(syms, Symbol{Symbol: s, Footprint: "SOT-23-6_" + n})
	}
	syms[0].Symbol.Graphics = drawnSymbol()
	return fps, syms
}

// arc is an arc of a circle on a layer, the increasing way from one
// angle to another; backwards, its points are given end first.
func arc(layer string, c cad.Pt, r, from, to, w float64, backwards bool) cad.Graphic {
	g := cad.ArcFrom(c, r, from, to)
	g.Layer, g.Width = layer, w
	if backwards {
		g.Pts[0], g.Pts[2] = g.Pts[2], g.Pts[0]
	}
	return g
}

// drawnFootprint is a footprint with a shape of every kind drawn on it:
// arcs every way, filled and unfilled rectangles, circles and polygons,
// and a turned text with a string beyond Windows-1252.
func drawnFootprint() cad.Footprint {
	return cad.Footprint{Name: "DRAWN", Description: "every shape", Mount: cad.MountThru,
		Pads:  []cad.Pad{{Number: "1", Kind: cad.PadSMD, Shape: "rect", X: -1, Y: 0, W: 0.6, H: 1}, {Number: "2", Kind: cad.PadThru, Shape: "circle", X: 1, Y: 0, W: 1.2, H: 1.2, Drill: 0.6}},
		Lines: []cad.Line{{Layer: cad.LayerSilk, A: cad.Pt{X: -3, Y: 3}, B: cad.Pt{X: 3, Y: 3}, Width: 0.12}},
		Graphics: []cad.Graphic{
			{Kind: cad.GraphicLine, Layer: cad.LayerFab, Pts: []cad.Pt{{X: -2, Y: -2}, {X: 2, Y: -2}}, Width: 0.1},
			{Kind: cad.GraphicLine, Layer: cad.LayerCopper, Pts: []cad.Pt{{X: -1, Y: 1}, {X: 1, Y: 1}}, Width: 0.2},
			{Kind: cad.GraphicRect, Layer: cad.LayerSilk, Pts: []cad.Pt{{X: -3, Y: -2.5}, {X: 3, Y: 2.5}}, Width: 0.12},
			{Kind: cad.GraphicRect, Layer: cad.LayerCopper, Pts: []cad.Pt{{X: 1.5, Y: 2}, {X: 0.5, Y: 1.5}}, Fill: true},
			{Kind: cad.GraphicRect, Layer: cad.LayerFab, Pts: []cad.Pt{{X: -0.5, Y: -1}, {X: 0.5, Y: -0.5}}, Fill: true},
			{Kind: cad.GraphicCircle, Layer: cad.LayerFab, Pts: []cad.Pt{{X: 0, Y: 0}}, R: 0.75, Width: 0.1},
			{Kind: cad.GraphicCircle, Layer: cad.LayerSilk, Pts: []cad.Pt{{X: -2.5, Y: -2}}, R: 0.2, Fill: true},
			{Kind: cad.GraphicCircle, Layer: cad.LayerCourtyard, Pts: []cad.Pt{{X: 2.5, Y: 2}}, R: 0.3, Fill: true, Width: 0.05},
			arc(cad.LayerSilk, cad.Pt{X: 1, Y: 1}, 1, 0, 90, 0.12, false),
			arc(cad.LayerFab, cad.Pt{X: -1, Y: 1}, 0.8, 300, 30, 0.1, false),
			arc(cad.LayerCourtyard, cad.Pt{X: 0, Y: -1}, 1.5, 120, 250, 0.05, true),
			arc(cad.LayerCopper, cad.Pt{X: 0, Y: 0}, 2, 200, 340, 0.2, true),
			{Kind: cad.GraphicPoly, Layer: cad.LayerSilk, Fill: true, Pts: []cad.Pt{{X: -3.5, Y: -3}, {X: -3, Y: -3}, {X: -3.5, Y: -2.5}}},
			{Kind: cad.GraphicPoly, Layer: cad.LayerCourtyard, Width: 0.05, Pts: []cad.Pt{{X: -4, Y: -4}, {X: 4, Y: -4}, {X: 4.5, Y: 4}, {X: -4, Y: 4}}},
			{Kind: cad.GraphicText, Layer: cad.LayerSilk, Pts: []cad.Pt{{X: -3, Y: -3.2}}, Text: "REF", Size: 1, Width: 0.15},
			{Kind: cad.GraphicText, Layer: cad.LayerFab, Pts: []cad.Pt{{X: 0, Y: 3}}, Text: "Ω 30°", Size: 0.8, Angle: 30},
		},
		Body: cad.Rect{X0: -2, Y0: -2, X1: 2, Y1: 2}, Courtyard: cad.Rect{X0: -4, Y0: -4, X1: 4.5, Y1: 4}}
}

// drawnSymbol are shapes of every kind drawn on a symbol, y up: arcs
// every way, filled and unfilled rectangles, circles and polygons, and
// texts, one turned.
func drawnSymbol() []cad.Graphic {
	b := cad.LayerBody
	return []cad.Graphic{
		{Kind: cad.GraphicLine, Layer: b, Pts: []cad.Pt{{X: -2.54, Y: 0}, {X: 2.54, Y: 1.27}}, Width: 0.254},
		{Kind: cad.GraphicRect, Layer: b, Pts: []cad.Pt{{X: -1.27, Y: -1.27}, {X: 1.27, Y: 1.27}}, Width: 0.254},
		{Kind: cad.GraphicRect, Layer: b, Pts: []cad.Pt{{X: 2, Y: 2}, {X: 1, Y: 1.5}}, Fill: true},
		{Kind: cad.GraphicCircle, Layer: b, Pts: []cad.Pt{{X: 0, Y: 0}}, R: 0.635, Width: 0.254},
		{Kind: cad.GraphicCircle, Layer: b, Pts: []cad.Pt{{X: 1.905, Y: -1.905}}, R: 0.3, Fill: true},
		arc(b, cad.Pt{X: 0, Y: 2.54}, 1.27, 0, 180, 0.254, false),
		arc(b, cad.Pt{X: -2.54, Y: -2.54}, 1, 300, 30, 0.254, false),
		arc(b, cad.Pt{X: 2.54, Y: -2.54}, 0.5, 90, 330, 0.254, true),
		{Kind: cad.GraphicPoly, Layer: b, Fill: true, Pts: []cad.Pt{{X: -1, Y: -3}, {X: 1, Y: -3}, {X: 0, Y: -2}}},
		{Kind: cad.GraphicPoly, Layer: b, Width: 0.254, Pts: []cad.Pt{{X: -3, Y: 3}, {X: -2, Y: 3}, {X: -2, Y: 4}, {X: -3, Y: 3.5}}},
		{Kind: cad.GraphicText, Layer: b, Pts: []cad.Pt{{X: -2.54, Y: -3.81}}, Text: "EN", Size: 1.27},
		{Kind: cad.GraphicText, Layer: b, Pts: []cad.Pt{{X: 3.81, Y: 0}}, Text: "VIN", Size: 2.54, Angle: 90},
	}
}

// kinds counts graphics by kind, and filled ones apart.
func kinds(gs []cad.Graphic) map[string]int {
	out := map[string]int{}
	for _, g := range gs {
		k := g.Kind
		if g.Fill {
			k += " filled"
		}
		out[k]++
	}
	return out
}

// Every shape drawn on a footprint or a symbol reads back the same, as
// the kind it was: tracks into lines, the rest as graphics.
func TestGraphicsReadBack(t *testing.T) {
	f := drawnFootprint()
	pb, err := PcbLib("SheetDiver", []Footprint{{Footprint: f, Height: 1}})
	if err != nil {
		t.Fatal(err)
	}
	got, err := ReadPcbLib(pb)
	if err != nil || len(got) != 1 {
		t.Fatal(err)
	}
	g := got[0].Footprint
	if cad.FootprintPrint(g) != cad.FootprintPrint(f) {
		t.Errorf("footprint changed:\nwrote %+v\nread %+v %+v", f.Graphics, g.Lines, g.Graphics)
	}
	want := map[string]int{"arc": 4, "circle": 1, "circle filled": 2, "rect filled": 2, "poly filled": 1, "text": 2, "line": 1}
	if k := kinds(g.Graphics); fmt.Sprint(k) != fmt.Sprint(want) {
		t.Errorf("graphics read back as %v, want %v", k, want)
	}
	if len(g.Lines) != 1+1+4+4 {
		t.Errorf("%d lines", len(g.Lines))
	}
	for _, x := range g.Graphics {
		if x.Kind == cad.GraphicText && x.Angle == 30 && x.Text != "Ω 30°" {
			t.Errorf("text %q", x.Text)
		}
		if x.Kind == cad.GraphicCircle && x.Layer == cad.LayerCourtyard && x.Width != 0.05 {
			t.Errorf("filled circle's stroke %+v", x)
		}
	}
	if d, err := DecodeFootprint(got[0].Raw); err != nil || cad.FootprintPrint(d) != cad.FootprintPrint(f) {
		t.Errorf("decoded footprint changed: %v", err)
	}

	_, syms := testLibrary(t)
	sb, err := SchLib("SheetDiver", syms)
	if err != nil {
		t.Fatal(err)
	}
	rs, err := ReadSchLib(sb)
	if err != nil {
		t.Fatal(err)
	}
	s := rs[0].Symbol
	if cad.SymbolPrint(s) != cad.SymbolPrint(syms[0].Symbol) {
		t.Errorf("symbol changed:\nwrote %+v\nread %+v", syms[0].Symbol.Graphics, s.Graphics)
	}
	want = map[string]int{"arc": 3, "circle": 1, "circle filled": 1, "rect": 1, "rect filled": 1, "poly filled": 1, "text": 2}
	if k := kinds(s.Graphics); fmt.Sprint(k) != fmt.Sprint(want) {
		t.Errorf("symbol graphics read back as %v, want %v", k, want)
	}
	if cad.SymbolPrint(rs[1].Symbol) != cad.SymbolPrint(syms[1].Symbol) {
		t.Error("a symbol without graphics changed")
	}
	if d, _, err := DecodeSymbol(rs[0].Raw); err != nil || len(d.Graphics) != len(s.Graphics) {
		t.Errorf("decoded symbol: %d graphics, %v", len(d.Graphics), err)
	}
}

// embed gives each footprint its 3D model.
func embed(t *testing.T, fps []cad.Footprint) []Footprint {
	t.Helper()
	var out []Footprint
	for _, f := range fps {
		out = append(out, Footprint{Footprint: f, Height: 1.75, Step: []byte("ISO-10303-21;\nmodel of " + f.Name + "\nEND-ISO-10303-21;\n")})
	}
	return out
}

// python runs a script with its arguments if python3 and the modules it
// imports are there, else skips.
func python(t *testing.T, modules, script string, args ...string) string {
	t.Helper()
	py, err := exec.LookPath("python3")
	if err != nil {
		t.Skip("no python3")
	}
	if exec.Command(py, "-c", "import "+modules).Run() != nil {
		t.Skipf("%s isn't installed", modules)
	}
	cmd := exec.Command(py, append([]string{"-c", script}, args...)...)
	cmd.Env = append(os.Environ(), "HOME="+t.TempDir())
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	return string(out)
}

// write writes the libraries to a folder.
func write(t *testing.T) (pcb, sch string) {
	t.Helper()
	fps, syms := testLibrary(t)
	dir := t.TempDir()
	pb, err := PcbLib("SheetDiver", embed(t, fps))
	if err != nil {
		t.Fatal(err)
	}
	sb, err := SchLib("SheetDiver", syms)
	if err != nil {
		t.Fatal(err)
	}
	pcb, sch = filepath.Join(dir, "t.PcbLib"), filepath.Join(dir, "t.SchLib")
	_ = os.WriteFile(pcb, pb, 0o644)
	_ = os.WriteFile(sch, sb, 0o644)
	return pcb, sch
}

// KiCad's own Altium importer reads the footprints: pads where they were
// drawn, their kinds, sizes and holes, and the paste windows.
func TestKiCadReadsPcbLib(t *testing.T) {
	pcb, _ := write(t)
	out := python(t, "pcbnew", `
import pcbnew, sys
p = pcbnew.IO_MGR.PluginFind(pcbnew.IO_MGR.ALTIUM_DESIGNER)
for n in p.FootprintEnumerate(sys.argv[1]):
    fp = p.FootprintLoad(sys.argv[1], n)
    for pd in fp.Pads():
        if pd.GetNumber() in ("1", ""):
            print(n, pd.GetNumber(), pd.GetAttribute(), round(pcbnew.ToMM(pd.GetPosition().x), 3), round(pcbnew.ToMM(pd.GetPosition().y), 3),
                  round(pcbnew.ToMM(pd.GetSize().x), 3), round(pcbnew.ToMM(pd.GetDrillSize().x), 3))
    paste = [g for g in fp.GraphicalItems() if g.GetLayer() == pcbnew.F_Paste]
    print(n, "pads", len(fp.Pads()), "paste", len(paste))
    if n != "DRAWN":
        continue
    mm = lambda v: round(pcbnew.ToMM(v), 3)
    for g in fp.GraphicalItems():
        if isinstance(g, pcbnew.FP_TEXT):
            print(n, "text", g.GetLayerName(), g.GetText(), mm(g.GetPosition().x), mm(g.GetPosition().y), mm(g.GetTextHeight()), g.GetTextAngle().AsDegrees())
        elif g.GetShapeStr() in ("Arc", "Circle"):
            print(n, g.GetShapeStr(), g.GetLayerName(), mm(g.GetCenter().x), mm(g.GetCenter().y), mm(g.GetRadius()), mm(g.GetStart().x), mm(g.GetStart().y), mm(g.GetWidth()))
        else:
            print(n, g.GetShapeStr(), g.GetLayerName(), g.IsFilled())
`, pcb)
	for _, want := range []string{
		"SOIC-8_TEST 1 1 -2.587 -1.905 1.775 0.0", // surface mount, top left
		"QFN-16_WITH_A_VERY_LONG_NAME_INDEED_X pads 21 paste 4",
		"DIP-8_TEST 1 0 -3.81 -3.81 1.6 0.8", // plated hole
		"CONN  3 0.0 1.0 0.65 0.65",          // bare hole
		"CONN pads 3 paste 0",
		// The drawn shapes: arcs the way they were drawn, y down (the
		// quarter from (2,1) clockwise as seen to (1,2)), circles, a disc
		// for a filled one, filled rectangles and polygon, and texts.
		"DRAWN Arc F.Silkscreen 1.0 1.0 1.0 2.0 1.0 0.12",
		"DRAWN Arc B.Fab -1.0 1.0 0.8 -0.6 0.307 0.1",
		"DRAWN Arc F.Cu 0.0 0.0 2.0 -1.879 -0.684 0.2",
		"DRAWN Circle B.Fab 0.0 0.0 0.75",
		"DRAWN Circle F.Silkscreen -2.5 -2.0 0.1 -2.5 -2.0 0.2",
		"DRAWN Rect F.Cu True",
		"DRAWN Polygon F.Silkscreen True",
		"DRAWN text F.Silkscreen REF -3.0 -3.2 1.0 0.0",
		"DRAWN text B.Fab ? 30° 0.0 3.0 0.8 30.0",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("KiCad read no %q in:\n%s", want, out)
		}
	}
}

// pyaltiumlib, an independent reader, reads both libraries: the symbols
// with their pins, designators and comments, and the footprints.
func TestPyAltiumLibReadsThem(t *testing.T) {
	pcb, sch := write(t)
	out := python(t, "pyaltiumlib", `
import logging, sys, pyaltiumlib
logging.disable(logging.WARNING)
s = pyaltiumlib.read(sys.argv[1])
for part in s.Parts:
    pins = sorted((r.rawdata.get('designator'), r.rawdata.get('name'), str(r.electrical_type), r.rawdata.get('location.x'), r.rawdata.get('location.y'))
                  for r in part.Records if type(r).__name__ == 'SchPin')
    print(part.Name, part.Designator, part.Parameter.get('Comment'), pins)
    kinds = {}
    for r in part.Records:
        kinds[type(r).__name__] = kinds.get(type(r).__name__, 0) + 1
    print(part.Name, sorted(kinds.items()))
    for r in part.Records:
        if type(r).__name__ == 'SchLabel':
            print(part.Name, 'label', r.text, r.orientation, r.Symbol.LibFile._Fonts[r.font_id].size)
p = pyaltiumlib.read(sys.argv[2])
print([(x.Name, len(x.Records)) for x in p.Parts])
for x in p.Parts:
    for r in x.Records:
        if type(r).__name__ == 'PcbString':
            print(x.Name, 'string', r.text, r.layer, round(r.rotation, 3))
`, sch, pcb)
	for _, want := range []string{
		"TPS563200 U? TPS563200 [('1', 'GND', 'Power', 0, -20), ('2', 'SW', 'Output', 20, 0), ('3', 'VIN', 'Power', 0, 20), ('4', 'VFB', 'Input', -20, 10)",
		"INA219_SOIC_WITH_A_LONG_NAME_X_Y_Z U?",
		"TPS563200 [('SchArc', 3), ('SchComponent', 1), ('SchDesignator', 1), ('SchEllipse', 2), ('SchImplementationList', 1), ('SchLabel', 2), ('SchLine', 1), " +
			"('SchParameter', 1), ('SchPin', 6), ('SchPolygon', 1), ('SchPolyline', 1), ('SchRectangle', 3)]",
		"TPS563200 label EN None 10.0",                // in the font of its size
		"TPS563200 label VIN Rotated 90 degrees 20.0", // turned
		"('SOIC-8_TEST', 23), ('QFN-16_WITH_A_VERY_LONG_NAME_INDEED_X', 46), ('DIP-8_TEST', 29), ('CONN', 21), ('DRAWN', 27)",
		"DRAWN string REF 33 0.0",
		"DRAWN string ? 30° 69 30.0",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("pyaltiumlib read no %q in:\n%s", want, out)
		}
	}
	if strings.Contains(out, "Failed") {
		t.Errorf("pyaltiumlib failed:\n%s", out)
	}
}

// What's written reads back: the pads, lines, pins and footprint links,
// and each footprint's embedded model.
func TestReadBack(t *testing.T) {
	fps, syms := testLibrary(t)
	pb, err := PcbLib("SheetDiver", embed(t, fps))
	if err != nil {
		t.Fatal(err)
	}
	got, err := ReadPcbLib(pb)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != len(fps) {
		t.Fatalf("%d footprints read back", len(got))
	}
	byName := map[string]ReadFootprint{}
	for _, g := range got {
		byName[g.Footprint.Name] = g
	}
	for _, f := range fps {
		g, ok := byName[f.Name]
		if !ok {
			t.Fatalf("%s wasn't read back", f.Name)
		}
		terminals := func(pads []cad.Pad) (n int) {
			for _, p := range pads {
				if p.Kind != cad.PadPaste {
					n++
				}
			}
			return
		}
		if terminals(g.Footprint.Pads) != terminals(f.Pads) || g.Footprint.Mount != f.Mount || g.Height != 1.75 {
			t.Errorf("%s read back: %d pads, %s, height %v", f.Name, len(g.Footprint.Pads), g.Footprint.Mount, g.Height)
		}
		if !bytes.Contains(g.Model, []byte("model of "+f.Name)) || len(g.Raw.Models) != 1 || g.Raw.Streams["Data"] == nil {
			t.Errorf("%s: model %q, raw %d models", f.Name, g.Model, len(g.Raw.Models))
		}
		p1 := g.Footprint.Pads[0]
		if p1.Number != f.Pads[0].Number || p1.X != f.Pads[0].X || p1.Y != f.Pads[0].Y || p1.W != f.Pads[0].W {
			t.Errorf("%s pad %+v, wrote %+v", f.Name, p1, f.Pads[0])
		}
	}
	if q := byName["QFN-16_WITH_A_VERY_LONG_NAME_INDEED_X"].Footprint; len(q.Pads) != 25 || !q.Pads[16].NoPaste {
		t.Errorf("QFN: %d pads, exposed pad %+v", len(q.Pads), q.Pads[16])
	}
	// Written again as read, it reads the same.
	var again []Footprint
	for _, g := range got {
		r := g.Raw
		again = append(again, Footprint{Footprint: g.Footprint, Raw: &r})
	}
	pb2, err := PcbLib("SheetDiver", again)
	if err != nil {
		t.Fatal(err)
	}
	got2, err := ReadPcbLib(pb2)
	if err != nil || len(got2) != len(got) {
		t.Fatal(err)
	}
	for i := range got {
		if cad.FootprintPrint(got[i].Footprint) != cad.FootprintPrint(got2[i].Footprint) || !bytes.Equal(got[i].Model, got2[i].Model) {
			t.Errorf("%s changed on rewriting", got[i].Footprint.Name)
		}
	}

	sb, err := SchLib("SheetDiver", syms)
	if err != nil {
		t.Fatal(err)
	}
	rs, err := ReadSchLib(sb)
	if err != nil {
		t.Fatal(err)
	}
	if len(rs) != 2 {
		t.Fatalf("%d symbols", len(rs))
	}
	for i, r := range rs {
		w := syms[i]
		if r.Symbol.Name != w.Symbol.Name || r.Footprint != w.Footprint || r.Symbol.Reference != "U" || r.Symbol.Value != w.Symbol.Value || r.Symbol.Body != w.Symbol.Body {
			t.Errorf("symbol read back %+v", r.Symbol)
		}
		for j, p := range r.Symbol.Pins {
			if q := w.Symbol.Pins[j]; p.Number != q.Number || p.X != q.X || p.Y != q.Y || p.Side != q.Side || p.Type != q.Type {
				t.Errorf("pin %+v, wrote %+v", p, q)
			}
		}
	}
}
