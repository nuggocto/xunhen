// Package undofile decodes bounded Neovim undo records. Successful decoding
// establishes framing and field validity, not graph validity or replay safety.
package undofile

import "slices"

// Sequence is a producer-assigned change identity; zero is an absent wire link.
type Sequence int32

// FormatInfo names the decoder profile. The file does not identify its producer
// release or ABI; Profile is an interpretation, not authenticated provenance.
type FormatInfo struct {
	Version uint16
	Profile string
}

// SaveNumber distinguishes missing optional metadata from an unsaved change.
type SaveNumber struct {
	Known bool
	Value int32
}

// Metadata describes the persisted reference buffer and navigation markers.
// TimelineSequence is a timeline position and need not name a retained record.
// Newest is meaningful for locating the reference only when NextRedo is absent.
type Metadata struct {
	Format    FormatInfo
	BaseHash  [32]byte
	BaseLines int32

	SavedLine       string
	SavedLineNumber int32
	SavedColumn     int32

	OldestRoot       Sequence
	Newest           Sequence
	NextRedo         Sequence
	HeaderCount      int
	LastSequence     Sequence
	TimelineSequence Sequence
	CurrentTime      int64
	LastSave         SaveNumber
}

// Position uses Neovim's line, byte-column, and virtual-column coordinates.
type Position struct {
	Line, Column, Extra int32
}

// Visual contains persisted selection metadata, without interpreting a mode.
type Visual struct {
	Start, End          Position
	Mode, DesiredColumn int32
}

// RecordInfo is a value copy of one change's metadata. Links describe logical
// ancestry and the recorded sibling preference order, not chronological order.
type RecordInfo struct {
	Offset          int64
	Sequence        Sequence
	Parent          Sequence
	PreferredChild  Sequence
	NextSibling     Sequence
	PreviousSibling Sequence

	Cursor              Position
	CursorVirtualColumn int32
	Flags               uint16
	Marks               [26]Position
	Visual              Visual
	Time                int64
	Save                SaveNumber
}

// Entry is an oriented line-range swap. Bottom == 0 means the buffer's line
// count plus one. Its range must be checked against actual text during replay.
// Strings use buffer API bytes: embedded source NUL is NUL, not wire-format LF.
type Entry struct {
	Top             int32
	Bottom          int32
	LineCountAtSave int32
	lines           []string
}

// LineCount is the number of lines stored on this side of the swap.
func (e Entry) LineCount() int { return len(e.lines) }

// Lines returns a copy of the stored lines. The strings themselves are immutable.
func (e Entry) Lines() []string { return slices.Clone(e.lines) }

// Line returns an immutable string, or false for an out-of-range selector.
func (e Entry) Line(index int) (string, bool) {
	if index < 0 || index >= len(e.lines) {
		return "", false
	}

	return e.lines[index], true
}

// ExtmarkKind identifies the two native records serialized by this producer.
type ExtmarkKind uint8

const (
	Splice ExtmarkKind = iota
	Move
)

// Extent preserves native signed coordinates without treating them as counts
// for allocation or as the text entry's line range.
type Extent struct {
	Row, Column int32
	Bytes       int64
}

// Extmark holds a splice's start/old/new or a move's start/extent/destination.
type Extmark struct {
	Kind            ExtmarkKind
	Start, Old, New Extent
}

// Record is an immutable handle. Accessors never expose writable backing slices.
type Record struct {
	info     RecordInfo
	entries  []Entry
	extmarks []Extmark
}

// Info returns a value copy, including the fixed-size mark array.
func (r Record) Info() RecordInfo { return r.info }

// EntryCount is the length of the oriented text-entry list.
func (r Record) EntryCount() int { return len(r.entries) }

// ExtmarkCount is the number of decoded native records.
func (r Record) ExtmarkCount() int { return len(r.extmarks) }

// Entry returns a swap in persisted list order, or false for an invalid index.
func (r Record) Entry(index int) (Entry, bool) {
	if index < 0 || index >= len(r.entries) {
		return Entry{}, false
	}

	return r.entries[index], true
}

// Extmark returns a value copy, or false for an invalid index.
func (r Record) Extmark(index int) (Extmark, bool) {
	if index < 0 || index >= len(r.extmarks) {
		return Extmark{}, false
	}

	return r.extmarks[index], true
}

// DecodedFile owns immutable records. Its zero value is not a decoded file.
type DecodedFile struct {
	source   string
	metadata Metadata
	records  []Record
	complete bool
}

// Metadata returns false for a nil or uninitialized file.
func (f *DecodedFile) Metadata() (Metadata, bool) {
	if f == nil || !f.complete {
		return Metadata{}, false
	}

	return f.metadata, true
}

// Source returns the caller-supplied diagnostic label. This package does not
// open it as a path, and presentation code must escape it for a terminal.
func (f *DecodedFile) Source() string {
	if f == nil {
		return ""
	}

	return f.source
}

// Record returns a handle in serialized order, or false for an invalid selector
// or incomplete file. Only Decode can create a complete file.
func (f *DecodedFile) Record(index int) (Record, bool) {
	if f == nil || !f.complete || index < 0 || index >= len(f.records) {
		return Record{}, false
	}

	return f.records[index], true
}
