package cadlib

import (
	"os"
	"path/filepath"
	"sort"

	"github.com/aitakemyjob/sheetdiver/internal/cad"
	"github.com/aitakemyjob/sheetdiver/internal/cad/altium"
)

// The Altium library: altium/<name>.SchLib and altium/<name>.PcbLib, each
// symbol linked to its footprint. Edits made in Altium aren't read back
// yet: edit in KiCad, or keep the library out of SheetDiver's formats.
func init() { Register(altiumFormat{}) }

// altiumFormat is Altium Designer's binary libraries.
type altiumFormat struct{}

// Name is the format's name.
func (altiumFormat) Name() string { return "altium" }

// Label is its name for people.
func (altiumFormat) Label() string { return "Altium" }

// Write writes the schematic and footprint libraries.
func (altiumFormat) Write(l *Library) (string, error) {
	dir := filepath.Join(l.Dir, "altium")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	fpByKey := l.footprints()
	var fps []cad.Footprint
	heights := map[string]float64{}
	for _, it := range l.Items {
		if it.Kind == "footprint" && it.Footprint != nil {
			fps = append(fps, *it.Footprint)
			if len(it.Solids) > 0 {
				_, hi := cad.Bounds(it.Solids)
				heights[it.Footprint.Name] = hi[2]
			}
		}
	}
	var syms []altium.Symbol
	for _, it := range l.Items {
		if it.Kind == "symbol" && it.Symbol != nil {
			s := altium.Symbol{Symbol: *it.Symbol}
			if f, ok := fpByKey[it.Key]; ok {
				s.Footprint = f.Footprint.Name
			}
			syms = append(syms, s)
		}
	}
	sort.Slice(fps, func(i, j int) bool { return fps[i].Name < fps[j].Name })
	sort.Slice(syms, func(i, j int) bool { return syms[i].Symbol.Name < syms[j].Symbol.Name })
	pcb, err := altium.PcbLib(l.Name, fps, heights)
	if err != nil {
		return "", err
	}
	sch, err := altium.SchLib(l.Name, syms)
	if err != nil {
		return "", err
	}
	if err := writeIfChanged(filepath.Join(dir, l.Name+".PcbLib"), pcb); err != nil {
		return "", err
	}
	return dir, writeIfChanged(filepath.Join(dir, l.Name+".SchLib"), sch)
}
