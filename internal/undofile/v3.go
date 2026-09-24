package undofile

import (
	"encoding/binary"
	"fmt"
)

const profileV3 = "neovim-v3-linux-amd64-le-lp64"

func (d *decoder) decodeV3() *DecodedFile {
	file := &DecodedFile{source: d.source}
	meta := &file.metadata
	meta.Format = FormatInfo{Version: 3, Profile: profileV3}

	d.read(meta.BaseHash[:], "base hash")
	meta.BaseLines = int32(d.count("base lines", d.limits.StateLines))
	if meta.BaseLines == 0 {
		d.fail(Invalid, 43, "base lines", "a Neovim buffer has at least one logical line")
	}

	meta.SavedLine = d.text("saved U line", "saved U line length")
	meta.SavedLineNumber = d.nonnegative("saved U line number")
	meta.SavedColumn = d.nonnegative("saved U column")

	meta.OldestRoot = Sequence(d.nonnegative("oldest root"))
	meta.Newest = Sequence(d.nonnegative("newest header"))
	meta.NextRedo = Sequence(d.nonnegative("next redo"))
	meta.HeaderCount = d.count("history nodes", d.limits.Nodes)
	meta.LastSequence = Sequence(d.nonnegative("last sequence"))
	meta.TimelineSequence = Sequence(d.nonnegative("timeline sequence"))
	meta.CurrentTime = d.i64("current time")
	meta.LastSave = d.optional()

	for range meta.HeaderCount {
		if d.cancelled() {
			break
		}

		record := d.recordV3()
		if d.err == nil {
			file.records = append(file.records, record)
		}
	}

	d.sequence = 0
	d.marker(0xe7aa, "headers end")
	return file
}

// optional reads a list of optional fields. Only ID 1 is supported and a
// duplicate is rejected, so a list holds at most one four-byte field and needs
// no size limit of its own.
func (d *decoder) optional() SaveNumber {
	var save SaveNumber

	for d.err == nil {
		offset := d.offset
		size := d.u8("optional-field length")
		if size == 0 || d.err != nil {
			break
		}

		id := d.u8("optional-field id")
		if id != 1 {
			d.fail(Unsupported, offset+1, "optional field", fmt.Sprintf("field ID %d is not supported", id))
			break
		}

		if save.Known {
			d.fail(Invalid, offset, "optional field", "duplicate field ID")
		}
		if size != 4 {
			d.fail(Invalid, offset, "save number", "expected four-byte optional payload")
		}

		save = SaveNumber{Known: true, Value: d.nonnegative("save number")}
	}

	return save
}

func (d *decoder) recordV3() Record {
	d.sequence = 0

	var record Record
	info := &record.info
	info.Offset = d.offset

	d.marker(0x5fd0, "header start")

	// Everything up to the event time has a fixed size. Reading it in one
	// call and parsing it from memory costs far less than a buffered read
	// per field, and it is about a hundred fields.
	var fixed [headerFieldBytes]byte
	start := d.offset
	d.read(fixed[:], "header fields")
	if d.err != nil {
		return record
	}
	f := block{d: d, bytes: fixed[:], start: start}

	info.Parent = Sequence(f.nonnegative("parent"))
	info.PreferredChild = Sequence(f.nonnegative("preferred child"))
	info.NextSibling = Sequence(f.nonnegative("next sibling"))
	info.PreviousSibling = Sequence(f.nonnegative("previous sibling"))
	info.Sequence = Sequence(f.nonnegative("sequence"))
	d.sequence = info.Sequence
	if info.Sequence == 0 {
		d.fail(Invalid, f.offset()-4, "sequence", "zero is reserved for absent links")
	}

	info.Cursor = f.position(cursorFields)
	info.CursorVirtualColumn = f.i32()
	if info.CursorVirtualColumn < -1 {
		d.fail(Invalid, f.offset()-4, "cursor virtual column", "value below -1")
	}

	info.Flags = f.u16()
	if info.Flags & ^uint16(7) != 0 {
		d.fail(Unsupported, f.offset()-2, "flags", fmt.Sprintf("flags 0x%04x include unsupported bits", info.Flags))
	}

	for range 26 {
		f.position(markFields)
	}
	f.position(visualStartFields)
	f.position(visualEndFields)
	f.nonnegative("visual mode")
	f.nonnegative("visual desired column")

	info.Time = f.i64()
	info.Save = d.optional()

	for d.nextEntry("text entries") {
		entry := d.entryV3()
		if d.err == nil {
			record.entries = append(record.entries, entry)
		}
	}

	for d.nextEntry("extmark entries") {
		mark := d.extmarkV3()
		if d.err == nil {
			record.extmarks = append(record.extmarks, mark)
		}
	}

	return record
}

func (d *decoder) entryV3() Entry {
	entry := Entry{
		Top:             int32(d.count("entry top", d.limits.StateLines)),
		Bottom:          int32(d.count("entry bottom", d.limits.StateLines+1)),
		LineCountAtSave: int32(d.count("entry saved line count", d.limits.StateLines)),
	}
	if entry.Bottom != 0 && entry.Bottom <= entry.Top {
		d.fail(Invalid, d.offset-8, "entry range", "bottom must follow top or be zero")
	}

	count := d.count("stored lines", d.limits.StoredLines-d.lines)
	d.lines += count

	for range count {
		if d.err != nil {
			break
		}

		line := d.text("entry line", "entry line length")
		if d.err == nil {
			entry.lines = append(entry.lines, line)
		}
	}

	return entry
}

// positionFields names a position's three scalars for diagnostics. The names
// are built once here rather than on each of the 29 positions every record
// holds.
type positionFields struct {
	line, column, extra string
}

func fieldsOf(name string) positionFields {
	return positionFields{line: name + " line", column: name + " column", extra: name + " extra column"}
}

var (
	cursorFields      = fieldsOf("cursor")
	markFields        = fieldsOf("named mark")
	visualStartFields = fieldsOf("visual start")
	visualEndFields   = fieldsOf("visual end")
)

// headerFieldBytes is the fixed part of a header after its start marker: five
// links and the sequence, the cursor and its virtual column, flags, 26 named
// marks, the visual selection, and the event time.
const headerFieldBytes = 5*4 + 12 + 4 + 2 + 26*12 + (2*12 + 2*4) + 8

// block parses big-endian scalars from bytes the decoder has already read in
// one call. A failure reports its field's own input offset, as a scalar read
// would, through the decoder's sticky error.
type block struct {
	d     *decoder
	bytes []byte
	start int64 // input offset of bytes[0]
	pos   int
}

func (b *block) offset() int64 { return b.start + int64(b.pos) }

func (b *block) u16() uint16 {
	v := binary.BigEndian.Uint16(b.bytes[b.pos:])
	b.pos += 2
	return v
}

func (b *block) i32() int32 {
	v := int32(binary.BigEndian.Uint32(b.bytes[b.pos:]))
	b.pos += 4
	return v
}

func (b *block) i64() int64 {
	v := int64(binary.BigEndian.Uint64(b.bytes[b.pos:]))
	b.pos += 8
	return v
}

func (b *block) nonnegative(field string) int32 {
	at := b.offset()
	v := b.i32()
	if v < 0 {
		b.d.fail(Invalid, at, field, "negative value")
	}

	return v
}

func (b *block) position(field positionFields) Position {
	return Position{
		Line:   b.nonnegative(field.line),
		Column: b.nonnegative(field.column),
		Extra:  b.nonnegative(field.extra),
	}
}

func (d *decoder) nextEntry(field string) bool {
	if d.err != nil {
		return false
	}

	offset := d.offset
	marker := d.u16(field)
	if marker == 0x3581 || d.err != nil {
		return false
	}
	if marker != 0xf518 {
		d.fail(Invalid, offset, field, "expected entry or list-end marker")
		return false
	}
	if d.entries == d.limits.Entries {
		d.fail(Limit, offset, "entries", "text and extmark entries exceed the limit")
		return false
	}

	d.entries++
	return true
}

func (d *decoder) extmarkV3() Extmark {
	offset := d.offset
	kind := d.i32("extmark type")
	if kind != 0 && kind != 1 {
		d.fail(Unsupported, offset, "extmark type", fmt.Sprintf("native record type %d is not supported", kind))
	}

	var body [48]byte
	d.read(body[:], "native extmark")

	var parts [3]Extent
	for i := range parts {
		parts[i] = Extent{
			Row:    int32(binary.LittleEndian.Uint32(body[i*8:])),
			Column: int32(binary.LittleEndian.Uint32(body[i*8+4:])),
			Bytes:  int64(binary.LittleEndian.Uint64(body[24+i*8:])),
		}
	}

	return Extmark{
		Kind:  ExtmarkKind(kind),
		Start: parts[0],
		Old:   parts[1],
		New:   parts[2],
	}
}
