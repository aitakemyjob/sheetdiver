package cadlib

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/aitakemyjob/sheetdiver/internal/cad"
	"github.com/aitakemyjob/sheetdiver/internal/cad/kicad"
	"github.com/aitakemyjob/sheetdiver/internal/cad/step"
)

// The KiCad library: one symbol library, a footprint folder and a 3D
// model folder, under kicad/ in the library's folder:
//
//	kicad/<name>.kicad_sym
//	kicad/<name>.pretty/<footprint>.kicad_mod
//	kicad/<name>.3dshapes/<footprint>.step
func init() { Register(kicadFormat{}) }

// kicadFormat is KiCad 7 and later.
type kicadFormat struct{}

// Name is the format's name.
func (kicadFormat) Name() string { return "kicad" }

// Label is its name for people.
func (kicadFormat) Label() string { return "KiCad" }

// The keys an item's raw and written maps use for KiCad's files.
const (
	kicadFile = "kicad"
	stepFile  = "kicad.step"
	// userModel marks a 3D model the user replaced: it isn't rewritten.
	userModel = "kicad.step.user"
)

// KiCadPaths are where the library's KiCad files are.
type KiCadPaths struct {
	Dir        string `json:"dir"`
	Symbols    string `json:"symbols"`
	Footprints string `json:"footprints"`
	Models     string `json:"models"`
}

// KiCad is where a library's KiCad files are.
func KiCad(l *Library) KiCadPaths {
	dir := filepath.Join(l.Dir, "kicad")
	return KiCadPaths{Dir: dir, Symbols: filepath.Join(dir, l.Name+".kicad_sym"),
		Footprints: filepath.Join(dir, l.Name+".pretty"), Models: filepath.Join(dir, l.Name+".3dshapes")}
}

// readFile reads a file, "" if it isn't there.
func readFile(path string) (string, error) {
	b, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return "", nil
	}
	return string(b), err
}

// Read adopts the symbols, footprints and models the user changed in
// KiCad since they were last written: a symbol or footprint whose
// fingerprint differs from the one written, and a model file that isn't
// the one written.
func (kicadFormat) Read(l *Library) ([]string, error) {
	p := KiCad(l)
	var adopted []string
	src, err := readFile(p.Symbols)
	if err != nil {
		return nil, err
	}
	if src != "" {
		syms, err := kicad.Symbols(src)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", p.Symbols, err)
		}
		for _, it := range l.Items {
			if it.Kind != "symbol" {
				continue
			}
			nd, ok := syms[kicad.SafeName(it.Name)]
			if !ok || it.Written[kicadFile] == "" {
				continue
			}
			if fp := kicad.Fingerprint(nd); fp != it.Written[kicadFile] {
				s := kicad.ReadSymbol(nd)
				s.Name = it.Name
				it.Symbol = &s
				it.adopt(kicadFile, nd.String(), fp)
				adopted = append(adopted, "symbol "+it.Name)
			}
		}
	}
	for _, it := range l.Items {
		if it.Kind != "footprint" || it.Written[kicadFile] == "" {
			continue
		}
		name := kicad.SafeName(it.Name)
		src, err := readFile(filepath.Join(p.Footprints, name+".kicad_mod"))
		if err != nil {
			return nil, err
		}
		if src != "" {
			nd, err := kicad.Parse(src)
			if err != nil {
				return nil, fmt.Errorf("%s.kicad_mod: %w", name, err)
			}
			if fp := kicad.Fingerprint(nd); fp != it.Written[kicadFile] {
				f := kicad.ReadFootprint(nd)
				f.Name = it.Name
				it.Footprint = &f
				it.adopt(kicadFile, nd.String(), fp)
				adopted = append(adopted, "footprint "+it.Name)
			}
		}
		if b, err := os.ReadFile(filepath.Join(p.Models, name+".step")); err == nil && it.Written[stepFile] != "" && hash(b) != it.Written[stepFile] && it.Written[userModel] == "" {
			it.wrote(userModel, hash(b))
			adopted = append(adopted, "3D model "+it.Name)
		}
	}
	return adopted, nil
}

// hash is a file's fingerprint.
func hash(b []byte) string {
	h := sha256.Sum256(b)
	return hex.EncodeToString(h[:12])
}

// writeIfChanged writes a file unless it already holds the same bytes.
func writeIfChanged(path string, data []byte) error {
	if old, err := os.ReadFile(path); err == nil && bytes.Equal(old, data) {
		return nil
	}
	return os.WriteFile(path, data, 0o644)
}

// Write writes the symbol library, each footprint and each footprint's
// 3D model. Edited symbols and footprints are written as the user left
// them; a model the user replaced is left alone.
func (kicadFormat) Write(l *Library) (string, error) {
	p := KiCad(l)
	for _, d := range []string{p.Footprints, p.Models} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			return "", err
		}
	}
	fps := l.footprints()
	var syms []*Item
	raw := map[string]string{}
	for _, it := range l.Items {
		if it.Kind == "symbol" && it.Symbol != nil {
			if f, ok := fps[it.Key]; ok && it.Raw[kicadFile] == "" {
				it.Symbol.Footprint = l.Name + ":" + kicad.SafeName(f.Name)
			}
			syms = append(syms, it)
			if r, ok := it.Raw[kicadFile]; ok && it.State == Edited {
				raw[kicad.SafeName(it.Name)] = r
			}
		}
	}
	sort.Slice(syms, func(i, j int) bool { return syms[i].Name < syms[j].Name })
	list := make([]cad.Symbol, 0, len(syms))
	for _, it := range syms {
		list = append(list, *it.Symbol)
	}
	lib, err := kicad.SymbolLibrary(list, raw)
	if err != nil {
		return "", err
	}
	if err := writeIfChanged(p.Symbols, lib); err != nil {
		return "", err
	}
	written, err := kicad.Symbols(string(lib))
	if err != nil {
		return "", err
	}
	for _, it := range syms {
		if nd, ok := written[kicad.SafeName(it.Name)]; ok {
			it.wrote(kicadFile, kicad.Fingerprint(nd))
		}
	}
	keep := map[string]bool{}
	for _, it := range l.Items {
		if it.Kind != "footprint" || it.Footprint == nil {
			continue
		}
		name := kicad.SafeName(it.Name)
		keep[name] = true
		model := filepath.Join(p.Models, name+".step")
		var text string
		if r, ok := it.Raw[kicadFile]; ok && it.State == Edited {
			text = r
		} else {
			text = kicad.FootprintNode(*it.Footprint, model).String()
		}
		if err := writeIfChanged(filepath.Join(p.Footprints, name+".kicad_mod"), []byte(text)); err != nil {
			return "", err
		}
		nd, err := kicad.Parse(text)
		if err != nil {
			return "", err
		}
		it.wrote(kicadFile, kicad.Fingerprint(nd))
		if it.Written[userModel] == "" && len(it.Solids) > 0 {
			b := step.Write(name, it.Solids, it.Updated)
			if err := writeIfChanged(model, b); err != nil {
				return "", err
			}
			it.wrote(stepFile, hash(b))
		}
	}
	if err := pruneKiCad(p, keep); err != nil {
		return "", err
	}
	readme := fmt.Sprintf(`SheetDiver keeps this library. Add it to KiCad once:

  Preferences > Manage Symbol Libraries > Global > + : %s
  Preferences > Manage Footprint Libraries > Global > + : %s

with the nickname %s. Edit symbols and footprints in KiCad as you like:
SheetDiver reads your changes back and keeps them. 3D models are in
%s.
`, p.Symbols, p.Footprints, l.Name, p.Models)
	if err := writeIfChanged(filepath.Join(p.Dir, "README.txt"), []byte(readme)); err != nil {
		return "", err
	}
	return p.Dir, nil
}

// pruneKiCad removes the footprints SheetDiver wrote that are no longer
// in the library (a part's package it turned out not to come in), and
// their 3D models. A file the user made, replaced or saved from KiCad
// says another generator and stays.
func pruneKiCad(p KiCadPaths, keep map[string]bool) error {
	entries, err := os.ReadDir(p.Footprints)
	if err != nil {
		return err
	}
	for _, e := range entries {
		name, ok := strings.CutSuffix(e.Name(), ".kicad_mod")
		if !ok || keep[name] {
			continue
		}
		path := filepath.Join(p.Footprints, e.Name())
		if b, err := os.ReadFile(path); err != nil || !strings.Contains(string(b), "(generator sheetdiver)") {
			continue
		}
		if err := os.Remove(path); err != nil {
			return err
		}
		model := filepath.Join(p.Models, name+".step")
		if b, err := os.ReadFile(model); err == nil && strings.Contains(string(b), "('SheetDiver'),('SheetDiver'),'SheetDiver'") {
			_ = os.Remove(model)
		}
	}
	return nil
}

// Decode is the symbol or footprint a KiCad edit holds.
func (kicadFormat) Decode(kind, raw string) (*cad.Symbol, *cad.Footprint, error) {
	nd, err := kicad.Parse(raw)
	if err != nil {
		return nil, nil, err
	}
	if kind == "symbol" {
		s := kicad.ReadSymbol(nd)
		return &s, nil, nil
	}
	f := kicad.ReadFootprint(nd)
	f.From = "your edit in KiCad"
	return nil, &f, nil
}
