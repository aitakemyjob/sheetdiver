package core

import (
	"context"
	"errors"
	"fmt"
	"math"
	"os"
	"sort"
	"strconv"
	"strings"

	"github.com/aitakemyjob/sheetdiver/internal/extract"
	"github.com/aitakemyjob/sheetdiver/internal/library"
	"github.com/aitakemyjob/sheetdiver/internal/pdf"
)

// PartDetail is the overview of a part: its datasheet, siblings on the
// same datasheet, variants, the best value for every canonical parameter
// key, counts of what was extracted, notes and projects.
type PartDetail struct {
	Part        library.Part             `json:"part"`
	Via         string                   `json:"via"`
	Matched     string                   `json:"matched,omitempty"`
	Datasheet   *library.Datasheet       `json:"datasheet,omitempty"`
	Siblings    []string                 `json:"siblings"`
	Variants    []library.Variant        `json:"variants"`
	Key         []KeyValue               `json:"keyParameters"`
	Sections    []library.Section        `json:"sections"`
	FigureKinds map[string]int           `json:"figureKinds"`
	Counts      map[string]int           `json:"counts"`
	Notes       []library.Note           `json:"notes"`
	Projects    []library.Project        `json:"projects"`
	Recommended []library.Recommendation `json:"recommended"`
}

// KeyValue is the chosen value of one canonical parameter.
type KeyValue struct {
	Key   string            `json:"key"`
	Label string            `json:"label"`
	Param library.Parameter `json:"param"`
	// Rating is true for absolute-maximum values.
	Rating bool `json:"rating,omitempty"`
}

// ResolvePart finds a part by MPN, variant, family pattern or ID.
func (s *Service) ResolvePart(ctx context.Context, ref string) (library.Resolution, error) {
	return s.Lib.Resolve(ctx, ref)
}

// partDatasheet resolves a part and its datasheet.
func (s *Service) partDatasheet(ctx context.Context, ref string) (library.Resolution, library.Datasheet, error) {
	r, err := s.Lib.Resolve(ctx, ref)
	if err != nil {
		return r, library.Datasheet{}, err
	}
	if r.Part.DatasheetID == 0 {
		return r, library.Datasheet{}, fmt.Errorf("%s has no datasheet in the library; import one", r.Part.MPN)
	}
	d, err := s.Lib.Datasheet(ctx, r.Part.DatasheetID)
	return r, d, err
}

// PartDetail builds a part's overview.
func (s *Service) PartDetail(ctx context.Context, ref string) (PartDetail, error) {
	r, err := s.Lib.Resolve(ctx, ref)
	if err != nil {
		return PartDetail{}, err
	}
	p := r.Part
	out := PartDetail{Part: p, Via: r.Via, Matched: r.Variant, Siblings: []string{}, Counts: map[string]int{}}
	out.Variants, _ = s.Lib.Variants(ctx, p.ID)
	out.Notes, _ = s.Lib.Notes(ctx, library.TargetPart, p.MPN)
	out.Projects, _ = s.Lib.ProjectsOfPart(ctx, p.ID)
	if p.DatasheetID == 0 {
		return out, nil
	}
	d, err := s.Lib.Datasheet(ctx, p.DatasheetID)
	if err != nil {
		return out, err
	}
	out.Datasheet = &d
	sibs, _ := s.Lib.PartsOfDatasheet(ctx, d.ID)
	for _, sp := range sibs {
		if sp.ID != p.ID {
			out.Siblings = append(out.Siblings, sp.MPN)
		}
	}
	out.Sections, _ = s.Lib.Sections(ctx, d.ID, false)
	out.FigureKinds, _ = s.Lib.FigureKinds(ctx, d.ID)
	params, _ := s.Lib.Parameters(ctx, d.ID, library.ParamFilter{MPN: p.MPN})
	out.Key = keyValues(params)
	pins, _ := s.PartPins(ctx, p, "")
	regs, _ := s.Lib.Registers(ctx, d.ID)
	tables, _ := s.Lib.Tables(ctx, d.ID, false)
	out.Recommended, _ = s.Lib.Recommendations(ctx, d.ID, p.MPN)
	figs := 0
	for _, n := range out.FigureKinds {
		figs += n
	}
	out.Counts = map[string]int{"pages": d.Pages, "sections": len(out.Sections), "figures": figs, "parameters": len(params), "pins": len(pins), "registers": len(regs), "tables": len(tables), "recommended": len(out.Recommended)}
	return out, nil
}

// operatingKeys are best taken from recommended operating conditions.
var operatingKeys = map[string]bool{"vin": true, "vout": true, "iout": true, "tj": true}

// keyOrder is the display order of canonical keys.
var keyOrder = []string{"vin", "vout", "iout", "fsw", "vref", "ilim", "ton_min", "toff_min", "dmax", "iq", "isd", "uvlo", "ven", "rdson_hs", "rdson_ls",
	"vds", "vgs", "vgs_th", "rdson", "id", "qg", "qgs", "qgd", "ciss", "coss", "rg", "vplateau", "vf", "vdropout", "ichg", "vreg", "vih", "vil", "tss", "tj", "rth_ja", "rth_jc", "efficiency"}

// keyValues picks one parameter per canonical key, preferring recorded
// (agent/user) values, then operating conditions over ratings.
func keyValues(params []library.Parameter) []KeyValue {
	best := map[string]library.Parameter{}
	rank := func(p library.Parameter) int {
		r := 0
		switch p.Source {
		case library.SourceUser:
			r += 0
		case library.SourceAgent:
			r += 10
		default:
			r += 20
		}
		switch p.Section {
		case extract.KindElectrical, extract.KindTiming, extract.KindThermal:
			// Device characteristics (a reference voltage, a frequency) are
			// specified here; a recommended range for the same pin is not it.
			if operatingKeys[p.Key] {
				r++
			}
		case extract.KindRecommended:
			if !operatingKeys[p.Key] {
				r += 2
			}
		case extract.KindAbsMax:
			r += 5
		default:
			r += 3
		}
		return r
	}
	for _, p := range params {
		if p.Key == "" {
			continue
		}
		if cur, ok := best[p.Key]; !ok || rank(p) < rank(cur) {
			best[p.Key] = p
		}
	}
	var out []KeyValue
	for _, k := range keyOrder {
		if p, ok := best[k]; ok {
			out = append(out, KeyValue{Key: k, Label: extract.Keys[k], Param: p, Rating: p.Section == extract.KindAbsMax})
			delete(best, k)
		}
	}
	rest := make([]string, 0, len(best))
	for k := range best {
		rest = append(rest, k)
	}
	sort.Strings(rest)
	for _, k := range rest {
		out = append(out, KeyValue{Key: k, Label: extract.Keys[k], Param: best[k]})
	}
	return out
}

// pdfBytes reads a stored datasheet's PDF.
func (s *Service) pdfBytes(d library.Datasheet) ([]byte, error) {
	data, err := os.ReadFile(s.Lib.DatasheetPath(d.File))
	if err != nil {
		return nil, fmt.Errorf("the PDF for %s is missing from the library folder: %w", d.Filename, err)
	}
	return data, nil
}

// DatasheetPDF returns a datasheet's original PDF.
func (s *Service) DatasheetPDF(ctx context.Context, dsID int64) (library.Datasheet, []byte, error) {
	d, err := s.Lib.Datasheet(ctx, dsID)
	if err != nil {
		return d, nil, err
	}
	data, err := s.pdfBytes(d)
	return d, data, err
}

// RenderPage renders a page (or a region of it) to PNG, caching the
// result in the library's cache folder.
func (s *Service) RenderPage(ctx context.Context, dsID int64, page int, region *pdf.Rect, dpi int) ([]byte, error) {
	d, err := s.Lib.Datasheet(ctx, dsID)
	if err != nil {
		return nil, err
	}
	if page < 0 || page >= d.Pages {
		return nil, fmt.Errorf("page %d is out of range (1–%d)", page+1, d.Pages)
	}
	dpi = clampDPI(dpi)
	key := fmt.Sprintf("%s-p%d-%d.png", d.SHA256[:16], page, dpi)
	if region != nil {
		key = fmt.Sprintf("%s-p%d-%.0f_%.0f_%.0f_%.0f-%d.png", d.SHA256[:16], page, region.X0, region.Y0, region.X1, region.Y1, dpi)
	}
	cache := s.Lib.CachePath(key)
	if b, err := os.ReadFile(cache); err == nil {
		return b, nil
	}
	data, err := s.pdfBytes(d)
	if err != nil {
		return nil, err
	}
	png, err := s.Engine.Render(data, page, region, dpi)
	if err != nil {
		return nil, err
	}
	_ = os.WriteFile(cache, png, 0o644)
	return png, nil
}

// clampDPI keeps render resolution sensible.
func clampDPI(dpi int) int {
	switch {
	case dpi <= 0:
		return 110
	case dpi < 36:
		return 36
	case dpi > 300:
		return 300
	}
	return dpi
}

// RenderFigure renders a figure's region with a small margin.
func (s *Service) RenderFigure(ctx context.Context, id int64, dpi int) (library.Figure, []byte, error) {
	f, err := s.Lib.Figure(ctx, id)
	if err != nil {
		return f, nil, err
	}
	box := f.Box
	box.X0, box.Y0 = math.Max(0, box.X0), math.Max(0, box.Y0)
	png, err := s.RenderPage(ctx, f.DatasheetID, f.Page, &box, dpi)
	return f, png, err
}

// RenderTable renders a table's region (caption included).
func (s *Service) RenderTable(ctx context.Context, id int64, dpi int) (library.Table, []byte, error) {
	t, err := s.Lib.Table(ctx, id)
	if err != nil {
		return t, nil, err
	}
	box := t.Box.Inset(6, 4)
	box.X0, box.Y0 = math.Max(0, box.X0), math.Max(0, box.Y0)
	png, err := s.RenderPage(ctx, t.DatasheetID, t.Page, &box, dpi)
	return t, png, err
}

// FindTable resolves a table on a part's datasheet by id, number ("7-2")
// or caption fragment.
func (s *Service) FindTable(ctx context.Context, part, ref string) (library.Table, error) {
	ref = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(ref), "Table"))
	if id, err := strconv.ParseInt(strings.TrimPrefix(ref, "#"), 10, 64); err == nil && !strings.ContainsAny(ref, "-.") {
		if t, err := s.Lib.Table(ctx, id); err == nil {
			return t, nil
		}
	}
	if part == "" {
		return library.Table{}, errors.New("give the part with a table number, or a table id")
	}
	_, d, err := s.partDatasheet(ctx, part)
	if err != nil {
		return library.Table{}, err
	}
	if t, err := s.Lib.TableByNumber(ctx, d.ID, ref); err == nil {
		return t, nil
	}
	ts, err := s.Lib.Tables(ctx, d.ID, false)
	if err != nil {
		return library.Table{}, err
	}
	low := strings.ToLower(ref)
	for _, t := range ts {
		if strings.Contains(strings.ToLower(t.Caption+" "+t.Title), low) {
			return s.Lib.Table(ctx, t.ID)
		}
	}
	return library.Table{}, fmt.Errorf("no table %q on the %s datasheet", ref, d.Filename)
}

// FindFigure resolves a figure reference on a part's datasheet: an ID, a
// figure number ("6-8", "Figure 6-8") or a caption fragment.
func (s *Service) FindFigure(ctx context.Context, part, ref string) (library.Figure, error) {
	ref = strings.TrimSpace(ref)
	var id int64
	if _, err := fmt.Sscanf(strings.TrimPrefix(ref, "#"), "%d", &id); err == nil && !strings.ContainsAny(ref, "-.") {
		if f, err := s.Lib.Figure(ctx, id); err == nil && part == "" {
			return f, nil
		}
	}
	if part == "" {
		return library.Figure{}, errors.New("give the part with a figure number, or a figure id")
	}
	_, d, err := s.partDatasheet(ctx, part)
	if err != nil {
		return library.Figure{}, err
	}
	num := strings.TrimSpace(strings.TrimPrefix(strings.TrimPrefix(ref, "Figure"), "Fig."))
	if f, err := s.Lib.FigureByNumber(ctx, d.ID, num); err == nil {
		return f, nil
	}
	if id != 0 {
		if f, err := s.Lib.Figure(ctx, id); err == nil && f.DatasheetID == d.ID {
			return f, nil
		}
	}
	figs, err := s.Lib.Figures(ctx, d.ID, library.FigureFilter{Query: ref})
	if err != nil {
		return library.Figure{}, err
	}
	if len(figs) == 0 {
		return library.Figure{}, fmt.Errorf("no figure %q on the %s datasheet", ref, d.Filename)
	}
	return figs[0], nil
}

// ParamsFor returns a part's parameters (filtered).
func (s *Service) ParamsFor(ctx context.Context, part string, f library.ParamFilter) (library.Resolution, []library.Parameter, error) {
	r, d, err := s.partDatasheet(ctx, part)
	if err != nil {
		return r, nil, err
	}
	f.MPN = r.Part.MPN
	ps, err := s.Lib.Parameters(ctx, d.ID, f)
	return r, ps, err
}

// Search runs a library search.
func (s *Service) Search(ctx context.Context, q string, o library.SearchOptions) ([]library.Hit, error) {
	return s.Lib.Search(ctx, q, o)
}

// DatasheetOf resolves a part to its datasheet ID.
func (s *Service) DatasheetOf(ctx context.Context, part string) (library.Resolution, library.Datasheet, error) {
	return s.partDatasheet(ctx, part)
}

// DeleteDatasheet removes a datasheet, its PDF and cached renders.
func (s *Service) DeleteDatasheet(ctx context.Context, id int64) error {
	d, err := s.Lib.DeleteDatasheet(ctx, id)
	if err != nil {
		return err
	}
	_ = os.Remove(s.Lib.DatasheetPath(d.File))
	s.emit(EvLibrary, map[string]any{"datasheetId": id, "action": "deleted"})
	return nil
}

// Changed tells the UI that library data changed (after agent writes).
func (s *Service) Changed(what string, dsID int64) {
	s.emit(EvLibrary, map[string]any{"datasheetId": dsID, "action": what})
}
