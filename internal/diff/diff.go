// Package diff compares two reconstructed states line by line. Equality uses
// the exact line bytes; terminal escaping belongs to presentation code.
package diff

import (
	"context"
	"errors"
	"iter"
	"slices"

	"github.com/nuggocto/xunhen/internal/history"
	"github.com/nuggocto/xunhen/internal/limits"
)

// ContextLines is the number of unchanged lines kept around each change.
// Changes separated by at most twice this many unchanged lines share a hunk.
const ContextLines = 3

// Op says how a line takes part in the comparison.
type Op uint8

// Line operations, in the order a unified diff prefixes them: " ", "-", "+".
const (
	Equal Op = iota
	Delete
	Insert
)

// Line is one hunk line. Text holds the exact compared bytes, unescaped.
type Line struct {
	Op   Op
	Text string
}

// run is a stretch of lines with one operation. Every run records both
// cursors, so a hunk's first run gives its start on each side.
type run struct {
	op          Op
	left, right int
	count       int
}

// Hunk is one group of changes with its context. Starts are zero-based line
// indexes; counts include context lines.
type Hunk struct {
	LeftStart, LeftCount   int
	RightStart, RightCount int

	runs        []run
	left, right []string
}

// Lines yields the hunk in display order. Within one change, every deleted
// line comes before every inserted line.
func (h Hunk) Lines() iter.Seq[Line] {
	return func(yield func(Line) bool) {
		for _, r := range h.runs {
			for i := range r.count {
				line := Line{Op: r.op}
				if r.op == Insert {
					line.Text = h.right[r.right+i]
				} else {
					line.Text = h.left[r.left+i]
				}

				if !yield(line) {
					return
				}
			}
		}
	}
}

// Diff is a completed comparison. Only Compare creates one, and a failed or
// cancelled comparison returns no Diff at all.
type Diff struct {
	from, to history.NodeRef
	hunks    []Hunk
}

// From identifies the left-hand state.
func (d *Diff) From() history.NodeRef {
	if d == nil {
		return history.NodeRef{}
	}

	return d.from
}

// To identifies the right-hand state.
func (d *Diff) To() history.NodeRef {
	if d == nil {
		return history.NodeRef{}
	}

	return d.to
}

// Hunks returns the hunks in line order. An identical pair has none.
func (d *Diff) Hunks() []Hunk {
	if d == nil {
		return nil
	}

	return slices.Clone(d.hunks)
}

// Compare finds a minimal line diff between two states of one history. It
// charges every budget before the work happens and returns a *LimitError
// naming the exhausted one. Cancellation is checked between bounded units.
func Compare(ctx context.Context, from, to *history.Snapshot, lim limits.Limits) (*Diff, error) {
	if from == nil || to == nil {
		return nil, errors.New("diff requires two snapshots")
	}
	if !from.Node().SameHistory(to.Node()) {
		return nil, errors.New("snapshots belong to different histories")
	}

	// Snapshot.Lines returns copies, so the hunks can own them directly.
	hunks, err := compare(ctx, from.Lines(), to.Lines(), lim)
	if err != nil {
		return nil, err
	}

	return &Diff{from: from.Node(), to: to.Node(), hunks: hunks}, nil
}

// compare owns left and right; the hunks keep references to them.
func compare(ctx context.Context, left, right []string, lim limits.Limits) ([]Hunk, error) {
	if err := lim.Validate(); err != nil {
		return nil, err
	}
	if len(left) > lim.StateLines || len(right) > lim.StateLines {
		return nil, &LimitError{Budget: "state lines"}
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	s := &search{ctx: ctx, limits: lim}
	runs, err := s.script(left, right)
	if err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	return group(runs, left, right), nil
}

// group turns a complete edit script into hunks. The script alternates equal
// runs with changes, because the builder merges adjacent equal runs.
func group(runs []run, left, right []string) []Hunk {
	var hunks []Hunk
	var open []run

	for i, r := range runs {
		if r.op != Equal {
			open = append(open, r)
			continue
		}

		first, last := i == 0, i == len(runs)-1
		switch {
		case first && last:
			// Identical states have nothing to show.
		case first:
			open = append(open, tail(r, ContextLines))
		case last:
			open = append(open, head(r, ContextLines))
		case r.count <= 2*ContextLines:
			open = append(open, r)
		default:
			open = append(open, head(r, ContextLines))
			hunks = append(hunks, newHunk(open, left, right))
			open = []run{tail(r, ContextLines)}
		}
	}

	if len(open) != 0 {
		hunks = append(hunks, newHunk(open, left, right))
	}

	return hunks
}

func head(r run, n int) run {
	r.count = min(r.count, n)
	return r
}

func tail(r run, n int) run {
	skip := r.count - min(r.count, n)
	return run{op: r.op, left: r.left + skip, right: r.right + skip, count: r.count - skip}
}

func newHunk(runs []run, left, right []string) Hunk {
	h := Hunk{
		LeftStart:  runs[0].left,
		RightStart: runs[0].right,
		runs:       runs,
		left:       left,
		right:      right,
	}

	for _, r := range runs {
		if r.op != Insert {
			h.LeftCount += r.count
		}
		if r.op != Delete {
			h.RightCount += r.count
		}
	}

	return h
}
