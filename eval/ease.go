package eval

import (
	"math"
	"sync"
)

// The math.ease_* family.
//
// CONFIRMED: there are THIRTY of them, not eighteen. The engine's compiled
// math-function registry carries all thirty, each of arity 3, as the full
// cross product
//
//	{quad, cubic, quart, quint, sine, expo, circ, bounce, back, elastic}
//	x {in, out, in_out}
//
// This package used to know six shapes. quint, bounce, back and elastic
// were missing entirely -- i.e. `math.ease_out_bounce(0, 1, t)` was a
// compile error here while being perfectly valid in game.
//
// CONFIRMED about their shape: every easing is literally
// `start + (end - start) * curve(t)`, computed entirely in float32, with
// NO clamping of t. That matches what this package already did, and
// matches math.lerp's own no-clamping behaviour.
//
// CONFIRMED constants: bounce's 7.5625, 2.75, 0.75, 0.9375 and 0.984375;
// back's c1 = 1.70158, c3 = 2.70158 and the 1.525 that c2 = c1 * 1.525 is
// built from. Their exact float32 bit patterns are in the var block below.
// The curves otherwise match the easings.net / Robert Penner formulas.
//
// INFERRED, and worth being honest about: the exact ALGEBRAIC FORM of each
// curve. easings.net and Penner's originals describe the same curves
// through different arrangements of the same arithmetic, and in float32
// those arrangements do not always round identically. Only the constants,
// the arity, the lerp framing, the branch predicate below and the three
// specific corrections below were established from the engine itself; which
// of two equivalent groupings the engine computes for, say, easeInOutQuad's
// second half was not. Curve VALUES here can therefore be off by an ULP or
// two from the engine's; curve SHAPE cannot.

// easeInOutSecondHalf is the branch every in_out easing takes.
//
// CONFIRMED: the in_out variants perform an actual float32 DIVISION of t
// by 0.5 and then branch on "if t/0.5 >= 1, take the second half". For
// every finite t that is exactly `t >= 0.5` (dividing by 0.5 is exact), so
// the observable difference from the `t < 0.5` test this package used to
// write is confined to NaN: a NaN quotient compares UNORDERED, the
// greater-or-equal test is false, and the engine takes the FIRST half.
// Go's `t < 0.5` would also be false for NaN and therefore took the SECOND
// half -- the opposite.
//
// Written as the division-and-compare rather than as `!(t < 0.5)` so the
// confirmed order of operations is legible at the point where it matters.
func easeInOutSecondHalf(t float64) bool {
	return Round32(t/0.5) >= 1
}

// ---------------------------------------------------------------------
// The engine's sine lookup table
// ---------------------------------------------------------------------

// sinTableScale is 10430.3779296875, the float32 constant the
// engine both DIVIDES by to build the table and MULTIPLIES by to index it.
// It is 65536 / 2pi rounded to float32 -- smaller than the exact value by
// about 4.0e-8 relative -- so one full turn spans the whole 65536-entry
// table only to float32 precision. That the same constant appears on both
// sides is the load-bearing part; see sinTableValues.
var sinTableScale = math.Float32frombits(0x4622F983)

const (
	sinTableSize = 1 << 16
	sinTableMask = sinTableSize - 1
	// sinTableQuarterTurn is the +16384.0 the engine adds to an index to
	// read a cosine out of a sine table: 16384/65536 of a turn is pi/2.
	sinTableQuarterTurn = 16384
)

// sinTableValues is the engine's own 65536-entry float32 sine table.
//
// CONFIRMED: the INDEX ARITHMETIC. easeInSine converts its argument to
// t*pi/2 and then multiplies by 10430.3779296875, adds 16384.0, converts to
// a 32-bit signed integer by TRUNCATION (not rounding), masks with 0xFFFF
// and loads that entry -- a raw indexed load with NO interpolation between
// neighbouring entries. easeOutSine is the identical sequence without the
// +16384.0; easeInOutSine uses pi instead of pi/2. The three ease_*_elastic
// functions index the same table.
//
// CONFIRMED, and this REPLACES an inference that was wrong: the table's
// CONTENTS. The table is not stored data -- the engine builds it once at
// startup, with a 65536-iteration loop that converts the index to float32,
// divides it by 10430.3779296875f, and takes the single-precision libm sine
// of the quotient.
//
// So entry i is sinf(float32(i) / 10430.3779296875f) -- a DIVISION by the
// same float32 constant the lookup later multiplies by, NOT
// sin(i * 2pi / 65536), which is the natural guess and is what this file
// carried until the real construction was found. The two formulas disagree
// for 43378 of the 65536 entries (66.2%), by up to 4.9e-7, because
// 10430.3779296875 is 65536/2pi only to float32 precision; building the table on
// the exact turn skews every entry's angle by that 4.0e-8 ratio.
//
// The reciprocal consistency is the point, and it is why the difference is
// not merely an ULP argument: because the same constant builds and indexes
// the table, a table sine of x is exactly `sinf(trunc(x*K)/K)`. The
// argument is snapped to a grid defined by K itself, not to a grid defined
// by 2pi, so the table is self-consistent in a way the inferred version
// was not.
//
// The QUANTISATION remains the large observable effect -- an ease_*_sine
// result is snapped to one of 65536 samples per turn, not computed, worth
// order 1e-4 -- and the contents correction is three orders of magnitude
// smaller than that. It is made anyway because the construction is known
// exactly and nothing has to be assumed.
//
// The same table backs the engine's own sine and cosine -- the non-libm
// trigonometry in the engine's own math library, used by parts of worldgen
// that have nothing to do with Molang. A consumer that models those has to
// build the same table the same way; this package's copy is not exported,
// so today there are two, which is a duplication worth removing once one of
// them has an owner.
//
// Built lazily: 65536 sines is around a millisecond, which is not a cost
// to impose on every process that merely imports this package, and most
// never evaluate a sine easing at all.
var sinTableValues = sync.OnceValue(func() []float32 {
	t := make([]float32, sinTableSize)
	for i := range t {
		// float32(i) / sinTableScale is the integer-to-float
		// conversion and the division, done in float32 exactly as the
		// engine builds the table; math.Sin of the widened quotient
		// narrowed back models sinf, which is correctly rounded to
		// well under a float32 ULP.
		t[i] = float32(math.Sin(float64(float32(i) / sinTableScale)))
	}
	return t
})

// sinTableAt performs the engine's index step on an already-scaled index:
// convert to a 32-bit signed integer by truncation toward zero (saturating
// -- see f32ToInt), mask with 0xFFFF, indexed load. The mask is what makes a
// negative angle work: -10430 becomes 55106, i.e. the same point read from
// the other end of the turn.
//
// TRUNCATE, not floor, and for a negative index those are different: -1.5
// becomes -1, not -2, so a negative angle reads the entry one NEARER to
// zero than a floor would give. Sampling negative angles on a grid that
// mostly misses integers, truncation and floor pick different entries
// 98.5% of the time and the values they read differ by up to 9.6e-5 --
// three orders of magnitude more than the contents correction above, and
// the easy thing for a port to get wrong, because `math.Floor` is the
// reflex and Go's own float-to-int conversion happens to be right.
func sinTableAt(index float32) float64 {
	i := uint32(int32(f32ToInt(float64(index)))) & sinTableMask
	return float64(sinTableValues()[i])
}

// tableSin is sin(x) for x in RADIANS, computed the engine's way.
//
// This is deliberately NOT what math.sin does. CONFIRMED: the math.sin and
// math.cos OPCODES are direct single-precision libm calls -- math.sin calls
// sinf and math.cos calls cosf -- so mathf32.go's f32Sin/f32Cos model is
// right for those and would be wrong here. The sine EASINGS and the
// elastic easings do not go through sinf/cosf at all; they index the
// table.
func tableSin(xRad float64) float64 {
	return sinTableAt(float32(xRad) * sinTableScale)
}

// tableCos is cos(x) for x in RADIANS: the same table read a quarter turn
// further along. The +16384 is applied in float32 AFTER the scale multiply
// and BEFORE the truncation, matching the engine's own multiply, then add,
// then truncate order -- the order decides which entry is read, so it is
// not a detail.
//
// The explicit float32() around the product is not decoration: Go is
// permitted to fuse a multiply into a following add, "possibly across
// statements", and gc does exactly that on arm64. The engine's own
// sequence is a separate multiply and add, so a fused build would round
// the index differently and could land on a different entry. The conversion
// forbids the fusion.
func tableCos(xRad float64) float64 {
	return sinTableAt(float32(float32(xRad)*sinTableScale) + sinTableQuarterTurn)
}

// ---------------------------------------------------------------------
// float32 arithmetic helpers
//
// Named for the float32 machine operations the engine's easings are built
// out of, because that is exactly what they are: one rounded
// single-precision operation each. Writing the curves through them keeps
// this file's "round after every individual operation" discipline visible
// instead of burying it under nested Round32 calls.
// ---------------------------------------------------------------------

func fadd(a, b float64) float64 { return Round32(a + b) }
func fsub(a, b float64) float64 { return Round32(a - b) }
func fmul(a, b float64) float64 { return Round32(a * b) }
func fdiv(a, b float64) float64 { return Round32(a / b) }

// fpow2 is powf(2, e). CONFIRMED that the expo easings call powf: e.g.
// easeOutExpo is `1 - powf(2, -10t)`.
func fpow2(e float64) float64 { return Round32(math.Pow(2, e)) }

// fpown raises an already-float32 base to a small positive integer power
// by repeated single-precision multiplication, which is what a compiler
// emits for t*t*t rather than a powf call.
func fpown(base float64, n int) float64 {
	r := 1.0
	for i := 0; i < n; i++ {
		r = fmul(r, base)
	}
	return r
}

// ---------------------------------------------------------------------
// Curve constants
// ---------------------------------------------------------------------

var (
	// halfPiF32/piF32 are the exact float32 constants the sine easings
	// load.
	halfPiF32 = float64(math.Float32frombits(0x3FC90FDB))
	piF32     = float64(math.Float32frombits(0x40490FDB))

	// bounce, all CONFIRMED as literal constants in the engine.
	bounceN1 = float64(math.Float32frombits(0x40F20000)) // 7.5625
	bounceD1 = 2.75

	// back. c1 and c3 are loaded directly; c2 is built as c1 * 1.525,
	// with 1.525 itself a loaded float32 constant.
	backC1 = float64(math.Float32frombits(0x3FD9CD60)) // 1.70158
	backC3 = float64(math.Float32frombits(0x402CE6B0)) // 2.70158
	backC2 = fmul(backC1, float64(math.Float32frombits(0x3FC33333)))

	// elastic's period constants. INFERRED (the standard easings.net
	// 2pi/3 and 2pi/4.5); only the fact that elastic reads the SIN table
	// is confirmed, not which period constant it multiplies by.
	elasticC4 = fdiv(fmul(2, piF32), 3)
	elasticC5 = fdiv(fmul(2, piF32), 4.5)
)

// easeOutBounceCurve is the four-arc bounce every bounce variant is built
// from. The three offsets (1.5/d1, 2.25/d1, 2.625/d1) and the three
// vertical offsets (0.75, 0.9375, 0.984375) are the confirmed constants;
// the arrangement is easings.net's.
func easeOutBounceCurve(t float64) float64 {
	switch {
	case t < fdiv(1, bounceD1):
		return fmul(bounceN1, fmul(t, t))
	case t < fdiv(2, bounceD1):
		u := fsub(t, fdiv(1.5, bounceD1))
		return fadd(fmul(bounceN1, fmul(u, u)), 0.75)
	case t < fdiv(2.5, bounceD1):
		u := fsub(t, fdiv(2.25, bounceD1))
		return fadd(fmul(bounceN1, fmul(u, u)), 0.9375)
	default:
		u := fsub(t, fdiv(2.625, bounceD1))
		return fadd(fmul(bounceN1, fmul(u, u)), 0.984375)
	}
}

// easeShapes is every curve family the engine's registry carries, and
// easeVariants every direction. Their cross product is the thirty
// confirmed ease functions, and the registry holds exactly those.
var easeShapes = []string{
	"quad", "cubic", "quart", "quint", "sine",
	"expo", "circ", "bounce", "back", "elastic",
}

var easeVariants = []string{"in", "out", "in_out"}

// easeCurves[shape][variant](t) -> the 0..1-ish curve position, which
// math.ease_<variant>_<shape>(start, end, t) then lerps between start and
// end. t is NOT clamped anywhere (confirmed), so every one of these is
// defined and meaningful outside [0, 1].
//
// NOTE the three endpoint special cases that are NOT here, and that most
// reference implementations do have. CONFIRMED: easeOutExpo is a bare
// `1 - powf(2, -10t)` with no `if (t == 1) return 1` -- so
// ease_out_expo(0, 1, 1) is 0.999023..., not 1 -- and easeInExpo is
// likewise a bare `powf(2, 10t - 10)`, giving 0.0009765625 rather than 0
// at t = 0. This package used to special-case both. The in_out expo and
// all three elastic curves have their endpoint cases dropped too, which is
// INFERRED by consistency with those two rather than separately
// confirmed.
var easeCurves = map[string]map[string]func(float64) float64{
	"quad": {
		"in":  func(t float64) float64 { return fmul(t, t) },
		"out": func(t float64) float64 { d := fsub(1, t); return fsub(1, fmul(d, d)) },
		"in_out": func(t float64) float64 {
			if !easeInOutSecondHalf(t) {
				return fmul(fmul(2, t), t)
			}
			d := fadd(fmul(-2, t), 2)
			return fsub(1, fdiv(fmul(d, d), 2))
		},
	},
	"cubic": {
		"in":  func(t float64) float64 { return fpown(t, 3) },
		"out": func(t float64) float64 { return fsub(1, fpown(fsub(1, t), 3)) },
		"in_out": func(t float64) float64 {
			if !easeInOutSecondHalf(t) {
				return fmul(4, fpown(t, 3))
			}
			d := fadd(fmul(-2, t), 2)
			return fsub(1, fdiv(fpown(d, 3), 2))
		},
	},
	"quart": {
		"in":  func(t float64) float64 { return fpown(t, 4) },
		"out": func(t float64) float64 { return fsub(1, fpown(fsub(1, t), 4)) },
		"in_out": func(t float64) float64 {
			if !easeInOutSecondHalf(t) {
				return fmul(8, fpown(t, 4))
			}
			d := fadd(fmul(-2, t), 2)
			return fsub(1, fdiv(fpown(d, 4), 2))
		},
	},
	"quint": {
		"in":  func(t float64) float64 { return fpown(t, 5) },
		"out": func(t float64) float64 { return fsub(1, fpown(fsub(1, t), 5)) },
		"in_out": func(t float64) float64 {
			if !easeInOutSecondHalf(t) {
				return fmul(16, fpown(t, 5))
			}
			d := fadd(fmul(-2, t), 2)
			return fsub(1, fdiv(fpown(d, 5), 2))
		},
	},
	// The sine curves go through the engine's 65536-entry SIN table, NOT
	// through sinf/cosf -- see sinTableValues. easeInOutSine has no
	// half/half branch of its own; it is a single cosine over the whole
	// turn, which is why easeInOutSecondHalf does not appear here.
	"sine": {
		"in":     func(t float64) float64 { return fsub(1, tableCos(fmul(t, halfPiF32))) },
		"out":    func(t float64) float64 { return tableSin(fmul(t, halfPiF32)) },
		"in_out": func(t float64) float64 { return fdiv(-fsub(tableCos(fmul(piF32, t)), 1), 2) },
	},
	"expo": {
		"in":  func(t float64) float64 { return fpow2(fsub(fmul(10, t), 10)) },
		"out": func(t float64) float64 { return fsub(1, fpow2(fmul(-10, t))) },
		"in_out": func(t float64) float64 {
			if !easeInOutSecondHalf(t) {
				return fdiv(fpow2(fsub(fmul(20, t), 10)), 2)
			}
			return fdiv(fsub(2, fpow2(fadd(fmul(-20, t), 10))), 2)
		},
	},
	"circ": {
		"in":  func(t float64) float64 { return fsub(1, Round32(math.Sqrt(fsub(1, fmul(t, t))))) },
		"out": func(t float64) float64 { d := fsub(t, 1); return Round32(math.Sqrt(fsub(1, fmul(d, d)))) },
		"in_out": func(t float64) float64 {
			if !easeInOutSecondHalf(t) {
				d := fmul(2, t)
				return fdiv(fsub(1, Round32(math.Sqrt(fsub(1, fmul(d, d))))), 2)
			}
			d := fadd(fmul(-2, t), 2)
			return fdiv(fadd(Round32(math.Sqrt(fsub(1, fmul(d, d)))), 1), 2)
		},
	},
	"bounce": {
		"in":  func(t float64) float64 { return fsub(1, easeOutBounceCurve(fsub(1, t))) },
		"out": easeOutBounceCurve,
		"in_out": func(t float64) float64 {
			if !easeInOutSecondHalf(t) {
				return fdiv(fsub(1, easeOutBounceCurve(fsub(1, fmul(2, t)))), 2)
			}
			return fdiv(fadd(1, easeOutBounceCurve(fsub(fmul(2, t), 1))), 2)
		},
	},
	"back": {
		"in": func(t float64) float64 {
			return fsub(fmul(backC3, fpown(t, 3)), fmul(backC1, fmul(t, t)))
		},
		"out": func(t float64) float64 {
			d := fsub(t, 1)
			return fadd(fadd(1, fmul(backC3, fpown(d, 3))), fmul(backC1, fmul(d, d)))
		},
		"in_out": func(t float64) float64 {
			if !easeInOutSecondHalf(t) {
				d := fmul(2, t)
				return fdiv(fmul(fmul(d, d), fsub(fmul(fadd(backC2, 1), d), backC2)), 2)
			}
			d := fsub(fmul(2, t), 2)
			return fdiv(fadd(fmul(fmul(d, d), fadd(fmul(fadd(backC2, 1), d), backC2)), 2), 2)
		},
	},
	// The elastic family, and the one place in this table where an endpoint
	// case is real.
	//
	// Expo has none -- that was read directly and is why the note above says
	// so. Elastic's cases were dropped at the same time BY INFERENCE from
	// expo, and that inference was wrong: elastic does have them, and only
	// at the two ends where the bare formula misses. in at t=0 would give
	// -0.000488 and out at t=1 would give 1.000488; both are 0 and 1 in game.
	// The other two ends need nothing, because the formula already lands on
	// them exactly -- so the cases are added where the values demand and
	// nowhere else, rather than symmetrically because that looks tidier.
	"elastic": {
		"in":     elasticIn,
		"out":    elasticOut,
		"in_out": elasticInOut,
	},
}

// elasticIn, elasticOut and elasticInOut are named rather than written inline
// in easeCurves above, because in_out calls the other two and a map literal
// cannot refer to itself while it is being built.
//
// The endpoint case in each of the first two is real, and this is the one
// place in the table where that is so. Expo has none -- that was read
// directly, and is what the note on easeCurves records. Elastic's were
// dropped at the same time BY INFERENCE from expo, and the inference was
// wrong. Without them, in at t=0 gives -0.000488 and out at t=1 gives
// 1.000488, where the game gives exactly 0 and 1.
//
// The other two ends need no case: the bare formula already lands on them.
// So the cases sit where the values demand and nowhere else, rather than
// symmetrically because that would look tidier.
func elasticIn(t float64) float64 {
	if t == 0 {
		return 0
	}
	return -fmul(fpow2(fsub(fmul(10, t), 10)), tableSin(fmul(fsub(fmul(10, t), 10.75), elasticC4)))
}

func elasticOut(t float64) float64 {
	if t == 1 {
		return 1
	}
	return fadd(fmul(fpow2(fmul(-10, t)), tableSin(fmul(fsub(fmul(10, t), 0.75), elasticC4))), 1)
}

// elasticInOut is COMPOSED from the two halves rather than computed by the
// single formula most references publish. They are not the same curve: at
// t = 0.3 the composition gives -0.015625 and the single formula +0.0239, so
// the difference is shape, not rounding. Composing also inherits the
// endpoints, so it needs no cases of its own.
func elasticInOut(t float64) float64 {
	if !easeInOutSecondHalf(t) {
		return fdiv(elasticIn(fmul(2, t)), 2)
	}
	return fadd(0.5, fdiv(elasticOut(fsub(fmul(2, t), 1)), 2))
}
