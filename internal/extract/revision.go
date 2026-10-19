package extract

import (
	"regexp"
	"strings"

	"github.com/aitakemyjob/sheetdiver/internal/pdf"
)

// Revision identifies which edition of a datasheet a PDF is.
type Revision struct {
	// Doc is the manufacturer's document number (SLVSCB0, DS14216).
	Doc string `json:"doc,omitempty"`
	// Rev is the revision within it (E, 5, 3.1).
	Rev string `json:"rev,omitempty"`
	// Date is the revision date as printed (MAY 2023, 6/23).
	Date string `json:"date,omitempty"`
}

// String renders a revision as "SLVSCB0 rev E (MAY 2023)".
func (r Revision) String() string {
	var parts []string
	if r.Doc != "" {
		parts = append(parts, r.Doc)
	}
	if r.Rev != "" {
		parts = append(parts, "rev "+r.Rev)
	}
	s := strings.Join(parts, " ")
	if r.Date != "" {
		if s == "" {
			return r.Date
		}
		s += " (" + r.Date + ")"
	}
	return s
}

// IsZero reports whether nothing was found.
func (r Revision) IsZero() bool { return r == Revision{} }

// revisionPatterns match the revision lines manufacturers print in page
// headers and footers, most specific first. Each names its groups doc,
// rev and date.
var revisionPatterns = []*regexp.Regexp{
	// TI: SLVSCB0E – JANUARY 2014 – REVISED MAY 2023 (the last letter of
	// the literature number is the revision; none for the original).
	regexp.MustCompile(`\b(?P<doc>S[A-Z]{3}[0-9A-Z]{3})(?P<rev>[A-Z])?\s*[–—-]\s*(?:[A-Z]+\s+\d{4})\s*[–—-]\s*REVISED\s+(?P<date>[A-Z]+\s+\d{4})`),
	regexp.MustCompile(`\b(?P<doc>S[A-Z]{3}[0-9A-Z]{3})(?P<rev>[A-Z])?\s*[–—-]\s*(?P<date>(?:JANUARY|FEBRUARY|MARCH|APRIL|MAY|JUNE|JULY|AUGUST|SEPTEMBER|OCTOBER|NOVEMBER|DECEMBER)\s+\d{4})\b`),
	// Maxim/ADI: 19-7515; Rev 8; 6/23
	regexp.MustCompile(`\b(?P<doc>19-\d{4,5});\s*Rev\s*(?P<rev>[0-9A-Z]+);\s*(?P<date>\d{1,2}/\d{2,4})`),
	// Diodes: Document number: DS39724 Rev. 2 - 2
	regexp.MustCompile(`Document number:\s*(?P<doc>DS\d+)\s+Rev\.\s*(?P<rev>\d+\s*-\s*\d+)`),
	// ST: DS14216 Rev 5
	regexp.MustCompile(`\b(?P<doc>DS\d{4,6})\s+Rev\s+(?P<rev>\d+)\b`),
	// Microchip: DS20001826E-page 1
	regexp.MustCompile(`\b(?P<doc>DS\d{8})(?P<rev>[A-Z])-page\b`),
	// AOS and others: Rev 3.1: July 2023
	regexp.MustCompile(`\bRev(?:ision)?\.?\s*(?P<rev>\d+(?:\.\d+)?)\s*[:,–-]\s*(?P<date>[A-Z][a-z]+\.?\s+\d{4})`),
	// ADI: Rev. C (anywhere on the first pages; weakest)
	regexp.MustCompile(`\bRev\.\s*(?P<rev>[A-Z]|\d+)\b`),
}

// titleRevRe matches TI's PDF title suffix "datasheet (Rev. E)".
var titleRevRe = regexp.MustCompile(`\(Rev\.\s*([A-Z0-9]+)\)\s*$`)

// DetectRevision finds a datasheet's document number, revision and date
// from its first pages' headers and footers, falling back to the PDF's
// title and keywords.
func DetectRevision(doc *pdf.Document) Revision {
	var lines []string
	for i := 0; i < len(doc.Pages) && i < 3; i++ {
		for _, s := range doc.Pages[i].Spans {
			lines = append(lines, s.Text)
		}
	}
	for _, re := range revisionPatterns {
		for _, l := range lines {
			if m := re.FindStringSubmatch(l); m != nil {
				r := Revision{}
				for i, name := range re.SubexpNames() {
					v := strings.Join(strings.Fields(m[i]), " ")
					switch name {
					case "doc":
						r.Doc = v
					case "rev":
						r.Rev = strings.ReplaceAll(v, " ", "")
					case "date":
						r.Date = strings.ToUpper(v)
					}
				}
				if r.Doc != "" && r.Rev == "" && strings.HasPrefix(r.Doc, "S") {
					r.Rev = "original"
				}
				return r
			}
		}
	}
	if m := titleRevRe.FindStringSubmatch(doc.Meta["Title"]); m != nil {
		return Revision{Rev: m[1]}
	}
	return Revision{}
}
