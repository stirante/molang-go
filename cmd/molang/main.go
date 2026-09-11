// Command molang is a command-line front end to this library.
//
// Stages chain by naming them together, and a chain is CHEAP: the source is
// parsed once and the tree is handed from stage to stage, never printed and
// re-parsed in between.
//
//	molang expand,fold,minify < in.molang
//
// That is also the only way to end a chain in something that is not Molang
// source -- `molang expand,fold,eval` folds first, then evaluates the tree.
//
// Every stage also reads stdin and writes stdout on its own, so stages do
// compose with a shell pipe:
//
//	molang expand < in.molang | molang fold | molang minify
//
// but that form pays a print and a re-parse at every boundary, so prefer the
// comma form unless the stages really need to be separate processes.
package main

import (
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"

	"github.com/stirante/molang-go/eval"
	"github.com/stirante/molang-go/mtrand"
	"github.com/stirante/molang-go/parser"
)

const usage = `molang -- a command-line front end for Molang

usage: molang <stage>[,<stage>...] [flags] [expression]
       molang repl [flags]

An expression may be given as the last argument. With none, or with "-",
the source is read from stdin.

Naming several stages runs them as a chain over ONE parse:

  molang expand,fold,minify        rewrite, fold, then print it short
  molang fold,eval                 fold first, then evaluate the tree

stages that hand on a tree (any position):
  fmt         reformat readably
  minify      shortest output that still parses
  fold        evaluate every constant subexpression
  expand      rewrite macro calls into core Molang

stages that end a chain:
  eval        evaluate and print the value
  validate    parse and check; exit 1 if it would not load
  ast         print the syntax tree
  refs        list what the program names
  ops         list the operations the program uses

other:
  repl        interactive session, keeping one scope and one RNG
  pipe        the chain spelled with the list as a separate word
  help        this text

Run "molang <command> -h" for a command's own flags.
`

func main() {
	if len(os.Args) < 2 {
		fmt.Fprint(os.Stderr, usage)
		os.Exit(2)
	}
	cmd := os.Args[1]
	args := os.Args[2:]

	var err error
	switch cmd {
	case "help", "-h", "--help":
		fmt.Print(usage)
		return
	case "repl":
		err = runREPL(args)
	case "pipe":
		// The same thing spelled with the stage list as the next word.
		if len(args) == 0 {
			fmt.Fprintln(os.Stderr, "molang: pipe needs a stage list, e.g. molang pipe expand,fold,minify")
			os.Exit(2)
		}
		err = runStages(args[0], args[1:])
	default:
		// One stage, or a comma-separated chain of them. Both go through the
		// same path, which parses once and hands the tree along.
		if !isStageSpec(cmd) {
			fmt.Fprintf(os.Stderr, "molang: unknown command %q\n\n%s", cmd, usage)
			os.Exit(2)
		}
		err = runStages(cmd, args)
	}

	if err != nil {
		fmt.Fprintf(os.Stderr, "molang: %v\n", err)
		os.Exit(1)
	}
}

// ---------------------------------------------------------------------
// shared input handling
// ---------------------------------------------------------------------

// readSource returns the program text: the last non-flag argument, or stdin
// when there is none or it is "-". Trailing newlines from a here-doc or a
// previous stage are not part of the expression.
func readSource(rest []string) (string, error) {
	if len(rest) > 0 && rest[len(rest)-1] != "-" {
		return strings.Join(rest, " "), nil
	}
	b, err := io.ReadAll(os.Stdin)
	if err != nil {
		return "", fmt.Errorf("reading stdin: %w", err)
	}
	return strings.TrimRight(string(b), "\r\n"), nil
}

// scopeFlag collects repeated -scope assignments.
type scopeFlag []string

func (s *scopeFlag) String() string { return strings.Join(*s, ",") }
func (s *scopeFlag) Set(v string) error {
	*s = append(*s, v)
	return nil
}

// applyScope fills sc from "namespace.member=value" strings. Arrays take a
// comma-separated list: -scope array.heights=1,2,3
func applyScope(sc *eval.Scope, assignments []string) error {
	for _, a := range assignments {
		name, value, ok := strings.Cut(a, "=")
		if !ok {
			return fmt.Errorf("-scope %q: expected namespace.member=value", a)
		}
		ns, member, ok := strings.Cut(strings.TrimSpace(name), ".")
		if !ok {
			return fmt.Errorf("-scope %q: name needs a namespace, e.g. v.x=1", a)
		}
		member = strings.ToLower(member)
		value = strings.TrimSpace(value)

		bag, isArray, err := scopeBag(sc, strings.ToLower(ns))
		if err != nil {
			return fmt.Errorf("-scope %q: %w", a, err)
		}
		if isArray {
			var nums []float64
			for _, part := range strings.Split(value, ",") {
				f, err := strconv.ParseFloat(strings.TrimSpace(part), 64)
				if err != nil {
					return fmt.Errorf("-scope %q: %q is not a number", a, part)
				}
				nums = append(nums, f)
			}
			sc.Array[member] = nums
			continue
		}
		f, err := strconv.ParseFloat(value, 64)
		if err != nil {
			return fmt.Errorf("-scope %q: %q is not a number", a, value)
		}
		bag[member] = f
	}
	return nil
}

func scopeBag(sc *eval.Scope, ns string) (map[string]float64, bool, error) {
	switch ns {
	case "v", "variable":
		return sc.Variable, false, nil
	case "t", "temp":
		return sc.Temp, false, nil
	case "q", "query":
		return sc.Query, false, nil
	case "a", "array":
		return nil, true, nil
	default:
		return nil, false, fmt.Errorf("namespace %q is not assignable (use variable., temp., query. or array.)", ns)
	}
}

// newRNG returns the reproducible RNG the evaluator draws from. Seeding it
// explicitly is what makes `eval` repeatable, which matters because the
// number and order of draws is part of what this library reproduces.
func newRNG(seed uint) eval.RNG { return mtrand.New(uint32(seed)) }

func extensions(comments bool) parser.Extensions {
	return parser.Extensions{Comments: comments}
}

// formatValue prints an evaluated result the way a person reads it: whole
// numbers without a decimal point, everything else at full float32-visible
// precision.
func formatValue(v float64) string {
	if v == float64(int64(v)) && v > -1e15 && v < 1e15 {
		return strconv.FormatInt(int64(v), 10)
	}
	return strconv.FormatFloat(v, 'g', -1, 64)
}
