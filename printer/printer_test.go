package printer_test

import (
	"reflect"
	"strings"
	"testing"

	"github.com/stirante/molang-go/ast"
	"github.com/stirante/molang-go/eval"
	"github.com/stirante/molang-go/parser"
	"github.com/stirante/molang-go/printer"
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
	"t.n = 0.0/0.0; return t.n ?? 5;",
	// `??`: looser than the conditional, tighter than `=`, left side a
	// bare read, chains parenthesized on the right.
	"v.a ?? 1 ? 2 : 3",
	"(v.a ?? 1) ? 2 : 3",
	"v.a ? (v.b ?? 1) : 2",
	"v.a ? 1 : (v.b ?? 2)",
	"v.a ?? (v.b ?? 3)",
	"v.a ?? 1 + 2 * 3",
	"v.a ?? 0 > 1 && 1",
	"v.x = v.y ?? 7; return v.x;",
	"math.max(v.a ?? 1, 2)",
	"1 ? (v.a ?? 2) : 3",
	"-(v.a ?? 1)",
	"(v.a ?? 1) * 2",
	"((v.a)) ?? 4",
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
	"c.other->v.hp + 1",
	"-v.e->q.health(1, t.x) * 2",
	"(v.a + 1)->v.hp",
	"-(-5)",
	"!(!0)",
	"1 - -1",
	"t.x = 1; t.x ? { t.y = 2; } : 3; return t.y;",
	"t.x = 0; t.x ? 3 : { t.y = 2; }; return t.y;",
	"t.r = 0 ? { t.y = 2; } : 3; return t.r;",
	"1 ? { return 4; } : 0 ? 1 : { return 5; };",
	"0 ? { return 4; } : 0 ? 1 : { return 5; };",
	"{ t.a = 1; }; return t.a;",
	"return 1 ? (0 ? 5) : 2;",
	"return 1 ? (0 ? { return 5; }) : 2;",
	"t.i = 0; loop(9, { t.i = t.i + 1; t.i == 4 ? { break; } : 0; }); return t.i;",
	"for_each(t.x, q.entities, { t.y = 1; }); return t.y ?? 3;",
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
		{"{return 7;};", "{ return 7; };", "{return 7;};"},
		// Multi-statement programs end with ';' too: the game refuses an
		// expression containing ';' that does not end with one.
		{"temp.a=1;temp.b=2;", "temp.a = 1; temp.b = 2;", "t.a=1;t.b=2;"},
		// Below, sources only the OptionalSemicolons extension parses. The
		// output must still load, and must still mean what the tree means.
		// A bare grouping block yields 0 unless it returns either way, so
		// it simply gains the ';'.
		{"{return 7;}", "{ return 7; };", "{return 7;};"},
		// A bare assignment's value is the assigned value; `t.a=5;` would
		// be 0, so the value is kept through return.
		{"temp.a = 5", "return temp.a = 5;", "return t.a=5;"},
	}
	lenient := parser.Extensions{OptionalSemicolons: true}
	for _, c := range cases {
		tree, err := parser.ParseWith(c.src, lenient)
		if err != nil {
			t.Fatalf("parse(%q): %v", c.src, err)
		}
		if got := printer.Format(tree); got != c.wantFormat {
			t.Errorf("Format(%q) = %q, want %q", c.src, got, c.wantFormat)
		}
		tree2, _ := parser.ParseWith(c.src, lenient)
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
	// The originals are parsed leniently, since several deliberately lack
	// the trailing ';'. The printed forms are parsed strictly: whatever the
	// input, the output must be something the game loads.
	run := func(src string, ext parser.Extensions) float64 {
		t.Helper()
		tree, err := parser.ParseWith(src, ext)
		if err != nil {
			t.Fatalf("parse(%q): %v", src, err)
		}
		prog, err := eval.Compile(tree)
		if err != nil {
			t.Fatalf("compile(%q): %v", src, err)
		}
		return prog.Run(&eval.Context{RNG: &stubRNG{}, Scope: eval.NewScope()})
	}
	lenient := parser.Extensions{OptionalSemicolons: true}
	strict := parser.Extensions{}
	for _, src := range []string{
		"1+1", "1+1;", "temp.a=5", "temp.a=5;", "return 3;",
		"{return 7;}", "{temp.a=5;}", "temp.a=1;temp.b=2;",
		"loop(2,{temp.a=temp.a+1;});return temp.a;",
		"temp.a=1;temp.b=2", "{temp.a=5}", "1 ? {return 4;} : 2",
	} {
		want := run(src, lenient)
		tree, _ := parser.ParseWith(src, lenient)
		if got := run(printer.Format(tree), strict); got != want {
			t.Errorf("Format round-trip of %q: got %v want %v", src, got, want)
		}
		tree2, _ := parser.ParseWith(src, lenient)
		if got := run(printer.Minify(tree2), strict); got != want {
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

// satisfiesSemicolonRules checks printed output against the game's two rules
// directly on the text, independently of the parser: a brace section contains
// a `;`, and output containing `=` or `;` ends with one. String literals are
// skipped, and `==`, `!=`, `<=`, `>=` are not assignments.
func satisfiesSemicolonRules(src string) bool {
	var open []bool
	complex := false
	last := byte(0)
	for i := 0; i < len(src); i++ {
		c := src[i]
		switch c {
		case ' ':
			continue
		case '\'':
			j := strings.IndexByte(src[i+1:], '\'')
			if j < 0 {
				return false
			}
			i += j + 1
		case '=':
			if i+1 < len(src) && src[i+1] == '=' {
				i++
			} else if i > 0 && strings.IndexByte("=!<>", src[i-1]) >= 0 {
			} else {
				complex = true
			}
		case ';':
			complex = true
			for k := range open {
				open[k] = true
			}
		case '{':
			open = append(open, false)
		case '}':
			if len(open) == 0 || !open[len(open)-1] {
				return false
			}
			open = open[:len(open)-1]
		}
		last = c
	}
	return !complex || last == ';'
}

// Format and Minify write only what the game loads, and what they write reads
// back as the same program.
func TestPrintedOutputSatisfiesSemicolonRules(t *testing.T) {
	extra := []string{
		"t.a=1;",
		"{t.a=1;};",
		"t.x ? {t.a=1;};",
		"t.x ? {t.a=1; t.b=2;} : {t.a=3;};",
		"loop(2, {t.a=1;});",
		"for_each(t.x, array.a, {t.a=1;});",
		"math.sin(q.anim_time)",
		"v.a == 1 ? 2 : 3",
	}
	for _, src := range append(append([]string{}, samples...), extra...) {
		tree, err := parser.Parse(src)
		if err != nil {
			t.Fatalf("parse(%q): %v", src, err)
		}
		for name, out := range map[string]string{
			"Format": printer.Format(tree),
			"Minify": printer.Minify(tree),
		} {
			if !satisfiesSemicolonRules(out) {
				t.Errorf("%s(%q) = %q breaks the game's semicolon rules", name, src, out)
			}
			again, err := parser.Parse(out)
			if err != nil {
				t.Errorf("%s(%q) = %q does not reparse: %v", name, src, out, err)
				continue
			}
			// Format keeps every spelling, so the reparsed tree is the
			// original tree. Minify respells true as 1, so its check is that
			// minifying again changes nothing.
			if name == "Format" && !reflect.DeepEqual(again, tree) {
				t.Errorf("Format(%q) = %q reparses to a different tree", src, out)
			}
			if name == "Minify" && printer.Minify(again) != out {
				t.Errorf("Minify(%q) = %q is not stable: %q", src, out, printer.Minify(again))
			}
		}
	}
}

func TestBlockArmsPrint(t *testing.T) {
	cases := []struct{ src, format, minify string }{
		{"q.x ? {v.a=1;} : 0;", "query.x ? { variable.a = 1; } : 0;", "q.x?{v.a=1;}:0;"},
		{"q.x ? 0 : {v.a=1;};", "query.x ? 0 : { variable.a = 1; };", "q.x?0:{v.a=1;};"},
		{"q.x ? {v.a=1;};", "query.x ? { variable.a = 1; };", "q.x?{v.a=1;};"},
		{"q.x ? {v.a=1;} : {v.a=2;};", "query.x ? { variable.a = 1; } : { variable.a = 2; };", "q.x?{v.a=1;}:{v.a=2;};"},
		{"{v.a=1; v.b=2;};", "{ variable.a = 1; variable.b = 2; };", "{v.a=1;v.b=2;};"},
		{"v.a=1; v.b=2;", "variable.a = 1; variable.b = 2;", "v.a=1;v.b=2;"},
		// A conditional with no else, as the then-arm of one with an else,
		// keeps its parentheses; without them it would take the else.
		{"q.x ? (q.y ? {v.a=1;}) : 2;", "query.x ? (query.y ? { variable.a = 1; }) : 2;", "q.x?(q.y?{v.a=1;}):2;"},
		{"q.x ? (q.y ? 1) : 2", "query.x ? (query.y ? 1) : 2", "q.x?(q.y?1):2"},
	}
	for _, c := range cases {
		tree, err := parser.Parse(c.src)
		if err != nil {
			t.Fatalf("parse(%q): %v", c.src, err)
		}
		if got := printer.Format(tree); got != c.format {
			t.Errorf("Format(%q) = %q, want %q", c.src, got, c.format)
		}
		if got := printer.Minify(tree); got != c.minify {
			t.Errorf("Minify(%q) = %q, want %q", c.src, got, c.minify)
		}
	}
}

// TestGroupingPrints pins where the printers put parentheses now that the
// tree is grouped as the game groups it: `/` binds tighter than `*`, so a
// quotient on the right of a `*` needs none and a product on the left of a
// `/` does; and an operand that starts with a `-` keeps its parentheses on
// the right of a `-` and under another `-`, because two `-` in a row are a
// `+` to the game.
func TestGroupingPrints(t *testing.T) {
	cases := []struct{ src, format, minify string }{
		{"7 * 3 / 9", "7 * 3 / 9", "7*3/9"},
		{"(7 * 3) / 9", "(7 * 3) / 9", "(7*3)/9"},
		{"7 / (9 * 3)", "7 / (9 * 3)", "7/(9*3)"},
		{"7 / 9 / 3", "7 / 9 / 3", "7/9/3"},
		{"7 / (9 / 3)", "7 / (9 / 3)", "7/(9/3)"},
		{"2 * 3 / 4 * 5", "2 * 3 / 4 * 5", "2*3/4*5"},
		{"2 - -3", "2 + 3", "2+3"},
		{"2 - (-3)", "2 - (-3)", "2-(-3)"},
		{"-(-3)", "-(-3)", "-(-3)"},
		// The parentheses go around the whole operand: what matters is
		// that its text does not start with `-` right after the `-`.
		{"2 - (-3) * 4", "2 - (-3 * 4)", "2-(-3*4)"},
		{"2 - (-3 * 4)", "2 - (-3 * 4)", "2-(-3*4)"},
		{"-(-3 * 4)", "-(-3 * 4)", "-(-3*4)"},
		{"2 - 3 * -4", "2 - 3 * -4", "2-3*-4"},
		{"2 * -3", "2 * -3", "2*-3"},
		{"v.b = (v.a = 1);", "variable.b = (variable.a = 1);", "v.b=(v.a=1);"},
	}
	for _, c := range cases {
		tree, err := parser.Parse(c.src)
		if err != nil {
			t.Fatalf("parse(%q): %v", c.src, err)
		}
		if got := printer.Format(tree); got != c.format {
			t.Errorf("Format(%q) = %q, want %q", c.src, got, c.format)
		}
		if got := printer.Minify(tree); got != c.minify {
			t.Errorf("Minify(%q) = %q, want %q", c.src, got, c.minify)
		}
	}
}

// TestNegativeLiteralsPrint: a tree built by hand or by constant folding can
// hold a negative literal, which is written with a `-` in front. It is
// placed as a negation would be, so it never runs into a `-` before it.
func TestNegativeLiteralsPrint(t *testing.T) {
	num := func(v float64) ast.Expr { return &ast.NumberLit{Value: v} }
	one := func(e ast.Expr) *ast.Program { return &ast.Program{Stmts: []ast.Stmt{&ast.ExprStmt{X: e}}} }
	cases := []struct {
		tree *ast.Program
		want string
	}{
		{one(&ast.BinaryExpr{Op: ast.Sub, X: num(1), Y: num(-2)}), "1 - (-2)"},
		{one(&ast.BinaryExpr{Op: ast.Sub, X: num(1), Y: &ast.UnaryExpr{Op: ast.Neg, X: num(2)}}), "1 - (-2)"},
		{one(&ast.UnaryExpr{Op: ast.Neg, X: num(-2)}), "-(-2)"},
		{one(&ast.UnaryExpr{Op: ast.Neg, X: &ast.UnaryExpr{Op: ast.Neg, X: num(2)}}), "-(-2)"},
		{one(&ast.BinaryExpr{Op: ast.Add, X: num(1), Y: num(-2)}), "1 + -2"},
		{one(&ast.BinaryExpr{Op: ast.Mul, X: num(-2), Y: num(3)}), "-2 * 3"},
		{one(&ast.ArrowExpr{Entity: num(-2), Read: &ast.Ident{Namespace: ast.Variable, Member: "x"}}), "(-2)->variable.x"},
		{one(&ast.BinaryExpr{Op: ast.Div, X: &ast.BinaryExpr{Op: ast.Mul, X: num(1), Y: num(2)}, Y: num(3)}), "(1 * 2) / 3"},
		{one(&ast.BinaryExpr{Op: ast.Mul, X: num(1), Y: &ast.BinaryExpr{Op: ast.Div, X: num(2), Y: num(3)}}), "1 * 2 / 3"},
	}
	for _, c := range cases {
		if got := printer.Format(c.tree); got != c.want {
			t.Errorf("Format = %q, want %q", got, c.want)
		}
		reparsed, err := parser.Parse(printer.Minify(c.tree))
		if err != nil {
			t.Errorf("Minify = %q does not parse: %v", printer.Minify(c.tree), err)
			continue
		}
		// Reparsed, a negative literal is a negation of the positive one;
		// the value is the same, and so is everything around it.
		if got, want := printer.Format(reparsed), c.want; got != want {
			t.Errorf("Minify round-trip: %q, want %q", got, want)
		}
	}
}
