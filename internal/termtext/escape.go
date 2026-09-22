// Package termtext renders untrusted bytes as bounded, terminal-safe text.
package termtext

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/nuggocto/xunhen/internal/limits"
)

// Escape renders controls, non-ASCII runes, invalid UTF-8, quotes, and
// backslashes as Go-style escapes, so the result is printable ASCII and
// unambiguous. maxBytes applies after escaping, so an input cannot evade the
// output budget.
func Escape(ctx context.Context, text string, maxBytes int) (string, error) {
	return escape(ctx, text, maxBytes, false)
}

// EscapeDisplay keeps source text readable. Tabs, quotes, backslashes, and
// printable Unicode pass through; controls, format characters, and invalid
// bytes become Go-style escapes. Because backslashes are literal, a source
// line containing `\x1b` looks the same as an escaped ESC. Use raw export when
// the exact bytes matter.
func EscapeDisplay(ctx context.Context, text string, maxBytes int) (string, error) {
	return escape(ctx, text, maxBytes, true)
}

func escape(ctx context.Context, text string, maxBytes int, display bool) (string, error) {
	if maxBytes <= 0 || maxBytes > limits.Default().OutputBytes {
		return "", errors.New("invalid terminal-text byte budget")
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}

	var out strings.Builder
	var scratch [16]byte

	for offset := 0; offset < len(text); {
		r, size := utf8.DecodeRuneInString(text[offset:])
		raw := text[offset : offset+size]
		offset += size

		part := append(scratch[:0], raw...)
		if !display || !readable(r, size) {
			part = strconv.AppendQuoteToASCII(scratch[:0], raw)
			part = part[1 : len(part)-1]
		}

		if len(part) > maxBytes-out.Len() {
			return "", errors.New("terminal text exceeds output byte budget")
		}
		out.Write(part)
	}

	if err := ctx.Err(); err != nil {
		return "", err
	}

	return out.String(), nil
}

// readable reports whether display text can show a rune as itself. A tab
// moves the cursor but cannot start a terminal control sequence.
func readable(r rune, size int) bool {
	if r == utf8.RuneError && size == 1 {
		return false
	}

	return r == '\t' || unicode.IsGraphic(r)
}
