// foreach_test.go pins `for_each(<variable>, <source>, { body })`.
//
// In the game the source is an entity array, and any expression parses there:
// only the argument count and the loop variable are checked when an
// expression loads. Here an entity array is what a host query returns with
// Context.EntityArrayRef, and each pass hands the loop variable a reference
// the body reads through with `->`. A bare `array.<name>` is walked
// numerically -- this package's own extension, not something the game does
// -- and any other source is evaluated and walked zero times.
package molang

import (
	"strings"
	"testing"

	"github.com/stirante/molang-go/printer"
)

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
		// The loop variable must be assignable.
		"for_each(math.pi, array.vals, {v.n = 1;});",
		"for_each(q.foo, array.vals, {v.n = 1;});",
		"for_each(this, array.vals, {v.n = 1;});",
		// Three arguments, no more and no fewer.
		"for_each(t.x, array.vals);",
		"for_each(t.x, array.vals, {v.n = 1;}, 1);",
		// The body is a block.
		"for_each(t.x, array.vals, v.n = 1);",
	}
	for _, src := range refused {
		if _, err := Compile(src); err == nil {
			t.Errorf("%q compiled, but it is not a well-formed for_each", src)
		}
	}
}

// The second argument is not checked when an expression loads: any expression
// parses there. In the game it is typically an entity query.
func TestForEachSourceIsAnyExpression(t *testing.T) {
	for _, src := range []string{
		"for_each(t.x, q.get_nearby_entities(4, 'player'), {v.n = 1;});",
		"for_each(v.e, query.get_nearby_entities_except_self(8), {v.n = v.n + 1;});",
		"for_each(t.x, v.baa, {v.n = 1;});",
		"for_each(t.x, array.vals[0], {v.n = 1;});",
		"for_each(t.x, 1 + 2, {v.n = 1;});",
		"for_each(t.x, q.a ? q.b : q.c, {v.n = 1;});",
	} {
		if _, err := Compile(src); err != nil {
			t.Errorf("%q: %v", src, err)
		}
	}
	// `a.` is not an alias of array. to the game, here or anywhere.
	if _, err := Compile("for_each(t.x, a.vals, {v.n = 1;});"); err == nil {
		t.Error("a.vals accepted; the game has no a. alias")
	}
}

// The game's own shape: a query returning an entity array, walked with the
// loop variable holding each entity in turn, and the body reading through it.
func TestForEachWalksAnEntityArray(t *testing.T) {
	ctx := newCtx()
	mobs := []*Entity{
		{Variable: map[string]float64{"hp": 3}},
		{Variable: map[string]float64{"hp": 5}},
		{Variable: map[string]float64{"hp": 20}},
	}
	radius := 0.0
	ctx.QueryFuncs = map[string]QueryFunc{
		"get_nearby_entities": func(args []float64, ctx *Context) float64 {
			radius = args[0]
			return ctx.EntityArrayRef(mobs)
		},
		"health": func(_ []float64, ctx *Context) float64 {
			return ctx.CurrentEntity().Variable["hp"]
		},
	}
	got, err := Eval("v.total = 0; for_each(t.e, q.get_nearby_entities(4, 'player'), {v.total = v.total + t.e->q.health;}); return v.total;", ctx)
	if err != nil {
		t.Fatalf("eval: %v", err)
	}
	if got != 28 {
		t.Errorf("sum of health over the walk = %v, want 28", got)
	}
	if radius != 4 {
		t.Errorf("the source query saw radius %v, want 4", radius)
	}

	// The loop variable is the entity itself: it keeps the last one after
	// the walk, compares equal to it, and can be stored for later.
	got, err = Eval("for_each(t.e, q.get_nearby_entities(4, 'player'), {v.last = t.e;}); return v.last == t.e && v.last->v.hp == 20;", ctx)
	if err != nil {
		t.Fatalf("eval: %v", err)
	}
	if got != 1 {
		t.Errorf("loop variable after the walk = %v, want 1 (the last entity, still readable)", got)
	}

	// break and continue work as they do over a host array, and the variable
	// form of the loop variable too.
	got, err = Eval("v.n = 0; for_each(v.e, q.get_nearby_entities(4, 'player'), {(v.e->v.hp == 5) ? continue; (v.e->v.hp == 20) ? break; v.n = v.n + 1;}); return v.n;", ctx)
	if err != nil {
		t.Fatalf("eval: %v", err)
	}
	if got != 1 {
		t.Errorf("walk with continue and break = %v, want 1", got)
	}

	// An empty array walks zero times.
	mobs = nil
	got, err = Eval("v.n = 0; for_each(t.e, q.get_nearby_entities(4, 'player'), {v.n = v.n + 1;}); return v.n;", ctx)
	if err != nil {
		t.Fatalf("eval: %v", err)
	}
	if got != 0 {
		t.Errorf("walk over an empty entity array = %v, want 0", got)
	}
}

// A source that is not an entity array is evaluated -- so a host query is
// still called, and its random draws and side effects still happen in order
// -- and then walked zero times. A single entity is not an array either. That
// is not an error: a loop over no entities is an ordinary answer, the same
// one `->` gives for a left side that is not an entity.
func TestForEachOverANonArraySourceWalksNothing(t *testing.T) {
	ctx := newCtx()
	ctx.Entities = map[string]*Entity{"owner": {Variable: map[string]float64{"hp": 1}}}
	got, err := Eval("v.n = 0; for_each(t.e, c.owner, {v.n = v.n + 1;}); return v.n;", ctx)
	if err != nil {
		t.Fatalf("eval: %v", err)
	}
	if got != 0 {
		t.Errorf("for_each over a single entity = %v, want 0 passes", got)
	}

	ctx = newCtx()
	calls := 0
	ctx.QueryFuncs = map[string]QueryFunc{
		"get_nearby_entities": func(args []float64, _ *Context) float64 {
			calls++
			return 3
		},
	}
	got, err = Eval("v.n = 0; t.x = 42; for_each(t.x, q.get_nearby_entities(4, 'player'), {v.n = v.n + 1;}); return v.n * 1000 + t.x;", ctx)
	if err != nil {
		t.Fatalf("eval: %v", err)
	}
	if got != 42 {
		t.Errorf("result = %v, want 42 -- no passes, and the loop variable untouched", got)
	}
	if calls != 1 {
		t.Errorf("the source query was called %d times, want once", calls)
	}

	// Assignments in the source still happen, and the statements after the
	// loop still run.
	ctx = newCtx()
	got, err = Eval("for_each(t.x, v.side = 7, {v.n = 1;}); return v.side + (v.n ?? 100);", ctx)
	if err != nil {
		t.Fatalf("eval: %v", err)
	}
	if got != 107 {
		t.Errorf("result = %v, want 107", got)
	}
}

// Printing keeps the source as written, and the output reparses to the same
// program.
func TestForEachSourcePrintsAndReparses(t *testing.T) {
	cases := []struct{ src, format, minify string }{
		{
			"for_each(t.x, array.vals, {v.n = t.x;});",
			"for_each(temp.x, array.vals, { variable.n = temp.x; });",
			"for_each(t.x,array.vals,{v.n=t.x;});",
		},
		{
			"for_each(variable.e, query.get_nearby_entities(4, 'player'), {v.n = v.n + 1;});",
			"for_each(variable.e, query.get_nearby_entities(4, 'player'), { variable.n = variable.n + 1; });",
			"for_each(v.e,q.get_nearby_entities(4,'player'),{v.n=v.n+1;});",
		},
	}
	for _, c := range cases {
		tree, err := Parse(c.src)
		if err != nil {
			t.Fatalf("parse(%q): %v", c.src, err)
		}
		if got := printer.Format(tree); got != c.format {
			t.Errorf("Format(%q) = %q, want %q", c.src, got, c.format)
		}
		if got := printer.Minify(tree); got != c.minify {
			t.Errorf("Minify(%q) = %q, want %q", c.src, got, c.minify)
		}
		for _, out := range []string{c.format, c.minify} {
			again, err := Parse(out)
			if err != nil {
				t.Errorf("reparse(%q): %v", out, err)
				continue
			}
			if printer.Minify(again) != c.minify {
				t.Errorf("reparse(%q) minifies to %q, want %q", out, printer.Minify(again), c.minify)
			}
		}
	}
}

func TestForEachReferencesTheSource(t *testing.T) {
	r := refsOf(t, "for_each(t.x, q.get_nearby_entities(v.radius), {v.n = 1;});")
	if strings.Join(r.Queries, ",") != "get_nearby_entities" {
		t.Errorf("Queries = %v", r.Queries)
	}
	if strings.Join(r.VariableReads, ",") != "radius" {
		t.Errorf("VariableReads = %v", r.VariableReads)
	}
	if len(r.Arrays) != 0 {
		t.Errorf("Arrays = %v, want none -- the source is not a host array", r.Arrays)
	}
	if r := refsOf(t, "for_each(t.x, array.vals, {v.n = 1;});"); strings.Join(r.Arrays, ",") != "vals" {
		t.Errorf("Arrays = %v, want [vals]", r.Arrays)
	}
}
