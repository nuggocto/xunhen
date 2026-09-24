// Package input opens files and directories read-only for loading histories
// and detects observable changes while they are read. It knows nothing about
// the undo format; callers pass a function that consumes the contents, which
// never run past the size the file had when it was opened.
//
// Change detection is best effort. Several reads of an ordinary filesystem do
// not form a transaction, so an input edited and restored between two checks
// can go unnoticed. What this package does catch is a file that grows, shrinks,
// or is rewritten while it is read, and a path that names a different file by
// the time reading ends, whether through atomic replacement or a retargeted
// symlink.
package input

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"syscall"
)

// Identity is what the filesystem reports about one open file. Two
// identities with the same device and inode are the same file; the size and
// times tell whether it changed.
type Identity struct {
	Device, Inode uint64
	Size          int64
	Modified      syscall.Timespec
	Changed       syscall.Timespec
	mode          uint32
}

func identityOf(st *syscall.Stat_t) Identity {
	return Identity{
		Device:   st.Dev,
		Inode:    st.Ino,
		Size:     st.Size,
		Modified: st.Mtim,
		Changed:  st.Ctim,
		mode:     st.Mode,
	}
}

// SameFile reports whether both identities name one file.
func (a Identity) SameFile(b Identity) bool {
	return a.Device == b.Device && a.Inode == b.Inode
}

// Unchanged reports whether both identities name one file with the same size
// and times.
func (a Identity) Unchanged(b Identity) bool {
	return a.SameFile(b) && a.Size == b.Size && a.Modified == b.Modified && a.Changed == b.Changed
}

func (a Identity) regular() bool { return a.mode&syscall.S_IFMT == syscall.S_IFREG }
func (a Identity) symlink() bool { return a.mode&syscall.S_IFMT == syscall.S_IFLNK }

// Errors that classify an input rather than report an I/O failure.
var (
	ErrNotRegular = errors.New("not a regular file")
	ErrSymlink    = errors.New("a symbolic link")
	ErrNotDir     = errors.New("not a directory")
)

// ChangedError reports an input that changed while it was loaded.
type ChangedError struct {
	Path, Kind, Detail string
}

func (e *ChangedError) Error() string {
	return fmt.Sprintf("%s: %s input %s; retry, or copy the inputs while the editor is idle", e.Path, e.Kind, e.Detail)
}

// TooLargeError reports a file above the caller's byte limit.
type TooLargeError struct {
	Path, Kind string
	Size, Max  int64
}

func (e *TooLargeError) Error() string {
	return fmt.Sprintf("%s: %s input has %d bytes, more than its %d-byte limit", e.Path, e.Kind, e.Size, e.Max)
}

// PathError reports a failed filesystem operation on a named input.
type PathError struct {
	Op, Path, Kind string
	Err            error
}

func (e *PathError) Error() string {
	return fmt.Sprintf("%s %s %s: %v", e.Op, e.Kind, e.Path, e.Err)
}

func (e *PathError) Unwrap() error { return e.Err }

// openFlags never block on a FIFO, never make a terminal the controlling
// terminal, and never leak into a child process.
const openFlags = syscall.O_RDONLY | syscall.O_NONBLOCK | syscall.O_NOCTTY | syscall.O_CLOEXEC

// oPath is Linux's O_PATH, which the syscall package does not export. Its
// value is the same on amd64 and arm64.
const oPath = 0x200000

// ReadFile opens path read-only, following symlinks, and passes its contents to
// read. It accepts only a regular file of at most maxBytes. read sees no more
// than the size the file had when it was opened, so a file that grows cannot
// push a read past the limit. ReadFile fails if the file changed while read
// ran or if path no longer names that file afterwards. The file is closed
// before ReadFile returns. kind names the input in messages, such as "undo" or
// "base".
func ReadFile(ctx context.Context, path, kind string, maxBytes int64, read func(io.Reader) error) (Identity, error) {
	if err := ctx.Err(); err != nil {
		return Identity{}, err
	}

	fd, err := syscall.Open(path, openFlags, 0)
	if err != nil {
		return Identity{}, &PathError{Op: "open", Path: path, Kind: kind + " file", Err: err}
	}

	return readOpen(ctx, os.NewFile(uintptr(fd), path), path, kind, maxBytes, nil, read, func() (Identity, error) {
		return Stat(path)
	})
}

// Stat returns the identity of the file path names, following symlinks.
func Stat(path string) (Identity, error) {
	var st syscall.Stat_t
	if err := syscall.Stat(path, &st); err != nil {
		return Identity{}, err
	}

	return identityOf(&st), nil
}

// Recheck confirms that path still names the file loaded with identity, with
// the same size and times.
func Recheck(path, kind string, identity Identity) error {
	now, err := Stat(path)
	if err != nil || !now.Unchanged(identity) {
		return &ChangedError{Path: path, Kind: kind, Detail: "changed after it was read"}
	}

	return nil
}

// readOpen checks, reads, and closes one open file. When expected is set, the
// open file must be that file, unchanged since it was looked up. named returns
// the identity that the file's name refers to once reading is done.
func readOpen(ctx context.Context, file *os.File, path, kind string, maxBytes int64, expected *Identity, read func(io.Reader) error, named func() (Identity, error)) (identity Identity, err error) {
	defer func() {
		if closeErr := file.Close(); closeErr != nil && err == nil {
			err = &PathError{Op: "close", Path: path, Kind: kind + " file", Err: closeErr}
		}
	}()

	before, err := fstat(file)
	if err != nil {
		return Identity{}, &PathError{Op: "stat", Path: path, Kind: kind + " file", Err: err}
	}
	if expected != nil && !before.Unchanged(*expected) {
		return Identity{}, &ChangedError{Path: path, Kind: kind, Detail: "changed or was replaced after it was found"}
	}
	if !before.regular() {
		return Identity{}, &PathError{Op: "read", Path: path, Kind: kind + " file", Err: ErrNotRegular}
	}
	if before.Size > maxBytes {
		return Identity{}, &TooLargeError{Path: path, Kind: kind, Size: before.Size, Max: maxBytes}
	}

	// Growth during the read is caught by the check below, but the reader
	// stops at the size seen at open, so the bytes read stay within maxBytes
	// and within whatever budget the caller charged for this file.
	if err := read(io.LimitReader(file, before.Size)); err != nil {
		return Identity{}, err
	}

	after, err := fstat(file)
	if err != nil {
		return Identity{}, &PathError{Op: "stat", Path: path, Kind: kind + " file", Err: err}
	}
	if !after.Unchanged(before) {
		return Identity{}, &ChangedError{Path: path, Kind: kind, Detail: "changed while it was read"}
	}

	// A rename over the path or a retargeted symlink leaves this file intact
	// but makes the path name something else.
	if current, err := named(); err != nil || !current.SameFile(after) {
		return Identity{}, &ChangedError{Path: path, Kind: kind, Detail: "was replaced while it was read"}
	}

	if err := ctx.Err(); err != nil {
		return Identity{}, err
	}

	return after, nil
}

func fstat(file *os.File) (Identity, error) {
	raw, err := file.SyscallConn()
	if err != nil {
		return Identity{}, err
	}

	var st syscall.Stat_t
	var statErr error
	if err := raw.Control(func(fd uintptr) { statErr = syscall.Fstat(int(fd), &st) }); err != nil {
		return Identity{}, err
	}
	if statErr != nil {
		return Identity{}, statErr
	}

	return identityOf(&st), nil
}

// Dir is a directory held open while its entries are examined, so that every
// lookup refers to the directory that was opened even if its path is
// retargeted meanwhile.
type Dir struct {
	path     string
	fd       int
	identity Identity
}

// OpenDir opens a directory, following symlinks in path.
func OpenDir(path string) (*Dir, error) {
	fd, err := syscall.Open(path, syscall.O_RDONLY|syscall.O_DIRECTORY|syscall.O_CLOEXEC, 0)
	if err != nil {
		if errors.Is(err, syscall.ENOTDIR) {
			err = ErrNotDir
		}
		return nil, &PathError{Op: "open", Path: path, Kind: "undo directory", Err: err}
	}

	var st syscall.Stat_t
	if err := syscall.Fstat(fd, &st); err != nil {
		_ = syscall.Close(fd) // The stat failure is the error worth reporting.
		return nil, &PathError{Op: "stat", Path: path, Kind: "undo directory", Err: err}
	}

	return &Dir{path: path, fd: fd, identity: identityOf(&st)}, nil
}

// Path is the path the directory was opened with.
func (d *Dir) Path() string { return d.path }

// Identity identifies the open directory.
func (d *Dir) Identity() Identity { return d.identity }

// Close releases the directory handle.
func (d *Dir) Close() error { return syscall.Close(d.fd) }

// Recheck confirms that the directory's path still names this directory.
func (d *Dir) Recheck() error {
	now, err := Stat(d.path)
	if err != nil || !now.SameFile(d.identity) {
		return &ChangedError{Path: d.path, Kind: "undo directory", Detail: "now names a different directory"}
	}

	return nil
}

// Entry describes one name in a directory without following a symlink.
type Entry struct {
	Exists   bool
	Identity Identity
}

// Regular reports whether the entry is a regular file.
func (e Entry) Regular() bool { return e.Exists && e.Identity.regular() }

// Symlink reports whether the entry is a symbolic link.
func (e Entry) Symlink() bool { return e.Exists && e.Identity.symlink() }

// Same reports whether two lookups of one name found the same state: both
// absent, or both the same unchanged file.
func (e Entry) Same(other Entry) bool {
	if !e.Exists || !other.Exists {
		return e.Exists == other.Exists
	}

	return e.Identity.Unchanged(other.Identity)
}

// Lookup reports what name refers to in the directory, without following a
// symlink or opening the entry for I/O: an O_PATH descriptor cannot read, so
// looking up a device or FIFO has no effect on it. name must be one path
// component. A name longer than the filesystem allows cannot exist, so it is
// reported as absent rather than as a failure.
func (d *Dir) Lookup(name string) (Entry, error) {
	fd, err := syscall.Openat(d.fd, name, oPath|syscall.O_NOFOLLOW|syscall.O_CLOEXEC, 0)
	if errors.Is(err, syscall.ENOENT) || errors.Is(err, syscall.ENAMETOOLONG) {
		return Entry{}, nil
	}
	if err != nil {
		return Entry{}, &PathError{Op: "look up", Path: d.join(name), Kind: "undo candidate", Err: err}
	}
	// A path descriptor has nothing to flush, so closing it cannot lose data.
	defer func() { _ = syscall.Close(fd) }()

	var st syscall.Stat_t
	if err := syscall.Fstat(fd, &st); err != nil {
		return Entry{}, &PathError{Op: "stat", Path: d.join(name), Kind: "undo candidate", Err: err}
	}

	return Entry{Exists: true, Identity: identityOf(&st)}, nil
}

// ReadEntry opens name in the directory and reads it like ReadFile, except
// that a symlink is refused at open time rather than followed. The opened
// file must be the one an earlier Lookup found, with the same size and times,
// so a caller that charged the looked-up size for the read gets no more than
// that. name must be one path component.
func (d *Dir) ReadEntry(ctx context.Context, name string, expected Identity, maxBytes int64, read func(io.Reader) error) (Identity, error) {
	path := d.join(name)
	if err := ctx.Err(); err != nil {
		return Identity{}, err
	}

	fd, err := syscall.Openat(d.fd, name, openFlags|syscall.O_NOFOLLOW, 0)
	if errors.Is(err, syscall.ELOOP) {
		err = ErrSymlink
	}
	if err != nil {
		return Identity{}, &PathError{Op: "open", Path: path, Kind: "undo candidate", Err: err}
	}

	return readOpen(ctx, os.NewFile(uintptr(fd), path), path, "undo", maxBytes, &expected, read, func() (Identity, error) {
		entry, err := d.Lookup(name)
		if err != nil || !entry.Exists {
			return Identity{}, errors.Join(err, fs.ErrNotExist)
		}
		return entry.Identity, nil
	})
}

func (d *Dir) join(name string) string {
	if len(d.path) != 0 && d.path[len(d.path)-1] == '/' {
		return d.path + name
	}

	return d.path + "/" + name
}
