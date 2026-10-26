package kicad

import (
	"fmt"
	"math"
	"strconv"
	"strings"
)

// KiCad's files are S-expressions: lists that start with a keyword,
// holding bare words, quoted strings, numbers and more lists.

// node is a list: its keyword and its items (atoms or nodes).
type node struct {
	items []any
}

// sym is a bare word (yes, passive, hide), written unquoted.
type sym string

// list starts a list with its keyword and items.
func list(head string, items ...any) *node {
	return &node{items: append([]any{sym(head)}, items...)}
}

// add appends items to a list; nil nodes are skipped, so optional parts
// can be passed inline.
func (n *node) add(items ...any) *node {
	for _, it := range items {
		if c, ok := it.(*node); ok && c == nil {
			continue
		}
		n.items = append(n.items, it)
	}
	return n
}

// num writes a coordinate or size as KiCad does: millimetres to the
// micrometre (a point reached two ways is written alike), without
// trailing zeros.
func num(f float64) string {
	f = math.Round(f*1e4) / 1e4
	if f == 0 {
		return "0"
	}
	return strconv.FormatFloat(f, 'f', -1, 64)
}

// atom writes one item that isn't a list.
func atom(b *strings.Builder, it any) {
	switch v := it.(type) {
	case sym:
		b.WriteString(string(v))
	case string:
		b.WriteString(strconv.Quote(v))
	case float64:
		b.WriteString(num(v))
	case int:
		b.WriteString(strconv.Itoa(v))
	}
}

// write writes a list, a list holding lists over several lines indented
// by depth, one holding only atoms on one line.
func (n *node) write(b *strings.Builder, depth int) {
	b.WriteByte('(')
	nested := false
	for i, it := range n.items {
		if c, ok := it.(*node); ok {
			nested = true
			b.WriteByte('\n')
			b.WriteString(strings.Repeat("  ", depth+1))
			c.write(b, depth+1)
			continue
		}
		if i > 0 {
			b.WriteByte(' ')
		}
		atom(b, it)
	}
	if nested {
		b.WriteByte('\n')
		b.WriteString(strings.Repeat("  ", depth))
	}
	b.WriteByte(')')
}

// String is the list as text.
func (n *node) String() string {
	var b strings.Builder
	n.write(&b, 0)
	b.WriteByte('\n')
	return b.String()
}

// Common pieces of KiCad lists.

// xy is a point.
func xy(x, y float64) *node { return list("xy", x, y) }

// at is a place and rotation.
func at(x, y, angle float64) *node { return list("at", x, y, angle) }

// font is KiCad's default text size.
func font() *node { return list("font", list("size", 1.27, 1.27)) }

// effects is how a text is drawn: hidden, justified, or as is.
func effects(hidden bool, justify ...string) *node {
	e := list("effects", font())
	if len(justify) > 0 {
		j := list("justify")
		for _, s := range justify {
			j.add(sym(s))
		}
		e.add(j)
	}
	if hidden {
		e.add(sym("hide"))
	}
	return e
}

// stroke is a solid line of a width (0: the default).
func stroke(width float64) *node {
	return list("stroke", list("width", width), list("type", sym("default")))
}

// fill is how a shape is filled: none, outline or background.
func fill(kind string) *node { return list("fill", list("type", sym(kind))) }

// yesNo is a flag as KiCad writes it.
func yesNo(b bool) sym {
	if b {
		return "yes"
	}
	return "no"
}

// Reading S-expressions, as KiCad's footprint files are written.

// parse reads one list from text: bare words become syms, quoted
// strings strings, lists nodes.
func parse(text string) (*node, error) {
	p := &parser{text: text}
	p.space()
	if p.pos >= len(p.text) || p.text[p.pos] != '(' {
		return nil, fmt.Errorf("not an S-expression")
	}
	return p.list()
}

// parser is a position in a text being read.
type parser struct {
	text string
	pos  int
}

// space skips white space.
func (p *parser) space() {
	for p.pos < len(p.text) && strings.ContainsRune(" \t\r\n", rune(p.text[p.pos])) {
		p.pos++
	}
}

// list reads a list from its opening parenthesis.
func (p *parser) list() (*node, error) {
	p.pos++ // (
	n := &node{}
	for {
		p.space()
		if p.pos >= len(p.text) {
			return nil, fmt.Errorf("unclosed list")
		}
		switch c := p.text[p.pos]; c {
		case ')':
			p.pos++
			return n, nil
		case '(':
			child, err := p.list()
			if err != nil {
				return nil, err
			}
			n.items = append(n.items, child)
		case '"':
			s, err := p.quoted()
			if err != nil {
				return nil, err
			}
			n.items = append(n.items, s)
		default:
			start := p.pos
			for p.pos < len(p.text) && !strings.ContainsRune(" \t\r\n()", rune(p.text[p.pos])) {
				p.pos++
			}
			n.items = append(n.items, sym(p.text[start:p.pos]))
		}
	}
}

// quoted reads a quoted string, unescaping it.
func (p *parser) quoted() (string, error) {
	start := p.pos
	p.pos++
	for p.pos < len(p.text) {
		switch p.text[p.pos] {
		case '\\':
			p.pos += 2
			continue
		case '"':
			p.pos++
			s, err := strconv.Unquote(p.text[start:p.pos])
			if err != nil {
				// KiCad escapes less than Go: keep the text as written.
				return p.text[start+1 : p.pos-1], nil
			}
			return s, nil
		}
		p.pos++
	}
	return "", fmt.Errorf("unclosed string")
}

// head is a list's keyword.
func (n *node) head() string {
	if len(n.items) == 0 {
		return ""
	}
	s, _ := n.items[0].(sym)
	return string(s)
}

// text is an atom of a list as text, quoted or not; "" when it isn't one.
func (n *node) text(i int) string {
	if i >= len(n.items) {
		return ""
	}
	switch v := n.items[i].(type) {
	case sym:
		return string(v)
	case string:
		return v
	}
	return ""
}

// float is an atom of a list as a number; 0 when it isn't one.
func (n *node) float(i int) float64 {
	f, _ := strconv.ParseFloat(n.text(i), 64)
	return f
}

// child is a list's first list with a keyword, nil when there's none.
func (n *node) child(head string) *node {
	for _, it := range n.items {
		if c, ok := it.(*node); ok && c.head() == head {
			return c
		}
	}
	return nil
}

// children are a list's lists with a keyword.
func (n *node) children(head string) []*node {
	var out []*node
	for _, it := range n.items {
		if c, ok := it.(*node); ok && c.head() == head {
			out = append(out, c)
		}
	}
	return out
}
