package main

import (
	"bufio"
	"bytes"
	"context"
	"debug/buildinfo"
	"debug/elf"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"syscall"
	"unicode/utf8"

	"github.com/nuggocto/xunhen/internal/limits"
)

const mainPackage = "github.com/nuggocto/xunhen/cmd/xunhen"

// checkMetadata reads the build settings the Go toolchain embeds and
// compares them with the one supported configuration, and every linked
// module with go.sum, so a build from other settings, another toolchain,
// modified sources, or other module versions fails.
func checkMetadata(binary string, c config) (string, error) {
	info, err := buildinfo.ReadFile(binary)
	if err != nil {
		return "", fmt.Errorf("read the embedded build information: %w", err)
	}
	if info.Path != mainPackage {
		return "", fmt.Errorf("the executable was built from %q, not %q", info.Path, mainPackage)
	}
	if info.GoVersion != c.goVersion {
		return "", fmt.Errorf("built with %s, want %s", info.GoVersion, c.goVersion)
	}

	want := map[string]string{
		"GOOS": "linux", "GOARCH": "amd64", "GOAMD64": "v1", "CGO_ENABLED": "0",
		"-trimpath": "true", "-buildmode": c.buildmode,
	}
	settings := map[string]string{}
	for _, s := range info.Settings {
		settings[s.Key] = s.Value
	}
	for key, value := range want {
		if settings[key] != value {
			return "", fmt.Errorf("build setting %s=%q, want %q", key, settings[key], value)
		}
	}
	if settings["vcs.modified"] == "true" {
		return "", errors.New("the executable was built from a modified working tree")
	}

	sums, versions, err := goSum(c.gosum)
	if err != nil {
		return "", err
	}
	vendored := 0
	for _, dep := range info.Deps {
		switch {
		case dep.Replace != nil:
			return "", fmt.Errorf("module %s was replaced by %s", dep.Path, dep.Replace.Path)
		case dep.Sum == "":
			// A vendored build, as Nix makes, records no module hash; the
			// package's vendorHash pins the content instead. The version
			// must still be the one go.sum names.
			if !versions[dep.Path+" "+dep.Version] {
				return "", fmt.Errorf("module %s %s is not in %s", dep.Path, dep.Version, c.gosum)
			}
			vendored++
		case !sums[dep.Path+" "+dep.Version+" "+dep.Sum]:
			return "", fmt.Errorf("module %s %s %s is not in %s", dep.Path, dep.Version, dep.Sum, c.gosum)
		}
	}
	detail := fmt.Sprintf(" (%s, %d modules", info.GoVersion, len(info.Deps))
	if vendored != 0 {
		detail += fmt.Sprintf(", %d vendored without recorded hashes", vendored)
	}
	return detail + ")", nil
}

// goSum reads go.sum into its lines and its module versions.
func goSum(path string) (sums, versions map[string]bool, err error) {
	data, err := readAll(path)
	if err != nil {
		return nil, nil, fmt.Errorf("read go.sum: %w", err)
	}
	sums, versions = map[string]bool{}, map[string]bool{}
	scanner := bufio.NewScanner(bytes.NewReader(data))
	for scanner.Scan() {
		sums[scanner.Text()] = true
		if fields := strings.Fields(scanner.Text()); len(fields) == 3 && !strings.HasSuffix(fields[1], "/go.mod") {
			versions[fields[0]+" "+fields[1]] = true
		}
	}
	return sums, versions, scanner.Err()
}

// checkLinkage confirms the executable needs nothing from the system: no
// dynamic loader and no shared library.
func checkLinkage(binary, buildmode string) (string, error) {
	f, err := elf.Open(binary)
	if err != nil {
		return "", fmt.Errorf("read the executable as ELF: %w", err)
	}
	defer func() { _ = f.Close() }()

	if f.Class != elf.ELFCLASS64 || f.Machine != elf.EM_X86_64 || f.OSABI != elf.ELFOSABI_NONE && f.OSABI != elf.ELFOSABI_LINUX {
		return "", fmt.Errorf("the executable is %v %v %v, not 64-bit x86-64 Linux", f.Class, f.Machine, f.OSABI)
	}
	wantType := elf.ET_EXEC
	if buildmode == "pie" {
		wantType = elf.ET_DYN
	}
	if f.Type != wantType {
		return "", fmt.Errorf("ELF type %v, want %v for -buildmode=%s", f.Type, wantType, buildmode)
	}
	for _, p := range f.Progs {
		if p.Type == elf.PT_INTERP {
			return "", errors.New("the executable names a dynamic loader")
		}
	}
	libs, err := f.ImportedLibraries()
	if err != nil {
		return "", err
	}
	if len(libs) != 0 {
		return "", fmt.Errorf("the executable needs shared libraries %s", strings.Join(libs, ", "))
	}
	return fmt.Sprintf(" (%v, no loader or shared libraries)", f.Type), nil
}

func (w *world) checkVersion(ctx context.Context, c config) (string, error) {
	o, err := w.run(ctx, runOptions{}, "--version")
	if err != nil {
		return "", err
	}
	want := fmt.Sprintf("xunhen %s\ncommit: %s\ngo: %s\n", c.version, c.commit, c.goVersion)
	if o.status != 0 || o.stdout != want || o.stderr != "" {
		return "", fmt.Errorf("status %d, stdout %q, stderr %q; want %q", o.status, o.stdout, o.stderr, want)
	}
	return "", nil
}

// oracle is the part of a fixture's oracle.json the verifier reads: the
// states Neovim itself reported after loading the undo file.
type oracle struct {
	Loaded struct {
		States []struct {
			Seq      int      `json:"seq"`
			LinesHex []string `json:"lines_hex"`
		} `json:"states"`
	} `json:"loaded"`
}

type state struct {
	seq   int
	lines []string
}

// states returns a fixture's distinct recorded states in oracle order.
func (w *world) states(fixture string) ([]state, error) {
	data, err := readAll(filepath.Join(w.corpus, fixture, "oracle.json"))
	if err != nil {
		return nil, err
	}
	var o oracle
	if err := json.Unmarshal(data, &o); err != nil {
		return nil, fmt.Errorf("%s oracle: %w", fixture, err)
	}
	var out []state
	seen := map[int]bool{}
	for _, s := range o.Loaded.States {
		if seen[s.Seq] {
			continue
		}
		seen[s.Seq] = true
		st := state{seq: s.Seq}
		for _, encoded := range s.LinesHex {
			line, err := hex.DecodeString(encoded)
			if err != nil {
				return nil, fmt.Errorf("%s oracle: %w", fixture, err)
			}
			st.lines = append(st.lines, string(line))
		}
		out = append(out, st)
	}
	return out, nil
}

// supportedText follows the documented base text profile: UTF-8 without a
// byte-order mark, NUL, or CRLF. It reads the base's bytes; it does not
// ask xunhen.
func supportedText(data []byte) bool {
	return utf8.Valid(data) && !bytes.HasPrefix(data, []byte("\xef\xbb\xbf")) &&
		!bytes.ContainsRune(data, 0) && !bytes.Contains(data, []byte("\r\n"))
}

// exportable follows the raw export profile: every line valid UTF-8
// without NUL.
func exportable(lines []string) bool {
	for _, line := range lines {
		if !utf8.ValidString(line) || strings.Contains(line, "\x00") {
			return false
		}
	}
	return true
}

// checkCorpus exports every recorded state of every fixture under both
// final-newline policies and compares the bytes with Neovim's record. A
// state raw export cannot represent must be refused with no output, and a
// base outside the supported text profile must be refused outright.
func (w *world) checkCorpus(ctx context.Context) (string, error) {
	fixtures, err := os.ReadDir(w.corpus)
	if err != nil {
		return "", err
	}
	exported, refused := 0, 0
	for _, f := range fixtures {
		name := f.Name()
		undo := filepath.Join(w.corpus, name, "history.undo")
		base := filepath.Join(w.corpus, name, "base.bin")
		baseData, err := readAll(base)
		if err != nil {
			return "", err
		}
		states, err := w.states(name)
		if err != nil {
			return "", err
		}
		if len(states) == 0 {
			return "", fmt.Errorf("%s records no states", name)
		}

		if !supportedText(baseData) {
			o, err := w.run(ctx, runOptions{}, "show", "--undo", undo, "--base", base, "--node", strconv.Itoa(states[0].seq))
			if err != nil {
				return "", err
			}
			if o.status != 1 || o.stdout != "" || !strings.Contains(o.stderr, "unsupported input") {
				return "", fmt.Errorf("%s: an unsupported base gave status %d, stdout %q, stderr %q", name, o.status, o.stdout, o.stderr)
			}
			refused++
			continue
		}

		for _, s := range states {
			for _, policy := range []string{"include", "omit"} {
				o, err := w.run(ctx, runOptions{}, "show", "--undo", undo, "--base", base,
					"--node", strconv.Itoa(s.seq), "--raw", "--final-newline="+policy)
				if err != nil {
					return "", err
				}
				if !exportable(s.lines) {
					if o.status != 1 || o.stdout != "" || !strings.Contains(o.stderr, "raw export unsupported") {
						return "", fmt.Errorf("%s node %d: status %d, stdout %q, stderr %q; want a refusal", name, s.seq, o.status, o.stdout, o.stderr)
					}
					refused++
					continue
				}
				want := strings.Join(s.lines, "\n")
				if policy == "include" {
					want += "\n"
				}
				if o.status != 0 || o.stdout != want || o.stderr != "" {
					return "", fmt.Errorf("%s node %d, --final-newline=%s: status %d, stderr %q, stdout %q; Neovim recorded %q",
						name, s.seq, policy, o.status, o.stderr, o.stdout, want)
				}
				exported++
			}
		}
	}
	return fmt.Sprintf(" (%d exports match Neovim, %d refusals)", exported, refused), nil
}

// walkthroughDiff compares the abandoned experiment against the saved fix,
// checked by hand against the two states Neovim recorded.
const walkthroughDiff = "--- node 2\n" +
	"+++ node 3\n" +
	"@@ -1,3 +1,3 @@\n" +
	" package sample\n" +
	" \n" +
	"-func experiment() int { return 42 }\n" +
	"+func chosen() int { return 1 }\n"

// checkWalkthrough repeats the documented session from a source file: find
// the history by the source's path, inspect it, preview the abandoned
// state, compare it with the saved one, and export it.
func (w *world) checkWalkthrough(ctx context.Context) (string, error) {
	project := filepath.Join(w.root, "project")
	states, err := w.states("abandoned-branch")
	if err != nil {
		return "", err
	}
	experiment := slices.IndexFunc(states, func(s state) bool { return s.seq == 2 })
	if experiment < 0 {
		return "", errors.New("the abandoned-branch oracle has no node 2")
	}
	text := strings.Join(states[experiment].lines, "\n")

	from := []string{"--source", "retry.go", "--undo-dir", "../undo"}
	steps := []struct {
		name     string
		args     []string
		env      []string
		want     string
		contains []string
		private  bool // the output must hold no recovered text
	}{
		{name: "inspect", args: append([]string{"inspect"}, from...), private: true, contains: []string{
			"(verified: its text matches the reference)", "Reference node: 3",
			"node 3: parent=1 preferred-child=0 next-sibling=2", "node 2: parent=1 preferred-child=0 next-sibling=0 previous-sibling=3",
		}},
		{name: "preview", args: append(append([]string{"show"}, from...), "--node", "2"), want: text + "\n"},
		{name: "compare", args: append(append([]string{"diff"}, from...), "--from", "2", "--to", "3"), want: walkthroughDiff},
		{name: "compare a state with itself", args: append(append([]string{"diff"}, from...), "--from", "3", "--to", "3"), want: ""},
		{name: "export", args: append(append([]string{"show"}, from...), "--node", "2", "--raw", "--final-newline=include"), want: text + "\n"},
		// The everyday form: the source as a plain argument, and the undo
		// directory from the environment instead of --undo-dir.
		{name: "preview through XUNHEN_UNDO_DIR", args: []string{"show", "retry.go", "--node", "2"}, env: []string{"XUNHEN_UNDO_DIR=../undo"}, want: text + "\n"},
	}
	for _, step := range steps {
		o, err := w.run(ctx, runOptions{dir: project, env: step.env}, step.args...)
		if err != nil {
			return "", err
		}
		if o.status != 0 || o.stderr != "" {
			return "", fmt.Errorf("%s: status %d, stderr %q", step.name, o.status, o.stderr)
		}
		if step.contains == nil && o.stdout != step.want {
			return "", fmt.Errorf("%s printed %q, want %q", step.name, o.stdout, step.want)
		}
		for _, part := range step.contains {
			if !strings.Contains(o.stdout, part) {
				return "", fmt.Errorf("%s output lacks %q:\n%s", step.name, part, o.stdout)
			}
		}
		if line := w.recoveredText(o.stdout); step.private && line != "" {
			return "", fmt.Errorf("%s printed the recovered line %q", step.name, line)
		}
	}
	return "", nil
}

// recoveredText returns a line of the abandoned-branch fixture's states
// that text contains, or "" if it holds none. Inspection and diagnostics
// describe a history; they must not repeat what it recovers.
func (w *world) recoveredText(text string) string {
	states, err := w.states("abandoned-branch")
	if err != nil {
		return ""
	}
	for _, s := range states {
		for _, line := range s.lines {
			if strings.TrimSpace(line) != "" && strings.Contains(text, line) {
				return line
			}
		}
	}
	return ""
}

// checkFailures feeds the executable wrong, damaged, and missing inputs and
// broken output. Each must fail with the documented status and a
// diagnostic, and none may print partial results.
func (w *world) checkFailures(ctx context.Context) (string, error) {
	fixture := filepath.Join(w.corpus, "abandoned-branch")
	undo := filepath.Join(fixture, "history.undo")
	base := filepath.Join(fixture, "base.bin")
	bad := filepath.Join(w.root, "bad")
	at := func(name string) string { return filepath.Join(bad, name) }

	full, err := os.OpenFile("/dev/full", os.O_WRONLY, 0)
	if err != nil {
		return "", fmt.Errorf("a full-device check needs /dev/full: %w", err)
	}
	defer func() { _ = full.Close() }()

	// A pipe whose reader is gone, as when the reading command exits.
	reader, closed, err := os.Pipe()
	if err != nil {
		return "", err
	}
	_ = reader.Close()
	defer func() { _ = closed.Close() }()

	project := filepath.Join(w.root, "project")
	show := func(undo, base string, extra ...string) []string {
		return append([]string{"show", "--undo", undo, "--base", base}, extra...)
	}
	tests := []struct {
		name   string
		dir    string
		args   []string
		status int
		stderr string // part of the diagnostic; empty when stderr itself fails
		stdout *os.File
		failed *os.File // stderr, when it is meant to fail
	}{
		{name: "base that does not match", args: show(undo, at("other.go"), "--node", "2"), status: 1, stderr: "base mismatch"},
		{name: "truncated history", args: show(at("truncated"), base, "--node", "2"), status: 1, stderr: "truncated input"},
		{name: "unsupported format", args: []string{"inspect", "--undo", at("future")}, status: 1, stderr: "unsupported input"},
		{name: "not an undo file", args: []string{"inspect", "--undo", at("text.txt")}, status: 1, stderr: "invalid input"},
		{name: "missing history", args: []string{"inspect", "--undo", at("absent.undo")}, status: 1, stderr: "no such file"},
		{name: "directory as history", args: []string{"inspect", "--undo", bad}, status: 1, stderr: "not a regular file"},
		{name: "unknown node", args: show(undo, base, "--node", "99"), status: 1, stderr: "unknown node 99"},
		{name: "raw export without a policy", args: show(undo, base, "--node", "2", "--raw"), status: 2, stderr: "--final-newline"},
		{name: "unknown command", args: []string{"recover"}, status: 2, stderr: "unknown command"},
		{name: "output to a full device", args: show(undo, base, "--node", "2", "--raw", "--final-newline=include"), status: 1, stderr: "cannot write output", stdout: full},
		{name: "output to a closed pipe", args: show(undo, base, "--node", "2"), status: 1, stderr: "cannot write output", stdout: closed},
		{name: "a diagnostic to a full device", args: []string{"recover"}, status: 1, failed: full},
		{name: "missing base", args: show(undo, at("absent.go"), "--node", "2"), status: 1, stderr: "no such file"},
		{name: "missing source", dir: project, args: []string{"show", "--undo-dir", "../undo", "--node", "2", "absent.go"}, status: 1, stderr: "open source file"},
		{name: "histories in two undo directories", dir: project, args: []string{"show", "--undo-dir", "../undo", "--undo-dir", "../undo2", "--node", "2", "retry.go"}, status: 1, stderr: "more than one undo history matches"},
		{name: "FIFO as history", args: []string{"inspect", "--undo", at("pipe.undo")}, status: 1, stderr: "not a regular file"},
		{name: "base over its size limit", args: show(undo, filepath.Join(w.scratch, "large-base.go"), "--node", "2"), status: 1, stderr: "-byte limit"},
		{name: "invalid node ID", args: show(undo, base, "--node", "two"), status: 2, stderr: "non-negative decimal ID"},
		{name: "browse without a terminal", args: []string{"browse", "--undo", undo, "--base", base}, status: 1, stderr: "browse needs an interactive terminal"},
	}
	for _, tt := range tests {
		o, err := w.run(ctx, runOptions{dir: tt.dir, stdout: tt.stdout, stderr: tt.failed}, tt.args...)
		if err != nil {
			return "", fmt.Errorf("%s: %w", tt.name, err)
		}
		diagnosed := tt.failed != nil || strings.HasPrefix(o.stderr, "xunhen: ") && strings.Contains(o.stderr, tt.stderr)
		if o.status != tt.status || o.stdout != "" || !diagnosed {
			return "", fmt.Errorf("%s: status %d, stdout %q, stderr %q; want status %d and a diagnostic about %q",
				tt.name, o.status, o.stdout, o.stderr, tt.status, tt.stderr)
		}
		if line := w.recoveredText(o.stderr); line != "" {
			return "", fmt.Errorf("%s: the diagnostic repeats the recovered line %q", tt.name, line)
		}
	}
	return fmt.Sprintf(" (%d cases)", len(tests)), nil
}

// prepareInputs writes the walkthrough's source tree and the damaged files
// the failure checks read. They exist before the first fingerprint, so any
// change to them counts.
func (w *world) prepareInputs() error {
	fixture := filepath.Join(w.corpus, "abandoned-branch")
	base, err := readAll(filepath.Join(fixture, "base.bin"))
	if err != nil {
		return err
	}
	undo, err := readAll(filepath.Join(fixture, "history.undo"))
	if err != nil {
		return err
	}

	project := filepath.Join(w.root, "project")
	undoDir := filepath.Join(w.root, "undo")
	secondDir := filepath.Join(w.root, "undo2")
	bad := filepath.Join(w.root, "bad")
	for _, d := range []string{project, undoDir, secondDir, bad} {
		if err := os.Mkdir(d, 0o755); err != nil {
			return err
		}
	}
	source := filepath.Join(project, "retry.go")
	future := slices.Clone(undo)
	future[10] = 4 // format version 4, big-endian after the nine-byte magic
	files := map[string][]byte{
		source: base,
		// Neovim names an undo file after the source's full physical path
		// with every slash replaced by a percent sign.
		filepath.Join(undoDir, strings.ReplaceAll(source, "/", "%")):   undo,
		filepath.Join(secondDir, strings.ReplaceAll(source, "/", "%")): undo,
		filepath.Join(bad, "other.go"):                                 []byte("package other\n"),
		filepath.Join(bad, "truncated"):                                undo[:len(undo)/2],
		filepath.Join(bad, "future"):                                   future,
		filepath.Join(bad, "text.txt"):                                 []byte("not an undo file\n"),
	}
	for path, data := range files {
		if err := os.WriteFile(path, data, 0o444); err != nil {
			return err
		}
	}
	if err := syscall.Mkfifo(filepath.Join(bad, "pipe.undo"), 0o444); err != nil {
		return err
	}

	// A sparse base one byte over the limit costs no disk.
	large, err := os.Create(filepath.Join(w.scratch, "large-base.go"))
	if err != nil {
		return err
	}
	if err := large.Truncate(limits.Default().BaseBytes + 1); err != nil {
		_ = large.Close()
		return err
	}
	return large.Close()
}
