// Package cadlib keeps the user's CAD library in step with SheetDiver:
// it writes each part's symbol, footprint and 3D model in every format
// the user keeps (KiCad, Eagle…) and, before writing, reads back what the
// user changed in their CAD tool, so their edits are kept rather than
// overwritten. Formats are pluggable: a new tool is a new Format.
package cadlib

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/aitakemyjob/sheetdiver/internal/cad"
)

// State is where a symbol or footprint came from, which decides whether
// SheetDiver may redraw it.
type State string

// The states.
const (
	// Auto is made by SheetDiver from the pin table or a standard
	// package; it is redrawn whenever they change.
	Auto State = "auto"
	// Drawn is drawn by the agent from the datasheet; it is redrawn from
	// the agent's spec when the pin table changes.
	Drawn State = "drawn"
	// Edited is changed by the user, in a CAD tool or in SheetDiver's own
	// editor; it is kept as they left it until they reset it.
	Edited State = "edited"
)

// next are the states each can move to.
var next = map[State][]State{
	Auto:   {Auto, Drawn, Edited},
	Drawn:  {Drawn, Edited, Auto},
	Edited: {Edited, Auto},
}

// ErrEdited is the refusal to redraw what the user edited.
type ErrEdited struct{ Name string }

// Error says what to do.
func (e ErrEdited) Error() string {
	return fmt.Sprintf("%s was edited in a CAD tool and is kept as the user left it: reset it first if they agree to redraw it", e.Name)
}

// To checks a move from one state to another.
func (s State) To(n State, name string) error {
	for _, ok := range next[s] {
		if ok == n {
			return nil
		}
	}
	if s == Edited {
		return ErrEdited{name}
	}
	return fmt.Errorf("%s can't go from %s to %s", name, s, n)
}

// Item is a symbol or a footprint (with its 3D body) in the library.
type Item struct {
	// Kind is symbol or footprint.
	Kind string
	// Name is its name in the library.
	Name string
	// Key pairs a part's symbol with its footprint (part and package).
	Key   string
	State State
	// Symbol or Footprint and Solids, as drawn or as read back from an
	// edit.
	Symbol    *cad.Symbol
	Footprint *cad.Footprint
	Solids    []cad.Solid
	// Raw is what the user left in each format's file; Written is the
	// fingerprint of what was last written to each file.
	Raw     map[string]string
	Written map[string]string
	// Updated dates the model, so a file is only rewritten when it
	// changed.
	Updated time.Time
	// Changed says the item needs saving after a sync.
	Changed bool
}

// adopt takes the user's edit in a format: kept as raw, the model read
// back from it.
func (it *Item) adopt(format, raw, fingerprint string) {
	// The newest edit wins: what was kept from another tool goes, and
	// that tool's file is rewritten from this one.
	it.Raw = it.keep3D(map[string]string{format: raw})
	if it.Written == nil {
		it.Written = map[string]string{}
	}
	it.Written[format] = fingerprint
	it.State = Edited
	it.Changed = true
}

// Edit takes the user's edit made in SheetDiver's own editor: kept as the
// model itself, every format's file rewritten from it.
func (it *Item) Edit(raw string) {
	it.Raw = it.keep3D(map[string]string{ModelKey: raw})
	it.State = Edited
	it.Changed = true
}

// wrote records what was written to a file.
func (it *Item) wrote(file, fingerprint string) {
	if it.Written == nil {
		it.Written = map[string]string{}
	}
	if it.Written[file] != fingerprint {
		it.Written[file] = fingerprint
		it.Changed = true
	}
}

// Library is the user's library: where it is kept, its name, and its
// items.
type Library struct {
	Dir   string
	Name  string
	Items []*Item
}

// Footprints are the library's footprints by key.
func (l *Library) footprints() map[string]*Item {
	out := map[string]*Item{}
	for _, it := range l.Items {
		if it.Kind == "footprint" && it.Footprint != nil {
			out[it.Key] = it
		}
	}
	return out
}

// Format writes the library for one CAD tool.
type Format interface {
	// Name is the format's name in settings (kicad, eagle).
	Name() string
	// Label is its name for people.
	Label() string
	// Write writes the library; it returns where.
	Write(l *Library) (string, error)
}

// Reader is a format that can read the user's edits back.
type Reader interface {
	// Read adopts what the user changed since the last write.
	Read(l *Library) ([]string, error)
}

// Decoder is a format that can turn what it kept of an edit back into
// the model.
type Decoder interface {
	// Decode is the symbol or footprint (by kind) a raw edit holds.
	Decode(kind, raw string) (*cad.Symbol, *cad.Footprint, error)
}

// Decode is the symbol or footprint the user's kept edit holds, from
// whichever tool they made it in.
func Decode(kind string, raw map[string]string) (*cad.Symbol, *cad.Footprint) {
	names := make([]string, 0, len(raw))
	for n := range raw {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		if d, ok := decoders[n]; ok {
			if s, f, err := d.Decode(kind, raw[n]); err == nil {
				return s, f
			}
		}
	}
	return nil, nil
}

// formats are the known formats by name; decoders are what can read a
// kept edit back, by the key it's kept under.
var (
	formats  = map[string]Format{}
	decoders = map[string]Decoder{}
)

// Register adds a format.
func Register(f Format) {
	formats[f.Name()] = f
	if d, ok := f.(Decoder); ok {
		decoders[f.Name()] = d
	}
}

// RegisterDecoder adds a way to read back an edit kept under a key that
// isn't a library format's: one made in SheetDiver itself.
func RegisterDecoder(key string, d Decoder) { decoders[key] = d }

// FormatInfo describes a format.
type FormatInfo struct {
	Name  string `json:"name"`
	Label string `json:"label"`
	// ReadsEdits says edits made in the tool are read back.
	ReadsEdits bool `json:"readsEdits"`
}

// Formats are the known formats.
func Formats() []FormatInfo {
	var out []FormatInfo
	for _, f := range formats {
		_, r := f.(Reader)
		out = append(out, FormatInfo{f.Name(), f.Label(), r})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// Report says what a sync did.
type Report struct {
	Dir string `json:"dir"`
	// Written are the folders or files written, by format.
	Written map[string]string `json:"written"`
	// Adopted are the items the user edited, now kept as they left them.
	Adopted []string `json:"adopted,omitempty"`
	// Symbols and Footprints are how many were written.
	Symbols    int `json:"symbols"`
	Footprints int `json:"footprints"`
}

// Sync reads the user's edits in each format that can, then writes every
// format.
func Sync(l *Library, names []string) (Report, error) {
	rep := Report{Dir: l.Dir, Written: map[string]string{}}
	var fs []Format
	for _, n := range names {
		f, ok := formats[strings.ToLower(strings.TrimSpace(n))]
		if !ok {
			return rep, fmt.Errorf("unknown library format %q", n)
		}
		fs = append(fs, f)
	}
	for _, f := range fs {
		if r, ok := f.(Reader); ok {
			got, err := r.Read(l)
			if err != nil {
				return rep, fmt.Errorf("reading the %s library: %w", f.Label(), err)
			}
			rep.Adopted = append(rep.Adopted, got...)
		}
	}
	for _, f := range fs {
		where, err := f.Write(l)
		if err != nil {
			return rep, fmt.Errorf("writing the %s library: %w", f.Label(), err)
		}
		rep.Written[f.Name()] = where
	}
	for _, it := range l.Items {
		switch it.Kind {
		case "symbol":
			rep.Symbols++
		case "footprint":
			rep.Footprints++
		}
	}
	return rep, nil
}

// Reset is the state after dropping the newest layer of work: an edit
// goes back to the agent's drawing (or SheetDiver's when there is none),
// a drawing back to SheetDiver's.
func (s State) Reset(drawn bool) State {
	if s == Edited && drawn {
		return Drawn
	}
	return Auto
}
