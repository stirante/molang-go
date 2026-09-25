package bridge

import (
	"encoding/json"
	goast "go/ast"
	goparser "go/parser"
	gotoken "go/token"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"
	"unicode/utf16"

	molang "github.com/stirante/molang-go"
	"github.com/stirante/molang-go/eval"
	"github.com/stirante/molang-go/lexer"
	"github.com/stirante/molang-go/parser"
	"github.com/stirante/molang-go/token"
)

// u16 is the UTF-16 length of s, which is what an editor counts.
func u16(s string) int { return len(utf16.Encode([]rune(s))) }

const testCatalogue = `{
  "gameVersion": "test",
  "queries": [
    {"name": "is_baby", "args": []},
    {"name": "position", "args": [{"name": "axis", "type": "number", "optional": false}]},
    {"name": "get_name", "args": [{"name": "a", "optional": true}, {"name": "b", "optional": true}]},
    {"name": "any_of", "args": [{"name": "x"}], "variadic": true},
    {"name": "old_thing", "args": [], "deprecated": {"replacement": "query.new_thing", "note": ""}},
    {"name": "client_only", "args": [], "contexts": ["client_entity"]}
  ],
  "contexts": {"client_entity": {"name": "client entity"}, "block_description": "block description"}
}`

func analyzer(t *testing.T) *Analyzer {
	t.Helper()
	a := &Analyzer{}
	if s := a.SetCatalogue(testCatalogue); !s.OK {
		t.Fatal(s.Error)
	}
	return a
}

func codes(r Result) []string {
	var out []string
	for _, d := range r.Diagnostics {
		out = append(out, d.Code)
	}
	return out
}

// Offsets are UTF-16 code units, and the byte offsets alongside them are
// UTF-8. The two part company at the first character outside ASCII, which in
// Molang can only be inside a string literal -- so every error after one is
// where an off-by-bytes bug would show.
func TestDiagnosticOffsetsAreUTF16(t *testing.T) {
	a := &Analyzer{}
	for _, prefix := range []string{
		"t.s = 'zażółć gęślą jaźń'; ",
		"t.s = '😀'; ",   // outside the BMP: 4 bytes, 2 units
		"t.s = '日本語'; ", // 3 bytes, 1 unit each
		"t.s = 'e\u0301'; ",
	} {
		src := prefix + "v.a = ;"
		r := a.Analyze(src, Options{})
		if len(r.Diagnostics) != 1 {
			t.Fatalf("%q: got %v", src, r.Diagnostics)
		}
		d := r.Diagnostics[0]
		semi := strings.LastIndex(src, ";")
		if d.ByteStart != semi || d.ByteEnd != semi+1 {
			t.Errorf("%q: byte range %d-%d, want %d-%d", src, d.ByteStart, d.ByteEnd, semi, semi+1)
		}
		want := u16(src[:semi])
		if d.Start != want || d.End != want+1 {
			t.Errorf("%q: UTF-16 range %d-%d, want %d-%d", src, d.Start, d.End, want, want+1)
		}
	}
}

// A character the language has no use for is reported once, where it is,
// and measured as the one character it is.
func TestIllegalCharacterRange(t *testing.T) {
	src := "'ą' == 'b' ⊕ 1"
	r := (&Analyzer{}).Analyze(src, Options{})
	if len(r.Diagnostics) != 1 {
		t.Fatalf("got %v", r.Diagnostics)
	}
	d := r.Diagnostics[0]
	at := strings.Index(src, "⊕")
	if d.ByteStart != at || d.ByteEnd != at+len("⊕") {
		t.Errorf("byte range %d-%d, want %d-%d", d.ByteStart, d.ByteEnd, at, at+len("⊕"))
	}
	if d.Start != u16(src[:at]) || d.End != u16(src[:at])+1 {
		t.Errorf("UTF-16 range %d-%d, want %d-%d", d.Start, d.End, u16(src[:at]), u16(src[:at])+1)
	}
}

func TestSeveralSyntaxErrors(t *testing.T) {
	r := (&Analyzer{}).Analyze("v.a = ; v.b = 1; v.c = );", Options{})
	if got := codes(r); strings.Join(got, ",") != "syntax,syntax" {
		t.Fatalf("codes %v", got)
	}
	if r.OK {
		t.Error("OK with errors")
	}
}

// A missing final `;` is reported where it belongs: straight after the last
// token, not after the whitespace that trails it.
func TestMissingSemicolonRange(t *testing.T) {
	src := "v.a = 1   \n"
	r := (&Analyzer{}).Analyze(src, Options{})
	if len(r.Diagnostics) != 1 {
		t.Fatalf("got %v", r.Diagnostics)
	}
	if d := r.Diagnostics[0]; d.Start != 7 || d.End != 7 {
		t.Errorf("range %d-%d, want 7-7", d.Start, d.End)
	}
	// OptionalSemicolons lifts it.
	if r := (&Analyzer{}).Analyze(src, Options{OptionalSemicolons: true}); len(r.Diagnostics) != 0 {
		t.Errorf("with OptionalSemicolons: %v", r.Diagnostics)
	}
}

func TestEmptySourceSaysNothing(t *testing.T) {
	for _, src := range []string{"", "  \n\t"} {
		if r := (&Analyzer{}).Analyze(src, Options{}); len(r.Diagnostics) != 0 || !r.OK {
			t.Errorf("%q: %v", src, r.Diagnostics)
		}
	}
}

func TestMathChecks(t *testing.T) {
	a := &Analyzer{}
	cases := []struct {
		src, code, msg string
		at, end        string // the text the range covers
	}{
		{"1 + math.nope(1)", "unknown-math", "unknown math function 'math.nope'", "math.nope", ""},
		{"math.sin", "math-not-called", "math.sin must be called, e.g. math.sin(...)", "math.sin", ""},
		{"1 + math.clamp(1, 2)", "math-arity", "Unexpected number of parameters to Clamp 'math.clamp' function - expected 3, found 2.", "math.clamp(1, 2)", ""},
		{"math.abs(1, 2)", "math-arity", "Malformed Absolute Value 'math.abs' expression. It has 2 children but should have between 1 and 1", "math.abs(1, 2)", ""},
		{"math.pi(1)", "math-arity", "math.pi takes no arguments", "math.pi(1)", ""},
	}
	for _, c := range cases {
		r := a.Analyze(c.src, Options{})
		if len(r.Diagnostics) != 1 {
			t.Errorf("%q: %v", c.src, r.Diagnostics)
			continue
		}
		d := r.Diagnostics[0]
		if d.Code != c.code || d.Message != c.msg {
			t.Errorf("%q: %s %q", c.src, d.Code, d.Message)
		}
		if got := c.src[d.ByteStart:d.ByteEnd]; got != c.at {
			t.Errorf("%q: range covers %q, want %q", c.src, got, c.at)
		}
		// Compile says the same thing without a position; the bridge's own
		// check must agree with it word for word.
		prog, _ := molang.Parse(c.src)
		if _, err := eval.Compile(prog); err == nil || err.Error() != c.msg {
			t.Errorf("%q: Compile says %v, bridge says %q", c.src, err, c.msg)
		}
	}
	if r := a.Analyze("math.pi + math.clamp(math.sin(1), 0, 1)", Options{}); len(r.Diagnostics) != 0 {
		t.Errorf("valid math: %v", r.Diagnostics)
	}
}

func TestQueryChecks(t *testing.T) {
	a := analyzer(t)
	cases := []struct {
		src      string
		opts     Options
		want     string // code, or "" for nothing
		severity string
		msg      string
	}{
		{"q.is_baby", Options{}, "", "", ""},
		{"Query.IS_BABY", Options{}, "", "", ""},
		{"q.nope", Options{}, "unknown-query", "warning", "query.nope is not a known query function"},
		{"q.nope", Options{UnknownQueries: "error"}, "unknown-query", "error", ""},
		{"q.nope", Options{UnknownQueries: "off"}, "", "", ""},
		{"q.position", Options{}, "query-arity", "warning", "query.position takes 1 argument, found 0"},
		{"q.position(0)", Options{}, "", "", ""},
		{"q.position(0, 1)", Options{}, "query-arity", "warning", "query.position takes 1 argument, found 2"},
		{"q.get_name('a', 1, 2)", Options{}, "query-arity", "warning", "query.get_name takes 0 to 2 arguments, found 3"},
		{"q.get_name", Options{}, "", "", ""},
		{"q.any_of(1, 2, 3, 4)", Options{}, "", "", ""},
		{"q.any_of()", Options{}, "query-arity", "warning", "query.any_of takes at least 1 argument, found 0"},
		{"q.old_thing", Options{}, "query-deprecated", "hint", "query.old_thing is deprecated; use query.new_thing instead"},
		{"q.client_only", Options{}, "", "", ""},
		{"q.client_only", Options{Context: "client_entity"}, "", "", ""},
		{"q.client_only", Options{Context: "block_description"}, "query-context", "warning", "query.client_only is not available in block description"},
		{"v.e->q.nope", Options{}, "unknown-query", "warning", ""},
	}
	for _, c := range cases {
		r := a.Analyze(c.src, c.opts)
		if c.want == "" {
			if len(r.Diagnostics) != 0 {
				t.Errorf("%q: %v", c.src, r.Diagnostics)
			}
			continue
		}
		if len(r.Diagnostics) != 1 {
			t.Errorf("%q: %v", c.src, r.Diagnostics)
			continue
		}
		d := r.Diagnostics[0]
		if d.Code != c.want || d.Severity != c.severity || (c.msg != "" && d.Message != c.msg) {
			t.Errorf("%q: %s/%s %q", c.src, d.Code, d.Severity, d.Message)
		}
		if !r.OK && d.Severity != "error" {
			t.Errorf("%q: a %s made the result not OK", c.src, d.Severity)
		}
	}

	// A partial catalogue does not know enough to call a name unknown.
	p := &Analyzer{}
	p.SetCatalogue(`{"partial": true, "queries": [{"name": "is_baby"}]}`)
	if r := p.Analyze("q.whatever", Options{}); len(r.Diagnostics) != 1 || r.Diagnostics[0].Severity != "hint" {
		t.Errorf("partial catalogue: %v", r.Diagnostics)
	}
	// And with no catalogue there is nothing to say about queries at all.
	if r := (&Analyzer{}).Analyze("q.whatever(1, 2)", Options{}); len(r.Diagnostics) != 0 {
		t.Errorf("no catalogue: %v", r.Diagnostics)
	}
}

// A syntax error elsewhere makes argument counts unreliable, so they are
// not reported; the name checks still are.
func TestArityNotCheckedWhenSyntaxFails(t *testing.T) {
	r := analyzer(t).Analyze("q.position(1, 2); v.a = ; q.nope;", Options{})
	got := strings.Join(codes(r), ",")
	if got != "syntax,unknown-query" {
		t.Errorf("codes %s", got)
	}
}

func TestRestrictions(t *testing.T) {
	a := analyzer(t)
	src := "v.a = math.random(0, 1); t.b = 2; return v.a;"
	r := a.Analyze(src, Options{Restrict: "no_side_effects"})
	var at []string
	for _, d := range r.Diagnostics {
		if d.Code != "op-not-allowed" {
			t.Errorf("unexpected %s %q", d.Code, d.Message)
		}
		at = append(at, src[d.ByteStart:d.ByteEnd])
	}
	if strings.Join(at, "|") != "=|=" {
		t.Errorf("no_side_effects flagged %q", at)
	}
	if msg := r.Diagnostics[0].Message; msg != "Expression uses operation Assignment '=' which is not allowed in this context" {
		t.Errorf("message %q", msg)
	}

	r = a.Analyze("math.random(0, 1) + math.die_roll(1, 0, 1)", Options{Restrict: "no_side_effects_or_random"})
	if len(r.Diagnostics) != 1 || r.Diagnostics[0].ByteStart != 0 || r.Diagnostics[0].ByteEnd != len("math.random(0, 1)") {
		t.Errorf("no_side_effects_or_random: %v", r.Diagnostics)
	}

	// A catalogue context can refuse operations too, spelled any of three ways.
	c := &Analyzer{}
	c.SetCatalogue(`{"contexts": [{"id": "pure", "disallowedOps": ["Assignment '='", "??", "loop"]}]}`)
	r = c.Analyze("v.a = v.b ?? 1; loop(2, {t.x = 1;});", Options{Context: "pure"})
	if len(r.Diagnostics) != 4 { // two '=', one '??', one loop
		t.Errorf("catalogue context: %v", r.Diagnostics)
	}
	if r := c.Analyze("v.a", Options{Restrict: "bogus"}); len(r.Diagnostics) != 1 || r.Diagnostics[0].Severity != "warning" {
		t.Errorf("unknown restriction: %v", r.Diagnostics)
	}
}

func TestTokensAndRefs(t *testing.T) {
	a := analyzer(t)
	src := "v.x = q.old_thing + math.sin(c.y) * 'é'; return v.x;"
	r := a.Analyze(src, Options{})
	type tk struct{ text, typ, mods string }
	var got []tk
	for _, x := range r.Tokens {
		got = append(got, tk{string([]rune(src)[x.Start:x.End]), x.Type, strings.Join(x.Mods, "+")})
	}
	want := []tk{
		{"v", "namespace", ""}, {"x", "variable", "modification"}, {"=", "operator", ""},
		{"q", "namespace", ""}, {"old_thing", "function", "deprecated"}, {"+", "operator", ""},
		{"math", "namespace", ""}, {"sin", "function", "defaultLibrary"},
		{"c", "namespace", ""}, {"y", "variable", "readonly"}, {"*", "operator", ""},
		{"'é'", "string", ""}, {"return", "keyword", ""}, {"v", "namespace", ""}, {"x", "variable", ""},
	}
	if len(got) != len(want) {
		t.Fatalf("tokens:\n got  %v\n want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("token %d: got %v want %v", i, got[i], want[i])
		}
	}
	var refs []string
	for _, x := range r.Refs {
		refs = append(refs, x.Namespace+":"+x.Name+":"+strconv.Itoa(x.Args))
	}
	if s := strings.Join(refs, " "); s != "variable:x:-1 query:old_thing:-1 math:sin:1 context:y:-1 variable:x:-1" {
		t.Errorf("refs %s", s)
	}
	if !r.Refs[0].Write || r.Refs[4].Write {
		t.Error("write flags wrong")
	}
	var syms []string
	for _, s := range r.Symbols {
		syms = append(syms, s.Namespace+"."+s.Name+":"+strconv.Itoa(len(s.Reads))+"r"+strconv.Itoa(len(s.Writes))+"w")
	}
	if s := strings.Join(syms, " "); s != "variable.x:1r1w context.y:1r0w" {
		t.Errorf("symbols %s", s)
	}
}

// harvest returns every string literal in the module's test files, as the
// parser's ParseAll test does: the cases the suite was written around.
func harvest(t *testing.T) []string {
	t.Helper()
	var files []string
	for _, p := range []string{"../../../*_test.go", "../../../*/*_test.go"} {
		m, _ := filepath.Glob(p)
		files = append(files, m...)
	}
	fset := gotoken.NewFileSet()
	seen := map[string]bool{}
	var out []string
	for _, f := range files {
		file, err := goparser.ParseFile(fset, f, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		goast.Inspect(file, func(n goast.Node) bool {
			if lit, ok := n.(*goast.BasicLit); ok && lit.Kind == gotoken.STRING {
				if s, err := strconv.Unquote(lit.Value); err == nil && !seen[s] {
					seen[s] = true
					out = append(out, s)
				}
			}
			return true
		})
	}
	if len(out) < 500 {
		t.Fatalf("harvested %d sources", len(out))
	}
	return out
}

// The first thing a Molang editor must not do is complain about Molang the
// game loads. Every source in the test suite that parses and compiles must
// analyse with no error, and every source that does not must analyse with
// one -- the bridge adds positions, never opinions.
func TestAnalyzeAgreesWithParseAndCompile(t *testing.T) {
	a := &Analyzer{}
	n := 0
	for _, src := range harvest(t) {
		if strings.TrimSpace(src) == "" {
			continue
		}
		prog, err := parser.Parse(src)
		valid := err == nil
		if valid {
			_, cerr := eval.Compile(prog)
			valid = cerr == nil
		}
		r := a.Analyze(src, Options{})
		if r.OK != valid {
			t.Errorf("%q: Parse+Compile valid=%v, Analyze OK=%v %v", src, valid, r.OK, r.Diagnostics)
		}
		if valid {
			n++
			if len(r.Diagnostics) != 0 {
				t.Errorf("%q: valid, yet %v", src, r.Diagnostics)
			}
		}
	}
	t.Logf("%d valid sources analysed clean", n)
}

// The symbol list comes from molang.References when there is a tree and
// from the token scan when there is not. The two must name the same things,
// or the outline would change shape as a typo came and went.
func TestScanAgreesWithReferences(t *testing.T) {
	for _, src := range harvest(t) {
		prog, err := parser.Parse(src)
		if err != nil {
			continue
		}
		toks, _ := lexerTokens(src)
		refs := scanRefs(src, toks)
		idx := newUTF16Index(src)
		fromTree := symbols(prog, refs, idx)
		fromScan := symbols(nil, refs, idx)
		if a, b := symbolKeys(fromTree), symbolKeys(fromScan); a != b {
			t.Errorf("%q:\n References %s\n scan       %s", src, a, b)
		}
		for _, s := range fromTree {
			if len(s.Reads)+len(s.Writes) == 0 {
				t.Errorf("%q: %s.%s has no position", src, s.Namespace, s.Name)
			}
		}
	}
}

func symbolKeys(ss []Symbol) string {
	var out []string
	for _, s := range ss {
		w := ""
		if len(s.Writes) > 0 {
			w = "w"
		}
		out = append(out, s.Namespace+"."+s.Name+w)
	}
	sort.Strings(out)
	return strings.Join(out, " ")
}

func TestFormatAndMinify(t *testing.T) {
	if r := Format("v.a=q.b?1:2;", Options{}); !r.OK || r.Text != "variable.a = query.b ? 1 : 2;" {
		t.Errorf("Format: %+v", r)
	}
	if r := Minify("variable.a = query.b ? 1 : 2;", Options{}); !r.OK || r.Text != "v.a=q.b?1:2;" {
		t.Errorf("Minify: %+v", r)
	}
	if r := Format("v.a = ", Options{}); r.OK || r.Error == "" {
		t.Errorf("Format of a broken source: %+v", r)
	}
}

func TestCall(t *testing.T) {
	a := &Analyzer{}
	var s CatalogueSummary
	json.Unmarshal([]byte(a.Call("setCatalogue", `["{\"queries\":[{\"name\":\"is_baby\"}]}"]`)), &s)
	if !s.OK || s.Queries != 1 {
		t.Errorf("setCatalogue: %+v", s)
	}
	var r Result
	if err := json.Unmarshal([]byte(a.Call("analyze", `["q.is_baby + q.nope", {"unknownQueries": "error"}]`)), &r); err != nil {
		t.Fatal(err)
	}
	if len(r.Diagnostics) != 1 || r.Diagnostics[0].Severity != "error" {
		t.Errorf("analyze: %+v", r)
	}
	// Options may be omitted or null.
	for _, args := range []string{`["1"]`, `["1", null]`, `["1", ""]`, `["1", "{\"restrict\":\"no_side_effects\"}"]`} {
		if out := a.Call("analyze", args); !strings.Contains(out, `"ok":true`) {
			t.Errorf("%s: %s", args, out)
		}
	}
	var p PrintResult
	json.Unmarshal([]byte(a.Call("minify", `["temp.x = 1;"]`)), &p)
	if p.Text != "t.x=1;" {
		t.Errorf("minify: %+v", p)
	}
	for _, c := range [][2]string{{"nope", `["x"]`}, {"analyze", `not json`}, {"analyze", `[]`}, {"analyze", `["x", 5]`}} {
		if out := a.Call(c[0], c[1]); !strings.Contains(out, `"error"`) {
			t.Errorf("%s %s: %s", c[0], c[1], out)
		}
	}
	// A failed catalogue load keeps the one already loaded.
	json.Unmarshal([]byte(a.Call("setCatalogue", `["{"]`)), &s)
	if s.OK || a.Catalogue == nil || len(a.Catalogue.Queries) != 1 {
		t.Errorf("bad catalogue replaced the good one")
	}
}

// The catalogue the extension ships must load, and must list exactly the
// math functions the evaluator has: completion offering a function the
// diagnostics then refuse, or missing one they accept, is a bug either way.
func TestShippedCatalogue(t *testing.T) {
	data, err := os.ReadFile("../../../apps/vscode/catalogue/catalogue.json")
	if err != nil {
		t.Fatal(err)
	}
	c, err := ParseCatalogue(string(data))
	if err != nil {
		t.Fatal(err)
	}
	listed := map[string]bool{}
	for _, f := range c.Math {
		listed[f.Name] = true
		if f.Name == "pi" {
			continue
		}
		arity, ok := eval.MathArity[f.Name]
		if !ok {
			t.Errorf("catalogue lists math.%s, which the evaluator does not have", f.Name)
			continue
		}
		if min, max := f.ArgRange(); min != arity || max != arity {
			t.Errorf("math.%s: catalogue says %d-%d arguments, evaluator %d", f.Name, min, max, arity)
		}
	}
	for name := range eval.MathArity {
		if !listed[name] {
			t.Errorf("catalogue does not list math.%s", name)
		}
	}
	if !listed["pi"] {
		t.Error("catalogue does not list math.pi")
	}
	for _, q := range c.Queries {
		if q.Description == "" {
			t.Errorf("query.%s has no description", q.Name)
		}
	}
}

func lexerTokens(src string) ([]token.Token, []*lexer.Error) {
	return lexer.TokenizeAll(src, lexer.Extensions{})
}
