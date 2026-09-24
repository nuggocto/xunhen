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
		{name: "inspection help", args: []string{"help", "inspect"}, out: "Usage: xunhen inspect --undo PATH"},
		{name: "show help topic", args: []string{"help", "show"}, out: "Usage: xunhen show"},
		{name: "planned command", args: []string{"browse"}, status: 1, err: "browse is not available"},
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
		other      string // stream that still works
		want       string
	}{
		{name: "help to failing stdout", args: []string{"--help"}, other: "stderr", want: "cannot write output"},
		{name: "inspection to failing stdout", args: []string{"inspect", "--undo", undo}, other: "stderr", want: "cannot write output"},
		{name: "state to failing stdout", args: []string{"show", "--undo", undo, "--base", base, "--node", "2"}, other: "stderr", want: "cannot write output"},
		{name: "diff to failing stdout", args: []string{"diff", "--undo", undo, "--base", base, "--from", "2", "--to", "3"}, other: "stderr", want: "cannot write output"},
		{name: "diagnostic to failing stderr", args: []string{"unknown"}, failStderr: true, other: "stdout"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var working bytes.Buffer
			stdout, stderr := io.Writer(failedWriter{}), io.Writer(&working)
			if tt.failStderr {
				stdout, stderr = &working, failedWriter{}
			}

			if status := run(t.Context(), tt.args, stdout, stderr); status != 1 {
				t.Fatalf("exit status = %d, want 1", status)
			}

			assertOutput(t, tt.other, working.String(), tt.want)
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

type failedWriter struct{}

func (failedWriter) Write([]byte) (int, error) {
	return 0, errors.New("output unavailable")
}

// TestCommandArgumentContract applies one set of argument rules to every
// command that reads inputs. Each invalid case fails before a file is opened,
// so the paths are fake. Adding a command means adding one row to commands.
func TestCommandArgumentContract(t *testing.T) {
	t.Parallel()

	commands := []struct {
		name  string
		mode  string   // input form, for the case names
		valid []string // flag/value pairs forming one valid invocation
		nodes []string // node selector flags
	}{
		{name: "inspect", mode: "explicit", valid: []string{"--undo", "u"}},
		{name: "show", mode: "explicit", valid: []string{"--undo", "u", "--base", "b", "--node", "1"}, nodes: []string{"--node"}},
		{name: "diff", mode: "explicit", valid: []string{"--undo", "u", "--base", "b", "--from", "1", "--to", "2"}, nodes: []string{"--from", "--to"}},
		{name: "inspect", mode: "source", valid: []string{"--source", "s", "--undo-dir", "d"}},
		{name: "show", mode: "source", valid: []string{"--source", "s", "--undo-dir", "d", "--node", "1"}, nodes: []string{"--node"}},
		{name: "diff", mode: "source", valid: []string{"--source", "s", "--undo-dir", "d", "--from", "1", "--to", "2"}, nodes: []string{"--from", "--to"}},
	}

	type contractCase struct {
		name, stdout, diagnostic string
		args                     []string
		status                   int
	}

	var tests []contractCase
	for _, c := range commands {
		with := func(extra ...string) []string {
			return slices.Concat([]string{c.name}, c.valid, extra)
		}
		usage := "Usage: xunhen " + c.name
		label := c.name + " " + c.mode

		tests = append(tests,
			contractCase{name: label + "/long help", args: []string{c.name, "--help"}, stdout: usage},
			contractCase{name: label + "/short help", args: []string{c.name, "-h"}, stdout: usage},
			contractCase{name: label + "/help topic", args: []string{"help", c.name}, stdout: usage},
			contractCase{name: label + "/help among flags", args: with("--help"), status: exitUsage, diagnostic: "must be used alone"},
			contractCase{name: label + "/repeated flag", args: with(c.valid[0], c.valid[1]), status: exitUsage, diagnostic: "only once"},
			contractCase{name: label + "/trailing positional", args: with("extra"), status: exitUsage, diagnostic: "expected " + c.name},
			contractCase{name: label + "/leading positional", args: slices.Concat([]string{c.name, "extra"}, c.valid), status: exitUsage},
			contractCase{name: label + "/unknown flag", args: with("--bogus"), status: exitUsage},
			contractCase{name: label + "/unsafe flag", args: with("--bad\n\x1b[2J"), status: exitUsage},
			contractCase{name: label + "/flag without value", args: with("--undo"), status: exitUsage},
			contractCase{name: label + "/empty path", args: slices.Concat([]string{c.name, "--undo="}, c.valid[2:]), status: exitUsage},
		)

		for i := 0; i < len(c.valid); i += 2 {
			missing := slices.Concat([]string{c.name}, c.valid[:i], c.valid[i+2:])
			tests = append(tests, contractCase{name: label + "/missing " + c.valid[i], args: missing, status: exitUsage})
		}

		for _, flag := range c.nodes {
			// Replace the valid selector so the flag still appears only once.
			selector := func(value string) []string {
				args := slices.Concat([]string{c.name}, c.valid)
				args[slices.Index(args, flag)+1] = value
				return args
			}

			for _, value := range []string{"-1", "+1", "0x1", "1_0", ""} {
				tests = append(tests, contractCase{
					name: label + "/" + flag + " " + value, args: selector(value),
					status: exitUsage, diagnostic: flag + " requires a non-negative decimal ID",
				})
			}
			tests = append(tests, contractCase{
				name: label + "/" + flag + " overflow", args: selector("2147483648"),
				status: exitUsage, diagnostic: flag + " exceeds the supported ID range",
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

			// A usage error is one escaped diagnostic line and no result.
			diagnostic := stderr.String()
			if stdout.Len() != 0 || !strings.HasPrefix(diagnostic, "xunhen: ") || strings.Count(diagnostic, "\n") != 1 {
				t.Fatalf("stdout = %q, stderr = %q; want one diagnostic line", stdout.String(), diagnostic)
			}
			for _, char := range strings.TrimSuffix(diagnostic, "\n") {
				if char < 0x20 || char > 0x7e {
					t.Fatalf("diagnostic contains unsafe character %U: %q", char, diagnostic)
				}
			}
			if !strings.Contains(diagnostic, tt.diagnostic) {
				t.Fatalf("stderr = %q, want it to contain %q", diagnostic, tt.diagnostic)
			}
		})
	}
}
