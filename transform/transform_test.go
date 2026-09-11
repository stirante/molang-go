package transform_test

import (
	"math"
	"strconv"
	"testing"

	"github.com/stirante/molang-go/ast"
	"github.com/stirante/molang-go/eval"
	"github.com/stirante/molang-go/parser"
	"github.com/stirante/molang-go/printer"
	"github.com/stirante/molang-go/transform"
)

type zeroRNG struct{}

func (zeroRNG) NextFloat() float64     { return 0.5 }
func (zeroRNG) NextIntBound(b int) int { return 0 }

func runSrc(t *testing.T, src string) float64 {
	t.Helper()
	tree, err := parser.Parse(src)
	if err != nil {
		t.Fatalf("parse(%q): %v", src, err)
	}
	prog, err := eval.Compile(tree)
	if err != nil {
		t.Fatalf("compile(%q): %v", src, err)
	}
	return prog.Run(&eval.Context{RNG: zeroRNG{}, Scope: eval.NewScope()})
}

func TestFoldConstants(t *testing.T) {
	cases := []struct{ src, wantFolded string }{
		{"3 + 4", "7"},
		{"3 + 4 * 2", "11"},
		{"math.floor(3.7)", "3"},
		{"math.pi", strconv.FormatFloat(eval.Round32(math.Pi), 'f', -1, 64)},
		{"1 < 2", "1"},
		{"1 ? 2 : 3", "2"},
		{"t.x = 3 + 4", "t.x=7"},
	}
	for _, tc := range cases {
		tree, err := parser.Parse(tc.src)
		if err != nil {
			t.Fatalf("parse(%q): %v", tc.src, err)
		}
		folded := transform.FoldConstants(tree)
		got := printer.Minify(folded)
		if got != tc.wantFolded {
			t.Errorf("fold(%q) = %q, want %q", tc.src, got, tc.wantFolded)
		}
	}
}

func TestFoldPreservesRandomCalls(t *testing.T) {
	src := "math.random(0, 1)"
	tree, _ := parser.Parse(src)
	folded := transform.FoldConstants(tree)
	got := printer.Minify(folded)
	if got != "math.random(0,1)" {
		t.Errorf("random call should not be folded, got %q", got)
	}
}

func TestFoldDoesNotChangeEvalResult(t *testing.T) {
	cases := []string{
		"3 + 4 * (2 - 1)",
		"t.x = 5; t.x > 3 ? { t.x = t.x + math.floor(1.9); }; return t.x;",
		"math.clamp(math.floor(7.5), 0, 5)",
		"1 == 1 ? math.sin(30) : math.cos(60)",
	}
	for _, src := range cases {
		want := runSrc(t, src)
		tree, _ := parser.Parse(src)
		folded := transform.FoldConstants(tree)
		p, err := eval.Compile(folded)
		if err != nil {
			t.Fatalf("compile folded(%q): %v", src, err)
		}
		got := p.Run(&eval.Context{RNG: zeroRNG{}, Scope: eval.NewScope()})
		if got != want {
			t.Errorf("fold changed result for %q: want %v got %v", src, want, got)
		}
	}
}

func TestBitshiftMacroExpandsToCoreMolang(t *testing.T) {
	src := "math.bitshift(8, 2)"
	tree, err := parser.Parse(src)
	if err != nil {
		t.Fatal(err)
	}
	reg := transform.NewRegistryWithDefaults()
	expanded, err := reg.Expand(tree)
	if err != nil {
		t.Fatal(err)
	}
	// Should compile with the stock evaluator (no macro knowledge needed)
	// and produce the same result as a real right-shift: 8 >> 2 == 2.
	p, err := eval.Compile(expanded)
	if err != nil {
		t.Fatalf("compiled expansion should be core Molang: %v", err)
	}
	got := p.Run(&eval.Context{RNG: zeroRNG{}, Scope: eval.NewScope()})
	if got != 2 {
		t.Errorf("bitshift(8,2) = %v, want 2", got)
	}

	minified := printer.Minify(expanded)
	if minified == "math.bitshift(8,2)" {
		t.Errorf("expansion should not still contain the macro call")
	}
	reparsed, err := parser.Parse(minified)
	if err != nil {
		t.Fatalf("expanded+minified output should reparse as vanilla Molang: %v", err)
	}
	if _, err := eval.Compile(reparsed); err != nil {
		t.Fatalf("expanded+minified output should compile without the macro registry: %v", err)
	}
}

func TestCustomMacroRegistration(t *testing.T) {
	reg := transform.NewRegistry()
	reg.Register(transform.Macro{
		Name:  "double",
		Arity: 1,
		Expand: func(args []ast.Expr) ast.Expr {
			return &ast.BinaryExpr{Op: ast.Mul, X: args[0], Y: &ast.NumberLit{Value: 2}}
		},
	})
	tree, err := parser.Parse("math.double(21)")
	if err != nil {
		t.Fatal(err)
	}
	expanded, err := reg.Expand(tree)
	if err != nil {
		t.Fatal(err)
	}
	p, err := eval.Compile(expanded)
	if err != nil {
		t.Fatal(err)
	}
	if got := p.Run(&eval.Context{RNG: zeroRNG{}, Scope: eval.NewScope()}); got != 42 {
		t.Errorf("custom macro double(21) = %v, want 42", got)
	}
}
