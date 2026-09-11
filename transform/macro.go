package transform

import (
	"fmt"
	"strings"

	"github.com/stirante/molang-go/ast"
)

// Macro is a user-function that expands to core Molang at transform time,
// so evaluated/printed output never contains anything but constructs
// vanilla Bedrock (and this library's own evaluator) already understands.
//
// Macros are invoked with math.<Name>(...) call syntax (the only "callable"
// spelling real Molang has) — e.g. registering {Name: "bitshift", Arity: 2}
// makes math.bitshift(x, n) (or m.bitshift(x, n)) recognized.
type Macro struct {
	Name  string
	Arity int
	// Expand builds the replacement expression from the call's (already
	// parsed, not yet evaluated or expanded) argument expressions. It may
	// itself reference other macros — Registry.Expand keeps expanding
	// until no registered macro name remains, so macros can be defined in
	// terms of each other.
	Expand func(args []ast.Expr) ast.Expr
}

// Registry is a set of registered macros. The zero value via NewRegistry
// is empty; register whatever your project needs.
type Registry struct {
	macros map[string]Macro
}

// NewRegistry returns an empty macro registry.
func NewRegistry() *Registry {
	return &Registry{macros: map[string]Macro{}}
}

// Register adds (or replaces) a macro. Matching is case-insensitive, same
// as every other Molang identifier.
func (r *Registry) Register(m Macro) {
	r.macros[strings.ToLower(m.Name)] = m
}

// BitshiftMacro is the worked example: bitshift(x, n) is a right bit-shift,
// x >> n, expressed the only way Molang can (there is no bitwise operator)
// as floor(x / 2^n). Ship it registered by default via
// NewRegistryWithDefaults so "make registering new macros a public API"
// has a real example to point at, without forcing every caller to use it.
func BitshiftMacro() Macro {
	return Macro{
		Name:  "bitshift",
		Arity: 2,
		Expand: func(args []ast.Expr) ast.Expr {
			x, n := args[0], args[1]
			return &ast.CallExpr{
				Callee: &ast.Ident{Namespace: ast.Math, Member: "floor"},
				Args: []ast.Expr{
					&ast.BinaryExpr{
						Op: ast.Div,
						X:  x,
						Y: &ast.CallExpr{
							Callee: &ast.Ident{Namespace: ast.Math, Member: "pow"},
							Args:   []ast.Expr{&ast.NumberLit{Value: 2}, n},
						},
					},
				},
			}
		},
	}
}

// NewRegistryWithDefaults returns a Registry pre-populated with this
// library's built-in macros (currently just bitshift).
func NewRegistryWithDefaults() *Registry {
	r := NewRegistry()
	r.Register(BitshiftMacro())
	return r
}

// Expand rewrites every macro call in prog to its expansion, repeatedly
// until no registered macro name remains (so a macro may expand to another
// macro call). Returns an error if a call site's argument count doesn't
// match its macro's declared arity, or if expansion doesn't reach a fixed
// point within a generous budget (almost certainly a cyclic macro
// definition).
func (r *Registry) Expand(prog *ast.Program) (out *ast.Program, err error) {
	if len(r.macros) == 0 {
		return prog, nil
	}
	ex := &expander{reg: r, budget: 100000}
	defer func() {
		if rec := recover(); rec != nil {
			if e, ok := rec.(error); ok {
				err = e
				return
			}
			panic(rec)
		}
	}()
	for i, s := range prog.Stmts {
		prog.Stmts[i] = ex.stmt(s)
	}
	return prog, nil
}

type expander struct {
	reg    *Registry
	budget int
}

func (e *expander) consumeBudget() {
	e.budget--
	if e.budget < 0 {
		panic(fmt.Errorf("macro expansion did not terminate (possible cyclic macro definition)"))
	}
}

func (e *expander) stmt(s ast.Stmt) ast.Stmt {
	switch s := s.(type) {
	case *ast.ExprStmt:
		s.X = e.expr(s.X)
		return s
	case *ast.ReturnStmt:
		if s.Value != nil {
			s.Value = e.expr(s.Value)
		}
		return s
	case *ast.LoopStmt:
		s.Count = e.expr(s.Count)
		e.block(s.Body)
		return s
	case *ast.ForEachStmt:
		e.block(s.Body)
		return s
	case *ast.CondBlockStmt:
		return e.condBlock(s)
	}
	return s
}

func (e *expander) condBlock(cb *ast.CondBlockStmt) *ast.CondBlockStmt {
	cb.Cond = e.expr(cb.Cond)
	e.block(cb.Body)
	if cb.Else != nil {
		cb.Else = e.condBlock(cb.Else)
	}
	return cb
}

func (e *expander) block(b *ast.Block) {
	for i, s := range b.Stmts {
		b.Stmts[i] = e.stmt(s)
	}
}

func (e *expander) expr(x ast.Expr) ast.Expr {
	switch x := x.(type) {
	case *ast.NumberLit, *ast.BoolLit, *ast.StringLit, *ast.Ident:
		return x
	case *ast.UnaryExpr:
		x.X = e.expr(x.X)
		return x
	case *ast.BinaryExpr:
		x.X = e.expr(x.X)
		x.Y = e.expr(x.Y)
		return x
	case *ast.AssignExpr:
		x.Value = e.expr(x.Value)
		return x
	case *ast.TernaryExpr:
		x.Cond = e.expr(x.Cond)
		x.Then = e.expr(x.Then)
		if x.Else != nil {
			x.Else = e.expr(x.Else)
		}
		return x
	case *ast.CondBlockStmt:
		return e.condBlock(x)
	case *ast.ArrowExpr:
		x.Read = e.expr(x.Read)
		return x

	case *ast.ArrayAccess:
		x.Index = e.expr(x.Index)
		return x

	case *ast.CallExpr:
		for i, a := range x.Args {
			x.Args[i] = e.expr(a)
		}
		if x.Callee.Namespace == ast.Math {
			member := strings.ToLower(x.Callee.Member)
			if macro, ok := e.reg.macros[member]; ok {
				if len(x.Args) != macro.Arity {
					panic(fmt.Errorf("macro %s expects %d argument(s), got %d", x.Callee.Member, macro.Arity, len(x.Args)))
				}
				e.consumeBudget()
				replacement := macro.Expand(x.Args)
				return e.expr(replacement)
			}
		}
		return x
	}
	return x
}
