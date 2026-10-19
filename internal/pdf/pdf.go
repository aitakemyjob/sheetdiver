// Package pdf reads datasheet PDFs: positioned text, the bounds of drawn
// graphics, bookmarks and metadata, and renders pages or regions of pages
// to PNG. It wraps PDFium compiled to WebAssembly (run by wazero), so it
// needs no cgo and never touches the filesystem from inside the engine:
// documents are passed in as bytes.
//
// Every coordinate this package returns is in PDF points with the origin
// at the top-left of the page and y growing downwards, which is what the
// UI and the renderer's crop rectangles use.
package pdf

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image/png"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/klippa-app/go-pdfium"
	"github.com/klippa-app/go-pdfium/enums"
	"github.com/klippa-app/go-pdfium/references"
	"github.com/klippa-app/go-pdfium/requests"
	"github.com/klippa-app/go-pdfium/responses"
	"github.com/klippa-app/go-pdfium/webassembly"
	"github.com/tetratelabs/wazero"
	"github.com/tetratelabs/wazero/api"
	"github.com/tetratelabs/wazero/experimental"
)

// Rect is a box in points, top-left origin.
type Rect struct {
	X0 float64 `json:"x0"`
	Y0 float64 `json:"y0"`
	X1 float64 `json:"x1"`
	Y1 float64 `json:"y1"`
}

// W is the rectangle's width.
func (r Rect) W() float64 { return r.X1 - r.X0 }

// H is the rectangle's height.
func (r Rect) H() float64 { return r.Y1 - r.Y0 }

// Empty reports whether the rectangle has no area.
func (r Rect) Empty() bool { return r.X1 <= r.X0 || r.Y1 <= r.Y0 }

// Union returns the smallest rectangle containing both. An empty
// rectangle is ignored.
func (r Rect) Union(o Rect) Rect {
	if r.Empty() {
		return o
	}
	if o.Empty() {
		return r
	}
	return Rect{min(r.X0, o.X0), min(r.Y0, o.Y0), max(r.X1, o.X1), max(r.Y1, o.Y1)}
}

// Intersects reports whether the rectangles overlap.
func (r Rect) Intersects(o Rect) bool {
	return r.X0 < o.X1 && o.X0 < r.X1 && r.Y0 < o.Y1 && o.Y0 < r.Y1
}

// Contains reports whether o lies entirely inside r.
func (r Rect) Contains(o Rect) bool {
	return o.X0 >= r.X0 && o.X1 <= r.X1 && o.Y0 >= r.Y0 && o.Y1 <= r.Y1
}

// Inset grows (d > 0) or shrinks (d < 0) the rectangle on every side.
func (r Rect) Inset(dx, dy float64) Rect {
	return Rect{r.X0 - dx, r.Y0 - dy, r.X1 + dx, r.Y1 + dy}
}

// Span is a run of text on one line in one font.
type Span struct {
	Text string  `json:"text"`
	Box  Rect    `json:"box"`
	Size float64 `json:"size"`
	Bold bool    `json:"bold,omitempty"`
}

// Graphic is the bounding box of a non-text page object: a vector path,
// an image, a shading or a form XObject (TI draws each graph as one form).
type Graphic struct {
	Box  Rect   `json:"box"`
	Kind string `json:"kind"`
}

// Page is one page's extracted content.
type Page struct {
	Index    int       `json:"index"`
	Width    float64   `json:"width"`
	Height   float64   `json:"height"`
	Spans    []Span    `json:"spans"`
	Graphics []Graphic `json:"graphics"`
	// Ink is set for pages that mention figures (see InkMap).
	Ink *InkMap `json:"-"`
}

// Bookmark is an outline entry. Y is the destination's top in points when
// the PDF records one (HasY).
type Bookmark struct {
	Title    string     `json:"title"`
	Page     int        `json:"page"`
	Y        float64    `json:"y"`
	HasY     bool       `json:"hasY"`
	Children []Bookmark `json:"children,omitempty"`
}

// Document is everything extracted from a PDF.
type Document struct {
	Pages     []Page            `json:"pages"`
	Meta      map[string]string `json:"meta"`
	Bookmarks []Bookmark        `json:"bookmarks"`
}

// Engine owns the PDFium runtime. It is created lazily on first use (the
// WebAssembly module takes a moment to compile) and serializes calls,
// since one PDFium instance is not safe for concurrent use.
type Engine struct {
	// CacheDir, when set, holds wazero's compilation cache so later
	// starts skip compiling the module.
	CacheDir string

	mu   sync.Mutex
	pool pdfium.Pool
	inst pdfium.Pdfium
	err  error
}

// NewEngine creates an engine; nothing is loaded until the first call.
func NewEngine(cacheDir string) *Engine { return &Engine{CacheDir: cacheDir} }

// instance returns the PDFium instance, starting it on first use. Callers
// must hold e.mu.
func (e *Engine) instance() (pdfium.Pdfium, error) {
	if e.inst != nil || e.err != nil {
		return e.inst, e.err
	}
	// PDFium's module needs exception handling on top of the core features.
	rc := wazero.NewRuntimeConfig().WithCoreFeatures(api.CoreFeaturesV2 | experimental.CoreFeaturesExceptionHandling)
	if e.CacheDir != "" {
		if err := os.MkdirAll(e.CacheDir, 0o755); err == nil {
			if cache, err := wazero.NewCompilationCacheWithDir(e.CacheDir); err == nil {
				rc = rc.WithCompilationCache(cache)
			}
		}
	}
	pool, err := webassembly.Init(webassembly.Config{
		MinIdle: 1, MaxIdle: 1, MaxTotal: 1,
		// No filesystem mounts: documents always arrive as bytes.
		FSConfig:      wazero.NewFSConfig(),
		RuntimeConfig: rc,
	})
	if err != nil {
		e.err = fmt.Errorf("start PDF engine: %w", err)
		return nil, e.err
	}
	inst, err := pool.GetInstance(time.Minute)
	if err != nil {
		_ = pool.Close()
		e.err = fmt.Errorf("start PDF engine: %w", err)
		return nil, e.err
	}
	e.pool, e.inst = pool, inst
	return inst, nil
}

// Close releases the PDFium runtime.
func (e *Engine) Close() error {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.pool == nil {
		return nil
	}
	if e.inst != nil {
		_ = e.inst.Close()
	}
	err := e.pool.Close()
	e.pool, e.inst = nil, nil
	return err
}

// ErrNotPDF is returned for data that doesn't start with a PDF header.
var ErrNotPDF = errors.New("not a PDF file")

// IsPDF reports whether data looks like a PDF (the header may follow a
// little leading junk, as the spec allows).
func IsPDF(data []byte) bool {
	head := data
	if len(head) > 1024 {
		head = head[:1024]
	}
	return bytes.Contains(head, []byte("%PDF-"))
}

// withDoc opens data, runs fn with the document reference and closes it.
func (e *Engine) withDoc(data []byte, fn func(p pdfium.Pdfium, doc references.FPDF_DOCUMENT) error) error {
	if !IsPDF(data) {
		return ErrNotPDF
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	p, err := e.instance()
	if err != nil {
		return err
	}
	d, err := p.OpenDocument(&requests.OpenDocument{File: &data})
	if err != nil {
		return fmt.Errorf("open PDF: %w", err)
	}
	defer p.FPDF_CloseDocument(&requests.FPDF_CloseDocument{Document: d.Document}) //nolint:errcheck // closing a read-only document
	return fn(p, d.Document)
}

// Progress is called after each page is extracted.
type Progress func(done, total int)

// Extract reads text spans, graphic bounds, bookmarks and metadata from
// every page. ctx cancels between pages.
func (e *Engine) Extract(ctx context.Context, data []byte, progress Progress) (*Document, error) {
	var out *Document
	err := e.withDoc(data, func(p pdfium.Pdfium, doc references.FPDF_DOCUMENT) error {
		n, err := p.FPDF_GetPageCount(&requests.FPDF_GetPageCount{Document: doc})
		if err != nil {
			return err
		}
		out = &Document{Meta: meta(p, doc)}
		heights := make([]float64, n.PageCount)
		for i := 0; i < n.PageCount; i++ {
			if err := ctx.Err(); err != nil {
				return err
			}
			pg, err := extractPage(p, doc, i)
			if err != nil {
				return fmt.Errorf("page %d: %w", i+1, err)
			}
			heights[i] = pg.Height
			if mentionsFigure(pg.Spans) {
				pg.Ink = inkMap(p, doc, i)
			}
			out.Pages = append(out.Pages, pg)
			if progress != nil {
				progress(i+1, n.PageCount)
			}
		}
		out.Bookmarks = bookmarks(p, doc, heights)
		return nil
	})
	return out, err
}

// meta returns the document information dictionary.
func meta(p pdfium.Pdfium, doc references.FPDF_DOCUMENT) map[string]string {
	m := map[string]string{}
	md, err := p.GetMetaData(&requests.GetMetaData{Document: doc})
	if err != nil {
		return m
	}
	for _, t := range md.Tags {
		if v := strings.TrimSpace(t.Value); v != "" {
			m[t.Tag] = v
		}
	}
	return m
}

// extractPage reads one page's text runs and graphic object bounds.
func extractPage(p pdfium.Pdfium, doc references.FPDF_DOCUMENT, index int) (Page, error) {
	ref := requests.Page{ByIndex: &requests.PageByIndex{Document: doc, Index: index}}
	size, err := p.GetPageSize(&requests.GetPageSize{Page: ref})
	if err != nil {
		return Page{}, err
	}
	pg := Page{Index: index, Width: size.Width, Height: size.Height, Spans: []Span{}, Graphics: []Graphic{}}
	txt, err := p.GetPageTextStructured(&requests.GetPageTextStructured{
		Page: ref, Mode: requests.GetPageTextStructuredModeChars, CollectFontInformation: true,
	})
	if err != nil {
		return Page{}, err
	}
	pg.Spans = charSpans(txt.Chars, pg.Height)
	cnt, err := p.FPDFPage_CountObjects(&requests.FPDFPage_CountObjects{Page: ref})
	if err != nil {
		return pg, nil // text is still useful without graphics
	}
	for i := 0; i < cnt.Count; i++ {
		o, err := p.FPDFPage_GetObject(&requests.FPDFPage_GetObject{Page: ref, Index: i})
		if err != nil {
			continue
		}
		t, err := p.FPDFPageObj_GetType(&requests.FPDFPageObj_GetType{PageObject: o.PageObject})
		if err != nil || t.Type == enums.FPDF_PAGEOBJ_TEXT || t.Type == enums.FPDF_PAGEOBJ_UNKNOWN {
			continue
		}
		b, err := p.FPDFPageObj_GetBounds(&requests.FPDFPageObj_GetBounds{PageObject: o.PageObject})
		if err != nil {
			continue
		}
		box := Rect{float64(b.Left), pg.Height - float64(b.Top), float64(b.Right), pg.Height - float64(b.Bottom)}
		pg.Graphics = append(pg.Graphics, Graphic{Box: box, Kind: objKind(t.Type)})
	}
	return pg, nil
}

// mentionsFigure reports whether a page's text has a figure caption.
func mentionsFigure(spans []Span) bool {
	for _, s := range spans {
		t := strings.TrimSpace(s.Text)
		if strings.HasPrefix(t, "Figure") || strings.HasPrefix(t, "FIGURE") || strings.HasPrefix(t, "Fig.") || strings.HasPrefix(t, "Fig ") {
			return true
		}
	}
	return false
}

// inkMap renders a page at InkDPI and thresholds it; nil on failure.
func inkMap(p pdfium.Pdfium, doc references.FPDF_DOCUMENT, index int) *InkMap {
	img, err := p.RenderPageInDPI(&requests.RenderPageInDPI{
		Page: requests.Page{ByIndex: &requests.PageByIndex{Document: doc, Index: index}},
		DPI:  InkDPI,
	})
	if err != nil {
		return nil
	}
	defer img.Cleanup()
	return newInkMap(img.Result.RenderedImage, float64(InkDPI)/72)
}

// objKind names a PDFium page object type.
func objKind(t enums.FPDF_PAGEOBJ) string {
	switch t {
	case enums.FPDF_PAGEOBJ_PATH:
		return "path"
	case enums.FPDF_PAGEOBJ_IMAGE:
		return "image"
	case enums.FPDF_PAGEOBJ_SHADING:
		return "shading"
	case enums.FPDF_PAGEOBJ_FORM:
		return "form"
	}
	return "other"
}

// charSpans groups characters into spans: runs on one baseline in one
// font size, broken at gaps wider than a word space. Building spans from
// characters (rather than PDFium's runs) keeps table columns apart even
// when a PDF draws a whole table row as one text run.
func charSpans(chars []*responses.GetPageTextStructuredChar, pageH float64) []Span {
	var out []Span
	var cur *Span
	var b strings.Builder
	space := false
	flush := func() {
		if cur != nil {
			cur.Text = strings.TrimSpace(b.String())
			if cur.Text != "" {
				out = append(out, *cur)
			}
		}
		cur = nil
		b.Reset()
		space = false
	}
	for _, c := range chars {
		t := strings.ReplaceAll(c.Text, "\u0000", "")
		if strings.TrimSpace(t) == "" {
			space = true
			if t == "\n" || t == "\r" || t == "\r\n" {
				flush()
			}
			continue
		}
		pp := c.PointPosition
		box := Rect{pp.Left, pageH - pp.Top, pp.Right, pageH - pp.Bottom}
		size, bold := box.H(), false
		if fi := c.FontInformation; fi != nil {
			if fi.Size > 0 {
				size = fi.Size
			}
			if fi.RenderedSize > 0 {
				size = fi.RenderedSize
			}
			name := strings.ToLower(fi.Name)
			bold = fi.Weight >= 600 || strings.Contains(name, "bold") || strings.Contains(name, "black") || strings.Contains(name, "heavy")
		}
		if cur != nil {
			gap := box.X0 - cur.Box.X1
			cy, ny := (cur.Box.Y0+cur.Box.Y1)/2, (box.Y0+box.Y1)/2
			sameLine := math.Abs(cy-ny) < math.Max(1.5, math.Min(cur.Size, size)*0.35)
			sameFont := math.Abs(cur.Size-size) < 0.6
			if !sameLine || !sameFont || gap > size*0.8 || gap < -size*0.5 {
				flush()
			}
		}
		if cur == nil {
			cur = &Span{Box: box, Size: size, Bold: bold}
		} else {
			// Narrow glyphs ("1") leave wide gaps between ink boxes, so trust
			// the PDF's own spaces and only add one for a clearly wider gap.
			if space || box.X0-cur.Box.X1 > size*0.4 {
				b.WriteByte(' ')
			}
			cur.Box = cur.Box.Union(box)
			cur.Bold = cur.Bold && bold
		}
		b.WriteString(t)
		space = false
	}
	flush()
	return out
}

// bookmarks reads the outline tree with destination pages and positions.
func bookmarks(p pdfium.Pdfium, doc references.FPDF_DOCUMENT, heights []float64) []Bookmark {
	bm, err := p.GetBookmarks(&requests.GetBookmarks{Document: doc})
	if err != nil {
		return nil
	}
	var conv func(in []responses.GetBookmarksBookmark) []Bookmark
	conv = func(in []responses.GetBookmarksBookmark) []Bookmark {
		out := make([]Bookmark, 0, len(in))
		for _, b := range in {
			o := Bookmark{Title: strings.Join(strings.Fields(b.Title), " "), Page: -1}
			dest := b.DestInfo
			if dest == nil && b.ActionInfo != nil && b.ActionInfo.DestInfo != nil {
				dest = b.ActionInfo.DestInfo
			}
			if dest != nil {
				o.Page = dest.PageIndex
				if loc, err := p.FPDFDest_GetLocationInPage(&requests.FPDFDest_GetLocationInPage{Dest: dest.Reference}); err == nil && loc.Y != nil && o.Page >= 0 && o.Page < len(heights) {
					o.Y, o.HasY = heights[o.Page]-float64(*loc.Y), true
					if o.Y < 0 {
						o.Y = 0
					}
				}
			}
			o.Children = conv(b.Children)
			out = append(out, o)
		}
		return out
	}
	return conv(bm.Bookmarks)
}

// Render draws a page (or the crop region of it, in points) at dpi and
// returns PNG bytes.
func (e *Engine) Render(data []byte, page int, crop *Rect, dpi int) ([]byte, error) {
	if dpi <= 0 {
		dpi = 110
	}
	var out []byte
	err := e.withDoc(data, func(p pdfium.Pdfium, doc references.FPDF_DOCUMENT) error {
		req := &requests.RenderPageInDPI{
			Page: requests.Page{ByIndex: &requests.PageByIndex{Document: doc, Index: page}},
			DPI:  dpi,
		}
		if crop != nil && !crop.Empty() {
			req.Crop = &requests.RenderPageCrop{X: max(0, crop.X0), Y: max(0, crop.Y0), Width: crop.W(), Height: crop.H()}
		}
		img, err := p.RenderPageInDPI(req)
		if err != nil {
			return err
		}
		defer img.Cleanup()
		var buf bytes.Buffer
		if err := png.Encode(&buf, img.Result.RenderedImage); err != nil {
			return err
		}
		out = buf.Bytes()
		return nil
	})
	return out, err
}

// PageCount returns the number of pages in data.
func (e *Engine) PageCount(data []byte) (int, error) {
	var n int
	err := e.withDoc(data, func(p pdfium.Pdfium, doc references.FPDF_DOCUMENT) error {
		c, err := p.FPDF_GetPageCount(&requests.FPDF_GetPageCount{Document: doc})
		if err == nil {
			n = c.PageCount
		}
		return err
	})
	return n, err
}

// SortSpans orders spans in reading order: top to bottom, then left to
// right for spans on the same line.
func SortSpans(spans []Span) {
	sort.SliceStable(spans, func(i, j int) bool {
		a, b := spans[i].Box, spans[j].Box
		ca, cb := (a.Y0+a.Y1)/2, (b.Y0+b.Y1)/2
		tol := min(a.H(), b.H()) / 2
		if d := ca - cb; d > tol || d < -tol {
			return ca < cb
		}
		return a.X0 < b.X0
	})
}

// DefaultCacheDir is the wazero compilation cache under the user cache.
func DefaultCacheDir() string {
	if d, err := os.UserCacheDir(); err == nil {
		return filepath.Join(d, "sheetdiver", "wazero")
	}
	return ""
}
