// Package synth writes synthetic format 3 undo files for tests and workload
// generation, and provides a context that cancels at a chosen check. The
// files follow docs/undo-format.md for the Linux/amd64 profile and record
// nothing a real producer would add beyond what replay reads. It is test and
// tooling code: the command never imports it.
package synth

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"io"
	"math"
	"strings"
	"sync"
	"sync/atomic"
)

// Entry is one text entry: it replaces the lines strictly between Top and
// Bottom with Lines. Bottom zero means one past the last line.
type Entry struct {
	Top, Bottom int32
	Lines       []string
}

// Node is one change record. IDs are the producer's sequence numbers; zero
// links mean absent, and Parent zero names the retained root.
type Node struct {
	ID, Parent, Child, Next, Previous int32
	Time                              int64
	Entries                           []Entry
}

// File is a complete history: its change records in file order, the
// envelope's navigation pointers, and the reference text the base must
// match.
type File struct {
	Nodes                          []Node
	Oldest, Newest, Redo           int32
	LastSequence, TimelineSequence int32
	Reference                      []string
}

// WriteTo streams the file, so a generator can write hundreds of megabytes
// without holding them. The reference hash follows Neovim: each line and a
// terminating NUL, which is why reference lines may not contain NUL or LF.
func (f File) WriteTo(w io.Writer) (int64, error) {
	hash := sha256.New()
	for _, line := range f.Reference {
		if strings.ContainsAny(line, "\x00\n") {
			return 0, errors.New("synth: reference lines may not contain NUL or LF")
		}
		hash.Write([]byte(line))
		hash.Write([]byte{0})
	}
	if len(f.Reference) == 0 {
		return 0, errors.New("synth: a buffer has at least one line")
	}

	out := &counter{w: bufio.NewWriterSize(w, 1<<20)}
	out.bytes([]byte("Vim\x9fUnDo\xe5\x00\x03"))
	out.bytes(hash.Sum(nil))
	for _, value := range []int32{
		// The line count, an empty saved U line, and the saved cursor.
		int32(len(f.Reference)), 0, 0, 0,
		f.Oldest, f.Newest, f.Redo, int32(len(f.Nodes)), f.LastSequence, f.TimelineSequence,
	} {
		out.u32(uint32(value))
	}
	out.u64(0)           // time of the last write
	out.bytes([]byte{0}) // no optional file fields

	for _, n := range f.Nodes {
		out.u16(0x5fd0)
		for _, value := range []int32{n.Parent, n.Child, n.Next, n.Previous, n.ID} {
			out.u32(uint32(value))
		}
		out.bytes(make([]byte, 12)) // cursor position
		out.u32(^uint32(0))         // cursor virtual column -1
		out.bytes(make([]byte, 2+26*12+32))
		out.u64(uint64(n.Time))
		out.bytes([]byte{0}) // no optional change fields

		for _, e := range n.Entries {
			out.u16(0xf518)
			for _, value := range []int32{e.Top, e.Bottom, 0, int32(len(e.Lines))} {
				out.u32(uint32(value))
			}
			for _, line := range e.Lines {
				out.u32(uint32(len(line)))
				out.bytes([]byte(line))
			}
		}
		out.bytes([]byte{0x35, 0x81, 0x35, 0x81}) // ends of the text and extmark lists
	}
	out.bytes([]byte{0xe7, 0xaa})

	if out.err == nil {
		out.err = out.w.(*bufio.Writer).Flush()
	}
	return out.n, out.err
}

// Bytes returns the whole file.
func (f File) Bytes() ([]byte, error) {
	var b bytes.Buffer
	if _, err := f.WriteTo(&b); err != nil {
		return nil, err
	}
	return b.Bytes(), nil
}

// Chain is a linear history through states: the root holds states[0], node
// k holds states[k], and the last node is the reference. Each change
// replaces the whole buffer, facing undo as a change on the reference path
// must, so it holds the previous state.
func Chain(states [][]string) File {
	last := int32(len(states) - 1)
	f := File{
		Oldest:           min(last, 1),
		Newest:           last,
		LastSequence:     last,
		TimelineSequence: last,
		Reference:        states[last],
	}

	for k := int32(1); k <= last; k++ {
		n := Node{ID: k, Parent: k - 1, Time: 1_700_000_000 + int64(k)}
		if k < last {
			n.Child = k + 1
		}
		n.Entries = []Entry{{Top: 0, Bottom: 0, Lines: states[k-1]}}
		f.Nodes = append(f.Nodes, n)
	}

	return f
}

type counter struct {
	w   io.Writer
	n   int64
	err error
}

func (c *counter) bytes(b []byte) {
	if c.err != nil {
		return
	}
	n, err := c.w.Write(b)
	c.n += int64(n)
	c.err = err
}

func (c *counter) u16(v uint16) { c.bytes(binary.BigEndian.AppendUint16(nil, v)) }
func (c *counter) u32(v uint32) { c.bytes(binary.BigEndian.AppendUint32(nil, v)) }
func (c *counter) u64(v uint64) { c.bytes(binary.BigEndian.AppendUint64(nil, v)) }

// Countdown is a context that reports cancellation from its next Err call
// once a set number of calls have passed, so a test can cancel work partway
// through deterministically, at any cancellation check, without timing.
// Work checks cancellation through Err; Done closes when Err first reports
// it, for code that waits on the channel.
type Countdown struct {
	context.Context
	calls, after atomic.Int64
	once         sync.Once
	done         chan struct{}
}

// CountChecks returns a context that never cancels and counts Err calls.
func CountChecks(parent context.Context) *Countdown {
	c := &Countdown{Context: parent, done: make(chan struct{})}
	c.after.Store(math.MaxInt64)
	return c
}

// CancelAfter returns a context whose Err reports context.Canceled from call
// after+1 onward.
func CancelAfter(parent context.Context, after int64) *Countdown {
	c := CountChecks(parent)
	c.after.Store(after)
	return c
}

// Checks returns the number of Err calls so far.
func (c *Countdown) Checks() int64 { return c.calls.Load() }

func (c *Countdown) Err() error {
	if c.calls.Add(1) > c.after.Load() {
		c.once.Do(func() { close(c.done) })
		return context.Canceled
	}
	return c.Context.Err()
}

func (c *Countdown) Done() <-chan struct{} { return c.done }
