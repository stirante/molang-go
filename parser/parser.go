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

	// loopDepth counts the loop()/for_each() constructs being parsed, so a
	// `break` can be refused where the game refuses it: outside any loop.
	loopDepth int

	// parens counts the parentheses written directly around each
	// expression. The tree keeps no parentheses, but two of the game's
	// checks look at the text as written: what may stand on the left of `=`
	// (`(v.x)` is a parenthesis there, not a variable), and how deep the
	// expression nests, where every pair is a level (see ast.Depth).
	parens map[ast.Expr]int

	// plain records the CondBlockStmts that stand for a bare `{ ... }`
	// rather than a conditional, for the same depth count.
	plain map[*ast.CondBlockStmt]bool

	// nest counts the constructs being parsed that each put at least one
	// node above what they contain in the game's tree: a parenthesis, a
	// call, a block, a negation, an arm, a value being assigned. A source
	// nested past ast.DepthLimit is refused as soon as the parser reaches
	// that depth, so the recursion here stays bounded whatever the input;
	// the exact count, which also sees operator chains and statement
	// lists, is made once the tree is built.
	nest int
}

// enter records entering a construct that nests what follows one level
// deeper in the game's tree, and refuses the source once the game would.
func (p *parser) enter(pos int) {
	p.nest++
	if p.nest >= ast.DepthLimit {
		p.errorf(pos, "%s", ast.DepthOverflowMessage)
	}
}

func (p *parser) leave() { p.nest-- }

// chainGuard refuses a run of one level's binary operators long enough that
// the game's tree for it is bound to exceed ast.DepthLimit: a run groups to
// the left, so its first operand sits one level below the top for every
// operator in the run. The exact depth is measured after parsing; this only
// keeps the parser from building a chain it will throw away.
func (p *parser) chainGuard(operands, pos int) {
	if operands > ast.DepthLimit {
		p.errorf(pos, "%s", ast.DepthOverflowMessage)
	}
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
func ParseWith(source string, ext Extensions) (*ast.Program, error) {
	prog, _, err := parse(source, ext)
	return prog, err
}

// Depth reports how deep the game's tree for source nests -- the number
// ast.DepthLimit is compared against, an expression's first node being
// depth 0 -- so a tool can say how close an expression is to being refused.
// It is measured from the source as written, parentheses included. A source
// that does not parse, including one refused for nesting past the limit,
// returns its parse error.
func Depth(source string) (int, error) {
	prog, g, err := parse(source, Extensions{})
	if err != nil {
		return 0, err
	}
	return ast.Depth(prog, g), nil
}

// parse is ParseWith, also returning what the parser wrote down about the
// source's grouping.
func parse(source string, ext Extensions) (prog *ast.Program, g *ast.Grouping, err error) {
	if strings.TrimSpace(source) == "" {
		return nil, nil, &Error{Msg: "empty expression", Source: source, Pos: 0}
	}

	toks, lexErr := lexer.TokenizeWith(source, ext)
	if lexErr != nil {
		if le, ok := lexErr.(*lexer.Error); ok {
			return nil, nil, &Error{Msg: le.Msg, Source: source, Pos: le.Pos}
		}
		return nil, nil, &Error{Msg: lexErr.Error(), Source: source, Pos: 0}
	}

	p := &parser{toks: toks, source: source, parens: map[ast.Expr]int{}, plain: map[*ast.CondBlockStmt]bool{}}

	defer func() {
		if r := recover(); r != nil {
			if pe, ok := r.(*Error); ok {
				prog, g, err = nil, nil, pe
				return
			}
			panic(r)
		}
	}()

	stmts, hasSemi := p.parseStatementList(token.EOF)
	if p.cur().Kind != token.EOF {
		p.errorf(p.cur().Pos, "unexpected trailing input")
	}
	if !ext.OptionalSemicolons {
		if semiErr := checkSemicolons(toks, source); semiErr != nil {
			return nil, nil, semiErr
		}
	}
	prog = &ast.Program{Stmts: stmts, HasSemicolon: hasSemi}
	g = &ast.Grouping{Parens: p.parens, PlainBlocks: p.plain}
	// The game builds its tree and then refuses it if it nests too deep.
	// Its tree is coarser than this one and counts the parentheses and
	// bare blocks the tree does not keep, which is what the parser wrote
	// down in parens and plain.
	if ast.Depth(prog, g) >= ast.DepthLimit {
		return nil, nil, &Error{Msg: ast.DepthOverflowMessage, Source: source, Pos: 0}
	}
	return prog, g, nil
}

// checkSemicolons applies the game's two semicolon rules. Both are about
// tokens rather than grammar, and the game states them that way, so they are
// checked over the token stream once the grammar has accepted it:
//
//   - A brace section must contain a `;` somewhere between its braces.
//     `{v.a = 1}` is refused; `{v.a = 1;}` is not. A `;` inside a nested
//     brace section counts for the enclosing one too -- the rule is about
//     the tokens between the braces, not about statements.
//   - An expression containing `=` or `;` ANYWHERE, braces included, must
//     end with `;`. So `v.a = 1`, `{v.a = 1;}` and `q.x ? {v.a = 0;}` are
//     all refused on their own, and `math.sin(q.anim_time)` needs nothing.
//
// Checked after parsing rather than before, so a source that is wrong in both
// ways reports the grammar problem first -- that is the one an author has to
// fix before the rest means anything.
//
// The messages keep the game's wording, so an error from here and one from
// the game's content log can be matched up.
func checkSemicolons(toks []token.Token, source string) *Error {
	type brace struct {
		pos  int
		semi bool
	}
	var open []brace
	complex := false
	last := token.EOF
	for _, t := range toks {
		if t.Kind == token.EOF {
			break
		}
		last = t.Kind
		switch t.Kind {
		case token.Assign:
			complex = true
		case token.Semi:
			complex = true
			for i := range open {
				open[i].semi = true
			}
		case token.LBrace:
			open = append(open, brace{pos: t.Pos})
		case token.RBrace:
			// The grammar has already balanced the braces, so open is never
			// empty here.
			b := open[len(open)-1]
			open = open[:len(open)-1]
			if !b.semi {
				return &Error{
					Msg: "Brace sections must only contain semicolon-delimited expressions, " +
						"even if only one expression is contained.",
					Source: source, Pos: b.pos,
				}
			}
		}
	}
	if complex && last != token.Semi {
		return &Error{
			Msg:    "complex expressions (contains either '=' or ';') must end with a ';'",
			Source: source, Pos: len(source),
		}
	}
	return nil
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
			if p.cur().Kind == token.Comma {
				p.errorf(p.cur().Pos, "Unexpected %s operator not inside an arguments list for a query, loop, or math function", ast.OpComma)
			}
			p.errorf(p.cur().Pos, "expected ';' or end of block, got %s", p.cur().Kind)
		}
	}
	if deadAfter >= 0 && deadAfter != len(stmts)-1 {
		p.errorf(deadPos, "unreachable statements after %s.", deadName)
	}
	return stmts, hasSemi
}

// terminatingStmtName names the statement kinds that end execution where they
// stand, the way the game names them, or "" for everything else.
//
// It deliberately looks at the statement itself and not into it. The game
// checks each statement list on its own: a statement is unreachable only
// when a return, break or continue precedes it IN THE SAME LIST. A
// conditional whose body returns is not terminating -- the conditional may
// not fire -- and that is the whole early-exit guard idiom, so treating it
// as terminating would refuse the most common shape in real content.
func terminatingStmtName(s ast.Stmt) string {
	switch s.(type) {
	case *ast.ReturnStmt:
		return ast.OpReturn.String()
	case *ast.BreakStmt:
		return ast.OpBreak.String()
	case *ast.ContinueStmt:
		return ast.OpContinue.String()
	}
	return ""
}

func (p *parser) parseBlock() *ast.Block {
	tok := p.expect(token.LBrace, "'{'")
	// A block is two levels in the game's tree: the braces, and the `;`
	// list they hold.
	p.enter(tok.Pos)
	p.enter(tok.Pos)
	defer p.leave()
	defer p.leave()
	stmts, _ := p.parseStatementList(token.RBrace)
	p.expect(token.RBrace, "'}'")
	return &ast.Block{Stmts: stmts}
}

func (p *parser) parseStatement() ast.Stmt {
	switch p.cur().Kind {
	case token.Return:
		tok := p.advance()
		if p.cur().Kind == token.Semi || p.cur().Kind == token.RBrace || p.cur().Kind == token.EOF {
			return &ast.ReturnStmt{Value: nil}
		}
		p.enter(tok.Pos)
		defer p.leave()
		return &ast.ReturnStmt{Value: p.parseAssignment()}
	case token.Break:
		// A break has to be inside a loop() or for_each() -- anywhere
		// inside: a nested block, a conditional's arm, a block arm. The
		// game refuses the expression otherwise. A continue is NOT
		// checked the same way: the game lets one stand anywhere, and at
		// run time one outside a loop does nothing.
		tok := p.advance()
		if p.loopDepth == 0 {
			p.errorf(tok.Pos, "%s", ast.BreakOutsideLoopMessage)
		}
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
		return p.plainBlock(p.parseBlock())
	default:
		x := p.parseAssignment()
		if cb, ok := x.(*ast.CondBlockStmt); ok {
			return cb
		}
		return &ast.ExprStmt{X: x}
	}
}

// parseForEach parses `for_each(<variable>, <source>, { body })`.
//
// The game checks two things about the header when it loads: that there are
// three arguments, and that the first is a plain variable. or temp. name --
// not a member path such as `v.a.b`, which is a member access rather than a
// variable. The second is NOT checked -- any expression parses there. What
// it iterates at run time is decided then, not here (see eval's
// compileForEach). Each refusal is worded as the game words it.
//
// The one special shape is a bare `array.<name>`, which is not an expression
// anywhere else: this is the one position where an array NAME, rather than
// an indexed element, is legal, because the loop wants the array itself. It
// becomes an *ast.Ident in the Array namespace.
func (p *parser) parseForEach() ast.Stmt {
	tok := p.advance() // 'for_each'
	p.expect(token.LParen, "'('")
	p.loopDepth++
	defer func() { p.loopDepth-- }()
	p.enter(tok.Pos)
	defer p.leave()

	const shape = "for_each requires three parameters - a variable to represent an element of an array, an expression resulting in an array, and an expression to run per element of that array."
	varPos := p.cur().Pos
	if p.cur().Kind != token.Ident {
		p.errorf(varPos, "%s", shape)
	}
	loopVar := p.parseIdentOrCall()
	id, ok := loopVar.(*ast.Ident)
	if !ok || (id.Namespace != ast.Temp && id.Namespace != ast.Variable) || strings.Contains(id.Member, ".") {
		p.errorf(varPos, "%s", shape)
	}
	if p.cur().Kind != token.Comma {
		p.errorf(p.cur().Pos, "%s", shape)
	}
	p.advance()

	var source ast.Expr
	if name, ok := p.bareArrayName(); ok {
		source = &ast.Ident{Namespace: ast.Array, Member: name}
	} else {
		source = p.parseAssignment()
	}

	if p.cur().Kind != token.Comma {
		p.errorf(p.cur().Pos, "%s", shape)
	}
	p.advance()
	if p.cur().Kind != token.LBrace {
		p.errorf(p.cur().Pos, "%s", shape)
	}
	body := p.parseBlock()
	p.expect(token.RParen, "')'")
	return &ast.ForEachStmt{Var: id, Source: source, Body: body}
}

// bareArrayName consumes `array.<name>` when it stands alone as a whole
// argument -- followed directly by ',' -- and reports the name. Anything
// else, including `array.<name>[i]`, is left untouched for the expression
// parser.
func (p *parser) bareArrayName() (string, bool) {
	i := p.pos
	at := func(k token.Kind) bool { return i < len(p.toks) && p.toks[i].Kind == k }
	if !at(token.Ident) {
		return "", false
	}
	if ns, known := ast.NamespaceAliases[strings.ToLower(p.toks[i].Text)]; !known || ns != ast.Array {
		return "", false
	}
	i++
	name := ""
	for at(token.Dot) {
		i++
		if !at(token.Ident) {
			return "", false
		}
		if name != "" {
			name += "."
		}
		name += p.toks[i].Text
		i++
	}
	if name == "" || !at(token.Comma) {
		return "", false
	}
	p.pos = i
	return name, true
}

// parseLoop parses `loop(<count>, { body })`. The game wants exactly those
// two arguments, the second a brace section, and says so in one message.
func (p *parser) parseLoop() ast.Stmt {
	tok := p.advance() // 'loop'
	p.expect(token.LParen, "'('")
	p.loopDepth++
	defer func() { p.loopDepth-- }()
	p.enter(tok.Pos)
	defer p.leave()

	const shape = "loop requires two parameters - an expression resulting in a number of times to loop, and a {}-delimited expression to loop."
	count := p.parseAssignment()
	if p.cur().Kind != token.Comma {
		p.errorf(p.cur().Pos, "%s", shape)
	}
	p.advance()
	if p.cur().Kind != token.LBrace {
		p.errorf(p.cur().Pos, "%s", shape)
	}
	body := p.parseBlock()
	if p.cur().Kind != token.RParen {
		p.errorf(p.cur().Pos, "%s", shape)
	}
	p.advance()
	return &ast.LoopStmt{Count: count, Body: body}
}

// ---------------------------------------------------------------------
// Expression grammar (precedence climbing, lowest to highest):
//
//	assignment  ::= nullish ('=' nullish)?             (target must be temp./variable.; `a = b = c` is refused)
//	nullish     ::= ternary ('??' ternary)*            (left-assoc; left operand must be a direct variable reference)
//	ternary     ::= or ('?' ( '{' block '}' | arm ) (':' elseArm)?)?
//	arm         ::= ternary ('=' assignment)?          (a `??` inside an arm needs parentheses)
//	elseArm     ::= '{' block '}' | ternary
//	or          ::= and ('||' and)*
//	and         ::= equality ('&&' equality)*
//	equality    ::= relational (('=='|'!=') relational)*
//	relational  ::= additive (('<'|'<='|'>'|'>=') additive)*
//	additive    ::= multiplicative (('+'|'-'|'- -') multiplicative)*
//	multiplicative ::= divisive ('*' divisive)*
//	divisive    ::= unary ('/' unary)*
//	unary       ::= ('-'|'!') unary | primary            (`- -x` is refused; see parseUnary)
//	primary     ::= operand ('->' read)?                 (one arrow only; see parseArrow)
//	operand     ::= number | string | 'true' | 'false' | 'this' | '(' assignment ')' | namespaced-ident ('(' args ')')?
//	read        ::= variable-ident | query-ident ('(' args ')')?
//
// The levels are the game's, and they are not quite C's. The game groups an
// expression in a fixed sequence of passes, one operator (or set of
// operators) per pass, each pass scanning left to right and taking the two
// neighbours of every operator it owns -- so the operators of one pass are
// left-associative among themselves, and an earlier pass binds tighter than
// a later one. The passes that matter here, in order:
//
//	->                      (before every operator, unary ones included)
//	- and ! (unary)         (a binary `-` is rewritten to `+` of a negation)
//	/
//	*                       (so `a * b / c` is `a * (b / c)`)
//	+                       (`a - b` is `a + -b`)
//	<  <=  >  >=            (one pass, so `a < b > c` is `(a < b) > c`)
//	==  !=                  (one pass)
//	&&
//	||
//	? :
//	??
//	,
//	=                       (one pass, left to right: `a = b = c` assigns to an assignment, and is refused)
//	return
//
// Everything but the `/`-before-`*` split reads as it would in C.
//
// ---------------------------------------------------------------------

func (p *parser) parseAssignment() ast.Expr {
	return p.finishAssignment(p.parseNullish())
}

// parseArm parses the value arm of a conditional. It is an assignment-level
// expression except that a `??` is not read here: the game groups `??` after
// it has grouped the conditional, so `c ? v.b ?? 1 : 2` becomes
// `(c ? v.b) ?? 1`, which it then refuses because a conditional is not a
// variable reference. Stopping at the `??` here hands it back to parseNullish
// with the conditional as its left side, and the refusal is worded the same.
func (p *parser) parseArm() ast.Expr {
	return p.finishAssignment(p.parseTernary())
}

// finishAssignment reads an optional `= value` after an already-parsed left
// side.
//
// What may stand on the left is decided by the game in two passes, and the
// wording of a refusal comes from whichever pass refuses it (see
// ast.AssignTargetProblem): a statement's left side is checked as written,
// so `(v.x) = 1` is an assignment to a parenthesis and `-v.x = 1` one to a
// negation, whatever they would simplify to; the tree is then checked member
// by member, which is where `t.a.b = 1` (a temp that is not on its own),
// `c.a.b = 1` and `a->v.b = 1` are refused. The game runs the first pass only
// on a statement-level assignment and the second on every assignment; this
// applies both to every assignment, so an assignment nested in an argument
// is held to the statement's rules too. That is stricter than the game for a
// few shapes nobody writes, such as `return (v.x) = 1;`, and documented here
// rather than modelled.
func (p *parser) finishAssignment(left ast.Expr) ast.Expr {
	if p.cur().Kind == token.Assign {
		if p.parens[left] > 0 {
			p.errorf(p.cur().Pos, ast.NonVariableAssignMessage, ast.OpLeftParenthesis)
		}
		if msg := ast.AssignTargetProblem(left); msg != "" {
			p.errorf(p.cur().Pos, "%s", msg)
		}
		id := left.(*ast.Ident)
		tok := p.advance()
		p.enter(tok.Pos)
		defer p.leave()
		value := p.parseNullish()
		// The game groups `=` left to right like any other binary operator,
		// so `v.a = v.b = 1` is `(v.a = v.b) = 1`: an assignment whose target
		// is an assignment, refused as such. `v.a = (v.b = 1)` is fine.
		if p.cur().Kind == token.Assign {
			p.errorf(p.cur().Pos, ast.NonVariableAssignMessage, ast.OpAssignment)
		}
		return &ast.AssignExpr{Target: id, Value: value}
	}
	return left
}

func (p *parser) parseTernary() ast.Expr {
	cond := p.parseOr()
	if p.cur().Kind != token.Question {
		return cond
	}
	qTok := p.advance() // '?'

	switch p.cur().Kind {
	case token.EOF, token.Semi, token.RParen, token.RBrace, token.RBracket, token.Colon, token.Comma:
		p.errorf(qTok.Pos, "the ? operator requires a following 'then' clause and an optional ':' and 'else' clause")
	}

	// The arms hang one level below the conditional.
	p.enter(qTok.Pos)
	defer p.leave()

	if p.cur().Kind == token.LBrace {
		body := p.parseBlock()
		cb := &ast.CondBlockStmt{Cond: cond, Body: body}
		if p.cur().Kind != token.Colon {
			return cb
		}
		p.advance() // ':'
		els := p.parseElseArm()
		if elseCB, ok := els.(*ast.CondBlockStmt); ok {
			cb.Else = elseCB
			return cb
		}
		// `cond ? { ... } : value` -- a block on one side and a value on the
		// other. The block becomes the Then arm of an ordinary ternary.
		return &ast.TernaryExpr{Cond: cond, Then: p.plainBlock(body), Else: els}
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

	then := p.parseArm()
	var els ast.Expr
	if p.cur().Kind == token.Colon {
		p.advance()
		els = p.parseElseArm()
	}
	return &ast.TernaryExpr{Cond: cond, Then: then, Else: els}
}

// parseElseArm parses what follows a conditional's ':'. Any mix of block and
// value is legal on the two sides -- `c ? {...} : {...}`, `c ? {...} : 1`,
// `c ? 1 : {...}` -- so this is either a plain `{ ... }` block, represented
// as a CondBlockStmt with an always-true condition, or an ordinary ternary-
// level expression, which covers a value, an else-if chain
// (`c1 ? {...} : c2 ? {...}`), and anything mixing the two.
//
// A block arm runs its statements and yields 0, unless a `return` inside it
// ends the program.
func (p *parser) parseElseArm() ast.Expr {
	if p.cur().Kind == token.LBrace {
		return p.plainBlock(p.parseBlock())
	}
	return p.parseTernary()
}

// plainBlock wraps a block as the always-true CondBlockStmt that stands for
// an unconditional `{ ... }` wherever an expression or else-clause is
// expected. See ast.CondBlockStmt. It is recorded as written bare, which
// the depth count needs: `{ ... }` is one brace node in the game's tree,
// `true ? { ... }` a conditional holding one.
func (p *parser) plainBlock(b *ast.Block) *ast.CondBlockStmt {
	cb := &ast.CondBlockStmt{Cond: &ast.BoolLit{Value: true}, Body: b}
	p.plain[cb] = true
	return cb
}

// parseNullish parses `??`, which sits between assignment and the
// conditional: `v.a ?? 1 ? 2 : 3` is `v.a ?? (1 ? 2 : 3)`, `v.a ?? 0 > 1`
// is `v.a ?? (0 > 1)`, and `v.x = v.y ?? 1` assigns the whole `??`.
//
// The game only lets a direct variable read stand on the left -- a bare
// context./variable./temp. name, see ast.IsDirectVariableRef -- and refuses
// everything else when the expression loads: a number, a query, a math call,
// an array element, `this`, a negation, an arrow, a dotted member path, a
// conditional, or another `??`. That last one is why a chain is refused:
// `??` groups to the left, so `v.a ?? v.b ?? 1` is `(v.a ?? v.b) ?? 1`, and
// the left side of the outer `??` is a `??`. It has to be written
// `v.a ?? (v.b ?? 1)`. Parentheses around a read itself change nothing,
// because the game checks the tree it built, not the text: `(v.x) ?? 1` is
// fine. Refused here at parse time, with the game's wording.
func (p *parser) parseNullish() ast.Expr {
	left := p.parseTernary()
	for p.cur().Kind == token.Coalesce {
		if !ast.IsDirectVariableRef(left) {
			p.errorf(p.cur().Pos, "found left-hand-side of ?? expression that isn't a direct-variable reference - this is unsupported at this time.")
		}
		p.advance()
		right := p.parseTernary()
		left = &ast.BinaryExpr{Op: ast.NullCoalesce, X: left, Y: right}
	}
	return left
}

// binary builds `x op y` after applying the game's two operand rules, each
// worded as the game words it:
//
//   - Arithmetic, a comparison other than == and !=, and && and || refuse
//     an operand that is not a number: a string, a resource, an assignment
//     (see ast.IsNonNumericOperand). == and != are exempt, which is what
//     lets `texture.a == texture.b` and `'a' != v.s` load. The game has
//     applied this rule since Molang version 1.17.40, and every pack this
//     package is likely to meet is past that.
//   - An array element refuses a constant folded onto it, and only that:
//     `array.a[i] + 1` is refused, `array.a[i] + v.x` is not (see
//     ast.FoldsIntoArrayElement).
//
// `a - b` is `a + (-b)` to the game, so the right side of a `-` is refused
// as a negation and the left as an addition.
//
// Checked at each binary level rather than in one place, because a
// precedence-climbing parser has no single point where an operand is "used".
func (p *parser) binary(op ast.BinaryOp, x, y ast.Expr, opTok token.Token) ast.Expr {
	if opNum := ast.BinaryExprOp(op); op != ast.CmpEq && op != ast.CmpNe {
		p.checkNumeric(opNum, x, opTok.Pos)
		if op == ast.Sub {
			p.checkNumeric(ast.OpNegate, y, opTok.Pos)
		} else {
			p.checkNumeric(opNum, y, opTok.Pos)
		}
	}
	if ast.FoldsIntoArrayElement(op, x, y) {
		p.errorf(opTok.Pos, "%s", ast.ArrayElementMathMessage)
	}
	return &ast.BinaryExpr{Op: op, X: x, Y: y}
}

// checkNumeric refuses arg as an operand of op when it is not a number.
func (p *parser) checkNumeric(op ast.Op, arg ast.Expr, pos int) {
	if ast.IsNonNumericOperand(arg) {
		p.errorf(pos, "%s", ast.NonNumericOperandMessage(op, arg))
	}
}

func (p *parser) parseOr() ast.Expr {
	left := p.parseAnd()
	for n := 1; p.cur().Kind == token.Or; n++ {
		opTok := p.advance()
		p.chainGuard(n+1, opTok.Pos)
		right := p.parseAnd()
		left = p.binary(ast.LOr, left, right, opTok)
	}
	return left
}

func (p *parser) parseAnd() ast.Expr {
	left := p.parseEquality()
	for n := 1; p.cur().Kind == token.And; n++ {
		opTok := p.advance()
		p.chainGuard(n+1, opTok.Pos)
		right := p.parseEquality()
		left = p.binary(ast.LAnd, left, right, opTok)
	}
	return left
}

func (p *parser) parseEquality() ast.Expr {
	left := p.parseRelational()
	for n := 1; ; n++ {
		var op ast.BinaryOp
		switch p.cur().Kind {
		case token.Eq:
			op = ast.CmpEq
		case token.Ne:
			op = ast.CmpNe
		default:
			return left
		}
		opTok := p.advance()
		p.chainGuard(n+1, opTok.Pos)
		right := p.parseRelational()
		left = p.binary(op, left, right, opTok)
	}
}

func (p *parser) parseRelational() ast.Expr {
	left := p.parseAdditive()
	for n := 1; ; n++ {
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
		opTok := p.advance()
		p.chainGuard(n+1, opTok.Pos)
		right := p.parseAdditive()
		left = p.binary(op, left, right, opTok)
	}
}

// parseAdditive parses `+` and `-`. To the game a binary `-` is a `+` whose
// right operand is negated, and its sign pass has one rule about two `-` in
// a row: `a - -b` is `a + b`, both signs gone (the tree has an Add, not a Sub
// of a negation -- see doubleMinus for what else that pass does).
func (p *parser) parseAdditive() ast.Expr {
	left := p.parseMultiplicative()
	for n := 1; ; n++ {
		var op ast.BinaryOp
		switch p.cur().Kind {
		case token.Plus:
			op = ast.Add
		case token.Minus:
			op = ast.Sub
		default:
			return left
		}
		opTok := p.advance()
		p.chainGuard(n+1, opTok.Pos)
		if op == ast.Sub && p.cur().Kind == token.Minus {
			p.doubleMinus()
			op = ast.Add
		}
		right := p.parseMultiplicative()
		left = p.binary(op, left, right, opTok)
	}
}

// doubleMinus is called at the second of two adjacent `-` tokens, the first
// of which followed an operand. The game's sign pass turns the pair into a
// `+`; a third `-` would then follow a `+` and be negated onto it, leaving
// a `+` with nothing on its right, which the game refuses in these words.
func (p *parser) doubleMinus() {
	tok := p.advance()
	if p.cur().Kind == token.Minus {
		p.errorf(tok.Pos, "binary %s operator at end of expression", ast.OpAdd)
	}
}

func (p *parser) parseMultiplicative() ast.Expr {
	left := p.parseDivisive()
	for n := 1; p.cur().Kind == token.Star; n++ {
		opTok := p.advance()
		p.chainGuard(n+1, opTok.Pos)
		right := p.parseDivisive()
		left = p.binary(ast.Mul, left, right, opTok)
	}
	return left
}

// parseDivisive parses `/`, which the game groups before `*`: `a * b / c`
// is `a * (b / c)`, and `a / b * c` is `(a / b) * c`.
func (p *parser) parseDivisive() ast.Expr {
	left := p.parseUnary()
	for n := 1; p.cur().Kind == token.Slash; n++ {
		opTok := p.advance()
		p.chainGuard(n+1, opTok.Pos)
		right := p.parseUnary()
		left = p.binary(ast.Div, left, right, opTok)
	}
	return left
}

// parseUnary parses `-x` and `!x`. Both refuse a non-numeric operand, and a
// negation refuses an array element: the game folds the sign onto the
// operand, and an element cannot carry one.
//
// Two `-` in a row are refused here. The game's sign pass turns a `- -` pair
// into a `+` wherever it stands; after an operand that is `a - -b` read as
// `a + b` (see parseAdditive), but at the start of an expression or after an
// operator the `+` has nothing on its left, and the game refuses it as a
// binary `+` with a missing operand. `-(-x)` is the way to write it.
func (p *parser) parseUnary() ast.Expr {
	switch p.cur().Kind {
	case token.Minus:
		tok := p.advance()
		if p.cur().Kind == token.Minus {
			p.errorf(tok.Pos, "binary %s operator at end of expression", ast.OpAdd)
		}
		p.enter(tok.Pos)
		x := p.parseUnary()
		p.leave()
		p.checkNumeric(ast.OpNegate, x, tok.Pos)
		if ast.NegatesArrayElement(x) {
			p.errorf(tok.Pos, "%s", ast.ArrayElementMathMessage)
		}
		return &ast.UnaryExpr{Op: ast.Neg, X: x}
	case token.Not:
		tok := p.advance()
		p.enter(tok.Pos)
		x := p.parseUnary()
		p.leave()
		p.checkNumeric(ast.OpLogicalNot, x, tok.Pos)
		return &ast.UnaryExpr{Op: ast.LNot, X: x}
	default:
		return p.parsePrimary()
	}
}

func (p *parser) parsePrimary() ast.Expr {
	return p.parseArrow(p.parseOperand())
}

func (p *parser) parseOperand() ast.Expr {
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
		p.enter(tok.Pos)
		p.advance()
		x := p.parseAssignment()
		p.expect(token.RParen, "')'")
		p.leave()
		p.parens[x]++
		return x
	case token.Ident:
		return p.parseIdentOrCall()
	default:
		p.errorf(tok.Pos, "unexpected token %s", tok.Kind)
		panic("unreachable")
	}
}

// parseArrow parses `<operand>-><read>`, the read of another entity's
// variable or query.
//
// The arrow is resolved before any operator, unary ones included, and after
// grouping, calls and array indexing -- so it takes exactly the operand that
// precedes it, whatever that operand is, and `-a->v.x` is `-(a->v.x)`. The
// game checks the operand on the left only for being another arrow, so a
// number, a string, `this`, a query call, an array element or a parenthesised
// expression are all accepted there; they just fail to be an entity when
// evaluated. What is checked, with the game's own wording:
//
//   - the right side must be a `variable.<name>` read or a `query.<name>`
//     read or call. A dotted member path (`v.a.b`) is a member access in the
//     game, not a variable read, so it is refused too;
//   - one arrow only. `a->b->c` is refused, and the game's message says what
//     to do instead: store `a->b` in a variable, then read through that.
func (p *parser) parseArrow(left ast.Expr) ast.Expr {
	if p.cur().Kind != token.Arrow {
		return left
	}
	arrowTok := p.advance()
	// `(a->b)->c` is the same chain with parentheses on, and the game sees
	// the same tree: an arrow whose left side is an arrow.
	if _, isArrow := left.(*ast.ArrowExpr); isArrow {
		p.errorf(arrowTok.Pos, "nested pointer statements (eg: A->B->C) are not yet supported.  Store A->B in a variable (eg: D), then use D->C")
	}
	if p.cur().Kind != token.Ident {
		p.errorf(p.cur().Pos, "right-hand-side of pointer expression did not evaluate to an entity variable or query function")
	}
	p.enter(arrowTok.Pos)
	read := p.parseIdentOrCall()
	p.leave()
	var callee *ast.Ident
	switch r := read.(type) {
	case *ast.Ident:
		callee = r
	case *ast.CallExpr:
		callee = r.Callee
	}
	if callee == nil || (callee.Namespace != ast.Variable && callee.Namespace != ast.Query) ||
		strings.Contains(callee.Member, ".") {
		p.errorf(arrowTok.Pos, "right-hand-side of pointer expression did not evaluate to an entity variable or query function")
	}
	if p.cur().Kind == token.Arrow {
		p.errorf(p.cur().Pos, "nested pointer statements (eg: A->B->C) are not yet supported.  Store A->B in a variable (eg: D), then use D->C")
	}
	return &ast.ArrowExpr{Entity: left, Read: read}
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
		bracket := p.expect(token.LBracket, "'[' after an array name")
		p.enter(bracket.Pos)
		index := p.parseAssignment()
		p.leave()
		p.expect(token.RBracket, "']'")
		return &ast.ArrayAccess{Name: member, Index: index}
	}

	id := &ast.Ident{Namespace: ns, Member: member}

	if p.cur().Kind != token.LParen {
		return id
	}
	parenTok := p.advance() // '('
	p.enter(parenTok.Pos)
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
	p.leave()
	p.expect(token.RParen, "')'")
	// A math function refuses a non-numeric argument exactly as an operator
	// does -- `math.abs('a')` and `math.max(v.a = 5, 3)` do not load. A
	// query does not: its arguments are its own business. A math name this
	// package does not know (a macro, say) is left alone here; whatever it
	// expands to is checked when compiled.
	if ns == ast.Math {
		if op, known := ast.MathOp(member); known {
			for _, a := range args {
				p.checkNumeric(op, a, parenTok.Pos)
			}
		}
	}
	return &ast.CallExpr{Callee: id, Args: args}
}
