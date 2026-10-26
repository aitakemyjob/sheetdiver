package docpdf

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"image/jpeg"
	"strings"
	"testing"

	"github.com/aitakemyjob/sheetdiver/internal/pdf"
)

func TestDocumentReadsBack(t *testing.T) {
	d := New("Layout guide", "SheetDiver")
	d.Heading(1, "Layout guide")
	d.Paragraph("Keep the input capacitor (10 µF) close to VIN and GND: ≤ 2 mm, with 100 Ω (pull-up).")
	for i := 0; i < 80; i++ {
		d.Bullet(strings.Repeat("A long bullet that wraps across the width of the page, ", 2))
	}
	img := image.NewRGBA(image.Rect(0, 0, 40, 20))
	for x := 0; x < 40; x++ {
		img.Set(x, 10, color.RGBA{255, 0, 0, 255})
	}
	var jpg bytes.Buffer
	if err := jpeg.Encode(&jpg, img, nil); err != nil {
		t.Fatal(err)
	}
	d.Image(jpg.Bytes(), 40, 20, "Figure 1. A line")
	data := d.Bytes()

	e := pdf.NewEngine(pdf.DefaultCacheDir())
	defer e.Close()
	doc, err := e.Extract(context.Background(), data, nil)
	if err != nil {
		t.Fatalf("pdfium can't read it: %v", err)
	}
	if len(doc.Pages) < 2 {
		t.Fatalf("pages = %d, want the bullets to flow onto more", len(doc.Pages))
	}
	var text strings.Builder
	for _, p := range doc.Pages {
		for _, s := range p.Spans {
			text.WriteString(s.Text + " ")
		}
	}
	for _, want := range []string{"Layout", "10", "µF", "<=", "Ohm", "page 1 of", "Figure"} {
		if !strings.Contains(text.String(), want) {
			t.Errorf("text lacks %q", want)
		}
	}
	if _, err := e.Render(data, len(doc.Pages)-1, nil, 50); err != nil {
		t.Errorf("render the page with the image: %v", err)
	}
	// Lines stay inside the margins.
	for _, p := range doc.Pages {
		for _, s := range p.Spans {
			if s.Box.X1 > pageW-margin+2 {
				t.Errorf("%q runs to %.0f", s.Text, s.Box.X1)
			}
		}
	}
}
