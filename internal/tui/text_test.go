package tui

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
	"unicode"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/nuggocto/xunhen/internal/termtext"
)

// Export commands are pasted into a shell. Each quoted argument must reach
// the command as exactly the original bytes, and must display as printable
// text, which is a separate question from being safe to paste.
func TestShellQuoting(t *testing.T) {
	t.Parallel()

	bash, err := exec.LookPath("bash")
	if err != nil {
		t.Fatal("bash is required to check shell quoting")
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
		{name: "combining run too long to draw", arg: "a" + strings.Repeat("\u0301", 16) + ".go", want: "$'a" + strings.Repeat(`\xcc\x81`, 16) + ".go'"},
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
// wide that it needs thousands of columns. The command is read back from
// the drawn screen, under both width methods, because drawing escapes some
// printable text, and an escape inside single quotes names other bytes.
// Running those rows through bash, redirection included, with a function in
// place of xunhen that writes its arguments, checks what a user would copy.
func TestExportInstructions(t *testing.T) {
	t.Parallel()

	bash, err := exec.LookPath("bash")
	if err != nil {
		t.Fatal("bash is required to check shell quoting")
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
		{name: "a run of combining marks", inputs: []string{"--undo", "history.undo", "--base", "a" + strings.Repeat("\u0301", 16) + ".go"}},
		{name: "invalid UTF-8", inputs: []string{"--undo", "hist\xffory.undo", "--base", "retry.go"}},
		{name: "the most undo directories", inputs: many},
	}
	methods := []struct {
		name   string
		method termtext.Method
	}{
		{name: "rune widths", method: termtext.Runes},
		{name: "cluster widths", method: termtext.Clusters},
	}

	for _, tt := range tests {
		for _, m := range methods {
			t.Run(tt.name+" by "+m.name, func(t *testing.T) {
				t.Parallel()

				b := newBrowser(t, 1000, 60)
				s := load(t, readFixture(t, "abandoned-branch").loader())
				s.labels.Inputs = tt.inputs
				b.loaded(s)
				if m.method == termtext.Clusters {
					b.m.Update(tea.ModeReportMsg{Mode: ansi.ModeUnicodeCore, Value: ansi.ModeSet})
				}
				b.press("e")

				var rows []string
				for _, row := range b.screen() {
					rows = append(rows, strings.TrimRight(row, " "))
				}
				first := slices.Index(rows, "  (set -C; xunhen show \\")
				last := slices.IndexFunc(rows, func(row string) bool { return strings.HasSuffix(row, "> recovered.go)") })
				if first < 0 || last < first {
					t.Fatalf("no command on the screen:\n%s", strings.Join(rows, "\n"))
				}
				for _, row := range rows {
					if width := ansi.StringWidth(row); width > 200 {
						t.Fatalf("a %d-column row: %q", width, row)
					}
				}

				dir := t.TempDir()
				runDrawn(t, bash, dir, strings.Join(rows[first:last+1], "\n"))
				out, err := os.ReadFile(filepath.Join(dir, "recovered.go"))
				if err != nil {
					t.Fatal(err)
				}

				want := slices.Concat([]string{"show"}, tt.inputs, []string{"--node", "3", "--raw", "--final-newline=include"})
				if got := strings.Split(strings.TrimSuffix(string(out), "\x00"), "\x00"); !slices.Equal(got, want) {
					t.Fatalf("xunhen received %q, want %q", got, want)
				}
			})
		}
	}
}

// runDrawn runs the export command as drawn, in dir, with xunhen defined as
// a bash function that writes its arguments, NUL-separated. A function
// rather than a script avoids executing a file the test just wrote.
func runDrawn(t *testing.T, bash, dir, command string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, bash, "-c", "xunhen() { printf '%s\\0' \"$@\"; }\n"+command)
	cmd.Dir = dir
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("bash failed on the drawn command: %v\n%s\n%s", err, out, command)
	}
}

// A shell empties a redirection target before the command starts, so the
// suggested export must not replace an existing file: a source or base
// named recovered.go would be lost before xunhen could read it. The command
// has to fail with the file untouched and xunhen never run.
func TestExportCommandKeepsExistingFiles(t *testing.T) {
	t.Parallel()

	bash, err := exec.LookPath("bash")
	if err != nil {
		t.Fatal("bash is required to run the export command")
	}

	command := strings.Join(exportLines(Labels{Inputs: []string{"--undo", "h.undo", "--base", "recovered.go"}}, 3), "\n")
	start := strings.Index(command, "(set -C;")
	end := strings.Index(command, "> recovered.go)")
	if start < 0 || end < start {
		t.Fatalf("no command in the instructions:\n%s", command)
	}
	command = command[start : end+len("> recovered.go)")]

	dir := t.TempDir()
	base := filepath.Join(dir, "recovered.go")
	if err := os.WriteFile(base, []byte("package sample\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	ran := filepath.Join(dir, "ran")

	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, bash, "-c", "xunhen() { : > ran; }\n"+command)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("the command replaced an existing file:\n%s", out)
	}
	if data, err := os.ReadFile(base); err != nil || string(data) != "package sample\n" {
		t.Fatalf("recovered.go now holds %q (%v)", data, err)
	}
	if _, err := os.Stat(ran); err == nil {
		t.Fatal("xunhen ran although the redirection failed")
	}
}
