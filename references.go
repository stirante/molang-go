package molang

import (
	"sort"
	"strings"

	"github.com/stirante/molang-go/ast"
	"github.com/stirante/molang-go/eval"
)

// Refs is everything a parsed program names.
//
// This is what a tool needs before it can do anything useful with an
// expression it did not write. An editor asking "which values do I have to
// prompt for" needs the reads; a pack-wide rewriter needs the writes as well,
// to know what it may not rename; something plotting an expression over a time
// query needs to know which query drives it, and whether a random draw makes
// the plot meaningless.
//
// Every list is sorted and free of duplicates, so two Refs over equivalent
// programs compare equal and a diff between them is readable.
//
// Names are as written, lower-cased, with the namespace stripped: a read of
// `variable.st.height` appears in VariableReads as "st.height". That matches
// how the evaluator keys its scopes, so a caller can use these directly
// against Scope.Variable and friends.
type Refs struct {
	// Reads and writes of the three assignable-or-readable namespaces.
	// A name can appear in both: `v.n = v.n + 1` reads and writes "n".
	VariableReads  []string
	VariableWrites []string
	TempReads      []string
	TempWrites     []string

	// ContextReads is context.* -- readable, never assignable.
	ContextReads []string

	// Queries is every query.<name> read or called, including through `->`.
	Queries []string

	// MathFuncs is every math.<name> called. math.pi is not a call and does
	// not appear.
	MathFuncs []string

	// Arrays is every array.<name> indexed or walked by for_each.
	Arrays []string

	// Entities is every context.<name> used on the left of `->`. Such a name
	// is NOT in ContextReads: it is being dereferenced, not read.
	Entities []string

	// Resources is every geometry./material./texture. name, with its
	// namespace kept, since the three do not share a space: "texture.foo".
	Resources []string

	// UsesThis reports whether the bare keyword `this` appears.
	UsesThis bool

	// UsesRandom reports whether any math function that consumes a random
	// draw is called. An expression where this is true has no single value
	// to plot or cache, however many other things are held fixed.
	UsesRandom bool
}

// References reports what prog names. It walks the tree and nothing else: no
// evaluation, no host, no scope required.
func References(prog *ast.Program) Refs {
	c := &refCollector{seen: map[string]map[string]bool{}}
	for _, s := range prog.Stmts {
		c.stmt(s)
	}
	return c.result()
}

// NeedsFromHost is the names a caller must supply before the program can run:
// everything read that the program never writes for itself, as
// "<namespace>.<member>".
//
// It is a LOWER BOUND, and the two ways it under-reports are worth knowing
// before building a prompt on it. A name written only inside a conditional is
// treated as written, though the branch may not run. And a name read before
// the statement that writes it still needs a value on that first read. Both
// would need dataflow to see, which this deliberately does not attempt --
// a wrong answer that looks precise would be worse than an honest floor.
//
// For an editor prompting a user, the safe move is to offer these first and
// the full read lists as the complete set.
func (r Refs) NeedsFromHost() []string {
	var out []string
	out = append(out, missing("variable", r.VariableReads, r.VariableWrites)...)
	out = append(out, missing("temp", r.TempReads, r.TempWrites)...)
	for _, n := range r.ContextReads {
		out = append(out, "context."+n)
	}
	sort.Strings(out)
	return out
}

func missing(ns string, reads, writes []string) []string {
	written := make(map[string]bool, len(writes))
	for _, w := range writes {
		written[w] = true
	}
	var out []string
	for _, rd := range reads {
		if !written[rd] {
			out = append(out, ns+"."+rd)
		}
	}
	return out
}

// ---------------------------------------------------------------------

type refCollector struct {
	seen       map[string]map[string]bool
	usesThis   bool
	usesRandom bool
}

func (c *refCollector) mark(bucket, name string) {
	m := c.seen[bucket]
	if m == nil {
		m = map[string]bool{}
		c.seen[bucket] = m
	}
	m[strings.ToLower(name)] = true
}

func (c *refCollector) list(bucket string) []string {
	m := c.seen[bucket]
	if len(m) == 0 {
		return nil
	}
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func (c *refCollector) result() Refs {
	return Refs{
		VariableReads:  c.list("vr"),
		VariableWrites: c.list("vw"),
		TempReads:      c.list("tr"),
		TempWrites:     c.list("tw"),
		ContextReads:   c.list("cr"),
		Queries:        c.list("q"),
		MathFuncs:      c.list("m"),
		Arrays:         c.list("a"),
		Entities:       c.list("e"),
		Resources:      c.list("res"),
		UsesThis:       c.usesThis,
		UsesRandom:     c.usesRandom,
	}
}

// read records a bare identifier being read.
func (c *refCollector) read(id *ast.Ident) {
	switch id.Namespace {
	case ast.Variable:
		c.mark("vr", id.Member)
	case ast.Temp:
		c.mark("tr", id.Member)
	case ast.Context:
		c.mark("cr", id.Member)
	case ast.Query:
		c.mark("q", id.Member)
	case ast.Geometry, ast.Material, ast.Texture:
		c.mark("res", id.Namespace.String()+"."+id.Member)
	case ast.Math:
		// math.pi is the only bare math read; it calls nothing and needs
		// nothing, so there is nothing to record.
	}
}

func (c *refCollector) stmt(s ast.Stmt) {
	switch s := s.(type) {
	case *ast.ExprStmt:
		c.expr(s.X)
	case *ast.ReturnStmt:
		if s.Value != nil {
			c.expr(s.Value)
		}
	case *ast.BreakStmt, *ast.ContinueStmt:
	case *ast.CondBlockStmt:
		c.condBlock(s)
	case *ast.LoopStmt:
		c.expr(s.Count)
		c.block(s.Body)
	case *ast.ForEachStmt:
		// The loop variable is WRITTEN on every pass, and the array is read.
		c.mark("a", s.Array)
		switch s.Var.Namespace {
		case ast.Variable:
			c.mark("vw", s.Var.Member)
		case ast.Temp:
			c.mark("tw", s.Var.Member)
		}
		c.block(s.Body)
	}
}

func (c *refCollector) condBlock(cb *ast.CondBlockStmt) {
	c.expr(cb.Cond)
	c.block(cb.Body)
	if cb.Else != nil {
		c.condBlock(cb.Else)
	}
}

func (c *refCollector) block(b *ast.Block) {
	if b == nil {
		return
	}
	for _, s := range b.Stmts {
		c.stmt(s)
	}
}

func (c *refCollector) expr(e ast.Expr) {
	switch e := e.(type) {
	case nil, *ast.NumberLit, *ast.BoolLit, *ast.StringLit:
	case *ast.ThisExpr:
		c.usesThis = true
	case *ast.Ident:
		c.read(e)
	case *ast.UnaryExpr:
		c.expr(e.X)
	case *ast.BinaryExpr:
		c.expr(e.X)
		c.expr(e.Y)
	case *ast.TernaryExpr:
		c.expr(e.Cond)
		c.expr(e.Then)
		c.expr(e.Else)
	case *ast.AssignExpr:
		switch e.Target.Namespace {
		case ast.Variable:
			c.mark("vw", e.Target.Member)
		case ast.Temp:
			c.mark("tw", e.Target.Member)
		}
		c.expr(e.Value)
	case *ast.CallExpr:
		switch e.Callee.Namespace {
		case ast.Math:
			name := strings.ToLower(e.Callee.Member)
			c.mark("m", name)
			if eval.RandomFnNames[name] {
				c.usesRandom = true
			}
		default:
			c.read(e.Callee)
		}
		for _, a := range e.Args {
			c.expr(a)
		}
	case *ast.ArrayAccess:
		c.mark("a", e.Name)
		c.expr(e.Index)
	case *ast.ArrowExpr:
		// The left side names an ENTITY being dereferenced, which is not the
		// same as reading a context member, so it goes in its own list.
		c.mark("e", e.Entity.Member)
		c.expr(e.Read)
	case *ast.CondBlockStmt:
		c.condBlock(e)
	}
}
