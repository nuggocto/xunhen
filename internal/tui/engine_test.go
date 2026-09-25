package tui

import (
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/nuggocto/xunhen/internal/history"
	"github.com/nuggocto/xunhen/internal/limits"
)

// Every state Neovim recorded for the checked-in histories must preview
// exactly as Neovim had it, whether it comes from replay or from the cache.
func TestPreviewsMatchNeovim(t *testing.T) {
	t.Parallel()

	entries, err := os.ReadDir(filepath.Join("..", "..", "testdata", "undo"))
	if err != nil {
		t.Fatal(err)
	}

	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}

		t.Run(entry.Name(), func(t *testing.T) {
			t.Parallel()

			f := readFixture(t, entry.Name())
			e := &engine{load: f.loader(), limits: limits.Default(), cache: newCache(cacheBudget)}
			s, err := e.loadSession(t.Context())
			if err != nil {
				t.Fatal(err)
			}

			for pass := range 2 {
				for id, want := range f.states {
					r := e.perform(t.Context(), request{kind: previewRequest, session: s, target: ref(t, s, id)})
					if r.err != nil {
						t.Fatalf("pass %d, node %d: %v", pass, id, r.err)
					}
					if got := documentLines(r.doc); r.doc.id != id || !slices.Equal(got, want) {
						t.Fatalf("pass %d, node %d previews as node %d with %q, want %q", pass, id, r.doc.id, got, want)
					}
				}
			}
		})
	}
}

func TestComparisons(t *testing.T) {
	t.Parallel()

	f := readFixture(t, "abandoned-branch")
	tests := []struct {
		name     string
		from, to history.NodeID
		// want lists the comparison rows as a unified diff shows them,
		// without hunk headers.
		want []string
	}{
		{
			name: "chosen fix against the abandoned experiment", from: 3, to: 2,
			want: []string{" package sample", " ", "-func chosen() int { return 1 }", "+func experiment() int { return 42 }"},
		},
		{
			name: "the same pair in the other direction", from: 2, to: 3,
			want: []string{" package sample", " ", "-func experiment() int { return 42 }", "+func chosen() int { return 1 }"},
		},
		{name: "a state against itself", from: 2, to: 2},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			e := &engine{load: f.loader(), limits: limits.Default(), cache: newCache(cacheBudget)}
			s, err := e.loadSession(t.Context())
			if err != nil {
				t.Fatal(err)
			}

			r := e.perform(t.Context(), request{kind: compareRequest, session: s, origin: ref(t, s, tt.from), target: ref(t, s, tt.to)})
			if r.err != nil {
				t.Fatal(r.err)
			}
			if r.cmp.from.id != tt.from || r.cmp.to.id != tt.to {
				t.Fatalf("compared node %d -> node %d, want %d -> %d", r.cmp.from.id, r.cmp.to.id, tt.from, tt.to)
			}
			if got := comparisonText(r.cmp); !slices.Equal(got, tt.want) {
				t.Fatalf("comparison rows %q, want %q", got, tt.want)
			}
		})
	}
}

// comparisonText reads every row of a comparison through its row index, the
// way the view does, and drops the hunk headers.
func comparisonText(c *comparison) []string {
	var rows []string
	for r := range c.rows {
		hunk, i := c.row(r)
		if i < 0 {
			continue
		}
		line, _ := c.hunks[hunk].Line(i)
		rows = append(rows, " -+"[line.Op:line.Op+1]+line.Text)
	}
	return rows
}

// The row index must visit each hunk's header and then its lines, in order,
// for a comparison with several hunks. Scrolling depends on it.
func TestComparisonRowIndex(t *testing.T) {
	t.Parallel()

	left := make([]string, 40)
	for i := range left {
		left[i] = string(rune('a' + i%26))
	}
	right := slices.Clone(left)
	right[2], right[20], right[38] = "changed", "changed", "changed"

	s := load(t, loaderOf(chain([][]string{left, right})))
	e := &engine{limits: limits.Default(), cache: newCache(cacheBudget)}
	out := e.perform(t.Context(), request{kind: compareRequest, session: s, origin: ref(t, s, 0), target: ref(t, s, 1)})
	if out.err != nil {
		t.Fatal(out.err)
	}
	c := out.cmp
	if len(c.hunks) != 3 {
		t.Fatalf("%d hunks, want 3", len(c.hunks))
	}

	r := 0
	for i, h := range c.hunks {
		for line := -1; line < h.Len(); line++ {
			if gotHunk, gotLine := c.row(r); gotHunk != i || gotLine != line {
				t.Fatalf("row %d is hunk %d line %d, want hunk %d line %d", r, gotHunk, gotLine, i, line)
			}
			r++
		}
	}
	if r != c.rows {
		t.Fatalf("%d rows visited, comparison has %d", r, c.rows)
	}
}

// A reload builds a new session. A node reference from the old one must not
// resolve in it, even when the numeric ID exists in both.
func TestReloadedSessionsShareNothing(t *testing.T) {
	t.Parallel()

	f := readFixture(t, "abandoned-branch")
	before, after := load(t, f.loader()), load(t, f.loader())

	old := ref(t, before, 2)
	if _, err := after.history.Info(old); err == nil {
		t.Fatal("the new history accepted a node reference from the old one")
	}
	if _, err := after.reconstructor.Reconstruct(t.Context(), old); err == nil {
		t.Fatal("the new reconstructor replayed a node reference from the old history")
	}
}
