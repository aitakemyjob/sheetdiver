package core

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/aitakemyjob/sheetdiver/internal/catalog"
	"github.com/aitakemyjob/sheetdiver/internal/library"
)

// CatalogStatus is the parts catalogue's state with the refresh reminder.
type CatalogStatus struct {
	catalog.Status
	// AgeDays is how long ago it was published; Stale is set when that
	// exceeds the reminder setting (RemindDays, 0 = never).
	AgeDays    int  `json:"ageDays"`
	RemindDays int  `json:"remindDays"`
	Stale      bool `json:"stale"`
	// Progress of a running download.
	Progress *catalog.Progress `json:"progress,omitempty"`
	Error    string            `json:"error,omitempty"`
}

// CatalogStatus reports the catalogue's state.
func (s *Service) CatalogStatus(ctx context.Context) CatalogStatus {
	st := CatalogStatus{Status: s.Catalog.Status(ctx), RemindDays: s.Config.Get().Settings.RemindDays()}
	s.mu.Lock()
	st.Progress, st.Error = s.catalogProgress, s.catalogErr
	s.mu.Unlock()
	if st.Installed {
		from := st.Published
		if from.IsZero() {
			from = st.Downloaded
		}
		st.AgeDays = int(time.Since(from).Hours() / 24)
		st.Stale = st.RemindDays > 0 && st.AgeDays >= st.RemindDays
	}
	return st
}

// StartCatalogDownload downloads (or refreshes) the parts catalogue in
// the background; progress and the outcome arrive as catalog events. It
// is only ever started by the user.
func (s *Service) StartCatalogDownload() error {
	if s.Catalog.Status(context.Background()).Updating {
		return errors.New("the catalogue is already being downloaded")
	}
	s.mu.Lock()
	s.catalogProgress, s.catalogErr = &catalog.Progress{Stage: "starting"}, ""
	s.mu.Unlock()
	go func() {
		last := time.Time{}
		err := s.Catalog.Download(context.Background(), func(p catalog.Progress) {
			s.mu.Lock()
			s.catalogProgress = &p
			s.mu.Unlock()
			// Progress arrives per read; a few updates a second are plenty.
			if time.Since(last) > 250*time.Millisecond || p.Stage == "done" {
				last = time.Now()
				s.emit(EvCatalog, s.CatalogStatus(context.Background()))
			}
		})
		s.mu.Lock()
		s.catalogProgress = nil
		if err != nil {
			s.catalogErr = err.Error()
		}
		s.mu.Unlock()
		s.emit(EvCatalog, s.CatalogStatus(context.Background()))
	}()
	return nil
}

// CatalogCategories lists the catalogue's categories with part counts,
// cached until the catalogue is refreshed (listing scans it).
func (s *Service) CatalogCategories(ctx context.Context) ([]catalog.Category, error) {
	key := s.Catalog.Status(ctx).Downloaded
	s.mu.Lock()
	if s.catalogCats != nil && s.catalogCatsKey.Equal(key) {
		cats := s.catalogCats
		s.mu.Unlock()
		return cats, nil
	}
	s.mu.Unlock()
	cats, err := s.Catalog.Categories(ctx)
	if err != nil {
		return nil, err
	}
	s.mu.Lock()
	s.catalogCats, s.catalogCatsKey = cats, key
	s.mu.Unlock()
	return cats, nil
}

// CatalogItem is a catalogue part with whether the library has it.
type CatalogItem struct {
	catalog.Item
	// InLibrary names the library part this one resolves to, if any.
	InLibrary string `json:"inLibrary,omitempty"`
}

// CatalogSearch searches the catalogue and marks parts already in the
// library.
func (s *Service) CatalogSearch(ctx context.Context, q catalog.Query) ([]CatalogItem, error) {
	items, err := s.Catalog.Search(ctx, q)
	if err != nil {
		return nil, err
	}
	out := make([]CatalogItem, 0, len(items))
	for _, it := range items {
		ci := CatalogItem{Item: it}
		if r, err := s.Lib.Resolve(ctx, it.MPN); err == nil && r.Part.DatasheetID != 0 {
			ci.InLibrary = r.Part.MPN
		}
		out = append(out, ci)
	}
	return out, nil
}

// CatalogRequest builds the import of a catalogue part: its datasheet
// link (else a part-number lookup), with its orderable code kept as a
// variant so BOMs that list it resolve.
func CatalogRequest(it catalog.Item) ImportRequest {
	req := ImportRequest{URL: it.Datasheet, Orderable: []string{it.MPN}, Manufacturer: strings.TrimSpace(it.Manufacturer)}
	if req.URL == "" {
		req.Lookup = it.MPN
	}
	return req
}

// CatalogImport queues the datasheets of catalogue parts (by LCSC code)
// for import and returns the jobs.
func (s *Service) CatalogImport(ctx context.Context, codes []string) ([]ImportJob, error) {
	items, err := s.Catalog.Get(ctx, codes)
	if err != nil {
		return nil, err
	}
	if len(items) == 0 {
		return nil, fmt.Errorf("none of %s is in the catalogue", strings.Join(codes, ", "))
	}
	var reqs []ImportRequest
	for _, it := range items {
		reqs = append(reqs, CatalogRequest(it))
	}
	return s.StartImports(reqs), nil
}

// catalogLookup finds a datasheet through the catalogue when the
// manufacturers' URLs don't have it.
func (s *Service) catalogLookup(ctx context.Context, mpn string) ([]byte, string, string, error) {
	items, err := s.Catalog.ByMPN(ctx, mpn)
	if err != nil {
		return nil, "", "", err
	}
	var lastErr error
	tried := 0
	for _, it := range items {
		if it.Datasheet == "" || tried == 3 {
			continue
		}
		tried++
		data, name, src, err := fetchPDF(ctx, it.Datasheet, it.MPN+".pdf", 1)
		if err == nil {
			return data, name, src, nil
		}
		lastErr = err
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("the parts catalogue has no datasheet link for %s", mpn)
	}
	return nil, "", "", lastErr
}

// addOrderable records orderable codes (such as TPS563200DDCR) as
// variants of the imported datasheet's best-matching part (the longest
// part number the code starts with, else the first), unless they already
// resolve. They are recorded as the user's, since the user picked them,
// so re-extraction keeps them.
func (s *Service) addOrderable(ctx context.Context, parts []library.Part, codes []string) {
	for _, code := range codes {
		code = strings.TrimSpace(code)
		if code == "" || len(parts) == 0 {
			continue
		}
		if _, err := s.Lib.Resolve(ctx, code); err == nil {
			continue
		}
		best, bestLen := parts[0], -1
		for _, p := range parts {
			if strings.HasPrefix(strings.ToUpper(code), strings.ToUpper(p.MPN)) && len(p.MPN) > bestLen {
				best, bestLen = p, len(p.MPN)
			}
		}
		_ = s.Lib.SetVariants(ctx, best.ID, []library.Variant{{MPN: code}}, library.SourceUser)
	}
}
