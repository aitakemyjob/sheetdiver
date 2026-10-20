package core

import (
	"context"
	"os"
	"strings"
	"time"
	"unicode"

	"github.com/aitakemyjob/sheetdiver/internal/extract"
	"github.com/aitakemyjob/sheetdiver/internal/library"
	"github.com/aitakemyjob/sheetdiver/internal/pdf"
	"github.com/aitakemyjob/sheetdiver/internal/translate"
)

// TranslateStatus lists the translation models, and the progress or
// error of the last download.
type TranslateStatus struct {
	Models   []translate.ModelStatus `json:"models"`
	Progress *translate.Progress     `json:"progress,omitempty"`
	Error    string                  `json:"error,omitempty"`
}

// TranslateStatus reports the translation models.
func (s *Service) TranslateStatus() TranslateStatus {
	st := TranslateStatus{Models: s.Translate.Status()}
	s.mu.Lock()
	st.Progress, st.Error = s.translateProgress, s.translateErr
	s.mu.Unlock()
	return st
}

// StartTranslateDownload downloads a translation model in the background;
// progress arrives as translate events. Only the user starts it.
func (s *Service) StartTranslateDownload(from, to string) error {
	if _, ok := translate.Find(from, to); !ok {
		return errUnknownModel(from, to)
	}
	s.mu.Lock()
	s.translateProgress, s.translateErr = &translate.Progress{}, ""
	s.mu.Unlock()
	go func() {
		last := time.Time{}
		err := s.Translate.Download(context.Background(), from, to, func(p translate.Progress) {
			s.mu.Lock()
			s.translateProgress = &p
			s.mu.Unlock()
			if time.Since(last) > 250*time.Millisecond {
				last = time.Now()
				s.emit(EvTranslate, s.TranslateStatus())
			}
		})
		s.mu.Lock()
		s.translateProgress = nil
		if err != nil {
			s.translateErr = err.Error()
		}
		s.mu.Unlock()
		s.emit(EvTranslate, s.TranslateStatus())
	}()
	return nil
}

// errUnknownModel reports a language pair without a model.
func errUnknownModel(from, to string) error {
	return &unknownModel{from, to}
}

// unknownModel is a language pair without a model.
type unknownModel struct{ from, to string }

// Error describes the missing model.
func (e *unknownModel) Error() string { return "no " + e.from + "→" + e.to + " translation model" }

// Translations returns saved translations of texts from a language.
func (s *Service) Translations(ctx context.Context, lang string, texts []string) (map[string]string, error) {
	return s.Lib.Translations(ctx, lang, texts)
}

// SaveTranslations stores translations made by the engine in the UI.
func (s *Service) SaveTranslations(ctx context.Context, ts []library.Translation) error {
	return s.Lib.SaveTranslations(ctx, ts)
}

// Sources is the text of a datasheet to translate before extracting
// from its translation: each distinct line in its own language.
type Sources struct {
	Lang  string   `json:"lang"`
	Texts []string `json:"texts"`
}

// TranslationSources reads a datasheet's PDF and lists the lines to
// translate (those in a language other than English), so the UI can
// translate them on this machine before Reextract with translated set.
func (s *Service) TranslationSources(ctx context.Context, dsID int64) (Sources, error) {
	d, err := s.Lib.Datasheet(ctx, dsID)
	if err != nil {
		return Sources{}, err
	}
	data, err := os.ReadFile(s.Lib.DatasheetPath(d.File))
	if err != nil {
		return Sources{}, err
	}
	doc, err := s.Engine.Extract(ctx, data, nil)
	if err != nil {
		return Sources{}, err
	}
	out := Sources{Lang: extract.DetectLanguage(doc), Texts: []string{}}
	if out.Lang == "en" || out.Lang == "" {
		return out, nil
	}
	seen := map[string]bool{}
	for _, p := range doc.Pages {
		for _, sp := range p.Spans {
			t := strings.TrimSpace(sp.Text)
			if t != "" && !seen[t] && foreign(t) {
				seen[t] = true
				out.Texts = append(out.Texts, t)
			}
		}
	}
	return out, nil
}

// foreign reports whether a text has CJK characters (Han, kana, Hangul).
func foreign(t string) bool {
	for _, r := range t {
		if unicode.In(r, unicode.Han, unicode.Hiragana, unicode.Katakana, unicode.Hangul) {
			return true
		}
	}
	return false
}

// translateDoc replaces the text of a document's lines with their saved
// English translations, so extraction reads English. It reports whether
// any line was translated.
func (s *Service) translateDoc(ctx context.Context, doc *pdf.Document, lang string) bool {
	if lang == "en" || lang == "" {
		return false
	}
	var texts []string
	for _, p := range doc.Pages {
		for _, sp := range p.Spans {
			if t := strings.TrimSpace(sp.Text); t != "" && foreign(t) {
				texts = append(texts, t)
			}
		}
	}
	tr, err := s.Lib.Translations(ctx, lang, texts)
	if err != nil || len(tr) == 0 {
		return false
	}
	for pi := range doc.Pages {
		for si := range doc.Pages[pi].Spans {
			sp := &doc.Pages[pi].Spans[si]
			if en, ok := tr[strings.TrimSpace(sp.Text)]; ok {
				sp.Text = en
			}
		}
	}
	return true
}
