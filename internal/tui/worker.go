package tui

import (
	"context"
	"fmt"
	"runtime/debug"
	"sync"

	"github.com/nuggocto/xunhen/internal/history"
	"github.com/nuggocto/xunhen/internal/termtext"
)

type requestKind uint8

const (
	loadRequest requestKind = iota + 1
	previewRequest
	compareRequest
)

// request is one unit of background work. Every request carries the load and
// selection generations current when the model made it, and the model
// accepts a result only while both still match, so a late result, success or
// failure, can never stand in for a newer request.
type request struct {
	kind            requestKind
	load, selection uint64

	// session is the load the request reads; nil for a load request.
	session *session
	// target is the previewed state, or the right-hand side of a
	// comparison whose left-hand side is origin.
	target, origin history.NodeRef
	// method is the renderer's width method, which column indexes follow.
	method termtext.Method
}

// result answers one request. A worker delivers exactly one result for every
// request it starts, unless it is closing.
type result struct {
	request

	session *session    // for a load
	doc     *document   // for a preview
	cmp     *comparison // for a comparison
	err     error

	// crash holds a panic raised while performing the request, with its
	// stack. The browser shuts down and reports it.
	crash error
}

// worker runs background requests one at a time on a single goroutine. It
// keeps at most one pending request: a newer submission replaces the pending
// one and cancels the running one, which is obsolete by construction. The
// exception is loading: a pending load is never replaced by a selection
// request, and a running load is cancelled only by another load, so moving
// through the tree cannot discard a reload.
//
// Results go out on an unbuffered channel read by one waiting command at a
// time. A result the model is slow to take holds up only the worker, and
// closing the worker releases that send.
type worker struct {
	perform func(context.Context, request) result

	mu      sync.Mutex
	pending *request
	running *request
	cancel  context.CancelFunc
	closed  bool

	base    context.Context
	stop    context.CancelFunc
	wake    chan struct{} // capacity one: "pending may have changed"
	results chan result
	done    chan struct{}
}

func newWorker(ctx context.Context, perform func(context.Context, request) result) *worker {
	base, stop := context.WithCancel(ctx)
	w := &worker{
		perform: perform,
		base:    base,
		stop:    stop,
		wake:    make(chan struct{}, 1),
		results: make(chan result),
		done:    make(chan struct{}),
	}
	go w.loop()

	return w
}

// submit hands a request to the worker without blocking and reports whether
// the worker took it. It never starts a goroutine.
func (w *worker) submit(r request) bool {
	w.mu.Lock()
	defer w.mu.Unlock()

	if w.closed {
		return false
	}
	if w.pending != nil && w.pending.kind == loadRequest && r.kind != loadRequest {
		return false
	}
	if w.running != nil && (r.kind == loadRequest || w.running.kind != loadRequest) {
		w.cancel()
	}

	w.pending = &r
	select {
	case w.wake <- struct{}{}:
	default:
		// A wake-up is already queued; it will find this request.
	}

	return true
}

// close cancels any running request, drops the pending one, and waits for
// the worker goroutine to end. It is safe to call more than once.
func (w *worker) close() {
	w.mu.Lock()
	w.closed = true
	w.pending = nil
	w.mu.Unlock()

	w.stop()
	<-w.done
}

// next returns the next result, or nil once the worker has closed. The model
// keeps exactly one call of it outstanding as a Bubble Tea command.
func (w *worker) next() any {
	select {
	case r := <-w.results:
		return r
	case <-w.done:
		return nil
	}
}

func (w *worker) loop() {
	defer close(w.done)

	for {
		select {
		case <-w.base.Done():
			return
		case <-w.wake:
		}

		w.mu.Lock()
		r := w.pending
		w.pending = nil
		if r == nil {
			w.mu.Unlock()
			continue
		}
		ctx, cancel := context.WithCancel(w.base)
		w.running, w.cancel = r, cancel
		w.mu.Unlock()

		out := w.run(ctx, *r)

		w.mu.Lock()
		w.running, w.cancel = nil, nil
		w.mu.Unlock()
		cancel()

		select {
		case w.results <- out:
		case <-w.base.Done():
			return
		}
	}
}

// run performs one request and turns a panic into a crash result, so the
// browser can restore the terminal before it reports the failure.
func (w *worker) run(ctx context.Context, r request) (out result) {
	defer func() {
		if value := recover(); value != nil {
			out = result{request: r, crash: fmt.Errorf("panic in the browser worker: %v\n%s", value, debug.Stack())}
		}
	}()

	return w.perform(ctx, r)
}
