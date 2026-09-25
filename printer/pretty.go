package printer

import (
	"fmt"

	"github.com/stirante/molang-go/ast"
)

// ---------------------------------------------------------------------
// FormatLayout -- readable output over several lines.
// ---------------------------------------------------------------------

// Layout is how FormatLayout lays a program out.
type Layout struct {
	// IndentWidth is the width of one indentation level; 0 means 4. With
	// UseTabs it is the width a tab is counted as.
	IndentWidth int
	// UseTabs indents with tabs rather than spaces.
	UseTabs bool
	// MaxWidth is the line width the printer keeps to where it can; 0 means
	// 100. A long name, string or comment can still run past it.
	MaxWidth int
}

func (l Layout) withDefaults() Layout {
	if l.IndentWidth <= 0 {
		l.IndentWidth = 4
	}
	if l.MaxWidth <= 0 {
		l.MaxWidth = 100
	}
	return l
}

// FormatLayout renders prog as readable Molang over as many lines as it
// takes: one statement per line, the statements of a block indented on the
// lines between its braces, and a conditional, a `??` chain or any other
// chain of operators broken before its operators when it does not fit in
// l.MaxWidth.
//
// It writes the same program Format does -- the same spellings, the same
// parentheses, the same semicolons -- and differs only in whitespace, which
// Molang does not read. Format stays the one-line form, for Molang that
// lives inside a JSON string.
func FormatLayout(prog *ast.Program, l Layout) string {
	p := &pretty{}
	return render(p.program(prog), l.withDefaults())
}

// pretty builds the layout of a program. Its hooks let FormatSource put the
// comments and templates of the source back where they were.
type pretty struct {
	// lists, when set, gives the source's comments and blank lines for a
	// statement list: the program's first, then each block's in the order
	// the blocks are printed, which is the order their braces appear in.
	lists func(i, stmts int) (*listTrivia, error)
	next  int
	err   error
	// name and value put templates back (see FormatSource).
	name  func(string) string
	value func(ns ast.Namespace, member string) (string, bool)
}

func (p *pretty) trivia(stmts int) *listTrivia {
	if p.lists == nil {
		return nil
	}
	i := p.next
	p.next++
	t, err := p.lists(i, stmts)
	if err != nil && p.err == nil {
		p.err = err
	}
	return t
}

func (p *pretty) member(m string) string {
	if p.name != nil {
		return p.name(m)
	}
	return m
}

func (p *pretty) ident(ns ast.Namespace, member string) doc {
	if p.value != nil {
		if s, ok := p.value(ns, member); ok {
			return docText(s)
		}
	}
	return docText(namespaceName(ns, false) + "." + p.member(member))
}

func (p *pretty) program(prog *ast.Program) doc {
	t := p.trivia(len(prog.Stmts))
	shape := topLevel(prog)
	stmt := func(i int) doc {
		s := prog.Stmts[i]
		switch shape {
		case shapeBare:
			if es, ok := s.(*ast.ExprStmt); ok {
				return p.expr(es.X, precNone)
			}
			return p.stmt(s)
		case shapeReturn:
			return cat(docText("return "), p.expr(s.(*ast.ExprStmt).X, precNone), docText(";"))
		}
		return cat(p.stmt(s), docText(";"))
	}
	return p.list(len(prog.Stmts), t, stmt)
}

// list lays out n statements and the comments among them, one per line.
func (p *pretty) list(n int, t *listTrivia, stmt func(int) doc) doc {
	items := defaultItems(n)
	if t != nil {
		items = t.items
	}
	var out docCat
	for k, it := range items {
		if k > 0 {
			out = append(out, hardline)
			if it.blank {
				out = append(out, hardline)
			}
		}
		if it.stmt < 0 {
			out = append(out, docText(it.comment))
			continue
		}
		out = append(out, stmt(it.stmt))
		if it.comment != "" {
			out = append(out, docTrailing(it.comment))
		}
	}
	return out
}

func defaultItems(n int) []listItem {
	items := make([]listItem, n)
	for i := range items {
		items[i] = listItem{stmt: i}
	}
	return items
}

// block lays a brace section out over its own lines (see docBraces).
func (p *pretty) block(b *ast.Block) doc {
	t := p.trivia(len(b.Stmts))
	if len(b.Stmts) == 0 && (t == nil || len(t.items) == 0) {
		return docText("{}")
	}
	return docBraces{p.list(len(b.Stmts), t, func(i int) doc { return cat(p.stmt(b.Stmts[i]), docText(";")) })}
}

func (p *pretty) stmt(s ast.Stmt) doc {
	switch s := s.(type) {
	case *ast.ExprStmt:
		return p.expr(s.X, precNone)
	case *ast.ReturnStmt:
		if s.Value == nil {
			return docText("return")
		}
		return cat(docText("return "), p.expr(s.Value, precNone))
	case *ast.BreakStmt:
		return docText("break")
	case *ast.ContinueStmt:
		return docText("continue")
	case *ast.LoopStmt:
		count := p.expr(s.Count, precNone)
		return cat(docText("loop("), count, docText(", "), p.block(s.Body), docText(")"))
	case *ast.ForEachStmt:
		v := p.ident(s.Var.Namespace, s.Var.Member)
		src := p.expr(s.Source, precNone)
		return cat(docText("for_each("), v, docText(", "), src, docText(", "), p.block(s.Body), docText(")"))
	case *ast.CondBlockStmt:
		if isPlainElse(s) {
			return p.block(s.Body)
		}
		return p.chain(s)
	}
	return docText("?")
}

func (p *pretty) expr(e ast.Expr, parentPrec int) doc {
	d, prec := p.exprPrec(e)
	if prec < parentPrec {
		return paren(d)
	}
	return d
}

func (p *pretty) exprRHS(e ast.Expr, opPrec int, afterMinus bool) doc {
	d, prec := p.exprPrec(e)
	if prec <= opPrec || (afterMinus && startsWithMinus(d)) {
		return paren(d)
	}
	return d
}

func paren(d doc) doc { return cat(docText("("), d, docText(")")) }

func (p *pretty) exprPrec(e ast.Expr) (doc, int) {
	switch e := e.(type) {
	case *ast.NumberLit:
		return docText(formatNumber(e.Value)), numberPrec(e)
	case *ast.BoolLit:
		if e.Value {
			return docText("true"), precPrimary
		}
		return docText("false"), precPrimary
	case *ast.StringLit:
		return docText("'" + e.Value + "'"), precPrimary
	case *ast.Ident:
		return p.ident(e.Namespace, e.Member), precPrimary
	case *ast.ThisExpr:
		return docText("this"), precPrimary
	case *ast.ArrowExpr:
		left := p.expr(e.Entity, precPrimary)
		return cat(left, docText("->"), p.expr(e.Read, precPrimary)), precPrimary
	case *ast.ArrayAccess:
		return cat(docText("array."+p.member(e.Name)+"["), p.expr(e.Index, 0), docText("]")), precPrimary
	case *ast.CallExpr:
		return p.call(e), precPrimary
	case *ast.UnaryExpr:
		sym := "-"
		if e.Op == ast.LNot {
			sym = "!"
		}
		return cat(docText(sym), p.exprRHS(e.X, precUnary-1, e.Op == ast.Neg)), precUnary
	case *ast.BinaryExpr:
		return p.binary(e), binaryPrec(e.Op)
	case *ast.AssignExpr:
		target := p.ident(e.Target.Namespace, e.Target.Member)
		return cat(target, docText(" = "), p.expr(e.Value, precNullish)), precNone
	case *ast.TernaryExpr:
		return p.chain(e), precTernary
	case *ast.CondBlockStmt:
		if isPlainElse(e) {
			return p.block(e.Body), precPrimary
		}
		return p.chain(e), precTernary
	}
	return docText("?"), precPrimary
}

// binary lays out a run of operators of one precedence -- `a && b && c`,
// `a + b - c`, `a ?? b ?? c` -- as one group, broken before every operator
// or none. The run is the left spine of the tree: these operators group to
// the left, so it is exactly the part printed without parentheses.
func (p *pretty) binary(e *ast.BinaryExpr) doc {
	if e.Op == ast.NullCoalesce {
		return p.nullish(e)
	}
	prec := binaryPrec(e.Op)
	spine := []*ast.BinaryExpr{e}
	for {
		x, ok := spine[len(spine)-1].X.(*ast.BinaryExpr)
		if !ok || binaryPrec(x.Op) != prec {
			break
		}
		spine = append(spine, x)
	}
	first := p.expr(spine[len(spine)-1].X, prec)
	var rest docCat
	for i := len(spine) - 1; i >= 0; i-- {
		b := spine[i]
		rest = append(rest, line, docText(binarySymbol(b.Op)+" "), p.exprRHS(b.Y, prec, b.Op == ast.Sub))
	}
	return group(first, docNest{rest})
}

// nullish lays out a `??` and the `??` on its right as one run. The left
// of a `??` has to be a variable, so a chain of them nests to the right and
// keeps its parentheses; they are closed together at the end rather than
// stepping every link further right:
//
//	v.x = v.first
//	    ?? (v.second
//	    ?? (v.third
//	    ?? 0));
func (p *pretty) nullish(e *ast.BinaryExpr) doc {
	first := p.expr(e.X, precNullish)
	var rest docCat
	closes := ""
	for cur := e; ; {
		if y, ok := cur.Y.(*ast.BinaryExpr); ok && y.Op == ast.NullCoalesce {
			rest = append(rest, line, docText("?? ("), p.expr(y.X, precNullish))
			closes += ")"
			cur = y
			continue
		}
		rest = append(rest, line, docText("?? "), p.exprRHS(cur.Y, precNullish, false))
		break
	}
	rest = append(rest, docText(closes))
	return group(first, docNest{rest})
}

// chain lays out a conditional and the conditionals in its else arm as one
// run, the way an else-if chain reads:
//
//	q.variant == 1 ? 'a'
//	    : q.variant == 2 ? 'b'
//	    : 'c'
//
// broken before each `:` or none, and each `cond ? then` broken before its
// `?` when that alone does not fit. With blocks for arms the braces carry
// the lines, and the run stays `} : cond ? {`.
func (p *pretty) chain(e ast.Expr) doc {
	var arms []doc
	// seps[i] goes before arms[i+1] (or the final else): a line that can
	// break, except after a block, whose `}` the `:` stays beside.
	var seps []doc
	var final doc
	cur := e
	for {
		cond, then, block, els, ok := p.conditional(cur)
		if !ok {
			final = p.elseArm(cur)
			break
		}
		if block {
			// A block arm is never moved to a line of its own: the braces
			// carry the lines, and a long condition breaks inside itself.
			arms = append(arms, cat(cond, docText(" ? "), then))
			seps = append(seps, docText(" "))
		} else {
			arms = append(arms, group(cond, docNest{cat(line, docText("? "), then)}))
			seps = append(seps, line)
		}
		if els == nil {
			break
		}
		cur = els
	}
	var rest docCat
	for i, a := range arms[1:] {
		rest = append(rest, seps[i], docText(": "), a)
	}
	if final != nil {
		rest = append(rest, seps[len(arms)-1], docText(": "), final)
	}
	return group(arms[0], docNest{rest})
}

// conditional splits a conditional that continues the chain into its
// condition, its Then arm and its else, laid out in source order. An else
// that is a block, or anything that is not a conditional, ends the chain.
func (p *pretty) conditional(e ast.Expr) (cond, then doc, block bool, els ast.Expr, ok bool) {
	switch x := e.(type) {
	case *ast.TernaryExpr:
		cond = p.expr(x.Cond, precOr)
		_, block = plainArm(x.Then)
		then = p.thenArm(x)
		if x.Else != nil {
			els = x.Else
		}
		return cond, then, block, els, true
	case *ast.CondBlockStmt:
		if isPlainElse(x) {
			return nil, nil, false, nil, false
		}
		cond = p.expr(x.Cond, precOr)
		then = p.block(x.Body)
		if x.Else != nil {
			els = x.Else
		}
		return cond, then, true, els, true
	}
	return nil, nil, false, nil, false
}

func (p *pretty) thenArm(t *ast.TernaryExpr) doc {
	if b, ok := plainArm(t.Then); ok {
		return p.block(b)
	}
	if t.Else != nil && danglingElse(t.Then) {
		return paren(p.expr(t.Then, precNone))
	}
	return p.expr(t.Then, precTernary)
}

// elseArm is the end of a chain: a block, or an expression that binds
// tighter than a conditional (one that does not is a conditional, and
// continues the chain instead).
func (p *pretty) elseArm(e ast.Expr) doc {
	if b, ok := plainArm(e); ok {
		return p.block(b)
	}
	return p.expr(e, precTernary)
}

// call lays out a call's arguments on one line, or one per line when they
// do not fit.
func (p *pretty) call(c *ast.CallExpr) doc {
	name := docText(namespaceName(c.Callee.Namespace, false) + "." + p.member(c.Callee.Member) + "(")
	if len(c.Args) == 0 {
		return cat(name, docText(")"))
	}
	prec := argPrec(c)
	var args docCat
	for i, a := range c.Args {
		if i > 0 {
			args = append(args, docText(","), line)
		}
		args = append(args, p.expr(a, prec))
	}
	return group(name, docNest{cat(softline, args)}, softline, docText(")"))
}

// errUnplaced is returned when the source's statements cannot be matched to
// the tree's, which would leave its comments with nowhere to go.
type errUnplaced struct{ what string }

func (e *errUnplaced) Error() string {
	return fmt.Sprintf("cannot place the comments: %s", e.what)
}
