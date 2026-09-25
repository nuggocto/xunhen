package tui

import (
	"cmp"
	"context"
	"fmt"
	"slices"

	"github.com/nuggocto/xunhen/internal/history"
)

// tree is the navigation index of one loaded history, built once in the
// worker. Rows are the history's recorded branch order, which is a preorder:
// a node, then its preferred child's subtree, then its other children's
// subtrees in sibling order. A subtree is therefore one contiguous range of
// positions, which makes collapsing and skipping a subtree a jump rather than
// a walk.
//
// The history stays the source of truth. The index holds four int32 per node
// for navigation and an ID lookup table, 24 bytes per node, and no metadata or
// text. Metadata for a drawn row comes from the history when it is drawn.
type tree struct {
	ids    []history.NodeID
	parent []int32 // parent position; -1 for the root
	end    []int32 // one past the last position of the node's subtree

	// level is the drawing indentation. A preferred child continues its
	// parent's line at the same level, and every other child starts a
	// branch one level deeper, so a long linear history stays flat.
	level []int32

	byID      []positionOfID // sorted by ID
	reference int32
}

type positionOfID struct {
	id       history.NodeID
	position int32
}

// buildTree indexes a validated history.
func buildTree(ctx context.Context, h *history.History) (*tree, error) {
	ref, err := h.Reference()
	if err != nil {
		return nil, err
	}
	info, err := h.Info(ref)
	if err != nil {
		return nil, err
	}

	return indexTree(ctx, h.Count(), info.ID, func(p int) (id, parent history.NodeID, err error) {
		ref, err := h.At(p)
		if err != nil {
			return 0, 0, err
		}
		info, err := h.Info(ref)
		return info.ID, info.Parent, err
	})
}

// indexTree indexes n nodes given in recorded branch order, in O(nodes) and
// with a cancellation check every 4096 nodes. at returns the node at a
// position and its parent's ID; the root's parent is ignored.
func indexTree(ctx context.Context, n int, reference history.NodeID, at func(int) (id, parent history.NodeID, err error)) (*tree, error) {
	t := &tree{
		ids:    make([]history.NodeID, n),
		parent: make([]int32, n),
		end:    make([]int32, n),
		level:  make([]int32, n),
		byID:   make([]positionOfID, n),
	}

	// ancestors holds the positions from the root to the previous node. In
	// a preorder, a node's parent is always on it.
	ancestors := make([]int32, 0, 64)
	for p := range n {
		if p%4096 == 0 {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
		}

		id, parentID, err := at(p)
		if err != nil {
			return nil, err
		}

		position := int32(p)
		t.ids[p] = id
		t.byID[p] = positionOfID{id: id, position: position}

		if p == 0 {
			t.parent[0] = -1
			ancestors = append(ancestors, 0)
			continue
		}

		for len(ancestors) != 0 && t.ids[ancestors[len(ancestors)-1]] != parentID {
			t.end[ancestors[len(ancestors)-1]] = position
			ancestors = ancestors[:len(ancestors)-1]
		}
		if len(ancestors) == 0 {
			panic(fmt.Sprintf("history order puts node %d outside its parent's subtree", id))
		}

		parent := ancestors[len(ancestors)-1]
		t.parent[p] = parent
		t.level[p] = t.level[parent]
		if parent+1 != position {
			t.level[p]++
		}
		ancestors = append(ancestors, position)
	}
	for _, p := range ancestors {
		t.end[p] = int32(n)
	}

	slices.SortFunc(t.byID, func(a, b positionOfID) int { return cmp.Compare(a.id, b.id) })

	position, ok := t.position(reference)
	if !ok {
		panic(fmt.Sprintf("reference node %d missing from the index", reference))
	}
	t.reference = position

	return t, nil
}

func (t *tree) len() int32 {
	return int32(len(t.ids))
}

// position finds a node ID in O(log nodes).
func (t *tree) position(id history.NodeID) (int32, bool) {
	i, found := slices.BinarySearchFunc(t.byID, id, func(entry positionOfID, id history.NodeID) int {
		return cmp.Compare(entry.id, id)
	})
	if !found {
		return 0, false
	}

	return t.byID[i].position, true
}

func (t *tree) hasChildren(p int32) bool {
	return t.end[p] > p+1
}

// branches reports whether p starts an alternate branch: it is a child other
// than its parent's preferred one.
func (t *tree) branches(p int32) bool {
	return p > 0 && t.parent[p]+1 != p
}

// folds is the collapse state of one tree, owned by the model and kept apart
// from the history. A position is hidden when any proper ancestor is folded.
type folds []bool

// next returns the visible row after the visible row p. A folded node's
// subtree is one range, so the row after it is the end of that range; any
// other row is followed by its first child or, failing that, by the end of
// the subtrees it closes. Every ancestor of either candidate is an ancestor of
// p or p itself, so the candidate is visible.
func (t *tree) next(f folds, p int32) (int32, bool) {
	q := p + 1
	if f[p] {
		q = t.end[p]
	}
	if q >= t.len() {
		return p, false
	}

	return q, true
}

// previous returns the visible row before the visible row p. It is p's
// parent, or else the last row drawn for the previous sibling's subtree: the
// outermost folded node on the path down to that subtree's last position.
// The walk is bounded by the depth of that subtree.
func (t *tree) previous(f folds, p int32) (int32, bool) {
	if p == 0 {
		return p, false
	}

	candidate := p - 1
	stop := t.parent[p]
	if candidate == stop {
		return candidate, true
	}

	best := candidate
	for q := t.parent[candidate]; q != stop; q = t.parent[q] {
		if f[q] {
			best = q
		}
	}

	return best, true
}

// last returns the last visible row.
func (t *tree) last(f folds) int32 {
	// Folding hides only descendants, so the last position's outermost
	// folded ancestor, if any, is the last row drawn.
	candidate := t.len() - 1
	best := candidate
	for q := t.parent[candidate]; q >= 0; q = t.parent[q] {
		if f[q] {
			best = q
		}
	}

	return best
}

// shown returns p when it is visible, or else its outermost folded ancestor,
// which is the row that stands for it. The walk is bounded by p's depth.
func (t *tree) shown(f folds, p int32) int32 {
	best := p
	for q := t.parent[p]; q >= 0; q = t.parent[q] {
		if f[q] {
			best = q
		}
	}

	return best
}

// reveal unfolds every ancestor of p, so p becomes visible.
func (t *tree) reveal(f folds, p int32) {
	for q := t.parent[p]; q >= 0; q = t.parent[q] {
		f[q] = false
	}
}
