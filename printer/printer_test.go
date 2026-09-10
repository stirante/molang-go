package printer_test

import (
	"strings"
	"testing"

	"molang-go/eval"
	"molang-go/parser"
	"molang-go/printer"
)

type stubRNG struct{ n int }

func (s *stubRNG) NextFloat() float64 { s.n++; return 0.5 }
func (s *stubRNG) NextIntBound(b int) int {
	if b <= 0 {
		return 0
	}
	s.n++
	return (s.n - 1) % b
}

func run(t *testing.T, src string) float64 {
	t.Helper()
	tree, err := parser.Parse(src)
	if err != nil {
		t.Fatalf("parse(%q): %v", src, err)
	}
	prog, err := eval.Compile(tree)
	if err != nil {
		t.Fatalf("compile(%q): %v", src, err)
	}
	ctx := &eval.Context{RNG: &stubRNG{}, Scope: eval.NewScope()}
	return prog.Run(ctx)
}

var samples = []string{
	"3 + 4 * 2",
	"(3 + 4) * 2",
	"3 - (4 - 2)",
	"3 - 4 - 2",
	"-5 + 2",
	"!0",
	"1 < 2 && 2 < 3",
	"1 == 1 || 0 == 1",
	"0.0/0.0 ?? 5",
	"1 ? 2 : 3",
	"0 ? 99",
	"math.sin(30) + math.cos(60)",
	"math.clamp(5, 0, 3)",
	"t.x = 5; return t.x * 2;",
	"t.x = 1; t.x = 2;",
	"v.a = query.foo; return v.a;",
	"t.x = 0; loop(5, { t.x = t.x + 1; }); return t.x;",
	"t.i = 0; t.sum = 0; loop(10, { t.i = t.i + 1; t.i > 5 ? { break; }; t.i == 3 ? { continue; }; t.sum = t.sum + t.i; }); return t.sum;",
	"t.x = 5; t.x > 3 ? { return 1; } : { return 0; };",
	"t.x = 1; t.x == 1 ? { return 1; } : t.x == 2 ? { return 2; } : { return 3; };",
	"math.random(0, 1) + math.random_integer(0, 10)",
	"'hello' == 'hello'",
	"true && false",
	"math.floor(1.5) + q.foo + v.bar + t.baz",
	"context.block_face",
	"-(-5)",
	"!(!0)",
	"1 - -1",
}

func TestFormatRoundTrip(t *testing.T) {
	for _, src := range samples {
		tree, err := parser.Parse(src)
		if err != nil {
			t.Fatalf("parse(%q): %v", src, err)
		}
		formatted := printer.Format(tree)
		reparsed, err := parser.Parse(formatted)
		if err != nil {
			t.Fatalf("src=%q formatted=%q reparse error: %v", src, formatted, err)
		}
		want := run(t, src)
		progF, err := eval.Compile(reparsed)
		if err != nil {
			t.Fatalf("compile formatted(%q): %v", formatted, err)
		}
		got := progF.Run(&eval.Context{RNG: &stubRNG{}, Scope: eval.NewScope()})
		if got != want {
			t.Errorf("Format round-trip mismatch\n  src:       %q\n  formatted: %q\n  want=%v got=%v", src, formatted, want, got)
		}
	}
}

func TestMinifyRoundTrip(t *testing.T) {
	for _, src := range samples {
		tree, err := parser.Parse(src)
		if err != nil {
			t.Fatalf("parse(%q): %v", src, err)
		}
		minified := printer.Minify(tree)
		reparsed, err := parser.Parse(minified)
		if err != nil {
			t.Fatalf("src=%q minified=%q reparse error: %v", src, minified, err)
		}
		want := run(t, src)
		progM, err := eval.Compile(reparsed)
		if err != nil {
			t.Fatalf("compile minified(%q): %v", minified, err)
		}
		got := progM.Run(&eval.Context{RNG: &stubRNG{}, Scope: eval.NewScope()})
		if got != want {
			t.Errorf("Minify round-trip mismatch\n  src:      %q\n  minified: %q\n  want=%v got=%v", src, minified, want, got)
		}
	}
}

func TestMinifyIsNotLonger(t *testing.T) {
	for _, src := range samples {
		tree, _ := parser.Parse(src)
		minified := printer.Minify(tree)
		formatted := printer.Format(tree)
		if len(minified) > len(formatted) {
			t.Errorf("minify longer than format for %q: minify=%q (%d) format=%q (%d)", src, minified, len(minified), formatted, len(formatted))
		}
	}
}

// TestTrailingSemicolonSurvivesPrinting: a top-level ';' is semantics, not
// punctuation (see ast.Program.HasSemicolon and eval.Compile). Format used
// to append one to every single-statement non-expression program, and
// Minify used to drop the one on `1+1;` -- either rewrite changes what the
// program evaluates to, so both directions are pinned here.
func TestTrailingSemicolonSurvivesPrinting(t *testing.T) {
	cases := []struct{ src, wantFormat, wantMinify string }{
		{"1+1", "1 + 1", "1+1"},
		{"1+1;", "1 + 1;", "1+1;"},
		{"temp.a = 5;", "temp.a = 5;", "t.a=5;"},
		{"return 3;", "return 3;", "return 3;"},
		// No top-level ';': a bare grouping block must NOT gain one.
		{"{return 7;}", "true ? { return 7; }", "1?{return 7}"},
		// Multi-statement programs need no trailing ';' -- the separators
		// already mark them as sequences on reparse.
		{"temp.a=1;temp.b=2;", "temp.a = 1; temp.b = 2;", "t.a=1;t.b=2"},
	}
	for _, c := range cases {
		tree, err := parser.Parse(c.src)
		if err != nil {
			t.Fatalf("parse(%q): %v", c.src, err)
		}
		if got := printer.Format(tree); got != c.wantFormat {
			t.Errorf("Format(%q) = %q, want %q", c.src, got, c.wantFormat)
		}
		tree2, _ := parser.Parse(c.src)
		if got := printer.Minify(tree2); got != c.wantMinify {
			t.Errorf("Minify(%q) = %q, want %q", c.src, got, c.wantMinify)
		}
	}
}

// TestPrintRoundTripPreservesProgramValue is the property the case table
// above is a readable stand-in for: reparsing either printed form must
// evaluate to what the original did, for programs that differ ONLY in a
// trailing ';'.
func TestPrintRoundTripPreservesProgramValue(t *testing.T) {
	run := func(src string) float64 {
		t.Helper()
		tree, err := parser.Parse(src)
		if err != nil {
			t.Fatalf("parse(%q): %v", src, err)
		}
		prog, err := eval.Compile(tree)
		if err != nil {
			t.Fatalf("compile(%q): %v", src, err)
		}
		return prog.Run(&eval.Context{RNG: &stubRNG{}, Scope: eval.NewScope()})
	}
	for _, src := range []string{
		"1+1", "1+1;", "temp.a=5", "temp.a=5;", "return 3;",
		"{return 7;}", "{temp.a=5;}", "temp.a=1;temp.b=2;",
		"loop(2,{temp.a=temp.a+1;});return temp.a;",
	} {
		want := run(src)
		tree, _ := parser.Parse(src)
		if got := run(printer.Format(tree)); got != want {
			t.Errorf("Format round-trip of %q: got %v want %v", src, got, want)
		}
		tree2, _ := parser.Parse(src)
		if got := run(printer.Minify(tree2)); got != want {
			t.Errorf("Minify round-trip of %q: got %v want %v", src, got, want)
		}
	}
}

// TestMinifyNeverEmitsMathAlias pins the one namespace Minify must NOT
// shorten.
//
// CONFIRMED (1.26.50.24): `m.` is not a Molang alias. The engine's Molang
// token reader carries a hand-written chain of exactly four two-character
// prefixes -- "v.", "q.", "c.", "t." -- with no "m." anywhere in it and no
// "m." token in the engine's operator/token metadata table. Minify used to
// emit `m.floor(1.5)`, i.e. source Bedrock refuses to load with
// "Error: unknown token: %s". The lexer used to ACCEPT `m.` as well; it no
// longer does, so the two halves agree.
//
// The other four aliases stay: they are real, and shortening them is the
// point of Minify.
func TestMinifyNeverEmitsMathAlias(t *testing.T) {
	tree, err := parser.Parse("math.floor(1.5) + query.foo + variable.bar + temp.baz + context.qux")
	if err != nil {
		t.Fatal(err)
	}
	got := printer.Minify(tree)
	want := "math.floor(1.5)+q.foo+v.bar+t.baz+c.qux"
	if got != want {
		t.Errorf("Minify = %q, want %q", got, want)
	}
	if strings.Contains(got, "m.") && !strings.Contains(got, "math.") {
		t.Errorf("Minify emitted an m. alias: %q", got)
	}

	// Every math.* spelling minifies long -- there is no shorter legal one.
	tree2, err := parser.Parse("math.sin(30)")
	if err != nil {
		t.Fatal(err)
	}
	if got := printer.Minify(tree2); got != "math.sin(30)" {
		t.Errorf("Minify(%q) = %q, want %q", "math.sin(30)", got, "math.sin(30)")
	}

	// The input side now agrees with the output side: `m.` is refused, so this
	// package can no longer read back a spelling it would never write.
	if _, err := parser.Parse("m.sin(30)"); err == nil {
		t.Error("parser accepted m.sin(30); `m.` is not an engine alias")
	}
}

// Printing must not touch the bytes of a string literal. The lexer scans
// bytes rather than runes, so a literal's body reaches the AST as a slice of
// the source; anything that re-encoded it on the way out would corrupt text
// outside ASCII.
func TestPrintingPreservesNonASCIIStringLiterals(t *testing.T) {
	for _, src := range []string{
		"temp.s = 'zażółć gęślą jaźń'; return temp.s == 'zażółć gęślą jaźń';",
		"temp.s = '日本語'; return temp.s == '日本語';",
		"temp.s = '🙂🚀'; return temp.s == '🙂🚀';",
		"'ą' == 'a'",
	} {
		tree, err := parser.Parse(src)
		if err != nil {
			t.Fatalf("parse(%q): %v", src, err)
		}
		for _, p := range []struct {
			name string
			out  string
		}{
			{"Format", printer.Format(tree)},
			{"Minify", printer.Minify(tree)},
		} {
			// Every literal body in the source must appear verbatim in the
			// output, and the output must re-parse to the same value.
			for _, body := range literalBodies(src) {
				if !strings.Contains(p.out, body) {
					t.Errorf("%s(%q) lost literal %q: %s", p.name, src, body, p.out)
				}
			}
			if got, want := run(t, p.out), run(t, src); got != want {
				t.Errorf("%s(%q) changed the value: got %v want %v", p.name, src, got, want)
			}
		}
	}
}

// literalBodies pulls the text between each pair of single quotes.
func literalBodies(src string) []string {
	var out []string
	for {
		i := strings.IndexByte(src, '\'')
		if i < 0 {
			return out
		}
		rest := src[i+1:]
		j := strings.IndexByte(rest, '\'')
		if j < 0 {
			return out
		}
		out = append(out, rest[:j])
		src = rest[j+1:]
	}
}
