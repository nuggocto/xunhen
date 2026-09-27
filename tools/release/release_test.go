package main

import (
	"context"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// repo is a throwaway git repository. Commands pass their identity and
// disable signing on the command line, so the user's configuration cannot
// change what the tests see.
type repo struct {
	t   *testing.T
	dir string
}

func newRepo(t *testing.T, files map[string]string) *repo {
	t.Helper()
	r := &repo{t: t, dir: t.TempDir()}
	r.git("init", "-q", "-b", "main")
	r.write(files)
	r.commit("first")
	return r
}

func (r *repo) git(args ...string) string {
	r.t.Helper()
	ctx, cancel := context.WithTimeout(r.t.Context(), 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", append([]string{"-c", "commit.gpgsign=false", "-c", "tag.gpgsign=false"}, args...)...)
	cmd.Dir = r.dir
	cmd.Env = append(os.Environ(),
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
