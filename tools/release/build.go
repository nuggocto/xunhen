package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"debug/buildinfo"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"runtime/debug"
	"slices"
	"strings"
	"time"

	"github.com/nuggocto/xunhen/tools/internal/layout"
	"github.com/nuggocto/xunhen/tools/internal/tarball"
)

const (
	modulePath  = "github.com/nuggocto/xunhen"
	mainPackage = modulePath + "/cmd/xunhen"
	repository  = "https://github.com/nuggocto/xunhen"
	noticesFile = "THIRD_PARTY_NOTICES.txt"
)

// target is the one supported release configuration.
var target = []string{"GOOS=linux", "GOARCH=amd64", "GOAMD64=v1", "CGO_ENABLED=0"}

// Environment variables a release refuses. Each one can change what is
// compiled, or weaken the checksum verification of downloaded modules,
// without appearing in the source.
var refusedEnv = []string{"GOFLAGS", "GOEXPERIMENT", "GOINSECURE", "GONOSUMDB", "GONOSUMCHECK", "GOPRIVATE"}

// requiredEnv must hold these values, which tools/release.sh sets before
// the release tool is even compiled: no go.env file, no workspace, and no
// automatic toolchain download.
var requiredEnv = map[string]string{"GOENV": "off", "GOWORK": "off", "GOTOOLCHAIN": "local"}

// passedEnv reaches the go command unchanged: where to find tools, caches,
// and the module proxy. Everything else is dropped.
var passedEnv = []string{
	"PATH", "HOME", "TMPDIR", "GOROOT", "GOPATH", "GOCACHE", "GOMODCACHE", "GOPROXY", "GOSUMDB", "GONOPROXY",
	"HTTP_PROXY", "HTTPS_PROXY", "NO_PROXY", "http_proxy", "https_proxy", "no_proxy", "SSL_CERT_FILE", "SSL_CERT_DIR",
}

func checkEnvironment(getenv func(string) string) error {
	for _, name := range refusedEnv {
		if value := getenv(name); value != "" {
			return fmt.Errorf("%s=%q is set; unset it, since it could change the release without changing its source", name, value)
		}
	}
	for name, want := range requiredEnv {
		if got := getenv(name); got != want {
			return fmt.Errorf("%s must be %q, not %q; run the release through tools/release.sh", name, want, got)
		}
	}
	if getenv("GOSUMDB") == "off" {
		return errors.New("GOSUMDB=off disables checksum verification of downloaded modules")
	}
	return nil
}

// buildEnv is the whole environment of every go command a release runs.
func buildEnv(getenv func(string) string) []string {
	env := []string{"GOENV=off", "GOWORK=off", "GOTOOLCHAIN=local", "GOFLAGS=-mod=readonly", "LC_ALL=C"}
	for _, name := range passedEnv {
		if value := getenv(name); value != "" {
			env = append(env, name+"="+value)
		}
	}
	return append(env, target...)
}

// options configure one build. Tests replace getenv.
type options struct {
	root, tag, out string
	getenv         func(string) string
	log            io.Writer
}

// built is what a release produced, for the caller to report.
type built struct {
	version string
	out     string
	sums    []byte
}

func build(ctx context.Context, o options) (b *built, err error) {
	if err := checkEnvironment(o.getenv); err != nil {
		return nil, err
	}
	s, err := resolve(ctx, o.root, o.tag)
	if err != nil {
		return nil, err
	}
	if err := checkDocs(s); err != nil {
		return nil, err
	}
	logf(o.log, "source %s (tree %s, %s), version %s", s.commit, s.tree, s.commitTime.Format(time.RFC3339), s.version())

	out := o.out
	if out == "" {
		out = filepath.Join(o.root, "dist", s.archiveVersion())
	}
	if err := os.MkdirAll(filepath.Dir(out), 0o755); err != nil {
		return nil, err
	}
	// The directory must be new, so no earlier build's files can mix in,
	// and a failed build removes it, so no partial set is left to publish.
	if err := os.Mkdir(out, 0o755); err != nil {
		return nil, fmt.Errorf("output directory: %w; choose a new one", err)
	}
	defer func() {
		if err != nil {
			_ = os.RemoveAll(out)
		}
	}()

	stage, err := os.MkdirTemp("", "xunhen-release-")
	if err != nil {
		return nil, err
	}
	defer func() { _ = os.RemoveAll(stage) }()

	src := filepath.Join(stage, "src")
	if err := writeTree(src, s.files); err != nil {
		return nil, err
	}
	env := buildEnv(o.getenv)
	if err := checkModule(ctx, src, env); err != nil {
		return nil, err
	}

	executable := filepath.Join(stage, "xunhen")
	ldflags := "-X main.version=" + s.version() + " -X main.commit=" + s.commit
	logf(o.log, "building %s for %s", mainPackage, strings.Join(target, " "))
	if _, err := goCommand(ctx, src, env, "build", "-trimpath", "-buildvcs=false", "-ldflags="+ldflags, "-o", executable, "./cmd/xunhen"); err != nil {
		return nil, err
	}
	info, err := buildinfo.ReadFile(executable)
	if err != nil {
		return nil, err
	}

	notices, err := generateNotices(ctx, src, env, info)
	if err != nil {
		return nil, err
	}
	if committed, _ := s.file(noticesFile); !bytes.Equal(committed, notices) {
		return nil, fmt.Errorf("%s does not match the modules linked into the executable; run tools/release.sh notices and commit the result", noticesFile)
	}

	exe, err := os.ReadFile(executable)
	if err != nil {
		return nil, err
	}
	binaryName := layout.BinaryArchive(s.version())
	sourceName := layout.SourceArchive(s.version())
	archives := []struct {
		name  string
		files []tarball.File
	}{
		{name: binaryName + ".tar.gz", files: binaryFiles(s, exe)},
		{name: sourceName + ".tar.gz", files: sourceFiles(s)},
	}

	var sums bytes.Buffer
	var artifacts []artifact
	for _, a := range archives {
		var data bytes.Buffer
		if err := tarball.Write(&data, strings.TrimSuffix(a.name, ".tar.gz"), a.files, s.commitTime); err != nil {
			return nil, err
		}
		// Read the archive back through the same checks a user's
		// verification applies, and compare every file.
		back, err := tarball.Read(bytes.NewReader(data.Bytes()))
		if err != nil || !tarball.Equal(back.Files, a.files) {
			return nil, fmt.Errorf("%s does not read back as written: %v", a.name, err)
		}
		if err := os.WriteFile(filepath.Join(out, a.name), data.Bytes(), 0o644); err != nil {
			return nil, err
		}
		digest := sha256.Sum256(data.Bytes())
		fmt.Fprintf(&sums, "%x  %s\n", digest, a.name)
		artifacts = append(artifacts, artifact{Name: a.name, SHA256: hex.EncodeToString(digest[:]), Size: int64(data.Len())})
	}
	if err := os.WriteFile(filepath.Join(out, "SHA256SUMS.txt"), sums.Bytes(), 0o644); err != nil {
		return nil, err
	}

	exeDigest := sha256.Sum256(exe)
	artifacts = append(artifacts, artifact{Name: binaryName + "/" + layout.Executable, SHA256: hex.EncodeToString(exeDigest[:]), Size: int64(len(exe))})
	p, err := newProvenance(ctx, o, s, info, ldflags, artifacts)
	if err != nil {
		return nil, err
	}
	data, err := json.MarshalIndent(p, "", "  ")
	if err != nil {
		return nil, err
	}
	if err := os.WriteFile(filepath.Join(out, "provenance.json"), append(data, '\n'), 0o644); err != nil {
		return nil, err
	}

	return &built{version: s.version(), out: out, sums: sums.Bytes()}, nil
}

// writeTree writes the commit's files for the build, so the go command
// compiles exactly the commit and nothing else in the working tree.
func writeTree(dir string, files []treeFile) error {
	for _, f := range files {
		path := filepath.Join(dir, filepath.FromSlash(f.path))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return err
		}
		mode := os.FileMode(0o644)
		if f.executable {
			mode = 0o755
		}
		if err := os.WriteFile(path, f.data, mode); err != nil {
			return err
		}
	}
	return nil
}

// checkModule refuses a go.mod that points module paths at other sources,
// confirms that the toolchain is the one go.mod selects, and verifies the
// module cache against go.sum.
func checkModule(ctx context.Context, dir string, env []string) error {
	out, err := goCommand(ctx, dir, env, "mod", "edit", "-json")
	if err != nil {
		return err
	}
	var mod struct {
		Module    struct{ Path string }
		Go        string
		Toolchain string
		Replace   []json.RawMessage
	}
	if err := json.Unmarshal(out, &mod); err != nil {
		return fmt.Errorf("read go.mod: %w", err)
	}
	switch {
	case mod.Module.Path != modulePath:
		return fmt.Errorf("go.mod declares module %q, not %q", mod.Module.Path, modulePath)
	case len(mod.Replace) != 0:
		return errors.New("go.mod has replace directives, which would compile other sources than the ones go.sum names")
	}

	want := "go" + mod.Go
	if mod.Toolchain != "" {
		want = mod.Toolchain
	}
	got, err := goCommand(ctx, dir, env, "env", "GOVERSION")
	if err != nil {
		return err
	}
	if strings.TrimSpace(string(got)) != want {
		return fmt.Errorf("the go command is %s, but go.mod selects %s; install that toolchain", strings.TrimSpace(string(got)), want)
	}

	if _, err := goCommand(ctx, dir, env, "mod", "download"); err != nil {
		return err
	}
	_, err = goCommand(ctx, dir, env, "mod", "verify")
	return err
}

func binaryFiles(s *source, exe []byte) []tarball.File {
	files := []tarball.File{{Name: layout.Executable, Executable: true, Data: exe}}
	for _, name := range layout.BinaryDocs {
		data, _ := s.file(name)
		files = append(files, tarball.File{Name: name, Data: data})
	}
	return files
}

func sourceFiles(s *source) []tarball.File {
	files := make([]tarball.File, 0, len(s.files))
	for _, f := range s.files {
		files = append(files, tarball.File{Name: f.path, Executable: f.executable, Data: f.data})
	}
	return files
}

// markdownLink matches the target of an inline Markdown link.
var markdownLink = regexp.MustCompile(`\]\(([^)\s]+)\)`)

// checkDocs confirms every file the binary archive names is in the commit,
// and that their relative links lead only to one another: someone who
// unpacks the archive has nothing else. Anything outside it needs a URL.
func checkDocs(s *source) error {
	for _, name := range layout.BinaryDocs {
		data, ok := s.file(name)
		if !ok {
			return fmt.Errorf("the commit has no %s, which the binary archive carries", name)
		}
		for _, m := range markdownLink.FindAllSubmatch(data, -1) {
			target, _, _ := strings.Cut(string(m[1]), "#")
			if target == "" || strings.Contains(target, "://") || strings.HasPrefix(target, "mailto:") {
				continue
			}
			if !slices.Contains(layout.BinaryDocs, path.Join(path.Dir(name), target)) {
				return fmt.Errorf("%s links to %s, which the binary archive does not carry; link to it by URL", name, m[1])
			}
		}
	}
	return nil
}

// maxGoOutput bounds one go command's output, which is at most a module
// listing or a compiler error.
const maxGoOutput = 8 << 20

func goCommand(ctx context.Context, dir string, env []string, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Minute)
	defer cancel()

	cmd := exec.CommandContext(ctx, "go", args...)
	cmd.Dir = dir
	cmd.Env = env
	cmd.WaitDelay = waitDelay
	var stdout, stderr limitedBuffer
	stdout.max, stderr.max = maxGoOutput, 1<<20
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("go %s: %w\n%s", strings.Join(args, " "), err, strings.TrimSpace(stderr.String()))
	}
	if stdout.overflow {
		return nil, fmt.Errorf("go %s printed more than %d bytes", strings.Join(args, " "), stdout.max)
	}
	return stdout.Bytes(), nil
}

// provenance records where the artifacts came from. It changes from run to
// run, so it stays outside the archives, which must reproduce exactly. It
// holds no file system paths and no environment beyond the CI run's
// identity.
type provenance struct {
	Schema       string       `json:"schema"`
	Version      string       `json:"version"`
	Tag          string       `json:"tag,omitempty"`
	Snapshot     bool         `json:"snapshot"`
	Source       sourceInfo   `json:"source"`
	Build        buildInfo    `json:"build"`
	Dependencies []dependency `json:"dependencies"`
	Artifacts    []artifact   `json:"artifacts"`
	Tools        toolInfo     `json:"tools"`
	Run          runInfo      `json:"run"`
}

type sourceInfo struct {
	Repository string `json:"repository"`
	Commit     string `json:"commit"`
	Tree       string `json:"tree"`
	CommitTime string `json:"commit_time"`
}

type buildInfo struct {
	Main     string            `json:"main"`
	Go       string            `json:"go"`
	Settings map[string]string `json:"settings"`
}

type dependency struct {
	Path    string `json:"path"`
	Version string `json:"version"`
	Sum     string `json:"sum"`
}

type artifact struct {
	Name   string `json:"name"`
	SHA256 string `json:"sha256"`
	Size   int64  `json:"size"`
}

type toolInfo struct {
	Git string `json:"git"`
}

type runInfo struct {
	Kind        string `json:"kind"`
	Server      string `json:"server,omitempty"`
	Repository  string `json:"repository,omitempty"`
	RunID       string `json:"run_id,omitempty"`
	RunAttempt  string `json:"run_attempt,omitempty"`
	WorkflowRef string `json:"workflow_ref,omitempty"`
	Runner      string `json:"runner_image,omitempty"`
}

func newProvenance(ctx context.Context, o options, s *source, info *debug.BuildInfo, ldflags string, artifacts []artifact) (*provenance, error) {
	gitVersion, err := gitLine(ctx, o.root, "version")
	if err != nil {
		return nil, err
	}

	p := &provenance{
		Schema:   "xunhen-release-provenance/1",
		Version:  s.archiveVersion(),
		Tag:      s.tag,
		Snapshot: s.tag == "",
		Source: sourceInfo{
			Repository: repository,
			Commit:     s.commit,
			Tree:       s.tree,
			CommitTime: s.commitTime.Format(time.RFC3339),
		},
		Build:     buildInfo{Main: info.Path, Go: info.GoVersion, Settings: map[string]string{}},
		Artifacts: artifacts,
		Tools:     toolInfo{Git: gitVersion},
		Run:       runInfo{Kind: "local"},
	}
	for _, setting := range info.Settings {
		p.Build.Settings[setting.Key] = setting.Value
	}
	// With -trimpath the toolchain leaves -ldflags out of the executable's
	// build information, since linker flags can hold paths, so record the
	// flags this build passed. The version output shows their effect.
	p.Build.Settings["-ldflags"] = ldflags
	for _, dep := range info.Deps {
		if dep.Replace != nil {
			return nil, fmt.Errorf("dependency %s was replaced", dep.Path)
		}
		p.Dependencies = append(p.Dependencies, dependency{Path: dep.Path, Version: dep.Version, Sum: dep.Sum})
	}
	slices.SortFunc(p.Dependencies, func(a, b dependency) int { return strings.Compare(a.Path, b.Path) })

	if o.getenv("GITHUB_ACTIONS") == "true" {
		p.Run = runInfo{
			Kind:        "github-actions",
			Server:      o.getenv("GITHUB_SERVER_URL"),
			Repository:  o.getenv("GITHUB_REPOSITORY"),
			RunID:       o.getenv("GITHUB_RUN_ID"),
			RunAttempt:  o.getenv("GITHUB_RUN_ATTEMPT"),
			WorkflowRef: o.getenv("GITHUB_WORKFLOW_REF"),
			Runner:      strings.TrimSpace(o.getenv("ImageOS") + " " + o.getenv("ImageVersion")),
		}
	}
	return p, nil
}

func logf(w io.Writer, format string, args ...any) {
	if w != nil {
		fmt.Fprintf(w, "release: "+format+"\n", args...)
	}
}
