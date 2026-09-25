package tui

import (
	"context"
	"slices"
	"unsafe"

	"github.com/nuggocto/xunhen/internal/diff"
	"github.com/nuggocto/xunhen/internal/history"
	"github.com/nuggocto/xunhen/internal/termtext"
)

// document is one reconstructed state prepared for drawing: the immutable
// snapshot plus a column index for each long line, built in the worker so
// that drawing never measures a whole long line. A document never changes
// after preparation, so the cache and the view can share it freely.
type document struct {
	id       history.NodeID
	snapshot *history.Snapshot

	// long lists the lines longer than termtext's index stride, in
	// ascending order, and columns holds their indexes.
	long    []int
	columns []*termtext.Columns
	index   int // bytes held by long and columns
}

// prepareDocument indexes the long lines of a state, measured with the
// renderer's width method. Its cost grows with the bytes of those lines, so
// it checks cancellation every 65,536 lines and termtext.Index checks within
// long lines.
func prepareDocument(ctx context.Context, id history.NodeID, snapshot *history.Snapshot, method termtext.Method) (*document, error) {
	d := &document{id: id, snapshot: snapshot}
	for i := range snapshot.Len() {
		if i%65536 == 0 {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
		}

		line, _ := snapshot.Line(i)
		columns, err := termtext.Index(ctx, line, method)
		if err != nil {
			return nil, err
		}
		if columns != nil {
			d.long = append(d.long, i)
			d.columns = append(d.columns, columns)
			d.index += columns.Size()
		}
	}
	d.index += cap(d.long)*int(unsafe.Sizeof(0)) + cap(d.columns)*int(unsafe.Sizeof(d))

	return d, nil
}

func (d *document) len() int {
	return d.snapshot.Len()
}

// line returns line i and its column index, nil for a short line.
func (d *document) line(i int) (string, *termtext.Columns) {
	text, _ := d.snapshot.Line(i)
	if at, found := slices.BinarySearch(d.long, i); found {
		return text, d.columns[at]
	}

	return text, nil
}

// descriptorBytes is the size of one string header, the part of a line that
// a snapshot owns. The line bytes themselves belong to the loaded session:
// every snapshot line is a string from the decoded undo file or the verified
// base, which snapshots share rather than copy.
const descriptorBytes = int(unsafe.Sizeof(""))

// documentOverhead covers a document's fixed fields and its snapshot header.
const documentOverhead = 256

// charge is what holding the document costs beyond the session: the
// snapshot's line array, the column indexes, and the fixed fields.
func (d *document) charge() int {
	return documentCharge(d.len(), d.index)
}

func documentCharge(lines, index int) int {
	return lines*descriptorBytes + index + documentOverhead
}

// comparison is a completed diff prepared for drawing. Each hunk takes one
// header row and then one row per line; starts holds each hunk's first row,
// so any row is found with a binary search and a hunk lookup.
//
// A comparison keeps both documents, for their long-line indexes, and the
// diff keeps copies of both states' line arrays: the diff package owns the
// lines its hunks refer to.
type comparison struct {
	from, to *document
	hunks    []diff.Hunk
	starts   []int
	rows     int
}

func newComparison(from, to *document, d *diff.Diff) *comparison {
	c := &comparison{from: from, to: to, hunks: d.Hunks()}
	c.starts = make([]int, len(c.hunks))
	for i, h := range c.hunks {
		c.starts[i] = c.rows
		c.rows += 1 + h.Len()
	}

	return c
}

// row returns the hunk that holds display row r and the line within it, or
// line -1 for the hunk's header row. r must be in [0, rows).
func (c *comparison) row(r int) (hunk, line int) {
	i, found := slices.BinarySearch(c.starts, r)
	if !found {
		i--
	}

	return i, r - c.starts[i] - 1
}
