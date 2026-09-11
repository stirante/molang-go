// Package lexer tokenizes Molang source text.
package lexer

import (
	"fmt"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/stirante/molang-go/token"
)

// Error is a lexical error with a source position.
type Error struct {
	Msg string
	Pos int
}

func (e *Error) Error() string {
	return fmt.Sprintf("%s (at char %d)", e.Msg, e.Pos)
}

// Lexer converts Molang source into a stream of tokens.
// Extensions are additions this package can accept that vanilla Bedrock is not
// known to accept. Every one is off by default, because the value of parsing at
// all is that "this parses" means "the game will load it", and an unmarked
// extension quietly turns that into "probably".
//
// A caller that wants them asks, and then knows its input is not plain Molang.
// For a build pipeline that is the right shape anyway: parse with extensions,
// expand and fold, emit vanilla.
type Extensions struct {
	// Comments allows `#` to end of line.
	//
	// NOT ESTABLISHED as vanilla. The language's own 250 expression tests
	// never mention a comment, which is absence of evidence rather than
	// evidence of absence -- so it is offered rather than assumed. Tooling
	// that keeps Molang in files of its own, where a comment earns its
	// place, turns this on; anything checking whether a pack will load
	// leaves it off.
	Comments bool
}

// Lexer scans the source as BYTES, not runes.
//
// Every construct the grammar has -- identifiers, numbers, operators,
// keywords, the quote that opens a string -- is ASCII, so decoding UTF-8 buys
// this package nothing and costs it a great deal: the previous []rune form
// copied the whole source at 4 bytes per character up front, and then paid a
// fresh allocation to turn each token's runes BACK into a string. Scanning
// bytes makes Token.Text a substring of src, which allocates nothing at all.
//
// Non-ASCII bytes can only appear inside a string literal (or a comment, with
// the extension on), and UTF-8's continuation bytes are all >= 0x80, so a
// byte-wise search for the closing `'` can never land inside a multi-byte
// character. Anywhere else a non-ASCII byte is an error either way, and the
// error message decodes the rune so it still reads as one character.
type Lexer struct {
	src string
	pos int
	ext Extensions
}

// New creates a Lexer over src that accepts vanilla Molang only.
func New(src string) *Lexer {
	return &Lexer{src: src}
}

// NewWith creates a Lexer that also accepts the named extensions.
func NewWith(src string, ext Extensions) *Lexer {
	return &Lexer{src: src, ext: ext}
}

func (l *Lexer) peek() byte {
	if l.pos >= len(l.src) {
		return 0
	}
	return l.src[l.pos]
}

func (l *Lexer) peekAt(off int) byte {
	if l.pos+off >= len(l.src) {
		return 0
	}
	return l.src[l.pos+off]
}

func (l *Lexer) advance() byte {
	c := l.peek()
	l.pos++
	return c
}

func isDigit(c byte) bool { return c >= '0' && c <= '9' }

func isIdentStart(c byte) bool {
	return c == '_' || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')
}

func isIdentPart(c byte) bool {
	return isIdentStart(c) || isDigit(c)
}

// tokensPerByte is how many source bytes one token takes on average across
// the reference corpus (~3.3). Sizing the slice up front turns the ~14
// grow-and-copy rounds a 31KB expression used to need into one allocation.
const tokensPerByte = 3

// Tokenize lexes the entire source and returns all tokens including a
// trailing EOF token.
// Tokenize lexes vanilla Molang.
func Tokenize(src string) ([]token.Token, error) { return TokenizeWith(src, Extensions{}) }

// TokenizeWith lexes src, accepting the named extensions as well.
func TokenizeWith(src string, ext Extensions) ([]token.Token, error) {
	l := NewWith(src, ext)
	toks := make([]token.Token, 0, len(src)/tokensPerByte+8)
	for {
		tok, err := l.next()
		if err != nil {
			return nil, err
		}
		toks = append(toks, tok)
		if tok.Kind == token.EOF {
			break
		}
	}
	return toks, nil
}

func (l *Lexer) skipSpaceAndComments() {
	for {
		c := l.peek()
		if c == '#' && l.ext.Comments {
			for l.peek() != '\n' && l.peek() != 0 {
				l.advance()
			}
			continue
		}
		if c == ' ' || c == '\t' || c == '\r' || c == '\n' {
			l.advance()
			continue
		}
		break
	}
}

// op builds an n-byte operator token. Its text is a substring of the source,
// so it costs no allocation. This used to be a closure created fresh on every
// call to next(), which cost one allocation per punctuation token on top of
// the string it built.
func (l *Lexer) op(k token.Kind, n int) (token.Token, error) {
	start := l.pos
	l.pos += n
	return token.Token{Kind: k, Text: l.src[start:l.pos], Pos: start}, nil
}

func (l *Lexer) next() (token.Token, error) {
	l.skipSpaceAndComments()
	start := l.pos
	c := l.peek()
	if c == 0 {
		return token.Token{Kind: token.EOF, Pos: start}, nil
	}

	switch {
	case isDigit(c) || (c == '.' && isDigit(l.peekAt(1))):
		return l.lexNumber()
	case isIdentStart(c):
		return l.lexIdent()
	case c == '\'':
		return l.lexString()
	}

	// Two-character operators, matched as a byte pair. The previous form
	// built a two-character string (two rune-to-string conversions and a
	// concatenation, all allocating) and switched on it -- for every
	// operator in the source, including the single-character ones that
	// could never match.
	if d := l.peekAt(1); d != 0 {
		switch uint16(c)<<8 | uint16(d) {
		case '='<<8 | '=':
			return l.op(token.Eq, 2)
		case '!'<<8 | '=':
			return l.op(token.Ne, 2)
		case '<'<<8 | '=':
			return l.op(token.Le, 2)
		case '>'<<8 | '=':
			return l.op(token.Ge, 2)
		case '&'<<8 | '&':
			return l.op(token.And, 2)
		case '|'<<8 | '|':
			return l.op(token.Or, 2)
		case '?'<<8 | '?':
			return l.op(token.Coalesce, 2)
		}
	}

	switch c {
	case '+':
		return l.op(token.Plus, 1)
	case '-':
		// `->` before `-`: the arrow is one token, and a minus followed by
		// a greater-than is not a thing anyone writes by accident.
		if l.peekAt(1) == '>' {
			return l.op(token.Arrow, 2)
		}
		return l.op(token.Minus, 1)
	case '*':
		return l.op(token.Star, 1)
	case '/':
		return l.op(token.Slash, 1)
	case '=':
		return l.op(token.Assign, 1)
	case '<':
		return l.op(token.Lt, 1)
	case '>':
		return l.op(token.Gt, 1)
	case '!':
		return l.op(token.Not, 1)
	case '?':
		return l.op(token.Question, 1)
	case ':':
		return l.op(token.Colon, 1)
	case ';':
		return l.op(token.Semi, 1)
	case ',':
		return l.op(token.Comma, 1)
	case '.':
		return l.op(token.Dot, 1)
	case '(':
		return l.op(token.LParen, 1)
	case ')':
		return l.op(token.RParen, 1)
	case '[':
		return l.op(token.LBracket, 1)
	case ']':
		return l.op(token.RBracket, 1)
	case '{':
		return l.op(token.LBrace, 1)
	case '}':
		return l.op(token.RBrace, 1)
	}

	// Decode the rune rather than reporting the leading byte, so a stray
	// non-ASCII character is named as itself in the message.
	r, _ := utf8.DecodeRuneInString(l.src[start:])
	return token.Token{}, &Error{Msg: fmt.Sprintf("unexpected character %q", r), Pos: start}
}

func (l *Lexer) lexNumber() (token.Token, error) {
	start := l.pos
	for isDigit(l.peek()) {
		l.advance()
	}
	if l.peek() == '.' && isDigit(l.peekAt(1)) {
		l.advance()
		for isDigit(l.peek()) {
			l.advance()
		}
	} else if l.peek() == '.' {
		// lone trailing dot, e.g. "3." — accept it too.
		l.advance()
	}
	// optional exponent
	if l.peek() == 'e' || l.peek() == 'E' {
		save := l.pos
		l.advance()
		if l.peek() == '+' || l.peek() == '-' {
			l.advance()
		}
		if isDigit(l.peek()) {
			for isDigit(l.peek()) {
				l.advance()
			}
		} else {
			l.pos = save
		}
	}
	// A trailing `f` marks the literal as single-precision, the way it does
	// in C. Molang accepts it on every literal shape -- `1.0f`, `0.0f`,
	// `123.456f`, `1e10f` -- and packs use it heavily, because the values
	// being written are float32 and the suffix says so.
	//
	// It carries no meaning HERE: every number in this package is already
	// rounded to float32 at the points the language rounds. The suffix only
	// has to be consumed rather than left for the parser, which used to see
	// it as an identifier and reject the whole expression. That is the worst
	// failure a tool like this can produce -- refusing something the game
	// loads without complaint.
	//
	// The suffix is consumed after the exponent, so `1e10f` works. Whether a
	// bare `1f` with no dot and no exponent is legal in game is NOT
	// established; it is accepted here because rejecting it would need a rule
	// nothing supports either.
	text := l.src[start:l.pos]
	if c := l.peek(); c == 'f' || c == 'F' {
		l.advance()
	}
	f, err := strconv.ParseFloat(text, 64)
	if err != nil {
		return token.Token{}, &Error{Msg: fmt.Sprintf("invalid number %q", text), Pos: start}
	}
	return token.Token{Kind: token.Number, Text: text, Pos: start, Num: f}, nil
}

// hasUpperASCII reports whether text contains a letter the keyword lookup
// would have to fold. Real Molang is overwhelmingly lower-case already, and
// checking first means the common identifier costs no allocation at all --
// strings.ToLower allocates unconditionally.
func hasUpperASCII(text string) bool {
	for i := 0; i < len(text); i++ {
		if c := text[i]; c >= 'A' && c <= 'Z' {
			return true
		}
	}
	return false
}

func (l *Lexer) lexIdent() (token.Token, error) {
	start := l.pos
	for isIdentPart(l.peek()) {
		l.advance()
	}
	text := l.src[start:l.pos]
	lower := text
	if hasUpperASCII(text) {
		lower = strings.ToLower(text)
	}
	if kw, ok := token.Keywords[lower]; ok {
		return token.Token{Kind: kw, Text: text, Pos: start}, nil
	}
	return token.Token{Kind: token.Ident, Text: text, Pos: start}, nil
}

func (l *Lexer) lexString() (token.Token, error) {
	start := l.pos
	l.advance() // opening '
	content := l.pos
	for {
		c := l.peek()
		if c == 0 {
			return token.Token{}, &Error{Msg: "unterminated string literal", Pos: start}
		}
		if c == '\'' {
			break
		}
		l.advance()
	}
	// Text is the content between the quotes; Pos is the opening quote. The
	// body is copied out as a substring rather than rebuilt character by
	// character through a strings.Builder.
	text := l.src[content:l.pos]
	l.advance() // closing '
	return token.Token{Kind: token.String, Text: text, Pos: start}, nil
}
