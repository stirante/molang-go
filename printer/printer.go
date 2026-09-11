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

// precedence levels, low to high. Matches the parser's grammar comment.
const (
	precNone = iota
	precTernary
	precNullish
	precOr
	precAnd
	precEquality
	precRelational
	precAdditive
	precMultiplicative
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
	case ast.Mul, ast.Div:
		return precMultiplicative
	}
	return precNone
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

// program renders the top level. The trailing ';' is load-bearing, not
// cosmetic: ast.Program.HasSemicolon distinguishes a bare expression (whose
// value IS the program's value) from a one-statement sequence (which yields
// 0 unless it returns), and eval.Compile reads it. So a program that parsed
// without a top-level ';' must print without one, and a single-statement
// program that parsed WITH one must keep it -- otherwise Format's output
// means something different from its input. Format used to append ';' to
// every single-statement non-expression program unconditionally, which
// silently converted a bare `{ ... }` grouping block into a sequence.
func (f *formatter) program(p *ast.Program) string {
	if len(p.Stmts) == 1 && !p.HasSemicolon {
		if es, ok := p.Stmts[0].(*ast.ExprStmt); ok {
			return f.expr(es.X, precNone)
		}
		return f.stmt(p.Stmts[0])
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
			", array." + s.Array + ", " + f.block(s.Body) + ")"
	case *ast.CondBlockStmt:
		return f.condBlock(s)
	}
	return "?"
}

func (f *formatter) condBlock(cb *ast.CondBlockStmt) string {
	out := f.expr(cb.Cond, precNullish) + " ? " + f.block(cb.Body)
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
// parser uses to represent a plain `: { ... }` else-clause (as opposed to a
// real else-if chain link).
func isPlainElse(cb *ast.CondBlockStmt) bool {
	b, ok := cb.Cond.(*ast.BoolLit)
	return ok && b.Value
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
// needs parens even at equal precedence (a-(b-c) != a-b-c).
func (f *formatter) exprRHS(e ast.Expr, opPrec int) string {
	s, prec := f.exprPrec(e)
	if prec <= opPrec {
		return "(" + s + ")"
	}
	return s
}

func (f *formatter) exprPrec(e ast.Expr) (string, int) {
	switch e := e.(type) {
	case *ast.NumberLit:
		return formatNumber(e.Value), precPrimary
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
		return namespaceName(e.Entity.Namespace, false) + "." + e.Entity.Member +
			"->" + f.expr(e.Read, precPrimary), precPrimary
	case *ast.ArrayAccess:
		return "array." + e.Name + "[" + f.expr(e.Index, 0) + "]", precPrimary
	case *ast.CallExpr:
		return f.call(e, false), precPrimary
	case *ast.UnaryExpr:
		sym := "-"
		if e.Op == ast.LNot {
			sym = "!"
		}
		return sym + f.expr(e.X, precUnary), precUnary
	case *ast.BinaryExpr:
		prec := binaryPrec(e.Op)
		left := f.expr(e.X, prec)
		right := f.exprRHS(e.Y, prec)
		return left + " " + binarySymbol(e.Op) + " " + right, prec
	case *ast.AssignExpr:
		return namespaceName(e.Target.Namespace, false) + "." + e.Target.Member + " = " + f.expr(e.Value, precTernary), precNone
	case *ast.TernaryExpr:
		out := f.expr(e.Cond, precNullish) + " ? " + f.expr(e.Then, precTernary)
		if e.Else != nil {
			out += " : " + f.expr(e.Else, precTernary)
		}
		return out, precTernary
	case *ast.CondBlockStmt:
		return f.condBlock(e), precTernary
	}
	return "?", precPrimary
}

func (f *formatter) call(c *ast.CallExpr, short bool) string {
	args := make([]string, len(c.Args))
	for i, a := range c.Args {
		args[i] = f.expr(a, precTernary)
	}
	return namespaceName(c.Callee.Namespace, short) + "." + c.Callee.Member + "(" + strings.Join(args, ", ") + ")"
}
