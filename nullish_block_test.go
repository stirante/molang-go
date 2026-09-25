package molang

import (
	"testing"

	"github.com/stirante/molang-go/printer"
)

// A brace block may stand on the right of `??`, and runs only when the left
// side does not resolve. Vanilla particles default their script-supplied
// variables this way in their creation expressions, so it has to load; this
// is the shape they use.
func TestNullCoalesceBlock(t *testing.T) {
	const vanillaShape = "variable.direction ?? { variable.direction.x = 0.0; variable.direction.y = 1.0; variable.direction.z = 0.0; };"
	if _, err := Compile(vanillaShape); err != nil {
		t.Fatalf("the vanilla particle shape is refused: %v", err)
	}

	cases := map[string]float64{
		// Unresolved: the block runs.
		"v.d ?? { v.d.x = 3; }; return v.d.x;": 3,
		// Resolved: it does not.
		"v.d = 1; v.d ?? { v.hit = 1; }; return v.hit ?? 7;": 7,
		// A resolved zero is a value like any other.
		"v.d = 0; v.d ?? { v.hit = 1; }; return v.hit ?? 7;": 7,
		// The block yields 0, as a block does, so the `??` does too.
		"return v.d ?? { v.x = 5; };": 0,
		// A return inside it ends the program.
		"v.d ?? { return 9; }; return 1;": 9,
	}
	for src, want := range cases {
		if got := evalOK(t, src); got != want {
			t.Errorf("%s = %v, want %v", src, got, want)
		}
	}

	// The left side is held to the usual rule, and the block to the brace
	// rule.
	for _, src := range []string{
		"q.x ?? { v.a = 1; };",
		"v.d ?? { v.a = 1 };",
	} {
		if _, err := Compile(src); err == nil {
			t.Errorf("%q: accepted", src)
		}
	}

	// Both printers write the block back as a block.
	prog, err := Parse("v.d??{v.d.x=1;};")
	if err != nil {
		t.Fatal(err)
	}
	if got, want := printer.Format(prog), "variable.d ?? { variable.d.x = 1; };"; got != want {
		t.Errorf("Format: %q, want %q", got, want)
	}
	if got, want := printer.Minify(prog), "v.d??{v.d.x=1;};"; got != want {
		t.Errorf("Minify: %q, want %q", got, want)
	}
	for _, out := range []string{printer.Format(prog), printer.Minify(prog)} {
		if _, err := Compile(out); err != nil {
			t.Errorf("printed %q does not load: %v", out, err)
		}
	}

	// What it names, the block included.
	refs := References(prog)
	if len(refs.VariableReads) != 1 || len(refs.VariableWrites) != 1 || refs.VariableWrites[0] != "d.x" {
		t.Errorf("References: %+v", refs)
	}
}
