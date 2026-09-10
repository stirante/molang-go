package worldgen

import (
	"math"
	"sync"

	"molang-go/mtrand"
)

// query.noise — Bedrock's 2D value-noise Molang query.
//
// The query builds ONE noise generator, once, lazily, and keeps it for the
// life of the process: a single-octave simplex noise seeded with the constant
// below. Both Molang arguments are forwarded straight to it. Two consequences
// worth knowing before using it in a pack:
//
//   - It is completely independent of the world seed. The same coordinates
//     give the same value in every world, on every machine, forever. It is a
//     fixed pattern you can place against, not a source of per-world variety.
//   - It is a single octave, so it has one scale of detail. Layering it with
//     itself at different coordinate scales is on you.
//
// The seed is 2345, read directly out of the game rather than taken from any
// second-hand note: the construction site loads the immediate 0x929 (2345) as
// the generator's seed argument and 1 as its octave count. It is emphatically
// not 12345 — a plausible mis-memory, and the reason this comment now states
// where the number came from.
const noiseSeed = 2345

var f2 = 0.5 * (math.Sqrt(3) - 1)
var g2 = (3 - math.Sqrt(3)) / 6

var grad3 = [12][3]float64{
	{1, 1, 0}, {-1, 1, 0}, {1, -1, 0}, {-1, -1, 0},
	{1, 0, 1}, {-1, 0, 1}, {1, 0, -1}, {-1, 0, -1},
	{0, 1, 1}, {0, -1, 1}, {0, 1, -1}, {0, -1, -1},
}

// fastfloor is the exact truncate-and-adjust idiom the engine uses in
// place of a plain floor: at v == 0 this returns -1, not 0
// (math.Floor(0) would be 0) — reproducing the engine's literal behavior
// deliberately, not "fixing" it.
func fastfloor(v float64) int {
	truncated := math.Trunc(v)
	if v <= 0 {
		return int(truncated) - 1
	}
	return int(truncated)
}

func buildPermutation(seed uint32) [512]byte {
	rng := mtrand.New(seed)
	// Three offset draws (xo/yo/zo) the real constructor makes and stores,
	// unused by the 2D formula below but still consumed here to keep the
	// shuffle's own draws aligned with the real RNG stream.
	rng.NextFloat()
	rng.NextFloat()
	rng.NextFloat()

	var p [512]byte
	for i := 0; i < 256; i++ {
		p[i] = byte(i)
	}
	for i := 0; i < 256; i++ {
		j := i + rng.NextIntBound(256-i)
		p[i], p[j] = p[j], p[i]
		p[i+256] = p[i]
	}
	return p
}

var (
	permOnce  sync.Once
	permTable [512]byte
)

func permutation() [512]byte {
	permOnce.Do(func() {
		permTable = buildPermutation(noiseSeed)
	})
	return permTable
}

func dot2(g [3]float64, x, y float64) float64 {
	return g[0]*x + g[1]*y
}

// simplexNoise2D is the engine's own 2D simplex-noise sample, transcribed
// 1:1 from it -- including its gradient table and its skew constants, so
// the output matches value for value and not merely in character.
func simplexNoise2D(x, y float64) float64 {
	p := permutation()

	s := (x + y) * f2
	i := fastfloor(x + s)
	j := fastfloor(y + s)
	ii := i & 0xff
	jj := j & 0xff

	t := float64(i+j) * g2
	x0 := x - (float64(i) - t)
	y0 := y - (float64(j) - t)

	var i1, j1 int
	if x0 > y0 {
		i1, j1 = 1, 0
	} else {
		i1, j1 = 0, 1
	}

	x1 := x0 - float64(i1) + g2
	y1 := y0 - float64(j1) + g2
	x2 := x0 - 1 + 2*g2
	y2 := y0 - 1 + 2*g2

	n0 := 0.0
	if t0 := 0.5 - x0*x0 - y0*y0; t0 >= 0 {
		gi0 := int(p[(ii+int(p[jj]))&0x1ff]) % 12
		n0 = t0 * t0 * t0 * t0 * dot2(grad3[gi0], x0, y0)
	}

	n1 := 0.0
	if t1 := 0.5 - x1*x1 - y1*y1; t1 >= 0 {
		gi1 := int(p[(ii+i1+int(p[(jj+j1)&0x1ff]))&0x1ff]) % 12
		n1 = t1 * t1 * t1 * t1 * dot2(grad3[gi1], x1, y1)
	}

	n2 := 0.0
	if t2 := 0.5 - x2*x2 - y2*y2; t2 >= 0 {
		gi2 := int(p[(ii+1+int(p[(jj+1)&0x1ff]))&0x1ff]) % 12
		n2 = t2 * t2 * t2 * t2 * dot2(grad3[gi2], x2, y2)
	}

	return 70 * (n0 + n1 + n2)
}

// Noise implements query.noise(x, z).
func Noise(x, z float64) float64 {
	return simplexNoise2D(x, z)
}
