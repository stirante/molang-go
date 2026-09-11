package transform_test

import (
	"math"
	"strconv"
	"testing"

	"github.com/stirante/molang-go/eval"
	"github.com/stirante/molang-go/parser"
	"github.com/stirante/molang-go/printer"
	"github.com/stirante/molang-go/transform"
)

// TestFoldMatchesEval is the net for one specific failure class: package
// transform re-implementing evaluation semantics that live in package eval,
// and the two silently drifting apart.
//
// That is not hypothetical. transform.foldBinary's ast.Div case was a bare
// Go `x / y` while eval.compileBinary's was the engine's guarded divide
// (|den| < FLT_EPSILON short-circuits to +0.0), so FoldConstants turned
// `1/0.0000001` into the literal `1e+07` -- and printer.Minify then emitted
// `10000000` as source that this very evaluator reads back as `0`. The
// folder's own doc comment claimed it mirrored the evaluator "exactly" the
// whole time, which is exactly why nothing caught it: the only check was a
// sentence.
//
// So this test checks the property that sentence asserts, mechanically, on
// every operator, over a value grid deliberately dense inside the divide
// guard's window:
//
//	eval(src) == eval(fold(src)) == eval(reparse(minify(fold(src))))
//
// The third leg matters as much as the second: a fold that computes the
// right value but prints as source meaning something else is the same bug
// wearing a hat.
//
// This proves internal agreement between two implementations in THIS
// module. It says nothing about whether either matches real Bedrock -- see
// golden_test.go's header comment in the root package for that distinction.

// foldGrid spans the ordinary cases plus the ones that historically broke:
// denominators inside, at, and just outside the divide guard's
// FLT_EPSILON window (1.1920928955078125e-7); float32 boundary magnitudes;
// signed zero; and values whose float32 rounding is lossy.
var foldGrid = []float64{
	0,
	-0.0,
	1,
	-1,
	2,
	-3,
	0.5,
	-0.5,
	3.7,
	1e-8,
	-1e-8,
	1e-7,
	-1e-7,
	1.1920928955078125e-7, // exactly FLT_EPSILON: guard must NOT fire
	1.1920928e-7,          // just under FLT_EPSILON: guard MUST fire
	1.2e-7,                // just over
	1e7,
	-1e7,
	1e30,
	-1e30,
	1e-38,
	16777217, // first integer float32 cannot represent exactly
	0.1,
}

var foldBinaryOps = []string{"+", "-", "*", "/", "<", "<=", ">", ">=", "==", "!=", "&&", "||", "??"}

func lit(v float64) string { return "(" + strconv.FormatFloat(v, 'g', -1, 64) + ")" }

// sameValue treats NaN as equal to NaN (Molang programs legitimately
// produce it) and otherwise demands exact equality, including the sign of
// zero -- a fold that turned -0 into +0 would be a real divergence.
func sameValue(a, b float64) bool {
	if math.IsNaN(a) && math.IsNaN(b) {
		return true
	}
	return a == b && math.Signbit(a) == math.Signbit(b)
}

// evalFoldTriple returns eval(src), eval(fold(src)) and
// eval(reparse(minify(fold(src)))). Each parse is independent because
// FoldConstants rewrites its argument in place.
func evalFoldTriple(t *testing.T, src string) (plain, folded, printed float64, ok bool) {
	t.Helper()

	run := func(s string) (float64, bool) {
		tree, err := parser.Parse(s)
		if err != nil {
			return 0, false
		}
		prog, err := eval.Compile(tree)
		if err != nil {
			return 0, false
		}
		return prog.Run(&eval.Context{RNG: zeroRNG{}, Scope: eval.NewScope()}), true
	}

	plain, ok = run(src)
	if !ok {
		return 0, 0, 0, false
	}

	tree, err := parser.Parse(src)
	if err != nil {
		return 0, 0, 0, false
	}
	foldedTree := transform.FoldConstants(tree)
	prog, err := eval.Compile(foldedTree)
	if err != nil {
		t.Fatalf("fold(%q) produced a tree eval.Compile rejects: %v", src, err)
	}
	folded = prog.Run(&eval.Context{RNG: zeroRNG{}, Scope: eval.NewScope()})

	minified := printer.Minify(foldedTree)
	printed, ok = run(minified)
	if !ok {
		t.Fatalf("fold+minify(%q) produced unparseable/uncompilable source %q", src, minified)
	}
	return plain, folded, printed, true
}

func checkFoldAgreement(t *testing.T, src string) {
	t.Helper()
	plain, folded, printed, ok := evalFoldTriple(t, src)
	if !ok {
		return // not a valid program on its own; nothing to compare
	}
	if !sameValue(plain, folded) {
		t.Errorf("fold changed the result of %q: unfolded=%v folded=%v", src, plain, folded)
	}
	if !sameValue(plain, printed) {
		t.Errorf("fold+minify changed the result of %q: unfolded=%v round-tripped=%v", src, plain, printed)
	}
}

func TestFoldMatchesEvalBinary(t *testing.T) {
	for _, op := range foldBinaryOps {
		for _, x := range foldGrid {
			for _, y := range foldGrid {
				checkFoldAgreement(t, lit(x)+op+lit(y))
			}
		}
	}
}

// TestFoldMatchesEvalUnaryAndTernary covers the other two constant-operand
// paths foldExpr implements independently of eval: unary -/! and the
// branch-selecting ternary fold (including the binary `cond ? then` form,
// whose falsy value is 0).
func TestFoldMatchesEvalUnaryAndTernary(t *testing.T) {
	for _, x := range foldGrid {
		checkFoldAgreement(t, "-"+lit(x))
		checkFoldAgreement(t, "!"+lit(x))
		for _, y := range foldGrid {
			checkFoldAgreement(t, lit(x)+"?"+lit(y)+":7")
			checkFoldAgreement(t, lit(x)+"?"+lit(y))
		}
	}
}

// TestFoldMatchesEvalMathCalls covers the third independent path: the
// folder calling eval.EvalMathPure where a compiled program would call the
// same math table entry through compileMathCall. Every non-RNG-drawing
// math.* function is exercised, so a new one added to the table without a
// thought for folding shows up here.
func TestFoldMatchesEvalMathCalls(t *testing.T) {
	// A small argument set per arity -- the point is coverage of every
	// function, not of every input.
	args := []float64{0, 1, -1, 0.5, -0.5, 2, 90, 1e-8, 1e7}

	for name, arity := range eval.MathArity {
		if eval.RandomFnNames[name] {
			continue // folding these is refused by design; nothing to agree on
		}
		for _, a := range args {
			for _, b := range args {
				var src string
				switch arity {
				case 1:
					src = "math." + name + "(" + lit(a) + ")"
				case 2:
					src = "math." + name + "(" + lit(a) + "," + lit(b) + ")"
				case 3:
					src = "math." + name + "(" + lit(a) + "," + lit(b) + ",0.25)"
				default:
					t.Fatalf("math.%s has unexpected arity %d -- extend this test", name, arity)
				}
				checkFoldAgreement(t, src)
			}
			if arity == 1 {
				break // second loop variable is unused for arity 1
			}
		}
	}
}

// TestFoldRejectsCallsEvalRejects pins the other half of "folding never
// changes a program's meaning": a call eval.Compile REFUSES must not be
// folded into something it accepts, and folding it must not panic. Both
// used to fail -- math.lerp(1) panicked with an index-out-of-range from
// inside FoldConstants, and math.pi(1,2) folded to a literal, quietly
// making an invalid program valid.
func TestFoldRejectsCallsEvalRejects(t *testing.T) {
	for _, src := range []string{
		"math.lerp(1)",
		"math.lerp(1,2)",
		"math.pi(1,2)",
		"math.floor(1,2,3)",
		"math.floor()",
		"math.not_a_function(1)",
	} {
		tree, err := parser.Parse(src)
		if err != nil {
			continue // parser already rejects it; fine
		}
		if _, err := eval.Compile(tree); err == nil {
			t.Fatalf("%q: expected eval.Compile to reject this; test premise is stale", src)
		}

		tree2, err := parser.Parse(src)
		if err != nil {
			t.Fatalf("parse(%q): %v", src, err)
		}
		func() {
			defer func() {
				if r := recover(); r != nil {
					t.Errorf("FoldConstants(%q) panicked: %v", src, r)
				}
			}()
			folded := transform.FoldConstants(tree2)
			if _, err := eval.Compile(folded); err == nil {
				t.Errorf("FoldConstants(%q) turned a program eval.Compile rejects into one it accepts", src)
			}
		}()
	}
}
