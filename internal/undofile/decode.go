package undofile

import (
	"bufio"
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/nuggocto/xunhen/internal/limits"
)

// Decode reads at most InputBytes+1 bytes, owns all retained text, and returns
// nil on every failure. No partial records can become an ordinary DecodedFile.
// Cancellation is checked between records and text chunks; the caller owns
// blocking I/O policy.
func Decode(ctx context.Context, source string, input io.Reader, lim limits.Limits) (*DecodedFile, error) {
	if err := lim.Validate(); err != nil {
		return nil, err
	}
	if input == nil {
		return nil, errors.New("nil undo reader")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	d := decoder{
		ctx:    ctx,
		source: source,
		limits: lim,
		reader: bufio.NewReaderSize(io.LimitReader(input, lim.InputBytes+1), 32<<10),
	}

	var magic [9]byte
	d.read(magic[:], "magic")
	if d.err == nil && string(magic[:]) != "Vim\x9fUnDo\xe5" {
		d.fail(Invalid, 0, "magic", "not a Neovim undo file")
	}

	version := d.u16("version")
	if d.err == nil && version != 3 {
		d.fail(Unsupported, 9, "version", fmt.Sprintf("format %d; supported format is 3", version))
	}
	if d.err != nil {
		return nil, d.err
	}

	file := d.decodeV3()
	if d.err != nil {
		return nil, d.err
	}

	if err := d.end(); err != nil {
		return nil, err
	}

	file.complete = true
	return file, nil
}

func (d *decoder) end() error {
	if d.cancelled() {
		return d.err
	}

	_, err := d.reader.ReadByte()
	if err == nil {
		if d.offset == d.limits.InputBytes {
			d.fail(Limit, d.offset, "undo input bytes", "input exceeds its budget")
		} else {
			d.fail(Invalid, d.offset, "end of file", "trailing data after headers-end marker")
		}
	} else if !errors.Is(err, io.EOF) {
		d.readError(err, "end of file")
	}

	return d.err
}

// A sticky error keeps scalar reads explicit without allowing a failed count
// read to drive allocation. Every variable-length loop also tests this error.
type decoder struct {
	ctx           context.Context
	source        string
	reader        *bufio.Reader
	limits        limits.Limits
	offset        int64
	sequence      Sequence
	err           error
	entries       int
	lines         int
	optionalBytes int
	textBytes     int
	scratch       [64 << 10]byte
}

func (d *decoder) fail(kind ErrorKind, offset int64, field, detail string) {
	if d.err != nil {
		return
	}

	d.err = &InputError{
		Kind:     kind,
		Source:   d.source,
		Offset:   offset,
		Sequence: d.sequence,
		Field:    field,
		Detail:   detail,
	}
}

func (d *decoder) readError(err error, field string) {
	kind := ReadFailure
	if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
		kind = Truncated
	}

	d.err = &InputError{
		Kind:     kind,
		Source:   d.source,
		Offset:   d.offset,
		Sequence: d.sequence,
		Field:    field,
		Detail:   "cannot read complete field",
		Cause:    err,
	}
}

// cancelled stores cancellation as the sticky error. Decoding checks it once
// per record and text chunk rather than on every scalar field.
func (d *decoder) cancelled() bool {
	if d.err == nil {
		d.err = d.ctx.Err()
	}

	return d.err != nil
}

func (d *decoder) read(dst []byte, field string) {
	if d.err != nil {
		return
	}
	if int64(len(dst)) > d.limits.InputBytes-d.offset {
		d.fail(Limit, d.offset, "undo input bytes", "field exceeds remaining input budget")
		return
	}

	n, err := io.ReadFull(d.reader, dst)
	d.offset += int64(n)
	if err != nil {
		d.readError(err, field)
	}
}

func (d *decoder) u8(field string) byte {
	var b [1]byte
	d.read(b[:], field)
	return b[0]
}

func (d *decoder) u16(field string) uint16 {
	var b [2]byte
	d.read(b[:], field)
	return binary.BigEndian.Uint16(b[:])
}

func (d *decoder) i32(field string) int32 {
	var b [4]byte
	d.read(b[:], field)
	return int32(binary.BigEndian.Uint32(b[:]))
}

func (d *decoder) nonnegative(field string) int32 {
	offset := d.offset
	n := d.i32(field)
	if n < 0 {
		d.fail(Invalid, offset, field, "negative value")
	}

	return n
}

func (d *decoder) i64(field string) int64 {
	var b [8]byte
	d.read(b[:], field)
	return int64(binary.BigEndian.Uint64(b[:]))
}

func (d *decoder) count(field string, limit int) int {
	offset := d.offset
	n := d.nonnegative(field)
	if int64(n) > int64(limit) {
		d.fail(Limit, offset, field, fmt.Sprintf("count exceeds budget %d", limit))
	}
	if d.err != nil {
		return 0
	}

	return int(n)
}

func (d *decoder) marker(want uint16, field string) {
	offset := d.offset
	if d.u16(field) != want {
		d.fail(Invalid, offset, field, "unexpected record marker")
	}
}

func (d *decoder) text(field string) string {
	n := d.count(field+" length", d.limits.LineBytes)
	if n > d.limits.TextBytes-d.textBytes {
		d.fail(Limit, d.offset-4, "decoded text bytes", "text exceeds remaining budget")
	}
	if int64(n) > d.limits.InputBytes-d.offset {
		d.fail(Limit, d.offset-4, "undo input bytes", "text exceeds remaining input budget")
	}
	if d.err != nil {
		return ""
	}

	d.textBytes += n

	var text strings.Builder
	// Grow only as actual chunks arrive, rather than allocating a claimed length
	// before finding out that a tiny input is truncated.
	for remaining := n; remaining > 0 && d.err == nil; {
		if d.cancelled() {
			break
		}

		size := min(remaining, len(d.scratch))
		chunk := d.scratch[:size]
		start := d.offset
		d.read(chunk, field)
		if d.err != nil {
			break
		}

		if index := bytes.IndexByte(chunk, 0); index >= 0 {
			d.fail(Invalid, start+int64(index), field, "NUL in serialized line")
			break
		}

		for i, b := range chunk {
			if b == '\n' {
				chunk[i] = 0
			}
		}

		text.Write(chunk)
		remaining -= size
	}

	return text.String()
}
