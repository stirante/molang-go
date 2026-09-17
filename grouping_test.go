package molang

import (
	"fmt"
	"strings"
	"testing"

	"github.com/stirante/molang-go/ast"
	"github.com/stirante/molang-go/parser"
	"github.com/stirante/molang-go/printer"
)

// How the game groups operators, pinned shape by shape.
//
// The game does not parse with a precedence table. It groups a token list
// in a fixed sequence of passes, one operator (or set of operators) per
// pass, each pass scanning left to right and folding every operator it owns
// with its two neighbours. So the operators of one pass are left-associative
// among themselves, an earlier pass binds tighter than a later one, and the
// order of passes is the whole story. Most of it reads as C would; the part
// that does not is that `/` has a pass of its own before `*`, so `a * b / c`
// is `a * (b / c)`, which is a different float32 number from `(a * b) / c`.
//
// Each case here is a source, the tree it must parse to, and the float32
// value that tree evaluates to, computed independently with Go's float32
// arithmetic in the same grouping. Where the grouping actually changes the
// number, the case says so and checks that the C reading disagrees, so the
// case is known to discriminate.

// shape writes a tree as an s-expression, which is the easiest thing to
// compare a grouping against by eye.
func shape(e ast.Expr) string {
	switch e := e.(type) {
	case *ast.NumberLit:
		return fmt.Sprint(e.Value)
	case *ast.BoolLit:
		return fmt.Sprint(e.Value)
	case *ast.Ident:
		return e.Namespace.ShortAlias() + "." + e.Member
	case *ast.UnaryExpr:
		if e.Op == ast.LNot {
			return "(! " + shape(e.X) + ")"
		}
		return "(- " + shape(e.X) + ")"
	case *ast.BinaryExpr:
		return "(" + binarySym(e.Op) + " " + shape(e.X) + " " + shape(e.Y) + ")"
	case *ast.CallExpr:
		parts := []string{e.Callee.Namespace.ShortAlias() + "." + e.Callee.Member}
		for _, a := range e.Args {
			parts = append(parts, shape(a))
		}
		return "(call " + strings.Join(parts, " ") + ")"
	case *ast.AssignExpr:
		return "(= " + shape(e.Target) + " " + shape(e.Value) + ")"
	case *ast.TernaryExpr:
		s := "(? " + shape(e.Cond) + " " + shape(e.Then)
		if e.Else != nil {
			s += " " + shape(e.Else)
		}
		return s + ")"
	}
	return fmt.Sprintf("%T", e)
}

func binarySym(op ast.BinaryOp) string {
	return [...]string{"+", "-", "*", "/", "<", "<=", ">", ">=", "==", "!=", "&&", "||", "??"}[op]
}

func exprShape(t *testing.T, src string) string {
	t.Helper()
	prog, err := parser.Parse(src)
	if err != nil {
		t.Fatalf("Parse(%q): %v", src, err)
	}
	var e ast.Expr
	switch s := prog.Stmts[0].(type) {
	case *ast.ExprStmt:
		e = s.X
	case *ast.ReturnStmt:
		e = s.Value
	default:
		t.Fatalf("Parse(%q): statement %T", src, s)
	}
	return shape(e)
}

func f32(v float64) float32 { return float32(v) }

func TestOperatorGroupingFollowsTheGame(t *testing.T) {
	cases := []struct {
		src   string
		shape string
		want  float32
		// cReading is the value the C grouping would give, when it is
		// different; the case checks it really is different.
		cReading float32
		differs  bool
	}{
		// `/` before `*`, then each to the left.
		{"7 * 3 / 9", "(* 7 (/ 3 9))", f32(7) * (f32(3) / f32(9)), (f32(7) * f32(3)) / f32(9), true},
		{"7 / 9 * 3", "(* (/ 7 9) 3)", (f32(7) / f32(9)) * f32(3), 0, false},
		{"1 / 3 / 7", "(/ (/ 1 3) 7)", (f32(1) / f32(3)) / f32(7), 0, false},
		{"1.1 * 1.7 * 1.3", "(* (* 1.1 1.7) 1.3)", (f32(1.1) * f32(1.7)) * f32(1.3), 0, false},
		{"2 * 3 / 4 * 5", "(* (* 2 (/ 3 4)) 5)", (f32(2) * (f32(3) / f32(4))) * f32(5), 0, false},
		{"8 / 4 / 2", "(/ (/ 8 4) 2)", 1, 0, false},
		{"5 * 7 / 9 * 3", "(* (* 5 (/ 7 9)) 3)", (f32(5) * (f32(7) / f32(9))) * f32(3), 0, false},
		// `-` is `+` of a negation, and `+` groups to the left, so these
		// read as C reads them.
		{"0.1 - 0.7 + 0.3", "(+ (- 0.1 0.7) 0.3)", (f32(0.1) - f32(0.7)) + f32(0.3), 0, false},
		{"0.1 + 0.7 - 0.3", "(- (+ 0.1 0.7) 0.3)", (f32(0.1) + f32(0.7)) - f32(0.3), 0, false},
		{"0.1 - 0.7 - 0.3", "(- (- 0.1 0.7) 0.3)", (f32(0.1) - f32(0.7)) - f32(0.3), 0, false},
		{"1 - 2 * 3", "(- 1 (* 2 3))", -5, 0, false},
		// Two `-` in a row after an operand are a `+`.
		{"2 - -3", "(+ 2 3)", 5, 0, false},
		{"2 - -3 * 4", "(+ 2 (* 3 4))", 14, 0, false},
		{"2 - (-3)", "(- 2 (- 3))", 5, 0, false},
		// Unary `-` and `!` bind before `/` and `*`, and after `->`.
		{"-2 * 3", "(* (- 2) 3)", -6, 0, false},
		{"-3 / 4", "(/ (- 3) 4)", -0.75, 0, false},
		{"6 / -4", "(/ 6 (- 4))", -1.5, 0, false},
		{"-2 * -3", "(* (- 2) (- 3))", 6, 0, false},
		{"2 * -3 / 4", "(* 2 (/ (- 3) 4))", -1.5, 0, false},
		{"!0 * 5", "(* (! 0) 5)", 5, 0, false},
		{"!0 == 1", "(== (! 0) 1)", 1, 0, false},
		{"!-1", "(! (- 1))", 0, 0, false},
		{"-!1", "(- (! 1))", 0, 0, false},
		// The four relational operators share a pass; `==` and `!=` share
		// the next; `&&` comes before `||`.
		{"3 < 2 < 1", "(< (< 3 2) 1)", 1, 0, false},
		{"1 < 2 == 3 < 4", "(== (< 1 2) (< 3 4))", 1, 0, false},
		{"2 == 2 != 0", "(!= (== 2 2) 0)", 1, 0, false},
		{"0 && 1 || 1 && 1", "(|| (&& 0 1) (&& 1 1))", 1, 0, false},
		{"1 || 0 && 0", "(|| 1 (&& 0 0))", 1, 0, false},
		// What the nodes evaluate to: comparisons and logic are 1 or 0,
		// `==` is exact, a division by zero is 0 wherever it stands.
		{"0.3 + 0.6 == 0.9", "(== (+ 0.3 0.6) 0.9)", 0, 0, false},
		{"0.1 + 0.2 == 0.3", "(== (+ 0.1 0.2) 0.3)", 1, 0, false},
		{"1 / 0", "(/ 1 0)", 0, 0, false},
		{"1 / 0 * 5", "(* (/ 1 0) 5)", 0, 0, false},
		{"5 * 1 / 0", "(* 5 (/ 1 0))", 0, 0, false},
		{"5 / 0 / 2", "(/ (/ 5 0) 2)", 0, 0, false},
		{"3 && 0.5", "(&& 3 0.5)", 1, 0, false},
		{"math.sqrt(-1) < 1", "(< (call math.sqrt (- 1)) 1)", 0, 0, false},
		{"math.sqrt(-1) == math.sqrt(-1)", "(== (call math.sqrt (- 1)) (call math.sqrt (- 1)))", 0, 0, false},
	}
	for _, c := range cases {
		if got := exprShape(t, c.src); got != c.shape {
			t.Errorf("%q parses as %s, want %s", c.src, got, c.shape)
		}
		if got := evalOK(t, c.src); got != float64(c.want) {
			t.Errorf("%q = %v, want %v", c.src, got, c.want)
		}
		if c.differs && c.cReading == c.want {
			t.Errorf("%q: the C grouping gives the same %v, so the case proves nothing", c.src, c.want)
		}
	}
}

// TestOperatorGroupingSurvivesPrinting: the printers write a tree so that
// it reads back as the same tree under the game's grouping.
func TestOperatorGroupingSurvivesPrinting(t *testing.T) {
	srcs := []string{
		"7 * 3 / 9", "(7 * 3) / 9", "7 / (9 * 3)", "7 / 9 / 3", "7 / (9 / 3)", "2 * 3 / 4 * 5",
		"q.a * q.b / q.c * q.d", "(q.a * q.b) / (q.c * q.d)",
		"2 - -3", "2 - (-3)", "-(-3)", "2 - (-3) * 4", "-(-q.a) / 2",
		"3 < 2 < 1", "3 < (2 < 1)", "2 == 2 != 0", "2 == (2 != 0)",
		"0 && 1 || 1 && 1", "0 && (1 || 1) && 1", "1 || 0 && 0", "(1 || 0) && 0",
		"v.b = (v.a = 1);", "q.f(v.a = 1, 2);",
	}
	for _, src := range srcs {
		prog, err := parser.Parse(src)
		if err != nil {
			t.Fatalf("Parse(%q): %v", src, err)
		}
		want := exprShape(t, src)
		for name, out := range map[string]string{"Format": printer.Format(prog), "Minify": printer.Minify(prog)} {
			if got := exprShape(t, out); got != want {
				t.Errorf("%s(%q) = %q, which parses as %s, want %s", name, src, out, got, want)
			}
		}
	}
}

// TestGroupingRefusals: the shapes the game's grouping cannot make sense of.
func TestGroupingRefusals(t *testing.T) {
	refused := []string{
		// Two `-` in a row are a `+`; at the start of an expression or
		// after an operator it has nothing on its left.
		"--3",
		"- -3",
		"2 * - -3",
		"!--3",
		// A third `-` is negated onto the `+` the first two became.
		"2 - - -3",
		// `=` groups to the left: `v.b = v.a = 1` assigns to an
		// assignment.
		"v.b = v.a = 1;",
		"return v.b = v.a = 1;",
		"q.f(v.a = v.b = 1);",
	}
	for _, src := range refused {
		if _, err := parser.Parse(src); err == nil {
			t.Errorf("Parse(%q): accepted, want a refusal", src)
		}
	}
	accepted := []string{"2 - -3", "-(-3)", "2 - (-3)", "v.b = (v.a = 1);", "q.f(v.a = (v.b = 1));"}
	for _, src := range accepted {
		if _, err := parser.Parse(src); err != nil {
			t.Errorf("Parse(%q): %v", src, err)
		}
	}
}
