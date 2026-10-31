// Package kicad writes a CAD library as KiCad reads it: one symbol
// library (.kicad_sym), a footprint library folder (.pretty) of
// .kicad_mod files, and STEP models beside them. It also reads them back,
// so a symbol or footprint the user changed in KiCad is noticed and kept.
package kicad

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
)

// Node is an s-expression list: (name args…). An argument is an atom
// (Atom), a quoted string (Str) or a list (*Node).
type Node struct {
	Name string
	Args []any
}

// Atom is a bare word or number.
type Atom string

// Str is a quoted string.
type Str string

// n makes a node.
func n(name string, args ...any) *Node { return &Node{Name: name, Args: args} }

// num writes a length as KiCad does: up to four decimals, no trailing
// zeros.
func num(v float64) Atom {
	s := strconv.FormatFloat(v, 'f', 4, 64)
	s = strings.TrimRight(strings.TrimRight(s, "0"), ".")
	if s == "-0" {
		s = "0"
	}
	return Atom(s)
}

// xy is an (at x y) style pair under a name.
func xy(name string, x, y float64) *Node { return n(name, num(x), num(y)) }

// Child is the first child list with the name (nil if there is none).
func (nd *Node) Child(name string) *Node {
	if nd == nil {
		return nil
	}
	for _, a := range nd.Args {
		if c, ok := a.(*Node); ok && c.Name == name {
			return c
		}
	}
	return nil
}

// Children are the child lists with the name.
func (nd *Node) Children(name string) []*Node {
	var out []*Node
	if nd == nil {
		return nil
	}
	for _, a := range nd.Args {
		if c, ok := a.(*Node); ok && c.Name == name {
			out = append(out, c)
		}
	}
	return out
}

// Text is the i'th argument as text, whether atom or string ("" if
// there is none or it is a list).
func (nd *Node) Text(i int) string {
	if nd == nil || i >= len(nd.Args) {
		return ""
	}
	switch v := nd.Args[i].(type) {
	case Atom:
		return string(v)
	case Str:
		return string(v)
	}
	return ""
}

// Float is the i'th argument as a number (0 if it isn't one).
func (nd *Node) Float(i int) float64 {
	f, _ := strconv.ParseFloat(nd.Text(i), 64)
	return f
}

// Has reports whether an atom argument is present (hide, locked).
func (nd *Node) Has(atom string) bool {
	if nd == nil {
		return false
	}
	for _, a := range nd.Args {
		if v, ok := a.(Atom); ok && string(v) == atom {
			return true
		}
	}
	return false
}

// String writes the node as KiCad formats its files: a list whose
// arguments are all atoms or strings on one line, others with each child
// list on its own line, indented.
func (nd *Node) String() string {
	var b strings.Builder
	nd.write(&b, 0)
	b.WriteByte('\n')
	return b.String()
}

// flat reports whether a node goes on one line: none of its child lists
// has lists of its own.
func (nd *Node) flat() bool {
	for _, a := range nd.Args {
		if c, ok := a.(*Node); ok {
			for _, ca := range c.Args {
				if _, ok := ca.(*Node); ok {
					return false
				}
			}
		}
	}
	return true
}

// write writes the node at an indent.
func (nd *Node) write(b *strings.Builder, indent int) {
	b.WriteByte('(')
	b.WriteString(nd.Name)
	if nd.flat() {
		for _, a := range nd.Args {
			b.WriteByte(' ')
			writeArg(b, a, indent)
		}
		b.WriteByte(')')
		return
	}
	for _, a := range nd.Args {
		if c, ok := a.(*Node); ok {
			b.WriteByte('\n')
			b.WriteString(strings.Repeat("  ", indent+1))
			c.write(b, indent+1)
			continue
		}
		b.WriteByte(' ')
		writeArg(b, a, indent)
	}
	b.WriteByte('\n')
	b.WriteString(strings.Repeat("  ", indent))
	b.WriteByte(')')
}

// writeArg writes one argument.
func writeArg(b *strings.Builder, a any, indent int) {
	switch v := a.(type) {
	case Atom:
		b.WriteString(string(v))
	case Str:
		b.WriteString(quote(string(v)))
	case *Node:
		v.write(b, indent)
	default:
		b.WriteString(quote(fmt.Sprint(v)))
	}
}

// quote quotes a string as KiCad does.
func quote(s string) string {
	r := strings.NewReplacer(`\`, `\\`, `"`, `\"`, "\n", `\n`)
	return `"` + r.Replace(s) + `"`
}

// Parse reads one s-expression.
func Parse(src string) (*Node, error) {
	p := &parser{s: src}
	p.space()
	nd, err := p.list()
	if err != nil {
		return nil, err
	}
	return nd, nil
}

// parser reads s-expressions.
type parser struct {
	s string
	i int
}

// space skips white space.
func (p *parser) space() {
	for p.i < len(p.s) && strings.ContainsRune(" \t\r\n", rune(p.s[p.i])) {
		p.i++
	}
}

// list reads a list.
func (p *parser) list() (*Node, error) {
	if p.i >= len(p.s) || p.s[p.i] != '(' {
		return nil, fmt.Errorf("expected ( at %d", p.i)
	}
	p.i++
	p.space()
	name := p.word()
	nd := &Node{Name: name}
	for {
		p.space()
		if p.i >= len(p.s) {
			return nil, errors.New("unexpected end: a list isn't closed")
		}
		switch p.s[p.i] {
		case ')':
			p.i++
			return nd, nil
		case '(':
			c, err := p.list()
			if err != nil {
				return nil, err
			}
			nd.Args = append(nd.Args, c)
		case '"':
			s, err := p.str()
			if err != nil {
				return nil, err
			}
			nd.Args = append(nd.Args, Str(s))
		default:
			nd.Args = append(nd.Args, Atom(p.word()))
		}
	}
}

// word reads an atom.
func (p *parser) word() string {
	start := p.i
	for p.i < len(p.s) && !strings.ContainsRune(" \t\r\n()\"", rune(p.s[p.i])) {
		p.i++
	}
	return p.s[start:p.i]
}

// str reads a quoted string.
func (p *parser) str() (string, error) {
	p.i++
	var b strings.Builder
	for p.i < len(p.s) {
		c := p.s[p.i]
		switch c {
		case '\\':
			if p.i+1 < len(p.s) {
				p.i++
				switch p.s[p.i] {
				case 'n':
					b.WriteByte('\n')
				default:
					b.WriteByte(p.s[p.i])
				}
			}
		case '"':
			p.i++
			return b.String(), nil
		default:
			b.WriteByte(c)
		}
		p.i++
	}
	return "", errors.New("unexpected end: a string isn't closed")
}
