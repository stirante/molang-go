package eval

import (
	"fmt"
	"math"
	"strings"

	"molang-go/ast"
)

// Error is a compile-time error (unknown math function, wrong arity, bare
// math.<fn> reference that isn't called).
type Error struct {
	Msg string
}

func (e *Error) Error() string { return e.Msg }

// Signal is the control-flow outcome of executing a statement/block.
type Signal uint8

const (
	SigNone Signal = iota
	SigReturn
	SigBreak
	SigContinue
)

type exprFn func(ctx *Context) float64
type stmtFn func(ctx *Context) (float64, Signal)

// Program is a compiled Molang expression, cheap to Run repeatedly: all
// AST-shape decisions (which operator, which namespace, which math
// function, how many statements) were resolved once at Compile time into a
// closure tree, so Run pays only for the closure calls themselves — no
// further type switches or map lookups beyond the ones each construct
// inherently needs (namespace-bag reads/writes, registered query calls).
type Program struct {
	run func(ctx *Context) float64
}

// Run evaluates the compiled program against ctx and returns its numeric
// result. Booleans surface as 1/0; a statement sequence with no explicit
// `return` evaluates to 0 -- including a sequence of exactly one statement
// followed by a trailing `;` (see Compile, and ast.Program.HasSemicolon).
//
// Run can also stop EARLY, before reaching the end of the program, and
// still return 0: a temp./variable./context. read that finds no value in
// its bag and has no enclosing `??` aborts the whole program where it
// stands, exactly as the engine does (see eval/unresolved.go). Everything
// sequenced after such a read -- assignments, RNG draws -- does not happen.
// A host that cannot supply the scope such a read expects can set
// Context.ContinueOnUnresolvedRead to keep evaluating past it (and
// Context.OnUnresolvedRead to learn what it swallowed); the abort is the
// default, so a caller that says nothing gets the engine.
func (p *Program) Run(ctx *Context) float64 {
	return p.run(ctx)
}

// Compile compiles a parsed Program into a form ready for repeated Run
// calls. It also performs the checks that have to happen before anything
// runs: unknown math functions, wrong arity, and a bare math.<fn>
// reference that isn't called (except math.pi) are all errors here rather
// than at evaluation time, so a caller validating an expression learns
// about them without having to evaluate it.
func Compile(prog *ast.Program) (compiled *Program, err error) {
	defer func() {
		if r := recover(); r != nil {
			if ce, ok := r.(*Error); ok {
				err = ce
				return
			}
			panic(r)
		}
	}()

	c := &compiler{}
	stmts := make([]stmtFn, len(prog.Stmts))
	for i, s := range prog.Stmts {
		stmts[i] = c.compileStmt(s)
	}

	// A source string with NO top-level ';' is a bare expression, and its
	// value is the program's value. Anything else -- including a single
	// statement followed by a trailing ';' -- follows the normal sequence
	// rule below: 0 unless something returns.
	//
	// This is the ONLY thing ast.Program.HasSemicolon is for, and reading
	// it here is what makes the field mean what its doc comment says. It
	// was set by the parser and never read, so `1+1;` evaluated to 2 and
	// `temp.a=5;` to 5, both of which the documented rule says are 0.
	//
	// CONFIRMED (this was labelled INFERRED when it was implemented, and
	// has since been established directly). The engine's Molang program
	// builder compiles the ';' node by looping over its children emitting
	// each one, and then UNCONDITIONALLY emitting a pure constant push
	// carrying the ';' node's own folded `add` constant, normally 0. So
	// the last statement's value IS computed, and is then overwritten by
	// that constant. A ';'-terminated program yields 0 unless a `return`
	// jumped out before the push, and that holds for a sequence of length
	// one exactly as it does for a longer one.
	//
	// The `len(stmts) == 1` half of the condition matters for AST trees
	// built by hand rather than parsed (HasSemicolon defaults to false): a
	// multi-statement program is a sequence regardless, which is also what
	// printer.Format/Minify emit for one.
	var run func(ctx *Context) float64
	if len(stmts) == 1 && !prog.HasSemicolon {
		single := stmts[0]
		run = func(ctx *Context) float64 {
			v, _ := single(ctx)
			return v
		}
	} else {
		run = func(ctx *Context) float64 {
			for _, sf := range stmts {
				v, sig := sf(ctx)
				if sig == SigReturn {
					return v
				}
				// A stray break/continue at top level (outside any loop)
				// is a no-op yielding 0, matching the engine: the
				// compiled break instruction finds the break-address
				// stack empty, fires the engine's assert handler --
				// a debug assert, inert in a release build -- then
				// writes 0 and continues -- so at runtime it is not an
				// error at all. Whether a separate load-time validation
				// pass rejects it before that point was not checked.
			}
			return 0
		}
	}

	// Catch a return raised from inside an expression (see the
	// CondBlockStmt case in compileExpr). Installed unconditionally: it is
	// one deferred call per run, and making it conditional would mean
	// walking the tree for a construct that is rare enough not to be worth
	// the scan.
	inner := run
	run = func(ctx *Context) (result float64) {
		defer func() {
			if r := recover(); r != nil {
				if rs, ok := r.(returnSignal); ok {
					result = rs.value
					return
				}
				panic(r)
			}
		}()
		return inner(ctx)
	}

	// An unresolved temp./variable./context. read with no enclosing `??`
	// ends the program where it stands (eval/unresolved.go), unless the
	// Context it runs against opted out (Context.ContinueOnUnresolvedRead).
	// The catch root that implements both -- the recover, and the per-run
	// catch-stack reset -- is installed only if the tree actually contains
	// a read that can raise the sentinel, so a program of pure arithmetic,
	// literals and query. calls pays nothing.
	if c.canAbort {
		run = catchRoot(run)
	}
	return &Program{run: run}, nil
}

type compiler struct {
	// canAbort records whether the tree compiled so far contains at least
	// one namespace read that participates in the engine's unresolved-read
	// mechanism -- i.e. whether this program can abort mid-evaluation, and
	// therefore whether Compile needs to install the top-level recover.
	canAbort bool
}

func (c *compiler) fail(format string, args ...any) {
	panic(&Error{Msg: fmt.Sprintf(format, args...)})
}

// isTruthy is the condition test for ternaries and statement-conditionals.
//
// CONFIRMED: the engine's compiled conditional instruction tests the
// condition for float32 EQUALITY with 0.0. Only an exact 0.0 or -0.0
// takes the else branch. NaN compares UNORDERED, so the equality test is
// false and a NaN condition takes the THEN branch: `math.sqrt(-1) ? 111 :
// 222` is 111 in the engine.
//
// This package used to answer 222, having used JS-style "0 or NaN is
// falsy" truthiness for the ternary -- while `!` and `&&`/`||` already
// used the engine's exactly-zero test, so three operators
// disagreed about what NaN means and at most one of them could be right.
// The ternary was the odd one out; the other three are confirmed to match:
//
//   - `!`  -- compares against float32 0.0 and selects on NOT-equal, so
//     !NaN = 0.
//   - `&&` -- the same shape with the operands swapped, so NaN && 1 = 1.
//   - `||` -- likewise.
//
// So: false iff exactly zero, everywhere in this package.
func isTruthy(v float64) bool {
	return v != 0
}

// ---------------------------------------------------------------------
// Statements
// ---------------------------------------------------------------------

func (c *compiler) compileStmt(s ast.Stmt) stmtFn {
	switch s := s.(type) {
	case *ast.ExprStmt:
		xFn := c.compileExpr(s.X)
		return func(ctx *Context) (float64, Signal) { return xFn(ctx), SigNone }

	case *ast.ReturnStmt:
		if s.Value == nil {
			return func(ctx *Context) (float64, Signal) { return 0, SigReturn }
		}
		vFn := c.compileExpr(s.Value)
		return func(ctx *Context) (float64, Signal) { return vFn(ctx), SigReturn }

	case *ast.BreakStmt:
		return func(ctx *Context) (float64, Signal) { return 0, SigBreak }

	case *ast.ContinueStmt:
		return func(ctx *Context) (float64, Signal) { return 0, SigContinue }

	case *ast.CondBlockStmt:
		return c.compileCondBlock(s)

	case *ast.LoopStmt:
		return c.compileLoop(s)

	case *ast.ForEachStmt:
		return c.compileForEach(s)
	}
	c.fail("compile: unhandled statement %T", s)
	panic("unreachable")
}

func (c *compiler) compileBlock(b *ast.Block) stmtFn {
	stmts := make([]stmtFn, len(b.Stmts))
	for i, s := range b.Stmts {
		stmts[i] = c.compileStmt(s)
	}
	return func(ctx *Context) (float64, Signal) {
		for _, sf := range stmts {
			v, sig := sf(ctx)
			if sig != SigNone {
				return v, sig
			}
		}
		return 0, SigNone
	}
}

func (c *compiler) compileCondBlock(cb *ast.CondBlockStmt) stmtFn {
	condFn := c.compileExpr(cb.Cond)
	bodyFn := c.compileBlock(cb.Body)
	var elseFn stmtFn
	if cb.Else != nil {
		elseFn = c.compileCondBlock(cb.Else)
	}
	return func(ctx *Context) (float64, Signal) {
		if isTruthy(condFn(ctx)) {
			return bodyFn(ctx)
		}
		if elseFn != nil {
			return elseFn(ctx)
		}
		return 0, SigNone
	}
}

// LoopCounterMax is the maximum number of iterations a single loop()
// executes IN THIS PACKAGE. It is a deliberate DIVERGENCE from the engine,
// not a model of it. Read the next two paragraphs before relying on it.
//
// CONFIRMED, and it is the opposite of what this constant used to claim:
// **there is no 1024 loop cap in Bedrock 1.26.50.24.** `loop`'s compiled
// back-edge instruction reads a float32 counter, tests it against 0.0,
// decrements it by 1.0 and jumps back; the loop-setup
// instruction skips the loop entirely when the count is <= 0. Neither
// compares against 1024 or against anything else at all. A sweep of the
// whole Molang implementation for 1024, 1023 and the float32 bit pattern
// of 1024.0 found 55 hits, none of them in the program builder, the
// tokeniser, the loop setup or any evaluation instruction, and an
// engine-wide string sweep for a loop-limit diagnostic found nothing.
//
// MEASURED IN GAME, which is the evidence that actually settles it:
// `v.n = 0; loop(5000, { v.n = v.n + 1; }); return v.n;` evaluated in
// 1.26.50.24 returns 5000, not 1024. The expression also loads without
// complaint, so nothing rejects a large count at load time either.
//
// (An earlier revision of this comment asserted "the engine runs
// loop(1000000, {...}) a million times". Nothing supported that number --
// it was never run -- so it has been replaced by the count that was.
// 5000 is what is known; a million remains untested and is not needed.)
//
// The claim was challenged and re-checked from scratch before being
// written down again, because one this counter-intuitive should not survive
// on its own say-so. Nothing anywhere in the engine's loop handling limits
// the count: not where the count is taken, not on the way round the loop,
// and not as a constant hiding elsewhere in the implementation.
//
// Mojang's documentation says the opposite, and says it prominently -- the
// Molang syntax guide carries a Caution box reading "The maximum loop
// counter is 1024 for safety reasons", and the Versioned Changes table
// repeats it. Neither describes this build. The clamp is KEPT anyway, and
// relabelled, as an
// explicit HOST-PROTECTION measure: this package's consumer previews
// expressions from arbitrary third-party packs, where an author-typo'd
// `loop(v.big, {...})` hanging the tool is a worse outcome than a wrong
// number, and worse than it would be in the game. So:
//
//	CONFIRMED  the engine has no cap.
//	POLICY     this package caps at 1024 anyway, and is therefore known to
//	           disagree with the engine for any loop whose count exceeds
//	           1024. Set LoopCounterMax higher (or gut loopIterations) to
//	           trade the hang protection back for exactness.
const LoopCounterMax = 1024

// loopIterations converts a loop()'s evaluated count into the number of
// body iterations actually run. It is the single place the LoopCounterMax
// policy is applied -- change this function and nothing else to remove or
// widen the divergence described on LoopCounterMax.
//
// Clamping, not erroring, is what this package does. Erroring was never an
// option: Program.Run returns a float64 and has no error channel, so it
// would mean either a panic escaping Run (an evaluator that can crash its
// host mid-worldgen) or a Compile-time rejection, which cannot work at all
// -- the count is an arbitrary expression, so `loop(v.n, {...})` is only
// knowable at run time.
//
// Non-finite and non-positive counts collapse to zero iterations. That is
// not merely tidiness: Go leaves float64->int conversion undefined for NaN
// and out-of-range values, so the previous bare `int(count)` was
// platform-dependent for `loop(0/0, {...})` -- on amd64 it produced a large
// negative (harmless by luck), not a defined zero.
func loopIterations(count float64) int {
	if math.IsNaN(count) || count < 1 {
		return 0
	}
	if count >= LoopCounterMax {
		return LoopCounterMax
	}
	return int(count)
}

func (c *compiler) compileLoop(l *ast.LoopStmt) stmtFn {
	countFn := c.compileExpr(l.Count)
	bodyFn := c.compileBlock(l.Body)
	return func(ctx *Context) (float64, Signal) {
		n := loopIterations(countFn(ctx))
		for i := 0; i < n; i++ {
			v, sig := bodyFn(ctx)
			switch sig {
			case SigBreak:
				return 0, SigNone
			case SigContinue:
				continue
			case SigReturn:
				return v, SigReturn
			}
		}
		return 0, SigNone
	}
}

// compileForEach walks a host array, writing each element to the loop
// variable before running the body.
//
// break and continue behave as they do in loop(). A return ends the whole
// program, not just the walk -- the same as everywhere else, because the
// engine's return sets the program counter past the end and the loop body is
// compiled into the same instruction stream.
//
// An array the host never registered walks zero times. That is the same
// answer as an empty one, and there is nothing to report: a for_each over
// nothing is not an error in any reading of the language.
func (c *compiler) compileForEach(f *ast.ForEachStmt) stmtFn {
	name := f.Array
	bodyFn := c.compileBlock(f.Body)
	ns, member := f.Var.Namespace, f.Var.Member
	return func(ctx *Context) (float64, Signal) {
		arr := ctx.Scope.Array[name]
		for _, elem := range arr {
			bag := ctx.Scope.Temp
			if ns == ast.Variable {
				bag = ctx.Scope.Variable
			}
			bag[member] = Round32(elem)
			v, sig := bodyFn(ctx)
			switch sig {
			case SigBreak:
				return 0, SigNone
			case SigContinue:
				continue
			case SigReturn:
				return v, SigReturn
			}
		}
		return 0, SigNone
	}
}

// ---------------------------------------------------------------------
// Expressions
// ---------------------------------------------------------------------

func (c *compiler) compileExpr(e ast.Expr) exprFn {
	switch e := e.(type) {
	case *ast.NumberLit:
		v := Round32(e.Value)
		return func(*Context) float64 { return v }

	case *ast.BoolLit:
		v := 0.0
		if e.Value {
			v = 1
		}
		return func(*Context) float64 { return v }

	case *ast.StringLit:
		v := internString(e.Value)
		return func(*Context) float64 { return v }

	case *ast.Ident:
		return c.compileIdentRead(e)

	case *ast.UnaryExpr:
		xFn := c.compileExpr(e.X)
		switch e.Op {
		case ast.Neg:
			return func(ctx *Context) float64 { return Round32(-xFn(ctx)) }
		case ast.LNot:
			return func(ctx *Context) float64 {
				if xFn(ctx) == 0 {
					return 1
				}
				return 0
			}
		}

	case *ast.BinaryExpr:
		return c.compileBinary(e)

	case *ast.AssignExpr:
		return c.compileAssign(e)

	case *ast.CallExpr:
		return c.compileCall(e)

	case *ast.ThisExpr:
		return func(ctx *Context) float64 { return Round32(ctx.This) }

	case *ast.ArrowExpr:
		// A read against another entity. A name with no entity behind it
		// reads 0, and so does a query the entity does not answer.
		//
		// This is NOT the unresolved-read mechanism: reaching for a missing
		// entity does not end the expression. `v.x = c.nobody->v.y;` still
		// assigns, and assigns 0. That asymmetry is deliberate in the
		// language -- a missing VARIABLE is a mistake worth stopping for, a
		// missing ENTITY is an ordinary fact about the world.
		entity := strings.ToLower(e.Entity.Member)
		switch r := e.Read.(type) {
		case *ast.Ident:
			member := strings.ToLower(r.Member)
			isVar := r.Namespace == ast.Variable
			return func(ctx *Context) float64 {
				ent := ctx.Entities[entity]
				if ent == nil {
					return 0
				}
				if isVar {
					return Round32(ent.Variable[member])
				}
				if fn := ent.QueryFuncs[member]; fn != nil {
					return Round32(fn(nil, ctx))
				}
				return 0
			}
		case *ast.CallExpr:
			member := strings.ToLower(r.Callee.Member)
			argFns := make([]exprFn, len(r.Args))
			for i, a := range r.Args {
				argFns[i] = c.compileExpr(a)
			}
			return func(ctx *Context) float64 {
				// The arguments are evaluated whether or not anything
				// answers, so RNG draws inside them keep their order.
				args := make([]float64, len(argFns))
				for i, fn := range argFns {
					args[i] = fn(ctx)
				}
				ent := ctx.Entities[entity]
				if ent == nil {
					return 0
				}
				if fn := ent.QueryFuncs[member]; fn != nil {
					return Round32(fn(args, ctx))
				}
				return 0
			}
		}
		c.fail("compile: unsupported read through '->'")
		panic("unreachable")

	case *ast.ArrayAccess:
		// The index is truncated toward zero, a negative one reads element
		// 0, and a positive one wraps modulo the length. So an array read
		// is TOTAL: it cannot fail and cannot go out of range, whatever the
		// index expression computes -- including a fractional one, a huge
		// one, or a negative one.
		//
		// That is worth stating because it is the opposite of the usual
		// bargain. There is no bounds error to catch and no sentinel to
		// test for, so an off-by-one silently reads a neighbouring element
		// and an author never learns. A tool that wants to help can warn;
		// it must not refuse.
		//
		// An unregistered array name, and a registered but empty one, both
		// read as 0 -- there is no element to wrap onto.
		name := e.Name
		idxFn := c.compileExpr(e.Index)
		return func(ctx *Context) float64 {
			arr := ctx.Scope.Array[name]
			if len(arr) == 0 {
				return 0
			}
			i := f32ToInt(math.Trunc(idxFn(ctx)))
			if i < 0 {
				return Round32(arr[0])
			}
			return Round32(arr[i%len(arr)])
		}

	case *ast.TernaryExpr:
		condFn := c.compileExpr(e.Cond)
		thenFn := c.compileExpr(e.Then)
		var elseFn exprFn
		if e.Else != nil {
			elseFn = c.compileExpr(e.Else)
		}
		return func(ctx *Context) float64 {
			if isTruthy(condFn(ctx)) {
				return thenFn(ctx)
			}
			if elseFn != nil {
				return elseFn(ctx)
			}
			return 0
		}

	case *ast.CondBlockStmt:
		// A statement-conditional in expression position -- what you get by
		// wrapping one in parentheses:
		//
		//	v.x ? (v.y ? {return 3;} : {return 1;}) : (...); return 4;
		//
		// This used to say "Real Molang has no such construct" and drop the
		// block's return/break/continue on the floor, so the expression
		// above answered 4. It answers 3: the inner `return` ends the whole
		// program, and the trailing statement never runs.
		//
		// A return has to travel out through however much expression is
		// wrapped around it, and an expression here is a plain
		// func(*Context) float64 with nowhere to put a signal. So it is
		// raised, and caught at the program root -- the same shape the
		// unresolved-read sentinel already uses, and for the same reason.
		// It also gets the abort semantics right for free: the engine's
		// return jumps to the end of the program, so anything still pending
		// in the surrounding expression does not run either.
		//
		// break and continue are NOT propagated the same way. Nothing
		// attests what a parenthesised `(cond ? break)` does, and the form
		// loops actually use -- `cond ? break;` unparenthesised -- is parsed
		// as a statement and never reaches here.
		stmtFn := c.compileCondBlock(e)
		return func(ctx *Context) float64 {
			v, sig := stmtFn(ctx)
			if sig == SigReturn {
				panic(returnSignal{value: v})
			}
			return v
		}
	}
	c.fail("compile: unhandled expression %T", e)
	panic("unreachable")
}

func (c *compiler) compileBinary(e *ast.BinaryExpr) exprFn {
	xFn := c.compileExpr(e.X)
	yFn := c.compileExpr(e.Y)
	switch e.Op {
	case ast.Add:
		// CONFIRMED: the engine's add is a float32 add -- see Round32's
		// doc comment.
		return func(ctx *Context) float64 { return Round32(xFn(ctx) + yFn(ctx)) }
	case ast.Sub:
		// The engine has no separate binary Subtract opcode: its own
		// operator/token metadata table lists Add ('+', id 9, arity 2) and
		// Negate ('-', id 6, arity 1, UNARY only) but no "Subtract" entry
		// anywhere in the table, and the engine's Molang tree builder
		// processes Add/Multiply/Divide (ids 9/31/20) together as one
		// arithmetic tier while Negate is folded in earlier by a separate
		// negation-and-logical-not pass -- i.e. `a - b` is compiled as
		// `a + (-b)`. That decomposition is exact in IEEE-754 (negation
		// never rounds), so `Round32(x - y)` here is bit-for-bit identical
		// to the engine's Add-of-Negate, without needing to model it as
		// two opcodes.
		return func(ctx *Context) float64 { return Round32(xFn(ctx) - yFn(ctx)) }
	case ast.Mul:
		// Multiply ('*', id 31, arity 2): individually pinned from its own
		// compiled instruction, which computes
		// `(float)((float)(a * b) * scale) + offset` -- a float32 multiply
		// and a float32 add only, every intermediate a 4-byte float.
		return func(ctx *Context) float64 { return Round32(xFn(ctx) * yFn(ctx)) }
	case ast.Div:
		// Divide ('/', id 20, arity 2): individually pinned from its own
		// compiled instructions. The compiled program for a division is NOT
		// a bare float32 divide:
		//
		//   [denominator ops] [zero guard] [numerator ops] [divide]
		//
		//   - The DENOMINATOR is built and evaluated FIRST (the guard slot
		//     is allocated right after it, and the divide instruction
		//     divides last-result (numerator) by the guard-pushed stack
		//     value), so any side effects / RNG draws in the denominator
		//     happen before the numerator's.
		//   - The guard used for Molang version > 6 -- worldgen packs with
		//     min_engine_version 1.21.80 are far above that -- tests
		//     `fabsf(denom) < FLT_EPSILON`, with FLT_EPSILON the float32
		//     constant 2^-23 = 1.1920928955078125e-7. When it fires, the
		//     whole division evaluates to +0.0, the Molang default return
		//     value, and the NUMERATOR IS NEVER EVALUATED (the guard
		//     rewrites the program counter past it), so numerator-side RNG
		//     draws are not consumed.
		//   - The Molang version <= 6 variant instead pushes fabsf(denom)
		//     -- old packs divide by |denominator|. That variant is
		//     unreachable for this project's packs and is deliberately not
		//     implemented here.
		//   - The divide instruction itself computes
		//     `(float)((float)(num / den) * scale) + offset`, float32
		//     throughout.
		//
		// The threshold test itself lives in DivGuardFires (math.go) so
		// the constant folder in package transform can apply the identical
		// guard to a constant division without re-deriving it -- the two
		// used to have independent copies of "what division means" and
		// drifted (transform folded 1/0.0000001 to 1e7 where this
		// evaluates 0, and the printer then emitted that literal as
		// source). Only the SHAPE below (denominator first, numerator
		// skipped) is specific to the lazy evaluator; the numeric decision
		// is shared.
		return func(ctx *Context) float64 {
			den := yFn(ctx)
			if DivGuardFires(den) {
				return 0
			}
			return Round32(xFn(ctx) / den)
		}
	// Comparisons (id 50-55: '<','<=','>=','>','==','!=') are confirmed as
	// real, distinct binary opcodes (arity 2) in the engine's operator
	// metadata table, dispatched through the same binary-operator
	// mechanism as Add/Multiply/Divide above. Their result (0/1) is always
	// exactly representable in float32, and both operands are already
	// Round32-ed by induction (every exprFn's output funnels through
	// Round32 before reaching here), so comparing them as float64 gives
	// the identical ordering a float32 compare would -- float32->float64
	// promotion is exact and monotonic. No further rounding is needed at
	// this op itself.
	case ast.CmpLt:
		return boolExpr(xFn, yFn, func(a, b float64) bool { return a < b })
	case ast.CmpLe:
		return boolExpr(xFn, yFn, func(a, b float64) bool { return a <= b })
	case ast.CmpGt:
		return boolExpr(xFn, yFn, func(a, b float64) bool { return a > b })
	case ast.CmpGe:
		return boolExpr(xFn, yFn, func(a, b float64) bool { return a >= b })
	case ast.CmpEq:
		return boolExpr(xFn, yFn, func(a, b float64) bool { return a == b })
	case ast.CmpNe:
		return boolExpr(xFn, yFn, func(a, b float64) bool { return a != b })
	case ast.LAnd:
		// Boolean-normalizing, short-circuiting: falsy iff exactly 0, so
		// NaN counts as truthy. The ternary agrees; it did not always, and
		// the header comment above records which one was wrong.
		return func(ctx *Context) float64 {
			if xFn(ctx) == 0 {
				return 0
			}
			if yFn(ctx) != 0 {
				return 1
			}
			return 0
		}
	case ast.LOr:
		return func(ctx *Context) float64 {
			if xFn(ctx) != 0 {
				return 1
			}
			if yFn(ctx) != 0 {
				return 1
			}
			return 0
		}
	case ast.NullCoalesce:
		// `??` is a try/catch over its LHS, not a value test. It fires
		// when the LHS performs a variable read that finds NO VALUE --
		// which is a distinct state from reading the value 0, and is
		// nothing to do with NaN. See eval/unresolved.go for the full
		// derivation -- the engine's catch frames and its missing-variable
		// handler -- and for why the catch is scoped to the LHS only, so
		// `v.a ?? v.b` with both unset aborts the program rather than
		// yielding 0.
		//
		// This replaces a NaN-coalescing reading that could never fire for
		// the idiom real packs write: nothing in this value model produces
		// NaN from a missing member, so `t.cut_corner_chance ?? 0.35`
		// yielded 0 -- eight expressions in the golden corpus use exactly
		// that shape.
		return func(ctx *Context) float64 {
			if l, resolved := catchUnresolved(xFn, ctx); resolved {
				return l
			}
			return yFn(ctx)
		}
	}
	c.fail("compile: unhandled binary op %v", e.Op)
	panic("unreachable")
}

func boolExpr(xFn, yFn exprFn, cmp func(a, b float64) bool) exprFn {
	return func(ctx *Context) float64 {
		x, y := xFn(ctx), yFn(ctx)
		if cmp(x, y) {
			return 1
		}
		return 0
	}
}

// compileAssign rounds the assigned value to float32 before both storing it
// (Scope.Temp/Variable are storage the game reads through directly,
// which are 4-byte float payloads, not 8-byte double) and
// returning it, since AssignExpr's own value is the assigned value.
func (c *compiler) compileAssign(e *ast.AssignExpr) exprFn {
	member := strings.ToLower(e.Target.Member)
	switch e.Target.Namespace {
	case ast.Temp, ast.Variable:
	default:
		c.fail("compile: invalid assignment target namespace %v", e.Target.Namespace)
	}

	// `v.y = v.x` where v.x has dotted members copies the members too, so
	// afterwards v.y.<m> exists for every v.x.<m>. A name with members
	// behaves as a struct on the right of an assignment.
	//
	// The members are the flat keys sharing the name's prefix, because that
	// is how this package stores a dotted name: `v.x.y` is the single key
	// "x.y", never a field of "x". Both facts are true at once -- reads
	// split on the first dot only, and an assignment carries the subtree --
	// so the copy is a prefix scan rather than a structure walk.
	//
	// Only this form is special. A bare struct read anywhere else, `v.x + 1`
	// with just v.x.* set, stays an unresolved read: nothing establishes
	// what it should be and inventing an answer is worse than the gap.
	if src, ok := structSource(e.Value); ok {
		// This path can raise the unresolved sentinel itself, when the
		// source name has neither a value nor any member, so the catch root
		// has to be installed for it exactly as for an ordinary read.
		c.canAbort = true
		srcMember := strings.ToLower(src.Member)
		srcNS, dstNS := src.Namespace, e.Target.Namespace
		return func(ctx *Context) float64 {
			from := scopeBag(ctx, srcNS)
			to := scopeBag(ctx, dstNS)
			prefix := srcMember + "."
			copied := false
			for k, v := range from {
				if strings.HasPrefix(k, prefix) {
					to[member+"."+k[len(prefix):]] = v
					copied = true
				}
			}
			if scalar, has := from[srcMember]; has {
				v := Round32(scalar)
				to[member] = v
				return v
			}
			if copied {
				// A struct with no scalar of its own. The copy happened;
				// the expression's own value is 0, the same as the engine
				// leaves for a read that produced no number.
				return 0
			}
			// Neither a value nor any member: a genuinely unresolved read,
			// handled exactly as it would be outside an assignment. When the
			// host has opted out of aborting, that yields 0 and evaluation
			// continues -- and the assignment must still happen, or the
			// target silently stays absent where every other path would have
			// left a 0 behind.
			v := ctx.unresolved(srcNS.String() + "." + srcMember)
			to[member] = v
			return v
		}
	}

	valFn := c.compileExpr(e.Value)
	switch e.Target.Namespace {
	case ast.Temp:
		return func(ctx *Context) float64 {
			v := Round32(valFn(ctx))
			ctx.Scope.Temp[member] = v
			return v
		}
	case ast.Variable:
		return func(ctx *Context) float64 {
			v := Round32(valFn(ctx))
			ctx.Scope.Variable[member] = v
			return v
		}
	}
	panic("unreachable")
}

// structSource reports whether an assignment's right-hand side is a bare
// temp./variable. read -- the only shape that can carry a struct.
func structSource(e ast.Expr) (*ast.Ident, bool) {
	id, ok := e.(*ast.Ident)
	if !ok {
		return nil, false
	}
	if id.Namespace != ast.Temp && id.Namespace != ast.Variable {
		return nil, false
	}
	return id, true
}

// scopeBag is the map a namespace reads and writes.
func scopeBag(ctx *Context, ns ast.Namespace) map[string]float64 {
	if ns == ast.Variable {
		return ctx.Scope.Variable
	}
	return ctx.Scope.Temp
}

// compileIdentRead compiles a bare (non-called) namespaced identifier.
//
// Scope.Temp/Variable/Query are plain caller-owned map[string]float64 --
// the FFI boundary with whatever host populates them (e.g. featurelab-go
// seeding variable.* from real double-precision block/entity data). This
// package's own writers (compileAssign above) always store a Round32-ed
// value, but a host can bypass that and stash an un-rounded double
// directly; rounding again on read (a no-op when the stored value is
// already float32-exact) is what makes "a value can never carry more than
// single precision through an expression" a property of every Molang
// value this package hands back, not just the ones this package itself
// wrote.
//
// KEY PRESENCE IS NOW SEMANTIC for temp./variable./context.: a key that is
// absent from the bag is an UNRESOLVED read, which either diverts an
// enclosing `??` to its right-hand side or aborts the program (see
// eval/unresolved.go). A key that is present with the value 0 is a
// resolved zero and does neither. `v.x = 0; v.x ?? 5` is 0; `v.unset ?? 5`
// is 5. Hosts that want a name to read as a plain 0 must WRITE 0 into the
// bag; leaving it out no longer means the same thing.
func (c *compiler) compileIdentRead(id *ast.Ident) exprFn {
	member := strings.ToLower(id.Member)
	switch id.Namespace {
	case ast.Math:
		if member == "pi" {
			return func(*Context) float64 { return mathPiF32 }
		}
		c.fail("math.%s must be called, e.g. math.%s(...)", id.Member, id.Member)
	case ast.Query:
		// query. deliberately does NOT participate: the engine rejects an
		// unknown query name at tokenize time, so an unresolved query read
		// is not a state its evaluator can be in. See eval/unresolved.go.
		return func(ctx *Context) float64 { return Round32(ctx.Scope.Query[member]) }
	case ast.Variable:
		return c.resolvedRead(id.Namespace, member, func(ctx *Context) map[string]float64 { return ctx.Scope.Variable })
	case ast.Temp:
		return c.resolvedRead(id.Namespace, member, func(ctx *Context) map[string]float64 { return ctx.Scope.Temp })
	case ast.Context:
		// context.* has no backing scope of its own (nothing in worldgen
		// populates it), so it is read through the query bag. It DOES
		// participate in the
		// unresolved-read mechanism -- it is one of the namespaces backed
		// by that same storage, unlike query.
		return c.resolvedRead(id.Namespace, member, func(ctx *Context) map[string]float64 { return ctx.Scope.Query })
	case ast.Geometry, ast.Material, ast.Texture:
		// A resource is a NAME, not a number. It goes through the same
		// intern table as a string literal, which is what makes
		// `texture.a == texture.b` and `cond ? texture.a : texture.b`
		// work without a second value type in the evaluator.
		//
		// The id is stable for a given name within a process and carries
		// no meaning beyond identity -- do not do arithmetic on it, which
		// is why the parser refuses to let you.
		full := id.Namespace.String() + "." + member
		v := internString(full)
		return func(*Context) float64 { return v }
	}
	c.fail("compile: unknown namespace %v", id.Namespace)
	panic("unreachable")
}

// resolvedRead compiles one namespace read that participates in the
// engine's unresolved-variable mechanism: present key -> its (re-rounded)
// value, absent key -> Context.unresolved, which hands it to the nearest
// enclosing `??` and, if there is none, reports it and either aborts the
// program (the default) or substitutes 0 and continues.
//
// Compiling one of these is what sets canAbort, and therefore what decides
// whether Compile installs the catch root at all.
func (c *compiler) resolvedRead(ns ast.Namespace, member string, bag func(*Context) map[string]float64) exprFn {
	c.canAbort = true
	name := ns.String() + "." + member
	return func(ctx *Context) float64 {
		v, ok := bag(ctx)[member]
		if !ok {
			return ctx.unresolved(name)
		}
		return Round32(v)
	}
}

func (c *compiler) compileCall(call *ast.CallExpr) exprFn {
	member := strings.ToLower(call.Callee.Member)
	switch call.Callee.Namespace {
	case ast.Math:
		return c.compileMathCall(call, member)
	case ast.Query:
		return c.compileNamespaceCall(call, member, func(ctx *Context) map[string]float64 { return ctx.Scope.Query })
	case ast.Variable, ast.Temp, ast.Context:
		return c.compileNamespaceCall(call, member, c.bagFor(call.Callee.Namespace))
	}
	c.fail("compile: unknown namespace %v", call.Callee.Namespace)
	panic("unreachable")
}

func (c *compiler) compileMathCall(call *ast.CallExpr, member string) exprFn {
	if member == "pi" {
		if len(call.Args) != 0 {
			c.fail("math.pi takes no arguments")
		}
		return func(*Context) float64 { return mathPiF32 }
	}
	arity, ok := MathArity[member]
	if !ok {
		c.fail("unknown math function 'math.%s'", call.Callee.Member)
	}
	if len(call.Args) != arity {
		c.fail("math.%s expects %d argument(s), got %d", member, arity, len(call.Args))
	}
	fn := mathTable[member]
	argFns := make([]exprFn, len(call.Args))
	for i, a := range call.Args {
		argFns[i] = c.compileExpr(a)
	}
	return func(ctx *Context) float64 {
		args := make([]float64, len(argFns))
		for i, af := range argFns {
			args[i] = af(ctx)
		}
		// mathTable functions already round internally at every step (see
		// math.go); Round32 here is a defensive no-op that guards against
		// any future math.* implementation that forgets to.
		return Round32(fn(ctx.RNG, args))
	}
}

// compileNamespaceCall implements Bedrock's real behavior for call syntax
// on a non-math/unregistered-function name: evaluate the arguments in
// order (so RNG draws and nested calls inside them happen at the right
// point in the sequence), discard the results, and return the plain
// lookup — unless a QueryFunc has been registered for this name in the
// Context at run time, in which case it is called for real.
//
// The fallback read for a temp./variable./context. "call" participates in
// the unresolved-read mechanism exactly like the bare read of the same
// name does, so `v.x` and `v.x()` cannot mean different things. That
// consistency is INFERRED, not confirmed: `v.foo(1)` is not a shape the
// engine's grammar has an opcode for, and no case for it was found in the
// engine's Molang program builder, so which instruction sequence it
// compiles to (and hence whether the read is an ordinary namespace-read
// instruction at all) is unknown. Nothing in the golden
// corpus uses the shape.
func (c *compiler) compileNamespaceCall(call *ast.CallExpr, member string, bag func(*Context) map[string]float64) exprFn {
	argFns := make([]exprFn, len(call.Args))
	for i, a := range call.Args {
		argFns[i] = c.compileExpr(a)
	}
	isQuery := call.Callee.Namespace == ast.Query
	name := call.Callee.Namespace.String() + "." + member
	if !isQuery {
		c.canAbort = true
	}
	return func(ctx *Context) float64 {
		args := make([]float64, len(argFns))
		for i, af := range argFns {
			args[i] = af(ctx)
		}
		if isQuery && ctx.QueryFuncs != nil {
			if qf, ok := ctx.QueryFuncs[member]; ok {
				// QueryFuncs are host-supplied (e.g. worldgen.Register's
				// query.noise/heightmap/has_biome_tag) and not otherwise
				// constrained to round their own output, so this boundary
				// -- like the namespace-bag reads below and in
				// compileIdentRead -- is where a value re-enters the
				// Molang value stack and must become float32.
				return Round32(qf(args, ctx))
			}
		}
		v, ok := bag(ctx)[member]
		if !ok && !isQuery {
			return ctx.unresolved(name)
		}
		return Round32(v)
	}
}

// bagFor returns the scope-bag accessor for a namespace that has one.
// Shared by compileCall so the call path and compileIdentRead cannot drift
// about which bag context. reads through.
func (c *compiler) bagFor(ns ast.Namespace) func(*Context) map[string]float64 {
	switch ns {
	case ast.Variable:
		return func(ctx *Context) map[string]float64 { return ctx.Scope.Variable }
	case ast.Temp:
		return func(ctx *Context) map[string]float64 { return ctx.Scope.Temp }
	case ast.Context, ast.Query:
		return func(ctx *Context) map[string]float64 { return ctx.Scope.Query }
	}
	c.fail("compile: namespace %v has no scope bag", ns)
	panic("unreachable")
}
