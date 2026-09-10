package eval

import (
	"math"
	"testing"
)

// This file pins the engine's own 65536-entry float32 sine lookup table --
// the one the three sine and three elastic easings index -- as it was
// established against Bedrock 1.26.50.24.
//
// It lives in `package eval` rather than `package eval_test` because the
// table and its index step are exactly what is being pinned, and going
// through `math.ease_*_sine` would let a wrong table hide behind the
// easing's own arithmetic.
//
// Both halves here have been wrong in a shipped version of this file, in
// opposite directions: the CONTENTS were inferred as sin(i*2pi/65536) and
// are actually sinf(float32(i)/K) for the same float32 K the lookup
// multiplies by, and the index step is a TRUNCATION that a port reaches
// for math.Floor to write.

// TestSinTableContentsAreWhatTheInitialiserBuilds pins seven entries
// against literal float32 bit patterns rather than against the formula the
// implementation uses, because a test that recomputes the implementation's
// own expression cannot catch that expression changing.
//
// The engine builds the table at startup, 65536 iterations of "convert
// the index to float32, divide by 10430.3779296875f, take the
// single-precision libm sine". So entry i is
// sinf(float32(i) / 10430.3779296875f).
//
// Two of the seven are the discriminators. Entry 32768 is a half turn:
// under the turn-based inference it is sin(pi) computed at double width
// and narrowed, 1.2246469e-16, while the engine's own division puts the
// argument at float32 pi and sinf answers -8.742278e-08 -- opposite sign,
// nine orders of magnitude larger. Entry 65535 is the last step before the
// wrap, where the 4.0e-8 relative gap between 10430.3779296875 and the exact
// 65536/2pi has had a whole turn to accumulate. The other five agree under
// both rules and are here to hold the endpoints and the quarter turns.
func TestSinTableContentsAreWhatTheInitialiserBuilds(t *testing.T) {
	table := sinTableValues()
	if len(table) != 65536 {
		t.Fatalf("table has %d entries, want 65536", len(table))
	}

	for _, c := range []struct {
		i    int
		bits uint32
		note string
	}{
		{0, 0x00000000, "zero, exactly"},
		{1, 0x38C90FDB, "one step: 9.58738e-05"},
		{16383, 0x3F800000, "one below the quarter turn, already 1.0 in float32"},
		{16384, 0x3F800000, "the quarter turn: 1.0"},
		{32768, 0xB3BBBD2E, "the half turn: -8.742278e-08, NOT the +1.2246469e-16 the turn-based formula gives"},
		{49152, 0xBF800000, "the three-quarter turn: -1.0"},
		{65535, 0xB8C8A221, "the last entry: -9.566942e-05, NOT the -9.58738e-05 the turn-based formula gives"},
	} {
		want := math.Float32frombits(c.bits)
		if got := table[c.i]; got != want {
			t.Errorf("SIN[%d] = %v (0x%08X), want %v (0x%08X) -- %s",
				c.i, got, math.Float32bits(got), want, c.bits, c.note)
		}
	}
}

// TestSinTableIsNotBuiltOnTheExactTurn is the negative of the above: it
// measures the whole table against the formula this file used to carry, so
// a silent revert to `sin(i * 2pi / 65536)` fails with the size of its own
// error rather than with one entry's mismatch.
//
// 43378 of 65536 entries (66.2%) differ, by at most 4.9e-7. That is three
// orders of magnitude SMALLER than the table's quantisation error (~1e-4),
// which is why it went unnoticed; it is corrected because the initialiser
// is readable and nothing has to be assumed.
func TestSinTableIsNotBuiltOnTheExactTurn(t *testing.T) {
	table := sinTableValues()

	differing, worst := 0, 0.0
	for i, got := range table {
		turnBased := float32(math.Sin(float64(i) * 2 * math.Pi / 65536))
		if got == turnBased {
			continue
		}
		differing++
		if d := math.Abs(float64(got) - float64(turnBased)); d > worst {
			worst = d
		}
	}

	if differing != 43378 {
		t.Errorf("%d entries differ from sin(i*2pi/65536), want 43378; the table is not being built the way the initialiser builds it", differing)
	}
	if worst == 0 {
		t.Errorf("the table is identical to sin(i*2pi/65536); the correction has been lost")
	}
	if worst > 5e-7 {
		t.Errorf("worst disagreement with the turn-based formula is %.4g, want at most 4.9e-7", worst)
	}
}

// TestSinTableIndexTruncatesTowardZero is the case truncation and floor
// disagree on. The engine truncates toward zero, so a NEGATIVE scaled
// index picks the entry NEARER zero -- -10.43 becomes -10, not -11 -- and
// the 32-bit mask then reads it from the far end of the table.
//
// Sampling negative angles on a grid that mostly misses integers, the two
// rules pick different entries 98.5% of the time, and the entries they
// pick differ by up to 9.6e-5 -- three orders of magnitude more than the
// contents correction above. Every probe below is chosen where they
// differ, and each asserts the truncating answer AND that the flooring
// answer is not it, so neither half can pass by accident.
func TestSinTableIndexTruncatesTowardZero(t *testing.T) {
	table := sinTableValues()

	floorSin := func(xRad float64) float64 {
		idx := float32(xRad) * sinTableScale
		return float64(table[uint32(int32(math.Floor(float64(idx))))&sinTableMask])
	}

	for _, c := range []struct {
		x    float64
		bits uint32
	}{
		// -1 rad: index -10430.378 -> -10430 (entry 55106), not -10431
		// (entry 55105, which reads -0.8415031).
		{-1, 0xBF57695A},
		// -0.5 rad: index -5215.189 -> -5215 (entry 60321), not -5216.
		{-0.5, 0xBEF57529},
		// -0.001 rad, the worst case found by sweeping: index -10.430378
		// -> -10 (entry 65526, -0.00095826772). Flooring reads entry
		// 65525, -0.0010545887 -- 9.6e-5 away, and 10% wrong in relative
		// terms this close to zero.
		{-0.001, 0xBA7B3442},
	} {
		want := float64(math.Float32frombits(c.bits))
		if got := tableSin(c.x); got != want {
			t.Errorf("tableSin(%v) = %v, want %v (truncation toward zero)", c.x, got, want)
		}
		if floorSin(c.x) == want {
			t.Errorf("tableSin(%v): flooring reads the same entry, so this probe proves nothing", c.x)
		}
	}

	// And through a curve, so the difference is live in Molang rather
	// than only in the helper. ease_in_elastic's table argument is
	// (10t - 10.75) * c4, negative for every t below 1.075; at t = 0.05
	// the scaled index is -223914.66, where truncation reads -0.50005555
	// and flooring reads -0.49997252.
	const probe = 0.05
	arg := fmul(fsub(fmul(10, probe), 10.75), elasticC4)
	want := -fmul(fpow2(fsub(fmul(10, probe), 10)), tableSin(arg))
	if got := easeCurves["elastic"]["in"](probe); got != want {
		t.Errorf("ease_in_elastic curve at %v = %v, want %v", probe, got, want)
	}
	if tableSin(arg) == floorSin(arg) {
		t.Errorf("ease_in_elastic's t=%v table read does not distinguish truncation from floor", probe)
	}
}

// TestSinTableCosineOffsetIsAppliedToTheScaledIndex pins the ORDER of the
// cosine offset: multiply by 10430.3779296875, then add 16384.0f, then
// truncate. The
// natural alternative -- truncate first and add 16384 to the integer -- is
// identical for a positive angle and differs for a negative one, because
// adding the quarter turn first carries the value back across zero and
// turns a round-toward-zero into a round-down.
func TestSinTableCosineOffsetIsAppliedToTheScaledIndex(t *testing.T) {
	table := sinTableValues()

	// -0.001 rad: scaled index -10.430378. Adding first gives
	// trunc(16373.5696) = 16373; truncating first gives -10 + 16384 =
	// 16374.
	const x = -0.001
	scaled := float32(x) * sinTableScale

	want := float64(table[uint32(int32(scaled+sinTableQuarterTurn))&sinTableMask])
	truncateFirst := float64(table[uint32(int32(scaled)+sinTableQuarterTurn)&sinTableMask])
	if truncateFirst == want {
		t.Fatalf("the two orders agree at x = %v; this probe proves nothing", x)
	}
	if got := tableCos(x); got != want {
		t.Errorf("tableCos(%v) = %v, want %v (offset added to the scaled index, before the truncation)", x, got, want)
	}

	// The offset really is a quarter turn: cos(0) is the table's 1.0.
	if got := tableCos(0); got != 1 {
		t.Errorf("tableCos(0) = %v, want 1", got)
	}
}
