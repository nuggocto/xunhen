package main

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/nuggocto/xunhen/internal/limits"
)

func TestShowArguments(t *testing.T) {
	t.Parallel()

	// Every invalid case fails before a file is opened, so the paths are fake.
	args := func(extra ...string) []string {
		return append([]string{"show", "--undo", "file", "--base", "base"}, extra...)
	}

	tests := []struct {
		name, diagnostic string
		args             []string
		status           int
	}{
		{name: "help", args: []string{"show", "--help"}},
		{name: "missing base", args: []string{"show", "--undo", "file", "--node", "0"}, status: 2},
		{name: "missing node", args: args(), status: 2},
		{name: "negative node", args: args("--node", "-1"), status: 2},
		{name: "signed node", args: args("--node", "+1"), status: 2},
		{name: "node overflow", args: args("--node", "2147483648"), status: 2, diagnostic: "supported ID range"},
		{name: "duplicate node", args: args("--node", "0", "--node", "1"), status: 2, diagnostic: "only once"},
		{name: "raw without policy", args: args("--node", "0", "--raw"), status: 2},
		{name: "policy without raw", args: args("--node", "0", "--final-newline=omit"), status: 2},
		{name: "invalid policy", args: args("--node", "0", "--raw", "--final-newline=auto"), status: 2},
		{name: "empty policy without raw", args: args("--node", "0", "--final-newline="), status: 2},
		{name: "help among other flags", args: args("--help"), status: 2, diagnostic: "must be used alone"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var out, diagnostic bytes.Buffer
			status := run(t.Context(), tt.args, &out, &diagnostic)
			if status != tt.status {
				t.Fatalf("status = %d, want %d; stderr = %q", status, tt.status, diagnostic.String())
			}

			if tt.status == 0 {
				assertOutput(t, "help", out.String(), "Usage: xunhen show")
				return
			}
			if out.Len() != 0 || diagnostic.Len() == 0 || !strings.Contains(diagnostic.String(), tt.diagnostic) {
				t.Fatalf("invalid invocation: stdout %q, stderr %q", out.String(), diagnostic.String())
			}
		})
	}
}

func TestFixtureBaseLoading(t *testing.T) {
	t.Parallel()

	paths, err := filepath.Glob("../../testdata/undo/*/oracle.json")
	if err != nil || len(paths) == 0 {
		t.Fatalf("oracle inventory: %v", err)
	}

	unsupported := map[string]bool{"crlf": true, "embedded-nul": true, "invalid-utf8": true, "latin1": true}

	for _, path := range paths {
		name := filepath.Base(filepath.Dir(path))
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			var oracle struct {
				Loaded struct {
					Anchor struct {
						Seq      int
						LinesHex []string `json:"lines_hex"`
					}
				}
			}
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if err := json.Unmarshal(data, &oracle); err != nil {
				t.Fatal(err)
			}

			dir := filepath.Dir(path)
			args := []string{
				"show", "--undo", filepath.Join(dir, "history.undo"), "--base", filepath.Join(dir, "base.bin"),
				"--node", strconv.Itoa(oracle.Loaded.Anchor.Seq), "--raw", "--final-newline=include",
			}

			var out, diagnostic bytes.Buffer
			status := run(t.Context(), args, &out, &diagnostic)

			if unsupported[name] {
				if status != 1 || out.Len() != 0 || !strings.Contains(diagnostic.String(), "unsupported input") {
					t.Fatalf("status = %d, output = %q, error = %q", status, out.String(), diagnostic.String())
				}
				return
			}

			var want strings.Builder
			for _, encoded := range oracle.Loaded.Anchor.LinesHex {
				line, err := hex.DecodeString(encoded)
				if err != nil {
					t.Fatal(err)
				}
				want.Write(line)
				want.WriteByte('\n')
			}
			if status != 0 || out.String() != want.String() || diagnostic.Len() != 0 {
				t.Fatalf("status = %d, output = %q, error = %q; want %q", status, out.String(), diagnostic.String(), want.String())
			}
		})
	}
}

func TestShowRecovery(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	undoPath, undo := copyFixture(t, dir, "abandoned-branch", "history.undo")
	basePath, base := copyFixture(t, dir, "abandoned-branch", "base.bin")

	const experiment = "package sample\n\nfunc experiment() int { return 42 }"
	tests := []struct {
		name, node, policy, want, diagnostic string
		status                               int
	}{
		{name: "abandoned experiment", node: "2", policy: "include", want: experiment + "\n"},
		{name: "explicitly omit newline", node: "2", policy: "omit", want: experiment},
		{name: "retained root", node: "0", policy: "include", want: "package sample\n\n// seed\n"},
		{name: "display", node: "2", want: experiment + "\n"},
		{name: "unknown node", node: "99", status: 1, diagnostic: "unknown node"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			args := []string{"show", "--undo", undoPath, "--base", basePath, "--node", tt.node}
			if tt.policy != "" {
				args = append(args, "--raw", "--final-newline="+tt.policy)
			}

			var out, diagnostic bytes.Buffer
			if status := run(t.Context(), args, &out, &diagnostic); status != tt.status {
				t.Fatalf("status = %d, want %d; stderr = %q", status, tt.status, diagnostic.String())
			}

			assertOutput(t, "stderr", diagnostic.String(), tt.diagnostic)
			if out.String() != tt.want {
				t.Fatalf("stdout = %q, want %q", out.String(), tt.want)
			}
		})
	}

	assertUnchanged(t, undoPath, undo)
	assertUnchanged(t, basePath, base)
}

func TestShowDisplayEscaping(t *testing.T) {
	t.Parallel()

	dir := "../../testdata/undo/terminal-controls"
	args := []string{"show", "--undo", filepath.Join(dir, "history.undo"), "--base", filepath.Join(dir, "base.bin"), "--node", "1"}

	var out, diagnostic bytes.Buffer
	if status := run(t.Context(), args, &out, &diagnostic); status != 0 {
		t.Fatalf("status = %d; stderr = %q", status, diagnostic.String())
	}

	// The tab stays readable; ESC and BEL cannot reach the terminal.
	if want := "x\\x1b[31mred\\x1b[0m\ttab\\a\n"; out.String() != want {
		t.Fatalf("display = %q, want %q", out.String(), want)
	}
}

func TestBaseTextProfile(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name, text, errorText string
		want                  []string
		maxLines              int
		maxBytes              int64
	}{
		{name: "empty", text: "", want: []string{""}},
		{name: "final LF", text: "a\n", want: []string{"a"}},
		{name: "no final LF", text: "a", want: []string{"a"}},
		{name: "blank last line", text: "a\n\n", want: []string{"a", ""}},
		{name: "BOM", text: "\xef\xbb\xbfa\n", errorText: "UTF-8 BOM"},
		{name: "CRLF", text: "a\r\n", errorText: "CRLF"},
		{name: "lone CR", text: "a\rb", want: []string{"a\rb"}},
		{name: "NUL", text: "a\x00b", errorText: "embedded NUL"},
		{name: "invalid UTF-8", text: "a\xffb", errorText: "invalid UTF-8"},
		{name: "line budget", text: "a\nb\nc\n", errorText: "base lines", maxLines: 2},
		{name: "byte budget", text: "abcd", errorText: "base input bytes", maxBytes: 3},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			path := filepath.Join(t.TempDir(), "base")
			if err := os.WriteFile(path, []byte(tt.text), 0600); err != nil {
				t.Fatal(err)
			}

			lim := limits.Default()
			if tt.maxLines != 0 {
				lim.StateLines = tt.maxLines
			}
			if tt.maxBytes != 0 {
				lim.BaseBytes = tt.maxBytes
			}

			got, err := loadBase(t.Context(), path, lim)
			if tt.errorText != "" {
				if err == nil || got != nil || !strings.Contains(err.Error(), tt.errorText) {
					t.Fatalf("lines = %q, error = %v; want %q", got, err, tt.errorText)
				}
				return
			}
			if err != nil || !slices.Equal(got, tt.want) {
				t.Fatalf("lines = %q, error = %v; want %q", got, err, tt.want)
			}
		})
	}
}

func TestShowBaseAndSelectedTextPolicy(t *testing.T) {
	t.Parallel()

	// An empty base means the matching fixture base. A nonzero replacement
	// overwrites the first byte of the experiment's stored line; a wire LF
	// decodes to a buffer NUL.
	tests := []struct {
		name, base, diagnostic, want string
		missingBase                  bool
		replacement                  byte
		status                       int
	}{
		{name: "missing base", missingBase: true, status: 1, diagnostic: "open base file"},
		{name: "mismatched base", base: "different\n", status: 1, diagnostic: "base mismatch"},
		{name: "unsupported base", base: "bad\x00text", status: 1, diagnostic: "unsupported input"},
		{name: "NUL in selected state", replacement: '\n', status: 1, diagnostic: "raw export unsupported"},
		{name: "invalid UTF-8 in selected state", replacement: 0xff, status: 1, diagnostic: "raw export unsupported"},
		{name: "lone CR in selected state", replacement: '\r', want: "package sample\n\n\runc experiment() int { return 42 }\n"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			dir := t.TempDir()
			undoPath := filepath.Join(dir, "history.undo")
			basePath := filepath.Join(dir, "base.go")

			undo := undoFixture(t, "abandoned-branch")
			if tt.replacement != 0 {
				at := bytes.Index(undo, []byte("func experiment() int { return 42 }"))
				if at < 0 {
					t.Fatal("fixture entry missing")
				}
				undo[at] = tt.replacement
			}
			writeFile(t, undoPath, undo)

			var base []byte
			switch {
			case tt.missingBase:
			case tt.base != "":
				base = []byte(tt.base)
				writeFile(t, basePath, base)
			default:
				basePath, base = copyFixture(t, dir, "abandoned-branch", "base.bin")
			}

			var out, diagnostic bytes.Buffer
			args := []string{"show", "--undo", undoPath, "--base", basePath, "--node", "2", "--raw", "--final-newline=include"}
			status := run(t.Context(), args, &out, &diagnostic)

			if status != tt.status || out.String() != tt.want {
				t.Fatalf("status = %d, stdout = %q; want %d, %q", status, out.String(), tt.status, tt.want)
			}
			assertOutput(t, "stderr", diagnostic.String(), tt.diagnostic)

			assertUnchanged(t, undoPath, undo)
			if base != nil {
				assertUnchanged(t, basePath, base)
			}
		})
	}
}

func TestRawOutputDescriptor(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		open      func(*testing.T) *os.File
		wantError bool
	}{
		{name: "character device that is not a terminal", open: func(t *testing.T) *os.File {
			file, err := os.OpenFile(os.DevNull, os.O_WRONLY, 0)
			if err != nil {
				t.Fatal(err)
			}
			return file
		}},
		{name: "closed descriptor", wantError: true, open: func(t *testing.T) *os.File {
			file, err := os.CreateTemp(t.TempDir(), "output")
			if err != nil {
				t.Fatal(err)
			}
			if err := file.Close(); err != nil {
				t.Fatal(err)
			}
			return file
		}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			file := tt.open(t)
			t.Cleanup(func() { _ = file.Close() })

			terminal, err := outputIsTerminal(file)
			if terminal || (err != nil) != tt.wantError {
				t.Fatalf("terminal = %t, error = %v", terminal, err)
			}
		})
	}
}

// copyFixture writes a read-only copy of one fixture file into dir.
func copyFixture(t *testing.T, dir, fixture, file string) (string, []byte) {
	t.Helper()

	data, err := os.ReadFile(filepath.Join("../../testdata/undo", fixture, file))
	if err != nil {
		t.Fatal(err)
	}

	path := filepath.Join(dir, file)
	writeFile(t, path, data)
	return path, data
}

func writeFile(t *testing.T, path string, data []byte) {
	t.Helper()

	if err := os.WriteFile(path, data, 0444); err != nil {
		t.Fatal(err)
	}
}

func assertUnchanged(t *testing.T, path string, want []byte) {
	t.Helper()

	got, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(got, want) {
		t.Fatalf("%s changed: %v", path, err)
	}
}
