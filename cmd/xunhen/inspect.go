package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"strings"

	"github.com/nuggocto/xunhen/internal/history"
	"github.com/nuggocto/xunhen/internal/limits"
	"github.com/nuggocto/xunhen/internal/termtext"
	"github.com/nuggocto/xunhen/internal/undofile"
)

const inspectHelp = `Usage: xunhen inspect --undo PATH

Describe a persisted Neovim undo history without source text or Neovim.
  --undo PATH    Explicit undo-file path (required once; symlinks are followed)
  -h, --help     Show this help

This build interprets format 3 using the Linux/amd64 little-endian LP64 profile.
The file does not record its producer release or ABI. Other profiles are unverified.
Unknown versions, flags, and optional or native record types are rejected.

Node selectors preserve retained sequence numbers; node 0 is the retained root,
which can contain nonempty text after pruning. Nodes appear in recorded branch
order, not timestamp order. Times are signed Unix seconds; root time is unknown.
Zero child/sibling pointers mean absent. The timeline position is metadata and
need not identify a retained node.

Inspection validates records and relationships, not replay. Reconstructing text
requires a matching base. Historical encoding and newline options are not stored.
No source text is printed. Paths and diagnostics use printable ASCII escapes.
Inputs are opened read-only and must be regular files. Limits include 64 MiB of
undo input, 100,000 changes, and 16 MiB of output after escaping.

Exit status: 0 success, 1 input/output failure, 2 invalid arguments, 130 interrupt.
Output is prepared before writing. Discard partial stdout after a write failure;
closed pipes return status 1 without a panic trace.
`

func inspect(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	if len(args) == 1 && (args[0] == "--help" || args[0] == "-h") {
		return writeOutput(stdout, stderr, inspectHelp)
	}

	path, err := parseInspectArgs(args)
	if err != nil {
		return diagnostic(stderr, exitUsage, err.Error())
	}

	lim := limits.Default()
	file, err := loadUndo(ctx, path, lim)
	if err != nil {
		return operationError(stderr, err)
	}

	h, err := history.New(ctx, file, lim)
	if err != nil {
		return operationError(stderr, err)
	}

	text, err := inspectionText(ctx, path, h, lim.OutputBytes)
	if err != nil {
		return operationError(stderr, err)
	}
	if err := ctx.Err(); err != nil {
		return operationError(stderr, err)
	}

	return writeOutput(stdout, stderr, text)
}

func parseInspectArgs(args []string) (string, error) {
	flags := flag.NewFlagSet("inspect", flag.ContinueOnError)
	flags.SetOutput(io.Discard)

	var path string
	var supplied bool
	flags.Func("undo", "undo-file path", func(value string) error {
		if supplied {
			return errors.New("--undo must be supplied only once")
		}

		supplied = true
		path = value
		return nil
	})

	if err := flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return "", errors.New("inspect --help must be used alone")
		}
		return "", err
	}
	if !supplied || path == "" || flags.NArg() != 0 {
		return "", errors.New("expected inspect --undo PATH; see 'xunhen inspect --help'")
	}

	return path, nil
}

func operationError(stderr io.Writer, err error) int {
	if errors.Is(err, context.Canceled) {
		return diagnostic(stderr, exitInterrupted, "interrupted")
	}

	return diagnostic(stderr, exitFailure, err.Error())
}

func inspectionText(ctx context.Context, path string, h *history.History, maxBytes int) (string, error) {
	meta, err := h.Metadata()
	if err != nil {
		return "", err
	}

	ref, err := h.Reference()
	if err != nil {
		return "", err
	}

	anchor, err := h.Info(ref)
	if err != nil {
		return "", err
	}

	label, err := termtext.Escape(ctx, path, maxBytes)
	if err != nil {
		return "", err
	}

	out := inspectionOutput{ctx: ctx, maxBytes: maxBytes}
	out.line("History: \"%s\"\n", label)
	out.line("Format: Neovim undo %d\n", meta.Format.Version)
	out.line("Decode profile: %s (assumed; producer ABI is not recorded)\n", meta.Format.Profile)
	out.line("Validation: complete records and history relationships\n")

	out.line("Changes: %d\n", meta.HeaderCount)
	out.line("Retained states: %d (including node 0, the retained root)\n", h.Count())
	out.line("Reference node: %d\n", anchor.ID)
	out.line("Base: required for reconstruction; not supplied or verified\n")
	out.line("Base buffer SHA-256: %x\n", meta.BaseHash)
	out.line("Base logical lines: %d\n", meta.BaseLines)

	out.line("Recorded pointers: oldest-root=%d newest=%d next-redo=%d\n",
		meta.OldestRoot, meta.Newest, meta.NextRedo)
	out.line("Last allocated sequence: %d\n", meta.LastSequence)
	out.line("Timeline sequence: %d (metadata, not a node selector)\n", meta.TimelineSequence)
	out.line("Current time: %d Unix seconds\n", meta.CurrentTime)
	out.line("Last save number: %s\n", saveLabel(meta.LastSave))
	out.line("Historical encoding, fileformat, and final newline: not recorded\n")
	out.line("Reconstruction: not performed; replay ranges have not been checked against text\n")

	out.line("\nNodes in recorded branch order (zero child/sibling means absent):\n")
	for i := 0; i < h.Count() && out.err == nil; i++ {
		ref, err := h.At(i)
		if err != nil {
			return "", err
		}

		info, err := h.Info(ref)
		if err != nil {
			return "", err
		}

		out.node(info)
	}

	if out.err != nil {
		return "", out.err
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}

	return out.text.String(), nil
}

func saveLabel(save undofile.SaveNumber) string {
	if !save.Known {
		return "unavailable"
	}
	if save.Value == 0 {
		return "none"
	}

	return fmt.Sprint(save.Value)
}

type inspectionOutput struct {
	ctx      context.Context
	text     strings.Builder
	maxBytes int
	err      error
}

func (o *inspectionOutput) line(format string, args ...any) {
	if o.err != nil {
		return
	}
	if o.err = o.ctx.Err(); o.err != nil {
		return
	}

	// Arguments are bounded scalars or an already bounded, escaped path.
	line := fmt.Sprintf(format, args...)
	if len(line) > o.maxBytes-o.text.Len() {
		o.err = errors.New("inspection exceeds output byte budget")
		return
	}

	o.text.WriteString(line)
}

func (o *inspectionOutput) node(info history.NodeInfo) {
	if !info.HasEvent {
		o.line("node 0: retained root; preferred-child=%d; time=unavailable; save=unavailable\n",
			info.PreferredChild)
		return
	}

	const format = "node %d: parent=%d preferred-child=%d next-sibling=%d previous-sibling=%d " +
		"time=%d save=%s text-entries=%d extmarks=%d flags=0x%02x\n"

	o.line(format,
		info.ID, info.Parent, info.PreferredChild, info.NextSibling, info.PreviousSibling,
		info.Time, saveLabel(info.Save), info.TextEntries, info.Extmarks, info.Flags)
}
