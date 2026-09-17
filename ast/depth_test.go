package ast_test

import (
	"strings"
	"testing"

	"github.com/stirante/molang-go/ast"
	"github.com/stirante/molang-go/parser"
	"github.com/stirante/molang-go/printer"
)

// TestDepthWithoutGroupingMatchesPrintedForm: a tree measured without a
// Grouping is measured as the printer writes it. So for any tree, the depth
// the parser measures for its Minify output must be exactly what Depth
// says of the tree with no Grouping, and no more than what the parser
// measured for the original source (printing only ever drops parentheses).
func TestDepthWithoutGroupingMatchesPrintedForm(t *testing.T) {
	srcs := []string{
		"1",
		"(1)",
		"((1 + 2)) * 3",
		"1 - 2",
		"1 - -2",
		"1 - (-2)",
		"-(1 + 2)",
		"q.a * q.b / q.c",
		"q.a * (q.b / q.c)",
		"(q.a * q.b) / q.c",
		"q.a * q.b / q.c * q.d",
		"(q.a * q.b) / (q.c * q.d)",
		"q.a - (-q.b * 2)",
		"q.a - (-q.b) * 2",
		"-(-q.a)",
		"-(-q.a * 2)",
		"q.a - -q.b",
		"q.a - (q.b - q.c)",
		"q.a - q.b - q.c",
		"q.a < q.b == q.c",
		"(q.a == q.b) < q.c",
		"q.a && (q.b || q.c)",
		"(q.a && q.b) || q.c",
		"v.a ?? (v.b ?? 1)",
		"math.abs((1))",
		"math.max((1), 2)",
		"q.f(v.a = 1);",
		"q.f((v.a = 1), 2);",
		"q.f((1 + 2), (3))",
		"array.a[(1)]",
		"(c.o)->v.x",
		"(q.a + 1)->v.x",
		"-c.o->v.x",
		"q.c ? 1 : 2",
		"q.c ? (q.d ? 1) : 3",
		"q.c ? q.d ? 1 : 2 : 3",
		"(q.c ? 1 : 2) ? 3 : 4",
		"q.c ? (v.a = 1) : 2;",
		"v.a = (1);",
		"v.a = v.b ?? 1;",
		"return (1);",
		"{ v.a = 1; };",
		"q.c ? { v.a = 1; };",
		"q.c ? { v.a = 1; } : { v.b = 2; };",
		"q.c ? { v.a = 1; } : q.d ? { v.b = 2; } : { v.e = 3; };",
		"q.c ? { v.a = 1; } : 2;",
		"q.c ? 2 : { v.a = 1; };",
		"loop((2), { v.a = (1); });",
		"for_each(t.e, (q.f), { v.a = 1; });",
		"true ? { v.a = 1; };",
		"1; 2; (3);",
	}
	for _, src := range srcs {
		prog, err := parser.Parse(src)
		if err != nil {
			t.Errorf("Parse(%q): %v", src, err)
			continue
		}
		asWritten, _ := parser.Depth(src)
		structural := ast.Depth(prog, nil)
		for name, out := range map[string]string{"Minify": printer.Minify(prog), "Format": printer.Format(prog)} {
			printed, err := parser.Depth(out)
			if err != nil {
				t.Errorf("%s(%q) = %q does not parse: %v", name, src, out, err)
				continue
			}
			if printed != structural {
				t.Errorf("%s(%q) = %q: parser measures %d, Depth without a Grouping says %d", name, src, out, printed, structural)
			}
			if printed > asWritten {
				t.Errorf("%s(%q) = %q: depth %d, deeper than the source's %d", name, src, out, printed, asWritten)
			}
		}
	}
}

// TestDepthHandBuiltTrees: shapes a transform can produce that the parser
// never would, measured as they would be written.
func TestDepthHandBuiltTrees(t *testing.T) {
	num := func(v float64) ast.Expr { return &ast.NumberLit{Value: v} }
	bin := func(op ast.BinaryOp, x, y ast.Expr) ast.Expr { return &ast.BinaryExpr{Op: op, X: x, Y: y} }
	prog := func(e ast.Expr) *ast.Program { return &ast.Program{Stmts: []ast.Stmt{&ast.ExprStmt{X: e}}} }

	cases := []struct {
		name string
		prog *ast.Program
		want int
	}{
		// A right-nested run is written with parentheses on the right.
		{"right-nested add", prog(bin(ast.Add, num(1), bin(ast.Add, num(2), num(3)))), 3},
		{"left-nested add", prog(bin(ast.Add, bin(ast.Add, num(1), num(2)), num(3))), 2},
		// Written `1 - (-2)`, since `1 - -2` would read back as `1 + 2`:
		// the `+`, its negation, the parenthesis, the inner negation.
		{"double negative", prog(bin(ast.Sub, num(1), &ast.UnaryExpr{Op: ast.Neg, X: num(2)})), 4},
		// A negative literal is written `-2`, a negation of 2, and is kept
		// in parentheses after a `-` for the same reason.
		{"negative literal", prog(bin(ast.Mul, num(-2), num(3))), 2},
		{"subtracted negative literal", prog(bin(ast.Sub, num(1), num(-2))), 4},
		{"negated negative literal", prog(&ast.UnaryExpr{Op: ast.Neg, X: num(-2)}), 3},
		// `/` binds tighter than `*`, so a division on the right of a `*`
		// needs no parentheses and a product on the left of a `/` does.
		{"product of a quotient", prog(bin(ast.Mul, num(1), bin(ast.Div, num(2), num(3)))), 2},
		{"quotient of a product", prog(bin(ast.Div, bin(ast.Mul, num(1), num(2)), num(3))), 3},
		// A lone assignment with no `;` is written `return v.a = 1;`.
		{"bare assignment", &ast.Program{Stmts: []ast.Stmt{&ast.ExprStmt{X: &ast.AssignExpr{
			Target: &ast.Ident{Namespace: ast.Variable, Member: "a"}, Value: num(1)}}}}, 3},
		// A block whose condition is the literal true is written bare.
		{"plain block", &ast.Program{HasSemicolon: true, Stmts: []ast.Stmt{&ast.CondBlockStmt{
			Cond: &ast.BoolLit{Value: true},
			Body: &ast.Block{Stmts: []ast.Stmt{&ast.ExprStmt{X: num(1)}}}}}}, 3},
		{"conditional block", &ast.Program{HasSemicolon: true, Stmts: []ast.Stmt{&ast.CondBlockStmt{
			Cond: &ast.Ident{Namespace: ast.Query, Member: "c"},
			Body: &ast.Block{Stmts: []ast.Stmt{&ast.ExprStmt{X: num(1)}}}}}}, 4},
	}
	for _, c := range cases {
		if got := ast.Depth(c.prog, nil); got != c.want {
			t.Errorf("%s: Depth = %d, want %d (written %q)", c.name, got, c.want, printer.Minify(c.prog))
		}
	}
}

// TestDepthDoesNotRecurse: a tree nested far past the limit is measured
// without deep recursion, so Compile can refuse it before walking it.
func TestDepthDoesNotRecurse(t *testing.T) {
	var e ast.Expr = &ast.NumberLit{Value: 1}
	for i := 0; i < 1_000_000; i++ {
		e = &ast.UnaryExpr{Op: ast.LNot, X: e}
	}
	prog := &ast.Program{Stmts: []ast.Stmt{&ast.ExprStmt{X: e}}}
	if got := ast.Depth(prog, nil); got != 1_000_000 {
		t.Errorf("Depth = %d", got)
	}
	e = &ast.NumberLit{Value: 1}
	for i := 0; i < 1_000_000; i++ {
		e = &ast.BinaryExpr{Op: ast.Add, X: e, Y: &ast.NumberLit{Value: 1}}
	}
	prog = &ast.Program{Stmts: []ast.Stmt{&ast.ExprStmt{X: e}}}
	if got := ast.Depth(prog, nil); got != 1_000_000 {
		t.Errorf("Depth = %d", got)
	}
}

// TestDepthMessageIsTheGames pins the wording.
func TestDepthMessageIsTheGames(t *testing.T) {
	if !strings.HasPrefix(ast.DepthOverflowMessage, "Expression could not be parsed due to stack depth overflow") {
		t.Errorf("message changed: %q", ast.DepthOverflowMessage)
	}
	if ast.DepthLimit != 256 {
		t.Errorf("limit changed: %d", ast.DepthLimit)
	}
}
