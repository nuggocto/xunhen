package main

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"

	"github.com/nuggocto/xunhen/internal/pty"
)

// Terminal sequences the browser must balance: the alternate screen it
// enters and leaves.
const (
	enterAltScreen = "\x1b[?1049h"
	leaveAltScreen = "\x1b[?1049l"
)

// TestBrowserUnderATerminal runs the built command on a pseudo-terminal, as a
// user's shell would, and checks every way out: the terminal settings must
// be as they were, the alternate screen left, and the exit status right.
// Screen content is checked only through words the renderer writes whole;
// layout and styling are left to the model tests.
func TestBrowserUnderATerminal(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	binary := buildExecutable(t, dir)
	base, err := os.ReadFile("../../testdata/undo/abandoned-branch/base.bin")
	if err != nil {
		t.Fatal(err)
	}
	for name, data := range map[string][]byte{
		"history.undo": undoFixture(t, "abandoned-branch"),
		"retry.go":     base,
		"other.go":     []byte("package other\n"),
	} {
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o444); err != nil {
			t.Fatal(err)
		}
	}

	browse := []string{"browse", "--undo", "history.undo", "--base", "retry.go"}
	tests := []struct {
		name   string
		args   []string
		run    func(t *testing.T, term *terminal, process *os.Process)
		status int
		stderr string // expected after the browser leaves the screen
		// dumb runs with TERM=dumb, where the browser must refuse to start
		// and draw nothing.
		dumb bool
		// env adds variables to the command's environment.
		env []string
	}{
		{name: "quit", run: func(t *testing.T, term *terminal, _ *os.Process) {
			term.write(t, "q")
		}},
		{name: "ctrl+c", status: 130, run: func(t *testing.T, term *terminal, _ *os.Process) {
			term.write(t, "\x03")
		}},
		{name: "SIGINT", status: 130, run: sendSignal(syscall.SIGINT)},
		{name: "SIGTERM", status: 143, run: sendSignal(syscall.SIGTERM)},
		{name: "SIGHUP", status: 129, run: sendSignal(syscall.SIGHUP)},
		{name: "navigate after a resize", run: func(t *testing.T, term *terminal, _ *os.Process) {
			mark := term.mark()
			term.resize(t, 90, 12)
			term.waitText(t, mark, "chosen()")

			mark = term.mark()
			term.write(t, "j")
			term.waitText(t, mark, "experiment()")
			term.write(t, "q")
		}},
		{name: "suspend and resume", run: func(t *testing.T, term *terminal, process *os.Process) {
			// The test's process group has no job control, so the kernel
			// discards the stop signal and the browser waits in its
			// suspended state for SIGCONT, as it would under a shell.
			mark := term.mark()
			term.write(t, "\x1a")
			term.waitRaw(t, mark, leaveAltScreen)

			// Restoring the settings follows the last write, so no output
			// announces it; poll the settings instead.
			poll(t, "cooked mode while suspended", func() bool { return term.canonical(t) })

			// Without a real stop, SIGCONT can arrive before the browser
			// waits for it, so repeat it, as a user might repeat fg.
			mark = term.mark()
			poll(t, "the browser to resume", func() bool {
				if err := process.Signal(syscall.SIGCONT); err != nil {
					t.Fatal(err)
				}
				return strings.Contains(term.raw(mark), enterAltScreen)
			})
			term.write(t, "q")
		}},
		{name: "a dumb terminal", dumb: true, status: 1, stderr: "TERM is dumb"},
		// Bubble Tea would log every drawn frame, recovered text included,
		// to this file, and panic logs to the working directory.
		{
			name: "debugging variables write nothing",
			env:  []string{"TEA_TRACE=" + filepath.Join(dir, "trace.log"), "TEA_DEBUG=true"},
			run: func(t *testing.T, term *terminal, _ *os.Process) {
				mark := term.mark()
				term.write(t, "j")
				term.waitText(t, mark, "experiment()")
				term.write(t, "q")
			},
		},
		{
			name:   "a base that does not match",
			args:   []string{"browse", "--undo", "history.undo", "--base", "other.go"},
			status: 1, stderr: "xunhen: ",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			term := openTerminal(t)
			before := term.settings(t)

			ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
			defer cancel()

			args := tt.args
			if args == nil {
				args = browse
			}
			command := exec.CommandContext(ctx, binary, args...)
			command.Dir = dir
			command.Env = []string{"PATH=", "HOME=" + dir, "TERM=xterm-256color", "LC_ALL=C"}
			if tt.dumb {
				command.Env[2] = "TERM=dumb"
			}
			command.Env = append(command.Env, tt.env...)
			command.Stdin, command.Stdout, command.Stderr = term.slave, term.slave, term.slave
			command.SysProcAttr = pty.Attach()
			if err := command.Start(); err != nil {
				t.Fatal(err)
			}

			if tt.run != nil {
				term.waitText(t, 0, "chosen()")
				tt.run(t, term, command.Process)
			}

			err := command.Wait()
			var exit *exec.ExitError
			if err != nil && !errors.As(err, &exit) {
				t.Fatal(err)
			}
			if status := command.ProcessState.ExitCode(); status != tt.status {
				t.Fatalf("exit status %d, want %d; output:\n%q", status, tt.status, term.text(0))
			}

			if after := term.settings(t); after != before {
				t.Fatalf("terminal settings not restored:\nbefore %+v\nafter  %+v", before, after)
			}
			output := term.drain()
			if _, err := os.Stat(filepath.Join(dir, "trace.log")); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("the browser wrote a trace file: %v", err)
			}
			if tt.dumb {
				if strings.Contains(output, "\x1b") || !strings.Contains(output, tt.stderr) {
					t.Fatalf("output on a dumb terminal: %q", output)
				}
				return
			}
			if strings.Count(output, enterAltScreen) != strings.Count(output, leaveAltScreen) || !strings.Contains(output, enterAltScreen) {
				t.Fatalf("the alternate screen was entered %d times and left %d times",
					strings.Count(output, enterAltScreen), strings.Count(output, leaveAltScreen))
			}
			if tt.stderr != "" {
				last := strings.LastIndex(output, leaveAltScreen)
				if !strings.Contains(output[last:], tt.stderr) {
					t.Fatalf("no diagnostic after the browser closed: %q", output[last:])
				}
			}
		})
	}
}

func sendSignal(s syscall.Signal) func(*testing.T, *terminal, *os.Process) {
	return func(t *testing.T, _ *terminal, process *os.Process) {
		if err := process.Signal(s); err != nil {
			t.Fatal(err)
		}
	}
}

// terminal is a pseudo-terminal pair. The test keeps the slave side open
// so the terminal's settings survive the command and can be compared.
type terminal struct {
	pair          *pty.Pair
	master, slave *os.File
	closeSlave    func()
	finished      chan struct{} // the reader has read everything

	mu       sync.Mutex
	output   []byte
	overflow bool          // output passed maxTranscript and was cut
	changed  chan struct{} // capacity one: output grew
}

// maxTranscript bounds what one test keeps of a terminal's output. A
// browser run in these tests writes well under a megabyte.
const maxTranscript = 16 << 20

func openTerminal(t *testing.T) *terminal {
	t.Helper()

	// A missing pseudo-terminal fails rather than skips: these tests are
	// the only check of the built browser on a terminal.
	pair, err := pty.Open(100, 24)
	if err != nil {
		t.Fatal(err)
	}
	master, slave := pair.Master, pair.Slave

	term := &terminal{
		pair:       pair,
		master:     master,
		slave:      slave,
		closeSlave: sync.OnceFunc(func() { _ = slave.Close() }),
		finished:   make(chan struct{}),
		changed:    make(chan struct{}, 1),
	}

	// The reader ends with EIO once every slave descriptor is closed.
	go func() {
		defer close(term.finished)
		buffer := make([]byte, 4096)
		for {
			n, err := master.Read(buffer)
			if n > 0 {
				term.mu.Lock()
				if len(term.output)+n <= maxTranscript {
					term.output = append(term.output, buffer[:n]...)
				} else {
					term.overflow = true
				}
				term.mu.Unlock()
				select {
				case term.changed <- struct{}{}:
				default:
				}
			}
			if err != nil {
				return
			}
		}
	}()
	t.Cleanup(func() {
		term.closeSlave()
		// The reader ends when the last slave descriptor closes. A child
		// that outlived its test would hold one open; bound the wait.
		select {
		case <-term.finished:
		case <-time.After(20 * time.Second):
			t.Error("a process still held the terminal after the test")
		}
		_ = master.Close()
		term.mu.Lock()
		defer term.mu.Unlock()
		if term.overflow {
			t.Errorf("the terminal transcript passed %d bytes", maxTranscript)
		}
	})

	return term
}

// drain closes the test's slave side once the command has exited and returns
// everything the command wrote.
func (term *terminal) drain() string {
	term.closeSlave()
	<-term.finished
	return term.raw(0)
}

// poll waits for a condition that no output announces, checking it every
// few milliseconds. The deadline only turns a hang into a failure.
func poll(t *testing.T, what string, done func() bool) {
	t.Helper()

	tick := time.NewTicker(5 * time.Millisecond)
	defer tick.Stop()
	deadline := time.After(20 * time.Second)
	for !done() {
		select {
		case <-tick.C:
		case <-deadline:
			t.Fatalf("timed out waiting for %s", what)
		}
	}
}

func (term *terminal) resize(t *testing.T, columns, rows uint16) {
	t.Helper()
	if err := term.pair.Resize(columns, rows); err != nil {
		t.Fatal(err)
	}
}

// settings reads the terminal's line settings, which a program in raw mode
// changes and must put back. The test holds the slave side open, so they
// outlive the command.
func (term *terminal) settings(t *testing.T) syscall.Termios {
	t.Helper()
	attrs, err := term.pair.Settings()
	if err != nil {
		t.Fatal(err)
	}
	return attrs
}

func (term *terminal) canonical(t *testing.T) bool {
	return term.settings(t).Lflag&syscall.ICANON != 0
}

func (term *terminal) write(t *testing.T, keys string) {
	t.Helper()
	if _, err := term.master.WriteString(keys); err != nil {
		t.Fatal(err)
	}
}

// mark returns the current end of the output, so a later wait can look only
// at what came after it.
func (term *terminal) mark() int {
	term.mu.Lock()
	defer term.mu.Unlock()
	return len(term.output)
}

func (term *terminal) raw(from int) string {
	term.mu.Lock()
	defer term.mu.Unlock()
	return string(term.output[from:])
}

// text is the output after from with terminal sequences removed.
func (term *terminal) text(from int) string {
	return ansi.Strip(term.raw(from))
}

func (term *terminal) waitRaw(t *testing.T, from int, want string) {
	t.Helper()
	term.wait(t, want, func() bool { return strings.Contains(term.raw(from), want) })
}

func (term *terminal) waitText(t *testing.T, from int, want string) {
	t.Helper()
	term.wait(t, want, func() bool { return strings.Contains(term.text(from), want) })
}

// wait blocks until done reports true, rechecking whenever output arrives.
// The deadline only turns a hang into a failure.
func (term *terminal) wait(t *testing.T, what string, done func() bool) {
	t.Helper()

	deadline := time.After(20 * time.Second)
	for !done() {
		select {
		case <-term.changed:
		case <-deadline:
			t.Fatalf("timed out waiting for %q; output:\n%q", what, term.raw(0))
		}
	}
}
