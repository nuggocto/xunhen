package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"
	"unsafe"

	"github.com/nuggocto/xunhen/internal/synth"
)

// checkPermissions denies the executable an undo file, a base, and an undo
// directory, and expects a diagnostic for each rather than a partial
// result. A user who bypasses file permissions, such as root, would read
// the files anyway and prove nothing, so the check fails instead.
func (w *world) checkPermissions(ctx context.Context) (string, error) {
	dir := filepath.Join(w.scratch, "denied")
	locked := filepath.Join(dir, "locked")
	if err := os.MkdirAll(locked, 0o755); err != nil {
		return "", err
	}
	fixture := filepath.Join(w.corpus, "abandoned-branch")
	undo := filepath.Join(fixture, "history.undo")
	base := filepath.Join(fixture, "base.bin")
	deniedUndo := filepath.Join(dir, "history.undo")
	deniedBase := filepath.Join(dir, "base.go")
	for from, to := range map[string]string{undo: deniedUndo, base: deniedBase} {
		data, err := readAll(from)
		if err != nil {
			return "", err
		}
		if err := os.WriteFile(to, data, 0o000); err != nil {
			return "", err
		}
	}
	if err := os.Chmod(locked, 0o000); err != nil {
		return "", err
	}
	if f, err := os.Open(deniedUndo); err == nil {
		_ = f.Close()
		return "", errors.New("this user can read a file without read permission, so denials cannot be checked; run the verifier as an ordinary user")
	}

	tests := []struct {
		name   string
		dir    string
		args   []string
		stderr string
	}{
		{name: "unreadable history", args: []string{"inspect", "--undo", deniedUndo}, stderr: "permission denied"},
		{name: "unreadable base", args: []string{"show", "--undo", undo, "--base", deniedBase, "--node", "2"}, stderr: "permission denied"},
		{
			name:   "unreadable undo directory",
			dir:    filepath.Join(w.root, "project"),
			args:   []string{"show", "--undo-dir", locked, "--undo-dir", "../undo", "--node", "2", "retry.go"},
			stderr: "could not examine every undo directory",
		},
	}
	for _, tt := range tests {
		o, err := w.run(ctx, runOptions{dir: tt.dir}, tt.args...)
		if err != nil {
			return "", fmt.Errorf("%s: %w", tt.name, err)
		}
		if o.status != 1 || o.stdout != "" || !strings.Contains(o.stderr, tt.stderr) {
			return "", fmt.Errorf("%s: status %d, stdout %q, stderr %q; want status 1 and a diagnostic about %q",
				tt.name, o.status, o.stdout, o.stderr, tt.stderr)
		}
	}
	return fmt.Sprintf(" (%d cases)", len(tests)), nil
}

// checkPaths reads inputs whose names hold spaces, a leading dash,
// non-ASCII text, and a control byte, explicitly and through discovery. A
// missing file named with an escape sequence must be reported with the
// escape shown, not sent to the terminal.
func (w *world) checkPaths(ctx context.Context) (string, error) {
	dir := filepath.Join(w.scratch, "paths")
	project := filepath.Join(dir, "project")
	undoDir := filepath.Join(dir, "undo")
	for _, d := range []string{project, undoDir} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			return "", err
		}
	}
	fixture := filepath.Join(w.corpus, "abandoned-branch")
	undo, err := readAll(filepath.Join(fixture, "history.undo"))
	if err != nil {
		return "", err
	}
	base, err := readAll(filepath.Join(fixture, "base.bin"))
	if err != nil {
		return "", err
	}
	states, err := w.states("abandoned-branch")
	if err != nil {
		return "", err
	}
	var experiment string
	for _, s := range states {
		if s.seq == 2 {
			experiment = strings.Join(s.lines, "\n") + "\n"
		}
	}

	names := []string{"my history.undo", "-dash.undo", "履歴.undo", "control\x01.undo"}
	baseName := "-my base ü.go"
	if err := os.WriteFile(filepath.Join(dir, baseName), base, 0o444); err != nil {
		return "", err
	}
	for _, name := range names {
		if err := os.WriteFile(filepath.Join(dir, name), undo, 0o444); err != nil {
			return "", err
		}
	}
	// The source's own name is unusual too, and Neovim names its history
	// after the source's full physical path.
	source := filepath.Join(project, baseName)
	if err := os.WriteFile(source, base, 0o444); err != nil {
		return "", err
	}
	if err := os.WriteFile(filepath.Join(undoDir, strings.ReplaceAll(source, "/", "%")), undo, 0o444); err != nil {
		return "", err
	}

	for _, name := range names {
		o, err := w.run(ctx, runOptions{dir: dir}, "show", "--undo", name, "--base", baseName, "--node", "2", "--raw", "--final-newline=include")
		if err != nil {
			return "", err
		}
		if o.status != 0 || o.stdout != experiment || o.stderr != "" {
			return "", fmt.Errorf("history %q: status %d, stdout %q, stderr %q", name, o.status, o.stdout, o.stderr)
		}
	}
	o, err := w.run(ctx, runOptions{dir: project}, "show", "--undo-dir", "../undo", "--node", "2", "--raw", "--final-newline=include", "--", baseName)
	if err != nil {
		return "", err
	}
	if o.status != 0 || o.stdout != experiment || o.stderr != "" {
		return "", fmt.Errorf("discovery of %q: status %d, stdout %q, stderr %q", baseName, o.status, o.stdout, o.stderr)
	}

	o, err = w.run(ctx, runOptions{dir: dir}, "inspect", "--undo", "absent\x1b[2J.undo")
	if err != nil {
		return "", err
	}
	if o.status != 1 || strings.ContainsRune(o.stderr, 0x1b) || !strings.Contains(o.stderr, `absent\x1b[2J.undo`) {
		return "", fmt.Errorf("a missing file named with an escape: status %d, stderr %q", o.status, o.stderr)
	}
	return fmt.Sprintf(" (%d histories, discovery, an escape in a name)", len(names)), nil
}

// fionread is FIONREAD on Linux: how many bytes a pipe holds unread.
const fionread = 0x541b

// checkInterrupt starts an export to a pipe nobody reads, waits until the
// pipe is full and the export is blocked writing to it, and interrupts it.
// Outside the browser, an interrupt keeps its default disposition, so the
// process must end by SIGINT, as a shell expects.
func (w *world) checkInterrupt(ctx context.Context) (string, error) {
	dir := filepath.Join(w.scratch, "interrupt")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	// A state of several megabytes is far more than a pipe holds.
	lines := make([]string, 100_000)
	for i := range lines {
		lines[i] = fmt.Sprintf("line %06d of a state too large for one pipe buffer", i)
	}
	data, err := synth.Chain([][]string{{"start"}, lines}).Bytes()
	if err != nil {
		return "", err
	}
	undo, base := filepath.Join(dir, "history.undo"), filepath.Join(dir, "base.txt")
	if err := os.WriteFile(undo, data, 0o444); err != nil {
		return "", err
	}
	if err := os.WriteFile(base, []byte(strings.Join(lines, "\n")+"\n"), 0o444); err != nil {
		return "", err
	}

	reader, writer, err := os.Pipe()
	if err != nil {
		return "", err
	}
	defer func() { _ = reader.Close() }()

	ctx, cancel := context.WithTimeout(ctx, commandTimeout)
	defer cancel()
	cmd, err := startRetrying(func() *exec.Cmd {
		cmd := exec.CommandContext(ctx, w.binary, "show", "--undo", undo, "--base", base, "--node", "1")
		cmd.Dir = dir
		cmd.Env = w.env("dumb")
		cmd.Stdout = writer
		return cmd
	})
	_ = writer.Close()
	if err != nil {
		return "", err
	}

	// Once the pipe holds as much as it can, the next write blocks.
	size, err := pipeSize(reader)
	for err == nil {
		var held int
		held, err = unread(reader)
		if err != nil || held >= size {
			break
		}
		select {
		case <-ctx.Done():
			err = ctx.Err()
		case <-time.After(10 * time.Millisecond):
		}
	}
	if err == nil {
		err = cmd.Process.Signal(syscall.SIGINT)
	}
	if err != nil {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		return "", err
	}
	_ = cmd.Wait()
	status, ok := cmd.ProcessState.Sys().(syscall.WaitStatus)
	if !ok || !status.Signaled() || status.Signal() != syscall.SIGINT {
		return "", fmt.Errorf("an interrupted export ended with %v, want termination by SIGINT", cmd.ProcessState)
	}
	return "", nil
}

// pipeSize returns a pipe's capacity.
func pipeSize(f *os.File) (int, error) {
	n, _, errno := syscall.Syscall(syscall.SYS_FCNTL, f.Fd(), syscall.F_GETPIPE_SZ, 0)
	if errno != 0 {
		return 0, errno
	}
	return int(n), nil
}

// unread returns how many bytes a pipe holds.
func unread(f *os.File) (int, error) {
	var n int32
	if _, _, errno := syscall.Syscall(syscall.SYS_IOCTL, f.Fd(), fionread, uintptr(unsafe.Pointer(&n))); errno != 0 {
		return 0, errno
	}
	return int(n), nil
}
