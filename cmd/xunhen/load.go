package main

import (
	"context"
	"errors"
	"fmt"
	"syscall"

	"github.com/nuggocto/xunhen/internal/discover"
	"github.com/nuggocto/xunhen/internal/history"
	"github.com/nuggocto/xunhen/internal/input"
	"github.com/nuggocto/xunhen/internal/limits"
	"github.com/nuggocto/xunhen/internal/undofile"
)

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

// load resolves a command's inputs into one validated history. Every command
// uses it, and the browser's reload calls it again.
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
		return nil, &sourceError{err: baseErr}
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
	err error
}

func (e *sourceError) Error() string {
	return fmt.Sprintf("the source cannot be used as the base: %v", e.err)
}

func (e *sourceError) Unwrap() error { return e.err }
