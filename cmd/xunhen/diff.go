package main

import (
	"context"
	"errors"
	"io"
	"os"

	"github.com/nuggocto/xunhen/internal/diff"
	"github.com/nuggocto/xunhen/internal/history"
	"github.com/nuggocto/xunhen/internal/limits"
	"github.com/nuggocto/xunhen/internal/termtext"
)

const diffHelp = `Usage: xunhen diff FILE --from ID --to ID
       xunhen diff --undo PATH --base PATH --from ID --to ID

Compare two retained buffer states line by line using a matching base file.
  FILE             Source file: its path finds the history, and its text is
                   the base (same as --source FILE)
  --undo-dir DIR   Undo directory to search for FILE's history; repeat for
                   more (at most 32). Without it, the directories in
                   XUNHEN_UNDO_DIR, separated by colons
  --undo PATH      Undo-file path, instead of FILE
  --base PATH      Matching source text for --undo (read-only)
  --source PATH    Same as FILE
  --from ID        Left-hand retained sequence number; 0 is the root (required)
  --to ID          Right-hand retained sequence number (required)
  -h, --help       Show this help

With FILE, exactly one history in the searched directories must match the
source text, as for show.

Output is a unified diff with three lines of context and one-based line
numbers. Identical states print nothing, and the exit status is 0 whether or
not the states differ.

Lines are compared as exact bytes. Output then escapes terminal controls and
invalid bytes the same way show does. The undo file records neither state's
final newline, so the diff never reports one. An empty buffer is one empty
line.

The diff is minimal unless the states are very far apart. Past a fixed amount
of search work, the rest is aligned at lines that occur once on each side, and
then shown as plain deletions and insertions. Either way the diff is complete.
Inputs are never modified.
Exit status: 0 success, 1 input/output failure, 2 invalid arguments, 130 interrupt.
`

type diffOptions struct {
	in             *inputs
	from, to       history.NodeID
	hasFrom, hasTo bool
}

func compareStates(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	if helpRequested(args) {
		return writeOutput(stdout, stderr, diffHelp)
	}

	lim := limits.Default()
	options, err := parseDiffArgs(args, lim)
	if err != nil {
		return usageFailure(stderr, err)
	}

	states, err := recoverStates(ctx, options.in, []history.NodeID{options.from, options.to}, lim)
	if err != nil {
		return operationError(stderr, err)
	}

	result, err := diff.Compare(ctx, states[0], states[1], lim)
	if err != nil {
		return operationError(stderr, err)
	}

	out := newOutput(stdout)
	writeDiff(out, result, options.from, options.to)
	return out.finish(stderr)
}

func parseDiffArgs(args []string, lim limits.Limits) (diffOptions, error) {
	var options diffOptions

	flags := newFlags("diff")
	options.in = registerInputs(flags, true)
	nodeFlag(flags, "from", &options.from, &options.hasFrom)
	nodeFlag(flags, "to", &options.to, &options.hasTo)

	if err := parseInputs(flags, options.in, args, "diff", true, lim, os.Getenv); err != nil {
		return diffOptions{}, err
	}
	if !options.hasFrom || !options.hasTo {
		return diffOptions{}, errors.New("diff requires --from ID and --to ID; see 'xunhen diff --help'")
	}

	return options, nil
}

// writeDiff streams a completed diff in unified format. Escaping happens
// here, after the comparison, and cannot produce a newline, so recovered text
// cannot forge hunk structure.
func writeDiff(out *output, d *diff.Diff, from, to history.NodeID) {
	hunks := d.Hunks()
	if len(hunks) == 0 {
		return
	}

	out.printf("--- node %d\n+++ node %d\n", from, to)
	for _, h := range hunks {
		out.printf("%s\n", h.Header())
		for line := range h.Lines() {
			out.text(" -+"[line.Op : line.Op+1])
			out.text(termtext.EscapeDisplay(line.Text))
			out.text("\n")
		}
	}
}
