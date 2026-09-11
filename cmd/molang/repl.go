package main

import (
	"bufio"
	"flag"
	"fmt"
	"os"
	"sort"
	"strings"

	molang "github.com/stirante/molang-go"
	"github.com/stirante/molang-go/ast"
	"github.com/stirante/molang-go/eval"
	"github.com/stirante/molang-go/parser"
	"github.com/stirante/molang-go/printer"
	"github.com/stirante/molang-go/transform"
)

const replHelp = `  <expression>      evaluate it and print the value
  :ast <expr>       print the syntax tree
  :fmt <expr>       reformat
  :min <expr>       minify
  :fold <expr>      constant-fold, then print
  :expand <expr>    expand macros, then print
  :ops <expr>       list the operations it uses
  :refs <expr>      list what it names
  :check <expr>     validate against the current restrictions

  :scope            show every name currently set
  :set v.x = 1      set one (array.a = 1,2,3 works too)
  :unset v.x        remove one, which is NOT the same as setting it to 0
  :seed 7           reseed the RNG
  :continue on|off  keep going past an unresolved read
  :reset            clear the scope and reseed
  :help             this list
  :quit             leave (or Ctrl-D)
`

// The REPL keeps one scope and one RNG across lines, because that is how the
// engine treats them: temp. and variable. outlive a single expression, and
// the draw sequence is a continuing thing rather than a fresh one per line.
type repl struct {
	opts  *options
	scope *eval.Scope
	rng   eval.RNG
	out   *bufio.Writer
}

func runREPL(args []string) error {
	fs := flag.NewFlagSet("molang repl", flag.ExitOnError)
	opts := &options{}
	opts.register(fs, []string{"repl"})
	if err := fs.Parse(args); err != nil {
		return err
	}

	r := &repl{opts: opts, scope: molang.NewScope(), rng: newRNG(opts.seed), out: bufio.NewWriter(os.Stdout)}
	defer r.out.Flush()

	if err := applyScope(r.scope, opts.scope); err != nil {
		return err
	}

	fmt.Fprintln(r.out, "molang repl -- :help for commands, :quit to leave")
	if opts.comments {
		fmt.Fprintln(r.out, "(# comments accepted -- an extension, not vanilla Molang)")
	}
	r.out.Flush()

	in := bufio.NewScanner(os.Stdin)
	in.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	for {
		fmt.Fprint(r.out, "> ")
		r.out.Flush()
		if !in.Scan() {
			fmt.Fprintln(r.out)
			return in.Err()
		}
		line := strings.TrimSpace(in.Text())
		if line == "" {
			continue
		}
		if quit := r.dispatch(line); quit {
			return nil
		}
		r.out.Flush()
	}
}

func (r *repl) dispatch(line string) (quit bool) {
	if !strings.HasPrefix(line, ":") {
		r.eval(line)
		return false
	}
	cmd, rest, _ := strings.Cut(line, " ")
	rest = strings.TrimSpace(rest)

	switch cmd {
	case ":quit", ":q", ":exit":
		return true
	case ":help", ":h", ":?":
		fmt.Fprint(r.out, replHelp)
	case ":scope":
		r.showScope()
	case ":set":
		r.set(rest)
	case ":unset":
		r.unset(rest)
	case ":seed":
		r.seed(rest)
	case ":continue":
		r.setContinue(rest)
	case ":reset":
		r.scope = molang.NewScope()
		r.rng = newRNG(r.opts.seed)
		fmt.Fprintln(r.out, "scope cleared, RNG reseeded")
	case ":ast", ":fmt", ":min", ":fold", ":expand", ":ops", ":refs", ":check":
		r.stage(cmd, rest)
	default:
		fmt.Fprintf(r.out, "unknown command %s -- try :help\n", cmd)
	}
	return false
}

func (r *repl) parse(src string) (*ast.Program, bool) {
	tree, err := parser.ParseWith(src, extensions(r.opts.comments))
	if err != nil {
		fmt.Fprintf(r.out, "%v\n", err)
		return nil, false
	}
	return tree, true
}

func (r *repl) eval(src string) {
	tree, ok := r.parse(src)
	if !ok {
		return
	}
	prog, err := molang.CompileAST(tree)
	if err != nil {
		fmt.Fprintf(r.out, "%v\n", err)
		return
	}
	var unresolved []string
	ctx := &eval.Context{
		RNG:                      r.rng, // shared, so draws continue across lines
		Scope:                    r.scope,
		ContinueOnUnresolvedRead: r.opts.continueOn,
		OnUnresolvedRead:         func(n string) { unresolved = append(unresolved, n) },
	}
	fmt.Fprintln(r.out, formatValue(prog.Run(ctx)))
	for _, n := range dedupe(unresolved) {
		fmt.Fprintf(r.out, "  note: %s was never set", n)
		if !r.opts.continueOn {
			fmt.Fprint(r.out, ", which ended the program there (:continue on to evaluate past it)")
		}
		fmt.Fprintln(r.out)
	}
}

func (r *repl) stage(cmd, src string) {
	if src == "" {
		fmt.Fprintf(r.out, "%s needs an expression\n", cmd)
		return
	}
	tree, ok := r.parse(src)
	if !ok {
		return
	}
	switch cmd {
	case ":ast":
		_ = reportAST(tree, r.opts, r.out)
	case ":fmt":
		fmt.Fprintln(r.out, printer.Format(tree))
	case ":min":
		fmt.Fprintln(r.out, printer.Minify(tree))
	case ":fold":
		fmt.Fprintln(r.out, printer.Format(transform.FoldConstants(tree)))
	case ":expand":
		expanded, err := transform.NewRegistryWithDefaults().Expand(tree)
		if err != nil {
			fmt.Fprintf(r.out, "%v\n", err)
			return
		}
		fmt.Fprintln(r.out, printer.Format(expanded))
	case ":ops":
		_ = reportOps(tree, r.opts, r.out)
	case ":refs":
		_ = reportRefs(tree, r.opts, r.out)
	case ":check":
		if err := reportValidate(tree, r.opts, r.out); err != nil {
			fmt.Fprintf(r.out, "%v\n", err)
		}
	}
}

func (r *repl) showScope() {
	empty := len(r.scope.Variable) == 0 && len(r.scope.Temp) == 0 &&
		len(r.scope.Query) == 0 && len(r.scope.Array) == 0

	printBag(r.out, "variable", r.scope.Variable)
	printBag(r.out, "temp", r.scope.Temp)
	printBag(r.out, "query", r.scope.Query)

	if len(r.scope.Array) > 0 {
		names := make([]string, 0, len(r.scope.Array))
		for k := range r.scope.Array {
			names = append(names, k)
		}
		sort.Strings(names)
		for _, k := range names {
			parts := make([]string, len(r.scope.Array[k]))
			for i, v := range r.scope.Array[k] {
				parts[i] = formatValue(v)
			}
			fmt.Fprintf(r.out, "  array.%s = [%s]\n", k, strings.Join(parts, ", "))
		}
	}
	if empty {
		fmt.Fprintln(r.out, "  (nothing set -- every name would read as unresolved)")
	}
}

func (r *repl) set(rest string) {
	if rest == "" {
		fmt.Fprintln(r.out, ":set needs an assignment, e.g. :set v.x = 1")
		return
	}
	// Accept spaces around '=' for comfort; applyScope wants them trimmed.
	name, value, ok := strings.Cut(rest, "=")
	if !ok {
		fmt.Fprintln(r.out, ":set needs an assignment, e.g. :set v.x = 1")
		return
	}
	assignment := strings.TrimSpace(name) + "=" + strings.TrimSpace(value)
	if err := applyScope(r.scope, []string{assignment}); err != nil {
		fmt.Fprintf(r.out, "%v\n", err)
		return
	}
	fmt.Fprintln(r.out, "ok")
}

// unset removes a name, which is a different thing from setting it to zero:
// an absent key is an unresolved read and a present zero is a plain zero.
func (r *repl) unset(name string) {
	ns, member, ok := strings.Cut(strings.TrimSpace(name), ".")
	if !ok {
		fmt.Fprintln(r.out, ":unset needs a name, e.g. :unset v.x")
		return
	}
	member = strings.ToLower(member)
	switch strings.ToLower(ns) {
	case "v", "variable":
		delete(r.scope.Variable, member)
	case "t", "temp":
		delete(r.scope.Temp, member)
	case "q", "query":
		delete(r.scope.Query, member)
	case "a", "array":
		delete(r.scope.Array, member)
	default:
		fmt.Fprintf(r.out, "%q is not an assignable namespace\n", ns)
		return
	}
	fmt.Fprintln(r.out, "ok -- it now reads as unresolved, not as 0")
}

func (r *repl) seed(rest string) {
	var s uint
	if _, err := fmt.Sscanf(strings.TrimSpace(rest), "%d", &s); err != nil {
		fmt.Fprintln(r.out, ":seed needs a number")
		return
	}
	r.opts.seed = s
	r.rng = newRNG(s)
	fmt.Fprintf(r.out, "RNG reseeded to %d\n", s)
}

func (r *repl) setContinue(rest string) {
	switch strings.ToLower(strings.TrimSpace(rest)) {
	case "on", "true", "1":
		r.opts.continueOn = true
	case "off", "false", "0":
		r.opts.continueOn = false
	default:
		fmt.Fprintln(r.out, ":continue on   or   :continue off")
		return
	}
	fmt.Fprintf(r.out, "continue-on-unresolved-read: %v\n", r.opts.continueOn)
}
