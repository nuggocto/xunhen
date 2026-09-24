package discover_test

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"

	"github.com/nuggocto/xunhen/internal/discover"
	"github.com/nuggocto/xunhen/internal/limits"
	"github.com/nuggocto/xunhen/internal/undofile"
)

func fixture(t testing.TB, name, file string) []byte {
	t.Helper()

	data, err := os.ReadFile(filepath.Join("../../testdata/undo", name, file))
	if err != nil {
		t.Fatal(err)
	}

	return data
}

// world is a temporary layout: a source file holding the abandoned-branch
// base text, and undo directories u1 and u2 beside it.
type world struct {
	t      testing.TB
	root   string
	source string
	target discover.Target
	base   *undofile.PreparedBase
}

func newWorld(t testing.TB) *world {
	t.Helper()

	root := physicalRoot(t)
	for _, dir := range []string{"src", "u1", "u2"} {
		if err := os.Mkdir(filepath.Join(root, dir), 0700); err != nil {
			t.Fatal(err)
		}
	}

	text := fixture(t, "abandoned-branch", "base.bin")
	source := filepath.Join(root, "src", "retry.go")
	if err := os.WriteFile(source, text, 0600); err != nil {
		t.Fatal(err)
	}

	lines := strings.Split(strings.TrimSuffix(string(text), "\n"), "\n")
	base, err := undofile.PrepareBase(t.Context(), source, lines, limits.Default())
	if err != nil {
		t.Fatal(err)
	}

	return &world{t: t, root: root, source: source, target: discover.Resolve(source, root), base: base}
}

func (w *world) path(parts ...string) string {
	return filepath.Join(append([]string{w.root}, parts...)...)
}

// deepen moves the source into a directory deep enough that its encoded undo
// name is longer than the 255 bytes a directory entry allows.
func (w *world) deepen() {
	w.t.Helper()

	dir := w.path("src", strings.Repeat("deep-directory-name/", 14))
	if err := os.MkdirAll(dir, 0700); err != nil {
		w.t.Fatal(err)
	}
	w.source = filepath.Join(dir, "retry.go")
	w.write(w.source, fixture(w.t, "abandoned-branch", "base.bin"))
	w.target = discover.Resolve(w.source, w.root)
	if len(w.target.UndoName()) <= 255 {
		w.t.Fatalf("encoded name of %d bytes is not too long", len(w.target.UndoName()))
	}
}

// put writes data under the source's undo name in dir, or under the sidecar
// name when sidecar is set. An absolute dir is used as it is.
func (w *world) put(dir string, data []byte, sidecar bool) string {
	w.t.Helper()

	name := w.target.UndoName()
	if sidecar {
		name = w.target.SidecarName()
	}
	if !filepath.IsAbs(dir) {
		dir = w.path(dir)
	}
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, data, 0600); err != nil {
		w.t.Fatal(err)
	}

	return path
}

func (w *world) search(dirs []string, lim limits.Limits, keepUnverified bool) *discover.Result {
	w.t.Helper()

	result, err := discover.Search(w.t.Context(), discover.Request{
		Target: w.target, Dirs: dirs, Base: w.base, KeepUnverified: keepUnverified,
	}, lim)
	if err != nil {
		w.t.Fatalf("search failed: %v", err)
	}

	return result
}

func TestSearchSelection(t *testing.T) {
	t.Parallel()

	good := func(t testing.TB) []byte { return fixture(t, "abandoned-branch", "history.undo") }
	other := func(t testing.TB) []byte { return fixture(t, "linear", "history.undo") }

	tests := []struct {
		name    string
		setup   func(w *world) (dirs []string, want string)
		problem discover.Problem // zero when a verified history is expected
	}{
		{name: "no candidate", problem: discover.NotFound, setup: func(w *world) ([]string, string) {
			return []string{w.path("u1"), w.path("u2")}, ""
		}},
		{name: "one match", setup: func(w *world) ([]string, string) {
			return []string{w.path("u1"), w.path("u2")}, w.put("u2", good(w.t), false)
		}},
		{name: "copies in two directories", problem: discover.Ambiguous, setup: func(w *world) ([]string, string) {
			w.put("u1", good(w.t), false)
			w.put("u2", good(w.t), false)
			return []string{w.path("u1"), w.path("u2")}, ""
		}},
		{name: "one directory supplied twice", setup: func(w *world) ([]string, string) {
			return []string{w.path("u1"), w.path("u1") + "/"}, w.put("u1", good(w.t), false)
		}},
		{name: "directory and a symlink to it", setup: func(w *world) ([]string, string) {
			if err := os.Symlink("u1", w.path("alias")); err != nil {
				w.t.Fatal(err)
			}
			return []string{w.path("u1"), w.path("alias")}, w.put("u1", good(w.t), false)
		}},
		{name: "hard link in a second directory", setup: func(w *world) ([]string, string) {
			first := w.put("u1", good(w.t), false)
			if err := os.Link(first, filepath.Join(w.path("u2"), w.target.UndoName())); err != nil {
				w.t.Fatal(err)
			}
			return []string{w.path("u1"), w.path("u2")}, first
		}},
		{name: "history for other text", problem: discover.Mismatch, setup: func(w *world) ([]string, string) {
			w.put("u1", other(w.t), false)
			return []string{w.path("u1")}, ""
		}},
		{name: "match beside a history for other text", setup: func(w *world) ([]string, string) {
			w.put("u1", other(w.t), false)
			return []string{w.path("u1"), w.path("u2")}, w.put("u2", good(w.t), false)
		}},
		{name: "malformed candidate", problem: discover.Invalid, setup: func(w *world) ([]string, string) {
			w.put("u1", []byte("not an undo file"), false)
			return []string{w.path("u1")}, ""
		}},
		{name: "match beside a malformed candidate", setup: func(w *world) ([]string, string) {
			w.put("u1", []byte("not an undo file"), false)
			return []string{w.path("u1"), w.path("u2")}, w.put("u2", good(w.t), false)
		}},
		{name: "candidate symlink", problem: discover.Incomplete, setup: func(w *world) ([]string, string) {
			w.put("u2", good(w.t), false)
			if err := os.Symlink(filepath.Join(w.path("u2"), w.target.UndoName()), filepath.Join(w.path("u1"), w.target.UndoName())); err != nil {
				w.t.Fatal(err)
			}
			return []string{w.path("u1")}, ""
		}},
		{name: "dangling candidate symlink", problem: discover.Incomplete, setup: func(w *world) ([]string, string) {
			if err := os.Symlink("missing", filepath.Join(w.path("u1"), w.target.UndoName())); err != nil {
				w.t.Fatal(err)
			}
			return []string{w.path("u1")}, ""
		}},
		{name: "FIFO at the candidate name", problem: discover.Invalid, setup: func(w *world) ([]string, string) {
			if err := syscall.Mkfifo(filepath.Join(w.path("u1"), w.target.UndoName()), 0600); err != nil {
				w.t.Fatal(err)
			}
			return []string{w.path("u1")}, ""
		}},
		{name: "directory at the candidate name", problem: discover.Invalid, setup: func(w *world) ([]string, string) {
			if err := os.Mkdir(filepath.Join(w.path("u1"), w.target.UndoName()), 0700); err != nil {
				w.t.Fatal(err)
			}
			return []string{w.path("u1")}, ""
		}},
		{name: "missing directory", problem: discover.Incomplete, setup: func(w *world) ([]string, string) {
			w.put("u1", good(w.t), false)
			return []string{w.path("u1"), w.path("missing")}, ""
		}},
		{name: "file given as a directory", problem: discover.Incomplete, setup: func(w *world) ([]string, string) {
			return []string{w.source}, ""
		}},
		{name: "history in a subdirectory is out of scope", problem: discover.NotFound, setup: func(w *world) ([]string, string) {
			if err := os.Mkdir(w.path("u1", "nested"), 0700); err != nil {
				w.t.Fatal(err)
			}
			w.put(filepath.Join("u1", "nested"), good(w.t), false)
			return []string{w.path("u1")}, ""
		}},
		{name: "sidecar in the supplied source directory", setup: func(w *world) ([]string, string) {
			return []string{w.path("src")}, w.put("src", good(w.t), true)
		}},
		{name: "sidecar ignored when its directory is not supplied", problem: discover.NotFound, setup: func(w *world) ([]string, string) {
			w.put("src", good(w.t), true)
			return []string{w.path("u1")}, ""
		}},
		// An encoded name longer than a directory entry allows cannot exist,
		// so it neither blocks the search nor hides the sidecar.
		{name: "sidecar for a source path longer than an entry name", setup: func(w *world) ([]string, string) {
			w.deepen()
			return []string{w.path("u1"), w.target.Dir()}, w.put(w.target.Dir(), good(w.t), true)
		}},
		{name: "source path longer than an entry name", problem: discover.NotFound, setup: func(w *world) ([]string, string) {
			w.deepen()
			return []string{w.path("u1")}, ""
		}},
		{name: "sidecar and an undo directory copy", problem: discover.Ambiguous, setup: func(w *world) ([]string, string) {
			w.put("src", good(w.t), true)
			w.put("u1", good(w.t), false)
			return []string{w.path("src"), w.path("u1")}, ""
		}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			w := newWorld(t)
			dirs, want := tt.setup(w)
			result := w.search(dirs, limits.Default(), false)

			load, err := result.Verified()
			if tt.problem != 0 {
				var problem *discover.SearchError
				if load != nil || !errors.As(err, &problem) || problem.Problem != tt.problem {
					t.Fatalf("verified = %v, %v; want problem %d", load, err, tt.problem)
				}
				return
			}
			if err != nil {
				t.Fatalf("verified: %v", err)
			}
			if load.Path != want || load.Base == nil || load.Base.File() != load.File {
				t.Fatalf("chose %q with base %v, want %q verified", load.Path, load.Base, want)
			}
		})
	}
}

func TestSearchForInspection(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		setup    func(w *world) []string
		noBase   bool
		verified bool
		problem  discover.Problem
	}{
		{name: "lone history for other text", setup: func(w *world) []string {
			w.put("u1", fixture(w.t, "linear", "history.undo"), false)
			return []string{w.path("u1")}
		}},
		{name: "lone history without a base", noBase: true, setup: func(w *world) []string {
			w.put("u1", fixture(w.t, "abandoned-branch", "history.undo"), false)
			return []string{w.path("u1")}
		}},
		{name: "verified history beside another", verified: true, setup: func(w *world) []string {
			w.put("u1", fixture(w.t, "linear", "history.undo"), false)
			w.put("u2", fixture(w.t, "abandoned-branch", "history.undo"), false)
			return []string{w.path("u1"), w.path("u2")}
		}},
		{name: "two unverified histories", problem: discover.Ambiguous, setup: func(w *world) []string {
			w.put("u1", fixture(w.t, "linear", "history.undo"), false)
			w.put("u2", fixture(w.t, "pruned", "history.undo"), false)
			return []string{w.path("u1"), w.path("u2")}
		}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			w := newWorld(t)
			dirs := tt.setup(w)
			req := discover.Request{Target: w.target, Dirs: dirs, Base: w.base, KeepUnverified: true}
			if tt.noBase {
				req.Base, req.BaseErr = nil, errors.New("source unreadable")
			}
			result, err := discover.Search(t.Context(), req, limits.Default())
			if err != nil {
				t.Fatal(err)
			}

			load, err := result.ForInspection()
			if tt.problem != 0 {
				var problem *discover.SearchError
				if !errors.As(err, &problem) || problem.Problem != tt.problem {
					t.Fatalf("inspection = %v, %v; want problem %d", load, err, tt.problem)
				}
				return
			}
			if err != nil || load.History == nil {
				t.Fatalf("inspection = %v, %v", load, err)
			}
			if (load.Base != nil) != tt.verified || (load.Base == nil && load.Err == nil) {
				t.Fatalf("base = %v, reason = %v; want verified %t with a reason otherwise", load.Base, load.Err, tt.verified)
			}

			// Reconstruction still refuses an unverified association.
			if _, err := result.Verified(); tt.verified != (err == nil) {
				t.Fatalf("verified selection error = %v", err)
			}
		})
	}
}

func TestSearchByteLimit(t *testing.T) {
	t.Parallel()

	size := int64(len(fixture(t, "abandoned-branch", "history.undo")))
	junk := int64(len("not an undo file"))

	tests := []struct {
		name     string
		junk     bool // also place a malformed candidate in u1, examined first
		budget   int64
		complete bool
	}{
		{name: "exactly the candidate", budget: size, complete: true},
		{name: "one byte short", budget: size - 1},
		{name: "rejected candidates count", junk: true, budget: size + junk, complete: true},
		{name: "rejected candidates exhaust the limit", junk: true, budget: size + junk - 1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			w := newWorld(t)
			if tt.junk {
				w.put("u1", []byte("not an undo file"), false)
			}
			w.put("u2", fixture(t, "abandoned-branch", "history.undo"), false)

			lim := limits.Default()
			lim.SearchBytes = tt.budget
			_, err := w.search([]string{w.path("u1"), w.path("u2")}, lim, false).Verified()

			var problem *discover.SearchError
			switch {
			case tt.complete && err != nil:
				t.Fatalf("complete search failed: %v", err)
			case !tt.complete && (!errors.As(err, &problem) || problem.Problem != discover.Incomplete):
				t.Fatalf("error = %v, want an incomplete search", err)
			}
		})
	}
}

func TestSearchDirectoryLimit(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		count int
		ok    bool
	}{
		{name: "at the limit", count: 32, ok: true},
		{name: "one over the limit", count: 33},
		{name: "none", count: 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			w := newWorld(t)
			dirs := make([]string, tt.count)
			for i := range dirs {
				dirs[i] = w.path("u1")
			}

			_, err := discover.Search(t.Context(), discover.Request{Target: w.target, Dirs: dirs, Base: w.base}, limits.Default())
			if (err == nil) != tt.ok {
				t.Fatalf("error = %v, want ok %t", err, tt.ok)
			}
		})
	}
}

// Permission bits do not restrict the superuser, so the case only means
// something for an ordinary user.
func TestSearchWithoutPermission(t *testing.T) {
	t.Parallel()
	if os.Geteuid() == 0 {
		t.Skip("permission bits do not restrict root")
	}

	tests := []struct {
		name string
		mode os.FileMode
	}{
		{name: "unsearchable directory", mode: 0600},
		{name: "unreadable directory", mode: 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			w := newWorld(t)
			w.put("u1", fixture(t, "abandoned-branch", "history.undo"), false)
			w.put("u2", fixture(t, "abandoned-branch", "history.undo"), false)
			if err := os.Chmod(w.path("u1"), tt.mode); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = os.Chmod(w.path("u1"), 0700) })

			// The readable copy must not be chosen: the hidden one could be
			// another match.
			_, err := w.search([]string{w.path("u1"), w.path("u2")}, limits.Default(), false).Verified()
			var problem *discover.SearchError
			if !errors.As(err, &problem) || problem.Problem != discover.Incomplete {
				t.Fatalf("error = %v, want an incomplete search", err)
			}
		})
	}
}

// The search reads only candidate names: nothing is created or changed in
// the source or undo locations.
func TestSearchWritesNothing(t *testing.T) {
	t.Parallel()

	tests := []struct{ name string }{{name: "successful search"}}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			w := newWorld(t)
			w.put("u1", fixture(t, "abandoned-branch", "history.undo"), false)
			before := snapshotTree(t, w.root)

			if _, err := w.search([]string{w.path("u1"), w.path("src")}, limits.Default(), false).Verified(); err != nil {
				t.Fatal(err)
			}
			if after := snapshotTree(t, w.root); after != before {
				t.Fatalf("tree changed:\n%s\n---\n%s", before, after)
			}
		})
	}
}

// snapshotTree lists every entry under root with its size and modification
// time.
func snapshotTree(t *testing.T, root string) string {
	t.Helper()

	var out strings.Builder
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		out.WriteString(path + " " + info.Mode().String() + " " + info.ModTime().String() + "\n")
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	return out.String()
}

// BenchmarkSearch measures a search over realistic scopes. The search probes
// only the source's names, so unrelated files in a directory must not change
// its cost.
func BenchmarkSearch(b *testing.B) {
	good := fixture(b, "abandoned-branch", "history.undo")
	junk := make([]byte, 64<<10)

	cases := []struct {
		name  string
		setup func(w *world) []string
	}{
		{name: "one directory of 10000 unrelated files", setup: func(w *world) []string {
			for i := range 10_000 {
				w.write(filepath.Join(w.path("u1"), fmt.Sprintf("%%other%%file%d.go", i)), junk[:64])
			}
			w.put("u1", good, false)
			return []string{w.path("u1")}
		}},
		{name: "32 directories with the match in the last", setup: func(w *world) []string {
			return w.dirs(32, func(i int, dir string) {
				if i == 31 {
					w.put(dir, good, false)
				}
			})
		}},
		{name: "31 rejected candidates before the match", setup: func(w *world) []string {
			return w.dirs(32, func(i int, dir string) {
				if i == 31 {
					w.put(dir, good, false)
				} else {
					w.put(dir, junk, false)
				}
			})
		}},
	}

	for _, c := range cases {
		b.Run(c.name, func(b *testing.B) {
			w := newWorld(b)
			dirs := c.setup(w)
			for b.Loop() {
				result, err := discover.Search(b.Context(), discover.Request{Target: w.target, Dirs: dirs, Base: w.base}, limits.Default())
				if err != nil {
					b.Fatal(err)
				}
				if _, err := result.Verified(); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

func (w *world) write(path string, data []byte) {
	w.t.Helper()

	if err := os.WriteFile(path, data, 0600); err != nil {
		w.t.Fatal(err)
	}
}

// dirs creates n undo directories and lets fill populate each one.
func (w *world) dirs(n int, fill func(i int, dir string)) []string {
	w.t.Helper()

	paths := make([]string, n)
	for i := range n {
		dir := fmt.Sprintf("d%02d", i)
		if err := os.Mkdir(w.path(dir), 0700); err != nil {
			w.t.Fatal(err)
		}
		fill(i, dir)
		paths[i] = w.path(dir)
	}

	return paths
}
