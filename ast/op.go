package ast

import (
	"sort"
	"strings"
)

// Op identifies one Molang OPERATION, in the engine's own terms.
//
// The engine does not decide what an expression may contain in its grammar.
// It parses the text, records the set of operations the expression turned out
// to use, and then checks that set against a per-context ALLOW-LIST carried
// by the parse config. A field that forbids an operation rejects the
// expression at load time with a message naming the operation -- the same
// text that parses fine somewhere else.
//
// That is why this type exists, and why its values are the engine's own
// operation numbers rather than a numbering of this package's choosing:
// the numbers are the interface between an expression and the context it is
// written in, so inventing a different set would make every allow-list here
// untranslatable. See OpSet and molang.DisallowSideEffects.
//
// NOT EVERY ENGINE OPERATION HAS A CONSTANT HERE. The engine's list also
// numbers pure syntax -- braces, parentheses, brackets, the comma, the
// member-accessor dot, the quote that opens a string -- plus a few entries
// marked "(internal)". None of those survive into an AST: by the time there
// is a tree, a parenthesis is a shape rather than a node. An operation with
// no constant here can never be reported by OpsUsed, and a context that
// forbids one cannot be modelled by this package.
type Op uint8

// The operations this package's AST can express. Values are the engine's.
const (
	OpNegate     Op = 6 // -x
	OpLogicalNot Op = 7 // !x

	OpAbs            Op = 8
	OpAdd            Op = 9 // '+'; see BinaryExpr note about Sub
	OpArcCosine      Op = 10
	OpArcSine        Op = 11
	OpArcTangent     Op = 12
	OpATan2          Op = 13
	OpCeiling        Op = 14
	OpClamp          Op = 15
	OpCopySign       Op = 16
	OpCosine         Op = 17
	OpDieRoll        Op = 18
	OpDieRollInteger Op = 19
	OpDivide         Op = 20 // '/'
	OpExp            Op = 21
	OpFloor          Op = 22
	OpHermiteBlend   Op = 23
	OpLerp           Op = 24
	OpLerpRotate     Op = 25
	OpNaturalLog     Op = 26
	OpMax            Op = 27
	OpMin            Op = 28
	OpMinAngle       Op = 29
	OpMod            Op = 30
	OpMultiply       Op = 31 // '*'
	OpPower          Op = 32
	OpRandom         Op = 33
	OpRandomInteger  Op = 34
	OpRound          Op = 35
	OpSine           Op = 36
	OpSign           Op = 37
	OpSquareRoot     Op = 38
	OpTruncate       Op = 39

	OpQueryFunction   Op = 40 // query. / q.
	OpArrayVariable   Op = 41 // array. / a.
	OpContextVariable Op = 42 // context. / c.
	OpEntityVariable  Op = 43 // variable. / v.
	OpTempVariable    Op = 44 // temp. / t.

	OpString           Op = 46
	OpGeometryVariable Op = 47
	OpMaterialVariable Op = 48
	OpTextureVariable  Op = 49

	OpLessThan           Op = 50
	OpLessThanOrEqual    Op = 51
	OpGreaterThanOrEqual Op = 52
	OpGreaterThan        Op = 53
	OpLogicalEqual       Op = 54
	OpLogicalNotEqual    Op = 55
	OpLogicalOr          Op = 56
	OpLogicalAnd         Op = 57
	OpNullCoalescing     Op = 58
	OpConditional        Op = 59 // '?'
	OpConditionalElse    Op = 60 // ':'

	OpFloat Op = 61
	OpPi    Op = 62
	OpArray Op = 63 // the '[]' index itself, distinct from the array. name

	OpLoop       Op = 67
	OpForEach    Op = 68
	OpBreak      Op = 69
	OpContinue   Op = 70
	OpAssignment Op = 71 // '='
	OpPointer    Op = 72 // '->'
	OpSemicolon  Op = 73
	OpReturn     Op = 74
	OpThis       Op = 76

	OpInverseLerp Op = 78

	OpEaseInQuad       Op = 79
	OpEaseOutQuad      Op = 80
	OpEaseInOutQuad    Op = 81
	OpEaseInCubic      Op = 82
	OpEaseOutCubic     Op = 83
	OpEaseInOutCubic   Op = 84
	OpEaseInQuart      Op = 85
	OpEaseOutQuart     Op = 86
	OpEaseInOutQuart   Op = 87
	OpEaseInQuint      Op = 88
	OpEaseOutQuint     Op = 89
	OpEaseInOutQuint   Op = 90
	OpEaseInSine       Op = 91
	OpEaseOutSine      Op = 92
	OpEaseInOutSine    Op = 93
	OpEaseInExpo       Op = 94
	OpEaseOutExpo      Op = 95
	OpEaseInOutExpo    Op = 96
	OpEaseInCirc       Op = 97
	OpEaseOutCirc      Op = 98
	OpEaseInOutCirc    Op = 99
	OpEaseInBounce     Op = 100
	OpEaseOutBounce    Op = 101
	OpEaseInOutBounce  Op = 102
	OpEaseInBack       Op = 103
	OpEaseOutBack      Op = 104
	OpEaseInOutBack    Op = 105
	OpEaseInElastic    Op = 106
	OpEaseOutElastic   Op = 107
	OpEaseInOutElastic Op = 108
)

// opCount is one past the highest operation number the engine defines.
const opCount = 109

// opNames are the engine's own friendly names, which is what its
// "not allowed in this context" diagnostic prints. Keeping the exact text
// means an error from this package and an error from the game name the same
// thing the same way.
var opNames = map[Op]string{
	OpNegate:             "Negate '-'",
	OpLogicalNot:         "Logical Not '!'",
	OpAbs:                "Absolute Value 'math.abs'",
	OpAdd:                "Add '+'",
	OpArcCosine:          "Arc Cosine 'math.acos'",
	OpArcSine:            "Arc Sine 'math.asin'",
	OpArcTangent:         "Arc Tangent 'math.atan'",
	OpATan2:              "atan2 'math.atan2'",
	OpCeiling:            "Ceiling 'math.ceil'",
	OpClamp:              "Clamp 'math.clamp'",
	OpCopySign:           "Copy Sign 'math.copy_sign'",
	OpCosine:             "Cosine 'math.cos'",
	OpDieRoll:            "Die Roll 'math.die_roll'",
	OpDieRollInteger:     "Die Roll Integer 'math.die_roll_integer'",
	OpDivide:             "Divide '/'",
	OpExp:                "Base-e Exponent 'math.exp'",
	OpFloor:              "Floor 'math.floor'",
	OpHermiteBlend:       "Hermite Blend 'math.hermite_blend'",
	OpLerp:               "Lerp 'math.lerp'",
	OpLerpRotate:         "Lerp Rotate 'math.lerprotate'",
	OpNaturalLog:         "Natural Log 'math.ln'",
	OpMax:                "Max 'math.max'",
	OpMin:                "Min 'math.min'",
	OpMinAngle:           "Min Angle 'math.min_angle'",
	OpMod:                "Mod 'math.mod'",
	OpMultiply:           "Multiply '*'",
	OpPower:              "Power 'math.pow'",
	OpRandom:             "Random 'math.random'",
	OpRandomInteger:      "Random Integer 'math.random_integer'",
	OpRound:              "Round 'math.round'",
	OpSine:               "Sine 'math.sin'",
	OpSign:               "Sign 'math.sign'",
	OpSquareRoot:         "Square Root 'math.sqrt'",
	OpTruncate:           "Truncate 'math.trunc'",
	OpQueryFunction:      "Query Function 'query.' or 'q.'",
	OpArrayVariable:      "Array Variable 'array.'",
	OpContextVariable:    "Context Variable 'context.' or 'c.'",
	OpEntityVariable:     "Entity Variable 'variable.' or 'v.'",
	OpTempVariable:       "Temp Variable 'temp.' or 't.'",
	OpString:             "String '''",
	OpGeometryVariable:   "Geometry Variable 'geometry.'",
	OpMaterialVariable:   "Material Variable 'material.'",
	OpTextureVariable:    "Texture Variable 'texture.'",
	OpLessThan:           "Less Than '<'",
	OpLessThanOrEqual:    "Less Than Or Equal '<='",
	OpGreaterThanOrEqual: "Greater Than Or Equal '>='",
	OpGreaterThan:        "Greater Than '>'",
	OpLogicalEqual:       "Logical Equal '=='",
	OpLogicalNotEqual:    "Logical Not Equal '!='",
	OpLogicalOr:          "Logical Or '||'",
	OpLogicalAnd:         "Logical And '&&'",
	OpNullCoalescing:     "Null Coalescing '??'",
	OpConditional:        "Conditional '?'",
	OpConditionalElse:    "Conditional Else ':'",
	OpFloat:              "Float",
	OpPi:                 "Pi",
	OpArray:              "Array '[]'",
	OpLoop:               "Loop 'loop'",
	OpForEach:            "For Each 'for_each'",
	OpBreak:              "Break 'break'",
	OpContinue:           "Continue 'continue'",
	OpAssignment:         "Assignment '='",
	OpPointer:            "Pointer '->'",
	OpSemicolon:          "Semicolon ';'",
	OpReturn:             "Return 'return'",
	OpThis:               "This 'this'",
	OpInverseLerp:        "Inverse Lerp 'math.inverse_lerp'",
	OpEaseInQuad:         "Ease In Quad 'math.ease_in_quad'",
	OpEaseOutQuad:        "Ease Out Quad 'math.ease_out_quad'",
	OpEaseInOutQuad:      "Ease In Out Quad 'math.ease_in_out_quad'",
	OpEaseInCubic:        "Ease In Cubic 'math.ease_in_cubic'",
	OpEaseOutCubic:       "Ease Out Cubic 'math.ease_out_cubic'",
	OpEaseInOutCubic:     "Ease In Out Cubic 'math.ease_in_out_cubic'",
	OpEaseInQuart:        "Ease In Quart 'math.ease_in_quart'",
	OpEaseOutQuart:       "Ease Out Quart 'math.ease_out_quart'",
	OpEaseInOutQuart:     "Ease In Out Quart 'math.ease_in_out_quart'",
	OpEaseInQuint:        "Ease In Quint 'math.ease_in_quint'",
	OpEaseOutQuint:       "Ease Out Quint 'math.ease_out_quint'",
	OpEaseInOutQuint:     "Ease In Out Quint 'math.ease_in_out_quint'",
	OpEaseInSine:         "Ease In Sine 'math.ease_in_sine'",
	OpEaseOutSine:        "Ease Out Sine 'math.ease_out_sine'",
	OpEaseInOutSine:      "Ease In Out Sine 'math.ease_in_out_sine'",
	OpEaseInExpo:         "Ease In Expo 'math.ease_in_expo'",
	OpEaseOutExpo:        "Ease Out Expo 'math.ease_out_expo'",
	OpEaseInOutExpo:      "Ease In Out Expo 'math.ease_in_out_expo'",
	OpEaseInCirc:         "Ease In Circ 'math.ease_in_circ'",
	OpEaseOutCirc:        "Ease Out Circ 'math.ease_out_circ'",
	OpEaseInOutCirc:      "Ease In Out Circ 'math.ease_in_out_circ'",
	OpEaseInBounce:       "Ease In Bounce 'math.ease_in_bounce'",
	OpEaseOutBounce:      "Ease Out Bounce 'math.ease_out_bounce'",
	OpEaseInOutBounce:    "Ease In Out Bounce 'math.ease_in_out_bounce'",
	OpEaseInBack:         "Ease In Back 'math.ease_in_back'",
	OpEaseOutBack:        "Ease Out Back 'math.ease_out_back'",
	OpEaseInOutBack:      "Ease In Out Back 'math.ease_in_out_back'",
	OpEaseInElastic:      "Ease In Elastic 'math.ease_in_elastic'",
	OpEaseOutElastic:     "Ease Out Elastic 'math.ease_out_elastic'",
	OpEaseInOutElastic:   "Ease In Out Elastic 'math.ease_in_out_elastic'",
}

// String is the engine's friendly name for the operation, matching the text
// its own "not allowed in this context" diagnostic prints.
func (o Op) String() string {
	if n, ok := opNames[o]; ok {
		return n
	}
	return "<unknown expression op>"
}

// mathOps maps a lower-cased math.<member> to its operation. A member with no
// entry (math.pi is handled separately, as it is not a call) contributes no
// operation, because there is nothing honest to report for a name the math
// table does not have.
var mathOps = map[string]Op{
	"abs": OpAbs, "acos": OpArcCosine, "asin": OpArcSine, "atan": OpArcTangent,
	"atan2": OpATan2, "ceil": OpCeiling, "clamp": OpClamp, "copy_sign": OpCopySign,
	"cos": OpCosine, "die_roll": OpDieRoll, "die_roll_integer": OpDieRollInteger,
	"exp": OpExp, "floor": OpFloor, "hermite_blend": OpHermiteBlend,
	"lerp": OpLerp, "lerprotate": OpLerpRotate, "ln": OpNaturalLog,
	"max": OpMax, "min": OpMin, "min_angle": OpMinAngle, "mod": OpMod,
	"pow": OpPower, "random": OpRandom, "random_integer": OpRandomInteger,
	"round": OpRound, "sin": OpSine, "sign": OpSign, "sqrt": OpSquareRoot,
	"trunc": OpTruncate, "inverse_lerp": OpInverseLerp,

	"ease_in_quad": OpEaseInQuad, "ease_out_quad": OpEaseOutQuad,
	"ease_in_out_quad": OpEaseInOutQuad,
	"ease_in_cubic":    OpEaseInCubic, "ease_out_cubic": OpEaseOutCubic,
	"ease_in_out_cubic": OpEaseInOutCubic,
	"ease_in_quart":     OpEaseInQuart, "ease_out_quart": OpEaseOutQuart,
	"ease_in_out_quart": OpEaseInOutQuart,
	"ease_in_quint":     OpEaseInQuint, "ease_out_quint": OpEaseOutQuint,
	"ease_in_out_quint": OpEaseInOutQuint,
	"ease_in_sine":      OpEaseInSine, "ease_out_sine": OpEaseOutSine,
	"ease_in_out_sine": OpEaseInOutSine,
	"ease_in_expo":     OpEaseInExpo, "ease_out_expo": OpEaseOutExpo,
	"ease_in_out_expo": OpEaseInOutExpo,
	"ease_in_circ":     OpEaseInCirc, "ease_out_circ": OpEaseOutCirc,
	"ease_in_out_circ": OpEaseInOutCirc,
	"ease_in_bounce":   OpEaseInBounce, "ease_out_bounce": OpEaseOutBounce,
	"ease_in_out_bounce": OpEaseInOutBounce,
	"ease_in_back":       OpEaseInBack, "ease_out_back": OpEaseOutBack,
	"ease_in_out_back": OpEaseInOutBack,
	"ease_in_elastic":  OpEaseInElastic, "ease_out_elastic": OpEaseOutElastic,
	"ease_in_out_elastic": OpEaseInOutElastic,
}

// MathOp reports the operation a math.<member> call is, and whether the
// member names one at all.
func MathOp(member string) (Op, bool) {
	op, ok := mathOps[strings.ToLower(member)]
	return op, ok
}

// namespaceOps maps a Namespace to the operation naming it.
var namespaceOps = map[Namespace]Op{
	Query:    OpQueryFunction,
	Array:    OpArrayVariable,
	Context:  OpContextVariable,
	Variable: OpEntityVariable,
	Temp:     OpTempVariable,
	Geometry: OpGeometryVariable,
	Material: OpMaterialVariable,
	Texture:  OpTextureVariable,
	// Math has no namespace operation of its own: a math call reports the
	// specific function it calls, and math.pi reports Pi.
}

// OpSet is a set of operations, held the way the engine holds one: a bitset
// wide enough for every operation number.
//
// The zero value is the empty set and is ready to use.
type OpSet struct {
	w [2]uint64
}

// NewOpSet returns the set containing exactly ops.
func NewOpSet(ops ...Op) OpSet {
	var s OpSet
	for _, op := range ops {
		s.Add(op)
	}
	return s
}

// Add puts op in the set. An out-of-range op is ignored rather than
// panicking: the engine's own name lookup answers "<unknown expression op>"
// for one, and refusing to store it keeps every other method total.
func (s *OpSet) Add(op Op) {
	if op >= opCount {
		return
	}
	s.w[op>>6] |= 1 << (op & 63)
}

// Has reports whether op is in the set.
func (s OpSet) Has(op Op) bool {
	if op >= opCount {
		return false
	}
	return s.w[op>>6]&(1<<(op&63)) != 0
}

// Empty reports whether the set contains no operations.
func (s OpSet) Empty() bool { return s.w[0] == 0 && s.w[1] == 0 }

// Union returns the operations in either set.
func (s OpSet) Union(o OpSet) OpSet {
	return OpSet{w: [2]uint64{s.w[0] | o.w[0], s.w[1] | o.w[1]}}
}

// Intersect returns the operations in both sets. This is the whole of the
// engine's check: intersect what an expression uses with what a context
// forbids, and anything left over is a load error.
func (s OpSet) Intersect(o OpSet) OpSet {
	return OpSet{w: [2]uint64{s.w[0] & o.w[0], s.w[1] & o.w[1]}}
}

// Without returns the operations in s that are not in o.
func (s OpSet) Without(o OpSet) OpSet {
	return OpSet{w: [2]uint64{s.w[0] &^ o.w[0], s.w[1] &^ o.w[1]}}
}

// Ops lists the set's operations in ascending order.
func (s OpSet) Ops() []Op {
	var out []Op
	for op := Op(0); op < opCount; op++ {
		if s.Has(op) {
			out = append(out, op)
		}
	}
	return out
}

// String lists the set's operations by friendly name, comma-separated.
func (s OpSet) String() string {
	ops := s.Ops()
	names := make([]string, len(ops))
	for i, op := range ops {
		names[i] = op.String()
	}
	sort.Strings(names)
	return strings.Join(names, ", ")
}

// OpsUsed reports every operation prog uses.
//
// It walks the tree and nothing else -- no evaluation, no host, no scope --
// so it answers the same question for an expression a caller did not write
// and cannot run.
func OpsUsed(prog *Program) OpSet {
	c := opCollector{}
	if prog == nil {
		return c.set
	}
	// A top-level `;` is an operation in its own right, and it is what makes
	// a statement sequence evaluate to 0 rather than to its last statement.
	if prog.HasSemicolon {
		c.set.Add(OpSemicolon)
	}
	for _, s := range prog.Stmts {
		c.stmt(s)
	}
	return c.set
}

type opCollector struct{ set OpSet }

func (c *opCollector) ident(id *Ident) {
	if id == nil {
		return
	}
	if id.Namespace == Math {
		// math.pi is the one math name that is read rather than called.
		if strings.EqualFold(id.Member, "pi") {
			c.set.Add(OpPi)
		}
		return
	}
	if op, ok := namespaceOps[id.Namespace]; ok {
		c.set.Add(op)
	}
}

func (c *opCollector) stmt(s Stmt) {
	switch n := s.(type) {
	case *ExprStmt:
		c.expr(n.X)
	case *ReturnStmt:
		c.set.Add(OpReturn)
		c.expr(n.Value)
	case *BreakStmt:
		c.set.Add(OpBreak)
	case *ContinueStmt:
		c.set.Add(OpContinue)
	case *CondBlockStmt:
		c.condBlock(n)
	case *LoopStmt:
		c.set.Add(OpLoop)
		c.expr(n.Count)
		c.block(n.Body)
	case *ForEachStmt:
		c.set.Add(OpForEach)
		c.set.Add(OpArrayVariable)
		c.ident(n.Var)
		c.block(n.Body)
	}
}

func (c *opCollector) condBlock(cb *CondBlockStmt) {
	if cb == nil {
		return
	}
	c.set.Add(OpConditional)
	c.expr(cb.Cond)
	c.block(cb.Body)
	if cb.Else != nil {
		c.set.Add(OpConditionalElse)
		c.condBlock(cb.Else)
	}
}

func (c *opCollector) block(b *Block) {
	if b == nil {
		return
	}
	// A block's statements are sequenced, which is the `;` operation.
	if len(b.Stmts) > 0 {
		c.set.Add(OpSemicolon)
	}
	for _, s := range b.Stmts {
		c.stmt(s)
	}
}

func (c *opCollector) expr(e Expr) {
	switch n := e.(type) {
	case nil:
		return
	case *NumberLit:
		c.set.Add(OpFloat)
	case *BoolLit:
		// `true`/`false` are keywords that stand for 1 and 0. The engine
		// has no boolean operation of its own, and a folded literal is what
		// reaches its program either way, so they report as Float.
		c.set.Add(OpFloat)
	case *StringLit:
		c.set.Add(OpString)
	case *Ident:
		c.ident(n)
	case *ThisExpr:
		c.set.Add(OpThis)
	case *UnaryExpr:
		switch n.Op {
		case Neg:
			c.set.Add(OpNegate)
		case LNot:
			c.set.Add(OpLogicalNot)
		}
		c.expr(n.X)
	case *BinaryExpr:
		c.binary(n)
	case *AssignExpr:
		c.set.Add(OpAssignment)
		c.ident(n.Target)
		c.expr(n.Value)
	case *CallExpr:
		c.call(n)
	case *TernaryExpr:
		c.set.Add(OpConditional)
		if n.Else != nil {
			c.set.Add(OpConditionalElse)
		}
		c.expr(n.Cond)
		c.expr(n.Then)
		c.expr(n.Else)
	case *ArrowExpr:
		c.set.Add(OpPointer)
		// The entity is named through the context. namespace, which is the
		// operation the token carries even though Refs treats the name as a
		// dereference rather than a read.
		c.ident(n.Entity)
		c.expr(n.Read)
	case *ArrayAccess:
		c.set.Add(OpArrayVariable)
		c.set.Add(OpArray)
		c.expr(n.Index)
	case *CondBlockStmt:
		c.condBlock(n)
	}
}

func (c *opCollector) binary(n *BinaryExpr) {
	switch n.Op {
	case Add:
		c.set.Add(OpAdd)
	case Sub:
		// The engine has NO subtract operation. Its list numbers Negate and
		// Add but nothing between Divide and Multiply for '-' as an infix
		// operator, so `a - b` is the two of them: negate, then add.
		c.set.Add(OpNegate)
		c.set.Add(OpAdd)
	case Mul:
		c.set.Add(OpMultiply)
	case Div:
		c.set.Add(OpDivide)
	case CmpLt:
		c.set.Add(OpLessThan)
	case CmpLe:
		c.set.Add(OpLessThanOrEqual)
	case CmpGt:
		c.set.Add(OpGreaterThan)
	case CmpGe:
		c.set.Add(OpGreaterThanOrEqual)
	case CmpEq:
		c.set.Add(OpLogicalEqual)
	case CmpNe:
		c.set.Add(OpLogicalNotEqual)
	case LAnd:
		c.set.Add(OpLogicalAnd)
	case LOr:
		c.set.Add(OpLogicalOr)
	case NullCoalesce:
		c.set.Add(OpNullCoalescing)
	}
	c.expr(n.X)
	c.expr(n.Y)
}

func (c *opCollector) call(n *CallExpr) {
	if n.Callee != nil {
		if n.Callee.Namespace == Math {
			if op, ok := MathOp(n.Callee.Member); ok {
				c.set.Add(op)
			}
		} else {
			c.ident(n.Callee)
		}
	}
	for _, a := range n.Args {
		c.expr(a)
	}
}
