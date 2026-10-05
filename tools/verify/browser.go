package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"syscall"
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
// help, and leave by every documented way out with the terminal's settings
// restored and the alternate screen left. Raw export to the same terminal
// must be refused.
func (w *world) checkBrowser(ctx context.Context) (string, error) {
	fixture := filepath.Join(w.corpus, "abandoned-branch")
	undo := filepath.Join(fixture, "history.undo")
	base := filepath.Join(fixture, "base.bin")
	browse := []string{"browse", "--undo", undo, "--base", base}

	// The reload session changes its history, so it reads copies kept
	// outside the inputs that must stay unchanged.
	reloaded := filepath.Join(w.scratch, "reloaded.undo")
	data, err := readAll(undo)
	if err != nil {
		return "", err
	}
	if err := os.WriteFile(reloaded, data, 0o644); err != nil {
		return "", err
	}

	sessions := []browserSession{
		{
			name: "navigate, compare, and quit",
			args: browse,
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
			name: "go to nodes, pin, compare, and export",
			args: browse,
			keys: []step{
				{wait: "chosen()"},
				{send: "g", wait: "Go to node:"},
				{send: "2\r", wait: "experiment()"},
				{send: " ", wait: "Comparisons now start from node 2."},
				{send: "g3\r", wait: "chosen()"},
				{send: "d", wait: "+func chosen"},
				{send: "g99\r", wait: "No node 99 in this history."},
				// Pinning while comparing compares again from the new pin.
				{send: " ", wait: "identical states"},
				{send: "e", wait: "Save node 3 to a file"},
				{send: "\x1b", wait: "identical states"},
				{send: "q"},
			},
		},
		{
			name: "a failed reload keeps the earlier load",
			args: []string{"browse", "--undo", reloaded, "--base", base},
			keys: []step{
				{wait: "chosen()"},
				{do: func() error { return os.WriteFile(reloaded, []byte("not an undo file\n"), 0o644) }},
				// The status row may already read "Reloading", and the
				// renderer redraws only the cells that change.
				{send: "r", wait: "failed; still showing the earlier load."},
				{send: "j", wait: "experiment()"},
				{send: "q"},
			},
		},
		{
			name: "no color",
			args: browse,
			env:  []string{"NO_COLOR=1"},
			keys: []step{{wait: "chosen()"}, {send: "j", wait: "experiment()"}, {send: "d", wait: "+func experiment"}, {send: "q"}},
			output: func(raw string) error {
				if sgr := colorSGR(raw); sgr != "" {
					return fmt.Errorf("NO_COLOR output still sets color with %q", sgr)
				}
				return nil
			},
		},
		{
			name:   "interrupt",
			args:   browse,
			keys:   []step{{wait: "chosen()"}, {send: "\x03"}},
			status: 130,
		},
		{
			name:   "SIGTERM",
			args:   browse,
			keys:   []step{{wait: "chosen()"}, {signal: syscall.SIGTERM}},
			status: 143,
		},
		{
			name:   "SIGHUP, as from a dropped connection",
			args:   browse,
			keys:   []step{{wait: "chosen()"}, {signal: syscall.SIGHUP}},
			status: 129,
		},
		{
			// The browser takes the screen before it loads, so a failed
			// first load gives it back and then prints the diagnostic.
			name:   "a first load that fails",
			args:   []string{"browse", "--undo", filepath.Join(fixture, "absent.undo"), "--base", base},
			status: 1,
			output: func(raw string) error {
				_, after, _ := strings.Cut(raw, leaveAltScreen)
				if !strings.Contains(after, "xunhen: ") || !strings.Contains(after, "no such file") {
					return fmt.Errorf("no diagnostic after the browser left the screen: %q", raw)
				}
				return nil
			},
		},
		{
			name:   "raw export refuses the terminal",
			args:   []string{"show", "--undo", undo, "--base", base, "--node", "2", "--raw", "--final-newline=include"},
			status: 1,
			stderr: "raw output requires redirected stdout",
		},
	}
	for _, s := range sessions {
		if err := w.session(ctx, s); err != nil {
			return "", fmt.Errorf("%s: %w", s.name, err)
		}
	}
	return fmt.Sprintf(" (%d sessions)", len(sessions)), nil
}

// browserSession is one run on a terminal and what it must end with.
type browserSession struct {
	name   string
	args   []string
	env    []string // added to the private environment
	keys   []step
	status int
	stderr string                 // the whole output must be this diagnostic
	output func(raw string) error // a further check of everything drawn
}

// step does one thing, then waits until the screen shows text: it sends
// keys, delivers a signal to the browser, or runs do, such as changing an
// input.
type step struct {
	send   string
	signal syscall.Signal
	do     func() error
	wait   string
}

// colorSGR returns the first select-graphic-rendition sequence in raw that
// sets a foreground or background color, or "" if none does. Bold, dim,
// reverse video, and resets carry no color.
func colorSGR(raw string) string {
	for _, match := range sgrSequence.FindAllStringSubmatch(raw, -1) {
		for _, parameter := range strings.Split(match[1], ";") {
			n, err := strconv.Atoi(parameter)
			if err != nil {
				continue
			}
			if n >= 30 && n <= 38 || n >= 40 && n <= 48 || n >= 90 && n <= 97 || n >= 100 && n <= 107 {
				return match[0]
			}
		}
	}
	return ""
}

var sgrSequence = regexp.MustCompile(`\x1b\[([0-9;:]*)m`)

func (w *world) session(ctx context.Context, s browserSession) error {
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
	newCmd := func() *exec.Cmd {
		cmd := exec.CommandContext(ctx, w.binary, s.args...)
		cmd.Dir = w.root
		cmd.Env = append(w.env("xterm-256color"), s.env...)
		cmd.Stdin, cmd.Stdout, cmd.Stderr = pair.Slave, pair.Slave, pair.Slave
		cmd.SysProcAttr = pty.Attach()
		return cmd
	}

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

	cmd, err := startRetrying(newCmd)
	if err != nil {
		return err
	}
	// A wait looks at the output since the last key, so it cannot pass on
	// what an earlier key drew. Before the first key that is everything
	// since the start: the first screen may arrive before the loop runs.
	mark := 0
	for _, k := range s.keys {
		var err error
		switch {
		case k.send != "":
			mark = t.mark()
			_, err = pair.Master.WriteString(k.send)
		case k.signal != 0:
			mark = t.mark()
			err = cmd.Process.Signal(k.signal)
		case k.do != nil:
			err = k.do()
		}
		if err == nil && k.wait != "" {
			err = t.waitFor(ctx, mark, k.wait)
		}
		if err != nil {
			_ = cmd.Process.Kill()
			_ = cmd.Wait()
			return err
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
	if got := cmd.ProcessState.ExitCode(); got != s.status {
		return fmt.Errorf("exit status %d, want %d; screen:\n%s", got, s.status, t.text())
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
	if s.stderr != "" {
		if strings.Contains(output, "\x1b") || !strings.Contains(output, s.stderr) {
			return fmt.Errorf("terminal output %q, want only a diagnostic about %q", output, s.stderr)
		}
		return nil
	}
	enter, leave := strings.Count(output, enterAltScreen), strings.Count(output, leaveAltScreen)
	if enter == 0 || enter != leave {
		return fmt.Errorf("the alternate screen was entered %d times and left %d times", enter, leave)
	}
	if s.output != nil {
		return s.output(output)
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
