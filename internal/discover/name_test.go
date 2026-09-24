package discover_test

import (
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/nuggocto/xunhen/internal/discover"
)

// namingCase is one record of testdata/discovery/names.json, which the
// fixture generator filled with Neovim's own undofile() answers.
type namingCase struct {
	Name     string   `json:"name"`
	DirsHex  []string `json:"dirs_hex"`
	FilesHex []string `json:"files_hex"`
	Links    []struct {
		PathHex   string `json:"path_hex"`
		TargetHex string `json:"target_hex"`
	} `json:"links"`
	CwdHex     string `json:"cwd_hex"`
	SourceHex  string `json:"source_hex"`
	UndoDirHex string `json:"undodir_hex"`
	Neovim     struct {
		UndofileHex string `json:"undofile_hex"`
	} `json:"neovim"`
}

func unhex(t testing.TB, s string) string {
	t.Helper()

	raw, err := hex.DecodeString(s)
	if err != nil {
		t.Fatal(err)
	}

	return string(raw)
}

func namingCases(t *testing.T) []namingCase {
	t.Helper()

	data, err := os.ReadFile("../../testdata/discovery/names.json")
	if err != nil {
		t.Fatal(err)
	}
	var corpus struct {
		Cases []namingCase `json:"cases"`
	}
	if err := json.Unmarshal(data, &corpus); err != nil {
		t.Fatal(err)
	}
	if len(corpus.Cases) == 0 {
		t.Fatal("naming corpus is empty")
	}

	return corpus.Cases
}

// physicalRoot returns a fresh directory by its physical path, the form
// Neovim names files by.
func physicalRoot(t testing.TB) string {
	t.Helper()

	root, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}

	return root
}

// build recreates the case's layout under root and returns a function that
// expands {root} in the case's paths.
func (c namingCase) build(t *testing.T, root string) func(string) string {
	t.Helper()

	fill := func(s string) string { return strings.ReplaceAll(unhex(t, s), "{root}", root) }
	for _, dir := range c.DirsHex {
		if err := os.MkdirAll(filepath.Join(root, unhex(t, dir)), 0700); err != nil {
			t.Fatal(err)
		}
	}
	for _, file := range c.FilesHex {
		if err := os.WriteFile(filepath.Join(root, unhex(t, file)), []byte("x\n"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	for _, link := range c.Links {
		if err := os.Symlink(fill(link.TargetHex), filepath.Join(root, unhex(t, link.PathHex))); err != nil {
			t.Fatal(err)
		}
	}

	return fill
}

func TestNamesMatchNeovim(t *testing.T) {
	t.Parallel()

	for _, c := range namingCases(t) {
		t.Run(c.Name, func(t *testing.T) {
			t.Parallel()

			root := physicalRoot(t)
			fill := c.build(t, root)

			// Neovim joined relative names to its physical working
			// directory, which a symlinked case directory resolves to.
			cwd, err := filepath.EvalSymlinks(filepath.Join(root, unhex(t, c.CwdHex)))
			if err != nil {
				t.Fatal(err)
			}

			want := strings.ReplaceAll(unhex(t, c.Neovim.UndofileHex), "{mroot}", strings.ReplaceAll(root, "/", "%"))
			want = strings.ReplaceAll(want, "{root}", root)

			target := discover.Resolve(fill(c.SourceHex), cwd)
			got := target.Dir() + "/" + target.SidecarName()
			if undodir := fill(c.UndoDirHex); undodir != "." {
				got = strings.TrimRight(undodir, "/") + "/" + target.UndoName()
			}
			if got != want {
				t.Fatalf("name = %q, want Neovim's %q", got, want)
			}
		})
	}
}

// The encoded names of these two sources are equal, which is why a name
// alone never proves which source a history belongs to.
func TestPercentCollision(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		left, right string
	}{
		{name: "percent in a directory or a file name", left: "/a%b/c.go", right: "/a/b%c.go"},
		{name: "percent against a slash at the start", left: "/%x", right: "//x"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			left, right := discover.Target{Path: tt.left}, discover.Target{Path: tt.right}
			if left.UndoName() != right.UndoName() {
				t.Fatalf("%q and %q encode differently: %q, %q", tt.left, tt.right, left.UndoName(), right.UndoName())
			}
		})
	}
}

// FuzzNames checks the naming invariants for arbitrary path bytes: an undo
// name is one path component of the same length, and a sidecar sits in the
// source's directory.
func FuzzNames(f *testing.F) {
	f.Add("/a/b/c.go")
	f.Add("/")
	f.Add("/a%b/c.go")
	f.Add("/src/bad\xffname.go")
	f.Add("relative")

	f.Fuzz(func(t *testing.T, path string) {
		if len(path) > 4096 {
			t.Skip()
		}

		target := discover.Target{Path: path}
		name := target.UndoName()
		if strings.Contains(name, "/") || len(name) != len(path) {
			t.Fatalf("undo name %q of %q is not one component of the same length", name, path)
		}
		if utf8.ValidString(path) != utf8.ValidString(name) {
			t.Fatalf("encoding %q changed its UTF-8 validity", path)
		}

		sidecar := target.SidecarName()
		if strings.Contains(sidecar, "/") || !strings.HasPrefix(sidecar, ".") || !strings.HasSuffix(sidecar, ".un~") {
			t.Fatalf("sidecar %q of %q is malformed", sidecar, path)
		}
		if dir := target.Dir(); strings.Contains(path, "/") && !strings.HasPrefix(path, dir) {
			t.Fatalf("directory %q is not a prefix of %q", dir, path)
		}
	})
}
