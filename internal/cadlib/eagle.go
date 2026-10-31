package cadlib

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sort"

	"github.com/aitakemyjob/sheetdiver/internal/cad"
	"github.com/aitakemyjob/sheetdiver/internal/cad/eagle"
)

// The Eagle library: eagle/<name>.lbr, each part's symbol on its
// footprint. Fusion Electronics opens it; other tools import it. The
// user's edits in Eagle or Fusion are read back like KiCad's: an edited
// package, or symbol with its device set, is kept as they saved it.
func init() { Register(eagleFormat{}) }

// eagleFormat is Eagle 9 / Fusion Electronics XML.
type eagleFormat struct{}

// eagleFile is the key an item's raw and written maps use for Eagle.
const eagleFile = "eagle"

// Name is the format's name.
func (eagleFormat) Name() string { return eagleFile }

// Label is its name for people.
func (eagleFormat) Label() string { return "Eagle / Fusion" }

// eaglePath is the library's file.
func eaglePath(l *Library) string { return filepath.Join(l.Dir, "eagle", l.Name+".lbr") }

// rawSymbol is what's kept of a symbol edited in Eagle: its symbol and
// device set elements.
type rawSymbol struct {
	Symbol string `json:"symbol"`
	Device string `json:"device"`
}

// Write writes the library, a device for each symbol that has a
// footprint, then reads it back to remember what each item reads as.
func (eagleFormat) Write(l *Library) (string, error) {
	path := eaglePath(l)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return "", err
	}
	fps := l.footprints()
	var parts []eagle.Part
	for _, it := range l.Items {
		if it.Kind != "symbol" || it.Symbol == nil {
			continue
		}
		f, ok := fps[it.Key]
		if !ok {
			continue
		}
		p := eagle.Part{Symbol: *it.Symbol, Footprint: *f.Footprint}
		if raw, ok := f.Raw[eagleFile]; ok && f.State == Edited {
			p.RawPackage = raw
		}
		if raw, ok := it.Raw[eagleFile]; ok && it.State == Edited {
			var r rawSymbol
			if err := json.Unmarshal([]byte(raw), &r); err != nil {
				return "", err
			}
			p.RawSymbol, p.RawDevice = r.Symbol, r.Device
		}
		parts = append(parts, p)
	}
	sort.Slice(parts, func(i, j int) bool { return parts[i].Symbol.Name < parts[j].Symbol.Name })
	b := eagle.Library(l.Name, parts)
	if err := writeIfChanged(path, b); err != nil {
		return "", err
	}
	got, err := eagle.ReadLibrary(b)
	if err != nil {
		return "", err
	}
	for _, it := range l.Items {
		switch {
		case it.Kind == "footprint" && it.Footprint != nil:
			if p, ok := got.Packages[eagle.Name(it.Footprint.Name)]; ok {
				it.wrote(eagleFile, cad.FootprintPrint(p.Footprint))
			}
		case it.Kind == "symbol" && it.Symbol != nil:
			if s, ok := got.Symbols[eagle.Name(it.Symbol.Name)]; ok {
				it.wrote(eagleFile, cad.SymbolPrint(s.Symbol))
			}
		}
	}
	return path, nil
}

// Read adopts the packages and symbols the user changed in Eagle.
func (eagleFormat) Read(l *Library) ([]string, error) {
	b, err := readIfThere(eaglePath(l))
	if err != nil || b == nil {
		return nil, err
	}
	got, err := eagle.ReadLibrary(b)
	if err != nil {
		return nil, err
	}
	var adopted []string
	for _, it := range l.Items {
		if it.Written[eagleFile] == "" {
			continue
		}
		switch {
		case it.Kind == "footprint" && it.Footprint != nil:
			p, ok := got.Packages[eagle.Name(it.Footprint.Name)]
			if !ok {
				continue
			}
			if fp := cad.FootprintPrint(p.Footprint); fp != it.Written[eagleFile] {
				f := p.Footprint
				f.Name = it.Footprint.Name
				it.Footprint = &f
				it.adopt(eagleFile, p.Raw, fp)
				adopted = append(adopted, "footprint "+it.Name+" (Eagle)")
			}
		case it.Kind == "symbol" && it.Symbol != nil:
			s, ok := got.Symbols[eagle.Name(it.Symbol.Name)]
			if !ok {
				continue
			}
			if sp := cad.SymbolPrint(s.Symbol); sp != it.Written[eagleFile] {
				raw, _ := json.Marshal(rawSymbol{s.RawSymbol, s.RawDevice})
				sy := s.Symbol
				sy.Name = it.Symbol.Name
				it.Symbol = &sy
				it.adopt(eagleFile, string(raw), sp)
				adopted = append(adopted, "symbol "+it.Name+" (Eagle)")
			}
		}
	}
	return adopted, nil
}

// Decode is the symbol or footprint an Eagle edit holds.
func (eagleFormat) Decode(kind, raw string) (*cad.Symbol, *cad.Footprint, error) {
	if kind == "symbol" {
		var r rawSymbol
		if err := json.Unmarshal([]byte(raw), &r); err != nil {
			return nil, nil, err
		}
		s, err := eagle.DecodeSymbol(r.Symbol, r.Device)
		return &s, nil, err
	}
	f, err := eagle.DecodePackage(raw)
	return nil, &f, err
}
