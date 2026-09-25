package tui

import (
	"context"
	"errors"
	"slices"
	"testing"

	"github.com/nuggocto/xunhen/internal/history"
)

// node is one row of a synthetic history in recorded branch order.
type node struct {
	id, parent history.NodeID
}

func treeOf(t *testing.T, reference history.NodeID, nodes []node) *tree {
	t.Helper()

	tr, err := indexTree(t.Context(), len(nodes), reference, func(p int) (history.NodeID, history.NodeID, error) {
		return nodes[p].id, nodes[p].parent, nil
	})
	if err != nil {
		t.Fatal(err)
	}

	return tr
}

// sample is this tree, listed in branch order. IDs differ from positions so
// a test cannot pass by confusing the two.
//
//	0 ── 10 ── 20 ── 30
//	 │     └── 40 ── 50
//	 └── 60
var sample = []node{{0, 0}, {10, 0}, {20, 10}, {30, 20}, {40, 10}, {50, 40}, {60, 0}}

func TestTreeNavigation(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		folded []history.NodeID
		from   history.NodeID
		move   string // next, previous, last, or shown
		want   history.NodeID
		moved  bool
	}{
		{name: "next enters the preferred child", from: 10, move: "next", want: 20, moved: true},
		{name: "next leaves a finished branch", from: 30, move: "next", want: 40, moved: true},
		{name: "next skips a folded subtree", folded: []history.NodeID{20}, from: 20, move: "next", want: 40, moved: true},
		{name: "next skips a folded branch point", folded: []history.NodeID{10}, from: 10, move: "next", want: 60, moved: true},
		{name: "next stops at the last row", from: 60, move: "next", want: 60},
		{name: "previous returns to the parent", from: 20, move: "previous", want: 10, moved: true},
		{name: "previous lands on the last row of a sibling", from: 40, move: "previous", want: 30, moved: true},
		{name: "previous lands on a folded sibling", folded: []history.NodeID{20}, from: 40, move: "previous", want: 20, moved: true},
		{name: "previous picks the outermost fold", folded: []history.NodeID{10, 40}, from: 60, move: "previous", want: 10, moved: true},
		{name: "previous stops at the root", from: 0, move: "previous", want: 0},
		{name: "last row without folds", move: "last", want: 60},
		{name: "last row under a fold", folded: []history.NodeID{0}, move: "last", want: 0},
		{name: "a hidden node shows as its outermost fold", folded: []history.NodeID{10, 40}, from: 50, move: "shown", want: 10},
		{name: "a visible node shows as itself", folded: []history.NodeID{40}, from: 30, move: "shown", want: 30},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			tr := treeOf(t, 30, sample)
			f := make(folds, tr.len())
			for _, id := range tt.folded {
				f[at(t, tr, id)] = true
			}

			var got int32
			moved := false
			switch tt.move {
			case "next":
				got, moved = tr.next(f, at(t, tr, tt.from))
			case "previous":
				got, moved = tr.previous(f, at(t, tr, tt.from))
			case "last":
				got = tr.last(f)
			case "shown":
				got = tr.shown(f, at(t, tr, tt.from))
			}

			if tr.ids[got] != tt.want || moved != tt.moved {
				t.Fatalf("%s from %d = node %d (moved %t), want node %d (moved %t)",
					tt.move, tt.from, tr.ids[got], moved, tt.want, tt.moved)
			}
		})
	}
}

func at(t *testing.T, tr *tree, id history.NodeID) int32 {
	t.Helper()

	p, ok := tr.position(id)
	if !ok {
		t.Fatalf("node %d is not in the tree", id)
	}
	return p
}

// Walking down and back up through the visible rows must visit the same rows
// in opposite orders, whatever is folded. The two directions use different
// algorithms, so any disagreement is a bug in one of them.
func TestTreeWalksAgree(t *testing.T) {
	t.Parallel()

	tr := treeOf(t, 0, sample)
	for mask := range 1 << len(sample) {
		f := make(folds, tr.len())
		for p := range f {
			f[p] = mask&(1<<p) != 0
		}

		down := []int32{0}
		for p, ok := tr.next(f, 0); ok; p, ok = tr.next(f, p) {
			down = append(down, p)
		}

		up := []int32{tr.last(f)}
		for p, ok := tr.previous(f, up[0]); ok; p, ok = tr.previous(f, p) {
			up = append(up, p)
		}
		slices.Reverse(up)

		if !slices.Equal(down, up) {
			t.Fatalf("folds %07b: down %v, up %v", mask, down, up)
		}
		for _, p := range down {
			if tr.shown(f, p) != p {
				t.Fatalf("folds %07b: visited row %d is hidden", mask, p)
			}
		}
	}
}

func TestTreeStructure(t *testing.T) {
	t.Parallel()

	tr := treeOf(t, 30, sample)
	tests := []struct {
		name      string
		id        history.NodeID
		level     int32
		branches  bool
		reference bool
	}{
		{name: "root", id: 0},
		{name: "preferred child continues its parent's level", id: 20},
		{name: "reference", id: 30, reference: true},
		{name: "alternate child starts a branch", id: 40, level: 1, branches: true},
		{name: "preferred child inside a branch", id: 50, level: 1},
		{name: "alternate child of the root", id: 60, level: 1, branches: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			p := at(t, tr, tt.id)
			if tr.level[p] != tt.level || tr.branches(p) != tt.branches || (p == tr.reference) != tt.reference {
				t.Fatalf("node %d: level %d, branches %t, reference %t", tt.id, tr.level[p], tr.branches(p), p == tr.reference)
			}
		})
	}

	t.Run("revealing a hidden node unfolds its ancestors", func(t *testing.T) {
		t.Parallel()

		f := make(folds, tr.len())
		f[at(t, tr, 0)], f[at(t, tr, 10)], f[at(t, tr, 40)] = true, true, true
		tr.reveal(f, at(t, tr, 50))
		if tr.shown(f, at(t, tr, 50)) != at(t, tr, 50) {
			t.Fatal("node 50 is still hidden")
		}
	})

	t.Run("unknown IDs are not found", func(t *testing.T) {
		t.Parallel()

		if _, ok := tr.position(15); ok {
			t.Fatal("found node 15")
		}
	})
}

// A long linear history, the common shape of an undo tree, must index and
// walk without recursion and stay at level zero.
func TestDeepTrees(t *testing.T) {
	t.Parallel()

	const count = 200_000
	tests := []struct {
		name      string
		nodes     func() []node
		lastLevel int32
	}{
		{name: "linear history", nodes: func() []node {
			nodes := make([]node, count)
			for i := range nodes {
				nodes[i] = node{id: history.NodeID(i), parent: history.NodeID(max(i-1, 0))}
			}
			return nodes
		}},
		// Each stair node has a leaf as its preferred child and the next
		// stair as its second child, so every step starts a deeper branch.
		{name: "staircase of branches", nodes: func() []node {
			// Even IDs are the stair nodes and odd IDs their leaves.
			nodes := make([]node, count)
			for i := 1; i < count; i++ {
				parent := i - 2
				if i%2 == 1 {
					parent = i - 1
				}
				nodes[i] = node{id: history.NodeID(i), parent: history.NodeID(parent)}
			}
			return nodes
		}, lastLevel: count/2 - 1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			tr := treeOf(t, 0, tt.nodes())
			f := make(folds, tr.len())
			last := tr.last(f)
			if last != count-1 || tr.level[last] != tt.lastLevel {
				t.Fatalf("last row %d at level %d, want %d at level %d", last, tr.level[last], count-1, tt.lastLevel)
			}

			// Folding the root hides everything in one step.
			f[0] = true
			if p, ok := tr.next(f, 0); ok {
				t.Fatalf("row %d follows the folded root", p)
			}
		})
	}
}

func TestTreeIndexingStopsWhenCancelled(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	_, err := indexTree(ctx, len(sample), 0, func(p int) (history.NodeID, history.NodeID, error) {
		return sample[p].id, sample[p].parent, nil
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v, want cancellation", err)
	}
}
