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
			to.Items[i].Symbol, to.Items[i].Footprint = it.Symbol, it.Footprint
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
	rep, err := Sync(l, []string{"kicad", "eagle"})
	if err != nil {
		t.Fatal(err)
	}
	p := KiCad(l)
	for _, f := range []string{p.Symbols, filepath.Join(p.Footprints, "SOIC-8_OPA2333.kicad_mod"), filepath.Join(p.Models, "SOIC-8_OPA2333.step"), filepath.Join(dir, "eagle", "MyParts.lbr")} {
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
