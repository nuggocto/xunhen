package main

import (
	"context"
	"errors"
	"io"
	"os"
	"strings"

	"github.com/nuggocto/xunhen/internal/discover"
	"github.com/nuggocto/xunhen/internal/history"
	"github.com/nuggocto/xunhen/internal/limits"
	"github.com/nuggocto/xunhen/internal/tui"
)

const browseHelp = `Usage: xunhen browse --undo PATH --base PATH
       xunhen browse --source PATH --undo-dir DIR...

Explore a history in the terminal: move through its branches, preview any
retained state, and compare two of them.
  --undo PATH      Undo-file path
  --base PATH      Matching source text (read-only)
  --source PATH    Find the history by this source file's path; the file is
                   also the base
  --undo-dir DIR   Undo directory to search; repeat for more (at most 32)
  -h, --help       Show this help

Keys: arrows or h/j/k/l move and scroll, tab switches panes, p previews the
selected node, d compares the pinned node with it, space pins it, g goes to
a node ID, e shows how to export it, r reloads, ? lists every key, q quits.

The browser starts at the reference node, the state the base text verified
against, and compares from it until another node is pinned. Inputs are read
and validated once; r reads them again the same way, and a failed reload
keeps the earlier load. Inputs are never modified.

browse needs a terminal on stdin and stdout and a TERM other than dumb.
Without one it exits with status 1; inspect, show, and diff read the same
history with the same input flags and print plain text instead.

Exit status: 0 success, 1 input/output failure, 2 invalid arguments,
129 hangup, 130 interrupt, 143 termination.
`

func browse(ctx context.Context, args []string, stdin *os.File, stdout, stderr io.Writer) int {
	if helpRequested(args) {
		return writeOutput(stdout, stderr, browseHelp)
	}

	lim := limits.Default()
	in, err := parseBrowseArgs(args, lim)
	if err != nil {
		return diagnostic(stderr, exitUsage, err.Error())
	}

	// Check the terminal before opening anything, so a redirected run fails
	// at once and nothing full-screen reaches a file or pipe.
	terminal, ok := stdout.(*os.File)
	problem := terminalProblem(stdin, stdout)
	if problem == "" && !ok {
		problem = "stdout is not a file"
	}
	if problem != "" {
		return diagnostics(stderr, []string{
			"browse needs an interactive terminal: " + problem,
			"  inspect, show, and diff read the same history as plain text:",
			"    xunhen inspect INPUTS                 list the nodes and their IDs",
			"    xunhen show INPUTS --node ID          print one state",
			"    xunhen diff INPUTS --from ID --to ID  compare two states",
			"  INPUTS are the same --undo and --base, or --source and --undo-dir",
			"  flags; inspect needs no --base.",
		})
	}

	outcome, err := tui.Run(ctx, tui.Config{
		Load:    browseLoader(in, lim),
		Limits:  lim,
		Input:   stdin,
		Output:  terminal,
		Environ: os.Environ(),
	})

	switch {
	case err != nil:
		// A worker crash carries its stack; one escaped line per line keeps
		// it readable in a bug report.
		return diagnostics(stderr, strings.Split("browser failed: "+err.Error(), "\n"))
	case outcome.LoadErr != nil:
		return operationError(stderr, outcome.LoadErr)
	case outcome.Signal != 0:
		return 128 + int(outcome.Signal)
	case outcome.Interrupted:
		return exitInterrupt
	}

	return exitSuccess
}

func parseBrowseArgs(args []string, lim limits.Limits) (*inputs, error) {
	flags := newFlags("browse")
	in := registerInputs(flags, true)

	if err := parseFlags(flags, args); err != nil {
		return nil, err
	}
	if flags.NArg() != 0 {
		return nil, usage("browse", true)
	}
	if err := in.check("browse", true, lim); err != nil {
		return nil, err
	}

	return in, nil
}

// terminalProblem says why stdin and stdout cannot host the browser, or
// returns "" when they can.
func terminalProblem(stdin *os.File, stdout io.Writer) string {
	switch term := os.Getenv("TERM"); {
	case !isTerminal(stdin):
		return "stdin is not a terminal"
	case !isTerminal(stdout):
		return "stdout is not a terminal"
	case term == "":
		return "TERM is not set"
	case term == "dumb":
		return "TERM is dumb"
	}

	return ""
}

func isTerminal(w io.Writer) bool {
	terminal, err := outputIsTerminal(w)
	return err == nil && terminal
}

// browseLoader loads through the same coordinator as show and diff, so a
// reload repeats discovery, base verification, and change detection in the
// original input form.
func browseLoader(in *inputs, lim limits.Limits) tui.Loader {
	labels := tui.Labels{Base: in.base, Inputs: []string{"--undo", in.undo, "--base", in.base}}
	if in.source != "" {
		labels = tui.Labels{Base: in.source, Source: true, Inputs: []string{"--source", in.source}}
		for _, dir := range in.undoDirs {
			labels.Inputs = append(labels.Inputs, "--undo-dir", dir)
		}
	}

	return func(ctx context.Context) (*tui.Loaded, error) {
		l, err := load(ctx, in, needs{base: true}, lim, discover.Search)
		if err != nil {
			return nil, err
		}
		if l.base == nil {
			return nil, errors.New("the history has no verified base")
		}

		r, err := history.Bind(l.history, l.base, lim)
		if err != nil {
			return nil, err
		}

		found := labels
		found.Undo = l.undoPath
		found.Inputs = append([]string(nil), labels.Inputs...)
		return &tui.Loaded{History: l.history, Reconstructor: r, Labels: found}, nil
	}
}
