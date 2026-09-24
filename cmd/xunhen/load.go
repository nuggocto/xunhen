package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"syscall"

	"github.com/nuggocto/xunhen/internal/discover"
	"github.com/nuggocto/xunhen/internal/history"
	"github.com/nuggocto/xunhen/internal/input"
	"github.com/nuggocto/xunhen/internal/limits"
	"github.com/nuggocto/xunhen/internal/undofile"
)

// inputs are the input flags shared by inspect, show, and diff. Either undo
// (with base, for reconstruction) names the history explicitly, or source and
// undoDirs find it.
type inputs struct {
	undo, base, source string
	undoDirs           []string
}

// registerInputs adds the input flags to a command. withBase adds --base, which
// only reconstructing commands accept.
func registerInputs(flags *flag.FlagSet, withBase bool) *inputs {
	in := &inputs{}
	pathFlag(flags, "undo", &in.undo)
	if withBase {
		pathFlag(flags, "base", &in.base)
	}
	pathFlag(flags, "source", &in.source)

	// Each --undo-dir is one literal path. Commas are filename characters,
	// not separators as in 'undodir'; repeat the flag for more directories.
	flags.Func("undo-dir", "undo directory to search", func(value string) error {
		if value == "" {
			return errors.New("--undo-dir requires a directory path")
		}
		in.undoDirs = append(in.undoDirs, value)
		return nil
	})

	return in
}

// usage is the error for an invocation that names no usable input form.
func usage(command string, withBase bool) error {
	explicit := "--undo PATH"
	if withBase {
		explicit += " --base PATH"
	}

	return fmt.Errorf("expected %s %s, or %s --source PATH --undo-dir DIR; see 'xunhen %s --help'",
		command, explicit, command, command)
}

// check applies the input rules before any file is opened.
func (in *inputs) check(command string, withBase bool, lim limits.Limits) error {
	switch {
	case in.source != "" && (in.undo != "" || in.base != ""):
		if withBase {
			return errors.New("--source cannot be combined with --undo or --base")
		}
		return errors.New("--source cannot be combined with --undo")
	case in.source != "" && len(in.undoDirs) == 0:
		return errors.New("--source requires at least one --undo-dir")
	case len(in.undoDirs) > lim.SearchDirs:
		return fmt.Errorf("at most %d --undo-dir directories can be searched", lim.SearchDirs)
	case in.source == "" && len(in.undoDirs) != 0:
		return errors.New("--undo-dir requires --source")
	case in.source == "" && (in.undo == "" || (withBase && in.base == "")):
		return usage(command, withBase)
	}

	return nil
}

// searcher runs a discovery search. Commands pass discover.Search; tests pass
// a wrapper that changes files between the source read and the search.
type searcher func(context.Context, discover.Request, limits.Limits) (*discover.Result, error)

// loaded is one consistent load. It owns the decoded records and base text and
// holds no open files. Every call to load builds new objects, so a reload
// shares nothing with an earlier load: a node reference from the old history
// fails against the new one even when its numeric ID still exists.
type loaded struct {
	undoPath string
	history  *history.History

	// base is the verified reference text. It is nil only when inspecting a
	// source-based load whose association did not verify; baseErr says why.
	base    *undofile.VerifiedBase
	baseErr error

	// source is the --source path, empty for an explicit load.
	source string
}

// needs says what a command requires of a load.
type needs struct {
	// base requires a verified base, which reconstruction depends on.
	base bool
	// check validates the history before the base is read in an explicit
	// load, so an unknown node selector fails without opening the base.
	check func(*history.History) error
}

// load resolves a command's inputs into one validated history. show, diff,
// and later the terminal browser use this operation, and reloading means
// calling it again.
func load(ctx context.Context, in *inputs, need needs, lim limits.Limits, search searcher) (*loaded, error) {
	if in.source != "" {
		return loadFromSource(ctx, in, need, lim, search)
	}

	return loadExplicit(ctx, in, need, lim)
}

func loadExplicit(ctx context.Context, in *inputs, need needs, lim limits.Limits) (*loaded, error) {
	file, undoIdentity, err := loadUndo(ctx, in.undo, lim)
	if err != nil {
		return nil, err
	}

	h, err := history.New(ctx, file, lim)
	if err != nil {
		return nil, err
	}
	if need.check != nil {
		if err := need.check(h); err != nil {
			return nil, err
		}
	}

	result := &loaded{undoPath: in.undo, history: h}
	if !need.base {
		return result, nil
	}

	lines, _, err := readText(ctx, in.base, "base", lim)
	if err != nil {
		return nil, err
	}
	if result.base, err = undofile.VerifyBase(ctx, file, in.base, lines, lim); err != nil {
		return nil, err
	}

	// The base was read after the undo file; a history rewritten meanwhile
	// would no longer be the one verified.
	if err := input.Recheck(in.undo, "undo", undoIdentity); err != nil {
		return nil, err
	}

	return result, nil
}

func loadFromSource(ctx context.Context, in *inputs, need needs, lim limits.Limits, search searcher) (*loaded, error) {
	// Neovim joins a relative source to the physical working directory, which
	// getcwd(2) returns and os.Getwd may not.
	cwd, err := syscall.Getwd()
	if err != nil {
		return nil, fmt.Errorf("find the working directory: %w", err)
	}
	target := discover.Resolve(in.source, cwd)

	lines, sourceIdentity, readErr := readText(ctx, in.source, "source", lim)
	if isFatal(readErr) {
		return nil, readErr
	}

	var base *undofile.PreparedBase
	baseErr := readErr
	if readErr == nil {
		base, baseErr = undofile.PrepareBase(ctx, in.source, lines, lim)
	}
	if baseErr != nil && need.base {
		return nil, &sourceError{path: in.source, err: baseErr}
	}

	result, err := search(ctx, discover.Request{
		Target: target, Dirs: in.undoDirs, Base: base, BaseErr: baseErr, KeepUnverified: !need.base,
	}, lim)
	if err != nil {
		return nil, err
	}

	var found *discover.Load
	if need.base {
		found, err = result.Verified()
	} else {
		found, err = result.ForInspection()
	}
	if err != nil {
		return nil, err
	}

	// The search took time. The source must still be the text it verified,
	// and its path must still name the same undo file.
	if readErr == nil {
		if err := input.Recheck(in.source, "source", sourceIdentity); err != nil {
			return nil, err
		}
	}
	if discover.Resolve(in.source, cwd) != target {
		return nil, &input.ChangedError{Path: in.source, Kind: "source", Detail: "now resolves to a different file"}
	}

	if need.check != nil {
		if err := need.check(found.History); err != nil {
			return nil, err
		}
	}

	return &loaded{
		undoPath: found.Path,
		history:  found.History,
		base:     found.Base,
		baseErr:  found.Err,
		source:   in.source,
	}, nil
}

// recoverStates loads the inputs and reconstructs each selected node, each
// from the verified reference in a fresh workspace. In an explicit load the
// selectors resolve before the base is read, so an unknown node fails without
// opening the base file.
func recoverStates(ctx context.Context, in *inputs, nodes []history.NodeID, lim limits.Limits) ([]*history.Snapshot, error) {
	refs := make([]history.NodeRef, len(nodes))
	resolve := func(h *history.History) error {
		for i, id := range nodes {
			var err error
			if refs[i], err = h.Lookup(id); err != nil {
				return err
			}
		}
		return nil
	}

	l, err := load(ctx, in, needs{base: true, check: resolve}, lim, discover.Search)
	if err != nil {
		return nil, err
	}

	reconstructor, err := history.Bind(l.history, l.base, lim)
	if err != nil {
		return nil, err
	}

	states := make([]*history.Snapshot, len(refs))
	for i, ref := range refs {
		if states[i], err = reconstructor.Reconstruct(ctx, ref); err != nil {
			return nil, err
		}
	}

	return states, nil
}

// isFatal reports source read errors that end a load even for inspection:
// cancellation and a source that changed while it was read.
func isFatal(err error) bool {
	var changed *input.ChangedError
	return errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) || errors.As(err, &changed)
}

// sourceError reports a source that cannot serve as the base for
// reconstruction.
type sourceError struct {
	path string
	err  error
}

func (e *sourceError) Error() string {
	return fmt.Sprintf("the source cannot be used as the base: %v", e.err)
}

func (e *sourceError) Unwrap() error { return e.err }

// operationError reports a failed input, limit, search, or output operation.
// Search and source failures get several lines: what was searched, what each
// candidate turned out to be, and how to continue with explicit paths.
func operationError(stderr io.Writer, err error) int {
	var searchErr *discover.SearchError
	var sourceErr *sourceError
	switch {
	case errors.As(err, &searchErr):
		return explainSearch(stderr, searchErr)
	case errors.As(err, &sourceErr):
		lines := []string{
			sourceErr.Error(),
			"  Undo files are found by the source's path, but reconstruction also needs",
			"  its text. A moved, renamed, or deleted source can still be inspected with",
			"  --source. To recover with a copy of the text the history was written against:",
			"    xunhen show --undo PATH --base COPY --node ID",
		}
		return diagnostics(stderr, lines)
	default:
		return diagnostic(stderr, exitFailure, err.Error())
	}
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
