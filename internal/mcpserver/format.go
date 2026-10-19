package mcpserver

import (
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/aitakemyjob/sheetdiver/internal/core"
	"github.com/aitakemyjob/sheetdiver/internal/library"
)

// num formats an optional value ("–" when absent).
func num(v *float64) string {
	if v == nil {
		return "–"
	}
	return strconv.FormatFloat(*v, 'g', 6, 64)
}

// paramLine renders a parameter on one line with its id and page.
func paramLine(p library.Parameter) string {
	var b strings.Builder
	fmt.Fprintf(&b, "#%d ", p.ID)
	if p.Key != "" {
		fmt.Fprintf(&b, "[%s] ", p.Key)
	}
	label := strings.TrimSpace(p.Symbol + " " + p.Name)
	if label == "" {
		label = p.Raw
	}
	b.WriteString(label)
	fmt.Fprintf(&b, ": min %s / typ %s / max %s %s", num(p.Min), num(p.Typ), num(p.Max), p.Unit)
	if p.Conditions != "" {
		fmt.Fprintf(&b, " | %s", p.Conditions)
	}
	fmt.Fprintf(&b, " | %s p%d", p.Section, p.Page+1)
	if len(p.AppliesTo) > 0 {
		fmt.Fprintf(&b, " | only %s", strings.Join(p.AppliesTo, ","))
	}
	if p.Source != library.SourceAuto {
		fmt.Fprintf(&b, " | %s", p.Source)
	}
	if p.Note != "" {
		fmt.Fprintf(&b, " | note: %s", p.Note)
	}
	return b.String()
}

// figureLine renders a figure on one line.
func figureLine(f library.Figure) string {
	var b strings.Builder
	fmt.Fprintf(&b, "#%d Figure %s p%d [%s] %s", f.ID, f.Number, f.Page+1, f.Kind, f.Caption)
	if f.YAxis != "" || f.XAxis != "" {
		fmt.Fprintf(&b, " (y: %s, x: %s)", f.YAxis, f.XAxis)
	}
	if f.Conditions != "" {
		fmt.Fprintf(&b, " | %s", f.Conditions)
	}
	if len(f.AppliesTo) > 0 {
		fmt.Fprintf(&b, " | only %s", strings.Join(f.AppliesTo, ","))
	}
	if f.Source != library.SourceAuto {
		fmt.Fprintf(&b, " | described by %s", f.Source)
	}
	return b.String()
}

// figureDetail renders everything known about a figure.
func figureDetail(f library.Figure) string {
	var b strings.Builder
	b.WriteString(figureLine(f) + "\n")
	fmt.Fprintf(&b, "section: %s\nregion (pt, top-left origin): %.0f,%.0f – %.0f,%.0f\n", f.Section, f.Box.X0, f.Box.Y0, f.Box.X1, f.Box.Y1)
	if f.Title != "" {
		fmt.Fprintf(&b, "title: %s\n", f.Title)
	}
	if f.Description != "" {
		fmt.Fprintf(&b, "description: %s\n", f.Description)
	}
	for _, c := range f.Curves {
		fmt.Fprintf(&b, "curve %s: %s %s\n", c.Label, c.Conditions, c.Summary)
	}
	return b.String()
}

// partHeader renders a part's identity line.
func partHeader(p library.Part) string {
	s := p.MPN
	if p.Manufacturer != "" {
		s += " · " + p.Manufacturer
	}
	if p.Category != "" {
		s += " · " + p.Category
	}
	return s
}

// partDetail renders a part overview.
func partDetail(d core.PartDetail) string {
	var b strings.Builder
	b.WriteString(partHeader(d.Part) + "\n")
	if d.Via != "mpn" && d.Via != "id" {
		fmt.Fprintf(&b, "(matched %q by %s)\n", d.Matched, d.Via)
	}
	if d.Part.Description != "" {
		b.WriteString(d.Part.Description + "\n")
	}
	if d.Datasheet != nil {
		ds := d.Datasheet
		fmt.Fprintf(&b, "datasheet #%d: %s (%d pages) %s\n", ds.ID, ds.Filename, ds.Pages, ds.Title)
		if len(ds.Families) > 0 {
			fmt.Fprintf(&b, "families: %s\n", strings.Join(ds.Families, ", "))
		}
	} else {
		b.WriteString("no datasheet imported\n")
	}
	if len(d.Siblings) > 0 {
		fmt.Fprintf(&b, "same datasheet: %s\n", strings.Join(d.Siblings, ", "))
	}
	if len(d.Variants) > 0 {
		var vs []string
		for i, v := range d.Variants {
			if i == 30 {
				vs = append(vs, fmt.Sprintf("… %d more", len(d.Variants)-30))
				break
			}
			vs = append(vs, v.MPN)
		}
		fmt.Fprintf(&b, "orderable variants: %s\n", strings.Join(vs, ", "))
	}
	if len(d.Counts) > 0 {
		keys := make([]string, 0, len(d.Counts))
		for k := range d.Counts {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		var parts []string
		for _, k := range keys {
			parts = append(parts, fmt.Sprintf("%s %d", k, d.Counts[k]))
		}
		fmt.Fprintf(&b, "extracted: %s\n", strings.Join(parts, ", "))
	}
	if len(d.FigureKinds) > 0 {
		var parts []string
		for k, n := range d.FigureKinds {
			parts = append(parts, fmt.Sprintf("%s %d", k, n))
		}
		sort.Strings(parts)
		fmt.Fprintf(&b, "figures by kind: %s\n", strings.Join(parts, ", "))
	}
	if len(d.Key) > 0 {
		b.WriteString("\nkey parameters (best value per key; ratings are absolute maximum):\n")
		for _, k := range d.Key {
			tag := ""
			if k.Rating {
				tag = " (rating)"
			}
			fmt.Fprintf(&b, "  %s%s: %s\n", k.Key, tag, paramLine(k.Param))
		}
	}
	if len(d.Recommended) > 0 {
		b.WriteString("\nrecommended components:\n")
		for _, r := range d.Recommended {
			fmt.Fprintf(&b, "  %s\n", recLine(r))
		}
	}
	if len(d.Sections) > 0 {
		b.WriteString("\nsections (use get_section with a kind or title):\n")
		for _, s := range d.Sections {
			fmt.Fprintf(&b, "  %s%s %s [%s] p%d\n", strings.Repeat("  ", max(0, s.Level-1)), s.Num, s.Title, s.Kind, s.Page+1)
		}
	}
	if len(d.Projects) > 0 {
		var ns []string
		for _, p := range d.Projects {
			ns = append(ns, p.Name)
		}
		fmt.Fprintf(&b, "\nused in projects: %s\n", strings.Join(ns, ", "))
	}
	if len(d.Notes) > 0 {
		b.WriteString("\nnotes:\n")
		for _, n := range d.Notes {
			fmt.Fprintf(&b, "  #%d (%s, %s): %s\n", n.ID, n.Author, n.CreatedAt.Format("2006-01-02"), n.Body)
		}
	}
	return b.String()
}

// recLine renders a recommendation.
func recLine(r library.Recommendation) string {
	s := fmt.Sprintf("#%d %s", r.ID, r.Role)
	if r.Value != "" {
		s += ": " + r.Value
	}
	if r.MPN != "" {
		s += " (" + strings.TrimSpace(r.Manufacturer+" "+r.MPN) + ")"
	}
	if r.Conditions != "" {
		s += " | " + r.Conditions
	}
	if r.Notes != "" {
		s += " | " + r.Notes
	}
	if r.Page >= 0 {
		s += fmt.Sprintf(" | p%d", r.Page+1)
	}
	if len(r.AppliesTo) > 0 {
		s += " | only " + strings.Join(r.AppliesTo, ",")
	}
	return s
}

// truncate shortens long text, saying how to read the rest.
func truncate(s string, n int, more string) string {
	if n <= 0 || len(s) <= n {
		return s
	}
	cut := strings.LastIndex(s[:n], "\n")
	if cut < n/2 {
		cut = n
	}
	return s[:cut] + fmt.Sprintf("\n… (%d more characters; %s)", len(s)-cut, more)
}
