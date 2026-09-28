package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/nuggocto/xunhen/tools/internal/layout"
	"github.com/nuggocto/xunhen/tools/internal/tarball"
)

const (
	testVersion = "v1.2.3-test"
	testCommit  = "0123456789abcdef0123456789abcdef01234567"
)

// buildXunhen builds the command the way a release does. The verifier
// itself never compiles; only its tests make an executable to check.
func buildXunhen(t *testing.T) string {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Minute)
	defer cancel()

	binary := filepath.Join(t.TempDir(), "xunhen")
	cmd := exec.CommandContext(ctx, "go", "build", "-trimpath", "-buildvcs=false",
		"-ldflags=-X main.version="+testVersion+" -X main.commit="+testCommit, "-o", binary, "../../cmd/xunhen")
	cmd.Env = append(os.Environ(), "CGO_ENABLED=0", "GOOS=linux", "GOARCH=amd64", "GOAMD64=v1", "GOTOOLCHAIN=local", "GOWORK=off")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build: %v\n%s", err, out)
	}
	return binary
}

func testConfig(binary string) config {
	return config{
		binary: binary, corpus: "../../testdata/undo", gosum: "../../go.sum",
		version: testVersion, commit: testCommit, goVersion: runtime.Version(), buildmode: "exe",
	}
}

// failures returns the names of the checks that failed.
func failures(results []result) []string {
	var names []string
	for _, r := range results {
		if r.err != nil {
			names = append(names, r.name)
		}
	}
	return names
}

// The verifier passes a correct executable and fails each check that a
// wrong one breaks. The wrappers stand in for a tampered or mis-built
// executable: they run the real one, and also write to HOME or start an
// editor, which only the verifier's own watch can see.
func TestVerifierJudgesExecutables(t *testing.T) {
	t.Parallel()

	binary := buildXunhen(t)
	wrapper := func(t *testing.T, body string) string {
		path := filepath.Join(t.TempDir(), "xunhen")
		script := fmt.Sprintf("#!/bin/sh\n%s\nexec '%s' \"$@\"\n", body, binary)
		if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
			t.Fatal(err)
		}
		return path
	}

	tests := []struct {
		name   string
		config func(t *testing.T) config
		want   []string // the checks that must fail, in order
	}{
		{name: "the built executable", config: func(t *testing.T) config { return testConfig(binary) }},
		{name: "another expected version", config: func(t *testing.T) config {
			c := testConfig(binary)
			c.version = "v1.2.4"
			return c
		}, want: []string{"version output"}},
		{name: "another expected toolchain", config: func(t *testing.T) config {
			c := testConfig(binary)
			c.goVersion = "go1.0"
			return c
		}, want: []string{"executable metadata", "version output"}},
		{name: "a module missing from go.sum", config: func(t *testing.T) config {
			c := testConfig(binary)
			c.gosum = filepath.Join(t.TempDir(), "go.sum")
			if err := os.WriteFile(c.gosum, nil, 0o644); err != nil {
				t.Fatal(err)
			}
			return c
		}, want: []string{"executable metadata"}},
		{name: "a wrapper that writes to HOME", config: func(t *testing.T) config {
			return testConfig(wrapper(t, `echo x >> "$HOME/.xunhen-history"`))
		}, want: []string{"executable metadata", "static linkage", "inputs and HOME unchanged"}},
		{name: "a wrapper that starts an editor", config: func(t *testing.T) config {
			return testConfig(wrapper(t, `nvim --version >/dev/null 2>&1`))
		}, want: []string{"executable metadata", "static linkage", "no editor, git, Go, or shell started"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			results := verify(t.Context(), tt.config(t))
			got := failures(results)
			if strings.Join(got, "; ") != strings.Join(tt.want, "; ") {
				for _, r := range results {
					t.Logf("%s: %v", r.name, r.err)
				}
				t.Fatalf("failed checks %q, want %q", got, tt.want)
			}
		})
	}
}

// A release archive must hold exactly the documented files with the
// documented modes, and match its published checksum. Each case damages a
// correct archive in one way.
func TestVerifierJudgesArchives(t *testing.T) {
	t.Parallel()

	exe, err := os.ReadFile(buildXunhen(t))
	if err != nil {
		t.Fatal(err)
	}
	files := func() []tarball.File {
		out := []tarball.File{{Name: layout.Executable, Executable: true, Data: exe}}
		for _, name := range layout.BinaryDocs {
			out = append(out, tarball.File{Name: name, Data: []byte(name + "\n")})
		}
		return out
	}
	prefix := layout.BinaryArchive(testVersion)

	tests := []struct {
		name   string
		files  func() []tarball.File
		damage func(data []byte) []byte
		sums   func(name, sum string) string
		want   string
	}{
		{name: "a correct archive", files: files},
		{name: "a changed byte", files: files, damage: func(data []byte) []byte {
			data[len(data)/2] ^= 1
			return data
		}, want: "lists"},
		{name: "not listed in the checksums", files: files, sums: func(string, string) string { return "" }, want: "does not list"},
		{name: "an executable document", files: func() []tarball.File {
			f := files()
			f[1].Executable = true
			return f
		}, want: "is executable"},
		{name: "a non-executable program", files: func() []tarball.File {
			f := files()
			f[0].Executable = false
			return f
		}, want: "is not executable"},
		{name: "an extra file", files: func() []tarball.File {
			return append(files(), tarball.File{Name: "testdata/undo/linear/history.undo", Data: []byte("x")})
		}, want: "want exactly"},
		{name: "a missing document", files: func() []tarball.File { return files()[:len(files())-1] }, want: "want exactly"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var data bytes.Buffer
			if err := tarball.Write(&data, prefix, tt.files(), time.Unix(1_790_000_000, 0)); err != nil {
				t.Fatal(err)
			}
			sum := fmt.Sprintf("%x", sha256.Sum256(data.Bytes()))
			archive := data.Bytes()
			if tt.damage != nil {
				archive = tt.damage(bytes.Clone(archive))
			}

			dir := t.TempDir()
			c := testConfig("")
			c.binary, c.archive, c.sums = "", filepath.Join(dir, prefix+".tar.gz"), filepath.Join(dir, "SHA256SUMS.txt")
			sums := sum + "  " + prefix + ".tar.gz\n"
			if tt.sums != nil {
				sums = tt.sums(prefix+".tar.gz", sum)
			}
			if err := os.WriteFile(c.archive, archive, 0o644); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(c.sums, []byte(sums), 0o644); err != nil {
				t.Fatal(err)
			}

			exePath, _, err := checkArchive(c)
			switch {
			case tt.want == "" && err != nil:
				t.Fatal(err)
			case tt.want != "" && (err == nil || !strings.Contains(err.Error(), tt.want)):
				t.Fatalf("checkArchive returned %v, want an error about %q", err, tt.want)
			case err == nil:
				defer func() { _ = os.RemoveAll(filepath.Dir(exePath)) }()
				extracted, err := os.ReadFile(exePath)
				if err != nil || !bytes.Equal(extracted, exe) {
					t.Fatalf("the extracted executable differs from the archived one: %v", err)
				}
			}
		})
	}
}

// The verifier refuses an archive larger than its limit before reading it.
// A sparse file makes the size without the memory or disk it describes.
func TestVerifierRefusesOversizedArchives(t *testing.T) {
	t.Parallel()

	c := testConfig("")
	c.binary, c.archive = "", filepath.Join(t.TempDir(), layout.BinaryArchive(testVersion)+".tar.gz")
	f, err := os.Create(c.archive)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.Truncate(maxArchiveBytes + 1); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}

	if _, _, err := checkArchive(c); err == nil || !strings.Contains(err.Error(), "larger than") {
		t.Fatalf("checkArchive returned %v, want a refusal of the size", err)
	}
}
