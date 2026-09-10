// arrow_test.go pins `context.<entity>-><read>`.
//
// The arrow reads a public variable, or asks a query, of another entity. Its
// one surprising property is what happens when the entity is not there: the
// read is 0 and evaluation carries on. That is the opposite of what an
// unresolved VARIABLE does, which ends the expression -- and the asymmetry is
// the point. A missing variable is a mistake worth stopping for; a missing
// entity is an ordinary fact about the world.
package molang

import "testing"

func entityCtx() *Context {
	ctx := newCtx()
	ctx.Entities = map[string]*Entity{
		"owner": {
			Variable: map[string]float64{"baa": 1.25, "hp": 7},
			QueryFuncs: map[string]QueryFunc{
				"is_baby": func([]float64, *Context) float64 { return 1 },
				"sum": func(args []float64, _ *Context) float64 {
					s := 0.0
					for _, a := range args {
						s += a
					}
					return s
				},
			},
		},
		"empty": {},
	}
	ctx.Scope.Variable["baa"] = 99
	return ctx
}

func evalEnt(t *testing.T, src string) float64 {
	t.Helper()
	v, err := Eval(src, entityCtx())
	if err != nil {
		t.Fatalf("Eval(%q): %v", src, err)
	}
	return v
}

func TestArrowReadsTheOtherEntity(t *testing.T) {
	cases := map[string]float64{
		"context.owner->variable.baa": 1.25,
		"c.owner->v.baa":              1.25,
		"c.owner->v.hp":               7,
		// The current entity's own variable of the same name is untouched.
		"variable.baa": 99,
		// A query through the arrow, with and without arguments.
		"c.owner->q.is_baby":      1,
		"c.owner->query.is_baby":  1,
		"c.owner->q.sum(1, 2, 3)": 6,
		"c.owner->q.sum()":        0,
	}
	for src, want := range cases {
		if got := evalEnt(t, src); got != want {
			t.Errorf("%s = %v, want %v", src, got, want)
		}
	}
}

// A missing entity, and a name the entity does not answer, both read 0 --
// without ending the expression.
func TestArrowOnAMissingEntityReadsZeroAndContinues(t *testing.T) {
	cases := map[string]float64{
		"c.nobody->v.baa":     0,
		"c.nobody->q.is_baby": 0,
		"c.empty->v.baa":      0,
		"c.empty->q.is_baby":  0,
		// The read is a value like any other, so it takes part in arithmetic.
		"c.nobody->v.baa + 5": 5,
		"c.owner->v.hp + 1":   8,
	}
	for src, want := range cases {
		if got := evalEnt(t, src); got != want {
			t.Errorf("%s = %v, want %v", src, got, want)
		}
	}

	// The assignment still happens, and the statement after it still runs --
	// this is what separates a missing entity from an unresolved variable.
	ctx := entityCtx()
	got, err := Eval("v.becomes_zero = c.nobody->v.foo; return 1;", ctx)
	if err != nil {
		t.Fatalf("eval: %v", err)
	}
	if got != 1 {
		t.Errorf("= %v, want 1 -- a missing entity must not end the expression", got)
	}
	if v, ok := ctx.Scope.Variable["becomes_zero"]; !ok || v != 0 {
		t.Errorf("v.becomes_zero = %v (present=%v), want 0 -- the assignment still happens", v, ok)
	}
}

// Arguments of a query call through the arrow are evaluated even when nothing
// answers, so any RNG draw inside them keeps its place in the sequence.
func TestArrowEvaluatesArgumentsEvenWhenNothingAnswers(t *testing.T) {
	ctx := entityCtx()
	if _, err := Eval("v.n = 0; v.r = c.nobody->q.whatever(v.n = v.n + 1, v.n = v.n + 1);", ctx); err != nil {
		t.Fatalf("eval: %v", err)
	}
	if got := ctx.Scope.Variable["n"]; got != 2 {
		t.Errorf("v.n = %v, want 2 -- both arguments must be evaluated", got)
	}
}

func TestArrowRejectsBadShapes(t *testing.T) {
	refused := []string{
		// Only a context. name on the left.
		"v.x->v.y",
		"t.x->v.y",
		"q.x->v.y",
		"math.pi->v.y",
		// Only variable./query. on the right.
		"c.owner->temp.x",
		"c.owner->math.pi",
		"c.owner->c.other",
		// No chaining.
		"c.a->v.b->v.c",
		// A math call through the arrow is not a thing.
		"c.owner->math.floor(1.5)",
	}
	for _, src := range refused {
		if _, err := Compile(src); err == nil {
			t.Errorf("%q compiled, but it is not a well-formed arrow read", src)
		}
	}
}

// The lexer must keep telling `->` apart from a minus, including where the two
// meet.
func TestArrowDoesNotDisturbMinus(t *testing.T) {
	cases := map[string]float64{
		"5 - 3":    2,
		"5 - -3":   8,
		"-(5 - 3)": -2,
		"5 -3":     2,
		"5- 3":     2,
	}
	for src, want := range cases {
		if got := evalOK(t, src); got != want {
			t.Errorf("%s = %v, want %v", src, got, want)
		}
	}
}

// `this` is a single number the host supplies for the run. The engine's own
// cases use 2.34, which is as good a value as any to pin the shapes with.
func TestThisIsAHostSuppliedNumber(t *testing.T) {
	ctx := newCtx()
	ctx.This = 2.34
	cases := map[string]float64{
		"return this;":      2.3399999141693115,
		"return -this;":     -2.3399999141693115,
		"return -this * 2;": -4.679999828338623,
		"this + 1":          3.3399999141693115,
		"math.floor(this)":  2,
	}
	for src, want := range cases {
		got, err := Eval(src, ctx)
		if err != nil {
			t.Errorf("%s: %v", src, err)
			continue
		}
		if got != want {
			t.Errorf("%s = %v, want %v", src, got, want)
		}
	}
}

// A host that never sets it gets 0, not an error -- the same as every other
// absent host input here.
func TestThisDefaultsToZero(t *testing.T) {
	if got := evalOK(t, "this"); got != 0 {
		t.Errorf("this = %v with no host value, want 0", got)
	}
}

// It is a value, not a namespace: there is nothing to read off it and nothing
// to assign to it.
func TestThisIsNotANamespace(t *testing.T) {
	for _, src := range []string{"this.x", "this = 1;", "this->v.x"} {
		if _, err := Compile(src); err == nil {
			t.Errorf("%q compiled, but `this` is a single value", src)
		}
	}
}
