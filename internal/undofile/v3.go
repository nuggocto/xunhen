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

	meta.SavedLine = d.text("saved U line")
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

func (d *decoder) optional() SaveNumber {
	var save SaveNumber

	for d.err == nil {
		offset := d.offset
		d.chargeOptional(1)
		size := d.u8("optional-field length")
		if size == 0 || d.err != nil {
			break
		}

		d.chargeOptional(1 + int(size))
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

func (d *decoder) chargeOptional(n int) {
	if n > d.limits.OptionalBytes-d.optionalBytes {
		d.fail(Limit, d.offset, "optional-field bytes", "fields exceed remaining budget")
		return
	}

	d.optionalBytes += n
}

func (d *decoder) recordV3() Record {
	d.sequence = 0

	var record Record
	info := &record.info
	info.Offset = d.offset

	d.marker(0x5fd0, "header start")
	info.Parent = Sequence(d.nonnegative("parent"))
	info.PreferredChild = Sequence(d.nonnegative("preferred child"))
	info.NextSibling = Sequence(d.nonnegative("next sibling"))
	info.PreviousSibling = Sequence(d.nonnegative("previous sibling"))
	info.Sequence = Sequence(d.nonnegative("sequence"))
	d.sequence = info.Sequence
	if info.Sequence == 0 {
		d.fail(Invalid, d.offset-4, "sequence", "zero is reserved for absent links")
	}

	info.Cursor = d.position("cursor")
	info.CursorVirtualColumn = d.i32("cursor virtual column")
	if info.CursorVirtualColumn < -1 {
		d.fail(Invalid, d.offset-4, "cursor virtual column", "value below -1")
	}

	info.Flags = d.u16("flags")
	if info.Flags & ^uint16(7) != 0 {
		d.fail(Unsupported, d.offset-2, "flags", fmt.Sprintf("flags 0x%04x include unsupported bits", info.Flags))
	}

	for mark := range info.Marks {
		info.Marks[mark] = d.position("named mark")
	}

	info.Visual = Visual{
		Start:         d.position("visual start"),
		End:           d.position("visual end"),
		Mode:          d.nonnegative("visual mode"),
		DesiredColumn: d.nonnegative("visual desired column"),
	}
	info.Time = d.i64("event time")
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

		line := d.text("entry line")
		if d.err == nil {
			entry.lines = append(entry.lines, line)
		}
	}

	return entry
}

func (d *decoder) position(field string) Position {
	return Position{
		Line:   d.nonnegative(field + " line"),
		Column: d.nonnegative(field + " column"),
		Extra:  d.nonnegative(field + " extra column"),
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
		d.fail(Limit, offset, "entries", "text and extmark entries exceed budget")
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
