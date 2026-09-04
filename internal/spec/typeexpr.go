package spec

import (
	"fmt"
	"strconv"
	"strings"
	"text/scanner"
)

// A variable's or a field's `type:` is a single-line Python type expression in
// Pydantic's own vocabulary, and this file is the grammar for it:
//
//	type  := atom ("|" atom)*          // only "| None" is meaningful
//	atom  := name | name "[" args "]"
//	args  := arg ("," arg)*
//	arg   := type | string
//
// This parser is **structure only**. Every name it reads is a name, so
// `datetime`, `dict` and a shape nobody declared all parse the same way and all
// arrive at internal/ir carrying their column. The type scope, which names are
// legal and what to write instead of each one that is not, has one owner and it
// is internal/ir: that is where a shape name can be checked against `shapes:`
// and where the refusal knows the file and the line to print with the column.
//
// Written by hand over stdlib text/scanner rather than with go/parser, which
// would have given the whole grammar and its positions for free and was refuted
// by measurement. go/parser accepts `str`, `Appointment | None`,
// `list[Appointment]`, `Literal["haircut"]` and even `dict[str, str]`, and
// refuses the one shape this feature exists for:
//
//	list[Literal["create_booking", "modify_booking", "cancel_booking"]]
//	  1:32: expected type, found "modify_booking"
//
// Go allows a multi-index only at the top level of an index expression; nested
// inside one it wants a type, and a second string literal is not a type. So a
// route that cannot parse a list of literals is not a route. Do not retry it.

// TypeExpr is one parsed type expression. Union holds the atoms written with
// "|" between them, in the order they were written, and every expression has at
// least one.
type TypeExpr struct {
	Union []TypeAtom
}

// TypeAtom is one member of a union: a name, a name with brackets, or a quoted
// string, which is what `Literal[...]` takes.
type TypeAtom struct {
	// Name is the identifier. Empty when the atom is a quoted string.
	Name string
	// Entry is the text of a quoted string, unquoted. Quoted says the atom was
	// one, so an empty entry (`Literal[""]`) is still an entry.
	Entry  string
	Quoted bool
	// Args is what was written inside brackets. Bracket says whether brackets
	// were written at all, so `list` and `list[]` reach internal/ir as different
	// things and get different refusals.
	Args    []TypeExpr
	Bracket bool
	// Col is the 1-based column the atom starts at inside the expression, which
	// is what a refusal prints beside the file and the line.
	Col int
}

// TypeError is a refusal inside a type expression. It knows its column and not
// its file or its line, the way PairError knows its line and not its file:
// whoever asked for the parse owns those and joins the three.
type TypeError struct {
	Col int
	Msg string
}

func (e *TypeError) Error() string { return fmt.Sprintf("column %d: %s", e.Col, e.Msg) }

// ParseType reads one type expression. Its refusals are the ones a reader can
// see in the text: a bracket that never closes, a comma where a type belongs, a
// single-quoted entry. Everything about meaning belongs to internal/ir.
func ParseType(expr string) (TypeExpr, error) {
	tokens, err := scanType(expr)
	if err != nil {
		return TypeExpr{}, err
	}
	if len(tokens) == 0 {
		return TypeExpr{}, &TypeError{Col: 1, Msg: "an empty type. Write the type on one line, " +
			`for example "str", "list[Appointment]" or "Literal[\"yes\", \"no\"]"`}
	}
	p := &typeParser{tokens: tokens, end: len(expr) + 1}
	out, err := p.parseType()
	if err != nil {
		return TypeExpr{}, err
	}
	if token, ok := p.peek(); ok {
		return TypeExpr{}, &TypeError{Col: token.col, Msg: fmt.Sprintf(
			"%s after the type has ended. A type is one expression on one line", describe(token))}
	}
	return out, nil
}

// typeToken is one scanned token: an identifier, a quoted string, or one of the
// four punctuation runes the grammar uses.
type typeToken struct {
	kind rune
	text string
	col  int
}

type typeParser struct {
	tokens []typeToken
	pos    int
	// end is the column one past the expression, which is where a refusal points
	// when the text runs out before the grammar does.
	end int
}

func (p *typeParser) peek() (typeToken, bool) {
	if p.pos >= len(p.tokens) {
		return typeToken{}, false
	}
	return p.tokens[p.pos], true
}

func (p *typeParser) next() (typeToken, bool) {
	token, ok := p.peek()
	if ok {
		p.pos++
	}
	return token, ok
}

func (p *typeParser) parseType() (TypeExpr, error) {
	var out TypeExpr
	for {
		atom, err := p.parseAtom()
		if err != nil {
			return TypeExpr{}, err
		}
		out.Union = append(out.Union, atom)
		token, ok := p.peek()
		if !ok || token.kind != '|' {
			return out, nil
		}
		p.next()
	}
}

func (p *typeParser) parseAtom() (TypeAtom, error) {
	token, ok := p.next()
	if !ok {
		return TypeAtom{}, &TypeError{Col: p.end, Msg: "the type ends where a name belongs"}
	}
	switch token.kind {
	case scanner.String:
		entry, err := strconv.Unquote(token.text)
		if err != nil {
			return TypeAtom{}, &TypeError{Col: token.col, Msg: fmt.Sprintf(
				"%s is not a readable entry: %v", token.text, err)}
		}
		return TypeAtom{Entry: entry, Quoted: true, Col: token.col}, nil
	case scanner.Ident:
		atom := TypeAtom{Name: token.text, Col: token.col}
		open, ok := p.peek()
		if !ok || open.kind != '[' {
			return atom, nil
		}
		p.next()
		atom.Bracket = true
		// An empty bracket parses. `list[]` and `Literal[]` are both about
		// meaning rather than shape, so internal/ir refuses each with the
		// sentence that fits it.
		if closing, ok := p.peek(); ok && closing.kind == ']' {
			p.next()
			return atom, nil
		}
		for {
			// The bracket is open, so text running out here is an unclosed
			// bracket rather than a missing name, and the refusal that names the
			// bracket is the one the author can act on.
			if _, ok := p.peek(); !ok {
				return TypeAtom{}, p.unclosed(open)
			}
			arg, err := p.parseType()
			if err != nil {
				return TypeAtom{}, err
			}
			atom.Args = append(atom.Args, arg)
			separator, ok := p.next()
			if !ok {
				return TypeAtom{}, p.unclosed(open)
			}
			switch separator.kind {
			case ']':
				return atom, nil
			case ',':
			default:
				return TypeAtom{}, &TypeError{Col: separator.col, Msg: fmt.Sprintf(
					"%s where %q or %q belongs", describe(separator), ",", "]")}
			}
		}
	default:
		return TypeAtom{}, &TypeError{Col: token.col, Msg: fmt.Sprintf(
			"%s where a name belongs", describe(token))}
	}
}

// unclosed is the refusal for text that runs out with a bracket still open. It
// points at the end and names the bracket's own column, because that is the
// character the author has to go back to.
func (p *typeParser) unclosed(open typeToken) *TypeError {
	return &TypeError{Col: p.end, Msg: fmt.Sprintf(
		"the type ends before %q closes the %q opened at column %d", "]", "[", open.col)}
}

// scanType tokenizes the expression. text/scanner returns exactly the tokens
// this grammar needs and the column of each, which is the whole reason it is
// here rather than a hand-rolled lexer.
func scanType(expr string) ([]typeToken, error) {
	var s scanner.Scanner
	var failure *TypeError
	s.Init(strings.NewReader(expr))
	// No filename: the caller prints the file, and an unset one keeps
	// text/scanner from writing "<input>" into a message a reader sees.
	s.Mode = scanner.ScanIdents | scanner.ScanStrings
	s.Error = func(_ *scanner.Scanner, msg string) {
		if failure == nil {
			failure = &TypeError{Col: s.Column, Msg: msg}
		}
	}
	var out []typeToken
	for token := s.Scan(); token != scanner.EOF; token = s.Scan() {
		if failure != nil {
			return nil, failure
		}
		switch token {
		case scanner.Ident, scanner.String, '[', ']', ',', '|':
			out = append(out, typeToken{kind: token, text: s.TokenText(), col: s.Column})
		case '\'':
			return nil, &TypeError{Col: s.Column, Msg: fmt.Sprintf(
				"a single-quoted entry. Write the entries in double quotes, as %s",
				`Literal["yes", "no"]`)}
		default:
			return nil, &TypeError{Col: s.Column, Msg: fmt.Sprintf(
				"%q is not part of a type expression", string(token))}
		}
	}
	if failure != nil {
		return nil, failure
	}
	return out, nil
}

// describe names a token the way a refusal should read it back.
func describe(token typeToken) string {
	switch token.kind {
	case scanner.Ident:
		return fmt.Sprintf("the name %q", token.text)
	case scanner.String:
		return fmt.Sprintf("the entry %s", token.text)
	default:
		return fmt.Sprintf("%q", token.text)
	}
}

// String writes the expression back the way an author would, which is what a
// refusal naming a type prints and what the resolved schema publishes.
func (t TypeExpr) String() string {
	parts := make([]string, 0, len(t.Union))
	for _, atom := range t.Union {
		parts = append(parts, atom.String())
	}
	return strings.Join(parts, " | ")
}

func (a TypeAtom) String() string {
	if a.Quoted {
		return strconv.Quote(a.Entry)
	}
	if !a.Bracket {
		return a.Name
	}
	args := make([]string, 0, len(a.Args))
	for _, arg := range a.Args {
		args = append(args, arg.String())
	}
	return a.Name + "[" + strings.Join(args, ", ") + "]"
}
