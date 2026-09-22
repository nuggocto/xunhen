package termtext_test

import (
	"context"
	"errors"
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

			got, err := termtext.Escape(t.Context(), tt.input, 100)
			if err != nil || got != tt.want {
				t.Fatalf("escaped = %q, %v; want %q", got, err, tt.want)
			}
		})
	}
}

func TestTerminalTextBoundsAndCancellation(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		limit   int
		cancel  bool
		wantErr bool
	}{
		{"exact expanded limit", 4, false, false},
		{"above expanded limit", 3, false, true},
		{"unset limit", 0, false, true},
		{"excess limit", 17 << 20, false, true},
		{"cancelled", 100, true, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			if tt.cancel {
				cancel()
			}

			got, err := termtext.Escape(ctx, "\x1b", tt.limit)
			if (err != nil) != tt.wantErr || (err != nil && got != "") {
				t.Fatalf("text = %q, error = %v", got, err)
			}

			if tt.cancel && !errors.Is(err, context.Canceled) {
				t.Fatalf("lost cancellation: %v", err)
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

			got, err := termtext.EscapeDisplay(t.Context(), tt.input, 100)
			if err != nil || got != tt.want {
				t.Fatalf("display = %q, %v; want %q", got, err, tt.want)
			}
		})
	}
}
