// language_test.go is what MOLANG means, written by hand and explained.
//
// The test files in this package are split by the kind of claim they make,
// not by topic, so that there is a rule for where a new test goes:
//
//	vanilla_cases_test.go   a generated table of "this text is this number",
//	                        taken from the game. Broad, no prose, regenerable.
//	mathlib_test.go         the math library, including the functions the
//	                        generated table cannot pin, plus the guards that
//	                        keep its own coverage honest.
//	language_test.go        this file: language semantics that need a reason
//	                        written next to them.
//	library_test.go         this library's own surface -- Context options,
//	                        RNG injection, what it reports and when.
//
// A test belongs here when it would still be true of any correct Molang
// implementation. If it is about a choice THIS package made, it belongs in
// library_test.go instead.
package molang

import (
	"math"
	"strings"
	"testing"

	"github.com/stirante/molang-go/eval"
)

func TestBasicArithmetic(t *testing.T) {
	cases := map[string]float64{
		"3 + 4":         7,
		"3 + 4 * 2":     11,
		"(3 + 4) * 2":   14,
		"10 / 4":        2.5,
		"-5 + 2":        -3,
		"!0":            1,
		"!1":            0,
		"1 < 2":         1,
		"2 <= 2":        1,
		"3 == 3":        1,
		"3 != 3":        0,
		"1 && 1":        1,
		"1 && 0":        0,
		"0 || 0":        0,
		"0 || 5":        1,
		"1 ? 2 : 3":     2,
		"0 ? 2 : 3":     3,
		"0 ? 99":        0,
		"1 ? 99":        99,
		"math.pi":       eval.Round32(math.Pi),
		"math.abs(-5)":  5,
		"math.mod(7,3)": 1,
	}
	for src, want := range cases {
		got := evalOK(t, src)
		if got != want {
			t.Errorf("%q = %v, want %v", src, got, want)
		}
	}
}

func TestNoModuloOperator(t *testing.T) {
	if _, err := Compile("7 % 3"); err == nil {
		t.Fatalf("expected parse error for %%, got none")
	}
}

// TestNullCoalesceIsNotNaNCoalescing pins the reading of '??'.
//
// It used to be TestNaNCoalesce, and it used to assert
// `math.sqrt(-1) ?? 5 == 5`. That premise is wrong twice over: '??' is a
// try/catch over an UNRESOLVED VARIABLE READ, not a value test of any kind,
// so a NaN a variable holds passes straight through it; and the game does
// not load `math.sqrt(-1) ?? 5` at all, because only a bare variable read
// may stand on the left of `??` (see TestNullCoalesceLeftSide). See
// eval/unresolved.go for the mechanism.
func TestNullCoalesceIsNotNaNCoalescing(t *testing.T) {
	got := evalOK(t, "v.x = math.sqrt(-1); return v.x ?? 5;")
	if !math.IsNaN(got) {
		t.Errorf("v.x = NaN; v.x ?? 5 = %v, want NaN (NaN is a value, not a missing read)", got)
	}
	if got := evalOK(t, "v.x = 3; return v.x ?? 5;"); got != 3 {
		t.Errorf("v.x = 3; v.x ?? 5 = %v, want 3", got)
	}
}

// TestNullCoalesceLeftSide: the game accepts exactly one shape on the left
// of `??` -- a bare context./variable./temp. read -- and refuses everything
// else when the expression loads. The refusal is applied to the grouped
// tree, so parentheses around the read change nothing, and the grouping
// rules decide what ends up on the left: `??` groups to the left and is
// grouped after the conditional, so a chain and a `??` inside a
// conditional's arm both put something other than a read there.
func TestNullCoalesceLeftSide(t *testing.T) {
	const wantMsg = "left-hand-side of ?? expression that isn't a direct-variable reference"
	rejected := []string{
		"3 ?? 5",
		"'s' ?? 5",
		"true ?? 5",
		"math.sqrt(-1) ?? 5",
		"math.pi ?? 1",
		"q.x ?? 1",
		"query.x(1) ?? 1",
		"array.a[0] ?? 1",
		"geometry.g ?? 1",
		"this ?? 1",
		"-v.x ?? 1",
		"!v.x ?? 1",
		"v.x + 0 ?? 1",
		"(v.x + 0) ?? 1",
		"v.a.b ?? 1",
		"variable.a.b ?? 1",
		"c.o->v.x ?? 1",
		"return (v.a = 1) ?? 2;",
		"v.a ?? v.b ?? 1",
		"v.a || v.b ?? 1",
		"v.a ? v.b ?? 1 : 2",
		"v.a ? 1 : v.b ?? 2",
		"(v.a ? 1 : 2) ?? 3",
	}
	for _, src := range rejected {
		_, err := Compile(src)
		if err == nil {
			t.Errorf("Compile(%q): accepted, want refusal", src)
			continue
		}
		if !strings.Contains(err.Error(), wantMsg) {
			t.Errorf("Compile(%q): error %q does not carry the game's wording", src, err)
		}
	}
	accepted := []string{
		"v.x ?? 1",
		"variable.x ?? 1",
		"t.x ?? 1",
		"temp.x ?? 1",
		"c.x ?? 1",
		"context.x ?? 1",
		"(v.x) ?? 1",
		"((v.x)) ?? 1",
		"v.a ?? (v.b ?? 1)",
		"v.a ?? 1 + 2",
		"v.a ?? 0 > 1",
		"v.a ?? v.b || 1",
		"v.a ?? 1 ? 2 : 3",
		"(v.a ?? 1) ? 2 : 3",
		"v.a ? (v.b ?? 1) : 2",
		"v.a ? 1 : (v.b ?? 2)",
		"!(v.a ?? 0)",
		"v.x = v.y ?? 1;",
		"math.max(v.a ?? 1, 2)",
	}
	for _, src := range accepted {
		if _, err := Compile(src); err != nil {
			t.Errorf("Compile(%q): %v, want acceptance", src, err)
		}
	}
}

// TestNullCoalescePrecedence: `??` is grouped after every other operator
// except `=`, so everything to its right up to a `=` or a `,` is the
// fallback. The first case is the one that tells the two readings apart:
// with v.a resolved to 0, `v.a ?? 1 ? 2 : 3` is v.a itself, not
// `0 ? 2 : 3`.
func TestNullCoalescePrecedence(t *testing.T) {
	evalCases(t, "coalesce precedence", map[string]float64{
		"v.a = 0; return v.a ?? 1 ? 2 : 3;":  0,
		"v.a ?? 1 ? 2 : 3":                   2,
		"v.a ?? 0 ? 2 : 3":                   3,
		"v.a = 5; return v.a ?? 0 > 1;":      5,
		"v.a ?? 0 > 1":                       0,
		"v.a ?? 2 > 1":                       1,
		"v.a ?? 1 + 1":                       2,
		"v.a ?? (v.b ?? 3)":                  3,
		"v.b = 4; return v.a ?? (v.b ?? 3);": 4,
		"v.x = v.y ?? 7; return v.x;":        7,
		"math.max(v.a ?? 1, 2)":              2,
		"(v.a ?? 1) ? 2 : 3":                 2,
		"1 ? (v.a ?? 6) : 3":                 6,
	})
}

// TestNullCoalesceCatchesUnresolvedRead is the behaviour the corpus needs:
// the `t.x = t.x ?? 0.35` idiom eight golden expressions are written
// around. The key distinction is the last case -- a RESOLVED zero does not
// divert, so this cannot be implemented as "falsy takes the right side".

func TestStatementSequenceDefaultsToZero(t *testing.T) {
	if got := evalOK(t, "t.x = 1; t.x = 2;"); got != 0 {
		t.Errorf("sequence without return = %v, want 0", got)
	}
	if got := evalOK(t, "t.x = 1; t.x = 2; return t.x;"); got != 2 {
		t.Errorf("sequence with return = %v, want 2", got)
	}
}

func TestSingleBareExpressionIsDirectValue(t *testing.T) {
	if got := evalOK(t, "3 + 4"); got != 7 {
		t.Errorf("bare expr = %v, want 7", got)
	}
}

func TestTempVariablePersistAcrossCalls(t *testing.T) {
	ctx := newCtx()
	// t.x has to be SEEDED, not merely absent: an absent key is an
	// unresolved read, which aborts the program before the increment ever
	// happens (see eval/unresolved.go). Real packs that use this idiom
	// either write the slot first or guard it with `?? 0`.
	ctx.Scope.Temp["x"] = 0
	p, err := Compile("t.x = t.x + 1; return t.x;")
	if err != nil {
		t.Fatal(err)
	}
	if v := p.Run(ctx); v != 1 {
		t.Fatalf("first run = %v, want 1", v)
	}
	if v := p.Run(ctx); v != 2 {
		t.Fatalf("second run = %v, want 2", v)
	}
}

func TestLoopBreakContinue(t *testing.T) {
	// t.i is initialised in-source for the same reason TestTempVariable
	// PersistAcrossCalls seeds its scope: reading an unwritten slot aborts.
	src := "t.i = 0; t.sum = 0; loop(10, { t.i = t.i + 1; t.i > 5 ? { break; }; t.i == 3 ? { continue; }; t.sum = t.sum + t.i; }); return t.sum;"
	// i: 1,2,3(skip via continue),4,5 -> sum = 1+2+4+5 = 12; at i=6 break.
	if got := evalOK(t, src); got != 12 {
		t.Errorf("loop result = %v, want 12", got)
	}
}

func TestCondBlockStatement(t *testing.T) {
	src := "t.x = 5; t.x > 3 ? { return 1; } : { return 0; };"
	if got := evalOK(t, src); got != 1 {
		t.Errorf("cond block = %v, want 1", got)
	}
}

func TestNamespaceAliases(t *testing.T) {
	// t./v. must be exactly equivalent to temp./variable. — set through the
	// alias, read through the full name.
	src := "t.x = 5; return temp.x;"
	if got := evalOK(t, src); got != 5 {
		t.Errorf("alias test = %v, want 5", got)
	}
}

// TestContextNamespaceParsesAsUnknownZero: context.* is legal Molang
// syntax that must parse rather than error. An unpopulated context member
// is an UNRESOLVED read now, so this aborts the program -- but the value
// the caller sees is still 0, which is exactly what the engine leaves in
// the result slot. The two readings agree on the value and differ only on
// what would have run afterwards.

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
func TestMathAliasMIsRejected(t *testing.T) {
	if _, err := Compile("m.floor(1.5)"); err == nil {
		t.Error("m.floor(1.5) compiled; `m.` is not an engine alias and must be refused")
	}
	// The spelling the author actually wants still works, so the rejection
	// cannot be mistaken for math.floor being unsupported.
	if got := evalOK(t, "math.floor(1.5)"); got != 1 {
		t.Errorf("math.floor(1.5) = %v, want 1", got)
	}
}

// -------------------------------------------------------------------------
// Grammar and shapes: precedence, associativity, the conditional and loop
// forms, literals, scopes and namespaces.
//
// These are the things an author assumes without checking, which is exactly
// why nothing would have caught them breaking. The generated table in
// vanilla_cases_test.go covers many of the same shapes with far more cases;
// what these add is the reasoning -- why a particular arrangement is the one
// that tells two readings apart, and what an author gets wrong if it flips.
// -------------------------------------------------------------------------

// Basic coverage of the core language: precedence, associativity, the shapes
// of the conditional and loop forms, scopes and literals.
//
// The rest of the suite is weighted heavily towards the places where Molang
// surprises people -- unresolved reads, NaN truthiness, the quantised sine
// table, float32 rounding. That is the interesting half, and it stays where
// it is. This file is the half that had no home: the things an author
// assumes without checking, which is exactly why nothing here would have
// caught them breaking.

// evalCases runs a table of source -> expected value against a fresh context
// per case, so RNG draws and scope writes cannot leak between rows.
func evalCases(t *testing.T, name string, cases map[string]float64) {
	t.Helper()
	t.Run(name, func(t *testing.T) {
		for src, want := range cases {
			if got := evalOK(t, src); got != want {
				t.Errorf("%q = %v, want %v", src, got, want)
			}
		}
	})
}

func TestPrecedenceAndAssociativity(t *testing.T) {
	evalCases(t, "arithmetic", map[string]float64{
		"2 + 3 * 4":   14,
		"2 * 3 + 4":   10,
		"(2 + 3) * 4": 20,
		"2 + 12 / 4":  5,
		// Left-associative: 10-3-2 is (10-3)-2, not 10-(3-2).
		"10 - 3 - 2":     5,
		"100 / 10 / 2":   5,
		"100 / (10 / 2)": 20,
	})

	evalCases(t, "unary", map[string]float64{
		"-2 + 3":   1,
		"-(2 + 3)": -5,
		"2 - -3":   5,
		"-(-3)":    3,
		"-2 * 3":   -6,
	})

	evalCases(t, "comparison_below_arithmetic", map[string]float64{
		"1 + 1 == 2": 1,
		"1 + 1 == 3": 0,
		"2 * 3 > 5":  1,
		"1 < 2 == 1": 1,
	})

	// `&&` binds tighter than `||`. This is the one arrangement that tells
	// the two readings apart: with `&&` tighter it is 1 || 0, which is 1;
	// with `||` tighter it is (1 || 0) && 0, which is 0.
	evalCases(t, "logical", map[string]float64{
		"1 || 0 && 0": 1,
		"0 || 1 && 1": 1,
		"1 && 1 || 0": 1,
	})

	// The ternary is lower than everything above it, so the branches
	// swallow whole expressions without needing parentheses.
	evalCases(t, "ternary_is_lowest", map[string]float64{
		"1 ? 2 : 3 + 4":   2,
		"0 ? 2 : 3 + 4":   7,
		"1 + 1 ? 10 : 20": 10,
		"0 * 5 ? 10 : 20": 20,
		"1 ? 2 + 3 : 4":   5,
	})
}

// Every logical and comparison operator produces exactly 1 or 0 -- not the
// operand that happened to be truthy. `5 || 0` is 1, not 5. Worth pinning
// because the JavaScript-shaped intuition is the other one, and an author
// who writes `variable.hp || 20` expecting a default gets 1.
func TestLogicalOperatorsNormaliseToOneOrZero(t *testing.T) {
	evalCases(t, "normalisation", map[string]float64{
		"2 && 3": 1,
		"5 || 0": 1,
		"0 || 7": 1,
		"!5":     0,
		"!0":     1,
		"!!5":    1,
		"3 > 1":  1,
		"1 > 3":  0,
		"3 >= 3": 1,
		"3 != 4": 1,
		// Comparisons are values like any other, so they add up.
		"(3 > 1) + (2 > 1)": 2,
	})
}

func TestBooleanLiterals(t *testing.T) {
	evalCases(t, "literals", map[string]float64{
		"true":          1,
		"false":         0,
		"true + true":   2,
		"true ? 5 : 6":  5,
		"false ? 5 : 6": 6,
		// Keywords are case-insensitive, like the rest of the language.
		"TRUE":  1,
		"False": 0,
	})
}

func TestNumberLiteralForms(t *testing.T) {
	evalCases(t, "forms", map[string]float64{
		"5":    5,
		"5.25": 5.25,
		".5":   0.5,
		"1.":   1,
		"007":  7,
		"0.0":  0,
		"-.25": -0.25,
	})
}

// `#` comments are an EXTENSION this package offers, not part of the language.
// Nothing in Molang's own tests mentions a comment, so accepting one silently
// would mean "this parses" no longer promises "the game will load it". They
// are asked for by name instead.
func TestCommentsAreAnOptInExtension(t *testing.T) {
	cases := map[string]float64{
		"1 + # everything after this is gone ? : ; { }\n2": 3,
		"# leading comment\n40 + 2":                        42,
		"7 # trailing":                                     7,
	}
	ext := Extensions{Comments: true}
	for src, want := range cases {
		// Refused by default.
		if _, err := Compile(src); err == nil {
			t.Errorf("%q compiled as vanilla Molang; comments are an extension", src)
		}
		// Accepted, and correct, when asked for.
		p, err := CompileWith(src, ext)
		if err != nil {
			t.Errorf("%q with Comments enabled: %v", src, err)
			continue
		}
		if got := p.Run(newCtx()); got != want {
			t.Errorf("%q = %v, want %v", src, got, want)
		}
	}

	// A `#` inside a string literal is text, not the start of a comment,
	// whether or not the extension is on.
	if got := evalOK(t, "'a#b' == 'a#b'"); got != 1 {
		t.Errorf("a # inside a string literal was treated as a comment")
	}
}

// Namespace names and the math library are case-insensitive, and the short
// aliases address the SAME storage as the long names -- `v.a` and
// `variable.a` are one slot, not two.
func TestNamespacesAreCaseInsensitiveAndAliased(t *testing.T) {
	evalCases(t, "case", map[string]float64{
		"MATH.FLOOR(1.5)":                    1,
		"Math.Pow(2, 3)":                     8,
		"VARIABLE.a = 5; return VARIABLE.a;": 5,
	})

	evalCases(t, "alias_shares_storage", map[string]float64{
		"v.a = 5; return variable.a;": 5,
		"variable.b = 6; return v.b;": 6,
		"t.c = 7; return temp.c;":     7,
		"temp.d = 8; return t.d;":     8,
	})
}

// A member name may itself contain dots. Molang splits a dotted identifier
// on the FIRST dot only; everything after it is one opaque key. So
// `variable.a.b` is a single name and not a field access on `variable.a`,
// and writing one does not create the other.
func TestMemberNamesMayContainDots(t *testing.T) {
	evalCases(t, "dotted", map[string]float64{
		"variable.a.b = 3; return variable.a.b;":     3,
		"variable.x.y.z = 4; return variable.x.y.z;": 4,
		"v.a.b = 3; return variable.a.b;":            3,
		// The parent name is a different key entirely, and stays unset --
		// so `?? 99` catches it.
		"variable.a.b = 3; return variable.a ?? 99;": 99,
	})
}

func TestScopeAssignmentAndReads(t *testing.T) {
	evalCases(t, "assignment", map[string]float64{
		"variable.a = 5; return variable.a;":              5,
		"temp.a = 2; temp.b = 3; return temp.a * temp.b;": 6,
		// An assignment is itself a statement, and the sequence's value
		// comes from `return`.
		"temp.a = 1; temp.a = temp.a + 1; return temp.a;": 2,
	})
}

// A return ends the program where it stands: nothing after it runs, no
// assignment it would have made happens, and no RNG draw it would have
// consumed is taken.
//
// The shape below is the one real content uses -- an early-exit guard. A bare
// `return` followed by more statements cannot be used to demonstrate this,
// because the game refuses to parse it at all; see
// TestUnreachableStatementsAreRefused.
func TestReturnEndsTheProgram(t *testing.T) {
	evalCases(t, "return", map[string]float64{
		"return 3 + 4;":                                  7,
		"1 ? {return 9;}; return 1;":                     9,
		"0 ? {return 9;}; return 1;":                     1,
		"v.n = 0; 1 ? {return 9;}; v.n = 5; return v.n;": 9,
	})

	// The side effect after the guard must not happen either -- this is the
	// half that decides what later expressions in a chain see.
	ctx := newCtx()
	if _, err := Eval("v.n = 0; 1 ? {return 9;}; v.n = 5;", ctx); err != nil {
		t.Fatalf("guard idiom: %v", err)
	}
	if got := ctx.Scope.Variable["n"]; got != 0 {
		t.Errorf("v.n = %v after a guard returned; the assignment after the "+
			"return must not run", got)
	}
}

// Anything after a bare return, break or continue IN THE SAME STATEMENT LIST
// is unreachable, and the game refuses the expression rather than dropping
// the dead code, naming the statement the way it names operations.
// Reproduced here, because a tool that accepts what the game refuses sends an
// author away believing a pack will load.
func TestUnreachableStatementsAreRefused(t *testing.T) {
	refused := map[string]string{
		"return 0; return 0;":                           "unreachable statements after Return 'return'.",
		"return 1; v.x = 5;":                            "unreachable statements after Return 'return'.",
		"v.x = 0; loop(3, {continue; v.x = v.x + 1;});": "unreachable statements after Continue 'continue'.",
		"v.x = 0; loop(3, {break; v.x = v.x + 1;});":    "unreachable statements after Break 'break'.",
		"loop(3, {v.i ? {break; v.i = 1;};});":          "unreachable statements after Break 'break'.",
		"{return 1; v.x = 2;};":                         "unreachable statements after Return 'return'.",
	}
	for src, want := range refused {
		_, err := Compile(src)
		if err == nil {
			t.Errorf("%q compiled; statements after a terminating one are unreachable", src)
			continue
		}
		if !strings.Contains(err.Error(), want) {
			t.Errorf("%q: error %q does not carry the game's wording %q", src, err, want)
		}
	}

	// A return inside a conditional is NOT a terminating statement -- the
	// conditional may not fire -- so the guard idiom stays legal. It is the
	// most common shape in real content and refusing it would be worse than
	// the bug this check fixes.
	accepted := []string{
		"1 ? {return 9;}; v.x = 5;",
		"v.a = 1; (v.a == 1) ? {return 0;}; v.b = 2; return v.b;",
		"loop(3, {(v.i == 1) ? break; v.i = v.i + 1;});",
		"loop(3, {(v.i == 1) ? continue; v.i = v.i + 1;});",
		"return 1;",
		"v.x = 1; return v.x;",
	}
	for _, src := range accepted {
		if _, err := Compile(src); err != nil {
			t.Errorf("%q was refused: %v", src, err)
		}
	}
}

func TestLoopRunsItsBodyTheGivenNumberOfTimes(t *testing.T) {
	evalCases(t, "loop", map[string]float64{
		"temp.i = 0; loop(3, {temp.i = temp.i + 1;}); return temp.i;":  3,
		"temp.i = 0; loop(0, {temp.i = temp.i + 1;}); return temp.i;":  0,
		"temp.i = 0; loop(-1, {temp.i = temp.i + 1;}); return temp.i;": 0,
		// A non-integer count rounds up: the body runs while the counter
		// is above zero, one taken off it per pass.
		"temp.i = 0; loop(2.9, {temp.i = temp.i + 1;}); return temp.i;": 3,
		"temp.i = 0; loop(0.5, {temp.i = temp.i + 1;}); return temp.i;": 1,
		// The body sees and updates the outer scope.
		"temp.n = 1; loop(4, {temp.n = temp.n * 2;}); return temp.n;": 16,
	})
}

// `??` on an unset read yields the right-hand side; on a set one it yields
// the left. The interesting half of this operator -- that it is a try/catch
// around its left-hand side rather than a null test -- lives in
// eval/unresolved.go's tests. This is just the shape an author writes.
func TestNullCoalesceBasics(t *testing.T) {
	evalCases(t, "coalesce", map[string]float64{
		"variable.nope ?? 7":                      7,
		"variable.a = 5; return variable.a ?? 7;": 5,
		"variable.a = 0; return variable.a ?? 7;": 0,
		"temp.nope ?? 1 + 1":                      2,
		// A chain groups to the left and is refused (see
		// TestNullCoalesceLeftSide); the fallback has to be parenthesized.
		"variable.nope ?? (variable.also_nope ?? 3)": 3,
	})
}

// String literals compare by identity, which is what packs use them for:
// stashing a name in temp./variable. and testing it later with == or !=.
func TestStringLiteralsCompareByEquality(t *testing.T) {
	evalCases(t, "strings", map[string]float64{
		"'abc' == 'abc'": 1,
		"'abc' == 'abd'": 0,
		"'abc' != 'abd'": 1,
		"variable.s = 'oak'; return variable.s == 'oak';":   1,
		"variable.s = 'oak'; return variable.s == 'birch';": 0,
	})
}

// Whitespace is not significant anywhere, including inside call argument
// lists and across newlines.
func TestWhitespaceIsInsignificant(t *testing.T) {
	evalCases(t, "whitespace", map[string]float64{
		"1+2*3":                            7,
		"  1  +  2  *  3  ":                7,
		"math.max(\n  1,\n  2\n)":          2,
		"temp.a\n=\n5;\nreturn\ntemp.a;\n": 5,
	})
}

// A string literal carries its bytes. Molang has no escape sequences, so
// whatever sits between the quotes is the value -- including text outside
// ASCII, which real packs do use for display strings they stash in a
// temp./variable. slot and compare later.
func TestStringLiteralsCarryNonASCIIText(t *testing.T) {
	evalCases(t, "utf8 strings", map[string]float64{
		"'zażółć gęślą jaźń' == 'zażółć gęślą jaźń'": 1,
		"'zażółć gęślą jaźń' == 'zazolc gesla jazn'": 0,
		"'日本語' == '日本語'":                             1,
		"'日本語' == '日本'":                              0,
		"'🙂' == '🙂'":                                 1,
		"'🙂' == '🚀'":                                 0,
		// Two literals differing only in a character's encoding length must
		// not collide: 'ą' is two bytes, 'a' is one.
		"'ą' == 'a'": 0,
		"variable.s = 'dąb'; return variable.s == 'dąb';":    1,
		"variable.s = 'dąb'; return variable.s == 'brzoza';": 0,
	})
}

// An assignment is an EXPRESSION, and its value is the value assigned. So
// `return v.a = 5;` is 5, `1 ? (v.a = 7) : 0` is 7, and the write happens
// either way.
//
// **CONFIRMED** against the game (1.26.50.24). The engine compiles an
// assignment into one of eight instructions, specialised by the shape of the
// right-hand side, and all of them agree:
//
//   - the specialised forms, where the right-hand side folded to a constant,
//     perform the write and then push a number carrying that same constant,
//     which their execute() also returns;
//   - the general form takes the already-evaluated right-hand side off the
//     top of the stack, writes it to the variable, and LEAVES IT THERE as
//     the instruction's result -- it never materialises a second slot,
//     because the value it would push is the one already sitting there.
//
// So the answer is the assigned value, not 0 and not NaN, whichever form the
// engine picks. (Whether a `;` then discards it is a separate question, and
// a separate CONFIRMED behaviour -- see ast.Program.HasSemicolon.)
//
// Where the value can be USED is narrower than where it exists: arithmetic
// and math functions refuse an assignment as an operand (`(v.a = 5) + 1`,
// `math.max(v.a = 5, 3)`), while a conditional's arm, a query's argument,
// a return and another assignment's right side take it. See
// TestNonNumericOperandsAreRefused.
//
// Writing through `->` is refused when the expression loads rather than
// evaluated: the engine content-logs an error. This package refuses it at
// parse time.
func TestAssignmentIsAnExpressionYieldingTheAssignedValue(t *testing.T) {
	evalCases(t, "assignment value", map[string]float64{
		// An expression containing `=` must end with `;`, and a `;` throws
		// the value away, so the value is observed through return.
		"return v.a = 5;":                      5,
		"return t.a = 5;":                      5,
		"return (v.a = 5);":                    5,
		"return 1 ? (v.a = 7) : 0;":            7,
		"return 0 ? 0 : (v.a = 7);":            7,
		"return v.b = (v.a = 6);":              6,
		"v.b = (v.a = 2) ? 8 : 9; return v.b;": 8,
		// A `;` overwrites the sequence's value with 0, so the same
		// assignment as a whole statement is 0 -- and still assigns.
		"v.a = 5;": 0,
	})

	// Without the trailing `;` -- which only the OptionalSemicolons extension
	// accepts -- the program is a bare expression and its value is the
	// assignment's.
	lenient, err := CompileWith("1 ? (v.a = 5) : 0", Extensions{OptionalSemicolons: true})
	if err != nil {
		t.Fatal(err)
	}
	if got := lenient.Run(newCtx()); got != 5 {
		t.Errorf("bare 1 ? (v.a = 5) : 0 = %v, want 5", got)
	}

	// The write must happen wherever the assignment sits.
	ctx := newCtx()
	prog, err := Compile("q.f(v.a = 5, 3);")
	if err != nil {
		t.Fatal(err)
	}
	prog.Run(ctx)
	if got, ok := ctx.Scope.Variable["a"]; !ok || got != 5 {
		t.Errorf("variable.a = %v (present=%v), want 5", got, ok)
	}
}

// Arithmetic, a comparison other than == and !=, a logical operator, a
// negation and every math function take numbers only. A string, a resource
// and an assignment are refused as their operand, with the game's wording,
// naming both the operation and the operand. == and != take anything, which
// is what lets a string or a resource be compared at all; so do a
// conditional, `??`, a query's arguments, a return and an assignment's right
// side, none of which look at what they are given.
//
// The game has applied this since Molang version 1.17.40 (a pack's
// min_engine_version); every pack this package is likely to meet is past
// that, and no older behaviour is offered.
func TestNonNumericOperandsAreRefused(t *testing.T) {
	refused := map[string]string{
		"'a' + 1":                    "'Add '+'' expression cannot take a 'String '''' argument. It only supports numerical arguments.",
		"1 + 'a'":                    "'Add '+'' expression cannot take a 'String '''' argument",
		"'a' - 1":                    "'Add '+'' expression cannot take a 'String '''' argument",
		"1 - 'a'":                    "'Negate '-'' expression cannot take a 'String '''' argument",
		"'a' * 2":                    "'Multiply '*'' expression cannot take a 'String '''' argument",
		"'a' / 2":                    "'Divide '/'' expression cannot take a 'String '''' argument",
		"'a' < 1":                    "'Less Than '<'' expression cannot take a 'String '''' argument",
		"'a' >= 1":                   "'Greater Than Or Equal '>='' expression cannot take a 'String '''' argument",
		"'a' && 1":                   "'Logical And '&&'' expression cannot take a 'String '''' argument",
		"'a' || 1":                   "'Logical Or '||'' expression cannot take a 'String '''' argument",
		"-'a'":                       "'Negate '-'' expression cannot take a 'String '''' argument",
		"!'a'":                       "'Logical Not '!'' expression cannot take a 'String '''' argument",
		"math.abs('a')":              "'Absolute Value 'math.abs'' expression cannot take a 'String '''' argument",
		"math.max(1, 'a')":           "'Max 'math.max'' expression cannot take a 'String '''' argument",
		"texture.foo + 1":            "'Add '+'' expression cannot take a 'Texture Variable 'texture.'' argument",
		"geometry.foo * 2":           "'Multiply '*'' expression cannot take a 'Geometry Variable 'geometry.'' argument",
		"-material.foo":              "'Negate '-'' expression cannot take a 'Material Variable 'material.'' argument",
		"math.floor(texture.foo)":    "'Floor 'math.floor'' expression cannot take a 'Texture Variable 'texture.'' argument",
		"(v.a = 1) + 2;":             "'Add '+'' expression cannot take a 'Assignment '='' argument",
		"math.max(v.a = 5, 3);":      "'Max 'math.max'' expression cannot take a 'Assignment '='' argument",
		"math.abs(v.a = -4);":        "'Absolute Value 'math.abs'' expression cannot take a 'Assignment '='' argument",
		"(v.a = 2) * (t.b = 3);":     "'Multiply '*'' expression cannot take a 'Assignment '='' argument",
		"!(v.a = 1);":                "'Logical Not '!'' expression cannot take a 'Assignment '='' argument",
		"(v.a = 1) < 2;":             "'Less Than '<'' expression cannot take a 'Assignment '='' argument",
		"v.x = 1 + (v.a = 1);":       "'Add '+'' expression cannot take a 'Assignment '='' argument",
		"loop(2, {v.n = 'a' + 1;});": "'Add '+'' expression cannot take a 'String '''' argument",
	}
	for src, want := range refused {
		_, err := Compile(src)
		if err == nil {
			t.Errorf("%q compiled; the game refuses a non-numeric operand", src)
			continue
		}
		if !strings.Contains(err.Error(), want) {
			t.Errorf("%q: error %q does not carry the game's wording %q", src, err, want)
		}
	}

	accepted := []string{
		"'a' == 'b'",
		"'a' != v.s",
		"texture.foo == texture.bar",
		"1 ? 'a' : 'b'",
		"v.s = 'a';",
		"q.f('a', texture.foo)",
		"return 'a';",
		"v.a ?? 'a'",
		"(v.a = 1) == 1;",
		"(v.a = 1) != 1;",
		"1 ? (v.a = 1) : 0;",
		"q.f(v.a = 1);",
		"v.b = (v.a = 1);",
		"return v.a = 1;",
		"v.b = (v.a = 1) ? 2 : 3;",
		"(1 ? 'a' : 2) + 1",
		"math.abs(1 ? 'a' : 2)",
	}
	for _, src := range accepted {
		if _, err := Compile(src); err != nil {
			t.Errorf("%q was refused: %v", src, err)
		}
	}
}

// What may stand on the left of `=`, and what the game says about the rest.
func TestAssignmentLeftSides(t *testing.T) {
	refused := map[string]string{
		"c.x = 1;":                             "assignment to non-variable not allowed. Expression is trying to assign to a: Context Variable 'context.' or 'c.'",
		"q.x = 1;":                             "assignment to non-variable not allowed. Expression is trying to assign to a: Query Function 'query.' or 'q.'",
		"q.x(1) = 1;":                          "assignment to non-variable not allowed. Expression is trying to assign to a: Query Function 'query.' or 'q.'",
		"1 = 2;":                               "assignment to non-variable not allowed. Expression is trying to assign to a: Float",
		"'a' = 2;":                             "assignment to non-variable not allowed. Expression is trying to assign to a: String '''",
		"this = 2;":                            "assignment to non-variable not allowed. Expression is trying to assign to a: This 'this'",
		"math.pi = 2;":                         "assignment to non-variable not allowed. Expression is trying to assign to a: Pi",
		"texture.a = 2;":                       "assignment to non-variable not allowed. Expression is trying to assign to a: Texture Variable 'texture.'",
		"array.a[0] = 2;":                      "assignment to non-variable not allowed. Expression is trying to assign to a: Array '[]'",
		"-v.x = 1;":                            "assignment to non-variable not allowed. Expression is trying to assign to a: Negate '-'",
		"!v.x = 1;":                            "assignment to non-variable not allowed. Expression is trying to assign to a: Logical Not '!'",
		"v.x + 1 = 2;":                         "assignment to non-variable not allowed. Expression is trying to assign to a: Add '+'",
		"v.x - 1 = 2;":                         "assignment to non-variable not allowed. Expression is trying to assign to a: Add '+'",
		"v.x * 2 = 2;":                         "assignment to non-variable not allowed. Expression is trying to assign to a: Multiply '*'",
		"math.abs(v.x) = 1;":                   "assignment to non-variable not allowed. Expression is trying to assign to a: Absolute Value 'math.abs'",
		"v.a ?? 1 = 2;":                        "assignment to non-variable not allowed. Expression is trying to assign to a: Null Coalescing '??'",
		"(v.x + 1) = 0;":                       "assignment to non-variable not allowed. Expression is trying to assign to a: Left Parenthesis '('",
		"(v.x) = 1;":                           "assignment to non-variable not allowed. Expression is trying to assign to a: Left Parenthesis '('",
		"return (v.x) = 1;":                    "assignment to non-variable not allowed. Expression is trying to assign to a: Left Parenthesis '('",
		"t.a.b = 1;":                           "left side of an assignment expression can only use temp variables if they are on their own and not part of a more complicated expression.",
		"temp.a.b.c = 1;":                      "left side of an assignment expression can only use temp variables if they are on their own",
		"c.a.b = 1;":                           "cannot use Context Variable 'context.' or 'c.' operators on the left side of an assignment expression",
		"v.x->v.y = 1;":                        "Assignment attempted on Pointer result",
		"for_each(v.a.b, q.list, {v.n = 1;});": "for_each requires three parameters",
		"for_each(t.a.b, q.list, {v.n = 1;});": "for_each requires three parameters",
		"for_each(c.e, q.list, {v.n = 1;});":   "for_each requires three parameters",
	}
	for src, want := range refused {
		_, err := Compile(src)
		if err == nil {
			t.Errorf("%q compiled; nothing can be assigned to that", src)
			continue
		}
		if !strings.Contains(err.Error(), want) {
			t.Errorf("%q: error %q does not carry the game's wording %q", src, err, want)
		}
	}

	accepted := []string{
		"v.x = 1;",
		"variable.x = 1;",
		"t.x = 1;",
		"temp.x = 1;",
		"v.a.b = 1;",
		"v.a.b.c = 1;",
		"v.x = t.a.b;",
		"t.x = v.a.b;",
		"for_each(t.e, q.list, {v.n = 1;});",
		"for_each(v.e, q.list, {v.n = 1;});",
	}
	for _, src := range accepted {
		if _, err := Compile(src); err != nil {
			t.Errorf("%q was refused: %v", src, err)
		}
	}
}

// A break must be inside a loop() or for_each() -- anywhere inside: a nested
// block, a conditional's arm, a block arm. Outside every loop it is refused
// with the game's wording. A continue is not checked: one outside a loop
// loads, and does nothing when reached.
func TestBreakOutsideLoopIsRefused(t *testing.T) {
	const want = "break encountered outside of loop"
	refused := []string{
		"break",
		"break;",
		"v.x = 1; break;",
		"{break;};",
		"v.x ? {break;};",
		"v.x ? break;",
		"v.x ? {v.y ? {break;};};",
		"loop(2, {v.x = 1;}); break;",
		"1 ? {break;} : 0;",
	}
	for _, src := range refused {
		_, err := Compile(src)
		if err == nil {
			t.Errorf("%q compiled; a break outside a loop is refused", src)
			continue
		}
		if !strings.Contains(err.Error(), want) {
			t.Errorf("%q: error %q does not carry the game's wording", src, err)
		}
	}

	accepted := []string{
		"loop(2, {break;});",
		"loop(2, {v.x ? break;});",
		"loop(2, {v.x ? {break;};});",
		"loop(2, {v.x ? {v.y ? {break;};};});",
		"loop(2, {{break;};});",
		"loop(2, {v.x ? 1 : {break;};});",
		"loop(2, {loop(2, {break;}); break;});",
		"for_each(t.e, q.list, {break;});",
		"for_each(t.e, q.list, {t.e->v.hp ? break;});",
		"continue",
		"continue;",
		"v.x = 1; continue;",
		"v.x ? {continue;};",
	}
	for _, src := range accepted {
		if _, err := Compile(src); err != nil {
			t.Errorf("%q was refused: %v", src, err)
		}
	}

	// At run time a continue outside a loop does nothing.
	if got := evalOK(t, "v.x = 1; 1 ? {continue;}; v.x = 2; return v.x;"); got != 2 {
		t.Errorf("continue outside a loop stopped the program: got %v, want 2", got)
	}
}

// A block arm runs its statements and yields 0, unless a return inside it ends
// the program. A value arm yields its value.
func TestConditionalBlockArmsEvaluate(t *testing.T) {
	evalCases(t, "block arms", map[string]float64{
		// The value of a block arm is 0.
		"v.r = 1 ? {v.a = 1;} : 5; return v.r;":       0,
		"v.r = 0 ? {v.a = 1;} : 5; return v.r;":       5,
		"v.r = 1 ? 5 : {v.a = 1;}; return v.r;":       5,
		"v.r = 0 ? 5 : {v.a = 1;}; return v.r;":       0,
		"v.r = (1 ? {v.a = 1;} : 5) + 2; return v.r;": 2,
		// The statements in the chosen arm run, and only those.
		"v.a = 0; 1 ? {v.a = 3;} : 5; return v.a;":         3,
		"v.a = 0; 0 ? {v.a = 3;} : 5; return v.a;":         0,
		"v.a = 0; 0 ? 5 : {v.a = 4;}; return v.a;":         4,
		"v.a = 0; 1 ? (v.a = 6) : {v.a = 4;}; return v.a;": 6,
		// A return inside a block arm ends the whole program.
		"1 ? {return 9;} : 5; return 1;":       9,
		"0 ? 5 : {return 8;}; return 1;":       8,
		"v.r = 0 ? 5 : {return 7;}; return 1;": 7,
		"0 ? {return 9;} : 5; return 1;":       1,
		// Else-if chains mixing both shapes.
		"v.a = 0; 0 ? {v.a = 1;} : 1 ? 2 : {v.a = 3;}; return v.a;": 0,
		"v.a = 0; 0 ? {v.a = 1;} : 0 ? 2 : {v.a = 3;}; return v.a;": 3,
	})

	// break and continue inside a block arm reach the enclosing loop.
	evalCases(t, "block arms in loops", map[string]float64{
		"v.i = 0; loop(10, {v.i = v.i + 1; v.i == 3 ? {break;} : 0;}); return v.i;":                           3,
		"v.i = 0; loop(10, {v.i = v.i + 1; v.i < 3 ? 0 : {break;};}); return v.i;":                            3,
		"v.i = 0; v.n = 0; loop(5, {v.i = v.i + 1; v.i == 2 ? {continue;} : 0; v.n = v.n + 1;}); return v.n;": 4,
	})

	// Without a trailing `;` -- only under OptionalSemicolons -- the program is
	// a bare expression, and its value is the arm's.
	for src, want := range map[string]float64{
		"0 ? {v.a = 1;} : 5": 5,
		"1 ? {v.a = 1;} : 5": 0,
		"1 ? 5 : {v.a = 1;}": 5,
	} {
		prog, err := CompileWith(src, Extensions{OptionalSemicolons: true})
		if err != nil {
			t.Fatalf("%q: %v", src, err)
		}
		if got := prog.Run(newCtx()); got != want {
			t.Errorf("%q = %v, want %v", src, got, want)
		}
	}
}
