package core

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"strings"
	"testing"

	"github.com/aitakemyjob/sheetdiver/internal/cad"
	"github.com/aitakemyjob/sheetdiver/internal/library"
)

// f is a value's address.
func f(v float64) *float64 { return &v }

func TestRecordPackageDrawsFootprint(t *testing.T) {
	s := newService(t)
	ctx := context.Background()
	p := cadTestPart(t, s)

	// TI's DDC drawing has no table: the agent reads it and records it.
	rec := library.PackageDims{Name: "SOT-23-6", Code: "DDC0006A", Page: 30, Dims: []library.PackageDim{
		{Symbol: "D", Key: "bodyLength", Min: f(2.75), Max: f(3.05)},
		{Symbol: "E1", Key: "bodyWidth", Min: f(1.45), Max: f(1.75)},
		{Symbol: "E", Key: "span", Min: f(2.55), Max: f(3.05)},
		{Symbol: "e", Key: "pitch", Nom: f(0.95), Note: "BSC"},
		{Symbol: "b", Key: "leadWidth", Min: f(0.3), Max: f(0.5)},
		{Symbol: "L", Key: "leadLength", Min: f(0.3), Max: f(0.6)},
		{Symbol: "A", Key: "height", Max: f(1.1)},
	}, Land: []library.PackageDim{
		{Symbol: "Y", Key: "padLength", Nom: f(1.1)},
		{Symbol: "X", Key: "padWidth", Nom: f(0.6)},
		{Symbol: "C", Key: "rowSpan", Nom: f(2.7)},
	}}
	pp, err := s.RecordPackage(ctx, p.MPN, rec, library.SourceAgent)
	if err != nil {
		t.Fatal(err)
	}
	if len(pp.Packages) != 1 || pp.Packages[0].Pins != 6 || len(pp.Packages[0].Checks) != 0 || pp.Packages[0].Dims[0].Source != library.SourceAgent {
		t.Fatalf("packages %+v", pp.Packages)
	}

	pc, err := s.PartCad(ctx, p.MPN)
	if err != nil {
		t.Fatal(err)
	}
	fp := pc.Packages[0].Footprint
	if fp == nil || fp.Standard || fp.DatasheetPage != 31 || fp.Package.Height.Max != 1.1 || fp.Package.Land == nil || fp.Package.Land.RowSpan != 2.7 {
		t.Fatalf("footprint %+v", fp)
	}

	// The extractor's reading of the same package fills in what the agent
	// left out, and the agent's values win.
	auto := library.PackageDims{DatasheetID: p.DatasheetID, Name: "SOT-23-6", Source: library.SourceAuto, Dims: []library.PackageDim{
		{Symbol: "A", Key: "height", Max: f(1.45)},
		{Symbol: "A1", Key: "standoff", Min: f(0), Max: f(0.1)},
	}}
	merged := mergePackageRows([]library.PackageDims{auto, {Name: "SOT-23-6", Source: library.SourceAgent, Dims: rec.Dims}})
	if len(merged) != 1 || len(merged[0].Dims) != 8 {
		t.Fatalf("merged %+v", merged)
	}
	for _, d := range merged[0].Dims {
		if d.Key == "height" && (*d.Max != 1.1 || d.Source != library.SourceAgent) {
			t.Errorf("height %+v: the agent's should win", d)
		}
	}

	// Checks: a nominal outside its limits, pins that don't fit the body,
	// pads that end inside the body.
	bad := PartPackage{Name: "SOT-23-6", Pins: 6, Dims: []PackageDim{
		{library.PackageDim{Symbol: "D", Key: "bodyLength", Min: f(2.75), Nom: f(4), Max: f(3.05)}, "auto"},
		{library.PackageDim{Symbol: "e", Key: "pitch", Nom: f(2)}, "auto"},
		{library.PackageDim{Symbol: "E1", Key: "bodyWidth", Max: f(1.75)}, "auto"},
	}, Land: []PackageDim{
		{library.PackageDim{Symbol: "C", Key: "rowSpan", Nom: f(1.0)}, "auto"},
		{library.PackageDim{Symbol: "Y", Key: "padLength", Nom: f(0.5)}, "auto"},
	}}
	got := strings.Join(checkPackage(bad), "\n")
	for _, want := range []string{"D: nominal", "pitch take", "inside the 1.75 mm body"} {
		if !strings.Contains(got, want) {
			t.Errorf("checks %q lack %q", got, want)
		}
	}
}

func TestLandOf(t *testing.T) {
	// Diodes' SOT25: Z and G give the rows' span.
	pp := PartPackage{Land: []PackageDim{
		{library.PackageDim{Key: "outerSpan", Nom: f(3.2)}, "auto"},
		{library.PackageDim{Key: "gap", Nom: f(1.6)}, "auto"},
		{library.PackageDim{Key: "padWidth", Nom: f(0.55)}, "auto"},
		{library.PackageDim{Key: "padLength", Nom: f(0.8)}, "auto"},
	}}
	land, ok := landOf(pp, 0.95)
	if !ok || land.RowSpan != 2.4 || land.PadWidth != 0.55 || land.Pitch != 0.95 {
		t.Errorf("land %+v %v", land, ok)
	}
	// A C equal to the pitch isn't the rows' span.
	pp.Land = append(pp.Land[2:], PackageDim{library.PackageDim{Key: "rowSpan", Nom: f(0.95)}, "auto"})
	if _, ok := landOf(pp, 0.95); ok {
		t.Error("C equal to the pitch taken for the rows' span")
	}
}

func TestNoPackagesIsAnEmptyList(t *testing.T) {
	s := newService(t)
	pp, err := s.PartPackages(context.Background(), "SD1234")
	if err != nil {
		t.Fatal(err)
	}
	b, _ := json.Marshal(pp)
	if !strings.Contains(string(b), `"packages":[]`) {
		t.Errorf("a part with no package dimensions: %s", b)
	}
}

func TestPinCountWithoutNumbers(t *testing.T) {
	pins := []library.Pin{{Name: "G"}, {Name: "S"}, {Name: "D"}, {Number: "1", Name: "A"}, {Number: "1", Name: "A2"}}
	if n := pinCount(pins, "SOT-23", true); n != 4 {
		t.Errorf("%d pins, want 4 (three unnumbered, one number twice)", n)
	}
}

func TestFamilyOf(t *testing.T) {
	for name, want := range map[string]string{"UFQFPN48": "qfn", "LQFP48": "qfp", "QFN-16": "qfn", "UFBGA64": "bga", "WLCSP56": "bga", "SOIC-8": "gullwing"} {
		if got := familyOf(name); got != want {
			t.Errorf("familyOf(%s) = %q, want %q", name, got, want)
		}
	}
}

func TestPackagesByOrderingScheme(t *testing.T) {
	s := newService(t)
	ctx := context.Background()
	p := cadTestPart(t, s) // pins recorded for SOT-23-6
	dfn := func(name string, parts ...string) library.PackageDims {
		return library.PackageDims{Name: name, Page: 31, Parts: parts, Dims: []library.PackageDim{
			{Symbol: "D", Key: "bodyLength", Nom: f(2)}, {Symbol: "E", Key: "bodyWidth", Nom: f(2)},
			{Symbol: "e", Key: "pitch", Nom: f(0.65)}, {Symbol: "b", Key: "leadWidth", Nom: f(0.3)},
			{Symbol: "L", Key: "leadLength", Nom: f(0.3)}, {Symbol: "A", Key: "height", Max: f(0.8)},
		}}
	}
	// The ordering scheme gives the part three packages, and a sibling
	// (with its own pin table) the fourth.
	for _, rec := range []library.PackageDims{
		{Name: "SOT-23-6", Parts: []string{p.MPN}, Dims: []library.PackageDim{{Symbol: "e", Key: "pitch", Nom: f(0.95)}}}, dfn("WSON-6", p.MPN), dfn("WSON-8", "SD9999"),
		{Name: "DSBGA-6", Parts: []string{p.MPN}, Dims: []library.PackageDim{{Symbol: "e", Key: "pitch", Nom: f(0.4)}}},
	} {
		if _, err := s.RecordPackage(ctx, p.MPN, rec, library.SourceAgent); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.RecordPins(ctx, p.DatasheetID, "WSON-8", []library.Pin{{Number: "1", Name: "X"}}, library.SourceAgent); err != nil {
		t.Fatal(err)
	}
	pins, _ := s.PartPins(ctx, p, "")
	for _, pin := range pins {
		if pin.Package == "WSON-8" {
			t.Errorf("the sibling's WSON-8 pins apply to %s", p.MPN)
		}
	}
	pp, _ := s.PartPackages(ctx, p.MPN)
	if len(pp.Packages) != 3 {
		t.Errorf("package tab %+v, want its three", pp.Packages)
	}

	// What SheetDiver kept for the sibling's package before the scheme
	// was read is stale; what an agent drew stays.
	for _, it := range []library.CadItem{
		{PartID: p.ID, Kind: library.CadFootprint, Package: "WSON-8", State: "auto", Source: library.SourceAuto},
		{PartID: p.ID, Kind: library.CadFootprint, Package: "SOIC-8", State: "drawn", Source: library.SourceAgent, Spec: []byte(`{"family":"gullwing","pins":8}`)},
	} {
		if _, err := s.Lib.SaveCadItem(ctx, it); err != nil {
			t.Fatal(err)
		}
	}
	pc, err := s.PartCad(ctx, p.MPN)
	if err != nil {
		t.Fatal(err)
	}
	by := map[string]CadPackage{}
	for _, pk := range pc.Packages {
		by[pk.Package] = pk
	}
	// Its other 6-pin package numbered round its edge takes the pin
	// table's numbers and is drawn; the sibling's isn't its.
	if w := by["WSON-6"]; w.PinsFrom != "SOT-23-6" || w.Pins != 6 || w.Footprint == nil || w.Footprint.Footprint == nil || w.Symbol == nil {
		t.Errorf("WSON-6 %+v", w)
	}
	if _, ok := by["WSON-8"]; ok {
		t.Error("WSON-8 is the sibling's")
	}
	// One with no pins of its own and none to take still has its pads 1 to
	// N round its edges: drawn, without a symbol.
	if err := s.RecordPins(ctx, p.DatasheetID, "SOT-23-6", nil, library.SourceAgent); err != nil {
		t.Fatal(err)
	}
	bare, _ := s.PartCad(ctx, p.MPN)
	for _, pk := range bare.Packages {
		if pk.Package == "WSON-6" && (pk.Symbol != nil || pk.Footprint == nil || pk.Footprint.Footprint == nil || len(pk.Footprint.Footprint.Pads) != 6) {
			t.Errorf("WSON-6 without pins %+v", pk)
		}
	}
	// A ball grid's balls aren't the pin table's numbers.
	if b := by["DSBGA-6"]; b.PinsFrom != "" || b.Pins != 0 || !strings.Contains(b.Missing, "record its pins") {
		t.Errorf("DSBGA-6 %+v", b)
	}
	if _, ok := by["SOIC-8"]; !ok || len(pc.Packages) != 4 {
		t.Errorf("CAD packages %d, want SOT-23-6, WSON-6, DSBGA-6 and the agent's SOIC-8", len(pc.Packages))
	}
}

func TestStaggeredBalls(t *testing.T) {
	// A WLCSP's map: three rows, alternate ones on the odd and even
	// columns, 0.4 mm between neighbours; the drawing gives the spans.
	var pins []library.Pin
	for r, row := range []string{"A", "B", "C"} {
		for c := 1; c <= 7; c++ {
			if (r+c)%2 == 1 {
				pins = append(pins, library.Pin{Number: fmt.Sprintf("%s%d", row, c)})
			}
		}
	}
	pp := PartPackage{Name: "WLCSP11", Dims: []PackageDim{
		{library.PackageDim{Symbol: "e1", Nom: f(1.2)}, "auto"}, {library.PackageDim{Symbol: "e2", Nom: f(0.6928)}, "auto"},
	}}
	sp := cad.Package{Family: "bga", Pins: len(pins), Pitch: 0.4, BodyLength: cad.Dim{Nom: 1.5}, BodyWidth: cad.Dim{Nom: 1.5}, LeadWidth: cad.Dim{Nom: 0.25}, Height: cad.Dim{Nom: 0.5}}
	var probs []cad.Problem
	gridOf(&sp, pp, pins, &probs)
	if len(probs) > 0 || len(sp.Pads) != len(pins) {
		t.Fatalf("pads %d, problems %v", len(sp.Pads), probs)
	}
	// A1 top left, columns 0.2 apart across, rows 0.346 apart down.
	a1, b2 := sp.Pads[0], sp.Pads[4]
	if a1.Number != "A1" || !near(a1.X, -0.6) || !near(a1.Y, -0.3464) || b2.Number != "B2" || !near(b2.X-a1.X, 0.2) || !near(b2.Y-a1.Y, 0.3464) {
		t.Errorf("A1 %+v, B2 %+v", a1, b2)
	}
	if fp, ps := cad.MakeFootprint("WLCSP11", sp); cad.Err(ps) != nil || len(fp.Pads) != len(pins) {
		t.Errorf("footprint %d pads, %v", len(fp.Pads), ps)
	}
}

// near reports whether two lengths agree to a micron.
func near(a, b float64) bool { return math.Abs(a-b) < 0.001 }

func TestVariantPins(t *testing.T) {
	pins := []library.Pin{
		{Number: "1", Name: "dVdt"},
		{Number: "2", Name: "EN/UVLO", Description: "Active high enable for the TPS2595x0, TPS2595x1, TPS2595x5 variants."},
		{Number: "2", Name: "EN/OVLO", Description: "Active low enable for the TPS2595x3 variants."},
		{Number: "6", Name: "FLT", Description: "TPS2595x0, TPS2595x1, TPS2595x3: Fault event indicator."},
		{Number: "6", Name: "QOD", Description: "TPS2595x5: Quick Output Discharge."},
	}
	got := variantPins(pins, []string{"TPS259520", "TPS259533DSG", "TPS259575"})
	tables := map[string]string{}
	for _, p := range got {
		tables[p.Part] += p.Number + " " + p.Name + "; "
	}
	for part, want := range map[string]string{
		"TPS259520":    "1 dVdt; 2 EN/UVLO; 6 FLT; ",
		"TPS259533DSG": "1 dVdt; 2 EN/OVLO; 6 FLT; ",
		"TPS259575":    "1 dVdt; 2 EN/UVLO; 6 QOD; ",
	} {
		if tables[part] != want {
			t.Errorf("%s: %q, want %q", part, tables[part], want)
		}
	}
	// No variant pins: one table for every part.
	plain := []library.Pin{{Number: "1", Name: "VIN"}, {Number: "2", Name: "GND"}}
	if got := variantPins(plain, []string{"A1", "A2"}); len(got) != 2 || got[0].Part != "" {
		t.Errorf("plain %+v", got)
	}
}
