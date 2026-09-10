package eval

import "strings"

// EvalMathPure evaluates a non-RNG-drawing math.<name> function given
// already-constant arguments. It exists for the transform package's
// constant folder (e.g. folding math.floor(3.7) into a literal 3) without
// duplicating the math table. Returns ok=false for the four RNG-drawing
// functions (random, random_integer, die_roll, die_roll_integer), for
// unknown names, and for the wrong number of arguments -- callers should
// leave all of those calls unfolded.
//
// The arity check is not a nicety: mathTable's implementations index args
// positionally, so a call the parser accepts but compileMathCall would
// reject (`math.lerp(1)`) used to panic with an index-out-of-range right
// out of the middle of transform.FoldConstants -- reachable from any
// parse-then-fold-then-print pipeline that never calls eval.Compile at
// all. Checking MathArity here, the same table compileMathCall checks,
// keeps the two agreeing on which calls are even evaluable rather than
// leaving the folder to find out by crashing.
//
// math.pi is deliberately absent from mathTable/MathArity (it is a
// constant, not a function), so it returns ok=false here and the folder
// handles it separately.
func EvalMathPure(name string, args []float64) (result float64, ok bool) {
	name = strings.ToLower(name)
	if RandomFnNames[name] {
		return 0, false
	}
	arity, known := MathArity[name]
	if !known || len(args) != arity {
		return 0, false
	}
	fn, found := mathTable[name]
	if !found {
		return 0, false
	}
	// Round32 mirrors compileMathCall's own defensive rounding of every
	// math.* result, so a folded call and an evaluated one cannot differ
	// even if some future math.* implementation forgets to round.
	return Round32(fn(nil, args)), true
}
