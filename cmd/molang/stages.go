package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"sort"
	"strings"

	molang "molang-go"
	"molang-go/ast"
	"molang-go/eval"
	"molang-go/parser"
	"molang-go/printer"
	"molang-go/transform"
)

// A pipeline carries a parsed tree. Stages come in three kinds, and the
// difference is what a chain may end with:
//
//	transform  tree -> tree      may appear anywhere
//	printer    tree -> Molang    ends the chain, output is source again
//	report     tree -> text      ends the chain, output is not Molang
//
// A chain ending in a transform prints its tree with fmt, so `molang fold`
// does the obvious thing without having to say `molang pipe fold,fmt`.
type stageKind int

const (
	kindTransform stageKind = iota
	kindPrinter
	kindReport
)

type stage struct {
	kind      stageKind
	transform func(*ast.Program, *options) (*ast.Program, error)
	print     func(*ast.Program) string
	report    func(*ast.Program, *options, io.Writer) error
}

// options is everything the stages can be told, gathered from one flag set
// so that `molang eval -seed 7` and `molang pipe fold,eval -seed 7` accept
// the same words.
type options struct {
	comments   bool
	scope      scopeFlag
	seed       uint
	continueOn bool
	verbose    bool

	noSideEffects bool
	noRandom      bool
	forbid        string

	jsonOut bool
}

// register declares the flags the named stages actually use, so a chain gets
// the union of its stages' flags and nothing else -- `molang fmt -seed 3`
// stays an error rather than silently accepting a meaningless number.
func (o *options) register(fs *flag.FlagSet, names []string) {
	fs.BoolVar(&o.comments, "comments", false, "accept `#` comments (an extension, not vanilla Molang)")

	has := func(want ...string) bool {
		for _, n := range names {
			for _, w := range want {
				if n == w {
					return true
				}
			}
		}
		return false
	}

	if has("eval", "repl") {
		fs.Var(&o.scope, "scope", "seed a name, repeatable: -scope v.x=1 -scope array.h=1,2,3")
		fs.UintVar(&o.seed, "seed", 0, "RNG seed, so a program using math.random is reproducible")
		fs.BoolVar(&o.continueOn, "continue", false, "keep going past an unresolved read instead of ending the program")
		fs.BoolVar(&o.verbose, "v", false, "also report draws consumed and unresolved reads")
	}
	if has("validate", "repl") {
		fs.BoolVar(&o.noSideEffects, "no-side-effects", false, "reject assignment, as the contexts that forbid it do")
		fs.BoolVar(&o.noRandom, "no-random", false, "with -no-side-effects, also reject math.random/random_integer")
		fs.StringVar(&o.forbid, "forbid", "", "reject these operations by name, comma-separated (see `molang ops`)")
	}
	if has("ast", "repl") {
		fs.BoolVar(&o.jsonOut, "json", false, "print the tree as JSON instead of an indented outline")
	}
}

func stages() map[string]stage {
	return map[string]stage{
		"fold": {kind: kindTransform, transform: func(p *ast.Program, _ *options) (*ast.Program, error) {
			return transform.FoldConstants(p), nil
		}},
		"expand": {kind: kindTransform, transform: func(p *ast.Program, _ *options) (*ast.Program, error) {
			return transform.NewRegistryWithDefaults().Expand(p)
		}},

		"fmt":    {kind: kindPrinter, print: printer.Format},
		"minify": {kind: kindPrinter, print: printer.Minify},

		"eval":     {kind: kindReport, report: reportEval},
		"validate": {kind: kindReport, report: reportValidate},
		"ast":      {kind: kindReport, report: reportAST},
		"refs":     {kind: kindReport, report: reportRefs},
		"ops":      {kind: kindReport, report: reportOps},
	}
}

// ---------------------------------------------------------------------
// dispatch
// ---------------------------------------------------------------------

// runStages handles one stage or a comma-separated chain of them, which is
// the same code path either way: `molang fold` and `molang expand,fold,eval`
// differ only in how many names were given.
//
// A chain named this way parses ONCE and hands the tree from stage to stage.
// That is the whole reason to prefer it over a shell pipe, which re-parses
// at every stage and prints the source out in between.
func runStages(spec string, args []string) error {
	names := splitStages(spec)
	if err := knownStages(names); err != nil {
		return err
	}

	fs := flag.NewFlagSet("molang "+spec, flag.ExitOnError)
	opts := &options{}
	opts.register(fs, names)
	if err := fs.Parse(args); err != nil {
		return err
	}
	src, err := readSource(fs.Args())
	if err != nil {
		return err
	}
	return runChain(names, src, opts, os.Stdout)
}

func splitStages(spec string) []string {
	parts := strings.Split(spec, ",")
	out := parts[:0]
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

// knownStages checks the names before any flag is declared, so an unknown
// stage is reported as such rather than as a missing flag.
func knownStages(names []string) error {
	if len(names) == 0 {
		return fmt.Errorf("no stage given")
	}
	all := stages()
	for _, n := range names {
		if _, ok := all[n]; !ok {
			return fmt.Errorf("unknown stage %q", n)
		}
	}
	return nil
}

// isStageSpec reports whether a command word names a stage or a chain of
// them, which is how main tells `molang fold,minify` from a typo.
func isStageSpec(spec string) bool {
	return knownStages(splitStages(spec)) == nil
}

// runChain is the whole command surface: parse once, walk the stages, and
// let whichever stage ends the chain decide what comes out.
func runChain(names []string, src string, opts *options, out io.Writer) error {
	all := stages()

	for i, n := range names {
		n = strings.TrimSpace(n)
		st, ok := all[n]
		if !ok {
			return fmt.Errorf("unknown stage %q", n)
		}
		if st.kind != kindTransform && i != len(names)-1 {
			return fmt.Errorf("stage %q ends a chain and cannot be followed by %q", n, names[i+1])
		}
		names[i] = n
	}

	tree, err := parser.ParseWith(src, extensions(opts.comments))
	if err != nil {
		return err
	}

	for _, n := range names {
		st := all[n]
		switch st.kind {
		case kindTransform:
			tree, err = st.transform(tree, opts)
			if err != nil {
				return err
			}
		case kindPrinter:
			fmt.Fprintln(out, st.print(tree))
			return nil
		case kindReport:
			return st.report(tree, opts, out)
		}
	}

	// The chain was all transforms, so hand back Molang.
	fmt.Fprintln(out, printer.Format(tree))
	return nil
}

// ---------------------------------------------------------------------
// reports
// ---------------------------------------------------------------------

func buildContext(opts *options) (*eval.Context, *[]string, error) {
	sc := molang.NewScope()
	if err := applyScope(sc, opts.scope); err != nil {
		return nil, nil, err
	}
	var unresolved []string
	ctx := &eval.Context{
		RNG:                      newRNG(opts.seed),
		Scope:                    sc,
		ContinueOnUnresolvedRead: opts.continueOn,
		OnUnresolvedRead:         func(name string) { unresolved = append(unresolved, name) },
	}
	return ctx, &unresolved, nil
}

func reportEval(tree *ast.Program, opts *options, out io.Writer) error {
	prog, err := molang.CompileAST(tree)
	if err != nil {
		return err
	}
	ctx, unresolved, err := buildContext(opts)
	if err != nil {
		return err
	}
	v := prog.Run(ctx)
	fmt.Fprintln(out, formatValue(v))

	if opts.verbose {
		fmt.Fprintf(out, "\nscope after the run:\n")
		printBag(out, "variable", ctx.Scope.Variable)
		printBag(out, "temp", ctx.Scope.Temp)
		if len(*unresolved) > 0 {
			fmt.Fprintf(out, "unresolved reads (%d): %s\n",
				len(*unresolved), strings.Join(dedupe(*unresolved), ", "))
			if !opts.continueOn {
				fmt.Fprintln(out, "  the first of these ended the program -- pass -continue to evaluate past it")
			}
		}
	}
	return nil
}

func printBag(out io.Writer, ns string, bag map[string]float64) {
	if len(bag) == 0 {
		return
	}
	keys := make([]string, 0, len(bag))
	for k := range bag {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		fmt.Fprintf(out, "  %s.%s = %s\n", ns, k, formatValue(bag[k]))
	}
}

func dedupe(in []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, s := range in {
		if !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	return out
}

// reportValidate answers "would the game load this", as far as this package
// can tell. Parsing is the necessary half; the operation allow-list is the
// half that depends on where the expression is written.
func reportValidate(tree *ast.Program, opts *options, out io.Writer) error {
	forbidden := ast.OpSet{}
	if opts.noSideEffects {
		forbidden = forbidden.Union(molang.SideEffectOps(opts.noRandom))
	}
	if opts.forbid != "" {
		named, err := parseOpNames(opts.forbid)
		if err != nil {
			return err
		}
		forbidden = forbidden.Union(named)
	}
	if err := molang.CheckOps(tree, forbidden); err != nil {
		return err
	}
	if _, err := molang.CompileAST(tree); err != nil {
		return err
	}
	fmt.Fprintln(out, "ok")
	return nil
}

// parseOpNames accepts the operation names `molang ops` prints, matched
// loosely so that "assignment", "Assignment '='" and "math.random" all work.
func parseOpNames(list string) (ast.OpSet, error) {
	var set ast.OpSet
	for _, want := range strings.Split(list, ",") {
		want = strings.ToLower(strings.TrimSpace(want))
		if want == "" {
			continue
		}
		found := false
		for op := ast.Op(0); op < 109; op++ {
			name := strings.ToLower(op.String())
			if name == "<unknown expression op>" {
				continue
			}
			if name == want || strings.Contains(name, want) {
				set.Add(op)
				found = true
			}
		}
		if !found {
			return set, fmt.Errorf("-forbid: no operation matches %q", want)
		}
	}
	return set, nil
}

func reportRefs(tree *ast.Program, _ *options, out io.Writer) error {
	r := molang.References(tree)
	list := func(label string, v []string) {
		if len(v) > 0 {
			fmt.Fprintf(out, "%-16s %s\n", label+":", strings.Join(v, ", "))
		}
	}
	list("variable reads", r.VariableReads)
	list("variable writes", r.VariableWrites)
	list("temp reads", r.TempReads)
	list("temp writes", r.TempWrites)
	list("context reads", r.ContextReads)
	list("queries", r.Queries)
	list("math", r.MathFuncs)
	list("arrays", r.Arrays)
	list("entities", r.Entities)
	list("resources", r.Resources)
	if r.UsesThis {
		fmt.Fprintln(out, "uses `this`")
	}
	if r.UsesRandom {
		fmt.Fprintln(out, "consumes random draws")
	}
	if need := r.NeedsFromHost(); len(need) > 0 {
		fmt.Fprintf(out, "\nneeds from the host: %s\n", strings.Join(need, ", "))
	}
	return nil
}

func reportOps(tree *ast.Program, _ *options, out io.Writer) error {
	for _, op := range ast.OpsUsed(tree).Ops() {
		fmt.Fprintf(out, "%3d  %s\n", int(op), op.String())
	}
	return nil
}
