package eval

// Round32 rounds v to the nearest float32 and widens the result back to
// float64. This is the package's single rounding primitive, and it is applied
// far more often than a reader coming from float64 arithmetic would expect.
//
// MOLANG IS SINGLE PRECISION, EVERYWHERE. Not "mostly", and not "at the
// boundaries": every value the game computes for an expression is a float32,
// at every step. Literals are stored as float32. Every operator produces a
// float32. Every variable, temp and context read yields one. Every math
// builtin returns one. There is no double anywhere in the path.
//
// So this package rounds at every one of those points, which is what stops a
// value carrying more precision through an expression than the game would.
// Rounding only at the end would not be the same thing: the difference
// compounds through a long expression, and worldgen expressions are long.
//
// Two consequences worth knowing before comparing results against another
// tool:
//
//   - An expression evaluated in double precision and rounded once at the end
//     can differ from this in the last bits, and the gap grows with the number
//     of operations rather than staying put.
//   - The degree-to-radian conversion for math.cos and math.sin is itself done
//     in float32, against a float32 constant, BEFORE the trig call rather than
//     as a float64 factor after it. That one is not a last-bit difference; a
//     port that converts in float64 drifts visibly.
//
// See eval/mathf32.go's header for the one place where matching the game
// cannot be stated absolutely at all -- math.cos, math.sin and math.pow, which
// the game does not compute itself.
func Round32(v float64) float64 {
	return float64(float32(v))
}
