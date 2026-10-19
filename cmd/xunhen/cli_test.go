package main

import (
	"bytes"
	"errors"
	"io"
	"runtime"
	"slices"
	"strings"
	"testing"
)

func TestCommandResponses(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		args   []string
		status int
		out    string
		err    string
	}{
		{name: "no arguments", out: "Usage:"},
		{name: "long help", args: []string{"--help"}, out: "Usage:"},
		{name: "short help", args: []string{"-h"}, out: "Usage:"},
		{name: "help command", args: []string{"help"}, out: "Usage:"},
		{name: "version flag", args: []string{"--version"}, out: "go: " + runtime.Version()},
		{name: "version command", args: []string{"version"}, out: "commit:"},
		{name: "version help", args: []string{"help", "version"}, out: "Usage: xunhen version"},
		{name: "inspection help", args: []string{"help", "inspect"}, out: "Usage: xunhen inspect FILE"},
		{name: "show help topic", args: []string{"help", "show"}, out: "Usage: xunhen show"},
		{name: "diff help topic", args: []string{"help", "diff"}, out: "Usage: xunhen diff"},
		{name: "browse help topic", args: []string{"help", "browse"}, out: "Usage: xunhen browse"},
		{name: "browse without inputs", args: []string{"browse"}, status: 2, err: "expected browse FILE"},
		// The paths do not exist. Reaching the terminal check first means no
		// input was opened, and nothing full-screen was written.
		{name: "browse without a terminal", args: []string{"browse", "--undo", "u", "--base", "b"}, status: 1, err: "browse needs an interactive terminal"},
		{name: "unknown command", args: []string{"unknown"}, status: 2, err: "unknown command"},
		{name: "unknown option", args: []string{"--unknown"}, status: 2, err: "unknown command or option"},
		{name: "unknown help topic", args: []string{"help", "unknown"}, status: 2, err: "unknown command"},
		{name: "extra help argument", args: []string{"--help", "extra"}, status: 2, err: "extra arguments"},
		{name: "extra help topic", args: []string{"help", "inspect", "extra"}, status: 2, err: "at most one"},
		{name: "extra version argument", args: []string{"version", "extra"}, status: 2, err: "extra arguments"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var stdout, stderr bytes.Buffer
			if status := run(t.Context(), tt.args, &stdout, &stderr); status != tt.status {
				t.Fatalf("exit status = %d, want %d; stderr = %q", status, tt.status, stderr.String())
			}

			assertOutput(t, "stdout", stdout.String(), tt.out)
			assertOutput(t, "stderr", stderr.String(), tt.err)
		})
	}
}

func TestUnknownArgumentsCannotControlTerminal(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		args []string
	}{
		{name: "command with clipboard escape", args: []string{"\x1b]52;c;dGVzdA==\a"}},
		{name: "help topic with forged diagnostic", args: []string{"help", "bad\nforged diagnostic\x1b[2J"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var stdout, stderr bytes.Buffer
			if status := run(t.Context(), tt.args, &stdout, &stderr); status != 2 {
				t.Fatalf("exit status = %d, want 2", status)
			}

			if stdout.Len() != 0 || strings.ContainsAny(stderr.String(), "\x1b\a") || strings.Contains(stderr.String(), "forged diagnostic") {
				t.Fatalf("unsafe response: stdout = %q; stderr = %q", stdout.String(), stderr.String())
			}
		})
	}
}

func TestWriteFailures(t *testing.T) {
	t.Parallel()

	const fixture = "../../testdata/undo/abandoned-branch/"
	undo, base := fixture+"history.undo", fixture+"base.bin"

	// One stream fails; the other must carry only the expected text.
	tests := []struct {
		name       string
		args       []string
		failStderr bool
		// accepted is how many bytes the failing stdout takes before its
		// first error, so output can fail partway through.
		accepted int
		other    string // stream that still works
		want     string
	}{
		{name: "help to failing stdout", args: []string{"--help"}, other: "stderr", want: "cannot write output"},
		{name: "inspection to failing stdout", args: []string{"inspect", "--undo", undo}, other: "stderr", want: "cannot write output"},
		{name: "state to failing stdout", args: []string{"show", "--undo", undo, "--base", base, "--node", "2"}, other: "stderr", want: "cannot write output"},
		{name: "diff to failing stdout", args: []string{"diff", "--undo", undo, "--base", base, "--from", "2", "--to", "3"}, other: "stderr", want: "cannot write output"},
		{name: "diagnostic to failing stderr", args: []string{"unknown"}, failStderr: true, other: "stdout"},
		{name: "usage hint to failing stderr", args: []string{"inspect", "source.go"}, failStderr: true, other: "stdout"},
		{name: "raw state to failing stdout", args: []string{"show", "--undo", undo, "--base", base, "--node", "2", "--raw", "--final-newline=include"}, other: "stderr", want: "cannot write output"},
		{name: "state failing partway", args: []string{"show", "--undo", undo, "--base", base, "--node", "2"}, accepted: 10, other: "stderr", want: "cannot write output"},
		{name: "diff failing partway", args: []string{"diff", "--undo", undo, "--base", base, "--from", "2", "--to", "3"}, accepted: 20, other: "stderr", want: "cannot write output"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var working bytes.Buffer
			stdout, stderr := io.Writer(&shortWriter{room: tt.accepted}), io.Writer(&working)
			if tt.failStderr {
				stdout, stderr = &working, failedWriter{}
			}

			if status := run(t.Context(), tt.args, stdout, stderr); status != 1 {
				t.Fatalf("exit status = %d, want 1", status)
			}

			assertOutput(t, tt.other, working.String(), tt.want)
			if n := strings.Count(working.String(), "cannot write output"); tt.want != "" && n != 1 {
				t.Fatalf("the failure was reported %d times: %q", n, working.String())
			}
		})
	}
}

func assertOutput(t *testing.T, stream, got, contains string) {
	t.Helper()

	if contains == "" && got != "" {
		t.Errorf("%s = %q, want empty", stream, got)
	} else if !strings.Contains(got, contains) {
		t.Errorf("%s = %q, want it to contain %q", stream, got, contains)
	}
}

// shortWriter accepts room bytes and then fails every write, as a pipe or
// disk that fills up would. A short write returns an error with it.
type shortWriter struct {
	room int
}

func (w *shortWriter) Write(p []byte) (int, error) {
	n := min(len(p), w.room)
	w.room -= n
	if n < len(p) {
		return n, errors.New("no space left")
	}
	return n, nil
}

type failedWriter struct{}

func (failedWriter) Write([]byte) (int, error) {
	return 0, errors.New("output unavailable")
}

// TestCommandArgumentContract checks the argument rules the input commands
// share, then the flags each command registers and requires. Every case fails
// before a file is opened, so the paths are fake. The shared rules run once,
// against show, because it takes both input flags and a node selector.
func TestCommandArgumentContract(t *testing.T) {
	t.Parallel()

	show := []string{"show", "--undo", "u", "--base", "b", "--node", "1"}
	withShow := func(extra ...string) []string { return slices.Concat(show, extra) }

	type contractCase struct {
		name, stdout, diagnostic string
		args                     []string
		status                   int
	}

	tests := []contractCase{
		{name: "short help", args: []string{"show", "-h"}, stdout: "Usage: xunhen show"},
		{name: "help among flags", args: withShow("--help"), status: exitUsage, diagnostic: "must be used alone"},
		{name: "repeated flag", args: withShow("--undo", "u"), status: exitUsage, diagnostic: "only once"},
		{name: "trailing positional", args: withShow("extra"), status: exitUsage, diagnostic: "cannot be combined"},
		{name: "two positionals", args: withShow("extra", "more"), status: exitUsage, diagnostic: "expected show"},
		{name: "leading positional", args: slices.Concat([]string{"show", "extra"}, show[1:]), status: exitUsage},
		{name: "unknown flag", args: withShow("--bogus"), status: exitUsage},
		{name: "unsafe flag", args: withShow("--bad\n\x1b[2J"), status: exitUsage},
		{name: "flag without value", args: withShow("--undo"), status: exitUsage},
		{name: "empty path", args: slices.Concat([]string{"show", "--undo="}, show[3:]), status: exitUsage},
	}

	for _, value := range []string{"-1", "+1", "0x1", "1_0", ""} {
		tests = append(tests, contractCase{
			name: "--node " + value, args: slices.Concat(show[:6], []string{value}),
			status: exitUsage, diagnostic: "--node requires a non-negative decimal ID",
		})
	}
	tests = append(tests, contractCase{
		name: "--node overflow", args: slices.Concat(show[:6], []string{"2147483648"}),
		status: exitUsage, diagnostic: "--node exceeds the supported ID range",
	})

	commands := []struct {
		name  string
		valid []string // flag/value pairs forming one valid invocation
		nodes []string // node selector flags
	}{
		{name: "inspect", valid: []string{"--undo", "u"}},
		{name: "show", valid: show[1:], nodes: []string{"--node"}},
		{name: "diff", valid: []string{"--undo", "u", "--base", "b", "--from", "1", "--to", "2"}, nodes: []string{"--from", "--to"}},
		{name: "browse", valid: []string{"--undo", "u", "--base", "b"}},
	}

	for _, c := range commands {
		tests = append(tests, contractCase{name: c.name + "/help", args: []string{c.name, "--help"}, stdout: "Usage: xunhen " + c.name})

		for i := 0; i < len(c.valid); i += 2 {
			missing := slices.Concat([]string{c.name}, c.valid[:i], c.valid[i+2:])
			tests = append(tests, contractCase{name: c.name + "/missing " + c.valid[i], args: missing, status: exitUsage})
		}

		// A selector registered without the node rules would accept this.
		for _, flag := range c.nodes {
			args := slices.Concat([]string{c.name}, c.valid)
			args[slices.Index(args, flag)+1] = "-1"
			tests = append(tests, contractCase{
				name: c.name + "/" + flag + " -1", args: args,
				status: exitUsage, diagnostic: flag + " requires a non-negative decimal ID",
			})
		}
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var stdout, stderr bytes.Buffer
			if status := run(t.Context(), tt.args, &stdout, &stderr); status != tt.status {
				t.Fatalf("status = %d, want %d; stderr = %q", status, tt.status, stderr.String())
			}

			if tt.status == exitSuccess {
				assertOutput(t, "stdout", stdout.String(), tt.stdout)
				assertOutput(t, "stderr", stderr.String(), "")
				return
			}

			// A usage error is escaped diagnostic lines and no result.
			diagnostic := stderr.String()
			if stdout.Len() != 0 || !strings.HasSuffix(diagnostic, "\n") {
				t.Fatalf("stdout = %q, stderr = %q; want only diagnostic lines", stdout.String(), diagnostic)
			}
			for line := range strings.SplitSeq(strings.TrimSuffix(diagnostic, "\n"), "\n") {
				if !strings.HasPrefix(line, "xunhen: ") {
					t.Fatalf("diagnostic line %q lacks the xunhen: prefix", line)
				}
				for _, char := range line {
					if char < 0x20 || char > 0x7e {
						t.Fatalf("diagnostic contains unsafe character %U: %q", char, diagnostic)
					}
				}
			}
			if !strings.Contains(diagnostic, tt.diagnostic) {
				t.Fatalf("stderr = %q, want it to contain %q", diagnostic, tt.diagnostic)
			}
		})
	}
}
