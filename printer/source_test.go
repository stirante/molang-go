package printer

import (
	"encoding/json"
	"flag"
	"fmt"
	goast "go/ast"
	goparser "go/parser"
	gotoken "go/token"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/stirante/molang-go/parser"
)

var update = flag.Bool("update", false, "rewrite the golden files in testdata/format")

// corpusSnippet is a piece of Molang from somewhere else in the repository,
// and how it has to be read.
type corpusSnippet struct {
	src  string
	opts SourceOptions
	// vanilla: it parses with no extension, so its formatted text has to as
	// well -- formatting must not turn Molang the game loads into Molang it
	// does not.
	vanilla bool
}

// testCorpus is every string literal in the module's tests that parses as
// Molang: the expressions every other package's tests pin, reused here so
// the formatter is held to all of them without a list of its own to keep up.
func testCorpus(t *testing.T) []corpusSnippet {
	t.Helper()
	seen := map[string]bool{}
	var out []corpusSnippet
	root := ".."
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			switch d.Name() {
			case ".git", "node_modules", "dist", "research":
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, "_test.go") {
			return nil
		}
		file, err := goparser.ParseFile(gotoken.NewFileSet(), path, nil, 0)
		if err != nil {
			return err
		}
		goast.Inspect(file, func(n goast.Node) bool {
			lit, ok := n.(*goast.BasicLit)
			if !ok || lit.Kind != gotoken.STRING {
				return true
			}
			s, err := strconv.Unquote(lit.Value)
			if err != nil || seen[s] || strings.TrimSpace(s) == "" {
				return true
			}
			seen[s] = true
			if _, err := parser.Parse(s); err == nil {
				out = append(out, corpusSnippet{src: s, vanilla: true})
			} else if _, err := parser.ParseWith(s, parser.Extensions{OptionalSemicolons: true}); err == nil {
				out = append(out, corpusSnippet{src: s, opts: SourceOptions{OptionalSemicolons: true}})
			} else if _, err := parser.ParseWith(s, parser.Extensions{Comments: true}); err == nil {
				out = append(out, corpusSnippet{src: s, opts: SourceOptions{Comments: true}})
			}
			return true
		})
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].src < out[j].src })
	return out
}

var layouts = []Layout{
	{},
	{IndentWidth: 2, MaxWidth: 40},
	{UseTabs: true, MaxWidth: 16},
	{IndentWidth: 3, MaxWidth: 1},
}

// checkFormat holds one source to the formatter's two promises -- that
// formatting its output again changes nothing, and that the output parses
// to the source's tree -- and returns the output.
func checkFormat(t *testing.T, src string, o SourceOptions, vanilla bool) string {
	t.Helper()
	before, err := readSource(src, o)
	if err != nil {
		t.Errorf("read %q: %v", src, err)
		return ""
	}
	out, err := FormatSource(src, o)
	if err != nil {
		t.Errorf("FormatSource(%q, %+v): %v", src, o, err)
		return ""
	}
	after, err := readSource(out, o)
	if err != nil {
		t.Errorf("FormatSource(%q, %+v) =\n%s\ndoes not parse: %v", src, o, out, err)
		return out
	}
	want := before.prog
	if want != nil && o.OptionalSemicolons {
		// Without the semicolon rules a program can parse that the game
		// would not load, and Format writes it as one that does: `v.x = 5`
		// as `return v.x = 5;`, `loop(2, {...})` with its `;` (see
		// topLevel). The tree to match is the one Format's text reads as.
		want, err = parser.ParseWith(Format(want), parser.Extensions{Comments: o.Comments, OptionalSemicolons: true})
		if err != nil {
			t.Fatal(err)
		}
	}
	if !reflect.DeepEqual(want, after.prog) {
		t.Errorf("FormatSource(%q, %+v) =\n%s\nparses to a different tree", src, o, out)
	}
	if again, err := FormatSource(out, o); err != nil || again != out {
		t.Errorf("FormatSource is not idempotent on %q (%+v):\nonce:\n%s\ntwice:\n%s\n(err %v)", src, o, out, again, err)
	}
	if vanilla && !o.Templates {
		if _, err := parser.Parse(out); err != nil {
			t.Errorf("FormatSource(%q) =\n%s\nno longer loads: %v", src, out, err)
		}
	}
	return out
}

func TestFormatSourceOverTheTestCorpus(t *testing.T) {
	corpus := testCorpus(t)
	if len(corpus) < 500 {
		t.Fatalf("only %d snippets in the test corpus; the walk is missing the tests", len(corpus))
	}
	t.Logf("%d snippets", len(corpus))
	for _, c := range corpus {
		for _, l := range layouts {
			o := c.opts
			o.Layout = l
			out := checkFormat(t, c.src, o, c.vanilla)
			if c.vanilla && out != "" {
				// A statement per line, and nothing but whitespace
				// different from Format's one line.
				prog, _ := parser.Parse(c.src)
				if got := FormatLayout(prog, l); got != out {
					t.Errorf("FormatLayout(%q) = %q, FormatSource = %q", c.src, got, out)
				}
			}
		}
		// The one-line styles are Format and Minify, exactly.
		prog, _ := readSource(c.src, c.opts)
		if prog.empty || len(prog.comments) > 0 {
			continue
		}
		for _, style := range []Style{StyleOneLine, StyleMinified} {
			o := c.opts
			o.Style = style
			out, err := FormatSource(c.src, o)
			want := Format(prog.prog)
			if style == StyleMinified {
				want = Minify(prog.prog)
			}
			if err != nil || out != want {
				t.Errorf("FormatSource(%q, style %d) = %q, %v; want %q", c.src, style, out, err, want)
			}
		}
	}
}

// TestFormatSourceKeepsEveryComment puts a comment on its own line before
// every line, and another at the end of every line, of each snippet's
// formatted text -- in blocks, between the arms of a broken conditional,
// after an opening brace -- and checks that each comes out once, and that
// the formatter's two promises still hold.
func TestFormatSourceKeepsEveryComment(t *testing.T) {
	for _, c := range testCorpus(t) {
		o := c.opts
		o.Comments = true
		o.Layout = Layout{IndentWidth: 2, MaxWidth: 30}
		base, err := FormatSource(c.src, o)
		if err != nil {
			t.Errorf("FormatSource(%q): %v", c.src, err)
			continue
		}
		var b strings.Builder
		for i, ln := range strings.Split(base, "\n") {
			fmt.Fprintf(&b, "# above %d\n", i)
			if i%3 == 0 {
				b.WriteString("\n\n")
			}
			fmt.Fprintf(&b, "%s # after %d\n", ln, i)
		}
		src := b.String()
		out := checkFormat(t, src, o, false)
		if out == "" {
			continue
		}
		for i := range strings.Split(base, "\n") {
			for _, want := range []string{fmt.Sprintf("# above %d", i), fmt.Sprintf("# after %d", i)} {
				if n := strings.Count(out+"\n", want+"\n"); n != 1 {
					t.Errorf("%q appears %d times in\n%s\nformatted from\n%s", want, n, out, src)
				}
			}
		}
	}
}

// TestFormatSourceGolden formats the vanilla-shaped files in
// testdata/format and compares them with the .golden file beside each.
// go test -run Golden -update rewrites the golden files.
func TestFormatSourceGolden(t *testing.T) {
	files, _ := filepath.Glob(filepath.Join("testdata", "format", "*.molang"))
	if len(files) == 0 {
		t.Fatal("no samples")
	}
	o := SourceOptions{Comments: true, Templates: true, Layout: Layout{IndentWidth: 4, MaxWidth: 80}}
	for _, name := range files {
		src, err := os.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		out := checkFormat(t, string(src), o, false) + "\n"
		golden := strings.TrimSuffix(name, ".molang") + ".golden"
		if *update {
			os.WriteFile(golden, []byte(out), 0o644)
			continue
		}
		want, _ := os.ReadFile(golden)
		if out != strings.ReplaceAll(string(want), "\r\n", "\n") {
			t.Errorf("%s:\n%s\nwant:\n%s", name, out, want)
		}
		for _, l := range layouts {
			o := o
			o.Layout = l
			checkFormat(t, string(src), o, false)
		}
	}
}

// TestFormatSourceOverVanillaPacks runs the formatter over every Molang
// string of the vanilla packs, when BEDROCK_SAMPLES names a checkout of
// github.com/Mojang/bedrock-samples.
func TestFormatSourceOverVanillaPacks(t *testing.T) {
	root := os.Getenv("BEDROCK_SAMPLES")
	if root == "" {
		t.Skip("set BEDROCK_SAMPLES to a bedrock-samples checkout to run")
	}
	seen := map[string]bool{}
	var strs []string
	var collect func(v any)
	collect = func(v any) {
		switch x := v.(type) {
		case string:
			if !seen[x] && (strings.Contains(x, ".") || strings.ContainsAny(x, "?=")) {
				seen[x] = true
				strs = append(strs, x)
			}
		case []any:
			for _, e := range x {
				collect(e)
			}
		case map[string]any:
			for _, e := range x {
				collect(e)
			}
		}
	}
	filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() && d.Name() == "textures" {
			return filepath.SkipDir
		}
		if d.IsDir() || !strings.HasSuffix(path, ".json") {
			return nil
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return nil
		}
		var v any
		// The packs' files may start with a byte order mark.
		if json.Unmarshal([]byte(strings.TrimPrefix(string(b), "\uFEFF")), &v) == nil {
			collect(v)
		}
		return nil
	})
	n := 0
	for _, s := range strs {
		if _, err := parser.Parse(s); err != nil {
			continue
		}
		n++
		checkFormat(t, s, SourceOptions{Layout: Layout{MaxWidth: 60}}, true)
	}
	t.Logf("%d vanilla expressions", n)
	if n == 0 {
		t.Error("no Molang found under BEDROCK_SAMPLES")
	}
}

func TestFormatSourceLayout(t *testing.T) {
	cases := []struct {
		name, src string
		width     int
		want      string
	}{
		{"statements", "v.a = 1; v.b = 2;", 0, "variable.a = 1;\nvariable.b = 2;"},
		{"bare expression keeps no semicolon", "q.is_baby ? 1 : 2", 0, "query.is_baby ? 1 : 2"},
		{"block arms", "q.a ? { v.x = 1; } : { v.x = 2; };", 0,
			"query.a ? {\n    variable.x = 1;\n} : {\n    variable.x = 2;\n};"},
		{"else-if chain of blocks", "q.a ? { v.x = 1; } : q.b ? { v.x = 2; } : { v.x = 3; };", 0,
			"query.a ? {\n    variable.x = 1;\n} : query.b ? {\n    variable.x = 2;\n} : {\n    variable.x = 3;\n};"},
		{"nested blocks", "loop(2, { q.a ? { v.x = 1; }; });", 0,
			"loop(2, {\n    query.a ? {\n        variable.x = 1;\n    };\n});"},
		{"a long conditional chain", "v.x = q.v == 1 ? 'one' : q.v == 2 ? 'two' : 'many';", 36,
			"variable.x = query.v == 1 ? 'one'\n    : query.v == 2 ? 'two'\n    : 'many';"},
		{"a long ?? chain", "v.x = v.first_choice ?? (v.second_choice ?? (v.third_choice ?? 0));", 40,
			"variable.x = variable.first_choice\n    ?? (variable.second_choice\n    ?? (variable.third_choice\n    ?? 0));"},
		{"a long call", "v.x = math.clamp(v.some_value, v.lower_bound, 1);", 30,
			"variable.x = math.clamp(\n    variable.some_value,\n    variable.lower_bound,\n    1\n);"},
		{"comments and blank lines", "# a\n\n\nv.a = 1;   # b   \n# c\n\nv.b = 2;\n", 0,
			"# a\n\nvariable.a = 1; # b\n# c\n\nvariable.b = 2;"},
		{"a comment inside a statement moves before it", "v.a = q.b # why\n ? 1 : 2;", 0,
			"# why\nvariable.a = query.b ? 1 : 2;"},
		{"comments in a block", "q.a ? { # first\n v.x = 1; # set\n # last\n};", 0,
			"query.a ? {\n    # first\n    variable.x = 1; # set\n    # last\n};"},
		{"only comments", "# one\n\n# two\n", 0, "# one\n\n# two"},
		{"templates", "v.#{name} = #{value};", 0, "variable.#{name} = #{value};"},
		{"a template is not a comment", "v.a = #{ {'a': 1}.a } + 1; # really", 0,
			"variable.a = #{ {'a': 1}.a } + 1; # really"},
		{"a # in a string is not a comment", "v.a = 'x # y';", 0, "variable.a = 'x # y';"},
	}
	for _, c := range cases {
		o := SourceOptions{Comments: true, Templates: true, Layout: Layout{MaxWidth: c.width}}
		got := checkFormat(t, c.src, o, false)
		if got != c.want {
			t.Errorf("%s:\n%s\nwant:\n%s", c.name, got, c.want)
		}
	}
}

func TestFormatSourceTabs(t *testing.T) {
	got, err := FormatSource("loop(2, { v.a = 1; });", SourceOptions{Layout: Layout{UseTabs: true}})
	if err != nil || got != "loop(2, {\n\tvariable.a = 1;\n});" {
		t.Errorf("got %q, %v", got, err)
	}
}

func TestFormatSourceOneLineKeepsTemplates(t *testing.T) {
	o := SourceOptions{Templates: true, Style: StyleOneLine}
	if got, err := FormatSource("v.#{n}=#{v}*2;", o); err != nil || got != "variable.#{n} = #{v} * 2;" {
		t.Errorf("one line: %q, %v", got, err)
	}
	o.Style = StyleMinified
	if got, err := FormatSource("variable.#{n} = #{v} * 2;", o); err != nil || got != "v.#{n}=#{v}*2;" {
		t.Errorf("minified: %q, %v", got, err)
	}
}

func TestFormatSourceRefuses(t *testing.T) {
	for _, c := range []struct {
		src string
		o   SourceOptions
	}{
		// A one-line style has nowhere to put a comment.
		{"v.a = 1; # note", SourceOptions{Comments: true, Style: StyleOneLine}},
		{"v.a = 1; # note", SourceOptions{Comments: true, Style: StyleMinified}},
		// A template where no value or name can stand.
		{"v.a #{op} 1", SourceOptions{Templates: true}},
		// Comments not asked for.
		{"v.a = 1; # note", SourceOptions{}},
		{"v.a = ", SourceOptions{}},
	} {
		if out, err := FormatSource(c.src, c.o); err == nil {
			t.Errorf("FormatSource(%q, %+v) = %q, want an error", c.src, c.o, out)
		}
	}
}

func TestFormatSourceRange(t *testing.T) {
	src := "# head\nv.a=1;\n\n\nv.b=q.x?{v.c=1;}:0;   # tail\nv.d=#{x};\n"
	o := SourceOptions{Comments: true, Templates: true}
	whole, err := FormatSource(src, o)
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		start, end int
		want       string
		from, to   int
	}{
		// Inside v.b's block: all of v.b, its trailing comment with it.
		{strings.Index(src, "v.c"), strings.Index(src, "v.c") + 1,
			"variable.b = query.x ? {\n    variable.c = 1;\n} : 0; # tail",
			strings.Index(src, "v.b"), strings.Index(src, "\nv.d")},
		// A cursor in the first statement.
		{strings.Index(src, "v.a"), strings.Index(src, "v.a"), "variable.a = 1;", 7, 13},
		// A range across two statements keeps the blank line between them.
		{strings.Index(src, "=1;"), strings.Index(src, "q.x"),
			"variable.a = 1;\n\nvariable.b = query.x ? {\n    variable.c = 1;\n} : 0; # tail",
			strings.Index(src, "v.a"), strings.Index(src, "\nv.d")},
		// Into a template.
		{strings.Index(src, "{x}"), strings.Index(src, "{x}") + 1, "variable.d = #{x};",
			strings.Index(src, "v.d"), len(src) - 1},
		// Blank lines between statements: nothing.
		{strings.Index(src, "\n\n") + 1, strings.Index(src, "\n\n") + 2, "", strings.Index(src, "\n\n") + 1, strings.Index(src, "\n\n") + 1},
	} {
		text, from, to, err := FormatSourceRange(src, c.start, c.end, o)
		if err != nil || text != c.want || from != c.from || to != c.to {
			t.Errorf("range [%d,%d): %q [%d,%d) %v\nwant %q [%d,%d)", c.start, c.end, text, from, to, err, c.want, c.from, c.to)
		}
		if text != "" && !strings.Contains(whole, text) {
			t.Errorf("range [%d,%d) formatted as %q, which the whole file does not contain:\n%s", c.start, c.end, text, whole)
		}
	}
}
