package main

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func TestExecutable(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	binary := filepath.Join(dir, "xunhen")
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Minute)
	defer cancel()
	build := exec.CommandContext(ctx, "go", "build", "-trimpath", "-ldflags=-X main.version=v0.0.0-test", "-o", binary, ".")
	build.Env = append(os.Environ(), "GOWORK=off", "GOENV=off", "GOTOOLCHAIN=local", "GOPROXY=off", "GOSUMDB=off", "GOFLAGS=-mod=readonly", "CGO_ENABLED=0")
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build executable: %v\n%s", err, output)
	}

	tests := []struct {
		name         string
		args         []string
		status       int
		out          string
		err          string
		closedStdout bool
	}{
		{name: "help", args: []string{"--help"}, out: "Usage:"},
		{name: "stamped version", args: []string{"--version"}, out: "xunhen v0.0.0-test\n"},
		{name: "invalid invocation", args: []string{"unknown"}, status: 2, err: "unknown command"},
		{name: "unavailable operation", args: []string{"show"}, status: 1, err: "not available"},
		{name: "closed output pipe", args: []string{"--version"}, status: 1, err: "cannot write output", closedStdout: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			command := exec.CommandContext(ctx, binary, tt.args...)
			command.Dir = dir
			// The installed command must not need editor or Go executables.
			command.Env = []string{"PATH=", "HOME=" + dir, "LC_ALL=C"}
			var stdout, stderr bytes.Buffer
			command.Stdout = &stdout
			command.Stderr = &stderr
			if tt.closedStdout {
				reader, writer, err := os.Pipe()
				if err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() {
					if err := writer.Close(); err != nil {
						t.Error(err)
					}
				})
				if err := reader.Close(); err != nil {
					t.Fatal(err)
				}
				command.Stdout = writer
			}
			err := command.Run()
			var exitErr *exec.ExitError
			if err != nil && !errors.As(err, &exitErr) {
				t.Fatalf("run executable: %v", err)
			}
			if status := command.ProcessState.ExitCode(); status != tt.status {
				t.Fatalf("exit status = %d, want %d; stderr = %q", status, tt.status, stderr.String())
			}
			assertOutput(t, "stdout", stdout.String(), tt.out)
			assertOutput(t, "stderr", stderr.String(), tt.err)
		})
	}
}
