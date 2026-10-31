package core

import (
	"context"
	"strings"
	"testing"

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
