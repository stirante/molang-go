// Package mtrand is a byte-exact port of the engine's core RNG, an
// MT19937-family generator. Confirmed against the engine end to end: the
// constructor's seeding constant 1812433253, the per-call twist+temper
// formula, and every integer, bounded-integer and float output wrapper.
// Where this port deliberately differs in form but not in output, the
// method's own comment says so and why.
//
// It exists for two reasons:
//   - query.noise's internal permutation shuffle (molang-go/worldgen) uses
//     this exact generator, seeded with a hardcoded literal (2345),
//     independent of whatever RNG a caller injects for math.random et al.
//   - It is a convenient, fully deterministic, reproducible eval.RNG
//     implementation for testing: seeded once, Rand always produces the
//     same draw sequence, which is exactly the "scripted deterministic
//     RNG stub" the differential harness needs so both sides of a
//     comparison consume identical draws.
//
// Rand is not used by core Molang evaluation itself — molang-go/eval never
// reaches for a package-level RNG; every consumer, including this one,
// injects it explicitly.
package mtrand

const (
	n                = 624
	m                = 397
	matrixA   uint32 = 0x9908b0df
	upperMask uint32 = 0x80000000
	lowerMask uint32 = 0x7fffffff
	inv2_32          = 2.3283064365386963e-10
)

// Rand is the engine's core RNG.
type Rand struct {
	mt  [n]uint32
	mti int
}

// New returns a Rand seeded with seed, ready to draw from.
func New(seed uint32) *Rand {
	r := &Rand{}
	r.SetSeed(seed)
	return r
}

// SetSeed re-seeds the generator, matching the core RNG's constructor: an
// eager, textbook init_genrand fill.
//
// The engine's own constructor fills only the first 398 words eagerly and
// generates the rest lazily on first use. That is the same state by the
// time anything can observe it: the lazy words are produced by this exact
// recurrence, in this exact order, before the first draw that would read
// them. Filling all n up front is therefore the same generator in a
// simpler form, not an approximation of it.
func (r *Rand) SetSeed(seed uint32) {
	s := seed
	r.mt[0] = s
	for i := 1; i < n; i++ {
		s = 1812433253*(s^(s>>30)) + uint32(i)
		r.mt[i] = s
	}
	r.mti = n
}

// NextUint32 is the core RNG's raw 32-bit draw: the twisted, tempered
// value every other method is built from.
func (r *Rand) NextUint32() uint32 {
	if r.mti >= n {
		var kk int
		for kk = 0; kk < n-m; kk++ {
			y := (r.mt[kk] & upperMask) | (r.mt[kk+1] & lowerMask)
			var mag uint32
			if y&1 != 0 {
				mag = matrixA
			}
			r.mt[kk] = r.mt[kk+m] ^ (y >> 1) ^ mag
		}
		for ; kk < n-1; kk++ {
			y := (r.mt[kk] & upperMask) | (r.mt[kk+1] & lowerMask)
			var mag uint32
			if y&1 != 0 {
				mag = matrixA
			}
			r.mt[kk] = r.mt[kk+(m-n)] ^ (y >> 1) ^ mag
		}
		y := (r.mt[n-1] & upperMask) | (r.mt[0] & lowerMask)
		var mag uint32
		if y&1 != 0 {
			mag = matrixA
		}
		r.mt[n-1] = r.mt[m-1] ^ (y >> 1) ^ mag
		r.mti = 0
	}
	y := r.mt[r.mti]
	r.mti++
	y ^= y >> 11
	y ^= (y << 7) & 0x9d2c5680
	y ^= (y << 15) & 0xefc60000
	y ^= y >> 18
	return y
}

// NextInt is the engine's unbounded integer draw: [0, 2^31 - 1].
func (r *Rand) NextInt() int32 {
	return int32(r.NextUint32() >> 1)
}

// NextIntBound is the engine's bounded integer draw: [0, bound), plain
// modulo (no rejection sampling). bound == 0 returns 0 WITHOUT drawing
// (confirmed behaviour — an earlier engine version raised instead, which
// aborted the whole placement; the current one just carries on). A
// negative bound only trips a non-fatal assert in the real engine and then
// falls through to the same modulo with the bound reinterpreted as
// unsigned, so that case is mirrored here (and still draws) rather than
// also special-cased to 0.
func (r *Rand) NextIntBound(bound int) int {
	if bound == 0 {
		return 0
	}
	return int(r.NextUint32() % uint32(bound))
}

// NextFloat is the engine's float draw: a single draw, * 2^-32 — matches
// eval.RNG's contract of a value in [0, 1).
func (r *Rand) NextFloat() float64 {
	return float64(r.NextUint32()) * inv2_32
}
