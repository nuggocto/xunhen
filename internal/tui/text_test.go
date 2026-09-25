package tui

import (
	"context"
	"fmt"
	"os/exec"
	"slices"
	"strings"
	"testing"
	"time"
	"unicode"
)

// Export commands are pasted into a shell. Each quoted argument must reach
// the command as exactly the original bytes, and must display as printable
// text, which is a separate question from being safe to paste.
func TestShellQuoting(t *testing.T) {
	t.Parallel()

	bash, err := exec.LookPath("bash")
	if err != nil {
		t.Skip("bash is not installed")
	}

	tests := []struct {
		name, arg, want string
	}{
		{name: "plain path", arg: "src/retry.go", want: "src/retry.go"},
		{name: "spaces", arg: "my history.undo", want: "'my history.undo'"},
		{name: "single quote", arg: "it's.go", want: `'it'\''s.go'`},
		{name: "shell syntax", arg: "$(rm -rf ~)`x`;|&*?", want: "'$(rm -rf ~)`x`;|&*?'"},
		{name: "unicode", arg: "\u5c0b\u75d5.go", want: "'\u5c0b\u75d5.go'"},
		{name: "empty", arg: "", want: "''"},
		{name: "leading dash", arg: "-x.undo", want: "-x.undo"},
		{name: "terminal escape", arg: "a\x1b[2Jb", want: `$'a\x1b[2Jb'`},
		{name: "newline and quote", arg: "a\n'b\\", want: `$'a\x0a\'b\\'`},
		{name: "invalid UTF-8", arg: "a\xffb", want: `$'a\xffb'`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			quoted := shellQuote(tt.arg)
			if quoted != tt.want {
				t.Fatalf("quoted %q as %q, want %q", tt.arg, quoted, tt.want)
			}
			for _, r := range quoted {
				if !unicode.IsPrint(r) {
					t.Fatalf("quoted form %q holds %U", quoted, r)
				}
			}

			ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
			defer cancel()
			out, err := exec.CommandContext(ctx, bash, "-c", "printf '%s' "+quoted).Output()
			if err != nil {
				t.Fatal(err)
			}
			if string(out) != tt.arg {
				t.Fatalf("bash read %q as %q", quoted, out)
			}
		})
	}
}

// The export command must reach the shell as the exact arguments that
// loaded the history, across its continuation lines, and no line may be so
// wide that it needs thousands of columns. Running the displayed lines
// through bash, with printf in place of xunhen, checks both.
func TestExportInstructions(t *testing.T) {
	t.Parallel()

	bash, err := exec.LookPath("bash")
	if err != nil {
		t.Skip("bash is not installed")
	}

	many := []string{"--source", "retry.go"}
	for i := range 32 {
		many = append(many, "--undo-dir", fmt.Sprintf("/undo/%03d/%s", i, strings.Repeat("d", 150)))
	}

	tests := []struct {
		name   string
		inputs []string
	}{
		{name: "explicit inputs", inputs: []string{"--undo", "history.undo", "--base", "my retry.go"}},
		{name: "unusual paths", inputs: []string{"--undo", "it's \x1b.undo", "--base", "\u5c0b\u75d5.go"}},
		{name: "the most undo directories", inputs: many},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			lines := exportLines(Labels{Inputs: tt.inputs}, 42)
			first := slices.Index(lines, "  xunhen show \\")
			last := slices.IndexFunc(lines, func(line string) bool { return strings.HasSuffix(line, "> recovered.go") })
			if first < 0 || last < first {
				t.Fatalf("no command in the instructions:\n%s", strings.Join(lines, "\n"))
			}
			for _, line := range lines {
				if len(line) > 200 {
					t.Fatalf("a %d-column line: %q", len(line), line)
				}
			}

			script := strings.Join(lines[first:last+1], "\n")
			script = strings.Replace(script, "xunhen show", "printf '%s\\0'", 1)
			script = strings.TrimSuffix(script, " > recovered.go")

			ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
			defer cancel()
			out, err := exec.CommandContext(ctx, bash, "-c", script).Output()
			if err != nil {
				t.Fatal(err)
			}

			want := append(slices.Clone(tt.inputs), "--node", "42", "--raw", "--final-newline=include")
			if got := strings.Split(strings.TrimSuffix(string(out), "\x00"), "\x00"); !slices.Equal(got, want) {
				t.Fatalf("bash received %q, want %q", got, want)
			}
		})
	}
}
