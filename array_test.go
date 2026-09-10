// array_test.go pins `array.<name>[index]`.
//
// The index rule is the whole of it, and it is not the one any other language
// would give you: the index is truncated toward zero, a negative index reads
// element 0, and a positive one wraps modulo the length. An array read is
// therefore TOTAL -- it cannot fail and cannot go out of range, whatever the
// index expression works out to.
//
// That is worth a file of its own because it is the opposite of the usual
// bargain. There is no bounds error and no sentinel, so an off-by-one reads a
// neighbouring element in silence and the author never finds out.
package molang

import "testing"

func arrayCtx(arrays map[string][]float64) *Context {
	ctx := newCtx()
	for name, values := range arrays {
		ctx.Scope.Array[name] = values
	}
	return ctx
}

func evalArr(t *testing.T, src string, arrays map[string][]float64) float64 {
	t.Helper()
	v, err := Eval(src, arrayCtx(arrays))
	if err != nil {
		t.Fatalf("Eval(%q): %v", src, err)
	}
	return v
}

func TestArrayIndexWrapsAndClampsNegative(t *testing.T) {
	arr := map[string][]float64{"test": {1, 2, 3}}
	cases := map[string]float64{
		"array.test[0]": 1,
		"array.test[1]": 2,
		"array.test[2]": 3,
		// Past the end wraps.
		"array.test[3]": 1,
		"array.test[6]": 1,
		"array.test[4]": 2,
		// Negative does NOT wrap -- it reads the first element.
		"array.test[-1]": 1,
		"array.test[-2]": 1,
		"array.test[-9]": 1,
	}
	for src, want := range cases {
		if got := evalArr(t, src, arr); got != want {
			t.Errorf("%s = %v, want %v", src, got, want)
		}
	}
}

func TestArrayIndexTruncatesTowardZero(t *testing.T) {
	arr := map[string][]float64{"test": {0, 1, 2}}
	cases := map[string]float64{
		// The index is an expression, and it is truncated, not rounded.
		"array.test[1 + 1]":         2,
		"array.test[2 + 2]":         1,
		"array.test[1 + 0.5]":       1,
		"array.test[1 + 0.5 + 0.5]": 2,
		// 1.999 truncates to 1, where rounding would give 2.
		"array.test[1 + 0.5 + 0.499]": 1,
		"array.test[4.5]":             1,
		// Truncation is toward zero, so -4.5 becomes -4, and a negative
		// index then reads element 0.
		"array.test[-4.5]": 0,
	}
	for src, want := range cases {
		if got := evalArr(t, src, arr); got != want {
			t.Errorf("%s = %v, want %v", src, got, want)
		}
	}
}

// An array the host never registered, and one registered empty, both read 0.
// There is no element to wrap onto and nothing to report -- the read is still
// total.
func TestArrayMissingOrEmptyReadsZero(t *testing.T) {
	cases := []struct {
		name   string
		arrays map[string][]float64
	}{
		{"unregistered", map[string][]float64{}},
		{"empty", map[string][]float64{"test": {}}},
	}
	for _, c := range cases {
		for _, src := range []string{"array.test[0]", "array.test[5]", "array.test[-1]"} {
			if got := evalArr(t, src, c.arrays); got != 0 {
				t.Errorf("%s (%s array) = %v, want 0", src, c.name, got)
			}
		}
	}
}

// The index is an ordinary expression, so it can read variables and call math.
func TestArrayIndexIsAFullExpression(t *testing.T) {
	arr := map[string][]float64{"test": {10, 20, 30}}
	cases := map[string]float64{
		"v.i = 1; return array.test[v.i];":              20,
		"v.i = 2; return array.test[v.i - 1];":          20,
		"array.test[math.floor(2.9)]":                   30,
		"v.i = 1; v.j = 1; return array.test[v.i+v.j];": 30,
	}
	for src, want := range cases {
		if got := evalArr(t, src, arr); got != want {
			t.Errorf("%s = %v, want %v", src, got, want)
		}
	}
}

// `array.<name>` alone is not a value. Only the indexed form is an expression,
// so a bare array name must not parse.
func TestBareArrayNameIsNotAnExpression(t *testing.T) {
	for _, src := range []string{"array.test", "array.test + 1", "v.x = array.test;"} {
		if _, err := Compile(src); err == nil {
			t.Errorf("%q compiled, but an array name is only a value when indexed", src)
		}
	}
}

// Values coming out of an array are rounded like every other value: an array
// the host filled with float64 precision still reads as float32.
func TestArrayValuesAreRoundedToFloat32(t *testing.T) {
	arr := map[string][]float64{"test": {0.1, 0.2}}
	got := evalArr(t, "array.test[0]", arr)
	if float64(float32(got)) != got {
		t.Errorf("array.test[0] = %v, which is not a float32", got)
	}
}

// An array element cannot be an operand of a binary operator. Arithmetic
// inside the index is fine; arithmetic on what comes out is refused, the same
// way the game refuses it.
func TestArrayElementIsNotABinaryOperand(t *testing.T) {
	refused := []string{
		"array.test[1 + 1] + 1",
		"1 + array.test[0]",
		"array.test[0] - 1",
		"array.test[0] * 2",
		"array.test[0] / 2",
		"array.test[0] == 1",
		"array.test[0] < 1",
		"array.test[0] && 1",
		"array.test[0] || 1",
	}
	for _, src := range refused {
		if _, err := Compile(src); err == nil {
			t.Errorf("%q compiled, but an array element cannot be an operand", src)
		}
	}

	// The forms that must keep working.
	accepted := []string{
		"array.test[1 + 1]",
		"array.test[v.i * 2 - 1]",
		"math.floor(array.test[0])",
		"array.test[0] ? 1 : 2",
		"v.x = array.test[0];",
		"return array.test[0];",
	}
	for _, src := range accepted {
		if _, err := Compile(src); err != nil {
			t.Errorf("%q was refused: %v", src, err)
		}
	}
}
