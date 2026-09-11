package printer

import (
	"strings"

	"github.com/stirante/molang-go/ast"
)

// Minify renders prog as the shortest valid equivalent Molang source.
//
// It shortens namespace spellings to their one-letter alias where the
// engine has one — variable. -> v., query. -> q., temp. -> t.,
// context. -> c. — which is always safe, since those four spellings are
// exactly equivalent everywhere.
//
// math. is NOT shortened. Minify used to emit `m.`, and `m.` is not a
// Molang alias: the engine's tokenizer has a hand-written chain of exactly
// four two-character prefixes and math is not among them, so every
// minified expression containing a math call was source Bedrock rejects at
// load time with "unknown token". See ast.Namespace.ShortAlias for the
// detail; this function just renders whatever that returns, so the
// fix lives there and Format/Minify cannot drift apart on it.
//
// It deliberately does NOT rename temp./variable.
// member names: those bags are shared thread-/placement-chain-wide in the
// real engine (see the package doc comment), and a per-expression rename
// would silently break packs that pass values between files through them.
func Minify(prog *ast.Program) string {
	m := &minifier{}
	return m.program(prog)
}

type minifier struct{}

// program renders the top level. The one ';' Minify is NOT allowed to drop
// is the trailing one on a single-statement program: ast.Program.
// HasSemicolon is what tells eval.Compile that program is a sequence
// (yielding 0 unless it returns) rather than a bare expression (yielding
// its value), so dropping it -- as Minify used to -- rewrites the program's
// meaning to buy one byte. Multi-statement programs need no trailing ';':
// the separators between statements already set HasSemicolon on reparse.
func (m *minifier) program(p *ast.Program) string {
	if len(p.Stmts) == 1 && !p.HasSemicolon {
		if es, ok := p.Stmts[0].(*ast.ExprStmt); ok {
			return m.expr(es.X, precNone)
		}
		return m.stmt(p.Stmts[0])
	}
	parts := make([]string, len(p.Stmts))
	for i, s := range p.Stmts {
		parts[i] = m.stmt(s)
	}
	out := strings.Join(parts, ";")
	if len(p.Stmts) == 1 {
		out += ";"
	}
	return out
}

func (m *minifier) block(b *ast.Block) string {
	parts := make([]string, len(b.Stmts))
	for i, s := range b.Stmts {
		parts[i] = m.stmt(s)
	}
	return "{" + strings.Join(parts, ";") + "}"
}

func (m *minifier) stmt(s ast.Stmt) string {
	switch s := s.(type) {
	case *ast.ExprStmt:
		return m.expr(s.X, precNone)
	case *ast.ReturnStmt:
		if s.Value == nil {
			return "return"
		}
		// The one unavoidable mandatory space: "return" is a word token,
		// and every possible value starts with a character (letter,
		// digit, quote is fine actually, but keyword true/false, '-', '!'
		// are not all word-safe) that could merge with it — a leading
		// digit or letter would silently become part of the keyword text.
		return "return " + m.expr(s.Value, precNone)
	case *ast.BreakStmt:
		return "break"
	case *ast.ContinueStmt:
		return "continue"
	case *ast.LoopStmt:
		return "loop(" + m.expr(s.Count, precNone) + "," + m.block(s.Body) + ")"
	case *ast.ForEachStmt:
		return "for_each(" + namespaceName(s.Var.Namespace, true) + "." + s.Var.Member +
			",array." + s.Array + "," + m.block(s.Body) + ")"
	case *ast.CondBlockStmt:
		return m.condBlock(s)
	}
	return "?"
}

func (m *minifier) condBlock(cb *ast.CondBlockStmt) string {
	out := m.expr(cb.Cond, precNullish) + "?" + m.block(cb.Body)
	if cb.Else != nil {
		if isPlainElse(cb.Else) {
			out += ":" + m.block(cb.Else.Body)
		} else {
			out += ":" + m.condBlock(cb.Else)
		}
	}
	return out
}

func (m *minifier) expr(e ast.Expr, parentPrec int) string {
	s, prec := m.exprPrec(e)
	if prec < parentPrec {
		return "(" + s + ")"
	}
	return s
}

func (m *minifier) exprRHS(e ast.Expr, opPrec int) string {
	s, prec := m.exprPrec(e)
	if prec <= opPrec {
		return "(" + s + ")"
	}
	return s
}

func (m *minifier) exprPrec(e ast.Expr) (string, int) {
	switch e := e.(type) {
	case *ast.NumberLit:
		return formatNumber(e.Value), precPrimary
	case *ast.BoolLit:
		// Shorter than true/false and exactly equivalent.
		if e.Value {
			return "1", precPrimary
		}
		return "0", precPrimary
	case *ast.StringLit:
		return "'" + e.Value + "'", precPrimary
	case *ast.Ident:
		return namespaceName(e.Namespace, true) + "." + e.Member, precPrimary
	case *ast.ThisExpr:
		return "this", precPrimary
	case *ast.ArrowExpr:
		return namespaceName(e.Entity.Namespace, true) + "." + e.Entity.Member +
			"->" + m.expr(e.Read, precPrimary), precPrimary
	case *ast.ArrayAccess:
		return "array." + e.Name + "[" + m.expr(e.Index, 0) + "]", precPrimary
	case *ast.CallExpr:
		return m.call(e), precPrimary
	case *ast.UnaryExpr:
		sym := "-"
		if e.Op == ast.LNot {
			sym = "!"
		}
		return sym + m.expr(e.X, precUnary), precUnary
	case *ast.BinaryExpr:
		prec := binaryPrec(e.Op)
		left := m.expr(e.X, prec)
		right := m.exprRHS(e.Y, prec)
		return left + binarySymbol(e.Op) + right, prec
	case *ast.AssignExpr:
		return namespaceName(e.Target.Namespace, true) + "." + e.Target.Member + "=" + m.expr(e.Value, precTernary), precNone
	case *ast.TernaryExpr:
		out := m.expr(e.Cond, precNullish) + "?" + m.expr(e.Then, precTernary)
		if e.Else != nil {
			out += ":" + m.expr(e.Else, precTernary)
		}
		return out, precTernary
	case *ast.CondBlockStmt:
		return m.condBlock(e), precTernary
	}
	return "?", precPrimary
}

func (m *minifier) call(c *ast.CallExpr) string {
	args := make([]string, len(c.Args))
	for i, a := range c.Args {
		args[i] = m.expr(a, precTernary)
	}
	return namespaceName(c.Callee.Namespace, true) + "." + c.Callee.Member + "(" + strings.Join(args, ",") + ")"
}
