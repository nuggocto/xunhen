package main

import (
	"bytes"
	"context"
	"path/filepath"
	"testing"

	"github.com/nuggocto/xunhen/internal/diff"
	"github.com/nuggocto/xunhen/internal/history"
	"github.com/nuggocto/xunhen/internal/limits"
)

const experimentAgainstChoice = "--- node 2\n+++ node 3\n@@ -1,3 +1,3 @@\n package sample\n \n" +
	"-func experiment() int { return 42 }\n+func chosen() int { return 1 }\n"

func TestDiffRecovery(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name, fixture, from, to string
		base                    string // fixture base when empty
		want, diagnostic        string
		status                  int
	}{
		{name: "abandoned experiment against the chosen fix", fixture: "abandoned-branch", from: "2", to: "3", want: experimentAgainstChoice},
		{
			name: "reverse direction", fixture: "abandoned-branch", from: "3", to: "2",
			want: "--- node 3\n+++ node 2\n@@ -1,3 +1,3 @@\n package sample\n \n" +
				"-func chosen() int { return 1 }\n+func experiment() int { return 42 }\n",
		},
		{name: "identical states print nothing", fixture: "abandoned-branch", from: "3", to: "3"},
		{
			name: "retained root against the reference", fixture: "linear", from: "0", to: "3",
			want: "--- node 0\n+++ node 3\n@@ -1,2 +1,2 @@\n-stem\n" +
				" unchanged anchor: this text is not present in any saved edit\n+leaf\n",
		},
		{
			// Comparison happens on raw bytes; only the rendered line is escaped.
			name: "terminal controls are escaped after comparison", fixture: "terminal-controls", from: "0", to: "1",
			want: "--- node 0\n+++ node 1\n@@ -1 +1 @@\n-safe\n+x\\x1b[31mred\\x1b[0m\ttab\\a\n",
		},
		{
			// Selectors are resolved before the base is opened.
			name: "unknown node before reading the base", fixture: "abandoned-branch", from: "2", to: "99",
			base: "missing", status: exitFailure, diagnostic: "unknown node 99",
		},
		{
			name: "mismatched base", fixture: "abandoned-branch", from: "2", to: "3",
			base: "../linear/base.bin", status: exitFailure, diagnostic: "base mismatch",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			dir := t.TempDir()
			undoPath, undo := copyFixture(t, dir, tt.fixture, "history.undo")
			basePath, base := copyFixture(t, dir, tt.fixture, "base.bin")
			if tt.base != "" {
				basePath = filepath.Join("../../testdata/undo", tt.fixture, tt.base)
			}

			var out, diagnostic bytes.Buffer
			args := []string{"diff", "--undo", undoPath, "--base", basePath, "--from", tt.from, "--to", tt.to}
			if status := run(t.Context(), args, &out, &diagnostic); status != tt.status {
				t.Fatalf("status = %d, want %d; stderr = %q", status, tt.status, diagnostic.String())
			}

			if out.String() != tt.want {
				t.Fatalf("stdout =\n%s\nwant\n%s", out.String(), tt.want)
			}
			assertOutput(t, "stderr", diagnostic.String(), tt.diagnostic)
			assertUnchanged(t, undoPath, undo)
			assertUnchanged(t, filepath.Join(dir, "base.bin"), base)
		})
	}
}

func TestDiffOutputBudget(t *testing.T) {
	t.Parallel()

	dir := "../../testdata/undo/abandoned-branch"
	lim := limits.Default()
	states, err := recoverStates(t.Context(), filepath.Join(dir, "history.undo"), filepath.Join(dir, "base.bin"), []history.NodeID{2, 3}, lim)
	if err != nil {
		t.Fatal(err)
	}
	result, err := diff.Compare(t.Context(), states[0], states[1], lim)
	if err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name    string
		budget  int
		cancel  bool
		wantErr bool
	}{
		{name: "exact output budget", budget: len(experimentAgainstChoice)},
		{name: "one byte short", budget: len(experimentAgainstChoice) - 1, wantErr: true},
		{name: "cancelled rendering", budget: len(experimentAgainstChoice), cancel: true, wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			if tt.cancel {
				cancel()
			}

			text, err := diffText(ctx, result, 2, 3, tt.budget)
			if tt.wantErr {
				if err == nil || text != "" {
					t.Fatalf("incomplete diff exposed as success: %q, %v", text, err)
				}
				return
			}
			if err != nil || text != experimentAgainstChoice {
				t.Fatalf("text = %q, error = %v", text, err)
			}
		})
	}
}

func TestHunkRange(t *testing.T) {
	t.Parallel()

	// GNU unified headers: one-based starts, ",1" omitted, and an empty range
	// named by the line before it.
	tests := []struct {
		name         string
		start, count int
		want         string
	}{
		{name: "empty at start", start: 0, count: 0, want: "0,0"},
		{name: "empty after line four", start: 4, count: 0, want: "4,0"},
		{name: "single line", start: 0, count: 1, want: "1"},
		{name: "several lines", start: 2, count: 3, want: "3,3"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := hunkRange(tt.start, tt.count); got != tt.want {
				t.Fatalf("range = %q, want %q", got, tt.want)
			}
		})
	}
}
