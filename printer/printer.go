// Package printer turns an ast.Program back into Molang source text, either
// as readable output (Format) or the shortest valid equivalent (Minify).
//
// Both are precedence-aware: they never rely on the original source's
// parenthesization (none is kept in the AST — see ast.Node), they compute
// exactly which parens are structurally required from operator precedence
// and associativity, so Parse -> Format -> Parse and Parse -> Minify ->
// Parse always round-trip to an equivalent AST and identical evaluation
// result.
//
// Minify correctness trap: temp./variable. member names are NOT safe to
// shorten/rename per-expression. In the engine, temp. is thread-local
// storage shared across a whole worldgen generation and variable. is shared
// along an entire placement chain — packs deliberately pass values between
// separate feature files through them. Renaming member identifiers is only
// ever safe when a whole pack is analyzed together as one unit, which this
// package does not attempt. Minify therefore never touches member names —
// it only shortens namespace spelling (variable. -> v., etc.), which is
// always safe because the two spellings are exactly equivalent everywhere,
// not a rename of anything.
package printer

import (
	"math"
	"strconv"
	"strings"

	"github.com/stirante/molang-go/ast"
)

// precedence levels, low to high. Matches the parser's grammar comment:
// `??` binds looser than the conditional (`v.a ?? 1 ? 2 : 3` is
// `v.a ?? (1 ? 2 : 3)`) and tighter than assignment, so a `??` inside a
// conditional's arm or condition is parenthesized and one on the right of
// `=` is not. `/` binds tighter than `*`, as the game groups it: `a * b / c`
// is `a * (b / c)` and needs no parentheses, while `(a * b) / c` keeps its.
const (
	precNone = iota
	precNullish
	precTernary
	precOr
	precAnd
	precEquality
	precRelational
	precAdditive
	precMultiplicative
	precDivisive
	precUnary
	precPrimary
)

func binaryPrec(op ast.BinaryOp) int {
	switch op {
	case ast.NullCoalesce:
		return precNullish
	case ast.LOr:
		return precOr
	case ast.LAnd:
		return precAnd
	case ast.CmpEq, ast.CmpNe:
		return precEquality
	case ast.CmpLt, ast.CmpLe, ast.CmpGt, ast.CmpGe:
		return precRelational
	case ast.Add, ast.Sub:
		return precAdditive
	case ast.Mul:
		return precMultiplicative
	case ast.Div:
		return precDivisive
	}
	return precNone
}

// numberPrec is the precedence a literal is written at: a negative literal
// is written with a `-` in front, which reads back as a negation, so it is
// placed as a negation would be.
func numberPrec(n *ast.NumberLit) int {
	if math.Signbit(n.Value) {
		return precUnary
	}
	return precPrimary
}

// keepsSign reports whether an operand written after a `-` -- the right
// side of a subtraction, or the operand of a negation -- has to be
// parenthesized because its text starts with a `-` itself. Two `-` in a row
// are a `+` to the game: `a - -b` is `a + b`, and `- -b` on its own does not
// load. `a - (-b)`, `-(-b)` and `a - (-b * c)` say what the tree says. (The
// depth model in ast decides the same thing from the tree's shape; the two
// have to agree, and a test in ast holds them together.)
func keepsSign(printed string, afterMinus bool) bool {
	return afterMinus && strings.HasPrefix(printed, "-")
}

func binarySymbol(op ast.BinaryOp) string {
	switch op {
	case ast.Add:
		return "+"
	case ast.Sub:
		return "-"
	case ast.Mul:
		return "*"
	case ast.Div:
		return "/"
	case ast.CmpLt:
		return "<"
	case ast.CmpLe:
		return "<="
	case ast.CmpGt:
		return ">"
	case ast.CmpGe:
		return ">="
	case ast.CmpEq:
		return "=="
	case ast.CmpNe:
		return "!="
	case ast.LAnd:
		return "&&"
	case ast.LOr:
		return "||"
	case ast.NullCoalesce:
		return "??"
	}
	return "?"
}

// formatNumber is the canonical textual form of a numeric literal, shared
// by Format and Minify: the shortest decimal that reads back to exactly v,
// with no exponent notation (Molang source doesn't use it).
func formatNumber(v float64) string {
	if math.IsNaN(v) || math.IsInf(v, 0) {
		// Not producible by the lexer and guarded against by the constant
		// folder; kept as a defined (if unparseable) fallback rather than
		// panicking.
		return strconv.FormatFloat(v, 'g', -1, 64)
	}
	return strconv.FormatFloat(v, 'f', -1, 64)
}

func namespaceName(ns ast.Namespace, short bool) string {
	if short {
		return ns.ShortAlias()
	}
	return ns.String()
}

// ---------------------------------------------------------------------
// Format — readable output.
// ---------------------------------------------------------------------

// Format renders prog as readable Molang source.
func Format(prog *ast.Program) string {
	f := &formatter{}
	return f.program(prog)
}

type formatter struct{}

// program renders the top level. See topLevel for when the trailing ';' is
// written; it is load-bearing both ways.
func (f *formatter) program(p *ast.Program) string {
	switch topLevel(p) {
	case shapeBare:
		if es, ok := p.Stmts[0].(*ast.ExprStmt); ok {
			return f.expr(es.X, precNone)
		}
		return f.stmt(p.Stmts[0])
	case shapeReturn:
		return "return " + f.expr(p.Stmts[0].(*ast.ExprStmt).X, precNone) + ";"
	}
	parts := make([]string, len(p.Stmts))
	for i, s := range p.Stmts {
		parts[i] = f.stmt(s) + ";"
	}
	return strings.Join(parts, " ")
}

func (f *formatter) block(b *ast.Block) string {
	if len(b.Stmts) == 0 {
		return "{}"
	}
	parts := make([]string, len(b.Stmts))
	for i, s := range b.Stmts {
		parts[i] = f.stmt(s) + ";"
	}
	return "{ " + strings.Join(parts, " ") + " }"
}

func (f *formatter) stmt(s ast.Stmt) string {
	switch s := s.(type) {
	case *ast.ExprStmt:
		return f.expr(s.X, precNone)
	case *ast.ReturnStmt:
		if s.Value == nil {
			return "return"
		}
		return "return " + f.expr(s.Value, precNone)
	case *ast.BreakStmt:
		return "break"
	case *ast.ContinueStmt:
		return "continue"
	case *ast.LoopStmt:
		return "loop(" + f.expr(s.Count, precNone) + ", " + f.block(s.Body) + ")"
	case *ast.ForEachStmt:
		return "for_each(" + namespaceName(s.Var.Namespace, false) + "." + s.Var.Member +
			", " + f.expr(s.Source, precNone) + ", " + f.block(s.Body) + ")"
	case *ast.CondBlockStmt:
		if isPlainElse(s) {
			return f.block(s.Body)
		}
		return f.condBlock(s)
	}
	return "?"
}

func (f *formatter) condBlock(cb *ast.CondBlockStmt) string {
	out := f.expr(cb.Cond, precOr) + " ? " + f.block(cb.Body)
	if cb.Else != nil {
		if isPlainElse(cb.Else) {
			out += " : " + f.block(cb.Else.Body)
		} else {
			out += " : " + f.condBlock(cb.Else)
		}
	}
	return out
}

// isPlainElse reports whether cb is the synthetic "cond=true" wrapper the
// parser uses to represent an unconditional `{ ... }`: a plain `: { ... }`
// else-clause (as opposed to a real else-if chain link), a bare grouping
// block, or a block arm of a ternary.
func isPlainElse(cb *ast.CondBlockStmt) bool {
	b, ok := cb.Cond.(*ast.BoolLit)
	return ok && b.Value
}

// plainArm reports the block of a ternary arm that is an unconditional block,
// which prints as the bare `{ ... }` it was written as.
func plainArm(e ast.Expr) (*ast.Block, bool) {
	cb, ok := e.(*ast.CondBlockStmt)
	if !ok || !isPlainElse(cb) {
		return nil, false
	}
	return cb.Body, true
}

// danglingElse reports whether e, printed as the Then arm of a ternary that
// has an else, would capture that else itself on reparse: a conditional whose
// else-chain ends without an else. `c ? (d ? 1) : 2` printed without its
// parentheses reads back as `c ? (d ? 1 : 2)`.
func danglingElse(e ast.Expr) bool {
	switch x := e.(type) {
	case *ast.TernaryExpr:
		return x.Else == nil || danglingElse(x.Else)
	case *ast.CondBlockStmt:
		if isPlainElse(x) {
			return false
		}
		return x.Else == nil || danglingElse(x.Else)
	}
	return false
}

// programShape is how the top level of a program is written.
type programShape uint8

const (
	// shapeBare: one statement, written with no ';' at all.
	shapeBare programShape = iota
	// shapeReturn: one expression, written as `return <expr>;`.
	shapeReturn
	// shapeSequence: every statement followed by ';'.
	shapeSequence
)

// topLevel decides how a program's top level is written.
//
// Two things constrain it, and both are hard:
//
//   - The game refuses an expression that contains `=` or `;` anywhere and
//     does not end with ';'. Anything with an assignment, or with a non-empty
//     block (whose statements are always written with their ';'), must end
//     with one.
//   - ast.Program.HasSemicolon distinguishes a bare expression (whose value
//     IS the program's value) from a sequence (0 unless it returns), and
//     eval.Compile reads it. A program that parsed WITH a trailing ';' must
//     keep it, and one without must not silently change meaning.
//
// The two only collide for a single expression with no ';' that nonetheless
// needs one -- `v.x = 5`, which only a caller opting out of the semicolon
// rules can parse, and which evaluates to 5. `v.x = 5;` would load but
// evaluate to 0, so it is written `return v.x = 5;`, which loads AND keeps
// the value. A lone block, loop or for_each needs no such care: it yields 0
// unless it returns either way, so it simply gains the ';'.
func topLevel(p *ast.Program) programShape {
	if len(p.Stmts) != 1 || p.HasSemicolon {
		return shapeSequence
	}
	if !needsTrailingSemicolon(p) {
		return shapeBare
	}
	if es, ok := p.Stmts[0].(*ast.ExprStmt); ok {
		if _, isBlock := es.X.(*ast.CondBlockStmt); !isBlock {
			return shapeReturn
		}
	}
	return shapeSequence
}

// needsTrailingSemicolon reports whether the printed program will contain a
// `=` or `;` token: an assignment, or a block with at least one statement.
func needsTrailingSemicolon(p *ast.Program) bool {
	found := false
	ast.Walk(p, func(n ast.Node) bool {
		switch x := n.(type) {
		case *ast.AssignExpr:
			found = true
		case *ast.Block:
			if len(x.Stmts) > 0 {
				found = true
			}
		}
		return !found
	})
	return found
}

// expr renders e, parenthesizing it if its own precedence is lower than
// (or, for the right operand of a left-associative operator, equal to)
// parentPrec.
func (f *formatter) expr(e ast.Expr, parentPrec int) string {
	s, prec := f.exprPrec(e)
	if prec < parentPrec {
		return "(" + s + ")"
	}
	return s
}

// exprRHS renders the right operand of a left-associative binary operator:
// needs parens even at equal precedence (a-(b-c) != a-b-c), and after a
// `-` when it starts with a `-` itself (see keepsSign).
func (f *formatter) exprRHS(e ast.Expr, opPrec int, afterMinus bool) string {
	s, prec := f.exprPrec(e)
	if prec <= opPrec || keepsSign(s, afterMinus) {
		return "(" + s + ")"
	}
	return s
}

func (f *formatter) exprPrec(e ast.Expr) (string, int) {
	switch e := e.(type) {
	case *ast.NumberLit:
		return formatNumber(e.Value), numberPrec(e)
	case *ast.BoolLit:
		if e.Value {
			return "true", precPrimary
		}
		return "false", precPrimary
	case *ast.StringLit:
		return "'" + e.Value + "'", precPrimary
	case *ast.Ident:
		return namespaceName(e.Namespace, false) + "." + e.Member, precPrimary
	case *ast.ThisExpr:
		return "this", precPrimary
	case *ast.ArrowExpr:
		// The arrow binds tighter than any operator, so a left side that is
		// itself an operation -- `(v.a + 1)->v.x` -- keeps its parentheses,
		// and `-c.o->v.x`, which is -(c.o->v.x), needs none.
		return f.expr(e.Entity, precPrimary) + "->" + f.expr(e.Read, precPrimary), precPrimary
	case *ast.ArrayAccess:
		return "array." + e.Name + "[" + f.expr(e.Index, 0) + "]", precPrimary
	case *ast.CallExpr:
		return f.call(e, false), precPrimary
	case *ast.UnaryExpr:
		sym := "-"
		if e.Op == ast.LNot {
			sym = "!"
		}
		return sym + f.exprRHS(e.X, precUnary-1, e.Op == ast.Neg), precUnary
	case *ast.BinaryExpr:
		prec := binaryPrec(e.Op)
		left := f.expr(e.X, prec)
		right := f.exprRHS(e.Y, prec, e.Op == ast.Sub)
		return left + " " + binarySymbol(e.Op) + " " + right, prec
	case *ast.AssignExpr:
		return namespaceName(e.Target.Namespace, false) + "." + e.Target.Member + " = " + f.expr(e.Value, precNullish), precNone
	case *ast.TernaryExpr:
		out := f.expr(e.Cond, precOr) + " ? " + f.thenArm(e)
		if e.Else != nil {
			out += " : " + f.elseArm(e.Else)
		}
		return out, precTernary
	case *ast.CondBlockStmt:
		return f.condBlock(e), precTernary
	}
	return "?", precPrimary
}

// thenArm renders a ternary's Then arm: a block as the block it was written
// as, and a conditional that would capture this ternary's else in
// parentheses.
func (f *formatter) thenArm(t *ast.TernaryExpr) string {
	if b, ok := plainArm(t.Then); ok {
		return f.block(b)
	}
	if t.Else != nil && danglingElse(t.Then) {
		return "(" + f.expr(t.Then, precNone) + ")"
	}
	return f.expr(t.Then, precTernary)
}

func (f *formatter) elseArm(e ast.Expr) string {
	if b, ok := plainArm(e); ok {
		return f.block(b)
	}
	return f.expr(e, precTernary)
}

// argPrec is the precedence an argument is written at: an assignment among
// several arguments has to be parenthesized, because the game groups the
// commas of an argument list before it groups `=`, so `q.f(v.a = 1, 2)`
// does not load and `q.f((v.a = 1), 2)` does. A lone argument needs no
// such care, and the parentheses would be one more level of nesting for
// the game to count (see ast.Depth).
func argPrec(c *ast.CallExpr) int {
	if len(c.Args) > 1 {
		return precNullish
	}
	return precNone
}

func (f *formatter) call(c *ast.CallExpr, short bool) string {
	args := make([]string, len(c.Args))
	prec := argPrec(c)
	for i, a := range c.Args {
		args[i] = f.expr(a, prec)
	}
	return namespaceName(c.Callee.Namespace, short) + "." + c.Callee.Member + "(" + strings.Join(args, ", ") + ")"
}
