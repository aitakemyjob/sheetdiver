package altium

import (
	"bytes"
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
	return fps, syms
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
`, pcb)
	for _, want := range []string{
		"SOIC-8_TEST 1 1 -2.587 -1.905 1.775 0.0", // surface mount, top left
		"QFN-16_WITH_A_VERY_LONG_NAME_INDEED_X pads 21 paste 4",
		"DIP-8_TEST 1 0 -3.81 -3.81 1.6 0.8", // plated hole
		"CONN  3 0.0 1.0 0.65 0.65",          // bare hole
		"CONN pads 3 paste 0",
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
p = pyaltiumlib.read(sys.argv[2])
print([(x.Name, len(x.Records)) for x in p.Parts])
`, sch, pcb)
	for _, want := range []string{
		"TPS563200 U? TPS563200 [('1', 'GND', 'Power', 0, -20), ('2', 'SW', 'Output', 20, 0), ('3', 'VIN', 'Power', 0, 20), ('4', 'VFB', 'Input', -20, 10)",
		"INA219_SOIC_WITH_A_LONG_NAME_X_Y_Z U?",
		"('SOIC-8_TEST', 23), ('QFN-16_WITH_A_VERY_LONG_NAME_INDEED_X', 46), ('DIP-8_TEST', 29), ('CONN', 21)",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("pyaltiumlib read no %q in:\n%s", want, out)
		}
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
