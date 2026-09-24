package diff_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/nuggocto/xunhen/internal/diff"
	"github.com/nuggocto/xunhen/internal/history"
	"github.com/nuggocto/xunhen/internal/limits"
	"github.com/nuggocto/xunhen/internal/undofile"
)

// render writes hunks in a small test-only notation: a header with zero-based
// starts and counts, then one prefixed line per hunk line.
func render(hunks []diff.Hunk) string {
	var out strings.Builder
	for _, h := range hunks {
		fmt.Fprintf(&out, "@@ %d,%d %d,%d\n", h.LeftStart, h.LeftCount, h.RightStart, h.RightCount)
		for line := range h.Lines() {
			out.WriteByte(" -+"[line.Op])
			out.WriteString(line.Text)
			out.WriteByte('\n')
		}
	}

	return out.String()
}

func numbered(n int) []string {
	lines := make([]string, n)
	for i := range lines {
		lines[i] = fmt.Sprint(i + 1)
	}

	return lines
}

func replaced(lines []string, changes map[int]string) []string {
	out := slices.Clone(lines)
	for i, line := range changes {
		out[i] = line
	}

	return out
}

func TestKnownHunks(t *testing.T) {
	t.Parallel()

	long := strings.Repeat("x", 1<<20)
	ten := numbered(10)
	twenty := numbered(20)

	tests := []struct {
		name        string
		left, right []string
		want        string
	}{
		{name: "identical", left: []string{"a", "b"}, right: []string{"a", "b"}, want: ""},
		{name: "both empty", want: ""},
		{name: "empty buffers", left: []string{""}, right: []string{""}, want: ""},
		{name: "insert into nothing", right: []string{"a"}, want: "@@ 0,0 0,1\n+a\n"},
		{name: "empty buffer gains text", left: []string{""}, right: []string{"a"}, want: "@@ 0,1 0,1\n-\n+a\n"},
		{name: "insert at start", left: []string{"b", "c"}, right: []string{"a", "b", "c"}, want: "@@ 0,2 0,3\n+a\n b\n c\n"},
		{name: "delete at end", left: []string{"a", "b", "c"}, right: []string{"a", "b"}, want: "@@ 0,3 0,2\n a\n b\n-c\n"},
		{
			name: "deletions before insertions",
			left: []string{"a", "b"}, right: []string{"x", "y"},
			want: "@@ 0,2 0,2\n-a\n-b\n+x\n+y\n",
		},
		{
			// With three equal lines, the prefix match keeps the first two.
			name: "repeated lines",
			left: []string{"a", "a", "a"}, right: []string{"a", "a"},
			want: "@@ 0,3 0,2\n a\n a\n-a\n",
		},
		{
			name: "context is three lines",
			left: ten, right: replaced(ten, map[int]string{4: "X"}),
			want: "@@ 1,7 1,7\n 2\n 3\n 4\n-5\n+X\n 6\n 7\n 8\n",
		},
		{
			name: "six unchanged lines join hunks",
			left: twenty, right: replaced(twenty, map[int]string{2: "X", 9: "Y"}),
			want: "@@ 0,13 0,13\n 1\n 2\n-3\n+X\n 4\n 5\n 6\n 7\n 8\n 9\n-10\n+Y\n 11\n 12\n 13\n",
		},
		{
			name: "seven unchanged lines split hunks",
			left: twenty, right: replaced(twenty, map[int]string{2: "X", 10: "Y"}),
			want: "@@ 0,6 0,6\n 1\n 2\n-3\n+X\n 4\n 5\n 6\n" +
				"@@ 7,7 7,7\n 8\n 9\n 10\n-11\n+Y\n 12\n 13\n 14\n",
		},
		{
			name: "long line differing in its last byte",
			left: []string{long + "a"}, right: []string{long + "b"},
			want: "@@ 0,1 0,1\n-" + long + "a\n+" + long + "b\n",
		},
		{
			// Equality uses raw bytes, so an ESC and its escaped spelling differ.
			name: "control byte versus its escape",
			left: []string{"a\x1b"}, right: []string{`a\x1b`},
			want: "@@ 0,1 0,1\n-a\x1b\n+a\\x1b\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			hunks, err := diff.Lines(t.Context(), tt.left, tt.right, limits.Default())
			if err != nil {
				t.Fatal(err)
			}
			if got := render(hunks); got != tt.want {
				t.Fatalf("hunks =\n%s\nwant\n%s", got, tt.want)
			}
			checkDiff(t, tt.left, tt.right, hunks)
		})
	}
}

// TestSmallInputsExhaustively compares every pair of short sequences over a
// small alphabet. Three lines up to four long give 121 sequences and 14,641
// pairs. Repeated lines are common at this size, which is where alignment
// choices matter.
func TestSmallInputsExhaustively(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		alphabet  []string
		maxLength int
	}{
		{name: "alphabet of three, up to four lines", alphabet: []string{"a", "b", "c"}, maxLength: 4},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			sequences := [][]string{nil}
			for length := 1; length <= tt.maxLength; length++ {
				for _, prefix := range sequences {
					if len(prefix) != length-1 {
						continue
					}
					for _, line := range tt.alphabet {
						sequences = append(sequences, append(slices.Clone(prefix), line))
					}
				}
			}

			for _, left := range sequences {
				for _, right := range sequences {
					hunks, err := diff.Lines(t.Context(), left, right, limits.Default())
					if err != nil {
						t.Fatalf("%q -> %q: %v", left, right, err)
					}
					checkDiff(t, left, right, hunks)
				}
			}
		})
	}
}

// checkDiff verifies the hunk contract without trusting the implementation:
// applying the hunks to left yields right, the edit count equals the one
// implied by an independent longest-common-subsequence computation, and the
// context and ordering rules hold.
func checkDiff(t *testing.T, left, right []string, hunks []diff.Hunk) {
	t.Helper()

	var result []string
	cursor, edits := 0, 0

	for i, h := range hunks {
		if h.LeftStart < cursor || (i > 0 && h.LeftStart == cursor) {
			t.Fatalf("%q -> %q: hunk %d overlaps or touches the previous one", left, right, i)
		}
		result = append(result, left[cursor:h.LeftStart]...)
		if h.RightStart != len(result) {
			t.Fatalf("%q -> %q: hunk %d right start %d, want %d", left, right, i, h.RightStart, len(result))
		}

		index, produced, changes := h.LeftStart, 0, 0
		var ops []diff.Op
		for line := range h.Lines() {
			ops = append(ops, line.Op)
			switch line.Op {
			case diff.Insert:
				result = append(result, line.Text)
				produced++
				changes++
			case diff.Delete, diff.Equal:
				if index >= len(left) || left[index] != line.Text {
					t.Fatalf("%q -> %q: hunk %d does not match the left text at %d", left, right, i, index)
				}
				if line.Op == diff.Equal {
					result = append(result, line.Text)
					produced++
				} else {
					changes++
				}
				index++
			}
		}

		if index-h.LeftStart != h.LeftCount || produced != h.RightCount {
			t.Fatalf("%q -> %q: hunk %d counts do not match its lines", left, right, i)
		}
		if changes == 0 {
			t.Fatalf("%q -> %q: hunk %d has no change", left, right, i)
		}
		checkContext(t, ops)
		edits += changes
		cursor = h.LeftStart + h.LeftCount
	}

	result = append(result, left[cursor:]...)
	if !slices.Equal(result, right) {
		t.Fatalf("%q -> %q: applying hunks gave %q", left, right, result)
	}
	if want := len(left) + len(right) - 2*lcs(left, right); edits != want {
		t.Fatalf("%q -> %q: %d edits, minimal is %d", left, right, edits, want)
	}
}

// checkContext requires at most three leading and trailing context lines, at
// most six between changes, and deletions before insertions in each change.
func checkContext(t *testing.T, ops []diff.Op) {
	t.Helper()

	lead := slices.IndexFunc(ops, func(op diff.Op) bool { return op != diff.Equal })
	trail := len(ops) - 1 - lastChange(ops)
	if lead > diff.ContextLines || trail > diff.ContextLines {
		t.Fatalf("context %d before and %d after exceeds %d", lead, trail, diff.ContextLines)
	}

	equal := 0
	for i, op := range ops {
		if op == diff.Equal {
			equal++
			continue
		}
		if equal > 2*diff.ContextLines && i > equal {
			t.Fatalf("%d unchanged lines inside one hunk", equal)
		}
		if op == diff.Delete && i > 0 && ops[i-1] == diff.Insert {
			t.Fatal("deletion follows an insertion within one change")
		}
		equal = 0
	}
}

func lastChange(ops []diff.Op) int {
	for i, op := range slices.Backward(ops) {
		if op != diff.Equal {
			return i
		}
	}

	return -1
}

// lcs is the textbook O(n*m) dynamic program, independent of the diff search.
func lcs(left, right []string) int {
	row := make([]int, len(right)+1)
	for _, l := range left {
		diagonal := 0
		for j, r := range right {
			above := row[j+1]
			if l == r {
				row[j+1] = diagonal + 1
			} else {
				row[j+1] = max(row[j+1], row[j])
			}
			diagonal = above
		}
	}

	return row[len(right)]
}

func TestBudgets(t *testing.T) {
	t.Parallel()

	// "a b" to "b a" runs a two-round search. Steps: round 0 visits one
	// diagonal; round 1 visits two and follows one matched line on each; round
	// 2 reaches the corner on its second diagonal. That is 1 + 4 + 2 = 7.
	swap := [2][]string{{"a", "b"}, {"b", "a"}}
	// Identical three-byte lines cost three compared bytes, found by the
	// common-prefix scan.
	same := [2][]string{{"abc"}, {"abc"}}

	tests := []struct {
		name   string
		input  [2][]string
		set    func(*limits.Limits)
		budget string
	}{
		{name: "steps at limit", input: swap, set: func(l *limits.Limits) { l.DiffSteps = 7 }},
		{name: "steps above limit", input: swap, set: func(l *limits.Limits) { l.DiffSteps = 6 }, budget: "diff steps"},
		{name: "compared bytes at limit", input: same, set: func(l *limits.Limits) { l.DiffCompareBytes = 3 }},
		{name: "compared bytes above limit", input: same, set: func(l *limits.Limits) { l.DiffCompareBytes = 2 }, budget: "diff compared bytes"},
		{name: "workspace", input: swap, set: func(l *limits.Limits) { l.DiffWorkspaceBytes = 16 }, budget: "diff workspace bytes"},
		{name: "state lines", input: swap, set: func(l *limits.Limits) { l.StateLines = 1 }, budget: "state lines"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			lim := limits.Default()
			tt.set(&lim)

			hunks, err := diff.Lines(t.Context(), tt.input[0], tt.input[1], lim)
			if tt.budget == "" {
				if err != nil {
					t.Fatalf("within budget: %v", err)
				}
				checkDiff(t, tt.input[0], tt.input[1], hunks)
				return
			}

			var problem *diff.LimitError
			if hunks != nil || !errors.As(err, &problem) || problem.Budget != tt.budget {
				t.Fatalf("hunks = %v, error = %v; want the %s budget", hunks, err, tt.budget)
			}
		})
	}
}

func TestLargeChanges(t *testing.T) {
	t.Parallel()

	// Fresh text on both sides leaves nothing to search after filtering.
	rewrite := func() ([]string, []string) {
		left, right := make([]string, 5000), make([]string, 5000)
		for i := range left {
			left[i], right[i] = fmt.Sprint("old ", i), fmt.Sprint("new ", i)
		}
		return left, right
	}
	// A shared line between every pair of moved lines forces about 3,000
	// edits, past the stored-round ceiling of the workspace budget.
	shuffle := func() ([]string, []string) {
		var left, right []string
		for i := range 1500 {
			left = append(left, "shared", fmt.Sprint("moved ", i))
			right = append(right, fmt.Sprint("moved ", 1499-i), "shared")
		}
		return left, right
	}

	tests := []struct {
		name   string
		input  func() ([]string, []string)
		budget string
	}{
		{name: "complete rewrite", input: rewrite},
		{name: "edit distance above the ceiling", input: shuffle, budget: "diff workspace bytes"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			left, right := tt.input()
			first, err := diff.Lines(t.Context(), left, right, limits.Default())
			if tt.budget != "" {
				var problem *diff.LimitError
				if first != nil || !errors.As(err, &problem) || problem.Budget != tt.budget {
					t.Fatalf("hunks = %d, error = %v; want the %s budget", len(first), err, tt.budget)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			checkDiff(t, left, right, first)

			second, err := diff.Lines(t.Context(), left, right, limits.Default())
			if err != nil || render(second) != render(first) {
				t.Fatalf("repeated comparison differs: %v", err)
			}
		})
	}
}

// cancelAfter reports cancellation once Err has been called a set number of
// times, which cancels at a chosen checkpoint without timing.
type cancelAfter struct {
	context.Context
	calls, limit int
}

func (c *cancelAfter) Err() error {
	c.calls++
	if c.calls > c.limit {
		return context.Canceled
	}

	return nil
}

func TestCancellation(t *testing.T) {
	t.Parallel()

	left, right := numbered(50), slices.Concat(numbered(50)[25:], numbered(50)[:25])

	tests := []struct {
		name   string
		checks int
	}{
		{name: "before work", checks: 0},
		{name: "after indexing", checks: 3},
		{name: "during the search", checks: 10},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			ctx := &cancelAfter{Context: t.Context(), limit: tt.checks}
			hunks, err := diff.Lines(ctx, left, right, limits.Default())
			if hunks != nil || !errors.Is(err, context.Canceled) {
				t.Fatalf("hunks = %d, error = %v; want cancellation", len(hunks), err)
			}
		})
	}
}

func fixtureSnapshots(t *testing.T, name string, nodes ...history.NodeID) []*history.Snapshot {
	t.Helper()

	dir := filepath.Join("../../testdata/undo", name)
	undo, err := os.ReadFile(filepath.Join(dir, "history.undo"))
	if err != nil {
		t.Fatal(err)
	}
	base, err := os.ReadFile(filepath.Join(dir, "base.bin"))
	if err != nil {
		t.Fatal(err)
	}

	lim := limits.Default()
	file, err := undofile.Decode(t.Context(), name, bytes.NewReader(undo), lim)
	if err != nil {
		t.Fatal(err)
	}
	h, err := history.New(t.Context(), file, lim)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSuffix(string(base), "\n"), "\n")
	verified, err := undofile.VerifyBase(t.Context(), file, name, lines, lim)
	if err != nil {
		t.Fatal(err)
	}
	r, err := history.Bind(h, verified, lim)
	if err != nil {
		t.Fatal(err)
	}

	var out []*history.Snapshot
	for _, id := range nodes {
		ref, err := h.Lookup(id)
		if err != nil {
			t.Fatal(err)
		}
		snapshot, err := r.Reconstruct(t.Context(), ref)
		if err != nil {
			t.Fatal(err)
		}
		out = append(out, snapshot)
	}

	return out
}

func TestCompareStates(t *testing.T) {
	t.Parallel()

	states := fixtureSnapshots(t, "abandoned-branch", 2, 3)
	foreign := fixtureSnapshots(t, "abandoned-branch", 3)[0]

	tests := []struct {
		name     string
		from, to *history.Snapshot
		want     string
		wantErr  string
	}{
		{
			name: "abandoned experiment against the chosen fix",
			from: states[0], to: states[1],
			want: "@@ 0,3 0,3\n package sample\n \n-func experiment() int { return 42 }\n+func chosen() int { return 1 }\n",
		},
		{name: "same state", from: states[1], to: states[1], want: ""},
		{name: "foreign history", from: states[0], to: foreign, wantErr: "different histories"},
		{name: "missing snapshot", from: states[0], wantErr: "two snapshots"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			d, err := diff.Compare(t.Context(), tt.from, tt.to, limits.Default())
			if tt.wantErr != "" {
				if d != nil || err == nil || !strings.Contains(err.Error(), tt.wantErr) {
					t.Fatalf("diff = %v, error = %v; want %q", d, err, tt.wantErr)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}

			if d.From() != tt.from.Node() || d.To() != tt.to.Node() {
				t.Fatal("diff lost the identity of its states")
			}
			if got := render(d.Hunks()); got != tt.want {
				t.Fatalf("hunks =\n%s\nwant\n%s", got, tt.want)
			}
		})
	}
}

func FuzzLines(f *testing.F) {
	// Myers (1986), figure 1, with its letters shifted: five edits. Seeds run
	// under plain go test, and checkDiff checks that count against the LCS.
	f.Add([]byte("abcabba"), []byte("cbabac"))
	f.Add([]byte(""), []byte("aaaa"))
	f.Add([]byte("abababababab"), []byte("babababababa"))

	// Each byte picks one of four lines, so repeats are common. Inputs are
	// capped at 64 lines to keep the LCS check and each run small.
	f.Fuzz(func(t *testing.T, a, b []byte) {
		if len(a) > 64 || len(b) > 64 {
			t.Skip()
		}

		toLines := func(data []byte) []string {
			lines := make([]string, len(data))
			for i, c := range data {
				lines[i] = string(rune('a' + c%4))
			}
			return lines
		}

		left, right := toLines(a), toLines(b)
		hunks, err := diff.Lines(t.Context(), left, right, limits.Default())
		if err != nil {
			t.Fatal(err)
		}
		checkDiff(t, left, right, hunks)
	})
}
