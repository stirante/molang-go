// float32_model_test.go pins the two claims the whole precision model rests
// on. Neither is obvious, and both are cheap to check, so they are checked
// rather than asserted in a comment.
//
// Molang computes in single precision. This package holds values in float64
// and rounds to float32 at every point the language produces a value. Someone
// reading that for the first time reasonably asks why it does not just use
// float32, and whether rounding after the fact is even the same thing.
package eval_test

import (
	"math"
	"math/rand"
	"testing"

	"github.com/stirante/molang-go/eval"
)

// TestRoundingAfterFloat64IsExactlyFloat32Arithmetic answers "is this even the
// same thing": yes, exactly, and not by luck.
//
// Rounding twice -- to float64 and then to float32 -- can in general land
// somewhere a single rounding would not. It cannot here. Double rounding is
// harmless when the wide format carries at least 2p+2 bits and the narrow one
// carries p: float32 has p = 24, so 50 bits suffice and float64 has 53. That
// covers +, -, *, / and sqrt, which is every arithmetic operation in the
// language.
//
// So this is not an approximation of float32 arithmetic that happens to be
// close. It is float32 arithmetic, computed a different way. The test exists
// because the guarantee is a property of the two formats' widths, which is
// exactly the kind of thing that gets "optimised" by someone who does not know
// it is load-bearing.
func TestRoundingAfterFloat64IsExactlyFloat32Arithmetic(t *testing.T) {
	r := rand.New(rand.NewSource(1))
	ops := []struct {
		name string
		wide func(a, b float64) float64
		narr func(a, b float32) float32
	}{
		{"+", func(a, b float64) float64 { return a + b }, func(a, b float32) float32 { return a + b }},
		{"-", func(a, b float64) float64 { return a - b }, func(a, b float32) float32 { return a - b }},
		{"*", func(a, b float64) float64 { return a * b }, func(a, b float32) float32 { return a * b }},
		{"/", func(a, b float64) float64 { return a / b }, func(a, b float32) float32 { return a / b }},
	}
	const n = 200000
	for _, op := range ops {
		for i := 0; i < n; i++ {
			a := math.Float32frombits(r.Uint32())
			b := math.Float32frombits(r.Uint32())
			if isNotFinite32(a) || isNotFinite32(b) {
				continue
			}
			viaWide := eval.Round32(op.wide(float64(a), float64(b)))
			direct := float64(op.narr(a, b))
			if viaWide != direct && !(math.IsNaN(viaWide) && math.IsNaN(direct)) {
				t.Fatalf("%s: a=%v b=%v -- float64 then Round32 gives %v, float32 directly gives %v; "+
					"the double-rounding guarantee this package relies on does not hold",
					op.name, a, b, viaWide, direct)
			}
		}
	}
}

// TestNoOperationLeaksExtraPrecision answers "did someone forget a Round32":
// every value an expression produces must be exactly representable as float32.
//
// A missing rounding call does not fail loudly. It leaves a value carrying
// more precision than the game would, which agrees with the game on most
// inputs and drifts on the rest -- so it shows up as a handful of wrong
// placements much later, in something that looks unrelated. This catches it at
// the operator instead.
//
// The property is checked over random operands rather than hand-picked ones,
// because the hand-picked case for `+` (0.1 + 0.2) is already pinned
// elsewhere and a hand-picked case per operator would only catch the operators
// somebody thought to list.
func TestNoOperationLeaksExtraPrecision(t *testing.T) {
	exprs := []string{
		"v.a + v.b", "v.a - v.b", "v.a * v.b", "v.a / v.b",
		"-v.a", "!v.a",
		"v.a < v.b", "v.a <= v.b", "v.a == v.b", "v.a != v.b",
		"v.a && v.b", "v.a || v.b",
		"v.a ? v.b : v.a",
		"math.abs(v.a)", "math.floor(v.a)", "math.ceil(v.a)", "math.round(v.a)",
		"math.trunc(v.a)", "math.sign(v.a)", "math.sqrt(math.abs(v.a))",
		"math.exp(v.a)", "math.ln(math.abs(v.a))", "math.pow(v.a, v.b)",
		"math.mod(v.a, v.b)", "math.min(v.a, v.b)", "math.max(v.a, v.b)",
		"math.clamp(v.a, v.b, v.a)", "math.lerp(v.a, v.b, 0.5)",
		"math.sin(v.a)", "math.cos(v.a)", "math.atan(v.a)", "math.atan2(v.a, v.b)",
		"math.acos(v.a)", "math.asin(v.a)",
		"math.inverse_lerp(v.a, v.b, 0.25)", "math.hermite_blend(v.a)",
		"math.min_angle(v.a)", "math.lerprotate(v.a, v.b, 0.5)",
		"math.ease_in_out_elastic(v.a, v.b, 0.3)",
		// A chain, because a leak can also come from a value passing THROUGH
		// an operator rather than being produced by one.
		"(v.a + v.b) * (v.a - v.b) / (math.abs(v.b) + 1)",
	}
	r := rand.New(rand.NewSource(7))
	for _, src := range exprs {
		p, err := compile(t, src)
		if err != nil {
			t.Errorf("%s: %v", src, err)
			continue
		}
		for i := 0; i < 3000; i++ {
			a := math.Float32frombits(r.Uint32())
			b := math.Float32frombits(r.Uint32())
			if isNotFinite32(a) || isNotFinite32(b) {
				continue
			}
			scope := eval.NewScope()
			scope.Variable["a"] = float64(a)
			scope.Variable["b"] = float64(b)
			got := p.Run(&eval.Context{Scope: scope})
			if math.IsNaN(got) || math.IsInf(got, 0) {
				continue
			}
			if float64(float32(got)) != got {
				t.Fatalf("%s with a=%v b=%v produced %v, which is not a float32 -- "+
					"something on that path is missing its rounding", src, a, b, got)
			}
		}
	}
}

func isNotFinite32(f float32) bool {
	d := float64(f)
	return math.IsNaN(d) || math.IsInf(d, 0)
}
