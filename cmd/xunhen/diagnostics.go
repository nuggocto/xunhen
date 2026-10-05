package main

import (
	"errors"
	"io"

	"github.com/nuggocto/xunhen/internal/discover"
)

// usageFailure reports an invalid invocation with status 2. A hintError
// gets one diagnostic line per line of its explanation. If stderr fails,
// the status is the output failure's, as for any other diagnostic.
func usageFailure(stderr io.Writer, err error) int {
	hint, ok := errors.AsType[*hintError](err)
	if !ok {
		return diagnostic(stderr, exitUsage, err.Error())
	}
	for _, line := range hint.lines {
		if status := diagnostic(stderr, exitUsage, line); status != exitUsage {
			return status
		}
	}

	return exitUsage
}

// operationError reports a failed input, limit, search, or output operation.
// Search and source failures get several lines: what was searched, what each
// candidate turned out to be, and how to continue with explicit paths.
func operationError(stderr io.Writer, err error) int {
	if searchErr, ok := errors.AsType[*discover.SearchError](err); ok {
		return explainSearch(stderr, searchErr)
	}
	if sourceErr, ok := errors.AsType[*sourceError](err); ok {
		lines := []string{
			sourceErr.Error(),
			"  Undo files are found by the source's path, but reconstruction also needs",
			"  its text. A moved, renamed, or deleted source can still be inspected with",
			"  --source. To recover with a copy of the text the history was written against:",
			"    xunhen show --undo PATH --base COPY --node ID",
		}
		return diagnostics(stderr, lines)
	}
	return diagnostic(stderr, exitFailure, err.Error())
}

func explainSearch(stderr io.Writer, e *discover.SearchError) int {
	r := e.Result
	lines := []string{
		e.Error(),
		"  source resolves to " + r.Target.Path,
		"  undo file name " + r.Target.UndoName(),
	}

	for _, s := range r.Scope {
		switch s.Status {
		case discover.Searched:
			lines = append(lines, "  searched "+s.Path)
		case discover.Repeated:
			lines = append(lines, "  skipped "+s.Path+": the same directory as "+s.SameAs)
		case discover.Unavailable:
			lines = append(lines, "  could not search: "+s.Err.Error())
		}
	}

	for _, rep := range r.Reports {
		line := "  " + rep.Path + ": " + rep.Outcome.String()
		switch {
		case rep.Outcome == discover.Duplicate:
			line += ", the same file as " + rep.SameAs
		case rep.Err != nil:
			line += ": " + rep.Err.Error()
		}
		lines = append(lines, line)
	}

	switch e.Problem {
	case discover.NotFound:
		lines = append(lines,
			"  Neovim names an undo file after the source's full path when it is written,",
			"  so a moved or renamed source keeps its history under the old path. A deleted",
			"  source can still be inspected with --source and its old path.")
	case discover.Mismatch, discover.NoBase:
		lines = append(lines,
			"  Inspection does not need the text: xunhen inspect --source works here.",
			"  To recover, name the history and a copy of the text it was written against.")
	}
	lines = append(lines,
		"  To use a history directly:",
		"    xunhen inspect --undo PATH",
		"    xunhen show --undo PATH --base COPY --node ID")

	return diagnostics(stderr, lines)
}

// diagnostics writes several escaped diagnostic lines and returns the status
// for an operational failure.
func diagnostics(stderr io.Writer, lines []string) int {
	for _, line := range lines {
		diagnostic(stderr, exitFailure, line)
	}

	return exitFailure
}
