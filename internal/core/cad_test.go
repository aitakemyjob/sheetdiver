package core

import (
	"bytes"
	"context"
	"errors"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/aitakemyjob/sheetdiver/internal/cad"
	"github.com/aitakemyjob/sheetdiver/internal/cadlib"
	"github.com/aitakemyjob/sheetdiver/internal/library"
)

// cadTestPart is the test part with a SOT-23-6 pin table of its own.
func cadTestPart(t *testing.T, s *Service) library.Part {
	t.Helper()
	ctx := context.Background()
	r, err := s.Lib.Resolve(ctx, "SD1234")
	if err != nil {
		t.Fatal(err)
	}
	pins := []library.Pin{{Number: "1", Name: "BST", Type: "I"}, {Number: "2", Name: "GND"}, {Number: "3", Name: "FB", Type: "I"},
		{Number: "4", Name: "EN", Type: "I"}, {Number: "5", Name: "VIN", Type: "P"}, {Number: "6", Name: "SW", Type: "O"}}
	if err := s.RecordPins(ctx, r.Part.DatasheetID, "SOT-23-6", pins, library.SourceAgent); err != nil {
		t.Fatal(err)
	}
	return r.Part
}

func TestCadLibrary(t *testing.T) {
	s := newService(t)
	ctx := context.Background()
	p := cadTestPart(t, s)

	// SheetDiver draws both from what it knows: the pin table, and the
	// package's standard outline.
	pc, err := s.PartCad(ctx, p.MPN)
	if err != nil {
		t.Fatal(err)
	}
	var sot *CadPackage
	for i := range pc.Packages {
		if pc.Packages[i].Package == "SOT-23-6" {
			sot = &pc.Packages[i]
		}
	}
	if sot == nil || sot.Symbol == nil || sot.Footprint == nil || !sot.Footprint.Standard || sot.Footprint.State != "auto" || len(sot.Match) != 0 {
		t.Fatalf("SOT-23-6: %+v", sot)
	}
	if sot.Footprint.Model == nil || len(sot.Footprint.Model.Items) == 0 || len(sot.Symbol.Drawing.Items) == 0 {
		t.Error("no drawings")
	}

	rep, err := s.SyncCad(ctx)
	if err != nil {
		t.Fatal(err)
	}
	lib := s.CadLibrary()
	if rep.Symbols == 0 || rep.Footprints == 0 {
		t.Fatalf("report %+v", rep)
	}
	sym, err := os.ReadFile(lib.KiCad.Symbols)
	if err != nil || !strings.Contains(string(sym), `"SheetDiver:SOT-23-6_`+p.MPN+`"`) {
		t.Fatalf("symbol library: %v", err)
	}

	// The agent draws the footprint from the datasheet's land pattern.
	spec, _ := cad.Standard("SOT-23-6", 6)
	spec.Land = &cad.Land{PadLength: 1.1, PadWidth: 0.6, RowSpan: 2.6, Page: 31}
	pc, ps, err := s.DrawFootprint(ctx, p.MPN, "SOT-23-6", spec, "TI's DDC land pattern")
	if err != nil {
		t.Fatalf("%v %v", err, ps)
	}
	fp := pc.Packages[0].Footprint
	if fp.State != "drawn" || fp.Footprint.From != cad.FromDatasheet || fp.Notes != "TI's DDC land pattern" {
		t.Errorf("drawn footprint %+v", fp)
	}
	file := filepath.Join(lib.KiCad.Footprints, "SOT-23-6_"+p.MPN+".kicad_mod")
	b, _ := os.ReadFile(file)
	if !strings.Contains(string(b), "(size 1.1 0.6)") {
		t.Error("the drawn land pattern wasn't written")
	}
	// A drawing that can't be made is refused with its problems.
	bad := spec
	bad.Pitch, bad.Land = 0, nil
	if _, _, err := s.DrawFootprint(ctx, p.MPN, "SOT-23-6", bad, ""); err == nil {
		t.Error("a footprint with no pitch was saved")
	}

	// The user edits the footprint in KiCad: it's kept, and the agent
	// can't redraw it until it's reset.
	_ = os.WriteFile(file, []byte(strings.Replace(string(b), "(size 1.1 0.6)", "(size 1.3 0.6)", 1)), 0o644)
	if rep, err = s.SyncCad(ctx); err != nil || len(rep.Adopted) != 1 {
		t.Fatalf("adopted %v %v", rep.Adopted, err)
	}
	pc, _ = s.PartCad(ctx, p.MPN)
	if fp := pc.Packages[0].Footprint; fp.State != "edited" || fp.Footprint.Pads[0].W != 1.3 {
		t.Errorf("edited footprint %+v", fp.Footprint.Pads[0])
	}
	if _, _, err := s.DrawFootprint(ctx, p.MPN, "SOT-23-6", spec, ""); !errors.As(err, &cadlib.ErrEdited{}) {
		t.Errorf("redrawing an edit: %v", err)
	}
	// Reset goes back to the agent's drawing.
	pc, err = s.ResetCad(ctx, p.MPN, "SOT-23-6", "footprint")
	if err != nil {
		t.Fatal(err)
	}
	if fp := pc.Packages[0].Footprint; fp.State != "drawn" || fp.Footprint.Pads[0].W != 1.1 {
		t.Errorf("reset footprint %s %+v", fp.State, fp.Footprint.Pads[0])
	}
	if b, _ := os.ReadFile(file); !strings.Contains(string(b), "(size 1.1 0.6)") {
		t.Error("the reset wasn't written")
	}

	// The agent moves the bootstrap pin to the right.
	pc, _, err = s.DrawSymbol(ctx, p.MPN, "SOT-23-6", cad.SymbolSpec{Pins: []cad.PinPlace{{Number: "1", Side: cad.Right, Position: 1}}}, "")
	if err != nil {
		t.Fatal(err)
	}
	for _, pin := range pc.Packages[0].Symbol.Symbol.Pins {
		if pin.Number == "1" && pin.Side != cad.Right {
			t.Errorf("BST on the %s", pin.Side)
		}
	}
	// Pictures for the agent.
	for _, what := range []string{"symbol", "footprint", "3d"} {
		img, _, err := s.CadImage(ctx, p.MPN, "SOT-23-6", what)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := png.Decode(bytes.NewReader(img)); err != nil {
			t.Errorf("%s: %v", what, err)
		}
	}
}
