package undofile_test

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/nuggocto/xunhen/internal/limits"
	"github.com/nuggocto/xunhen/internal/undofile"
)

func fixtureBytes(t testing.TB, name string) []byte {
	t.Helper()

	data, err := os.ReadFile(filepath.Join("../../testdata/undo", name, "history.undo"))
	if err != nil {
		t.Fatal(err)
	}

	return data
}

func decode(t testing.TB, data []byte) *undofile.DecodedFile {
	t.Helper()

	file, err := undofile.Decode(t.Context(), "fixture", bytes.NewReader(data), limits.Default())
	if err != nil {
		t.Fatal(err)
	}

	return file
}

func firstRecord(t testing.TB, data []byte) int {
	t.Helper()

	record, ok := decode(t, data).Record(0)
	if !ok {
		t.Fatal("fixture has no change record")
	}

	return int(record.Info().Offset)
}

// Fixed header metadata occupies 392 bytes, including its start marker.
// Skip independently framed optional fields to locate the text list.
func firstEntry(data []byte, header int) int {
	pos := header + 392
	for data[pos] != 0 {
		pos += int(data[pos]) + 2
	}

	return pos + 1
}

func put32(data []byte, offset int, value int32) {
	binary.BigEndian.PutUint32(data[offset:offset+4], uint32(value))
}

func replaceByte(offset int, value byte) func([]byte) []byte {
	return func(data []byte) []byte {
		data[offset] = value
		return data
	}
}

func replaceInt32(offset int, value int32) func([]byte) []byte {
	return func(data []byte) []byte {
		put32(data, offset, value)
		return data
	}
}

func TestDecodeStoredBytes(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name, want string
	}{
		{"linear", "stem"},
		{"latin1", "café"},
		{"invalid-utf8", "\xffseed"},
		{"embedded-nul", "a\x00b"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			data := fixtureBytes(t, tt.name)
			file := decode(t, data)

			record, ok := file.Record(0)
			if !ok {
				t.Fatal("missing first record")
			}

			entry, ok := record.Entry(0)
			if !ok {
				t.Fatal("missing first swap")
			}

			line, ok := entry.Line(0)
			if !ok || line != tt.want {
				t.Fatalf("stored line = %q, %v; want %q", line, ok, tt.want)
			}

			// Decoded text must not borrow the caller's input buffer.
			clear(data)
			got, _ := entry.Line(0)
			if got != tt.want {
				t.Fatal("decoded text shares the caller's input buffer")
			}

			for _, index := range []int{-1, entry.LineCount()} {
				if _, ok := entry.Line(index); ok {
					t.Fatalf("accepted line index %d", index)
				}
			}
		})
	}
}

func TestDecodeNativeMove(t *testing.T) {
	t.Parallel()

	t.Run("move-lines", func(t *testing.T) {
		record, _ := decode(t, fixtureBytes(t, "move-lines")).Record(0)
		if record.EntryCount() != 2 || record.ExtmarkCount() != 1 {
			t.Fatalf("move has %d text entries and %d extmarks", record.EntryCount(), record.ExtmarkCount())
		}

		mark, ok := record.Extmark(0)
		want := undofile.Extmark{
			Kind:  undofile.Move,
			Start: undofile.Extent{},
			Old:   undofile.Extent{Row: 1, Bytes: 6},
			// Destination is measured after removing "first\n".
			New: undofile.Extent{Row: 2, Bytes: 13},
		}
		if !ok || mark != want {
			t.Fatalf("native move = %+v, want %+v", mark, want)
		}
	})
}

func TestDecodeRejectsMalformedInput(t *testing.T) {
	t.Parallel()

	original := fixtureBytes(t, "linear")
	header := firstRecord(t, original)
	entry := firstEntry(original, header)
	savedLength := int(binary.BigEndian.Uint32(original[47:51]))
	options := 91 + savedLength

	tests := []struct {
		name   string
		change func([]byte) []byte
		kind   undofile.ErrorKind
		offset int64
	}{
		{"magic", replaceByte(0, 0), undofile.Invalid, 0},
		{"old version", replaceByte(10, 2), undofile.Unsupported, 9},
		{"encrypted marker", replaceByte(9, 0x80), undofile.Unsupported, 9},
		{"zero base lines", replaceInt32(43, 0), undofile.Invalid, 43},
		{"negative length", replaceInt32(47, -1), undofile.Invalid, 47},
		{"oversized line", replaceInt32(47, 1<<30), undofile.Limit, 47},
		{"oversized header count", replaceInt32(71+savedLength, 1<<30), undofile.Limit, int64(71 + savedLength)},
		{"wrong header marker", replaceByte(header, 0), undofile.Invalid, int64(header)},
		{"zero identity", replaceInt32(header+18, 0), undofile.Invalid, int64(header + 18)},
		{"negative link", replaceInt32(header+2, -1), undofile.Invalid, int64(header + 2)},
		{"bad virtual column", replaceInt32(header+34, -2), undofile.Invalid, int64(header + 34)},
		{"unknown flags", replaceByte(header+38, 0x80), undofile.Unsupported, int64(header + 38)},
		{"unknown file option", replaceByte(options+1, 2), undofile.Unsupported, int64(options + 1)},
		{"bad option length", replaceByte(options, 3), undofile.Invalid, int64(options)},
		{
			name: "duplicate option",
			change: func(data []byte) []byte {
				out := bytes.Clone(data[:options+6])
				out = append(out, data[options:options+6]...)
				return append(out, data[options+6:]...)
			},
			kind:   undofile.Invalid,
			offset: int64(options + 6),
		},
		{"unknown header option", replaceByte(header+393, 2), undofile.Unsupported, int64(header + 393)},
		{"entry marker", replaceByte(entry, 0), undofile.Invalid, int64(entry)},
		{
			name: "backward range",
			change: func(data []byte) []byte {
				put32(data, entry+2, 2)
				put32(data, entry+6, 1)
				return data
			},
			kind:   undofile.Invalid,
			offset: int64(entry + 6),
		},
		{"oversized stored count", replaceInt32(entry+14, 1<<30), undofile.Limit, int64(entry + 14)},
		{"NUL on wire", replaceByte(entry+22, 0), undofile.Invalid, int64(entry + 22)},
		{"oversized entry line", replaceInt32(entry+18, 1<<30), undofile.Limit, int64(entry + 18)},
		{"unknown native type", replaceInt32(len(original)-56, 99), undofile.Unsupported, int64(len(original) - 56)},
		{"trailing bytes", func(b []byte) []byte { return append(b, 0) }, undofile.Invalid, int64(len(original))},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			data := tt.change(bytes.Clone(original))
			file, err := undofile.Decode(t.Context(), "malformed", bytes.NewReader(data), limits.Default())

			var problem *undofile.InputError
			if file != nil || !errors.As(err, &problem) {
				t.Fatalf("file = %v; error = %v; want an input error", file, err)
			}
			if problem.Kind != tt.kind || problem.Offset != tt.offset || problem.Source != "malformed" {
				t.Fatalf("file = %v; error = %#v; want %s at %d", file, err, tt.kind, tt.offset)
			}
		})
	}
}

func TestDecodeTruncations(t *testing.T) {
	t.Parallel()

	for _, name := range []string{"linear", "move-lines"} {
		t.Run(name, func(t *testing.T) {
			data := fixtureBytes(t, name)

			// Every byte cut includes partial scalars, strings, native bodies,
			// and missing list terminators. A complete prefix is never success.
			for cut := range data {
				file, err := undofile.Decode(t.Context(), name, bytes.NewReader(data[:cut]), limits.Default())

				var problem *undofile.InputError
				if file != nil || !errors.As(err, &problem) || problem.Kind != undofile.Truncated || problem.Offset != int64(cut) {
					t.Fatalf("cut %d: file = %v; error = %#v", cut, file, err)
				}
			}
		})
	}
}

func TestDecodeErrorRecordContext(t *testing.T) {
	t.Parallel()

	original := fixtureBytes(t, "linear")
	r, _ := decode(t, original).Record(1)
	header := int(r.Info().Offset)

	tests := []struct {
		name     string
		change   func([]byte) []byte
		sequence undofile.Sequence
	}{
		{"before identity", replaceByte(header, 0), 0},
		{"after identity", replaceInt32(firstEntry(original, header)+14, 1<<30), 2},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data := tt.change(bytes.Clone(original))
			file, err := undofile.Decode(t.Context(), "fixture", bytes.NewReader(data), limits.Default())

			var problem *undofile.InputError
			if file != nil || !errors.As(err, &problem) || problem.Sequence != tt.sequence {
				t.Fatalf("error associated with wrong record: %#v", err)
			}
		})
	}
}

func TestDecodeBudgets(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		set   func(*limits.Limits)
		field string
	}{
		{"input", func(l *limits.Limits) { l.InputBytes = 64 }, "undo input bytes"},
		{"headers", func(l *limits.Limits) { l.Nodes = 2 }, "history nodes"},
		{"all entry kinds", func(l *limits.Limits) { l.Entries = 3 }, "entries"},
		{"cumulative lines", func(l *limits.Limits) { l.StoredLines = 1 }, "stored lines"},
		{"reference lines", func(l *limits.Limits) { l.StateLines = 1 }, "base lines"},
		{"saved line bytes", func(l *limits.Limits) { l.LineBytes = 1 }, "saved U line length"},
		{"cumulative options", func(l *limits.Limits) { l.OptionalBytes = 10 }, "optional-field bytes"},
		{"cumulative text", func(l *limits.Limits) { l.TextBytes = 5 }, "decoded text bytes"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			lim := limits.Default()
			tt.set(&lim)

			file, err := undofile.Decode(t.Context(), "budget", bytes.NewReader(fixtureBytes(t, "linear")), lim)

			var problem *undofile.InputError
			if file != nil || !errors.As(err, &problem) || problem.Kind != undofile.Limit || problem.Field != tt.field {
				t.Fatalf("file = %v; error = %#v; want limit on %s", file, err, tt.field)
			}
		})
	}
}

func TestDecodeReaderFailures(t *testing.T) {
	t.Parallel()

	readErr := errors.New("synthetic I/O error")
	tests := []struct {
		name     string
		reader   func([]byte, context.CancelFunc) io.Reader
		wantErr  error
		wantKind undofile.ErrorKind
	}{
		{
			name: "read error",
			reader: func(data []byte, _ context.CancelFunc) io.Reader {
				return io.MultiReader(bytes.NewReader(data[:64]), errorReader{readErr})
			},
			wantErr: readErr,
		},
		{
			name: "cancelled",
			reader: func(data []byte, cancel context.CancelFunc) io.Reader {
				cancel()
				return bytes.NewReader(data)
			},
			wantErr: context.Canceled,
		},
		{
			name: "cancel during read",
			reader: func(data []byte, cancel context.CancelFunc) io.Reader {
				return cancelReader{reader: bytes.NewReader(data), cancel: cancel}
			},
			wantErr: context.Canceled,
		},
		{
			name: "exact input limit",
			reader: func(data []byte, _ context.CancelFunc) io.Reader {
				return bytes.NewReader(data)
			},
		},
		{
			name: "input limit plus one",
			reader: func(data []byte, _ context.CancelFunc) io.Reader {
				return bytes.NewReader(append(data, 0))
			},
			wantKind: undofile.Limit,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			data := fixtureBytes(t, "linear")
			lim := limits.Default()
			lim.InputBytes = int64(len(data))

			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()

			reader := tt.reader(data, cancel)
			file, err := undofile.Decode(ctx, "reader", reader, lim)

			switch {
			case tt.wantKind != "":
				var problem *undofile.InputError
				if file != nil || !errors.As(err, &problem) || problem.Kind != tt.wantKind {
					t.Fatalf("expected input limit error; got %v", err)
				}

			case tt.wantErr != nil:
				if file != nil || !errors.Is(err, tt.wantErr) {
					t.Fatalf("file = %v; error = %v, want %v", file, err, tt.wantErr)
				}

			default:
				if err != nil || file == nil {
					t.Fatalf("exact-boundary input failed: %v", err)
				}
			}
		})
	}
}

type errorReader struct{ err error }

func (r errorReader) Read([]byte) (int, error) { return 0, r.err }

type cancelReader struct {
	reader io.Reader
	cancel context.CancelFunc
}

func (r cancelReader) Read(p []byte) (int, error) {
	n, err := r.reader.Read(p)
	r.cancel()
	return n, err
}

func FuzzDecode(f *testing.F) {
	paths, err := filepath.Glob("../../testdata/undo/*/history.undo")
	if err != nil || len(paths) == 0 {
		f.Fatalf("fixture inventory: %v", err)
	}

	for _, path := range paths {
		data, err := os.ReadFile(path)
		if err != nil {
			f.Fatal(err)
		}
		f.Add(data)
	}
	f.Add([]byte{})

	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) > 64<<10 {
			t.Skip()
		}

		lim := limits.Default()
		lim.InputBytes = 64 << 10
		lim.TextBytes = 64 << 10
		lim.LineBytes = 4096
		lim.Nodes = 128
		lim.Entries = 512
		lim.StoredLines = 2048
		lim.StateLines = 2048
		lim.OptionalBytes = 4096

		before := bytes.Clone(data)
		file, err := undofile.Decode(t.Context(), "fuzz", bytes.NewReader(data), lim)
		if !bytes.Equal(data, before) {
			t.Fatal("decoder changed the input")
		}

		if err != nil {
			if file != nil {
				t.Fatal("error returned partial records")
			}
			return
		}

		m, ok := file.Metadata()
		if !ok || m.HeaderCount > lim.Nodes || m.BaseLines < 1 {
			t.Fatal("successful decode violated its bounds")
		}

		for i := range m.HeaderCount {
			if _, ok := file.Record(i); !ok {
				t.Fatal("incomplete successful decode")
			}
		}
	})
}
