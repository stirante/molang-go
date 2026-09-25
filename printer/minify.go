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

// program renders the top level, deciding the trailing ';' exactly as Format
// does (see topLevel).
//
// No ';' is dropped to save a byte. The trailing one on a single-statement
// program carries meaning (ast.Program.HasSemicolon), and the game refuses
// any expression containing `=` or `;` that does not end with one -- Minify
// used to write `t.a=1;t.b=2` and `{t.a=1}`, neither of which loads.
func (m *minifier) program(p *ast.Program) string {
	switch topLevel(p) {
	case shapeBare:
		if es, ok := p.Stmts[0].(*ast.ExprStmt); ok {
			return m.expr(es.X, precNone)
		}
		return m.stmt(p.Stmts[0])
	case shapeReturn:
		return "return " + m.expr(p.Stmts[0].(*ast.ExprStmt).X, precNone) + ";"
	}
	var out strings.Builder
	for _, s := range p.Stmts {
		out.WriteString(m.stmt(s))
		out.WriteByte(';')
	}
	return out.String()
}

// block renders a brace section. Every statement is followed by ';',
// including the last: the game refuses a brace section with no ';' in it,
// so `{t.a=1}` does not load where `{t.a=1;}` does.
func (m *minifier) block(b *ast.Block) string {
	var out strings.Builder
	out.WriteByte('{')
	for _, s := range b.Stmts {
		out.WriteString(m.stmt(s))
		out.WriteByte(';')
	}
	out.WriteByte('}')
	return out.String()
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
			"," + m.expr(s.Source, precNone) + "," + m.block(s.Body) + ")"
	case *ast.CondBlockStmt:
		if isPlainElse(s) {
			return m.block(s.Body)
		}
		return m.condBlock(s)
	}
	return "?"
}

func (m *minifier) condBlock(cb *ast.CondBlockStmt) string {
	out := m.expr(cb.Cond, precOr) + "?" + m.block(cb.Body)
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

// exprRHS is the formatter's: the right operand of a left-associative
// operator, parenthesized at equal precedence and, after a `-`, when it
// starts with a `-` itself.
func (m *minifier) exprRHS(e ast.Expr, opPrec int, afterMinus bool) string {
	s, prec := m.exprPrec(e)
	if prec <= opPrec || keepsSign(s, afterMinus) {
		return "(" + s + ")"
	}
	return s
}

func (m *minifier) exprPrec(e ast.Expr) (string, int) {
	switch e := e.(type) {
	case *ast.NumberLit:
		return formatNumber(e.Value), numberPrec(e)
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
		return m.expr(e.Entity, precPrimary) + "->" + m.expr(e.Read, precPrimary), precPrimary
	case *ast.ArrayAccess:
		return "array." + e.Name + "[" + m.expr(e.Index, 0) + "]", precPrimary
	case *ast.CallExpr:
		return m.call(e), precPrimary
	case *ast.UnaryExpr:
		sym := "-"
		if e.Op == ast.LNot {
			sym = "!"
		}
		return sym + m.exprRHS(e.X, precUnary-1, e.Op == ast.Neg), precUnary
	case *ast.BinaryExpr:
		prec := binaryPrec(e.Op)
		left := m.expr(e.X, prec)
		right := m.exprRHS(e.Y, prec, e.Op == ast.Sub)
		return left + binarySymbol(e.Op) + right, prec
	case *ast.AssignExpr:
		return namespaceName(e.Target.Namespace, true) + "." + e.Target.Member + "=" + m.expr(e.Value, precNullish), precNone
	case *ast.TernaryExpr:
		out := m.expr(e.Cond, precOr) + "?" + m.thenArm(e)
		if e.Else != nil {
			out += ":" + m.elseArm(e.Else)
		}
		return out, precTernary
	case *ast.CondBlockStmt:
		// The right side of a `??`; see formatter.exprPrec.
		if isPlainElse(e) {
			return m.block(e.Body), precPrimary
		}
		return m.condBlock(e), precTernary
	}
	return "?", precPrimary
}

func (m *minifier) thenArm(t *ast.TernaryExpr) string {
	if b, ok := plainArm(t.Then); ok {
		return m.block(b)
	}
	if t.Else != nil && danglingElse(t.Then) {
		return "(" + m.expr(t.Then, precNone) + ")"
	}
	return m.expr(t.Then, precTernary)
}

func (m *minifier) elseArm(e ast.Expr) string {
	if b, ok := plainArm(e); ok {
		return m.block(b)
	}
	return m.expr(e, precTernary)
}

func (m *minifier) call(c *ast.CallExpr) string {
	args := make([]string, len(c.Args))
	prec := argPrec(c)
	for i, a := range c.Args {
		args[i] = m.expr(a, prec)
	}
	return namespaceName(c.Callee.Namespace, true) + "." + c.Callee.Member + "(" + strings.Join(args, ",") + ")"
}
