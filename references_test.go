// references_test.go pins what References reports.
//
// This is the API a tool needs before it can do anything with an expression it
// did not write: an editor prompting for values, a pack-wide rewriter deciding
// what it may rename, something plotting an expression over a time query and
// needing to know whether a random draw makes the plot meaningless.
package molang

import (
	"reflect"
	"testing"
)

func refsOf(t *testing.T, src string) Refs {
	t.Helper()
	tree, err := ParseWith(src, Extensions{Comments: true})
	if err != nil {
		t.Fatalf("Parse(%q): %v", src, err)
	}
	return References(tree)
}

func TestReferencesSeparatesReadsFromWrites(t *testing.T) {
	r := refsOf(t, "v.n = v.n + t.step; t.acc = 1; return v.n + c.moo;")

	want := Refs{
		VariableReads:  []string{"n"},
		VariableWrites: []string{"n"},
		TempReads:      []string{"step"},
		TempWrites:     []string{"acc"},
		ContextReads:   []string{"moo"},
	}
	if !reflect.DeepEqual(r, want) {
		t.Errorf("References =\n  %+v\nwant\n  %+v", r, want)
	}
}

func TestReferencesCoversEveryNamespace(t *testing.T) {
	src := "c.owner->q.health + math.floor(array.vals[t.i]) + " +
		"(texture.skin == geometry.body ? 1 : 0) + this + q.anim_time"
	r := refsOf(t, src)

	checks := []struct {
		name string
		got  []string
		want []string
	}{
		{"Queries", r.Queries, []string{"anim_time", "health"}},
		{"MathFuncs", r.MathFuncs, []string{"floor"}},
		{"Arrays", r.Arrays, []string{"vals"}},
		{"Entities", r.Entities, []string{"owner"}},
		{"Resources", r.Resources, []string{"geometry.body", "texture.skin"}},
		{"TempReads", r.TempReads, []string{"i"}},
	}
	for _, c := range checks {
		if !reflect.DeepEqual(c.got, c.want) {
			t.Errorf("%s = %v, want %v", c.name, c.got, c.want)
		}
	}
	if !r.UsesThis {
		t.Error("UsesThis = false, but the expression contains `this`")
	}
	// An entity dereferenced with `->` is not a context READ.
	if len(r.ContextReads) != 0 {
		t.Errorf("ContextReads = %v, want none -- c.owner is dereferenced, not read", r.ContextReads)
	}
}

func TestReferencesFindsRandomDraws(t *testing.T) {
	for _, src := range []string{
		"math.random(0, 1)",
		"math.random_integer(1, 6)",
		"math.die_roll(2, 1, 6)",
		"math.die_roll_integer(2, 1, 6)",
		"v.x = 1 + math.floor(math.random(0, 3));",
	} {
		if !refsOf(t, src).UsesRandom {
			t.Errorf("%q: UsesRandom = false, but it draws", src)
		}
	}
	for _, src := range []string{
		"math.floor(1.5)",
		"v.x = q.anim_time * 2;",
		"math.sin(q.anim_time)",
	} {
		if refsOf(t, src).UsesRandom {
			t.Errorf("%q: UsesRandom = true, but nothing in it draws", src)
		}
	}
}

// The walk must reach inside every construct, not just top-level statements.
func TestReferencesReachesIntoEveryConstruct(t *testing.T) {
	cases := map[string]string{
		"loop body":        "loop(3, {v.inside = t.src;});",
		"for_each body":    "for_each(t.e, array.list, {v.inside = t.src;});",
		"conditional body": "1 ? {v.inside = t.src;};",
		"else branch":      "0 ? {v.a = 1;} : {v.inside = t.src;};",
		"ternary arm":      "1 ? (v.inside = t.src) : 0",
		"call argument":    "math.max(v.inside = t.src, 0)",
		"array index":      "array.list[t.src]",
		"arrow read":       "c.e->v.inside",
		"unary operand":    "-t.src",
	}
	for what, src := range cases {
		r := refsOf(t, src)
		found := len(r.TempReads) > 0 || len(r.VariableWrites) > 0 ||
			len(r.VariableReads) > 0 || len(r.Arrays) > 0
		if !found {
			t.Errorf("%s (%q): the walk found nothing inside", what, src)
		}
	}

	// for_each writes its loop variable on every pass.
	r := refsOf(t, "for_each(t.e, array.list, {v.n = t.e;});")
	if !reflect.DeepEqual(r.TempWrites, []string{"e"}) {
		t.Errorf("for_each loop variable: TempWrites = %v, want [e]", r.TempWrites)
	}
	if !reflect.DeepEqual(r.Arrays, []string{"list"}) {
		t.Errorf("for_each source: Arrays = %v, want [list]", r.Arrays)
	}
}

// NeedsFromHost is a floor, and the test says so in both directions rather
// than pretending it is exact.
func TestNeedsFromHostIsALowerBound(t *testing.T) {
	// Read and never written: definitely needed.
	got := refsOf(t, "return v.a + t.b + c.d;").NeedsFromHost()
	want := []string{"context.d", "temp.b", "variable.a"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("NeedsFromHost = %v, want %v", got, want)
	}

	// Written before read: not needed, and correctly absent.
	if got := refsOf(t, "v.a = 1; return v.a;").NeedsFromHost(); len(got) != 0 {
		t.Errorf("NeedsFromHost = %v, want none -- the program writes it first", got)
	}

	// The under-report this is documented to have: written only inside a
	// branch that may not run, so the name is treated as written even though
	// a host might still have to supply it.
	if got := refsOf(t, "0 ? {v.a = 1;}; return v.a;").NeedsFromHost(); len(got) != 0 {
		t.Errorf("NeedsFromHost = %v; the documented floor treats a conditional "+
			"write as a write, so this should be empty", got)
	}
	// The full read list is the complete set a caller can fall back to.
	if r := refsOf(t, "0 ? {v.a = 1;}; return v.a;"); !reflect.DeepEqual(r.VariableReads, []string{"a"}) {
		t.Errorf("VariableReads = %v, want [a] -- the complete set must still show it", r.VariableReads)
	}
}

// Names come back lower-cased, deduplicated and sorted, so two Refs over
// equivalent programs compare equal.
func TestReferencesAreNormalised(t *testing.T) {
	a := refsOf(t, "return V.Zed + v.alpha + VARIABLE.zed;")
	b := refsOf(t, "return v.alpha + v.zed;")
	if !reflect.DeepEqual(a.VariableReads, b.VariableReads) {
		t.Errorf("%v != %v -- names must be lower-cased, deduplicated and sorted",
			a.VariableReads, b.VariableReads)
	}
	if !reflect.DeepEqual(a.VariableReads, []string{"alpha", "zed"}) {
		t.Errorf("VariableReads = %v, want [alpha zed]", a.VariableReads)
	}
}

// A dotted member is one name, so it comes back whole and usable as a scope
// key exactly as it is.
func TestReferencesKeepDottedNamesWhole(t *testing.T) {
	r := refsOf(t, "return v.st.can_place_in_cave;")
	if !reflect.DeepEqual(r.VariableReads, []string{"st.can_place_in_cave"}) {
		t.Errorf("VariableReads = %v, want [st.can_place_in_cave]", r.VariableReads)
	}
}
