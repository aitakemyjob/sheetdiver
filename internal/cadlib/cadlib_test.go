package cadlib

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/aitakemyjob/sheetdiver/internal/cad"
	"github.com/aitakemyjob/sheetdiver/internal/cad/altium"
)

// library is a library of one part, an SOIC-8 op-amp.
func library(t *testing.T, dir string) *Library {
	t.Helper()
	pins := []cad.PinInfo{{Number: "1", Name: "OUTA", Type: "O"}, {Number: "2", Name: "-INA", Type: "I"}, {Number: "3", Name: "+INA", Type: "I"}, {Number: "4", Name: "V-", Type: "P"},
		{Number: "5", Name: "+INB", Type: "I"}, {Number: "6", Name: "-INB", Type: "I"}, {Number: "7", Name: "OUTB", Type: "O"}, {Number: "8", Name: "V+", Type: "P"}}
	s, ps := cad.MakeSymbol("OPA2333", pins, cad.SymbolSpec{})
	if err := cad.Err(ps); err != nil {
		t.Fatal(err)
	}
	p, _ := cad.Standard("SOIC-8", 8)
	f, _ := cad.MakeFootprint("SOIC-8_OPA2333", p)
	ss, _ := cad.MakeSolids(p)
	when := time.Date(2026, 10, 31, 0, 0, 0, 0, time.UTC)
	return &Library{Dir: dir, Name: "MyParts", Items: []*Item{
		{Kind: "symbol", Name: "OPA2333", Key: "OPA2333|SOIC-8", State: Auto, Symbol: &s, Updated: when},
		{Kind: "footprint", Name: "SOIC-8_OPA2333", Key: "OPA2333|SOIC-8", State: Auto, Footprint: &f, Solids: ss, Updated: when},
	}}
}

// carry copies what a sync left in the items (as the store would keep
// it) into a freshly drawn library, as the next sync would see it.
func carry(from, to *Library) {
	for i, it := range from.Items {
		to.Items[i].Raw, to.Items[i].Written, to.Items[i].State = it.Raw, it.Written, it.State
		if it.State == Edited {
			// As the service does: the model from what was kept.
			s, f := Decode(it.Kind, it.Raw)
			if s != nil {
				s.Name = it.Name
				to.Items[i].Symbol = s
			}
			if f != nil {
				f.Name = it.Name
				to.Items[i].Footprint = f
			}
		}
	}
}

func TestStates(t *testing.T) {
	if err := Edited.To(Drawn, "U1"); !errors.As(err, &ErrEdited{}) {
		t.Errorf("redrawing an edit: %v", err)
	}
	if Auto.To(Drawn, "x") != nil || Edited.To(Auto, "x") != nil || Drawn.To(Edited, "x") != nil {
		t.Error("allowed moves refused")
	}
}

func TestSyncKeepsEdits(t *testing.T) {
	dir := t.TempDir()
	l := library(t, dir)
	rep, err := Sync(l, []string{"kicad", "eagle", "altium"})
	if err != nil {
		t.Fatal(err)
	}
	p := KiCad(l)
	for _, f := range []string{p.Symbols, filepath.Join(p.Footprints, "SOIC-8_OPA2333.kicad_mod"), filepath.Join(p.Models, "SOIC-8_OPA2333.step"), filepath.Join(dir, "eagle", "MyParts.lbr"),
		filepath.Join(dir, "altium", "MyParts.SchLib"), filepath.Join(dir, "altium", "MyParts.PcbLib")} {
		if _, err := os.Stat(f); err != nil {
			t.Errorf("%s wasn't written", f)
		}
	}
	if rep.Symbols != 1 || rep.Footprints != 1 || len(rep.Adopted) != 0 {
		t.Errorf("report %+v", rep)
	}
	sym, _ := os.ReadFile(p.Symbols)
	if !strings.Contains(string(sym), `"MyParts:SOIC-8_OPA2333"`) {
		t.Error("the symbol doesn't name its footprint")
	}

	// KiCad re-saving the library isn't an edit.
	if cli, err := exec.LookPath("kicad-cli"); err == nil {
		for _, args := range [][]string{{"sym", "upgrade", "--force", p.Symbols}, {"fp", "upgrade", "--force", p.Footprints}} {
			cmd := exec.Command(cli, args...)
			cmd.Env = append(os.Environ(), "HOME="+dir)
			if out, err := cmd.CombinedOutput(); err != nil {
				t.Fatalf("kicad-cli %v: %v\n%s", args, err, out)
			}
		}
		after, _ := os.ReadFile(p.Symbols)
		if string(after) == string(sym) {
			t.Log("kicad-cli left the symbol library as it was")
		}
	}
	l2 := library(t, dir)
	carry(l, l2)
	rep, err = Sync(l2, []string{"kicad"})
	if err != nil {
		t.Fatal(err)
	}
	if len(rep.Adopted) != 0 {
		t.Fatalf("a re-save was taken for an edit: %v", rep.Adopted)
	}

	// The user moves V+ and widens a pad in KiCad.
	edit := func(path, old, new string) {
		b, _ := os.ReadFile(path)
		if !strings.Contains(string(b), old) {
			t.Fatalf("%s has no %q", path, old)
		}
		_ = os.WriteFile(path, []byte(strings.Replace(string(b), old, new, 1)), 0o644)
	}
	symText, _ := os.ReadFile(p.Symbols)
	nd := strings.Index(string(symText), `"V+"`)
	if nd < 0 {
		t.Fatal("no V+")
	}
	edit(p.Symbols, `(name "V+"`, `(name "VCC"`)
	fpPath := filepath.Join(p.Footprints, "SOIC-8_OPA2333.kicad_mod")
	edit(fpPath, "(size 1.775 0.59)", "(size 2 0.59)")
	l3 := library(t, dir)
	carry(l2, l3)
	rep, err = Sync(l3, []string{"kicad", "eagle"})
	if err != nil {
		t.Fatal(err)
	}
	if len(rep.Adopted) != 2 || l3.Items[0].State != Edited || l3.Items[1].State != Edited {
		t.Fatalf("adopted %v, states %s %s", rep.Adopted, l3.Items[0].State, l3.Items[1].State)
	}
	// Kept in the file, in the model read back, and in the Eagle library.
	if b, _ := os.ReadFile(p.Symbols); !strings.Contains(string(b), `"VCC"`) {
		t.Error("the edit was overwritten")
	}
	found := false
	for _, pin := range l3.Items[0].Symbol.Pins {
		found = found || pin.Name == "VCC"
	}
	if !found {
		t.Error("the model wasn't read back from the edit")
	}
	if b, _ := os.ReadFile(filepath.Join(dir, "eagle", "MyParts.lbr")); !strings.Contains(string(b), `name="VCC"`) || !strings.Contains(string(b), `dx="2"`) {
		t.Error("the edit didn't reach the Eagle library")
	}
	// The next sync keeps it without adopting it again.
	l4 := library(t, dir)
	carry(l3, l4)
	rep, err = Sync(l4, []string{"kicad"})
	if err != nil || len(rep.Adopted) != 0 {
		t.Fatalf("second sync: %v %v", rep.Adopted, err)
	}
	if b, _ := os.ReadFile(fpPath); !strings.Contains(string(b), "(size 2 0.59)") {
		t.Error("the footprint edit was lost")
	}

	// A model the user replaced is kept.
	model := filepath.Join(p.Models, "SOIC-8_OPA2333.step")
	_ = os.WriteFile(model, []byte("ISO-10303-21; their own model"), 0o644)
	l5 := library(t, dir)
	carry(l4, l5)
	if rep, _ = Sync(l5, []string{"kicad"}); len(rep.Adopted) != 1 {
		t.Errorf("adopted %v", rep.Adopted)
	}
	if b, _ := os.ReadFile(model); string(b) != "ISO-10303-21; their own model" {
		t.Error("the user's model was overwritten")
	}
}

func TestUnknownFormat(t *testing.T) {
	if _, err := Sync(&Library{Dir: t.TempDir(), Name: "x"}, []string{"orcad"}); err == nil {
		t.Error("unknown format")
	}
}

// Edits made in Altium and in Eagle are kept and carried into the other
// tools' files; a library the tool merely saved again isn't an edit.
func TestAltiumAndEagleEdits(t *testing.T) {
	dir := t.TempDir()
	all := []string{"kicad", "eagle", "altium"}
	l := library(t, dir)
	if _, err := Sync(l, all); err != nil {
		t.Fatal(err)
	}
	_, schPath, pcbPath := altiumPaths(l)
	pcb, _ := os.ReadFile(pcbPath)
	fps, err := altium.ReadPcbLib(pcb)
	if err != nil || len(fps) != 1 || len(fps[0].Model) == 0 {
		t.Fatalf("the Altium library has %d footprints, model %d bytes: %v", len(fps), len(fps[0].Model), err)
	}

	// Altium saves it again: new ids, the same footprint.
	again, _ := altium.PcbLib("Renamed", []altium.Footprint{{Footprint: fps[0].Footprint, Height: fps[0].Height, Step: fps[0].Model}})
	_ = os.WriteFile(pcbPath, again, 0o644)
	l2 := library(t, dir)
	carry(l, l2)
	rep, err := Sync(l2, all)
	if err != nil || len(rep.Adopted) != 0 {
		t.Fatalf("a re-save was taken for an edit: %v %v", rep.Adopted, err)
	}

	// The user widens pad 1 in Altium.
	edited := fps[0].Footprint
	edited.Pads = append([]cad.Pad{}, edited.Pads...)
	edited.Pads[0].W = 2.2
	b, _ := altium.PcbLib("SheetDiver", []altium.Footprint{{Footprint: edited, Height: fps[0].Height, Step: fps[0].Model}})
	_ = os.WriteFile(pcbPath, b, 0o644)
	l3 := library(t, dir)
	carry(l2, l3)
	rep, err = Sync(l3, all)
	if err != nil || len(rep.Adopted) != 1 || l3.Items[1].State != Edited || l3.Items[1].Raw[altiumFile] == "" {
		t.Fatalf("adopted %v, %s: %v", rep.Adopted, l3.Items[1].State, err)
	}
	if b, _ := os.ReadFile(filepath.Join(KiCad(l3).Footprints, "SOIC-8_OPA2333.kicad_mod")); !strings.Contains(string(b), "(size 2.2 ") {
		t.Error("the Altium edit didn't reach the KiCad footprint")
	}
	if b, _ := os.ReadFile(eaglePath(l3)); !strings.Contains(string(b), `dx="2.2"`) {
		t.Error("the Altium edit didn't reach the Eagle library")
	}
	got, _ := os.ReadFile(pcbPath)
	if back, _ := altium.ReadPcbLib(got); back[0].Footprint.Pads[0].W != 2.2 {
		t.Error("the Altium file lost the edit")
	}
	// The model it decodes to is the edit.
	if _, f := Decode("footprint", l3.Items[1].Raw); f == nil || f.Pads[0].W != 2.2 {
		t.Errorf("decoded %+v", f)
	}
	// And it stays: the next sync adopts nothing.
	l4 := library(t, dir)
	carry(l3, l4)
	if rep, err = Sync(l4, all); err != nil || len(rep.Adopted) != 0 {
		t.Fatalf("second sync adopted %v: %v", rep.Adopted, err)
	}

	// The user renames a pin in Eagle.
	lbr, _ := os.ReadFile(eaglePath(l4))
	if !strings.Contains(string(lbr), `name="OUTA"`) {
		t.Fatal("no OUTA pin in the Eagle library")
	}
	_ = os.WriteFile(eaglePath(l4), []byte(strings.ReplaceAll(string(lbr), `"OUTA"`, `"OUT_A"`)), 0o644)
	l5 := library(t, dir)
	carry(l4, l5)
	rep, err = Sync(l5, all)
	if err != nil || len(rep.Adopted) != 1 || l5.Items[0].State != Edited {
		t.Fatalf("Eagle edit: adopted %v, %s: %v", rep.Adopted, l5.Items[0].State, err)
	}
	if b, _ := os.ReadFile(KiCad(l5).Symbols); !strings.Contains(string(b), `"OUT_A"`) {
		t.Error("the Eagle edit didn't reach the KiCad symbol")
	}
	syms, _ := os.ReadFile(schPath)
	rs, _ := altium.ReadSchLib(syms)
	renamed := false
	for _, p := range rs[0].Symbol.Pins {
		renamed = renamed || p.Number == "1" && p.Name == "OUT_A"
	}
	if !renamed {
		t.Errorf("the Eagle edit didn't reach the Altium symbol: %v", rs[0].Symbol.Pins)
	}
	l6 := library(t, dir)
	carry(l5, l6)
	if rep, err = Sync(l6, all); err != nil || len(rep.Adopted) != 0 {
		t.Fatalf("third sync adopted %v: %v", rep.Adopted, err)
	}
}

func TestSyncPrunesDroppedFootprints(t *testing.T) {
	dir := t.TempDir()
	l := library(t, dir)
	if _, err := Sync(l, []string{"kicad"}); err != nil {
		t.Fatal(err)
	}
	p := KiCad(l)
	mine := filepath.Join(p.Footprints, "Mine.kicad_mod")
	if err := os.WriteFile(mine, []byte("(footprint \"Mine\" (generator pcbnew))"), 0o644); err != nil {
		t.Fatal(err)
	}
	// The part's footprint is gone from the library (its package turned
	// out to be another part's): its files go; the user's own stays.
	l.Items = l.Items[:1]
	if _, err := Sync(l, []string{"kicad"}); err != nil {
		t.Fatal(err)
	}
	for _, f := range []string{filepath.Join(p.Footprints, "SOIC-8_OPA2333.kicad_mod"), filepath.Join(p.Models, "SOIC-8_OPA2333.step")} {
		if _, err := os.Stat(f); err == nil {
			t.Errorf("%s is still there", f)
		}
	}
	if _, err := os.Stat(mine); err != nil {
		t.Error("the user's own footprint was removed")
	}
}
