// Package ast defines the Molang abstract syntax tree.
//
// The AST is the shared currency between the evaluator, the printer
// (Format/Minify) and source-to-source transforms (constant folding,
// macro expansion). Nodes are plain data — no behavior is attached here;
// packages eval/printer/transform each walk the tree for their own purpose.
package ast

// Namespace identifies which of Molang's four dotted namespaces an
// Identifier belongs to.
type Namespace uint8

const (
	Math Namespace = iota
	Query
	Variable
	Temp
	// Array is the namespace of host-supplied arrays. Unlike every other
	// namespace it is never read bare: `array.foo` alone is not an
	// expression, only `array.foo[index]` is.
	Array
	// Geometry, Material and Texture name RESOURCES rather than values. A
	// resource is a name: two can be compared and a ternary can select
	// between them, but arithmetic on one is a parse error. Nothing
	// distinguishes the three beyond the spelling.
	Geometry
	Material
	Texture
	// Context is a genuine namespace (context.block_face and friends,
	// confirmed against the game) rather than an alias for anything.
	// Nothing in worldgen populates it, so member lookups always resolve
	// as unknown (0, recorded as unresolved) — but the syntax is legal
	// Molang and must parse, not error.
	Context
)

func (n Namespace) String() string {
	switch n {
	case Math:
		return "math"
	case Query:
		return "query"
	case Variable:
		return "variable"
	case Temp:
		return "temp"
	case Context:
		return "context"
	case Array:
		return "array"
	case Geometry:
		return "geometry"
	case Material:
		return "material"
	case Texture:
		return "texture"
	}
	return "?"
}

// ShortAlias returns the spelling to use when emitting this namespace as
// short as the ENGINE will actually accept — which is not the same as "the
// first letter", and the difference is a real bug this used to have.
//
// CONFIRMED: the engine's Molang token reader contains a hand-written
// alias chain of EXACTLY FOUR two-character prefixes — "v.", "q.", "c."
// and "t.". There is no "m." anywhere in it, and no "m." token in the
// engine's operator/token metadata table: every math.* op carries its full
// canonical token. `m.sin(1)` is not shorthand in Bedrock, it is
// `"Error: unknown token: %s"`.
//
// So Math returns "math", not "m". printer.Minify emits this string, and
// while it emitted "m." every minified expression this module produced was
// source the engine refuses to load.
//
// The four real aliases are unaffected: q./v./t./c. are exactly equivalent
// to their long spellings and always safe to emit.
func (n Namespace) ShortAlias() string {
	switch n {
	case Math:
		return "math"
	case Query:
		return "q"
	case Variable:
		return "v"
	case Temp:
		return "t"
	case Context:
		return "c"
	case Array:
		// No short form is emitted for arrays. `a.` is accepted on input
		// because the tokenizer takes it, but a one-letter namespace that
		// collides with nothing else gains little and reads badly.
		return "array"
	case Geometry:
		return "geometry"
	case Material:
		return "material"
	case Texture:
		return "texture"
	}
	return ""
}

// NamespaceAliases maps every lower-cased spelling this module's LEXER
// accepts to its Namespace. It is exactly the set the engine accepts: the
// four long names, and the four two-character prefixes v./q./t./c.
//
// "m" USED TO BE HERE and was removed deliberately. It is not engine-legal
// — see ShortAlias — and accepting it was justified as a one-way leniency,
// on the grounds that this module is not a load-time validator and that
// older tooling had emitted it. That reasoning does not survive contact
// with what the module is FOR: an expression written `m.floor(1.5)` would
// evaluate here and then fail to load in game with "unknown token", which
// is the single worst outcome a tool like this can produce. A spelling the
// game rejects is better rejected here, loudly, at the point the author can
// still fix it.
//
// (Exponent literals, in the same family of "is this really legal?"
// questions, are CONFIRMED legal Molang and correctly accepted:
// the engine's positive-only float parser carries an explicit "expected
// '+' or '-' after 'e'" error path, which only exists because the digits
// before it were an accepted exponent.)
var NamespaceAliases = map[string]Namespace{
	"math":     Math,
	"query":    Query,
	"q":        Query,
	"variable": Variable,
	"v":        Variable,
	"temp":     Temp,
	"t":        Temp,
	"context":  Context,
	"c":        Context,
	"array":    Array,
	"a":        Array,
	"geometry": Geometry,
	"material": Material,
	"texture":  Texture,
}

// IsResource reports whether n names a resource rather than a value. A
// resource may be compared and selected between; it may not be an operand of
// an arithmetic or comparison-with-a-number expression.
func (n Namespace) IsResource() bool {
	return n == Geometry || n == Material || n == Texture
}

// ThisExpr is the bare keyword `this`.
//
// It is a single number the host supplies for the run, not a namespace and not
// an object: there is nothing to read off it and nothing to assign to it. This
// package carries the value and does not interpret it, because what it means
// depends entirely on who is evaluating.
//
// What the game puts there, since it is worth knowing and is not what most
// people assume:
//
//   - In an ANIMATION, `this` is the current value of the channel component
//     being evaluated -- the running pose, per component, so the x expression
//     sees x, the y expression sees y. It is re-set between components.
//   - In a RENDER CONTROLLER, the current value of the colour or UV component
//     being computed, the same way.
//   - In a PARTICLE emitter shape, the constant 0.
//
// And the part that catches people: an animation channel's result is ADDED to
// the running pose (scaled by the layer's blend weight), not substituted for
// it. So `"rotation": ["this + 5", 0, 0]` at full weight gives
// current + (current + 5), which is 2*current + 5 -- not current + 5. Writing
// plain `5` there is what gives current + 5. Scale blends multiplicatively
// rather than additively, so it has its own formula again.
//
// None of that is this package's business to reproduce -- it belongs to
// whatever host is animating -- but a host wiring Context.This up should know
// which value the game would have put there.
type ThisExpr struct{}

func (*ThisExpr) node()     {}
func (*ThisExpr) exprNode() {}

// ArrowExpr is `context.<entity>-><read>` -- a variable or query read
// evaluated against another entity rather than the current one.
//
// Entity is always a context. name. Read is a variable. read or a query.
// read or call. Neither a chain (`a->b->c`) nor a non-context left side is
// legal, and both are refused at parse time.
//
// A missing entity reads 0. That is NOT an unresolved read: it does not end
// the expression, which is what lets `v.x = c.nobody->v.y;` still assign.
type ArrowExpr struct {
	Entity *Ident
	Read   Expr
}

func (*ArrowExpr) node()     {}
func (*ArrowExpr) exprNode() {}

// ArrayAccess is `array.<name>[<index>]`.
//
// The index rule is the part worth knowing, because it is not the one any
// other language would give you: the index is truncated toward zero, a
// negative index reads element 0, and a positive one wraps modulo the array's
// length. An array read is therefore total -- it never fails, and never goes
// out of range, whatever the expression computes.
//
// Arrays are supplied by the host (eval.Scope.Array), not declared in Molang.
type ArrayAccess struct {
	Name  string
	Index Expr
}

func (*ArrayAccess) node()     {}
func (*ArrayAccess) exprNode() {}

// Node is implemented by every AST node.
type Node interface {
	node()
}

// Expr is any node usable as an expression (produces a numeric value).
type Expr interface {
	Node
	exprNode()
}

// Stmt is any node usable as a statement inside a Block/Program.
type Stmt interface {
	Node
	stmtNode()
}

// ---------------------------------------------------------------------
// Expressions
// ---------------------------------------------------------------------

// NumberLit is a numeric literal, e.g. 3, 3.14.
type NumberLit struct {
	Value float64
}

// BoolLit is a `true`/`false` literal. Evaluates to 1/0 but is tracked
// separately so the printer can round-trip the keyword spelling in Format
// (Minify still prefers the shorter "1"/"0").
type BoolLit struct {
	Value bool
}

// StringLit is a 'quoted' string literal. Molang has no string type; the
// evaluator interns each distinct string to a stable numeric id (see the
// eval package), matching real packs that stash strings in temp/variable
// slots purely for later `==`/`!=` comparison.
type StringLit struct {
	Value string
}

// Ident is a dotted namespace reference: math.sin, query.foo, t.x, v.y.
// Namespace has already been normalized (q/v/t expanded); Member preserves
// original source casing (identifiers are compared case-insensitively at
// eval/analysis time, but original casing is kept for Format).
type Ident struct {
	Namespace Namespace
	Member    string
}

// UnaryOp enumerates prefix unary operators.
type UnaryOp uint8

const (
	Neg  UnaryOp = iota // -x
	LNot                // !x
)

type UnaryExpr struct {
	Op UnaryOp
	X  Expr
}

// BinaryOp enumerates infix binary operators. There is deliberately no
// modulo operator — real Molang only exposes it via math.mod.
type BinaryOp uint8

const (
	Add BinaryOp = iota
	Sub
	Mul
	Div
	CmpLt
	CmpLe
	CmpGt
	CmpGe
	CmpEq
	CmpNe
	LAnd
	LOr
	NullCoalesce
)

type BinaryExpr struct {
	Op   BinaryOp
	X, Y Expr
}

// AssignExpr assigns Value to Target (temp.* or variable.* only) and
// evaluates to the assigned numeric value.
type AssignExpr struct {
	Target *Ident
	Value  Expr
}

// CallExpr is a namespaced function call: math.sin(x), query.foo(1, 2).
type CallExpr struct {
	Callee *Ident
	Args   []Expr
}

// TernaryExpr is `cond ? then : else` or the binary-conditional shorthand
// `cond ? then` (Else == nil), which yields 0 when Cond is falsy.
type TernaryExpr struct {
	Cond, Then, Else Expr
}

// ---------------------------------------------------------------------
// Statements
// ---------------------------------------------------------------------

// ExprStmt is an expression evaluated for its value/side effects.
type ExprStmt struct {
	X Expr
}

// ReturnStmt immediately yields Value as the enclosing Program's result.
type ReturnStmt struct {
	Value Expr
}

// BreakStmt exits the nearest enclosing loop.
type BreakStmt struct{}

// ContinueStmt skips to the next iteration of the nearest enclosing loop.
type ContinueStmt struct{}

// Block is a brace-delimited `{ statements }` body, used by loop bodies and
// the statement-conditional form. A Block always follows "0 unless returned"
// semantics, regardless of statement count.
type Block struct {
	Stmts []Stmt
}

// CondBlockStmt is the statement-conditional shorthand real packs use
// heavily: `cond ? { ...statements };`. If Cond is falsy, Body does not run
// (and, absent an Else, the statement contributes 0).
//
// Else, if present, is always another CondBlockStmt: a plain `: { ... }`
// else-clause is represented as an Else with Cond set to a BoolLit(true), so
// `cond ? {a} : {b};` and `cond1 ? {a} : cond2 ? {b} : {c};` (else-if
// chains) share one representation.
//
// CondBlockStmt implements Expr as well as Stmt purely so the parser can
// return it from the same expression-parsing code path that produces a
// TernaryExpr (both start with `cond ?`); evaluating it as a bare
// expression uses the same "0 unless the block returns" rule as everywhere
// else a Block appears.
type CondBlockStmt struct {
	Cond Expr
	Body *Block
	Else *CondBlockStmt
}

// LoopStmt is `loop(count, { body })`.
type LoopStmt struct {
	Count Expr
	Body  *Block
}

// ForEachStmt is `for_each(<variable>, <array>, { body })`.
//
// Var is the name the current element is written to on each pass -- a
// variable. or temp. name, assigned before the body runs and left holding the
// last element afterwards.
//
// Array names the host array to walk. Molang also allows a variable holding
// an entity array here; this package has no entities and no array-valued
// variables, so `array.<name>` is the only source it accepts. That is a
// SUBSET, not a difference in behaviour: what it does iterate, it iterates
// the same way.
type ForEachStmt struct {
	Var   *Ident
	Array string
	Body  *Block
}

// Program is the parsed result of a whole Molang source string.
//
// HasSemicolon records whether the source used `;` anywhere at the top
// level. When false, the source parsed as a single bare expression and its
// value is the program's value directly (e.g. "3 + math.random(0,1) * 4").
// When true, the program follows Block's normal "0 unless returned"
// sequence semantics, even if it happens to contain only one statement
// followed by a trailing `;`.
//
// That last clause is the whole reason this field exists, and it is
// CONFIRMED against the game itself (1.26.50.24): the engine's Molang
// program builder compiles the `;` node by emitting its children and then
// UNCONDITIONALLY pushing a constant carrying the node's folded `add`
// value, normally 0 — so the last statement's value is
// computed and then overwritten, for a one-statement sequence exactly as
// for a longer one. See eval.Compile for the full note. (This comment used
// to say INFERRED, reasoning from the documented complex-expression rule;
// the reasoning turned out to be right.)
//
// The field is READ, by eval.Compile — it is the difference between
// `temp.a=5;` evaluating to 0 (assignment still performed) and to 5. It is
// also honored by printer.Format and printer.Minify, which must keep a
// single-statement program's trailing `;` rather than trimming it, since
// trimming it changes what the program means.
type Program struct {
	Stmts        []Stmt
	HasSemicolon bool
}

func (*NumberLit) node()     {}
func (*BoolLit) node()       {}
func (*StringLit) node()     {}
func (*Ident) node()         {}
func (*UnaryExpr) node()     {}
func (*BinaryExpr) node()    {}
func (*AssignExpr) node()    {}
func (*CallExpr) node()      {}
func (*TernaryExpr) node()   {}
func (*ExprStmt) node()      {}
func (*ReturnStmt) node()    {}
func (*BreakStmt) node()     {}
func (*ContinueStmt) node()  {}
func (*Block) node()         {}
func (*CondBlockStmt) node() {}
func (*LoopStmt) node()      {}
func (*ForEachStmt) node()   {}
func (*Program) node()       {}

func (*NumberLit) exprNode()   {}
func (*BoolLit) exprNode()     {}
func (*StringLit) exprNode()   {}
func (*Ident) exprNode()       {}
func (*UnaryExpr) exprNode()   {}
func (*BinaryExpr) exprNode()  {}
func (*AssignExpr) exprNode()  {}
func (*CallExpr) exprNode()    {}
func (*TernaryExpr) exprNode() {}

func (*ExprStmt) stmtNode()      {}
func (*ReturnStmt) stmtNode()    {}
func (*BreakStmt) stmtNode()     {}
func (*ContinueStmt) stmtNode()  {}
func (*CondBlockStmt) stmtNode() {}
func (*ForEachStmt) stmtNode()   {}
func (*LoopStmt) stmtNode()      {}

// CondBlockStmt also satisfies Expr — see its doc comment.
func (*CondBlockStmt) exprNode() {}

// Walk calls visit on node and every descendant, depth-first, pre-order.
// visit returning false stops descent into that node's children (but
// siblings are still visited).
func Walk(n Node, visit func(Node) bool) {
	if n == nil || !visit(n) {
		return
	}
	switch v := n.(type) {
	case *UnaryExpr:
		Walk(v.X, visit)
	case *BinaryExpr:
		Walk(v.X, visit)
		Walk(v.Y, visit)
	case *AssignExpr:
		Walk(v.Target, visit)
		Walk(v.Value, visit)
	case *CallExpr:
		Walk(v.Callee, visit)
		for _, a := range v.Args {
			Walk(a, visit)
		}
	case *TernaryExpr:
		Walk(v.Cond, visit)
		Walk(v.Then, visit)
		if v.Else != nil {
			Walk(v.Else, visit)
		}
	case *ExprStmt:
		Walk(v.X, visit)
	case *ReturnStmt:
		if v.Value != nil {
			Walk(v.Value, visit)
		}
	case *Block:
		for _, s := range v.Stmts {
			Walk(s, visit)
		}
	case *CondBlockStmt:
		Walk(v.Cond, visit)
		Walk(v.Body, visit)
		if v.Else != nil {
			Walk(v.Else, visit)
		}
	case *LoopStmt:
		Walk(v.Count, visit)
		Walk(v.Body, visit)
	case *Program:
		for _, s := range v.Stmts {
			Walk(s, visit)
		}
	}
}
