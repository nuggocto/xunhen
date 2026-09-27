package tui

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/nuggocto/xunhen/internal/history"
	"github.com/nuggocto/xunhen/internal/limits"
	"github.com/nuggocto/xunhen/internal/undofile"
)

// TestMeasureWorkload times the browser's worker operations on a workload
// from tools/workload and appends raw samples as JSON lines. It measures,
// and checks nothing a regression test would, so it runs only when asked:
//
//	XUNHEN_WORKLOAD=DIR XUNHEN_MEASURE_OUT=FILE XUNHEN_SAMPLES=N \
//	  go test ./internal/tui -run TestMeasureWorkload -count=1
//
// Each operation starts from a cold application cache, a fresh engine,
// unless it says cached. The files are read once first, so the filesystem
// cache is warm throughout.
func TestMeasureWorkload(t *testing.T) {
	dir := os.Getenv("XUNHEN_WORKLOAD")
	if dir == "" {
		t.Skip("set XUNHEN_WORKLOAD to measure a generated workload")
	}
	// The command keeps the heap near its live size with this soft limit;
	// without it the collector lets the heap double, which the command
	// never does.
	if os.Getenv("GOMEMLIMIT") == "" {
		debug.SetMemoryLimit(768 << 20)
	}
	out, err := os.OpenFile(os.Getenv("XUNHEN_MEASURE_OUT"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	defer out.Close()
	samples, err := strconv.Atoi(os.Getenv("XUNHEN_SAMPLES"))
	if err != nil || samples < 1 {
		samples = 5
	}

	var m struct {
		Recipe  string   `json:"recipe"`
		Version int      `json:"version"`
		Compare [2]int32 `json:"compare"`
	}
	data, err := os.ReadFile(filepath.Join(dir, "manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatal(err)
	}

	loader := diskLoader(dir)
	encoder := json.NewEncoder(out)
	record := func(operation string, i int, elapsed time.Duration, alloc allocation) {
		err := encoder.Encode(map[string]any{
			"recipe": m.Recipe, "version": m.Version, "operation": operation, "sample": i,
			"seconds": elapsed.Seconds(), "alloc_bytes": alloc.bytes, "allocs": alloc.count,
		})
		if err != nil {
			t.Fatal(err)
		}
	}

	s := load(t, loader)
	right := ref(t, s, history.NodeID(m.Compare[1]))
	left := ref(t, s, history.NodeID(m.Compare[0]))
	preview := request{kind: previewRequest, session: s, target: right}
	compare := request{kind: compareRequest, session: s, origin: left, target: right}

	for i := range samples {
		fresh := func() *engine { return &engine{load: loader, limits: limits.Default(), cache: newCache(cacheBudget)} }

		elapsed, alloc := measure(t, func() error { return fresh().perform(t.Context(), request{kind: loadRequest}).err })
		record("load", i, elapsed, alloc)

		e := fresh()
		elapsed, alloc = measure(t, func() error { return e.perform(t.Context(), preview).err })
		record("preview, uncached", i, elapsed, alloc)
		elapsed, alloc = measure(t, func() error { return e.perform(t.Context(), preview).err })
		record("preview, cached", i, elapsed, alloc)

		e = fresh()
		elapsed, alloc = measure(t, func() error { return e.perform(t.Context(), compare).err })
		record("comparison, uncached", i, elapsed, alloc)
		elapsed, alloc = measure(t, func() error { return e.perform(t.Context(), compare).err })
		record("comparison, both states cached", i, elapsed, alloc)

		// Cancel halfway through an uncached comparison and a load, and
		// time how long the operation takes to return after that.
		for _, op := range []struct {
			name string
			r    request
		}{{"comparison", compare}, {"load", request{kind: loadRequest}}} {
			full, _ := measure(t, func() error { return fresh().perform(t.Context(), op.r).err })
			// Stop does not wait for a callback that has started, so the
			// cancellation time arrives on a channel instead of a variable.
			ctx, cancel := context.WithCancel(t.Context())
			stamp := make(chan time.Time, 1)
			timer := time.AfterFunc(full/2, func() { stamp <- time.Now(); cancel() })
			result := fresh().perform(ctx, op.r)
			returned := time.Now()
			fired := !timer.Stop()
			cancel()
			if fired && errors.Is(result.err, context.Canceled) {
				cancelled := <-stamp
				record(op.name+", cancelled halfway", i, returned.Sub(cancelled), allocation{})
			}
		}
	}

	// Retained heap after a comparison fills the cache, and while a reload
	// holds the old load and the new one together.
	e := &engine{load: loader, limits: limits.Default(), cache: newCache(cacheBudget)}
	result := e.perform(t.Context(), compare)
	record("heap after a comparison", 0, 0, allocation{bytes: liveHeap()})
	reloaded := e.perform(t.Context(), request{kind: loadRequest})
	record("heap while reloading", 0, 0, allocation{bytes: liveHeap()})
	runtime.KeepAlive(result)
	runtime.KeepAlive(reloaded)
	runtime.KeepAlive(s)
}

type allocation struct {
	bytes, count uint64
}

// measure runs f once and reports its wall time and what it allocated. A
// collection first keeps an earlier operation's garbage out of the timing.
func measure(t *testing.T, f func() error) (time.Duration, allocation) {
	t.Helper()

	runtime.GC()
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	start := time.Now()
	err := f()
	elapsed := time.Since(start)
	runtime.ReadMemStats(&after)
	if err != nil {
		t.Fatal(err)
	}
	return elapsed, allocation{bytes: after.TotalAlloc - before.TotalAlloc, count: after.Mallocs - before.Mallocs}
}

func liveHeap() uint64 {
	runtime.GC()
	var stats runtime.MemStats
	runtime.ReadMemStats(&stats)
	return stats.HeapAlloc
}

// diskLoader reads a generated workload the way the command's explicit mode
// does, without its change detection: base.txt split at LF, then the undo
// file decoded, validated, and bound to it.
func diskLoader(dir string) Loader {
	return func(ctx context.Context) (*Loaded, error) {
		lim := limits.Default()
		text, err := os.ReadFile(filepath.Join(dir, "base.txt"))
		if err != nil {
			return nil, err
		}
		base := strings.Split(strings.TrimSuffix(string(text), "\n"), "\n")

		undo, err := os.ReadFile(filepath.Join(dir, "history.undo"))
		if err != nil {
			return nil, err
		}
		file, err := undofile.Decode(ctx, "history.undo", bytes.NewReader(undo), lim)
		if err != nil {
			return nil, err
		}
		h, err := history.New(ctx, file, lim)
		if err != nil {
			return nil, err
		}
		verified, err := undofile.VerifyBase(ctx, file, "base.txt", base, lim)
		if err != nil {
			return nil, err
		}
		r, err := history.Bind(h, verified, lim)
		if err != nil {
			return nil, err
		}
		return &Loaded{History: h, Reconstructor: r}, nil
	}
}
