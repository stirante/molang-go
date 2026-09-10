// library_test.go is how THIS LIBRARY behaves, as distinct from what Molang
// means. See language_test.go's header for the split across all four files.
//
// Everything here is about a decision this package made and a caller can see:
// the Context options that change what an unresolved read does, the injected
// RNG and exactly when it is drawn from, and what gets reported to a host
// rather than silently swallowed. A different Molang implementation could be
// entirely correct and fail every test in this file.
//
// The shared fixtures live here too -- the scripted RNG, the context builder
// and evalOK -- because this is the file about the harness rather than about
// the language.
package molang

import (
	"testing"

	"molang-go/eval"
)

type stubRNG struct {
	floats []float64
	fi     int
	ints   []int
	ii     int
}

func (s *stubRNG) NextFloat() float64 {
	v := s.floats[s.fi%len(s.floats)]
	s.fi++
	return v
}

func (s *stubRNG) NextIntBound(bound int) int {
	if bound <= 0 {
		return 0
	}
	v := s.ints[s.ii%len(s.ints)] % bound
	s.ii++
	return v
}

func newCtx() *Context {
	return &Context{
		RNG:   &stubRNG{floats: []float64{0.25, 0.5, 0.75}, ints: []int{1, 2, 3}},
		Scope: NewScope(),
	}
}

func evalOK(t *testing.T, src string) float64 {
	t.Helper()
	v, err := Eval(src, newCtx())
	if err != nil {
		t.Fatalf("Eval(%q) error: %v", src, err)
	}
	return v
}

// TestNullCoalesceCatchesUnresolvedRead is the behaviour the corpus needs:
// the `t.x = t.x ?? 0.35` idiom eight golden expressions are written
// around. The key distinction is the last case -- a RESOLVED zero does not
// divert, so this cannot be implemented as "falsy takes the right side".
func TestNullCoalesceCatchesUnresolvedRead(t *testing.T) {
	if got := evalOK(t, "v.unset ?? 5"); got != 5 {
		t.Errorf("v.unset ?? 5 = %v, want 5", got)
	}
	want035 := eval.Round32(0.35)
	if got := evalOK(t, "t.unset ?? 0.35"); got != want035 {
		t.Errorf("t.unset ?? 0.35 = %v, want %v", got, want035)
	}
	if got := evalOK(t, "v.x = 0; return v.x ?? 5;"); got != 0 {
		t.Errorf("a resolved zero must NOT divert: got %v, want 0", got)
	}
	if got := evalOK(t, "t.x = t.x ?? 0.35; return t.x;"); got != want035 {
		t.Errorf("t.x = t.x ?? 0.35 = %v, want %v", got, want035)
	}
}

// TestUnresolvedReadAbortsProgram pins the half of the unresolved-read
// mechanism that is easy to lose while implementing '??': with no
// enclosing '??' to catch it, an unresolved read ENDS the program. The
// result is 0 either way -- what the test actually watches is the RNG,
// because a draw sequenced after the failed read must not happen.

// TestUnresolvedReadAbortsProgram pins the half of the unresolved-read
// mechanism that is easy to lose while implementing '??': with no
// enclosing '??' to catch it, an unresolved read ENDS the program. The
// result is 0 either way -- what the test actually watches is the RNG,
// because a draw sequenced after the failed read must not happen.
func TestUnresolvedReadAbortsProgram(t *testing.T) {
	ctx := newCtx()
	p, err := Compile("v.missing; return math.random(0,1);")
	if err != nil {
		t.Fatal(err)
	}
	if got := p.Run(ctx); got != 0 {
		t.Errorf("aborted program = %v, want 0", got)
	}
	if drawn := ctx.RNG.(*stubRNG).fi; drawn != 0 {
		t.Errorf("draws after the failed read = %d, want 0", drawn)
	}

	// The abort blows through sibling subexpressions too, not just
	// trailing statements: the engine's program counter goes to -1, and
	// everything the flat instruction list still held is skipped.
	ctx2 := newCtx()
	p2, err := Compile("v.missing + math.random(0,1)")
	if err != nil {
		t.Fatal(err)
	}
	if got := p2.Run(ctx2); got != 0 {
		t.Errorf("aborted expression = %v, want 0", got)
	}
	if drawn := ctx2.RNG.(*stubRNG).fi; drawn != 0 {
		t.Errorf("draws after the failed read = %d, want 0", drawn)
	}

	// And a '??' catch frame covers its LHS only: once it has been used,
	// an unresolved read in the RHS has nothing left to catch it.
	ctx3 := newCtx()
	p3, err := Compile("v.a ?? v.b")
	if err != nil {
		t.Fatal(err)
	}
	if got := p3.Run(ctx3); got != 0 {
		t.Errorf("v.a ?? v.b (both unset) = %v, want 0", got)
	}
}

// TestContinueOnUnresolvedRead pins the opt-out (Context.
// ContinueOnUnresolvedRead, see eval/unresolved.go under "Opting out"): a
// host that cannot supply the scope a read expects can ask for the read to
// yield 0 and evaluation to CARRY ON, instead of ending the program. What
// it must not change is anything else -- so the same three cases
// TestUnresolvedReadAbortsProgram watches are watched here from the other
// side, including the RNG.

// TestContinueOnUnresolvedRead pins the opt-out (Context.
// ContinueOnUnresolvedRead, see eval/unresolved.go under "Opting out"): a
// host that cannot supply the scope a read expects can ask for the read to
// yield 0 and evaluation to CARRY ON, instead of ending the program. What
// it must not change is anything else -- so the same three cases
// TestUnresolvedReadAbortsProgram watches are watched here from the other
// side, including the RNG.
func TestContinueOnUnresolvedRead(t *testing.T) {
	ctx := newCtx()
	ctx.ContinueOnUnresolvedRead = true
	p, err := Compile("v.missing; t.after = 7; return math.random(0,1);")
	if err != nil {
		t.Fatal(err)
	}
	// 0.25 is the stub's first float; math.random(0,1) returns it directly.
	if got := p.Run(ctx); got != eval.Round32(0.25) {
		t.Errorf("continued program = %v, want the draw (0.25)", got)
	}
	if drawn := ctx.RNG.(*stubRNG).fi; drawn != 1 {
		t.Errorf("draws after the swallowed read = %d, want 1", drawn)
	}
	if got, ok := ctx.Scope.Temp["after"]; !ok || got != 7 {
		t.Errorf("assignment after the swallowed read = %v (present=%v), want 7", got, ok)
	}

	// Sibling subexpressions too, not just trailing statements.
	ctx2 := newCtx()
	ctx2.ContinueOnUnresolvedRead = true
	p2, err := Compile("v.missing + math.random(0,1)")
	if err != nil {
		t.Fatal(err)
	}
	if got := p2.Run(ctx2); got != eval.Round32(0.25) {
		t.Errorf("v.missing + math.random(0,1) = %v, want 0 + 0.25", got)
	}
	if drawn := ctx2.RNG.(*stubRNG).fi; drawn != 1 {
		t.Errorf("draws beside the swallowed read = %d, want 1", drawn)
	}
}

// TestContinueOnUnresolvedReadLeavesNullCoalesceAlone is the half of the
// opt-out that is easiest to get wrong: `??` is a separate mechanism (a
// catch frame, not a policy) and must behave IDENTICALLY under both
// settings. The naive implementation of the option -- "when opted out,
// don't raise the sentinel" -- fails the first case here, because the LHS
// then completes normally with the value 0 and `v.unset ?? 5` becomes 0.

// TestContinueOnUnresolvedReadLeavesNullCoalesceAlone is the half of the
// opt-out that is easiest to get wrong: `??` is a separate mechanism (a
// catch frame, not a policy) and must behave IDENTICALLY under both
// settings. The naive implementation of the option -- "when opted out,
// don't raise the sentinel" -- fails the first case here, because the LHS
// then completes normally with the value 0 and `v.unset ?? 5` becomes 0.
func TestContinueOnUnresolvedReadLeavesNullCoalesceAlone(t *testing.T) {
	run := func(src string) float64 {
		t.Helper()
		ctx := newCtx()
		ctx.ContinueOnUnresolvedRead = true
		p, err := Compile(src)
		if err != nil {
			t.Fatal(err)
		}
		return p.Run(ctx)
	}
	if got := run("v.unset ?? 5"); got != 5 {
		t.Errorf("v.unset ?? 5 (opted out) = %v, want 5", got)
	}
	if got := run("v.x = 0; return v.x ?? 5;"); got != 0 {
		t.Errorf("a resolved zero must NOT divert, opted out or not: got %v, want 0", got)
	}
	if got := run("t.x = t.x ?? 0.35; return t.x;"); got != eval.Round32(0.35) {
		t.Errorf("t.x = t.x ?? 0.35 (opted out) = %v, want 0.35", got)
	}
	// The catch frame covers the LHS only. Opting out changes what happens
	// to the RHS read afterwards (it is swallowed rather than aborting),
	// not whether the frame is still open when it runs -- so the RHS read
	// yields its own 0 either way.
	if got := run("v.a ?? v.b"); got != 0 {
		t.Errorf("v.a ?? v.b (both unset, opted out) = %v, want 0", got)
	}
}

// TestOnUnresolvedReadReportsOnlyUncaughtReads pins what the host is told.
// The callback exists so an opted-out host can say what it swallowed, and
// it reports exactly the reads the engine content-logs about: the ones
// nothing catches. A read a `??` diverts is ordinary control flow and is
// NOT reported -- reporting it would make the idiom every real pack writes
// (`t.x = t.x ?? 0.35`) look like a problem.

// TestOnUnresolvedReadReportsOnlyUncaughtReads pins what the host is told.
// The callback exists so an opted-out host can say what it swallowed, and
// it reports exactly the reads the engine content-logs about: the ones
// nothing catches. A read a `??` diverts is ordinary control flow and is
// NOT reported -- reporting it would make the idiom every real pack writes
// (`t.x = t.x ?? 0.35`) look like a problem.
func TestOnUnresolvedReadReportsOnlyUncaughtReads(t *testing.T) {
	var got []string
	ctx := newCtx()
	ctx.ContinueOnUnresolvedRead = true
	ctx.OnUnresolvedRead = func(name string) { got = append(got, name) }
	p, err := Compile("t.guarded = t.guarded ?? 1; return v.missing + t.other;")
	if err != nil {
		t.Fatal(err)
	}
	p.Run(ctx)
	want := []string{"variable.missing", "temp.other"}
	if len(got) != len(want) {
		t.Fatalf("reported %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("reported %v, want %v", got, want)
		}
	}

	// It fires under the DEFAULT too -- the read that ends the program is
	// the same read the engine logs -- and there only once, because the
	// abort means nothing after it runs.
	var aborted []string
	ctx2 := newCtx()
	ctx2.OnUnresolvedRead = func(name string) { aborted = append(aborted, name) }
	p2, err := Compile("v.missing + t.other")
	if err != nil {
		t.Fatal(err)
	}
	if v := p2.Run(ctx2); v != 0 {
		t.Errorf("aborted program = %v, want 0", v)
	}
	if len(aborted) != 1 || aborted[0] != "variable.missing" {
		t.Fatalf("reported %v, want exactly [variable.missing]", aborted)
	}
}

// TestContextNamespaceParsesAsUnknownZero: context.* is legal Molang
// syntax that must parse rather than error. An unpopulated context member
// is an UNRESOLVED read now, so this aborts the program -- but the value
// the caller sees is still 0, which is exactly what the engine leaves in
// the result slot. The two readings agree on the value and differ only on
// what would have run afterwards.
func TestContextNamespaceParsesAsUnknownZero(t *testing.T) {
	if got := evalOK(t, "context.block_face"); got != 0 {
		t.Errorf("context.block_face = %v, want 0", got)
	}
	if got := evalOK(t, "c.block_face"); got != 0 {
		t.Errorf("c.block_face = %v, want 0", got)
	}
}

// TestMathAliasMIsRejected pins that `m.` is not accepted, in either
// direction. `m.` is NOT a Molang alias: the engine's tokenizer knows
// exactly four two-character prefixes (v./q./t./c.) and math is not among
// them, so `m.floor(1.5)` is an unknown token in game.
//
// This module used to accept it on input as a deliberate leniency and
// refuse to emit it. That asymmetry is gone, because it produced the one
// outcome this package exists to prevent: an expression that evaluates
// perfectly here and then will not load in the game. See
// ast.Namespace.ShortAlias, and TestMinifyNeverEmitsMathAlias in package
// printer for the emitting half.

func TestRandomDrawsFromInjectedRNG(t *testing.T) {
	ctx := newCtx()
	p, err := Compile("math.random(0, 1)")
	if err != nil {
		t.Fatal(err)
	}
	if v := p.Run(ctx); v != 0.25 {
		t.Errorf("first draw = %v, want 0.25", v)
	}
	if v := p.Run(ctx); v != 0.5 {
		t.Errorf("second draw = %v, want 0.5", v)
	}
}

// TestDivideNearZeroGuardSkipsNumeratorRNG pins the RNG-order half of the
// Divide guard's contract (see eval/compile.go's ast.Div case and
// eval/eval_test.go's TestDivideNearZeroGuard for the value-only half):
// when the denominator's |value| is below the guard threshold, the
// numerator is never evaluated at all -- so any math.random draw inside it
// must not be consumed. This is exactly the shipped-bug scenario
// (a real pack expression drew 5436 RNG values where the engine draws 42,
// because the numerator's RNG calls were wrongly always
// evaluated) -- pinned here as a draw-count assertion, not merely a value
// assertion, since RNG call order is this package's correctness contract.

// TestDivideNearZeroGuardSkipsNumeratorRNG pins the RNG-order half of the
// Divide guard's contract (see eval/compile.go's ast.Div case and
// eval/eval_test.go's TestDivideNearZeroGuard for the value-only half):
// when the denominator's |value| is below the guard threshold, the
// numerator is never evaluated at all -- so any math.random draw inside it
// must not be consumed. This is exactly the shipped-bug scenario
// (a real pack expression drew 5436 RNG values where the engine draws 42,
// because the numerator's RNG calls were wrongly always
// evaluated) -- pinned here as a draw-count assertion, not merely a value
// assertion, since RNG call order is this package's correctness contract.
func TestDivideNearZeroGuardSkipsNumeratorRNG(t *testing.T) {
	ctx := newCtx()
	p, err := Compile("math.random(0, 1) / 0")
	if err != nil {
		t.Fatal(err)
	}
	if got := p.Run(ctx); got != 0 {
		t.Errorf("math.random(0,1)/0 = %v, want 0", got)
	}
	rng := ctx.RNG.(*stubRNG)
	if rng.fi != 0 {
		t.Errorf("draws consumed = %d, want 0 (numerator must not be evaluated when the guard fires)", rng.fi)
	}

	// Sanity: an ordinary (non-near-zero) denominator DOES evaluate and
	// draw from the numerator, so the assertion above is actually
	// exercising the guard and not e.g. a broken compile of math.random.
	ctx2 := newCtx()
	p2, err := Compile("math.random(0, 1) / 4")
	if err != nil {
		t.Fatal(err)
	}
	p2.Run(ctx2)
	rng2 := ctx2.RNG.(*stubRNG)
	if rng2.fi != 1 {
		t.Errorf("draws consumed = %d, want 1 (ordinary division must still evaluate its numerator)", rng2.fi)
	}
}

func TestCallDiscardOnUnknownQuery(t *testing.T) {
	// query.foo(1, math.random(0,1)) should evaluate args (consuming a
	// draw) then discard and return the plain query.foo lookup (0, since
	// nothing set it).
	ctx := newCtx()
	p, err := Compile("query.foo(1, math.random(0,1))")
	if err != nil {
		t.Fatal(err)
	}
	v := p.Run(ctx)
	if v != 0 {
		t.Errorf("call-discard result = %v, want 0", v)
	}
	rng := ctx.RNG.(*stubRNG)
	if rng.fi != 1 {
		t.Errorf("draws consumed = %d, want 1", rng.fi)
	}
}
