package core

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/aitakemyjob/sheetdiver/internal/extract"
	"github.com/aitakemyjob/sheetdiver/internal/library"
	"github.com/aitakemyjob/sheetdiver/internal/pdf"
)

// maxPDF bounds the size of an imported datasheet.
const maxPDF = 200 << 20

// ImportRequest names a PDF to import: a local path, raw bytes (UI
// uploads) or an http(s) URL. MPNs, Manufacturer and Category override
// what extraction finds.
type ImportRequest struct {
	Path     string `json:"path,omitempty"`
	Data     []byte `json:"data,omitempty"`
	Filename string `json:"filename,omitempty"`
	URL      string `json:"url,omitempty"`
	// Lookup is a part number to find a datasheet for at the
	// manufacturers' well-known datasheet URLs.
	Lookup       string   `json:"lookup,omitempty"`
	MPNs         []string `json:"mpns,omitempty"`
	Manufacturer string   `json:"manufacturer,omitempty"`
	Category     string   `json:"category,omitempty"`
	// Force re-extracts a datasheet that is already in the library.
	Force bool `json:"force,omitempty"`
}

// ImportResult summarizes an import.
type ImportResult struct {
	Datasheet library.Datasheet `json:"datasheet"`
	Parts     []library.Part    `json:"parts"`
	Existing  bool              `json:"existing"`
	Counts    map[string]int    `json:"counts"`
	Families  []string          `json:"families"`
	Variants  int               `json:"variants"`
	Took      string            `json:"took"`
}

// Import reads a datasheet into the library and tracks it as an import
// job. A PDF already in the library (same content) is returned as is
// unless Force is set, in which case its mechanical extraction is redone
// and agent/user data is kept.
func (s *Service) Import(ctx context.Context, req ImportRequest) (ImportResult, error) {
	job := s.newJob(req)
	return s.runJob(ctx, job, req.Data)
}

// importPDF does the work of an import, reporting progress to job.
func (s *Service) importPDF(ctx context.Context, req ImportRequest, job *ImportJob) (ImportResult, error) {
	start := time.Now()
	s.updateJob(job, func(j *ImportJob) { j.Stage = "downloading" })
	data, name, src, err := s.loadPDF(ctx, req)
	if err != nil {
		return ImportResult{}, err
	}
	s.updateJob(job, func(j *ImportJob) { j.Name = name })
	sum := sha256.Sum256(data)
	sha := hex.EncodeToString(sum[:])
	if d, ok, err := s.Lib.DatasheetBySHA(ctx, sha); err != nil {
		return ImportResult{}, err
	} else if ok && !req.Force {
		parts, _ := s.Lib.PartsOfDatasheet(ctx, d.ID)
		if len(req.MPNs) > 0 {
			s.importMu.Lock()
			parts, err = s.addMPNs(ctx, d, req.MPNs, req)
			s.importMu.Unlock()
			if err != nil {
				return ImportResult{}, err
			}
		}
		return ImportResult{Datasheet: d, Parts: parts, Existing: true, Families: d.Families, Took: time.Since(start).Round(time.Millisecond).String()}, nil
	}
	// Reading and analysing run in parallel up to the engine's workers;
	// saving is serialized.
	s.updateJob(job, func(j *ImportJob) { j.Stage = "waiting" })
	select {
	case s.slots <- struct{}{}:
	case <-ctx.Done():
		return ImportResult{}, ctx.Err()
	}
	s.updateJob(job, func(j *ImportJob) { j.Stage = "reading" })
	doc, err := s.Engine.Extract(ctx, data, func(done, total int) {
		if done%4 == 0 || done == total {
			s.updateJob(job, func(j *ImportJob) { j.Done, j.Total = done, total })
		}
	})
	if err != nil {
		<-s.slots
		return ImportResult{}, fmt.Errorf("read %s: %w", name, err)
	}
	s.updateJob(job, func(j *ImportJob) { j.Stage = "analyzing" })
	res := extract.Analyze(doc, name)
	<-s.slots
	file := sha[:16] + "-" + safeName(name)
	if err := os.WriteFile(s.Lib.DatasheetPath(file), data, 0o644); err != nil {
		return ImportResult{}, err
	}
	in := buildImport(res, doc, req, sha, file, name, src)
	s.updateJob(job, func(j *ImportJob) { j.Stage = "saving" })
	s.importMu.Lock()
	id, err := s.Lib.SaveImport(ctx, in)
	s.importMu.Unlock()
	if err != nil {
		return ImportResult{}, err
	}
	d, err := s.Lib.Datasheet(ctx, id)
	if err != nil {
		return ImportResult{}, err
	}
	parts, _ := s.Lib.PartsOfDatasheet(ctx, id)
	out := ImportResult{
		Datasheet: d, Parts: parts, Families: res.Families, Variants: len(res.Variants),
		Counts: map[string]int{
			"pages": len(doc.Pages), "sections": len(res.Sections), "figures": len(res.Figures),
			"tables": len(res.Tables), "parameters": len(res.Params), "pins": len(res.Pins),
		},
		Took: time.Since(start).Round(time.Millisecond).String(),
	}
	s.emit(EvLibrary, map[string]any{"datasheetId": id, "action": "imported"})
	return out, nil
}

// addMPNs attaches extra part numbers to an already-imported datasheet.
func (s *Service) addMPNs(ctx context.Context, d library.Datasheet, mpns []string, req ImportRequest) ([]library.Part, error) {
	existing, _ := s.Lib.PartsOfDatasheet(ctx, d.ID)
	var desc, cat string
	if len(existing) > 0 {
		desc, cat = existing[0].Description, existing[0].Category
	}
	for _, m := range mpns {
		_, err := s.Lib.AddPart(ctx, library.Part{MPN: m, Manufacturer: firstNonEmpty(req.Manufacturer, d.Manufacturer), Description: desc, Category: firstNonEmpty(req.Category, cat), DatasheetID: d.ID})
		if err != nil {
			return nil, err
		}
	}
	return s.Lib.PartsOfDatasheet(ctx, d.ID)
}

// Reextract redoes the mechanical extraction of a stored datasheet.
func (s *Service) Reextract(ctx context.Context, dsID int64) (ImportResult, error) {
	d, err := s.Lib.Datasheet(ctx, dsID)
	if err != nil {
		return ImportResult{}, err
	}
	return s.Import(ctx, ImportRequest{Path: s.Lib.DatasheetPath(d.File), Filename: d.Filename, Force: true})
}

// loadPDF reads the request's PDF bytes and returns them with a display
// filename and source URL.
func (s *Service) loadPDF(ctx context.Context, req ImportRequest) ([]byte, string, string, error) {
	switch {
	case len(req.Data) > 0:
		name := firstNonEmpty(req.Filename, "datasheet.pdf")
		if !pdf.IsPDF(req.Data) {
			return nil, "", "", fmt.Errorf("%s is not a PDF", name)
		}
		return req.Data, filepath.Base(name), "", nil
	case req.Path != "":
		p := expandHome(req.Path)
		st, err := os.Stat(p)
		if err != nil {
			return nil, "", "", err
		}
		if st.IsDir() {
			return nil, "", "", fmt.Errorf("%s is a folder; import its PDFs one at a time", p)
		}
		if st.Size() > maxPDF {
			return nil, "", "", fmt.Errorf("%s is larger than %d MB", p, maxPDF>>20)
		}
		data, err := os.ReadFile(p)
		if err != nil {
			return nil, "", "", err
		}
		if !pdf.IsPDF(data) {
			return nil, "", "", fmt.Errorf("%s is not a PDF", p)
		}
		return data, firstNonEmpty(req.Filename, filepath.Base(p)), "", nil
	case req.URL != "":
		return fetchPDF(ctx, req.URL, req.Filename, 1)
	case req.Lookup != "":
		return lookupPDF(ctx, req.Lookup)
	}
	return nil, "", "", errors.New("give a path, a URL, a part number to look up, or file data to import")
}

// datasheetURLs are manufacturers' predictable datasheet locations; %s
// is the part number (lower case unless noted by the upper flag).
var datasheetURLs = []struct {
	pattern string
	upper   bool
}{
	{"https://www.ti.com/lit/ds/symlink/%s.pdf", false},
	{"https://www.st.com/resource/en/datasheet/%s.pdf", false},
	{"https://www.analog.com/media/en/technical-documentation/data-sheets/%s.pdf", false},
	{"https://www.onsemi.com/download/data-sheet/pdf/%s-d.pdf", false},
	{"https://assets.nexperia.com/documents/data-sheet/%s.pdf", true},
	{"https://www.diodes.com/assets/Datasheets/%s.pdf", true},
}

// vendorHints send part-number prefixes to the manufacturer most likely
// to publish them, so a lookup tries that site first.
var vendorHints = []struct {
	re   *regexp.Regexp
	host string
}{
	{regexp.MustCompile(`^(TPS|TLV|TL\d|LM|LMR|LP|BQ|INA|OPA|CSD|TMP|ADS|DAC\d|DRV|TCA|TXS|TXB|SN|UCC|ISO|TPD|TS\d|HDC|LMV|REF|TRS)`), "ti.com"},
	{regexp.MustCompile(`^(STM|L78|L79|LD\d|LDL|VN|ST\d|STL|STD|STP|LSM|LIS|VL\d|M24|L6)`), "st.com"},
	{regexp.MustCompile(`^(MAX|LT|LTC|LTM|ADP|AD\d|ADM|ADUM|ADA|ADG|ADL|DS\d)`), "analog.com"},
	{regexp.MustCompile(`^(NCP|NCV|MC\d|FDN|FDS|FDD|NTD|NTR|NVM|FAN|NC7|CAT)`), "onsemi.com"},
	{regexp.MustCompile(`^(PMV|BUK|PSMN|PESD|BC8|BAS|74LVC|74HC|PMEG|NX)`), "nexperia.com"},
	{regexp.MustCompile(`^(AP\d|AZ|DMN|DMP|DMG|PAM|AL\d)`), "diodes.com"},
}

// basePartRe captures a part number up to its ordering suffix:
// TPS563200DDCR → TPS563200, LM317DCYR → LM317, MAX77801EWP+T → MAX77801.
var basePartRe = regexp.MustCompile(`^[A-Z]{1,6}\d+(?:[A-Z]\d+)*`)

// lookupCandidates lists the names to try for a part number: as given,
// without packing suffixes (+T, #, /…), and its base number.
func lookupCandidates(mpn string) []string {
	mpn = strings.ToUpper(strings.TrimSpace(mpn))
	var out []string
	add := func(c string) {
		if len(c) >= 4 && !slices.Contains(out, c) {
			out = append(out, c)
		}
	}
	add(mpn)
	if i := strings.IndexAny(mpn, "+#/,"); i > 0 {
		add(mpn[:i])
	}
	add(basePartRe.FindString(mpn))
	return out
}

// orderedURLs puts the hinted manufacturer's URL patterns first.
func orderedURLs(mpn string) []struct {
	pattern string
	upper   bool
} {
	host := ""
	for _, h := range vendorHints {
		if h.re.MatchString(strings.ToUpper(mpn)) {
			host = h.host
			break
		}
	}
	urls := slices.Clone(datasheetURLs)
	sort.SliceStable(urls, func(i, j int) bool {
		return host != "" && strings.Contains(urls[i].pattern, host) && !strings.Contains(urls[j].pattern, host)
	})
	return urls
}

// lookupPDF tries the well-known datasheet URLs for a part number and its
// shorter forms, the likely manufacturer first.
func lookupPDF(ctx context.Context, mpn string) ([]byte, string, string, error) {
	tried := 0
	for _, m := range lookupCandidates(mpn) {
		for _, u := range orderedURLs(m) {
			if err := ctx.Err(); err != nil {
				return nil, "", "", err
			}
			name := strings.ToLower(m)
			if u.upper {
				name = strings.ToUpper(m)
			}
			tried++
			data, fn, src, err := fetchPDF(ctx, fmt.Sprintf(u.pattern, name), "", 0)
			if err == nil {
				return data, fn, src, nil
			}
		}
	}
	return nil, "", "", fmt.Errorf("no datasheet for %s at the usual manufacturer URLs (tried %d); find its URL and import that, or download it and import the file", mpn, tried)
}

// pdfLinkRe finds links to PDFs in an HTML page.
var pdfLinkRe = regexp.MustCompile(`(?i)href\s*=\s*["']([^"']+?(?:\.pdf|datasheet)[^"']*)["']`)

// fetchPDF downloads a datasheet over http(s). If the URL returns an HTML
// product page and follow > 0, the page's most datasheet-like PDF link is
// followed.
func fetchPDF(ctx context.Context, raw, filename string, follow int) ([]byte, string, string, error) {
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return nil, "", "", fmt.Errorf("%q is not an http(s) URL", raw)
	}
	ctx, cancel := context.WithTimeout(ctx, 90*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, "", "", err
	}
	// Many manufacturer sites refuse clients that don't look like a browser.
	req.Header.Set("User-Agent", "Mozilla/5.0 (X11; Linux x86_64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/124.0 Safari/537.36 SheetDiver")
	req.Header.Set("Accept", "application/pdf,*/*")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, "", "", fmt.Errorf("download %s: %w", raw, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, "", "", fmt.Errorf("download %s: %s", raw, resp.Status)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxPDF+1))
	if err != nil {
		return nil, "", "", err
	}
	if len(data) > maxPDF {
		return nil, "", "", fmt.Errorf("%s is larger than %d MB", raw, maxPDF>>20)
	}
	if !pdf.IsPDF(data) {
		if follow > 0 {
			if link := bestPDFLink(resp.Request.URL, string(data)); link != "" {
				return fetchPDF(ctx, link, filename, follow-1)
			}
		}
		return nil, "", "", fmt.Errorf("%s did not return a PDF (the site may block scripted downloads: download it yourself and import the file or its bytes)", raw)
	}
	name := filename
	if name == "" {
		name = path.Base(u.Path)
		if !strings.HasSuffix(strings.ToLower(name), ".pdf") {
			name += ".pdf"
		}
	}
	return data, name, raw, nil
}

// bestPDFLink picks the link on an HTML page most likely to be the
// datasheet PDF, resolved against the page URL.
func bestPDFLink(base *url.URL, html string) string {
	best, score := "", 0
	for _, m := range pdfLinkRe.FindAllStringSubmatch(html, 200) {
		href := strings.ReplaceAll(m[1], "&amp;", "&")
		u, err := base.Parse(href)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") {
			continue
		}
		low := strings.ToLower(u.String())
		sc := 1
		if strings.Contains(low, "datasheet") || strings.Contains(low, "data-sheet") || strings.Contains(low, "/ds/") {
			sc += 2
		}
		if strings.HasSuffix(low, ".pdf") {
			sc++
		}
		if strings.Contains(low, "errata") || strings.Contains(low, "appnote") || strings.Contains(low, "application-note") || strings.Contains(low, "user-guide") {
			sc -= 2
		}
		if sc > score {
			best, score = u.String(), sc
		}
	}
	return best
}

// withRevision copies PDF metadata and adds the detected revision as
// revision (rendered), revisionDoc, revisionRev and revisionDate.
func withRevision(meta map[string]string, r extract.Revision) map[string]string {
	out := map[string]string{}
	for k, v := range meta {
		out[k] = v
	}
	if !r.IsZero() {
		out["revision"], out["revisionDoc"], out["revisionRev"], out["revisionDate"] = r.String(), r.Doc, r.Rev, r.Date
	}
	return out
}

// buildImport turns an extraction result into a library import.
func buildImport(res *extract.Result, doc *pdf.Document, req ImportRequest, sha, file, name, src string) *library.Import {
	mfr := firstNonEmpty(req.Manufacturer, res.Manufacturer)
	cat := firstNonEmpty(req.Category, res.Category)
	in := &library.Import{Datasheet: library.Datasheet{
		SHA256: sha, File: file, Filename: name, Title: res.Title, Description: res.Description,
		Manufacturer: mfr, Pages: len(doc.Pages), Families: res.Families, SourceURL: src, Meta: doc.Meta, Extractor: extract.Version,
	}}
	in.Datasheet.Meta = withRevision(doc.Meta, res.Revision)
	mpns := res.PartNumbers
	if len(req.MPNs) > 0 {
		mpns = req.MPNs
	}
	for _, m := range mpns {
		in.Parts = append(in.Parts, library.Part{MPN: m, Manufacturer: mfr, Description: res.Description, Category: cat})
	}
	for _, v := range res.Variants {
		in.Variants = append(in.Variants, library.ImportVariant{MPN: v.MPN, Base: v.Base})
	}
	for i, p := range res.Pages {
		pg := library.Page{Page: i, Width: p.Width, Height: p.Height, Text: res.PageText[i]}
		for _, l := range res.Lines[i] {
			pg.Lines = append(pg.Lines, library.Line{Text: l.Text, Box: l.Box, Size: round1(l.Size)})
		}
		in.Pages = append(in.Pages, pg)
	}
	for _, sec := range res.Sections {
		in.Sections = append(in.Sections, library.Section{Num: sec.Num, Title: sec.Title, Level: sec.Level, Kind: sec.Kind, Page: sec.Page, Y: sec.Y, EndPage: sec.EndPage, EndY: sec.EndY, Text: sec.Text})
	}
	for _, f := range res.Figures {
		in.Figures = append(in.Figures, library.Figure{Page: f.Page, Number: f.Number, Caption: f.Caption, Kind: f.Kind, Box: f.Box, Section: f.Section, XAxis: f.XAxis, YAxis: f.YAxis, AppliesTo: mentionedParts(f.Caption+" "+f.Section, mpns)})
	}
	for _, t := range res.Tables {
		in.Tables = append(in.Tables, library.Table{Page: t.Page, Number: t.Number, Caption: t.Caption, Kind: t.Kind, Section: t.Section, Box: t.Box, Header: t.Header, Rows: t.Rows, Irregular: t.Irregular})
		in.Recommendations = append(in.Recommendations, tableRecommendations(t, mpns)...)
	}
	for _, p := range res.Params {
		box := p.Box
		in.Params = append(in.Params, library.Parameter{
			AppliesTo: mentionedParts(p.Conditions+" "+p.Name, mpns), Key: p.Key, Symbol: p.Symbol, Name: p.Name, Conditions: p.Conditions,
			Min: p.Min, Typ: p.Typ, Max: p.Max, Unit: p.Unit, Section: p.Section, Page: p.Page, Box: &box, Raw: p.Raw,
		})
	}
	for _, p := range res.Pins {
		in.Pins = append(in.Pins, library.Pin{Package: p.Package, Number: p.Number, Name: p.Name, Type: p.Type, Description: p.Description, Page: p.Page})
	}
	return in
}

// recTableRe matches captions of recommended-component tables.
var recTableRe = regexp.MustCompile(`(?i)recommended (inductors?|capacitors?|components?|resistors?|diodes?|mosfets?)|list of (inductors|capacitors)|(inductor|capacitor) (selection|recommendations)`)

// unitInBrackets finds a unit such as [µH] in a header cell.
var unitInBrackets = regexp.MustCompile(`\[([^\]]{1,6})\]`)

// tableRecommendations turns a regular "List of Recommended Inductors"
// style table into recommendations: the role from the caption, the value
// from the first column, the part number and manufacturer from their
// columns, and the remaining columns as conditions.
func tableRecommendations(t extract.Table, mpns []string) []library.Recommendation {
	m := recTableRe.FindStringSubmatch(t.Caption)
	if m == nil || t.Irregular != "" || len(t.Rows) == 0 {
		return nil
	}
	role := strings.ToLower(strings.TrimSuffix(firstNonEmpty(m[1], m[2], m[3]), "s"))
	if role == "component" {
		role = ""
	}
	col := func(words ...string) int {
		for i, h := range t.Header {
			hu := strings.ToUpper(h)
			for _, w := range words {
				if strings.Contains(hu, w) {
					return i
				}
			}
		}
		return -1
	}
	mpnCol, mfrCol := col("PART NUMBER", "PART NO", "P/N", "ORDER"), col("MANUFACTURER", "VENDOR", "MFR")
	valCol := col("INDUCTOR", "INDUCTANCE", "CAPACITOR", "CAPACITANCE", "VALUE", "RESISTOR")
	if valCol < 0 {
		valCol = 0
	}
	if mpnCol < 0 && mfrCol < 0 {
		return nil
	}
	// TI sets units on a second header row ("VALUE [µH]", "[A]"): fold
	// such rows (no part number or manufacturer) into the header.
	header := append([]string(nil), t.Header...)
	valueUnit := ""
	var data [][]string
	for _, row := range t.Rows {
		cell := func(i int) string {
			if i >= 0 && i < len(row) {
				return strings.TrimSpace(row[i])
			}
			return ""
		}
		if cell(mpnCol) == "" && cell(mfrCol) == "" {
			for i, c := range row {
				if i < len(header) && strings.TrimSpace(c) != "" {
					header[i] = strings.TrimSpace(header[i] + " " + c)
				}
			}
			if m := unitInBrackets.FindStringSubmatch(cell(valCol)); m != nil {
				valueUnit = extract.NormalizeUnit(m[1])
			}
			continue
		}
		data = append(data, row)
	}
	var out []library.Recommendation
	for _, row := range data {
		get := func(i int) string {
			if i >= 0 && i < len(row) {
				return strings.TrimSpace(row[i])
			}
			return ""
		}
		value := get(valCol)
		if valueUnit != "" && value != "" && !strings.ContainsAny(value, "µunpmFH") {
			value += " " + valueUnit
		}
		r := library.Recommendation{Role: firstNonEmpty(role, "component"), Value: value, MPN: get(mpnCol), Manufacturer: get(mfrCol), Page: t.Page,
			Notes: "from Table " + t.Number, AppliesTo: mentionedParts(t.Caption, mpns)}
		var cond []string
		for i, c := range row {
			if i == valCol || i == mpnCol || i == mfrCol || strings.TrimSpace(c) == "" || i >= len(header) {
				continue
			}
			cond = append(cond, strings.TrimSpace(header[i])+": "+strings.TrimSpace(c))
		}
		r.Conditions = strings.Join(cond, "; ")
		if r.MPN == "" && r.Value == "" {
			continue
		}
		out = append(out, r)
	}
	return out
}

// mentionedParts returns which of a multi-part datasheet's MPNs some text
// names (rows and figures specific to one device), or nil when it names
// none or the datasheet has a single part.
func mentionedParts(text string, mpns []string) []string {
	if len(mpns) < 2 {
		return nil
	}
	up := strings.ToUpper(text)
	var out []string
	for _, m := range mpns {
		if strings.Contains(up, strings.ToUpper(m)) {
			out = append(out, m)
		}
	}
	if len(out) == len(mpns) {
		return nil
	}
	return out
}

// round1 rounds to one decimal place.
func round1(v float64) float64 { return float64(int(v*10+0.5)) / 10 }

// unsafeChars are replaced in stored filenames.
var unsafeChars = regexp.MustCompile(`[^A-Za-z0-9._-]+`)

// safeName makes a filename safe to store.
func safeName(name string) string {
	n := unsafeChars.ReplaceAllString(filepath.Base(name), "_")
	if !strings.HasSuffix(strings.ToLower(n), ".pdf") {
		n += ".pdf"
	}
	if len(n) > 80 {
		n = n[len(n)-80:]
	}
	return n
}

// expandHome expands a leading ~/.
func expandHome(p string) string {
	if strings.HasPrefix(p, "~/") {
		if h, err := os.UserHomeDir(); err == nil {
			return filepath.Join(h, p[2:])
		}
	}
	return p
}

// firstNonEmpty returns the first non-empty string.
func firstNonEmpty(ss ...string) string {
	for _, s := range ss {
		if strings.TrimSpace(s) != "" {
			return strings.TrimSpace(s)
		}
	}
	return ""
}
