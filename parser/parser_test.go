package parser_test

import (
	"reflect"
	"strings"
	"testing"

	"github.com/stirante/molang-go/ast"
	"github.com/stirante/molang-go/parser"
)

// Note: math arity/unknown-function/bare-without-call checks are performed
// by eval.Compile, not the parser — Format/Minify/transform need to work on
// syntactically valid ASTs regardless of whether every math.* reference
// makes semantic sense (e.g. a macro call like math.bitshift(x, n) parses
// fine here and is only meaningful once expanded). See eval package tests
// for those checks.
func TestParseErrors(t *testing.T) {
	cases := []string{
		"",
		" ",
		";1;2;",       // leading ';'
		"foo.bar",     // unknown namespace
		"query.x = 1", // invalid assignment target
		"1 +",         // dangling operand
		"loop(5 { })", // missing comma
		"7 % 3",       // no modulo operator
		"this.some",   // `this` is a value, not a namespace
		// `->`: the right side must be a variable or query read, one arrow
		// only, no writing through it, and not on the left of `??`.
		"c.a->t.b",
		"c.a->math.pi",
		"c.a->v.b.c",
		"c.a->v.b->v.c",
		"c.a->v.b = 1;",
		"c.a->v.b ?? 0",
		"c.a->",
		// `??`: only a bare context./variable./temp. read on the left. It
		// groups to the left and after the conditional, so a chain and a
		// `??` inside a conditional's arm are refused too.
		"3 ?? 5",
		"q.a ?? 1",
		"v.a.b ?? 1",
		"-v.a ?? 1",
		"v.a + 0 ?? 1",
		"v.a ?? v.b ?? 1",
		"v.a || v.b ?? 1",
		"v.a ? v.b ?? 1 : 2",
		"v.a ? 1 : v.b ?? 2",
	}
	for _, src := range cases {
		if _, err := parser.Parse(src); err == nil {
			t.Errorf("Parse(%q): expected error, got none", src)
		}
	}
}

// TestNullCoalesceGrouping pins where `??` sits: looser than the conditional
// and every operator above it, tighter than `=`, and left-grouped.
func TestNullCoalesceGrouping(t *testing.T) {
	va := &ast.Ident{Namespace: ast.Variable, Member: "a"}
	cases := []struct {
		src  string
		want ast.Expr
	}{
		{"v.a ?? 1 ? 2 : 3", &ast.BinaryExpr{Op: ast.NullCoalesce, X: va, Y: &ast.TernaryExpr{
			Cond: &ast.NumberLit{Value: 1}, Then: &ast.NumberLit{Value: 2}, Else: &ast.NumberLit{Value: 3}}}},
		{"v.a ?? 0 > 1", &ast.BinaryExpr{Op: ast.NullCoalesce, X: va, Y: &ast.BinaryExpr{
			Op: ast.CmpGt, X: &ast.NumberLit{Value: 0}, Y: &ast.NumberLit{Value: 1}}}},
		{"v.a ?? v.b || 1", &ast.BinaryExpr{Op: ast.NullCoalesce, X: va, Y: &ast.BinaryExpr{
			Op: ast.LOr, X: &ast.Ident{Namespace: ast.Variable, Member: "b"}, Y: &ast.NumberLit{Value: 1}}}},
		{"(v.a) ?? 1", &ast.BinaryExpr{Op: ast.NullCoalesce, X: va, Y: &ast.NumberLit{Value: 1}}},
		{"v.x = v.a ?? 1;", &ast.AssignExpr{Target: &ast.Ident{Namespace: ast.Variable, Member: "x"},
			Value: &ast.BinaryExpr{Op: ast.NullCoalesce, X: va, Y: &ast.NumberLit{Value: 1}}}},
	}
	for _, tc := range cases {
		prog, err := parser.Parse(tc.src)
		if err != nil {
			t.Errorf("Parse(%q): %v", tc.src, err)
			continue
		}
		got := prog.Stmts[0].(*ast.ExprStmt).X
		if !reflect.DeepEqual(got, tc.want) {
			t.Errorf("Parse(%q) = %#v, want %#v", tc.src, got, tc.want)
		}
	}
}

func TestParseAccepts(t *testing.T) {
	valid := []string{
		"3 + 4 * 2",
		"t.x=1;;t.y=2;",
		"return 5",
		"return 5;",
		"loop(5, { t.x = t.x + 1; });",
		"t.x > 3 ? { return 1; } : { return 0; };",
		"context.block_face",
		"c.block_face",
		"math.sin",            // bare math ref: syntactically fine, eval.Compile rejects it
		"math.sin(1,2)",       // wrong arity: syntactically fine
		"math.bitshift(1, 2)", // unknown to eval.Compile unless expanded, but parses fine
		// `->`: any operand on the left, a variable or query read on the right.
		"c.a->v.b",
		"v.e->q.b(1, t.x)",
		"t.e->q.b",
		"(v.a + 1)->v.b",
		"1->v.b",
		"this->v.b",
		"q.a(1)->v.b",
		"array.a[0]->v.b",
		"-c.a->v.b + 1",
		// `??`: a bare read on the left, anything on the right.
		"(v.a) ?? 1",
		"v.a ?? (v.b ?? 1)",
		"v.a ?? 1 ? 2 : 3",
		"v.a ? (v.b ?? 1) : 2",
		"math.max(v.a ?? 1, 2)",
		"t.e ?? (c.x ?? 1 ? 0 : 1)",
	}
	for _, src := range valid {
		if _, err := parser.Parse(src); err != nil {
			t.Errorf("Parse(%q): unexpected error: %v", src, err)
		}
	}
}

// The game's two semicolon rules. Inside braces a `;` is required even for a
// single statement; and an expression containing `=` or `;` anywhere must end
// with `;`.
func TestSemicolonRules(t *testing.T) {
	const (
		braceMsg   = "Brace sections must only contain semicolon-delimited expressions"
		complexMsg = "complex expressions (contains either '=' or ';') must end with a ';'"
	)
	refused := []struct{ src, msg string }{
		{"v.a=1", complexMsg},
		{"v.a = 1; v.b = 2", complexMsg},
		{"{v.a=1;}", complexMsg},
		{"q.x ? {v.a=0;}", complexMsg},
		{"q.x ? {v.a=0;} : 1", complexMsg},
		{"loop(2, {v.a=1;})", complexMsg},
		{"q.f(v.a = 5, 3)", complexMsg},
		{"1;2", complexMsg},
		{"{v.a=1};", braceMsg},
		{"q.x ? {v.a=1} : 0;", braceMsg},
		{"loop(2, {v.a=1});", braceMsg},
		{"for_each(t.x, array.a, {v.n=t.x});", braceMsg},
		{"loop(2, {});", braceMsg},
	}
	for _, c := range refused {
		_, err := parser.Parse(c.src)
		if err == nil {
			t.Errorf("Parse(%q) accepted; the game refuses it", c.src)
			continue
		}
		if !strings.Contains(err.Error(), c.msg) {
			t.Errorf("Parse(%q) error %q, want it to say %q", c.src, err, c.msg)
		}
		// Every one of them is accepted when the rules are lifted.
		if _, err := parser.ParseWith(c.src, parser.Extensions{OptionalSemicolons: true}); err != nil {
			t.Errorf("ParseWith(%q, OptionalSemicolons): %v", c.src, err)
		}
	}

	accepted := []string{
		"v.a=1;",
		"v.a = 1; v.b = 2;",
		"{v.a=1;};",
		"q.x ? {v.a=1;} : 0;",
		"q.x ? {v.a=0;};",
		"loop(2, {v.a=1;});",
		// A `;` in a nested brace section is between the outer braces too.
		"{ {v.a=1;} };",
		// The brace rule is satisfied by any `;`; the last statement's own
		// is not required.
		"{v.a=1; v.b=2};",
		// Simple expressions need nothing.
		"math.sin(q.anim_time)",
		"q.x ? 1 : 0",
		"v.a == 1",
		"v.a != 1 && v.b <= 2 && v.c >= 3",
		"'a=b;c'",
		"return 1",
	}
	for _, src := range accepted {
		if _, err := parser.Parse(src); err != nil {
			t.Errorf("Parse(%q): %v", src, err)
		}
	}
}

// A block may stand on either side of a conditional, or both, or be the only
// arm.
func TestConditionalBlockArms(t *testing.T) {
	block := func(stmts ...ast.Stmt) *ast.CondBlockStmt {
		return &ast.CondBlockStmt{Cond: &ast.BoolLit{Value: true}, Body: &ast.Block{Stmts: stmts}}
	}
	cond := &ast.Ident{Namespace: ast.Query, Member: "x"}
	assign := &ast.ExprStmt{X: &ast.AssignExpr{
		Target: &ast.Ident{Namespace: ast.Variable, Member: "a"},
		Value:  &ast.NumberLit{Value: 1},
	}}
	one := &ast.NumberLit{Value: 1}

	cases := []struct {
		src  string
		want ast.Stmt
	}{
		{"q.x ? {v.a = 1;};", &ast.CondBlockStmt{Cond: cond, Body: block(assign).Body}},
		{"q.x ? {v.a = 1;} : {v.a = 1;};", &ast.CondBlockStmt{Cond: cond, Body: block(assign).Body, Else: block(assign)}},
		{"q.x ? {v.a = 1;} : 1;", &ast.ExprStmt{X: &ast.TernaryExpr{Cond: cond, Then: block(assign), Else: one}}},
		{"q.x ? 1 : {v.a = 1;};", &ast.ExprStmt{X: &ast.TernaryExpr{Cond: cond, Then: one, Else: block(assign)}}},
		{"q.x ? {v.a = 1;} : q.x ? {v.a = 1;};", &ast.CondBlockStmt{Cond: cond, Body: block(assign).Body,
			Else: &ast.CondBlockStmt{Cond: cond, Body: block(assign).Body}}},
		{"q.x ? {v.a = 1;} : q.x ? 1 : {v.a = 1;};", &ast.ExprStmt{X: &ast.TernaryExpr{Cond: cond, Then: block(assign),
			Else: &ast.TernaryExpr{Cond: cond, Then: one, Else: block(assign)}}}},
	}
	for _, c := range cases {
		prog, err := parser.Parse(c.src)
		if err != nil {
			t.Errorf("Parse(%q): %v", c.src, err)
			continue
		}
		if len(prog.Stmts) != 1 || !reflect.DeepEqual(prog.Stmts[0], c.want) {
			t.Errorf("Parse(%q) has the wrong shape", c.src)
		}
	}

	// As a value, in an assignment.
	for _, src := range []string{
		"v.r = q.x ? {v.a = 1;} : 5;",
		"v.r = q.x ? 5 : {v.a = 1;};",
		"v.r = (q.x ? {v.a = 1;} : 5) + 1;",
	} {
		if _, err := parser.Parse(src); err != nil {
			t.Errorf("Parse(%q): %v", src, err)
		}
	}
}
