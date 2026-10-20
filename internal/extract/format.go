package extract

import (
	"math"
	"regexp"
	"strings"
	"unicode"
)

// Section text is formatted for reading: wrapped lines joined into
// paragraphs (a blank line between them), larger or bold lines as
// sub-headings ("### TEMP(PIN1): Battery temperature input") and bullet
// points as list items ("• Standby current 40uA"). Agents get the same
// text, which reads as light markdown.

// bulletRe matches a bullet at the start of a line, including the
// Symbol- and Wingdings-font bullets PDFs extract as private-use
// characters.
var bulletRe = regexp.MustCompile(`^\s*(?:[•●○▪■□◆◇►▶·‣⁃\x{f0b7}\x{f06c}\x{f0a7}\x{f0d8}\x{f076}\x{f0fc}\x{f0e0}]\s*|[–—-]\s+)`)

// labelRe matches a line that starts with a short label and a colon
// ("Charge termination: …", "TEMP(PIN1)：…").
var labelRe = regexp.MustCompile(`^[\p{L}\p{N}][\p{L}\p{N} ()/_.,+-]{0,38}[:：]\s*\S`)

// sentenceEnd matches text that ends a sentence.
var sentenceEnd = regexp.MustCompile(`[.。!?！？:：;；]["')）]?$`)

// formatText formats a section's lines. The body size is the size most
// of the section's text is set in (a document's overall body size can
// be its tables'), and never below the document's, body. letters are the
// letters the document uses as bullets (see letterBullets).
func formatText(lines []Line, body float64, letters map[string]bool) string {
	if len(lines) == 0 {
		return ""
	}
	body = math.Max(body, sectionBody(lines))
	// The right edge of the text in each page column, to tell a line that
	// fills its column (the paragraph goes on) from one that ends short.
	right := map[[2]int]float64{}
	colOf := func(l Line) [2]int {
		c := 0
		if l.PageH > 0 && l.Box.X0 > colSplit(lines, l.Page) {
			c = 1
		}
		return [2]int{l.Page, c}
	}
	for _, l := range lines {
		k := colOf(l)
		right[k] = math.Max(right[k], l.Box.X1)
	}
	type block struct {
		kind string // "p", "h" or "li"
		text string
	}
	var blocks []block
	var prev *Line
	pendingBullet := false
	bulletX := math.Inf(1)
	for i := range lines {
		l := lines[i]
		text := strings.TrimSpace(l.Text)
		if text == "" {
			continue
		}
		// A figure or table, already formatted.
		if strings.HasPrefix(l.Text, "\x00") {
			blocks = append(blocks, block{"raw", strings.TrimPrefix(l.Text, "\x00")})
			prev, pendingBullet = nil, false
			continue
		}
		// An indented line under a list item goes on with it.
		if n := len(blocks); n > 0 && blocks[n-1].kind == "li" && prev != nil && !bulletRe.MatchString(text) && !letterBullet(text, letters) &&
			l.Box.X0 > bulletX+6 && l.Page == prev.Page && l.Box.Y0-prev.Box.Y1 < 0.8*body && l.Box.Y0 > prev.Box.Y0 {
			blocks[n-1].text = joinLine(blocks[n-1].text, text)
			prev = &lines[i]
			continue
		}
		bullet := bulletRe.MatchString(text) || pendingBullet || letterBullet(text, letters)
		pendingBullet = false
		if bullet {
			bulletX = l.Box.X0
			if letterBullet(text, letters) {
				text = text[2:]
			}
			text = strings.TrimSpace(bulletRe.ReplaceAllString(text, ""))
			if text == "" {
				// A bullet extracted apart from its text marks the line
				// level with it (already read, or next).
				if n := len(blocks); n > 0 && prev != nil && math.Abs(prev.Box.Y0-l.Box.Y0) < body && blocks[n-1].kind == "p" {
					blocks[n-1].kind = "li"
				} else {
					pendingBullet = true
				}
				continue
			}
		}
		size := textSize(l)
		heading := !bullet && len([]rune(text)) <= 100 && (size >= body+1.5 || (l.Bold && size >= body-0.5 && !sentenceEnd.MatchString(text) && len([]rune(text)) <= 70))
		var kind string
		switch {
		case heading:
			kind = "h"
		case bullet:
			kind = "li"
		}
		newBlock := len(blocks) == 0 || kind != "" || blocks[len(blocks)-1].kind == "h" || blocks[len(blocks)-1].kind == "raw"
		if !newBlock && prev != nil {
			newBlock = paragraphBreak(*prev, l, right[colOf(*prev)], body) || labelRe.MatchString(text)
		}
		if newBlock {
			if kind == "" {
				kind = "p"
			}
			blocks = append(blocks, block{kind, text})
		} else {
			b := &blocks[len(blocks)-1]
			b.text = joinLine(b.text, text)
		}
		prev = &lines[i]
	}
	var out []string
	for i, b := range blocks {
		switch b.kind {
		case "h":
			b.text = "### " + b.text
		case "li":
			b.text = "• " + b.text
		}
		// List items sit on consecutive lines; everything else is
		// separated by a blank line.
		if i > 0 && (b.kind != "li" || blocks[i-1].kind != "li") {
			out = append(out, "")
		}
		out = append(out, b.text)
	}
	return strings.Join(out, "\n")
}

// letterBulletRe matches a line starting with a lone lower-case letter,
// which is how Wingdings and Symbol bullets often extract ("z Low
// quiescent current").
var letterBulletRe = regexp.MustCompile(`^([a-z]) [A-Z0-9(±]`)

// letterBullets finds the letters a document uses as bullets: one that
// starts three or more of its lines.
func letterBullets(lines [][]Line) map[string]bool {
	n := map[string]int{}
	for _, ls := range lines {
		for _, l := range ls {
			if m := letterBulletRe.FindStringSubmatch(strings.TrimSpace(l.Text)); m != nil {
				n[m[1]]++
			}
		}
	}
	out := map[string]bool{}
	for c, k := range n {
		if k >= 3 && c != "a" {
			out[c] = true
		}
	}
	return out
}

// letterBullet reports whether a line starts with a letter bullet.
func letterBullet(text string, letters map[string]bool) bool {
	m := letterBulletRe.FindStringSubmatch(text)
	return m != nil && letters[m[1]]
}

// sectionBody is the font size most of a section's characters are set
// in.
func sectionBody(lines []Line) float64 {
	count := map[float64]int{}
	best, n := 0.0, 0
	for _, l := range lines {
		for _, s := range l.Spans {
			k := math.Round(s.Size*2) / 2
			count[k] += len([]rune(strings.TrimSpace(s.Text)))
			if count[k] > n {
				best, n = k, count[k]
			}
		}
	}
	return best
}

// textSize is the font size most of a line's characters are set in (a
// single larger glyph, such as a dash or symbol, doesn't make a heading).
func textSize(l Line) float64 {
	if len(l.Spans) == 0 {
		return l.Size
	}
	count := map[float64]int{}
	best, n := l.Size, 0
	for _, s := range l.Spans {
		k := math.Round(s.Size*2) / 2
		count[k] += len([]rune(strings.TrimSpace(s.Text)))
		if count[k] > n {
			best, n = s.Size, count[k]
		}
	}
	return best
}

// colSplit is the x position dividing a page's two columns, or +Inf for
// a page whose lines don't form two columns.
func colSplit(lines []Line, page int) float64 {
	var left, rightN int
	var w float64
	for _, l := range lines {
		if l.Page != page {
			continue
		}
		w = math.Max(w, l.Box.X1)
	}
	mid := w / 2
	for _, l := range lines {
		if l.Page != page {
			continue
		}
		switch {
		case l.Box.X1 < mid+10:
			left++
		case l.Box.X0 > mid-10:
			rightN++
		}
	}
	if left > 0 && rightN > 0 {
		return mid - 10
	}
	return math.Inf(1)
}

// paragraphBreak reports whether line b starts a new paragraph after a:
// a gap wider than the line spacing, a move back up the page (a new
// column or page) after a finished sentence, or a short line that ends a
// sentence.
func paragraphBreak(a, b Line, right, body float64) bool {
	size := math.Max(body, 6)
	aText := strings.TrimSpace(a.Text)
	ended := sentenceEnd.MatchString(aText)
	if a.Page != b.Page || b.Box.Y0 < a.Box.Y0-2 {
		return ended
	}
	short := a.Box.X1 < right-math.Max(3*size, 0.12*(right-a.Box.X0))
	// A gap ends a paragraph unless the line before runs on (a figure or
	// table between the halves of a sentence).
	if gap := b.Box.Y0 - a.Box.Y1; gap > 0.9*size {
		// Chinese wraps anywhere, so a short line can stop mid-word.
		ar, br := []rune(aText), []rune(strings.TrimSpace(b.Text))
		midWord := len(ar) > 0 && len(br) > 0 && unicode.Is(unicode.Han, ar[len(ar)-1]) && unicode.Is(unicode.Han, br[0]) && gap < 2*size
		return ended || (short && !midWord)
	}
	return short && ended
}

// joinLine joins a wrapped line to its paragraph: with a space, without
// one between Chinese or Japanese characters, and without one after a
// line-end hyphen.
func joinLine(a, b string) string {
	ar, br := []rune(a), []rune(b)
	if len(ar) == 0 || len(br) == 0 {
		return a + b
	}
	last, first := ar[len(ar)-1], br[0]
	switch {
	case cjk(last) || cjk(first):
		return a + b
	case last == '-' && len(ar) > 1 && unicode.IsLetter(ar[len(ar)-2]) && unicode.IsLower(first):
		return a + b
	}
	return a + " " + b
}

// cjk reports whether r is a Chinese, Japanese or Korean character or
// full-width punctuation.
func cjk(r rune) bool {
	return unicode.In(r, unicode.Han, unicode.Hiragana, unicode.Katakana, unicode.Hangul) || (r >= 0x3000 && r <= 0x303f) || (r >= 0xff00 && r <= 0xffef)
}
