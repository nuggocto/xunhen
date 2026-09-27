package history_test

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/nuggocto/xunhen/internal/history"
	"github.com/nuggocto/xunhen/internal/limits"
	"github.com/nuggocto/xunhen/internal/undofile"
)

// Per-input ceilings keep one fuzz input to a few milliseconds.
const (
	fuzzNodes       = 24
	fuzzVisits      = 12
	fuzzInputBytes  = 4096
	fuzzMaxLines    = 5
	fuzzMaxBetween  = 2 // intermediate states inside one change
	fuzzMutations   = 16
	fuzzHistoryByte = 64 << 10
)

// fuzzLimits are small enough that a damaged fixture cannot make one input
// expensive, and large enough for every generated history.
func fuzzLimits() limits.Limits {
	lim := limits.Default()
	lim.InputBytes = fuzzHistoryByte
	lim.Nodes = 64
	lim.Entries = 256
	lim.StoredLines = 2048
	lim.StateLines = 2048
	lim.StateBytes = 256 << 10
	lim.LineBytes = 4096
	return lim
}

// FuzzReconstruct runs the whole path from undo bytes to reconstructed text:
// decoding, graph validation, base verification, and replay. An even first
// byte builds a small history from the rest of the input and checks every
// state against a model that knows each node's text by construction. An odd
// first byte damages a Neovim fixture and checks that reconstruction either
// fails without a snapshot or returns a state within the limits. Neovim is
// not involved; its recorded corpus stays the compatibility authority.
func FuzzReconstruct(f *testing.F) {
	fixtures := damageableFixtures(f)

	f.Add([]byte{0})
	f.Add([]byte{0, 5, 0, 0, 1, 2, 3, 4, 1, 0, 2, 3, 1, 1, 4, 0, 5, 3, 2, 1})
	f.Add([]byte{2, 12, 0, 1, 1, 2, 3, 3, 0, 5, 5, 5, 2, 1, 0, 4, 4, 4, 3, 3, 9, 9, 7, 1})
	f.Add([]byte{1, 0, 0, 1, 2, 3})
	f.Add([]byte{3, 4, 0, 50, 7, 90, 1, 0, 255})

	f.Fuzz(func(t *testing.T, data []byte) {
		if len(data) == 0 || len(data) > fuzzInputBytes {
			t.Skip()
		}

		s := &script{data: data[1:]}
		if data[0]%2 == 0 {
			checkModel(t, s)
		} else {
			checkDamaged(t, s, fixtures)
		}
	})
}

// script turns fuzz bytes into choices. An exhausted script chooses zero,
// so every input describes a complete history.
type script struct {
	data []byte
}

func (s *script) choose(n int) int {
	if len(s.data) == 0 || n <= 1 {
		return 0
	}
	b := s.data[0]
	s.data = s.data[1:]
	return int(b) % n
}

// Lines come from a tiny alphabet, so states repeat lines and share
// prefixes and suffixes, and some lines are empty.
var fuzzAlphabet = []string{"a", "b", "", "a", "longer line", "b"}

func (s *script) state() []string {
	lines := make([]string, 1+s.choose(fuzzMaxLines))
	for i := range lines {
		lines[i] = fuzzAlphabet[s.choose(len(fuzzAlphabet))]
	}
	return lines
}

// model is a generated history: the text of every node, the tree, the
// reference, and for each change the states its entries pass through in
// the direction replay applies them.
type model struct {
	states    [][]string // by node ID; 0 is the root
	parent    []int
	reference int
	steps     [][][]string // by node ID: the states one change passes through
	undo      []byte
	base      []string
}

func buildModel(s *script) model {
	count := 1 + s.choose(fuzzNodes)
	m := model{parent: make([]int, count+1), states: make([][]string, count+1), steps: make([][][]string, count+1)}
	children := make([][]int, count+1)
	for id := range m.states {
		m.states[id] = s.state()
		if id > 0 {
			m.parent[id] = s.choose(id)
			children[m.parent[id]] = append(children[m.parent[id]], id)
		}
	}

	// The preferred path follows first children from the root. The
	// reference is somewhere on it: at its leaf with no redo pending, or
	// above a pending redo.
	path := []int{0}
	for p := 0; len(children[p]) > 0; p = children[p][0] {
		path = append(path, children[p][0])
	}
	at := s.choose(len(path))
	m.reference = path[at]
	newest, redo := int32(path[len(path)-1]), int32(0)
	if at < len(path)-1 {
		redo = int32(path[at+1])
	}

	onReferencePath := make([]bool, count+1)
	for n := m.reference; n != 0; n = m.parent[n] {
		onReferencePath[n] = true
	}

	nodes := make([]wireNode, count)
	for id := 1; id <= count; id++ {
		siblings := children[m.parent[id]]
		i := slices.Index(siblings, id)
		n := wireNode{id: int32(id), parent: int32(m.parent[id])}
		if len(children[id]) > 0 {
			n.child = int32(children[id][0])
		}
		if i > 0 {
			n.previous = int32(siblings[i-1])
		}
		if i < len(siblings)-1 {
			n.next = int32(siblings[i+1])
		}

		// A change on the reference path faces undo: it turns the node's
		// text into its parent's. Any other change faces redo.
		from, to := m.states[m.parent[id]], m.states[id]
		if onReferencePath[id] {
			from, to = to, from
		}
		steps := [][]string{from}
		for range s.choose(fuzzMaxBetween + 1) {
			steps = append(steps, s.state())
		}
		steps = append(steps, to)
		m.steps[id] = steps

		for k := 1; k < len(steps); k++ {
			if e, ok := entryBetween(steps[k-1], steps[k], s.choose(2) == 1); ok {
				n.entries = append(n.entries, e)
			}
		}
		nodes[id-1] = n
	}

	oldest := int32(0)
	if len(children[0]) > 0 {
		oldest = int32(children[0][0])
	}
	m.base = m.states[m.reference]
	m.undo = withReference(graphBytes(nodes, oldest, newest, redo, int32(count), int32(count)), m.base)
	return m
}

// entryBetween is one text entry that turns from into to: it keeps the
// common first and last lines and replaces the rest. With whole set, a
// change to a single empty line instead deletes every line, which replay
// must turn back into Neovim's one empty line. Equal states need no entry.
func entryBetween(from, to []string, whole bool) (wireEntry, bool) {
	if slices.Equal(from, to) {
		return wireEntry{}, false
	}
	if whole && slices.Equal(to, []string{""}) {
		return wireEntry{top: 0, bottom: 0}, true
	}

	prefix := 0
	for prefix < min(len(from), len(to)) && from[prefix] == to[prefix] {
		prefix++
	}
	suffix := 0
	for suffix < min(len(from), len(to))-prefix && from[len(from)-1-suffix] == to[len(to)-1-suffix] {
		suffix++
	}

	return wireEntry{
		top:    int32(prefix),
		bottom: int32(len(from) - suffix + 1),
		lines:  slices.Clone(to[prefix : len(to)-suffix]),
	}, true
}

// peakLines is the most lines any state on the replay path to target holds,
// the reference included: up from the reference to the shared ancestor,
// then down to the target.
func (m model) peakLines(target int) int {
	isAncestor := make([]bool, len(m.states))
	for n := target; ; n = m.parent[n] {
		isAncestor[n] = true
		if n == 0 {
			break
		}
	}

	peak := len(m.base)
	visit := func(steps [][]string) {
		for _, state := range steps {
			peak = max(peak, len(state))
		}
	}

	n := m.reference
	for !isAncestor[n] {
		visit(m.steps[n])
		n = m.parent[n]
	}
	for d := target; d != n; d = m.parent[d] {
		visit(m.steps[d])
	}
	return peak
}

func checkModel(t *testing.T, s *script) {
	m := buildModel(s)
	undo := bytes.Clone(m.undo)
	base := slices.Clone(m.base)

	lim := fuzzLimits()
	file, err := undofile.Decode(t.Context(), "model", bytes.NewReader(m.undo), lim)
	if err != nil {
		t.Fatal(err)
	}
	h, err := history.New(t.Context(), file, lim)
	if err != nil {
		t.Fatal(err)
	}
	r := bind(t, file, h, m.base, lim)

	// Visit nodes in a script-chosen order, with repeats. Every snapshot
	// must match the model, and none may change as later ones are built.
	visits := make([]int, 1+s.choose(fuzzVisits))
	for i := range visits {
		visits[i] = s.choose(len(m.states))
	}
	snapshots := make([]*history.Snapshot, len(visits))
	for i, id := range visits {
		ref, err := h.Lookup(history.NodeID(id))
		if err != nil {
			t.Fatal(err)
		}
		if snapshots[i], err = r.Reconstruct(t.Context(), ref); err != nil {
			t.Fatalf("node %d: %v", id, err)
		}
		if got := snapshots[i].Lines(); !slices.Equal(got, m.states[id]) {
			t.Fatalf("node %d = %q, want %q", id, got, m.states[id])
		}
	}
	for i, id := range visits {
		if got := snapshots[i].Lines(); !slices.Equal(got, m.states[id]) {
			t.Fatalf("node %d changed to %q after later reconstructions", id, got)
		}
	}

	// A tighter state-line limit, bound after decoding, fails exactly the
	// targets whose replay passes through a larger state, and fails them
	// without a snapshot.
	tight := lim
	tight.StateLines = max(len(m.base), 1+s.choose(fuzzMaxLines+1))
	r = bind(t, file, h, m.base, tight)
	target := visits[len(visits)-1]
	ref, err := h.Lookup(history.NodeID(target))
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := r.Reconstruct(t.Context(), ref)
	var problem *undofile.InputError
	switch fits := m.peakLines(target) <= tight.StateLines; {
	case fits && err != nil:
		t.Fatalf("node %d failed under a %d-line limit it fits: %v", target, tight.StateLines, err)
	case !fits && (snapshot != nil || !errors.As(err, &problem) || problem.Kind != undofile.Limit):
		t.Fatalf("node %d under a %d-line limit: snapshot %v, error %v; want a limit failure", target, tight.StateLines, snapshot, err)
	}

	cancelled, cancel := context.WithCancel(t.Context())
	cancel()
	if snapshot, err := r.Reconstruct(cancelled, ref); snapshot != nil || !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled reconstruction: snapshot %v, error %v", snapshot, err)
	}

	if !bytes.Equal(m.undo, undo) || !slices.Equal(m.base, base) {
		t.Fatal("reconstruction changed its input")
	}
}

// bind verifies a base against the history's own decoded file and binds it
// under lim. Binding under tighter limits than decoding used makes replay
// enforce them without the decoder rejecting the file first.
func bind(t *testing.T, file *undofile.DecodedFile, h *history.History, base []string, lim limits.Limits) *history.Reconstructor {
	t.Helper()

	verified, err := undofile.VerifyBase(t.Context(), file, "model", base, lim)
	if err != nil {
		t.Fatal(err)
	}
	r, err := history.Bind(h, verified, lim)
	if err != nil {
		t.Fatal(err)
	}

	return r
}

// damaged is a Neovim fixture with the reference text its base verifies.
type damaged struct {
	undo []byte
	base []string
}

func damageableFixtures(tb testing.TB) []damaged {
	tb.Helper()

	paths, err := filepath.Glob("../../testdata/undo/*/oracle.json")
	if err != nil || len(paths) == 0 {
		tb.Fatalf("fixture inventory: %v", err)
	}

	var out []damaged
	for _, path := range paths {
		raw, err := os.ReadFile(path)
		if err != nil {
			tb.Fatal(err)
		}
		var oracle oracleFile
		if err := json.Unmarshal(raw, &oracle); err != nil {
			tb.Fatal(err)
		}
		undo, err := os.ReadFile(filepath.Join(filepath.Dir(path), "history.undo"))
		if err != nil {
			tb.Fatal(err)
		}

		var base []string
		for _, encoded := range oracle.Loaded.Anchor.LinesHex {
			line, err := hex.DecodeString(encoded)
			if err != nil {
				tb.Fatal(err)
			}
			base = append(base, string(line))
		}
		out = append(out, damaged{undo: undo, base: base})
	}

	return out
}

func checkDamaged(t *testing.T, s *script, fixtures []damaged) {
	f := fixtures[s.choose(len(fixtures))]
	undo := bytes.Clone(f.undo)
	for range s.choose(fuzzMutations + 1) {
		at := (s.choose(256)<<8 | s.choose(256)) % len(undo)
		undo[at] = byte(s.choose(256))
	}
	damagedCopy := bytes.Clone(undo)
	base := slices.Clone(f.base)

	lim := fuzzLimits()
	file, err := undofile.Decode(t.Context(), "damaged", bytes.NewReader(undo), lim)
	if err != nil {
		return
	}
	h, err := history.New(t.Context(), file, lim)
	if err != nil {
		return
	}
	verified, err := undofile.VerifyBase(t.Context(), file, "base", f.base, lim)
	if err != nil {
		return
	}
	r, err := history.Bind(h, verified, lim)
	if err != nil {
		return
	}

	for i := range h.Count() {
		ref, err := h.At(i)
		if err != nil {
			t.Fatal(err)
		}
		snapshot, err := r.Reconstruct(t.Context(), ref)
		switch {
		case err != nil && snapshot != nil:
			t.Fatalf("node %d failed but returned a snapshot", i)
		case err == nil && (snapshot.Len() < 1 || snapshot.Len() > lim.StateLines):
			t.Fatalf("node %d has %d lines", i, snapshot.Len())
		}
	}

	if !bytes.Equal(undo, damagedCopy) || !slices.Equal(f.base, base) {
		t.Fatal("reconstruction changed its input")
	}
}
