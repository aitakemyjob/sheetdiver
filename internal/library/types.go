package library

import (
	"time"

	"github.com/aitakemyjob/sheetdiver/internal/pdf"
)

// Datasheet is a stored PDF and its document-level facts.
type Datasheet struct {
	ID           int64             `json:"id"`
	SHA256       string            `json:"sha256"`
	File         string            `json:"file"`
	Filename     string            `json:"filename"`
	Title        string            `json:"title"`
	Description  string            `json:"description"`
	Manufacturer string            `json:"manufacturer"`
	Pages        int               `json:"pages"`
	Families     []string          `json:"families"`
	SourceURL    string            `json:"sourceUrl,omitempty"`
	Meta         map[string]string `json:"meta,omitempty"`
	Extractor    string            `json:"extractor"`
	ImportedAt   time.Time         `json:"importedAt"`
}

// Part is a base manufacturer part number covered by a datasheet.
type Part struct {
	ID           int64    `json:"id"`
	MPN          string   `json:"mpn"`
	Manufacturer string   `json:"manufacturer"`
	Description  string   `json:"description"`
	Category     string   `json:"category"`
	Package      string   `json:"package,omitempty"`
	DatasheetID  int64    `json:"datasheetId"`
	Tags         []string `json:"tags"`
	// Favorite marks a part the user starred.
	Favorite bool `json:"favorite"`
	// Status is the part's standing in the user's designs: preferred,
	// approved, avoid, nrnd (not recommended for new designs), eol, or
	// empty.
	Status    string    `json:"status,omitempty"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

// Part statuses.
const (
	StatusPreferred = "preferred"
	StatusApproved  = "approved"
	StatusAvoid     = "avoid"
	StatusNRND      = "nrnd"
	StatusEOL       = "eol"
)

// Statuses lists the valid part statuses with a description.
var Statuses = map[string]string{
	StatusPreferred: "first choice for new designs",
	StatusApproved:  "qualified and fine to use",
	StatusAvoid:     "do not use (problems, cost, supply)",
	StatusNRND:      "not recommended for new designs",
	StatusEOL:       "end of life / obsolete",
}

// Discouraged reports whether a status warns against using the part.
func Discouraged(status string) bool {
	return status == StatusAvoid || status == StatusNRND || status == StatusEOL
}

// Variant is an orderable code (or other alias) of a part.
type Variant struct {
	MPN     string `json:"mpn"`
	PartID  int64  `json:"partId"`
	Package string `json:"package,omitempty"`
	Notes   string `json:"notes,omitempty"`
	Source  string `json:"source"`
}

// Line is a stored text line with its position, for page overlays and
// region text.
type Line struct {
	Text string   `json:"t"`
	Box  pdf.Rect `json:"b"`
	Size float64  `json:"s"`
}

// Page is a stored page: size, text and lines.
type Page struct {
	DatasheetID int64   `json:"datasheetId"`
	Page        int     `json:"page"`
	Width       float64 `json:"width"`
	Height      float64 `json:"height"`
	Text        string  `json:"text"`
	Lines       []Line  `json:"lines,omitempty"`
}

// Section is a heading and its text.
type Section struct {
	ID          int64   `json:"id"`
	DatasheetID int64   `json:"datasheetId"`
	Ord         int     `json:"ord"`
	Num         string  `json:"num"`
	Title       string  `json:"title"`
	Level       int     `json:"level"`
	Kind        string  `json:"kind"`
	Page        int     `json:"page"`
	Y           float64 `json:"y"`
	EndPage     int     `json:"endPage"`
	EndY        float64 `json:"endY"`
	Text        string  `json:"text,omitempty"`
}

// Curve describes one trace on a graph (filled in by the agent).
type Curve struct {
	Label      string `json:"label"`
	Conditions string `json:"conditions,omitempty"`
	Summary    string `json:"summary,omitempty"`
}

// Figure is a captioned figure. Title, axes, conditions, description and
// curves are what the figure shows, refined by the agent after it looks
// at the image.
type Figure struct {
	ID          int64     `json:"id"`
	DatasheetID int64     `json:"datasheetId"`
	Page        int       `json:"page"`
	Number      string    `json:"number"`
	Caption     string    `json:"caption"`
	Kind        string    `json:"kind"`
	Box         pdf.Rect  `json:"box"`
	Section     string    `json:"section"`
	Title       string    `json:"title,omitempty"`
	XAxis       string    `json:"xAxis,omitempty"`
	YAxis       string    `json:"yAxis,omitempty"`
	Conditions  string    `json:"conditions,omitempty"`
	Description string    `json:"description,omitempty"`
	Curves      []Curve   `json:"curves,omitempty"`
	AppliesTo   []string  `json:"appliesTo,omitempty"`
	Source      string    `json:"source"`
	UpdatedAt   time.Time `json:"updatedAt"`
}

// Table is a detected table.
type Table struct {
	ID          int64      `json:"id"`
	DatasheetID int64      `json:"datasheetId"`
	Page        int        `json:"page"`
	Number      string     `json:"number,omitempty"`
	Caption     string     `json:"caption,omitempty"`
	Kind        string     `json:"kind"`
	Section     string     `json:"section"`
	Box         pdf.Rect   `json:"box"`
	Header      []string   `json:"header"`
	Rows        [][]string `json:"rows,omitempty"`
	// Irregular says why automatically read cells are unreliable (the
	// table's image is then the reference); empty for a plain grid or a
	// table the agent or user transcribed.
	Irregular   string    `json:"irregular,omitempty"`
	Title       string    `json:"title,omitempty"`
	Description string    `json:"description,omitempty"`
	Source      string    `json:"source"`
	UpdatedAt   time.Time `json:"updatedAt"`
}

// Parameter is one specified value: min/typ/max with unit, conditions and
// where it was found.
type Parameter struct {
	ID          int64     `json:"id"`
	DatasheetID int64     `json:"datasheetId"`
	AppliesTo   []string  `json:"appliesTo,omitempty"`
	Key         string    `json:"key,omitempty"`
	Symbol      string    `json:"symbol,omitempty"`
	Name        string    `json:"name"`
	Conditions  string    `json:"conditions,omitempty"`
	Min         *float64  `json:"min,omitempty"`
	Typ         *float64  `json:"typ,omitempty"`
	Max         *float64  `json:"max,omitempty"`
	Unit        string    `json:"unit,omitempty"`
	Section     string    `json:"section"`
	Page        int       `json:"page"`
	Box         *pdf.Rect `json:"box,omitempty"`
	Raw         string    `json:"raw,omitempty"`
	Note        string    `json:"note,omitempty"`
	Source      string    `json:"source"`
	UpdatedAt   time.Time `json:"updatedAt"`
}

// Pin is one pin of a package.
type Pin struct {
	ID          int64 `json:"id"`
	DatasheetID int64 `json:"datasheetId"`
	// Part is the part number the pin is recorded for, when the datasheet
	// covers several parts with different pinouts; "" for every part.
	Part        string `json:"part,omitempty"`
	Package     string `json:"package,omitempty"`
	Number      string `json:"number"`
	Name        string `json:"name"`
	Type        string `json:"type,omitempty"`
	Description string `json:"description,omitempty"`
	Page        int    `json:"page"`
	Source      string `json:"source"`
}

// RegisterField is a bit field of a register.
type RegisterField struct {
	Name        string `json:"name"`
	Bits        string `json:"bits"`
	Access      string `json:"access,omitempty"`
	Reset       string `json:"reset,omitempty"`
	Description string `json:"description,omitempty"`
}

// Register is a device register (recorded by the agent from the register
// map text).
type Register struct {
	ID          int64           `json:"id"`
	DatasheetID int64           `json:"datasheetId"`
	Name        string          `json:"name"`
	Address     string          `json:"address,omitempty"`
	Width       int             `json:"width,omitempty"`
	Reset       string          `json:"reset,omitempty"`
	Access      string          `json:"access,omitempty"`
	Description string          `json:"description,omitempty"`
	Fields      []RegisterField `json:"fields,omitempty"`
	Page        int             `json:"page"`
	Source      string          `json:"source"`
}

// Recommendation is a component the datasheet recommends for a role
// (inductor, output capacitor, feedback resistor, …), possibly with
// specific part numbers.
type Recommendation struct {
	ID           int64    `json:"id"`
	DatasheetID  int64    `json:"datasheetId"`
	AppliesTo    []string `json:"appliesTo,omitempty"`
	Role         string   `json:"role"`
	Value        string   `json:"value,omitempty"`
	MPN          string   `json:"mpn,omitempty"`
	Manufacturer string   `json:"manufacturer,omitempty"`
	Conditions   string   `json:"conditions,omitempty"`
	Notes        string   `json:"notes,omitempty"`
	Page         int      `json:"page"`
	Source       string   `json:"source"`
}

// Note is free text attached to a part, datasheet, figure, parameter or
// project.
type Note struct {
	ID         int64     `json:"id"`
	TargetKind string    `json:"targetKind"`
	TargetID   string    `json:"targetId"`
	Body       string    `json:"body"`
	Author     string    `json:"author"`
	CreatedAt  time.Time `json:"createdAt"`
	UpdatedAt  time.Time `json:"updatedAt"`
}

// Note target kinds.
const (
	TargetPart      = "part"
	TargetDatasheet = "datasheet"
	TargetFigure    = "figure"
	TargetParameter = "parameter"
	TargetProject   = "project"
)
