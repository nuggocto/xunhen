package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"syscall"
	"time"
)

// commandTimeout bounds one run of the executable. The slowest command here
// replays a fixture of a few lines; a run that takes seconds is a hang.
const commandTimeout = 30 * time.Second

// waitDelay bounds how long a run waits for the child's output pipes to
// close after the child exits or is killed. The executable starts no
// processes, so only a broken one leaves a descendant holding them open;
// run then ends it.
const waitDelay = time.Second

// maxOutput bounds what one run may print. The largest expected output is a
// few kilobytes; anything near this is a failure in itself.
const maxOutput = 4 << 20

// world is a private directory tree the executable runs in: copies of the
// fixtures, a HOME, and a PATH of decoy programs.
type world struct {
	binary string
	root   string // physical path: xunhen resolves sources through symlinks
	corpus string // the copied fixtures
	home   string
	bin    string // PATH: decoys named after programs xunhen must not run
	marker string // a decoy creates this file when run
	// scratch holds files a check changes on purpose. It sits outside
	// root, so those changes do not count as changed inputs.
	scratch string
}

// Programs the executable must never start. A decoy with each name records
// that it ran; none may.
var decoys = []string{"nvim", "vim", "git", "go", "sh", "bash", "xdg-open", "less", "more"}

func verify(ctx context.Context, c config) []result {
	var results []result
	binary := c.binary
	if c.archive != "" {
		exe, detail, err := checkArchive(c)
		results = append(results, result{name: "release archive", detail: detail, err: err})
		if err != nil {
			return results
		}
		defer func() { _ = os.RemoveAll(filepath.Dir(exe)) }()
		binary = exe
	}

	w, err := newWorld(binary, c.corpus)
	if err != nil {
		return append(results, result{name: "setup", err: err})
	}
	defer w.remove()

	before, err := fingerprint(w.root)
	if err != nil {
		return append(results, result{name: "setup", err: err})
	}

	checks := []struct {
		name string
		run  func(context.Context) (string, error)
	}{
		{name: "executable metadata", run: func(context.Context) (string, error) { return checkMetadata(w.binary, c) }},
		{name: "static linkage", run: func(context.Context) (string, error) { return checkLinkage(w.binary, c.buildmode) }},
		{name: "version output", run: func(ctx context.Context) (string, error) { return w.checkVersion(ctx, c) }},
		{name: "recovery corpus", run: w.checkCorpus},
		{name: "discovery and diff walkthrough", run: w.checkWalkthrough},
		{name: "failures and diagnostics", run: w.checkFailures},
		{name: "permission denials", run: w.checkPermissions},
		{name: "unusual paths", run: w.checkPaths},
		{name: "interrupt while output is blocked", run: w.checkInterrupt},
		{name: "browser on a terminal", run: w.checkBrowser},
	}
	for _, check := range checks {
		detail, err := check.run(ctx)
		results = append(results, result{name: check.name, detail: detail, err: err})
	}

	after, err := fingerprint(w.root)
	if err == nil {
		err = sameFingerprint(before, after)
	}
	results = append(results, result{name: "inputs and HOME unchanged", err: err})

	_, err = os.Stat(w.marker)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		results = append(results, result{name: "no editor, git, Go, or shell started"})
	case err == nil:
		ran, _ := os.ReadFile(w.marker)
		results = append(results, result{name: "no editor, git, Go, or shell started", err: fmt.Errorf("the executable started %s", strings.TrimSpace(string(ran)))})
	default:
		results = append(results, result{name: "no editor, git, Go, or shell started", err: err})
	}
	return results
}

func newWorld(binary, corpus string) (_ *world, err error) {
	info, err := os.Stat(binary)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Mode().Perm()&0o111 == 0 {
		return nil, fmt.Errorf("%s is not an executable file", binary)
	}
	absolute, err := filepath.Abs(binary)
	if err != nil {
		return nil, err
	}

	dir, err := os.MkdirTemp("", "xunhen-verify-")
	if err != nil {
		return nil, err
	}
	root, err := filepath.EvalSymlinks(dir)
	if err != nil {
		_ = os.RemoveAll(dir)
		return nil, err
	}
	w := &world{
		binary: absolute,
		root:   root,
		corpus: filepath.Join(root, "corpus"),
		home:   filepath.Join(root, "home"),
		bin:    filepath.Join(root, "bin"),
		// The marker sits outside root, so writing it does not also show
		// up as a changed input.
		marker:  root + ".ran",
		scratch: root + ".scratch",
	}
	defer func() {
		if err != nil {
			w.remove()
		}
	}()
	for _, d := range []string{w.home, w.bin, w.scratch} {
		if err := os.Mkdir(d, 0o755); err != nil {
			return nil, err
		}
	}
	if err := copyCorpus(corpus, w.corpus); err != nil {
		return nil, err
	}
	if err := w.installDecoys(); err != nil {
		return nil, err
	}
	if err := w.prepareInputs(); err != nil {
		return nil, err
	}
	return w, nil
}

// remove deletes the world and the marker and scratch directory beside it.
// Permission checks leave directories without permissions, so those are
// opened up first.
func (w *world) remove() {
	for _, dir := range []string{w.root, w.scratch} {
		_ = filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
			if d != nil && d.IsDir() {
				_ = os.Chmod(path, 0o755)
			}
			return nil
		})
		_ = os.RemoveAll(dir)
	}
	_ = os.Remove(w.marker)
}

// copyCorpus copies each fixture's files read-only, so the executable only
// ever sees copies, and a write attempt fails as well as being detected.
func copyCorpus(from, to string) error {
	dirs, err := os.ReadDir(from)
	if err != nil {
		return fmt.Errorf("read the fixture corpus: %w", err)
	}
	copied := 0
	for _, d := range dirs {
		if !d.IsDir() {
			continue
		}
		if err := os.MkdirAll(filepath.Join(to, d.Name()), 0o755); err != nil {
			return err
		}
		for _, name := range []string{"history.undo", "base.bin", "oracle.json"} {
			data, err := os.ReadFile(filepath.Join(from, d.Name(), name))
			if err != nil {
				return err
			}
			if err := os.WriteFile(filepath.Join(to, d.Name(), name), data, 0o444); err != nil {
				return err
			}
		}
		copied++
	}
	if copied == 0 {
		return fmt.Errorf("no fixtures in %s", from)
	}
	return nil
}

// installDecoys writes the PATH programs and proves one works, so a decoy
// that could not run cannot pass for one that was never started.
func (w *world) installDecoys() error {
	script := fmt.Sprintf("#!/bin/sh\necho \"$0\" >> '%s'\nexit 1\n", w.marker)
	for _, name := range decoys {
		if err := os.WriteFile(filepath.Join(w.bin, name), []byte(script), 0o755); err != nil {
			return err
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), commandTimeout)
	defer cancel()
	cmd, err := startRetrying(func() *exec.Cmd {
		cmd := exec.CommandContext(ctx, filepath.Join(w.bin, "nvim"))
		cmd.Env = w.env("dumb")
		return cmd
	})
	if err != nil {
		return fmt.Errorf("start a decoy program: %w", err)
	}
	_ = cmd.Wait() // A decoy always fails; what matters is the marker.
	if _, err := os.Stat(w.marker); err != nil {
		return fmt.Errorf("the decoy programs cannot run, so they could not detect a started program: %w", err)
	}
	return os.Remove(w.marker)
}

// env is the whole environment of every run: decoys on PATH, a private
// HOME and XDG directories, and the C locale.
func (w *world) env(term string) []string {
	return []string{
		"PATH=" + w.bin,
		"HOME=" + w.home,
		"XDG_CONFIG_HOME=" + filepath.Join(w.home, ".config"),
		"XDG_DATA_HOME=" + filepath.Join(w.home, ".local", "share"),
		"XDG_STATE_HOME=" + filepath.Join(w.home, ".local", "state"),
		"XDG_CACHE_HOME=" + filepath.Join(w.home, ".cache"),
		"LC_ALL=C",
		"TERM=" + term,
	}
}

// outcome is one finished run.
type outcome struct {
	status         int
	stdout, stderr string
}

// runOptions change how a run connects its standard streams.
type runOptions struct {
	dir    string   // working directory; root when empty
	stdout *os.File // instead of a captured pipe, such as /dev/full
	stderr *os.File // likewise
	env    []string // added to the private environment
}

func (w *world) run(parent context.Context, o runOptions, args ...string) (outcome, error) {
	ctx, cancel := context.WithTimeout(parent, commandTimeout)
	defer cancel()

	var stdout, stderr capped
	cmd, err := startRetrying(func() *exec.Cmd {
		cmd := exec.CommandContext(ctx, w.binary, args...)
		cmd.Dir = o.dir
		if cmd.Dir == "" {
			cmd.Dir = w.root
		}
		cmd.Env = append(w.env("dumb"), o.env...)
		cmd.WaitDelay = waitDelay
		// The child leads a process group of its own, so the run can end
		// every process it started, not only the child.
		cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
		cmd.Stdin = nil
		cmd.Stdout, cmd.Stderr = &stdout, &stderr
		if o.stdout != nil {
			cmd.Stdout = o.stdout
		}
		if o.stderr != nil {
			cmd.Stderr = o.stderr
		}
		return cmd
	})
	if err == nil {
		err = cmd.Wait()
		// The executable starts no processes, so anything left in the group
		// belongs to a broken one, such as a descendant WaitDelay stopped
		// waiting for. The kernel reuses the group's ID only after its PID
		// counter wraps, so the signal reaches no other process. An empty
		// group fails it with ESRCH, the usual case.
		_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	}
	var exit *exec.ExitError
	switch {
	case parent.Err() != nil:
		return outcome{}, fmt.Errorf("xunhen %s: %w", strings.Join(args, " "), parent.Err())
	case ctx.Err() != nil:
		return outcome{}, fmt.Errorf("xunhen %s did not finish within %v", strings.Join(args, " "), commandTimeout)
	case err != nil && !errors.As(err, &exit):
		return outcome{}, fmt.Errorf("run xunhen %s: %w", strings.Join(args, " "), err)
	case stdout.overflow || stderr.overflow:
		return outcome{}, fmt.Errorf("xunhen %s printed more than %d bytes", strings.Join(args, " "), maxOutput)
	}
	return outcome{status: cmd.ProcessState.ExitCode(), stdout: stdout.String(), stderr: stderr.String()}, nil
}

// capped keeps at most maxOutput bytes and records whether more arrived.
// The buffer is a named field: an embedded bytes.Buffer would give capped
// its ReadFrom, which io.Copy calls instead of Write, and the child's
// output would bypass the limit.
type capped struct {
	buf      bytes.Buffer
	overflow bool
}

func (c *capped) Write(p []byte) (int, error) {
	if room := maxOutput - c.buf.Len(); len(p) > room {
		c.overflow = true
		c.buf.Write(p[:max(room, 0)])
		return len(p), nil
	}
	return c.buf.Write(p)
}

func (c *capped) String() string { return c.buf.String() }

// fingerprint records every entry under root: kind, mode, size, time, and
// the content hash of files. Any write, rename, creation, or removal
// changes it.
func fingerprint(root string) (map[string]string, error) {
	prints := map[string]string{}
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		entry := fmt.Sprintf("%v %d %d", info.Mode(), info.Size(), info.ModTime().UnixNano())
		if info.Mode().IsRegular() {
			data, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			entry += fmt.Sprintf(" %x", sha256.Sum256(data))
		}
		prints[path] = entry
		return nil
	})
	return prints, err
}

func sameFingerprint(before, after map[string]string) error {
	var changed []string
	for path, print := range before {
		if after[path] != print {
			changed = append(changed, path)
		}
	}
	for path := range after {
		if _, ok := before[path]; !ok {
			changed = append(changed, path+" (new)")
		}
	}
	if len(changed) != 0 {
		slices.Sort(changed)
		return fmt.Errorf("changed during verification: %s", strings.Join(changed, ", "))
	}
	return nil
}

// readAll reads a small regular file: one the verifier wrote or copied, or
// the go.sum and SHA256SUMS.txt it was given.
func readAll(path string) ([]byte, error) {
	f, _, err := openRegular(path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }()
	return io.ReadAll(io.LimitReader(f, maxOutput))
}

// maxStartAttempts bounds how often startRetrying tries one command.
const maxStartAttempts = 20

// startRetrying starts the command newCmd builds. Starting a file this
// process wrote moments ago, such as a decoy or a test's wrapper script, can
// fail with ETXTBSY: a child that another goroutine forks at that moment
// briefly holds the file open for writing (golang.org/issue/22315). Only
// that failure is retried, a bounded number of times, each with a new Cmd,
// since a Cmd cannot be started twice.
func startRetrying(newCmd func() *exec.Cmd) (*exec.Cmd, error) {
	for attempt := 1; ; attempt++ {
		cmd := newCmd()
		err := cmd.Start()
		if err == nil || !errors.Is(err, syscall.ETXTBSY) || attempt == maxStartAttempts {
			return cmd, err
		}
		time.Sleep(10 * time.Millisecond)
	}
}
