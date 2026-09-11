package eval_test

import (
	"math"
	"testing"

	"github.com/stirante/molang-go/eval"
	"github.com/stirante/molang-go/parser"
)

func compile(t *testing.T, src string) (*eval.Program, error) {
	t.Helper()
	tree, err := parser.Parse(src)
	if err != nil {
		t.Fatalf("parse(%q): %v", src, err)
	}
	return eval.Compile(tree)
}

// TestAddRoundsToFloat32 pins the property this package's Round32 pass
// exists for: `+` computes in float32, not float64. 0.1+0.2 is the classic
// case where the two precisions produce genuinely different results --
// float64 gives 0.30000000000000004; float32 gives a different value,
// 0.30000001192092896 once widened back to float64, because float32's
// 24-bit mantissa rounds 0.1 and
// 0.2 differently than float64's 53-bit mantissa does before the add ever
// happens. If eval/compile.go's Add case stopped calling Round32 (e.g.
// reverted to plain `xFn(ctx) + yFn(ctx)`), this test fails immediately --
// it was run red against exactly that change and green again after
// reverting it, as part of the float32 migration's own verification.
func TestAddRoundsToFloat32(t *testing.T) {
	p, err := compile(t, "0.1+0.2")
	if err != nil {
		t.Fatal(err)
	}
	got := p.Run(&eval.Context{Scope: eval.NewScope()})

	wantFloat32 := eval.Round32(eval.Round32(0.1) + eval.Round32(0.2))
	wantFloat64 := 0.1 + 0.2 // the double-precision answer, for contrast

	if wantFloat32 == wantFloat64 {
		t.Fatalf("test setup is broken: float32 and float64 0.1+0.2 unexpectedly coincide (%v)", wantFloat32)
	}
	if got != wantFloat32 {
		t.Errorf("0.1+0.2 = %.20g, want the float32 result %.20g (double-precision would give %.20g)", got, wantFloat32, wantFloat64)
	}
}

// TestDivideNearZeroGuard pins the engine's Divide guard instruction (the
// variant used for Molang version > 6): a division whose denominator has
// |d| < 2^-23 (FLT_EPSILON, confirmed as the float32 threshold the guard
// compares against) evaluates to +0.0 --
// it does NOT produce ±Inf or a huge finite quotient -- and the numerator
// is never evaluated (the guard jumps the program counter past it), so
// numerator-side math.random draws are not consumed. Denominators at or
// above the threshold divide normally. Verified red against the previous
// plain `Round32(x/y)` implementation and green with the guard.
func TestDivideNearZeroGuard(t *testing.T) {
	cases := []struct {
		src  string
		want float64
	}{
		{"1/0", 0},
		{"-5/0", 0},
		{"1/-0", 0},
		{"3/0.00000005", 0},                      // 5e-8 < 2^-23: guard fires
		{"1/0.00000011920928955078125", 8388608}, // exactly 2^-23: guard does NOT fire, 1/2^-23 = 2^23
		{"-2/0.00000011920928955078125", -16777216}, // -2/2^-23
		{"10/4", 2.5}, // sanity: ordinary division intact
	}
	for _, c := range cases {
		p, err := compile(t, c.src)
		if err != nil {
			t.Fatalf("compile(%q): %v", c.src, err)
		}
		if got := p.Run(&eval.Context{Scope: eval.NewScope()}); got != c.want {
			t.Errorf("%s = %v, want %v", c.src, got, c.want)
		}
	}
}

func TestCompileErrors(t *testing.T) {
	cases := []string{
		"math.sin",       // bare math reference must be called
		"math.sin(1, 2)", // wrong arity
		"math.nope(1)",   // unknown math function
		"math.pi(1)",     // math.pi takes no arguments
	}
	for _, src := range cases {
		if _, err := compile(t, src); err == nil {
			t.Errorf("Compile(%q): expected error, got none", src)
		}
	}
}

func TestCompileAccepts(t *testing.T) {
	cases := []string{
		"math.pi",
		"math.pi()",
		"math.sin(30)",
	}
	for _, src := range cases {
		if _, err := compile(t, src); err != nil {
			t.Errorf("Compile(%q): unexpected error: %v", src, err)
		}
	}
}

type seqRNG struct {
	floats []float64
	fi     int
}

func (s *seqRNG) NextFloat() float64 {
	v := s.floats[s.fi%len(s.floats)]
	s.fi++
	return v
}
func (s *seqRNG) NextIntBound(bound int) int {
	if bound <= 0 {
		return 0
	}
	return s.fi % bound
}

func TestQueryFuncExtension(t *testing.T) {
	tree, err := parser.Parse("query.noise(1, 2) + query.noise(3, 4)")
	if err != nil {
		t.Fatal(err)
	}
	p, err := eval.Compile(tree)
	if err != nil {
		t.Fatal(err)
	}
	calls := 0
	ctx := &eval.Context{
		RNG:   &seqRNG{floats: []float64{0.1}},
		Scope: eval.NewScope(),
		QueryFuncs: map[string]eval.QueryFunc{
			"noise": func(args []float64, ctx *eval.Context) float64 {
				calls++
				return args[0] + args[1]
			},
		},
	}
	got := p.Run(ctx)
	if got != (1+2)+(3+4) {
		t.Errorf("registered query func result = %v, want %v", got, (1+2)+(3+4))
	}
	if calls != 2 {
		t.Errorf("registered query func calls = %d, want 2", calls)
	}
}

func TestUnregisteredQueryCallDiscards(t *testing.T) {
	tree, err := parser.Parse("query.unknown_thing(math.random(0,1), math.random(0,1))")
	if err != nil {
		t.Fatal(err)
	}
	p, err := eval.Compile(tree)
	if err != nil {
		t.Fatal(err)
	}
	rng := &seqRNG{floats: []float64{0.5}}
	ctx := &eval.Context{RNG: rng, Scope: eval.NewScope()}
	got := p.Run(ctx)
	if got != 0 {
		t.Errorf("call-discard result = %v, want 0", got)
	}
	if rng.fi != 2 {
		t.Errorf("draws consumed = %d, want 2 (args must still be evaluated in order)", rng.fi)
	}
}

// TestLoopCounterMax pins this package's loop() iteration ceiling.
//
// Read eval.LoopCounterMax's doc comment before touching this: the 1024
// cap is now CONFIRMED to be a DIVERGENCE from the engine, not a model of
// it -- Bedrock 1.26.50.24 has no loop cap at all. The cap is kept as host
// protection (this module's consumer previews arbitrary third-party packs,
// where a `loop(v.big, ...)` typo hanging the tool is worse than a wrong
// number), and this test pins that policy, not engine behaviour.
//
// Every case seeds t.n in-source. It has to: an unwritten temp slot is an
// unresolved read, which aborts the program before the loop runs at all
// (see eval/unresolved.go).
func TestLoopCounterMax(t *testing.T) {
	cases := []struct {
		src  string
		want float64
	}{
		{"t.n=0;loop(3,{t.n=t.n+1;});return t.n;", 3},
		{"t.n=0;loop(1024,{t.n=t.n+1;});return t.n;", 1024},
		{"t.n=0;loop(1025,{t.n=t.n+1;});return t.n;", eval.LoopCounterMax},
		{"t.n=0;loop(1000000,{t.n=t.n+1;});return t.n;", eval.LoopCounterMax},
		// A count that is not a whole number truncates toward zero, as it
		// always did -- the cap changes nothing below it.
		{"t.n=0;loop(3.9,{t.n=t.n+1;});return t.n;", 3},
		// Non-positive and non-finite counts run the body zero times.
		// `int(NaN)` in Go is explicitly undefined, so this case used to
		// be platform-dependent rather than merely unspecified.
		{"t.n=0;loop(0,{t.n=t.n+1;});return t.n;", 0},
		{"t.n=0;loop(-5,{t.n=t.n+1;});return t.n;", 0},
		{"t.n=0;loop(0.5,{t.n=t.n+1;});return t.n;", 0},
		{"t.n=0;loop(math.sqrt(-1),{t.n=t.n+1;});return t.n;", 0},
	}
	for _, c := range cases {
		p, err := compile(t, c.src)
		if err != nil {
			t.Fatalf("compile(%q): %v", c.src, err)
		}
		if got := p.Run(&eval.Context{Scope: eval.NewScope()}); got != c.want {
			t.Errorf("%s = %v, want %v", c.src, got, c.want)
		}
	}
}

// TestInverseLerpZeroSpanIsUnguarded pins the deliberate asymmetry between
// math.inverse_lerp's divide and the `/` operator's: the operator's
// near-zero-denominator guard is an instruction the Divide OPCODE emits,
// and inverse_lerp is a single native math-registry call with no Divide
// opcode in it, so it divides plainly.
//
// CONFIRMED (this comment said INFERRED until the engine was read): the
// inverse-lerp in the engine's own math library is four instructions
// ending in a bare float32 divide -- no absolute value, no FLT_EPSILON
// compare, no branch.
// The test exists so that if someone "fixes" inverse_lerp by analogy with
// `/`, they have to come here and read why it is that way first.
func TestInverseLerpZeroSpanIsUnguarded(t *testing.T) {
	cases := []struct {
		src   string
		check func(float64) bool
		want  string
	}{
		{"math.inverse_lerp(5,5,9)", func(v float64) bool { return math.IsInf(v, 1) }, "+Inf"},
		{"math.inverse_lerp(5,5,1)", func(v float64) bool { return math.IsInf(v, -1) }, "-Inf"},
		{"math.inverse_lerp(5,5,5)", math.IsNaN, "NaN"},
		// The operator, on identical numbers, does guard.
		{"(9-5)/(5-5)", func(v float64) bool { return v == 0 }, "0"},
		// Sanity: an ordinary span is unaffected.
		{"math.inverse_lerp(0,10,2.5)", func(v float64) bool { return v == 0.25 }, "0.25"},
	}
	for _, c := range cases {
		p, err := compile(t, c.src)
		if err != nil {
			t.Fatalf("compile(%q): %v", c.src, err)
		}
		if got := p.Run(&eval.Context{Scope: eval.NewScope()}); !c.check(got) {
			t.Errorf("%s = %v, want %s", c.src, got, c.want)
		}
	}
}

// TestLerpRotateRoundsItsSubtraction pins that lerprotate's `y - x` is
// rounded to float32 BEFORE the modulo, like every other arithmetic step in
// eval/math.go. The inputs below are chosen so the two orderings differ:
// 16777217 is the first integer float32 cannot represent (it rounds to
// 16777216), so a float64 `y - x` carries a value into math.Mod that no
// float32 Molang value could ever hold.
func TestLerpRotateRoundsItsSubtraction(t *testing.T) {
	const src = "math.lerprotate(0.1,16777217,1)"
	p, err := compile(t, src)
	if err != nil {
		t.Fatalf("compile(%q): %v", src, err)
	}
	got := p.Run(&eval.Context{Scope: eval.NewScope()})

	x, y := eval.Round32(0.1), eval.Round32(16777217)
	want := eval.Round32(math.Mod(eval.Round32(y-x), 360))
	for want < -180 {
		want = eval.Round32(want + 360)
	}
	for want > 180 {
		want = eval.Round32(want - 360)
	}
	want = eval.Round32(x + eval.Round32(want*1))

	unrounded := math.Mod(y-x, 360)
	if unrounded == eval.Round32(y-x) {
		t.Fatalf("test premise is stale: the two orderings no longer differ for these inputs")
	}
	if got != want {
		t.Errorf("%s = %v, want %v (float64 y-x would give a different modulo input, %v)", src, got, want, unrounded)
	}
}

// TestTrailingSemicolonMakesASequence pins ast.Program.HasSemicolon's one
// job: `1+1` is a bare expression worth 2, `1+1;` is a one-statement
// sequence worth 0. The field was set by the parser and read by nobody, so
// both used to be 2 -- the documented rule and the code disagreed, and only
// the comment said so.
//
// INFERRED, not confirmed against the game: see ast.Program.HasSemicolon.
func TestTrailingSemicolonMakesASequence(t *testing.T) {
	cases := []struct {
		src  string
		want float64
	}{
		{"1+1", 2},
		{"1+1;", 0},
		{"temp.a=5", 5},
		{"temp.a=5;", 0},
		{"return 1+1;", 2},
		{"return temp.a=5;", 5},
		{"temp.a=5;return temp.a;", 5},
		// A bare `{ ... }` grouping block has no top-level ';' and keeps
		// bare-expression semantics, which is what the real terraform
		// expressions in the corpus rely on.
		{"{return 7;}", 7},
		{"{temp.a=5;}", 0},
	}
	for _, c := range cases {
		p, err := compile(t, c.src)
		if err != nil {
			t.Fatalf("compile(%q): %v", c.src, err)
		}
		if got := p.Run(&eval.Context{Scope: eval.NewScope()}); got != c.want {
			t.Errorf("%s = %v, want %v", c.src, got, c.want)
		}
	}
}

// TestTrailingSemicolonSideEffectsStillHappen: the sequence rule changes
// the program's VALUE, not whether its statements run. `temp.a=5;` yields 0
// and still assigns 5.
func TestTrailingSemicolonSideEffectsStillHappen(t *testing.T) {
	p, err := compile(t, "temp.a=5;")
	if err != nil {
		t.Fatalf("compile: %v", err)
	}
	ctx := &eval.Context{Scope: eval.NewScope()}
	if got := p.Run(ctx); got != 0 {
		t.Errorf("value = %v, want 0", got)
	}
	if got := ctx.Scope.Temp["a"]; got != 5 {
		t.Errorf("temp.a = %v, want 5 (the assignment must still happen)", got)
	}
}
