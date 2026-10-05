package tarball

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"io"
	"slices"
	"strings"
	"testing"
	"time"
)

var epoch = time.Date(2026, 9, 27, 12, 0, 0, 0, time.UTC)

func sample() []File {
	return []File{
		{Name: "docs/note.txt", Data: []byte("note\n")},
		{Name: "xunhen", Executable: true, Data: []byte("\x7fELF")},
		{Name: "README.md", Data: []byte("readme\n")},
	}
}

func written(t *testing.T, files []File) []byte {
	t.Helper()
	var out bytes.Buffer
	if err := Write(&out, "xunhen_1.0.0_linux_amd64", files, epoch); err != nil {
		t.Fatal(err)
	}
	return out.Bytes()
}

// Two builds that list the same files in another order, at another time of
// day within the same second, must produce the same bytes, and reading them
// back must return the same files.
func TestWriteIsDeterministic(t *testing.T) {
	t.Parallel()

	first := written(t, sample())
	shuffled := sample()
	shuffled[0], shuffled[2] = shuffled[2], shuffled[0]
	var second bytes.Buffer
	if err := Write(&second, "xunhen_1.0.0_linux_amd64", shuffled, epoch.Add(300*time.Millisecond)); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(first, second.Bytes()) {
		t.Fatal("the same files in another order gave different archive bytes")
	}

	a, err := Read(bytes.NewReader(first))
	if err != nil {
		t.Fatal(err)
	}
	if a.Prefix != "xunhen_1.0.0_linux_amd64" || !a.MTime.Equal(epoch) || !Equal(a.Files, sample()) {
		t.Fatalf("read back %q at %v with files %+v", a.Prefix, a.MTime, a.Files)
	}
	if exe, _ := a.Lookup("xunhen"); !exe.Executable {
		t.Fatal("the executable lost its mode")
	}
}

func TestWriteRejectsBadNames(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		files []File
	}{
		{name: "absolute", files: []File{{Name: "/etc/passwd"}}},
		{name: "parent", files: []File{{Name: "docs/../../x"}}},
		{name: "dot", files: []File{{Name: "./x"}}},
		{name: "duplicate", files: []File{{Name: "x"}, {Name: "x"}}},
		{name: "file and directory", files: []File{{Name: "docs"}, {Name: "docs/a"}}},
		{name: "control character", files: []File{{Name: "a\x1b[2J"}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if err := Write(&bytes.Buffer{}, "top", tt.files, epoch); err == nil {
				t.Fatal("Write accepted the entries")
			}
		})
	}
}

// hand builds an archive entry by entry, the way a tampered or foreign
// archive would be made, without Write's normalization.
func hand(t *testing.T, headers []*tar.Header, gzipName string) []byte {
	t.Helper()
	var out bytes.Buffer
	zw := gzip.NewWriter(&out)
	zw.Name = gzipName
	tw := tar.NewWriter(zw)
	for _, shared := range headers {
		// Cases share headers and run in parallel, so fill in a copy.
		h := *shared
		if h.ModTime.IsZero() {
			h.ModTime = epoch
		}
		if err := tw.WriteHeader(&h); err != nil {
			t.Fatal(err)
		}
		if h.Typeflag == tar.TypeReg {
			if _, err := tw.Write(bytes.Repeat([]byte("x"), int(h.Size))); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return out.Bytes()
}

func TestReadRejectsUnsafeArchives(t *testing.T) {
	t.Parallel()

	top := &tar.Header{Name: "top/", Typeflag: tar.TypeDir, Mode: ModeDir}
	file := func(name string, mode int64) *tar.Header {
		return &tar.Header{Name: name, Typeflag: tar.TypeReg, Mode: mode, Size: 1}
	}

	tests := []struct {
		name     string
		headers  []*tar.Header
		gzipName string
		want     string
	}{
		{name: "absolute path", headers: []*tar.Header{top, file("/top/x", ModeFile)}, want: "absolute"},
		{name: "traversal", headers: []*tar.Header{top, file("top/../x", ModeFile)}, want: `".."`},
		{name: "outside the top directory", headers: []*tar.Header{top, file("other/x", ModeFile)}, want: "outside"},
		{name: "duplicate", headers: []*tar.Header{top, file("top/x", ModeFile), file("top/x", ModeFile)}, want: "twice"},
		{name: "symlink", headers: []*tar.Header{top, {Name: "top/x", Typeflag: tar.TypeSymlink, Linkname: "/etc/passwd"}}, want: "type"},
		{name: "hard link", headers: []*tar.Header{top, {Name: "top/x", Typeflag: tar.TypeLink, Linkname: "top/y"}}, want: "type"},
		{name: "device", headers: []*tar.Header{top, {Name: "top/x", Typeflag: tar.TypeChar, Mode: ModeFile}}, want: "type"},
		{name: "world-writable file", headers: []*tar.Header{top, file("top/x", 0o666)}, want: "mode"},
		{name: "setuid executable", headers: []*tar.Header{top, file("top/x", 0o4755)}, want: "mode"},
		{name: "owned entry", headers: []*tar.Header{top, {Name: "top/x", Typeflag: tar.TypeReg, Mode: ModeFile, Uid: 1000, Size: 1}}, want: "owner"},
		{name: "differing time", headers: []*tar.Header{top, {Name: "top/x", Typeflag: tar.TypeReg, Mode: ModeFile, Size: 1, ModTime: epoch.Add(time.Hour)}}, want: "time"},
		{name: "missing directory", headers: []*tar.Header{top, file("top/docs/x", ModeFile)}, want: "before its directory"},
		{name: "no top directory", headers: []*tar.Header{file("x", ModeFile)}, want: "top directory"},
		{name: "named gzip member", headers: []*tar.Header{top}, gzipName: "build/host/path.tar", want: "gzip header"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			_, err := Read(bytes.NewReader(hand(t, tt.headers, tt.gzipName)))
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("Read returned %v, want an error about %q", err, tt.want)
			}
		})
	}
}

// Bytes a tar reader never looks at still change the archive's checksum,
// and a damaged gzip trailer means damaged contents. An archive with either
// is not one Write made. Each case damages a correct archive in one way.
func TestReadRejectsDamagedStreams(t *testing.T) {
	t.Parallel()

	// padded is a correct archive with zero blocks after the tar end
	// marker, inside the gzip member, as GNU tar's record padding adds.
	padded := func() []byte {
		var tarData bytes.Buffer
		tw := tar.NewWriter(&tarData)
		if err := tw.WriteHeader(&tar.Header{Name: "top/", Typeflag: tar.TypeDir, Mode: ModeDir, ModTime: epoch, Format: tar.FormatUSTAR}); err != nil {
			t.Fatal(err)
		}
		if err := tw.Close(); err != nil {
			t.Fatal(err)
		}
		tarData.Write(make([]byte, 8192))
		var out bytes.Buffer
		zw := gzip.NewWriter(&out)
		if _, err := zw.Write(tarData.Bytes()); err != nil {
			t.Fatal(err)
		}
		if err := zw.Close(); err != nil {
			t.Fatal(err)
		}
		return out.Bytes()
	}

	tests := []struct {
		name string
		data func() []byte
		want string
	}{
		{name: "a byte after the gzip stream", data: func() []byte { return append(written(t, sample()), 0) }, want: "follows the gzip stream"},
		{name: "a second gzip member", data: func() []byte {
			return append(written(t, sample()), written(t, sample())...)
		}, want: "follows the gzip stream"},
		{name: "a wrong CRC", data: func() []byte {
			data := written(t, sample())
			data[len(data)-8] ^= 0xff
			return data
		}, want: "checksum"},
		{name: "data after the tar end marker", data: padded, want: "account for"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			// A reader that is not a ByteReader, like a file, must be
			// judged the same way as a byte slice.
			_, err := Read(struct{ io.Reader }{bytes.NewReader(tt.data())})
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("Read returned %v, want an error about %q", err, tt.want)
			}
		})
	}
}

// archive/tar consumes GNU long-name headers inside Next, so neither an
// entry count nor a header check sees them. Each case places such headers
// around one ordinary file: before an entry they make a format Write never
// produces, after the last entry they leave blocks no entry accounts for,
// and many of them are bounded by the size of the uncompressed stream.
func TestReadRejectsHiddenHeaders(t *testing.T) {
	t.Parallel()

	// blocks writes headers with archive/tar and returns the raw blocks,
	// without the end marker.
	blocks := func(t *testing.T, headers ...*tar.Header) []byte {
		t.Helper()
		var raw bytes.Buffer
		tw := tar.NewWriter(&raw)
		for _, h := range headers {
			if err := tw.WriteHeader(h); err != nil {
				t.Fatal(err)
			}
			if _, err := tw.Write(bytes.Repeat([]byte("x"), int(h.Size))); err != nil {
				t.Fatal(err)
			}
		}
		if err := tw.Flush(); err != nil {
			t.Fatal(err)
		}
		return raw.Bytes()
	}
	gzipped := func(t *testing.T, parts ...[]byte) []byte {
		t.Helper()
		var out bytes.Buffer
		zw := gzip.NewWriter(&out)
		for _, p := range append(parts, make([]byte, 2*blockSize)) {
			if _, err := zw.Write(p); err != nil {
				t.Fatal(err)
			}
		}
		if err := zw.Close(); err != nil {
			t.Fatal(err)
		}
		return out.Bytes()
	}

	start := blocks(t,
		&tar.Header{Name: "top/", Typeflag: tar.TypeDir, Mode: ModeDir, ModTime: epoch},
		&tar.Header{Name: "top/x", Typeflag: tar.TypeReg, Mode: ModeFile, ModTime: epoch, Size: 3})
	// A file with a long name in GNU format is a long-name header, a block
	// holding the name, and the file's own header block.
	gnu := blocks(t, &tar.Header{Name: "top/" + strings.Repeat("a", 120), Typeflag: tar.TypeReg, Mode: ModeFile, ModTime: epoch, Format: tar.FormatGNU})
	longName, gnuFile := gnu[:len(gnu)-blockSize], gnu[len(gnu)-blockSize:]

	tests := []struct {
		name  string
		parts [][]byte
		limit int64
		want  string
	}{
		{name: "a long-name header before a file", parts: [][]byte{start, longName, gnuFile}, limit: maxStreamBytes, want: "tar format"},
		{name: "a long-name header after the last file", parts: [][]byte{start, longName}, limit: maxStreamBytes, want: "account for"},
		{name: "more long-name headers than the stream holds", parts: append([][]byte{start}, append(slices.Repeat([][]byte{longName}, 100), gnuFile)...), limit: 64 << 10, want: errStreamTooLarge.Error()},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			_, err := read(bytes.NewReader(gzipped(t, tt.parts...)), tt.limit)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("read returned %v, want an error about %q", err, tt.want)
			}
		})
	}
}
