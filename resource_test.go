// resource_test.go pins geometry., material. and texture.
//
// A resource is a NAME, not a number. Two can be compared, a ternary can
// select between them, and arithmetic on one is refused -- the same shape as
// a string literal, and modelled the same way, through the intern table.
package molang

import "testing"

func TestResourceNamesParseWithDots(t *testing.T) {
	for _, src := range []string{
		"geometry.example.name",
		"material.example.name",
		"texture.example.name",
		"geometry.foo",
		"texture.a.b.c.d",
	} {
		if _, err := Compile(src); err != nil {
			t.Errorf("%q was refused: %v", src, err)
		}
	}
}

// The value is an identity, not a quantity: the same name is always the same
// value, different names never collide, and the three namespaces do not
// collide with each other even for the same member.
func TestResourcesCompareByName(t *testing.T) {
	cases := map[string]float64{
		"texture.a == texture.a":   1,
		"texture.a == texture.b":   0,
		"texture.a != texture.b":   1,
		"geometry.a == material.a": 0,
		"geometry.a == geometry.a": 1,
		// Selecting between two resources is the thing packs actually do.
		"1 ? texture.a : texture.b": func() float64 { return 0 }(),
	}
	// The ternary case needs its own comparison, since the value is an
	// opaque id rather than a number to write down.
	delete(cases, "1 ? texture.a : texture.b")
	for src, want := range cases {
		if got := evalOK(t, src); got != want {
			t.Errorf("%s = %v, want %v", src, got, want)
		}
	}

	chosen := evalOK(t, "1 ? texture.a : texture.b")
	if want := evalOK(t, "texture.a"); chosen != want {
		t.Errorf("a ternary between two resources gave %v, want texture.a's own value %v", chosen, want)
	}
	other := evalOK(t, "0 ? texture.a : texture.b")
	if want := evalOK(t, "texture.b"); other != want {
		t.Errorf("the false branch gave %v, want texture.b's own value %v", other, want)
	}
	if chosen == other {
		t.Error("texture.a and texture.b have the same value; resource names must be distinct")
	}
}

// Arithmetic on a resource is a parse error, the same way it is on an array
// element. A resource names a thing; there is nothing to add to it.
func TestResourceIsNotABinaryOperand(t *testing.T) {
	refused := []string{
		"geometry.foo + 1",
		"material.foo + 1",
		"texture.foo + 1",
		"1 + texture.foo",
		"texture.foo * 2",
		"texture.foo - texture.bar",
	}
	for _, src := range refused {
		if _, err := Compile(src); err == nil {
			t.Errorf("%q compiled, but a resource is a name rather than a number", src)
		}
	}

	// What must keep working: comparison for identity, and selection.
	accepted := []string{
		"texture.foo == texture.bar",
		"texture.foo != texture.bar",
		"1 ? texture.foo : texture.bar",
		"v.x = texture.foo;",
		"return texture.foo;",
	}
	for _, src := range accepted {
		if _, err := Compile(src); err != nil {
			t.Errorf("%q was refused: %v", src, err)
		}
	}
}

// A resource value must not collide with a plain number an expression can
// produce, or `v.x == texture.foo` would match by accident.
func TestResourceValuesDoNotCollideWithOrdinaryNumbers(t *testing.T) {
	for _, src := range []string{"texture.a", "geometry.b", "material.c"} {
		v := evalOK(t, src)
		if v >= -1e6 && v <= 1e6 {
			t.Errorf("%s = %v, which sits in the range ordinary arithmetic produces; "+
				"a resource id must not be mistakable for a computed number", src, v)
		}
	}
}
