package main

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"
)

// source is one resolved commit: everything a release reads from the
// repository, read once, so every later step works from the same bytes.
type source struct {
	commit     string // full object ID
	tree       string
	commitTime time.Time
	declared   string // VERSION, without the leading v
	tag        string // the release tag, or "" for a snapshot
	files      []treeFile
}

// treeFile is one file of the commit's tree.
type treeFile struct {
	path       string
	executable bool
	data       []byte
}

// version is the version the executable reports: the tag for a release, or
// a snapshot version that no tag can share.
func (s *source) version() string {
	if s.tag != "" {
		return s.tag
	}
	return "v" + s.declared + "-snapshot.g" + s.commit[:12]
}

// archiveVersion is the version in artifact names, without the leading v.
func (s *source) archiveVersion() string {
	return strings.TrimPrefix(s.version(), "v")
}

func (s *source) file(path string) ([]byte, bool) {
	for _, f := range s.files {
		if f.path == path {
			return f.data, true
		}
	}
	return nil, false
}

// Bounds on what a release reads from the repository. The tree holds a few
// hundred small files; these stop a mistaken commit of something huge.
const (
	maxBlobBytes = 64 << 20
	maxTreeBytes = 256 << 20
)

// semver matches a Semantic Versioning 2.0.0 version without build metadata,
// which would put a "+" in file names and URLs.
var semver = regexp.MustCompile(`^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)` +
	`(-(0|[1-9][0-9]*|[0-9]*[A-Za-z-][0-9A-Za-z-]*)(\.(0|[1-9][0-9]*|[0-9]*[A-Za-z-][0-9A-Za-z-]*))*)?$`)

var objectID = regexp.MustCompile(`^[0-9a-f]{40}$`)

// resolve reads the commit at HEAD of the repository at root. The working
// tree must be clean, untracked files included, because a release built
// from a tree that differs from its commit would carry the commit's name
// without its content. With a tag, the tag must be annotated, name HEAD, and
// equal "v" followed by VERSION.
func resolve(ctx context.Context, root, tag string) (*source, error) {
	status, err := git(ctx, root, nil, "status", "--porcelain=v1", "-z", "--untracked-files=all", "--ignore-submodules=none")
	if err != nil {
		return nil, err
	}
	if len(status) != 0 {
		return nil, errors.New("the working tree has uncommitted or untracked changes; commit or remove them, since a release must be exactly its commit")
	}

	s := &source{}
	if s.commit, err = gitLine(ctx, root, "rev-parse", "--verify", "HEAD^{commit}"); err != nil {
		return nil, err
	}
	if s.tree, err = gitLine(ctx, root, "rev-parse", "--verify", s.commit+"^{tree}"); err != nil {
		return nil, err
	}
	if !objectID.MatchString(s.commit) || !objectID.MatchString(s.tree) {
		return nil, fmt.Errorf("unexpected object IDs %q and %q; only SHA-1 repositories are supported", s.commit, s.tree)
	}
	seconds, err := gitLine(ctx, root, "show", "-s", "--format=%ct", s.commit)
	if err != nil {
		return nil, err
	}
	unix, err := strconv.ParseInt(seconds, 10, 64)
	if err != nil {
		return nil, fmt.Errorf("commit time %q: %w", seconds, err)
	}
	s.commitTime = time.Unix(unix, 0).UTC()

	if s.files, err = readTree(ctx, root, s.commit); err != nil {
		return nil, err
	}
	declared, ok := s.file("VERSION")
	if !ok {
		return nil, errors.New("the commit has no VERSION file")
	}
	if s.declared, err = parseVersion(declared); err != nil {
		return nil, err
	}

	if tag != "" {
		if err := checkTag(ctx, root, tag, s); err != nil {
			return nil, err
		}
		s.tag = tag
	}
	return s, nil
}

// parseVersion reads VERSION: one Semantic Versioning line with no "v".
func parseVersion(data []byte) (string, error) {
	line, rest, _ := strings.Cut(string(data), "\n")
	if rest != "" || !semver.MatchString(line) {
		return "", fmt.Errorf("VERSION must hold one semantic version such as 1.2.3 or 1.2.3-rc.1 on a single line, not %q", data)
	}
	return line, nil
}

func checkTag(ctx context.Context, root, tag string, s *source) error {
	if tag != "v"+s.declared {
		return fmt.Errorf("tag %q does not match VERSION %q; the tag must be v%s", tag, s.declared, s.declared)
	}
	kind, err := gitLine(ctx, root, "cat-file", "-t", "refs/tags/"+tag)
	if err != nil {
		return fmt.Errorf("tag %q not found: %w", tag, err)
	}
	if kind != "tag" {
		return fmt.Errorf("tag %q is a lightweight tag; release tags are annotated (git tag -a)", tag)
	}
	target, err := gitLine(ctx, root, "rev-parse", "--verify", "refs/tags/"+tag+"^{commit}")
	if err != nil {
		return err
	}
	if target != s.commit {
		return fmt.Errorf("tag %q names commit %s, but HEAD is %s", tag, target, s.commit)
	}
	return nil
}

// readTree reads every file of a commit's tree straight from the object
// store. git archive would apply export-ignore, export-subst, and tar.umask
// from the repository's attributes and configuration; reading blobs cannot
// be changed that way. Symbolic links and submodules are refused, since
// neither belongs in a release.
func readTree(ctx context.Context, root, commit string) ([]treeFile, error) {
	listing, err := git(ctx, root, nil, "ls-tree", "-r", "-z", "--full-tree", commit)
	if err != nil {
		return nil, err
	}

	var files []treeFile
	var ids bytes.Buffer
	for entry := range bytes.SplitSeq(bytes.TrimSuffix(listing, []byte{0}), []byte{0}) {
		meta, path, ok := bytes.Cut(entry, []byte{'\t'})
		fields := strings.Fields(string(meta))
		if !ok || len(fields) != 3 {
			return nil, fmt.Errorf("unexpected tree entry %q", entry)
		}
		mode, kind, id := fields[0], fields[1], fields[2]
		switch {
		case kind != "blob":
			return nil, fmt.Errorf("%s is a %s; releases hold only files", path, kind)
		case mode != "100644" && mode != "100755":
			return nil, fmt.Errorf("%s has mode %s; releases hold only regular files", path, mode)
		case strings.HasPrefix(string(path), "vendor/") || string(path) == "go.work":
			return nil, fmt.Errorf("%s would change which module sources are compiled", path)
		}
		files = append(files, treeFile{path: string(path), executable: mode == "100755"})
		ids.WriteString(id)
		ids.WriteByte('\n')
	}

	// One cat-file process reads every blob in listing order.
	out, err := git(ctx, root, &ids, "cat-file", "--batch")
	if err != nil {
		return nil, err
	}
	r := bufio.NewReader(bytes.NewReader(out))
	total := 0
	for i := range files {
		header, err := r.ReadString('\n')
		if err != nil {
			return nil, fmt.Errorf("read blob for %s: %w", files[i].path, err)
		}
		fields := strings.Fields(header)
		if len(fields) != 3 || fields[1] != "blob" {
			return nil, fmt.Errorf("unexpected object header %q for %s", header, files[i].path)
		}
		size, err := strconv.Atoi(fields[2])
		if err != nil || size < 0 || size > maxBlobBytes || total+size > maxTreeBytes {
			return nil, fmt.Errorf("%s is too large for a release", files[i].path)
		}
		total += size
		files[i].data = make([]byte, size)
		if _, err := io.ReadFull(r, files[i].data); err != nil {
			return nil, fmt.Errorf("read blob for %s: %w", files[i].path, err)
		}
		if b, err := r.ReadByte(); err != nil || b != '\n' {
			return nil, fmt.Errorf("blob for %s is not terminated", files[i].path)
		}
	}
	return files, nil
}

// repositoryEnv lists the variables that make git read another repository,
// index, object store, or configuration than the one in its working
// directory: the list "git rev-parse --local-env-vars" prints. A git hook
// exports some of them, so a release run from a hook, or with them left set
// in a shell, would otherwise read some other repository's commit. Dropping
// GIT_NO_REPLACE_OBJECTS with the rest is safe: git always runs with
// --no-replace-objects.
var repositoryEnv = []string{
	"GIT_ALTERNATE_OBJECT_DIRECTORIES", "GIT_CONFIG", "GIT_CONFIG_PARAMETERS", "GIT_CONFIG_COUNT",
	"GIT_OBJECT_DIRECTORY", "GIT_DIR", "GIT_WORK_TREE", "GIT_IMPLICIT_WORK_TREE", "GIT_GRAFT_FILE",
	"GIT_INDEX_FILE", "GIT_NO_REPLACE_OBJECTS", "GIT_REPLACE_REF_BASE", "GIT_PREFIX",
	"GIT_SHALLOW_FILE", "GIT_COMMON_DIR",
}

// gitEnv is this process's environment without repositoryEnv, so git finds
// the repository from its working directory alone.
func gitEnv() []string {
	return slices.DeleteFunc(os.Environ(), func(entry string) bool {
		name, _, _ := strings.Cut(entry, "=")
		return slices.Contains(repositoryEnv, name)
	})
}

// maxGitOutput bounds what one git command may print; the largest is the
// batch of every blob, which maxTreeBytes already bounds.
const maxGitOutput = maxTreeBytes + 1<<20

func git(ctx context.Context, dir string, stdin io.Reader, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()

	// A replacement under refs/replace shows another object under the
	// original's ID, so a release would carry the commit's name with other
	// contents.
	cmd := exec.CommandContext(ctx, "git", append([]string{"--no-replace-objects"}, args...)...)
	cmd.Dir = dir
	cmd.Env = gitEnv()
	cmd.Stdin = stdin
	cmd.WaitDelay = waitDelay
	var stdout, stderr limitedBuffer
	stdout.max, stderr.max = maxGitOutput, 64<<10
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("git %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(stderr.String()))
	}
	if stdout.overflow {
		return nil, fmt.Errorf("git %s printed more than %d bytes", strings.Join(args, " "), stdout.max)
	}
	return stdout.Bytes(), nil
}

func gitLine(ctx context.Context, dir string, args ...string) (string, error) {
	out, err := git(ctx, dir, nil, args...)
	return strings.TrimSpace(string(out)), err
}

// limitedBuffer keeps at most max bytes and records whether more arrived.
// The buffer is a named field: an embedded bytes.Buffer would give
// limitedBuffer its ReadFrom, which io.Copy calls instead of Write, and a
// command's output would bypass the limit.
type limitedBuffer struct {
	buf      bytes.Buffer
	max      int
	overflow bool
}

func (b *limitedBuffer) Write(p []byte) (int, error) {
	if room := b.max - b.buf.Len(); len(p) > room {
		b.overflow = true
		b.buf.Write(p[:max(room, 0)])
		return len(p), nil
	}
	return b.buf.Write(p)
}

func (b *limitedBuffer) Bytes() []byte  { return b.buf.Bytes() }
func (b *limitedBuffer) String() string { return b.buf.String() }

// waitDelay bounds how long a command's output may stay open after the
// command exits or is killed, as it can when a process it started inherits
// the pipe and outlives it.
const waitDelay = 5 * time.Second
