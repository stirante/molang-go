// vanilla_cases_test.go is a large table of Molang expressions with the value
// each one produces, transcribed from the game's own behaviour rather than
// reasoned about here.
//
// It is the widest check in this repository and the least clever: no shape, no
// property, just a long list of "this text produces this number". That is
// exactly its value. Every subtle behaviour this package gets right was found
// by someone noticing a specific expression, and this table is what stops any
// of them being lost again.
//
// Cases this package deliberately does not implement are absent: entity
// context and the `->` operator, arrays and for_each, resource namespaces, and
// custom query functions. Cases whose result depends on a random draw are
// absent too, because their real assertion is a range rather than a value;
// those are covered in mathlib_test.go against a scripted generator.
//
// The tolerance is the same absolute 1e-6 the values were pinned with. Where a
// case needs more than that, it is a finding rather than a rounding issue.
package molang

import (
	"math"
	"testing"
)

// vanillaTolerance is the absolute tolerance these values were pinned with.
const vanillaTolerance = 1e-6

type vanillaCase struct {
	expr string
	want float64
}

func runVanillaCases(t *testing.T, name string, cases []vanillaCase) {
	t.Helper()
	t.Run(name, func(t *testing.T) {
		for _, c := range cases {
			got, err := Eval(c.expr, newCtx())
			if err != nil {
				t.Errorf("%s: %v", c.expr, err)
				continue
			}
			if math.Abs(got-c.want) > vanillaTolerance {
				t.Errorf("%s = %v, want %v", c.expr, got, c.want)
			}
		}
	})
}

func TestVanillaCodeBlocks(t *testing.T) {
	runVanillaCases(t, "codeblocks", []vanillaCase{
		{"1 ? { variable.a = 0.1; }; return variable.a ?? 0.2;", 0.10000000149011612},
		{"0 ? { variable.a = 0.1; }; return variable.a ?? 0.2;", 0.20000000298023224},
		{"1 ? { variable.a = 0.1; variable.b = 0.2; } : { variable.a = 1.0; variable.b = 2.0; }; return variable.a + variable.b;", 0.30000001192092896},
		{"0 ? { variable.a = 0.1; variable.b = 0.2; } : { variable.a = 1.0; variable.b = 2.0; }; return variable.a + variable.b;", 3.0},
		{"1 ? { variable.a = 0.1; 1 ? { variable.a = 0.3; variable.b = 0.2; }; } : { variable.a = 1.0; variable.b = 2.0; }; return variable.a + variable.b;", 0.5},
		{"variable.a = -10.0f; variable.b = 123.0f; 1 ? { variable.a = 0.1; 0 ? { variable.a = 0.3; variable.b = 0.2; }; } : { variable.a = 1.0; variable.b = 2.0; }; return variable.a + variable.b;", 123.0999984741211},
		{"variable.a = -10.0f; variable.b = 123.0f; 0 ? { variable.a = 0.1; 1 ? { variable.a = 0.3; variable.b = 0.2; }; } : { variable.a = 1.0; variable.b = 2.0; }; return variable.a + variable.b;", 3.0},
	})
}

func TestVanillaCommandAliases(t *testing.T) {
	runVanillaCases(t, "commandaliases", []vanillaCase{
		{"v.a = 1.0f; return v.a ? 2.0 : 3.0;", 2.0},
		{"v.b = 1.1f; return v.b ? 2.1 : 3.1;", 2.0999999046325684},
	})
}

func TestVanillaConditionals(t *testing.T) {
	runVanillaCases(t, "conditionals", []vanillaCase{
		{"(3 ? 1 : 2) + 1", 2.0},
		{"(3 ? 1 : 2) * 2", 2.0},
		{"(3 ? 1 : 2) * 2 + 1", 3.0},
		{"(3 ? 1 : 2) * -2 + 1", -1.0},
		{"(3 ? 1 : 2) * -2 - 1", -3.0},
	})
}

func TestVanillaEaseInBack(t *testing.T) {
	runVanillaCases(t, "easeinback", []vanillaCase{
		{"math.ease_in_back(1.0, 5.0, 0.0)", 1.0},
		{"math.ease_in_back(1.0, 5.0, 0.3)", 0.6792018413543701},
		{"math.ease_in_back(1.0, 5.0, 1.0)", 5.0},
		{"v.x = 0.0; return math.ease_in_back(1.0, 5.0, v.x);", 1.0},
		{"v.x = 0.3; return math.ease_in_back(1.0, 5.0, v.x);", 0.6792018413543701},
		{"v.x = 1.0; return math.ease_in_back(1.0, 5.0, v.x);", 5.0},
	})
}

func TestVanillaEaseInBounce(t *testing.T) {
	runVanillaCases(t, "easeinbounce", []vanillaCase{
		{"math.ease_in_bounce(1.0, 5.0, 0.0)", 1.0},
		{"math.ease_in_bounce(1.0, 5.0, 0.3)", 1.2775001525878906},
		{"math.ease_in_bounce(1.0, 5.0, 1.0)", 5.0},
		{"v.x = 0.0; return math.ease_in_bounce(1.0, 5.0, v.x);", 1.0},
		{"v.x = 0.3; return math.ease_in_bounce(1.0, 5.0, v.x);", 1.2775001525878906},
		{"v.x = 1.0; return math.ease_in_bounce(1.0, 5.0, v.x);", 5.0},
	})
}

func TestVanillaEaseInCirc(t *testing.T) {
	runVanillaCases(t, "easeincirc", []vanillaCase{
		{"math.ease_in_circ(1.0, 5.0, 0.0)", 1.0},
		{"math.ease_in_circ(1.0, 5.0, 0.3)", 1.1842432022094727},
		{"math.ease_in_circ(1.0, 5.0, 1.0)", 5.0},
		{"v.x = 0.0; return math.ease_in_circ(1.0, 5.0, v.x);", 1.0},
		{"v.x = 0.3; return math.ease_in_circ(1.0, 5.0, v.x);", 1.1842432022094727},
		{"v.x = 1.0; return math.ease_in_circ(1.0, 5.0, v.x);", 5.0},
	})
}

func TestVanillaEaseInCubic(t *testing.T) {
	runVanillaCases(t, "easeincubic", []vanillaCase{
		{"math.ease_in_cubic(1.0, 5.0, 0.0)", 1.0},
		{"math.ease_in_cubic(1.0, 5.0, 0.3)", 1.1080000400543213},
		{"math.ease_in_cubic(1.0, 5.0, 1.0)", 5.0},
		{"v.x = 0.0; return math.ease_in_cubic(1.0, 5.0, v.x);", 1.0},
		{"v.x = 0.3; return math.ease_in_cubic(1.0, 5.0, v.x);", 1.1080000400543213},
		{"v.x = 1.0; return math.ease_in_cubic(1.0, 5.0, v.x);", 5.0},
	})
}

func TestVanillaEaseInElastic(t *testing.T) {
	runVanillaCases(t, "easeinelastic", []vanillaCase{
		{"math.ease_in_elastic(1.0, 5.0, 0.0)", 1.0},
		{"math.ease_in_elastic(1.0, 5.0, 0.3)", 0.9843758940696716},
		{"math.ease_in_elastic(1.0, 5.0, 1.0)", 5.0},
		{"v.x = 0.0; return math.ease_in_elastic(1.0, 5.0, v.x);", 1.0},
		{"v.x = 0.3; return math.ease_in_elastic(1.0, 5.0, v.x);", 0.9843758940696716},
		{"v.x = 1.0; return math.ease_in_elastic(1.0, 5.0, v.x);", 5.0},
	})
}

func TestVanillaEaseInExpo(t *testing.T) {
	runVanillaCases(t, "easeinexpo", []vanillaCase{
		{"math.ease_in_expo(1.0, 5.0, 0.0)", 1.00390625},
		{"math.ease_in_expo(1.0, 5.0, 0.3)", 1.03125},
		{"math.ease_in_expo(1.0, 5.0, 1.0)", 5.0},
		{"v.x = 0.0; return math.ease_in_expo(1.0, 5.0, v.x);", 1.00390625},
		{"v.x = 0.3; return math.ease_in_expo(1.0, 5.0, v.x);", 1.03125},
		{"v.x = 1.0; return math.ease_in_expo(1.0, 5.0, v.x);", 5.0},
	})
}

func TestVanillaEaseInOutBack(t *testing.T) {
	runVanillaCases(t, "easeinoutback", []vanillaCase{
		{"math.ease_in_out_back(1.0, 5.0, 0.0)", 1.0},
		{"math.ease_in_out_back(1.0, 5.0, 0.3)", 0.684666097164154},
		{"math.ease_in_out_back(1.0, 5.0, 1.0)", 5.0},
		{"v.x = 0.0; return math.ease_in_out_back(1.0, 5.0, v.x);", 1.0},
		{"v.x = 0.3; return math.ease_in_out_back(1.0, 5.0, v.x);", 0.684666097164154},
		{"v.x = 1.0; return math.ease_in_out_back(1.0, 5.0, v.x);", 5.0},
	})
}

func TestVanillaEaseInOutBounce(t *testing.T) {
	runVanillaCases(t, "easeinoutbounce", []vanillaCase{
		{"math.ease_in_out_bounce(1.0, 5.0, 0.0)", 1.0},
		{"math.ease_in_out_bounce(1.0, 5.0, 0.3)", 1.179999828338623},
		{"math.ease_in_out_bounce(1.0, 5.0, 1.0)", 5.0},
		{"v.x = 0.0; return math.ease_in_out_bounce(1.0, 5.0, v.x);", 1.0},
		{"v.x = 0.3; return math.ease_in_out_bounce(1.0, 5.0, v.x);", 1.179999828338623},
		{"v.x = 1.0; return math.ease_in_out_bounce(1.0, 5.0, v.x);", 5.0},
	})
}

func TestVanillaEaseInOutCirc(t *testing.T) {
	runVanillaCases(t, "easeinoutcirc", []vanillaCase{
		{"math.ease_in_out_circ(1.0, 5.0, 0.0)", 1.0},
		{"math.ease_in_out_circ(1.0, 5.0, 0.3)", 1.399999976158142},
		{"math.ease_in_out_circ(1.0, 5.0, 1.0)", 5.0},
		{"v.x = 0.0; return math.ease_in_out_circ(1.0, 5.0, v.x);", 1.0},
		{"v.x = 0.3; return math.ease_in_out_circ(1.0, 5.0, v.x);", 1.399999976158142},
		{"v.x = 1.0; return math.ease_in_out_circ(1.0, 5.0, v.x);", 5.0},
	})
}

func TestVanillaEaseInOutCubic(t *testing.T) {
	runVanillaCases(t, "easeinoutcubic", []vanillaCase{
		{"math.ease_in_out_cubic(1.0, 5.0, 0.0)", 1.0},
		{"math.ease_in_out_cubic(1.0, 5.0, 0.3)", 1.4320000410079956},
		{"math.ease_in_out_cubic(1.0, 5.0, 1.0)", 5.0},
		{"v.x = 0.0; return math.ease_in_out_cubic(1.0, 5.0, v.x);", 1.0},
		{"v.x = 0.3; return math.ease_in_out_cubic(1.0, 5.0, v.x);", 1.4320000410079956},
		{"v.x = 1.0; return math.ease_in_out_cubic(1.0, 5.0, v.x);", 5.0},
	})
}

func TestVanillaEaseInOutElastic(t *testing.T) {
	runVanillaCases(t, "easeinoutelastic", []vanillaCase{
		{"math.ease_in_out_elastic(1.0, 5.0, 0.0)", 1.0},
		{"math.ease_in_out_elastic(1.0, 5.0, 0.3)", 0.937503457069397},
		{"math.ease_in_out_elastic(1.0, 5.0, 1.0)", 5.0},
		{"v.x = 0.0; return math.ease_in_out_elastic(1.0, 5.0, v.x);", 1.0},
		{"v.x = 0.3; return math.ease_in_out_elastic(1.0, 5.0, v.x);", 0.937503457069397},
		{"v.x = 1.0; return math.ease_in_out_elastic(1.0, 5.0, v.x);", 5.0},
	})
}

func TestVanillaEaseInOutExpo(t *testing.T) {
	runVanillaCases(t, "easeinoutexpo", []vanillaCase{
		{"math.ease_in_out_expo(1.0, 5.0, 0.0)", 1.001953125},
		{"math.ease_in_out_expo(1.0, 5.0, 0.3)", 1.125},
		{"math.ease_in_out_expo(1.0, 5.0, 1.0)", 4.998046875},
		{"v.x = 0.0; return math.ease_in_out_expo(1.0, 5.0, v.x);", 1.001953125},
		{"v.x = 0.3; return math.ease_in_out_expo(1.0, 5.0, v.x);", 1.125},
		{"v.x = 1.0; return math.ease_in_out_expo(1.0, 5.0, v.x);", 4.998046875},
	})
}

func TestVanillaEaseInOutQuad(t *testing.T) {
	runVanillaCases(t, "easeinoutquad", []vanillaCase{
		{"math.ease_in_out_quad(1.0, 5.0, 0.0)", 1.0},
		{"math.ease_in_out_quad(1.0, 5.0, 0.3)", 1.7200000286102295},
		{"math.ease_in_out_quad(1.0, 5.0, 1.0)", 5.0},
		{"v.x = 0.0; return math.ease_in_out_quad(1.0, 5.0, v.x);", 1.0},
		{"v.x = 0.3; return math.ease_in_out_quad(1.0, 5.0, v.x);", 1.7200000286102295},
		{"v.x = 1.0; return math.ease_in_out_quad(1.0, 5.0, v.x);", 5.0},
	})
}

func TestVanillaEaseInOutQuart(t *testing.T) {
	runVanillaCases(t, "easeinoutquart", []vanillaCase{
		{"math.ease_in_out_quart(1.0, 5.0, 0.0)", 1.0},
		{"math.ease_in_out_quart(1.0, 5.0, 0.3)", 1.259200096130371},
		{"math.ease_in_out_quart(1.0, 5.0, 1.0)", 5.0},
		{"v.x = 0.0; return math.ease_in_out_quart(1.0, 5.0, v.x);", 1.0},
		{"v.x = 0.3; return math.ease_in_out_quart(1.0, 5.0, v.x);", 1.259200096130371},
		{"v.x = 1.0; return math.ease_in_out_quart(1.0, 5.0, v.x);", 5.0},
	})
}

func TestVanillaEaseInOutQuint(t *testing.T) {
	runVanillaCases(t, "easeinoutquint", []vanillaCase{
		{"math.ease_in_out_quint(1.0, 5.0, 0.0)", 1.0},
		{"math.ease_in_out_quint(1.0, 5.0, 0.3)", 1.155519962310791},
		{"math.ease_in_out_quint(1.0, 5.0, 1.0)", 5.0},
		{"v.x = 0.0; return math.ease_in_out_quint(1.0, 5.0, v.x);", 1.0},
		{"v.x = 0.3; return math.ease_in_out_quint(1.0, 5.0, v.x);", 1.155519962310791},
		{"v.x = 1.0; return math.ease_in_out_quint(1.0, 5.0, v.x);", 5.0},
	})
}

func TestVanillaEaseInOutSine(t *testing.T) {
	runVanillaCases(t, "easeinoutsine", []vanillaCase{
		{"math.ease_in_out_sine(1.0, 5.0, 0.0)", 1.0},
		{"math.ease_in_out_sine(1.0, 5.0, 0.3)", 1.8243674039840698},
		{"math.ease_in_out_sine(1.0, 5.0, 1.0)", 5.0},
		{"v.x = 0.0; return math.ease_in_out_sine(1.0, 5.0, v.x);", 1.0},
		{"v.x = 0.3; return math.ease_in_out_sine(1.0, 5.0, v.x);", 1.8243674039840698},
		{"v.x = 1.0; return math.ease_in_out_sine(1.0, 5.0, v.x);", 5.0},
	})
}

func TestVanillaEaseInQuad(t *testing.T) {
	runVanillaCases(t, "easeinquad", []vanillaCase{
		{"math.ease_in_quad(1.0, 5.0, 0.0)", 1.0},
		{"math.ease_in_quad(1.0, 5.0, 0.3)", 1.3600000143051147},
		{"math.ease_in_quad(1.0, 5.0, 1.0)", 5.0},
		{"v.x = 0.0; return math.ease_in_quad(1.0, 5.0, v.x);", 1.0},
		{"v.x = 0.3; return math.ease_in_quad(1.0, 5.0, v.x);", 1.3600000143051147},
		{"v.x = 1.0; return math.ease_in_quad(1.0, 5.0, v.x);", 5.0},
	})
}

func TestVanillaEaseInQuart(t *testing.T) {
	runVanillaCases(t, "easeinquart", []vanillaCase{
		{"math.ease_in_quart(1.0, 5.0, 0.0)", 1.0},
		{"math.ease_in_quart(1.0, 5.0, 0.3)", 1.0324000120162964},
		{"math.ease_in_quart(1.0, 5.0, 1.0)", 5.0},
		{"v.x = 0.0; return math.ease_in_quart(1.0, 5.0, v.x);", 1.0},
		{"v.x = 0.3; return math.ease_in_quart(1.0, 5.0, v.x);", 1.0324000120162964},
		{"v.x = 1.0; return math.ease_in_quart(1.0, 5.0, v.x);", 5.0},
	})
}

func TestVanillaEaseInQuint(t *testing.T) {
	runVanillaCases(t, "easeinquint", []vanillaCase{
		{"math.ease_in_quint(1.0, 5.0, 0.0)", 1.0},
		{"math.ease_in_quint(1.0, 5.0, 0.3)", 1.009719967842102},
		{"math.ease_in_quint(1.0, 5.0, 1.0)", 5.0},
		{"v.x = 0.0; return math.ease_in_quint(1.0, 5.0, v.x);", 1.0},
		{"v.x = 0.3; return math.ease_in_quint(1.0, 5.0, v.x);", 1.009719967842102},
		{"v.x = 1.0; return math.ease_in_quint(1.0, 5.0, v.x);", 5.0},
	})
}

func TestVanillaEaseInSine(t *testing.T) {
	runVanillaCases(t, "easeinsine", []vanillaCase{
		{"math.ease_in_sine(1.0, 5.0, 0.0)", 1.0},
		{"math.ease_in_sine(1.0, 5.0, 0.3)", 1.435939073562622},
		{"math.ease_in_sine(1.0, 5.0, 1.0)", 5.0},
		{"v.x = 0.0; return math.ease_in_sine(1.0, 5.0, v.x);", 1.0},
		{"v.x = 0.3; return math.ease_in_sine(1.0, 5.0, v.x);", 1.435939073562622},
		{"v.x = 1.0; return math.ease_in_sine(1.0, 5.0, v.x);", 5.0},
	})
}

func TestVanillaEaseOutBack(t *testing.T) {
	runVanillaCases(t, "easeoutback", []vanillaCase{
		{"math.ease_out_back(1.0, 5.0, 0.0)", 1.0},
		{"math.ease_out_back(1.0, 5.0, 0.3)", 4.628529071807861},
		{"math.ease_out_back(1.0, 5.0, 1.0)", 5.0},
		{"v.x = 0.0; return math.ease_out_back(1.0, 5.0, v.x);", 1.0},
		{"v.x = 0.3; return math.ease_out_back(1.0, 5.0, v.x);", 4.628529071807861},
		{"v.x = 1.0; return math.ease_out_back(1.0, 5.0, v.x);", 5.0},
	})
}

func TestVanillaEaseOutBounce(t *testing.T) {
	runVanillaCases(t, "easeoutbounce", []vanillaCase{
		{"math.ease_out_bounce(1.0, 5.0, 0.0)", 1.0},
		{"math.ease_out_bounce(1.0, 5.0, 0.3)", 3.7225003242492676},
		{"math.ease_out_bounce(1.0, 5.0, 1.0)", 5.0},
		{"v.x = 0.0; return math.ease_out_bounce(1.0, 5.0, v.x);", 1.0},
		{"v.x = 0.3; return math.ease_out_bounce(1.0, 5.0, v.x);", 3.7225003242492676},
		{"v.x = 1.0; return math.ease_out_bounce(1.0, 5.0, v.x);", 5.0},
	})
}

func TestVanillaEaseOutCirc(t *testing.T) {
	runVanillaCases(t, "easeoutcirc", []vanillaCase{
		{"math.ease_out_circ(1.0, 5.0, 0.0)", 1.0},
		{"math.ease_out_circ(1.0, 5.0, 0.3)", 3.8565714359283447},
		{"math.ease_out_circ(1.0, 5.0, 1.0)", 5.0},
		{"v.x = 0.0; return math.ease_out_circ(1.0, 5.0, v.x);", 1.0},
		{"v.x = 0.3; return math.ease_out_circ(1.0, 5.0, v.x);", 3.8565714359283447},
		{"v.x = 1.0; return math.ease_out_circ(1.0, 5.0, v.x);", 5.0},
	})
}

func TestVanillaEaseOutCubic(t *testing.T) {
	runVanillaCases(t, "easeoutcubic", []vanillaCase{
		{"math.ease_out_cubic(1.0, 5.0, 0.0)", 1.0},
		{"math.ease_out_cubic(1.0, 5.0, 0.3)", 3.628000020980835},
		{"math.ease_out_cubic(1.0, 5.0, 1.0)", 5.0},
		{"v.x = 0.0; return math.ease_out_cubic(1.0, 5.0, v.x);", 1.0},
		{"v.x = 0.3; return math.ease_out_cubic(1.0, 5.0, v.x);", 3.628000020980835},
		{"v.x = 1.0; return math.ease_out_cubic(1.0, 5.0, v.x);", 5.0},
	})
}

func TestVanillaEaseOutElastic(t *testing.T) {
	runVanillaCases(t, "easeoutelastic", []vanillaCase{
		{"math.ease_out_elastic(1.0, 5.0, 0.0)", 1.0},
		{"math.ease_out_elastic(1.0, 5.0, 0.3)", 4.5},
		{"math.ease_out_elastic(1.0, 5.0, 1.0)", 5.0},
		{"v.x = 0.0; return math.ease_out_elastic(1.0, 5.0, v.x);", 1.0},
		{"v.x = 0.3; return math.ease_out_elastic(1.0, 5.0, v.x);", 4.5},
		{"v.x = 1.0; return math.ease_out_elastic(1.0, 5.0, v.x);", 5.0},
	})
}

func TestVanillaEaseOutExpo(t *testing.T) {
	runVanillaCases(t, "easeoutexpo", []vanillaCase{
		{"math.ease_out_expo(1.0, 5.0, 0.0)", 1.0},
		{"math.ease_out_expo(1.0, 5.0, 0.3)", 4.5},
		{"math.ease_out_expo(1.0, 5.0, 1.0)", 4.99609375},
		{"v.x = 0.0; return math.ease_out_expo(1.0, 5.0, v.x);", 1.0},
		{"v.x = 0.3; return math.ease_out_expo(1.0, 5.0, v.x);", 4.5},
		{"v.x = 1.0; return math.ease_out_expo(1.0, 5.0, v.x);", 4.99609375},
	})
}

func TestVanillaEaseOutQuad(t *testing.T) {
	runVanillaCases(t, "easeoutquad", []vanillaCase{
		{"math.ease_out_quad(1.0, 5.0, 0.0)", 1.0},
		{"math.ease_out_quad(1.0, 5.0, 0.3)", 3.0400002002716064},
		{"math.ease_out_quad(1.0, 5.0, 1.0)", 5.0},
		{"v.x = 0.0; return math.ease_out_quad(1.0, 5.0, v.x);", 1.0},
		{"v.x = 0.3; return math.ease_out_quad(1.0, 5.0, v.x);", 3.0400002002716064},
		{"v.x = 1.0; return math.ease_out_quad(1.0, 5.0, v.x);", 5.0},
	})
}

func TestVanillaEaseOutQuart(t *testing.T) {
	runVanillaCases(t, "easeoutquart", []vanillaCase{
		{"math.ease_out_quart(1.0, 5.0, 0.0)", 1.0},
		{"math.ease_out_quart(1.0, 5.0, 0.3)", 4.039600372314453},
		{"math.ease_out_quart(1.0, 5.0, 1.0)", 5.0},
		{"v.x = 0.0; return math.ease_out_quart(1.0, 5.0, v.x);", 1.0},
		{"v.x = 0.3; return math.ease_out_quart(1.0, 5.0, v.x);", 4.039600372314453},
		{"v.x = 1.0; return math.ease_out_quart(1.0, 5.0, v.x);", 5.0},
	})
}

func TestVanillaEaseOutQuint(t *testing.T) {
	runVanillaCases(t, "easeoutquint", []vanillaCase{
		{"math.ease_out_quint(1.0, 5.0, 0.0)", 1.0},
		{"math.ease_out_quint(1.0, 5.0, 0.3)", 4.3277201652526855},
		{"math.ease_out_quint(1.0, 5.0, 1.0)", 5.0},
		{"v.x = 0.0; return math.ease_out_quint(1.0, 5.0, v.x);", 1.0},
		{"v.x = 0.3; return math.ease_out_quint(1.0, 5.0, v.x);", 4.3277201652526855},
		{"v.x = 1.0; return math.ease_out_quint(1.0, 5.0, v.x);", 5.0},
	})
}

func TestVanillaEaseOutSine(t *testing.T) {
	runVanillaCases(t, "easeoutsine", []vanillaCase{
		{"math.ease_out_sine(1.0, 5.0, 0.0)", 1.0},
		{"math.ease_out_sine(1.0, 5.0, 0.3)", 2.8158936500549316},
		{"math.ease_out_sine(1.0, 5.0, 1.0)", 5.0},
		{"v.x = 0.0; return math.ease_out_sine(1.0, 5.0, v.x);", 1.0},
		{"v.x = 0.3; return math.ease_out_sine(1.0, 5.0, v.x);", 2.8158936500549316},
		{"v.x = 1.0; return math.ease_out_sine(1.0, 5.0, v.x);", 5.0},
	})
}

func TestVanillaLoops(t *testing.T) {
	runVanillaCases(t, "loops", []vanillaCase{
		{"v.count = 0; loop(3, {v.count = v.count + 1;}); return v.count;", 3.0},
		{"v.count = 0; loop(3, {v.count = v.count + 1; (v.count == 2) ? break; }); return v.count;", 2.0},
		{"v.iterations = 0; v.count = 0; loop(3, {v.iterations = v.iterations + 1; (v.count == 1) ? continue; v.count = v.count + 1;}); return v.count + v.iterations;", 4.0},
		{"v.loop_count = 3; v.count = 0; loop(v.loop_count, {v.count = v.count + 1;}); return v.count;", 3.0},
		{"v.loop_count = 3; v.count = 0; loop(v.loop_count, {v.count = v.count + 1; (v.count == 2) ? break; }); return v.count;", 2.0},
		{"v.loop_count = 3; v.iterations = 0; v.count = 0; loop(v.loop_count, {v.iterations = v.iterations + 1; (v.count == 1) ? continue; v.count = v.count + 1;}); return v.count + v.iterations;", 4.0},
	})
}

func TestVanillaMathCeil(t *testing.T) {
	runVanillaCases(t, "mathceil", []vanillaCase{
		{"math.ceil(0.0f)", 0.0},
		{"math.ceil(1.0f)", 1.0},
		{"math.ceil(-1.0f)", -1.0},
		{"math.ceil((-1.0f))", -1.0},
		{"math.ceil(2.0f)", 2.0},
		{"math.ceil(-2.0f)", -2.0},
		{"math.ceil(0.5f)", 1.0},
		{"math.ceil(-0.5f)", -0.0},
		{"math.ceil(1.5f)", 2.0},
		{"math.ceil(-1.5f)", -1.0},
		{"math.ceil(1.51f)", 2.0},
		{"math.ceil(-1.51f)", -1.0},
		{"math.ceil(1.4f)", 2.0},
		{"math.ceil(-1.4f)", -1.0},
		{"math.ceil(1.4999f)", 2.0},
		{"math.ceil(-1.4999f)", -1.0},
		{"math.ceil(1.000001f)", 2.0},
		{"math.ceil(-1.000001f)", -1.0},
		{"math.ceil(0.99999f)", 1.0},
		{"math.ceil(-0.99999f)", -0.0},
		{"math.ceil(100000.000001f)", 100000.0},
		{"math.ceil(-100000.000001f)", -100000.0},
		{"math.ceil(100000.99999f)", 100001.0},
		{"math.ceil(-100000.99999f)", -100001.0},
		{"math.ceil(1.1f) + 1", 3.0},
		{"math.ceil(1.1f) * 2", 4.0},
		{"math.ceil(1.1f) * 2 + 1", 5.0},
		{"math.ceil(1.1f) * -2", -4.0},
		{"math.ceil(1.1f) * -2 + 1", -3.0},
		{"math.ceil(1.1f) * -2 - 1", -5.0},
		{"v.x = 0.0f; return math.ceil(v.x);", 0.0},
		{"v.x = 1.0f; return math.ceil(v.x);", 1.0},
		{"v.x = -1.0f; return math.ceil(v.x);", -1.0},
		{"v.x = 2.0f; return math.ceil(v.x);", 2.0},
		{"v.x = -2.0f; return math.ceil(v.x);", -2.0},
		{"v.x = 0.5f; return math.ceil(v.x);", 1.0},
		{"v.x = -0.5f; return math.ceil(v.x);", -0.0},
		{"v.x = 1.5f; return math.ceil(v.x);", 2.0},
		{"v.x = -1.5f; return math.ceil(v.x);", -1.0},
		{"v.x = 1.51f; return math.ceil(v.x);", 2.0},
		{"v.x = -1.51f; return math.ceil(v.x);", -1.0},
		{"v.x = 1.4f; return math.ceil(v.x);", 2.0},
		{"v.x = -1.4f; return math.ceil(v.x);", -1.0},
		{"v.x = 1.4999f; return math.ceil(v.x);", 2.0},
		{"v.x = -1.4999f; return math.ceil(v.x);", -1.0},
		{"v.x = 1.000001f; return math.ceil(v.x);", 2.0},
		{"v.x = -1.000001f; return math.ceil(v.x);", -1.0},
		{"v.x = 0.99999f; return math.ceil(v.x);", 1.0},
		{"v.x = -0.99999f; return math.ceil(v.x);", -0.0},
		{"v.x = 100000.000001f; return math.ceil(v.x);", 100000.0},
		{"v.x = -100000.000001f; return math.ceil(v.x);", -100000.0},
		{"v.x = 100000.99999f; return math.ceil(v.x);", 100001.0},
		{"v.x = -100000.99999f; return math.ceil(v.x);", -100001.0},
	})
}

func TestVanillaMathClamp(t *testing.T) {
	runVanillaCases(t, "mathclamp", []vanillaCase{
		{"math.clamp(1.0f, 2.0f, 3.0f)", 2.0},
		{"math.clamp(2.0f, 1.0f, 3.0f)", 2.0},
		{"math.clamp(3.0f, 1.0f, 2.0f)", 2.0},
		{"math.clamp(3.0f, 2.0f, 1.0f)", 1.0},
		{"math.clamp(-1.0f, -2.0f, -3.0f)", -3.0},
		{"math.clamp(-2.0f, -1.0f, -3.0f)", -3.0},
		{"math.clamp(-3.0f, -1.0f, -2.0f)", -1.0},
		{"math.clamp(-3.0f, -2.0f, -1.0f)", -2.0},
		{"math.clamp(2.1f, 0, 1.1) + 1", 2.0999999046325684},
		{"math.clamp(2.1f, 0, 1.1) * 2", 2.200000047683716},
		{"math.clamp(2.1f, 0, 1.1) * 2 + 1", 3.200000047683716},
		{"math.clamp(2.1f, 0, 1.1) * -2", -2.200000047683716},
		{"math.clamp(2.1f, 0, 1.1) * -2 + 1", -1.2000000476837158},
		{"math.clamp(2.1f, 0, 1.1) * -2 - 1", -3.200000047683716},
		{"v.x =  1.0f; v.y =  2.0f; v.z =  3.0f; return math.clamp(v.x, v.y, v.z);", 2.0},
		{"v.x =  2.0f; v.y =  1.0f; v.z =  3.0f; return math.clamp(v.x, v.y, v.z);", 2.0},
		{"v.x =  3.0f; v.y =  1.0f; v.z =  2.0f; return math.clamp(v.x, v.y, v.z);", 2.0},
		{"v.x =  3.0f; v.y =  2.0f; v.z =  1.0f; return math.clamp(v.x, v.y, v.z);", 1.0},
		{"v.x = -1.0f; v.y = -2.0f; v.z = -3.0f; return math.clamp(v.x, v.y, v.z);", -3.0},
		{"v.x = -2.0f; v.y = -1.0f; v.z = -3.0f; return math.clamp(v.x, v.y, v.z);", -3.0},
		{"v.x = -3.0f; v.y = -1.0f; v.z = -2.0f; return math.clamp(v.x, v.y, v.z);", -1.0},
		{"v.x = -3.0f; v.y = -2.0f; v.z = -1.0f; return math.clamp(v.x, v.y, v.z);", -2.0},
	})
}

func TestVanillaMathCopySign(t *testing.T) {
	runVanillaCases(t, "mathcopysign", []vanillaCase{
		{"math.copy_sign(1.0f, 0.0f)", 1.0},
		{"math.copy_sign(0.0f, 1.0f)", 0.0},
		{"math.copy_sign(1.0f, -1.0f)", -1.0},
		{"math.copy_sign(1.0f, (-1.0f))", -1.0},
		{"math.copy_sign(2.0f, 0.0f)", 2.0},
		{"math.copy_sign(0.0f, 2.0f)", 0.0},
		{"math.copy_sign(2.0f, -2.0f)", -2.0},
		{"math.copy_sign(2.0f, (-2.0f))", -2.0},
		{"math.copy_sign(0.5f, 0.0f)", 0.5},
		{"math.copy_sign(0.0f, 0.5f)", 0.0},
		{"math.copy_sign((2.0f), 0.0f)", 2.0},
		{"math.copy_sign((0.0f), 2.0f)", 0.0},
		{"math.copy_sign((2.0f), -2.0f)", -2.0},
		{"math.copy_sign((2.0f), (-2.0f))", -2.0},
		{"math.copy_sign(-1.1f, 3.1) + 1", 2.0999999046325684},
		{"math.copy_sign(-1.1f, 3.1) * 2", 2.200000047683716},
		{"math.copy_sign(-1.1f, 3.1) * 2 + 1", 3.200000047683716},
		{"math.copy_sign(-1.1f, 3.1) * -2", -2.200000047683716},
		{"math.copy_sign(-1.1f, 3.1) * -2 + 1", -1.2000000476837158},
		{"math.copy_sign(-1.1f, 3.1) * -2 - 1", -3.200000047683716},
	})
}

func TestVanillaMathCos(t *testing.T) {
	runVanillaCases(t, "mathcos", []vanillaCase{
		{"math.cos(0.0) + 1.1", 2.0999999046325684},
		{"math.cos(0.0) * 2", 2.0},
		{"math.cos(0.0) * 2 + 1.1", 3.0999999046325684},
		{"math.cos(0.0) * -2", -2.0},
		{"math.cos(0.0) * -2 + 1.1", -0.8999999761581421},
		{"math.cos(0.0) * -2 - 1.1", -3.0999999046325684},
	})
}

func TestVanillaMathExp(t *testing.T) {
	runVanillaCases(t, "mathexp", []vanillaCase{
		{"math.exp(0.0) + 1.1", 2.0999999046325684},
		{"math.exp(0.0) * 2", 2.0},
		{"math.exp(0.0) * 2.2 + 1", 3.200000047683716},
		{"math.exp(0.0) * -2.2", -2.200000047683716},
		{"math.exp(0.0) * -2.2 + 1", -1.2000000476837158},
		{"math.exp(0.0) * -2.2 - 1", -3.200000047683716},
	})
}

func TestVanillaMathFloor(t *testing.T) {
	runVanillaCases(t, "mathfloor", []vanillaCase{
		{"math.floor(0.0f)", 0.0},
		{"math.floor(1.0f)", 1.0},
		{"math.floor(-1.0f)", -1.0},
		{"math.floor((-1.0f))", -1.0},
		{"math.floor(2.0f)", 2.0},
		{"math.floor(-2.0f)", -2.0},
		{"math.floor(0.5f)", 0.0},
		{"math.floor(-0.5f)", -1.0},
		{"math.floor(1.5f)", 1.0},
		{"math.floor(-1.5f)", -2.0},
		{"math.floor(1.51f)", 1.0},
		{"math.floor(-1.51f)", -2.0},
		{"math.floor(1.4f)", 1.0},
		{"math.floor(-1.4f)", -2.0},
		{"math.floor(1.4999f)", 1.0},
		{"math.floor(-1.4999f)", -2.0},
		{"math.floor(1.000001f)", 1.0},
		{"math.floor(-1.000001f)", -2.0},
		{"math.floor(0.99999f)", 0.0},
		{"math.floor(-0.99999f)", -1.0},
		{"math.floor(100000.000001f)", 100000.0},
		{"math.floor(-100000.000001f)", -100000.0},
		{"math.floor(100000.99999f)", 100001.0},
		{"math.floor(-100000.99999f)", -100001.0},
		{"math.floor(1.9) + 1.0", 2.0},
		{"math.floor(1.9) * 2.2", 2.200000047683716},
		{"math.floor(1.9) * 2.2 + 1", 3.200000047683716},
		{"math.floor(1.9) * -2.2", -2.200000047683716},
		{"math.floor(1.9) * -2.2 + 1", -1.2000000476837158},
		{"math.floor(1.9) * -2.2 - 1", -3.200000047683716},
		{"v.x = 0.0f; return math.floor(v.x);", 0.0},
		{"v.x = 1.0f; return math.floor(v.x);", 1.0},
		{"v.x = -1.0f; return math.floor(v.x);", -1.0},
		{"v.x = 2.0f; return math.floor(v.x);", 2.0},
		{"v.x = -2.0f; return math.floor(v.x);", -2.0},
		{"v.x = 0.5f; return math.floor(v.x);", 0.0},
		{"v.x = -0.5f; return math.floor(v.x);", -1.0},
		{"v.x = 1.5f; return math.floor(v.x);", 1.0},
		{"v.x = -1.5f; return math.floor(v.x);", -2.0},
		{"v.x = 1.51f; return math.floor(v.x);", 1.0},
		{"v.x = -1.51f; return math.floor(v.x);", -2.0},
		{"v.x = 1.4f; return math.floor(v.x);", 1.0},
		{"v.x = -1.4f; return math.floor(v.x);", -2.0},
		{"v.x = 1.4999f; return math.floor(v.x);", 1.0},
		{"v.x = -1.4999f; return math.floor(v.x);", -2.0},
		{"v.x = 1.000001f; return math.floor(v.x);", 1.0},
		{"v.x = -1.000001f; return math.floor(v.x);", -2.0},
		{"v.x = 0.99999f; return math.floor(v.x);", 0.0},
		{"v.x = -0.99999f; return math.floor(v.x);", -1.0},
		{"v.x = 100000.000001f; return math.floor(v.x);", 100000.0},
		{"v.x = -100000.000001f; return math.floor(v.x);", -100000.0},
		{"v.x = 100000.99999f; return math.floor(v.x);", 100001.0},
		{"v.x = -100000.99999f; return math.floor(v.x);", -100001.0},
	})
}

func TestVanillaMathInverseLerp(t *testing.T) {
	runVanillaCases(t, "mathinverselerp", []vanillaCase{
		{"math.inverse_lerp(1.0, 5.0, 1.0)", 0.0},
		{"math.inverse_lerp(1.0, 5.0, 2.0)", 0.25},
		{"math.inverse_lerp(1.0, 5.0, 3.0)", 0.5},
		{"v.x = 1.0; return math.inverse_lerp(1.0, 5.0, v.x);", 0.0},
		{"v.x = 2.0; return math.inverse_lerp(1.0, 5.0, v.x);", 0.25},
		{"v.x = 3.0; return math.inverse_lerp(1.0, 5.0, v.x);", 0.5},
	})
}

func TestVanillaMathMax(t *testing.T) {
	runVanillaCases(t, "mathmax", []vanillaCase{
		{"math.max(0.0f, 1.0f)", 1.0},
		{"math.max(1.0f, 0.0f)", 1.0},
		{"math.max(-1.0f, 1.0f)", 1.0},
		{"math.max((-1.0f), 1.0f)", 1.0},
		{"v.x =  0.0; v.y = 1.0; return math.max( v.x,  v.y);", 1.0},
		{"v.x =  1.0; v.y = 0.0; return math.max( v.x,  v.y);", 1.0},
		{"v.x = -0.0; v.y = 1.0; return math.max( v.x,  v.y);", 1.0},
		{"v.x = -1.0; v.y = 1.0; return math.max((v.x), v.y);", 1.0},
	})
}

func TestVanillaMathMin(t *testing.T) {
	runVanillaCases(t, "mathmin", []vanillaCase{
		{"math.min(0.0f, 1.0f)", 0.0},
		{"math.min(1.0f, 0.0f)", 0.0},
		{"math.min(-1.0f, 1.0f)", -1.0},
		{"math.min((-1.0f), 1.0f)", -1.0},
		{"v.x =  0.0; v.y = 1.0; return math.min( v.x,  v.y);", 0.0},
		{"v.x =  1.0; v.y = 0.0; return math.min( v.x,  v.y);", 0.0},
		{"v.x = -1.0; v.y = 1.0; return math.min( v.x,  v.y);", -1.0},
		{"v.x = -1.0; v.y = 1.0; return math.min((v.x), v.y);", -1.0},
	})
}

func TestVanillaMathMod(t *testing.T) {
	runVanillaCases(t, "mathmod", []vanillaCase{
		{"v.x =  0.0f ; v.y = 1.0; return math.mod(v.x, v.y) + 0.125;", 0.125},
		{"v.x =  0.0f ; v.y = 2.0; return math.mod(v.x, v.y) + 0.125;", 0.125},
		{"v.x =  0.0f ; v.y = 0.0; return math.mod(v.x, v.y) + 0.125;", 0.125},
		{"v.x =  1.0f ; v.y = 0.0; return math.mod(v.x, v.y) + 0.125;", 0.125},
		{"v.x =  2.0f ; v.y = 0.0; return math.mod(v.x, v.y) + 0.125;", 0.125},
		{"v.x = -2.0f ; v.y = 0.0; return math.mod(v.x, v.y) + 0.125;", 0.125},
		{"v.x =  1.0f ; v.y = 3.0; return math.mod(v.x, v.y);", 1.0},
		{"v.x =  2.0f ; v.y = 3.0; return math.mod(v.x, v.y);", 2.0},
		{"v.x =  3.0f ; v.y = 3.0; return math.mod(v.x, v.y) + 0.125;", 0.125},
		{"v.x =  4.0f ; v.y = 3.0; return math.mod(v.x, v.y);", 1.0},
		{"v.x =  0.25f; v.y = 0.5; return math.mod(v.x, v.y);", 0.25},
		{"v.x =  0.4f ; v.y = 0.5; return math.mod(v.x, v.y);", 0.4000000059604645},
		{"v.x =  0.5f ; v.y = 0.5; return math.mod(v.x, v.y) + 0.125;", 0.125},
		{"v.x =  0.6f ; v.y = 0.5; return math.mod(v.x, v.y);", 0.10000000149011612},
		{"v.x = -5.1f ; v.y = 3.0; return math.mod(v.x, v.y);", -2.0999999046325684},
		{"v.x =  0.0f ; v.y = 1.0; return math.mod(v.x, v.y) - 1;", -1.0},
		{"v.x =  0.0f ; v.y = 1.0; return 1 - math.mod(v.x, v.y);", 1.0},
		{"v.x =  0.0f ; v.y = 1.0; return math.mod(v.x, v.y) - math.mod(v.x, v.y) + 0.125;", 0.125},
	})
}

func TestVanillaMathRound(t *testing.T) {
	runVanillaCases(t, "mathround", []vanillaCase{
		{"math.round(0.0f)", 0.0},
		{"math.round(1.0f)", 1.0},
		{"math.round(-1.0f)", -1.0},
		{"math.round((-1.0f))", -1.0},
		{"math.round(2.0f)", 2.0},
		{"math.round(-2.0f)", -2.0},
		{"math.round(0.5f)", 1.0},
		{"math.round(-0.5f)", -1.0},
		{"math.round(1.5f)", 2.0},
		{"math.round(-1.5f)", -2.0},
		{"math.round(1.51f)", 2.0},
		{"math.round(-1.51f)", -2.0},
		{"math.round(1.4f)", 1.0},
		{"math.round(-1.4f)", -1.0},
		{"math.round(1.4999f)", 1.0},
		{"math.round(-1.4999f)", -1.0},
		{"math.round(1.000001f)", 1.0},
		{"math.round(-1.000001f)", -1.0},
		{"math.round(0.99999f)", 1.0},
		{"math.round(-0.99999f)", -1.0},
		{"math.round(100000.000001f)", 100000.0},
		{"math.round(-100000.000001f)", -100000.0},
		{"math.round(100000.99999f)", 100001.0},
		{"math.round(-100000.99999f)", -100001.0},
		{"v.x = 0.0f; return math.round(v.x);", 0.0},
		{"v.x = 1.0f; return math.round(v.x);", 1.0},
		{"v.x = -1.0f; return math.round(v.x);", -1.0},
		{"v.x = 2.0f; return math.round(v.x);", 2.0},
		{"v.x = -2.0f; return math.round(v.x);", -2.0},
		{"v.x = 0.5f; return math.round(v.x);", 1.0},
		{"v.x = -0.5f; return math.round(v.x);", -1.0},
		{"v.x = 1.5f; return math.round(v.x);", 2.0},
		{"v.x = -1.5f; return math.round(v.x);", -2.0},
		{"v.x = 1.51f; return math.round(v.x);", 2.0},
		{"v.x = -1.51f; return math.round(v.x);", -2.0},
		{"v.x = 1.4f; return math.round(v.x);", 1.0},
		{"v.x = -1.4f; return math.round(v.x);", -1.0},
		{"v.x = 1.4999f; return math.round(v.x);", 1.0},
		{"v.x = -1.4999f; return math.round(v.x);", -1.0},
		{"v.x = 1.000001f; return math.round(v.x);", 1.0},
		{"v.x = -1.000001f; return math.round(v.x);", -1.0},
		{"v.x = 0.99999f; return math.round(v.x);", 1.0},
		{"v.x = -0.99999f; return math.round(v.x);", -1.0},
		{"v.x = 100000.000001f; return math.round(v.x);", 100000.0},
		{"v.x = -100000.000001f; return math.round(v.x);", -100000.0},
		{"v.x = 100000.99999f; return math.round(v.x);", 100001.0},
		{"v.x = -100000.99999f; return math.round(v.x);", -100001.0},
	})
}

func TestVanillaMathSign(t *testing.T) {
	runVanillaCases(t, "mathsign", []vanillaCase{
		{"math.sign(0.0f)", 1.0},
		{"math.sign(math.pi/1.3f)", 1.0},
		{"math.sign(math.pi)", 1.0},
		{"math.sign(-math.pi/2.0f)", -1.0},
		{"math.sign(123.456f)", 1.0},
		{"math.sign(-123.456f)", -1.0},
		{"math.sign(1.0) + 1", 2.0},
		{"math.sign(1.0) * 2", 2.0},
		{"math.sign(1.0) * 2 + 1", 3.0},
		{"math.sign(1.0) * -2 + 1", -1.0},
		{"math.sign(1.0) * -2 - 1", -3.0},
		{"v.x = 0.0f; return math.sign(v.x);", 1.0},
		{"v.x = math.pi / 1.3f; return math.sign(v.x);", 1.0},
		{"v.x = math.pi; return math.sign(v.x);", 1.0},
		{"v.x = -math.pi / 2.0f; return math.sign(v.x);", -1.0},
		{"v.x = 123.456f; return math.sign(v.x);", 1.0},
		{"v.x = -123.456f; return math.sign(v.x);", -1.0},
	})
}

func TestVanillaMathSin(t *testing.T) {
	runVanillaCases(t, "mathsin", []vanillaCase{
		{"math.sin(90) + 1.1", 2.0999999046325684},
		{"math.sin(90) * 2", 2.0},
		{"math.sin(90) * 2 + 1.1", 3.0999999046325684},
		{"math.sin(90) * -2", -2.0},
		{"math.sin(90) * -2 + 1.1", -0.8999999761581421},
		{"math.sin(90) * -2 - 1.1", -3.0999999046325684},
	})
}

func TestVanillaMathSqrt(t *testing.T) {
	runVanillaCases(t, "mathsqrt", []vanillaCase{
		{"math.sqrt(1.0) + 1", 2.0},
		{"math.sqrt(1.0) * 2", 2.0},
		{"math.sqrt(1.0) * 2 + 1", 3.0},
		{"math.sqrt(1.0) * -2 + 1", -1.0},
		{"math.sqrt(1.0) * -2 - 1", -3.0},
	})
}

func TestVanillaMolangScriptExpression(t *testing.T) {
	runVanillaCases(t, "molangscriptexpression", []vanillaCase{
		{"v.x.x = 1; v.x.y = 2; return v.x.x + v.x.y;", 3.0},
		{"v.x.x = 1; v.x.y = 2; v.xx = v.x.x; v.xy = v.x.y; return v.xx + v.xy;", 3.0},
		{"v.test.a.b.c = 1; v.testabc = v.test.a.b.c; return v.testabc;", 1.0},
		{"v.x.x = 1; return v.x.x;", 1.0},
		{"v.testabc = 2; v.test.a.b.c = v.testabc; return v.test.a.b.c;", 2.0},
		{"return -!!0;", 0.0},
		{"return -!!!0;", -1.0},
		{"return -!!!0 + 1;", 0.0},
		{"return !-!!!0;", 0.0},
		{"return 1+!-!!!0;", 1.0},
		{"return !1+!-!!!0;", 0.0},
		{"(1 < 0) + 1", 1.0},
		{"(1 <= 0) + 1", 1.0},
		{"(1 > 0) + 1", 2.0},
		{"(1 >= 0) + 1", 2.0},
		{"(1 == 0) + 1", 1.0},
		{"(1 != 0) + 1", 2.0},
	})
}

func TestVanillaMultDiv(t *testing.T) {
	runVanillaCases(t, "multdiv", []vanillaCase{
		{"2.0 * 3.0 * 4.0 * 5.0", 120.0},
		{"2.0 * 3.0 / 4.0 * 5.0", 7.5},
		{"2.0 * 3.0 * 4.0 * -5.0", -120.0},
		{"2.0 * 3.0 / 4.0 * -5.0", -7.5},
		{"2.0 * 3.0 * -4.0 * 5.0", -120.0},
		{"2.0 * 3.0 / -4.0 * 5.0", -7.5},
		{"2.0 * -3.0 * 4.0 * 5.0", -120.0},
		{"2.0 * -3.0 / 4.0 * 5.0", -7.5},
		{"-2.0 * 3.0 * 4.0 * 5.0", -120.0},
		{"-2.0 * 3.0 / 4.0 * 5.0", -7.5},
		{"2.0 * 3.0 * (4.0 * 5.0)", 120.0},
		{"2.0 * 3.0 * (4.0 * -5.0)", -120.0},
		{"2.0 * 3.0 * (-4.0 * 5.0)", -120.0},
		{"2.0 * -3.0 * (4.0 * 5.0)", -120.0},
		{"-2.0 * 3.0 * (4.0 * 5.0)", -120.0},
		{"2.0 * 3.0 * -(4.0 * 5.0)", -120.0},
		{"2.0 * 3.0 * -(4.0 * -5.0)", 120.0},
		{"2.0 * 3.0 * -(-4.0 * 5.0)", 120.0},
		{"2.0 * -3.0 * -(4.0 * 5.0)", 120.0},
		{"-2.0 * 3.0 * -(4.0 * 5.0)", 120.0},
		{"2.0 * (3.0 * 4.0) * 5.0", 120.0},
		{"2.0 * (3.0 / 4.0) * 5.0", 7.5},
		{"2.0 * (3.0 * 4.0) * -5.0", -120.0},
		{"2.0 * (3.0 / 4.0) * -5.0", -7.5},
		{"2.0 * (3.0 * -4.0) * 5.0", -120.0},
		{"2.0 * (3.0 / -4.0) * 5.0", -7.5},
		{"2.0 * (-3.0 * 4.0) * 5.0", -120.0},
		{"2.0 * (-3.0 / 4.0) * 5.0", -7.5},
		{"-2.0 * (3.0 * 4.0) * 5.0", -120.0},
		{"-2.0 * (3.0 / 4.0) * 5.0", -7.5},
		{"(2.0 * 3.0) * 4.0 * 5.0", 120.0},
		{"(2.0 * 3.0) / 4.0 * 5.0", 7.5},
		{"(2.0 * 3.0) * 4.0 * -5.0", -120.0},
		{"(2.0 * 3.0) / 4.0 * -5.0", -7.5},
		{"(2.0 * 3.0) * -4.0 * 5.0", -120.0},
		{"(2.0 * 3.0) / -4.0 * 5.0", -7.5},
		{"(2.0 * -3.0) * 4.0 * 5.0", -120.0},
		{"(2.0 * -3.0) / 4.0 * 5.0", -7.5},
		{"(-2.0 * 3.0) * 4.0 * 5.0", -120.0},
		{"(-2.0 * 3.0) / 4.0 * 5.0", -7.5},
	})
}

func TestVanillaNullCoalescing(t *testing.T) {
	runVanillaCases(t, "nullcoalescing", []vanillaCase{
		{"variable.a = 0.1; variable.b = 0.2; return (variable.a ?? 2) + (variable.b ?? 3);", 0.30000001192092896},
		{"                                    return (variable.a ?? 2) + (variable.b ?? 3);", 5.0},
		{"                  variable.b = 0.2; return (variable.a ?? 2) + (variable.b ?? 3);", 2.200000047683716},
		{"variable.a = 0.1;                   return (variable.a ?? 2) + (variable.b ?? 3);", 3.0999999046325684},
	})
}

func TestVanillaOptimization(t *testing.T) {
	runVanillaCases(t, "optimization", []vanillaCase{
		{"v.x = 1; v.y = 2; v.z = 3; return (((v.x * v.y * 3.0 + 1.0 * 2.0) * (v.x - v.x + 1)) * v.z + 4) + v.x - v.x + 2 * v.y * 2 + v.y - v.y * (v.z + v.y);", 28.0},
		{"v.x = 1; v.y = 2; v.z = 3; return (((v.x * v.y * 3.0 + 1.0 * 2.0) * (v.x - v.x + 1)) * v.z + 4) + v.x - v.x + 2 * v.y * 2 + v.y - v.y * -(v.z + v.y);", 48.0},
	})
}

func TestVanillaParsing(t *testing.T) {
	runVanillaCases(t, "parsing", []vanillaCase{
		{"0.", 0.0},
		{"0.0", 0.0},
		{"0.0f", 0.0},
		{"1.", 1.0},
		{"1.0", 1.0},
		{"1.0f", 1.0},
		{"-1.", -1.0},
		{"-1.0", -1.0},
		{"-1.0f", -1.0},
		{"2.", 2.0},
		{"2.0", 2.0},
		{"2.0f", 2.0},
		{"-2.", -2.0},
		{"-2.0", -2.0},
		{"-2.0f", -2.0},
		{"-1e10", -10000000000.0},
		{"-1e10f", -10000000000.0},
		{"-1e9", -1000000000.0},
		{"-1e9f", -1000000000.0},
		{"-1e8", -100000000.0},
		{"-1e8f", -100000000.0},
		{"-1e7", -10000000.0},
		{"-1e7f", -10000000.0},
		{"-1e6", -1000000.0},
		{"-1e6f", -1000000.0},
		{"-1e5", -100000.0},
		{"-1e5f", -100000.0},
		{"-1e4", -10000.0},
		{"-1e4f", -10000.0},
		{"-1e3", -1000.0},
		{"-1e3f", -1000.0},
		{"-1e2", -100.0},
		{"-1e2f", -100.0},
		{"-1e1", -10.0},
		{"-1e1f", -10.0},
		{"1e10", 10000000000.0},
		{"1e10f", 10000000000.0},
		{"1e9", 1000000000.0},
		{"1e9f", 1000000000.0},
		{"1e8", 100000000.0},
		{"1e8f", 100000000.0},
		{"1e7", 10000000.0},
		{"1e7f", 10000000.0},
		{"1e6", 1000000.0},
		{"1e6f", 1000000.0},
		{"1e5", 100000.0},
		{"1e5f", 100000.0},
		{"1e4", 10000.0},
		{"1e4f", 10000.0},
		{"1e3", 1000.0},
		{"1e3f", 1000.0},
		{"1e2", 100.0},
		{"1e2f", 100.0},
		{"1e1", 10.0},
		{"1e1f", 10.0},
		{"10.0f", 10.0},
		{"123.456f", 123.45600128173828},
		{"123.4567890123456789f", 123.456787109375},
		{"-123.4567890123456789f", -123.456787109375},
	})
}

func TestVanillaReturnStatement(t *testing.T) {
	runVanillaCases(t, "returnstatement", []vanillaCase{
		{"v.x = 1.23f; return v.x;", 1.2300000190734863},
		{"v.x = 1.23f; return -v.x;", -1.2300000190734863},
		{"v.x = 1; v.y = 1; v.x ? (v.y ? {return 3;} : {return 1;}) : (v.y ? {return 2;} : {return 0;}); return 4;", 3.0},
		{"v.x = 0; v.y = 1; v.x ? (v.y ? {return 3;} : {return 1;}) : (v.y ? {return 2;} : {return 0;}); return 4;", 2.0},
		{"v.x = 1; v.y = 0; v.x ? (v.y ? {return 3;} : {return 1;}) : (v.y ? {return 2;} : {return 0;}); return 4;", 1.0},
		{"v.x = 0; v.y = 0; v.x ? (v.y ? {return 3;} : {return 1;}) : (v.y ? {return 2;} : {return 0;}); return 4;", 0.0},
	})
}

func TestVanillaTempVariables(t *testing.T) {
	runVanillaCases(t, "tempvariables", []vanillaCase{
		{"t.x = 1; v.x = 2; v.y = t.x; return v.y;", 1.0},
		{"t.x = 1; v.x = 2; v.y = t.x; return t.x;", 1.0},
	})
}

func TestVanillaValidAddition(t *testing.T) {
	runVanillaCases(t, "validaddition", []vanillaCase{
		{"v.x = 1.23; v.y = 2.34; return v.x + v.y;", 3.569999933242798},
		{"v.x = 1.23; v.y = 2.34; return v.x + v.y + math.pi;", 6.711592674255371},
	})
}

func TestVanillaValidDivision(t *testing.T) {
	runVanillaCases(t, "validdivision", []vanillaCase{
		{"3.14159265 / 3.14159265", 1.0},
		{"v.x = 1.23; return v.x + 1;", 2.2300000190734863},
		{"v.x = 1.23; return v.x * 2;", 2.4600000381469727},
		{"v.x = 1.23; return v.x * 2 + 1;", 3.4600000381469727},
		{"v.x = 1.23; return v.x * -2 + 1;", -1.4600000381469727},
		{"v.x = 1.23; return v.x * -2 - 1;", -3.4600000381469727},
	})
}
