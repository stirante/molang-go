package parser_test

import (
	"go/ast"
	goparser "go/parser"
	"go/token"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"github.com/stirante/molang-go/lexer"
	"github.com/stirante/molang-go/parser"
	mtoken "github.com/stirante/molang-go/token"
)

// testSources is every string literal in the module's test files. Most are
// Molang, valid and invalid, written to pin one rule each; the rest are
// messages and names, which are just more inputs. Harvesting them means
// every case a test was ever written for is also a case for ParseAll, with
// no second list to keep in step.
func testSources(t *testing.T) []string {
	t.Helper()
	var files []string
	for _, pattern := range []string{"../*_test.go", "../*/*_test.go"} {
		m, err := filepath.Glob(pattern)
		if err != nil {
			t.Fatal(err)
		}
		files = append(files, m...)
	}
	seen := map[string]bool{}
	var out []string
	fset := token.NewFileSet()
	for _, f := range files {
		file, err := goparser.ParseFile(fset, f, nil, 0)
		if err != nil {
			t.Fatalf("%s: %v", f, err)
		}
		ast.Inspect(file, func(n ast.Node) bool {
			lit, ok := n.(*ast.BasicLit)
			if !ok || lit.Kind != token.STRING {
				return true
			}
			s, err := strconv.Unquote(lit.Value)
			if err != nil || seen[s] || len(s) > 4000 {
				return true
			}
			seen[s] = true
			out = append(out, s)
			return true
		})
	}
	if len(out) < 500 {
		t.Fatalf("harvested only %d sources; the glob is probably wrong", len(out))
	}
	return out
}

// mutations derives broken sources from a working one: each token deleted in
// turn, and a stray token inserted before each. That is how real mistakes
// look -- one thing missing or one thing extra -- and it reaches the
// recovery paths far more often than hand-written cases would.
func mutations(src string) []string {
	toks, err := lexer.Tokenize(src)
	if err != nil || len(toks) > 60 {
		return nil
	}
	var out []string
	for i, tk := range toks {
		if tk.Kind == mtoken.EOF {
			break
		}
		end := tk.Pos + len(tk.Text)
		if tk.Kind == mtoken.String {
			end += 2
		}
		out = append(out, src[:tk.Pos]+" "+src[end:])
		for _, junk := range []string{";", "(", "}", "{", ",", "@", "q.", "'"} {
			out = append(out, src[:toks[i].Pos]+junk+src[toks[i].Pos:])
		}
	}
	return out
}

// ParseAll's first error is ParseWith's error, and a clean parse is
// ParseWith's tree -- for every source the test suite knows, both extension
// settings, and every mutation of each.
func TestParseAllAgreesWithParse(t *testing.T) {
	sources := testSources(t)
	var all []string
	for _, s := range sources {
		all = append(all, s)
		all = append(all, mutations(s)...)
	}
	checked, failing, multi := 0, 0, 0
	for _, ext := range []parser.Extensions{{}, {OptionalSemicolons: true}} {
		for _, src := range all {
			checked++
			want, wantErr := parser.ParseWith(src, ext)
			got, errs := parser.ParseAll(src, ext)
			if wantErr == nil {
				if len(errs) != 0 {
					t.Errorf("%q: ParseWith accepts it, ParseAll reports %v", src, errs)
					continue
				}
				if !reflect.DeepEqual(want, got) {
					t.Errorf("%q: ParseAll's tree differs from ParseWith's", src)
				}
				continue
			}
			failing++
			if len(errs) == 0 {
				t.Errorf("%q: ParseWith refuses it (%v), ParseAll accepts it", src, wantErr)
				continue
			}
			if got != nil {
				t.Errorf("%q: ParseAll returned a tree alongside errors", src)
			}
			pe := wantErr.(*parser.Error)
			if errs[0].Msg != pe.Msg || errs[0].Pos != pe.Pos {
				t.Errorf("%q: first error\n got  %q at %d\n want %q at %d", src, errs[0].Msg, errs[0].Pos, pe.Msg, pe.Pos)
			}
			if len(errs) > 1 {
				multi++
			}
			for _, e := range errs {
				if e.Pos < 0 || e.Pos > len(src) {
					t.Errorf("%q: error %q at %d, outside the source", src, e.Msg, e.Pos)
				}
			}
		}
	}
	t.Logf("%d parses compared, %d failing, %d with more than one error", checked, failing, multi)
}

func TestParseAllReportsEachBrokenStatement(t *testing.T) {
	cases := []struct {
		src  string
		want []string // messages, in order
	}{
		{
			// Two broken statements and a good one between them.
			"v.a = ; v.b = 1; v.c = );",
			[]string{"unexpected token ;", "unexpected token )"},
		},
		{
			// A mistake inside a block costs only that statement of it.
			"loop(2, { t.x = ; t.y = 1; }); v.z = 1 +;",
			[]string{"unexpected token ;", "unexpected token ;"},
		},
		{
			// A bad character is reported by the lexer alone, and the
			// statement after it is still checked.
			"v.a = 1 @ 2; v.b = (;",
			[]string{"unexpected character '@'", "unexpected token ;"},
		},
		{
			// The semicolon rules still apply around a broken statement.
			"v.a = ; v.b = 1",
			[]string{"unexpected token ;", "complex expressions (contains either '=' or ';') must end with a ';'"},
		},
		{
			// A block left open at the end: one report, not one per level.
			"q.x ? { v.a = 1;",
			[]string{"unexpected token EOF"},
		},
		{
			"'unterminated",
			[]string{"unterminated string literal"},
		},
	}
	for _, c := range cases {
		_, errs := parser.ParseAll(c.src, parser.Extensions{})
		var got []string
		for _, e := range errs {
			got = append(got, e.Msg)
		}
		if strings.Join(got, "\n") != strings.Join(c.want, "\n") {
			t.Errorf("%q:\n got  %q\n want %q", c.src, got, c.want)
		}
	}
}

func TestTokenizeAllKeepsGoing(t *testing.T) {
	toks, errs := lexer.TokenizeAll("1 @ 2 é 'open", lexer.Extensions{})
	if len(errs) != 3 {
		t.Fatalf("got %d errors, want 3: %v", len(errs), errs)
	}
	var kinds []mtoken.Kind
	for _, tk := range toks {
		kinds = append(kinds, tk.Kind)
	}
	want := []mtoken.Kind{mtoken.Number, mtoken.Illegal, mtoken.Number, mtoken.Illegal, mtoken.String, mtoken.EOF}
	if !reflect.DeepEqual(kinds, want) {
		t.Fatalf("kinds %v, want %v", kinds, want)
	}
	// The illegal token is the whole character, not its first byte.
	if toks[3].Text != "é" {
		t.Errorf("illegal token text %q, want %q", toks[3].Text, "é")
	}
	if toks[4].Text != "open" {
		t.Errorf("unterminated string text %q, want %q", toks[4].Text, "open")
	}
	// With nothing wrong, the tokens are TokenizeWith's.
	src := "v.a = q.b(1, 'x') ?? 2.5f;"
	a, _ := lexer.Tokenize(src)
	b, errs := lexer.TokenizeAll(src, lexer.Extensions{})
	if len(errs) != 0 || !reflect.DeepEqual(a, b) {
		t.Errorf("TokenizeAll differs from Tokenize on a clean source")
	}
}
