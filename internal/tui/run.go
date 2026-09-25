// Package tui is the interactive history browser. It draws a validated
// history as a tree beside the selected state's text or a comparison, and
// does all loading, replay, and comparison on one background worker.
package tui

import (
	"context"
	"errors"
	"os"
	"os/signal"
	"strings"
	"syscall"

	tea "charm.land/bubbletea/v2"

	"github.com/nuggocto/xunhen/internal/limits"
)

// Config is what the browser needs from its command.
type Config struct {
	Load   Loader
	Limits limits.Limits

	// Input and Output are the terminal. The command checks that both are
	// terminals before calling Run.
	Input, Output *os.File
	Environ       []string
}

// Outcome says how the browser ended once the terminal was restored and the
// worker had stopped.
type Outcome struct {
	// LoadErr is the failure of the first load, when nothing was shown.
	LoadErr error
	// Signal is the termination signal that ended the browser, if any.
	Signal syscall.Signal
	// Interrupted is set when ctrl+c ended the browser.
	Interrupted bool
}

// Run shows the browser until the user quits, then restores the terminal
// and joins the worker before returning. Every way out takes that path: q,
// ctrl+c, SIGINT, SIGTERM, SIGHUP, a failed first load, a terminal error,
// and a crash in the worker, which Run reports as an error after cleanup.
func Run(ctx context.Context, config Config) (Outcome, error) {
	if err := config.Limits.Validate(); err != nil {
		return Outcome{}, err
	}

	e := &engine{load: config.Load, limits: config.Limits, cache: newCache(cacheBudget)}
	w := newWorker(ctx, e.perform)
	defer w.close()

	m := newModel(w, func() tea.Msg { return w.next() }, colorAllowed(config.Environ))
	program := tea.NewProgram(m,
		tea.WithContext(ctx),
		tea.WithInput(config.Input),
		tea.WithOutput(config.Output),
		tea.WithEnvironment(config.Environ),
		// The browser handles signals itself, so it can report which one
		// ended it; Bubble Tea turns SIGTERM into a plain quit.
		tea.WithoutSignalHandler(),
	)

	// Signals only matter while the browser owns the terminal. Other
	// commands keep the default disposition, which ends a blocked write.
	signals := make(chan os.Signal, 1)
	signal.Notify(signals, syscall.SIGINT, syscall.SIGTERM, syscall.SIGHUP)
	var outcome Outcome
	stopped, relayed := make(chan struct{}), make(chan struct{})
	go func() {
		defer close(relayed)
		select {
		case s := <-signals:
			outcome.Signal = s.(syscall.Signal)
			// Quit returns once the program takes the message or has
			// already ended.
			program.Quit()
		case <-stopped:
		}
	}()

	final, err := program.Run()
	signal.Stop(signals)
	close(stopped)
	<-relayed
	w.close()

	switch {
	case errors.Is(err, tea.ErrInterrupted):
		outcome.Interrupted = true
		err = nil
	case err != nil:
		return outcome, err
	}

	if last, ok := final.(*model); ok && last.fatal != nil {
		if last.crashed {
			return outcome, last.fatal
		}
		outcome.LoadErr = last.fatal
	}

	return outcome, nil
}

// colorAllowed follows the NO_COLOR convention: any non-empty value turns
// color off. Reverse video and bold remain, since they are not colors.
func colorAllowed(environ []string) bool {
	for _, entry := range environ {
		if value, ok := strings.CutPrefix(entry, "NO_COLOR="); ok && value != "" {
			return false
		}
	}

	return true
}
