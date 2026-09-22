package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strconv"

	"github.com/nuggocto/xunhen/internal/diff"
	"github.com/nuggocto/xunhen/internal/history"
	"github.com/nuggocto/xunhen/internal/limits"
	"github.com/nuggocto/xunhen/internal/termtext"
)

const diffHelp = `Usage: xunhen diff --undo PATH --base PATH --from ID --to ID

Compare two retained buffer states line by line using a matching base file.
  --undo PATH    Undo-file path (required)
  --base PATH    Matching source text (required; read-only)
  --from ID      Left-hand retained sequence number; 0 is the root (required)
  --to ID        Right-hand retained sequence number (required)
  -h, --help     Show this help

Output is a unified diff with three lines of context and one-based line
numbers. Identical states print nothing, and the exit status is 0 whether or
not the states differ.

Lines are compared as exact bytes. Output then escapes terminal controls and
invalid bytes the same way show does. The undo file records neither state's
final newline, so the diff never reports one. An empty buffer is one empty
line.

A comparison that exceeds its step or workspace budget fails with the name of
the budget; it never prints a partial diff. Inputs are never modified.
Exit status: 0 success, 1 input/output failure, 2 invalid arguments, 130 interrupt.
`

type diffOptions struct {
	undo, base     string
	from, to       history.NodeID
	hasFrom, hasTo bool
}

func compareStates(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	if helpRequested(args) {
		return writeOutput(stdout, stderr, diffHelp)
	}

	options, err := parseDiffArgs(args)
	if err != nil {
		return diagnostic(stderr, exitUsage, err.Error())
	}

	lim := limits.Default()
	states, err := recoverStates(ctx, options.undo, options.base, []history.NodeID{options.from, options.to}, lim)
	if err != nil {
		return operationError(stderr, err)
	}

	result, err := diff.Compare(ctx, states[0], states[1], lim)
	if err != nil {
		return operationError(stderr, err)
	}

	text, err := diffText(ctx, result, options.from, options.to, lim.OutputBytes)
	if err != nil {
		return operationError(stderr, err)
	}

	return writeOutput(stdout, stderr, text)
}

func parseDiffArgs(args []string) (diffOptions, error) {
	var options diffOptions

	flags := newFlags("diff")
	pathFlag(flags, "undo", &options.undo)
	pathFlag(flags, "base", &options.base)
	nodeFlag(flags, "from", &options.from, &options.hasFrom)
	nodeFlag(flags, "to", &options.to, &options.hasTo)

	if err := parseFlags(flags, args); err != nil {
		return diffOptions{}, err
	}
	if options.undo == "" || options.base == "" || !options.hasFrom || !options.hasTo || flags.NArg() != 0 {
		return diffOptions{}, errors.New("expected diff --undo PATH --base PATH --from ID --to ID; see 'xunhen diff --help'")
	}

	return options, nil
}

// diffText renders a completed diff in unified format. Escaping happens here,
// after the comparison, and cannot produce a newline, so recovered text cannot
// forge hunk structure.
func diffText(ctx context.Context, d *diff.Diff, from, to history.NodeID, maxBytes int) (string, error) {
	hunks := d.Hunks()
	out := boundedOutput{ctx: ctx, maxBytes: maxBytes}
	if len(hunks) == 0 {
		return out.result()
	}

	out.line("--- node %d\n+++ node %d\n", from, to)
	for _, h := range hunks {
		out.line("@@ -%s +%s @@\n", hunkRange(h.LeftStart, h.LeftCount), hunkRange(h.RightStart, h.RightCount))

		for line := range h.Lines() {
			if out.err != nil {
				break
			}

			escaped, err := termtext.EscapeDisplay(ctx, line.Text, maxBytes)
			if err != nil {
				return "", err
			}
			out.line("%c%s\n", " -+"[line.Op], escaped)
		}
	}

	return out.result()
}

// hunkRange follows GNU unified headers: a one-line range omits its count, and
// an empty range names the line before it.
func hunkRange(start, count int) string {
	switch count {
	case 0:
		return fmt.Sprintf("%d,0", start)
	case 1:
		return strconv.Itoa(start + 1)
	default:
		return fmt.Sprintf("%d,%d", start+1, count)
	}
}
