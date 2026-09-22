package main

import (
	"bytes"
	"errors"
	"io"
	"runtime"
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
		{name: "planned command", args: []string{"show"}, status: 1, err: "show is not available"},
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

	tests := []struct {
		name       string
		args       []string
		writer     io.Writer
		failStderr bool
		diagnostic string
	}{
		{name: "stdout error", args: []string{"--help"}, writer: failedWriter{}, diagnostic: "cannot write output"},
		{name: "short stdout write", args: []string{"--version"}, writer: shortWriter{}, diagnostic: "cannot write output"},
		{name: "stderr error", args: []string{"unknown"}, writer: failedWriter{}, failStderr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var stderr bytes.Buffer
			stdout, diagnostic := tt.writer, io.Writer(&stderr)
			if tt.failStderr {
				stdout, diagnostic = io.Discard, tt.writer
			}

			if status := run(t.Context(), tt.args, stdout, diagnostic); status != 1 {
				t.Fatalf("exit status = %d, want 1", status)
			}

			assertOutput(t, "stderr", stderr.String(), tt.diagnostic)
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

type shortWriter struct{}

func (shortWriter) Write(p []byte) (int, error) {
	return len(p) / 2, nil
}
