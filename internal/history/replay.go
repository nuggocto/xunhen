package history

import (
	"context"
	"errors"
	"fmt"
	"slices"

	"github.com/nuggocto/xunhen/internal/limits"
	"github.com/nuggocto/xunhen/internal/undofile"
)

// Reconstructor binds a validated history to its verified reference text.
// Each request owns a fresh workspace, so requests cannot change one another.
type Reconstructor struct {
	history *History
	base    *undofile.VerifiedBase
	limits  limits.Limits
}

// Bind rejects unchecked or foreign base text before replay is available.
func Bind(h *History, base *undofile.VerifiedBase, lim limits.Limits) (*Reconstructor, error) {
	if err := lim.Validate(); err != nil {
		return nil, err
	}
	if err := h.ready(); err != nil {
		return nil, err
	}
	if base == nil || base.File() != h.file {
		return nil, errors.New("history requires a matching verified base")
	}
	if base.LineCount() > lim.StateLines || base.StateBytes() > lim.StateBytes {
		return nil, errors.New("verified base exceeds the state limits")
	}

	return &Reconstructor{history: h, base: base, limits: lim}, nil
}

// Snapshot owns one completed buffer state. Historical file encoding and final
// newline options are unknown; serialization policy belongs to the caller.
type Snapshot struct {
	node  NodeRef
	lines []string
}

// Node identifies this snapshot within its owning history.
func (s *Snapshot) Node() NodeRef {
	if s == nil {
		return NodeRef{}
	}

	return s.node
}

// Lines returns a copy of the logical lines. An empty buffer has one empty
// line, as it does in Neovim.
func (s *Snapshot) Lines() []string {
	if s == nil {
		return nil
	}

	return slices.Clone(s.lines)
}

// Len returns the number of logical lines, at least one for a completed
// state. Together with Line it reads a state without copying it, which a
// viewer redrawing a few lines of a four-million-line state needs.
func (s *Snapshot) Len() int {
	if s == nil {
		return 0
	}

	return len(s.lines)
}

// Line returns one logical line, or false for an out-of-range index.
func (s *Snapshot) Line(index int) (string, bool) {
	if s == nil || index < 0 || index >= len(s.lines) {
		return "", false
	}

	return s.lines[index], true
}

// Reconstruct replays from the reference state to target, applying each
// persisted header once. Headers above the shared ancestor already face undo
// and headers below it on the target branch face redo, so a fresh workspace
// never needs to invert an entry.
func (r *Reconstructor) Reconstruct(ctx context.Context, target NodeRef) (*Snapshot, error) {
	if r == nil || r.history == nil {
		return nil, errors.New("uninitialized reconstructor")
	}

	h := r.history
	if err := h.check(target); err != nil {
		return nil, err
	}

	path, err := h.replayPath(ctx, target.index)
	if err != nil {
		return nil, err
	}

	// VerifiedBase.Lines returns a fresh copy, so the workspace can own it.
	w := &replay{ctx: ctx, history: h, limits: r.limits, text: newText(r.base.Lines(), chunkLines)}
	for _, index := range path {
		if err := w.applyHeader(index); err != nil {
			return nil, err
		}
	}

	if err := ctx.Err(); err != nil {
		return nil, err
	}

	return &Snapshot{node: target, lines: w.text.all()}, nil
}

// replayPath lists headers in application order: up from the reference to the
// shared ancestor, then down to the target. New proved that every parent chain
// ends at the root, and the node limit bounds each walk. A walk can visit a
// million nodes, so it checks cancellation as it goes.
func (h *History) replayPath(ctx context.Context, target int) ([]int, error) {
	onReferencePath := make([]bool, len(h.nodes))
	for step, i := 0, h.reference; ; step, i = step+1, h.parent(i) {
		if err := checkpoint(ctx, step); err != nil {
			return nil, err
		}
		onReferencePath[i] = true
		if i == 0 {
			break
		}
	}

	var down []int
	ancestor := target
	for !onReferencePath[ancestor] {
		if err := checkpoint(ctx, len(down)); err != nil {
			return nil, err
		}
		down = append(down, ancestor)
		ancestor = h.parent(ancestor)
	}

	var path []int
	for i := h.reference; i != ancestor; i = h.parent(i) {
		if err := checkpoint(ctx, len(path)); err != nil {
			return nil, err
		}
		path = append(path, i)
	}

	slices.Reverse(down)
	return append(path, down...), nil
}

// replay is one request's workspace. It needs no work limit of its own: a
// request applies each header on its path once, the decoder bounds the file's
// entries and stored lines, and one entry costs a few chunks plus a scan of
// the chunk list. The state limits bound memory; cancellation stops the rest.
type replay struct {
	ctx     context.Context
	history *History
	limits  limits.Limits
	text    *text
}

func (w *replay) applyHeader(index int) error {
	record := w.history.record(w.history.nodes[index])
	for i := range record.EntryCount() {
		entry, _ := record.Entry(i)
		if err := w.applyEntry(record, i+1, entry); err != nil {
			return err
		}
	}

	return nil
}

// applyEntry replaces the lines strictly between Top and Bottom with the
// entry's stored lines, following Neovim's u_undoredo. Bottom zero means one
// past the last line.
func (w *replay) applyEntry(record undofile.Record, number int, entry undofile.Entry) error {
	if err := w.ctx.Err(); err != nil {
		return err
	}

	count := w.text.lines
	top, bottom := int(entry.Top), int(entry.Bottom)
	if bottom == 0 {
		bottom = count + 1
	}
	if top < 0 || top >= bottom || bottom > count+1 {
		detail := fmt.Sprintf("entry %d spans lines %d-%d of a %d-line state", number, top, bottom, count)
		return w.history.invalid(record, "entry range", detail)
	}

	end := bottom - 1
	size := count - (end - top) + entry.LineCount()
	if size > w.limits.StateLines {
		return w.limit(record, "state lines")
	}

	span := w.text.find(top, end)
	lines := entry.Lines()
	bytes := w.text.bytes - w.text.bytesIn(span) + textBytes(lines)
	if size == 0 {
		lines, bytes = []string{""}, 1 // Neovim keeps one dummy empty line.
	}
	if bytes > w.limits.StateBytes {
		return w.limit(record, "state bytes")
	}

	w.text.replace(span, lines)
	return nil
}

func (w *replay) limit(record undofile.Record, field string) error {
	return w.history.failure(undofile.Limit, record, field, "limit exceeded during replay")
}
