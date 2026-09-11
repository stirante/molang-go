package ast_test

import (
	"strings"
	"testing"

	"github.com/stirante/molang-go/ast"
	"github.com/stirante/molang-go/parser"
)

// This is an EXTERNAL test package (ast_test), so it may import the parser
// without the import cycle that would stop the ast package itself.
var parse = parser.Parse

func TestOpSetAlgebra(t *testing.T) {
	a := ast.NewOpSet(ast.OpAssignment, ast.OpRandom)
	b := ast.NewOpSet(ast.OpRandom, ast.OpLoop)

	if !a.Has(ast.OpAssignment) || !a.Has(ast.OpRandom) {
		t.Error("NewOpSet lost a member")
	}
	if a.Has(ast.OpLoop) {
		t.Error("NewOpSet invented a member")
	}
	if (ast.OpSet{}).Empty() != true {
		t.Error("the zero OpSet is not empty")
	}
	if a.Empty() {
		t.Error("a populated set reported empty")
	}

	if u := a.Union(b); !u.Has(ast.OpAssignment) || !u.Has(ast.OpRandom) || !u.Has(ast.OpLoop) {
		t.Errorf("Union = %v", u)
	}
	i := a.Intersect(b)
	if !i.Has(ast.OpRandom) || i.Has(ast.OpAssignment) || i.Has(ast.OpLoop) {
		t.Errorf("Intersect = %v", i)
	}
	w := a.Without(b)
	if !w.Has(ast.OpAssignment) || w.Has(ast.OpRandom) {
		t.Errorf("Without = %v", w)
	}
}

// Ops has to come back in ascending order, because the diagnostic names the
// lowest-numbered forbidden operation and that has to be deterministic.
func TestOpSetOpsAreAscending(t *testing.T) {
	s := ast.NewOpSet(ast.OpEaseInOutElastic, ast.OpAssignment, ast.OpAdd, ast.OpRandom)
	got := s.Ops()
	want := []ast.Op{ast.OpAdd, ast.OpRandom, ast.OpAssignment, ast.OpEaseInOutElastic}
	if len(got) != len(want) {
		t.Fatalf("Ops() = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("Ops() = %v, want %v", got, want)
		}
	}
}

// An operation number the engine does not define must not corrupt the set or
// panic -- the bitset is exactly as wide as the engine's, and a caller
// holding a number from somewhere else should get a clean "no".
func TestOutOfRangeOpIsIgnored(t *testing.T) {
	var s ast.OpSet
	s.Add(ast.Op(200))
	if !s.Empty() {
		t.Error("an out-of-range op was stored")
	}
	if s.Has(ast.Op(200)) {
		t.Error("an out-of-range op reported present")
	}
	if got := ast.Op(200).String(); got != "<unknown expression op>" {
		t.Errorf("String() = %q, want the engine's unknown-op text", got)
	}
}

// Nothing may report an operation it cannot name: the name IS the diagnostic
// a pack author reads. This walks every operation the collector can produce.
func TestEveryReportableOpHasAName(t *testing.T) {
	srcs := []string{
		"-1", "!1", "1 + 1", "1 - 1", "1 * 1", "1 / 1",
		"1 < 1", "1 <= 1", "1 > 1", "1 >= 1", "1 == 1", "1 != 1",
		"1 && 1", "1 || 1", "v.x ?? 1", "1 ? 2 : 3", "math.pi", "'s' == 's'",
		"v.a = 1", "t.a = 1", "c.x", "q.x", "array.a[0]", "this",
		"geometry.g == geometry.h", "material.m == material.n", "texture.t == texture.u",
		"c.e->v.x", "return 1;", "loop(1, { break; });", "loop(1, { continue; });",
		"for_each(t.x, array.a, { t.s = 1; });",
		"math.abs(1)", "math.acos(1)", "math.asin(1)", "math.atan(1)", "math.atan2(1,1)",
		"math.ceil(1)", "math.clamp(1,0,2)", "math.copy_sign(1,1)", "math.cos(1)",
		"math.die_roll(1,1,1)", "math.die_roll_integer(1,1,1)", "math.exp(1)",
		"math.floor(1)", "math.hermite_blend(1)", "math.inverse_lerp(0,1,1)",
		"math.lerp(0,1,1)", "math.lerprotate(0,1,1)", "math.ln(1)", "math.max(1,1)",
		"math.min(1,1)", "math.min_angle(1)", "math.mod(1,1)", "math.pow(1,1)",
		"math.random(0,1)", "math.random_integer(0,1)", "math.round(1)",
		"math.sign(1)", "math.sin(1)", "math.sqrt(1)", "math.trunc(1)",
	}
	for shape := range easeShapeNames {
		for _, v := range []string{"in", "out", "in_out"} {
			srcs = append(srcs, "math.ease_"+v+"_"+shape+"(0, 1, 0.5)")
		}
	}

	var all ast.OpSet
	for _, src := range srcs {
		prog, err := parse(src)
		if err != nil {
			t.Fatalf("parse(%q): %v", src, err)
		}
		all = all.Union(ast.OpsUsed(prog))
	}

	for _, op := range all.Ops() {
		name := op.String()
		if name == "" || strings.HasPrefix(name, "<unknown") {
			t.Errorf("op %d is reportable but has no name (%q)", op, name)
		}
	}
	if len(all.Ops()) < 60 {
		t.Errorf("only %d distinct operations exercised; the corpus above has shrunk", len(all.Ops()))
	}
}

var easeShapeNames = map[string]bool{
	"quad": true, "cubic": true, "quart": true, "quint": true, "sine": true,
	"expo": true, "circ": true, "bounce": true, "back": true, "elastic": true,
}
