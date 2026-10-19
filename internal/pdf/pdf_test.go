package pdf

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/color"
	"image/png"
	"testing"

	"github.com/aitakemyjob/sheetdiver/internal/testutil"
	pdfium "github.com/klippa-app/go-pdfium"
	"github.com/klippa-app/go-pdfium/webassembly"
)

func TestExtractAndRender(t *testing.T) {
	e := NewEngine(DefaultCacheDir())
	defer e.Close()
	data := testutil.Datasheet()
	doc, err := e.Extract(context.Background(), data, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(doc.Pages) != 3 || doc.Meta["Title"] == "" || len(doc.Bookmarks) != 8 {
		t.Fatalf("pages %d, meta %v, bookmarks %d", len(doc.Pages), doc.Meta, len(doc.Bookmarks))
	}
	if b := doc.Bookmarks[1]; b.Page != 0 || !b.HasY || b.Y < 190 || b.Y > 210 {
		t.Errorf("bookmark position %+v", b)
	}
	// Text positions are top-left origin.
	var found bool
	for _, s := range doc.Pages[1].Spans {
		if s.Text == "0.765" {
			found = s.Box.Y0 > 100 && s.Box.Y1 < 120 && s.Box.X0 > 395
		}
	}
	if !found {
		t.Error("0.765 not at its position")
	}
	if doc.Pages[1].Ink == nil || doc.Pages[0].Ink != nil {
		t.Error("ink maps are only made for pages with figures")
	}
	if n, _ := e.PageCount(data); n != 3 {
		t.Errorf("page count %d", n)
	}
	img, err := e.Render(data, 1, &Rect{X0: 90, Y0: 220, X1: 310, Y1: 420}, 72)
	if err != nil {
		t.Fatal(err)
	}
	m, err := png.Decode(bytes.NewReader(img))
	if err != nil || m.Bounds().Dx() != 220 {
		t.Fatalf("render: %v %v", err, m.Bounds())
	}
	if _, err := e.Extract(context.Background(), []byte("not a pdf"), nil); err != ErrNotPDF {
		t.Errorf("err = %v", err)
	}
}

func TestInkTrim(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 100, 100))
	for y := range 100 {
		for x := range 100 {
			img.Set(x, y, color.White)
		}
	}
	for x := 20; x < 40; x++ {
		img.Set(x, 50, color.Black)
	}
	m := newInkMap(img, 1)
	got := m.Trim(Rect{0, 0, 100, 100})
	if got != (Rect{20, 50, 40, 51}) {
		t.Errorf("trim = %+v", got)
	}
	var nilMap *InkMap
	if r := (Rect{1, 2, 3, 4}); nilMap.Trim(r) != r {
		t.Error("nil map keeps the rectangle")
	}
}

func TestRect(t *testing.T) {
	a, b := Rect{0, 0, 10, 10}, Rect{5, 5, 20, 20}
	if !a.Intersects(b) || a.Contains(b) || a.Union(b) != (Rect{0, 0, 20, 20}) || (Rect{}).Union(a) != a {
		t.Error("rect ops")
	}
	if !IsPDF([]byte("junk%PDF-1.4")) || IsPDF([]byte("<html>")) {
		t.Error("IsPDF")
	}
}

func TestPunctuationStaysInItsSpan(t *testing.T) {
	// Comma glyph boxes sit below the baseline; "A3, B3" must stay one
	// span instead of splitting off a "," line.
	data := testutil.PDF("punct", []testutil.PageSpec{{Texts: []testutil.Text{
		{X: 60, Y: 100, Size: 9, S: "A3, B3"},
		{X: 200, Y: 100, Size: 9, S: "VIN = 3.6 V, VOUT = 5 V."},
	}}}, nil)
	e := NewEngine(DefaultCacheDir())
	defer e.Close()
	doc, err := e.Extract(context.Background(), data, nil)
	if err != nil {
		t.Fatal(err)
	}
	var texts []string
	for _, s := range doc.Pages[0].Spans {
		texts = append(texts, s.Text)
		if s.Text == "," || s.Text == "." {
			t.Errorf("punctuation split into its own span: %q", texts)
		}
	}
	if len(texts) != 2 || texts[0] != "A3, B3" || texts[1] != "VIN = 3.6 V, VOUT = 5 V." {
		t.Errorf("spans = %q", texts)
	}
}

func TestConcurrentExtraction(t *testing.T) {
	e := NewEngine(DefaultCacheDir())
	e.Workers = 3
	defer e.Close()
	data := testutil.Datasheet()
	errs := make(chan error, 8)
	for range 8 {
		go func() {
			doc, err := e.Extract(context.Background(), data, nil)
			if err == nil && len(doc.Pages) != 3 {
				err = ErrNotPDF
			}
			if err == nil {
				_, err = e.Render(data, 0, nil, 30)
			}
			errs <- err
		}()
	}
	for range 8 {
		if err := <-errs; err != nil {
			t.Fatal(err)
		}
	}
}

func TestSymbolChar(t *testing.T) {
	for in, want := range map[string]string{"m": "µ", "W": "Ω", "q": "θ", "£": "≤", "5": "5", "±": "±"} {
		if got := symbolChar(in); got != want {
			t.Errorf("symbolChar(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestEngineRetriesWithoutTheCompilationCache(t *testing.T) {
	saved := initPool
	defer func() { initPool = saved }()
	calls := 0
	initPool = func(cfg webassembly.Config) (pdfium.Pool, error) {
		calls++
		if calls == 1 {
			// What Windows reports when another process writes the same
			// cache file.
			return nil, errors.New("could not compile webassembly module: rename …tmp …: Access is denied.")
		}
		return saved(cfg)
	}
	e := NewEngine(t.TempDir())
	defer e.Close()
	if _, err := e.Extract(context.Background(), testutil.Datasheet(), nil); err != nil {
		t.Fatalf("a failing compilation cache must not stop the engine: %v", err)
	}
	if calls != 2 {
		t.Errorf("compile attempts = %d, want 2 (cached, then without the cache)", calls)
	}

	// Without a cache there's nothing to fall back to: the error stands.
	calls = 0
	initPool = func(webassembly.Config) (pdfium.Pool, error) {
		calls++
		return nil, errors.New("broken")
	}
	e2 := NewEngine("")
	if _, err := e2.Extract(context.Background(), testutil.Datasheet(), nil); err == nil || calls != 1 {
		t.Errorf("err = %v after %d attempts", err, calls)
	}
}
