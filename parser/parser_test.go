package parser_test

import (
	"testing"

	"molang-go/parser"
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
	}
	for _, src := range cases {
		if _, err := parser.Parse(src); err == nil {
			t.Errorf("Parse(%q): expected error, got none", src)
		}
	}
}

func TestParseAccepts(t *testing.T) {
	valid := []string{
		"3 + 4 * 2",
		"t.x=1;;t.y=2;",
		"return 5",
		"return 5;",
		"loop(5, { t.x = t.x + 1; })",
		"t.x > 3 ? { return 1; } : { return 0; };",
		"context.block_face",
		"c.block_face",
		"math.sin",            // bare math ref: syntactically fine, eval.Compile rejects it
		"math.sin(1,2)",       // wrong arity: syntactically fine
		"math.bitshift(1, 2)", // unknown to eval.Compile unless expanded, but parses fine
	}
	for _, src := range valid {
		if _, err := parser.Parse(src); err != nil {
			t.Errorf("Parse(%q): unexpected error: %v", src, err)
		}
	}
}
