package pdf

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"image/png"
	"testing"

	"github.com/aitakemyjob/sheetdiver/internal/testutil"
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
