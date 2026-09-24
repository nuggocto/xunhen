package termtext_test

import (
	"testing"

	"github.com/nuggocto/xunhen/internal/termtext"
)

func TestTerminalSafeText(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name, input, want string
	}{
		{"plain", "history with spaces.undo", "history with spaces.undo"},
		{"terminal escape", "\x1b]52;c;test\a", `\x1b]52;c;test\a`},
		{"forged lines", "one\ntwo\rthree\t", `one\ntwo\rthree\t`},
		{"invalid bytes", "a\xffb", `a\xffb`},
		{"unicode controls", "\u0085\u202e", `\u0085\u202e`},
		{"unicode text", "尋痕", `\u5c0b\u75d5`},
		{"literal escapes", `\n"`, `\\n\"`},
		{"empty", "", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := termtext.Escape(tt.input); got != tt.want {
				t.Fatalf("escaped = %q, want %q", got, tt.want)
			}
		})
	}
}

// Callers size their buffers from the input, so no input may expand more
// than fourfold in either mode.
func TestEscapeExpansion(t *testing.T) {
	t.Parallel()

	tests := []struct{ name, input string }{
		{name: "ASCII control", input: "\x1b"},
		{name: "invalid byte", input: "\xff"},
		{name: "C1 control", input: "\u0085"},
		{name: "format character", input: "\u202e"},
		{name: "rune outside the BMP", input: "\U0001f600"},
		{name: "quote and backslash", input: `"\\`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			for _, escaped := range []string{termtext.Escape(tt.input), termtext.EscapeDisplay(tt.input)} {
				if len(escaped) > 4*len(tt.input) {
					t.Fatalf("%q escaped to %d bytes, more than four times %d", tt.input, len(escaped), len(tt.input))
				}
			}
		})
	}
}

func TestDisplayText(t *testing.T) {
	t.Parallel()

	tests := []struct{ name, input, want string }{
		{name: "readable Unicode", input: "// café 尋痕", want: "// café 尋痕"},
		{name: "source punctuation", input: "\tfmt.Println(\"a\\n\")", want: "\tfmt.Println(\"a\\n\")"},
		{name: "terminal controls", input: "a\x1b[31m\a\r\x7f\u0085", want: `a\x1b[31m\a\r\x7f\u0085`},
		{name: "line separators", input: "a\n\u2028b", want: `a\n\u2028b`},
		{name: "bidi formatting", input: "a\u202eb", want: `a\u202eb`},
		{name: "invalid byte", input: "a\xffb", want: `a\xffb`},
		{name: "encoded replacement character", input: "\ufffd", want: "\ufffd"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := termtext.EscapeDisplay(tt.input); got != tt.want {
				t.Fatalf("display = %q, want %q", got, tt.want)
			}
		})
	}
}
