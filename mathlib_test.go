package molang

import (
	"math"
	"sort"
	"strings"
	"testing"

	"github.com/stirante/molang-go/eval"
)

// mathlib_test.go is the math library. See language_test.go's header for how
// the four test files in this package divide up.
//
// This one carries what the generated table in vanilla_cases_test.go CANNOT.
// A third of the game's own math cases compute their expectation at run time
// -- they call the C library and compare -- rather than pinning a literal, so
// there is nothing to transcribe for acos, asin, atan, atan2, ln, pow,
// hermite_blend, lerp, lerprotate, min_angle or trunc. The RNG-drawing four
// are absent from the table for a different reason: their real assertion is a
// range, not a value. All of that lives here.
//
// Basic coverage of the math.* library: one or two ordinary values per
// function, of the kind a pack author actually writes.
//
// This is deliberately NOT bit-pinning. eval/ease_table_test.go and
// eval/parity_test.go already pin the exact float32 patterns for the cases
// where the engine does something surprising, and those tests are the ones
// to read when a value looks wrong. What was missing was the boring half:
// that math.floor(1.7) is 1, that math.pow(2, 10) is 1024, and that every
// name in the table can be called at all.
//
// The boring half earns its place through TestMathTableIsFullyCovered
// below, which fails when a function is added to eval.MathArity without a
// case here. Coverage that has to be maintained by hand stops being
// maintained.

// mathCase is one call and what it should produce. tol is 0 for values that
// must come out exact; the only cases that need a tolerance are the ones
// that convert radians to degrees in float32, where 90 degrees is 90.00001.
type mathCase struct {
	expr string
	want float64
	tol  float64
}

// degTol covers the float32 radian->degree conversion. The conversion is a
// single float32 multiply by 180/pi, so a right angle lands a few ulps off
// 90 rather than on it. That is the engine's own arithmetic, not a defect
// here, which is why these cases are checked to a tolerance instead of
// being pinned to whatever this package currently produces -- a pinned
// value would just be this implementation agreeing with itself.
const degTol = 1e-4

var mathCases = []mathCase{
	{"math.abs(-5)", 5, 0},
	{"math.abs(5)", 5, 0},
	{"math.abs(0)", 0, 0},

	{"math.acos(1)", 0, degTol},
	{"math.acos(0)", 90, degTol},
	{"math.acos(-1)", 180, degTol},
	{"math.asin(0)", 0, degTol},
	{"math.asin(1)", 90, degTol},
	{"math.asin(-1)", -90, degTol},
	{"math.atan(0)", 0, degTol},
	{"math.atan(1)", 45, degTol},
	{"math.atan2(1, 1)", 45, degTol},
	{"math.atan2(0, 1)", 0, degTol},
	{"math.atan2(1, 0)", 90, degTol},

	{"math.ceil(1.2)", 2, 0},
	{"math.ceil(-1.2)", -1, 0},
	{"math.ceil(3)", 3, 0},

	{"math.clamp(5, 0, 10)", 5, 0},
	{"math.clamp(-1, 0, 10)", 0, 0},
	{"math.clamp(11, 0, 10)", 10, 0},
	// Reversed bounds. The upper bound is tested first, so these are not
	// what a lower-bound-first clamp answers; see eval/math.go.
	{"math.clamp(3, 2, 1)", 1, 0},
	{"math.clamp(-1, -2, -3)", -3, 0},
	{"math.clamp(-2, -1, -3)", -3, 0},
	{"math.clamp(-3, -1, -2)", -1, 0},
	{"math.clamp(-3, -2, -1)", -2, 0},

	{"math.copy_sign(3, -1)", -3, 0},
	{"math.copy_sign(-3, 1)", 3, 0},
	{"math.copy_sign(3, 1)", 3, 0},

	// Trig is degree-based and reads a quantised table, so these are the
	// values the table holds at those exact indices. math.sin(180) is the
	// one worth knowing about; it has its own case further down.
	{"math.cos(0)", 1, 0},
	{"math.cos(180)", -1, 0},
	{"math.sin(0)", 0, 0},
	{"math.sin(90)", 1, 0},

	{"math.exp(0)", 1, 0},
	{"math.exp(1)", eval.Round32(math.E), 0},

	{"math.floor(1.7)", 1, 0},
	{"math.floor(-1.2)", -2, 0},
	{"math.floor(3)", 3, 0},

	{"math.hermite_blend(0)", 0, 0},
	{"math.hermite_blend(1)", 1, 0},
	{"math.hermite_blend(0.5)", 0.5, 0},

	{"math.inverse_lerp(0, 10, 5)", 0.5, 0},
	{"math.inverse_lerp(0, 10, 0)", 0, 0},
	{"math.inverse_lerp(0, 10, 10)", 1, 0},

	{"math.lerp(0, 10, 0.5)", 5, 0},
	{"math.lerp(0, 10, 0)", 0, 0},
	{"math.lerp(0, 10, 1)", 10, 0},
	// lerp does not clamp: t outside [0,1] extrapolates.
	{"math.lerp(0, 10, 2)", 20, 0},
	{"math.lerp(0, 10, -1)", -10, 0},

	// lerprotate takes the short way round the circle, so 350 -> 10 is a
	// 20-degree move forwards rather than a 340-degree move backwards.
	{"math.lerprotate(350, 10, 0.5)", 360, 0},
	{"math.lerprotate(0, 90, 0.5)", 45, 0},

	{"math.ln(1)", 0, 0},
	{"math.ln(math.exp(1))", 1, 1e-6},

	{"math.max(3, 5)", 5, 0},
	{"math.max(5, 3)", 5, 0},
	{"math.min(3, 5)", 3, 0},
	{"math.min(5, 3)", 3, 0},

	{"math.min_angle(370)", 10, 0},
	{"math.min_angle(-370)", -10, 0},
	{"math.min_angle(180)", 180, 0},

	{"math.mod(7, 3)", 1, 0},
	{"math.mod(1, 0)", 0, 0},
	{"math.mod(-7, 3)", -1, 0},
	{"math.mod(7.5, 2)", 1.5, 0},

	{"math.pow(2, 10)", 1024, 0},
	{"math.pow(9, 0.5)", 3, 1e-5},
	{"math.pow(2, 0)", 1, 0},

	{"math.round(1.5)", 2, 0},
	{"math.round(1.4)", 1, 0},
	{"math.round(-1.5)", -2, 0},

	{"math.sign(-3)", -1, 0},
	// Zero is positive here. See eval/math.go; this row read the other way
	// round until the language's own behaviour said otherwise.
	{"math.sign(0)", 1, 0},
	{"math.sign(3)", 1, 0},

	{"math.sqrt(9)", 3, 0},
	{"math.sqrt(0)", 0, 0},

	{"math.trunc(1.9)", 1, 0},
	{"math.trunc(-1.9)", -1, 0},

	// The RNG-drawing four, against newCtx's stub: floats cycle
	// 0.25, 0.5, 0.75 and NextIntBound is fed 1, 2, 3.
	{"math.random(0, 1)", 0.25, 0},
	{"math.random(10, 20)", 12.5, 0},
	{"math.random_integer(1, 6)", 2, 0},
	// Two draws each, taken in order: 0.25 then 0.5 of a span of 10.
	{"math.die_roll(2, 0, 10)", 7.5, 0},
	// bound is (6-1)+1 = 6; the stub yields 1 then 2, each offset by low.
	{"math.die_roll_integer(2, 1, 6)", 5, 0},
}

func TestMathLibraryBasics(t *testing.T) {
	for _, c := range mathCases {
		got := evalOK(t, c.expr)
		if c.tol == 0 {
			if got != c.want {
				t.Errorf("%s = %v, want %v", c.expr, got, c.want)
			}
			continue
		}
		if math.Abs(got-c.want) > c.tol {
			t.Errorf("%s = %v, want %v (+/- %g)", c.expr, got, c.want, c.tol)
		}
	}
}

// math.sin(180) is not zero, and neither is math.sin(360).
//
// This is not a rounding artefact of this port. Trig reads a 65,536-entry
// table built by the engine, and the entry at the half turn holds
// -8.742278e-08 rather than 0. An author comparing math.sin(angle) to 0
// exactly will not get a match at 180 degrees, and an author using it to
// drive a placement offset gets a value that floors to -1 rather than 0.
//
// Kept as its own test rather than a row in the table above because the
// row would read like a mistake.
func TestSinOfHalfTurnIsNotZero(t *testing.T) {
	got := evalOK(t, "math.sin(180)")
	if got == 0 {
		t.Fatal("math.sin(180) came out exactly 0; the engine's table holds a small negative there")
	}
	if got > 0 || got < -1e-6 {
		t.Errorf("math.sin(180) = %v, want a small negative near -8.7e-08", got)
	}
	// The consequence an author actually meets.
	if floored := evalOK(t, "math.floor(math.sin(180))"); floored != -1 {
		t.Errorf("math.floor(math.sin(180)) = %v, want -1", floored)
	}
}

// TestMathTableIsFullyCovered fails when a function is added to
// eval.MathArity without a case in mathCases. The ease_* family is excluded
// because eval/parity_test.go's TestAllThirtyEaseFunctionsExist already
// covers the whole cross product by construction, and thirty near-identical
// rows here would only make this table harder to read.
func TestMathTableIsFullyCovered(t *testing.T) {
	covered := map[string]bool{}
	for _, c := range mathCases {
		rest := strings.TrimPrefix(c.expr, "math.")
		if i := strings.IndexByte(rest, '('); i > 0 {
			covered[rest[:i]] = true
		}
	}
	var missing []string
	for name := range eval.MathArity {
		if strings.HasPrefix(name, "ease_") {
			continue
		}
		if !covered[name] {
			missing = append(missing, name)
		}
	}
	sort.Strings(missing)
	if len(missing) > 0 {
		t.Errorf("math.* functions with no case in mathCases: %v", missing)
	}
}

// TestMathArityIsEnforced checks that every function rejects a call with
// one argument too few and one too many. Arity is the most common thing to
// get wrong in a pack, and the whole value of catching it here is that the
// error arrives before the game does.
func TestMathArityIsEnforced(t *testing.T) {
	for name, arity := range eval.MathArity {
		for _, n := range []int{arity - 1, arity + 1} {
			if n < 0 {
				continue
			}
			args := ""
			for i := 0; i < n; i++ {
				if i > 0 {
					args += ", "
				}
				args += "1"
			}
			src := "math." + name + "(" + args + ")"
			if _, err := Compile(src); err == nil {
				t.Errorf("%s compiled, but %s takes %d argument(s)", src, name, arity)
			}
		}
	}
}
