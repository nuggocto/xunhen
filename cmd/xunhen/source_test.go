package main

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/nuggocto/xunhen/internal/discover"
	"github.com/nuggocto/xunhen/internal/history"
	"github.com/nuggocto/xunhen/internal/input"
	"github.com/nuggocto/xunhen/internal/limits"
)

func TestInputModeRules(t *testing.T) {
	t.Parallel()

	many := func(n int) []string {
		var args []string
		for range n {
			args = append(args, "--undo-dir", "d")
		}
		return args
	}

	tests := []struct {
		name       string
		args       []string
		status     int
		diagnostic string
	}{
		{name: "source with undo", args: []string{"inspect", "--source", "s", "--undo-dir", "d", "--undo", "u"}, status: exitUsage, diagnostic: "cannot be combined"},
		{name: "source with base", args: []string{"show", "--source", "s", "--undo-dir", "d", "--base", "b", "--node", "1"}, status: exitUsage, diagnostic: "cannot be combined"},
		{name: "source without a directory", args: []string{"diff", "--source", "s", "--from", "1", "--to", "2"}, status: exitUsage, diagnostic: "at least one --undo-dir"},
		{name: "directory without a source", args: []string{"show", "--undo", "u", "--base", "b", "--undo-dir", "d", "--node", "1"}, status: exitUsage, diagnostic: "requires --source"},
		{name: "repeated source", args: []string{"inspect", "--source", "s", "--source", "t", "--undo-dir", "d"}, status: exitUsage, diagnostic: "only once"},
		{name: "empty source", args: []string{"inspect", "--source=", "--undo-dir", "d"}, status: exitUsage, diagnostic: "requires a path"},
		{name: "empty directory", args: []string{"inspect", "--source", "s", "--undo-dir="}, status: exitUsage, diagnostic: "requires a directory path"},
		{name: "inspect has no base", args: []string{"inspect", "--undo", "u", "--base", "b"}, status: exitUsage, diagnostic: "not defined"},
		{name: "one directory over the limit", args: slices.Concat([]string{"inspect", "--source", "s"}, many(33)), status: exitUsage, diagnostic: "at most 32"},
		// The arguments are valid, so the missing directories are a search
		// failure rather than a usage error.
		{name: "directories at the limit", args: slices.Concat([]string{"inspect", "--source", "s"}, many(32)), status: exitFailure, diagnostic: "could not search"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var stdout, stderr bytes.Buffer
			if status := run(t.Context(), tt.args, &stdout, &stderr); status != tt.status {
				t.Fatalf("status = %d, want %d; stderr = %q", status, tt.status, stderr.String())
			}
			if stdout.Len() != 0 || !strings.Contains(stderr.String(), tt.diagnostic) {
				t.Fatalf("stdout = %q, stderr = %q; want only a diagnostic containing %q", stdout.String(), stderr.String(), tt.diagnostic)
			}
		})
	}
}

// sourceWorld is a source file holding the abandoned-branch base text and two
// undo directories, all under one physical temporary root.
type sourceWorld struct {
	t      *testing.T
	root   string
	source string
}

func newSourceWorld(t *testing.T, dirs ...string) *sourceWorld {
	t.Helper()

	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	for _, dir := range append([]string{"src", "u1", "u2"}, dirs...) {
		if err := os.Mkdir(filepath.Join(root, dir), 0700); err != nil {
			t.Fatal(err)
		}
	}

	w := &sourceWorld{t: t, root: root, source: filepath.Join(root, "src", "retry.go")}
	w.write(w.source, readFixtureFile(t, "abandoned-branch", "base.bin"))
	return w
}

func readFixtureFile(t *testing.T, fixture, file string) []byte {
	t.Helper()

	data, err := os.ReadFile(filepath.Join("../../testdata/undo", fixture, file))
	if err != nil {
		t.Fatal(err)
	}

	return data
}

func (w *sourceWorld) write(path string, data []byte) {
	w.t.Helper()

	if err := os.WriteFile(path, data, 0600); err != nil {
		w.t.Fatal(err)
	}
}

// history places a fixture history under the source's undo name in dir.
func (w *sourceWorld) history(dir, fixture string) string {
	w.t.Helper()

	path := filepath.Join(w.root, dir, discover.Resolve(w.source, w.root).UndoName())
	w.write(path, readFixtureFile(w.t, fixture, "history.undo"))
	return path
}

func (w *sourceWorld) dir(name string) string { return filepath.Join(w.root, name) }

func TestSourceCommands(t *testing.T) {
	t.Parallel()

	const experiment = "package sample\n\nfunc experiment() int { return 42 }\n"

	tests := []struct {
		name       string
		setup      func(w *sourceWorld) []string // returns the command arguments
		status     int
		stdout     string // exact stdout, or a substring when contains is set
		contains   bool
		diagnostic []string
	}{
		{
			name: "recover the abandoned experiment", stdout: experiment,
			setup: func(w *sourceWorld) []string {
				w.history("u2", "abandoned-branch")
				return []string{"show", "--source", w.source, "--undo-dir", w.dir("u1"), "--undo-dir", w.dir("u2"), "--node", "2", "--raw", "--final-newline=include"}
			},
		},
		{
			name: "compare branches", stdout: experimentAgainstChoice,
			setup: func(w *sourceWorld) []string {
				w.history("u1", "abandoned-branch")
				return []string{"diff", "--source", w.source, "--undo-dir", w.dir("u1"), "--from", "2", "--to", "3"}
			},
		},
		{
			name: "inspect a verified history", contains: true, stdout: "(verified: its text matches the reference)",
			setup: func(w *sourceWorld) []string {
				w.history("u1", "abandoned-branch")
				return []string{"inspect", "--source", w.source, "--undo-dir", w.dir("u1")}
			},
		},
		{
			name: "inspect a history whose text differs", contains: true, stdout: "(unverified: ",
			setup: func(w *sourceWorld) []string {
				w.history("u1", "linear")
				return []string{"inspect", "--source", w.source, "--undo-dir", w.dir("u1")}
			},
		},
		{
			name: "inspect after the source was deleted", contains: true, stdout: "(unverified: ",
			setup: func(w *sourceWorld) []string {
				w.history("u1", "abandoned-branch")
				if err := os.Remove(w.source); err != nil {
					w.t.Fatal(err)
				}
				return []string{"inspect", "--source", w.source, "--undo-dir", w.dir("u1")}
			},
		},
		{
			name: "recover when the text differs", status: exitFailure,
			diagnostic: []string{"reference text differs", "not verified", "xunhen show --undo PATH --base COPY"},
			setup: func(w *sourceWorld) []string {
				w.history("u1", "linear")
				return []string{"show", "--source", w.source, "--undo-dir", w.dir("u1"), "--node", "1"}
			},
		},
		{
			name: "recover after the source was deleted", status: exitFailure,
			diagnostic: []string{"cannot be used as the base", "no such file", "--undo PATH --base COPY"},
			setup: func(w *sourceWorld) []string {
				w.history("u1", "abandoned-branch")
				if err := os.Remove(w.source); err != nil {
					w.t.Fatal(err)
				}
				return []string{"show", "--source", w.source, "--undo-dir", w.dir("u1"), "--node", "2"}
			},
		},
		{
			name: "no history", status: exitFailure,
			diagnostic: []string{"no undo history", "searched ", "moved or renamed", "xunhen inspect --undo PATH"},
			setup: func(w *sourceWorld) []string {
				return []string{"diff", "--source", w.source, "--undo-dir", w.dir("u1"), "--from", "2", "--to", "3"}
			},
		},
		{
			name: "copies in two directories", status: exitFailure,
			diagnostic: []string{"more than one", "verified"},
			setup: func(w *sourceWorld) []string {
				w.history("u1", "abandoned-branch")
				w.history("u2", "abandoned-branch")
				return []string{"show", "--source", w.source, "--undo-dir", w.dir("u1"), "--undo-dir", w.dir("u2"), "--node", "2"}
			},
		},
		{
			name: "unsupported source text", status: exitFailure,
			diagnostic: []string{"cannot be used as the base", "CRLF"},
			setup: func(w *sourceWorld) []string {
				w.history("u1", "abandoned-branch")
				w.write(w.source, []byte("a\r\nb\r\n"))
				return []string{"show", "--source", w.source, "--undo-dir", w.dir("u1"), "--node", "2"}
			},
		},
		{
			name: "unknown node after a verified search", status: exitFailure,
			diagnostic: []string{"unknown node 99"},
			setup: func(w *sourceWorld) []string {
				w.history("u1", "abandoned-branch")
				return []string{"show", "--source", w.source, "--undo-dir", w.dir("u1"), "--node", "99"}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			w := newSourceWorld(t)
			args := tt.setup(w)

			var stdout, stderr bytes.Buffer
			if status := run(t.Context(), args, &stdout, &stderr); status != tt.status {
				t.Fatalf("status = %d, want %d; stderr = %q", status, tt.status, stderr.String())
			}

			switch {
			case tt.status != exitSuccess && stdout.Len() != 0:
				t.Fatalf("failed search wrote stdout %q", stdout.String())
			case tt.status == exitSuccess && stderr.Len() != 0:
				t.Fatalf("stderr = %q, want empty", stderr.String())
			case tt.contains && !strings.Contains(stdout.String(), tt.stdout):
				t.Fatalf("stdout = %q, want it to contain %q", stdout.String(), tt.stdout)
			case !tt.contains && tt.status == exitSuccess && stdout.String() != tt.stdout:
				t.Fatalf("stdout = %q, want %q", stdout.String(), tt.stdout)
			}
			for _, want := range tt.diagnostic {
				if !strings.Contains(stderr.String(), want) {
					t.Fatalf("stderr = %q, want it to contain %q", stderr.String(), want)
				}
			}
		})
	}
}

// Directory and candidate names come from the user and the filesystem, so
// the diagnostics that list them must not pass terminal controls through.
func TestSearchDiagnosticsAreEscaped(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		dir  string
	}{
		{name: "escape sequence in a directory name", dir: "u\x1b[2J"},
		{name: "line break in a directory name", dir: "u\nforged"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			w := newSourceWorld(t, tt.dir)
			w.write(filepath.Join(w.dir(tt.dir), discover.Resolve(w.source, w.root).UndoName()), []byte("junk"))

			var stdout, stderr bytes.Buffer
			args := []string{"show", "--source", w.source, "--undo-dir", w.dir(tt.dir), "--node", "1"}
			if status := run(t.Context(), args, &stdout, &stderr); status != exitFailure {
				t.Fatalf("status = %d; stderr = %q", status, stderr.String())
			}
			for _, line := range strings.Split(strings.TrimSuffix(stderr.String(), "\n"), "\n") {
				if !strings.HasPrefix(line, "xunhen: ") {
					t.Fatalf("diagnostic line %q lacks its prefix; stderr = %q", line, stderr.String())
				}
				for _, char := range line {
					if char < 0x20 || char > 0x7e {
						t.Fatalf("diagnostic contains unsafe character %U: %q", char, line)
					}
				}
			}
		})
	}
}

func sourceInputs(w *sourceWorld, dirs ...string) *inputs {
	in := &inputs{source: w.source}
	for _, dir := range dirs {
		in.undoDirs = append(in.undoDirs, w.dir(dir))
	}

	return in
}

// A reload is a second call to load. It must build new objects that share
// nothing with the first: old references fail, old snapshots keep their text,
// and a failed reload leaves the earlier load usable.
func TestReload(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		between     func(w *sourceWorld) // changes the files before the reload
		reloadFails bool
	}{
		{name: "unchanged files"},
		{name: "history rewritten for new text", between: func(w *sourceWorld) {
			w.write(w.source, readFixtureFile(w.t, "linear", "base.bin"))
			w.history("u1", "linear")
		}},
		{name: "history corrupted", reloadFails: true, between: func(w *sourceWorld) {
			w.write(filepath.Join(w.dir("u1"), discover.Resolve(w.source, w.root).UndoName()), []byte("junk"))
		}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			w := newSourceWorld(t)
			w.history("u1", "abandoned-branch")
			in := sourceInputs(w, "u1")
			lim := limits.Default()

			first, err := load(t.Context(), in, needs{base: true}, lim, discover.Search)
			if err != nil {
				t.Fatal(err)
			}
			old := reconstruct(t, first, 2)
			oldLines := old.Lines()
			oldRef, err := first.history.Lookup(2)
			if err != nil {
				t.Fatal(err)
			}

			if tt.between != nil {
				tt.between(w)
			}

			second, err := load(t.Context(), in, needs{base: true}, lim, discover.Search)
			if tt.reloadFails {
				if err == nil || second != nil {
					t.Fatalf("reload = %v, %v; want a failure", second, err)
				}
			} else {
				if err != nil {
					t.Fatal(err)
				}
				if second.history == first.history || second.base == first.base {
					t.Fatal("reload reused the earlier history or base")
				}
				if _, err := second.history.Info(oldRef); err == nil {
					t.Fatal("a reference from the earlier load was accepted by the reload")
				}
			}

			// The earlier load and its snapshot are untouched either way.
			if !slices.Equal(old.Lines(), oldLines) || !slices.Equal(reconstruct(t, first, 2).Lines(), oldLines) {
				t.Fatal("the earlier load changed after a reload")
			}
		})
	}
}

func reconstruct(t *testing.T, l *loaded, id history.NodeID) *history.Snapshot {
	t.Helper()

	r, err := history.Bind(l.history, l.base, limits.Default())
	if err != nil {
		t.Fatal(err)
	}
	ref, err := l.history.Lookup(id)
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := r.Reconstruct(t.Context(), ref)
	if err != nil {
		t.Fatal(err)
	}

	return snapshot
}

// An input changed after it was read, but before the load completed, must
// fail the load instead of combining two versions. Each change runs at a
// fixed step of the load, so no timing is involved.
func TestLoadRejectsChangedInputs(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		setup func(w *sourceWorld) (*inputs, needs, searcher)
	}{
		{
			name: "source rewritten during the search",
			setup: func(w *sourceWorld) (*inputs, needs, searcher) {
				w.history("u1", "abandoned-branch")
				search := func(ctx context.Context, req discover.Request, lim limits.Limits) (*discover.Result, error) {
					w.write(w.source, []byte("package sample\n"))
					return discover.Search(ctx, req, lim)
				}
				return sourceInputs(w, "u1"), needs{base: true}, search
			},
		},
		{
			name: "source replaced during the search",
			setup: func(w *sourceWorld) (*inputs, needs, searcher) {
				w.history("u1", "abandoned-branch")
				search := func(ctx context.Context, req discover.Request, lim limits.Limits) (*discover.Result, error) {
					replacement := filepath.Join(w.root, "src", "replacement")
					w.write(replacement, readFixtureFile(w.t, "abandoned-branch", "base.bin"))
					if err := os.Rename(replacement, w.source); err != nil {
						w.t.Fatal(err)
					}
					return discover.Search(ctx, req, lim)
				}
				return sourceInputs(w, "u1"), needs{base: true}, search
			},
		},
		{
			name: "explicit undo file rewritten before the base is read",
			setup: func(w *sourceWorld) (*inputs, needs, searcher) {
				undo := w.history("u1", "abandoned-branch")
				rewrite := func(*history.History) error {
					w.write(undo, readFixtureFile(w.t, "linear", "history.undo"))
					return nil
				}
				return &inputs{undo: undo, base: w.source}, needs{base: true, check: rewrite}, discover.Search
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			w := newSourceWorld(t)
			in, need, search := tt.setup(w)

			l, err := load(t.Context(), in, need, limits.Default(), search)
			var changed *input.ChangedError
			if l != nil || !errors.As(err, &changed) {
				t.Fatalf("load = %v, %v; want a changed-input error", l, err)
			}
		})
	}
}
