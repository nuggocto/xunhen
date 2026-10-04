package main

import (
	"context"
	"io"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nuggocto/xunhen/tools/internal/layout"
)

// repo is a throwaway git repository. Commands pass their identity, disable
// signing and hooks on the command line, and drop the variables that would
// point git at another repository; the repository starts from no template.
// So neither the user's git setup nor a git hook running the tests can
// change what they see or touch.
type repo struct {
	t   *testing.T
	dir string
}

func newRepo(t *testing.T, files map[string]string) *repo {
	t.Helper()
	r := &repo{t: t, dir: t.TempDir()}
	r.git("init", "-q", "--template=", "-b", "main")
	r.write(files)
	r.commit("first")
	return r
}

func (r *repo) git(args ...string) string {
	r.t.Helper()
	ctx, cancel := context.WithTimeout(r.t.Context(), 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", append([]string{"-c", "commit.gpgsign=false", "-c", "tag.gpgsign=false", "-c", "core.hooksPath=/dev/null"}, args...)...)
	cmd.Dir = r.dir
	cmd.Env = append(gitEnv(),
		"GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_NOSYSTEM=1",
		"GIT_AUTHOR_NAME=test", "GIT_AUTHOR_EMAIL=test@example.invalid",
		"GIT_COMMITTER_NAME=test", "GIT_COMMITTER_EMAIL=test@example.invalid",
		"GIT_COMMITTER_DATE=2026-09-27T12:00:00Z", "GIT_AUTHOR_DATE=2026-09-27T12:00:00Z")
	out, err := cmd.CombinedOutput()
	if err != nil {
		r.t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
	return strings.TrimSpace(string(out))
}

func (r *repo) write(files map[string]string) {
	r.t.Helper()
	for name, data := range files {
		path := filepath.Join(r.dir, name)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			r.t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(data), 0o644); err != nil {
			r.t.Fatal(err)
		}
	}
}

func (r *repo) commit(message string) {
	r.t.Helper()
	r.git("add", "-A")
	r.git("commit", "-q", "--allow-empty", "-m", message)
}

// A release carries its commit's name, so it must be exactly that commit,
// and a release tag must say which version it is. Each case changes one
// thing about an otherwise releasable repository.
func TestResolveRefusesUnidentifiedSources(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		setup func(r *repo)
		tag   string
		want  string
	}{
		{name: "modified file", setup: func(r *repo) { r.write(map[string]string{"main.go": "package changed\n"}) }, want: "uncommitted"},
		{name: "untracked file", setup: func(r *repo) { r.write(map[string]string{"notes.txt": "x"}) }, want: "untracked"},
		{name: "staged change", setup: func(r *repo) {
			r.write(map[string]string{"main.go": "package staged\n"})
			r.git("add", "main.go")
		}, want: "uncommitted"},
		{name: "version with a v", setup: func(r *repo) {
			r.write(map[string]string{"VERSION": "v1.0.0\n"})
			r.commit("v")
		}, want: "semantic version"},
		{name: "version on two lines", setup: func(r *repo) {
			r.write(map[string]string{"VERSION": "1.0.0\n1.0.1\n"})
			r.commit("two")
		}, want: "semantic version"},
		{name: "version with a leading zero", setup: func(r *repo) {
			r.write(map[string]string{"VERSION": "1.0.0-rc.01\n"})
			r.commit("zero")
		}, want: "semantic version"},
		{name: "tag for another version", setup: func(r *repo) { r.git("tag", "-a", "-m", "x", "v1.0.1") }, tag: "v1.0.1", want: "does not match VERSION"},
		{name: "lightweight tag", setup: func(r *repo) { r.git("tag", "v1.0.0") }, tag: "v1.0.0", want: "lightweight"},
		{name: "tag on an earlier commit", setup: func(r *repo) {
			r.git("tag", "-a", "-m", "x", "v1.0.0")
			r.commit("later")
		}, tag: "v1.0.0", want: "but HEAD is"},
		{name: "missing tag", tag: "v1.0.0", want: "not found"},
		{name: "symbolic link", setup: func(r *repo) {
			if err := os.Symlink("main.go", filepath.Join(r.dir, "link.go")); err != nil {
				r.t.Fatal(err)
			}
			r.commit("link")
		}, want: "only regular files"},
		{name: "vendored modules", setup: func(r *repo) {
			r.write(map[string]string{"vendor/modules.txt": "# x\n"})
			r.commit("vendor")
		}, want: "which module sources"},
		{name: "no version file", setup: func(r *repo) {
			r.git("rm", "-q", "VERSION")
			r.commit("remove")
		}, want: "no VERSION"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			r := newRepo(t, map[string]string{"VERSION": "1.0.0\n", "main.go": "package main\n"})
			if tt.setup != nil {
				tt.setup(r)
			}
			_, err := resolve(t.Context(), r.dir, tt.tag)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("resolve returned %v, want an error about %q", err, tt.want)
			}
		})
	}
}

// state is everything about a repository that work in another one must not
// change: HEAD, the index entries, and the working tree's changes.
func (r *repo) state() string {
	r.t.Helper()
	return r.git("rev-parse", "HEAD") + "\n" + r.git("ls-files", "--stage") + "\n" +
		r.git("status", "--porcelain=v1", "--untracked-files=all")
}

// GIT_DIR, GIT_WORK_TREE, and GIT_INDEX_FILE take precedence over a
// command's working directory, and a git hook that runs the tests or a
// release exports some of them. Pointed at another repository, they must
// change neither what resolve reads nor that repository's commits, index,
// or uncommitted work. The cases set process-wide variables, so they cannot
// run in parallel.
func TestGitIgnoresAnotherRepositorysVariables(t *testing.T) {
	tests := []struct {
		name string
		env  func(outside string) map[string]string
	}{
		{name: "a shell's GIT_DIR and GIT_WORK_TREE", env: func(outside string) map[string]string {
			return map[string]string{"GIT_DIR": filepath.Join(outside, ".git"), "GIT_WORK_TREE": outside}
		}},
		{name: "a pre-commit hook's GIT_DIR and GIT_INDEX_FILE", env: func(outside string) map[string]string {
			return map[string]string{"GIT_DIR": filepath.Join(outside, ".git"), "GIT_INDEX_FILE": filepath.Join(outside, ".git", "index")}
		}},
		{name: "GIT_INDEX_FILE alone", env: func(outside string) map[string]string {
			return map[string]string{"GIT_INDEX_FILE": filepath.Join(outside, ".git", "index")}
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			outside := newRepo(t, map[string]string{"VERSION": "9.9.9\n", "notes.txt": "committed\n"})
			outside.write(map[string]string{"staged.txt": "staged\n"})
			outside.git("add", "staged.txt")
			outside.write(map[string]string{"notes.txt": "changed\n", "draft.txt": "untracked\n"})
			before := outside.state()
			for name, value := range tt.env(outside.dir) {
				t.Setenv(name, value)
			}

			r := newRepo(t, map[string]string{"VERSION": "1.0.0\n", "main.go": "package main\n"})
			s, err := resolve(t.Context(), r.dir, "")
			if err != nil {
				t.Fatal(err)
			}
			if version, _ := s.file("VERSION"); string(version) != "1.0.0\n" {
				t.Fatalf("resolve read VERSION %q from another repository", version)
			}
			if after := outside.state(); after != before {
				t.Fatalf("the other repository changed from\n%s\nto\n%s", before, after)
			}
		})
	}
}

// git replace makes git show another object in place of one, under the
// original's ID, without changing the working tree. A release must read the
// objects its commit ID names, whatever refs/replace says.
func TestResolveIgnoresReplacements(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		replace func(t *testing.T, r *repo)
	}{
		{name: "a replaced file", replace: func(t *testing.T, r *repo) {
			other := filepath.Join(t.TempDir(), "other.go")
			if err := os.WriteFile(other, []byte("package replaced\n"), 0o644); err != nil {
				t.Fatal(err)
			}
			r.git("replace", r.git("rev-parse", "HEAD:main.go"), r.git("hash-object", "-w", other))
		}},
		{name: "a replaced commit", replace: func(t *testing.T, r *repo) {
			head := r.git("rev-parse", "HEAD")
			r.write(map[string]string{"main.go": "package replaced\n"})
			r.commit("other")
			other := r.git("rev-parse", "HEAD")
			r.git("reset", "-q", "--hard", head)
			r.git("replace", head, other)
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			r := newRepo(t, map[string]string{"VERSION": "1.0.0\n", "main.go": "package main\n"})
			head := r.git("rev-parse", "HEAD")
			tt.replace(t, r)

			s, err := resolve(t.Context(), r.dir, "")
			if err != nil {
				t.Fatal(err)
			}
			if data, _ := s.file("main.go"); s.commit != head || string(data) != "package main\n" {
				t.Fatalf("resolved %s with main.go %q, want %s with its own main.go", s.commit, data, head)
			}
		})
	}
}

// A user's git template or configuration can install hooks, which the
// helper's commits would run. The tests must run none: a hook can do
// anything, anywhere. The cases set process-wide variables, so they cannot
// run in parallel.
func TestRepoRunsNoHooks(t *testing.T) {
	tests := []struct {
		name string
		env  func(hooks string) map[string]string
	}{
		{name: "a template with hooks", env: func(hooks string) map[string]string {
			return map[string]string{"GIT_TEMPLATE_DIR": filepath.Dir(hooks)}
		}},
		{name: "core.hooksPath from the environment", env: func(hooks string) map[string]string {
			return map[string]string{"GIT_CONFIG_COUNT": "1", "GIT_CONFIG_KEY_0": "core.hooksPath", "GIT_CONFIG_VALUE_0": hooks}
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			canary := filepath.Join(t.TempDir(), "ran")
			hooks := filepath.Join(t.TempDir(), "template", "hooks")
			if err := os.MkdirAll(hooks, 0o755); err != nil {
				t.Fatal(err)
			}
			script := "#!/bin/sh\necho \"$0\" >> '" + canary + "'\n"
			for _, name := range []string{"pre-commit", "commit-msg", "post-commit", "post-checkout"} {
				if err := os.WriteFile(filepath.Join(hooks, name), []byte(script), 0o755); err != nil {
					t.Fatal(err)
				}
			}
			for name, value := range tt.env(hooks) {
				t.Setenv(name, value)
			}

			r := newRepo(t, map[string]string{"VERSION": "1.0.0\n"})
			r.write(map[string]string{"main.go": "package main\n"})
			r.commit("second")
			if ran, err := os.ReadFile(canary); err == nil {
				t.Fatalf("hooks ran: %s", ran)
			}
		})
	}
}

// A clean commit resolves to its own bytes. Without a tag the version names
// the commit, so a snapshot can never pass for the release it precedes.
func TestResolveIdentifiesTheCommit(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name, tag, version string
	}{
		{name: "snapshot", version: "v1.0.0-rc.1-snapshot.g"},
		{name: "release tag", tag: "v1.0.0-rc.1", version: "v1.0.0-rc.1"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			r := newRepo(t, map[string]string{"VERSION": "1.0.0-rc.1\n", "cmd/main.go": "package main\n", "run.sh": "#!/bin/sh\n"})
			if err := os.Chmod(filepath.Join(r.dir, "run.sh"), 0o755); err != nil {
				t.Fatal(err)
			}
			r.commit("executable")
			if tt.tag != "" {
				r.git("tag", "-a", "-m", "release", tt.tag)
			}
			head := r.git("rev-parse", "HEAD")

			s, err := resolve(t.Context(), r.dir, tt.tag)
			if err != nil {
				t.Fatal(err)
			}
			want := tt.version
			if tt.tag == "" {
				want += head[:12]
			}
			if s.commit != head || s.version() != want {
				t.Fatalf("resolved %s as %s, want %s as %s", s.commit, s.version(), head, want)
			}
			if !s.commitTime.Equal(time.Date(2026, 9, 27, 12, 0, 0, 0, time.UTC)) {
				t.Fatalf("commit time %v", s.commitTime)
			}
			if data, ok := s.file("cmd/main.go"); !ok || string(data) != "package main\n" {
				t.Fatalf("cmd/main.go read as %q", data)
			}
			for _, f := range s.files {
				if f.executable != (f.path == "run.sh") {
					t.Fatalf("%s executable = %v", f.path, f.executable)
				}
			}
		})
	}
}

// Someone who unpacks the binary archive has only the files in it, so a
// relative link from a shipped document must lead to another shipped one.
// Each case gives one document a link; the others hold none.
func TestCheckDocs(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name, file, text, want string
	}{
		{name: "links between shipped documents", file: "docs/usage.md", text: "[a](install.md#go-source) [b](../README.md) [c](#limits)"},
		{name: "a link by URL", file: "README.md", text: "[a](https://github.com/nuggocto/xunhen/blob/shrek/docs/browse.md)"},
		{name: "a guide the archive lacks", file: "docs/usage.md", text: "[a](browse.md)", want: "browse.md"},
		{name: "a file outside docs", file: "docs/install.md", text: "[a](../CONTRIBUTING.md)", want: "../CONTRIBUTING.md"},
		{name: "a missing document", file: "docs/usage.md", want: "has no docs/usage.md"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			s := &source{}
			for _, name := range layout.BinaryDocs {
				switch {
				case name != tt.file:
					s.files = append(s.files, treeFile{path: name, data: []byte("text")})
				case tt.text != "":
					s.files = append(s.files, treeFile{path: name, data: []byte(tt.text)})
				}
			}
			err := checkDocs(s)
			switch {
			case tt.want == "" && err != nil:
				t.Fatal(err)
			case tt.want != "" && (err == nil || !strings.Contains(err.Error(), tt.want)):
				t.Fatalf("checkDocs returned %v, want an error about %q", err, tt.want)
			}
		})
	}
}

// Each of these variables could change the compiled program or skip
// checksum verification without any change to the source.
func TestEnvironmentRules(t *testing.T) {
	t.Parallel()

	release := map[string]string{"GOENV": "off", "GOWORK": "off", "GOTOOLCHAIN": "local"}
	tests := []struct {
		name  string
		set   map[string]string
		valid bool
	}{
		{name: "the release environment", valid: true},
		{name: "a module proxy", set: map[string]string{"GOPROXY": "https://proxy.example"}, valid: true},
		{name: "injected build flags", set: map[string]string{"GOFLAGS": "-ldflags=-X=main.version=v9"}},
		{name: "a compiler experiment", set: map[string]string{"GOEXPERIMENT": "arenas"}},
		{name: "skipped checksum database", set: map[string]string{"GONOSUMDB": "*"}},
		{name: "checksum database off", set: map[string]string{"GOSUMDB": "off"}},
		{name: "private modules", set: map[string]string{"GOPRIVATE": "*"}},
		{name: "automatic toolchain", set: map[string]string{"GOTOOLCHAIN": "auto"}},
		{name: "a workspace", set: map[string]string{"GOWORK": "/tmp/go.work"}},
		{name: "a go.env file", set: map[string]string{"GOENV": ""}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			env := maps.Clone(release)
			maps.Copy(env, tt.set)
			err := checkEnvironment(func(name string) string { return env[name] })
			if (err == nil) != tt.valid {
				t.Fatalf("checkEnvironment returned %v", err)
			}
		})
	}
}

// buildEnv must not pass through a variable that checkEnvironment would
// refuse, or anything outside its list, such as a stray CGO flag.
func TestBuildEnvironmentIsExplicit(t *testing.T) {
	t.Parallel()

	caller := map[string]string{"PATH": "/usr/bin", "HOME": "/home/x", "CGO_ENABLED": "1", "GOAMD64": "v3", "GOFLAGS": "-race", "CC": "evil-cc"}
	env := buildEnv(func(name string) string { return caller[name] })
	seen := map[string]string{}
	for _, entry := range env {
		name, value, _ := strings.Cut(entry, "=")
		if _, dup := seen[name]; dup {
			t.Fatalf("%s appears twice", name)
		}
		seen[name] = value
	}
	want := map[string]string{"CGO_ENABLED": "0", "GOAMD64": "v1", "GOFLAGS": "-mod=readonly", "PATH": "/usr/bin", "GOTOOLCHAIN": "local"}
	for name, value := range want {
		if seen[name] != value {
			t.Fatalf("%s=%q, want %q", name, seen[name], value)
		}
	}
	if _, ok := seen["CC"]; ok {
		t.Fatal("CC reached the build")
	}
}

// A replace directive compiles other sources than go.sum names, so a
// release refuses it even when the build would succeed.
func TestModuleRules(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name, gomod, want string
	}{
		{name: "replace directive", gomod: "module github.com/nuggocto/xunhen\n\ngo 1.27.1\n\nreplace example.com/a => ../a\n", want: "replace"},
		{name: "another module", gomod: "module example.com/other\n\ngo 1.27.1\n", want: "declares module"},
		{name: "another toolchain", gomod: "module github.com/nuggocto/xunhen\n\ngo 1.27.1\n\ntoolchain go1.99.0\n", want: "selects go1.99.0"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte(tt.gomod), 0o644); err != nil {
				t.Fatal(err)
			}
			err := checkModule(t.Context(), dir, buildEnv(os.Getenv))
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("checkModule returned %v, want an error about %q", err, tt.want)
			}
		})
	}
}

// A child's output reaches the buffer through io.Copy from a pipe, an
// *os.File, as os/exec copies it. io.Copy prefers the destination's
// ReadFrom to its Write, so the limit must hold on that path too.
func TestLimitedBufferThroughCopy(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		size         int
		wantOverflow bool
	}{
		{name: "under the limit", size: 7},
		{name: "at the limit", size: 8},
		{name: "past the limit", size: 9, wantOverflow: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			path := filepath.Join(t.TempDir(), "output")
			if err := os.WriteFile(path, []byte(strings.Repeat("x", tt.size)), 0o644); err != nil {
				t.Fatal(err)
			}
			f, err := os.Open(path)
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = f.Close() }()

			b := limitedBuffer{max: 8}
			if _, err := io.Copy(&b, f); err != nil {
				t.Fatal(err)
			}
			if kept := len(b.Bytes()); kept != min(tt.size, 8) || b.overflow != tt.wantOverflow {
				t.Fatalf("kept %d bytes with overflow %t, want %d and %t", kept, b.overflow, min(tt.size, 8), tt.wantOverflow)
			}
		})
	}
}
