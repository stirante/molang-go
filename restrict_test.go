package molang

import (
	"errors"
	"strings"
	"testing"

	"github.com/stirante/molang-go/ast"
	"github.com/stirante/molang-go/eval"
)

func parseOK(t *testing.T, src string) *ast.Program {
	t.Helper()
	prog, err := Parse(src)
	if err != nil {
		t.Fatalf("parse(%q): %v", src, err)
	}
	return prog
}

func opsOf(t *testing.T, src string) ast.OpSet {
	t.Helper()
	return ast.OpsUsed(parseOK(t, src))
}

// The operation set is what a context's allow-list is checked against, so it
// has to name what an expression contains wherever it contains it.
func TestOpsUsedNamesWhatAnExpressionContains(t *testing.T) {
	for _, c := range []struct {
		src  string
		want []ast.Op
	}{
		{"1 + 2", []ast.Op{ast.OpFloat, ast.OpAdd}},
		{"1 - 2", []ast.Op{ast.OpFloat, ast.OpAdd, ast.OpNegate}},
		{"v.a = 5", []ast.Op{ast.OpAssignment, ast.OpEntityVariable, ast.OpFloat}},
		{"t.a = 5", []ast.Op{ast.OpAssignment, ast.OpTempVariable}},
		{"math.random(0, 1)", []ast.Op{ast.OpRandom}},
		{"math.die_roll(1, 2, 3)", []ast.Op{ast.OpDieRoll}},
		{"math.ease_in_out_elastic(0, 1, 0.5)", []ast.Op{ast.OpEaseInOutElastic}},
		{"math.pi", []ast.Op{ast.OpPi}},
		{"'x' == 'y'", []ast.Op{ast.OpString, ast.OpLogicalEqual}},
		{"q.foo", []ast.Op{ast.OpQueryFunction}},
		{"array.a[1]", []ast.Op{ast.OpArrayVariable, ast.OpArray}},
		{"c.other->q.test", []ast.Op{ast.OpPointer, ast.OpContextVariable, ast.OpQueryFunction}},
		{"this", []ast.Op{ast.OpThis}},
		{"1 ? 2 : 3", []ast.Op{ast.OpConditional, ast.OpConditionalElse}},
		{"1 ? 2", []ast.Op{ast.OpConditional}},
		{"v.x ?? 5", []ast.Op{ast.OpNullCoalescing}},
		{"loop(3, { t.i = t.i + 1; });", []ast.Op{ast.OpLoop, ast.OpAssignment, ast.OpSemicolon}},
		{"for_each(t.x, array.a, { t.s = t.s + t.x; });", []ast.Op{ast.OpForEach, ast.OpArrayVariable}},
		{"return 1;", []ast.Op{ast.OpReturn, ast.OpSemicolon}},
		{"1 + 1", []ast.Op{ast.OpAdd}},
		{"geometry.default == geometry.other", []ast.Op{ast.OpGeometryVariable}},
	} {
		got := opsOf(t, c.src)
		for _, op := range c.want {
			if !got.Has(op) {
				t.Errorf("%q: missing %v; got %v", c.src, op, got)
			}
		}
	}
}

// A bare expression uses no ';'. This is the same distinction eval.Compile
// reads from Program.HasSemicolon, seen from the operation side.
func TestSemicolonIsAnOperationOnlyWhenPresent(t *testing.T) {
	if opsOf(t, "1 + 1").Has(ast.OpSemicolon) {
		t.Error("a bare expression reported the ';' operation")
	}
	if !opsOf(t, "1 + 1;").Has(ast.OpSemicolon) {
		t.Error("a statement sequence did not report the ';' operation")
	}
}

// The engine has NO subtract operation: its list numbers Negate and Add and
// nothing else for '-'. `a - b` therefore reports as both.
func TestSubtractIsNegatePlusAdd(t *testing.T) {
	ops := opsOf(t, "5 - 3")
	if !ops.Has(ast.OpNegate) || !ops.Has(ast.OpAdd) {
		t.Errorf("5 - 3 reported %v, want both Negate and Add", ops)
	}
}

// An assignment hiding in an argument is the case that makes an
// operation-set check necessary rather than decorative: the expression is
// syntactically ordinary and still carries a side effect.
func TestAssignmentIsFoundInsideAnArgument(t *testing.T) {
	for _, src := range []string{
		"math.max(v.a = 5, 3)",
		"(v.a = 5) + 1",
		"q.foo(v.a = 5)",
		"1 ? (v.a = 5) : 0",
		"math.max(1, math.min(2, v.a = 5))",
	} {
		if !opsOf(t, src).Has(ast.OpAssignment) {
			t.Errorf("%q: assignment not reported", src)
		}
		if err := DisallowSideEffects(parseOK(t, src), false); err == nil {
			t.Errorf("%q: accepted where side effects are disallowed", src)
		}
	}
}

// Assignment is refused whatever alsoRandom says -- turning the switch on at
// all is what forbids it.
func TestAssignmentIsRefusedUnderBothSettings(t *testing.T) {
	prog := parseOK(t, "v.a = 5")
	for _, alsoRandom := range []bool{false, true} {
		if err := DisallowSideEffects(prog, alsoRandom); err == nil {
			t.Errorf("alsoRandom=%v: assignment accepted", alsoRandom)
		}
	}
}

// alsoRandom's entire effect is math.random and math.random_integer.
func TestRandomIsRefusedOnlyWhenAsked(t *testing.T) {
	for _, src := range []string{"math.random(0, 1)", "math.random_integer(0, 1)"} {
		prog := parseOK(t, src)
		if err := DisallowSideEffects(prog, false); err != nil {
			t.Errorf("%q: refused with alsoRandom=false: %v", src, err)
		}
		if err := DisallowSideEffects(prog, true); err == nil {
			t.Errorf("%q: accepted with alsoRandom=true", src)
		}
	}
}

// The engine does NOT remove the die_roll pair, though both draw exactly as
// math.random does. This pins the asymmetry so that "tidying" it up later is
// a deliberate act with a failing test attached, not a silent drift.
//
// eval.RandomFnNames is the authority on which math functions draw, so the
// test derives the gap rather than restating it.
func TestDieRollDrawsButIsNotASideEffectOp(t *testing.T) {
	disallowed := SideEffectOps(true)

	var notRefused []string
	for name := range eval.RandomFnNames {
		op, ok := ast.MathOp(name)
		if !ok {
			t.Fatalf("math.%s draws but has no operation", name)
		}
		if !disallowed.Has(op) {
			notRefused = append(notRefused, name)
		}
	}

	want := map[string]bool{"die_roll": true, "die_roll_integer": true}
	if len(notRefused) != len(want) {
		t.Fatalf("drawing functions not refused: %v, want exactly %v", notRefused, keys(want))
	}
	for _, n := range notRefused {
		if !want[n] {
			t.Errorf("math.%s draws and is not refused; expected only the die_roll pair", n)
		}
	}
}

func keys(m map[string]bool) []string {
	var out []string
	for k := range m {
		out = append(out, k)
	}
	return out
}

// The diagnostic has to name the operation the way the engine names it, or
// an author matching a message from the game against one from a tool has to
// translate between two vocabularies.
func TestErrorNamesTheOperationTheWayTheEngineDoes(t *testing.T) {
	err := DisallowSideEffects(parseOK(t, "v.a = 5"), false)
	if err == nil {
		t.Fatal("expected an error")
	}
	const want = "Expression uses operation Assignment '=' which is not allowed in this context"
	if err.Error() != want {
		t.Errorf("message:\n got %q\nwant %q", err.Error(), want)
	}

	var opErr *OpNotAllowedError
	if !errors.As(err, &opErr) {
		t.Fatalf("got %T, want *OpNotAllowedError", err)
	}
	if opErr.Op != ast.OpAssignment {
		t.Errorf("Op = %v, want %v", opErr.Op, ast.OpAssignment)
	}
}

// A program breaking several rules should report them all, so an author
// fixes the expression once rather than once per attempt.
func TestErrorCarriesEveryForbiddenOperation(t *testing.T) {
	err := DisallowSideEffects(parseOK(t, "v.a = math.random(0, 1)"), true)
	if err == nil {
		t.Fatal("expected an error")
	}
	var opErr *OpNotAllowedError
	if !errors.As(err, &opErr) {
		t.Fatalf("got %T, want *OpNotAllowedError", err)
	}
	for _, op := range []ast.Op{ast.OpAssignment, ast.OpRandom} {
		if !opErr.Ops.Has(op) {
			t.Errorf("Ops missing %v; got %v", op, opErr.Ops)
		}
	}
	// The named one is the lowest-numbered, which is math.random (33)
	// rather than assignment (71).
	if opErr.Op != ast.OpRandom {
		t.Errorf("named %v, want the lowest-numbered forbidden op %v", opErr.Op, ast.OpRandom)
	}
}

// An ordinary pure expression passes every context.
func TestPureExpressionsPass(t *testing.T) {
	for _, src := range []string{
		"1 + 2 * 3",
		"math.sin(q.life_time * 30)",
		"math.clamp(v.x, 0, 1)",
		"q.is_alive ? 1 : 0",
		"math.die_roll(1, 2, 3)", // draws, but is not a side-effect op
	} {
		if err := DisallowSideEffects(parseOK(t, src), true); err != nil {
			t.Errorf("%q: %v", src, err)
		}
	}
}

func TestEmptyDisallowedSetAlwaysPasses(t *testing.T) {
	for _, src := range []string{"v.a = 5", "math.random(0, 1)", "loop(3, { t.i = 1; });"} {
		if err := CheckOps(parseOK(t, src), ast.OpSet{}); err != nil {
			t.Errorf("%q: %v", src, err)
		}
	}
}

// A context may forbid anything, not only side effects -- the mechanism is
// general even though this package ships one preset.
func TestAnyOperationCanBeForbidden(t *testing.T) {
	noLoops := ast.NewOpSet(ast.OpLoop, ast.OpForEach)
	if err := CheckOps(parseOK(t, "loop(3, { t.i = 1; });"), noLoops); err == nil {
		t.Error("loop accepted where loops are forbidden")
	}
	if err := CheckOps(parseOK(t, "1 + 1"), noLoops); err != nil {
		t.Errorf("plain arithmetic refused: %v", err)
	}
}

// Every math function the evaluator knows must map to an operation, or a
// function added later would be invisible to every allow-list.
func TestEveryMathFunctionHasAnOperation(t *testing.T) {
	var missing []string
	for name := range eval.MathArity {
		if _, ok := ast.MathOp(name); !ok {
			missing = append(missing, name)
		}
	}
	if len(missing) > 0 {
		t.Errorf("math functions with no operation: %v", missing)
	}
}

// ...and no operation may be nameless, since the name is the diagnostic.
func TestEveryMathOperationHasAFriendlyName(t *testing.T) {
	for name := range eval.MathArity {
		op, ok := ast.MathOp(name)
		if !ok {
			continue // reported by TestEveryMathFunctionHasAnOperation
		}
		if got := op.String(); got == "" || strings.HasPrefix(got, "<unknown") {
			t.Errorf("math.%s (op %d) has no friendly name: %q", name, op, got)
		}
	}
}
