package kicad

import (
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
