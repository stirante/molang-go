package main

import (
	"flag"
	"strings"
	"testing"
)

// run drives the command surface the way the binary does, minus the process.
func run(t *testing.T, stages []string, src string, opts *options) (string, error) {
	t.Helper()
	if opts == nil {
		opts = &options{}
	}
	var out strings.Builder
	err := runChain(stages, src, opts, &out)
	return out.String(), err
}

func mustRun(t *testing.T, stages []string, src string, opts *options) string {
	t.Helper()
	out, err := run(t, stages, src, opts)
	if err != nil {
		t.Fatalf("%v on %q: %v", stages, src, err)
	}
	return strings.TrimRight(out, "\n")
}

// A chain of transforms ends in Molang, so `molang fold` prints source
// without having to be told to.
func TestTransformOnlyChainPrintsSource(t *testing.T) {
	for _, c := range []struct{ stages, src, want string }{
		{"fold", "1 + 2 * 3", "7"},
		{"fmt", "temp.a=1;return temp.a;", "temp.a = 1; return temp.a;"},
		{"minify", "temp.a = 1; return temp.a;", "t.a=1;return t.a"},
		{"expand", "math.bitshift(v.x, 3)", "math.floor(variable.x / math.pow(2, 3))"},
		{"expand,fold", "math.bitshift(64, 3)", "8"},
		{"fold,minify", "1 + 2 * 3 + variable.x", "7+v.x"},
	} {
		got := mustRun(t, strings.Split(c.stages, ","), c.src, nil)
		if got != c.want {
			t.Errorf("%s(%q) = %q, want %q", c.stages, c.src, got, c.want)
		}
	}
}

// A chain may end in something that is not source, which the shell-pipe
// spelling cannot do -- a stage there can only hand on text.
func TestChainCanEndInAReport(t *testing.T) {
	got := mustRun(t, []string{"expand", "fold", "eval"}, "math.bitshift(64, 3)", nil)
	if got != "8" {
		t.Errorf("expand,fold,eval = %q, want 8", got)
	}
}

// A report or printer consumes the tree, so nothing may follow it. Catching
// that up front beats running three stages and discarding the result.
func TestTerminalStageMustBeLast(t *testing.T) {
	for _, stages := range [][]string{
		{"eval", "fold"},
		{"minify", "fold"},
		{"ast", "minify"},
	} {
		if _, err := run(t, stages, "1 + 1", nil); err == nil {
			t.Errorf("%v: accepted, want an error", stages)
		}
	}
}

func TestUnknownStageIsRefused(t *testing.T) {
	_, err := run(t, []string{"fold", "nope"}, "1 + 1", nil)
	if err == nil {
		t.Fatal("accepted an unknown stage")
	}
	if !strings.Contains(err.Error(), "nope") {
		t.Errorf("error %q does not name the bad stage", err)
	}
}

func TestParseErrorsReachTheCaller(t *testing.T) {
	if _, err := run(t, []string{"fmt"}, "1 +", nil); err == nil {
		t.Error("a syntax error was not reported")
	}
	// `#` comments are an extension and must stay off unless asked for.
	if _, err := run(t, []string{"fmt"}, "1 # c\n+ 2", nil); err == nil {
		t.Error("a comment parsed without -comments")
	}
	if _, err := run(t, []string{"fmt"}, "1 # c\n+ 2", &options{comments: true}); err != nil {
		t.Errorf("-comments did not enable the extension: %v", err)
	}
}

func TestEvalUsesTheSuppliedScope(t *testing.T) {
	opts := &options{scope: scopeFlag{"v.x=10", "q.mul=3"}}
	if got := mustRun(t, []string{"eval"}, "v.x * q.mul", opts); got != "30" {
		t.Errorf("got %q, want 30", got)
	}

	opts = &options{scope: scopeFlag{"array.h=5,6,7"}}
	if got := mustRun(t, []string{"eval"}, "array.h[1]", opts); got != "6" {
		t.Errorf("array element: got %q, want 6", got)
	}
}

// The seed is what makes an expression drawing from the RNG reproducible,
// which is the point of the RNG being injected at all.
func TestEvalIsReproducibleForASeed(t *testing.T) {
	opts := func() *options { return &options{seed: 7} }
	a := mustRun(t, []string{"eval"}, "math.random(0, 100)", opts())
	b := mustRun(t, []string{"eval"}, "math.random(0, 100)", opts())
	if a != b {
		t.Errorf("same seed gave %q then %q", a, b)
	}
	c := mustRun(t, []string{"eval"}, "math.random(0, 100)", &options{seed: 8})
	if a == c {
		t.Errorf("different seeds both gave %q", a)
	}
}

// An unresolved read ends the program, and -continue is the documented way
// out. Both halves are worth pinning, since the CLI is how most people will
// first meet this behaviour.
func TestEvalUnresolvedReadStopsUnlessToldOtherwise(t *testing.T) {
	if got := mustRun(t, []string{"eval"}, "v.missing + 1", nil); got != "0" {
		t.Errorf("got %q, want 0 (the read ends the program)", got)
	}
	if got := mustRun(t, []string{"eval"}, "v.missing + 1", &options{continueOn: true}); got != "1" {
		t.Errorf("-continue: got %q, want 1", got)
	}
}

func TestValidateAppliesRestrictions(t *testing.T) {
	if got := mustRun(t, []string{"validate"}, "math.max(v.a = 5, 3)", nil); got != "ok" {
		t.Errorf("unrestricted validate said %q", got)
	}

	_, err := run(t, []string{"validate"}, "math.max(v.a = 5, 3)", &options{noSideEffects: true})
	if err == nil {
		t.Fatal("-no-side-effects accepted an assignment")
	}
	if !strings.Contains(err.Error(), "Assignment") {
		t.Errorf("error %q does not name the operation", err)
	}

	// -no-random only bites together with -no-side-effects, matching the
	// engine's own switch.
	if _, err := run(t, []string{"validate"}, "math.random(0, 1)",
		&options{noSideEffects: true}); err != nil {
		t.Errorf("math.random refused without -no-random: %v", err)
	}
	if _, err := run(t, []string{"validate"}, "math.random(0, 1)",
		&options{noSideEffects: true, noRandom: true}); err == nil {
		t.Error("-no-random accepted math.random")
	}
}

func TestForbidMatchesOperationsByName(t *testing.T) {
	for _, name := range []string{"loop", "Loop 'loop'", "assignment"} {
		set, err := parseOpNames(name)
		if err != nil {
			t.Errorf("%q: %v", name, err)
			continue
		}
		if set.Empty() {
			t.Errorf("%q matched nothing", name)
		}
	}
	if _, err := parseOpNames("definitely-not-an-operation"); err == nil {
		t.Error("a nonsense name was accepted")
	}

	_, err := run(t, []string{"validate"}, "loop(3, { t.i = 1; });", &options{forbid: "loop"})
	if err == nil {
		t.Error("-forbid loop accepted a loop")
	}
}

func TestOpsAndRefsDescribeTheProgram(t *testing.T) {
	ops := mustRun(t, []string{"ops"}, "v.a = math.random(0, 1)", nil)
	for _, want := range []string{"Random 'math.random'", "Assignment '='", "Entity Variable"} {
		if !strings.Contains(ops, want) {
			t.Errorf("ops output missing %q:\n%s", want, ops)
		}
	}

	refs := mustRun(t, []string{"refs"}, "v.out = q.life_time * math.sin(v.angle); return v.out;", nil)
	for _, want := range []string{"life_time", "sin", "needs from the host: variable.angle"} {
		if !strings.Contains(refs, want) {
			t.Errorf("refs output missing %q:\n%s", want, refs)
		}
	}
}

// The outline and the JSON form are built from one description, so a node
// present in one is present in the other.
func TestASTRendersBothWays(t *testing.T) {
	const src = "v.a = math.max(1, q.x) ? 'yes' : 'no';"

	outline := mustRun(t, []string{"ast"}, src, nil)
	for _, want := range []string{"Program", "AssignExpr", "TernaryExpr", "CallExpr", "└─", "then:"} {
		if !strings.Contains(outline, want) {
			t.Errorf("outline missing %q:\n%s", want, outline)
		}
	}

	js := mustRun(t, []string{"ast"}, src, &options{jsonOut: true})
	for _, want := range []string{`"kind": "Program"`, `"kind": "TernaryExpr"`, `"name": "then"`} {
		if !strings.Contains(js, want) {
			t.Errorf("json missing %q:\n%s", want, js)
		}
	}
}

// The outline must actually nest -- a flat dump is not a tree, and this
// caught exactly that bug.
func TestASTOutlineIndents(t *testing.T) {
	out := mustRun(t, []string{"ast"}, "loop(3, { t.i = t.i + 1; });", nil)
	var deepest int
	for _, line := range strings.Split(out, "\n") {
		if d := len(line) - len(strings.TrimLeft(line, "│  ")); d > deepest {
			deepest = d
		}
	}
	if deepest < 6 {
		t.Errorf("outline barely indents (deepest prefix %d):\n%s", deepest, out)
	}
}

func TestScopeAssignmentErrors(t *testing.T) {
	for _, bad := range []string{"vx=1", "v.x", "v.x=abc", "math.pi=3"} {
		if _, err := run(t, []string{"eval"}, "1", &options{scope: scopeFlag{bad}}); err == nil {
			t.Errorf("-scope %q was accepted", bad)
		}
	}
}

// A comma-separated list of stages IS the command, and that spelling is the
// cheap one: it parses once. The shell-pipe spelling re-parses per stage,
// which is why the help text steers people here.
func TestStageListIsTheCommand(t *testing.T) {
	for _, spec := range []string{"fold", "fold,minify", "expand,fold,eval", " fold , minify "} {
		if !isStageSpec(spec) {
			t.Errorf("%q was not recognised as a stage list", spec)
		}
	}
	for _, spec := range []string{"nope", "fold,nope", "", ",", "repl"} {
		if isStageSpec(spec) {
			t.Errorf("%q was accepted as a stage list", spec)
		}
	}
}

func TestSplitStagesIgnoresBlanksAndSpaces(t *testing.T) {
	got := splitStages(" expand , fold ,, minify ")
	want := []string{"expand", "fold", "minify"}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got %v, want %v", got, want)
		}
	}
}

// Flags come from the stages actually named, so a chain gets the union and a
// single stage does not silently accept a flag it would ignore.
func TestFlagsFollowTheStagesNamed(t *testing.T) {
	hasFlag := func(names []string, flagName string) bool {
		fs := flag.NewFlagSet("test", flag.ContinueOnError)
		(&options{}).register(fs, names)
		return fs.Lookup(flagName) != nil
	}
	cases := []struct {
		names []string
		flag  string
		want  bool
	}{
		{[]string{"eval"}, "seed", true},
		{[]string{"fmt"}, "seed", false},
		{[]string{"fold", "eval"}, "seed", true},
		{[]string{"validate"}, "no-side-effects", true},
		{[]string{"eval"}, "no-side-effects", false},
		{[]string{"ast"}, "json", true},
		{[]string{"minify"}, "json", false},
		{[]string{"fmt"}, "comments", true},
	}
	for _, c := range cases {
		if got := hasFlag(c.names, c.flag); got != c.want {
			t.Errorf("%v: -%s present=%v, want %v", c.names, c.flag, got, c.want)
		}
	}
}
