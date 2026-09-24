package discover

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/nuggocto/xunhen/internal/input"
	"github.com/nuggocto/xunhen/internal/limits"
)

// A change to a directory or candidate name between examining it and
// finishing the search could hide or add a history, so the search must
// fail rather than report its findings. The test runs the search's steps
// directly to change the tree between them.
func TestRecheckDetectsChanges(t *testing.T) {
	t.Parallel()

	target := Target{Path: "/src/retry.go"}
	name := target.UndoName()

	retarget := func(t *testing.T, root string) {
		if err := os.Remove(filepath.Join(root, "link")); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink("elsewhere", filepath.Join(root, "link")); err != nil {
			t.Fatal(err)
		}
	}

	// Every case has undo/, elsewhere/, and link pointing at undo/. dirs are
	// the directories supplied to the search, relative to the root.
	tests := []struct {
		name    string
		present bool // a candidate exists before the search
		dirs    []string
		change  func(t *testing.T, root string)
		changed bool
	}{
		{name: "nothing changes", dirs: []string{"undo", "link"}, change: func(*testing.T, string) {}},
		{name: "candidate appears", dirs: []string{"undo"}, changed: true, change: func(t *testing.T, root string) {
			writeFile(t, filepath.Join(root, "undo", name), "late")
		}},
		{name: "candidate disappears", present: true, dirs: []string{"undo"}, changed: true, change: func(t *testing.T, root string) {
			if err := os.Remove(filepath.Join(root, "undo", name)); err != nil {
				t.Fatal(err)
			}
		}},
		{name: "candidate is replaced", present: true, dirs: []string{"undo"}, changed: true, change: func(t *testing.T, root string) {
			writeFile(t, filepath.Join(root, "new"), "other")
			if err := os.Rename(filepath.Join(root, "new"), filepath.Join(root, "undo", name)); err != nil {
				t.Fatal(err)
			}
		}},
		{name: "directory symlink is retargeted", dirs: []string{"link"}, changed: true, change: retarget},
		// The alias is deduplicated against undo/ and never read, but its
		// path must still name undo/ when the search ends: retargeted, it
		// could hold another matching history.
		{name: "repeated alias is retargeted", dirs: []string{"undo", "link"}, changed: true, change: retarget},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			root := t.TempDir()
			for _, dir := range []string{"undo", "elsewhere"} {
				if err := os.Mkdir(filepath.Join(root, dir), 0700); err != nil {
					t.Fatal(err)
				}
			}
			if err := os.Symlink("undo", filepath.Join(root, "link")); err != nil {
				t.Fatal(err)
			}
			if tt.present {
				writeFile(t, filepath.Join(root, "undo", name), "not an undo file")
			}

			lim := limits.Default()
			s := &search{ctx: t.Context(), req: Request{Target: target}, lim: lim, result: &Result{}, remaining: lim.SearchBytes}
			t.Cleanup(func() {
				for _, d := range s.held {
					_ = d.Close()
				}
			})
			for _, dir := range tt.dirs {
				if err := s.searchDir(filepath.Join(root, dir), input.Identity{}); err != nil {
					t.Fatal(err)
				}
			}

			tt.change(t, root)
			err := s.recheck()
			var changed *input.ChangedError
			if tt.changed != (err != nil) || (tt.changed && !errors.As(err, &changed)) {
				t.Fatalf("recheck = %v, want changed %t", err, tt.changed)
			}
		})
	}
}

// A candidate that grows after its lookup was charged at its old size. The
// search must refuse it without reading it, or the search byte limit would
// not bound what is actually read.
func TestLoadRefusesGrowthAfterLookup(t *testing.T) {
	t.Parallel()

	history, err := os.ReadFile("../../testdata/undo/abandoned-branch/history.undo")
	if err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name    string
		initial string
	}{
		{name: "empty candidate grows into a history", initial: ""},
		{name: "short candidate grows into a history", initial: "x"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			root := t.TempDir()
			target := Target{Path: "/src/retry.go"}
			path := filepath.Join(root, target.UndoName())
			writeFile(t, path, tt.initial)

			d, err := input.OpenDir(root)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = d.Close() })
			entry, err := d.Lookup(target.UndoName())
			if err != nil {
				t.Fatal(err)
			}

			if err := os.WriteFile(path, history, 0600); err != nil {
				t.Fatal(err)
			}

			lim := limits.Default()
			lim.SearchBytes = 1
			s := &search{ctx: t.Context(), req: Request{Target: target}, lim: lim, result: &Result{}, remaining: lim.SearchBytes}
			err = s.load(d, target.UndoName(), path, entry)

			var changed *input.ChangedError
			if !errors.As(err, &changed) || len(s.result.Reports) != 0 {
				t.Fatalf("load = %v with reports %+v; want a change refused before reading", err, s.result.Reports)
			}
		})
	}
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()

	if err := os.WriteFile(path, []byte(content), 0600); err != nil {
		t.Fatal(err)
	}
}
