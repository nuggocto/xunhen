package history

import (
	"fmt"
	"slices"
)

// chunkLines is the most lines one chunk of a replay workspace holds. An edit
// scans the chunk list and copies at most three chunks plus its own lines, so
// its cost grows with chunkLines + lines/chunkLines instead of with the whole
// state. At a million lines that is a few thousand line references per edit,
// where a flat slice could move a million.
const chunkLines = 1024

// text holds buffer lines in chunks, the way Neovim's memline holds them in
// blocks. Every chunk holds between maxLines/2 and maxLines lines, except a
// lone chunk, which holds between one and maxLines. The chunk list therefore
// stays short, and an edit that changes the line count moves only the chunks
// it touches.
type text struct {
	maxLines int
	chunks   []chunk
	lines    int
	bytes    int // each line plus its logical terminator
}

type chunk struct {
	lines []string
	bytes int
}

// newText divides lines into chunks of at most maxLines lines. It takes
// ownership of the slice, which it never writes to. A buffer always has at
// least one line.
func newText(lines []string, maxLines int) *text {
	if len(lines) == 0 || maxLines < 2 {
		panic("replay text needs at least one line and a chunk size of two")
	}

	t := &text{maxLines: maxLines, lines: len(lines)}
	t.chunks = split(lines, maxLines)
	for _, c := range t.chunks {
		t.bytes += c.bytes
	}

	return t
}

// span is a range of lines [top, end), located once so that replay can
// measure it and then replace it without scanning the chunk list twice.
// first holds line top, or the insertion point when the range is empty; last
// holds line end-1, or is first when the range is empty.
type span struct {
	top, end          int
	first, firstStart int
	last, lastStart   int
}

// find locates [top, end) in one pass over the chunk list. The position one
// past the last line belongs to the last chunk.
func (t *text) find(top, end int) span {
	if top < 0 || top > end || end > t.lines {
		panic(fmt.Sprintf("replay text range [%d, %d) of %d lines", top, end, t.lines))
	}

	s := span{top: top, end: end, first: -1}
	final := max(top, end-1)
	start := 0
	for i, c := range t.chunks {
		next := start + len(c.lines)
		lastChunk := i == len(t.chunks)-1
		if s.first < 0 && (top < next || lastChunk) {
			s.first, s.firstStart = i, start
		}
		if s.first >= 0 && (final < next || lastChunk) {
			s.last, s.lastStart = i, start
			return s
		}
		start = next
	}

	panic("replay text has no chunks")
}

// bytesIn counts the bytes of the span's lines. It reads the chunks between
// the two ends by their totals.
func (t *text) bytesIn(s span) int {
	if s.top == s.end {
		return 0
	}

	first, last := t.chunks[s.first].lines, t.chunks[s.last].lines
	if s.first == s.last {
		return textBytes(first[s.top-s.firstStart : s.end-s.firstStart])
	}

	total := textBytes(first[s.top-s.firstStart:]) + textBytes(last[:s.end-s.lastStart])
	for _, c := range t.chunks[s.first+1 : s.last] {
		total += c.bytes
	}

	return total
}

// replace swaps the span's lines for added. The caller has checked the range
// against the current line count and keeps the result at one line or more.
func (t *text) replace(s span, added []string) {
	size := t.lines - (s.end - s.top) + len(added)
	if size < 1 {
		panic(fmt.Sprintf("replay text replace [%d, %d) with %d lines in %d", s.top, s.end, len(added), t.lines))
	}

	if t.replaceWithin(s, added, size) {
		return
	}

	// Rebuild the touched chunks: the kept head of the first, the new lines,
	// and the kept tail of the last.
	first, last := s.first, s.last
	head := t.chunks[first].lines[:s.top-s.firstStart]
	tail := t.chunks[last].lines[s.end-s.lastStart:]
	region := make([]string, 0, len(head)+len(added)+len(tail))
	region = append(region, head...)
	region = append(region, added...)
	region = append(region, tail...)

	// A short region borrows a neighbour, which holds at least maxLines/2
	// lines, so no chunk falls below half full.
	if len(region) < t.maxLines/2 {
		switch {
		case last+1 < len(t.chunks):
			last++
			region = append(region, t.chunks[last].lines...)
		case first > 0:
			first--
			region = append(slices.Clone(t.chunks[first].lines), region...)
		}
	}

	removed := 0
	for _, c := range t.chunks[first : last+1] {
		removed += c.bytes
	}

	rebuilt := split(region, t.maxLines)
	for _, c := range rebuilt {
		t.bytes += c.bytes
	}
	t.bytes -= removed
	t.lines = size
	t.chunks = slices.Replace(t.chunks, first, last+1, rebuilt...)

	// The cost bound rests on this: chunks stay at least half full, so the
	// list a later edit scans stays short.
	for _, c := range rebuilt {
		if len(c.lines) > t.maxLines || (len(t.chunks) > 1 && len(c.lines) < t.maxLines/2) {
			panic(fmt.Sprintf("replay text chunk of %d lines, limit %d", len(c.lines), t.maxLines))
		}
	}
}

// replaceWithin edits one chunk in place when the span lies inside it and the
// chunk stays within its size bounds, which is the usual case. It reports
// false, changing nothing, otherwise. A chunk that grows gets room for
// maxLines lines, so later insertions into it allocate nothing. The text owns
// every chunk's backing array, and a chunk never writes past its capacity,
// so the edit cannot reach another chunk or a snapshot.
func (t *text) replaceWithin(s span, added []string, size int) bool {
	if s.first != s.last {
		return false
	}

	c := &t.chunks[s.first]
	from, to := s.top-s.firstStart, s.end-s.firstStart
	length := len(c.lines) - (to - from) + len(added)
	if length > t.maxLines || length < 1 || (len(t.chunks) > 1 && length < t.maxLines/2) {
		return false
	}

	delta := textBytes(added) - textBytes(c.lines[from:to])
	if length <= cap(c.lines) {
		c.lines = slices.Replace(c.lines, from, to, added...)
	} else {
		grown := make([]string, 0, t.maxLines)
		grown = append(grown, c.lines[:from]...)
		grown = append(grown, added...)
		c.lines = append(grown, c.lines[to:]...)
	}

	c.bytes += delta
	t.bytes += delta
	t.lines = size
	return true
}

// all returns every line in order, in a new slice.
func (t *text) all() []string {
	out := make([]string, 0, t.lines)
	for _, c := range t.chunks {
		out = append(out, c.lines...)
	}

	return out
}

// split divides lines into the fewest chunks of at most maxLines lines, sized
// evenly so that each holds at least maxLines/2 when there is more than one.
// The chunks share the backing array of lines, each capped at its own length.
func split(lines []string, maxLines int) []chunk {
	count := (len(lines) + maxLines - 1) / maxLines
	chunks := make([]chunk, 0, count)
	for i := range count {
		part := lines[i*len(lines)/count : (i+1)*len(lines)/count]
		chunks = append(chunks, chunk{lines: part[:len(part):len(part)], bytes: textBytes(part)})
	}

	return chunks
}

func textBytes(lines []string) int {
	total := 0
	for _, line := range lines {
		total += len(line) + 1
	}

	return total
}
