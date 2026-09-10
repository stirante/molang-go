package eval

// Scope holds the temp./variable./query. numeric bags a Program reads and
// writes. It is always caller-owned: never global, and expected to outlive
// a single Run() call so callers can thread it through a chain of nested
// evaluations (this is how real packs pass values between features via
// temp.*/variable.*).
//
// Namespace member lookups are case-insensitive; keys stored here should
// always be the lower-cased member name (see Context helpers below, or just
// use NewScope which returns empty maps ready to be written through the
// normal namespace-qualified accessors).
type Scope struct {
	Temp     map[string]float64
	Variable map[string]float64
	Query    map[string]float64
	// Array holds the host's arrays, the backing for `array.<name>[i]`.
	// Molang cannot declare one; a pack reads arrays the host registered.
	// An unregistered name, and an empty array, both read as 0.
	Array map[string][]float64
}

// NewScope returns an empty, ready-to-use Scope.
func NewScope() *Scope {
	return &Scope{
		Temp:     make(map[string]float64),
		Variable: make(map[string]float64),
		Query:    make(map[string]float64),
		Array:    make(map[string][]float64),
	}
}

// QueryFunc is a caller-registered implementation of a query.<name>(...)
// call. Args have already been evaluated, left to right (so RNG draws and
// nested query calls inside them happen in source order regardless of
// whether the function ends up using every argument).
//
// This is the extension point a consuming project (e.g. a worldgen tool
// wanting query.noise/query.has_biome_tag) hooks into; the core library
// itself ships none. A query.<name>(...) call whose name has no registered
// QueryFunc falls back to Bedrock's own real behavior for calling a
// non-function namespace member: evaluate the arguments (so RNG order stays
// correct), discard them, and return the plain query.<name> lookup.
type QueryFunc func(args []float64, ctx *Context) float64

// Entity is another entity an expression can reach with `->`.
//
// Only what `->` can read is here: the entity's public variables, and the
// queries that answer about it. A host that has richer objects hands over a
// view of them rather than the objects themselves, which keeps this package
// free of any notion of what an entity is.
type Entity struct {
	// Variable holds what the entity has published. In game an entity's
	// variables are not visible to others until it publishes them, so a
	// host that models that should put the published snapshot here rather
	// than the live map.
	Variable map[string]float64

	// QueryFuncs answers query. reads made through the arrow. A name with
	// no function here reads 0.
	QueryFuncs map[string]QueryFunc
}

// Context is everything a compiled Program needs to run once.
type Context struct {
	RNG   RNG
	Scope *Scope
	// This is the value the bare keyword `this` reads. Zero unless the host
	// sets it, which is the right default: an expression using `this` in a
	// context that has no such value gets 0 rather than an error, the same
	// as every other absent host input here.
	This float64

	// Entities holds the other entities an expression can reach with `->`.
	// The key is the context. member naming one; a name with no entity, and
	// an entity the host supplies as nil, both read 0.
	Entities map[string]*Entity

	// QueryFuncs optionally maps a query.<name> member (lower-case) to a
	// QueryFunc implementation. Nil/absent entries fall back to the
	// evaluate-args-and-discard read described on QueryFunc.
	QueryFuncs map[string]QueryFunc

	// ContinueOnUnresolvedRead opts OUT of the engine's "an unresolved
	// temp./variable./context. read that nothing catches ends the program"
	// behaviour: the read still yields 0, but evaluation CONTINUES past it,
	// so the assignments and RNG draws sequenced after it still happen.
	//
	// The zero value is the engine's own behaviour -- a caller that says
	// nothing gets the abort. Setting this is a deliberate, host-visible
	// divergence and is only ever right for a host that cannot supply the
	// scope the read expects (a preview tool evaluating one expression out
	// of the chain that would have written the slot); see eval/unresolved.go
	// under "Opting out" for the whole argument, including why `??` is
	// unaffected either way.
	ContinueOnUnresolvedRead bool

	// OnUnresolvedRead, if non-nil, is called with the "<namespace>.<member>"
	// name of every unresolved read that NOTHING CATCHES -- exactly the reads
	// the engine content-logs "Error: unhandled request for unknown variable
	// '%s'" for, and exactly the reads that abort (or, under
	// ContinueOnUnresolvedRead, are swallowed). A read an enclosing `??`
	// diverts is ordinary control flow, not a diagnostic, and never reported.
	//
	// It fires under BOTH settings of ContinueOnUnresolvedRead: the callback
	// says "this read found nothing and no `??` covered it", which is a fact
	// about the program either way. It is called once per occurrence, with no
	// deduplication -- a read inside a loop reports once per iteration, so a
	// host that surfaces these to a user is responsible for collapsing
	// repeats.
	OnUnresolvedRead func(name string)

	// catchDepth is how many `??` catch frames are currently open around the
	// expression being evaluated -- this package's stand-in for the engine's
	// own catch stack. Read at every failing read to answer the one
	// question the engine's missing-variable handler asks: is the stack
	// empty? See eval/unresolved.go.
	catchDepth int
}

// ---------------------------------------------------------------------
// String interning
//
// Molang has no string type; general string literals ('lush_caves' etc.)
// are only ever meaningful for round-tripping through temp./variable. slots
// via =/==/!=. Each distinct string is interned to a stable numeric id, far
// outside any range a real gameplay/molang numeric literal would plausibly
// use, so two occurrences of the same literal (or a literal compared
// against a value that came back out of scope storage) compare equal.
//
// The id is a pure function of the string's content (FNV-1a 64 over its
// UTF-8 bytes, folded into 23 bits and offset below internBase) rather
// than an order-of-first-use counter. A per-process sequential counter
// made the id -- and therefore any recorded Molang scope value derived
// from it -- depend on which strings had already been compiled earlier in
// the process, which differs any time a caller compiles a different
// subset of the corpus. A content hash is stable regardless of compile
// order or which other strings exist in the same process.
//
// 23 bits, not the wider range this scheme used before the float32
// rounding pass: Molang's value type is float32 (see eval/float32.go's
// Round32 doc comment), and float32 only carries 24 bits of exact-integer
// range (23 explicit mantissa bits + 1 implicit). internBase/internMask
// are sized so every id's magnitude stays under 2**24 -- i.e. every
// interned id is an EXACT float32 integer, with zero rounding error ever
// introduced by Round32 itself. This is a property this package invented
// (Molang has no string type; interning to a numeric id is purely this
// port's implementation strategy for round-tripping string literals
// through ==/!=), not something pinned against the engine binary, so it
// was free to redesign for float32 exactness rather than needing to
// preserve the old (wider, float64-only-safe) range.
// ---------------------------------------------------------------------

const internBase = -(1 << 23)
const internMask = (1 << 23) - 1

// InternString returns the stable numeric id a string literal with this
// exact content evaluates to (see the package doc comment above). Exported
// so callers building tooling around interned "strings" — e.g. an
// extension's biome-tag matcher — can compute the same id a compiled
// Program's StringLit would. Always float32-exact (see internBase's doc
// comment), so Round32 is a no-op here, applied for consistency with the
// rest of this package's "every value funnels through Round32" contract.
func InternString(s string) float64 { return internString(s) }

func internString(s string) float64 {
	h := uint64(0xcbf29ce484222325)
	for i := 0; i < len(s); i++ {
		h ^= uint64(s[i])
		h *= 0x100000001b3
	}
	return Round32(internBase - float64(h&internMask))
}
