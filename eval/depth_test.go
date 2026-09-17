package eval_test

import (
	"strings"
	"testing"

	"github.com/stirante/molang-go/ast"
	"github.com/stirante/molang-go/eval"
	"github.com/stirante/molang-go/parser"
)

// notChain builds `!!!...1` with n negations as a tree, which the parser
// never sees, so Compile's own check is what refuses it.
func notChain(n int) *ast.Program {
	var e ast.Expr = &ast.NumberLit{Value: 1}
	for i := 0; i < n; i++ {
		e = &ast.UnaryExpr{Op: ast.LNot, X: e}
	}
	return &ast.Program{Stmts: []ast.Stmt{&ast.ExprStmt{X: e}}}
}

// TestCompileRefusesDeepTree: a hand-built tree is held to the same nesting
// limit as a parsed one, one short of it, at it, and past it.
func TestCompileRefusesDeepTree(t *testing.T) {
	for _, n := range []int{ast.DepthLimit - 2, ast.DepthLimit - 1} {
		if _, err := eval.Compile(notChain(n)); err != nil {
			t.Errorf("Compile(depth %d): %v", n, err)
		}
	}
	for _, n := range []int{ast.DepthLimit, ast.DepthLimit + 1, 100000} {
		_, err := eval.Compile(notChain(n))
		if err == nil {
			t.Errorf("Compile(depth %d): accepted", n)
		} else if !strings.Contains(err.Error(), ast.DepthOverflowMessage) {
			t.Errorf("Compile(depth %d): refused for the wrong reason: %v", n, err)
		}
	}
}

// TestOneArgumentMathKeepsItsParenthesis: the game leaves the parenthesis
// of a one-argument math function in its tree, so the argument sits two
// levels down; every other function's arguments sit one level down. The
// set of one-argument functions is the arity table's.
func TestOneArgumentMathKeepsItsParenthesis(t *testing.T) {
	for name, arity := range eval.MathArity {
		args := strings.TrimSuffix(strings.Repeat("1, ", arity), ", ")
		src := "math." + name + "(" + args + ")"
		d, err := parser.Depth(src)
		if err != nil {
			t.Errorf("Depth(%q): %v", src, err)
			continue
		}
		want := 1
		if arity == 1 {
			want = 2
		}
		if d != want {
			t.Errorf("Depth(%q) = %d, want %d", src, d, want)
		}
	}
}
