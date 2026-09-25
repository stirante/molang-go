package printer

import (
	"errors"
	"sort"
	"strconv"
	"strings"

	"github.com/stirante/molang-go/ast"
	"github.com/stirante/molang-go/lexer"
	"github.com/stirante/molang-go/parser"
	"github.com/stirante/molang-go/token"
)

// ---------------------------------------------------------------------
// FormatSource -- formatting a file, with what the tree does not keep.
// ---------------------------------------------------------------------

// Style is the shape of FormatSource's output.
type Style uint8

const (
	// StyleLayout is FormatLayout's: over several lines, comments and blank
	// lines kept.
	StyleLayout Style = iota
	// StyleOneLine is Format's: one line, as Molang in a JSON string is
	// written.
	StyleOneLine
	// StyleMinified is Minify's.
	StyleMinified
)

// SourceOptions says how FormatSource reads and writes a source.
type SourceOptions struct {
	Layout
	Style Style
	// Comments reads `#` to the end of the line as a comment, the
	// convention of .molang files (see lexer.Extensions.Comments). A
	// comment is kept only by StyleLayout; the one-line styles refuse a
	// source that has one rather than drop it.
	Comments bool
	// Templates reads jsonte's `#{ ... }` as a template: text a pack's build
	// replaces before the game sees it, kept exactly as written. One stands
	// where a value can (`v.x = #{value};`) or as part of a name
	// (`v.#{name}`); anywhere else the source does not format.
	Templates bool
	// OptionalSemicolons: see lexer.Extensions.
	OptionalSemicolons bool
}

// FormatSource formats Molang source text rather than a tree, keeping what a
// tree has no place for: the comments and blank lines of a .molang file and
// jsonte templates.
//
// Each comment stays with the statement it was written against. One on its
// own line or lines is written before the statement that follows it, one at
// the end of a statement's line after that statement, and one after a
// block's or the file's last statement before the block's end. A comment
// inside a statement -- between the arms of a conditional, say -- is moved
// to before the statement: the printer chooses a statement's line breaks,
// and a comment cannot stay at a break that is no longer there. A run of
// blank lines between statements or comments becomes one; blank lines
// anywhere else are dropped.
//
// Formatting its output again changes nothing, and it parses to the same
// tree as the source.
func FormatSource(src string, o SourceOptions) (string, error) {
	f, err := readSource(src, o)
	if err != nil {
		return "", err
	}
	return f.format()
}

// FormatSourceRange formats the statements of src that overlap the byte
// range [start, end), with their comments, and reports the byte range of src
// they replace. Everything outside that range is left as written. With
// nothing to format in the range, text is empty and replStart == replEnd.
//
// Only StyleLayout formats a range. A statement is always formatted whole:
// top-level statements are the smallest units the layout of one does not
// depend on another's.
func FormatSourceRange(src string, start, end int, o SourceOptions) (text string, replStart, replEnd int, err error) {
	if o.Style != StyleLayout {
		return "", 0, 0, errors.New("only the multi-line layout formats a range")
	}
	f, err := readSource(src, o)
	if err != nil {
		return "", 0, 0, err
	}
	docs, items, err := f.topLevel()
	if err != nil {
		return "", 0, 0, err
	}
	first, last := -1, -1
	cs, ce := f.toCode(start), f.toCode(end)
	for k, it := range items {
		if it.start < ce && it.end > cs || (cs == ce && cs >= it.start && cs <= it.end) {
			if first < 0 {
				first = k
			}
			last = k
		}
	}
	if first < 0 {
		return "", start, start, nil
	}
	var out docCat
	for k := first; k <= last; k++ {
		if k > first {
			out = append(out, hardline)
			if items[k].blank {
				out = append(out, hardline)
			}
		}
		out = append(out, docs[k])
	}
	return render(out, f.layout), f.toSource(items[first].start), f.toSource(items[last].end), nil
}

// source is a source read for formatting: templates replaced by stand-ins
// the parser reads, and where its statements and comments are.
type source struct {
	o      SourceOptions
	layout Layout
	// code is the source with each template replaced by its stand-in;
	// every offset below is into it. Comments are kept, and the parser
	// skips them.
	code     string
	comments []comment
	// shifts maps offsets in code back to the source, at each template.
	shifts []shift
	names  map[string]string // name stand-in -> template
	values map[string]string // value stand-in member -> template
	marker string
	prog   *ast.Program
	lists  []*srcList
	empty  bool
}

type comment struct {
	start, end int
	text       string
}

// shift is where a template stood: [codeStart, codeEnd) of code holds the
// stand-in for [srcStart, srcEnd) of the source.
type shift struct{ codeStart, codeEnd, srcStart, srcEnd int }

// srcList is a statement list in the source: the program, or a block.
type srcList struct {
	open, close int // offsets of the braces; -1 and len(code) for the program
	stmts       []span
}

type span struct{ start, end int } // end is past the ';' when there is one

func readSource(src string, o SourceOptions) (*source, error) {
	f := &source{o: o, layout: o.Layout.withDefaults()}
	f.scan(src)
	if len(f.comments) > 0 && o.Style != StyleLayout {
		return nil, errors.New("comments need a line of their own; only the multi-line layout keeps them")
	}
	ext := lexer.Extensions{Comments: o.Comments, OptionalSemicolons: o.OptionalSemicolons}
	toks, err := lexer.TokenizeWith(f.code, ext)
	if err == nil && len(toks) == 1 {
		// Nothing but comments, or nothing at all.
		f.empty = true
		f.lists = []*srcList{{open: -1, close: len(f.code)}}
		return f, nil
	}
	// The parser reports a character it cannot read as well as the lexer
	// does, and in the words every other caller sees.
	prog, err := parser.ParseWith(f.code, ext)
	if err != nil {
		return nil, err
	}
	f.prog = prog
	f.structure(toks)
	return f, nil
}

// scan copies src to code, replacing templates and recording comments.
func (f *source) scan(src string) {
	// A stand-in must not be text the source already has.
	f.marker = "__jsonte"
	for strings.Contains(src, f.marker) {
		f.marker += "x"
	}
	f.names = map[string]string{}
	f.values = map[string]string{}
	var b strings.Builder
	n := 0
	for i := 0; i < len(src); {
		c := src[i]
		switch {
		case c == '\'':
			j := strings.IndexByte(src[i+1:], '\'')
			end := len(src)
			if j >= 0 {
				end = i + 1 + j + 1
			}
			b.WriteString(src[i:end])
			i = end
		case c == '#' && f.o.Templates && i+1 < len(src) && src[i+1] == '{':
			end := templateEnd(src, i+2)
			text := src[i:end]
			code := b.String()
			var prev, next byte
			if len(code) > 0 {
				prev = code[len(code)-1]
			}
			if end < len(src) {
				next = src[end]
			}
			var stand string
			if prev == '.' || isNameByte(prev) || isNameByte(next) || next == '.' {
				stand = f.marker + "n" + strconv.Itoa(n) + "__"
				f.names[stand] = text
			} else {
				// A variable reads wherever a value can stand, and can be
				// assigned to as well.
				member := f.marker + "v" + strconv.Itoa(n) + "__"
				stand = "v." + member
				f.values[member] = text
			}
			n++
			f.shifts = append(f.shifts, shift{b.Len(), b.Len() + len(stand), i, end})
			b.WriteString(stand)
			i = end
		case c == '#' && f.o.Comments:
			end := i
			for end < len(src) && src[end] != '\n' && src[end] != '\r' {
				end++
			}
			start := b.Len()
			b.WriteString(src[i:end])
			f.comments = append(f.comments, comment{start, b.Len(), strings.TrimRight(src[i:end], " \t")})
			i = end
		default:
			b.WriteByte(c)
			i++
		}
	}
	f.code = b.String()
}

func isNameByte(c byte) bool {
	return c == '_' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9'
}

// templateEnd is the offset just past the `}` closing a template whose body
// starts at from: braces nest, and neither quote's contents count.
func templateEnd(src string, from int) int {
	depth := 1
	var quote byte
	for i := from; i < len(src); i++ {
		c := src[i]
		if quote != 0 {
			if c == quote {
				quote = 0
			}
			continue
		}
		switch c {
		case '\'', '"':
			quote = c
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return i + 1
			}
		}
	}
	return len(src)
}

// toSource maps an offset in code to the source. An offset inside a
// stand-in maps to the end of its template.
func (f *source) toSource(c int) int {
	delta := 0
	for _, s := range f.shifts {
		if c <= s.codeStart {
			break
		}
		if c < s.codeEnd {
			return s.srcEnd
		}
		delta = s.srcEnd - s.codeEnd
	}
	return c + delta
}

// toCode maps an offset in the source to code. An offset inside a template
// maps to the end of its stand-in.
func (f *source) toCode(o int) int {
	delta := 0
	for _, s := range f.shifts {
		if o <= s.srcStart {
			break
		}
		if o < s.srcEnd {
			return s.codeEnd
		}
		delta = s.codeEnd - s.srcEnd
	}
	return o + delta
}

// tokenEnd is the offset just past t in code.
func (f *source) tokenEnd(t token.Token) int {
	switch t.Kind {
	case token.String:
		return t.Pos + len(t.Text) + 2
	case token.Number:
		end := t.Pos + len(t.Text)
		for end < len(f.code) && (f.code[end] == 'f' || f.code[end] == 'F') {
			end++
		}
		return end
	}
	return t.Pos + len(t.Text)
}

// structure finds the statement lists of the source from its tokens: the
// statements of a list are what lies between the `;` at its own level.
func (f *source) structure(toks []token.Token) {
	type frame struct {
		list       *srcList
		start, end int
	}
	top := &srcList{open: -1, close: len(f.code)}
	f.lists = []*srcList{top}
	stack := []*frame{{list: top, start: -1}}
	for k, t := range toks {
		fr := stack[len(stack)-1]
		if t.Kind == token.Question && (toks[k+1].Kind == token.Break || toks[k+1].Kind == token.Continue) {
			// `cond ? break` is read as `cond ? { break; }`: a block the
			// source has no braces for. It gets a list of its own, in its
			// place in the order, that no comment falls inside.
			jump := toks[k+1]
			end := f.tokenEnd(jump)
			f.lists = append(f.lists, &srcList{open: jump.Pos, close: jump.Pos, stmts: []span{{jump.Pos, end}}})
		}
		switch t.Kind {
		case token.EOF:
			if fr.start >= 0 {
				fr.list.stmts = append(fr.list.stmts, span{fr.start, fr.end})
			}
		case token.Semi:
			if fr.start >= 0 {
				fr.list.stmts = append(fr.list.stmts, span{fr.start, t.Pos + 1})
				fr.start = -1
			}
		case token.LBrace:
			if fr.start < 0 {
				fr.start = t.Pos
			}
			l := &srcList{open: t.Pos}
			f.lists = append(f.lists, l)
			stack = append(stack, &frame{list: l, start: -1})
		case token.RBrace:
			if len(stack) == 1 {
				continue // the parser has refused this already
			}
			if fr.start >= 0 {
				fr.list.stmts = append(fr.list.stmts, span{fr.start, fr.end})
			}
			fr.list.close = t.Pos
			stack = stack[:len(stack)-1]
			stack[len(stack)-1].end = t.Pos + 1
		default:
			if fr.start < 0 {
				fr.start = t.Pos
			}
			fr.end = f.tokenEnd(t)
		}
	}
}

// listTrivia is a statement list's statements and comments in the order
// they are written.
type listTrivia struct{ items []listItem }

type listItem struct {
	stmt    int    // the statement's index, or -1 for a comment line
	comment string // the comment line, or the statement's trailing comment
	blank   bool   // a blank line before it
	// start, end: where it was in code, the statement's trailing comment
	// included.
	start, end int
}

// trivia places the comments of list l among its statements.
func (f *source) trivia(l *srcList) *listTrivia {
	var mine []comment
	for _, c := range f.comments {
		if f.innermost(c.start) == l {
			mine = append(mine, c)
		}
	}
	n := len(l.stmts)
	leading := make([][]listItem, n+1) // [n]: after the last statement
	trailing := make([]*comment, n)
	var hoisted = make([][]listItem, n)
	for _, c := range mine {
		placed := false
		for i, s := range l.stmts {
			if c.start >= s.start && c.start < s.end {
				hoisted[i] = append(hoisted[i], listItem{stmt: -1, comment: c.text, start: s.start, end: s.start})
				placed = true
				break
			}
		}
		if placed {
			continue
		}
		prev := -1
		for i, s := range l.stmts {
			if s.end <= c.start {
				prev = i
			}
		}
		if prev >= 0 && trailing[prev] == nil && !strings.ContainsAny(f.code[l.stmts[prev].end:c.start], "\r\n") {
			c := c
			trailing[prev] = &c
			continue
		}
		next := n
		for i, s := range l.stmts {
			if s.start > c.start {
				next = i
				break
			}
		}
		leading[next] = append(leading[next], listItem{stmt: -1, comment: c.text, start: c.start, end: c.end})
	}
	var items []listItem
	for i := 0; i <= n; i++ {
		items = append(items, leading[i]...)
		if i == n {
			break
		}
		items = append(items, hoisted[i]...)
		it := listItem{stmt: i, start: l.stmts[i].start, end: l.stmts[i].end}
		if trailing[i] != nil {
			it.comment = trailing[i].text
			it.end = trailing[i].end
		}
		items = append(items, it)
	}
	for k := 1; k < len(items); k++ {
		if gap := items[k-1].end; gap <= items[k].start {
			items[k].blank = hasBlankLine(f.code[gap:items[k].start])
		}
	}
	return &listTrivia{items: items}
}

// innermost is the statement list whose braces most closely enclose offset.
func (f *source) innermost(offset int) *srcList {
	best := f.lists[0]
	for _, l := range f.lists[1:] {
		if l.open < offset && offset < l.close && l.open > best.open {
			best = l
		}
	}
	return best
}

func hasBlankLine(s string) bool {
	seen := false
	for i := 0; i < len(s); i++ {
		switch s[i] {
		case '\n':
			if seen {
				return true
			}
			seen = true
		case ' ', '\t', '\r':
		default:
			seen = false
		}
	}
	return false
}

// printer builds the layout, with the source's comments and templates.
func (f *source) printer() *pretty {
	p := &pretty{}
	p.lists = func(i, stmts int) (*listTrivia, error) {
		if i >= len(f.lists) {
			return nil, &errUnplaced{"the tree has more blocks than the source"}
		}
		if len(f.lists[i].stmts) != stmts {
			return nil, &errUnplaced{"a block's statements do not match the source's"}
		}
		return f.trivia(f.lists[i]), nil
	}
	if len(f.names) > 0 {
		p.name = func(m string) string {
			if !strings.Contains(m, f.marker) {
				return m
			}
			for stand, text := range f.names {
				m = strings.ReplaceAll(m, stand, text)
			}
			return m
		}
	}
	if len(f.values) > 0 {
		p.value = func(ns ast.Namespace, member string) (string, bool) {
			if ns != ast.Variable {
				return "", false
			}
			s, ok := f.values[member]
			return s, ok
		}
	}
	return p
}

func (f *source) format() (string, error) {
	switch f.o.Style {
	case StyleOneLine, StyleMinified:
		if f.empty {
			return "", nil
		}
		var out string
		if f.o.Style == StyleOneLine {
			out = Format(f.prog)
		} else {
			out = Minify(f.prog)
		}
		return f.restore(out), nil
	}
	docs, items, err := f.topLevel()
	if err != nil {
		return "", err
	}
	var out docCat
	for k := range items {
		if k > 0 {
			out = append(out, hardline)
			if items[k].blank {
				out = append(out, hardline)
			}
		}
		out = append(out, docs[k])
	}
	return render(out, f.layout), nil
}

// topLevel lays out each item of the program -- a statement with its
// trailing comment, or a comment line -- on its own. Each starts a line at
// the left margin and ends one, so none's layout depends on another's, and
// a range of them can be formatted alone.
func (f *source) topLevel() ([]doc, []listItem, error) {
	if f.empty {
		t := f.trivia(f.lists[0])
		docs := make([]doc, len(t.items))
		for k, it := range t.items {
			docs[k] = docText(it.comment)
		}
		return docs, t.items, nil
	}
	p := f.printer()
	shape := topLevel(f.prog)
	t, err := p.lists(0, len(f.prog.Stmts))
	if err != nil {
		return nil, nil, err
	}
	p.next = 1
	docs := make([]doc, len(t.items))
	for k, it := range t.items {
		if it.stmt < 0 {
			docs[k] = docText(it.comment)
			continue
		}
		s := f.prog.Stmts[it.stmt]
		var d doc
		switch shape {
		case shapeBare:
			if es, ok := s.(*ast.ExprStmt); ok {
				d = p.expr(es.X, precNone)
			} else {
				d = p.stmt(s)
			}
		case shapeReturn:
			d = cat(docText("return "), p.expr(s.(*ast.ExprStmt).X, precNone), docText(";"))
		default:
			d = cat(p.stmt(s), docText(";"))
		}
		if it.comment != "" {
			d = cat(d, docTrailing(it.comment))
		}
		docs[k] = d
	}
	if p.err != nil {
		return nil, nil, p.err
	}
	if p.next != len(f.lists) {
		return nil, nil, &errUnplaced{"the source has blocks the tree does not"}
	}
	return docs, t.items, nil
}

// restore puts the templates back into one-line output. Format writes a
// value stand-in as `variable.<member>` and Minify as `v.<member>`; a name
// stand-in is written as it was read.
func (f *source) restore(out string) string {
	if !strings.Contains(out, f.marker) {
		return out
	}
	keys := make([]string, 0, len(f.values)+len(f.names))
	for m := range f.values {
		keys = append(keys, m)
	}
	for m := range f.names {
		keys = append(keys, m)
	}
	// Longest first, so no stand-in is replaced inside a longer one.
	sort.Slice(keys, func(i, j int) bool { return len(keys[i]) > len(keys[j]) })
	for _, k := range keys {
		if text, ok := f.values[k]; ok {
			out = strings.ReplaceAll(out, "variable."+k, text)
			out = strings.ReplaceAll(out, "v."+k, text)
			continue
		}
		out = strings.ReplaceAll(out, k, f.names[k])
	}
	return out
}
