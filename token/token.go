// Package token defines the lexical tokens of Molang.
package token

// Kind identifies the type of a token.
type Kind uint8

const (
	Illegal Kind = iota
	EOF

	Number // 3, 3.14, .5
	String // 'quoted'
	Ident  // math, query, temp, variable, q, v, t, foo (namespace or member piece)

	// Keywords
	Return
	Loop
	ForEach
	Break
	Continue
	This
	True
	False

	// Operators / punctuation
	Plus     // +
	Minus    // -
	Star     // *
	Slash    // /
	Assign   // =
	Eq       // ==
	Ne       // !=
	Lt       // <
	Le       // <=
	Gt       // >
	Ge       // >=
	And      // &&
	Or       // ||
	Not      // !
	Arrow    // ->
	Coalesce // ??
	Question // ?
	Colon    // :
	Semi     // ;
	Comma    // ,
	Dot      // .
	LParen   // (
	RParen   // )
	LBracket // [
	RBracket // ]
	LBrace   // {
	RBrace   // }
)

var names = map[Kind]string{
	Illegal:  "ILLEGAL",
	EOF:      "EOF",
	Number:   "NUMBER",
	String:   "STRING",
	Ident:    "IDENT",
	Return:   "return",
	Loop:     "loop",
	ForEach:  "for_each",
	Break:    "break",
	Continue: "continue",
	This:     "this",
	True:     "true",
	False:    "false",
	Plus:     "+",
	Minus:    "-",
	Star:     "*",
	Slash:    "/",
	Assign:   "=",
	Eq:       "==",
	Ne:       "!=",
	Lt:       "<",
	Le:       "<=",
	Gt:       ">",
	Ge:       ">=",
	And:      "&&",
	Or:       "||",
	Not:      "!",
	Arrow:    "->",
	Coalesce: "??",
	Question: "?",
	Colon:    ":",
	Semi:     ";",
	Comma:    ",",
	Dot:      ".",
	LParen:   "(",
	RParen:   ")",
	LBrace:   "{",
	RBrace:   "}",
}

func (k Kind) String() string {
	if s, ok := names[k]; ok {
		return s
	}
	return "UNKNOWN"
}

// Keywords maps lower-cased identifier text to its keyword Kind.
var Keywords = map[string]Kind{
	"return":   Return,
	"loop":     Loop,
	"for_each": ForEach,
	"break":    Break,
	"continue": Continue,
	"this":     This,
	"true":     True,
	"false":    False,
}

// Token is a single lexical token.
type Token struct {
	Kind Kind
	Text string // raw source text (original casing preserved)
	Pos  int    // byte offset in source
	Num  float64
}
