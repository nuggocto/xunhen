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

	// Failures inside a header name its sequence.
	tests := []struct {
		name     string
		limits   func(*limits.Limits)
		mutate   func(*testing.T, []byte)
		target   history.NodeID
		kind     undofile.ErrorKind
		field    string
		sequence undofile.Sequence
	}{
		{name: "state byte limit", target: 2, limits: func(l *limits.Limits) { l.StateBytes = baseBytes }, kind: undofile.Limit, field: "state bytes", sequence: 2},
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

// TestReplayEntryBoundaries replays one change whose entries face undo, so
// reconstructing the root applies them in order to the reference text. Each
// case names the edge of the line range or the limit it exercises. Expected
// states follow the entry rule in docs/undo-format.md: the stored lines
// replace the lines strictly between top and bottom, and bottom zero means
// one past the last line.
func TestReplayEntryBoundaries(t *testing.T) {
	t.Parallel()

	abc := []string{"a", "b", "c"}
	bytesOf := func(lines []string) int {
		total := 0
		for _, line := range lines {
			total += len(line) + 1
		}
		return total
	}

	tests := []struct {
		name    string
		base    []string
		entries []wireEntry
		limits  func(*limits.Limits)
		want    []string
		field   string // the failure's field, empty when replay succeeds
	}{
		{name: "whole buffer", base: abc, entries: []wireEntry{{top: 0, bottom: 0, lines: []string{"x"}}}, want: []string{"x"}},
		{name: "insertion before the first line", base: abc, entries: []wireEntry{{top: 0, bottom: 1, lines: []string{"x"}}}, want: []string{"x", "a", "b", "c"}},
		{name: "insertion after the last line", base: abc, entries: []wireEntry{{top: 3, bottom: 4, lines: []string{"x"}}}, want: []string{"a", "b", "c", "x"}},
		{name: "deletion of the last line", base: abc, entries: []wireEntry{{top: 2, bottom: 0}}, want: []string{"a", "b"}},
		{name: "bottom one past the last line", base: abc, entries: []wireEntry{{top: 1, bottom: 4, lines: []string{"x"}}}, want: []string{"a", "x"}},
		{name: "deleting everything leaves one empty line", base: abc, entries: []wireEntry{{top: 0, bottom: 0}}, want: []string{""}},
		{name: "an empty buffer grows", base: []string{""}, entries: []wireEntry{{top: 0, bottom: 0, lines: []string{"x", "y"}}}, want: []string{"x", "y"}},
		{
			name: "entries apply in stored order", base: abc,
			entries: []wireEntry{{top: 0, bottom: 2, lines: []string{"x"}}, {top: 2, bottom: 0, lines: []string{"y", "z"}}},
			want:    []string{"x", "b", "y", "z"},
		},
		{name: "bottom two past the last line", base: abc, entries: []wireEntry{{top: 0, bottom: 5}}, field: "entry range"},
		{name: "top past the last line", base: abc, entries: []wireEntry{{top: 4, bottom: 0}}, field: "entry range"},
		{
			name: "a later entry sees the earlier one's line count", base: abc,
			entries: []wireEntry{{top: 0, bottom: 0, lines: []string{"x"}}, {top: 1, bottom: 3}},
			field:   "entry range",
		},
		{
			name: "state lines exactly at the limit", base: abc,
			entries: []wireEntry{{top: 3, bottom: 0, lines: []string{"x"}}},
			limits:  func(l *limits.Limits) { l.StateLines = 4 },
			want:    []string{"a", "b", "c", "x"},
		},
		{
			name: "state lines one over the limit", base: abc,
			entries: []wireEntry{{top: 3, bottom: 0, lines: []string{"x", "y"}}},
			limits:  func(l *limits.Limits) { l.StateLines = 4 },
			field:   "state lines",
		},
		{
			name: "state bytes exactly at the limit", base: abc,
			entries: []wireEntry{{top: 3, bottom: 0, lines: []string{"xy"}}},
			limits:  func(l *limits.Limits) { l.StateBytes = bytesOf([]string{"a", "b", "c", "xy"}) },
			want:    []string{"a", "b", "c", "xy"},
		},
		{
			name: "state bytes one over the limit", base: abc,
			entries: []wireEntry{{top: 3, bottom: 0, lines: []string{"xyz"}}},
			limits:  func(l *limits.Limits) { l.StateBytes = bytesOf([]string{"a", "b", "c", "xy"}) },
			field:   "state bytes",
		},
		{
			// The final state would fit, but the state between the two
			// entries does not, and replay must not pass through it.
			name: "an intermediate state over the limit", base: abc,
			entries: []wireEntry{{top: 3, bottom: 0, lines: []string{"x", "y"}}, {top: 0, bottom: 0, lines: []string{"z"}}},
			limits:  func(l *limits.Limits) { l.StateLines = 4 },
			field:   "state lines",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			lim := limits.Default()
			if tt.limits != nil {
				tt.limits(&lim)
			}

			nodes := []wireNode{{id: 1, entries: tt.entries}}
			data := withReference(graphBytes(nodes, 1, 1, 0, 1, 1), tt.base)
			h, r := reconstructor(t, data, tt.base, lim)
			root, err := h.Lookup(0)
			if err != nil {
				t.Fatal(err)
			}

			snapshot, err := r.Reconstruct(t.Context(), root)
			if tt.field != "" {
				var problem *undofile.InputError
				if snapshot != nil || !errors.As(err, &problem) || problem.Field != tt.field || problem.Sequence != 1 {
					t.Fatalf("snapshot = %v, error = %v; want a failure on %s in change 1", snapshot, err, tt.field)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if got := snapshot.Lines(); !slices.Equal(got, tt.want) {
				t.Fatalf("root = %q, want %q", got, tt.want)
			}
		})
	}
}

// Deep histories of long files must replay with the default limits. A
// rewritten line moves no other line; an inserted line shifts every line below
// it, which chunked replay keeps to the few chunks it touches.
func TestReplayOfLongHistories(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		lines, edits int
		inserts      bool // each change inserts a line instead of rewriting line 1
	}{
		// Neovim keeps 1,000 undo levels by default.
		{name: "default undo depth of rewrites in 10,000 lines", lines: 10_000, edits: 1_000},
		{name: "rewrites in 100,000 lines", lines: 100_000, edits: 100},
		{name: "default undo depth of insertions in 100,000 lines", lines: 100_000, edits: 1_000, inserts: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			// state returns the buffer after change k. Rewrites keep one
			// versioned line on top; insertions stack one line per change
			// above the body.
			state := func(k int) []string {
				body := slices.Repeat([]string{"unchanged"}, tt.lines)
				if !tt.inserts {
					return append([]string{fmt.Sprint("version ", k)}, body[1:]...)
				}

				head := make([]string, 0, k)
				for i := k; i >= 1; i-- {
					head = append(head, fmt.Sprint("added ", i))
				}
				return append(head, body...)
			}

			// Every change is on the reference path, so its entry faces undo:
			// it removes line 1, and a rewrite puts the previous version back.
			nodes := make([]wireNode, tt.edits)
			for i := range nodes {
				id := int32(i + 1)
				undo := wireEntry{top: 0, bottom: 2}
				if !tt.inserts {
					undo.lines = []string{fmt.Sprint("version ", i)}
				}
				nodes[i] = wireNode{id: id, parent: id - 1, child: id + 1, entries: []wireEntry{undo}}
			}
			nodes[tt.edits-1].child = 0

			last := int32(tt.edits)
			base := state(tt.edits)
			data := withReference(graphBytes(nodes, 1, last, 0, last, last), base)
			h, r := reconstructor(t, data, base, limits.Default())

			for _, target := range []int{0, tt.edits / 2} {
				ref, err := h.Lookup(history.NodeID(target))
				if err != nil {
					t.Fatal(err)
				}
				snapshot, err := r.Reconstruct(t.Context(), ref)
				if err != nil {
					t.Fatal(err)
				}
				if !slices.Equal(snapshot.Lines(), state(target)) {
					t.Fatalf("node %d differs from its expected state", target)
				}
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

func reconstructor(t testing.TB, undo []byte, base []string, lim limits.Limits) (*history.History, *history.Reconstructor) {
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

// BenchmarkReconstruct replays line-count changes at the top of long files, so
// each edit would shift every later line of a flat slice. The second case is
// the most edits one change can hold under the default entry limit.
func BenchmarkReconstruct(b *testing.B) {
	cases := []struct {
		name                    string
		lines, changes, entries int
	}{
		{name: "1000 insertions in 100000 lines", lines: 100_000, changes: 1_000, entries: 1},
		{name: "250000 insertions in 1000000 lines", lines: 1_000_000, changes: 1, entries: 250_000},
	}

	for _, c := range cases {
		b.Run(c.name, func(b *testing.B) {
			// Facing undo, each entry removes the line its insertion added.
			nodes := make([]wireNode, c.changes)
			for i := range nodes {
				id := int32(i + 1)
				entries := slices.Repeat([]wireEntry{{top: 0, bottom: 2}}, c.entries)
				nodes[i] = wireNode{id: id, parent: id - 1, child: id + 1, entries: entries}
			}
			nodes[c.changes-1].child = 0

			last := int32(c.changes)
			base := slices.Repeat([]string{"line"}, c.lines)
			h, r := reconstructor(b, withReference(graphBytes(nodes, 1, last, 0, last, last), base), base, limits.Default())
			root, err := h.Lookup(0)
			if err != nil {
				b.Fatal(err)
			}

			for b.Loop() {
				if _, err := r.Reconstruct(b.Context(), root); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
