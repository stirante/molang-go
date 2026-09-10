// Package eval compiles a parsed Molang ast.Program into a form that is
// cheap to evaluate repeatedly: a tree of closures built once at Compile
// time, so a hot Run() loop pays no further type-switch/interface-dispatch
// cost per AST node — only the cost of the closure calls themselves.
package eval

// RNG is the source of randomness for math.random, math.random_integer,
// math.die_roll and math.die_roll_integer.
//
// It is always caller-injected — evaluating a Program never reaches for a
// package-level/global source of randomness. This is deliberate: the
// consuming project's whole point is reproducing the game's exact draw
// sequence, so the number and order of draws is part of the contract, not
// an implementation detail hidden inside this library.
type RNG interface {
	// NextFloat returns a value in [0, 1).
	NextFloat() float64
	// NextIntBound returns a value in [0, bound). Implementations should
	// treat bound == 0 as "return 0 without drawing" (matching the
	// engine's own bounded integer draw contract) — but a NEGATIVE bound
	// (math.random_integer/math.die_roll_integer can be called with
	// low > high) should still draw, per that same contract: only exactly
	// zero skips the draw. See molang-go/mtrand.Rand for a reference
	// implementation.
	NextIntBound(bound int) int
}
