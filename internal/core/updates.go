package core

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"

	"github.com/aitakemyjob/sheetdiver/internal/extract"
	"github.com/aitakemyjob/sheetdiver/internal/library"
)

// Parameter change kinds.
const (
	ChangeChanged = "changed"
	ChangeAdded   = "added"
	ChangeRemoved = "removed"
)

// ParamChange is a spec-table row that differs between two revisions.
type ParamChange struct {
	Kind       string `json:"kind"`
	Key        string `json:"key,omitempty"`
	Name       string `json:"name"`
	Section    string `json:"section"`
	Conditions string `json:"conditions,omitempty"`
	Before     string `json:"before,omitempty"`
	After      string `json:"after,omitempty"`
	// Page in the new revision (−1 for removed rows).
	Page int `json:"page"`
}

// UpdateCheck is the result of looking for a newer datasheet revision.
type UpdateCheck struct {
	DatasheetID int64  `json:"datasheetId"`
	Filename    string `json:"filename"`
	Source      string `json:"source"`
	Current     string `json:"current"`
	Latest      string `json:"latest,omitempty"`
	UpToDate    bool   `json:"upToDate"`
	// Changes lists changed, added and removed spec rows; keyed
	// parameters come first.
	Changes []ParamChange `json:"changes,omitempty"`
	// Counts by change kind (Changes may be cut short).
	Counts map[string]int `json:"counts,omitempty"`
	// Recorded is how many agent/user parameters the datasheet has: they
	// are kept on update, but may cite pages of the old revision.
	Recorded int    `json:"recorded"`
	Error    string `json:"error,omitempty"`
}

// maxChanges bounds the changes listed in an update check.
const maxChanges = 80

// pendingUpdates holds downloaded newer revisions until applied.
type pendingUpdates struct {
	mu   sync.Mutex
	data map[int64]pendingPDF
}

// pendingPDF is a downloaded revision.
type pendingPDF struct {
	data      []byte
	name, src string
}

// CheckDatasheetUpdate downloads a datasheet again from where it came
// from (its source URL, else a part-number lookup) and, when the PDF
// differs, extracts it and lists what changed in its spec tables. The
// download is kept for ApplyDatasheetUpdate.
func (s *Service) CheckDatasheetUpdate(ctx context.Context, id int64) (UpdateCheck, error) {
	d, err := s.Lib.Datasheet(ctx, id)
	if err != nil {
		return UpdateCheck{}, err
	}
	out := UpdateCheck{DatasheetID: id, Filename: d.Filename, Current: firstNonEmpty(d.Meta["revision"], "unknown revision")}
	var data []byte
	var name, src string
	if strings.HasPrefix(d.SourceURL, "http") {
		out.Source = d.SourceURL
		data, name, src, err = fetchPDF(ctx, d.SourceURL, "", 1)
	} else {
		parts, _ := s.Lib.PartsOfDatasheet(ctx, id)
		if len(parts) == 0 {
			return out, errors.New("this datasheet has no source URL or part number to look it up by")
		}
		out.Source = "part number " + parts[0].MPN
		data, name, src, err = lookupPDF(ctx, parts[0].MPN)
	}
	if err != nil {
		return out, fmt.Errorf("download %s: %w", out.Source, err)
	}
	sum := sha256.Sum256(data)
	if hex.EncodeToString(sum[:]) == d.SHA256 {
		out.UpToDate, out.Latest = true, out.Current
		return out, nil
	}
	select {
	case s.slots <- struct{}{}:
	case <-ctx.Done():
		return out, ctx.Err()
	}
	doc, err := s.Engine.Extract(ctx, data, nil)
	var res *extract.Result
	if err == nil {
		res = extract.Analyze(doc, name)
	}
	<-s.slots
	if err != nil {
		return out, fmt.Errorf("read the new PDF: %w", err)
	}
	out.Latest = firstNonEmpty(res.Revision.String(), "unknown revision")
	old, err := s.Lib.Parameters(ctx, id, library.ParamFilter{})
	if err != nil {
		return out, err
	}
	var auto []library.Parameter
	for _, p := range old {
		if p.Source == library.SourceAuto {
			auto = append(auto, p)
		} else {
			out.Recorded++
		}
	}
	out.Changes, out.Counts = diffParams(auto, res.Params)
	if len(out.Changes) > maxChanges {
		out.Changes = out.Changes[:maxChanges]
	}
	s.pending.mu.Lock()
	if s.pending.data == nil {
		s.pending.data = map[int64]pendingPDF{}
	}
	s.pending.data[id] = pendingPDF{data: data, name: name, src: src}
	s.pending.mu.Unlock()
	return out, nil
}

// ApplyDatasheetUpdate replaces a datasheet with the newer revision found
// by CheckDatasheetUpdate: the new PDF is extracted into the same
// datasheet (same ID, parts and projects), mechanical data is replaced and
// everything recorded by agents or users is kept. The previous PDF stays
// on disk and its revision is remembered in the metadata.
func (s *Service) ApplyDatasheetUpdate(ctx context.Context, id int64) (ImportResult, error) {
	s.pending.mu.Lock()
	p, ok := s.pending.data[id]
	s.pending.mu.Unlock()
	if !ok {
		return ImportResult{}, errors.New("no downloaded update for this datasheet; check for updates first")
	}
	old, err := s.Lib.Datasheet(ctx, id)
	if err != nil {
		return ImportResult{}, err
	}
	sum := sha256.Sum256(p.data)
	sha := hex.EncodeToString(sum[:])
	doc, err := s.Engine.Extract(ctx, p.data, nil)
	if err != nil {
		return ImportResult{}, err
	}
	res := extract.Analyze(doc, p.name)
	file := sha[:16] + "-" + safeName(p.name)
	if err := os.WriteFile(s.Lib.DatasheetPath(file), p.data, 0o644); err != nil {
		return ImportResult{}, err
	}
	in := buildImport(res, doc, ImportRequest{}, sha, file, p.name, p.src)
	in.Replaces = id
	in.Datasheet.Meta["previousRevision"] = firstNonEmpty(old.Meta["revision"], "unknown")
	in.Datasheet.Meta["previousFile"] = old.File
	in.Datasheet.Meta["previousSha256"] = old.SHA256
	// Keep the parts the datasheet already covers (they may have been
	// renamed or added by hand).
	if parts, _ := s.Lib.PartsOfDatasheet(ctx, id); len(parts) > 0 {
		in.Parts = nil
		for _, pt := range parts {
			in.Parts = append(in.Parts, library.Part{MPN: pt.MPN, Manufacturer: pt.Manufacturer, Description: pt.Description, Category: pt.Category})
		}
	}
	s.importMu.Lock()
	_, err = s.Lib.SaveImport(ctx, in)
	s.importMu.Unlock()
	if err != nil {
		return ImportResult{}, err
	}
	s.pending.mu.Lock()
	delete(s.pending.data, id)
	s.pending.mu.Unlock()
	d, _ := s.Lib.Datasheet(ctx, id)
	parts, _ := s.Lib.PartsOfDatasheet(ctx, id)
	s.emit(EvLibrary, map[string]any{"datasheetId": id, "action": "updated"})
	return ImportResult{Datasheet: d, Parts: parts, Families: res.Families, Variants: len(res.Variants),
		Counts: map[string]int{"pages": len(doc.Pages), "figures": len(res.Figures), "tables": len(res.Tables), "parameters": len(res.Params), "pins": len(res.Pins)}}, nil
}

// CheckAllUpdates checks every datasheet with a source URL, a few at a
// time, and returns the outcome for each.
func (s *Service) CheckAllUpdates(ctx context.Context) ([]UpdateCheck, error) {
	sheets, err := s.Lib.Datasheets(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]UpdateCheck, len(sheets))
	var wg sync.WaitGroup
	sem := make(chan struct{}, 4)
	for i, d := range sheets {
		wg.Add(1)
		go func() {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			c, err := s.CheckDatasheetUpdate(ctx, d.ID)
			if err != nil {
				c.DatasheetID, c.Filename, c.Error = d.ID, d.Filename, err.Error()
			}
			out[i] = c
		}()
	}
	wg.Wait()
	return out, nil
}

// paramIdentity names a spec row across revisions: its section, symbol,
// name and conditions, normalized.
func paramIdentity(section, symbol, name, cond string) string {
	norm := func(s string) string { return strings.ToLower(strings.Join(strings.Fields(s), " ")) }
	return norm(section) + "|" + norm(symbol) + "|" + norm(name) + "|" + norm(cond)
}

// diffParams compares a datasheet's stored (automatic) parameters with a
// new extraction. Rows are matched by identity; values are compared after
// unit conversion. Keyed parameters come first.
func diffParams(old []library.Parameter, fresh []extract.Param) ([]ParamChange, map[string]int) {
	type row struct {
		key, name, section, cond, unit string
		min, typ, max                  *float64
		page                           int
	}
	before := map[string]row{}
	for _, p := range old {
		before[paramIdentity(p.Section, p.Symbol, p.Name, p.Conditions)] = row{p.Key, strings.TrimSpace(p.Symbol + " " + p.Name), p.Section, p.Conditions, p.Unit, p.Min, p.Typ, p.Max, p.Page}
	}
	after := map[string]row{}
	for _, p := range fresh {
		after[paramIdentity(p.Section, p.Symbol, p.Name, p.Conditions)] = row{p.Key, strings.TrimSpace(p.Symbol + " " + p.Name), p.Section, p.Conditions, p.Unit, p.Min, p.Typ, p.Max, p.Page}
	}
	text := func(r row) string {
		return fmt.Sprintf("%s / %s / %s %s", optNum(r.min), optNum(r.typ), optNum(r.max), r.unit)
	}
	var out []ParamChange
	counts := map[string]int{}
	for id, a := range after {
		b, ok := before[id]
		switch {
		case !ok:
			out = append(out, ParamChange{Kind: ChangeAdded, Key: a.key, Name: a.name, Section: a.section, Conditions: a.cond, After: text(a), Page: a.page})
		case !sameValue(b.min, b.unit, a.min, a.unit) || !sameValue(b.typ, b.unit, a.typ, a.unit) || !sameValue(b.max, b.unit, a.max, a.unit):
			out = append(out, ParamChange{Kind: ChangeChanged, Key: a.key, Name: a.name, Section: a.section, Conditions: a.cond, Before: text(b), After: text(a), Page: a.page})
		default:
			continue
		}
		counts[out[len(out)-1].Kind]++
	}
	for id, b := range before {
		if _, ok := after[id]; !ok {
			out = append(out, ParamChange{Kind: ChangeRemoved, Key: b.key, Name: b.name, Section: b.section, Conditions: b.cond, Before: text(b), Page: -1})
			counts[ChangeRemoved]++
		}
	}
	rank := map[string]int{ChangeChanged: 0, ChangeRemoved: 1, ChangeAdded: 2}
	sort.Slice(out, func(i, j int) bool {
		a, b := out[i], out[j]
		if (a.Key != "") != (b.Key != "") {
			return a.Key != ""
		}
		if rank[a.Kind] != rank[b.Kind] {
			return rank[a.Kind] < rank[b.Kind]
		}
		if a.Section != b.Section {
			return a.Section < b.Section
		}
		return a.Name < b.Name
	})
	return out, counts
}

// sameValue compares two optional values in their units (within 0.1%).
func sameValue(a *float64, ua string, b *float64, ub string) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	x, _, okA := extract.ToSI(*a, ua)
	y, _, okB := extract.ToSI(*b, ub)
	if !okA || !okB {
		x, y = *a, *b
		if ua != ub {
			return false
		}
	}
	return absDiffRel(x, y) <= 1e-3
}

// optNum formats an optional number ("–" when absent).
func optNum(v *float64) string {
	if v == nil {
		return "–"
	}
	return strconv.FormatFloat(*v, 'g', 6, 64)
}
