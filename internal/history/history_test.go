package history_test

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/nuggocto/xunhen/internal/history"
	"github.com/nuggocto/xunhen/internal/limits"
	"github.com/nuggocto/xunhen/internal/undofile"
)

type oracleEntry struct {
	Seq  int
	Time int64
	Save int
	Alt  []oracleEntry
}

type oracleFile struct {
	Loaded struct {
		Anchor struct {
			Seq      int
			LinesHex []string `json:"lines_hex"`
		}
		Tree struct {
			SeqLast int   `json:"seq_last"`
			SeqCur  int   `json:"seq_cur"`
			TimeCur int64 `json:"time_cur"`
			Entries []oracleEntry
		}
	}
}

func readFixture(t testing.TB, name, file string) []byte {
	t.Helper()

	data, err := os.ReadFile(filepath.Join("../../testdata/undo", name, file))
	if err != nil {
		t.Fatal(err)
	}

	return data
}

func decoded(t testing.TB, data []byte) *undofile.DecodedFile {
	t.Helper()

	file, err := undofile.Decode(t.Context(), "fixture", bytes.NewReader(data), limits.Default())
	if err != nil {
		t.Fatal(err)
	}

	return file
}

func built(t testing.TB, data []byte) *history.History {
	t.Helper()

	h, err := history.New(t.Context(), decoded(t, data), limits.Default())
	if err != nil {
		t.Fatal(err)
	}

	return h
}

func TestReferenceGraphs(t *testing.T) {
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

			h := built(t, readFixture(t, name, "history.undo"))
			want := oracleOrder(oracle.Loaded.Tree.Entries, 0, 0)
			if h.Count() != len(want)+1 {
				t.Fatalf("states = %d, want %d", h.Count(), len(want)+1)
			}

			ref, err := h.Reference()
			if err != nil {
				t.Fatal(err)
			}

			anchor, err := h.Info(ref)
			if err != nil || int(anchor.ID) != oracle.Loaded.Anchor.Seq {
				t.Fatalf("reference = %+v, %v; want %d", anchor, err, oracle.Loaded.Anchor.Seq)
			}

			meta, err := h.Metadata()
			if err != nil {
				t.Fatal(err)
			}
			if int(meta.LastSequence) != oracle.Loaded.Tree.SeqLast ||
				int(meta.TimelineSequence) != oracle.Loaded.Tree.SeqCur ||
				meta.CurrentTime != oracle.Loaded.Tree.TimeCur {
				t.Fatalf("timeline metadata differs from Neovim: %+v", meta)
			}
			if int(meta.BaseLines) != len(oracle.Loaded.Anchor.LinesHex) {
				t.Fatal("reference line count differs from Neovim")
			}

			hash := sha256.New()
			// This corpus case has ML_EMPTY set at automatic persistence time.
			if name != "empty" {
				for _, encoded := range oracle.Loaded.Anchor.LinesHex {
					line, err := hex.DecodeString(encoded)
					if err != nil {
						t.Fatal(err)
					}

					hash.Write(bytes.ReplaceAll(line, []byte{0}, []byte{'\n'}))
					hash.Write([]byte{0})
				}
			}
			if !bytes.Equal(meta.BaseHash[:], hash.Sum(nil)) {
				t.Fatal("decoded reference hash differs from Neovim's buffer bytes")
			}

			rootRef, err := h.Lookup(0)
			if err != nil {
				t.Fatal(err)
			}

			root, err := h.Info(rootRef)
			if err != nil || root.HasEvent || int(root.PreferredChild) != oracle.Loaded.Tree.Entries[0].Seq {
				t.Fatalf("invalid retained root: %+v, %v", root, err)
			}

			for i, expected := range want {
				ref, err := h.At(i + 1)
				if err != nil {
					t.Fatal(err)
				}

				got, err := h.Info(ref)
				if err != nil {
					t.Fatal(err)
				}

				if got.ID != expected.ID || got.Parent != expected.Parent || got.PreferredChild != expected.PreferredChild ||
					got.NextSibling != expected.NextSibling || got.PreviousSibling != expected.PreviousSibling {
					t.Fatalf("node = %+v, want %+v", got, expected)
				}
				if !got.HasEvent || got.Time != expected.Time || got.Save != expected.Save {
					t.Fatalf("event metadata = %+v, want %+v", got, expected)
				}

				lookup, err := h.Lookup(got.ID)
				if err != nil || lookup != ref {
					t.Fatalf("lookup changed identity: %v", err)
				}
			}
		})
	}
}

// Neovim's small checked-in oracle trees are capped at 64 nodes by their
// generator. This independent traversal follows undotree(), not decoded links.
func oracleOrder(entries []oracleEntry, parent, previous history.NodeID) []history.NodeInfo {
	if len(entries) == 0 {
		return nil
	}

	entry := entries[0]
	n := history.NodeInfo{
		ID:              history.NodeID(entry.Seq),
		Parent:          parent,
		PreviousSibling: previous,
		HasEvent:        true,
		Time:            entry.Time,
		Save:            undofile.SaveNumber{Known: true, Value: int32(entry.Save)},
	}
	if len(entries) > 1 {
		n.PreferredChild = history.NodeID(entries[1].Seq)
	}
	if len(entry.Alt) > 0 {
		n.NextSibling = history.NodeID(entry.Alt[0].Seq)
	}

	out := []history.NodeInfo{n}
	out = append(out, oracleOrder(entries[1:], n.ID, 0)...)
	return append(out, oracleOrder(entry.Alt, parent, n.ID)...)
}

type wireNode struct {
	id, parent, child, next, previous int32
	time                              int64
}

// Author raw records independently of the decoder to isolate graph invariants.
// These synthetic metadata-only changes have no text or extmark entries.
func graphBytes(nodes []wireNode, oldest, newest, redo, last, timeline int32) []byte {
	b := append([]byte("Vim\x9fUnDo\xe5\x00\x03"), make([]byte, 32)...)
	for _, n := range []int32{1, 0, 0, 0, oldest, newest, redo, int32(len(nodes)), last, timeline} {
		b = binary.BigEndian.AppendUint32(b, uint32(n))
	}
	b = binary.BigEndian.AppendUint64(b, 0)
	b = append(b, 0) // absent optional file metadata

	for _, n := range nodes {
		b = binary.BigEndian.AppendUint16(b, 0x5fd0)
		for _, value := range []int32{n.parent, n.child, n.next, n.previous, n.id} {
			b = binary.BigEndian.AppendUint32(b, uint32(value))
		}

		b = append(b, make([]byte, 12)...)
		b = binary.BigEndian.AppendUint32(b, ^uint32(0)) // cursor virtual column -1
		b = append(b, make([]byte, 2+26*12+32)...)
		b = binary.BigEndian.AppendUint64(b, uint64(n.time))
		b = append(b, 0, 0x35, 0x81, 0x35, 0x81)
	}

	return append(b, 0xe7, 0xaa)
}

func TestGraphValidation(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name                                 string
		nodes                                []wireNode
		oldest, newest, redo, last, timeline int32
		want                                 string
	}{
		{name: "no multilevel headers"},
		{
			name:   "pruned allocation gaps",
			nodes:  []wireNode{{id: 3}},
			oldest: 3, newest: 3, last: 8, timeline: 2,
		},
		{
			name: "nonmonotonic timestamps",
			nodes: []wireNode{
				{id: 1, child: 2, time: 10},
				{id: 2, parent: 1, time: -5},
			},
			oldest: 1, newest: 2, last: 2,
		},
		{
			name:   "missing optional saves",
			nodes:  []wireNode{{id: 1}},
			oldest: 1, newest: 1, last: 1,
		},
		{
			name:   "newest absent with pending redo",
			nodes:  []wireNode{{id: 1}},
			oldest: 1, redo: 1, last: 1,
		},
		{
			name:   "newest nonleaf with pending redo",
			nodes:  []wireNode{{id: 1, child: 2}, {id: 2, parent: 1}},
			oldest: 1, newest: 1, redo: 2, last: 2,
		},
		{
			name:   "duplicate identities",
			nodes:  []wireNode{{id: 1}, {id: 1}},
			oldest: 1, newest: 1, last: 1,
			want: "sequence",
		},
		{
			name:   "dangling parent",
			nodes:  []wireNode{{id: 1}, {id: 2, parent: 3}},
			oldest: 1, newest: 1, last: 2,
			want: "parent",
		},
		{
			name:   "dangling oldest root",
			nodes:  []wireNode{{id: 1}},
			oldest: 2, newest: 1, last: 1,
			want: "preferred child",
		},
		{
			name:   "dangling current",
			nodes:  []wireNode{{id: 1}},
			oldest: 1, newest: 1, redo: 2, last: 2,
			want: "reference position",
		},
		{
			name:   "self ancestry",
			nodes:  []wireNode{{id: 1, parent: 1}},
			newest: 1, last: 1,
			want: "parent",
		},
		{
			name:  "cycle in ancestry",
			nodes: []wireNode{{id: 1, parent: 2, child: 2}, {id: 2, parent: 1, child: 1}},
			last:  2,
			want:  "parent",
		},
		{
			name:   "unlinked alternate",
			nodes:  []wireNode{{id: 1}, {id: 2}},
			oldest: 1, newest: 1, last: 2,
			want: "preferred child",
		},
		{
			name:   "one-way sibling",
			nodes:  []wireNode{{id: 1, next: 2}, {id: 2}},
			oldest: 1, newest: 1, last: 2,
			want: "next sibling",
		},
		{
			name: "sibling cycle disconnected from root",
			nodes: []wireNode{
				{id: 1, next: 2, previous: 2},
				{id: 2, next: 1, previous: 1},
			},
			newest: 1, last: 2,
			want: "relationships",
		},
		{
			name: "wrong parent for alternate",
			nodes: []wireNode{
				{id: 1, child: 2},
				{id: 2, parent: 1, next: 3},
				{id: 3, previous: 2},
			},
			oldest: 1, newest: 2, last: 3,
			want: "next sibling",
		},
		{
			name:   "current on other branch",
			nodes:  []wireNode{{id: 1, next: 2}, {id: 2, previous: 1}},
			oldest: 1, newest: 1, redo: 2, last: 2,
			want: "next redo",
		},
		{
			name:   "newest on other branch without redo",
			nodes:  []wireNode{{id: 1, next: 2}, {id: 2, previous: 1}},
			oldest: 1, newest: 2, last: 2,
			want: "newest header",
		},
		{
			name:   "newest not leaf without redo",
			nodes:  []wireNode{{id: 1, child: 2}, {id: 2, parent: 1}},
			oldest: 1, newest: 1, last: 2,
			want: "newest header",
		},
		{
			name:   "identity above allocation",
			nodes:  []wireNode{{id: 2}},
			oldest: 2, newest: 2, last: 1,
			want: "sequence",
		},
		{
			name:   "timeline above allocation",
			nodes:  []wireNode{{id: 1}},
			oldest: 1, newest: 1, last: 1, timeline: 2,
			want: "timeline sequence",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			file := decoded(t, graphBytes(tt.nodes, tt.oldest, tt.newest, tt.redo, tt.last, tt.timeline))
			h, err := history.New(t.Context(), file, limits.Default())

			if tt.want != "" {
				var problem *undofile.InputError
				if h != nil || !errors.As(err, &problem) || problem.Kind != undofile.Invalid || problem.Field != tt.want {
					t.Fatalf("history = %v; error = %#v, want invalid %s", h, err, tt.want)
				}
				return
			}

			if err != nil || h.Count() != len(tt.nodes)+1 {
				t.Fatalf("valid graph rejected: %v", err)
			}

			for _, n := range tt.nodes {
				ref, _ := h.Lookup(history.NodeID(n.id))
				info, _ := h.Info(ref)
				if info.Save.Known || info.Time != n.time {
					t.Fatal("missing save or nonmonotonic time was changed")
				}
			}
		})
	}
}

func TestHistoryOwnershipAndInvalidSelectors(t *testing.T) {
	t.Parallel()

	data := readFixture(t, "linear", "history.undo")
	tests := []struct {
		name string
		call func(*testing.T, *history.History) error
	}{
		{
			name: "foreign history",
			call: func(t *testing.T, h *history.History) error {
				ref, _ := built(t, data).Lookup(1)
				_, err := h.Info(ref)
				return err
			},
		},
		{
			name: "zero reference",
			call: func(_ *testing.T, h *history.History) error {
				_, err := h.Info(history.NodeRef{})
				return err
			},
		},
		{
			name: "zero history",
			call: func(_ *testing.T, _ *history.History) error {
				_, err := new(history.History).Lookup(0)
				return err
			},
		},
		{
			name: "nil history",
			call: func(_ *testing.T, _ *history.History) error {
				_, err := (*history.History)(nil).Reference()
				return err
			},
		},
		{
			name: "unknown ID",
			call: func(_ *testing.T, h *history.History) error {
				_, err := h.Lookup(-1)
				return err
			},
		},
		{
			name: "negative index",
			call: func(_ *testing.T, h *history.History) error {
				_, err := h.At(-1)
				return err
			},
		},
		{
			name: "past last index",
			call: func(_ *testing.T, h *history.History) error {
				_, err := h.At(h.Count())
				return err
			},
		},
		{
			name: "nil decoded file",
			call: func(t *testing.T, _ *history.History) error {
				_, err := history.New(t.Context(), nil, limits.Default())
				return err
			},
		},
		{
			name: "zero decoded file",
			call: func(t *testing.T, _ *history.History) error {
				_, err := history.New(t.Context(), new(undofile.DecodedFile), limits.Default())
				return err
			},
		},
		{
			name: "root record",
			call: func(_ *testing.T, h *history.History) error {
				root, _ := h.Lookup(0)
				_, err := h.Record(root)
				return err
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			h := built(t, data)
			if err := tt.call(t, h); err == nil {
				t.Fatal("invalid state or selector accepted")
			}
		})
	}
}

func TestHistoryBoundsAndCancellation(t *testing.T) {
	t.Parallel()

	const count = 4096
	nodes := make([]wireNode, count)
	for i := range nodes {
		nodes[i] = wireNode{id: int32(i + 1), parent: int32(i), child: int32(i + 2)}
	}
	nodes[count-1].child = 0
	file := decoded(t, graphBytes(nodes, 1, count, 0, count, count))

	tests := []struct {
		name     string
		maxNodes int
		cancel   bool
		wantKind undofile.ErrorKind
	}{
		{name: "deep chain at configured limit", maxNodes: count},
		{name: "above configured limit", maxNodes: count - 1, wantKind: undofile.Limit},
		{name: "cancelled", maxNodes: count, cancel: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			lim := limits.Default()
			lim.Nodes = tt.maxNodes

			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			if tt.cancel {
				cancel()
			}

			h, err := history.New(ctx, file, lim)

			switch {
			case tt.cancel:
				if h != nil || !errors.Is(err, context.Canceled) {
					t.Fatalf("cancelled construction: %v", err)
				}

			case tt.wantKind != "":
				var problem *undofile.InputError
				if h != nil || !errors.As(err, &problem) || problem.Kind != tt.wantKind {
					t.Fatalf("expected node budget error, got %v", err)
				}

			default:
				if err != nil || h.Count() != count+1 {
					t.Fatalf("deep chain: %v", err)
				}
			}
		})
	}
}

func FuzzHistory(f *testing.F) {
	// Mutate a syntactically valid graph rather than mostly fuzzing envelope
	// rejection. Each input encodes at most 64 nodes and four references per node.
	f.Add([]byte{0, 2, 0, 0, 1, 0, 0, 0}, byte(1), byte(2), byte(0))
	f.Add([]byte{0, 0, 2, 0, 0, 0, 0, 1}, byte(1), byte(1), byte(0))

	f.Fuzz(func(t *testing.T, links []byte, oldest, newest, redo byte) {
		if len(links) > 256 {
			t.Skip()
		}

		nodes := make([]wireNode, len(links)/4)
		for i := range nodes {
			b := links[4*i : 4*i+4]
			nodes[i] = wireNode{
				id:       int32(i + 1),
				parent:   int32(b[0]),
				child:    int32(b[1]),
				next:     int32(b[2]),
				previous: int32(b[3]),
			}
		}

		data := graphBytes(nodes, int32(oldest), int32(newest), int32(redo), int32(len(nodes)), 0)
		file := decoded(t, data)
		h, err := history.New(t.Context(), file, limits.Default())
		if err != nil {
			if h != nil {
				t.Fatal("invalid graph returned a history")
			}
			return
		}
		if h.Count() != len(nodes)+1 {
			t.Fatal("validated graph lost records")
		}

		seen := make(map[history.NodeID]bool)
		for i := 0; i < h.Count(); i++ {
			ref, err := h.At(i)
			if err != nil {
				t.Fatal(err)
			}

			n, err := h.Info(ref)
			if err != nil || seen[n.ID] {
				t.Fatalf("duplicate or unavailable node: %v", err)
			}

			seen[n.ID] = true
			if n.ID != 0 && !seen[n.Parent] {
				t.Fatal("child visited before parent")
			}
		}
	})
}
