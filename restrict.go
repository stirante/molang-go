package molang

import (
	"fmt"

	"github.com/stirante/molang-go/ast"
)

// Where an expression is written decides what it may contain.
//
// Molang's grammar is the same everywhere, but a field can forbid individual
// OPERATIONS, and the engine enforces that after parsing rather than in the
// grammar: it records the set of operations the expression turned out to use
// and intersects it with the set the surrounding context allows. Anything
// left over is a load error naming the operation. The same text therefore
// loads in one field and is refused in another.
//
// This matters to anything using this package to predict whether a pack will
// load: parsing here is necessary but NOT sufficient. `math.max(v.a = 5, 3)`
// parses -- an assignment is an ordinary operand -- and still fails to load
// wherever assignment is not on the allow-list.
//
// What this package cannot model: the engine's list also numbers pure syntax
// (braces, parentheses, the comma, the member-accessor dot) and a few
// internal entries, none of which survive into an AST. A context forbidding
// one of those is out of reach here. See ast.Op.

// OpNotAllowedError reports that a program uses an operation its context
// forbids. Its message is the engine's own wording, naming the operation the
// way the engine names it, so a diagnostic from this package and one from the
// game read the same.
type OpNotAllowedError struct {
	// Op is the lowest-numbered forbidden operation the program uses -- the
	// one the message names.
	Op ast.Op
	// Ops is every forbidden operation the program uses, not just the named
	// one, so a caller reporting to an author can list them all rather than
	// making them fix one per attempt.
	Ops ast.OpSet
}

func (e *OpNotAllowedError) Error() string {
	return fmt.Sprintf("Expression uses operation %s which is not allowed in this context", e.Op)
}

// CheckOps reports whether prog uses any operation in disallowed.
//
// This is the whole of the engine's check: intersect what the expression uses
// with what the context refuses, and a non-empty result is the error. Passing
// an empty set always succeeds.
func CheckOps(prog *ast.Program, disallowed ast.OpSet) error {
	bad := ast.OpsUsed(prog).Intersect(disallowed)
	if bad.Empty() {
		return nil
	}
	return &OpNotAllowedError{Op: bad.Ops()[0], Ops: bad}
}

// SideEffectOps is the set the engine's own "disallow side effects" switch
// removes from a context's allow-list.
//
// The shape is the engine's, and it is not the shape the name suggests:
//
//   - Assignment is removed ALWAYS, whatever alsoRandom says. Turning the
//     switch on at all is what forbids `v.x = 1`.
//   - math.random and math.random_integer are removed only when alsoRandom
//     is true. That is the parameter's entire effect.
//   - math.die_roll and math.die_roll_integer are NOT removed by either,
//     even though both consume random draws exactly as math.random does.
//     That asymmetry is the engine's, reproduced here rather than tidied up:
//     a caller wanting the tidy version can Union in ast.OpDieRoll and
//     ast.OpDieRollInteger, but it would no longer describe the game.
func SideEffectOps(alsoRandom bool) ast.OpSet {
	s := ast.NewOpSet(ast.OpAssignment)
	if alsoRandom {
		s.Add(ast.OpRandom)
		s.Add(ast.OpRandomInteger)
	}
	return s
}

// DisallowSideEffects checks prog the way a context with side effects turned
// off would. It is SideEffectOps plus CheckOps, named after the engine's own
// switch because that is what a caller is looking for.
//
// Three places in the engine turn this on, and knowing which they are is more
// useful than the switch itself, because it says which authoring surfaces are
// pure by construction:
//
//   - entity property-group queries — alsoRandom FALSE, so assignment is
//     refused but math.random is still allowed;
//   - block descriptions — alsoRandom true;
//   - a block geometry's bone_visibility map — alsoRandom true.
//
// Each of those three also restricts WHICH queries may be named, through a
// registered allow-list of its own. That is a second, separate mechanism and
// this package does not model it, so a program passing this check can still
// be refused by the game for naming a query the context does not publish.
func DisallowSideEffects(prog *ast.Program, alsoRandom bool) error {
	return CheckOps(prog, SideEffectOps(alsoRandom))
}
