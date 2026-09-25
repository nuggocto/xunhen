package tui

import (
	"slices"
	"strings"
	"testing"

	"github.com/nuggocto/xunhen/internal/history"
	"github.com/nuggocto/xunhen/internal/limits"
	"github.com/nuggocto/xunhen/internal/termtext"
)

// The cache must hold both sides of a comparison of the largest states. The
// largest document has the most lines the limits allow, and as many lines
// long enough for a column index as the remaining state bytes can hold, so
// its line array and its indexes are both at their maximum.
func TestCacheHoldsTheLargestPair(t *testing.T) {
	lim := limits.Default()
	long := strings.Repeat("x", 4097)
	longLines := (lim.StateBytes - lim.StateLines) / len(long)

	lines := make([]string, lim.StateLines)
	for i := range longLines {
		lines[i] = long
	}

	s := load(t, loaderOf(rootOnly(lines), lines))
	e := &engine{limits: lim, cache: newCache(cacheBudget)}
	doc, err := e.document(t.Context(), s, ref(t, s, 0))
	if err != nil {
		t.Fatal(err)
	}
	if len(doc.long) != longLines {
		t.Fatalf("%d lines indexed, want %d", len(doc.long), longLines)
	}

	if pair := 2 * (doc.charge() + entryOverhead); pair > cacheBudget {
		t.Fatalf("two of the largest documents charge %d bytes, over the %d byte budget", pair, cacheBudget)
	}
}

func TestCacheEviction(t *testing.T) {
	t.Parallel()

	f := readFixture(t, "abandoned-branch")

	tests := []struct {
		name string
		// run fills a cache that holds two of the fixture's documents and
		// returns the IDs it should still hold and the ones it should not.
		run        func(t *testing.T, c *cache, s *session, doc func(history.NodeID) *document) (kept, dropped []history.NodeID)
		budgetDocs int
	}{
		{
			name: "the least recently used document goes first", budgetDocs: 2,
			run: func(t *testing.T, c *cache, s *session, doc func(history.NodeID) *document) ([]history.NodeID, []history.NodeID) {
				c.put(s, ref(t, s, 1), doc(1))
				c.put(s, ref(t, s, 2), doc(2))
				c.get(s, ref(t, s, 1))
				c.put(s, ref(t, s, 3), doc(3))
				return []history.NodeID{1, 3}, []history.NodeID{2}
			},
		},
		{
			name: "a document larger than the budget is not kept", budgetDocs: 0,
			run: func(t *testing.T, c *cache, s *session, doc func(history.NodeID) *document) ([]history.NodeID, []history.NodeID) {
				c.put(s, ref(t, s, 1), doc(1))
				return nil, []history.NodeID{1}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			s := load(t, f.loader())
			documents := map[history.NodeID]*document{}
			doc := func(id history.NodeID) *document {
				if d, ok := documents[id]; ok {
					return d
				}
				d := prepared(t, s, id)
				documents[id] = d
				return d
			}

			// Every document of this fixture has three short lines, so
			// they all charge the same.
			c := newCache(tt.budgetDocs * (doc(0).charge() + entryOverhead))
			kept, dropped := tt.run(t, c, s, doc)

			for _, id := range kept {
				if got, ok := c.get(s, ref(t, s, id)); !ok || got != documents[id] {
					t.Fatalf("node %d was evicted", id)
				}
			}
			for _, id := range dropped {
				if _, ok := c.get(s, ref(t, s, id)); ok {
					t.Fatalf("node %d is still cached", id)
				}
			}
		})
	}
}

// Entries belong to one load. A document cached for an earlier load must
// never answer for a later one, even for the same node ID.
func TestCacheIsScopedToOneLoad(t *testing.T) {
	t.Parallel()

	f := readFixture(t, "abandoned-branch")
	first, second := load(t, f.loader()), load(t, f.loader())

	c := newCache(cacheBudget)
	c.put(first, ref(t, first, 2), prepared(t, first, 2))
	if _, ok := c.get(second, ref(t, second, 2)); ok {
		t.Fatal("the second load found the first load's document")
	}

	c.put(second, ref(t, second, 3), prepared(t, second, 3))
	if _, ok := c.get(first, ref(t, first, 2)); ok {
		t.Fatal("the first load's document survived a document of the second load")
	}
}

// Evicting a document removes only the cache's reference. The view may still
// be drawing it, so its text must stay exactly as reconstructed.
func TestEvictedDocumentsStayIntact(t *testing.T) {
	t.Parallel()

	f := readFixture(t, "abandoned-branch")
	s := load(t, f.loader())
	shown := prepared(t, s, 2)

	c := newCache(shown.charge() + entryOverhead)
	c.put(s, ref(t, s, 2), shown)
	for _, id := range []history.NodeID{0, 1, 3} {
		c.put(s, ref(t, s, id), prepared(t, s, id))
	}

	if _, ok := c.get(s, ref(t, s, 2)); ok {
		t.Fatal("node 2 was not evicted")
	}
	if got := documentLines(shown); !slices.Equal(got, f.states[2]) {
		t.Fatalf("evicted document now reads %q, want %q", got, f.states[2])
	}
}

func prepared(t *testing.T, s *session, id history.NodeID) *document {
	t.Helper()

	snapshot, err := s.reconstructor.Reconstruct(t.Context(), ref(t, s, id))
	if err != nil {
		t.Fatal(err)
	}
	d, err := prepareDocument(t.Context(), id, snapshot, termtext.Runes)
	if err != nil {
		t.Fatal(err)
	}
	return d
}

func documentLines(d *document) []string {
	lines := make([]string, d.len())
	for i := range lines {
		lines[i], _ = d.line(i)
	}
	return lines
}
