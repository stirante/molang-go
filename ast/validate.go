package ast

import (
	"fmt"
	"strings"
)

// This file holds the shape predicates behind the load-time rules the game
// applies AFTER it has built a tree: which operations refuse a non-numeric
// operand, which shapes may stand on the left of `=`, and when arithmetic on
// an array element is refused. The parser applies them as it goes, so an
// error can point at a character; eval.Compile applies them again to a tree
// it did not parse, so a program built or rewritten by hand is held to the
// same rules. Both call these so the two cannot drift.

// ExprOp reports the operation a node is, in the engine's own terms and
// before any folding: a number is Float, `a - b` is Add (the engine has no
// subtract; the right side is negated), a math call is the function it
// calls. The second result is false for a math call naming no known
// function, which has no operation to report.
func ExprOp(e Expr) (Op, bool) {
	switch e := e.(type) {
	case *NumberLit, *BoolLit:
		return OpFloat, true
	case *StringLit:
		return OpString, true
	case *ThisExpr:
		return OpThis, true
	case *Ident:
		if e.Namespace == Math {
			if strings.EqualFold(e.Member, "pi") {
				return OpPi, true
			}
			return MathOp(e.Member)
		}
		op, ok := namespaceOps[e.Namespace]
		return op, ok
	case *UnaryExpr:
		if e.Op == LNot {
			return OpLogicalNot, true
		}
		return OpNegate, true
	case *BinaryExpr:
		return BinaryExprOp(e.Op), true
	case *AssignExpr:
		return OpAssignment, true
	case *CallExpr:
		if e.Callee == nil {
			return 0, false
		}
		if e.Callee.Namespace == Math {
			return MathOp(e.Callee.Member)
		}
		op, ok := namespaceOps[e.Callee.Namespace]
		return op, ok
	case *TernaryExpr, *CondBlockStmt:
		return OpConditional, true
	case *ArrowExpr:
		return OpPointer, true
	case *ArrayAccess:
		return OpArray, true
	}
	return 0, false
}

// ExprOpName is the engine's friendly name for ExprOp(e), which is what its
// diagnostics print when they name an operand.
func ExprOpName(e Expr) string {
	op, ok := ExprOp(e)
	if !ok {
		return Op(opCount).String()
	}
	return op.String()
}

// BinaryExprOp is the operation a binary operator is to the engine. Sub is
// Add: `a - b` is built as `a + (-b)`.
func BinaryExprOp(op BinaryOp) Op {
	switch op {
	case Add, Sub:
		// `a - b` is `a + (-b)`; the node is an Add.
		return OpAdd
	case Mul:
		return OpMultiply
	case Div:
		return OpDivide
	case CmpLt:
		return OpLessThan
	case CmpLe:
		return OpLessThanOrEqual
	case CmpGt:
		return OpGreaterThan
	case CmpGe:
		return OpGreaterThanOrEqual
	case CmpEq:
		return OpLogicalEqual
	case CmpNe:
		return OpLogicalNotEqual
	case LAnd:
		return OpLogicalAnd
	case LOr:
		return OpLogicalOr
	case NullCoalesce:
		return OpNullCoalescing
	}
	return Op(opCount)
}

// IsConstant reports whether the game's optimizer folds e to a single number
// before checking anything else: literals, math.pi, and arithmetic, math
// calls and conditionals built only from those. A string is not a number
// and never folds; neither does anything reading a scope or a host.
func IsConstant(e Expr) bool {
	switch e := e.(type) {
	case *NumberLit, *BoolLit:
		return true
	case *Ident:
		return e.Namespace == Math && strings.EqualFold(e.Member, "pi")
	case *UnaryExpr:
		return IsConstant(e.X)
	case *BinaryExpr:
		return e.Op != NullCoalesce && IsConstant(e.X) && IsConstant(e.Y)
	case *CallExpr:
		if e.Callee == nil || e.Callee.Namespace != Math {
			return false
		}
		for _, a := range e.Args {
			if !IsConstant(a) {
				return false
			}
		}
		return true
	case *TernaryExpr:
		return IsConstant(e.Cond) && IsConstant(e.Then) && (e.Else == nil || IsConstant(e.Else))
	}
	return false
}

// IsNonNumericOperand reports whether e is a shape the game refuses as a
// direct operand of arithmetic, a comparison other than == and !=, a logical
// operator, a negation, or a math function: a string, a geometry./material./
// texture. resource, or an assignment. Each of those is a node the engine
// knows has no number to offer -- an assignment because the check is made
// before the value it would yield exists.
//
// Everything else is taken to be numeric, including a conditional, an array
// element, a query call, a variable and `this`; what those evaluate to is
// not examined.
func IsNonNumericOperand(e Expr) bool {
	switch e := e.(type) {
	case *StringLit, *AssignExpr:
		return true
	case *Ident:
		return e.Namespace.IsResource()
	}
	return false
}

// NonNumericOperandMessage is the game's wording for IsNonNumericOperand
// firing on arg as an operand of op.
func NonNumericOperandMessage(op Op, arg Expr) string {
	return fmt.Sprintf("'%s' expression cannot take a '%s' argument. It only supports numerical arguments.", op, ExprOpName(arg))
}

// ArrayElementMathMessage is the game's refusal of arithmetic that would
// scale or offset an array element; see FoldsIntoArrayElement.
const ArrayElementMathMessage = "can't currently do math operations on resource array results"

// FoldsIntoArrayElement reports whether `x op y` is refused because the game
// would fold a constant into the array element itself.
//
// The rule is not "no arithmetic on an element". The game's optimizer folds
// a constant added to or multiplied into a node onto that node, and then
// refuses an array element carrying such a fold; an element combined with
// something that is not a constant carries nothing and is accepted. So
// `array.a[i] + 1`, `array.a[i] * 2` and `array.a[i] - 1` are refused, while
// `array.a[i] + v.x`, `array.a[i] * v.x` and `array.a[i] - v.x` load. A
// negation is always folded, which is why `1 - array.a[i]` and `-array.a[i]`
// are refused (see NegatesArrayElement) though `array.a[i] - v.x` is not.
// Division is never folded this way, and neither is a comparison or a logical
// operator, so `array.a[i] / 2` and `array.a[i] == 1` load.
func FoldsIntoArrayElement(op BinaryOp, x, y Expr) bool {
	_, xArr := x.(*ArrayAccess)
	_, yArr := y.(*ArrayAccess)
	switch op {
	case Add, Mul:
		return (xArr && IsConstant(y)) || (yArr && IsConstant(x))
	case Sub:
		// The right side is negated, and a negation is a fold of its own.
		return (xArr && IsConstant(y)) || yArr
	}
	return false
}

// NegatesArrayElement reports whether `-x` is refused: a negation is folded
// onto its operand, and an array element cannot carry one.
func NegatesArrayElement(x Expr) bool {
	_, ok := x.(*ArrayAccess)
	return ok
}

// Assignment left sides. The game checks a left side in two passes, and the
// wording depends on which one refuses it.
//
// A statement `X = Y;` is first checked as written: X must be a variable.
// or temp. name, possibly with members (`v.a.b`), or a pointer read
// (`a->v.b`). Anything else is refused as an assignment to a non-variable,
// named by what it is.
//
// The tree is then checked again, member by member: a temp. name may only be
// the whole left side, a context. name may not appear in it at all, and a
// pointer cannot be written through.

// AssignTargetProblem reports why target may not stand on the left of `=`,
// or "" if it may. It expects the parser's own representation, where
// `v.a.b` is one Ident whose Member contains a dot.
func AssignTargetProblem(target Expr) string {
	switch t := target.(type) {
	case *Ident:
		switch t.Namespace {
		case Variable:
			return ""
		case Temp:
			if strings.Contains(t.Member, ".") {
				return TempNotAloneMessage
			}
			return ""
		case Context:
			if strings.Contains(t.Member, ".") {
				return fmt.Sprintf(LeftSideOperatorMessage, OpContextVariable)
			}
		}
	case *ArrowExpr:
		// A pointer whose read side is a variable is looked at as a
		// pointer, and refused as one. (The game also looks at what is
		// left of the arrow, and logs a second refusal when that is a
		// temp, a context. name or an operator; the pointer's own is the
		// one that tells an author what to change.) A pointer reading a
		// query is refused as a non-variable before it gets that far.
		if r, ok := t.Read.(*Ident); ok && r.Namespace == Variable {
			return PointerAssignMessage
		}
	}
	return fmt.Sprintf(NonVariableAssignMessage, ExprOpName(target))
}

// The game's wording for each refusal of an assignment's left side.
const (
	NonVariableAssignMessage = "assignment to non-variable not allowed. Expression is trying to assign to a: %s"
	LeftSideOperatorMessage  = "cannot use %s operators on the left side of an assignment expression"
	TempNotAloneMessage      = "left side of an assignment expression can only use temp variables if they are on their own and not part of a more complicated expression."
	PointerAssignMessage     = "Assignment attempted on Pointer result. Writing to another entity's public variables is not supported, only reading them."
	BreakOutsideLoopMessage  = "break encountered outside of loop"
)
