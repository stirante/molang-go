// foreach_test.go pins `for_each(<variable>, array.<name>, { body })`.
//
// Molang also allows an entity array as the source. This package has no
// entities and no array-valued variables, so a host array is the only source
// it accepts. That is a subset rather than a divergence: what it does walk, it
// walks the same way, and the rule about invalid entries being skipped has
// nothing to skip when the elements are numbers.
package molang

import "testing"

func TestForEachWalksEveryElement(t *testing.T) {
	arr := map[string][]float64{"vals": {10, 20, 30}}
	cases := map[string]float64{
		"v.n = 0; for_each(t.x, array.vals, {v.n = v.n + 1;}); return v.n;":   3,
		"v.s = 0; for_each(t.x, array.vals, {v.s = v.s + t.x;}); return v.s;": 60,
		// The loop variable may be variable. as well as temp.
		"v.s = 0; for_each(v.x, array.vals, {v.s = v.s + v.x;}); return v.s;": 60,
	}
	for src, want := range cases {
		if got := evalArr(t, src, arr); got != want {
			t.Errorf("%s = %v, want %v", src, got, want)
		}
	}
}

func TestForEachBreakAndContinue(t *testing.T) {
	arr := map[string][]float64{"vals": {1, 2, 3, 4}}
	cases := map[string]float64{
		// break leaves the walk; the elements after it are not visited.
		"v.n = 0; for_each(t.x, array.vals, {(t.x == 3) ? break; v.n = v.n + 1;}); return v.n;": 2,
		// continue skips the rest of THIS pass only.
		"v.n = 0; for_each(t.x, array.vals, {(t.x == 3) ? continue; v.n = v.n + 1;}); return v.n;": 3,
	}
	for src, want := range cases {
		if got := evalArr(t, src, arr); got != want {
			t.Errorf("%s = %v, want %v", src, got, want)
		}
	}
}

// A return inside the body ends the whole program, not just the walk -- the
// same as inside loop(), and for the same reason.
func TestForEachReturnEndsTheProgram(t *testing.T) {
	arr := map[string][]float64{"vals": {1, 2, 3}}
	ctx := arrayCtx(arr)
	got, err := Eval("v.n = 0; for_each(t.x, array.vals, {v.n = v.n + 1; (t.x == 2) ? {return 99;};}); v.n = 1000; return v.n;", ctx)
	if err != nil {
		t.Fatalf("eval: %v", err)
	}
	if got != 99 {
		t.Errorf("for_each with a returning body = %v, want 99", got)
	}
	if n := ctx.Scope.Variable["n"]; n != 2 {
		t.Errorf("v.n = %v, want 2 -- the assignment after the walk must not run", n)
	}
}

// An unregistered array and an empty one both walk zero times. A for_each over
// nothing is not an error.
func TestForEachOverNothingRunsZeroTimes(t *testing.T) {
	for _, arrays := range []map[string][]float64{
		{},
		{"vals": {}},
	} {
		src := "v.n = 0; for_each(t.x, array.vals, {v.n = v.n + 1;}); return v.n;"
		if got := evalArr(t, src, arrays); got != 0 {
			t.Errorf("%s over an absent/empty array = %v, want 0", src, got)
		}
	}
}

// The loop variable holds the last element after the walk, and the body sees
// the outer scope throughout.
func TestForEachLoopVariableAndOuterScope(t *testing.T) {
	arr := map[string][]float64{"vals": {5, 6, 7}}
	ctx := arrayCtx(arr)
	if _, err := Eval("for_each(t.x, array.vals, {v.seen = t.x;});", ctx); err != nil {
		t.Fatalf("eval: %v", err)
	}
	if got := ctx.Scope.Temp["x"]; got != 7 {
		t.Errorf("loop variable = %v after the walk, want the last element 7", got)
	}
	if got := ctx.Scope.Variable["seen"]; got != 7 {
		t.Errorf("v.seen = %v, want 7 -- the body writes to the enclosing scope", got)
	}
}

func TestForEachRejectsBadArguments(t *testing.T) {
	refused := []string{
		// The source must be an array namespace, not a variable: this package
		// has no entity arrays to walk.
		"for_each(t.x, v.baa, {v.n = 1;});",
		// The loop variable must be assignable.
		"for_each(math.pi, array.vals, {v.n = 1;});",
		"for_each(q.foo, array.vals, {v.n = 1;});",
		// The body is a block.
		"for_each(t.x, array.vals, v.n = 1);",
	}
	for _, src := range refused {
		if _, err := Compile(src); err == nil {
			t.Errorf("%q compiled, but it is not a well-formed for_each", src)
		}
	}
}
