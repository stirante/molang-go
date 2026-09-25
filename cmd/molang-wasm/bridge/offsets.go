package bridge

import "unicode/utf8"

// utf16Index converts byte offsets into a source to UTF-16 code-unit offsets.
//
// Everything this package reports is positioned for an editor, and editors
// in the VS Code family count positions in UTF-16 code units: a JavaScript
// string's own indexing. Go strings are UTF-8, so every byte offset the
// parser produces has to be translated, and the two disagree as soon as a
// string literal holds anything outside ASCII -- 'é' is two bytes and one
// unit, an emoji four bytes and two units.
//
// Built once per source, so each lookup afterwards is an index.
type utf16Index []int32

func newUTF16Index(src string) utf16Index {
	idx := make(utf16Index, len(src)+1)
	u := int32(0)
	for i := 0; i < len(src); {
		r, size := utf8.DecodeRuneInString(src[i:])
		// The bytes inside a multi-byte character are not positions anyone
		// can ask about, but they are given the character's own offset so a
		// stray lookup lands on the character rather than off the end.
		for k := 0; k < size; k++ {
			idx[i+k] = u
		}
		// A byte that is not valid UTF-8 decodes as one RuneError of size
		// 1. It cannot come from JavaScript, whose strings reach Go already
		// converted (a lone surrogate becomes U+FFFD, one unit either way),
		// so counting it as one unit keeps the arithmetic consistent.
		if r >= 0x10000 {
			u += 2
		} else {
			u++
		}
		i += size
	}
	idx[len(src)] = u
	return idx
}

// at returns the UTF-16 offset of byte offset b, clamped to the source.
func (x utf16Index) at(b int) int {
	if b < 0 {
		return 0
	}
	if b >= len(x) {
		return int(x[len(x)-1])
	}
	return int(x[b])
}
