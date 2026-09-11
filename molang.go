// Package molang is a standalone, general-purpose Molang implementation:
// lexer, parser, AST, a closure-compiled evaluator, a Format/Minify
// printer, and AST transforms (constant folding, macro expansion).
//
// The AST (package molang-go/ast) is the shared currency: the evaluator
// (molang-go/eval), printer (molang-go/printer) and transforms
// (molang-go/transform) all operate on the same tree, so the same parse can
// be evaluated, reformatted, minified, or rewritten without re-parsing.
//
// This file is a thin convenience layer over those packages for the common
// "parse once, run many times" case. Nothing here is required — every
// subpackage is independently usable.
package molang

import (
	"github.com/stirante/molang-go/ast"
	"github.com/stirante/molang-go/eval"
	"github.com/stirante/molang-go/parser"
)

// RNG is re-exported from eval for convenience; see eval.RNG's doc comment
// for why it is always caller-injected.
type RNG = eval.RNG

// Scope is re-exported from eval; see eval.Scope's doc comment.
type Scope = eval.Scope

// Context is re-exported from eval; see eval.Context's doc comment.
type Context = eval.Context

// QueryFunc is re-exported from eval; see eval.QueryFunc's doc comment.
type QueryFunc = eval.QueryFunc

// Entity is re-exported from eval: what a host supplies for another entity an
// expression reaches with `->`. See eval.Entity's doc comment.
type Entity = eval.Entity

// NewScope returns an empty, ready-to-use Scope.
func NewScope() *Scope { return eval.NewScope() }

// Program is a parsed-and-compiled Molang expression, ready to Run
// repeatedly.
type Program struct {
	AST      *ast.Program
	compiled *eval.Program
}

// Parse parses source into an AST without compiling it — useful when all
// you want is to Format/Minify/transform, not evaluate.
func Parse(source string) (*ast.Program, error) {
	return parser.Parse(source)
}

// Extensions is re-exported from parser; see lexer.Extensions for what each
// one costs.
type Extensions = parser.Extensions

// ParseWith is Parse, accepting the named language extensions as well.
func ParseWith(source string, ext Extensions) (*ast.Program, error) {
	return parser.ParseWith(source, ext)
}

// CompileWith is Compile, accepting the named language extensions as well.
func CompileWith(source string, ext Extensions) (*Program, error) {
	tree, err := parser.ParseWith(source, ext)
	if err != nil {
		return nil, err
	}
	return CompileAST(tree)
}

// Compile parses and compiles source in one step, ready for repeated Run
// calls.
func Compile(source string) (*Program, error) {
	tree, err := parser.Parse(source)
	if err != nil {
		return nil, err
	}
	c, err := eval.Compile(tree)
	if err != nil {
		return nil, err
	}
	return &Program{AST: tree, compiled: c}, nil
}

// CompileAST compiles an already-parsed (and possibly transformed) AST.
func CompileAST(tree *ast.Program) (*Program, error) {
	c, err := eval.Compile(tree)
	if err != nil {
		return nil, err
	}
	return &Program{AST: tree, compiled: c}, nil
}

// Run evaluates the compiled program against ctx. See eval.Program.Run for
// the two ways a run can end early -- an explicit `return`, and an
// unresolved temp./variable./context. read with no enclosing `??` (which
// ctx can opt out of; see eval/unresolved.go).
func (p *Program) Run(ctx *Context) float64 {
	return p.compiled.Run(ctx)
}

// Eval is a one-shot convenience: parse, compile, and run source once.
// Prefer Compile+Run when evaluating the same expression repeatedly —
// parsing is meant to happen once, not on every call.
func Eval(source string, ctx *Context) (float64, error) {
	p, err := Compile(source)
	if err != nil {
		return 0, err
	}
	return p.Run(ctx), nil
}
