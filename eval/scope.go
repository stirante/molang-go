package eval

import "math"

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
//
// An expression gets hold of one as a VALUE -- an entity reference -- in one
// of three ways, and can then read through it with `->`, store it in a
// variable or temp, pass it around, and compare it with `==`:
//
//   - a `context.<name>` read where Context.Entities has that name;
//   - the loop variable of a for_each over an entity array, which a host
//     query returns with Context.EntityArrayRef;
//   - a host QueryFunc returning Context.EntityRef(e) directly.
//
// The reference is an ordinary float64 as far as the language is concerned,
// which is how the game treats it too: a variable holds whatever it was
// last assigned, a number or an entity, and an arrow whose left side turns
// out not to be an entity reads 0. Doing arithmetic on one is meaningless
// and gives a meaningless number; the game's answer to that is not modelled.
type Entity struct {
	// Variable holds what the entity has PUBLISHED. In game only a variable
	// the entity marked public can be read by others: a private one, and a
	// name it never set, both read 0 through `->` without ending the
	// expression -- the missing-variable abort applies to an entity's own
	// reads, not to reads through an arrow. So a host that models
	// visibility should put the published snapshot here rather than the
	// live map, and leave out what is private.
	Variable map[string]float64

	// QueryFuncs answers query. reads and calls made through the arrow. A
	// name with no function here falls back to Context.QueryFuncs, which
	// can tell who is being asked from Context.CurrentEntity, and a name
	// with neither reads 0.
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

	// Entities binds context. names to entities: a read of `context.<key>`
	// yields a reference to the entity, which is what `c.other->v.x` and
	// `v.e = c.other;` need. A name with no entry here reads through the
	// query bag as any other context. name does, and an entry the host
	// supplies as nil reads as not-an-entity, so an arrow through it is 0.
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

	// cur is the entity whose scope the right side of an arrow is being
	// evaluated in, or nil for the expression's own. See CurrentEntity.
	cur *Entity

	// The entity reference tables -- see EntityRef and EntityArrayRef. A
	// reference is an index into these, encoded as a number; the tables are
	// what turn it back into an entity.
	entities     []*Entity
	entityIndex  map[*Entity]int
	entityArrays [][]*Entity
}

// ---------------------------------------------------------------------
// Entity references
//
// The game's value type can hold an entity, and an expression passes one
// around like a number: `v.e = c.other;` stores it, `v.e->q.health` reads
// through it, `t.e == v.e` compares it. This package's value type is float64,
// so an entity is carried the same way a string literal is: as a numeric
// handle no genuine number reaches, decoded back through a table on the
// Context that produced it.
//
// The handle ranges are chosen so every handle is an exact float32 integer
// and survives Round32 unchanged, and so they cannot collide with each other
// or with string ids (which live in [-(2^24 - 1), -2^23]):
//
//	entity k        -> -(2^24) - 2*(k+1)  : even integers in (-2^25, -2^24)
//	entity array k  -> -(2^25) - 4*(k+1)  : multiples of 4 in (-2^26, -2^25)
//
// float32 represents every even integer below 2^25 in magnitude and every
// multiple of 4 below 2^26, so both ranges round-trip exactly. A handle is
// only meaningful to the Context that made it; one stored into a variable and
// read back in a later Run of the same Context still resolves, which is what
// a pack that remembers a target across evaluations relies on.
// ---------------------------------------------------------------------

const (
	entityHandleBase = -(1 << 24)
	entityHandleStep = 2
	entityArrayBase  = -(1 << 25)
	entityArrayStep  = 4
)

// EntityRef returns the reference an expression uses for e: the value a
// `context.` name bound to e reads as, the value for_each writes to its loop
// variable, and the value a host QueryFunc returns to hand an entity to the
// expression. The same entity always gets the same reference within a
// Context. A nil entity is not an entity, and gets 0.
func (ctx *Context) EntityRef(e *Entity) float64 {
	if e == nil {
		return 0
	}
	if k, ok := ctx.entityIndex[e]; ok {
		return float64(entityHandleBase - entityHandleStep*(k+1))
	}
	if ctx.entityIndex == nil {
		ctx.entityIndex = map[*Entity]int{}
	}
	k := len(ctx.entities)
	ctx.entities = append(ctx.entities, e)
	ctx.entityIndex[e] = k
	return float64(entityHandleBase - entityHandleStep*(k+1))
}

// EntityArrayRef returns a reference to a list of entities -- what a host
// query such as `query.get_nearby_entities` returns, and the one thing a
// for_each walks: each pass writes EntityRef of the next element to the loop
// variable. Every call makes a new reference, even for an equal list.
func (ctx *Context) EntityArrayRef(es []*Entity) float64 {
	k := len(ctx.entityArrays)
	ctx.entityArrays = append(ctx.entityArrays, es)
	return float64(entityArrayBase - entityArrayStep*(k+1))
}

// EntityOf decodes a value back to the entity it references, or nil when the
// value is not an entity reference made by this Context -- a plain number, a
// string, an array reference, or a reference from another Context.
func (ctx *Context) EntityOf(v float64) *Entity {
	k, ok := decodeHandle(v, entityHandleBase, entityHandleStep, len(ctx.entities))
	if !ok {
		return nil
	}
	return ctx.entities[k]
}

// EntityArrayOf decodes a value back to the entity list it references. The
// second result is false when the value is not an entity array reference.
func (ctx *Context) EntityArrayOf(v float64) ([]*Entity, bool) {
	k, ok := decodeHandle(v, entityArrayBase, entityArrayStep, len(ctx.entityArrays))
	if !ok {
		return nil, false
	}
	return ctx.entityArrays[k], true
}

func decodeHandle(v float64, base, step, n int) (int, bool) {
	if v >= float64(base) || v != math.Trunc(v) {
		return 0, false
	}
	off := float64(base) - v
	if off > float64(step*n) {
		return 0, false
	}
	k := int(off)/step - 1
	if float64(step*(k+1)) != off || k < 0 || k >= n {
		return 0, false
	}
	return k, true
}

// ClearEntityRefs forgets every entity and entity-array reference this
// Context has handed out, so a long-lived Context does not keep every entity
// it ever saw alive. Afterwards a reference still held in a variable decodes
// as not-an-entity, and an arrow through it reads 0 -- the same as the game
// gives for an entity that has since been removed.
func (ctx *Context) ClearEntityRefs() {
	ctx.entities, ctx.entityIndex, ctx.entityArrays = nil, nil, nil
}

// CurrentEntity is the entity whose variables and queries a read resolves
// against right now: the one on the left of the arrow while its right side
// is being evaluated, and nil the rest of the time. A QueryFunc registered in
// Context.QueryFuncs consults it to answer for the right entity.
func (ctx *Context) CurrentEntity() *Entity { return ctx.cur }

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
