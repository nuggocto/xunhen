package main

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"github.com/nuggocto/xunhen/internal/limits"
	"github.com/nuggocto/xunhen/internal/undofile"
)

func TestExecutable(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	binary := filepath.Join(dir, "xunhen")

	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Minute)
	defer cancel()

	build := exec.CommandContext(ctx, "go", "build",
		"-trimpath", "-ldflags=-X main.version=v0.0.0-test", "-o", binary, ".")
	build.Env = append(os.Environ(),
		"GOWORK=off",
		"GOENV=off",
		"GOTOOLCHAIN=local",
		"GOPROXY=off",
		"GOSUMDB=off",
		"GOFLAGS=-mod=readonly",
		"CGO_ENABLED=0",
	)
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build executable: %v\n%s", err, output)
	}

	undo := undoFixture(t, "abandoned-branch")
	undoPath := filepath.Join(dir, "history with spaces.undo")
	if err := os.WriteFile(undoPath, undo, 0444); err != nil {
		t.Fatal(err)
	}
	base, err := os.ReadFile("../../testdata/undo/abandoned-branch/base.bin")
	if err != nil {
		t.Fatal(err)
	}
	basePath := filepath.Join(dir, "matching base.go")
	if err := os.WriteFile(basePath, base, 0444); err != nil {
		t.Fatal(err)
	}

	corruptPath := filepath.Join(dir, "corrupt.undo")
	if err := os.WriteFile(corruptPath, undo[:len(undo)-1], 0444); err != nil {
		t.Fatal(err)
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
		{name: "unavailable operation", args: []string{"diff"}, status: 1, err: "not available"},
		{
			name: "inspection without source or editor",
			args: []string{"inspect", "--undo", undoPath},
			out:  "node 2: parent=1",
		},
		{
			name: "recover abandoned experiment",
			args: []string{"show", "--undo", undoPath, "--base", basePath, "--node", "2", "--raw", "--final-newline=include"},
			out:  "package sample\n\nfunc experiment() int { return 42 }\n",
		},
		{
			name:         "recovery with closed output pipe",
			args:         []string{"show", "--undo", undoPath, "--base", basePath, "--node", "2", "--raw", "--final-newline=include"},
			status:       1,
			err:          "cannot write output",
			closedStdout: true,
		},
		{
			name:   "truncated inspection",
			args:   []string{"inspect", "--undo", corruptPath},
			status: 1,
			err:    "truncated input",
		},
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

	after, err := os.ReadFile(undoPath)
	if err != nil || !bytes.Equal(undo, after) {
		t.Fatalf("executable changed undo input: %v", err)
	}
	after, err = os.ReadFile(basePath)
	if err != nil || !bytes.Equal(base, after) {
		t.Fatalf("executable changed base input: %v", err)
	}

	t.Run("interrupt while stdout is blocked", func(t *testing.T) {
		checkBlockedOutputInterrupt(t, binary, dir)
	})
}

func checkBlockedOutputInterrupt(t *testing.T, binary, dir string) {
	t.Helper()

	path := filepath.Join(dir, "large.undo")
	if err := os.WriteFile(path, longInspection(t), 0444); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, binary, "inspect", "--undo", path)
	cmd.Env = []string{"PATH=", "HOME=" + dir, "LC_ALL=C"}

	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}

	fd := stdout.(*os.File).Fd()
	if _, _, errno := syscall.Syscall(syscall.SYS_FCNTL, fd, syscall.F_SETPIPE_SZ, 4096); errno != 0 {
		t.Fatal(errno)
	}

	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() {
		cancel()
		if cmd.ProcessState == nil {
			// Wait reaps the child even after an earlier assertion fails.
			_ = cmd.Wait()
		}
	}()

	var first [1]byte
	if _, err := stdout.Read(first[:]); err != nil {
		t.Fatalf("wait for inspection output: %v", err)
	}

	// Leave the pipe undrained after the readiness byte. This synchronizes the
	// interrupt with output without a timing sleep.
	if err := cmd.Process.Signal(os.Interrupt); err != nil {
		t.Fatal(err)
	}

	err = cmd.Wait()
	var exit *exec.ExitError
	if !errors.As(err, &exit) || exit.ProcessState.Sys().(syscall.WaitStatus).Signal() != syscall.SIGINT {
		t.Fatalf("interrupt did not terminate blocked output: %v; stderr = %q", err, stderr.String())
	}
}

func longInspection(t *testing.T) []byte {
	t.Helper()

	data := undoFixture(t, "linear")
	file, err := undofile.Decode(t.Context(), "fixture", bytes.NewReader(data), limits.Default())
	if err != nil {
		t.Fatal(err)
	}

	first, _ := file.Record(0)
	second, _ := file.Record(1)
	start, end := int(first.Info().Offset), int(second.Info().Offset)

	prefix := bytes.Clone(data[:start])
	saved := int(binary.BigEndian.Uint32(prefix[47:51]))

	const count = 256
	markers := map[int]uint32{
		59: 1,     // oldest root
		63: count, // newest header
		67: 0,     // next redo
		71: count, // header count
		75: count, // last allocated sequence
		79: count, // timeline position
	}
	for offset, value := range markers {
		binary.BigEndian.PutUint32(prefix[offset+saved:], value)
	}

	out := prefix
	for i := uint32(1); i <= count; i++ {
		record := bytes.Clone(data[start:end])
		binary.BigEndian.PutUint32(record[2:], i-1)

		child := i + 1
		if i == count {
			child = 0
		}

		binary.BigEndian.PutUint32(record[6:], child)
		binary.BigEndian.PutUint32(record[18:], i)
		out = append(out, record...)
	}

	return append(out, 0xe7, 0xaa)
}
