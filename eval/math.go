package eval

import "math"

// mathPiF32 is math.pi rounded to float32 and widened back -- Molang's
// numeric value type is float32 throughout (see Round32's doc comment), so
// the constant every math.pi read/math.<fn> call sees must already be
// float32-exact.
var mathPiF32 = Round32(math.Pi)

// deg2radF32 is the exact IEEE-754 binary32 bit pattern CONFIRMED for the
// real engine's math.cos/math.sin degree-to-radian step: the compiled
// instruction for math.cos loads the constant as a 32-bit integer
// immediate, moves it into a single-precision register and multiplies
// entirely in float32, before the argument ever reaches cosf/sinf. The bit
// pattern below decodes as binary32 to ≈0.017453292, the correctly-rounded
// float32 of π/180.
var deg2radF32 = math.Float32frombits(0x3C8EFA35)

// divEpsilon is the near-zero denominator threshold of the engine's Divide
// guard instruction: `fabsf(denom) < divEpsilon` short-circuits the whole
// division to +0.0 without evaluating the numerator. CONFIRMED: the
// threshold is the float32 2^-23 = 1.1920928955078125e-7 (FLT_EPSILON),
// and the compare is a strict less-than, so the guard fires strictly below
// the threshold. See compile.go's ast.Div case.
var divEpsilon = float64(math.Float32frombits(0x34000000))

// DivGuardFires reports whether the engine's Divide guard short-circuits a
// division with this denominator -- i.e. whether `<anything> / den`
// evaluates to +0.0 instead of performing the divide at all. Exported
// because it is the ONE numeric decision shared by the two places this
// module divides: eval.compileBinary's ast.Div case, and package
// transform's constant folder. Those two previously each carried their own
// idea of what `/` means, and the folder's (a bare Go `/`) disagreed with
// the evaluator's for every denominator in this window -- transform folded
// `1/0.0000001` to `1e+07`, which the printer then emitted as source that
// the evaluator reads back as `0`. Keep both callers routed through here
// so the same drift cannot recur.
//
// Note the ASYMMETRY between the two callers, which is deliberate and not
// drift: the evaluator additionally skips evaluating the numerator when
// this fires (the engine rewrites the program counter past it, so
// numerator-side RNG draws are not consumed -- see compile.go's ast.Div
// case). The folder has no such concern: it only ever folds two literal
// operands, which have no side effects and draw nothing.
func DivGuardFires(den float64) bool { return math.Abs(den) < divEpsilon }

// rad2degF32 is the float32 of 180/π, used for math.acos/asin/atan/atan2's
// radian-to-degree conversion. Not individually confirmed the way
// deg2radF32 was (only cos/sin/pow's own call sites were read this pass).
//
// CONFIRMED since, and it is NOT what rounding 180/pi to float32 gives.
// The value is 57.2957764, one ulp BELOW the correctly rounded 57.2957802.
// This used to be written as float32(180/math.Pi), which lands on the
// higher one; the literal below is the lower one, spelled by its bits so it
// cannot drift back.
//
// One ulp in a multiplier is not nothing here: it is applied to the result
// of every inverse trig call, and this package pins exact bit patterns
// elsewhere for the same reason -- a constant that is nearly right gives
// answers that are nearly right, and nearly compounds through an expression.
//
// The four functions that use it (acos, asin, atan, atan2) all take the
// C library's radian result and scale it by this. The forward direction is
// the separate deg2radF32 above.
var rad2degF32 = math.Float32frombits(0x42652EE0)

// mathFn is the shape of every math.* implementation. rng is only used by
// the four RNG-drawing functions (random, random_integer, die_roll,
// die_roll_integer); everything else ignores it. Every implementation
// below rounds to float32 after each individual operation, not just once
// at the end, per Round32's doc comment -- see this file's package doc
// note in eval/mathf32.go for math.cos/sin/pow specifically, which need
// float32 arithmetic *before* the libm call too, not just rounding of a
// float64 result.
type mathFn func(rng RNG, args []float64) float64

// MathArity is the arity of every supported math.* function, excluding
// math.pi (a bare constant, never called with arguments). Confirmed against
// the game's compiled math-function registry table.
var MathArity = map[string]int{
	"abs": 1, "acos": 1, "asin": 1, "atan": 1, "atan2": 2,
	"ceil": 1, "clamp": 3, "copy_sign": 2, "cos": 1,
	"die_roll": 3, "die_roll_integer": 3,
	"exp": 1, "floor": 1, "hermite_blend": 1, "inverse_lerp": 3,
	"lerp": 3, "lerprotate": 3, "ln": 1, "max": 2, "min": 2,
	"min_angle": 1, "mod": 2,
	"pow": 2, "random": 2, "random_integer": 2, "round": 1, "sign": 1,
	"sin": 1, "sqrt": 1, "trunc": 1,
}

// The 30 math.ease_* functions, all arity 3, are registered here from the
// shape/variant lists in eval/ease.go, which is where their confirmation
// lives.
func init() {
	for _, shape := range easeShapes {
		for _, variant := range easeVariants {
			MathArity["ease_"+variant+"_"+shape] = 3
		}
	}
}

// RandomFnNames is the set of math.* functions that draw from RNG.
var RandomFnNames = map[string]bool{
	"random": true, "random_integer": true, "die_roll": true, "die_roll_integer": true,
}

// f32ToInt converts an already-float32 Molang value to an integer with
// DEFINED behavior for the cases Go leaves undefined: NaN, the infinities,
// and anything outside the destination range.
//
// This is not pedantry. `int(high - low + 1)` in math.random_integer used
// to be a plain Go conversion, and `math.random_integer(0, 1e30)` therefore
// produced -9223372036854775808 on amd64, whose uint32 truncation is
// exactly 0 -- an integer divide by zero inside the RNG, i.e. a Molang
// expression crashing its host. (Found by evaluating the golden corpus
// against a scope seeded with float32-range extremes; the all-zero scope
// the corpus previously used could never produce such an argument.)
//
// The rule implemented here is the saturating float->int conversion the
// engine's own platform provides: NaN converts to 0, and out-of-range
// values SATURATE at the destination limits rather than wrapping.
// Saturation is architecture-defined, not guesswork; what is INFERRED is
// that the engine simply converts and lets the hardware do this, rather
// than range-checking the argument itself first -- that specific code was
// not read.
//
// The limits are int32: the engine holds these counts in a C++ `int`, so a
// value that would not fit in one is not reachable there anyway.
// clampUnit confines a value to [-1, 1], the domain of arcsine and arccosine.
// A NaN argument stays NaN: both comparisons are false, so it falls through.
func clampUnit(v float64) float64 {
	if v < -1 {
		return -1
	}
	if v > 1 {
		return 1
	}
	return v
}

func f32ToInt(v float64) int {
	switch {
	case math.IsNaN(v):
		return 0
	case v >= math.MaxInt32:
		return math.MaxInt32
	case v <= math.MinInt32:
		return math.MinInt32
	}
	return int(v)
}

// copySign returns |magnitude| with the sign of sign (0/+0 counts as
// positive, -0 as negative — math.Signbit already distinguishes these).
// No rounding math needed: magnitude is already Round32-ed by the caller
// (it came from an already-rounded arg), and negation/Abs never round.
func copySign(magnitude, sign float64) float64 {
	if math.Signbit(sign) {
		return -math.Abs(magnitude)
	}
	return math.Abs(magnitude)
}

var mathTable = map[string]mathFn{}

func init() {
	mathTable["abs"] = func(_ RNG, a []float64) float64 { return Round32(math.Abs(a[0])) }
	// math.acos and math.asin CLAMP their argument to [-1, 1] instead of
	// returning NaN outside it. math.acos(1.0005) is 0, the same as
	// math.acos(1); math.asin(-1.0005) is -90.
	//
	// This matters more than an edge case usually would, because the argument
	// is so often a computed one: a dot product of two normalised vectors, or
	// a ratio that should be in range and lands a few ulps outside it after
	// float32 rounding. Without the clamp that is a NaN, and a NaN spreads
	// through the rest of the expression; with it the value is simply the
	// endpoint, which is what the author meant.
	//
	// A plain clamp reproduces every case the language pins, and those cases
	// sit just outside the domain (|x| up to 1.0005) -- exactly the near-miss
	// this is for. The engine's own guard reads as a tolerance window rather
	// than a clamp, so what it does for an argument far outside the domain,
	// math.acos(2) say, is NOT established here. If that ever matters, it is
	// the thing to go and check rather than to assume from this line.
	mathTable["acos"] = func(_ RNG, a []float64) float64 {
		return Round32(float64(float32(math.Acos(clampUnit(a[0]))) * rad2degF32))
	}
	mathTable["asin"] = func(_ RNG, a []float64) float64 {
		return Round32(float64(float32(math.Asin(clampUnit(a[0]))) * rad2degF32))
	}
	mathTable["atan"] = func(_ RNG, a []float64) float64 {
		return Round32(float64(float32(math.Atan(a[0])) * rad2degF32))
	}
	mathTable["atan2"] = func(_ RNG, a []float64) float64 {
		return Round32(float64(float32(math.Atan2(a[0], a[1])) * rad2degF32))
	}
	mathTable["ceil"] = func(_ RNG, a []float64) float64 { return Round32(math.Ceil(a[0])) }
	// math.clamp tests the UPPER bound FIRST. The order is the whole content
	// of this function, and it is invisible until min > max:
	//
	//	math.clamp(-2, -1, -3)   is -3, not -1
	//
	// with min = -1 and max = -3 the wrong way round. Testing the lower bound
	// first -- the obvious way to write a clamp, and what this package did --
	// answers -1 there and agrees everywhere else, so seven of the eight
	// orderings a test would think to try cannot tell the two apart.
	//
	// Reversed bounds are not a silly case to care about: they are what an
	// author gets from `math.clamp(v.x, v.lo, v.hi)` when the two variables
	// arrive swapped, or when both are computed and one crosses the other.
	// The engine does not repair them, and neither does this.
	mathTable["clamp"] = func(_ RNG, a []float64) float64 {
		v, lo, hi := a[0], a[1], a[2]
		if v > hi {
			return hi
		}
		if v <= lo {
			return lo
		}
		return v
	}
	mathTable["copy_sign"] = func(_ RNG, a []float64) float64 { return Round32(copySign(a[0], a[1])) }
	mathTable["cos"] = func(_ RNG, a []float64) float64 { return f32Cos(a[0]) }
	// RNG-drawing functions: args are already evaluated left-to-right by
	// the caller before this is invoked; draws happen here, exactly once
	// per call for random/random_integer, `num` times in order for the
	// die_roll variants. rng.NextFloat()/NextIntBound's own output is
	// rounded to float32 at first use here (the RNG interface itself
	// stays float64 -- see rng.go -- this is the "store" boundary where a
	// drawn value re-enters the Molang value stack), and every
	// accumulation step of the running sum is rounded too, matching a
	// real engine loop that re-reads/re-writes a float32 accumulator slot
	// each iteration rather than accumulating in double the whole time.
	// NOTE on the iteration count: f32ToInt saturates rather than wrapping,
	// so a nonsense count cannot corrupt the loop -- but it can still be
	// INT32_MAX, and this loop, unlike loop(), has no documented ceiling to
	// clamp to (the "loop() counter maximum is 1024" line in Microsoft's
	// Versioned Changes table is about the loop() statement specifically).
	// Inventing one here would be a semantic guess with nothing behind it,
	// so the count stands as written and `math.die_roll(1e9, ...)` is slow
	// by design rather than by accident.
	mathTable["die_roll"] = func(rng RNG, a []float64) float64 {
		num, low, high := a[0], a[1], a[2]
		n := f32ToInt(math.Round(num))
		sum := 0.0
		span := Round32(high - low)
		for i := 0; i < n; i++ {
			draw := Round32(rng.NextFloat())
			sum = Round32(sum + Round32(low+Round32(draw*span)))
		}
		return sum
	}
	mathTable["die_roll_integer"] = func(rng RNG, a []float64) float64 {
		num, low, high := a[0], a[1], a[2]
		n := f32ToInt(math.Round(num))
		bound := f32ToInt(Round32(Round32(high-low) + 1))
		sum := 0.0
		for i := 0; i < n; i++ {
			sum = Round32(sum + Round32(low+Round32(float64(rng.NextIntBound(bound)))))
		}
		return sum
	}
	mathTable["exp"] = func(_ RNG, a []float64) float64 { return Round32(math.Exp(a[0])) }
	mathTable["floor"] = func(_ RNG, a []float64) float64 { return Round32(math.Floor(a[0])) }
	mathTable["hermite_blend"] = func(_ RNG, a []float64) float64 {
		t := a[0]
		t2 := Round32(t * t)
		term1 := Round32(3 * t2)
		t3 := Round32(t2 * t)
		term2 := Round32(2 * t3)
		return Round32(term1 - term2)
	}
	// inverse_lerp divides WITHOUT the near-zero denominator guard that
	// eval.DivGuardFires applies to the `/` operator, so
	// math.inverse_lerp(5, 5, x) yields +/-Inf (or NaN for x == 5) where
	// `(x-5)/(5-5)` yields 0.
	//
	// CONFIRMED -- this comment previously said INFERRED, reasoning that
	// the guard belongs to the Divide OPCODE rather than to division
	// itself, and the engine now says so directly: the inverse-lerp in the
	// engine's own math library is four instructions ending in a bare
	// float32 divide. No absolute value, no FLT_EPSILON compare, no
	// branch. It genuinely yields +/-Inf on a zero span.
	//
	// The label matters here more than most. A reader who knows what `/`
	// does and finds an unguarded divide two hundred lines away will
	// "correct" it by analogy unless the comment tells them not to. Do not
	// wrap this in DivGuardFires.
	//
	// (The original reasoning, retained because it is still why the two
	// differ: the guard is a separate instruction, emitted into the
	// compiled program right after the denominator, which rewrites the
	// program counter past the numerator when it fires -- see compile.go's
	// ast.Div case. math.inverse_lerp is one entry in the math-function
	// registry, a single native call, with no Divide opcode in its
	// compiled form for that instruction to attach to.)
	mathTable["inverse_lerp"] = func(_ RNG, a []float64) float64 {
		start, end, value := a[0], a[1], a[2]
		num := Round32(value - start)
		den := Round32(end - start)
		return Round32(num / den)
	}
	mathTable["lerp"] = func(_ RNG, a []float64) float64 {
		x, y, t := a[0], a[1], a[2]
		diff := Round32(y - x)
		return Round32(x + Round32(t*diff))
	}
	// lerprotate's `y - x` is a Molang subtraction like any other and must
	// be rounded before it feeds the modulo -- computing it in float64 and
	// rounding only the modulo's result was the one arithmetic step in this
	// file that carried more than single precision into the next operation,
	// contradicting this file's own "rounds after each individual
	// operation" contract. (math.Mod itself is exact in IEEE-754, so the
	// outer Round32 is a no-op once its input is float32-exact; it stays
	// for uniformity with every other entry here.)
	mathTable["lerprotate"] = func(_ RNG, a []float64) float64 {
		x, y, t := a[0], a[1], a[2]
		diff := Round32(math.Mod(Round32(y-x), 360))
		if diff < -180 {
			diff = Round32(diff + 360)
		}
		if diff > 180 {
			diff = Round32(diff - 360)
		}
		return Round32(x + Round32(diff*t))
	}
	mathTable["ln"] = func(_ RNG, a []float64) float64 { return Round32(math.Log(a[0])) }
	// math.max/math.min are RAW ORDERED COMPARES, not fmaxf/fminf and not
	// Go's NaN-propagating math.Max/math.Min.
	//
	// CONFIRMED: the compiled math.max instruction is one ordered compare
	// followed by one conditional select -- literally `(a > b) ? a : b`.
	// The compiled math.min instruction is the same with the less-than
	// predicate: `(a < b) ? a : b`.
	//
	// The consequence is an ASYMMETRY in NaN that matches neither C nor
	// Go. NaN compares unordered, so both predicates are false and the
	// SECOND operand always wins:
	//
	//	max(NaN, x) = x      max(x, NaN) = NaN
	//	min(NaN, x) = x      min(x, NaN) = NaN
	//
	// C's fmaxf/fminf return the non-NaN operand in both orders; Go's
	// math.Max/math.Min (which this used to call) return NaN in both
	// orders. Neither is what the engine does, and only the raw compare
	// reproduces the order-dependence, so it is written out as a compare
	// rather than delegated.
	//
	// No Round32: both operands are already float32-exact by induction,
	// and a select returns one of them unchanged.
	mathTable["max"] = func(_ RNG, a []float64) float64 {
		if a[0] > a[1] {
			return a[0]
		}
		return a[1]
	}
	mathTable["min"] = func(_ RNG, a []float64) float64 {
		if a[0] < a[1] {
			return a[0]
		}
		return a[1]
	}
	// math.min_angle: CONFIRMED to exist, with min=1 max=1 in the engine's
	// compiled math-function registry dump. It was simply absent from this
	// package, so `math.min_angle(x)` was a compile error here while being
	// valid in game -- the same class of gap as the four missing ease
	// families.
	//
	// INFERRED: the formula. min_angle's own implementation was not
	// read. What is implemented is the documented behaviour
	// ("minimize the angle magnitude into the range [-180, 180)"), written
	// as the same modulo-then-wrap this file's lerprotate already uses so
	// the two cannot disagree about what wrapping an angle means. Note
	// this arrangement yields the closed range [-180, 180]: exactly +180
	// stays +180 rather than folding to -180, which is the half-open
	// range's one disagreement with the doc wording and is unverified.
	mathTable["min_angle"] = func(_ RNG, a []float64) float64 {
		d := Round32(math.Mod(a[0], 360))
		if d < -180 {
			d = Round32(d + 360)
		}
		if d > 180 {
			d = Round32(d - 360)
		}
		return d
	}
	// math.mod by zero is 0, not NaN, and not an error.
	//
	// Go's math.Mod returns NaN for a zero divisor, and NaN propagates through
	// everything downstream, so a single `math.mod(x, 0)` anywhere in an
	// expression used to poison the whole result. In game it simply yields 0
	// and the expression carries on.
	//
	// The guard is on the DIVISOR being zero rather than on the result being
	// NaN. Both reproduce every case the language's own tests pin; they differ
	// only for a NaN input, which nothing pins, so the narrower rule is the
	// one to state.
	//
	// The sign of a non-zero result follows the DIVIDEND, as C fmod does:
	// math.mod(-5.1, 3) is -2.1, not 0.9.
	mathTable["mod"] = func(_ RNG, a []float64) float64 {
		if a[1] == 0 {
			return 0
		}
		return Round32(math.Mod(a[0], a[1]))
	}
	mathTable["pow"] = func(_ RNG, a []float64) float64 { return f32Pow(a[0], a[1]) }
	mathTable["random"] = func(rng RNG, a []float64) float64 {
		low, high := a[0], a[1]
		draw := Round32(rng.NextFloat())
		span := Round32(high - low)
		return Round32(low + Round32(draw*span))
	}
	mathTable["random_integer"] = func(rng RNG, a []float64) float64 {
		low, high := a[0], a[1]
		bound := f32ToInt(Round32(Round32(high-low) + 1))
		return Round32(low + Round32(float64(rng.NextIntBound(bound))))
	}
	mathTable["round"] = func(_ RNG, a []float64) float64 { return Round32(math.Round(a[0])) }
	// math.sign is a TWO-way test, not three: zero is positive.
	//
	// math.sign(0) is 1, not 0. This package used to answer 0, preserving the
	// sign of zero the way JavaScript's Math.sign does -- an assumption that
	// came in with the shape of the code rather than from the language being
	// modelled, and one nothing here had reason to question, because a suite
	// written from the outside asks what sign(-3) and sign(3) are and stops.
	//
	// The consequence for an author is not small: `math.sign(x)` never returns
	// 0, so a pack testing `math.sign(v.d) == 0` to mean "no direction" never
	// matches, and one multiplying by math.sign to zero something out gets the
	// value back unchanged instead.
	//
	// NaN takes the same path as zero, since `NaN < 0` is false. That case is
	// not one the language's own tests pin, so it is a consequence of the
	// two-way shape rather than an independently established fact.
	mathTable["sign"] = func(_ RNG, a []float64) float64 {
		if a[0] < 0 {
			return -1
		}
		return 1
	}
	mathTable["sin"] = func(_ RNG, a []float64) float64 { return f32Sin(a[0]) }
	mathTable["sqrt"] = func(_ RNG, a []float64) float64 { return Round32(math.Sqrt(a[0])) }
	mathTable["trunc"] = func(_ RNG, a []float64) float64 { return Round32(math.Trunc(a[0])) }

	for _, shape := range easeShapes {
		for _, variant := range easeVariants {
			curve := easeCurves[shape][variant]
			mathTable["ease_"+variant+"_"+shape] = func(_ RNG, a []float64) float64 {
				start, end, t := a[0], a[1], a[2]
				diff := Round32(end - start)
				return Round32(start + Round32(diff*curve(t)))
			}
		}
	}
}
