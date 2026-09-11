package lexer_test

import (
	"testing"

	"github.com/stirante/molang-go/lexer"
	"github.com/stirante/molang-go/token"
)

func TestTokenizeBasic(t *testing.T) {
	// The trailing comment needs the extension; the rest of the line is plain
	// vanilla and is what this test is actually about.
	toks, err := lexer.TokenizeWith("temp.x = 3.5 + query.foo(1, 'hi') # comment\n",
		lexer.Extensions{Comments: true})
	if err != nil {
		t.Fatal(err)
	}
	want := []token.Kind{
		token.Ident, token.Dot, token.Ident, token.Assign, token.Number, token.Plus,
		token.Ident, token.Dot, token.Ident, token.LParen, token.Number, token.Comma, token.String, token.RParen,
		token.EOF,
	}
	if len(toks) != len(want) {
		t.Fatalf("got %d tokens, want %d: %+v", len(toks), len(want), toks)
	}
	for i, k := range want {
		if toks[i].Kind != k {
			t.Errorf("token %d: got %v want %v", i, toks[i].Kind, k)
		}
	}
}

func TestNoModuloToken(t *testing.T) {
	if _, err := lexer.Tokenize("7 % 3"); err == nil {
		t.Fatal("expected lex error for '%', there is no modulo operator")
	}
}

// A `#` comment is an EXTENSION, not vanilla, so it has to be asked for. Both
// halves are pinned: without the extension the `#` is an unknown character,
// with it the rest of the line disappears.
func TestCommentToEndOfLineIsAnExtension(t *testing.T) {
	const src = "1 # this is a comment ? : ; { }\n+ 2"

	if _, err := lexer.Tokenize(src); err == nil {
		t.Error("a comment lexed as vanilla Molang; it is an extension and must be asked for")
	}

	toks, err := lexer.TokenizeWith(src, lexer.Extensions{Comments: true})
	if err != nil {
		t.Fatal(err)
	}
	kinds := []token.Kind{token.Number, token.Plus, token.Number, token.EOF}
	if len(toks) != len(kinds) {
		t.Fatalf("got %d tokens, want %d: %+v", len(toks), len(kinds), toks)
	}
}

func TestKeywordsCaseInsensitive(t *testing.T) {
	toks, err := lexer.Tokenize("RETURN Loop BREAK Continue TRUE False")
	if err != nil {
		t.Fatal(err)
	}
	want := []token.Kind{token.Return, token.Loop, token.Break, token.Continue, token.True, token.False, token.EOF}
	for i, k := range want {
		if toks[i].Kind != k {
			t.Errorf("token %d: got %v want %v", i, toks[i].Kind, k)
		}
	}
}

// The lexer scans bytes, not runes. Non-ASCII text can only legally appear
// inside a string literal (or a comment, with the extension on), and these
// tests pin that the byte-wise scan handles it exactly as a rune-wise one
// would: the literal's bytes survive untouched, and the closing quote is
// never found inside a multi-byte character.
func TestStringLiteralPreservesUTF8(t *testing.T) {
	for _, want := range []string{
		"zażółć gęślą jaźń", // 2-byte sequences
		"日本語",               // 3-byte sequences
		"🙂🚀",                // 4-byte sequences (surrogate pairs in UTF-16)
		"mixed ąćę 日本 🙂 ok",
		"", // empty literal
	} {
		toks, err := lexer.Tokenize("'" + want + "'")
		if err != nil {
			t.Fatalf("%q: %v", want, err)
		}
		if len(toks) != 2 || toks[0].Kind != token.String {
			t.Fatalf("%q: got %+v, want one STRING and EOF", want, toks)
		}
		if toks[0].Text != want {
			t.Errorf("string literal body: got %q want %q", toks[0].Text, want)
		}
	}
}

// A UTF-8 continuation byte is always >= 0x80, so no byte of a multi-byte
// character can ever be mistaken for the ASCII quote that ends the literal.
// This pins that with a literal whose characters bracket the quote's code
// point on both sides.
func TestQuoteIsNeverFoundInsideAMultiByteRune(t *testing.T) {
	const body = "§’＇\U0001f600" // section sign, right single quote, FULLWIDTH APOSTROPHE, emoji
	toks, err := lexer.Tokenize("'" + body + "' + 1")
	if err != nil {
		t.Fatal(err)
	}
	if toks[0].Kind != token.String || toks[0].Text != body {
		t.Fatalf("string literal: got %+v, want body %q", toks[0], body)
	}
	want := []token.Kind{token.String, token.Plus, token.Number, token.EOF}
	if len(toks) != len(want) {
		t.Fatalf("got %d tokens, want %d: %+v", len(toks), len(want), toks)
	}
	for i, k := range want {
		if toks[i].Kind != k {
			t.Errorf("token %d: got %v want %v", i, toks[i].Kind, k)
		}
	}
}

// Token.Pos is documented as a BYTE offset. It used to be a rune index,
// which only agreed with the doc comment for pure-ASCII source.
func TestPosIsAByteOffset(t *testing.T) {
	// '日本語' is 1 + 9 + 1 = 11 bytes, then a space, so '+' starts at 12.
	toks, err := lexer.Tokenize("'日本語' + 1")
	if err != nil {
		t.Fatal(err)
	}
	wantPos := []int{0, 12, 14, 15}
	for i, want := range wantPos {
		if toks[i].Pos != want {
			t.Errorf("token %d (%v): pos %d, want %d", i, toks[i].Kind, toks[i].Pos, want)
		}
	}
}

// Outside a string literal a non-ASCII character is an error -- and the
// message has to name the whole character, not the leading byte of its
// encoding, or it reads as mojibake to whoever has to fix the expression.
func TestNonASCIIOutsideAStringIsAnErrorNamingTheRune(t *testing.T) {
	for _, tc := range []struct {
		src     string
		wantMsg string
		wantPos int
	}{
		{"ż", `unexpected character 'ż'`, 0},
		{"1 + ż", `unexpected character 'ż'`, 4},
		{"v.ż", `unexpected character 'ż'`, 2},
		{"日", `unexpected character '日'`, 0},
		// Invalid UTF-8 must be rejected rather than panic or hang; the
		// decoder yields U+FFFD for it.
		{"\xff", `unexpected character '�'`, 0},
		{"1 + \x80\x80", `unexpected character '�'`, 4},
	} {
		_, err := lexer.Tokenize(tc.src)
		if err == nil {
			t.Errorf("%q: lexed without error, want a lex error", tc.src)
			continue
		}
		le, ok := err.(*lexer.Error)
		if !ok {
			t.Errorf("%q: got %T, want *lexer.Error", tc.src, err)
			continue
		}
		if le.Msg != tc.wantMsg {
			t.Errorf("%q: message %q, want %q", tc.src, le.Msg, tc.wantMsg)
		}
		if le.Pos != tc.wantPos {
			t.Errorf("%q: pos %d, want %d", tc.src, le.Pos, tc.wantPos)
		}
	}
}

// An unterminated literal containing multi-byte characters must report the
// opening quote and not run off the end of the source.
func TestUnterminatedUTF8StringIsReported(t *testing.T) {
	_, err := lexer.Tokenize("1 + 'ząb 日本")
	if err == nil {
		t.Fatal("expected an unterminated string literal error")
	}
	le, ok := err.(*lexer.Error)
	if !ok {
		t.Fatalf("got %T, want *lexer.Error", err)
	}
	if le.Msg != "unterminated string literal" {
		t.Errorf("message %q", le.Msg)
	}
	if le.Pos != 4 {
		t.Errorf("pos %d, want 4 (the opening quote)", le.Pos)
	}
}

// A comment (extension) is skipped byte-wise too: a multi-byte character in
// it must not swallow the newline that ends it.
func TestCommentWithUTF8EndsAtTheNewline(t *testing.T) {
	toks, err := lexer.TokenizeWith("1 # ką 日本 🙂\n+ 2", lexer.Extensions{Comments: true})
	if err != nil {
		t.Fatal(err)
	}
	want := []token.Kind{token.Number, token.Plus, token.Number, token.EOF}
	if len(toks) != len(want) {
		t.Fatalf("got %d tokens, want %d: %+v", len(toks), len(want), toks)
	}
	for i, k := range want {
		if toks[i].Kind != k {
			t.Errorf("token %d: got %v want %v", i, toks[i].Kind, k)
		}
	}
}

// Identifiers are ASCII-only, so the case folding the keyword lookup does
// must not be tempted into a Unicode fold. The Turkish dotless i is the
// classic trap: strings.ToLower leaves 'İ' as a multi-byte character, and
// it must not turn a name into a keyword or a keyword into a name.
func TestUTF8NeverFoldsIntoAKeyword(t *testing.T) {
	// 'İ' cannot start an identifier at all, so this is a lex error rather
	// than a sneaky `if`-like keyword match.
	if _, err := lexer.Tokenize("İf"); err == nil {
		t.Error("a non-ASCII letter lexed as part of an identifier")
	}
	// The ASCII keyword still folds, in both cases.
	toks, err := lexer.Tokenize("RETURN")
	if err != nil {
		t.Fatal(err)
	}
	if toks[0].Kind != token.Return {
		t.Errorf("got %v, want Return", toks[0].Kind)
	}
}
