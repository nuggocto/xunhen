// Package termtext renders untrusted bytes as terminal-safe text.
package termtext

import (
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"
)

// Escape renders controls, non-ASCII runes, invalid UTF-8, quotes, and
// backslashes as Go-style escapes, so the result is printable ASCII and
// unambiguous. The result is at most four times as long as text: a single
// invalid byte or ASCII control is the largest expansion, as in \x1b.
func Escape(text string) string {
	return escape(text, false)
}

// EscapeDisplay keeps source text readable. Tabs, quotes, backslashes, and
// printable Unicode pass through; controls, format characters, and invalid
// bytes become Go-style escapes. Because backslashes are literal, a source
// line containing `\x1b` looks the same as an escaped ESC. Use raw export when
// the exact bytes matter. The result is at most four times as long as text.
func EscapeDisplay(text string) string {
	return escape(text, true)
}

func escape(text string, display bool) string {
	if plain(text, display) {
		return text
	}

	var out strings.Builder
	var scratch [16]byte

	for offset := 0; offset < len(text); {
		r, size := utf8.DecodeRuneInString(text[offset:])
		raw := text[offset : offset+size]
		offset += size

		if display && readable(r, size) {
			out.WriteString(raw)
			continue
		}

		part := strconv.AppendQuoteToASCII(scratch[:0], raw)
		out.Write(part[1 : len(part)-1])
	}

	return out.String()
}

// plain reports whether every byte already renders as itself: printable
// ASCII, plus tab for display, and no quote or backslash for Escape. Most
// source lines pass, and they need no copy.
func plain(text string, display bool) bool {
	for i := range len(text) {
		b := text[i]
		switch {
		case b == '\t' && display:
		case b < 0x20 || b > 0x7e:
			return false
		case (b == '"' || b == '\\') && !display:
			return false
		}
	}

	return true
}

// readable reports whether display text can show a rune as itself. A tab
// moves the cursor but cannot start a terminal control sequence.
func readable(r rune, size int) bool {
	if r == utf8.RuneError && size == 1 {
		return false
	}

	return r == '\t' || unicode.IsGraphic(r)
}
