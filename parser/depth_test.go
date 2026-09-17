package parser

import (
	"strings"
	"testing"
	"time"

	"github.com/stirante/molang-go/ast"
)

// TestDepthGrouping pins the depth the game measures for each shape, which
// is not the depth of this package's tree: parentheses and blocks are
// levels, `/` groups before `*`, `-` is `+` of a negation, a one-argument
// math function keeps its parenthesis while every other call flattens its
// arguments, and a conditional's arms hang directly off the `?`.
func TestDepthGrouping(t *testing.T) {
	cases := []struct {
		src  string
		want int
	}{
		{"1", 0},
		{"q.a", 0},
		{"1 + 2", 1},
		{"1 + 2 + 3", 2},
		{"1 + 2 + 3 + 4", 3},
		{"(1)", 1},
		{"((1))", 2},
		{"(1) + 2", 2},
		{"1 + (2)", 2},
		{"(1 + 2) + 3", 3},
		{"1 + (2 + 3)", 3},
		// `-` is `+` of a negation: one more level for the subtracted
		// operand. `a - -b` collapses to `a + b`.
		{"1 - 2", 2},
		{"1 - -2", 1},
		{"1 - (-2)", 4},
		{"-1", 1},
		{"!q.a", 1},
		{"!!q.a", 2},
		// `/` groups before `*`, then each groups to the left.
		{"q.a * q.b", 1},
		{"q.a * q.b / q.c", 2},
		{"q.a * q.b * q.c / q.d", 2},
		{"q.a / q.b / q.c", 2},
		{"q.a / q.b * q.c", 2},
		{"q.a * q.b * q.c", 2},
		{"q.a / q.b * q.c / q.d", 2},
		{"q.a * q.b * q.c * q.d", 3},
		// Comparisons: the four relational operators in one pass, then
		// `==`/`!=`; `&&` before `||`; each pass to the left.
		{"q.a < q.b", 1},
		{"q.a < q.b > q.c", 2},
		{"q.a == q.b < q.c", 2},
		{"q.a < q.b == q.c", 2},
		{"q.a && q.b || q.c", 2},
		{"q.a || q.b && q.c", 2},
		{"q.a && q.b && q.c", 2},
		{"v.a ?? 1", 1},
		// Calls.
		{"math.abs(1)", 2},
		{"math.sin(q.a + 1)", 3},
		{"math.max(1, 2)", 1},
		{"math.lerp(1, 2, 3)", 1},
		{"math.pow(1 + 2, 3)", 2},
		{"q.f", 0},
		{"q.f(1)", 1},
		{"q.f(1, 2, 3)", 1},
		{"q.f((1))", 2},
		{"q.f(1 + 2)", 2},
		{"q.f(v.a = 1);", 3},
		{"q.f((v.a = 1), 2);", 4},
		{"array.a[1]", 1},
		{"array.a[q.i + 1]", 2},
		{"c.o->v.x", 1},
		{"c.o->q.f(1)", 2},
		{"(c.o)->v.x", 2},
		// Conditionals: arms directly under the `?`.
		{"q.c ? 1 : 2", 1},
		{"q.c ? 1", 1},
		{"q.c ? q.d ? 1 : 2 : 3", 2},
		{"q.c ? 1 : q.d ? 2 : 3", 2},
		{"q.c ? (q.d ? 1) : 3", 3},
		{"q.a + 1 ? 2 : 3", 2},
		// Statements: a `;` list is a node; a block is its braces and its
		// own `;` list.
		{"1;", 1},
		{"v.a = 1;", 2},
		{"v.a = 1 + 2;", 3},
		{"return 1;", 2},
		{"return;", 1},
		{"1; 2; 3;", 1},
		{"{ v.a = 1; };", 4},
		{"q.c ? { v.a = 1; };", 5},
		{"q.c ? { v.a = 1; } : { v.b = 2; };", 5},
		{"q.c ? { v.a = 1; } : q.d ? { v.b = 2; } : { v.e = 3; };", 6},
		{"q.c ? { v.a = 1; } : 2;", 5},
		{"q.c ? 2 : { v.a = 1; };", 5},
		{"loop(2, { v.a = 1; });", 5},
		{"loop(2 + 1, { break; });", 4},
		{"for_each(t.e, q.f, { v.a = 1; });", 5},
		// `true ? {...}` is written with a conditional, so it is measured
		// with one; the bare block it would print as is one level less.
		{"true ? { v.a = 1; };", 5},
		{"(q.c ? { v.a = 1; });", 6},
	}
	for _, c := range cases {
		got, err := Depth(c.src)
		if err != nil {
			t.Errorf("Depth(%q): %v", c.src, err)
			continue
		}
		if got != c.want {
			t.Errorf("Depth(%q) = %d, want %d", c.src, got, c.want)
		}
	}
}

// deepSource builds a source whose game-tree depth is a known function of
// n, for each of the shapes that nest.
var deepShapes = []struct {
	name  string
	build func(n int) string
	depth func(n int) int
}{
	{"parens", func(n int) string { return strings.Repeat("(", n) + "1" + strings.Repeat(")", n) }, func(n int) int { return n }},
	{"not", func(n int) string { return strings.Repeat("!", n) + "1" }, func(n int) int { return n }},
	{"sum", func(n int) string { return strings.Repeat("1 + ", n) + "1" }, func(n int) int { return n }},
	{"product", func(n int) string { return strings.Repeat("q.a * ", n) + "q.a" }, func(n int) int { return n }},
	{"query", func(n int) string { return strings.Repeat("q.f(", n) + "1" + strings.Repeat(")", n) }, func(n int) int { return n }},
	{"abs", func(n int) string { return strings.Repeat("math.abs(", n) + "1" + strings.Repeat(")", n) }, func(n int) int { return 2 * n }},
	{"ternary", func(n int) string { return strings.Repeat("q.c ? ", n) + "1" }, func(n int) int { return n }},
	// `=` groups to the left, so a chain of assignments has to be written
	// with parentheses: each is a level of its own.
	{"assign", func(n int) string {
		return strings.Repeat("v.a = (", n) + "1" + strings.Repeat(")", n) + ";"
	}, func(n int) int { return 2*n + 1 }},
	{"block", func(n int) string {
		return strings.Repeat("q.c ? { ", n) + "v.a = 1;" + strings.Repeat(" };", n)
	}, func(n int) int { return 3*n + 2 }},
	{"index", func(n int) string { return strings.Repeat("array.a[", n) + "1" + strings.Repeat("]", n) }, func(n int) int { return n }},
}

// TestDepthLimit checks each nesting shape one short of the limit, at it,
// and past it: the game refuses a tree with a node at depth DepthLimit.
func TestDepthLimit(t *testing.T) {
	for _, sh := range deepShapes {
		// The largest n that stays under the limit, and the smallest that
		// does not.
		nOK, nBad := 0, 0
		for n := 1; ; n++ {
			if sh.depth(n) >= ast.DepthLimit {
				nBad = n
				break
			}
			nOK = n
		}
		for _, n := range []int{nOK - 1, nOK, nBad, nBad + 1} {
			src := sh.build(n)
			d, err := Depth(src)
			if sh.depth(n) < ast.DepthLimit {
				if err != nil {
					t.Errorf("%s n=%d (depth %d): refused: %v", sh.name, n, sh.depth(n), err)
				} else if d != sh.depth(n) {
					t.Errorf("%s n=%d: Depth = %d, want %d", sh.name, n, d, sh.depth(n))
				}
				continue
			}
			if err == nil {
				t.Errorf("%s n=%d (depth %d): accepted, want refusal", sh.name, n, sh.depth(n))
			} else if !strings.Contains(err.Error(), ast.DepthOverflowMessage) {
				t.Errorf("%s n=%d: refused for the wrong reason: %v", sh.name, n, err)
			}
		}
	}
}

// TestDepthLimitIsCheap makes sure a source nested absurdly deep is refused
// without the parser recursing or building to match: the guard fires at the
// limit, and a long flat chain is cut off at a fixed length.
func TestDepthLimitIsCheap(t *testing.T) {
	const n = 200000
	for _, sh := range deepShapes {
		src := sh.build(n)
		start := time.Now()
		_, err := Parse(src)
		if err == nil {
			t.Errorf("%s n=%d: accepted", sh.name, n)
		} else if !strings.Contains(err.Error(), ast.DepthOverflowMessage) {
			t.Errorf("%s n=%d: refused for the wrong reason: %v", sh.name, n, err)
		}
		if took := time.Since(start); took > 2*time.Second {
			t.Errorf("%s n=%d took %v", sh.name, n, took)
		}
	}
}

// TestDepthErrorPosition: the game gives no position for this refusal, so
// the error points at the start of the source.
func TestDepthErrorPosition(t *testing.T) {
	src := strings.Repeat("(", ast.DepthLimit) + "1" + strings.Repeat(")", ast.DepthLimit)
	_, err := Parse(src)
	pe, ok := err.(*Error)
	if !ok {
		t.Fatalf("got %T %v, want *Error", err, err)
	}
	if pe.Msg != ast.DepthOverflowMessage {
		t.Errorf("Msg = %q", pe.Msg)
	}
}
