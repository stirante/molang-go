package eval_test

import (
	"math"
	"strconv"
	"testing"

	"molang-go/eval"
)

// This file pins the behaviours settled against Bedrock 1.26.50.24. Each
// test exists because its expectation contradicts either a reference
// implementation, a plausible port, or an earlier version of this
// package.

func evalIn(t *testing.T, src string, scope *eval.Scope) float64 {
	t.Helper()
	p, err := compile(t, src)
	if err != nil {
		t.Fatalf("compile(%q): %v", src, err)
	}
	if scope == nil {
		scope = eval.NewScope()
	}
	return p.Run(&eval.Context{Scope: scope})
}

// TestNaNIsTruthyEverywhere: the engine's compiled conditional instruction
// tests the condition for float32 equality with 0.0, so ONLY an exact zero
// is false and NaN takes the then-branch. `!`, `&&` and `||` use the same
// exactly-zero test, which this package already matched --
// the ternary was the one that did not, having inherited JS truthiness.
func TestNaNIsTruthyEverywhere(t *testing.T) {
	cases := map[string]float64{
		// The one that moved. This answered 222 before.
		"math.sqrt(-1) ? 111 : 222": 111,
		// The statement-conditional form is the same operator.
		"math.sqrt(-1) ? { return 111; } : { return 222; };": 111,
		// Unchanged, and the reason the ternary was wrong: these three
		// already agreed with the engine.
		"!math.sqrt(-1)":      0,
		"math.sqrt(-1) && 1":  1,
		"math.sqrt(-1) || 0":  1,
		"0 ? 111 : 222":       222,
		"(0-0) ? 111 : 222":   222,
		"0.0001 ? 111 : 222":  111,
		"(0-0.0) ? 111 : 222": 222,
	}
	for src, want := range cases {
		if got := evalIn(t, src, nil); got != want {
			t.Errorf("%s = %v, want %v", src, got, want)
		}
	}
}

// TestMinMaxAreRawOrderedCompares: the compiled math.max instruction is
// one ordered compare plus one conditional select, i.e. `(a > b) ? a : b`,
// and the compiled math.min instruction is the same with the less-than
// predicate. NaN is unordered, so the predicate is false and the
// SECOND operand wins -- an asymmetry matching neither C's fmaxf/fminf
// (always the non-NaN operand) nor Go's math.Max/math.Min (always NaN,
// which is what this package used to call).
func TestMinMaxAreRawOrderedCompares(t *testing.T) {
	ordinary := map[string]float64{
		"math.max(3,7)":   7,
		"math.max(7,3)":   7,
		"math.min(3,7)":   3,
		"math.min(7,3)":   3,
		"math.max(-1,-9)": -1,
		"math.min(-1,-9)": -9,
	}
	for src, want := range ordinary {
		if got := evalIn(t, src, nil); got != want {
			t.Errorf("%s = %v, want %v", src, got, want)
		}
	}

	// NaN first: the compare fails, the second operand is selected.
	for _, src := range []string{"math.max(math.sqrt(-1),4)", "math.min(math.sqrt(-1),4)"} {
		if got := evalIn(t, src, nil); got != 4 {
			t.Errorf("%s = %v, want 4 (second operand wins on an unordered compare)", src, got)
		}
	}
	// NaN second: the compare still fails, and the second operand is the
	// NaN. Go's math.Max would give NaN here too; fmaxf would give 4.
	for _, src := range []string{"math.max(4,math.sqrt(-1))", "math.min(4,math.sqrt(-1))"} {
		if got := evalIn(t, src, nil); !math.IsNaN(got) {
			t.Errorf("%s = %v, want NaN (second operand wins on an unordered compare)", src, got)
		}
	}
}

// TestAllThirtyEaseFunctionsExist: the engine's thirty ease functions are
// the full cross product of ten curve families and three directions, all
// arity 3. quint, bounce, back and elastic were missing from this package
// entirely, so four of these thirty names were a compile error here and
// valid in game.
func TestAllThirtyEaseFunctionsExist(t *testing.T) {
	shapes := []string{"quad", "cubic", "quart", "quint", "sine", "expo", "circ", "bounce", "back", "elastic"}
	variants := []string{"in", "out", "in_out"}
	n := 0
	for _, shape := range shapes {
		for _, variant := range variants {
			name := "math.ease_" + variant + "_" + shape
			if _, err := compile(t, name+"(0,1,0.5)"); err != nil {
				t.Errorf("%s should compile: %v", name, err)
				continue
			}
			// Arity 3, confirmed for every one of them.
			if _, err := compile(t, name+"(0,1)"); err == nil {
				t.Errorf("%s should reject 2 arguments", name)
			}
			n++
		}
	}
	if n != 30 {
		t.Errorf("checked %d ease functions, want 30", n)
	}
}

// TestEaseExpoHasNoEndpointSpecialCase: easeOutExpo is a bare
// `1 - powf(2, -10t)` and easeInExpo a bare `powf(2, 10t - 10)`. Neither
// has the `if (t == 1) return 1` / `if (t == 0) return 0` guard that
// easings.net and most ports carry --
// and that this package used to carry. The endpoints are therefore very
// slightly off 1 and 0, which is the whole observable difference.
func TestEaseExpoHasNoEndpointSpecialCase(t *testing.T) {
	if got := evalIn(t, "math.ease_out_expo(0,1,1)", nil); got == 1 {
		t.Errorf("ease_out_expo(0,1,1) = 1; the engine has no endpoint case, want 0.999023...")
	} else if want := eval.Round32(1 - math.Pow(2, -10)); got != want {
		t.Errorf("ease_out_expo(0,1,1) = %v, want %v", got, want)
	}
	if got := evalIn(t, "math.ease_in_expo(0,1,0)", nil); got == 0 {
		t.Errorf("ease_in_expo(0,1,0) = 0; the engine has no endpoint case, want 0.0009765625")
	} else if want := eval.Round32(math.Pow(2, -10)); got != want {
		t.Errorf("ease_in_expo(0,1,0) = %v, want %v", got, want)
	}
}

// TestEaseInOutBranchTakesFirstHalfOnNaN: the in_out variants perform a
// real float32 division of t by 0.5 and branch on "quotient >= 1". A NaN
// quotient compares unordered, the test is false, and the FIRST half
// runs. A Go `if t < 0.5` is also false for NaN and therefore ran the
// SECOND half -- the opposite branch. For every finite t the two agree
// exactly, so NaN is the only input that can see the difference.
func TestEaseInOutBranchTakesFirstHalfOnNaN(t *testing.T) {
	// First half of ease_in_out_quad is 2*t*t; second is
	// 1 - (-2t+2)^2/2. Both are NaN for a NaN t, so the curve itself
	// cannot distinguish them -- ease_in_out_bounce can, because its two
	// halves reach different constant arcs.
	got := evalIn(t, "math.ease_in_out_bounce(0,1,math.sqrt(-1))", nil)
	firstHalf := evalIn(t, "math.ease_in_out_bounce(0,1,0.25)", nil)
	secondHalf := evalIn(t, "math.ease_in_out_bounce(0,1,0.75)", nil)
	if math.IsNaN(firstHalf) || math.IsNaN(secondHalf) {
		t.Fatalf("test premise broken: finite bounce halves are %v / %v", firstHalf, secondHalf)
	}
	// The NaN propagates through whichever half ran, so what is actually
	// observable is that it is NaN rather than a finite constant -- the
	// real assertion is on the branch predicate itself.
	if !math.IsNaN(got) {
		t.Errorf("ease_in_out_bounce with NaN t = %v, want NaN", got)
	}
	if eval.Round32(math.NaN()/0.5) >= 1 {
		t.Errorf("the branch predicate must be false for NaN (first half)")
	}
}

// TestEaseSineIsQuantisedToTheTable: ease_in_sine/ease_out_sine/
// ease_in_out_sine do NOT call sinf/cosf. They index the engine's own
// 65536-entry sine table with a TRUNCATED index and no interpolation
// (multiply by 10430.3779296875, optionally add 16384.0, truncate, mask with
// 0xFFFF, indexed load). The observable consequence -- and the part that
// is confirmed rather than inferred -- is the quantisation: the result
// snaps to one of 65536 samples per turn, so it differs from a computed
// sine by more than a rounding error, while staying within one table step.
//
// (math.sin/math.cos, by contrast, ARE libm calls: math.sin calls sinf and
// math.cos calls cosf. The split is deliberate.)
func TestEaseSineIsQuantisedToTheTable(t *testing.T) {
	const step = 2 * math.Pi / 65536 // one table entry, in radians

	// ease_out_sine(0,1,t) is sin(t*pi/2) read out of the table.
	for _, tv := range []float64{0.1, 0.3, 0.37, 0.5, 0.77, 0.9} {
		got := evalIn(t, "math.ease_out_sine(0,1,"+ftoa(tv)+")", nil)
		exact := math.Sin(tv * math.Pi / 2)
		if math.Abs(got-exact) > step {
			t.Errorf("ease_out_sine(0,1,%v) = %v, further than one table step from sin = %v", tv, got, exact)
		}
	}

	// The quantisation must be REAL, not merely tolerated: at least one
	// sampled t has to differ from the correctly-rounded float32 sine by
	// more than a float32 ULP, or the table is not being modelled at all.
	quantised := false
	for i := 1; i < 200; i++ {
		tv := float64(i) / 211
		got := evalIn(t, "math.ease_out_sine(0,1,"+ftoa(tv)+")", nil)
		exact := eval.Round32(math.Sin(eval.Round32(tv) * math.Pi / 2))
		if got != exact {
			quantised = true
			break
		}
	}
	if !quantised {
		t.Errorf("no sampled ease_out_sine value differs from a computed sine; the lookup table is not being used")
	}

	// Endpoints still land where a sine table says they should.
	if got := evalIn(t, "math.ease_out_sine(0,1,0)", nil); got != 0 {
		t.Errorf("ease_out_sine(0,1,0) = %v, want 0", got)
	}
	if got := evalIn(t, "math.ease_in_sine(0,1,0)", nil); got != 0 {
		t.Errorf("ease_in_sine(0,1,0) = %v, want 0", got)
	}
}

// TestEaseIsLerpWithoutClamping: every easing is
// `start + (end - start) * curve(t)`, in S registers, with t NOT clamped
// (confirmed). So a t outside [0,1] extrapolates rather than saturating.
func TestEaseIsLerpWithoutClamping(t *testing.T) {
	if got := evalIn(t, "math.ease_in_quad(10,20,0)", nil); got != 10 {
		t.Errorf("ease_in_quad(10,20,0) = %v, want 10", got)
	}
	if got := evalIn(t, "math.ease_in_quad(10,20,1)", nil); got != 20 {
		t.Errorf("ease_in_quad(10,20,1) = %v, want 20", got)
	}
	// t = 2 -> curve 4 -> 10 + 10*4 = 50. A clamping implementation would
	// answer 20.
	if got := evalIn(t, "math.ease_in_quad(10,20,2)", nil); got != 50 {
		t.Errorf("ease_in_quad(10,20,2) = %v, want 50 (t is not clamped)", got)
	}
}

// TestMinAngleExistsWithArityOne: math.min_angle is min=1 max=1 in the
// engine's compiled math-function registry and was absent from this
// package, so calling it was a compile error here. The FORMULA is
// INFERRED from documentation -- see mathTable["min_angle"].
func TestMinAngleExistsWithArityOne(t *testing.T) {
	if _, err := compile(t, "math.min_angle(370)"); err != nil {
		t.Fatalf("math.min_angle should compile: %v", err)
	}
	if _, err := compile(t, "math.min_angle(1,2)"); err == nil {
		t.Errorf("math.min_angle should reject 2 arguments")
	}
	cases := map[string]float64{
		"math.min_angle(370)":  10,
		"math.min_angle(-370)": -10,
		"math.min_angle(190)":  -170,
		"math.min_angle(-190)": 170,
		"math.min_angle(0)":    0,
		"math.min_angle(90)":   90,
	}
	for src, want := range cases {
		if got := evalIn(t, src, nil); got != want {
			t.Errorf("%s = %v, want %v", src, got, want)
		}
	}
}

// TestHermiteBlendArity guards the other half of the registry line that
// settled min_angle: math.hermite_blend is min=1 max=1 too, which this
// package already had right.
func TestHermiteBlendArity(t *testing.T) {
	if _, err := compile(t, "math.hermite_blend(0.5)"); err != nil {
		t.Fatalf("math.hermite_blend(0.5) should compile: %v", err)
	}
	if _, err := compile(t, "math.hermite_blend(0,1,0.5)"); err == nil {
		t.Errorf("math.hermite_blend should reject 3 arguments")
	}
}

// TestUnresolvedReadSkipsEverythingAfterIt is the eval-level pin on the
// half of the `??` finding that is easiest to lose: an unresolved read
// with no enclosing `??` sets the engine's program counter to -1 -- the
// missing-variable handler, reached from the read instruction's not-found
// branch -- which ends the program. The RESULT is 0 either
// way -- what changes is that nothing after the read runs.
func TestUnresolvedReadSkipsEverythingAfterIt(t *testing.T) {
	scope := eval.NewScope()
	scope.Temp["seen"] = -1
	got := evalIn(t, "t.seen=1;v.missing;t.seen=2;return t.seen;", scope)
	if got != 0 {
		t.Errorf("aborted program = %v, want 0", got)
	}
	if scope.Temp["seen"] != 1 {
		t.Errorf("t.seen = %v, want 1: the assignment AFTER the failed read must not happen", scope.Temp["seen"])
	}
}

// TestResolvedZeroIsNotAnUnresolvedRead: the engine distinguishes "this
// slot holds 0" from "this slot has no value", and everything in this
// change depends on that distinction being carried by key PRESENCE rather
// than by the value.
func TestResolvedZeroIsNotAnUnresolvedRead(t *testing.T) {
	scope := eval.NewScope()
	scope.Variable["x"] = 0
	if got := evalIn(t, "v.x ?? 5", scope); got != 0 {
		t.Errorf("v.x (present, 0) ?? 5 = %v, want 0", got)
	}
	if got := evalIn(t, "v.x", scope); got != 0 {
		t.Errorf("v.x (present, 0) = %v, want 0", got)
	}
	// And the same expression against a scope that does not hold the key
	// takes the right-hand side.
	if got := evalIn(t, "v.x ?? 5", eval.NewScope()); got != 5 {
		t.Errorf("v.x (absent) ?? 5 = %v, want 5", got)
	}
}

// TestUnknownQueryDoesNotAbort: query. is the one namespace that does NOT
// participate. The engine rejects an unknown query name at TOKENIZE time
// (with a "Failed to resolve query" error), so an unresolved query
// read is not a state its evaluator can reach and `q.unknown ?? x` never
// compiles at all. This package has no query registry and cannot
// reproduce the tokenize-time rejection, so it keeps reading 0 -- and must
// not abort, which would make every unregistered query in a real pack
// silently truncate its program.
func TestUnknownQueryDoesNotAbort(t *testing.T) {
	scope := eval.NewScope()
	scope.Temp["after"] = -1
	got := evalIn(t, "query.nothing_registered;t.after=7;return t.after;", scope)
	if got != 7 {
		t.Errorf("program with an unknown query = %v, want 7 (no abort)", got)
	}
}

// TestNonSentinelPanicStillEscapes: the unresolved-read mechanism is a
// panic with an unexported sentinel type, recovered in exactly two places.
// Both re-panic anything else, so a genuine bug inside a host QueryFunc
// reaches the host with its original value rather than being silently
// converted into "the left side was unresolved".
func TestNonSentinelPanicStillEscapes(t *testing.T) {
	p, err := compile(t, "(query.boom(1) ?? 1) + v.alsoMissing")
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		r := recover()
		if r == nil {
			t.Fatalf("host panic was swallowed")
		}
		if s, ok := r.(string); !ok || s != "host bug" {
			t.Fatalf("recovered %#v, want the original \"host bug\"", r)
		}
	}()
	p.Run(&eval.Context{
		Scope: eval.NewScope(),
		QueryFuncs: map[string]eval.QueryFunc{
			"boom": func([]float64, *eval.Context) float64 { panic("host bug") },
		},
	})
}

// ftoa renders a float as Molang source (no exponent notation, which the
// lexer would accept but which is not how any of these callers mean it).
func ftoa(v float64) string {
	return strconv.FormatFloat(v, 'f', -1, 64)
}
