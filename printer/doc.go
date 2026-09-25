package printer

import (
	"strings"
	"unicode/utf8"
)

// A small layout algebra for the multi-line printer, in the style of
// Wadler's "prettier printer": the printer describes WHERE a line may break
// and a renderer decides, per group, whether it has to.
//
// A group is printed flat (every line a space, every softline nothing) when
// what it holds fits in the rest of the line, and broken otherwise. "What it
// holds" is measured only as far as its first forced line end -- a block's
// opening brace -- so `q.a ? {` stays on one line and the block below it
// breaks on its own, instead of the brace forcing every enclosing group
// apart.
type doc interface{}

type (
	docText string
	docLine struct{ soft bool } // flat: " " (or "" when soft); broken: a line end
	docHard struct{}            // always a line end
	docCat  []doc
	docNest struct{ d doc } // one indentation level deeper
	docAt   struct {
		level int
		d     doc
	} // at an absolute indentation level
	docGroup struct{ d doc }
	// docBraces is a brace section: `{`, the body on the lines below
	// indented one level past the line the `{` is on, and `}` back at that
	// line's level. Relative to the line rather than to the expression, so
	// a block reads the same wherever in a statement it opens.
	docBraces struct{ body doc }
	// docTrailing is a comment written after the code on a line. It is
	// never measured: a long remark should not reflow the code before it.
	docTrailing string
)

var (
	line     = docLine{}
	softline = docLine{soft: true}
	hardline = docHard{}
)

func cat(ds ...doc) doc   { return docCat(ds) }
func group(ds ...doc) doc { return docGroup{docCat(ds)} }
func nest(ds ...doc) doc  { return docNest{docCat(ds)} }

// startsWithMinus reports whether d's first character is a `-` (see
// keepsSign).
func startsWithMinus(d doc) bool {
	s, ok := firstText(d)
	return ok && strings.HasPrefix(s, "-")
}

func firstText(d doc) (string, bool) {
	switch x := d.(type) {
	case docText:
		if x != "" {
			return string(x), true
		}
	case docCat:
		for _, c := range x {
			if s, ok := firstText(c); ok {
				return s, true
			}
		}
	case docNest:
		return firstText(x.d)
	case docBraces:
		return "{", true
	case docGroup:
		return firstText(x.d)
	case docLine:
		// A line that prints as a space is not a character anyone parses
		// as a sign, and a group never begins with one.
	}
	return "", false
}

type renderCmd struct {
	indent int
	flat   bool
	d      doc
}

type renderer struct {
	layout  Layout
	out     strings.Builder
	col     int
	pending int // indentation owed before the next text, -1 when none
	line    int // the indentation level of the current line
}

// render lays d out at the layout's width.
func render(d doc, l Layout) string {
	r := &renderer{layout: l, pending: -1}
	stack := []renderCmd{{d: d}}
	for len(stack) > 0 {
		c := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		switch x := c.d.(type) {
		case nil:
		case docText:
			r.text(string(x))
		case docTrailing:
			r.text(" " + string(x))
		case docCat:
			for i := len(x) - 1; i >= 0; i-- {
				stack = append(stack, renderCmd{c.indent, c.flat, x[i]})
			}
		case docNest:
			stack = append(stack, renderCmd{c.indent + 1, c.flat, x.d})
		case docBraces:
			base := r.line
			r.text("{")
			stack = append(stack,
				renderCmd{base, c.flat, docText("}")},
				renderCmd{base, c.flat, hardline},
				renderCmd{base + 1, c.flat, cat(hardline, x.body)})
		case docGroup:
			// Every group is measured, even inside one printed flat: a flat
			// group was only measured to its first forced line end, and
			// what follows that line end has to be judged on its own.
			flat := fits(renderCmd{c.indent, true, x.d}, stack, r.layout.MaxWidth-r.column())
			stack = append(stack, renderCmd{c.indent, flat, x.d})
		case docLine:
			if c.flat {
				if !x.soft {
					r.text(" ")
				}
			} else {
				r.newline(c.indent)
			}
		case docHard:
			r.newline(c.indent)
		}
	}
	return r.out.String()
}

func (r *renderer) column() int {
	if r.pending >= 0 {
		return r.pending * r.layout.IndentWidth
	}
	return r.col
}

func (r *renderer) text(s string) {
	if s == "" {
		return
	}
	if r.pending >= 0 {
		if r.layout.UseTabs {
			r.out.WriteString(strings.Repeat("\t", r.pending))
		} else {
			r.out.WriteString(strings.Repeat(" ", r.pending*r.layout.IndentWidth))
		}
		r.col = r.pending * r.layout.IndentWidth
		r.pending = -1
	}
	r.out.WriteString(s)
	if i := strings.LastIndexByte(s, '\n'); i >= 0 {
		// A string literal or a template can span lines.
		r.col = utf8.RuneCountInString(s[i+1:])
	} else {
		r.col += utf8.RuneCountInString(s)
	}
}

// newline ends the line. The indentation is written with the next text, so
// a blank line has none.
func (r *renderer) newline(indent int) {
	r.out.WriteByte('\n')
	r.col = 0
	r.pending = indent
	r.line = indent
}

// fits reports whether next, printed flat, and whatever follows it up to the
// next line end, fit in width columns.
func fits(next renderCmd, rest []renderCmd, width int) bool {
	stack := []renderCmd{next}
	ri := len(rest)
	for width >= 0 {
		if len(stack) == 0 {
			if ri == 0 {
				return true
			}
			ri--
			stack = append(stack, rest[ri])
			continue
		}
		c := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		switch x := c.d.(type) {
		case docText:
			s := string(x)
			if i := strings.IndexByte(s, '\n'); i >= 0 {
				return width-utf8.RuneCountInString(s[:i]) >= 0
			}
			width -= utf8.RuneCountInString(s)
		case docTrailing:
			return true
		case docBraces:
			// The `{` ends the line.
			return width-1 >= 0
		case docCat:
			for i := len(x) - 1; i >= 0; i-- {
				stack = append(stack, renderCmd{c.indent, c.flat, x[i]})
			}
		case docNest:
			stack = append(stack, renderCmd{c.indent, c.flat, x.d})
		case docGroup:
			stack = append(stack, renderCmd{c.indent, c.flat, x.d})
		case docLine:
			if !c.flat {
				return true
			}
			if !x.soft {
				width--
			}
		case docHard:
			return true
		}
	}
	return false
}
