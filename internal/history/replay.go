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
		return nil, errors.New("verified base exceeds reconstruction budget")
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

	w := &replay{ctx: ctx, history: h, limits: r.limits}
	if err := w.charge(undofile.Record{}, r.base.LineCount()); err != nil {
		return nil, err
	}
	w.lines, w.bytes = r.base.Lines(), r.base.StateBytes()

	for _, index := range h.replayPath(target.index) {
		if err := w.applyHeader(index); err != nil {
			return nil, err
		}
	}

	if err := ctx.Err(); err != nil {
		return nil, err
	}

	return &Snapshot{node: target, lines: w.lines}, nil
}

// replayPath lists headers in application order: up from the reference to the
// shared ancestor, then down to the target. New proved that every parent chain
// ends at the root, and the node budget bounds each walk.
func (h *History) replayPath(target int) []int {
	onReferencePath := make([]bool, len(h.nodes))
	for i := h.reference; ; i = h.parent(i) {
		onReferencePath[i] = true
		if i == 0 {
			break
		}
	}

	var down []int
	ancestor := target
	for !onReferencePath[ancestor] {
		down = append(down, ancestor)
		ancestor = h.parent(ancestor)
	}

	var path []int
	for i := h.reference; i != ancestor; i = h.parent(i) {
		path = append(path, i)
	}

	slices.Reverse(down)
	return append(path, down...)
}

// replay is one request's workspace. Every budget is charged before its work.
type replay struct {
	ctx     context.Context
	history *History
	limits  limits.Limits

	lines []string
	bytes int // each line plus its logical terminator

	headers, entries, moves int
}

func (w *replay) applyHeader(index int) error {
	record := w.history.record(w.history.nodes[index])
	if w.headers == w.limits.ReplayHeaders {
		return w.limit(record, "replay headers")
	}
	w.headers++

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
	if w.entries == w.limits.ReplayEntries {
		return w.limit(record, "replay entries")
	}
	w.entries++

	top, bottom := int(entry.Top), int(entry.Bottom)
	if bottom == 0 {
		bottom = len(w.lines) + 1
	}
	if top < 0 || top >= bottom || bottom > len(w.lines)+1 {
		detail := fmt.Sprintf("entry %d spans lines %d-%d of a %d-line state", number, top, bottom, len(w.lines))
		return w.history.invalid(record, "entry range", detail)
	}

	end := bottom - 1
	removed := w.lines[top:end]
	added := entry.LineCount()
	tail := len(w.lines) - end

	size := len(w.lines) - len(removed) + added
	if size > w.limits.StateLines {
		return w.limit(record, "state lines")
	}

	// Visit the removed lines, copy the added ones, and shift the tail.
	if err := w.charge(record, len(removed)+added+tail); err != nil {
		return err
	}

	lines := entry.Lines()
	bytes := w.bytes - textBytes(removed) + textBytes(lines)
	if size == 0 {
		lines, bytes = []string{""}, 1 // Neovim keeps one dummy empty line.
	}
	if bytes > w.limits.StateBytes {
		return w.limit(record, "state bytes")
	}

	w.lines = slices.Replace(w.lines, top, end, lines...)
	w.bytes = bytes
	return nil
}

func (w *replay) charge(record undofile.Record, moves int) error {
	if moves > w.limits.ReplayMoves-w.moves {
		return w.limit(record, "replay line moves")
	}

	w.moves += moves
	return nil
}

func (w *replay) limit(record undofile.Record, field string) error {
	return w.history.failure(undofile.Limit, record, field, "budget exceeded during replay")
}

func textBytes(lines []string) int {
	total := 0
	for _, line := range lines {
		total += len(line) + 1
	}

	return total
}
