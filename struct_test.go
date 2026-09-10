// struct_test.go pins what a dotted name does on the right of an assignment.
//
// Two things are true at once and they look like they should not be. A dotted
// name is ONE opaque key -- `v.st.height` is the key "st.height", never a field
// of "st" -- which is what language_test.go's TestMemberNamesMayContainDots
// pins, and which real content relies on. And yet `v.y = v.x` carries every
// v.x.<member> across to v.y.<member>, which is what a struct would do.
//
// Both hold because the copy is a prefix scan over the flat keys rather than a
// walk over a structure. Reads split on the first dot; an assignment carries
// the subtree.
package molang

import "testing"

func TestAssignmentCopiesDottedMembers(t *testing.T) {
	cases := map[string]float64{
		// The case that matters: v.x has no value of its own, only members.
		"v.x.x = 1; v.x.y = 2; v.y = v.x; return v.y.x + v.y.y;": 3,
		// Deeper names carry too -- everything after the first dot is part
		// of the key.
		"v.a.b.c = 5; v.z = v.a; return v.z.b.c;": 5,
		// A name with BOTH a value and members carries both.
		"v.x = 7; v.x.m = 8; v.y = v.x; return v.y + v.y.m;": 15,
		// Copying to and from temp. works the same way, in either direction.
		"t.s.a = 4; v.d = t.s; return v.d.a;": 4,
		"v.s.a = 6; t.d = v.s; return t.d.a;": 6,
	}
	for src, want := range cases {
		ctx := newCtx()
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

// The copy is a copy, not an alias: writing to the source afterwards does not
// reach the destination.
func TestStructCopyIsNotAnAlias(t *testing.T) {
	ctx := newCtx()
	got, err := Eval("v.x.a = 1; v.y = v.x; v.x.a = 9; return v.y.a;", ctx)
	if err != nil {
		t.Fatalf("eval: %v", err)
	}
	if got != 1 {
		t.Errorf("v.y.a = %v after the source changed, want 1 -- the copy is a snapshot", got)
	}
}

// A name that is unrelated must not be dragged in by the prefix. `v.xx` is not
// a member of `v.x`, however similar the spelling.
func TestStructCopyDoesNotTakeSimilarNames(t *testing.T) {
	ctx := newCtx()
	if _, err := Eval("v.x.a = 1; v.xx = 2; v.y = v.x;", ctx); err != nil {
		t.Fatalf("eval: %v", err)
	}
	if _, present := ctx.Scope.Variable["yx"]; present {
		t.Error("v.xx was copied as if it were a member of v.x")
	}
	if got := ctx.Scope.Variable["y.a"]; got != 1 {
		t.Errorf("v.y.a = %v, want 1", got)
	}
}

// A source with neither a value nor any member is an ordinary unresolved read,
// and behaves exactly as one outside an assignment: it ends the program unless
// the caller opted out.
func TestCopyingAnUnsetNameIsAnUnresolvedRead(t *testing.T) {
	ctx := newCtx()
	got, err := Eval("v.y = v.nothing; return 5;", ctx)
	if err != nil {
		t.Fatalf("eval: %v", err)
	}
	if got != 0 {
		t.Errorf("= %v, want 0 -- an unresolved read ends the program before the return", got)
	}

	// And it is catchable, like any other unresolved read.
	if v := evalOK(t, "v.y = v.nothing ?? 7; return v.y;"); v != 7 {
		t.Errorf("`?? 7` around the source gave %v, want 7", v)
	}
}
