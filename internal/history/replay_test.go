package history_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/nuggocto/xunhen/internal/history"
	"github.com/nuggocto/xunhen/internal/limits"
	"github.com/nuggocto/xunhen/internal/undofile"
)

func oracleLines(t *testing.T, encoded []string) []string {
	t.Helper()

	lines := make([]string, len(encoded))
	for i, raw := range encoded {
		line, err := hex.DecodeString(raw)
		if err != nil {
			t.Fatal(err)
		}
		lines[i] = string(line)
	}

	return lines
}

func TestReplayOracle(t *testing.T) {
	t.Parallel()

	paths, err := filepath.Glob("../../testdata/undo/*/oracle.json")
	if err != nil || len(paths) == 0 {
		t.Fatalf("oracle inventory: %v", err)
	}

	for _, path := range paths {
		name := filepath.Base(filepath.Dir(path))
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			var oracle oracleFile
			if err := json.Unmarshal(readFixture(t, name, "oracle.json"), &oracle); err != nil {
				t.Fatal(err)
			}

			h, r := reconstructor(t, readFixture(t, name, "history.undo"), oracleLines(t, oracle.Loaded.Anchor.LinesHex), limits.Default())

			for i, want := range oracle.Loaded.States {
				t.Run(fmt.Sprintf("visit_%d_node_%d", i, want.Seq), func(t *testing.T) {
					ref, err := h.Lookup(history.NodeID(want.Seq))
					if err != nil {
						t.Fatal(err)
					}

					snapshot, err := r.Reconstruct(t.Context(), ref)
					if err != nil {
						t.Fatal(err)
					}

					if got, expected := snapshot.Lines(), oracleLines(t, want.LinesHex); !slices.Equal(got, expected) {
						t.Fatalf("lines = %q, want %q", got, expected)
					}
				})
			}
		})
	}
}

func TestReplayRejectsInvalidWork(t *testing.T) {
	t.Parallel()

	var oracle oracleFile
	if err := json.Unmarshal(readFixture(t, "abandoned-branch", "oracle.json"), &oracle); err != nil {
		t.Fatal(err)
	}

	undo := readFixture(t, "abandoned-branch", "history.undo")
	lines := oracleLines(t, oracle.Loaded.Anchor.LinesHex)
	baseBytes := 0
	for _, line := range lines {
		baseBytes += len(line) + 1
	}

	// Budget failures inside a header name its sequence. The initial base copy
	// belongs to no header, so it reports sequence zero.
	tests := []struct {
		name     string
		limits   func(*limits.Limits)
		mutate   func(*testing.T, []byte)
		target   history.NodeID
		kind     undofile.ErrorKind
		field    string
		sequence undofile.Sequence
	}{
		{name: "header budget", limits: func(l *limits.Limits) { l.ReplayHeaders = 1 }, kind: undofile.Limit, field: "replay headers", sequence: 1},
		{name: "entry budget", limits: func(l *limits.Limits) { l.ReplayEntries = 1 }, kind: undofile.Limit, field: "replay entries", sequence: 1},
		{name: "initial copy budget", limits: func(l *limits.Limits) { l.ReplayMoves = len(lines) - 1 }, kind: undofile.Limit, field: "replay line moves"},
		{name: "line move budget", limits: func(l *limits.Limits) { l.ReplayMoves = len(lines) }, kind: undofile.Limit, field: "replay line moves", sequence: 3},
		{name: "state byte budget", target: 2, limits: func(l *limits.Limits) { l.StateBytes = baseBytes }, kind: undofile.Limit, field: "state bytes", sequence: 2},
		{name: "invalid range", mutate: func(t *testing.T, data []byte) {
			at := bytes.Index(data, []byte{0xf5, 0x18})
			if at < 0 {
				t.Fatal("fixture text entry missing")
			}
			binary.BigEndian.PutUint32(data[at+2:], 1000)
			binary.BigEndian.PutUint32(data[at+6:], 0)
		}, kind: undofile.Invalid, field: "entry range", sequence: 1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			data := bytes.Clone(undo)
			if tt.mutate != nil {
				tt.mutate(t, data)
			}

			lim := limits.Default()
			if tt.limits != nil {
				tt.limits(&lim)
			}

			h, r := reconstructor(t, data, lines, lim)
			ref, err := h.Lookup(tt.target)
			if err != nil {
				t.Fatal(err)
			}

			snapshot, err := r.Reconstruct(t.Context(), ref)

			var problem *undofile.InputError
			if snapshot != nil || !errors.As(err, &problem) {
				t.Fatalf("snapshot = %v, error = %v; want an input error", snapshot, err)
			}
			if problem.Kind != tt.kind || problem.Field != tt.field || problem.Sequence != tt.sequence {
				t.Fatalf("error = %#v; want %s on %s at sequence %d", problem, tt.kind, tt.field, tt.sequence)
			}
		})
	}
}

// A one-line edit replaces one line with one line, so the lines below it never
// move. Deep chains of such edits at the top of a long file must replay within
// the default budgets instead of paying for the whole file on every edit.
func TestReplayOfOneLineEdits(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		lines, edits int
	}{
		// Neovim keeps 1,000 undo levels by default.
		{name: "default undo depth in a 10,000-line file", lines: 10_000, edits: 1_000},
		{name: "short history in a 100,000-line file", lines: 100_000, edits: 100},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			base := make([]string, tt.lines)
			for i := range base {
				base[i] = "unchanged"
			}
			base[0] = fmt.Sprint("version ", tt.edits)

			// Every change is on the reference path, so its entry faces undo:
			// undoing change id restores version id-1 on line 1.
			nodes := make([]wireNode, tt.edits)
			for i := range nodes {
				id := int32(i + 1)
				nodes[i] = wireNode{
					id: id, parent: id - 1, child: id + 1,
					entries: []wireEntry{{top: 0, bottom: 2, lines: []string{fmt.Sprint("version ", i)}}},
				}
			}
			nodes[tt.edits-1].child = 0

			last := int32(tt.edits)
			data := withReference(graphBytes(nodes, 1, last, 0, last, last), base)
			h, r := reconstructor(t, data, base, limits.Default())

			root, err := h.Lookup(0)
			if err != nil {
				t.Fatal(err)
			}
			snapshot, err := r.Reconstruct(t.Context(), root)
			if err != nil {
				t.Fatal(err)
			}

			got := snapshot.Lines()
			if len(got) != tt.lines || got[0] != "version 0" || got[len(got)-1] != "unchanged" {
				t.Fatalf("root has %d lines, first %q, last %q", len(got), got[0], got[len(got)-1])
			}
		})
	}
}

// withReference stores the reference hash and line count for lines in the
// envelope, at offsets 11 and 43 as docs/undo-format.md lays them out. The
// lines must not contain NUL, which the hash would store as LF.
func withReference(data []byte, lines []string) []byte {
	hash := sha256.New()
	for _, line := range lines {
		hash.Write([]byte(line))
		hash.Write([]byte{0})
	}

	copy(data[11:43], hash.Sum(nil))
	binary.BigEndian.PutUint32(data[43:47], uint32(len(lines)))
	return data
}

func TestReplayCancellation(t *testing.T) {
	t.Parallel()

	var oracle oracleFile
	if err := json.Unmarshal(readFixture(t, "abandoned-branch", "oracle.json"), &oracle); err != nil {
		t.Fatal(err)
	}

	h, r := reconstructor(t, readFixture(t, "abandoned-branch", "history.undo"), oracleLines(t, oracle.Loaded.Anchor.LinesHex), limits.Default())

	// Both targets need replay work: the root only undoes, while the abandoned
	// experiment undoes to the shared ancestor and then redoes.
	tests := []struct {
		name   string
		target history.NodeID
	}{
		{name: "up to the retained root", target: 0},
		{name: "across to another branch", target: 2},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			ref, err := h.Lookup(tt.target)
			if err != nil {
				t.Fatal(err)
			}

			ctx, cancel := context.WithCancel(t.Context())
			cancel()

			if snapshot, err := r.Reconstruct(ctx, ref); snapshot != nil || !errors.Is(err, context.Canceled) {
				t.Fatalf("snapshot = %v, error = %v; want cancellation", snapshot, err)
			}
		})
	}
}

func TestBaseBindingRejectsWrongInput(t *testing.T) {
	t.Parallel()

	var oracle oracleFile
	if err := json.Unmarshal(readFixture(t, "linear", "oracle.json"), &oracle); err != nil {
		t.Fatal(err)
	}
	lines := oracleLines(t, oracle.Loaded.Anchor.LinesHex)

	undo := readFixture(t, "linear", "history.undo")
	file := decoded(t, undo)
	h, err := history.New(t.Context(), file, limits.Default())
	if err != nil {
		t.Fatal(err)
	}

	changed := slices.Clone(lines)
	changed[0] += "x"

	// The hash stores a buffer NUL as LF, so LF in place of NUL would match
	// the reference if verification did not reject it.
	var nulOracle oracleFile
	if err := json.Unmarshal(readFixture(t, "embedded-nul", "oracle.json"), &nulOracle); err != nil {
		t.Fatal(err)
	}
	withLF := oracleLines(t, nulOracle.Loaded.Anchor.LinesHex)
	for i, line := range withLF {
		withLF[i] = strings.ReplaceAll(line, "\x00", "\n")
	}

	// Wrong text fails verification. A foreign file verifies on its own but
	// must not bind to a history decoded from different bytes in memory.
	tests := []struct {
		name  string
		file  *undofile.DecodedFile
		lines []string
		kind  undofile.ErrorKind // empty when verification succeeds
	}{
		{name: "changed text", file: file, lines: changed, kind: undofile.Mismatch},
		{name: "extra line", file: file, lines: append(slices.Clone(lines), ""), kind: undofile.Mismatch},
		{name: "LF standing in for NUL", file: decoded(t, readFixture(t, "embedded-nul", "history.undo")), lines: withLF, kind: undofile.Invalid},
		{name: "foreign decoded file", file: decoded(t, undo), lines: lines},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			base, err := undofile.VerifyBase(t.Context(), tt.file, tt.name, tt.lines, limits.Default())
			if tt.kind != "" {
				var problem *undofile.InputError
				if base != nil || !errors.As(err, &problem) || problem.Kind != tt.kind {
					t.Fatalf("base = %v, error = %v; want %s", base, err, tt.kind)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}

			if _, err := history.Bind(h, base, limits.Default()); err == nil {
				t.Fatal("foreign base accepted")
			}
		})
	}
}

func reconstructor(t *testing.T, undo []byte, base []string, lim limits.Limits) (*history.History, *history.Reconstructor) {
	t.Helper()

	file := decoded(t, undo)
	h, err := history.New(t.Context(), file, lim)
	if err != nil {
		t.Fatal(err)
	}

	verified, err := undofile.VerifyBase(t.Context(), file, "oracle", base, lim)
	if err != nil {
		t.Fatal(err)
	}

	r, err := history.Bind(h, verified, lim)
	if err != nil {
		t.Fatal(err)
	}

	return h, r
}
