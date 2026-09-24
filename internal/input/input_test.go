package input_test

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/nuggocto/xunhen/internal/input"
)

func write(t *testing.T, path, content string) {
	t.Helper()

	if err := os.WriteFile(path, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
}

func readAll(r io.Reader) error {
	_, err := io.ReadAll(r)
	return err
}

func TestReadFileAcceptsOnlyRegularFiles(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		setup   func(t *testing.T, dir string) string
		max     int64
		wantErr error // nil for success; checked with errors.Is or errors.As
	}{
		{name: "regular file", max: 3, setup: func(t *testing.T, dir string) string {
			write(t, filepath.Join(dir, "f"), "abc")
			return filepath.Join(dir, "f")
		}},
		{name: "symlink to a regular file", max: 3, setup: func(t *testing.T, dir string) string {
			write(t, filepath.Join(dir, "f"), "abc")
			if err := os.Symlink("f", filepath.Join(dir, "link")); err != nil {
				t.Fatal(err)
			}
			return filepath.Join(dir, "link")
		}},
		{name: "one byte over the limit", max: 2, wantErr: &input.TooLargeError{}, setup: func(t *testing.T, dir string) string {
			write(t, filepath.Join(dir, "f"), "abc")
			return filepath.Join(dir, "f")
		}},
		{name: "directory", max: 3, wantErr: input.ErrNotRegular, setup: func(t *testing.T, dir string) string {
			return dir
		}},
		{name: "FIFO without blocking", max: 3, wantErr: input.ErrNotRegular, setup: func(t *testing.T, dir string) string {
			path := filepath.Join(dir, "fifo")
			if err := syscall.Mkfifo(path, 0600); err != nil {
				t.Fatal(err)
			}
			return path
		}},
		{name: "character device", max: 3, wantErr: input.ErrNotRegular, setup: func(t *testing.T, dir string) string {
			return os.DevNull
		}},
		{name: "missing file", max: 3, wantErr: os.ErrNotExist, setup: func(t *testing.T, dir string) string {
			return filepath.Join(dir, "missing")
		}},
		{name: "dangling symlink", max: 3, wantErr: os.ErrNotExist, setup: func(t *testing.T, dir string) string {
			if err := os.Symlink("missing", filepath.Join(dir, "link")); err != nil {
				t.Fatal(err)
			}
			return filepath.Join(dir, "link")
		}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			path := tt.setup(t, t.TempDir())
			_, err := input.ReadFile(t.Context(), path, "test", tt.max, readAll)
			checkError(t, err, tt.wantErr)
		})
	}
}

func checkError(t *testing.T, err, want error) {
	t.Helper()

	switch target := want.(type) {
	case nil:
		if err != nil {
			t.Fatalf("error = %v, want success", err)
		}
	case *input.TooLargeError:
		if !errors.As(err, &target) {
			t.Fatalf("error = %v, want a size error", err)
		}
	case *input.ChangedError:
		if !errors.As(err, &target) {
			t.Fatalf("error = %v, want a change error", err)
		}
	default:
		if !errors.Is(err, want) {
			t.Fatalf("error = %v, want %v", err, want)
		}
	}
}

// Each change happens inside the read callback, after the file is open and
// before ReadFile takes its final look, so no timing is involved.
func TestReadFileDetectsChanges(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		link   bool // read through a symlink named "link"
		change func(t *testing.T, dir string)
	}{
		{name: "growth", change: func(t *testing.T, dir string) {
			appendTo(t, filepath.Join(dir, "f"), "more")
		}},
		{name: "truncation", change: func(t *testing.T, dir string) {
			if err := os.Truncate(filepath.Join(dir, "f"), 1); err != nil {
				t.Fatal(err)
			}
		}},
		{name: "rewrite at the same size", change: func(t *testing.T, dir string) {
			write(t, filepath.Join(dir, "f"), "xyz")
			stamp := time.Date(2001, 1, 1, 0, 0, 0, 0, time.UTC)
			if err := os.Chtimes(filepath.Join(dir, "f"), stamp, stamp); err != nil {
				t.Fatal(err)
			}
		}},
		{name: "atomic replacement", change: func(t *testing.T, dir string) {
			write(t, filepath.Join(dir, "new"), "abc")
			if err := os.Rename(filepath.Join(dir, "new"), filepath.Join(dir, "f")); err != nil {
				t.Fatal(err)
			}
		}},
		{name: "deletion", change: func(t *testing.T, dir string) {
			if err := os.Remove(filepath.Join(dir, "f")); err != nil {
				t.Fatal(err)
			}
		}},
		{name: "symlink retargeted", link: true, change: func(t *testing.T, dir string) {
			write(t, filepath.Join(dir, "other"), "abc")
			if err := os.Symlink("other", filepath.Join(dir, "next")); err != nil {
				t.Fatal(err)
			}
			if err := os.Rename(filepath.Join(dir, "next"), filepath.Join(dir, "link")); err != nil {
				t.Fatal(err)
			}
		}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			dir := t.TempDir()
			write(t, filepath.Join(dir, "f"), "abc")
			path := filepath.Join(dir, "f")
			if tt.link {
				if err := os.Symlink("f", filepath.Join(dir, "link")); err != nil {
					t.Fatal(err)
				}
				path = filepath.Join(dir, "link")
			}

			_, err := input.ReadFile(t.Context(), path, "test", 1024, func(r io.Reader) error {
				if err := readAll(r); err != nil {
					return err
				}
				tt.change(t, dir)
				return nil
			})
			checkError(t, err, &input.ChangedError{})
		})
	}
}

// A file that grows while it is read must not hand the reader more than the
// size it had at open: callers charge that size against their limits.
func TestReadStopsAtTheSizeSeenAtOpen(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		initial string
		growth  int
	}{
		{name: "empty file grows", initial: "", growth: 4096},
		{name: "short file grows past the limit", initial: "abc", growth: 1 << 20},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			path := filepath.Join(t.TempDir(), "f")
			write(t, path, tt.initial)

			var got []byte
			_, err := input.ReadFile(t.Context(), path, "test", 1024, func(r io.Reader) error {
				appendTo(t, path, strings.Repeat("x", tt.growth))
				var err error
				got, err = io.ReadAll(r)
				return err
			})
			if len(got) != len(tt.initial) {
				t.Fatalf("read %d bytes of a file opened at %d", len(got), len(tt.initial))
			}
			checkError(t, err, &input.ChangedError{})
		})
	}
}

func appendTo(t *testing.T, path, text string) {
	t.Helper()

	file, err := os.OpenFile(path, os.O_WRONLY|os.O_APPEND, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := file.WriteString(text); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestRecheck(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		change  func(t *testing.T, path string)
		changed bool
	}{
		{name: "untouched", change: func(*testing.T, string) {}},
		{name: "rewritten", changed: true, change: func(t *testing.T, path string) {
			appendTo(t, path, "x")
		}},
		{name: "removed", changed: true, change: func(t *testing.T, path string) {
			if err := os.Remove(path); err != nil {
				t.Fatal(err)
			}
		}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			path := filepath.Join(t.TempDir(), "f")
			write(t, path, "abc")
			identity, err := input.ReadFile(t.Context(), path, "test", 1024, readAll)
			if err != nil {
				t.Fatal(err)
			}

			tt.change(t, path)
			err = input.Recheck(path, "test", identity)
			if tt.changed {
				checkError(t, err, &input.ChangedError{})
			} else if err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestDirEntries(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		setup   func(t *testing.T, dir string)
		regular bool
		wantErr error
	}{
		{name: "regular file", regular: true, setup: func(t *testing.T, dir string) {
			write(t, filepath.Join(dir, "entry"), "abc")
		}},
		{name: "symlink refused", wantErr: input.ErrSymlink, setup: func(t *testing.T, dir string) {
			write(t, filepath.Join(dir, "target"), "abc")
			if err := os.Symlink("target", filepath.Join(dir, "entry")); err != nil {
				t.Fatal(err)
			}
		}},
		{name: "dangling symlink refused", wantErr: input.ErrSymlink, setup: func(t *testing.T, dir string) {
			if err := os.Symlink("missing", filepath.Join(dir, "entry")); err != nil {
				t.Fatal(err)
			}
		}},
		{name: "FIFO", wantErr: input.ErrNotRegular, setup: func(t *testing.T, dir string) {
			if err := syscall.Mkfifo(filepath.Join(dir, "entry"), 0600); err != nil {
				t.Fatal(err)
			}
		}},
		{name: "subdirectory", wantErr: input.ErrNotRegular, setup: func(t *testing.T, dir string) {
			if err := os.Mkdir(filepath.Join(dir, "entry"), 0700); err != nil {
				t.Fatal(err)
			}
		}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			path := t.TempDir()
			tt.setup(t, path)
			dir, err := input.OpenDir(path)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = dir.Close() })

			entry, err := dir.Lookup("entry")
			if err != nil || !entry.Exists || entry.Regular() != tt.regular {
				t.Fatalf("lookup = %+v, %v; want an existing entry, regular %t", entry, err, tt.regular)
			}

			_, err = dir.ReadEntry(t.Context(), "entry", entry.Identity, 1024, readAll)
			checkError(t, err, tt.wantErr)
		})
	}
}

func TestDirDetectsReplacement(t *testing.T) {
	t.Parallel()

	replace := func(t *testing.T, dir string) {
		write(t, filepath.Join(dir, "new"), "abc")
		if err := os.Rename(filepath.Join(dir, "new"), filepath.Join(dir, "entry")); err != nil {
			t.Fatal(err)
		}
	}

	grow := func(t *testing.T, dir string) { appendTo(t, filepath.Join(dir, "entry"), "more") }

	tests := []struct {
		name       string
		change     func(t *testing.T, dir string)
		beforeOpen bool // change between Lookup and ReadEntry rather than while reading
	}{
		{name: "replaced after lookup", change: replace, beforeOpen: true},
		// The same file, grown: a caller that charged the looked-up size
		// must not read more than it charged.
		{name: "grown after lookup", change: grow, beforeOpen: true},
		{name: "replaced while reading", change: replace},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			path := t.TempDir()
			write(t, filepath.Join(path, "entry"), "abc")
			dir, err := input.OpenDir(path)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = dir.Close() })

			entry, err := dir.Lookup("entry")
			if err != nil {
				t.Fatal(err)
			}
			if tt.beforeOpen {
				tt.change(t, path)
			}

			read := false
			_, err = dir.ReadEntry(t.Context(), "entry", entry.Identity, 1024, func(r io.Reader) error {
				read = true
				if err := readAll(r); err != nil {
					return err
				}
				if !tt.beforeOpen {
					tt.change(t, path)
				}
				return nil
			})
			checkError(t, err, &input.ChangedError{})
			if tt.beforeOpen && read {
				t.Fatal("a file changed after its lookup was read")
			}
		})
	}
}

func TestDirPaths(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		setup   func(t *testing.T, root string) (open string, retarget func())
		openErr error
		changed bool
	}{
		{name: "directory", setup: func(t *testing.T, root string) (string, func()) {
			return root, func() {}
		}},
		{name: "retargeted directory symlink", changed: true, setup: func(t *testing.T, root string) (string, func()) {
			for _, name := range []string{"one", "two"} {
				if err := os.Mkdir(filepath.Join(root, name), 0700); err != nil {
					t.Fatal(err)
				}
			}
			link := filepath.Join(root, "link")
			if err := os.Symlink("one", link); err != nil {
				t.Fatal(err)
			}
			return link, func() {
				if err := os.Remove(link); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink("two", link); err != nil {
					t.Fatal(err)
				}
			}
		}},
		{name: "regular file", openErr: input.ErrNotDir, setup: func(t *testing.T, root string) (string, func()) {
			write(t, filepath.Join(root, "f"), "abc")
			return filepath.Join(root, "f"), nil
		}},
		{name: "missing", openErr: os.ErrNotExist, setup: func(t *testing.T, root string) (string, func()) {
			return filepath.Join(root, "missing"), nil
		}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			path, retarget := tt.setup(t, t.TempDir())
			dir, err := input.OpenDir(path)
			if tt.openErr != nil {
				checkError(t, err, tt.openErr)
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = dir.Close() })

			retarget()
			err = dir.Recheck()
			if tt.changed {
				checkError(t, err, &input.ChangedError{})
			} else if err != nil {
				t.Fatal(err)
			}
		})
	}
}

// Permission bits do not restrict the superuser, so the case only means
// something for an ordinary user.
func TestDirWithoutSearchPermission(t *testing.T) {
	t.Parallel()
	if os.Geteuid() == 0 {
		t.Skip("permission bits do not restrict root")
	}

	tests := []struct {
		name string
		mode os.FileMode
	}{
		{name: "no search permission", mode: 0600},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			path := t.TempDir()
			write(t, filepath.Join(path, "entry"), "abc")
			if err := os.Chmod(path, tt.mode); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = os.Chmod(path, 0700) })

			dir, err := input.OpenDir(path)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = dir.Close() })

			if _, err := dir.Lookup("entry"); !errors.Is(err, os.ErrPermission) {
				t.Fatalf("lookup error = %v, want a permission error", err)
			}
		})
	}
}
