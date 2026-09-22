package diff

import (
	"context"
	"fmt"
	"slices"

	"github.com/nuggocto/xunhen/internal/limits"
)

// Workspace charges, in bytes. A lookup-table entry is an estimate covering
// the map slot, string header, and identifier; the key bytes are shared with
// the compared lines and are not copied.
const (
	idBytes         = 4
	keptLineBytes   = 8 // identifier plus original index
	tableEntryBytes = 64
	runBytes        = 32
	cancelInterval  = 1024 // lines between cancellation checks while indexing
)

// LimitError reports the diff budget that stopped a comparison.
type LimitError struct {
	Budget string
}

func (e *LimitError) Error() string {
	return e.Budget + " budget exceeded while comparing states"
}

// search is one comparison's workspace. Every budget is charged before its
// work, so an exhausted budget never leaves a partial edit script behind.
type search struct {
	ctx    context.Context
	limits limits.Limits

	steps, workspace, compared int
}

func (s *search) step(n int) error {
	if n > s.limits.DiffSteps-s.steps {
		return &LimitError{Budget: "diff steps"}
	}

	s.steps += n
	return nil
}

func (s *search) alloc(n int) error {
	if n > s.limits.DiffWorkspaceBytes-s.workspace {
		return &LimitError{Budget: "diff workspace bytes"}
	}

	s.workspace += n
	return nil
}

func (s *search) compare(n int) error {
	if n > s.limits.DiffCompareBytes-s.compared {
		return &LimitError{Budget: "diff compared bytes"}
	}

	s.compared += n
	return nil
}

// equal compares two lines, charging their bytes when the lengths match.
func (s *search) equal(a, b string) (bool, error) {
	if len(a) != len(b) {
		return false, nil
	}
	if err := s.compare(len(a)); err != nil {
		return false, err
	}

	return a == b, nil
}

// script returns a complete edit script in line order. Trimming the common
// prefix and suffix first keeps the search to the region that changed.
func (s *search) script(left, right []string) ([]run, error) {
	prefix := 0
	for prefix < min(len(left), len(right)) {
		same, err := s.equal(left[prefix], right[prefix])
		if err != nil {
			return nil, err
		}
		if !same {
			break
		}
		prefix++
	}

	suffix := 0
	for suffix < min(len(left), len(right))-prefix {
		same, err := s.equal(left[len(left)-1-suffix], right[len(right)-1-suffix])
		if err != nil {
			return nil, err
		}
		if !same {
			break
		}
		suffix++
	}

	middle, err := s.matches(left[prefix:len(left)-suffix], right[prefix:len(right)-suffix])
	if err != nil {
		return nil, err
	}

	b := builder{search: s}
	matched := []run{{op: Equal, count: prefix}}
	for _, m := range middle {
		matched = append(matched, run{op: Equal, left: prefix + m.left, right: prefix + m.right, count: m.count})
	}
	matched = append(matched, run{op: Equal, left: len(left) - suffix, right: len(right) - suffix, count: suffix})

	for _, m := range matched {
		if err := b.equal(m.left, m.right, m.count); err != nil {
			return nil, err
		}
	}
	if err := b.finish(len(left), len(right)); err != nil {
		return nil, err
	}

	return b.runs, nil
}

// builder turns ordered matches into an edit script with every line covered.
type builder struct {
	search      *search
	runs        []run
	left, right int
}

func (b *builder) add(r run) error {
	if r.count == 0 {
		return nil
	}
	if err := b.search.alloc(runBytes); err != nil {
		return err
	}

	b.runs = append(b.runs, r)
	return nil
}

// gap covers the unmatched lines before a match, deletions first.
func (b *builder) gap(left, right int) error {
	if err := b.add(run{op: Delete, left: b.left, right: b.right, count: left - b.left}); err != nil {
		return err
	}
	b.left = left

	if err := b.add(run{op: Insert, left: b.left, right: b.right, count: right - b.right}); err != nil {
		return err
	}
	b.right = right
	return nil
}

func (b *builder) equal(left, right, count int) error {
	if count == 0 {
		return nil
	}
	if err := b.gap(left, right); err != nil {
		return err
	}

	if n := len(b.runs); n != 0 && b.runs[n-1].op == Equal {
		// Contiguous matches on both sides extend the previous equal run.
		b.runs[n-1].count += count
	} else if err := b.add(run{op: Equal, left: left, right: right, count: count}); err != nil {
		return err
	}

	b.left, b.right = left+count, right+count
	return nil
}

func (b *builder) finish(left, right int) error {
	return b.gap(left, right)
}

// matches returns equal runs between two regions, in order, using indexes
// relative to those regions. Lines that occur on only one side cannot be part
// of any common subsequence, so they are removed before the search. That keeps
// a rewrite with fresh text cheap without making the result less minimal.
func (s *search) matches(left, right []string) ([]run, error) {
	if len(left) == 0 || len(right) == 0 {
		return nil, nil
	}
	if err := s.alloc(idBytes * (len(left) + len(right))); err != nil {
		return nil, err
	}

	table := make(map[string]int32)
	leftIDs := make([]int32, len(left))
	for i, line := range left {
		if err := s.checkpoint(i); err != nil {
			return nil, err
		}
		if err := s.compare(len(line)); err != nil {
			return nil, err
		}

		id, ok := table[line]
		if !ok {
			if err := s.alloc(tableEntryBytes); err != nil {
				return nil, err
			}
			id = int32(len(table))
			table[line] = id
		}
		leftIDs[i] = id
	}

	// A right-hand line missing from the table gets -1 and can never match.
	shared := make([]bool, len(table))
	rightIDs := make([]int32, len(right))
	for i, line := range right {
		if err := s.checkpoint(i); err != nil {
			return nil, err
		}
		if err := s.compare(len(line)); err != nil {
			return nil, err
		}

		id, ok := table[line]
		if !ok {
			id = -1
		} else {
			shared[id] = true
		}
		rightIDs[i] = id
	}

	a, aIndex, err := s.keep(leftIDs, func(id int32) bool { return shared[id] })
	if err != nil {
		return nil, err
	}
	b, bIndex, err := s.keep(rightIDs, func(id int32) bool { return id >= 0 })
	if err != nil {
		return nil, err
	}

	found, err := s.myers(a, b)
	if err != nil {
		return nil, err
	}

	// Translate kept positions back to region indexes, merging lines that stay
	// contiguous on both sides.
	var out []run
	for _, m := range found {
		for i := range m.count {
			l, r := int(aIndex[m.left+i]), int(bIndex[m.right+i])
			if n := len(out); n != 0 && out[n-1].left+out[n-1].count == l && out[n-1].right+out[n-1].count == r {
				out[n-1].count++
				continue
			}
			if err := s.alloc(runBytes); err != nil {
				return nil, err
			}
			out = append(out, run{op: Equal, left: l, right: r, count: 1})
		}
	}

	return out, nil
}

func (s *search) checkpoint(i int) error {
	if i%cancelInterval != 0 {
		return nil
	}

	return s.ctx.Err()
}

// keep returns the identifiers that pass the filter and their region indexes.
func (s *search) keep(ids []int32, ok func(int32) bool) ([]int32, []int32, error) {
	n := 0
	for _, id := range ids {
		if ok(id) {
			n++
		}
	}
	if err := s.alloc(keptLineBytes * n); err != nil {
		return nil, nil, err
	}

	kept, index := make([]int32, 0, n), make([]int32, 0, n)
	for i, id := range ids {
		if ok(id) {
			kept = append(kept, id)
			index = append(index, int32(i))
		}
	}

	return kept, index, nil
}

// myers runs the forward greedy search from Myers' "An O(ND) Difference
// Algorithm and Its Variations" (1986). Round d stores the furthest valid x on
// each diagonal k in [-d, d]; the stored rounds let backtracking recover the
// path without a second search. Work grows with the square of the edit
// distance, so the step and workspace budgets bound it.
func (s *search) myers(a, b []int32) ([]run, error) {
	n, m := len(a), len(b)
	if n == 0 || m == 0 {
		return nil, nil
	}

	var trace [][]int32
	for d := 0; d <= n+m; d++ {
		if err := s.ctx.Err(); err != nil {
			return nil, err
		}
		if err := s.alloc(idBytes * (2*d + 1)); err != nil {
			return nil, err
		}

		round := make([]int32, 2*d+1)
		for k := -d; k <= d; k += 2 {
			if err := s.step(1); err != nil {
				return nil, err
			}

			x, _ := furthest(trace, d, k, n, m)
			if x < 0 {
				round[k+d] = -1
				continue
			}

			for y := x - k; x < n && y < m && a[x] == b[y]; y++ {
				if err := s.step(1); err != nil {
					return nil, err
				}
				x++
			}
			round[k+d] = int32(x)

			if x == n && x-k == m {
				trace = append(trace, round)
				return backtrack(trace, n, m), nil
			}
		}
		trace = append(trace, round)
	}

	// A path of n deletions and m insertions always reaches the corner.
	panic("diff search ended without reaching the final corner")
}

// furthest picks the start of round d's path on diagonal k, before its snake,
// and the diagonal it came from. It returns -1 when no valid path of d edits
// reaches the diagonal. On a tie the insertion wins, as in Myers' paper.
func furthest(trace [][]int32, d, k, n, m int) (int, int) {
	if d == 0 {
		return 0, 0
	}

	previous := trace[d-1]
	down, right := -1, -1
	if k < d {
		// An insertion from diagonal k+1 keeps x and advances y.
		if x := int(previous[k+1+d-1]); x >= 0 && x-k <= m {
			down = x
		}
	}
	if k > -d {
		// A deletion from diagonal k-1 advances x.
		if x := int(previous[k-1+d-1]); x >= 0 && x+1 <= n {
			right = x + 1
		}
	}

	if down >= 0 && down >= right {
		return down, k + 1
	}
	if right >= 0 {
		return right, k - 1
	}

	return -1, 0
}

// backtrack walks the stored rounds from the final corner to the origin and
// returns the diagonal runs of the path in order.
func backtrack(trace [][]int32, n, m int) []run {
	var out []run
	x, y := n, m

	for d := len(trace) - 1; d > 0; d-- {
		k := x - y
		if int(trace[d][k+d]) != x {
			panic(fmt.Sprintf("diff backtrack left the stored path at round %d", d))
		}

		start, from := furthest(trace, d, k, n, m)
		if start < 0 {
			panic(fmt.Sprintf("diff backtrack found no predecessor at round %d", d))
		}
		if x > start {
			out = append(out, run{op: Equal, left: start, right: start - k, count: x - start})
		}

		x = int(trace[d-1][from+d-1])
		y = x - from
	}

	if x != y {
		panic("diff backtrack did not return to the main diagonal")
	}
	if x > 0 {
		out = append(out, run{op: Equal, count: x})
	}

	slices.Reverse(out)
	return out
}
