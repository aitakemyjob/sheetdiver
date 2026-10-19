package main

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/aitakemyjob/sheetdiver/internal/extract"
	"github.com/aitakemyjob/sheetdiver/internal/pdf"
)

// runInspect prints what extraction finds in a PDF without storing
// anything; useful when improving the extractor.
func runInspect(files []string) error {
	if len(files) != 1 {
		return fmt.Errorf("give one PDF to inspect")
	}
	data, err := os.ReadFile(files[0])
	if err != nil {
		return err
	}
	e := pdf.NewEngine(pdf.DefaultCacheDir())
	defer e.Close()
	doc, err := e.Extract(context.Background(), data, nil)
	if err != nil {
		return err
	}
	r := extract.Analyze(doc, files[0])
	fmt.Printf("title:        %s\nmanufacturer: %s\ncategory:     %s\nparts:        %s\nfamilies:     %s\npages:        %d (body text %.1f pt)\n",
		r.Title, r.Manufacturer, r.Category, strings.Join(r.PartNumbers, ", "), strings.Join(r.Families, ", "), len(doc.Pages), r.BodySize)
	if len(r.Variants) > 0 {
		var vs []string
		for _, v := range r.Variants {
			vs = append(vs, v.MPN+"→"+v.Base)
		}
		fmt.Printf("variants:     %s\n", strings.Join(vs, ", "))
	}
	fmt.Printf("\nsections (%d):\n", len(r.Sections))
	for _, s := range r.Sections {
		fmt.Printf("  %sp%-3d %s %s [%s]\n", strings.Repeat("  ", max(0, s.Level-1)), s.Page+1, s.Num, s.Title, s.Kind)
	}
	fmt.Printf("\nfigures (%d):\n", len(r.Figures))
	for _, f := range r.Figures {
		axes := ""
		if f.YAxis != "" {
			axes = fmt.Sprintf(" (y %s, x %s)", f.YAxis, f.XAxis)
		}
		fmt.Printf("  p%-3d %-7s [%s] %s%s\n", f.Page+1, f.Number, f.Kind, f.Caption, axes)
	}
	fmt.Printf("\nparameters (%d):\n", len(r.Params))
	for _, p := range r.Params {
		v := func(x *float64) string {
			if x == nil {
				return "–"
			}
			return fmt.Sprintf("%g", *x)
		}
		fmt.Printf("  p%-3d %-12s %-9s %s %s | %s/%s/%s %s | %s\n", p.Page+1, p.Section, p.Key, p.Symbol, p.Name, v(p.Min), v(p.Typ), v(p.Max), p.Unit, p.Conditions)
	}
	if len(r.Pins) > 0 {
		fmt.Printf("\npins (%d):\n", len(r.Pins))
		for _, p := range r.Pins {
			pkg := ""
			if p.Package != "" {
				pkg = "[" + p.Package + "] "
			}
			fmt.Printf("  %s%-6s %-8s %-4s %s\n", pkg, p.Number, p.Name, p.Type, p.Description)
		}
	}
	fmt.Printf("\ntables (%d):\n", len(r.Tables))
	for _, t := range r.Tables {
		irr := ""
		if t.Irregular != "" {
			irr = " IRREGULAR: " + t.Irregular
		}
		fmt.Printf("  p%-3d [%s] %s %s | %s (%d rows)%s\n", t.Page+1, t.Kind, t.Number, t.Caption, strings.Join(t.Header, ", "), len(t.Rows), irr)
	}
	return nil
}
