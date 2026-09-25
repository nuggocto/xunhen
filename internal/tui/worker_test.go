package tui

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

// operation is one request the scripted worker has started. It runs until
// the test releases it and then returns, cancelled or not, so a test can
// hold work mid-flight and can let obsolete work finish late.
type operation struct {
	request request
	ctx     context.Context
	release func()
}

type script struct {
	started chan operation

	mu       sync.Mutex
	releases []func()
}

func newScript() *script {
	return &script{started: make(chan operation, 16)}
}

// releaseAll lets every operation finish.
func (s *script) releaseAll() {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, release := range s.releases {
		release()
	}
}

func (s *script) perform(ctx context.Context, r request) result {
	released := make(chan struct{})
	op := operation{request: r, ctx: ctx, release: sync.OnceFunc(func() { close(released) })}
	s.mu.Lock()
	s.releases = append(s.releases, op.release)
	s.mu.Unlock()

	s.started <- op
	<-released
	return result{request: r, err: ctx.Err()}
}

// start makes a worker for a script and closes it when the test ends.
// Cleanups run last first, so every operation is released before the worker
// closes, and a failing test cannot leave the worker stuck and hang.
func start(t *testing.T, s *script) *worker {
	w := newWorker(t.Context(), s.perform)
	t.Cleanup(w.close)
	t.Cleanup(s.releaseAll)
	return w
}

// receive waits for a value, failing the test if none arrives. The deadline
// catches a deadlock; no test waits on it to synchronize.
func receive[T any](t *testing.T, ch <-chan T, what string) T {
	t.Helper()

	select {
	case v := <-ch:
		return v
	case <-time.After(10 * time.Second):
		t.Fatalf("timed out waiting for %s", what)
		panic("unreachable")
	}
}

func idle(t *testing.T, s *script) {
	t.Helper()

	select {
	case op := <-s.started:
		t.Fatalf("unexpected request with selection %d started", op.request.selection)
	default:
	}
}

func preview(selection uint64) request {
	return request{kind: previewRequest, load: 1, selection: selection}
}

func loading(generation uint64) request {
	return request{kind: loadRequest, load: generation}
}

func TestWorkerKeepsOneRunningAndTheLatestPending(t *testing.T) {
	t.Parallel()

	s := newScript()
	w := start(t, s)

	w.submit(preview(1))
	a := receive(t, s.started, "request 1 to start")

	// Requests 2 and 3 arrive while 1 runs. Each cancels the running one,
	// and 3 replaces 2 before 2 can start.
	w.submit(preview(2))
	w.submit(preview(3))
	receive(t, a.ctx.Done(), "request 1 to be cancelled")

	// Request 1 ignores its cancellation and finishes late. Its result is
	// still delivered, marked with its own generation, for the model to
	// discard.
	a.release()
	late := receive(t, w.results, "request 1's result")
	if late.selection != 1 || !errors.Is(late.err, context.Canceled) {
		t.Fatalf("first result is selection %d, error %v; want selection 1, cancelled", late.selection, late.err)
	}

	c := receive(t, s.started, "request 3 to start")
	if c.request.selection != 3 {
		t.Fatalf("request %d started after 1; want 3", c.request.selection)
	}
	c.release()
	if r := receive(t, w.results, "request 3's result"); r.selection != 3 || r.err != nil {
		t.Fatalf("result is selection %d, error %v; want selection 3", r.selection, r.err)
	}
	idle(t, s)
}

func TestWorkerProtectsLoads(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		// running is submitted and started first, then queued is submitted.
		running, queued request
		// A selection request submitted after queued.
		later          request
		laterAccepted  bool
		runningStopped bool
		startsNext     request
	}{
		{
			name:    "a pending load outlasts later selections",
			running: preview(1), queued: loading(2), later: preview(2),
			runningStopped: true, startsNext: loading(2),
		},
		{
			name:    "a running load continues through selections",
			running: loading(1), queued: preview(2), later: preview(3),
			laterAccepted: true, startsNext: preview(3),
		},
		{
			name:    "a newer load replaces a running load",
			running: loading(1), queued: loading(2), later: loading(3),
			laterAccepted: true, runningStopped: true, startsNext: loading(3),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			s := newScript()
			w := start(t, s)

			w.submit(tt.running)
			first := receive(t, s.started, "the first request to start")
			w.submit(tt.queued)
			if accepted := w.submit(tt.later); accepted != tt.laterAccepted {
				t.Fatalf("later request accepted = %t, want %t", accepted, tt.laterAccepted)
			}

			if stopped := first.ctx.Err() != nil; stopped != tt.runningStopped {
				t.Fatalf("running request cancelled = %t, want %t", stopped, tt.runningStopped)
			}

			first.release()
			receive(t, w.results, "the first result")
			next := receive(t, s.started, "the next request to start")
			if next.request.kind != tt.startsNext.kind || next.request.load != tt.startsNext.load || next.request.selection != tt.startsNext.selection {
				t.Fatalf("next request = %+v, want %+v", next.request, tt.startsNext)
			}
			next.release()
			receive(t, w.results, "the second result")
			idle(t, s)
		})
	}
}

func TestWorkerShutdown(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		// run leaves the worker in the state to shut down from.
		run func(t *testing.T, w *worker, s *script)
	}{
		{name: "idle", run: func(*testing.T, *worker, *script) {}},
		{name: "running a request", run: func(t *testing.T, w *worker, s *script) {
			w.submit(preview(1))
			op := receive(t, s.started, "the request to start")
			go func() {
				<-op.ctx.Done()
				op.release()
			}()
		}},
		{name: "holding a result nobody reads", run: func(t *testing.T, w *worker, s *script) {
			w.submit(preview(1))
			receive(t, s.started, "the request to start").release()
		}},
		{name: "with a request pending", run: func(t *testing.T, w *worker, s *script) {
			w.submit(preview(1))
			op := receive(t, s.started, "the request to start")
			w.submit(preview(2))
			go func() {
				<-op.ctx.Done()
				op.release()
			}()
		}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			s := newScript()
			w := newWorker(t.Context(), s.perform)
			tt.run(t, w, s)

			closed := make(chan struct{})
			go func() {
				w.close()
				close(closed)
			}()
			receive(t, closed, "the worker to close")

			if w.submit(preview(9)) {
				t.Fatal("a closed worker accepted a request")
			}
			if next := w.next(); next != nil {
				t.Fatalf("a closed worker delivered %v", next)
			}
			idle(t, s)
		})
	}
}

func TestWorkerReportsPanics(t *testing.T) {
	t.Parallel()

	w := newWorker(t.Context(), func(context.Context, request) result { panic("broken invariant") })
	defer w.close()

	w.submit(preview(1))
	r := receive(t, w.results, "the crash result")
	if r.crash == nil || r.selection != 1 {
		t.Fatalf("result = %+v, want a crash for selection 1", r)
	}
}
