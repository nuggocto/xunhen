// Package termtext renders untrusted bytes as bounded, terminal-safe text.
package termtext

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/nuggocto/xunhen/internal/limits"
)

// Escape renders controls, non-ASCII runes, invalid UTF-8, and literal
// backslashes as Go-style escapes. It emits only printable ASCII. maxBytes
// applies after escaping, so an input cannot evade the output budget.
func Escape(ctx context.Context, text string, maxBytes int) (string, error) {
	if maxBytes <= 0 || maxBytes > limits.Default().OutputBytes {
		return "", errors.New("invalid terminal-text byte budget")
	}

	var out strings.Builder
	var scratch [16]byte

	for offset, steps := 0, 0; offset < len(text); steps++ {
		if steps%4096 == 0 {
			if err := ctx.Err(); err != nil {
				return "", err
			}
		}

		_, size := utf8.DecodeRuneInString(text[offset:])
		quoted := strconv.AppendQuoteToASCII(scratch[:0], text[offset:offset+size])
		part := quoted[1 : len(quoted)-1]
		if len(part) > maxBytes-out.Len() {
			return "", errors.New("terminal text exceeds output byte budget")
		}

		out.Write(part)
		offset += size
	}

	if err := ctx.Err(); err != nil {
		return "", err
	}

	return out.String(), nil
}
