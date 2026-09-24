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

// newText copies lines into chunks of at most maxLines lines. A buffer always
// has at least one line.
func newText(lines []string, maxLines int) *text {
	if len(lines) == 0 || maxLines < 2 {
		panic("replay text needs at least one line and a chunk size of two")
	}

	t := &text{maxLines: maxLines, lines: len(lines)}
	t.chunks = split(slices.Clone(lines), maxLines)
	for _, c := range t.chunks {
		t.bytes += c.bytes
	}

	return t
}

// bytesBetween counts the bytes of lines [top, end). It reads whole chunks by
// their totals and only the lines of the two partial chunks at the edges.
func (t *text) bytesBetween(top, end int) int {
	total := 0
	start := 0
	for _, c := range t.chunks {
		next := start + len(c.lines)
		switch {
		case next <= top || start >= end:
		case start >= top && next <= end:
			total += c.bytes
		default:
			total += textBytes(c.lines[max(top, start)-start : min(end, next)-start])
		}
		start = next
	}

	return total
}

// replace swaps lines [top, end) for added. The caller has checked the range
// against the current line count and keeps the result at one line or more.
func (t *text) replace(top, end int, added []string) {
	size := t.lines - (end - top) + len(added)
	if top < 0 || top > end || end > t.lines || size < 1 {
		panic(fmt.Sprintf("replay text replace [%d, %d) with %d lines in %d", top, end, len(added), t.lines))
	}

	first, firstStart := t.locate(top)
	last, lastStart := t.locate(end)

	// Rebuild the touched chunks: the kept head of the first, the new lines,
	// and the kept tail of the last.
	region := make([]string, 0, top-firstStart+len(added)+lastStart+len(t.chunks[last].lines)-end)
	region = append(region, t.chunks[first].lines[:top-firstStart]...)
	region = append(region, added...)
	region = append(region, t.chunks[last].lines[end-lastStart:]...)

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

// locate returns the chunk holding line pos and that chunk's first line. The
// position one past the last line belongs to the last chunk.
func (t *text) locate(pos int) (int, int) {
	start := 0
	for i, c := range t.chunks {
		if pos < start+len(c.lines) || i == len(t.chunks)-1 {
			return i, start
		}
		start += len(c.lines)
	}

	panic("replay text has no chunks")
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
