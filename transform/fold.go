// Package transform implements source-to-source AST rewrites: constant
// folding and a macro/user-function expansion mechanism that lowers
// convenience calls to core Molang so the output still runs unmodified in
// vanilla Bedrock (and this library's own evaluator).
//
// Both operate on ast.Program in place and also return it, so calls chain:
// transform.FoldConstants(macros.Expand(prog)).
package transform

import (
	"math"
	"strings"

	"github.com/stirante/molang-go/ast"
	"github.com/stirante/molang-go/eval"
)

// FoldConstants replaces every subexpression that has no dependency on
// RNG, query/variable/temp/context reads, or assignment side effects with
// its computed literal value. Statement/control structure (loop, ?{...})
// is left intact even when its condition/count is constant — collapsing
// those is dead-code elimination, out of scope for this pass (see the
// package's "Defer for now" note in the README).
//
// A fold that would produce NaN or +/-Inf is skipped: Molang source has no
// literal for either, so folding one in would make the result unparseable
// (breaking Format/Minify round-tripping) for a case that already
// evaluates correctly unfolded.
func FoldConstants(prog *ast.Program) *ast.Program {
	for i, s := range prog.Stmts {
		prog.Stmts[i] = foldStmt(s)
	}
	return prog
}

func foldStmt(s ast.Stmt) ast.Stmt {
	switch s := s.(type) {
	case *ast.ExprStmt:
		s.X = foldExpr(s.X)
		return s
	case *ast.ReturnStmt:
		if s.Value != nil {
			s.Value = foldExpr(s.Value)
		}
		return s
	case *ast.LoopStmt:
		s.Count = foldExpr(s.Count)
		foldBlock(s.Body)
		return s
	case *ast.ForEachStmt:
		// Nothing to fold in the header: the array is a name rather than an
		// expression, and the loop variable is a write target.
		foldBlock(s.Body)
		return s
	case *ast.CondBlockStmt:
		return foldCondBlock(s)
	}
	return s
}

func foldCondBlock(cb *ast.CondBlockStmt) *ast.CondBlockStmt {
	cb.Cond = foldExpr(cb.Cond)
	foldBlock(cb.Body)
	if cb.Else != nil {
		cb.Else = foldCondBlock(cb.Else)
	}
	return cb
}

func foldBlock(b *ast.Block) {
	for i, s := range b.Stmts {
		b.Stmts[i] = foldStmt(s)
	}
}

// asNumber reports the literal numeric value of e, if e is already a
// NumberLit or BoolLit. String literals are deliberately never treated as
// foldable numbers here — see the package doc comment.
//
// NumberLit.Value is rounded to float32 here (eval.Round32), matching what
// eval.compileExpr does to every NumberLit at Compile time (see
// eval/compile.go). Without this, a folded constant subexpression could
// compute a very slightly different result than the same subexpression
// evaluated unfolded at runtime: eval rounds after every individual
// operation (Round32(Round32(x op y) op z)), while foldBinary below folds
// a whole chain in one pass -- rounding every intermediate step here too
// (not just the final result) is what keeps folded and unfolded
// evaluation bit-for-bit identical.
func asNumber(e ast.Expr) (float64, bool) {
	switch e := e.(type) {
	case *ast.NumberLit:
		return eval.Round32(e.Value), true
	case *ast.BoolLit:
		if e.Value {
			return 1, true
		}
		return 0, true
	}
	return 0, false
}

// isTruthy must stay bit-identical to eval.isTruthy, which this folder
// mirrors for the ternary: false iff EXACTLY zero, NaN included as truthy.
// CONFIRMED against the engine's compiled conditional instruction, which
// tests the condition for float32 equality with 0.0, so a NaN condition
// compares unordered and takes the then-branch. This used to carry the JS
// reading (NaN falsy) in both places; the evaluator was corrected, and a
// folder that disagreed would
// rewrite `math.sqrt(-1) ? a : b` into the wrong branch at print time.
func isTruthy(v float64) bool { return v != 0 }

func foldable(v float64) bool { return !math.IsNaN(v) && !math.IsInf(v, 0) }

func foldExpr(e ast.Expr) ast.Expr {
	switch e := e.(type) {
	case *ast.NumberLit, *ast.BoolLit, *ast.StringLit:
		return e

	case *ast.Ident:
		if e.Namespace == ast.Math && strings.ToLower(e.Member) == "pi" {
			return &ast.NumberLit{Value: eval.Round32(math.Pi)}
		}
		return e

	case *ast.UnaryExpr:
		e.X = foldExpr(e.X)
		if x, ok := asNumber(e.X); ok {
			switch e.Op {
			case ast.Neg:
				if v := eval.Round32(-x); foldable(v) {
					return &ast.NumberLit{Value: v}
				}
			case ast.LNot:
				if x == 0 {
					return &ast.NumberLit{Value: 1}
				}
				return &ast.NumberLit{Value: 0}
			}
		}
		return e

	case *ast.BinaryExpr:
		e.X = foldExpr(e.X)
		e.Y = foldExpr(e.Y)
		x, okx := asNumber(e.X)
		y, oky := asNumber(e.Y)
		if okx && oky {
			if v, ok := foldBinary(e.Op, x, y); ok && foldable(v) {
				return &ast.NumberLit{Value: v}
			}
		}
		return e

	case *ast.ArrowExpr:
		// Nothing folds: the value depends on an entity not known until
		// evaluation. The arguments of a query call through it still do.
		e.Read = foldExpr(e.Read)
		return e

	case *ast.ArrayAccess:
		// The index folds; the access itself never does. Its value depends
		// on the host's array, which is not known until evaluation.
		e.Index = foldExpr(e.Index)
		return e

	case *ast.CallExpr:
		for i, a := range e.Args {
			e.Args[i] = foldExpr(a)
		}
		if e.Callee.Namespace == ast.Math {
			member := strings.ToLower(e.Callee.Member)
			// math.pi() only, never math.pi(x): eval.compileMathCall
			// rejects arguments to math.pi outright, so folding
			// `math.pi(1,2)` to a literal would turn a program the
			// evaluator refuses to compile into one it accepts. Leaving it
			// alone keeps the error where it belongs.
			if member == "pi" {
				if len(e.Args) == 0 {
					return &ast.NumberLit{Value: eval.Round32(math.Pi)}
				}
				return e
			}
			args := make([]float64, len(e.Args))
			allConst := true
			for i, a := range e.Args {
				v, ok := asNumber(a)
				if !ok {
					allConst = false
					break
				}
				args[i] = v
			}
			// EvalMathPure applies the same unknown-name and arity checks
			// eval.compileMathCall does, and returns ok=false rather than
			// folding when either fails -- so a call the evaluator would
			// reject stays in the tree as written.
			if allConst {
				if v, ok := eval.EvalMathPure(member, args); ok && foldable(v) {
					return &ast.NumberLit{Value: v}
				}
			}
		}
		return e

	case *ast.AssignExpr:
		e.Value = foldExpr(e.Value)
		return e

	case *ast.TernaryExpr:
		e.Cond = foldExpr(e.Cond)
		e.Then = foldExpr(e.Then)
		if e.Else != nil {
			e.Else = foldExpr(e.Else)
		}
		if c, ok := asNumber(e.Cond); ok {
			if isTruthy(c) {
				return e.Then
			}
			if e.Else != nil {
				return e.Else
			}
			return &ast.NumberLit{Value: 0}
		}
		return e

	case *ast.CondBlockStmt:
		return foldCondBlock(e)
	}
	return e
}

// foldBinary reproduces eval.compileBinary's constant-operand semantics
// (float32 rounding on every arithmetic op, the Divide near-zero guard,
// boolean-normalizing &&/||, NaN-coalescing ??) so folding never changes a
// program's result.
//
// "Reproduces", not "shares": compileBinary builds lazy closures shaped
// around evaluation order (Divide evaluates its denominator first and
// skips the numerator when the guard fires; &&/||/?? short-circuit), none
// of which matters for two literal operands. That shape difference is why
// this second implementation exists at all -- and it is exactly how the
// two drifted before, so the ONE genuinely subtle numeric decision (the
// divide guard) is now shared verbatim via eval.DivGuardFires rather than
// restated, and TestFoldMatchesEval in fold_agreement_test.go cross-checks
// every operator in this switch against a compiled program over a grid of
// operand pairs. Add an operator here and it is covered automatically.
func foldBinary(op ast.BinaryOp, x, y float64) (float64, bool) {
	b2f := func(b bool) float64 {
		if b {
			return 1
		}
		return 0
	}
	switch op {
	case ast.Add:
		return eval.Round32(x + y), true
	case ast.Sub:
		return eval.Round32(x - y), true
	case ast.Mul:
		return eval.Round32(x * y), true
	case ast.Div:
		// The engine's Divide is NOT a bare divide: a denominator with
		// |den| < FLT_EPSILON short-circuits the whole division to +0.0
		// (see eval.DivGuardFires and compile.go's ast.Div case). Folding
		// with a plain Go `/` here is what made this function's "mirrors
		// eval exactly" claim false: `1/0.0000001` folded to `1e+07`,
		// which printer.Minify then emitted as the literal `10000000`,
		// while the evaluator reads that same source back as `0`.
		//
		// Guarding (rather than refusing to fold division at all) is the
		// resolution because the guard is a total function of the
		// denominator alone, and both operands here are literals: there is
		// no side effect or RNG draw whose ORDER the evaluator's
		// denominator-first shape would otherwise preserve, so applying
		// the same predicate is sufficient to make folded and unfolded
		// division agree for every constant pair. Refusing to fold would
		// also have been correct but strictly weaker -- it would leave
		// every constant division in the output unfolded to buy nothing.
		if eval.DivGuardFires(y) {
			return 0, true
		}
		return eval.Round32(x / y), true
	case ast.CmpLt:
		return b2f(x < y), true
	case ast.CmpLe:
		return b2f(x <= y), true
	case ast.CmpGt:
		return b2f(x > y), true
	case ast.CmpGe:
		return b2f(x >= y), true
	case ast.CmpEq:
		return b2f(x == y), true
	case ast.CmpNe:
		return b2f(x != y), true
	case ast.LAnd:
		if x == 0 {
			return 0, true
		}
		return b2f(y != 0), true
	case ast.LOr:
		if x != 0 {
			return 1, true
		}
		return b2f(y != 0), true
	case ast.NullCoalesce:
		// `??` catches an UNRESOLVED VARIABLE READ in its left operand
		// (see eval/unresolved.go). A constant is never unresolved, and
		// this function is only ever reached with two already-constant
		// operands, so a foldable `??` always yields its left side. The
		// NaN test that used to be here came from the old NaN-coalescing
		// reading and would now silently rewrite `math.sqrt(-1) ?? 5`
		// (NaN in the evaluator) into `5`.
		return x, true
	}
	return 0, false
}
