package extract

import (
	"unicode"

	"github.com/aitakemyjob/sheetdiver/internal/pdf"
)

// DetectLanguage names a datasheet's main language from its first pages'
// letters: "zh" (Chinese), "ja" (Japanese: kana), "ko" (Korean: Hangul),
// or "en" (Latin script, which includes the other European languages).
// Han characters make a page Chinese when they're at least a fifth of
// its letters; datasheets mix in part numbers and units, so a majority
// isn't needed.
func DetectLanguage(doc *pdf.Document) string {
	var latin, han, kana, hangul int
	for i := 0; i < len(doc.Pages) && i < 4; i++ {
		for _, s := range doc.Pages[i].Spans {
			for _, r := range s.Text {
				switch {
				case unicode.Is(unicode.Han, r):
					han++
				case unicode.Is(unicode.Hiragana, r) || unicode.Is(unicode.Katakana, r):
					kana++
				case unicode.Is(unicode.Hangul, r):
					hangul++
				case unicode.IsLetter(r) && r < 0x250:
					latin++
				}
			}
		}
	}
	total := latin + han + kana + hangul
	switch {
	case total == 0:
		return ""
	case hangul*5 >= total:
		return "ko"
	case kana*10 >= total && kana+han >= total/5:
		return "ja"
	case han*5 >= total:
		return "zh"
	}
	return "en"
}
