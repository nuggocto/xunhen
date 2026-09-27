package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/charmbracelet/x/ansi"

	"github.com/nuggocto/xunhen/internal/pty"
)

// Sequences the browser must balance: the alternate screen it enters and
// leaves.
const (
	enterAltScreen = "\x1b[?1049h"
	leaveAltScreen = "\x1b[?1049l"
)

// waitTimeout bounds each wait for the browser to draw something.
const waitTimeout = 20 * time.Second

// checkBrowser runs the browser on a pseudo-terminal as a user's shell
// would: it must draw the history, follow keys, compare two states, show
// help, and quit with the terminal's settings restored and the alternate
// screen left. Raw export to the same terminal must be refused.
func (w *world) checkBrowser(ctx context.Context) (string, error) {
	fixture := filepath.Join(w.corpus, "abandoned-branch")
	undo := filepath.Join(fixture, "history.undo")
	base := filepath.Join(fixture, "base.bin")

	sessions := []struct {
		name   string
		args   []string
		keys   []step
		status int
		stderr string
	}{
		{
			name: "navigate, compare, and quit",
			args: []string{"browse", "--undo", undo, "--base", base},
			keys: []step{
				{wait: "chosen()"},
				{send: "j", wait: "experiment()"},
				{send: "d", wait: "+func experiment"},
				{send: "?", wait: "select the previous or next row"},
				{send: "\x1b", wait: "+func experiment"},
				{send: "q"},
			},
		},
		{
			name:   "interrupt",
			args:   []string{"browse", "--undo", undo, "--base", base},
			keys:   []step{{wait: "chosen()"}, {send: "\x03"}},
			status: 130,
		},
		{
			name:   "raw export refuses the terminal",
			args:   []string{"show", "--undo", undo, "--base", base, "--node", "2", "--raw", "--final-newline=include"},
			status: 1,
			stderr: "raw output requires redirected stdout",
		},
	}
	for _, s := range sessions {
		if err := w.session(ctx, s.args, s.keys, s.status, s.stderr); err != nil {
			return "", fmt.Errorf("%s: %w", s.name, err)
		}
	}
	return "", nil
}

// step sends keys, then waits until the screen shows text.
type step struct {
	send, wait string
}

func (w *world) session(ctx context.Context, args []string, steps []step, status int, stderr string) error {
	// A missing pseudo-terminal fails: this is the only check of the
	// browser on a terminal.
	pair, err := pty.Open(100, 24)
	if err != nil {
		return err
	}
	defer func() { _ = pair.Master.Close() }()
	before, err := pair.Settings()
	if err != nil {
		return err
	}

	ctx, cancel := context.WithTimeout(ctx, commandTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, w.binary, args...)
	cmd.Dir = w.root
	cmd.Env = w.env("xterm-256color")
	cmd.Stdin, cmd.Stdout, cmd.Stderr = pair.Slave, pair.Slave, pair.Slave
	cmd.SysProcAttr = pty.Attach()

	t := &transcript{changed: make(chan struct{}, 1)}
	done := make(chan struct{})
	go func() {
		defer close(done)
		t.read(pair.Master)
	}()
	closeSlave := sync.OnceFunc(func() { _ = pair.Slave.Close() })
	defer func() {
		closeSlave()
		_ = pair.Master.Close()
		<-done
	}()

	if err := cmd.Start(); err != nil {
		return err
	}
	// A wait looks at the output since the last key, so it cannot pass on
	// what an earlier key drew. Before the first key that is everything
	// since the start: the first screen may arrive before the loop runs.
	mark := 0
	for _, s := range steps {
		if s.send != "" {
			mark = t.mark()
			if _, err := pair.Master.WriteString(s.send); err != nil {
				return err
			}
		}
		if s.wait != "" {
			if err := t.waitFor(ctx, mark, s.wait); err != nil {
				_ = cmd.Process.Kill()
				_ = cmd.Wait()
				return err
			}
		}
	}

	err = cmd.Wait()
	var exit *exec.ExitError
	if err != nil && !errors.As(err, &exit) {
		return err
	}
	if ctx.Err() != nil {
		return fmt.Errorf("did not exit within %v", commandTimeout)
	}
	if got := cmd.ProcessState.ExitCode(); got != status {
		return fmt.Errorf("exit status %d, want %d; screen:\n%s", got, status, t.text())
	}
	after, err := pair.Settings()
	if err != nil {
		return err
	}
	if after != before {
		return errors.New("the terminal settings were not restored")
	}

	// Every descriptor of the terminal's slave side must close before the
	// reader sees the end of the output.
	closeSlave()
	select {
	case <-done:
	case <-time.After(waitTimeout):
		return errors.New("a process still held the terminal after exit")
	}
	output := t.raw()
	if t.overflow {
		return fmt.Errorf("the terminal received more than %d bytes", maxOutput)
	}
	if stderr != "" {
		if strings.Contains(output, "\x1b") || !strings.Contains(output, stderr) {
			return fmt.Errorf("terminal output %q, want only a diagnostic about %q", output, stderr)
		}
		return nil
	}
	enter, leave := strings.Count(output, enterAltScreen), strings.Count(output, leaveAltScreen)
	if enter == 0 || enter != leave {
		return fmt.Errorf("the alternate screen was entered %d times and left %d times", enter, leave)
	}
	return nil
}

// transcript collects what the executable writes to the terminal.
type transcript struct {
	mu       sync.Mutex
	output   []byte
	overflow bool
	changed  chan struct{}
}

// read copies the master side until every slave descriptor closes.
func (t *transcript) read(master *os.File) {
	buffer := make([]byte, 4096)
	for {
		n, err := master.Read(buffer)
		if n > 0 {
			t.mu.Lock()
			if len(t.output)+n <= maxOutput {
				t.output = append(t.output, buffer[:n]...)
			} else {
				t.overflow = true
			}
			t.mu.Unlock()
			select {
			case t.changed <- struct{}{}:
			default:
			}
		}
		// Reading fails with EIO once every slave descriptor is closed.
		if err != nil {
			return
		}
	}
}

func (t *transcript) since(mark int) string {
	t.mu.Lock()
	defer t.mu.Unlock()
	return ansi.Strip(string(t.output[mark:]))
}

func (t *transcript) raw() string {
	t.mu.Lock()
	defer t.mu.Unlock()
	return string(t.output)
}

func (t *transcript) text() string {
	return ansi.Strip(t.raw())
}

// mark returns the current end of the output, so a wait can look only at
// what the browser drew after a key.
func (t *transcript) mark() int {
	t.mu.Lock()
	defer t.mu.Unlock()
	return len(t.output)
}

// waitFor blocks until the output after mark, without terminal sequences,
// holds text. The renderer rewrites only cells that change, so text must be
// something a key makes new, not something already on screen.
func (t *transcript) waitFor(ctx context.Context, mark int, text string) error {
	deadline := time.NewTimer(waitTimeout)
	defer deadline.Stop()
	for !strings.Contains(t.since(mark), text) {
		select {
		case <-t.changed:
		case <-deadline.C:
			return fmt.Errorf("timed out waiting for %q; screen:\n%s", text, t.text())
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return nil
}
