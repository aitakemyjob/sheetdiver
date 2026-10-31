package cadlib

import (
	"encoding/json"
	"fmt"

	"github.com/aitakemyjob/sheetdiver/internal/cad"
)

// ModelKey is the key an edit made in SheetDiver's own editor is kept
// under: the model itself, as JSON.
const ModelKey = "sheetdiver"

func init() { RegisterDecoder(ModelKey, modelDecoder{}) }

// modelDecoder reads back an edit made in SheetDiver.
type modelDecoder struct{}

// Decode is the symbol or footprint an edit made in SheetDiver holds.
func (modelDecoder) Decode(kind, raw string) (*cad.Symbol, *cad.Footprint, error) {
	switch kind {
	case "symbol":
		var s cad.Symbol
		if err := json.Unmarshal([]byte(raw), &s); err != nil {
			return nil, nil, err
		}
		return &s, nil, nil
	case "footprint":
		var f cad.Footprint
		if err := json.Unmarshal([]byte(raw), &f); err != nil {
			return nil, nil, err
		}
		f.From = "your edit in SheetDiver"
		return nil, &f, nil
	}
	return nil, nil, fmt.Errorf("kind %q isn't symbol or footprint", kind)
}

// EditedIn is where an edited item's edit was made: a format's name, or
// ModelKey for SheetDiver's own editor; "" when it isn't edited.
func EditedIn(raw map[string]string) string {
	if _, ok := raw[ModelKey]; ok {
		return ModelKey
	}
	for _, f := range []string{"kicad", "altium", "eagle"} {
		if _, ok := raw[f]; ok {
			return f
		}
	}
	for k := range raw {
		return k
	}
	return ""
}
