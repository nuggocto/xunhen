package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nuggocto/xunhen/internal/discover"
	"github.com/nuggocto/xunhen/internal/pty"
	"github.com/nuggocto/xunhen/internal/synth"
)

// secret stands for text a user deleted because it was private. Recovered
// text may show it when asked to, but no diagnostic may repeat it.
const secret = "XUNHEN-PRIVATE-7f3a91"

// integrityStates is a three-state history whose every state, like the
// mismatching base and the damaged copy, holds the secret.
var integrityStates = [][]string{
	{"package sample", "// " + secret + " at the root"},
	{"package sample", `token := "` + secret + `-abandoned"`},
	{"package sample", "// chosen " + secret},
}

// inputTree writes the inputs every integrity case reads: explicit copies,
// a mismatching base, a damaged history, and a source tree with its undo
// directory for discovery.
func inputTree(t *testing.T) string {
	t.Helper()

	dir := t.TempDir()
	f := synth.Chain(integrityStates)
	undo, err := f.Bytes()
	if err != nil {
		t.Fatal(err)
	}
	base := strings.Join(f.Reference, "\n") + "\n"

	physical, err := filepath.EvalSymlinks(dir)
	if err != nil {
		t.Fatal(err)
	}
	name := discover.Resolve(filepath.Join(physical, "src", "retry.go"), physical).UndoName()

	files := map[string][]byte{
		"explicit/history.undo": undo,
		"explicit/retry.go":     []byte(base),
		"explicit/other.go":     []byte("// " + secret + " in other text\n"),
		"explicit/damaged.undo": undo[:len(undo)-3],
		"src/retry.go":          []byte(base),
		"undo/" + name:          undo,
	}
	for path, data := range files {
		full := filepath.Join(dir, path)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, data, 0o644); err != nil {
			t.Fatal(err)
		}
	}

	return dir
}

// fingerprint records every entry under dir: its path, type, permissions,
// size, modification time, and for files the SHA-256 of the contents. Any
// write, creation, deletion, or permission change shows up as a difference.
func fingerprint(t *testing.T, dir string) map[string]string {
	t.Helper()

	out := map[string]string{}
	err := filepath.WalkDir(dir, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		record := fmt.Sprintf("%s %d %d", info.Mode(), info.Size(), info.ModTime().UnixNano())
		if info.Mode().IsRegular() {
			data, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			record += fmt.Sprintf(" %x", sha256.Sum256(data))
		}
		out[strings.TrimPrefix(path, dir)] = record
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	return out
}

func sameTree(t *testing.T, before, after map[string]string) {
	t.Helper()

	for path, record := range before {
		if after[path] != record {
			t.Errorf("%s changed:\nbefore %s\nafter  %s", path, record, after[path])
		}
	}
	for path := range after {
		if _, ok := before[path]; !ok {
			t.Errorf("%s was created", path)
		}
	}
}

// TestCommandsLeaveInputsUnchanged runs every command family in both input
// forms, successful and failing, against one input tree per case. Each run
// must leave every input byte, permission, and timestamp as it was, and
// only a command asked to show recovered text may print it.
func TestCommandsLeaveInputsUnchanged(t *testing.T) {
	t.Parallel()

	binary := buildExecutable(t, t.TempDir())
	explicit := []string{"--undo", "explicit/history.undo", "--base", "explicit/retry.go"}
	source := []string{"--source", "src/retry.go", "--undo-dir", "undo"}

	tests := []struct {
		name   string
		args   []string
		status int
		// shows is set when stdout carries recovered text by request.
		shows bool
		// raw sends stdout to a file outside the input tree.
		raw bool
	}{
		{name: "inspect explicit", args: []string{"inspect", "--undo", "explicit/history.undo"}},
		{name: "inspect by source", args: append([]string{"inspect"}, source...)},
		{name: "show explicit", args: append(append([]string{"show"}, explicit...), "--node", "1"), shows: true},
		{name: "show raw explicit", args: append(append([]string{"show"}, explicit...), "--node", "1", "--raw", "--final-newline=include"), shows: true, raw: true},
		{name: "show by source", args: append(append([]string{"show"}, source...), "--node", "0"), shows: true},
		{name: "show raw by source", args: append(append([]string{"show"}, source...), "--node", "0", "--raw", "--final-newline=omit"), shows: true, raw: true},
		{name: "diff explicit", args: append(append([]string{"diff"}, explicit...), "--from", "0", "--to", "2"), shows: true},
		{name: "diff by source", args: append(append([]string{"diff"}, source...), "--from", "1", "--to", "2"), shows: true},
		{name: "show with a mismatching base", args: []string{"show", "--undo", "explicit/history.undo", "--base", "explicit/other.go", "--node", "1"}, status: 1},
		{name: "inspect a damaged history", args: []string{"inspect", "--undo", "explicit/damaged.undo"}, status: 1},
		{name: "show a damaged history", args: []string{"show", "--undo", "explicit/damaged.undo", "--base", "explicit/retry.go", "--node", "1"}, status: 1},
		{name: "show by source with other text", args: []string{"show", "--source", "explicit/other.go", "--undo-dir", "undo", "--node", "1"}, status: 1},
		{name: "show an unknown node", args: append(append([]string{"show"}, explicit...), "--node", "99"), status: 1},
		{name: "diff with a mismatching base", args: []string{"diff", "--undo", "explicit/history.undo", "--base", "explicit/other.go", "--from", "0", "--to", "1"}, status: 1},
		{name: "browse without a terminal", args: append([]string{"browse"}, explicit...), status: 1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			dir := inputTree(t)
			before := fingerprint(t, dir)

			ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
			defer cancel()
			command := exec.CommandContext(ctx, binary, tt.args...)
			command.Dir = dir
			command.Env = []string{"PATH=", "HOME=" + dir, "LC_ALL=C"}
			var stdout, stderr bytes.Buffer
			command.Stdout, command.Stderr = &stdout, &stderr

			var rawPath string
			if tt.raw {
				rawPath = filepath.Join(t.TempDir(), "out")
				file, err := os.Create(rawPath)
				if err != nil {
					t.Fatal(err)
				}
				defer file.Close()
				command.Stdout = file
			}

			err := command.Run()
			var exit *exec.ExitError
			if err != nil && !errors.As(err, &exit) {
				t.Fatal(err)
			}
			out := stdout.String()
			if tt.raw {
				data, err := os.ReadFile(rawPath)
				if err != nil {
					t.Fatal(err)
				}
				out = string(data)
			}
			// Disclosure and input changes matter most, so they are checked
			// before the exit status, which a regression would also change.
			if strings.Contains(stderr.String(), secret) {
				t.Errorf("a diagnostic repeats recovered text: %q", stderr.String())
			}
			sameTree(t, before, fingerprint(t, dir))
			if status := command.ProcessState.ExitCode(); status != tt.status {
				t.Fatalf("exit status %d, want %d; stderr %q", status, tt.status, stderr.String())
			}
			if shown := strings.Contains(out, secret); shown != tt.shows {
				t.Fatalf("recovered text on stdout = %t, want %t: %q", shown, tt.shows, out)
			}
		})
	}
}

// TestBrowserLeavesInputsUnchanged drives the browser on a terminal through
// previews, comparisons, and reloads, one of which fails because the test
// itself rewrites the base. The only change to the inputs afterwards must be
// that rewrite.
func TestBrowserLeavesInputsUnchanged(t *testing.T) {
	t.Parallel()

	binary := buildExecutable(t, t.TempDir())
	explicit := []string{"--undo", "explicit/history.undo", "--base", "explicit/retry.go"}
	source := []string{"--source", "src/retry.go", "--undo-dir", "undo"}

	tests := []struct {
		name   string
		args   []string
		status int
		// rewrite is the file the test replaces during the run to make a
		// reload fail, or empty.
		rewrite string
		keys    func(t *testing.T, term *terminal, dir, rewrite string)
	}{
		{name: "explicit inputs", args: explicit, keys: previewCompareReload},
		{name: "found by source", args: source, keys: previewCompareReload},
		{name: "failed reload, explicit", args: explicit, rewrite: "explicit/retry.go", keys: failedReload},
		{name: "failed reload, by source", args: source, rewrite: "src/retry.go", keys: failedReload},
		{name: "mismatching base", args: []string{"--undo", "explicit/history.undo", "--base", "explicit/other.go"}, status: 1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			dir := inputTree(t)
			before := fingerprint(t, dir)

			term := openTerminal(t)
			ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
			defer cancel()
			command := exec.CommandContext(ctx, binary, append([]string{"browse"}, tt.args...)...)
			command.Dir = dir
			command.Env = []string{"PATH=", "HOME=" + dir, "TERM=xterm-256color", "LC_ALL=C"}
			command.Stdin, command.Stdout, command.Stderr = term.slave, term.slave, term.slave
			command.SysProcAttr = pty.Attach()
			if err := command.Start(); err != nil {
				t.Fatal(err)
			}

			if tt.keys != nil {
				term.waitText(t, 0, "chosen")
				tt.keys(t, term, dir, tt.rewrite)
			}

			err := command.Wait()
			var exit *exec.ExitError
			if err != nil && !errors.As(err, &exit) {
				t.Fatal(err)
			}
			if status := command.ProcessState.ExitCode(); status != tt.status {
				t.Fatalf("exit status %d, want %d; output %q", status, tt.status, term.text(0))
			}
			output := term.drain()
			if tt.status != 0 && strings.Contains(output, secret) {
				t.Fatalf("a failed start showed recovered text: %q", output)
			}

			after := fingerprint(t, dir)
			if tt.rewrite != "" {
				before[string(filepath.Separator)+tt.rewrite] = after[string(filepath.Separator)+tt.rewrite]
			}
			sameTree(t, before, after)
		})
	}
}

// previewCompareReload previews the abandoned state, pins it, compares it
// with the reference, and reloads. A reload returns the pin to the
// reference, so the comparison becomes one of identical states: a change
// on screen that proves the reload finished. A reload that left the screen
// as it was would draw nothing to wait for.
func previewCompareReload(t *testing.T, term *terminal, _, _ string) {
	mark := term.mark()
	term.write(t, "k")
	term.waitText(t, mark, "abandoned")
	mark = term.mark()
	term.write(t, " j")
	term.write(t, "d")
	term.waitText(t, mark, "hunk")
	mark = term.mark()
	term.write(t, "r")
	term.waitText(t, mark, "identical")
	term.write(t, "q")
}

// failedReload rewrites the base between loads, so the reload's
// verification fails and the browser must keep the earlier load.
func failedReload(t *testing.T, term *terminal, dir, rewrite string) {
	if err := os.WriteFile(filepath.Join(dir, rewrite), []byte("package changed\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	mark := term.mark()
	term.write(t, "r")
	term.waitText(t, mark, "Reload failed")
	mark = term.mark()
	term.write(t, "k")
	term.waitText(t, mark, "abandoned")
	term.write(t, "q")
}
