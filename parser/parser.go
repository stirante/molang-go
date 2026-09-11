// Package parser turns Molang source text into an *ast.Program.
package parser

import (
	"fmt"
	"strings"

	"github.com/stirante/molang-go/ast"
	"github.com/stirante/molang-go/lexer"
	"github.com/stirante/molang-go/token"
)

// Error is a parse error carrying the source string and the offset it
// failed at, so a caller can point at the character rather than just
// report that something was wrong.
type Error struct {
	Msg    string
	Source string
	Pos    int
}

func (e *Error) Error() string {
	return fmt.Sprintf("%s (at char %d in %q)", e.Msg, e.Pos, e.Source)
}

type parser struct {
	toks   []token.Token
	pos    int
	source string
}

func (p *parser) cur() token.Token { return p.toks[p.pos] }

func (p *parser) advance() token.Token {
	t := p.toks[p.pos]
	if p.pos < len(p.toks)-1 {
		p.pos++
	}
	return t
}

func (p *parser) errorf(pos int, format string, args ...any) {
	panic(&Error{Msg: fmt.Sprintf(format, args...), Source: p.source, Pos: pos})
}

func (p *parser) expect(k token.Kind, what string) token.Token {
	if p.cur().Kind != k {
		p.errorf(p.cur().Pos, "expected %s, got %s", what, p.cur().Kind)
	}
	return p.advance()
}

// Parse lexes and parses a Molang source string into a Program.
// Extensions is re-exported from lexer, so a caller naming them does not have
// to import the lexer to do it.
type Extensions = lexer.Extensions

// Parse parses vanilla Molang. Anything it accepts, the game accepts.
func Parse(source string) (*ast.Program, error) { return ParseWith(source, Extensions{}) }

// ParseWith parses source, accepting the named extensions as well. What it
// accepts is then NOT necessarily what the game accepts -- that is the trade,
// and naming the extensions is how a caller takes it deliberately.
func ParseWith(source string, ext Extensions) (prog *ast.Program, err error) {
	if strings.TrimSpace(source) == "" {
		return nil, &Error{Msg: "empty expression", Source: source, Pos: 0}
	}

	toks, lexErr := lexer.TokenizeWith(source, ext)
	if lexErr != nil {
		if le, ok := lexErr.(*lexer.Error); ok {
			return nil, &Error{Msg: le.Msg, Source: source, Pos: le.Pos}
		}
		return nil, &Error{Msg: lexErr.Error(), Source: source, Pos: 0}
	}

	p := &parser{toks: toks, source: source}

	defer func() {
		if r := recover(); r != nil {
			if pe, ok := r.(*Error); ok {
				err = pe
				return
			}
			panic(r)
		}
	}()

	stmts, hasSemi := p.parseStatementList(token.EOF)
	if p.cur().Kind != token.EOF {
		p.errorf(p.cur().Pos, "unexpected trailing input")
	}
	return &ast.Program{Stmts: stmts, HasSemicolon: hasSemi}, nil
}

// ---------------------------------------------------------------------
// Statement grammar
// ---------------------------------------------------------------------

// parseStatementList parses statements up to (not including) the term
// token (token.EOF for the program body, token.RBrace for a nested block).
// A leading ';' is a parse error; runs of ';' between/after statements are
// harmless no-op empty statements.
func (p *parser) parseStatementList(term token.Kind) ([]ast.Stmt, bool) {
	var stmts []ast.Stmt
	sawStmt := false
	hasSemi := false
	deadAfter, deadName, deadPos := -1, "", 0

	for {
		if p.cur().Kind == token.Semi {
			if !sawStmt {
				p.errorf(p.cur().Pos, "unexpected leading ';'")
			}
			hasSemi = true
			p.advance()
			continue
		}
		if p.cur().Kind == term {
			break
		}
		stmtPos := p.cur().Pos
		stmt := p.parseStatement()
		// Anything after a bare return/break/continue is unreachable, and
		// the game refuses the whole expression rather than dropping the
		// dead code. Recorded here, reported once the list is closed, so
		// the message can say what actually followed.
		if name := terminatingStmtName(stmt); name != "" {
			if deadAfter < 0 {
				deadAfter, deadName, deadPos = len(stmts), name, stmtPos
			}
		}
		stmts = append(stmts, stmt)
		sawStmt = true
		if p.cur().Kind != term && p.cur().Kind != token.Semi {
			p.errorf(p.cur().Pos, "expected ';' or end of block, got %s", p.cur().Kind)
		}
	}
	if deadAfter >= 0 && deadAfter != len(stmts)-1 {
		p.errorf(deadPos, "unreachable statements after '%s'", deadName)
	}
	return stmts, hasSemi
}

// terminatingStmtName names the statement kinds that end execution where they
// stand, or "" for everything else.
//
// It deliberately looks at the statement itself and not into it. A conditional
// whose body returns is NOT terminating -- the conditional may not fire -- and
// that is the whole early-exit guard idiom, so treating it as terminating
// would refuse the most common shape in real content.
func terminatingStmtName(s ast.Stmt) string {
	switch s.(type) {
	case *ast.ReturnStmt:
		return "return"
	case *ast.BreakStmt:
		return "break"
	case *ast.ContinueStmt:
		return "continue"
	}
	return ""
}

func (p *parser) parseBlock() *ast.Block {
	p.expect(token.LBrace, "'{'")
	stmts, _ := p.parseStatementList(token.RBrace)
	p.expect(token.RBrace, "'}'")
	return &ast.Block{Stmts: stmts}
}

func (p *parser) parseStatement() ast.Stmt {
	switch p.cur().Kind {
	case token.Return:
		p.advance()
		if p.cur().Kind == token.Semi || p.cur().Kind == token.RBrace || p.cur().Kind == token.EOF {
			return &ast.ReturnStmt{Value: nil}
		}
		return &ast.ReturnStmt{Value: p.parseAssignment()}
	case token.Break:
		p.advance()
		return &ast.BreakStmt{}
	case token.Continue:
		p.advance()
		return &ast.ContinueStmt{}
	case token.Loop:
		return p.parseLoop()
	case token.ForEach:
		return p.parseForEach()
	case token.LBrace:
		// A bare `{ ... }` statement with no preceding `cond ?` — real
		// packs use this purely as visual grouping (see e.g. the
		// `query.noise`-heavy terraform expressions in the reference
		// corpus, which wrap each named noise computation in its own
		// `{...}` block). Represented as an always-true statement-
		// conditional, matching how a plain `: { ... }` else-clause is
		// already represented (see CondBlockStmt's doc comment) — same
		// "0 unless the block returns" semantics, unconditionally.
		body := p.parseBlock()
		return &ast.CondBlockStmt{Cond: &ast.BoolLit{Value: true}, Body: body}
	default:
		x := p.parseAssignment()
		if cb, ok := x.(*ast.CondBlockStmt); ok {
			return cb
		}
		return &ast.ExprStmt{X: x}
	}
}

// parseForEach parses `for_each(<variable>, array.<name>, { body })`.
//
// The second argument is an array NAME, not an indexed access -- this is the
// one position where a bare `array.foo` is legal, because the loop wants the
// array itself rather than an element of it.
func (p *parser) parseForEach() ast.Stmt {
	p.advance() // 'for_each'
	p.expect(token.LParen, "'('")

	loopVar := p.parseIdentOrCall()
	id, ok := loopVar.(*ast.Ident)
	if !ok || (id.Namespace != ast.Temp && id.Namespace != ast.Variable) {
		p.errorf(p.cur().Pos, "for_each's first argument must be temp.<name> or variable.<name>")
	}
	p.expect(token.Comma, "','")

	nsTok := p.expect(token.Ident, "an array name")
	if ns, known := ast.NamespaceAliases[strings.ToLower(nsTok.Text)]; !known || ns != ast.Array {
		p.errorf(nsTok.Pos, "for_each's second argument must be array.<name>; "+
			"this package has no entities, so an entity array cannot be walked here")
	}
	p.expect(token.Dot, "'.'")
	nameTok := p.expect(token.Ident, "an array name")
	name := nameTok.Text
	for p.cur().Kind == token.Dot {
		p.advance()
		next := p.expect(token.Ident, "array name segment after '.'")
		name += "." + next.Text
	}

	p.expect(token.Comma, "','")
	body := p.parseBlock()
	p.expect(token.RParen, "')'")
	return &ast.ForEachStmt{Var: id, Array: name, Body: body}
}

func (p *parser) parseLoop() ast.Stmt {
	p.advance() // 'loop'
	p.expect(token.LParen, "'('")
	count := p.parseAssignment()
	p.expect(token.Comma, "','")
	body := p.parseBlock()
	p.expect(token.RParen, "')'")
	return &ast.LoopStmt{Count: count, Body: body}
}

// ---------------------------------------------------------------------
// Expression grammar (precedence climbing, lowest to highest):
//
//	assignment  ::= ternary ('=' assignment)?          (right-assoc; target must be temp./variable.)
//	ternary     ::= nullish ('?' ( '{' block '}' (':' elseClause)?  |  assignment (':' ternary)? ))?
//	nullish     ::= or ('??' or)*
//	or          ::= and ('||' and)*
//	and         ::= equality ('&&' equality)*
//	equality    ::= relational (('=='|'!=') relational)*
//	relational  ::= additive (('<'|'<='|'>'|'>=') additive)*
//	additive    ::= multiplicative (('+'|'-') multiplicative)*
//	multiplicative ::= unary (('*'|'/') unary)*
//	unary       ::= ('-'|'!') unary | primary
//	primary     ::= number | string | 'true' | 'false' | '(' assignment ')' | namespaced-ident ('(' args ')')?
//
// ---------------------------------------------------------------------

func (p *parser) parseAssignment() ast.Expr {
	left := p.parseTernary()
	if p.cur().Kind == token.Assign {
		id, ok := left.(*ast.Ident)
		if !ok || (id.Namespace != ast.Temp && id.Namespace != ast.Variable) {
			p.errorf(p.cur().Pos, "invalid assignment target — expected temp.<name> or variable.<name>")
		}
		p.advance()
		value := p.parseAssignment()
		return &ast.AssignExpr{Target: id, Value: value}
	}
	return left
}

func (p *parser) parseTernary() ast.Expr {
	cond := p.parseNullish()
	if p.cur().Kind != token.Question {
		return cond
	}
	p.advance() // '?'

	if p.cur().Kind == token.LBrace {
		body := p.parseBlock()
		cb := &ast.CondBlockStmt{Cond: cond, Body: body}
		if p.cur().Kind == token.Colon {
			p.advance()
			cb.Else = p.parseElseClause()
		}
		return cb
	}

	// `cond ? break;` and `cond ? continue;` -- a jump directly as the true
	// arm, with no braces. This is how loops are written in practice, and
	// writing `(v.i == 2) ? { break; }` instead is noise nobody adds.
	//
	// Represented as the same statement-conditional a braced arm produces,
	// with a one-statement body, so nothing downstream has to know the
	// difference. No else-clause is read: `cond ? break : x` is not a form
	// anything attests, and inventing it here would be guessing at a grammar
	// rather than following one.
	if k := p.cur().Kind; k == token.Break || k == token.Continue {
		jump := p.parseStatement()
		return &ast.CondBlockStmt{Cond: cond, Body: &ast.Block{Stmts: []ast.Stmt{jump}}}
	}

	then := p.parseAssignment()
	var els ast.Expr
	if p.cur().Kind == token.Colon {
		p.advance()
		els = p.parseTernary()
	}
	return &ast.TernaryExpr{Cond: cond, Then: then, Else: els}
}

// parseElseClause parses the else-side of a statement-conditional: either
// another `cond ? { ... }` (else-if chain) or a plain `{ ... }` block,
// represented as a CondBlockStmt with an always-true condition.
func (p *parser) parseElseClause() *ast.CondBlockStmt {
	if p.cur().Kind == token.LBrace {
		body := p.parseBlock()
		return &ast.CondBlockStmt{Cond: &ast.BoolLit{Value: true}, Body: body}
	}
	// else-if: must itself be a `cond ? { ... }` statement-conditional.
	expr := p.parseTernary()
	cb, ok := expr.(*ast.CondBlockStmt)
	if !ok {
		p.errorf(p.cur().Pos, "expected '{' or another conditional after ':'")
	}
	return cb
}

func (p *parser) parseNullish() ast.Expr {
	left := p.parseOr()
	for p.cur().Kind == token.Coalesce {
		p.advance()
		right := p.parseOr()
		left = &ast.BinaryExpr{Op: ast.NullCoalesce, X: left, Y: right}
	}
	return left
}

// rejectArrayOperand refuses `array.foo[i] <op> x` and `x <op> array.foo[i]`.
//
// Arithmetic INSIDE the index is fine and common -- `array.foo[v.i + 1]` --
// but the element that comes out cannot itself be an operand. The game
// refuses that at parse time, and a tool that accepts what the game rejects
// sends an author away believing a pack will load.
//
// Scope of the evidence, stated because it is not uniform: the refusal was
// established directly for `array.foo[i] + 1`. It is applied to every binary
// operator here by analogy with the same refusal for a query returning a
// string, which WAS established for +, -, * and / individually. The likely
// shared reason is that neither expression is a number. If a real pack is
// ever refused here for an operator other than +, this is the assumption to
// go and check.
//
// Checked at each binary level rather than in one place, because a
// precedence-climbing parser has no single point where an operand is "used".
func (p *parser) rejectArrayOperand(e ast.Expr, op token.Token) {
	if _, ok := e.(*ast.ArrayAccess); ok {
		p.errorf(op.Pos, "an array element cannot be an operand of '%s' — "+
			"arithmetic belongs inside the brackets", op.Text)
	}
	// A resource name is refused for a related reason:
	// geometry./material./texture. name a thing, not a number, so there is
	// nothing to add to one.
	//
	// Identity comparison is the exception and it is the whole point of
	// having them: `texture.a == texture.b` and a ternary selecting between
	// two resources are what a pack writes. Those stay legal, exactly as
	// they do for a string literal, which a resource otherwise behaves like.
	if id, ok := e.(*ast.Ident); ok && id.Namespace.IsResource() {
		switch op.Kind {
		case token.Eq, token.Ne:
		default:
			p.errorf(op.Pos, "a %s resource cannot be an operand of '%s' — "+
				"a resource is a name, not a number; only == and != apply",
				id.Namespace, op.Text)
		}
	}
}

func (p *parser) parseOr() ast.Expr {
	left := p.parseAnd()
	for p.cur().Kind == token.Or {
		opTok := p.cur()
		p.rejectArrayOperand(left, opTok)
		p.advance()
		right := p.parseAnd()
		p.rejectArrayOperand(right, opTok)
		left = &ast.BinaryExpr{Op: ast.LOr, X: left, Y: right}
	}
	return left
}

func (p *parser) parseAnd() ast.Expr {
	left := p.parseEquality()
	for p.cur().Kind == token.And {
		opTok := p.cur()
		p.rejectArrayOperand(left, opTok)
		p.advance()
		right := p.parseEquality()
		p.rejectArrayOperand(right, opTok)
		left = &ast.BinaryExpr{Op: ast.LAnd, X: left, Y: right}
	}
	return left
}

func (p *parser) parseEquality() ast.Expr {
	left := p.parseRelational()
	for {
		var op ast.BinaryOp
		switch p.cur().Kind {
		case token.Eq:
			op = ast.CmpEq
		case token.Ne:
			op = ast.CmpNe
		default:
			return left
		}
		opTok := p.cur()
		p.rejectArrayOperand(left, opTok)
		p.advance()
		right := p.parseRelational()
		p.rejectArrayOperand(right, opTok)
		left = &ast.BinaryExpr{Op: op, X: left, Y: right}
	}
}

func (p *parser) parseRelational() ast.Expr {
	left := p.parseAdditive()
	for {
		var op ast.BinaryOp
		switch p.cur().Kind {
		case token.Lt:
			op = ast.CmpLt
		case token.Le:
			op = ast.CmpLe
		case token.Gt:
			op = ast.CmpGt
		case token.Ge:
			op = ast.CmpGe
		default:
			return left
		}
		opTok := p.cur()
		p.rejectArrayOperand(left, opTok)
		p.advance()
		right := p.parseAdditive()
		p.rejectArrayOperand(right, opTok)
		left = &ast.BinaryExpr{Op: op, X: left, Y: right}
	}
}

func (p *parser) parseAdditive() ast.Expr {
	left := p.parseMultiplicative()
	for {
		var op ast.BinaryOp
		switch p.cur().Kind {
		case token.Plus:
			op = ast.Add
		case token.Minus:
			op = ast.Sub
		default:
			return left
		}
		opTok := p.cur()
		p.rejectArrayOperand(left, opTok)
		p.advance()
		right := p.parseMultiplicative()
		p.rejectArrayOperand(right, opTok)
		left = &ast.BinaryExpr{Op: op, X: left, Y: right}
	}
}

func (p *parser) parseMultiplicative() ast.Expr {
	left := p.parseUnary()
	for {
		var op ast.BinaryOp
		switch p.cur().Kind {
		case token.Star:
			op = ast.Mul
		case token.Slash:
			op = ast.Div
		default:
			return left
		}
		opTok := p.cur()
		p.rejectArrayOperand(left, opTok)
		p.advance()
		right := p.parseUnary()
		p.rejectArrayOperand(right, opTok)
		left = &ast.BinaryExpr{Op: op, X: left, Y: right}
	}
}

func (p *parser) parseUnary() ast.Expr {
	switch p.cur().Kind {
	case token.Minus:
		p.advance()
		return &ast.UnaryExpr{Op: ast.Neg, X: p.parseUnary()}
	case token.Not:
		p.advance()
		return &ast.UnaryExpr{Op: ast.LNot, X: p.parseUnary()}
	default:
		return p.parsePrimary()
	}
}

func (p *parser) parsePrimary() ast.Expr {
	tok := p.cur()
	switch tok.Kind {
	case token.Number:
		p.advance()
		return &ast.NumberLit{Value: tok.Num}
	case token.String:
		p.advance()
		return &ast.StringLit{Value: tok.Text}
	case token.True:
		p.advance()
		return &ast.BoolLit{Value: true}
	case token.False:
		p.advance()
		return &ast.BoolLit{Value: false}
	case token.This:
		p.advance()
		return &ast.ThisExpr{}
	case token.LParen:
		p.advance()
		x := p.parseAssignment()
		p.expect(token.RParen, "')'")
		return x
	case token.Ident:
		return p.parseIdentOrCall()
	default:
		p.errorf(tok.Pos, "unexpected token %s", tok.Kind)
		panic("unreachable")
	}
}

func (p *parser) parseIdentOrCall() ast.Expr {
	nsTok := p.advance()
	nsLower := strings.ToLower(nsTok.Text)
	ns, ok := ast.NamespaceAliases[nsLower]
	if !ok {
		p.errorf(nsTok.Pos, "unknown identifier '%s' — expected math./query./variable./temp./context.", nsTok.Text)
	}
	p.expect(token.Dot, "'.'")
	memberTok := p.expect(token.Ident, "member name")
	member := memberTok.Text
	// Real Molang member names may themselves contain further dots — e.g.
	// v.st.can_place_in_cave is namespace "variable", member
	// "st.can_place_in_cave" (a single, opaque, dot-containing scope key),
	// not a nested-property-access grammar. The reference tokenizer treats
	// the whole "v.st.can_place_in_cave" as one identifier and only splits
	// on the FIRST dot; greedily consuming further ".ident" segments here
	// reproduces that.
	for p.cur().Kind == token.Dot {
		p.advance()
		next := p.expect(token.Ident, "member name segment after '.'")
		member += "." + next.Text
	}
	// array.<name> is never a value on its own -- it must be indexed.
	if ns == ast.Array {
		p.expect(token.LBracket, "'[' after an array name")
		index := p.parseAssignment()
		p.expect(token.RBracket, "']'")
		return &ast.ArrayAccess{Name: member, Index: index}
	}

	id := &ast.Ident{Namespace: ns, Member: member}

	// `context.<entity>-><read>`: a variable or query read evaluated against
	// another entity. Only a context. name can be on the left, and only one
	// arrow -- a chain has nothing to mean, since what comes back from the
	// first one is a number.
	if p.cur().Kind == token.Arrow {
		arrowTok := p.advance()
		if ns != ast.Context {
			p.errorf(arrowTok.Pos, "only context.<name> can appear on the left of '->'")
		}
		read := p.parseIdentOrCall()
		switch r := read.(type) {
		case *ast.Ident:
			if r.Namespace != ast.Variable && r.Namespace != ast.Query {
				p.errorf(arrowTok.Pos, "'->' reads variable.<name> or query.<name> on the other entity")
			}
		case *ast.CallExpr:
			if r.Callee.Namespace != ast.Query {
				p.errorf(arrowTok.Pos, "only a query.<name>(...) call can be made through '->'")
			}
		case *ast.ArrowExpr:
			p.errorf(arrowTok.Pos, "'->' cannot be chained")
		default:
			p.errorf(arrowTok.Pos, "'->' must be followed by a variable or query read")
		}
		return &ast.ArrowExpr{Entity: id, Read: read}
	}

	if p.cur().Kind != token.LParen {
		return id
	}
	p.advance() // '('
	var args []ast.Expr
	if p.cur().Kind != token.RParen {
		for {
			args = append(args, p.parseAssignment())
			if p.cur().Kind == token.Comma {
				p.advance()
				continue
			}
			break
		}
	}
	p.expect(token.RParen, "')'")
	return &ast.CallExpr{Callee: id, Args: args}
}
