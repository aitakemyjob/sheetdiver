package cadlib

import (
	"os"
	"path/filepath"
	"sort"

	"github.com/aitakemyjob/sheetdiver/internal/cad/eagle"
)

// The Eagle library: eagle/<name>.lbr, each part's symbol on its
// footprint. Fusion Electronics opens it; Altium and others import it.
// Edits made there aren't read back: edit in KiCad, or edit the library
// in place and keep it out of SheetDiver's formats.
func init() { Register(eagleFormat{}) }

// eagleFormat is Eagle 9 / Fusion Electronics XML.
type eagleFormat struct{}

// Name is the format's name.
func (eagleFormat) Name() string { return "eagle" }

// Label is its name for people.
func (eagleFormat) Label() string { return "Eagle / Fusion" }

// Write writes the library: a device for each symbol that has a
// footprint.
func (eagleFormat) Write(l *Library) (string, error) {
	dir := filepath.Join(l.Dir, "eagle")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	fps := l.footprints()
	var parts []eagle.Part
	for _, it := range l.Items {
		if it.Kind != "symbol" || it.Symbol == nil {
			continue
		}
		if f, ok := fps[it.Key]; ok {
			parts = append(parts, eagle.Part{Symbol: *it.Symbol, Footprint: *f.Footprint})
		}
	}
	sort.Slice(parts, func(i, j int) bool { return parts[i].Symbol.Name < parts[j].Symbol.Name })
	path := filepath.Join(dir, l.Name+".lbr")
	return path, writeIfChanged(path, eagle.Library(l.Name, parts))
}
