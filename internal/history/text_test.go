package history

import (
	"fmt"
	"slices"
	"testing"
)

// Chunked text must behave exactly like one flat slice. Four-line chunks make
// small cases cross chunk boundaries, split, and merge.
func TestTextMatchesFlatLines(t *testing.T) {
	t.Parallel()

	ten := numberedLines("line", 10)

	tests := []struct {
		name     string
		start    []string
		top, end int
		added    []string
	}{
		{name: "insert at the start", start: ten, top: 0, end: 0, added: []string{"a", "b"}},
		{name: "insert at the end", start: ten, top: 10, end: 10, added: []string{"a"}},
		{name: "rewrite one line", start: ten, top: 5, end: 6, added: []string{"a"}},
		{name: "delete across chunks", start: ten, top: 1, end: 9},
		{name: "replace everything", start: ten, top: 0, end: 10, added: []string{"only"}},
		{name: "grow one line into many chunks", start: []string{"x"}, top: 0, end: 1, added: numberedLines("new", 17)},
		{name: "remove all but one line", start: ten, top: 0, end: 9},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			text := newText(tt.start, 4)
			text.replace(tt.top, tt.end, tt.added)

			want := slices.Replace(slices.Clone(tt.start), tt.top, tt.end, tt.added...)
			if got := text.all(); !slices.Equal(got, want) {
				t.Fatalf("lines = %q, want %q", got, want)
			}
			if text.lines != len(want) || text.bytesBetween(0, text.lines) != textBytes(want) {
				t.Fatalf("counts %d lines and %d bytes, want %d and %d", text.lines, text.bytesBetween(0, text.lines), len(want), textBytes(want))
			}
		})
	}
}

// FuzzText applies generated edits to chunked text and to a flat slice, and
// requires the same lines, counts, and byte totals after every edit. Each
// input encodes at most 64 edits of up to 15 added lines.
func FuzzText(f *testing.F) {
	f.Add(uint8(4), []byte{0, 0, 9, 3, 5, 0, 0, 20, 1})
	f.Add(uint8(2), []byte{1, 3, 0, 0, 0, 15, 7, 7, 7})

	f.Fuzz(func(t *testing.T, chunk uint8, ops []byte) {
		if len(ops) > 3*64 {
			t.Skip()
		}

		maxLines := 2 + int(chunk%15)
		want := []string{"seed"}
		text := newText(want, maxLines)

		for i := 0; i+2 < len(ops); i += 3 {
			top := int(ops[i]) % (len(want) + 1)
			end := top + int(ops[i+1])%(len(want)-top+1)
			added := numberedLines(fmt.Sprint("edit ", i/3, " "), int(ops[i+2]%16))
			if len(want)-(end-top)+len(added) == 0 {
				added = []string{""} // A buffer keeps one line, as replay does.
			}

			// Check a range before the edit changes it.
			if got, expect := text.bytesBetween(top, end), textBytes(want[top:end]); got != expect {
				t.Fatalf("edit %d: bytes in [%d, %d) = %d, want %d", i/3, top, end, got, expect)
			}

			want = slices.Replace(want, top, end, added...)
			text.replace(top, end, added)

			if !slices.Equal(text.all(), want) || text.lines != len(want) || text.bytes != textBytes(want) {
				t.Fatalf("edit %d: chunked text differs from the flat lines", i/3)
			}
		}
	})
}

func numberedLines(prefix string, n int) []string {
	lines := make([]string, n)
	for i := range lines {
		lines[i] = fmt.Sprint(prefix, i)
	}

	return lines
}
