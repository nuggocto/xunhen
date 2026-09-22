package main

import (
	"bytes"
	"context"
	"encoding/binary"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"

	"github.com/nuggocto/xunhen/internal/history"
	"github.com/nuggocto/xunhen/internal/limits"
	"github.com/nuggocto/xunhen/internal/undofile"
)

func undoFixture(t testing.TB, name string) []byte {
	t.Helper()

	data, err := os.ReadFile(filepath.Join("../../testdata/undo", name, "history.undo"))
	if err != nil {
		t.Fatal(err)
	}

	return data
}

func TestInspectArguments(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		args   []string
		status int
	}{
		{"long help", []string{"inspect", "--help"}, 0},
		{"short help", []string{"inspect", "-h"}, 0},
		{"missing path", []string{"inspect"}, 2},
		{"missing value", []string{"inspect", "--undo"}, 2},
		{"empty path", []string{"inspect", "--undo="}, 2},
		{"duplicate input", []string{"inspect", "--undo", "one", "--undo", "two"}, 2},
		{"positional input", []string{"inspect", "file"}, 2},
		{"extra argument", []string{"inspect", "--undo", "file", "extra"}, 2},
		{"unknown flag", []string{"inspect", "--source=file"}, 2},
		{"extra help argument", []string{"inspect", "--help", "extra"}, 2},
		{"unsafe flag", []string{"inspect", "--bad\n\x1b[2J"}, 2},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var out, diagnostic bytes.Buffer
			status := run(t.Context(), tt.args, &out, &diagnostic)
			if status != tt.status {
				t.Fatalf("status = %d; stderr = %q", status, diagnostic.String())
			}

			if tt.status == 0 {
				assertOutput(t, "help", out.String(), "Usage: xunhen inspect --undo PATH")
				assertOutput(t, "stderr", diagnostic.String(), "")
				return
			}

			if out.Len() != 0 {
				t.Fatalf("usage error wrote to stdout: %q", out.String())
			}
			if strings.Count(diagnostic.String(), "\n") != 1 || strings.ContainsRune(diagnostic.String(), '\x1b') {
				t.Fatalf("unsafe diagnostic: %q", diagnostic.String())
			}
		})
	}
}

func TestInspectReadOnlyAndSafeOutput(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name, filename string
		mutate         func(*testing.T, []byte)
		missing        bool
		status         int
		want           string
	}{
		{
			name:     "no source copy",
			filename: "undo with spaces",
			want:     "node 2: parent=1",
		},
		{
			name:     "escaped filename",
			filename: "undo\x1b[2J\nfile",
			want:     `undo\x1b[2J\nfile`,
		},
		{
			name:     "invalid records",
			filename: "invalid.undo",
			mutate: func(_ *testing.T, data []byte) {
				data[10] = 2
			},
			status: 1,
			want:   "unsupported input",
		},
		{
			name:     "invalid relationships",
			filename: "invalid-graph.undo",
			mutate: func(t *testing.T, data []byte) {
				file, err := undofile.Decode(t.Context(), "fixture", bytes.NewReader(data), limits.Default())
				if err != nil {
					t.Fatal(err)
				}

				record, _ := file.Record(0)
				binary.BigEndian.PutUint32(data[record.Info().Offset+2:], 9999)
			},
			status: 1,
			want:   "invalid input",
		},
		{
			name:     "missing unsafe path",
			filename: "missing\n\x1b]52;file",
			missing:  true,
			status:   1,
			want:     `missing\n\x1b]52;file`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			data := undoFixture(t, "abandoned-branch")
			if tt.mutate != nil {
				tt.mutate(t, data)
			}

			path := filepath.Join(t.TempDir(), tt.filename)
			if !tt.missing {
				if err := os.WriteFile(path, data, 0444); err != nil {
					t.Fatal(err)
				}
			}

			var out, diagnostic bytes.Buffer
			status := run(t.Context(), []string{"inspect", "--undo", path}, &out, &diagnostic)
			if status != tt.status {
				t.Fatalf("status = %d, want %d; stderr = %q", status, tt.status, diagnostic.String())
			}

			if tt.status != 0 {
				if out.Len() != 0 {
					t.Fatalf("failed input produced a normal result: %q", out.String())
				}
				assertOutput(t, "stderr", diagnostic.String(), tt.want)
			} else {
				assertOutput(t, "stderr", diagnostic.String(), "")

				for _, want := range []string{
					tt.want,
					"Reference node: 3",
					"not supplied or verified",
					"node 0: retained root",
					"Reconstruction: not performed",
				} {
					assertOutput(t, "stdout", out.String(), want)
				}
				if strings.Contains(out.String(), "func experiment") {
					t.Fatal("inspection disclosed saved source text")
				}
			}

			for _, stream := range []string{out.String(), diagnostic.String()} {
				for _, char := range stream {
					if char != '\n' && (char < 32 || char > 126) {
						t.Fatalf("unsafe output character %U", char)
					}
				}
			}

			if !tt.missing {
				after, err := os.ReadFile(path)
				if err != nil || !bytes.Equal(data, after) {
					t.Fatalf("inspection changed input: %v", err)
				}
			}
		})
	}
}

func TestUndoInputTypes(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		mode       os.FileMode
		symlink    bool
		inputLimit int64
		good       bool
	}{
		{name: "regular", good: true},
		{name: "symlink to regular", symlink: true, good: true},
		{name: "directory", mode: os.ModeDir},
		{name: "fifo", mode: os.ModeNamedPipe},
		{name: "symlink to fifo", mode: os.ModeNamedPipe, symlink: true},
		{name: "above byte limit", inputLimit: 16},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			dir := t.TempDir()
			path := filepath.Join(dir, "input")

			switch tt.mode {
			case os.ModeDir:
				path = dir

			case os.ModeNamedPipe:
				if err := syscall.Mkfifo(path, 0600); err != nil {
					t.Fatal(err)
				}

			case 0:
				if err := os.WriteFile(path, undoFixture(t, "linear"), 0600); err != nil {
					t.Fatal(err)
				}

			default:
				t.Fatalf("unhandled input mode %v", tt.mode)
			}

			if tt.symlink {
				link := filepath.Join(dir, "link")
				if err := os.Symlink(path, link); err != nil {
					t.Fatal(err)
				}
				path = link
			}

			lim := limits.Default()
			if tt.inputLimit != 0 {
				lim.InputBytes = tt.inputLimit
			}

			file, err := loadUndo(t.Context(), path, lim)
			if tt.good {
				if err != nil || file == nil {
					t.Fatalf("regular input rejected: %v", err)
				}
			} else if err == nil || file != nil {
				t.Fatal("invalid file type or excessive input accepted")
			}
		})
	}
}

func TestInspectCancellation(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		args []string
	}{
		{"before input", []string{"inspect", "--undo", "not opened"}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			cancel()

			var out, diagnostic bytes.Buffer
			status := run(ctx, tt.args, &out, &diagnostic)
			if status != 130 || out.Len() != 0 {
				t.Fatalf("cancelled invocation: status %d, output %q", status, out.String())
			}
		})
	}
}

func TestInspectionOutputBudget(t *testing.T) {
	t.Parallel()

	lim := limits.Default()
	file, err := loadUndo(t.Context(), "../../testdata/undo/linear/history.undo", lim)
	if err != nil {
		t.Fatal(err)
	}

	h, err := history.New(t.Context(), file, lim)
	if err != nil {
		t.Fatal(err)
	}

	complete, err := inspectionText(t.Context(), "fixture", h, lim.OutputBytes)
	if err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name    string
		budget  int
		cancel  bool
		wantErr bool
	}{
		{name: "cancel rendering", budget: len(complete), cancel: true, wantErr: true},
		{name: "bounded rendering", budget: len(complete) - 1, wantErr: true},
		{name: "exact rendering limit", budget: len(complete)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			if tt.cancel {
				cancel()
			}

			text, err := inspectionText(ctx, "fixture", h, tt.budget)
			if tt.wantErr {
				if err == nil || text != "" {
					t.Fatalf("incomplete output exposed as success: %q, %v", text, err)
				}
			} else if err != nil || text != complete {
				t.Fatalf("exact output limit failed: %v", err)
			}
		})
	}
}
