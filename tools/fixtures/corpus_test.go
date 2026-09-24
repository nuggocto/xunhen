package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"syscall"
	"testing"
)

const corpusPath = "../../testdata/undo"

func TestReferenceCorpus(t *testing.T) {
	t.Parallel()

	var index corpus
	if err := readJSON(filepath.Join(corpusPath, "corpus.json"), &index); err != nil {
		t.Fatal(err)
	}
	if index.Schema != 1 {
		t.Fatal("unexpected corpus schema")
	}
	if index.Producer.Version != producerVersion {
		t.Fatal("unexpected producer version")
	}
	if index.Producer.BinarySHA256 != producerDigest {
		t.Fatal("unexpected producer binary hash")
	}
	if index.Producer.SourceRevision != sourceRevision {
		t.Fatal("unexpected Neovim source revision")
	}
	if index.Producer.PackagingRevision != packagingRevision {
		t.Fatal("unexpected package revision")
	}

	cases := corpusCases()
	if len(index.Fixtures) != len(cases) {
		t.Fatal("fixture inventory does not match the authored cases")
	}

	for i, fixture := range cases {
		t.Run(fixture.Name, func(t *testing.T) {
			t.Parallel()

			entry := index.Fixtures[i]
			if entry.Name != fixture.Name {
				t.Fatal("fixture name differs from the inventory")
			}
			if len(entry.SHA256) != 4 {
				t.Fatal("expected four artifact hashes")
			}

			files := make(map[string][]byte)
			for _, name := range []string{"initial.bin", "base.bin", "history.undo", "oracle.json"} {
				path := filepath.Join(corpusPath, fixture.Name, name)
				data, err := readBounded(path, maxFixtureBytes)
				if err != nil {
					t.Fatal(err)
				}
				if digest(data) != entry.SHA256[name] {
					t.Fatalf("%s: content hash differs from the corpus manifest", name)
				}
				files[name] = data
			}

			if !bytes.Equal(files["initial.bin"], []byte(fixture.Initial)) {
				t.Fatal("initial bytes differ from the authored case")
			}
			if !bytes.Equal(files["base.bin"], []byte(fixture.Final)) {
				t.Fatal("base bytes differ from the authored case")
			}

			var result oracle
			if err := json.Unmarshal(files["oracle.json"], &result); err != nil {
				t.Fatal(err)
			}
			if err := validateOracle(fixture, result, files["history.undo"]); err != nil {
				t.Fatal(err)
			}

			wantSaved := fixture.Final
			if fixture.Persistence == "explicit" {
				wantSaved = fixture.Initial
			}
			if result.Created.SavedHex != hex.EncodeToString([]byte(wantSaved)) {
				t.Fatal("recorded source write differs from the recipe")
			}
		})
	}
}

func TestOracleRejectsCorruption(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		change func(*oracle, []byte) []byte
		want   string
	}{
		{
			name: "recipe drift",
			change: func(result *oracle, undo []byte) []byte {
				result.Recipe.Steps[0].LinesHex[0] = "00"
				return undo
			},
			want: "recipe",
		},
		{
			name: "purpose drift",
			change: func(result *oracle, undo []byte) []byte {
				result.Purpose = "a different scenario"
				return undo
			},
			want: "purpose",
		},
		{
			name: "classification drift",
			change: func(result *oracle, undo []byte) []byte {
				result.Classification = "a different policy"
				return undo
			},
			want: "classification",
		},
		{
			name: "missing base-mismatch evidence",
			change: func(result *oracle, undo []byte) []byte {
				result.Loaded.MismatchRejected = false
				return undo
			},
			want: "base-mismatch",
		},
		{
			name: "wrong reconstructed text",
			change: func(result *oracle, undo []byte) []byte {
				result.Loaded.States[0].LinesHex[0] = "00"
				return undo
			},
			want: "sequence 0 line",
		},
		{
			name: "missing replay observation",
			change: func(result *oracle, undo []byte) []byte {
				result.Loaded.States = result.Loaded.States[1:]
				return undo
			},
			want: "incomplete",
		},
		{
			name: "tree changed on reload",
			change: func(result *oracle, undo []byte) []byte {
				result.Loaded.Tree.SeqLast++
				return undo
			},
			want: "tree changed",
		},
		{
			name: "unsupported format",
			change: func(_ *oracle, undo []byte) []byte {
				undo[10] = 2
				return undo
			},
			want: "format envelope",
		},
		{
			name: "truncated envelope",
			change: func(_ *oracle, undo []byte) []byte {
				return undo[:10]
			},
			want: "format envelope",
		},
		{
			name: "base hash changed",
			change: func(_ *oracle, undo []byte) []byte {
				undo[11] ^= 0xff
				return undo
			},
			want: "base hash",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			fixture := corpusCases()[0]
			dir := filepath.Join(corpusPath, fixture.Name)

			var result oracle
			if err := readJSON(filepath.Join(dir, "oracle.json"), &result); err != nil {
				t.Fatal(err)
			}
			undo, err := readBounded(filepath.Join(dir, "history.undo"), maxFixtureBytes)
			if err != nil {
				t.Fatal(err)
			}

			undo = tt.change(&result, undo)
			err = validateOracle(fixture, result, undo)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("error = %v, want %q", err, tt.want)
			}
		})
	}
}

func TestFinalNewlineOracle(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		initial bool
		replay  bool
	}{
		{
			name:    "linear",
			initial: true,
			replay:  true,
		},
		{
			name:    "no-final-newline",
			initial: false,
			replay:  false,
		},
		{
			name:    "eol-option-change",
			initial: true,
			replay:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var result oracle
			path := filepath.Join(corpusPath, tt.name, "oracle.json")
			if err := readJSON(path, &result); err != nil {
				t.Fatal(err)
			}
			if len(result.Created.Observations) == 0 {
				t.Fatal("no creation observations")
			}
			if result.Created.Observations[0].State.EndOfLine != tt.initial {
				t.Fatal("unexpected original endofline option")
			}

			if len(result.Loaded.States) == 0 {
				t.Fatal("no replay observations")
			}
			for _, state := range result.Loaded.States {
				if state.EndOfLine != tt.replay {
					t.Fatalf("sequence %d endofline = %v, want %v", state.Seq, state.EndOfLine, tt.replay)
				}
			}
		})
	}
}

func TestEmptyBaseHash(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		fixture string
		input   string
		wantErr bool
	}{
		{
			name:    "automatic empty stream",
			fixture: "empty",
			input:   "",
		},
		{
			name:    "automatic rejects NUL hash",
			fixture: "empty",
			input:   "\x00",
			wantErr: true,
		},
		{
			name:    "explicit NUL terminator",
			fixture: "empty-wundo",
			input:   "\x00",
		},
		{
			name:    "explicit rejects empty hash",
			fixture: "empty-wundo",
			input:   "",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var fixture fixtureCase
			for _, candidate := range corpusCases() {
				if candidate.Name == tt.fixture {
					fixture = candidate
					break
				}
			}
			if fixture.Name == "" {
				t.Fatal("fixture not found")
			}

			dir := filepath.Join(corpusPath, fixture.Name)
			var result oracle
			if err := readJSON(filepath.Join(dir, "oracle.json"), &result); err != nil {
				t.Fatal(err)
			}
			undo, err := readBounded(filepath.Join(dir, "history.undo"), maxFixtureBytes)
			if err != nil {
				t.Fatal(err)
			}

			hash := sha256.Sum256([]byte(tt.input))
			copy(undo[11:43], hash[:])
			err = validateOracle(fixture, result, undo)
			if tt.wantErr {
				if err == nil || !strings.Contains(err.Error(), "base hash") {
					t.Fatalf("error = %v, want a base hash error", err)
				}
			} else if err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestBoundedFixtureReads(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		kind    string
		content string
		limit   int64
		wantErr bool
	}{
		{
			name: "empty",
		},
		{
			name:    "at limit",
			content: "abc",
			limit:   3,
		},
		{
			name:    "over limit",
			content: "abcd",
			limit:   3,
			wantErr: true,
		},
		{
			name:    "directory",
			kind:    "directory",
			limit:   3,
			wantErr: true,
		},
		{
			name:    "symlink",
			kind:    "symlink",
			content: "abc",
			limit:   3,
			wantErr: true,
		},
		{
			name:    "fifo",
			kind:    "fifo",
			limit:   3,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			dir := t.TempDir()
			path := filepath.Join(dir, "input")

			switch tt.kind {
			case "directory":
				path = dir

			case "symlink":
				target := filepath.Join(dir, "target")
				if err := os.WriteFile(target, []byte(tt.content), 0600); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(target, path); err != nil {
					t.Fatal(err)
				}

			case "fifo":
				if err := syscall.Mkfifo(path, 0600); err != nil {
					t.Fatal(err)
				}

			default:
				if err := os.WriteFile(path, []byte(tt.content), 0600); err != nil {
					t.Fatal(err)
				}
			}

			got, err := readBounded(path, tt.limit)
			if (err != nil) != tt.wantErr {
				t.Fatalf("read error = %v, want error = %v", err, tt.wantErr)
			}
			if !tt.wantErr && string(got) != tt.content {
				t.Fatalf("read = %q, want %q", got, tt.content)
			}
		})
	}
}

func TestProducerPin(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		kind string
		want string
	}{
		{
			name: "different executable",
			kind: "file",
			want: "SHA-256",
		},
		{
			name: "missing executable",
			kind: "missing",
			want: "no such file",
		},
		{
			name: "symlink to a different executable",
			kind: "symlink",
			want: "SHA-256",
		},
		{
			name: "relative executable",
			kind: "relative",
			want: "absolute executable path",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			dir := t.TempDir()
			path := filepath.Join(dir, "nvim")
			switch tt.kind {
			case "file":
				if err := os.WriteFile(path, []byte("not the pinned editor"), 0700); err != nil {
					t.Fatal(err)
				}
			case "symlink":
				target := filepath.Join(dir, "target")
				if err := os.WriteFile(target, []byte("not the pinned editor"), 0700); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(target, path); err != nil {
					t.Fatal(err)
				}
			case "relative":
				path = "nvim"
			}

			out := filepath.Join(dir, "output")
			err := generate(context.Background(), path, out)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("error = %v, want %q", err, tt.want)
			}
			if _, err := os.Stat(out); !os.IsNotExist(err) {
				t.Fatal("rejected producer created an output directory")
			}
		})
	}
}

func TestProducerOutputLimit(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		size    int
		wantErr bool
	}{
		{
			name: "at limit",
			size: 64 << 10,
		},
		{
			name:    "above limit",
			size:    (64 << 10) + 1,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			input := strings.NewReader(strings.Repeat("x", tt.size))
			// Avoid WriteTo so an accidental, unbounded ReadFrom is exercised too.
			reader := io.LimitReader(input, int64(tt.size))
			var output limitedOutput

			_, err := io.Copy(&output, reader)
			if (err != nil) != tt.wantErr {
				t.Fatalf("write error = %v, want error = %v", err, tt.wantErr)
			}

			size := len(output.String())
			if size > 64<<10 {
				t.Fatal("producer output exceeded the memory limit")
			}
			if !tt.wantErr && size != tt.size {
				t.Fatal("producer output was silently truncated")
			}
		})
	}
}

func TestNamingCorpus(t *testing.T) {
	t.Parallel()

	var index nameCorpus
	if err := readJSON("../../testdata/discovery/names.json", &index); err != nil {
		t.Fatal(err)
	}
	if index.Schema != 1 || index.Producer.BinarySHA256 != producerDigest || index.Producer.SourceRevision != sourceRevision {
		t.Fatal("naming corpus does not come from the pinned producer")
	}

	cases := nameCases()
	if len(index.Cases) != len(cases) {
		t.Fatal("naming inventory does not match the authored cases")
	}

	for i, c := range cases {
		t.Run(c.Name, func(t *testing.T) {
			t.Parallel()

			if err := checkNameRecord(c, index.Cases[i]); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestNameRecordRejectsDrift(t *testing.T) {
	t.Parallel()

	var index nameCorpus
	if err := readJSON("../../testdata/discovery/names.json", &index); err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name   string
		change func(*nameRecord)
		want   string
	}{
		{
			name:   "different recorded name",
			change: func(r *nameRecord) { r.Neovim.UndofileHex = hex.EncodeToString([]byte("{root}/undo/other")) },
			want:   "undofile()",
		},
		{
			name:   "different layout",
			change: func(r *nameRecord) { r.FilesHex = nil },
			want:   "layout",
		},
		{
			name:   "different invocation",
			change: func(r *nameRecord) { r.UndoDirHex = hex.EncodeToString([]byte(".")) },
			want:   "invocation",
		},
		{
			name:   "missing written file",
			change: func(r *nameRecord) { r.Neovim.WrittenHex = nil },
			want:   "written",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			record := index.Cases[0]
			record.Neovim.WrittenHex = slices.Clone(record.Neovim.WrittenHex)
			tt.change(&record)
			if err := checkNameRecord(nameCases()[0], record); err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("error = %v, want %q", err, tt.want)
			}
		})
	}
}
