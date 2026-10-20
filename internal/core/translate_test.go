package core

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/aitakemyjob/sheetdiver/internal/library"
	"github.com/aitakemyjob/sheetdiver/internal/pdf"
	"github.com/aitakemyjob/sheetdiver/internal/testutil"
)

func TestTranslateDocUsesSavedTranslations(t *testing.T) {
	s := newService(t)
	ctx := context.Background()
	doc := &pdf.Document{Pages: []pdf.Page{{Spans: []pdf.Span{{Text: "电气特性"}, {Text: " 静态电流 "}, {Text: "VIN"}, {Text: "未翻译"}}}}}
	if s.translateDoc(ctx, doc, "zh") {
		t.Fatal("nothing saved yet, nothing translated")
	}
	if err := s.SaveTranslations(ctx, []library.Translation{{Lang: "zh", Source: "电气特性", Text: "Electrical characteristics"}, {Lang: "zh", Source: "静态电流", Text: "Quiescent current"}}); err != nil {
		t.Fatal(err)
	}
	if s.translateDoc(ctx, doc, "en") {
		t.Error("English documents aren't translated")
	}
	if !s.translateDoc(ctx, doc, "zh") {
		t.Fatal("not translated")
	}
	var got []string
	for _, sp := range doc.Pages[0].Spans {
		got = append(got, sp.Text)
	}
	want := []string{"Electrical characteristics", "Quiescent current", "VIN", "未翻译"}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("spans = %q, want %q", got, want)
			break
		}
	}
	tr, err := s.Translations(ctx, "zh", []string{"静态电流", "未翻译"})
	if err != nil || len(tr) != 1 || tr["静态电流"] != "Quiescent current" {
		t.Errorf("translations = %v %v", tr, err)
	}
}

func TestTranslationModelDownload(t *testing.T) {
	testutil.StubTranslationModel(t)
	s := newService(t)
	if st := s.TranslateStatus(); len(st.Models) != 1 || st.Models[0].Installed {
		t.Fatalf("status = %+v", st)
	}
	if err := s.StartTranslateDownload("xx", "en"); err == nil || !strings.Contains(err.Error(), "no xx→en") {
		t.Errorf("unknown pair: %v", err)
	}
	events, stop := s.Bus.Subscribe()
	defer stop()
	if err := s.StartTranslateDownload("zh", "en"); err != nil {
		t.Fatal(err)
	}
	deadline := time.After(10 * time.Second)
	for {
		select {
		case ev := <-events:
			if ev.Type != EvTranslate {
				continue
			}
			st := ev.Data.(TranslateStatus)
			if st.Progress == nil {
				if st.Error != "" || !st.Models[0].Installed {
					t.Fatalf("finished: %+v", st)
				}
				return
			}
		case <-deadline:
			t.Fatal("no download event")
		}
	}
}

func TestTranslationSourcesOfAnEnglishDatasheet(t *testing.T) {
	s := newService(t)
	ctx := context.Background()
	sheets, _ := s.Lib.Datasheets(ctx)
	src, err := s.TranslationSources(ctx, sheets[0].ID)
	if err != nil || src.Lang != "en" || len(src.Texts) != 0 {
		t.Errorf("sources = %+v %v", src, err)
	}
	if _, err := s.TranslationSources(ctx, 999); err == nil {
		t.Error("unknown datasheet")
	}
}

func TestForeign(t *testing.T) {
	for in, want := range map[string]bool{"静态电流": true, "かな": true, "한국어": true, "VIN 4.2V": false, "µA ±1%": false} {
		if foreign(in) != want {
			t.Errorf("foreign(%q) = %v", in, !want)
		}
	}
}
