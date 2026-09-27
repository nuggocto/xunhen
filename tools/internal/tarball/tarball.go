// Package tarball writes and reads the release archives. Writing is
// deterministic: the same files give the same bytes on any machine with the
// same Go toolchain, whatever the host's tar and gzip versions, file order,
// umask, owner, or clock. Reading accepts only what writing produces, so a
// tampered or hand-made archive fails instead of being unpacked.
package tarball

import (
	"archive/tar"
	"bufio"
	"bytes"
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"path"
	"slices"
	"strings"
	"time"
)

// Modes are the only permissions an archive entry can have.
const (
	ModeFile       = 0o644
	ModeExecutable = 0o755
	ModeDir        = 0o755
)

// Limits bound what Read accepts. A release archive holds a few hundred
// small files and one executable; these leave wide room above that and still
// stop a decompression bomb.
const (
	maxEntries    = 4096
	maxEntryBytes = 64 << 20
	maxTotalBytes = 256 << 20
)

// File is one regular file of an archive. Name is relative to the archive's
// top directory and uses slashes.
type File struct {
	Name       string
	Executable bool
	Data       []byte
}

// Write writes files under the top directory prefix as a gzip-compressed tar
// stream. Every entry gets modification time mtime, owner and group 0 with no
// names, and mode 0644, or 0755 for executables and directories. Entries are
// sorted by name, and each directory is written before its contents. The
// gzip header records no name, time, or operating system.
func Write(w io.Writer, prefix string, files []File, mtime time.Time) error {
	if err := checkName(prefix); err != nil || strings.Contains(prefix, "/") {
		return fmt.Errorf("archive prefix %q must be one path component", prefix)
	}

	byName := map[string]File{}
	dirs := map[string]bool{prefix: true}
	for _, f := range files {
		if err := checkName(f.Name); err != nil {
			return err
		}
		full := prefix + "/" + f.Name
		if _, dup := byName[full]; dup {
			return fmt.Errorf("archive entry %q appears twice", f.Name)
		}
		byName[full] = f
		for dir := path.Dir(full); dir != "."; dir = path.Dir(dir) {
			dirs[dir] = true
		}
	}
	for dir := range dirs {
		if _, clash := byName[dir]; clash {
			return fmt.Errorf("archive entry %q is both a file and a directory", dir)
		}
	}

	names := make([]string, 0, len(byName)+len(dirs))
	for name := range byName {
		names = append(names, name)
	}
	for dir := range dirs {
		names = append(names, dir+"/")
	}
	// A directory name ends in a slash, which sorts it before its contents.
	slices.Sort(names)

	zw, err := gzip.NewWriterLevel(w, gzip.BestCompression)
	if err != nil {
		return err
	}
	zw.OS = 255 // unknown, rather than the building machine's
	tw := tar.NewWriter(zw)

	mtime = mtime.UTC().Truncate(time.Second)
	for _, name := range names {
		header := &tar.Header{Name: name, ModTime: mtime, Format: tar.FormatPAX}
		f, isFile := byName[name]
		switch {
		case !isFile:
			header.Typeflag, header.Mode = tar.TypeDir, ModeDir
		case f.Executable:
			header.Typeflag, header.Mode, header.Size = tar.TypeReg, ModeExecutable, int64(len(f.Data))
		default:
			header.Typeflag, header.Mode, header.Size = tar.TypeReg, ModeFile, int64(len(f.Data))
		}
		if err := tw.WriteHeader(header); err != nil {
			return err
		}
		if isFile {
			if _, err := tw.Write(f.Data); err != nil {
				return err
			}
		}
	}

	if err := tw.Close(); err != nil {
		return err
	}
	return zw.Close()
}

// Archive is a validated archive: its top directory, the modification time
// every entry shares, its directories, and its files in archive order.
type Archive struct {
	Prefix string
	MTime  time.Time
	Dirs   []string
	Files  []File
}

// Lookup returns the file with a name relative to the top directory.
func (a *Archive) Lookup(name string) (File, bool) {
	for _, f := range a.Files {
		if f.Name == name {
			return f, true
		}
	}
	return File{}, false
}

// Read reads and validates an archive that Write produced. It rejects
// absolute names, names with "." or ".." components, entries outside one top
// directory, duplicates, links and every type other than regular files and
// directories, entries whose parent directory is missing, owners other than
// 0, any other mode, differing times, and oversized contents.
func Read(r io.Reader) (*Archive, error) {
	// gzip reads straight from a ByteReader without buffering ahead, so
	// whatever follows the gzip member is still in br afterwards.
	br := bufio.NewReader(r)
	zr, err := gzip.NewReader(br)
	if err != nil {
		return nil, fmt.Errorf("read gzip header: %w", err)
	}
	zr.Multistream(false)
	if zr.Name != "" || zr.Comment != "" || len(zr.Extra) != 0 || !zr.ModTime.IsZero() {
		return nil, errors.New("gzip header records a name, comment, extra field, or time")
	}

	a := &Archive{}
	seen := map[string]bool{}
	total := int64(0)
	tr := tar.NewReader(zr)
	for count := 0; ; count++ {
		header, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("read tar entry: %w", err)
		}
		if count == maxEntries {
			return nil, fmt.Errorf("archive has more than %d entries", maxEntries)
		}

		name, err := a.entryName(header)
		if err != nil {
			return nil, err
		}
		if seen[name] {
			return nil, fmt.Errorf("archive entry %q appears twice", header.Name)
		}
		if name != a.Prefix && !seen[path.Dir(name)] {
			return nil, fmt.Errorf("archive entry %q comes before its directory", header.Name)
		}
		seen[name] = true
		if err := a.checkHeader(header); err != nil {
			return nil, err
		}

		if header.Typeflag == tar.TypeDir {
			a.Dirs = append(a.Dirs, name)
			continue
		}
		if header.Size > maxEntryBytes || total+header.Size > maxTotalBytes {
			return nil, fmt.Errorf("archive entry %q makes the archive too large", header.Name)
		}
		total += header.Size
		data, err := io.ReadAll(io.LimitReader(tr, header.Size))
		if err != nil {
			return nil, fmt.Errorf("read %q: %w", header.Name, err)
		}
		a.Files = append(a.Files, File{
			Name:       strings.TrimPrefix(name, a.Prefix+"/"),
			Executable: header.Mode == ModeExecutable,
			Data:       data,
		})
	}

	if a.Prefix == "" {
		return nil, errors.New("archive is empty")
	}
	// Read the gzip member to its end, which checks its CRC and length:
	// the tar reader stops at the end-of-archive marker and would leave
	// the trailer unread. Nothing may follow that marker inside the member,
	// and nothing may follow the member: either would be data that a tar
	// reader ignores and a checksum still covers.
	if n, err := io.Copy(io.Discard, io.LimitReader(zr, 1<<20)); err != nil {
		return nil, fmt.Errorf("read the end of the gzip stream: %w", err)
	} else if n != 0 {
		return nil, errors.New("data follows the end of the tar archive")
	}
	if _, err := br.ReadByte(); !errors.Is(err, io.EOF) {
		return nil, errors.New("data follows the gzip stream")
	}
	return a, nil
}

// entryName validates a header's name and returns it without a trailing
// slash. The first entry fixes the top directory.
func (a *Archive) entryName(header *tar.Header) (string, error) {
	name := header.Name
	if header.Typeflag == tar.TypeDir {
		name = strings.TrimSuffix(name, "/")
	}
	if err := checkName(name); err != nil {
		return "", err
	}

	if a.Prefix == "" {
		if header.Typeflag != tar.TypeDir || strings.Contains(name, "/") {
			return "", fmt.Errorf("archive does not start with its top directory: %q", header.Name)
		}
		a.Prefix = name
		a.MTime = header.ModTime
		return name, nil
	}
	if !strings.HasPrefix(name, a.Prefix+"/") {
		return "", fmt.Errorf("archive entry %q is outside %q", header.Name, a.Prefix)
	}
	return name, nil
}

func (a *Archive) checkHeader(header *tar.Header) error {
	switch {
	case header.Typeflag != tar.TypeDir && header.Typeflag != tar.TypeReg:
		return fmt.Errorf("archive entry %q has type %q; only files and directories are allowed", header.Name, header.Typeflag)
	case header.Uid != 0 || header.Gid != 0 || header.Uname != "" || header.Gname != "":
		return fmt.Errorf("archive entry %q has an owner", header.Name)
	case !header.ModTime.Equal(a.MTime):
		return fmt.Errorf("archive entry %q has a different time", header.Name)
	case len(header.PAXRecords) != 0:
		return fmt.Errorf("archive entry %q has extended attributes", header.Name)
	case header.Typeflag == tar.TypeDir && header.Mode != ModeDir:
		return fmt.Errorf("archive directory %q has mode %04o", header.Name, header.Mode)
	case header.Typeflag == tar.TypeReg && header.Mode != ModeFile && header.Mode != ModeExecutable:
		return fmt.Errorf("archive file %q has mode %04o", header.Name, header.Mode)
	}
	return nil
}

// checkName accepts a relative slash-separated name whose components are
// neither empty, ".", nor "..", and which holds no control characters.
func checkName(name string) error {
	if name == "" || strings.HasPrefix(name, "/") {
		return fmt.Errorf("archive name %q is empty or absolute", name)
	}
	for part := range strings.SplitSeq(name, "/") {
		if part == "" || part == "." || part == ".." {
			return fmt.Errorf("archive name %q has an empty, \".\", or \"..\" component", name)
		}
	}
	if strings.ContainsFunc(name, func(r rune) bool { return r < 0x20 || r == 0x7f || r == '\\' }) {
		return fmt.Errorf("archive name %q holds a control character or backslash", name)
	}
	return nil
}

// Equal reports whether two file lists hold the same names, modes, and
// contents, in any order.
func Equal(a, b []File) bool {
	if len(a) != len(b) {
		return false
	}
	index := map[string]File{}
	for _, f := range a {
		index[f.Name] = f
	}
	for _, f := range b {
		g, ok := index[f.Name]
		if !ok || g.Executable != f.Executable || !bytes.Equal(g.Data, f.Data) {
			return false
		}
	}
	return true
}
