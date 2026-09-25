package termtext

import (
	"context"
	"slices"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/charmbracelet/x/ansi"
)

// TabWidth is the distance between tab stops, the terminal default.
const TabWidth = 8

// indexStride is the number of line bytes between two column checkpoints,
// and lines no longer than it get no index. Clip reads at most about this
// much before the first visible cell, so a 16 MiB line scrolls as cheaply as
// a short one. A larger stride would make each drawn row slower; a smaller
// one would make the indexes of a 64 MiB state of long lines larger. At 4 KiB
// they stay under 2 MiB, which the browser's cache budget relies on.
const indexStride = 4096

// maxCluster bounds the bytes of one displayed character. A combining
// sequence longer than this is shown escaped rather than measured, so no
// input can make one cell cost unbounded work.
const maxCluster = 32

// Method is how a terminal measures a character cluster. Bubble Tea starts
// with Runes and switches to Clusters when the terminal reports Unicode core
// mode (2027), so text must be measured with whichever one the renderer uses
// at the time, or clipped lines lose their ends.
type Method uint8

const (
	// Runes gives a cluster the width of its first rune that has one.
	Runes Method = iota
	// Clusters measures the whole cluster, so an emoji presentation
	// selector can make a narrow symbol wide.
	Clusters
)

func (m Method) ansi() ansi.Method {
	if m == Clusters {
		return ansi.GraphemeWidth
	}

	return ansi.WcWidth
}

// Columns records where display columns fall in one long line, so drawing can
// start near any column without measuring the whole line first. Build it once
// with Index, outside rendering; it holds no line text. An index is valid only
// for the Method it was built with.
type Columns struct {
	method Method
	// offsets[i] is a byte offset at a character boundary and columns[i]
	// the display column where that character starts. Both increase.
	offsets []int
	columns []int
	width   int
}

// Index measures a line for Clip and Width. Lines of at most indexStride
// bytes need no index, and Index returns nil for them. Measuring checks ctx
// every 64 checkpoints, 256 KiB, because a 16 MiB line takes tens of
// milliseconds.
func Index(ctx context.Context, line string, method Method) (*Columns, error) {
	if len(line) <= indexStride {
		return nil, nil
	}

	c := &Columns{
		method:  method,
		offsets: make([]int, 0, len(line)/indexStride+1),
		columns: make([]int, 0, len(line)/indexStride+1),
	}

	offset, column, checkpoint := 0, 0, 0
	for offset < len(line) {
		if offset >= checkpoint {
			if len(c.offsets)%64 == 0 {
				if err := ctx.Err(); err != nil {
					return nil, err
				}
			}
			c.offsets = append(c.offsets, offset)
			c.columns = append(c.columns, column)
			checkpoint = offset + indexStride
		}

		u := next(line, offset, column, method)
		offset += u.size
		column += u.width
	}
	c.width = column

	return c, nil
}

// Size estimates the bytes an index holds, for memory accounting.
func (c *Columns) Size() int {
	if c == nil {
		return 0
	}

	const word = 8
	return 2*word*cap(c.offsets) + 8*word
}

// usable reports whether an index can measure a line with method.
func (c *Columns) usable(method Method) bool {
	return c != nil && c.method == method
}

// seek returns the last checkpoint at or before column.
func (c *Columns) seek(column int) (offset, start int) {

	i, found := slices.BinarySearch(c.columns, column)
	if !found {
		i--
	}
	i = max(i, 0)

	return c.offsets[i], c.columns[i]
}

// Width returns the number of cells a line takes when drawn by Clip. It
// measures the whole line unless an index built with the same method is
// supplied.
func Width(line string, index *Columns, method Method) int {
	if index.usable(method) {
		return index.width
	}

	column := 0
	for offset := 0; offset < len(line); {
		u := next(line, offset, column, method)
		offset += u.size
		column += u.width
	}

	return column
}

// Clip draws the cells [from, from+width) of an untrusted line, with tabs
// expanded to TabWidth stops, and returns the text and the cells it fills.
// The text holds only printable characters and spaces: terminal controls,
// format characters, and invalid bytes appear as the same escapes that
// EscapeDisplay writes. A wide character or tab cut by either edge is drawn
// as spaces. Pass the line's index from Index for a long line; without one,
// Clip reads the line from its start. It never reads more than about
// indexStride bytes before the first visible cell plus maxCluster bytes per
// cell drawn. An index built with another method is ignored, and the line is
// measured from its start instead.
//
// Pass the method the renderer currently uses. A terminal whose own widths
// differ from both methods, as some do for emoji sequences, may still shift
// the rest of such a line.
func Clip(line string, index *Columns, method Method, from, width int) (string, int) {
	if width <= 0 {
		return "", 0
	}
	from = max(from, 0)
	end := from + width

	offset, column := 0, 0
	if index.usable(method) {
		offset, column = index.seek(from)
	}
	var out strings.Builder
	cells := 0

	for offset < len(line) && column < end {
		u := next(line, offset, column, method)
		offset += u.size
		start := column
		column += u.width
		if column <= from {
			continue
		}

		// The visible cells of this unit are [low, high).
		low, high := max(start, from), min(column, end)
		cells += high - low

		switch {
		case u.tab:
			out.WriteString(strings.Repeat(" ", high-low))
		case low == start && high == column:
			out.WriteString(u.text)
		case u.ascii:
			// Escapes take one cell per byte, so a cut keeps what fits.
			out.WriteString(u.text[low-start : high-start])
		default:
			out.WriteString(strings.Repeat(" ", high-low))
		}
	}

	return out.String(), cells
}

// unit is one drawn character: a printable character or cluster, an escape,
// or a tab.
type unit struct {
	size  int    // line bytes consumed
	width int    // cells drawn
	text  string // what to draw; unused for a tab
	tab   bool
	ascii bool // text is printable ASCII, one cell per byte
}

// next reads the unit at offset, which starts at column.
func next(line string, offset, column int, method Method) unit {
	b := line[offset]
	switch {
	case b == '\t':
		return unit{size: 1, width: TabWidth - column%TabWidth, tab: true}
	case b >= 0x20 && b < 0x7f && (offset+1 == len(line) || line[offset+1] < 0x80):
		// Printable ASCII not followed by a possible combining mark.
		return unit{size: 1, width: 1, text: line[offset : offset+1], ascii: true}
	}

	r, size := utf8.DecodeRuneInString(line[offset:])
	if !readable(r, size) {
		return escaped(line[offset : offset+size])
	}

	// Measure at most maxCluster bytes, cut at a rune boundary. The cluster
	// then ends at its first tab or escaped rune, which the renderer also
	// sees as a break. Grapheme boundaries depend only on what precedes
	// them, so cutting a cluster short cannot move an earlier boundary.
	limit := min(offset+maxCluster, len(line))
	for limit > offset+size && limit < len(line) && !utf8.RuneStart(line[limit]) {
		limit--
	}
	cluster, width := ansi.FirstGraphemeCluster(line[offset:limit], method.ansi())
	for i := size; i < len(cluster); {
		r, n := utf8.DecodeRuneInString(cluster[i:])
		if r == '\t' || !readable(r, n) {
			cluster = cluster[:i]
			_, width = ansi.FirstGraphemeCluster(cluster, method.ansi())
			break
		}
		i += n
	}
	end := offset + len(cluster)

	// A zero-width character would vanish. A cluster that reaches the
	// maxCluster limit before another readable rune may continue past it,
	// where the renderer would measure the whole cluster differently.
	// Escaped ASCII is unambiguous in both cases, and it ends any cluster
	// the renderer is building.
	cut := end == limit && limit < len(line) && continues(line[limit:])
	if len(cluster) < size {
		panic("termtext: a character cluster shorter than its first rune")
	}
	if width == 0 || cut || joins(cluster) {
		return escaped(cluster)
	}

	return unit{size: len(cluster), width: width, text: cluster}
}

// joins reports whether a cluster would merge with printable ASCII drawn
// next to it: a combining or spacing mark with no base joins the character
// before it, and a prepended mark the character after it. Escapes, spaces,
// and the caller's own text surround a clipped cluster, so a cluster that
// joins them would change their width on screen.
func joins(cluster string) bool {
	if r, size := utf8.DecodeRuneInString(cluster); size == len(cluster) && independent(r) {
		return false
	}

	if before, _ := ansi.FirstGraphemeCluster("a"+cluster, ansi.WcWidth); len(before) != 1 {
		return true
	}
	after, _ := ansi.FirstGraphemeCluster(cluster+"a", ansi.WcWidth)
	return len(after) != len(cluster)
}

// continues reports whether rest starts with a rune that the cluster before
// it could absorb: anything readable other than a tab.
func continues(rest string) bool {
	r, n := utf8.DecodeRuneInString(rest)
	return r != '\t' && readable(r, n)
}

// independent reports, cheaply, runes that never join a neighbour: letters of
// scripts whose grapheme cluster rules give them no combining, spacing, or
// prepended forms. Han, Hangul syllables, and Hiragana cover most East Asian
// text, where the full check would dominate the cost of measuring a line.
// Other runes take the full check, so this list only has to be correct, not
// complete.
func independent(r rune) bool {
	switch {
	case r >= 0xac00 && r <= 0xd7a3: // Hangul syllables
		return true
	case !unicode.IsLetter(r):
		return false
	}

	return unicode.In(r, unicode.Han, unicode.Hiragana, unicode.Latin, unicode.Greek, unicode.Cyrillic)
}

// escaped renders raw bytes as printable ASCII. Printable ASCII stays as it
// is, as in EscapeDisplay; everything else becomes a Go-style escape.
func escaped(raw string) unit {
	var out []byte
	for offset := 0; offset < len(raw); {
		r, size := utf8.DecodeRuneInString(raw[offset:])
		part := raw[offset : offset+size]
		offset += size

		if r >= 0x20 && r < 0x7f {
			out = append(out, part...)
			continue
		}
		quoted := strconv.AppendQuoteToASCII(nil, part)
		out = append(out, quoted[1:len(quoted)-1]...)
	}

	return unit{size: len(raw), width: len(out), text: string(out), ascii: true}
}
