package kicad

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/aitakemyjob/sheetdiver/internal/cad"
	"github.com/aitakemyjob/sheetdiver/internal/cad/step"
)

// testParts are a symbol and footprint pair for each family tried.
func testParts(t *testing.T) ([]cad.Symbol, []cad.Footprint, map[string][]cad.Solid) {
	t.Helper()
	var syms []cad.Symbol
	var fps []cad.Footprint
	solids := map[string][]cad.Solid{}
	add := func(name string, p cad.Package, pins []cad.PinInfo) {
		f, ps := cad.MakeFootprint(name, p)
		if err := cad.Err(ps); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		s, ps := cad.MakeSymbol(name, pins, cad.SymbolSpec{})
		if err := cad.Err(ps); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		s.Footprint = "SheetDiver:" + name
		ss, _ := cad.MakeSolids(p)
		syms, fps, solids[name] = append(syms, s), append(fps, f), ss
	}
	soic, _ := cad.Standard("SOIC-8", 8)
	add("INA219_SOIC-8", soic, []cad.PinInfo{{Number: "1", Name: "A1"}, {Number: "2", Name: "A0"}, {Number: "3", Name: "SDA", Type: "I/O"}, {Number: "4", Name: "SCL", Type: "I"},
		{Number: "5", Name: "VS", Type: "P"}, {Number: "6", Name: "GND"}, {Number: "7", Name: "VIN-", Type: "A"}, {Number: "8", Name: "VIN+", Type: "A"}})
	qfn := cad.Package{Family: "qfn", Name: "QFN-16", Pins: 16, Pitch: 0.5, BodyLength: cad.Dim{Min: 2.9, Max: 3.1}, BodyWidth: cad.Dim{Min: 2.9, Max: 3.1},
		LeadLength: cad.Dim{Min: 0.3, Max: 0.5}, LeadWidth: cad.Dim{Min: 0.18, Max: 0.3}, Height: cad.Dim{Max: 0.8}, ExposedPad: &cad.Size{W: 1.7, H: 1.7},
		Land: nil}
	var pins []cad.PinInfo
	for i := 1; i <= 17; i++ {
		pins = append(pins, cad.PinInfo{Number: itoa(i), Name: "P" + itoa(i)})
	}
	pins[16].Name = "EP"
	add("TEST_QFN-16", qfn, pins)
	chip, _ := cad.Standard("0603", 2)
	add("C_0603", chip, []cad.PinInfo{{Number: "1", Name: "1"}, {Number: "2", Name: "2"}})
	bga := cad.Package{Family: "bga", Pins: 9, Pitch: 0.4, Rows: 3, Columns: 3, BodyLength: cad.Dim{Nom: 1.2}, BodyWidth: cad.Dim{Nom: 1.2}, LeadWidth: cad.Dim{Nom: 0.25}, Height: cad.Dim{Max: 0.5}}
	var balls []cad.PinInfo
	for _, r := range []string{"A", "B", "C"} {
		for c := 1; c <= 3; c++ {
			balls = append(balls, cad.PinInfo{Number: r + itoa(c), Name: "B" + r + itoa(c)})
		}
	}
	add("TEST_BGA-9", bga, balls)
	// Through hole: a DIP, a TO-220, a radial can and a custom connector
	// with shield tabs (one pin, four pads) and bare locating holes.
	dip, _ := cad.Standard("DIP-8", 8)
	add("NE555_DIP-8", dip, []cad.PinInfo{{Number: "1", Name: "GND"}, {Number: "2", Name: "TRIG", Type: "I"}, {Number: "3", Name: "OUT", Type: "O"}, {Number: "4", Name: "RESET", Type: "I"},
		{Number: "5", Name: "CV", Type: "I"}, {Number: "6", Name: "THR", Type: "I"}, {Number: "7", Name: "DIS", Type: "O"}, {Number: "8", Name: "VCC", Type: "P"}})
	to220, _ := cad.Standard("TO-220-3", 3)
	add("LM317_TO-220", to220, []cad.PinInfo{{Number: "1", Name: "ADJ", Type: "I"}, {Number: "2", Name: "VOUT", Type: "O"}, {Number: "3", Name: "VIN", Type: "P"}})
	add("CP_D6.3", cad.Package{Family: "radial", Pins: 2, Pitch: 2.5, BodyWidth: cad.Dim{Nom: 6.3}, Height: cad.Dim{Nom: 11}, LeadWidth: cad.Dim{Nom: 0.5}, Color: "blue"},
		[]cad.PinInfo{{Number: "1", Name: "+"}, {Number: "2", Name: "-"}})
	conn := cad.Package{Family: "custom", BodyWidth: cad.Dim{Nom: 9}, BodyLength: cad.Dim{Nom: 7}, Height: cad.Dim{Nom: 3}, Pads: []cad.PadSpec{
		{Number: "1", X: -1, Y: -3, W: 0.6, H: 1.2}, {Number: "2", X: 1, Y: -3, W: 0.6, H: 1.2},
		{Number: "S", X: -4.3, Y: -2.5, W: 1, H: 2, Shape: "oval", Drill: 0.6}, {Number: "S", X: 4.3, Y: -2.5, W: 1, H: 2, Shape: "oval", Drill: 0.6},
		{Number: "S", X: -4.3, Y: 1.5, W: 1, H: 1.6, Shape: "oval", Drill: 0.6}, {Number: "S", X: 4.3, Y: 1.5, W: 1, H: 1.6, Shape: "oval", Drill: 0.6},
		{X: -2.9, Y: -1.5, Drill: 0.65, Kind: "npth"}, {X: 2.9, Y: -1.5, Drill: 0.65, Kind: "npth"}}}
	add("CONN_TEST", conn, []cad.PinInfo{{Number: "1", Name: "D+"}, {Number: "2", Name: "D-"}, {Number: "S", Name: "SHIELD"}})
	return syms, fps, solids
}

// itoa writes an int.
func itoa(i int) string { return strconv.Itoa(i) }

func TestRoundTripAndFingerprints(t *testing.T) {
	syms, fps, _ := testParts(t)
	lib, err := SymbolLibrary(syms, nil)
	if err != nil {
		t.Fatal(err)
	}
	got, err := Symbols(string(lib))
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != len(syms) {
		t.Fatalf("%d symbols read back", len(got))
	}
	s := ReadSymbol(got["INA219_SOIC-8"])
	if len(s.Pins) != 8 || s.Body != syms[0].Body || s.Footprint != "SheetDiver:INA219_SOIC-8" {
		t.Errorf("symbol read back: %+v", s)
	}
	// Re-formatting isn't an edit; moving a pin is.
	before := Fingerprint(got["INA219_SOIC-8"])
	reformatted := strings.ReplaceAll(strings.ReplaceAll(got["INA219_SOIC-8"].String(), "\n", " "), "(length 2.54)", "(length 2.540)")
	nd, err := Parse(reformatted)
	if err != nil {
		t.Fatal(err)
	}
	if Fingerprint(nd) != before {
		t.Error("formatting changed the fingerprint")
	}
	moved, _ := Parse(strings.Replace(reformatted, `(name "SDA"`, `(name "SDA_1"`, 1))
	if Fingerprint(moved) == before {
		t.Error("a renamed pin kept the fingerprint")
	}

	fp := FootprintNode(fps[1], "/lib/TEST_QFN-16.step")
	back, err := Parse(fp.String())
	if err != nil {
		t.Fatal(err)
	}
	f := ReadFootprint(back)
	if len(f.Pads) != len(fps[1].Pads) || f.Courtyard != fps[1].Courtyard {
		t.Errorf("footprint read back: %d pads, courtyard %+v (want %+v)", len(f.Pads), f.Courtyard, fps[1].Courtyard)
	}
	paste := 0
	for _, p := range f.Pads {
		if p.Kind == cad.PadPaste {
			paste++
		}
		if p.Number == "17" && !p.NoPaste {
			t.Error("the exposed pad's paste is in windows")
		}
	}
	if paste != 4 {
		t.Errorf("%d paste windows", paste)
	}
	// An edited, raw symbol is written back as it was left.
	raw := strings.Replace(got["C_0603"].String(), `"1" (effects`, `"1" (effects`, 1)
	lib2, err := SymbolLibrary(syms, map[string]string{"C_0603": strings.Replace(raw, "(length 2.54)", "(length 5.08)", 1)})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(lib2), "(length 5.08)") {
		t.Error("the edited symbol wasn't kept")
	}
}

// boardHead is a minimal KiCad 7 board around the footprints.
const boardHead = `(kicad_pcb (version 20221018) (generator test)
  (general (thickness 1.6))
  (paper "A4")
  (layers (0 "F.Cu" signal) (31 "B.Cu" signal) (34 "B.Paste" user) (35 "F.Paste" user) (36 "B.SilkS" user "B.Silkscreen") (37 "F.SilkS" user "F.Silkscreen")
    (38 "B.Mask" user) (39 "F.Mask" user) (44 "Edge.Cuts" user) (46 "B.CrtYd" user "B.Courtyard") (47 "F.CrtYd" user "F.Courtyard") (48 "B.Fab" user) (49 "F.Fab" user))
  (setup (pad_to_mask_clearance 0))
  (net 0 "")
  (gr_rect (start 90 90) (end 160 140) (stroke (width 0.1) (type default)) (fill none) (layer "Edge.Cuts"))
`

// KiCad itself reads what's written: the symbol library and footprints
// export to SVG, and a board with the footprints exports to STEP with
// their models loaded.
func TestKiCadReadsThem(t *testing.T) {
	cli, err := exec.LookPath("kicad-cli")
	if err != nil {
		t.Skip("kicad-cli isn't installed")
	}
	dir := t.TempDir()
	syms, fps, solids := testParts(t)
	// Drawn graphics too.
	syms[0].Graphics, fps[0].Graphics = allGraphics(cad.LayerBody), allGraphics(cad.LayerSilk)
	lib, _ := SymbolLibrary(syms, nil)
	symPath := filepath.Join(dir, "SheetDiver.kicad_sym")
	pretty := filepath.Join(dir, "SheetDiver.pretty")
	shapes := filepath.Join(dir, "SheetDiver.3dshapes")
	for _, d := range []string{pretty, shapes} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(symPath, lib, 0o644); err != nil {
		t.Fatal(err)
	}
	board := boardHead
	for i, f := range fps {
		x := 100 + float64(i%5)*12
		y := 105 + float64(i/5)*14
		model := filepath.Join(shapes, f.Name+".step")
		if err := os.WriteFile(model, step.Write(f.Name, solids[f.Name], time.Now()), 0o644); err != nil {
			t.Fatal(err)
		}
		nd := FootprintNode(f, model)
		if err := os.WriteFile(filepath.Join(pretty, f.Name+".kicad_mod"), []byte(nd.String()), 0o644); err != nil {
			t.Fatal(err)
		}
		nd.Args[0] = Str("SheetDiver:" + f.Name)
		nd.Args = append(nd.Args[:1], append([]any{xy("at", x, y)}, nd.Args[1:]...)...)
		board += nd.String()
	}
	board += ")\n"
	if err := os.WriteFile(filepath.Join(dir, "test.kicad_pcb"), []byte(board), 0o644); err != nil {
		t.Fatal(err)
	}
	run := func(args ...string) string {
		cmd := exec.Command(cli, args...)
		cmd.Env = append(os.Environ(), "HOME="+dir)
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("kicad-cli %s: %v\n%s", strings.Join(args, " "), err, out)
		}
		return string(out)
	}
	run("sym", "export", "svg", symPath, "-o", filepath.Join(dir, "svg"))
	run("fp", "export", "svg", pretty, "-o", filepath.Join(dir, "fpsvg"))
	svgs, _ := filepath.Glob(filepath.Join(dir, "svg", "*.svg"))
	fpsvgs, _ := filepath.Glob(filepath.Join(dir, "fpsvg", "*.svg"))
	if len(svgs) != len(syms) || len(fpsvgs) != len(fps) {
		t.Errorf("KiCad drew %d symbols and %d footprints", len(svgs), len(fpsvgs))
	}
	out := run("pcb", "export", "step", "--force", filepath.Join(dir, "test.kicad_pcb"), "-o", filepath.Join(dir, "board.step"))
	if strings.Contains(out, "Could not") || strings.Contains(strings.ToLower(out), "error") {
		t.Errorf("STEP export complained:\n%s", out)
	}
	b, err := os.ReadFile(filepath.Join(dir, "board.step"))
	if err != nil {
		t.Fatal(err)
	}
	// The board's STEP holds each model's solids.
	if n := strings.Count(string(b), "MANIFOLD_SOLID_BREP"); n < 75 {
		t.Errorf("the board's STEP has %d solids: the models didn't load\n%s", n, out)
	}
	if os.Getenv("SHEETDIVER_KEEP") != "" {
		t.Log("kept in", dir)
		_ = exec.Command("cp", "-r", dir, os.Getenv("SHEETDIVER_KEEP")).Run()
	}
}
