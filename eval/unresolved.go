package eval

// Unresolved variable reads: the engine's catch-stack mechanism, and what
// `??` actually is.
//
// CONFIRMED. `??` is NOT NaN-coalescing and it is not null-coalescing. It
// is a try/catch scoped to its left-hand side, and the thing it catches is
// "a variable read found no value".
//
// `??` installs a catch frame around its left-hand side, before that side
// runs, and removes it the moment the side completes. A read that finds no
// value looks for the innermost frame: if there is one, evaluation resumes at
// the right-hand side; if there is none, the whole expression stops. Every
// namespace read participates -- variable., temp., context., the dotted
// member form, and the actor-scoped variants of each.
//
// A failing read is reported by name, matching what the game logs for it, and
// it always leaves 0 as the read's own value whether or not anything catches
// it.
//
// So there are two behaviours, and the second is as load-bearing as the
// first:
//
//	(a) `??` catches an unresolved read in its LHS and evaluates the RHS.
//	    `v.unset ?? 5` is 5. `v.x = 0; v.x ?? 5` is 0 -- a RESOLVED zero
//	    does not divert. The engine distinguishes "no value" from "the
//	    value 0"; this package previously could not.
//
//	(b) An unresolved read with NO enclosing `??` ABORTS the program.
//	    The missing-variable handler returns -1, the read instruction
//	    stores it into the unsigned pc, and the interpreter's own bounds
//	    check on the program counter fails immediately. The already-stored
//	    0 is the program's value, so "an unresolved read is 0" still holds
//	    for the RESULT -- but everything sequenced after the failing read
//	    is SKIPPED. That matters most for RNG draw order: draws downstream
//	    of a failed read do not happen in the engine.
//
// query. does NOT participate, in either direction. An unknown query name
// is rejected at TOKENIZE time, with a "Failed to resolve query" error, so
// `q.unknown ?? x` never reaches the program builder at all -- it fails to
// compile. A query. read that does compile always resolves.
// This package has no query registry (QueryFuncs is caller-supplied and
// open-ended), so it cannot reproduce the tokenize-time rejection; it
// keeps the old "missing query. member reads 0" instead, which is the
// closest reachable behaviour and never aborts.
//
// ---------------------------------------------------------------------
// How that is done here, and what it costs
// ---------------------------------------------------------------------
//
// The engine aborts by writing -1 into its program counter, which a Go
// expression tree has no equivalent of: an exprFn returns a float64 and has
// nowhere to put "and stop". So a failing read panics with an unexported
// sentinel, and exactly two places recover it -- catchUnresolved, which is
// `??`, and catchRoot, which is the program. Both re-panic anything that is
// not the sentinel, so a real bug still reaches the host with its own value
// and stack.
//
// The costs, stated rather than buried:
//
//   - panic/recover is far slower than a branch. It is paid only when a read
//     actually fails, which in a working pack is never.
//   - It is invisible in the type signatures. Nothing about exprFn says an
//     evaluation can stop early, which is why this comment exists.
//
// What Run reports is unchanged: an aborted program returns 0, which is both
// what the engine leaves in its result slot and what this package returned
// before. The observable difference is everything the abort skipped --
// assignments not performed, RNG draws not consumed.
//
// ---------------------------------------------------------------------
// Opting out: Context.ContinueOnUnresolvedRead
// ---------------------------------------------------------------------
//
// The abort is faithful GIVEN THE SAME SCOPE, and the scope is the part a
// host may not be able to supply. A tool that previews ONE feature in
// isolation starts from an empty scope, so a temp./variable. slot a parent
// would have written is unset here and set in the real chain. Every such read
// then ends its program at the first line and the tool shows nothing, for a
// reason that belongs to the tool rather than to the pack.
//
// The option changes ONE thing: an uncaught failing read returns 0 and the
// program continues instead of stopping. It is off by default, so a caller
// that says nothing gets the engine. It does NOT change `??`, which behaves
// identically under both settings.
//
// DO NOT simplify it to "if opted out, do not raise the sentinel". That
// breaks `??`: with nothing raised, `v.unset ?? 5` sees its LHS complete
// normally with 0 and yields 0 instead of 5. Nor can the sentinel be raised
// and then resumed -- once a panic has unwound, the continuation is gone. The
// decision has to be made AT THE READ, and it needs the one fact that
// separates the cases: is a `??` waiting? That is the same question the
// engine's own handler asks of its catch stack, so this package keeps the
// same counter -- Context.catchDepth, pushed and popped by catchUnresolved.
// The option is consulted only in the empty-stack case.
//
// catchDepth lives on the Context because a Context is one evaluation's
// state, already caller-owned and already non-concurrent. Program.Run resets
// it on entry and restores it on exit, so a program run from inside a host
// QueryFunc that was itself called from inside a `??`'s LHS does not mistake
// the outer program's catch frame for one of its own.

// returnSignal carries a `return` raised from inside an expression out to the
// program root. See compileExpr's CondBlockStmt case for when that happens and
// why it cannot be a return value instead.
//
// It is a distinct type from unresolvedRead on purpose: catchUnresolved must
// re-raise it rather than treat it as a caught read, and a shared type would
// make `(v.a ?? 1)` swallow a return that passed through it.
type returnSignal struct {
	value float64
}

// unresolvedRead is the sentinel value panicked by a namespace read that
// found no value in its bag. It is unexported and has no exported
// constructor, so nothing outside this package can fabricate one, and the
// two recover sites re-panic anything that is not this type -- a real bug
// panicking through an evaluated program still reaches the host with its
// original value and stack.
type unresolvedRead struct {
	// name is the "<namespace>.<member>" the read was for, matching the
	// name the engine puts in its own "unhandled request for unknown
	// variable '%s'" content-log line. Carried for debuggability only:
	// nothing branches on it, because the engine's catch stack does not
	// either -- any unresolved read is caught by any enclosing `??`.
	name string
}

// catchUnresolved runs fn and reports whether it completed without hitting
// an unresolved read. This is the engine's catch-frame pair: the frame
// `??` installs around its LHS, pushed before the LHS runs and popped the
// moment the LHS completes -- however it completes, which is why the pop
// is in the defer rather than after the call.
//
// The push/pop is what a failing read consults (Context.unresolved), so it
// has to happen even for a Context that has opted out of aborting: `??`
// works identically under both settings, and this is the code that makes
// that true.
//
// It deliberately does NOT catch anything else. A non-sentinel panic is
// re-panicked with its original value so a genuine bug in a host QueryFunc
// (or in this package) is never quietly converted into "the left side was
// unresolved, take the right side".
func catchUnresolved(fn exprFn, ctx *Context) (v float64, resolved bool) {
	ctx.catchDepth++
	defer func() {
		ctx.catchDepth--
		r := recover()
		if r == nil {
			return
		}
		if _, ok := r.(unresolvedRead); ok {
			v, resolved = 0, false
			return
		}
		panic(r)
	}()
	return fn(ctx), true
}

// unresolved is what a temp./variable./context. read calls when its bag has
// no entry for the name. It is the whole of this package's
// missing-variable handler: consult the catch stack, and act on whether it
// is empty.
//
//   - Not empty (catchDepth > 0): a `??` is waiting, so raise the sentinel
//     and let catchUnresolved above have it. Nothing is reported -- a
//     diverted read is ordinary control flow, and the engine logs nothing
//     for it either.
//   - Empty: this is the read the engine content-logs about. Report it to
//     OnUnresolvedRead if the host is listening, then either raise the
//     sentinel for catchRoot to stop the program on (the default, and the
//     engine's pc = -1) or -- for a host that has opted out -- return the 0
//     the read had already substituted and let evaluation carry on.
//
// Never returns to its caller under the default; the returned 0 is only
// ever reached under ContinueOnUnresolvedRead.
func (ctx *Context) unresolved(name string) float64 {
	if ctx.catchDepth > 0 {
		panic(unresolvedRead{name: name})
	}
	if ctx.OnUnresolvedRead != nil {
		ctx.OnUnresolvedRead(name)
	}
	if !ctx.ContinueOnUnresolvedRead {
		panic(unresolvedRead{name: name})
	}
	return 0
}

// catchRoot wraps a whole compiled program's run function with the bottom
// of the engine's catch stack. It does two things, both per Run:
//
//   - Resets catchDepth for the duration of this run and restores it after,
//     so a nested Run (a program evaluated from inside a host QueryFunc)
//     gets its own catch stack rather than inheriting the caller's open
//     `??` frames. In the engine each program has its own program counter
//     and its own eval params; here that is one saved int.
//   - Implements "pc = -1, stop": a sentinel that reached the top with no
//     `??` to catch it ends the program, and its value is the 0 the read
//     had already stored.
//
// Only installed when the compiled tree contains at least one participating
// read -- see compiler.canAbort. Installed regardless of
// ContinueOnUnresolvedRead: the setting lives on the Context, which is not
// known until Run, and a compiled Program is meant to be runnable against
// any Context.
func catchRoot(run func(ctx *Context) float64) func(ctx *Context) float64 {
	return func(ctx *Context) (result float64) {
		outer := ctx.catchDepth
		ctx.catchDepth = 0
		defer func() {
			ctx.catchDepth = outer
			r := recover()
			if r == nil {
				return
			}
			if _, ok := r.(unresolvedRead); ok {
				result = 0
				return
			}
			panic(r)
		}()
		return run(ctx)
	}
}
