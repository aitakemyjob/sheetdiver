package cadlib

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"sort"

	"github.com/aitakemyjob/sheetdiver/internal/cad"
	"github.com/aitakemyjob/sheetdiver/internal/cad/altium"
	"github.com/aitakemyjob/sheetdiver/internal/cad/kicad"
	"github.com/aitakemyjob/sheetdiver/internal/cad/step"
)

// The Altium library: altium/<name>.SchLib and altium/<name>.PcbLib, each
// symbol linked to its footprint and each footprint's 3D model embedded.
// The user's edits in Altium are read back like KiCad's: an edited
// symbol or footprint is kept as they saved it (its storage's streams)
// and written to the other tools from what it reads as.
func init() { Register(altiumFormat{}) }

// altiumFormat is Altium Designer's binary libraries.
type altiumFormat struct{}

// altiumFile is the key an item's raw and written maps use for Altium.
const altiumFile = "altium"

// Name is the format's name.
func (altiumFormat) Name() string { return altiumFile }

// Label is its name for people.
func (altiumFormat) Label() string { return "Altium" }

// altiumPaths are the library's files.
func altiumPaths(l *Library) (dir, sch, pcb string) {
	dir = filepath.Join(l.Dir, "altium")
	return dir, filepath.Join(dir, l.Name+".SchLib"), filepath.Join(dir, l.Name+".PcbLib")
}

// stepOf is a footprint's 3D model: the one the user put in the KiCad
// library in its place, else SheetDiver's.
func stepOf(l *Library, it *Item) []byte {
	if it.Written[userModel] != "" {
		if b, err := os.ReadFile(filepath.Join(KiCad(l).Models, kicad.SafeName(it.Name)+".step")); err == nil {
			return b
		}
	}
	if len(it.Solids) == 0 {
		return nil
	}
	return step.Write(kicad.SafeName(it.Name), it.Solids, it.Updated)
}

// Write writes the schematic and footprint libraries, then reads them
// back to remember what each item reads as.
func (altiumFormat) Write(l *Library) (string, error) {
	dir, schPath, pcbPath := altiumPaths(l)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	fpByKey := l.footprints()
	var fps []altium.Footprint
	var syms []altium.Symbol
	for _, it := range l.Items {
		switch {
		case it.Kind == "footprint" && it.Footprint != nil:
			f := altium.Footprint{Footprint: *it.Footprint}
			if raw, ok := it.Raw[altiumFile]; ok && it.State == Edited {
				var r altium.Raw
				if err := json.Unmarshal([]byte(raw), &r); err != nil {
					return "", err
				}
				f.Raw = &r
			} else {
				f.Step = stepOf(l, it)
				if len(it.Solids) > 0 {
					_, hi := cad.Bounds(it.Solids)
					f.Height = hi[2]
				}
			}
			fps = append(fps, f)
		case it.Kind == "symbol" && it.Symbol != nil:
			s := altium.Symbol{Symbol: *it.Symbol}
			if f, ok := fpByKey[it.Key]; ok {
				s.Footprint = f.Footprint.Name
			}
			if raw, ok := it.Raw[altiumFile]; ok && it.State == Edited {
				if err := json.Unmarshal([]byte(raw), &s.Raw); err != nil {
					return "", err
				}
			}
			syms = append(syms, s)
		}
	}
	sort.Slice(fps, func(i, j int) bool { return fps[i].Footprint.Name < fps[j].Footprint.Name })
	sort.Slice(syms, func(i, j int) bool { return syms[i].Symbol.Name < syms[j].Symbol.Name })
	pcb, err := altium.PcbLib(l.Name, fps)
	if err != nil {
		return "", err
	}
	sch, err := altium.SchLib(l.Name, syms)
	if err != nil {
		return "", err
	}
	if err := writeIfChanged(pcbPath, pcb); err != nil {
		return "", err
	}
	if err := writeIfChanged(schPath, sch); err != nil {
		return "", err
	}
	readFPs, err := altium.ReadPcbLib(pcb)
	if err != nil {
		return "", err
	}
	readSyms, err := altium.ReadSchLib(sch)
	if err != nil {
		return "", err
	}
	byFP, bySym := map[string]string{}, map[string]string{}
	for _, f := range readFPs {
		byFP[f.Footprint.Name] = cad.FootprintPrint(f.Footprint)
	}
	for _, s := range readSyms {
		bySym[s.Symbol.Name] = cad.SymbolPrint(s.Symbol)
	}
	for _, it := range l.Items {
		switch {
		case it.Kind == "footprint" && it.Footprint != nil:
			it.wrote(altiumFile, byFP[it.Footprint.Name])
		case it.Kind == "symbol" && it.Symbol != nil:
			it.wrote(altiumFile, bySym[it.Symbol.Name])
		}
	}
	return dir, nil
}

// readIfThere reads a file, nil if it isn't there.
func readIfThere(path string) ([]byte, error) {
	b, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	return b, err
}

// Read adopts the symbols and footprints the user changed in Altium.
func (altiumFormat) Read(l *Library) ([]string, error) {
	_, schPath, pcbPath := altiumPaths(l)
	var adopted []string
	if b, err := readIfThere(pcbPath); err != nil {
		return nil, err
	} else if b != nil {
		fps, err := altium.ReadPcbLib(b)
		if err != nil {
			return nil, err
		}
		got := map[string]altium.ReadFootprint{}
		for _, f := range fps {
			got[f.Footprint.Name] = f
		}
		for _, it := range l.Items {
			if it.Kind != "footprint" || it.Footprint == nil || it.Written[altiumFile] == "" {
				continue
			}
			f, ok := got[it.Footprint.Name]
			if !ok {
				continue
			}
			if p := cad.FootprintPrint(f.Footprint); p != it.Written[altiumFile] {
				raw, _ := json.Marshal(f.Raw)
				fp := f.Footprint
				fp.Name = it.Footprint.Name
				it.Footprint = &fp
				it.adopt(altiumFile, string(raw), p)
				adopted = append(adopted, "footprint "+it.Name+" (Altium)")
			}
		}
	}
	if b, err := readIfThere(schPath); err != nil {
		return nil, err
	} else if b != nil {
		syms, err := altium.ReadSchLib(b)
		if err != nil {
			return nil, err
		}
		got := map[string]altium.ReadSymbol{}
		for _, s := range syms {
			got[s.Symbol.Name] = s
		}
		for _, it := range l.Items {
			if it.Kind != "symbol" || it.Symbol == nil || it.Written[altiumFile] == "" {
				continue
			}
			s, ok := got[it.Symbol.Name]
			if !ok {
				continue
			}
			if p := cad.SymbolPrint(s.Symbol); p != it.Written[altiumFile] {
				raw, _ := json.Marshal(s.Raw)
				sy := s.Symbol
				sy.Name = it.Symbol.Name
				it.Symbol = &sy
				it.adopt(altiumFile, string(raw), p)
				adopted = append(adopted, "symbol "+it.Name+" (Altium)")
			}
		}
	}
	return adopted, nil
}

// Decode is the symbol or footprint an Altium edit holds.
func (altiumFormat) Decode(kind, raw string) (*cad.Symbol, *cad.Footprint, error) {
	if kind == "symbol" {
		var streams map[string][]byte
		if err := json.Unmarshal([]byte(raw), &streams); err != nil {
			return nil, nil, err
		}
		s, _, err := altium.DecodeSymbol(streams)
		return &s, nil, err
	}
	var r altium.Raw
	if err := json.Unmarshal([]byte(raw), &r); err != nil {
		return nil, nil, err
	}
	f, err := altium.DecodeFootprint(r)
	return nil, &f, err
}
