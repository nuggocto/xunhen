package main

import (
	"context"
	"io"
	"strconv"

	"github.com/nuggocto/xunhen/internal/discover"
	"github.com/nuggocto/xunhen/internal/history"
	"github.com/nuggocto/xunhen/internal/limits"
	"github.com/nuggocto/xunhen/internal/termtext"
	"github.com/nuggocto/xunhen/internal/undofile"
)

const inspectHelp = `Usage: xunhen inspect --undo PATH
       xunhen inspect --source PATH --undo-dir DIR...

Describe a persisted Neovim undo history without source text or Neovim.
  --undo PATH      Explicit undo-file path (symlinks are followed)
  --source PATH    Find the history by this source file's path instead
  --undo-dir DIR   Undo directory to search; repeat for more (at most 32)
  -h, --help       Show this help

With --source, the history must sit at the name Neovim gives it in one of the
directories, as docs/discovery.md describes; subdirectories are not searched.
When the source text matches the history, the association is verified. When
the source is missing, unsupported, or different, a single valid history is
still shown with the association marked unverified. Two or more candidates
are reported as ambiguous rather than chosen.

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
Inputs are opened read-only and must be regular files. Limits include 256 MiB
of undo input and 1,000,000 changes.

Exit status: 0 success, 1 input/output failure, 2 invalid arguments, 130 interrupt.
The history is fully validated before output starts. Discard partial stdout
after a write failure; closed pipes return status 1 without a panic trace.
`

func inspect(ctx context.Context, args []string, stdout, stderr io.Writer) int {
	if helpRequested(args) {
		return writeOutput(stdout, stderr, inspectHelp)
	}

	lim := limits.Default()
	in, err := parseInspectArgs(args, lim)
	if err != nil {
		return diagnostic(stderr, exitUsage, err.Error())
	}

	l, err := load(ctx, in, needs{}, lim, discover.Search)
	if err != nil {
		return operationError(stderr, err)
	}

	out := newOutput(stdout)
	if err := writeInspection(out, l); err != nil {
		return operationError(stderr, err)
	}

	return out.finish(stderr)
}

func parseInspectArgs(args []string, lim limits.Limits) (*inputs, error) {
	flags := newFlags("inspect")
	in := registerInputs(flags, false)

	if err := parseFlags(flags, args); err != nil {
		return nil, err
	}
	if flags.NArg() != 0 {
		return nil, usage("inspect", false)
	}
	if err := in.check("inspect", false, lim); err != nil {
		return nil, err
	}

	return in, nil
}

// writeInspection streams the report for a validated history. Its lookups
// fail only for an uninitialized history, which New never returns, so they
// cannot interrupt the report partway.
func writeInspection(out *output, l *loaded) error {
	h := l.history
	meta, err := h.Metadata()
	if err != nil {
		return err
	}

	ref, err := h.Reference()
	if err != nil {
		return err
	}

	anchor, err := h.Info(ref)
	if err != nil {
		return err
	}

	out.printf("History: \"%s\"\n", termtext.Escape(l.undoPath))
	switch {
	case l.source == "":
	case l.base != nil:
		out.printf("Found for source: \"%s\" (verified: its text matches the reference)\n", termtext.Escape(l.source))
	default:
		out.printf("Found for source: \"%s\" (unverified: %s)\n", termtext.Escape(l.source), termtext.Escape(l.baseErr.Error()))
	}
	out.printf("Format: Neovim undo %d\n", meta.Format.Version)
	out.printf("Decode profile: %s (assumed; producer ABI is not recorded)\n", meta.Format.Profile)
	out.printf("Validation: complete records and history relationships\n")

	out.printf("Changes: %d\n", meta.HeaderCount)
	out.printf("Retained states: %d (including node 0, the retained root)\n", h.Count())
	out.printf("Reference node: %d\n", anchor.ID)
	if l.base != nil {
		out.printf("Base: the source text verifies as this history's reference\n")
	} else {
		out.printf("Base: required for reconstruction; not supplied or verified\n")
	}
	out.printf("Base buffer SHA-256: %x\n", meta.BaseHash)
	out.printf("Base logical lines: %d\n", meta.BaseLines)

	out.printf("Recorded pointers: oldest-root=%d newest=%d next-redo=%d\n",
		meta.OldestRoot, meta.Newest, meta.NextRedo)
	out.printf("Last allocated sequence: %d\n", meta.LastSequence)
	out.printf("Timeline sequence: %d (metadata, not a node selector)\n", meta.TimelineSequence)
	out.printf("Current time: %d Unix seconds\n", meta.CurrentTime)
	out.printf("Last save number: %s\n", saveLabel(meta.LastSave))
	out.printf("Historical encoding, fileformat, and final newline: not recorded\n")
	out.printf("Reconstruction: not performed; replay ranges have not been checked against text\n")

	out.printf("\nNodes in recorded branch order (zero child/sibling means absent):\n")
	for i := range h.Count() {
		ref, err := h.At(i)
		if err != nil {
			return err
		}

		info, err := h.Info(ref)
		if err != nil {
			return err
		}

		writeNode(out, info)
	}

	return nil
}

func saveLabel(save undofile.SaveNumber) string {
	if !save.Known {
		return "unavailable"
	}
	if save.Value == 0 {
		return "none"
	}

	return strconv.Itoa(int(save.Value))
}

// writeNode renders one validated node. Its fields are bounded scalars.
func writeNode(out *output, info history.NodeInfo) {
	if !info.HasEvent {
		out.printf("node 0: retained root; preferred-child=%d; time=unavailable; save=unavailable\n",
			info.PreferredChild)
		return
	}

	const format = "node %d: parent=%d preferred-child=%d next-sibling=%d previous-sibling=%d " +
		"time=%d save=%s text-entries=%d extmarks=%d flags=0x%02x\n"

	out.printf(format,
		info.ID, info.Parent, info.PreferredChild, info.NextSibling, info.PreviousSibling,
		info.Time, saveLabel(info.Save), info.TextEntries, info.Extmarks, info.Flags)
}
