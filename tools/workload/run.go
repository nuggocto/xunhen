package main

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/charmbracelet/x/ansi"

	"github.com/nuggocto/xunhen/internal/pty"
)

// sample is one raw measurement. Seconds is wall time from the action to
// the observed result. PeakKiB is the process's peak resident memory when
// the tool can read it: exact for the browser, from /proc while it runs,
// and for other commands from wait4, which on Linux reports at least the
// runner's own peak because the child starts as a vfork of it; FloorKiB
// records that peak.
type sample struct {
	Recipe    string  `json:"recipe"`
	Version   int     `json:"version"`
	Operation string  `json:"operation"`
	Index     int     `json:"sample"`
	Seconds   float64 `json:"seconds"`
	PeakKiB   int64   `json:"peak_kib,omitempty"`
	FloorKiB  int64   `json:"floor_kib,omitempty"`
}

type runner struct {
	bin, dir string
	m        manifest
	samples  int
	out      *json.Encoder
}

func run(args []string) error {
	flags := flag.NewFlagSet("run", flag.ContinueOnError)
	r := &runner{}
	flags.StringVar(&r.bin, "bin", "", "the built command to measure")
	flags.StringVar(&r.dir, "dir", "", "a generated workload directory")
	flags.IntVar(&r.samples, "samples", 5, "samples per operation")
	out := flags.String("out", "", "file to append JSON lines to")
	browser := flags.Bool("browser", true, "also measure the browser on a pseudo-terminal")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if r.bin == "" || r.dir == "" || *out == "" || r.samples < 1 {
		return errors.New("run needs -bin, -dir, -out, and a positive -samples")
	}

	var err error
	if r.m, err = readManifest(r.dir); err != nil {
		return err
	}
	f, err := os.OpenFile(*out, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	r.out = json.NewEncoder(f)

	if err := r.verify(); err != nil {
		return fmt.Errorf("%s: wrong result, nothing measured: %w", r.m.Recipe, err)
	}
	if err := r.commands(); err != nil {
		return err
	}
	if *browser {
		return r.browser()
	}
	return nil
}

func (r *runner) inputs() []string {
	return []string{"--undo", "history.undo", "--base", "base.txt"}
}

// commandTimeout bounds one run of the measured command, as each browser
// session is bounded, so a hang fails the run instead of stalling it.
const commandTimeout = 10 * time.Minute

// verify exports every probed state and compares it with the digest the
// generator computed while building the history.
func (r *runner) verify() error {
	for _, p := range r.m.Probes {
		if err := r.verifyProbe(p); err != nil {
			return err
		}
	}
	return nil
}

func (r *runner) verifyProbe(p probe) error {
	ctx, cancel := context.WithTimeout(context.Background(), commandTimeout)
	defer cancel()
	args := append(append([]string{"show"}, r.inputs()...), "--node", strconv.Itoa(int(p.Node)), "--raw", "--final-newline=include")
	cmd := exec.CommandContext(ctx, r.bin, args...)
	cmd.Dir = r.dir
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	var stderr strings.Builder
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		return err
	}
	h := sha256.New()
	if _, err := io.Copy(h, bufio.NewReaderSize(stdout, 1<<20)); err != nil {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
		return err
	}
	if err := cmd.Wait(); err != nil {
		return fmt.Errorf("node %d: %v: %s", p.Node, err, stderr.String())
	}
	if got := hex.EncodeToString(h.Sum(nil)); got != p.SHA256 {
		return fmt.Errorf("node %d (%s) exported as %s, want %s", p.Node, p.Role, got, p.SHA256)
	}
	return nil
}

// commands times inspect, show, and diff as a script would run them, with
// output discarded.
func (r *runner) commands() error {
	far := r.m.Probes[0].Node // the first probe is always the root
	ops := []struct {
		name string
		args []string
	}{
		{"inspect", []string{"inspect", "--undo", "history.undo"}},
		{"show reference", append(append([]string{"show"}, r.inputs()...), "--node", strconv.Itoa(int(r.m.Compare[0])), "--raw", "--final-newline=include")},
		{"show root", append(append([]string{"show"}, r.inputs()...), "--node", strconv.Itoa(int(far)), "--raw", "--final-newline=include")},
		{"diff", append(append([]string{"diff"}, r.inputs()...), "--from", strconv.Itoa(int(r.m.Compare[0])), "--to", strconv.Itoa(int(r.m.Compare[1])))},
	}

	devNull, err := os.OpenFile(os.DevNull, os.O_WRONLY, 0)
	if err != nil {
		return err
	}
	defer devNull.Close()

	for _, op := range ops {
		for i := range r.samples {
			if err := r.coldCommand(op.name, op.args, i, devNull); err != nil {
				return err
			}
			floor := selfPeak()
			ctx, cancel := context.WithTimeout(context.Background(), commandTimeout)
			cmd := exec.CommandContext(ctx, r.bin, op.args...)
			cmd.Dir, cmd.Stdout = r.dir, devNull
			var stderr strings.Builder
			cmd.Stderr = &stderr
			start := time.Now()
			err := cmd.Run()
			elapsed := time.Since(start)
			cancel()
			if err != nil {
				return fmt.Errorf("%s: %v: %s", op.name, err, stderr.String())
			}
			usage := cmd.ProcessState.SysUsage().(*syscall.Rusage)
			if err := r.record(op.name, i, elapsed, usage.Maxrss, floor); err != nil {
				return err
			}
		}
	}
	return nil
}

// coldCommand runs one command with the inputs evicted from the page cache
// first, so it reads them from the disk. Only inspect and one show need it:
// every command reads its inputs the same way.
func (r *runner) coldCommand(name string, args []string, i int, stdout *os.File) error {
	if name != "inspect" && name != "show root" {
		return nil
	}
	if err := r.evict(); err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), commandTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, r.bin, args...)
	cmd.Dir, cmd.Stdout = r.dir, stdout
	start := time.Now()
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("%s, cold file cache: %w", name, err)
	}
	return r.record(name+", cold file cache", i, time.Since(start), 0, 0)
}

// evict drops the inputs from the page cache with posix_fadvise, which needs
// no privileges for clean pages. The kernel treats it as advice: pages in
// use elsewhere stay.
func (r *runner) evict() error {
	for _, name := range []string{"history.undo", "base.txt"} {
		f, err := os.Open(r.dir + "/" + name)
		if err != nil {
			return err
		}
		const dontNeed = 4 // POSIX_FADV_DONTNEED
		_ = f.Sync()
		_, _, errno := syscall.Syscall6(syscall.SYS_FADVISE64, f.Fd(), 0, 0, dontNeed, 0, 0)
		_ = f.Close()
		if errno != 0 {
			return fmt.Errorf("evict %s: %w", name, errno)
		}
	}
	return nil
}

func (r *runner) record(operation string, index int, elapsed time.Duration, peak, floor int64) error {
	return r.out.Encode(sample{
		Recipe: r.m.Recipe, Version: r.m.Version, Operation: operation, Index: index,
		Seconds: elapsed.Seconds(), PeakKiB: peak, FloorKiB: floor,
	})
}

func selfPeak() int64 {
	var usage syscall.Rusage
	if err := syscall.Getrusage(syscall.RUSAGE_SELF, &usage); err != nil {
		return 0
	}
	return usage.Maxrss
}

// browser runs two scripted sessions per sample on a pseudo-terminal. The
// first measures the time to the first drawn state and the peak memory over
// a comparison and two reloads. The second starts the comparison and quits
// at once, measuring how long exit takes while the worker is busy.
func (r *runner) browser() error {
	for i := range r.samples {
		if err := r.evict(); err != nil {
			return err
		}
		if err := r.firstState(i, "browser first state, cold file cache"); err != nil {
			return err
		}
		if err := r.session(i); err != nil {
			return fmt.Errorf("browser session: %w", err)
		}
		if err := r.quitDuringWork(i); err != nil {
			return fmt.Errorf("browser quit: %w", err)
		}
	}
	return nil
}

// firstState starts the browser and quits once the first state is drawn.
func (r *runner) firstState(i int, operation string) error {
	b, err := r.startBrowser()
	if err != nil {
		return err
	}
	defer b.stop()
	if err := b.waitText(0, "lines", time.Minute); err != nil {
		return err
	}
	if err := r.record(operation, i, time.Since(b.started), 0, 0); err != nil {
		return err
	}
	return b.quit()
}

func (r *runner) session(i int) error {
	b, err := r.startBrowser()
	if err != nil {
		return err
	}
	defer b.stop()

	if err := b.waitText(0, "lines", time.Minute); err != nil {
		return err
	}
	if err := r.record("browser first state", i, time.Since(b.started), 0, 0); err != nil {
		return err
	}

	// Select the comparison's right side and compare it with the reference.
	mark := b.mark()
	b.write(fmt.Sprintf("g%d\r", r.m.Compare[1]))
	b.write("d")
	if err := b.waitText(mark, "hunk", 2*time.Minute); err != nil {
		return err
	}
	for range 2 {
		b.write("r")
		if err := b.waitIdle(2 * time.Minute); err != nil {
			return err
		}
	}
	peak, err := b.peak()
	if err != nil {
		return err
	}
	// Only the peak means anything here; the session's length includes the
	// idle waits.
	if err := r.record("browser peak over comparison and two reloads", i, 0, peak, 0); err != nil {
		return err
	}
	return b.quit()
}

// quitDuringWork times the exit when q arrives while a comparison runs. It
// sends q once the pending comparison is on screen. Bubble Tea draws the
// last view again as it exits, so a comparison that finished before the
// quit took effect shows its result in that output; such a sample left no
// work to stop and is recorded under its own name. On the fast workloads
// every sample finishes first.
func (r *runner) quitDuringWork(i int) error {
	b, err := r.startBrowser()
	if err != nil {
		return err
	}
	defer b.stop()

	if err := b.waitText(0, "lines", time.Minute); err != nil {
		return err
	}
	mark := b.mark()
	b.write(fmt.Sprintf("g%d\r", r.m.Compare[1]))
	b.write("d")
	running, err := b.waitEither(mark, "comparing", "hunk", time.Minute)
	if err != nil {
		return err
	}

	mark = b.mark()
	start := time.Now()
	if err := b.quit(); err != nil {
		return err
	}
	elapsed := time.Since(start)
	<-b.done // the reader has taken the last frame once the terminal closes
	if running && !b.contains(mark, "hunk") {
		return r.record("browser quit while comparing", i, elapsed, 0, 0)
	}
	return r.record("browser quit, comparison already finished", i, elapsed, 0, 0)
}

// browserRun is one browser process on its own pseudo-terminal.
type browserRun struct {
	cmd     *exec.Cmd
	term    *pty.Pair
	started time.Time
	cancel  context.CancelFunc

	mu      sync.Mutex
	output  []byte
	changed chan struct{}
	done    chan struct{}
}

func (r *runner) startBrowser() (*browserRun, error) {
	term, err := pty.Open(160, 50)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), commandTimeout)
	cmd := exec.CommandContext(ctx, r.bin, append([]string{"browse"}, r.inputs()...)...)
	cmd.Dir = r.dir
	cmd.Env = append(os.Environ(), "TERM=xterm-256color")
	cmd.Stdin, cmd.Stdout, cmd.Stderr = term.Slave, term.Slave, term.Slave
	cmd.SysProcAttr = pty.Attach()

	b := &browserRun{cmd: cmd, term: term, cancel: cancel, changed: make(chan struct{}, 1), done: make(chan struct{})}
	b.started = time.Now()
	if err := cmd.Start(); err != nil {
		cancel()
		_ = term.Close()
		return nil, err
	}
	_ = term.Slave.Close() // the child holds its own copy

	go func() {
		defer close(b.done)
		buffer := make([]byte, 64<<10)
		for {
			n, err := term.Master.Read(buffer)
			if n > 0 {
				b.mu.Lock()
				// Only the recent output matters for waiting; keep it bounded.
				b.output = append(b.output, buffer[:n]...)
				if len(b.output) > 32<<20 {
					b.output = b.output[len(b.output)-(16<<20):]
				}
				b.mu.Unlock()
				select {
				case b.changed <- struct{}{}:
				default:
				}
			}
			if err != nil {
				return
			}
		}
	}()
	return b, nil
}

func (b *browserRun) mark() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return len(b.output)
}

func (b *browserRun) write(keys string) {
	_, _ = b.term.Master.WriteString(keys)
}

// waitText waits until the output after from, stripped of terminal
// sequences, contains want.
func (b *browserRun) waitText(from int, want string, limit time.Duration) error {
	_, err := b.waitEither(from, want, want, limit)
	return err
}

// waitEither waits until the output after from contains first or second,
// and reports whether first came first.
func (b *browserRun) waitEither(from int, first, second string, limit time.Duration) (bool, error) {
	deadline := time.After(limit)
	for {
		b.mu.Lock()
		text := ansi.Strip(string(b.output[min(from, len(b.output)):]))
		b.mu.Unlock()
		i, j := strings.Index(text, first), strings.Index(text, second)
		if i >= 0 || j >= 0 {
			return i >= 0 && (j < 0 || i < j), nil
		}
		select {
		case <-b.changed:
		case <-deadline:
			return false, fmt.Errorf("no %q or %q within %v", first, second, limit)
		}
	}
}

// contains reports whether the output after from, stripped of terminal
// sequences, contains want.
func (b *browserRun) contains(from int, want string) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	return strings.Contains(ansi.Strip(string(b.output[min(from, len(b.output)):])), want)
}

// waitIdle waits until the process has used no CPU for 300 ms, which a
// reload that redraws an unchanged screen gives no other sign of.
func (b *browserRun) waitIdle(limit time.Duration) error {
	deadline := time.Now().Add(limit)
	last, quiet := int64(-1), 0
	for time.Now().Before(deadline) {
		used, err := cpuTicks(b.cmd.Process.Pid)
		if err != nil {
			return err
		}
		if used == last {
			quiet++
		} else {
			quiet = 0
		}
		if quiet >= 6 {
			return nil
		}
		last = used
		time.Sleep(50 * time.Millisecond)
	}
	return fmt.Errorf("still busy after %v", limit)
}

func cpuTicks(pid int) (int64, error) {
	data, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid))
	if err != nil {
		return 0, err
	}
	// Fields after the command name, which ends with ')': utime is the
	// 12th and stime the 13th of them.
	fields := strings.Fields(string(data[strings.LastIndexByte(string(data), ')')+1:]))
	if len(fields) < 13 {
		return 0, errors.New("short /proc stat line")
	}
	utime, err1 := strconv.ParseInt(fields[11], 10, 64)
	stime, err2 := strconv.ParseInt(fields[12], 10, 64)
	return utime + stime, errors.Join(err1, err2)
}

// peak reads the process's peak resident memory while it runs.
func (b *browserRun) peak() (int64, error) {
	data, err := os.ReadFile(fmt.Sprintf("/proc/%d/status", b.cmd.Process.Pid))
	if err != nil {
		return 0, err
	}
	for line := range strings.SplitSeq(string(data), "\n") {
		if rest, ok := strings.CutPrefix(line, "VmHWM:"); ok {
			return strconv.ParseInt(strings.TrimSuffix(strings.TrimSpace(rest), " kB"), 10, 64)
		}
	}
	return 0, errors.New("no VmHWM in /proc status")
}

func (b *browserRun) quit() error {
	b.write("q")
	if err := b.cmd.Wait(); err != nil {
		return err
	}
	if status := b.cmd.ProcessState.ExitCode(); status != 0 {
		return fmt.Errorf("browser exited with status %d", status)
	}
	return nil
}

// stop ends a run on any path. Cancelling kills a browser still running,
// and the wait reaps it unless quit already has.
func (b *browserRun) stop() {
	b.cancel()
	if b.cmd.ProcessState == nil {
		_ = b.cmd.Wait()
	}
	_ = b.term.Master.Close()
	<-b.done
}
