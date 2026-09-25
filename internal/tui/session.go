package tui

import (
	"context"
	"errors"

	"github.com/nuggocto/xunhen/internal/diff"
	"github.com/nuggocto/xunhen/internal/history"
	"github.com/nuggocto/xunhen/internal/limits"
	"github.com/nuggocto/xunhen/internal/termtext"
)

// Loader resolves the browser's inputs into one validated history and its
// bound reconstructor. The browser calls it at startup and again for every
// reload, always from its worker, so each call must build new objects and
// share nothing with an earlier load.
type Loader func(context.Context) (*Loaded, error)

// Loaded is one consistent load of the inputs.
type Loaded struct {
	History       *history.History
	Reconstructor *history.Reconstructor
	Labels        Labels
}

// Labels describe where a load came from, for display and for the export
// instructions. They are untrusted text and are escaped when drawn.
type Labels struct {
	// Undo is the undo file that was read, whether named or found.
	Undo string
	// Base is the verified base file: --base, or --source when Source is set.
	Base   string
	Source bool
	// Inputs are the input arguments that reproduce this load, such as
	// "--undo" "PATH" "--base" "PATH", for the export command.
	Inputs []string
}

// session is one installed load: the history, its reconstructor, and the
// navigation index built from it. Sessions are immutable, and a reload makes
// a new one, so a node reference from one never resolves in another.
type session struct {
	history       *history.History
	reconstructor *history.Reconstructor
	tree          *tree
	labels        Labels
}

// engine performs the worker's requests. Only the worker goroutine calls it,
// so the cache needs no lock.
type engine struct {
	load   Loader
	limits limits.Limits
	cache  *cache

	// method is the width method the cached documents were indexed with.
	// The renderer changes it at most once, near startup, and the cache
	// starts over when it does.
	method termtext.Method
}

func (e *engine) perform(ctx context.Context, r request) result {
	if r.kind != loadRequest && r.method != e.method {
		e.cache.reset(nil)
		e.method = r.method
	}

	out := result{request: r}
	switch r.kind {
	case loadRequest:
		out.session, out.err = e.loadSession(ctx)
	case previewRequest:
		out.doc, out.err = e.document(ctx, r.session, r.target)
	case compareRequest:
		out.cmp, out.err = e.compare(ctx, r)
	default:
		panic("unknown worker request")
	}

	return out
}

// loadSession releases every cached document before it loads, so a reload
// holds at most the old session and the new one. A failed reload leaves the
// old session usable; its documents are simply reconstructed again.
func (e *engine) loadSession(ctx context.Context) (*session, error) {
	e.cache.reset(nil)

	loaded, err := e.load(ctx)
	if err != nil {
		return nil, err
	}
	if loaded == nil || loaded.History == nil || loaded.Reconstructor == nil {
		return nil, errors.New("the loader returned no history")
	}

	t, err := buildTree(ctx, loaded.History)
	if err != nil {
		return nil, err
	}

	return &session{
		history:       loaded.History,
		reconstructor: loaded.Reconstructor,
		tree:          t,
		labels:        loaded.Labels,
	}, nil
}

// document returns a prepared state from the cache or reconstructs it from
// the verified reference.
func (e *engine) document(ctx context.Context, s *session, node history.NodeRef) (*document, error) {
	if doc, ok := e.cache.get(s, node); ok {
		return doc, nil
	}

	info, err := s.history.Info(node)
	if err != nil {
		return nil, err
	}
	snapshot, err := s.reconstructor.Reconstruct(ctx, node)
	if err != nil {
		return nil, err
	}
	doc, err := prepareDocument(ctx, info.ID, snapshot, e.method)
	if err != nil {
		return nil, err
	}

	e.cache.put(s, node, doc)
	return doc, nil
}

func (e *engine) compare(ctx context.Context, r request) (*comparison, error) {
	from, err := e.document(ctx, r.session, r.origin)
	if err != nil {
		return nil, err
	}
	to, err := e.document(ctx, r.session, r.target)
	if err != nil {
		return nil, err
	}

	d, err := diff.Compare(ctx, from.snapshot, to.snapshot, e.limits)
	if err != nil {
		return nil, err
	}

	return newComparison(from, to, d), nil
}
