package core

import (
	"bytes"
	"context"
	"encoding/csv"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"

	"github.com/aitakemyjob/sheetdiver/internal/catalog"
	"github.com/aitakemyjob/sheetdiver/internal/extract"
	"github.com/aitakemyjob/sheetdiver/internal/library"
)

// BOMLine is one line of a bill of materials.
type BOMLine struct {
	Line         int      `json:"line"`
	Designators  []string `json:"designators"`
	MPN          string   `json:"mpn,omitempty"`
	Manufacturer string   `json:"manufacturer,omitempty"`
	Value        string   `json:"value,omitempty"`
	Description  string   `json:"description,omitempty"`
	// LCSC is the JLCPCB/LCSC part code (C20917) when the BOM has one.
	LCSC string `json:"lcsc,omitempty"`
	Qty  int    `json:"qty"`
}

// bomColumns maps normalized header names to fields.
var bomColumns = map[string]string{
	"reference": "ref", "references": "ref", "ref": "ref", "refs": "ref", "refdes": "ref", "designator": "ref", "designators": "ref", "parts": "ref", "partreferences": "ref",
	"mpn": "mpn", "manufacturerpartnumber": "mpn", "mfrpartnumber": "mpn", "mfrpn": "mpn", "mfgpn": "mpn", "mfgpartnumber": "mpn", "manufacturerpn": "mpn",
	"partnumber": "mpn", "pn": "mpn", "manufacturerpart": "mpn", "mfrno": "mpn", "manufacturerpartno": "mpn",
	"manufacturer": "mfr", "mfr": "mfr", "mfg": "mfr", "manufacturername": "mfr", "vendor": "mfr",
	"qty": "qty", "quantity": "qty", "count": "qty", "qnty": "qty",
	"value": "value", "val": "value",
	"description": "desc", "desc": "desc", "comment": "desc", "comments": "desc",
	"lcsc": "lcsc", "lcscpart": "lcsc", "lcscpartnumber": "lcsc", "lcscpartno": "lcsc", "jlcpcbpart": "lcsc", "jlcpcbpartnumber": "lcsc",
	"jlcpart": "lcsc", "jlcpcb": "lcsc", "supplierpart": "lcsc", "supplierpartnumber": "lcsc",
}

// normHeader lowercases a header and drops everything but letters/digits.
func normHeader(h string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(h) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// lcscRe matches a JLCPCB/LCSC part code.
var lcscRe = regexp.MustCompile(`^C\d{1,9}$`)

// refRangeRe matches a designator range such as R1-R4 or C10–C12.
var refRangeRe = regexp.MustCompile(`^([A-Za-z]+)(\d+)\s*[-–]\s*([A-Za-z]*)(\d+)$`)

// splitDesignators expands "R1, R2", "R1 R2" and "R1-R4" into designators.
func splitDesignators(s string) []string {
	var out []string
	for _, f := range strings.FieldsFunc(s, func(r rune) bool { return r == ',' || r == ';' || r == ' ' || r == '\t' }) {
		if m := refRangeRe.FindStringSubmatch(f); m != nil && (m[3] == "" || strings.EqualFold(m[1], m[3])) {
			a, _ := strconv.Atoi(m[2])
			b, _ := strconv.Atoi(m[4])
			if b >= a && b-a < 500 {
				for i := a; i <= b; i++ {
					out = append(out, fmt.Sprintf("%s%d", m[1], i))
				}
				continue
			}
		}
		out = append(out, f)
	}
	return out
}

// ParseBOM reads a CSV/TSV bill of materials (KiCad, Altium, JLCPCB and
// similar exports). The header row is found by its column names; the
// delimiter is detected. A Value that looks like a part number stands in
// for a missing MPN (KiCad BOMs often carry it there).
func ParseBOM(data []byte) ([]BOMLine, error) {
	data = bytes.TrimPrefix(data, []byte("\xef\xbb\xbf"))
	first := string(data)
	if i := strings.IndexByte(first, '\n'); i > 0 {
		first = first[:i]
	}
	delim := ','
	for _, d := range []rune{'\t', ';'} {
		if strings.Count(first, string(d)) > strings.Count(first, string(delim)) {
			delim = d
		}
	}
	r := csv.NewReader(bytes.NewReader(data))
	r.Comma = delim
	r.FieldsPerRecord = -1
	r.LazyQuotes = true
	r.TrimLeadingSpace = true
	records, err := r.ReadAll()
	if err != nil {
		return nil, fmt.Errorf("read BOM: %w", err)
	}
	cols, headerAt := map[string]int{}, -1
	for i, rec := range records {
		found := map[string]int{}
		for k, h := range rec {
			if f, ok := bomColumns[normHeader(h)]; ok {
				if _, dup := found[f]; !dup {
					found[f] = k
				}
			}
		}
		_, ref := found["ref"]
		_, mpn := found["mpn"]
		_, val := found["value"]
		_, lcsc := found["lcsc"]
		if ref && (mpn || val || lcsc) {
			cols, headerAt = found, i
			break
		}
		if i > 20 {
			break
		}
	}
	if headerAt < 0 {
		return nil, errors.New("no BOM header found: need a designator/reference column and a part number (MPN), LCSC or value column")
	}
	get := func(rec []string, f string) string {
		if k, ok := cols[f]; ok && k < len(rec) {
			return strings.TrimSpace(rec[k])
		}
		return ""
	}
	var out []BOMLine
	for i := headerAt + 1; i < len(records); i++ {
		rec := records[i]
		l := BOMLine{Line: i + 1, Designators: splitDesignators(get(rec, "ref")), MPN: get(rec, "mpn"), Manufacturer: get(rec, "mfr"), Value: get(rec, "value"), Description: get(rec, "desc")}
		if c := strings.ToUpper(get(rec, "lcsc")); lcscRe.MatchString(c) {
			l.LCSC = c
		}
		if len(l.Designators) == 0 && l.MPN == "" && l.Value == "" {
			continue
		}
		if l.MPN == "" && extract.IsPartNumber(strings.ToUpper(l.Value)) {
			l.MPN = l.Value
		}
		l.Qty = len(l.Designators)
		if q, err := strconv.Atoi(get(rec, "qty")); err == nil && q > 0 {
			l.Qty = q
		}
		if l.Qty == 0 {
			l.Qty = 1
		}
		out = append(out, l)
	}
	if len(out) == 0 {
		return nil, errors.New("the BOM has no lines")
	}
	return out, nil
}

// BOMRequest imports a BOM into a project.
type BOMRequest struct {
	Data     []byte `json:"data,omitempty"`
	Path     string `json:"path,omitempty"`
	Filename string `json:"filename,omitempty"`
	// Project is created if it doesn't exist (default: the file's name).
	Project string `json:"project,omitempty"`
	// Fetch downloads datasheets for parts not in the library, by part
	// number from the manufacturers' sites.
	Fetch bool `json:"fetch"`
}

// BOM line outcomes.
const (
	BOMInLibrary   = "in library"
	BOMImported    = "imported"
	BOMNoDatasheet = "no datasheet"
	BOMSkipped     = "skipped"
)

// BOMRow is what happened to one BOM line.
type BOMRow struct {
	BOMLine
	Part   string `json:"part,omitempty"`
	Status string `json:"status"`
	Note   string `json:"note,omitempty"`
	// JLC is the part in the JLCPCB catalogue, when it is downloaded.
	JLC *BOMJLC `json:"jlc,omitempty"`
}

// BOMJLC is a BOM line's JLCPCB assembly availability.
type BOMJLC struct {
	LCSC    string `json:"lcsc"`
	Library string `json:"library"`
	Stock   int    `json:"stock"`
}

// BOMResult summarizes a BOM import.
type BOMResult struct {
	Project string         `json:"project"`
	Rows    []BOMRow       `json:"rows"`
	Counts  map[string]int `json:"counts"`
}

// ImportBOM reads a BOM into a project: each line's part is found in the
// library (by MPN, orderable code or family) or, with Fetch, its
// datasheet is downloaded by part number (several at a time). Parts that
// can't be found still join the project, marked as having no datasheet,
// so the BOM is complete. Lines without a part number (most passives) are
// skipped.
func (s *Service) ImportBOM(ctx context.Context, req BOMRequest) (BOMResult, error) {
	data := req.Data
	if len(data) == 0 && req.Path != "" {
		b, err := readSmallFile(expandHome(req.Path), 10<<20)
		if err != nil {
			return BOMResult{}, err
		}
		data = b
		req.Filename = firstNonEmpty(req.Filename, filepath.Base(req.Path))
	}
	lines, err := ParseBOM(data)
	if err != nil {
		return BOMResult{}, err
	}
	name := firstNonEmpty(req.Project, strings.TrimSuffix(filepath.Base(firstNonEmpty(req.Filename, "BOM")), filepath.Ext(req.Filename)))
	proj, err := s.Lib.Project(ctx, name)
	if errors.Is(err, library.ErrNotFound) {
		proj, err = s.Lib.CreateProject(ctx, name, "Imported from "+firstNonEmpty(req.Filename, "a BOM"))
	}
	if err != nil {
		return BOMResult{}, err
	}
	out := BOMResult{Project: proj.Name, Counts: map[string]int{}}
	// With the parts catalogue, LCSC codes give part numbers and every
	// line gets its JLCPCB availability.
	jlc := map[int]*BOMJLC{}
	if s.Catalog.Status(ctx).Installed {
		for i, l := range lines {
			var it *catalogItem
			if l.LCSC != "" {
				if items, _ := s.Catalog.Get(ctx, []string{l.LCSC}); len(items) > 0 {
					it = &items[0]
				}
			}
			if it == nil && l.MPN != "" {
				if items, _ := s.Catalog.ByMPN(ctx, l.MPN); len(items) > 0 && strings.EqualFold(items[0].MPN, l.MPN) {
					it = &items[0]
				}
			}
			if it == nil {
				continue
			}
			if lines[i].MPN == "" {
				lines[i].MPN = it.MPN
			}
			jlc[i] = &BOMJLC{LCSC: it.LCSC, Library: it.Library, Stock: it.Stock}
		}
	}
	// Find what the library already has, and fetch the rest in parallel.
	missing := map[string]bool{}
	for _, l := range lines {
		if l.MPN == "" {
			continue
		}
		if _, err := s.Lib.Resolve(ctx, l.MPN); err != nil {
			missing[l.MPN] = true
		}
	}
	fetchErr := map[string]string{}
	if req.Fetch && len(missing) > 0 {
		var mu sync.Mutex
		var wg sync.WaitGroup
		sem := make(chan struct{}, 4)
		for mpn := range missing {
			wg.Add(1)
			go func() {
				defer wg.Done()
				sem <- struct{}{}
				defer func() { <-sem }()
				if _, err := s.Import(ctx, ImportRequest{Lookup: mpn, Orderable: []string{mpn}}); err != nil {
					mu.Lock()
					fetchErr[mpn] = err.Error()
					mu.Unlock()
				}
			}()
		}
		wg.Wait()
	}
	for i, l := range lines {
		row := BOMRow{BOMLine: l, JLC: jlc[i]}
		if l.MPN == "" {
			row.Status, row.Note = BOMSkipped, "no part number"
			out.Rows = append(out.Rows, row)
			out.Counts[row.Status]++
			continue
		}
		res, err := s.Lib.Resolve(ctx, l.MPN)
		switch {
		case err == nil && res.Part.DatasheetID != 0:
			row.Status = BOMInLibrary
			if missing[l.MPN] {
				row.Status = BOMImported
			}
		case err == nil:
			row.Status = BOMNoDatasheet
		default:
			// Keep the BOM complete: a placeholder part without a datasheet.
			p, err := s.Lib.AddPart(ctx, library.Part{MPN: l.MPN, Manufacturer: l.Manufacturer, Description: firstNonEmpty(l.Description, l.Value)})
			if err != nil {
				row.Status, row.Note = BOMSkipped, err.Error()
				out.Rows = append(out.Rows, row)
				out.Counts[row.Status]++
				continue
			}
			res = library.Resolution{Part: p, Via: "mpn"}
			row.Status = BOMNoDatasheet
			if e, ok := fetchErr[l.MPN]; ok {
				row.Note = e
			} else if !req.Fetch {
				row.Note = "not in the library (fetching was off)"
			}
		}
		row.Part = res.Part.MPN
		pp := library.ProjectPart{Designator: strings.Join(l.Designators, ", "), Qty: l.Qty, Role: firstNonEmpty(l.Description, l.Value)}
		if !strings.EqualFold(l.MPN, res.Part.MPN) {
			pp.Variant = l.MPN
		}
		if _, err := s.AddToProject(ctx, proj.Name, res.Part.MPN, pp); err != nil {
			row.Note = err.Error()
		}
		out.Rows = append(out.Rows, row)
		out.Counts[row.Status]++
	}
	s.ProjectChanged(proj.Name, "bom")
	s.emit(EvLibrary, map[string]any{"action": "bom"})
	return out, nil
}

// catalogItem is a catalogue part (an alias keeping bom.go readable).
type catalogItem = catalog.Item

// readSmallFile reads a file of at most max bytes.
func readSmallFile(path string, max int64) ([]byte, error) {
	st, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if st.Size() > max {
		return nil, fmt.Errorf("%s is larger than %d MB", path, max>>20)
	}
	return os.ReadFile(path)
}
