package ast

import "math"

// This file models the one size limit the game puts on an expression: how
// deeply its tree may nest. The game builds a tree from the source, then
// walks it top-down keeping a depth counter, and refuses the whole
// expression the moment a node at depth DepthLimit is reached (the first
// node is depth 0). Nothing else about an expression is bounded -- not its
// length, its token count, its statement count, a call's argument count or
// a program's instruction count -- so this is the only "too complex"
// refusal an author can meet, and it is a hard refusal: the expression does
// not load, and the content log carries DepthOverflowMessage.
//
// The tree the game counts is NOT this package's AST. It is a coarser tree
// built from the token stream by a fixed sequence of passes, and three
// things about it matter here:
//
//   - Parentheses are nodes. `(x)` is a node holding x, and it is still
//     there when the depth is checked (it is folded away afterwards), so
//     every pair of parentheses in the source is one level. So are the
//     braces of a block, and the block's `;` list is a second level under
//     them.
//   - Argument lists are flat for queries and for math functions of two or
//     three arguments, but a one-argument math function keeps its
//     parenthesis as a node, so its argument sits two levels down.
//   - `a - b` is `a + -b`: one more level for the subtracted operand. The
//     parser groups every other operator as the game does (see the
//     parser's grammar comment), so the shape of a chain needs no
//     correction here. A conditional's arms and a `:` else are lifted to be
//     direct children of the `?`.
//
// Depth walks this package's tree and computes the depth of the game's
// tree for it. Parentheses and bare blocks are not kept in the AST, so the
// parser records them in a Grouping as it goes; a tree that never went
// through the parser (built by hand, or produced by a transform) is
// measured as the printer would write it, which puts parentheses exactly
// where the grammar needs them.

// DepthLimit is the tree depth the game refuses: a node at this depth (the
// expression's first node being depth 0) fails the whole expression.
const DepthLimit = 256

// DepthOverflowMessage is the game's wording for an expression refused by
// DepthLimit.
const DepthOverflowMessage = "Expression could not be parsed due to stack depth overflow (too many sub-expressions)"

// Grouping is what the parser remembers about how a source grouped things
// that the tree does not keep, so Depth can count them.
type Grouping struct {
	// Parens is how many pairs of parentheses were written directly around
	// each expression: 2 for the x of `((x))`.
	Parens map[Expr]int
	// PlainBlocks holds every CondBlockStmt that stands for a bare `{ ... }`
	// (see CondBlockStmt): a block statement, a plain `: { ... }` else, a
	// block arm of a ternary. Those are one brace node in the game's tree;
	// a CondBlockStmt not in the set is a conditional holding a brace node.
	PlainBlocks map[*CondBlockStmt]bool
}

// Depth returns the depth of the game's tree for prog: the largest depth of
// any node, the first node being depth 0. It is what DepthLimit is compared
// against. With g nil, parentheses are counted where the grammar requires
// them (which is where the printer writes them) and a CondBlockStmt whose
// condition is the literal true is taken as a bare block.
//
// The walk keeps its own stack, so a tree far deeper than DepthLimit is
// measured without deep recursion.
func Depth(prog *Program, g *Grouping) int {
	w := &depthWalker{g: g}
	switch {
	case prog.HasSemicolon || len(prog.Stmts) != 1:
		// A statement list is a `;` node holding the statements.
		w.note(0)
		for _, s := range prog.Stmts {
			w.push(s, 1, precNone)
		}
	case g == nil && needsSemicolon(prog):
		// Written, a lone assignment gains a `return ... ;` around it to
		// keep both its value and the game's semicolon rule (see the
		// printer), and a lone block, loop or for_each gains the `;`.
		if es, ok := prog.Stmts[0].(*ExprStmt); ok {
			if _, isBlock := es.X.(*CondBlockStmt); !isBlock {
				w.note(1)
				w.push(es.X, 2, precNone)
				break
			}
		}
		w.note(0)
		w.push(prog.Stmts[0], 1, precNone)
	default:
		w.push(prog.Stmts[0], 0, precNone)
	}
	w.run()
	return w.max
}

// needsSemicolon reports whether prog, written out, contains a `=` or a `;`
// and so has to end with a `;`: an assignment anywhere, or a block with at
// least one statement.
func needsSemicolon(prog *Program) bool {
	found := false
	Walk(prog, func(n Node) bool {
		switch x := n.(type) {
		case *AssignExpr:
			found = true
		case *Block:
			if len(x.Stmts) > 0 {
				found = true
			}
		}
		return !found
	})
	return found
}

// Precedence levels, low to high, as the grammar has them. They are used
// only to decide where a tree that carries no Grouping would need
// parentheses when written, and the printer's own table must agree; a test
// in the printer package holds the two together.
const (
	precNone = iota
	precNullish
	precTernary
	precOr
	precAnd
	precEquality
	precRelational
	precAdditive
	precMultiplicative
	precDivisive
	precUnary
	precPrimary
)

func binaryPrec(op BinaryOp) int {
	switch op {
	case NullCoalesce:
		return precNullish
	case LOr:
		return precOr
	case LAnd:
		return precAnd
	case CmpEq, CmpNe:
		return precEquality
	case CmpLt, CmpLe, CmpGt, CmpGe:
		return precRelational
	case Add, Sub:
		return precAdditive
	case Mul:
		return precMultiplicative
	case Div:
		return precDivisive
	}
	return precNone
}

func exprPrec(e Expr) int {
	switch e := e.(type) {
	case *UnaryExpr:
		return precUnary
	case *NumberLit:
		// A negative literal is written with a `-` in front, which the
		// game reads as a negation of the positive literal.
		if isNegativeLit(e) {
			return precUnary
		}
	case *BinaryExpr:
		return binaryPrec(e.Op)
	case *AssignExpr:
		return precNone
	case *TernaryExpr, *CondBlockStmt:
		return precTernary
	}
	return precPrimary
}

func isNegativeLit(e *NumberLit) bool { return math.Signbit(e.Value) }

// startsWithMinus reports whether e, written without a Grouping, starts
// with a `-`: it is a negation or a negative literal, or its leftmost
// operand is and is not parenthesized on the way. The printer keeps such an
// operand in parentheses on the right of a `-` and under another `-`,
// because the game reads two `-` in a row as a `+` (`a - -b` is `a + b`,
// and `- -b` alone does not load). The printer decides from the text it
// wrote; this walks the tree the way the printer writes it.
func startsWithMinus(e Expr) bool {
	for {
		switch x := e.(type) {
		case *UnaryExpr:
			return x.Op == Neg
		case *NumberLit:
			return isNegativeLit(x)
		case *BinaryExpr:
			if exprPrec(x.X) < binaryPrec(x.Op) {
				return false
			}
			e = x.X
		case *TernaryExpr:
			if exprPrec(x.Cond) < precOr {
				return false
			}
			e = x.Cond
		case *ArrowExpr:
			if exprPrec(x.Entity) < precPrimary {
				return false
			}
			e = x.Entity
		default:
			return false
		}
	}
}

// keepsArgumentParenthesis reports whether the game leaves a math
// function's parenthesis in its tree as a node of its own: it does for a
// function of one argument, and unwraps the argument list of every other
// function and of every query before it looks at the arguments.
func keepsArgumentParenthesis(op Op) bool {
	switch op {
	case OpAbs, OpArcCosine, OpArcSine, OpArcTangent, OpCeiling, OpCosine,
		OpExp, OpFloor, OpHermiteBlend, OpNaturalLog, OpMinAngle, OpRound,
		OpSine, OpSign, OpSquareRoot, OpTruncate:
		return true
	}
	return false
}

// A work item is a node to measure, the depth of its outermost node (for an
// expression, that of its outermost parenthesis if it has any), and, for
// an expression measured without a Grouping, the precedence its position
// demands before it would need parentheses.
type depthItem struct {
	n    Node
	d    int
	need int
	// dangling marks the Then arm of a ternary that has an else, where a
	// conditional without an else of its own would need parentheses.
	dangling bool
	// negated marks the operand of a `-`, binary or unary, where an operand
	// itself written with a `-` would need parentheses.
	negated bool
}

type depthWalker struct {
	g     *Grouping
	max   int
	stack []depthItem
}

func (w *depthWalker) note(d int) {
	if d > w.max {
		w.max = d
	}
}

func (w *depthWalker) push(n Node, d, need int) {
	w.stack = append(w.stack, depthItem{n: n, d: d, need: need})
}

// parens is the number of parenthesis nodes wrapping e in the game's tree.
func (w *depthWalker) parens(e Expr, it depthItem) int {
	if w.g != nil {
		return w.g.Parens[e]
	}
	if exprPrec(e) < it.need {
		return 1
	}
	if it.dangling && danglingElse(e) {
		return 1
	}
	if it.negated && startsWithMinus(e) {
		return 1
	}
	return 0
}

// danglingElse mirrors the printer: a conditional whose else-chain ends
// without an else would capture an enclosing else if written bare.
func danglingElse(e Expr) bool {
	for {
		switch x := e.(type) {
		case *TernaryExpr:
			if x.Else == nil {
				return true
			}
			e = x.Else
		case *CondBlockStmt:
			if isTrueLit(x.Cond) {
				return false
			}
			if x.Else == nil {
				return true
			}
			e = x.Else
		default:
			return false
		}
	}
}

func isTrueLit(e Expr) bool {
	b, ok := e.(*BoolLit)
	return ok && b.Value
}

func (w *depthWalker) plain(cb *CondBlockStmt) bool {
	if w.g != nil {
		return w.g.PlainBlocks[cb]
	}
	return isTrueLit(cb.Cond)
}

func (w *depthWalker) run() {
	for len(w.stack) > 0 {
		it := w.stack[len(w.stack)-1]
		w.stack = w.stack[:len(w.stack)-1]
		switch n := it.n.(type) {
		case Expr:
			w.expr(n, it)
		case *ExprStmt:
			w.push(n.X, it.d, precNone)
		case *ReturnStmt:
			w.note(it.d)
			if n.Value != nil {
				w.push(n.Value, it.d+1, precNone)
			}
		case *BreakStmt, *ContinueStmt:
			w.note(it.d)
		case *LoopStmt:
			w.note(it.d)
			w.push(n.Count, it.d+1, precNone)
			w.block(n.Body, it.d+1)
		case *ForEachStmt:
			w.note(it.d + 1) // the loop variable
			w.push(n.Source, it.d+1, precNone)
			w.block(n.Body, it.d+1)
		case *Block:
			w.block(n, it.d)
		}
	}
}

// block is a brace node holding a `;` node holding the statements.
func (w *depthWalker) block(b *Block, d int) {
	w.note(d + 1)
	for _, s := range b.Stmts {
		w.push(s, d+2, precNone)
	}
}

// condBlock is either a bare block or a `?` node holding the condition, the
// block and, if there is one, the else -- itself a bare block or another
// conditional.
func (w *depthWalker) condBlock(cb *CondBlockStmt, d int) {
	if w.plain(cb) {
		w.block(cb.Body, d)
		return
	}
	w.note(d)
	w.push(cb.Cond, d+1, precOr)
	w.block(cb.Body, d+1)
	if cb.Else != nil {
		w.condBlock(cb.Else, d+1)
	}
}

// arm is a conditional's arm: a block arm is a brace node, anything else an
// expression.
func (w *depthWalker) arm(e Expr, d int, dangling bool) {
	if cb, ok := e.(*CondBlockStmt); ok && w.plain(cb) {
		w.block(cb.Body, d)
		return
	}
	w.stack = append(w.stack, depthItem{n: e, d: d, need: precTernary, dangling: dangling})
}

func (w *depthWalker) expr(e Expr, it depthItem) {
	inner := it.d + w.parens(e, it)
	w.note(inner)
	switch e := e.(type) {
	case *NumberLit:
		// Written with a `-` in front, a negative literal is a negation
		// node holding the positive literal. The parser never builds one
		// (a literal is lexed without its sign), so with a Grouping there
		// is nothing to count.
		if w.g == nil && isNegativeLit(e) {
			w.note(inner + 1)
		}
	case *ArrayAccess:
		w.push(e.Index, inner+1, precNone)
	case *ArrowExpr:
		w.push(e.Entity, inner+1, precPrimary)
		w.push(e.Read, inner+1, precPrimary)
	case *CallExpr:
		argDepth := inner + 1
		if e.Callee != nil && e.Callee.Namespace == Math {
			if op, ok := MathOp(e.Callee.Member); ok && keepsArgumentParenthesis(op) {
				argDepth = inner + 2
			}
		}
		// An assignment among several arguments has to be parenthesized
		// when written (the game groups the commas before the `=`); alone
		// it does not.
		need := precNone
		if len(e.Args) > 1 {
			need = precNullish
		}
		for _, a := range e.Args {
			w.push(a, argDepth, need)
		}
	case *UnaryExpr:
		w.stack = append(w.stack, depthItem{n: e.X, d: inner + 1, need: precUnary, negated: e.Op == Neg})
	case *BinaryExpr:
		// The tree is grouped as the game groups it, so the operands hang
		// directly off the operator. The one rewrite the game makes is
		// `a - b` to `a + -b`: one more level for the subtracted operand.
		// The first operand of a run is written as a left side, the
		// others as right sides, which is what decides their parentheses
		// when there is no Grouping.
		level := binaryPrec(e.Op)
		w.push(e.X, inner+1, level)
		if e.Op == Sub {
			w.stack = append(w.stack, depthItem{n: e.Y, d: inner + 2, need: level + 1, negated: true})
		} else {
			w.push(e.Y, inner+1, level+1)
		}
	case *AssignExpr:
		w.note(inner + 1) // the target
		w.push(e.Value, inner+1, precNullish)
	case *TernaryExpr:
		w.push(e.Cond, inner+1, precOr)
		w.arm(e.Then, inner+1, e.Else != nil)
		if e.Else != nil {
			w.arm(e.Else, inner+1, false)
		}
	case *CondBlockStmt:
		w.condBlock(e, inner)
	}
}
