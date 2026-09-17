// arrow_test.go pins `<entity>-><read>`.
//
// The arrow reads a public variable, or asks a query, of another entity. Its
// two surprising properties are what happens when the left side is not an
// entity -- the read is 0, the right side is skipped, and evaluation carries
// on -- and what the right side sees when it IS one: the other entity's
// variables and queries, but the expression's own temps.
//
// The left side is any operand at all. Only the shape of the right side, a
// chain, an assignment through the arrow and an arrow under `??` are refused,
// each with the game's own wording.
package molang

import (
	"strings"
	"testing"

	"github.com/stirante/molang-go/printer"
)

func entityCtx() *Context {
	ctx := newCtx()
	owner := &Entity{
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
	}
	ctx.Entities = map[string]*Entity{
		"owner": owner,
		"empty": {},
		// Bound, but to nothing: an entity that has since been removed.
		"gone": nil,
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

// An entity reference is a value. It can be stored, passed on, compared, and
// read through from wherever it ended up -- which is the whole point: a
// for_each loop variable is one, and so is a variable a pack keeps a target in.
func TestEntityReferenceIsAValue(t *testing.T) {
	cases := map[string]float64{
		"v.e = c.owner; return v.e->v.hp;":             7,
		"t.e = c.owner; return t.e->q.sum(2, 3);":      5,
		"v.e = c.owner; v.f = v.e; return v.f->v.baa;": 1.25,
		"return (c.owner)->v.hp;":                      7,
		// The same entity reads as the same reference every time.
		"v.e = c.owner; return v.e == c.owner;": 1,
		"return c.owner == c.empty;":            0,
		// Stored across statements, it still reads through.
		"v.e = c.owner; v.n = v.e->v.hp; return v.n + 1;": 8,
	}
	for src, want := range cases {
		if got := evalEnt(t, src); got != want {
			t.Errorf("%s = %v, want %v", src, got, want)
		}
	}

	// And across runs of the same Context.
	ctx := entityCtx()
	if _, err := Eval("v.e = c.owner;", ctx); err != nil {
		t.Fatal(err)
	}
	got, err := Eval("v.e->v.hp", ctx)
	if err != nil {
		t.Fatal(err)
	}
	if got != 7 {
		t.Errorf("v.e->v.hp in a later run = %v, want 7", got)
	}
}

// A left side that is not an entity reads 0 without ending the expression:
// a plain number, a string, `this`, a removed entity, a name the entity does
// not answer.
func TestArrowOnANonEntityReadsZeroAndContinues(t *testing.T) {
	cases := map[string]float64{
		"c.gone->v.baa":      0,
		"c.gone->q.is_baby":  0,
		"c.empty->v.baa":     0,
		"c.empty->q.is_baby": 0,
		"1->v.baa":           0,
		"'owner'->v.baa":     0,
		"this->v.baa":        0,
		"(1 + 2)->q.is_baby": 0,
		"v.baa->v.hp":        0,
		"math.pi->v.hp":      0,
		// The read is a value like any other, so it takes part in arithmetic.
		"c.gone->v.baa + 5": 5,
		"c.owner->v.hp + 1": 8,
	}
	for src, want := range cases {
		if got := evalEnt(t, src); got != want {
			t.Errorf("%s = %v, want %v", src, got, want)
		}
	}

	// The assignment still happens, and the statement after it still runs --
	// this is what separates a missing entity from an unresolved variable.
	ctx := entityCtx()
	got, err := Eval("v.becomes_zero = c.gone->v.foo; return 1;", ctx)
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

// A context. name the host never bound is an ordinary unresolved context
// read, and that DOES end the expression -- the arrow never gets as far as
// asking whether there is an entity. The line between the two: bound to
// nothing is an entity that is gone, and reads 0; never bound is a name the
// host forgot, and stops.
func TestArrowOnAnUnboundContextNameIsAnUnresolvedRead(t *testing.T) {
	ctx := entityCtx()
	var reported []string
	ctx.OnUnresolvedRead = func(name string) { reported = append(reported, name) }
	got, err := Eval("v.after = c.nobody->v.foo; return 1;", ctx)
	if err != nil {
		t.Fatalf("eval: %v", err)
	}
	if got != 0 {
		t.Errorf("= %v, want 0 -- the unresolved read ends the program", got)
	}
	if _, assigned := ctx.Scope.Variable["after"]; assigned {
		t.Error("v.after was assigned, but the program should have stopped first")
	}
	if strings.Join(reported, ",") != "context.nobody" {
		t.Errorf("reported %v, want [context.nobody]", reported)
	}

	// Under ContinueOnUnresolvedRead the name reads 0, which is not an
	// entity, so the arrow reads 0 and the program carries on.
	ctx = entityCtx()
	ctx.ContinueOnUnresolvedRead = true
	got, err = Eval("v.after = c.nobody->v.foo; return 1;", ctx)
	if err != nil {
		t.Fatalf("eval: %v", err)
	}
	if got != 1 {
		t.Errorf("= %v, want 1 under ContinueOnUnresolvedRead", got)
	}
}

// When nothing answers, the right side is skipped entirely -- the arguments
// of a query call through the arrow are NOT evaluated, so an assignment or a
// random draw inside them does not happen.
func TestArrowSkipsTheRightSideWhenNothingAnswers(t *testing.T) {
	ctx := entityCtx()
	if _, err := Eval("v.n = 0; v.r = c.gone->q.whatever(v.n = v.n + 1, v.n = v.n + 1);", ctx); err != nil {
		t.Fatalf("eval: %v", err)
	}
	if got := ctx.Scope.Variable["n"]; got != 0 {
		t.Errorf("v.n = %v, want 0 -- the arguments must not be evaluated", got)
	}
	if got := ctx.Scope.Variable["r"]; got != 0 {
		t.Errorf("v.r = %v, want 0", got)
	}

	// A random draw inside the arguments is not consumed either: the draw
	// after the arrow gets the FIRST value of the sequence.
	ctx = entityCtx()
	got, err := Eval("v.r = c.gone->q.whatever(math.random(0, 1)); return math.random(0, 1);", ctx)
	if err != nil {
		t.Fatalf("eval: %v", err)
	}
	if got != 0.25 {
		t.Errorf("draw after a skipped argument list = %v, want 0.25 (the first draw)", got)
	}
}

// The right side is evaluated in the OTHER entity's scope: a query call's
// arguments read the other entity's variables and ask its queries, while
// temps stay the expression's own.
func TestArrowEvaluatesTheRightSideAsTheOtherEntity(t *testing.T) {
	ctx := entityCtx()
	ctx.Scope.Variable["hp"] = 1000
	ctx.Scope.Temp["bonus"] = 3
	cases := map[string]float64{
		// v.hp is the owner's 7, not the expression's own 1000.
		"c.owner->q.sum(v.hp, 1)": 8,
		// q.is_baby inside the arguments is the owner's too.
		"c.owner->q.sum(q.is_baby, q.is_baby)": 2,
		// t.bonus is the expression's own temp.
		"c.owner->q.sum(t.bonus, v.hp)": 10,
		// A variable the other entity has not published reads 0 rather than
		// stopping the expression.
		"c.owner->q.sum(v.unpublished, 1)": 1,
		"c.owner->v.unpublished + 4":       4,
	}
	for src, want := range cases {
		got, err := Eval(src, ctx)
		if err != nil {
			t.Fatalf("Eval(%q): %v", src, err)
		}
		if got != want {
			t.Errorf("%s = %v, want %v", src, got, want)
		}
	}
}

// A host-wide query function is asked when the entity has none of its own,
// and can tell who is being asked from CurrentEntity.
func TestArrowFallsBackToHostQueriesWithCurrentEntity(t *testing.T) {
	ctx := entityCtx()
	owner := ctx.Entities["owner"]
	var asked []*Entity
	ctx.QueryFuncs = map[string]QueryFunc{
		"health": func(_ []float64, ctx *Context) float64 {
			e := ctx.CurrentEntity()
			asked = append(asked, e)
			if e == nil {
				return -1
			}
			return e.Variable["hp"]
		},
	}
	got, err := Eval("c.owner->q.health + q.health()", ctx)
	if err != nil {
		t.Fatalf("eval: %v", err)
	}
	// 7 through the arrow, then -1 for the expression's own call.
	if got != 6 {
		t.Errorf("= %v, want 6", got)
	}
	if len(asked) != 2 || asked[0] != owner || asked[1] != nil {
		t.Errorf("the host query was asked as %v, want [owner, nil]", asked)
	}
	if ctx.CurrentEntity() != nil {
		t.Error("CurrentEntity is still set after the arrow finished")
	}
}

// The current entity is restored however the right side ends -- here by an
// unresolved temp. read inside the arguments, which aborts the program.
func TestArrowRestoresTheCurrentEntityOnAbort(t *testing.T) {
	ctx := entityCtx()
	if _, err := Eval("c.owner->q.sum(t.unset)", ctx); err != nil {
		t.Fatal(err)
	}
	if ctx.CurrentEntity() != nil {
		t.Error("CurrentEntity is still set after an aborted right side")
	}
}

// A reference the Context has forgotten reads as no entity at all.
func TestClearedEntityReferencesReadZero(t *testing.T) {
	ctx := entityCtx()
	if _, err := Eval("v.e = c.owner;", ctx); err != nil {
		t.Fatal(err)
	}
	ctx.ClearEntityRefs()
	got, err := Eval("v.e->v.hp + 1", ctx)
	if err != nil {
		t.Fatal(err)
	}
	if got != 1 {
		t.Errorf("stale reference read %v, want 1 (0 through the arrow)", got)
	}
}

// The arrow binds tighter than every operator, unary ones included, and
// applies after grouping, calls and indexing.
func TestArrowPrecedence(t *testing.T) {
	cases := map[string]float64{
		"-c.owner->v.hp":                              -7,
		"!c.owner->q.is_baby":                         0,
		"c.owner->v.hp * 2":                           14,
		"2 * c.owner->v.hp":                           14,
		"c.owner->v.hp + 1":                           8,
		"c.owner->v.hp > 6":                           1,
		"c.owner->v.hp ? 3 : 4":                       3,
		"c.gone->v.hp ? 3 : 4":                        4,
		"c.owner->q.sum(1, 2) * c.owner->q.sum(3, 4)": 21,
		// A parenthesised left side is grouped first.
		"(1 ? c.owner : c.empty)->v.hp": 7,
	}
	for src, want := range cases {
		if got := evalEnt(t, src); got != want {
			t.Errorf("%s = %v, want %v", src, got, want)
		}
	}
}

// Every shape the game accepts on the left, and every one it accepts on the
// right.
func TestArrowAcceptedShapes(t *testing.T) {
	for _, src := range []string{
		"c.owner->v.hp",
		"v.e->v.hp",
		"t.e->q.hp",
		"q.owner->v.hp",
		"q.owner(1)->v.hp",
		"math.pi->v.hp",
		"math.floor(1.5)->v.hp",
		"1->v.hp",
		"'a'->v.hp",
		"this->v.hp",
		"(c.owner)->v.hp",
		"(v.a + 1)->v.hp",
		"array.a[0]->v.hp",
		"c.owner->q.hp",
		"c.owner->q.hp()",
		"c.owner->q.hp(1, v.x, t.y)",
		"c.owner->v.hp(1)",
	} {
		if _, err := Compile(src); err != nil {
			t.Errorf("%q: %v", src, err)
		}
	}
}

func TestArrowRejectsBadShapes(t *testing.T) {
	refused := map[string]string{
		// Only variable./query. on the right.
		"c.owner->temp.x":          "right-hand-side of pointer expression",
		"c.owner->t.x":             "right-hand-side of pointer expression",
		"c.owner->math.pi":         "right-hand-side of pointer expression",
		"c.owner->c.other":         "right-hand-side of pointer expression",
		"c.owner->math.floor(1.5)": "right-hand-side of pointer expression",
		"c.owner->array.a[0]":      "right-hand-side of pointer expression",
		"c.owner->1":               "right-hand-side of pointer expression",
		"c.owner->(v.x)":           "right-hand-side of pointer expression",
		"c.owner->this":            "right-hand-side of pointer expression",
		"c.owner->{v.x;}":          "right-hand-side of pointer expression",
		// A dotted path is a member access, not a variable read.
		"c.owner->v.a.b": "right-hand-side of pointer expression",
		// No chaining, with or without parentheses.
		"c.a->v.b->v.c":   "nested pointer statements",
		"(c.a->v.b)->v.c": "nested pointer statements",
		// No writing through the arrow, whatever is on either side of it.
		"c.owner->v.x = 1;":    "Assignment attempted on Pointer result",
		"v.owner->v.x = 1;":    "Assignment attempted on Pointer result",
		"t.owner->v.x = 1;":    "Assignment attempted on Pointer result",
		"q.owner()->v.x = 1;":  "Assignment attempted on Pointer result",
		"c.owner->q.hp = 1;":   "assignment to non-variable not allowed. Expression is trying to assign to a: Pointer '->'",
		"c.owner->q.hp() = 1;": "assignment to non-variable not allowed. Expression is trying to assign to a: Pointer '->'",
		// No arrow on the left of `??`.
		"c.owner->v.x ?? 0": "left-hand-side of ?? expression",
		// A dangling arrow.
		"c.owner->": "right-hand-side of pointer expression",
		"->v.x":     "unexpected token",
	}
	for src, want := range refused {
		_, err := Compile(src)
		if err == nil {
			t.Errorf("%q compiled, but it is not a well-formed arrow read", src)
			continue
		}
		if !strings.Contains(err.Error(), want) {
			t.Errorf("%q: error %q does not mention %q", src, err, want)
		}
	}
}

// Printing keeps the arrow where it was and adds parentheses only where the
// left side needs them; the output reparses to the same program.
func TestArrowPrintsAndReparses(t *testing.T) {
	cases := []struct{ src, format, minify string }{
		{"c.owner->v.hp", "context.owner->variable.hp", "c.owner->v.hp"},
		{"v.e->q.sum(1, t.x)", "variable.e->query.sum(1, temp.x)", "v.e->q.sum(1,t.x)"},
		{"-c.owner->v.hp * 2", "-context.owner->variable.hp * 2", "-c.owner->v.hp*2"},
		{"(v.a + 1)->v.hp", "(variable.a + 1)->variable.hp", "(v.a+1)->v.hp"},
		{"(1 ? v.a : v.b)->q.x", "(1 ? variable.a : variable.b)->query.x", "(1?v.a:v.b)->q.x"},
		{"this->v.hp + 1", "this->variable.hp + 1", "this->v.hp+1"},
		{"math.floor(1.5)->v.hp", "math.floor(1.5)->variable.hp", "math.floor(1.5)->v.hp"},
		{"array.a[t.i]->v.hp", "array.a[temp.i]->variable.hp", "array.a[t.i]->v.hp"},
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

func TestArrowReferences(t *testing.T) {
	r := refsOf(t, "c.owner->q.health + v.e->v.hp + t.e->q.sum(v.arg)")
	if strings.Join(r.Entities, ",") != "owner" {
		t.Errorf("Entities = %v, want [owner] -- only a context. name on the left is a dereference", r.Entities)
	}
	if strings.Join(r.VariableReads, ",") != "arg,e,hp" {
		t.Errorf("VariableReads = %v, want [arg e hp]", r.VariableReads)
	}
	if strings.Join(r.TempReads, ",") != "e" {
		t.Errorf("TempReads = %v, want [e]", r.TempReads)
	}
	if strings.Join(r.Queries, ",") != "health,sum" {
		t.Errorf("Queries = %v, want [health sum]", r.Queries)
	}
	if len(r.ContextReads) != 0 {
		t.Errorf("ContextReads = %v, want none", r.ContextReads)
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
// to assign to it. (`this->v.x` is accepted, as any operand is on the left of
// an arrow; a number is not an entity, so it reads 0.)
func TestThisIsNotANamespace(t *testing.T) {
	for _, src := range []string{"this.x", "this.some", "this.some = 1;", "this = 1;"} {
		if _, err := Compile(src); err == nil {
			t.Errorf("%q compiled, but `this` is a single value", src)
		}
	}
}
