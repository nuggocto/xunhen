package main

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

// nameCase is one source path whose undo filename Neovim computes. Each case
// runs in its own root directory. Paths in Dirs, Files, Links, Cwd, Resolved,
// and UndoIn are relative to that root; Source, UndoDir, and link targets may
// begin with {root}, which stands for its absolute path.
type nameCase struct {
	Name    string
	Purpose string

	Dirs  []string
	Files []string
	Links []nameLink

	Cwd     string
	Source  string
	UndoDir string

	// Resolved is the physical source path Neovim should name, and UndoIn
	// the directory its undo file should be in. An empty UndoIn expects the
	// sidecar beside Resolved.
	Resolved string
	UndoIn   string

	// Missing marks a source that does not exist. Neovim still names its
	// undo file, but the driver cannot write one.
	Missing bool
}

type nameLink struct {
	Path, Target string
}

const rootMarker, mungedRootMarker = "{root}", "{mroot}"

func nameCases() []nameCase {
	base := []string{"src", "undo"}
	plain := []string{"src/plain.go"}
	links := []string{"src", "undo", "links"}

	return []nameCase{
		{
			Name: "absolute path", Purpose: "An absolute source path in an ordinary undo directory.",
			Dirs: base, Files: plain,
			Source: "{root}/src/plain.go", UndoDir: "{root}/undo",
			Resolved: "src/plain.go", UndoIn: "undo",
		},
		{
			Name: "relative path", Purpose: "A relative source path joins the working directory.",
			Dirs: base, Files: plain,
			Cwd: "src", Source: "plain.go", UndoDir: "{root}/undo",
			Resolved: "src/plain.go", UndoIn: "undo",
		},
		{
			Name: "parent component", Purpose: "A .. component resolves before the name is encoded.",
			Dirs: []string{"src", "src/sub", "undo"}, Files: plain,
			Cwd: "src/sub", Source: "../plain.go", UndoDir: "{root}/undo",
			Resolved: "src/plain.go", UndoIn: "undo",
		},
		{
			Name: "spaces", Purpose: "Spaces pass through the encoding unchanged.",
			Dirs: []string{"src/with space", "undo"}, Files: []string{"src/with space/file name.go"},
			Source: "{root}/src/with space/file name.go", UndoDir: "{root}/undo",
			Resolved: "src/with space/file name.go", UndoIn: "undo",
		},
		{
			Name: "unicode", Purpose: "Multibyte UTF-8 passes through the encoding unchanged.",
			Dirs: []string{"src/café", "undo"}, Files: []string{"src/café/尋痕.go"},
			Source: "{root}/src/café/尋痕.go", UndoDir: "{root}/undo",
			Resolved: "src/café/尋痕.go", UndoIn: "undo",
		},
		{
			Name: "invalid UTF-8", Purpose: "A byte that is not UTF-8 passes through the encoding unchanged.",
			Dirs: base, Files: []string{"src/bad\xffname.go"},
			Source: "{root}/src/bad\xffname.go", UndoDir: "{root}/undo",
			Resolved: "src/bad\xffname.go", UndoIn: "undo",
		},
		{
			Name: "percent in a directory", Purpose: "One side of a collision: /a%b/c.go encodes as %a%b%c.go.",
			Dirs: []string{"a%b", "undo"}, Files: []string{"a%b/c.go"},
			Source: "{root}/a%b/c.go", UndoDir: "{root}/undo",
			Resolved: "a%b/c.go", UndoIn: "undo",
		},
		{
			Name: "percent in a file name", Purpose: "The other side of the collision: /a/b%c.go also encodes as %a%b%c.go.",
			Dirs: []string{"a", "undo"}, Files: []string{"a/b%c.go"},
			Source: "{root}/a/b%c.go", UndoDir: "{root}/undo",
			Resolved: "a/b%c.go", UndoIn: "undo",
		},
		{
			Name: "dotfile", Purpose: "A leading dot in the file name stays part of the encoded name.",
			Dirs: base, Files: []string{"src/.env"},
			Source: "{root}/src/.env", UndoDir: "{root}/undo",
			Resolved: "src/.env", UndoIn: "undo",
		},
		{
			Name: "absolute file symlink", Purpose: "A symlinked source is named after its target.",
			Dirs: links, Files: plain, Links: []nameLink{{"links/abs.go", "{root}/src/plain.go"}},
			Source: "{root}/links/abs.go", UndoDir: "{root}/undo",
			Resolved: "src/plain.go", UndoIn: "undo",
		},
		{
			Name: "relative symlink chain", Purpose: "Each relative link resolves against the link's own directory.",
			Dirs: links, Files: plain,
			Links:  []nameLink{{"links/rel.go", "hop.go"}, {"links/hop.go", "../src/plain.go"}},
			Source: "{root}/links/rel.go", UndoDir: "{root}/undo",
			Resolved: "src/plain.go", UndoIn: "undo",
		},
		{
			Name: "directory symlink", Purpose: "A symlinked directory in the source path resolves to the real directory.",
			Dirs: base, Files: plain, Links: []nameLink{{"dirlink", "src"}},
			Source: "{root}/dirlink/plain.go", UndoDir: "{root}/undo",
			Resolved: "src/plain.go", UndoIn: "undo",
		},
		{
			Name: "working directory through a symlink", Purpose: "A relative path joins the physical working directory.",
			Dirs: base, Files: plain, Links: []nameLink{{"dirlink", "src"}},
			Cwd: "dirlink", Source: "plain.go", UndoDir: "{root}/undo",
			Resolved: "src/plain.go", UndoIn: "undo",
		},
		{
			Name: "trailing separators", Purpose: "Trailing slashes on the undo directory do not change the name.",
			Dirs: base, Files: plain,
			Source: "{root}/src/plain.go", UndoDir: "{root}/undo//",
			Resolved: "src/plain.go", UndoIn: "undo",
		},
		{
			Name: "sidecar", Purpose: "An 'undodir' entry of . puts .name.un~ beside the source.",
			Dirs: base, Files: plain,
			Source: "{root}/src/plain.go", UndoDir: ".",
			Resolved: "src/plain.go",
		},
		{
			Name: "sidecar beside a symlink target", Purpose: "The sidecar goes beside the target, not the link.",
			Dirs: links, Files: plain, Links: []nameLink{{"links/abs.go", "{root}/src/plain.go"}},
			Source: "{root}/links/abs.go", UndoDir: ".",
			Resolved: "src/plain.go",
		},
		{
			Name: "missing source", Purpose: "A deleted source still names its undo file from its directory.",
			Dirs: base, Source: "{root}/src/gone.go", UndoDir: "{root}/undo",
			Resolved: "src/gone.go", UndoIn: "undo", Missing: true,
		},
		{
			Name: "missing directory", Purpose: "Without a directory to resolve, an absolute path is used as written.",
			Dirs: []string{"undo"}, Source: "{root}/nodir/gone.go", UndoDir: "{root}/undo",
			Resolved: "nodir/gone.go", UndoIn: "undo", Missing: true,
		},
		{
			Name: "dangling symlink", Purpose: "A link to a missing file is named after its target.",
			Dirs: links, Links: []nameLink{{"links/dangle.go", "../src/gone.go"}},
			Source: "{root}/links/dangle.go", UndoDir: "{root}/undo",
			Resolved: "src/gone.go", UndoIn: "undo", Missing: true,
		},
	}
}

// expectedName is the authored expectation for a case rooted at root.
func (c nameCase) expectedName(root string) string {
	source := root + "/" + c.Resolved
	if c.UndoIn == "" {
		dir, base := filepath.Split(source)
		return dir + "." + base + ".un~"
	}

	return root + "/" + c.UndoIn + "/" + strings.ReplaceAll(source, "/", "%")
}

type nameCorpus struct {
	Schema   int          `json:"schema"`
	Producer producer     `json:"producer"`
	Cases    []nameRecord `json:"cases"`
}

// nameRecord stores every path as hex, because file names can hold bytes that
// are not UTF-8. Recorded names replace the case root with {root} and its
// encoded form with {mroot}.
type nameRecord struct {
	Name       string          `json:"name"`
	Purpose    string          `json:"purpose"`
	DirsHex    []string        `json:"dirs_hex"`
	FilesHex   []string        `json:"files_hex"`
	Links      []nameLinkHex   `json:"links"`
	CwdHex     string          `json:"cwd_hex"`
	SourceHex  string          `json:"source_hex"`
	UndoDirHex string          `json:"undodir_hex"`
	Neovim     nameObservation `json:"neovim"`
}

type nameLinkHex struct {
	PathHex   string `json:"path_hex"`
	TargetHex string `json:"target_hex"`
}

type nameObservation struct {
	UndofileHex string   `json:"undofile_hex"`
	WrittenHex  []string `json:"written_hex"`
}

type namesRequest struct {
	Mode   string        `json:"mode"`
	Output string        `json:"output"`
	Cases  []nameRequest `json:"cases"`
}

type nameRequest struct {
	CwdHex     string `json:"cwd_hex"`
	SourceHex  string `json:"source_hex"`
	UndoDirHex string `json:"undodir_hex"`
	Write      bool   `json:"write"`
}

type namesResponse struct {
	Results []struct {
		UndofileHex string `json:"undofile_hex"`
	} `json:"results"`
}

func hexAll(paths []string) []string {
	out := make([]string, len(paths))
	for i, path := range paths {
		out[i] = hex.EncodeToString([]byte(path))
	}

	return out
}

// generateNames records Neovim's undofile() answer for every naming case and
// checks it against both the authored expectation and the file Neovim wrote.
func generateNames(ctx context.Context, nvim, out string) (err error) {
	nvim, err = resolveProducer(nvim)
	if err != nil {
		return err
	}

	work, err := os.MkdirTemp("", "xunhen-names-")
	if err != nil {
		return err
	}
	defer func() {
		err = errors.Join(err, os.RemoveAll(work))
	}()

	if err := prepareWork(work); err != nil {
		return err
	}
	version, err := readProducerVersion(ctx, nvim, work)
	if err != nil {
		return err
	}

	// Neovim names files by their physical path, so the roots must be
	// physical too.
	top, err := filepath.EvalSymlinks(filepath.Join(work, "tmp"))
	if err != nil {
		return err
	}

	cases := nameCases()
	roots := make([]string, len(cases))
	request := namesRequest{Mode: "names", Output: filepath.Join(work, "names.json")}
	for i, c := range cases {
		roots[i] = filepath.Join(top, fmt.Sprintf("case-%02d", i))
		if err := buildLayout(roots[i], c); err != nil {
			return fmt.Errorf("%s: %w", c.Name, err)
		}

		fill := func(s string) string { return strings.ReplaceAll(s, rootMarker, roots[i]) }
		request.Cases = append(request.Cases, nameRequest{
			CwdHex:     hex.EncodeToString([]byte(filepath.Join(roots[i], c.Cwd))),
			SourceHex:  hex.EncodeToString([]byte(fill(c.Source))),
			UndoDirHex: hex.EncodeToString([]byte(fill(c.UndoDir))),
			Write:      !c.Missing,
		})
	}

	if err := runDriver(ctx, nvim, work, request); err != nil {
		return err
	}
	var response namesResponse
	if err := readJSON(request.Output, &response); err != nil {
		return err
	}
	if len(response.Results) != len(cases) {
		return errors.New("Neovim answered a different number of naming cases")
	}

	corpus := nameCorpus{Schema: 1, Producer: pinnedProducer(version)}
	for i, c := range cases {
		named, err := hex.DecodeString(response.Results[i].UndofileHex)
		if err != nil {
			return err
		}
		written, err := newFiles(roots[i], c)
		if err != nil {
			return err
		}

		record, err := recordName(c, roots[i], string(named), written)
		if err != nil {
			return fmt.Errorf("%s: %w", c.Name, err)
		}
		corpus.Cases = append(corpus.Cases, record)
	}

	if err := os.Mkdir(out, 0755); err != nil {
		return fmt.Errorf("create new output directory: %w", err)
	}
	return writeJSON(filepath.Join(out, "names.json"), corpus)
}

func buildLayout(root string, c nameCase) error {
	for _, dir := range append([]string{""}, c.Dirs...) {
		if err := os.MkdirAll(filepath.Join(root, dir), 0700); err != nil {
			return err
		}
	}
	for _, file := range c.Files {
		if err := os.WriteFile(filepath.Join(root, file), []byte("x\n"), 0600); err != nil {
			return err
		}
	}
	for _, link := range c.Links {
		target := strings.ReplaceAll(link.Target, rootMarker, root)
		if err := os.Symlink(target, filepath.Join(root, link.Path)); err != nil {
			return err
		}
	}

	return nil
}

// newFiles lists the regular files under root that the layout did not create.
func newFiles(root string, c nameCase) ([]string, error) {
	var found []string
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		relative, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		if entry.Type().IsRegular() && !slices.Contains(c.Files, relative) {
			found = append(found, path)
		}
		return nil
	})

	return found, err
}

// recordName checks Neovim's answer and the written file against the authored
// expectation, then stores them relative to the case root.
func recordName(c nameCase, root, named string, written []string) (nameRecord, error) {
	want := c.expectedName(root)
	if named != want {
		return nameRecord{}, fmt.Errorf("undofile() = %q, want %q", named, want)
	}
	wantWritten := []string{want}
	if c.Missing {
		wantWritten = nil
	}
	if !slices.Equal(written, wantWritten) {
		return nameRecord{}, fmt.Errorf("Neovim wrote %q, want %q", written, wantWritten)
	}

	template := func(path string) string {
		path = strings.ReplaceAll(path, strings.ReplaceAll(root, "/", "%"), mungedRootMarker)
		return strings.ReplaceAll(path, root, rootMarker)
	}

	record := nameRecord{
		Name:       c.Name,
		Purpose:    c.Purpose,
		DirsHex:    hexAll(c.Dirs),
		FilesHex:   hexAll(c.Files),
		CwdHex:     hex.EncodeToString([]byte(c.Cwd)),
		SourceHex:  hex.EncodeToString([]byte(c.Source)),
		UndoDirHex: hex.EncodeToString([]byte(c.UndoDir)),
		Neovim: nameObservation{
			UndofileHex: hex.EncodeToString([]byte(template(named))),
			WrittenHex:  []string{},
		},
	}
	for _, path := range written {
		record.Neovim.WrittenHex = append(record.Neovim.WrittenHex, hex.EncodeToString([]byte(template(path))))
	}
	for _, link := range c.Links {
		record.Links = append(record.Links, nameLinkHex{
			PathHex:   hex.EncodeToString([]byte(link.Path)),
			TargetHex: hex.EncodeToString([]byte(link.Target)),
		})
	}

	return record, nil
}

// checkNameRecord confirms that a stored record still describes its authored
// case and that Neovim's recorded answer is the authored expectation.
func checkNameRecord(c nameCase, record nameRecord) error {
	if record.Name != c.Name || record.Purpose != c.Purpose {
		return errors.New("record does not describe the authored case")
	}

	decode := func(encoded string) string {
		raw, err := hex.DecodeString(encoded)
		if err != nil {
			return "\x00invalid hex"
		}
		return string(raw)
	}
	decodeAll := func(encoded []string) []string {
		out := make([]string, len(encoded))
		for i, e := range encoded {
			out[i] = decode(e)
		}
		return out
	}

	if !slices.Equal(decodeAll(record.DirsHex), c.Dirs) || !slices.Equal(decodeAll(record.FilesHex), c.Files) {
		return errors.New("recorded layout differs from the authored case")
	}
	if len(record.Links) != len(c.Links) {
		return errors.New("recorded links differ from the authored case")
	}
	for i, link := range record.Links {
		if decode(link.PathHex) != c.Links[i].Path || decode(link.TargetHex) != c.Links[i].Target {
			return errors.New("recorded links differ from the authored case")
		}
	}
	if decode(record.CwdHex) != c.Cwd || decode(record.SourceHex) != c.Source || decode(record.UndoDirHex) != c.UndoDir {
		return errors.New("recorded invocation differs from the authored case")
	}

	// Expand the authored expectation with a sample root and template it the
	// way the generator does.
	const sample = "/sample/root"
	want := strings.ReplaceAll(c.expectedName(sample), strings.ReplaceAll(sample, "/", "%"), mungedRootMarker)
	want = strings.ReplaceAll(want, sample, rootMarker)
	if got := decode(record.Neovim.UndofileHex); got != want {
		return fmt.Errorf("recorded undofile() %q, want %q", got, want)
	}
	wantWritten := []string{want}
	if c.Missing {
		wantWritten = []string{}
	}
	if !slices.Equal(decodeAll(record.Neovim.WrittenHex), wantWritten) {
		return errors.New("recorded written files differ from undofile()")
	}

	return nil
}
