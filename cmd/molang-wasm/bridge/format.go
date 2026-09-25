package bridge

import (
	"errors"

	"github.com/stirante/molang-go/parser"
	"github.com/stirante/molang-go/printer"
)

// FormatOptions is what FormatSource reads. Every field is optional.
type FormatOptions struct {
	// Style is "layout" (over several lines, comments kept; the default),
	// "oneLine" (printer.Format's) or "minify" (printer.Minify's).
	Style string `json:"style,omitempty"`
	// IndentSize is the width of one level; 0 means 4. UseTabs indents with
	// tabs, each counted as IndentSize wide.
	IndentSize int  `json:"indentSize,omitempty"`
	UseTabs    bool `json:"useTabs,omitempty"`
	// LineWidth is the width lines are kept to where they can be; 0 means
	// 100.
	LineWidth int `json:"lineWidth,omitempty"`
	// Comments reads `#` to the end of a line as a comment, Templates reads
	// jsonte's `#{ ... }` as a template: both what a .molang file holds.
	Comments           bool `json:"comments,omitempty"`
	Templates          bool `json:"templates,omitempty"`
	OptionalSemicolons bool `json:"optionalSemicolons,omitempty"`
	// RangeStart and RangeEnd, UTF-16 offsets, format only the top-level
	// statements the range touches (layout style only).
	RangeStart *int `json:"rangeStart,omitempty"`
	RangeEnd   *int `json:"rangeEnd,omitempty"`
}

// FormatSourceResult is what FormatSource reports. Text replaces
// [Start, End) of the source, in UTF-16 offsets: the whole source unless a
// range was asked for.
type FormatSourceResult struct {
	OK    bool   `json:"ok"`
	Text  string `json:"text"`
	Start int    `json:"start"`
	End   int    `json:"end"`
	Error string `json:"error,omitempty"`
}

// FormatSource prints src with printer.FormatSource, or the range of it
// the options name with printer.FormatSourceRange.
func FormatSource(src string, o FormatOptions) FormatSourceResult {
	so := printer.SourceOptions{
		Layout:             printer.Layout{IndentWidth: o.IndentSize, UseTabs: o.UseTabs, MaxWidth: o.LineWidth},
		Comments:           o.Comments,
		Templates:          o.Templates,
		OptionalSemicolons: o.OptionalSemicolons,
	}
	switch o.Style {
	case "", "layout":
	case "oneLine":
		so.Style = printer.StyleOneLine
	case "minify":
		so.Style = printer.StyleMinified
	default:
		return FormatSourceResult{Error: "unknown style " + o.Style}
	}
	u16 := newUTF16Index(src)
	if o.RangeStart != nil || o.RangeEnd != nil {
		start, end := 0, u16.at(len(src))
		if o.RangeStart != nil {
			start = *o.RangeStart
		}
		if o.RangeEnd != nil {
			end = *o.RangeEnd
		}
		text, from, to, err := printer.FormatSourceRange(src, byteOffset(src, start), byteOffset(src, end), so)
		if err != nil {
			return FormatSourceResult{Error: message(err)}
		}
		return FormatSourceResult{OK: true, Text: text, Start: u16.at(from), End: u16.at(to)}
	}
	text, err := printer.FormatSource(src, so)
	if err != nil {
		return FormatSourceResult{Error: message(err)}
	}
	return FormatSourceResult{OK: true, Text: text, End: u16.at(len(src))}
}

// message is an error in the words the other methods use: a parse error's
// own message, without the source it quotes.
func message(err error) string {
	var pe *parser.Error
	if errors.As(err, &pe) {
		return pe.Msg
	}
	return err.Error()
}

// byteOffset is the byte offset of UTF-16 offset u in src, clamped to it.
func byteOffset(src string, u int) int {
	n := 0
	for i, r := range src {
		if n >= u {
			return i
		}
		if r >= 0x10000 {
			n += 2
		} else {
			n++
		}
	}
	return len(src)
}
