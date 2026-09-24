package diff

import (
	"context"
	"fmt"
	"hash/maphash"
	"sort"
)

// Effort counts the comparison's search work: diagonals visited, matched
// lines followed, and frontier entries reset by the exact search, and lines
// and anchors handled while anchoring. The anchoring weights make one unit of
// either kind cost 1 to 1.5 ns on the development machine. Effort never fails
// a comparison. It decides how much of the comparison gets the exact search,
// so search time stays bounded however far apart the states are.
const (
	// defaultExactEffort is spent on the exact search before the remaining
	// regions switch to anchoring: about 0.1 s, far more than two branches
	// of one source file need.
	defaultExactEffort = 64_000_000

	// defaultTotalEffort ends all searching, at about 0.4 s. Whatever is
	// still unmatched becomes plain deletions followed by insertions.
	defaultTotalEffort = 256_000_000

	// smallRegion is the combined size of both sides below which an anchored
	// comparison still runs the exact search. The exact search visits at most
	// about smallRegion²/4 diagonal steps on such a region.
	smallRegion = 512

	// cancelInterval is the number of lines between cancellation checks
	// while trimming and identifying.
	cancelInterval = 1024
)

// LimitError reports the limit that stopped a comparison.
type LimitError struct {
	Budget string
}

func (e *LimitError) Error() string {
	return e.Budget + " limit exceeded while comparing states"
}

// search is one comparison's workspace.
type search struct {
	ctx context.Context

	exactEffort, totalEffort int
	effort                   int

	// forward and backward are the exact search's frontiers, sized once for
	// the largest region and reused by every smaller one.
	forward, backward []int32

	// Anchoring counts each line identifier's copies in a region. The slices
	// are indexed by identifier and reset after each region.
	leftCount, rightCount, rightAt []int32
}

// script returns a complete edit script in line order. Trimming the common
// prefix and suffix first keeps the search to the region that changed.
func (s *search) script(left, right []string) ([]run, error) {
	prefix := 0
	for prefix < min(len(left), len(right)) && left[prefix] == right[prefix] {
		if err := s.checkpoint(prefix); err != nil {
			return nil, err
		}
		prefix++
	}

	suffix := 0
	for suffix < min(len(left), len(right))-prefix && left[len(left)-1-suffix] == right[len(right)-1-suffix] {
		if err := s.checkpoint(suffix); err != nil {
			return nil, err
		}
		suffix++
	}

	middle, err := s.matches(left[prefix:len(left)-suffix], right[prefix:len(right)-suffix])
	if err != nil {
		return nil, err
	}

	b := builder{}
	b.equal(0, 0, prefix)
	for _, m := range middle {
		b.equal(prefix+m.left, prefix+m.right, m.count)
	}
	b.equal(len(left)-suffix, len(right)-suffix, suffix)
	b.gap(len(left), len(right))

	return b.runs, nil
}

// builder turns ordered matches into an edit script with every line covered.
type builder struct {
	runs        []run
	left, right int
}

func (b *builder) add(r run) {
	if r.count != 0 {
		b.runs = append(b.runs, r)
	}
}

// gap covers the unmatched lines before a match, deletions first.
func (b *builder) gap(left, right int) {
	b.add(run{op: Delete, left: b.left, right: b.right, count: left - b.left})
	b.left = left
	b.add(run{op: Insert, left: b.left, right: b.right, count: right - b.right})
	b.right = right
}

func (b *builder) equal(left, right, count int) {
	if count == 0 {
		return
	}
	b.gap(left, right)

	if n := len(b.runs); n != 0 && b.runs[n-1].op == Equal {
		// Contiguous matches on both sides extend the previous equal run.
		b.runs[n-1].count += count
	} else {
		b.add(run{op: Equal, left: left, right: right, count: count})
	}
	b.left, b.right = left+count, right+count
}

// matches returns equal runs between two regions, in order, using indexes
// relative to those regions. Lines that occur on only one side cannot be part
// of any common subsequence, so they are removed before the search. That keeps
// a rewrite with fresh text cheap without making the result less minimal.
func (s *search) matches(left, right []string) ([]run, error) {
	if len(left) == 0 || len(right) == 0 {
		return nil, nil
	}

	table := newLineTable(left)
	leftIDs := make([]int32, len(left))
	for i := range left {
		if err := s.checkpoint(i); err != nil {
			return nil, err
		}
		leftIDs[i] = table.add(i)
	}

	// A right-hand line missing from the table gets -1 and can never match.
	shared := make([]bool, table.count())
	rightIDs := make([]int32, len(right))
	for i, line := range right {
		if err := s.checkpoint(i); err != nil {
			return nil, err
		}

		id := table.find(line)
		if id >= 0 {
			shared[id] = true
		}
		rightIDs[i] = id
	}

	a, aIndex := keep(leftIDs, func(id int32) bool { return shared[id] })
	b, bIndex := keep(rightIDs, func(id int32) bool { return id >= 0 })

	found, err := s.align(a, b, table.count())
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
			out = append(out, run{op: Equal, left: l, right: r, count: 1})
		}
	}

	return out, nil
}

// lineTable gives each distinct left-hand line a dense identifier, in order
// of first appearance. It is an open-addressed hash table whose slots hold
// identifiers and whose identifiers point back to a left-hand line by index.
// Holding no strings, it adds nothing for the garbage collector to scan, which
// a map keyed by millions of lines would.
type lineTable struct {
	left  []string
	seed  maphash.Seed
	slots []uint64 // hash tag << 32 | identifier + 1; zero marks an empty slot
	first []int32  // left-hand index of each identifier's first line
}

// newLineTable sizes the slots to at least twice the left-hand lines, so the
// table is never more than half full and every probe finds an empty slot.
func newLineTable(left []string) *lineTable {
	size := 1
	for size < 2*len(left) {
		size <<= 1
	}

	return &lineTable{left: left, seed: maphash.MakeSeed(), slots: make([]uint64, size)}
}

// slot returns the slot holding line's identifier, or the empty slot where
// it belongs, along with the line's hash tag. A probe compares the tag stored
// in the slot before the line itself, so a slot taken by another line almost
// never costs a read of that line.
func (t *lineTable) slot(line string) (int, uint64) {
	hash := maphash.String(t.seed, line)
	tag := hash >> 32 << 32
	mask := len(t.slots) - 1
	for i := int(hash) & mask; ; i = (i + 1) & mask {
		entry := t.slots[i]
		if entry == 0 {
			return i, tag
		}
		if entry&^0xffffffff == tag && t.left[t.first[uint32(entry)-1]] == line {
			return i, tag
		}
	}
}

// add returns the identifier of left-hand line i, assigning the next one to
// a line not seen before.
func (t *lineTable) add(i int) int32 {
	slot, tag := t.slot(t.left[i])
	if t.slots[slot] == 0 {
		t.first = append(t.first, int32(i))
		t.slots[slot] = tag | uint64(len(t.first))
	}

	return int32(uint32(t.slots[slot])) - 1
}

// find returns line's identifier, or -1 when no left-hand line equals it.
func (t *lineTable) find(line string) int32 {
	slot, _ := t.slot(line)
	return int32(uint32(t.slots[slot])) - 1
}

func (t *lineTable) count() int {
	return len(t.first)
}

func (s *search) checkpoint(i int) error {
	if i%cancelInterval != 0 {
		return nil
	}

	return s.ctx.Err()
}

// keep returns the identifiers that pass the filter and their region indexes.
func keep(ids []int32, ok func(int32) bool) ([]int32, []int32) {
	var kept, index []int32
	for i, id := range ids {
		if ok(id) {
			kept = append(kept, id)
			index = append(index, int32(i))
		}
	}

	return kept, index
}

// task is a pending region of the alignment, [a0, a1) against [b0, b1), or a
// match of a1-a0 lines starting at a0 and b0.
type task struct {
	a0, a1, b0, b1 int
	match          bool
}

// align returns the matched runs between a and b in order. It works through
// regions with an explicit stack rather than recursion, so no input can
// exhaust the goroutine stack. Each region is trimmed and then split by the
// exact search, split at anchors, or left as a plain change, depending on the
// effort spent so far. Every split leaves strictly smaller regions, so the
// loop ends.
func (s *search) align(a, b []int32, ids int) ([]run, error) {
	if len(a) == 0 || len(b) == 0 {
		return nil, nil
	}

	s.forward = make([]int32, len(a)+len(b)+2)
	s.backward = make([]int32, len(a)+len(b)+2)
	s.leftCount = make([]int32, ids)
	s.rightCount = make([]int32, ids)
	s.rightAt = make([]int32, ids)

	var out []run
	emit := func(a0, b0, count int) {
		if n := len(out); n != 0 && out[n-1].left+out[n-1].count == a0 && out[n-1].right+out[n-1].count == b0 {
			out[n-1].count += count
			return
		}
		out = append(out, run{op: Equal, left: a0, right: b0, count: count})
	}

	stack := []task{{a1: len(a), b1: len(b)}}
	for len(stack) != 0 {
		t := stack[len(stack)-1]
		stack = stack[:len(stack)-1]

		if t.match {
			emit(t.a0, t.b0, t.a1-t.a0)
			continue
		}
		if err := s.ctx.Err(); err != nil {
			return nil, err
		}

		prefix := 0
		for t.a0+prefix < t.a1 && t.b0+prefix < t.b1 && a[t.a0+prefix] == b[t.b0+prefix] {
			prefix++
		}
		if prefix != 0 {
			emit(t.a0, t.b0, prefix)
			t.a0, t.b0 = t.a0+prefix, t.b0+prefix
		}

		suffix := 0
		for t.a0 < t.a1-suffix && t.b0 < t.b1-suffix && a[t.a1-1-suffix] == b[t.b1-1-suffix] {
			suffix++
		}
		if suffix != 0 {
			t.a1, t.b1 = t.a1-suffix, t.b1-suffix
			// Pushed before the region's parts, so it is emitted after them.
			stack = append(stack, task{a0: t.a1, a1: t.a1 + suffix, b0: t.b1, b1: t.b1 + suffix, match: true})
		}

		parts, err := s.split(a, b, t)
		if err != nil {
			return nil, err
		}
		for i := len(parts) - 1; i >= 0; i-- {
			stack = append(stack, parts[i])
		}
	}

	return out, nil
}

// split divides one trimmed region into ordered parts. It returns none when
// the region is a plain change: one side is empty, or the effort is spent.
func (s *search) split(a, b []int32, t task) ([]task, error) {
	x, y := a[t.a0:t.a1], b[t.b0:t.b1]
	n, m := len(x), len(y)

	switch {
	case n == 0 || m == 0:
		return nil, nil

	case n == 1 || m == 1:
		// One line on a side matches its first copy on the other, if any. That
		// is a longest common subsequence for this region.
		if n == 1 {
			if j := indexOf(y, x[0]); j >= 0 {
				return []task{{a0: t.a0, a1: t.a0 + 1, b0: t.b0 + j, match: true}}, nil
			}
			return nil, nil
		}
		if i := indexOf(x, y[0]); i >= 0 {
			return []task{{a0: t.a0 + i, a1: t.a0 + i + 1, b0: t.b0, match: true}}, nil
		}
		return nil, nil
	}

	exact := s.effort < s.exactEffort
	small := n+m <= smallRegion && s.effort < s.totalEffort
	if exact || small {
		limit := s.exactEffort
		if !exact {
			limit = s.totalEffort
		}

		sx, sy, result, err := s.bisect(x, y, limit)
		switch {
		case err != nil:
			return nil, err
		case result == noCommonLine:
			return nil, nil
		case result == splitFound:
			return []task{
				{a0: t.a0, a1: t.a0 + sx, b0: t.b0, b1: t.b0 + sy},
				{a0: t.a0 + sx, a1: t.a1, b0: t.b0 + sy, b1: t.b1},
			}, nil
		}
	}

	if s.effort < s.totalEffort {
		return s.anchor(x, y, t), nil
	}

	return nil, nil
}

func indexOf(ids []int32, id int32) int {
	for i, v := range ids {
		if v == id {
			return i
		}
	}

	return -1
}

// bisectResult says what the exact search found for one region.
type bisectResult uint8

const (
	splitFound   bisectResult = iota // a point on a shortest edit path
	noCommonLine                     // the region is a plain change
	outOfEffort                      // the search gave up; nothing is known
)

// bisect finds a point on a shortest edit path through x and y by running
// Myers' search from both corners until the paths meet ("An O(ND) Difference
// Algorithm and Its Variations", 1986, section 4b). It keeps two frontiers of
// len(x)+len(y) entries instead of every round, so memory stays linear. The
// caller has trimmed the region and gives both sides at least two lines.
//
// It gives up once the comparison's effort reaches limit.
func (s *search) bisect(x, y []int32, limit int) (int, int, bisectResult, error) {
	n, m := len(x), len(y)
	maxD := (n + m + 1) / 2
	offset := maxD
	size := 2 * maxD

	forward, backward := s.forward[:size], s.backward[:size]
	for i := range size {
		forward[i], backward[i] = -1, -1
	}
	forward[offset+1], backward[offset+1] = 0, 0
	s.effort += size

	delta := n - m
	// With an odd difference in length, the forward path meets the reverse
	// path; with an even one, the reverse path meets the forward path.
	front := delta%2 != 0

	// Diagonals that ran off an edge of the grid need no further visits.
	forwardStart, forwardEnd, backwardStart, backwardEnd := 0, 0, 0, 0

	for d := range maxD {
		if err := s.ctx.Err(); err != nil {
			return 0, 0, outOfEffort, err
		}
		if s.effort >= limit {
			return 0, 0, outOfEffort, nil
		}

		for k := -d + forwardStart; k <= d-forwardEnd; k += 2 {
			s.effort++
			i := offset + k

			var px int
			if k == -d || (k != d && forward[i-1] < forward[i+1]) {
				px = int(forward[i+1])
			} else {
				px = int(forward[i-1]) + 1
			}
			py := px - k
			for px < n && py < m && x[px] == y[py] {
				px++
				py++
				s.effort++
			}
			forward[i] = int32(px)

			switch {
			case px > n:
				forwardEnd += 2
			case py > m:
				forwardStart += 2
			case front:
				j := offset + delta - k
				if j >= 0 && j < size && backward[j] != -1 && px >= n-int(backward[j]) {
					return s.checkSplit(px, py, n, m)
				}
			}
		}

		for k := -d + backwardStart; k <= d-backwardEnd; k += 2 {
			s.effort++
			i := offset + k

			var qx int
			if k == -d || (k != d && backward[i-1] < backward[i+1]) {
				qx = int(backward[i+1])
			} else {
				qx = int(backward[i-1]) + 1
			}
			qy := qx - k
			for qx < n && qy < m && x[n-qx-1] == y[m-qy-1] {
				qx++
				qy++
				s.effort++
			}
			backward[i] = int32(qx)

			switch {
			case qx > n:
				backwardEnd += 2
			case qy > m:
				backwardStart += 2
			case !front:
				j := offset + delta - k
				if j >= 0 && j < size && forward[j] != -1 {
					px := int(forward[j])
					py := offset + px - j
					if px >= n-qx {
						return s.checkSplit(px, py, n, m)
					}
				}
			}
		}
	}

	// A shortest path of D edits meets within ceil(D/2) rounds. Only a region
	// with no line in common, where D = n+m, can use up every round.
	return 0, 0, noCommonLine, nil
}

// checkSplit enforces the progress invariant: the split point lies inside the
// region and differs from both corners, so both parts are smaller.
func (s *search) checkSplit(px, py, n, m int) (int, int, bisectResult, error) {
	if px < 0 || px > n || py < 0 || py > m || px+py == 0 || px+py == n+m {
		panic(fmt.Sprintf("diff bisect split (%d, %d) of a %d by %d region", px, py, n, m))
	}

	return px, py, splitFound, nil
}

// pair is one anchor: a line at index a on the left and index b on the right.
type pair struct {
	a, b int
}

// anchor splits a region at lines that occur exactly once on each side, the
// idea behind patience diff. Among those lines it keeps the longest chain
// that is in order on both sides, then returns the gaps between anchors as
// regions and the anchors as matches. A region without such lines stays a
// plain change.
func (s *search) anchor(x, y []int32, t task) []task {
	s.effort += 3 * (len(x) + len(y))
	for _, id := range x {
		s.leftCount[id]++
	}
	for j, id := range y {
		s.rightCount[id]++
		s.rightAt[id] = int32(j)
	}

	var pairs []pair
	for i, id := range x {
		if s.leftCount[id] == 1 && s.rightCount[id] == 1 {
			pairs = append(pairs, pair{a: i, b: int(s.rightAt[id])})
		}
	}
	for _, id := range x {
		s.leftCount[id] = 0
	}
	for _, id := range y {
		s.rightCount[id] = 0
	}

	chain := longestIncreasing(pairs)
	s.effort += 16 * len(pairs) // a binary search per pair

	var parts []task
	a0, b0 := 0, 0
	for _, p := range chain {
		if p.a > a0 || p.b > b0 {
			parts = append(parts, task{a0: t.a0 + a0, a1: t.a0 + p.a, b0: t.b0 + b0, b1: t.b0 + p.b})
		}
		parts = append(parts, task{a0: t.a0 + p.a, a1: t.a0 + p.a + 1, b0: t.b0 + p.b, match: true})
		a0, b0 = p.a+1, p.b+1
	}
	if len(chain) != 0 && (a0 < len(x) || b0 < len(y)) {
		parts = append(parts, task{a0: t.a0 + a0, a1: t.a1, b0: t.b0 + b0, b1: t.b1})
	}

	return parts
}

// longestIncreasing returns the longest chain of pairs, already ordered by a,
// whose b indexes also increase. Patience sorting with binary search makes it
// O(k log k) for k pairs.
func longestIncreasing(pairs []pair) []pair {
	if len(pairs) == 0 {
		return nil
	}

	// tails[k] is the pair ending the best chain of length k+1 found so far:
	// the one with the smallest b.
	var tails []int
	previous := make([]int, len(pairs))
	for i, p := range pairs {
		k := sort.Search(len(tails), func(j int) bool { return pairs[tails[j]].b >= p.b })
		previous[i] = -1
		if k > 0 {
			previous[i] = tails[k-1]
		}
		if k == len(tails) {
			tails = append(tails, i)
		} else {
			tails[k] = i
		}
	}

	chain := make([]pair, len(tails))
	for i, k := tails[len(tails)-1], len(tails)-1; k >= 0; i, k = previous[i], k-1 {
		chain[k] = pairs[i]
	}

	return chain
}
