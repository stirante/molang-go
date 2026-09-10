package eval

import "math"

// f32Cos, f32Sin and f32Pow back math.cos, math.sin and math.pow.
//
// These three are the only place in this package where "matches the game"
// cannot be made an absolute claim.
//
// THE GAME ITSELF IS NOT BIT-DETERMINISTIC HERE. Its cos, sin and pow are not
// computed in the game at all - the calls resolve to the C library of
// whatever platform it is running on. On a modern 64-bit Android device that
// is bionic's libm, which links ARM's optimized-routines implementations
// (external/arm-optimized-routines: math/cosf.c, math/sinf.c, math/powf.c,
// plus their data tables). Older Android builds shipped the freebsd-msun
// versions instead, and Windows resolves the same calls to MSVC's UCRT. So
// the same expression can differ in the last bit between a phone and a PC,
// and no single implementation here could match all of them at once.
//
// How far apart they can be, precisely: ARM's own comments give worst-case
// errors of 0.5607 ULP for sinf and cosf and 0.82 ULP for powf -- they are
// not correctly rounded. This package computes in float64 and rounds once,
// which IS the correctly-rounded float32 result except in astronomically
// rare double-rounding cases. The two therefore agree bit for bit EXCEPT
// where that sub-ULP error crosses a float32 rounding boundary, and there the
// disagreement is exactly one float32 ULP, never more.
//
// Correct rounding is kept deliberately as the canonical answer rather than
// chasing one device family's deviations. No expression has been observed to
// hit a boundary case, though no exhaustive scan has been run either; if a
// divergence is ever traced here, a bit-exact port of those polynomials and
// tables is the fix, and it is tractable -- roughly 200 lines plus data.
//
// The part that IS exact: the degree-to-radian multiply for cos and sin is
// done in genuine float32 against the constant the game uses, before the
// library call, rather than as a float64 factor applied after. That is where
// a naive port drifts first, and it drifts by much more than one ULP.

func f32Cos(xDeg float64) float64 {
	rad := float32(xDeg) * deg2radF32
	return Round32(math.Cos(float64(rad)))
}

func f32Sin(xDeg float64) float64 {
	rad := float32(xDeg) * deg2radF32
	return Round32(math.Sin(float64(rad)))
}

func f32Pow(x, y float64) float64 {
	return Round32(math.Pow(float64(float32(x)), float64(float32(y))))
}
