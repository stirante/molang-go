package bridge

import (
	"strings"

	"github.com/stirante/molang-go/ast"
	"github.com/stirante/molang-go/token"
)

// The tree has no positions, and an editor needs little else: where each
// name is, whether it is written or read, how many arguments a call was
// given. All of that is visible in the token stream, and reading it from
// there has a second advantage -- the tokens exist when the source does not
// parse, which is most of the time while someone is typing. So names are
// found by scanning tokens, and the tree is consulted only for what needs
// the grammar (the symbol set, see symbols, and the operation checks).

// ref is one namespaced name as written: `q.is_baby`, `v.speed`,
// `math.clamp(...)`. Offsets here are bytes; Analyze converts.
type ref struct {
	ns         ast.Namespace
	written    string // namespace spelling as written: "q", "query", "V"
	name       string // member, segments joined by '.', case as written
	start, end int    // the whole name, namespace included
	nameStart  int    // the member
	nameEnd    int
	write      bool // assigned to, or the variable of a for_each
	call       bool // followed by an argument list
	argc       int  // arguments in that list; -1 when the list is not closed
	callEnd    int  // after the ')' of a closed list
	closeTok   int  // token index of that ')'
	arrow      bool // the right side of `->`
	inQueryArg bool // inside the argument list of a query call
	firstTok   int  // token index of the namespace
	lastTok    int  // token index of the last member segment
}

func (r *ref) lowerName() string { return strings.ToLower(r.name) }

// tokEnd is the byte offset just past t. Token.Text is not always the whole
// token: a string's text is its content without the quotes, and a number's
// omits a trailing `f`.
func tokEnd(src string, t token.Token) int {
	switch t.Kind {
	case token.EOF:
		return t.Pos
	case token.String:
		end := t.Pos + 1 + len(t.Text)
		if end < len(src) && src[end] == '\'' {
			end++
		}
		return end
	case token.Number:
		end := t.Pos + len(t.Text)
		if end < len(src) && (src[end] == 'f' || src[end] == 'F') {
			end++
		}
		return end
	}
	return t.Pos + len(t.Text)
}

// scanRefs finds every namespaced name in toks. The stream always ends with
// an EOF token, which is what lets the look-ahead below index freely.
func scanRefs(src string, toks []token.Token) []ref {
	var refs []ref
	for i := 0; i < len(toks)-2; i++ {
		t := toks[i]
		if t.Kind != token.Ident || toks[i+1].Kind != token.Dot || toks[i+2].Kind != token.Ident {
			continue
		}
		ns, ok := ast.NamespaceAliases[strings.ToLower(t.Text)]
		if !ok {
			continue
		}
		j := i + 2
		name := toks[j].Text
		// Member names may carry further dots, as the parser reads them.
		for j+2 < len(toks) && toks[j+1].Kind == token.Dot && toks[j+2].Kind == token.Ident {
			j += 2
			name += "." + toks[j].Text
		}
		r := ref{
			ns: ns, written: t.Text, name: name,
			start: t.Pos, end: tokEnd(src, toks[j]),
			nameStart: toks[i+2].Pos, nameEnd: tokEnd(src, toks[j]),
			argc: -1, firstTok: i, lastTok: j,
		}
		if i > 0 && toks[i-1].Kind == token.Arrow {
			r.arrow = true
		}
		next := toks[j+1]
		switch {
		case next.Kind == token.LParen && ns != ast.Array:
			r.call = true
			r.argc, r.callEnd, r.closeTok = countArgs(src, toks, j+1)
		case next.Kind == token.Assign:
			r.write = true
		}
		// for_each(<var>, ...) writes its variable on every pass.
		if i >= 2 && toks[i-1].Kind == token.LParen && toks[i-2].Kind == token.ForEach {
			r.write = true
		}
		refs = append(refs, r)
		i = j
	}
	// A query's arguments are read afresh by the game, with the default
	// query set and none of the field's restrictions on which queries may be
	// named, so a name inside one is checked as an entity expression would
	// be, whatever field it is written in.
	for k := range refs {
		c := &refs[k]
		if c.ns != ast.Query || !c.call || c.argc < 0 {
			continue
		}
		for m := k + 1; m < len(refs) && refs[m].firstTok < c.closeTok; m++ {
			refs[m].inQueryArg = true
		}
	}
	return refs
}

// countArgs counts the arguments of the list opening at toks[open], and
// returns the offset just past its ')' and that token's index. A list that
// is never closed, or is closed by the wrong bracket, has no count: -1.
func countArgs(src string, toks []token.Token, open int) (int, int, int) {
	depth, commas, seen := 0, 0, false
	for k := open + 1; k < len(toks); k++ {
		switch toks[k].Kind {
		case token.EOF:
			return -1, 0, 0
		case token.LParen, token.LBracket, token.LBrace:
			depth++
		case token.RParen, token.RBracket, token.RBrace:
			if depth == 0 {
				if toks[k].Kind != token.RParen {
					return -1, 0, 0
				}
				if !seen {
					return 0, tokEnd(src, toks[k]), k
				}
				return commas + 1, tokEnd(src, toks[k]), k
			}
			depth--
		case token.Comma:
			if depth == 0 {
				commas++
			}
		}
		seen = true
	}
	return -1, 0, 0
}

// Semantic token types and modifiers. The names are VS Code's standard ones,
// so a theme colours them without the extension declaring anything custom.
const (
	tokNamespace = "namespace"
	tokFunction  = "function"
	tokVariable  = "variable"
	tokProperty  = "property"
	tokKeyword   = "keyword"
	tokNumber    = "number"
	tokString    = "string"
	tokOperator  = "operator"

	modDefaultLibrary = "defaultLibrary"
	modReadonly       = "readonly"
	modModification   = "modification"
	modDeprecated     = "deprecated"
)

type semTok struct {
	start, end int
	typ        string
	mods       []string
}

// semanticTokens classifies every token worth colouring. Names are coloured
// by what they refer to rather than by their spelling, which is what the
// TextMate grammar cannot do: a query is a function, a context member is
// read-only, an assignment target is marked as modified.
func semanticTokens(src string, toks []token.Token, refs []ref, cat *Catalogue) []semTok {
	byTok := map[int]*ref{}
	for i := range refs {
		for k := refs[i].firstTok; k <= refs[i].lastTok; k++ {
			byTok[k] = &refs[i]
		}
	}
	var out []semTok
	for i, t := range toks {
		if t.Kind == token.EOF {
			break
		}
		st := semTok{start: t.Pos, end: tokEnd(src, t)}
		switch t.Kind {
		case token.Ident:
			r := byTok[i]
			if r == nil {
				continue
			}
			if i == r.firstTok {
				st.typ = tokNamespace
				break
			}
			st.typ, st.mods = memberStyle(r, cat)
		case token.Dot:
			continue
		case token.Number:
			st.typ = tokNumber
		case token.String:
			st.typ = tokString
		case token.Return, token.Loop, token.ForEach, token.Break, token.Continue,
			token.This, token.True, token.False:
			st.typ = tokKeyword
		case token.Illegal, token.LParen, token.RParen, token.LBracket, token.RBracket,
			token.LBrace, token.RBrace, token.Comma, token.Semi:
			continue
		default:
			st.typ = tokOperator
		}
		out = append(out, st)
	}
	return out
}

func memberStyle(r *ref, cat *Catalogue) (string, []string) {
	switch r.ns {
	case ast.Query:
		if f := cat.query(r.name); f != nil && f.Deprecated != nil {
			return tokFunction, []string{modDeprecated}
		}
		return tokFunction, nil
	case ast.Math:
		if strings.EqualFold(r.name, "pi") {
			return tokVariable, []string{modReadonly, modDefaultLibrary}
		}
		return tokFunction, []string{modDefaultLibrary}
	case ast.Context:
		return tokVariable, []string{modReadonly}
	case ast.Geometry, ast.Material, ast.Texture:
		return tokProperty, nil
	}
	if r.write {
		return tokVariable, []string{modModification}
	}
	return tokVariable, nil
}
