package core

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/aitakemyjob/sheetdiver/internal/cad"
	"github.com/aitakemyjob/sheetdiver/internal/library"
)

func TestEditSymbolFootprintAnd3D(t *testing.T) {
	s := newService(t)
	ctx := context.Background()
	p := cadTestPart(t, s)
	m, err := s.CadModelOf(ctx, p.MPN, "")
	if err != nil || m.Symbol == nil || m.Footprint == nil || len(m.Shapes) == 0 {
		t.Fatalf("model %+v %v", m, err)
	}

	// The symbol: a pin moved to the top, an arrow drawn on the body.
	sym := *m.Symbol
	sym.Pins[0].Side, sym.Pins[0].X, sym.Pins[0].Y = cad.Top, 0, sym.Body.Y1+2.54
	sym.Graphics = []cad.Graphic{{Kind: cad.GraphicPoly, Layer: cad.LayerBody, Pts: []cad.Pt{{X: 0, Y: 0}, {X: 1, Y: 0.5}, {X: 0, Y: 1}}, Fill: true}}
	pc, ps, err := s.EditSymbol(ctx, p.MPN, "", sym, "arrow for the switch node", library.SourceAgent)
	if err != nil {
		t.Fatalf("%v %v", err, ps)
	}
	sv := pc.Packages[0].Symbol
	if sv.State != "edited" || sv.EditedIn != "sheetdiver" || sv.Source != library.SourceAgent || len(sv.Symbol.Graphics) != 1 || sv.Symbol.Pins[0].Side != cad.Top {
		t.Fatalf("edited symbol %+v", sv)
	}
	// A broken edit is refused.
	bad := sym
	bad.Pins = append([]cad.SymbolPin{}, sym.Pins...)
	bad.Pins[1].Side = "middle"
	if _, _, err := s.EditSymbol(ctx, p.MPN, "", bad, "", library.SourceUser); err == nil {
		t.Error("a pin on no side was kept")
	}

	// The footprint: pad 1 made bigger, a circle on the silkscreen.
	fp := *m.Footprint
	fp.Pads = append([]cad.Pad{}, fp.Pads...)
	fp.Pads[0].W += 0.2
	fp.Graphics = append(fp.Graphics, cad.Graphic{Kind: cad.GraphicCircle, Layer: cad.LayerSilk, Pts: []cad.Pt{{X: -2, Y: -2}}, R: 0.2, Width: 0.12})
	pc, ps, err = s.EditFootprint(ctx, p.MPN, "", fp, "", library.SourceUser)
	if err != nil {
		t.Fatalf("%v %v", err, ps)
	}
	fv := pc.Packages[0].Footprint
	if fv.State != "edited" || fv.Standard || fv.Model == nil {
		t.Fatalf("edited footprint %+v", fv)
	}
	lib := s.CadLibrary()
	if _, err := s.SyncCad(ctx); err != nil {
		t.Fatal(err)
	}
	kmod := filepath.Join(lib.KiCad.Footprints, fv.Name+".kicad_mod")
	b, _ := os.ReadFile(kmod)
	if !strings.Contains(string(b), "fp_circle") {
		t.Fatal("the drawn circle wasn't written to KiCad")
	}

	// The 3D model: the body made taller.
	shapes := append([]cad.Shape3D{}, fv.Shapes...)
	for i := range shapes {
		if shapes[i].Part == cad.PartBody && shapes[i].Kind == cad.Shape3DBox {
			shapes[i].Max[2] = 3
		}
	}
	shapes = append(shapes, cad.Shape3D{Kind: cad.Shape3DCylinder, Part: cad.PartMark, X: 0, Y: 0, R: 0.3, Min: cad.V3{0, 0, 3}, Max: cad.V3{0, 0, 3.05}})
	pc, _, err = s.EditModel3D(ctx, p.MPN, "", shapes, "")
	if err != nil {
		t.Fatal(err)
	}
	if fv := pc.Packages[0].Footprint; !fv.Model3DEdited || len(fv.Shapes) != len(shapes) {
		t.Fatalf("3D edit %+v", fv.Shapes)
	}

	// The user edits the footprint in KiCad: the 3D edit stays.
	b, _ = os.ReadFile(kmod)
	if err := os.WriteFile(kmod, []byte(strings.Replace(string(b), "(fp_circle", "(fp_circle (center 9 9) (end 9.5 9) (stroke (width 0.12) (type solid)) (fill none) (layer \"F.SilkS\"))\n  (fp_circle", 1)), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := s.SyncCad(ctx); err != nil {
		t.Fatal(err)
	}
	pc, _ = s.PartCad(ctx, p.MPN)
	fv = pc.Packages[0].Footprint
	if fv.EditedIn != "kicad" || !fv.Model3DEdited {
		t.Fatalf("after the KiCad edit: in %q, 3D kept %v", fv.EditedIn, fv.Model3DEdited)
	}

	// Resetting the 3D model leaves the footprint's edit.
	pc, err = s.ResetCad(ctx, p.MPN, "", "3d")
	if err != nil {
		t.Fatal(err)
	}
	if fv := pc.Packages[0].Footprint; fv.Model3DEdited || fv.State != "edited" {
		t.Errorf("after resetting the 3D model: %+v", fv)
	}
}
