// Package extract turns a PDF's positioned text and graphics into datasheet
// structure: the section tree, figures with their captions, spec tables
// with min/typ/max parameters, pin tables, part numbers, manufacturer and
// device category.
//
// It is purely mechanical and deterministic (no model, no network): the
// goal is a good draft with exact page provenance, which the agent
// connected over MCP then reviews and refines. Everything here works on
// pdf.Document values, so it is unit-tested without the PDF engine.
package extract

import (
	"github.com/aitakemyjob/sheetdiver/internal/pdf"
)

// Version identifies the extractor; stored with each datasheet so
// re-extraction can be offered when it changes.
const Version = "7"

// Line is a run of spans on one baseline that belong together (spans are
// split into separate lines at wide gaps, so two captions side by side
// stay apart).
type Line struct {
	Page  int        `json:"page"`
	Text  string     `json:"text"`
	Box   pdf.Rect   `json:"box"`
	Size  float64    `json:"size"`
	Bold  bool       `json:"bold,omitempty"`
	PageH float64    `json:"-"`
	Spans []pdf.Span `json:"-"`
}

// Section is a heading and the text under it, up to the next heading of
// the same or a higher level.
type Section struct {
	Num     string  `json:"num"`
	Title   string  `json:"title"`
	Level   int     `json:"level"`
	Kind    string  `json:"kind"`
	Page    int     `json:"page"`
	Y       float64 `json:"y"`
	EndPage int     `json:"endPage"`
	EndY    float64 `json:"endY"`
	Text    string  `json:"text"`
	// start and end are reading-order keys (see Layout.Key).
	start, end float64
}

// Figure is a captioned figure and the region it occupies.
type Figure struct {
	Page    int      `json:"page"`
	Number  string   `json:"number"`
	Caption string   `json:"caption"`
	Box     pdf.Rect `json:"box"`
	Kind    string   `json:"kind"`
	Section string   `json:"section"`
	XAxis   string   `json:"xAxis,omitempty"`
	YAxis   string   `json:"yAxis,omitempty"`
}

// Table is a detected table: its header cells and rows of cell text.
type Table struct {
	Page    int        `json:"page"`
	Number  string     `json:"number,omitempty"`
	Caption string     `json:"caption,omitempty"`
	Kind    string     `json:"kind"`
	Section string     `json:"section"`
	Box     pdf.Rect   `json:"box"`
	Header  []string   `json:"header"`
	Rows    [][]string `json:"rows"`
	// Irregular says why the cells are unreliable (merged or grouped rows,
	// matrix layouts), or is empty for a plain grid. The table image is
	// the source of truth for irregular tables.
	Irregular string `json:"irregular,omitempty"`
}

// Param is one spec-table row with numeric limits.
type Param struct {
	Key        string   `json:"key,omitempty"`
	Symbol     string   `json:"symbol,omitempty"`
	Name       string   `json:"name"`
	Conditions string   `json:"conditions,omitempty"`
	Min        *float64 `json:"min,omitempty"`
	Typ        *float64 `json:"typ,omitempty"`
	Max        *float64 `json:"max,omitempty"`
	Unit       string   `json:"unit,omitempty"`
	Section    string   `json:"section"`
	Page       int      `json:"page"`
	Box        pdf.Rect `json:"box"`
	Raw        string   `json:"raw"`
}

// Pin is one row of a pin-function table.
type Pin struct {
	// Package names the package column for multi-package tables
	// ("20-BUMP WLP"), empty otherwise.
	Package     string `json:"package,omitempty"`
	Number      string `json:"number"`
	Name        string `json:"name"`
	Type        string `json:"type,omitempty"`
	Description string `json:"description,omitempty"`
	Page        int    `json:"page"`
}

// Result is everything Analyze found.
type Result struct {
	Title        string    `json:"title"`
	Description  string    `json:"description"`
	Manufacturer string    `json:"manufacturer"`
	PartNumbers  []string  `json:"partNumbers"`
	Families     []string  `json:"families"`
	Revision     Revision  `json:"revision"`
	Variants     []Variant `json:"variants"`
	Category     string    `json:"category"`
	Lines        [][]Line  `json:"-"`
	PageText     []string  `json:"-"`
	Sections     []Section `json:"sections"`
	Figures      []Figure  `json:"figures"`
	Tables       []Table   `json:"tables"`
	Params       []Param   `json:"params"`
	Pins         []Pin     `json:"pins"`
	// Packages are the package outlines' dimensions and land patterns.
	Packages []Package `json:"packages,omitempty"`
	// PinWarnings say when the pins read fall short of what the datasheet
	// says its packages have (or none were read).
	PinWarnings []string    `json:"pinWarnings,omitempty"`
	BodySize    float64     `json:"bodySize"`
	Pages       []PageShape `json:"pages"`
	// Language is the datasheet's main language: en, zh, ja or ko.
	Language string `json:"language"`
}

// PageShape is a page's size in points.
type PageShape struct {
	Width  float64 `json:"width"`
	Height float64 `json:"height"`
}

// Analyze extracts datasheet structure from a document. filename is used
// as a last-resort part number.
func Analyze(doc *pdf.Document, filename string) *Result {
	r := &Result{Revision: DetectRevision(doc), Language: DetectLanguage(doc)}
	for _, p := range doc.Pages {
		r.Pages = append(r.Pages, PageShape{p.Width, p.Height})
	}
	r.BodySize = bodySize(doc)
	r.Lines = make([][]Line, len(doc.Pages))
	r.PageText = make([]string, len(doc.Pages))
	for i, p := range doc.Pages {
		r.Lines[i] = Lines(p)
	}
	lay0 := NewLayout(doc, r.Lines)
	for i := range r.Lines {
		lay0.Sort(i, r.Lines[i])
		r.PageText[i] = pageText(r.Lines[i])
	}
	lay := NewLayout(doc, r.Lines)
	r.Sections = Sections(doc, r.Lines, r.BodySize, lay)
	r.Figures = Figures(doc, r.Lines, r.Sections, lay)
	r.Figures = append(r.Figures, TitledFigures(doc, r.Lines, r.Sections, lay, r.Figures)...)
	r.Figures = append(r.Figures, SectionFigures(doc, r.Lines, r.Sections, lay, r.Figures)...)
	r.Title, r.Description = titleAndDescription(doc, r.Lines)
	r.Category = Category(r.Title, r.Description, first(r.PageText))
	tables, params, pins := Tables(doc, r.Sections, lay)
	r.Tables, r.Params, r.Pins = tables, params, pins
	if len(r.Pins) == 0 {
		r.Pins = ProsePins(r.Lines, r.Sections, lay)
	}
	if len(r.Pins) == 0 {
		r.Pins = drawingPins(r.Lines, r.Category)
	}
	fillSectionText(r.Sections, r.Lines, lay, r.BodySize, contentRegions(r.Figures, r.Tables))
	r.Pins = numberPads(r.Pins)
	r.Packages = Packages(doc, r.Sections)
	r.PinWarnings = pinWarnings(r.Pins, packagePinCounts(pinTexts(r.Sections, r.Figures), first(r.PageText)))
	r.Manufacturer = Manufacturer(doc, r.PageText)
	r.Families = Families(doc, r.PageText)
	r.PartNumbers, r.Variants = Expand(r.PageText, PartNumbers(doc, r.Lines, filename), r.Families)
	// A row whose minimum exceeds its maximum was misread; don't let it
	// stand for a key.
	for i, p := range r.Params {
		if p.Min != nil && p.Max != nil && *p.Min > *p.Max {
			r.Params[i].Key = ""
		}
	}
	if !hasKey(r.Params, "iout") && len(r.Lines) > 0 {
		r.Params = append(r.Params, RatedCurrent(r.Category, r.Title, r.Lines[0], r.PartNumbers)...)
	}
	return r
}

// hasKey reports whether any parameter has the key.
func hasKey(ps []Param, key string) bool {
	for _, p := range ps {
		if p.Key == key {
			return true
		}
	}
	return false
}

// first returns the first element or "".
func first(s []string) string {
	if len(s) == 0 {
		return ""
	}
	return s[0]
}
