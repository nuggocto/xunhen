package main

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"math/rand/v2"
	"slices"
	"strings"

	"github.com/nuggocto/xunhen/internal/synth"
)

// recipe describes one workload. Version changes whenever the generated
// bytes would change, so recorded measurements always name the exact input.
type recipe struct {
	Name    string
	Purpose string
	Version int
	Seed    uint64
	build   func(seed uint64) (*workload, error)
}

// workload is a generated history and what reconstruction must produce.
type workload struct {
	file synth.File
	// probes are states whose exported text the generator knows by
	// construction: an oracle independent of xunhen's replay.
	probes []probe
	// compare names two nodes the runner diffs: from, then to.
	compare [2]int32
}

type probe struct {
	Node   int32  `json:"node"`
	Role   string `json:"role"`
	Lines  int    `json:"lines"`
	Bytes  int    `json:"bytes"`
	SHA256 string `json:"sha256"`
}

// digest describes a state as `show --raw --final-newline=include` writes
// it: each line followed by LF.
func digest(node int32, role string, lines []string) probe {
	h := sha256.New()
	size := 0
	for _, line := range lines {
		h.Write([]byte(line))
		h.Write([]byte{'\n'})
		size += len(line) + 1
	}
	return probe{Node: node, Role: role, Lines: len(lines), Bytes: size, SHA256: hex.EncodeToString(h.Sum(nil))}
}

var recipes = []recipe{
	{Name: "small", Version: 1, Seed: 1, Purpose: "40 changes to a 200-line file: startup overhead",
		build: func(seed uint64) (*workload, error) {
			return growTree(seed, treeShape{nodes: 40, lines: 200, maxDelete: 3, maxInsert: 4, parent: mostlyLinear(0.8)})
		}},
	{Name: "ordinary", Version: 1, Seed: 2, Purpose: "1,000 changes to a 3,000-line file with occasional branches: normal interaction",
		build: func(seed uint64) (*workload, error) {
			return growTree(seed, treeShape{nodes: 1000, lines: 3000, maxDelete: 5, maxInsert: 6, parent: mostlyLinear(0.85)})
		}},
	{Name: "deep", Version: 1, Seed: 3, Purpose: "100,000 changes in a line, then 50,000 alternatives nested one inside the next",
		build: func(seed uint64) (*workload, error) {
			return growTree(seed, treeShape{nodes: 150_000, lines: 30, maxDelete: 2, maxInsert: 2, parent: deepStaircase(100_000)})
		}},
	{Name: "wide", Version: 1, Seed: 4, Purpose: "50,000 alternative first changes to one file, then 10,000 more below them",
		build: func(seed uint64) (*workload, error) {
			return growTree(seed, treeShape{nodes: 60_000, lines: 30, maxDelete: 2, maxInsert: 2, parent: wideFan(50_000)})
		}},
	{Name: "shuffled", Version: 1, Seed: 5, Purpose: "two 4,000,000-line states holding the same unique lines in shuffled order: the costliest diff",
		build: func(seed uint64) (*workload, error) {
			left := numbered(4_000_000, "line")
			right := slices.Clone(left)
			rand.New(rand.NewPCG(seed, 0)).Shuffle(len(right), func(i, j int) { right[i], right[j] = right[j], right[i] })
			return twoStates(left, right), nil
		}},
	{Name: "repeated", Version: 1, Seed: 6, Purpose: "two 1,000,000-line states of four repeated lines: diff with few unique anchors",
		build: func(uint64) (*workload, error) {
			left, right := make([]string, 1_000_000), make([]string, 1_000_000)
			for i := range left {
				left[i], right[i] = fmt.Sprint("value ", i%4), fmt.Sprint("value ", i*7%11%4)
			}
			return twoStates(left, right), nil
		}},
	{Name: "replaced", Version: 1, Seed: 7, Purpose: "a 1,000,000-line state with every third line replaced: widespread small hunks",
		build: func(uint64) (*workload, error) {
			left := numbered(1_000_000, "line")
			right := slices.Clone(left)
			for i := 0; i < len(right); i += 3 {
				right[i] = fmt.Sprint("changed ", i)
			}
			return twoStates(left, right), nil
		}},
	{Name: "changes-limit", Version: 1, Seed: 8, Purpose: "560,000 one-line changes to a 100-line file: an undo file near the 256 MiB input limit",
		build: func(seed uint64) (*workload, error) { return lineEdits(seed, 560_000, 100), nil }},
	{Name: "entries-limit", Version: 1, Seed: 9, Purpose: "one change of 1,000,000 entries to a 4,000,000-line state: entry and state-line limits together",
		build: func(uint64) (*workload, error) { return manyEntries(1_000_000, 4_000_000), nil }},
	{Name: "lines-limit", Version: 1, Seed: 10, Purpose: "three 16 MiB lines, one of them changed: the line-length limit",
		build: func(seed uint64) (*workload, error) { return longLines(seed), nil }},
	{Name: "empty-lines", Version: 1, Seed: 11, Purpose: "one entry storing 4,000,000 empty lines: the stored-line limit with no line text, where decoding and replay must still notice cancellation",
		build: func(uint64) (*workload, error) { return emptyLines(4_000_000), nil }},
}

func findRecipe(name string) (recipe, bool) {
	for _, r := range recipes {
		if r.Name == name {
			return r, true
		}
	}
	return recipe{}, false
}

func numbered(n int, prefix string) []string {
	lines := make([]string, n)
	for i := range lines {
		lines[i] = fmt.Sprintf("%s %07d", prefix, i)
	}
	return lines
}

// twoStates is a root and a reference whose one change replaces the whole
// buffer, as synth.Chain writes it.
func twoStates(left, right []string) *workload {
	return &workload{
		file:    synth.Chain([][]string{left, right}),
		probes:  []probe{digest(0, "root", left), digest(1, "reference", right)},
		compare: [2]int32{1, 0},
	}
}

// treeShape describes a branching history. parent picks each node's parent
// among the earlier nodes.
type treeShape struct {
	nodes, lines         int
	maxDelete, maxInsert int
	parent               func(id int, rng *rand.Rand) int
}

func mostlyLinear(continuing float64) func(int, *rand.Rand) int {
	return func(id int, rng *rand.Rand) int {
		if id == 1 || rng.Float64() < continuing {
			return id - 1
		}
		return rng.IntN(id)
	}
}

// deepStaircase is a chain of length chain, followed by stairs. Each stair
// has a leaf as its first child and the next stair as its second, so every
// stair starts a branch one level deeper than the last. The tail of the
// chain is the first stair.
func deepStaircase(chain int) func(int, *rand.Rand) int {
	return func(id int, _ *rand.Rand) int {
		offset := id - chain
		switch {
		case offset <= 0:
			return id - 1
		case offset <= 2:
			return chain // the first stair's leaf, then the second stair
		case offset%2 == 1:
			return id - 1 // the leaf of the stair just added
		default:
			return id - 2 // the next stair, beside that leaf
		}
	}
}

// wideFan makes fan children of the root, then attaches the rest below them.
func wideFan(fan int) func(int, *rand.Rand) int {
	return func(id int, rng *rand.Rand) int {
		if id <= fan {
			return 0
		}
		return 1 + rng.IntN(fan)
	}
}

// growTree generates a branching history by walking the tree depth-first
// over one buffer, applying each change on the way down and undoing it on
// the way back up, the way an editor moves through undo states. Each change
// is one random edit of its parent's text. Changes on the path from the
// root to the reference are written facing undo, the rest facing redo.
func growTree(seed uint64, shape treeShape) (*workload, error) {
	rng := rand.New(rand.NewPCG(seed, 0))
	n := shape.nodes
	parent := make([]int, n+1)
	children := make([][]int, n+1)
	for id := 1; id <= n; id++ {
		parent[id] = shape.parent(id, rng)
		if parent[id] < 0 || parent[id] >= id {
			return nil, fmt.Errorf("node %d has parent %d", id, parent[id])
		}
		children[parent[id]] = append(children[parent[id]], id)
	}

	// The reference ends the preferred path, which follows first children.
	reference := 0
	for len(children[reference]) > 0 {
		reference = children[reference][0]
	}
	onPath := make([]bool, n+1)
	depth := make([]int, n+1)
	for id := reference; id != 0; id = parent[id] {
		onPath[id] = true
	}
	deepest, sideways := 0, -1
	for id := 1; id <= n; id++ {
		depth[id] = depth[parent[id]] + 1
		if depth[id] > depth[deepest] {
			deepest = id
		}
		if !onPath[id] && sideways < 0 && id > n/2 {
			sideways = id
		}
	}
	if sideways < 0 {
		sideways = n / 2
	}
	probeRoles := map[int]string{0: "root", reference: "reference", deepest: "deepest", sideways: "off the reference path", n / 3: "a third of the way"}

	buffer := make([]string, shape.lines)
	for i := range buffer {
		buffer[i] = sentence(rng, 0, i)
	}

	w := &workload{compare: [2]int32{int32(reference), int32(sideways)}}
	nodes := make([]synth.Node, n+1)
	type frame struct {
		id, next   int
		top, added int
		removed    []string
	}
	visit := func(id int) {
		if role, ok := probeRoles[id]; ok {
			w.probes = append(w.probes, digest(int32(id), role, buffer))
		}
		if id == reference {
			w.file.Reference = slices.Clone(buffer)
		}
	}

	visit(0)
	stack := []frame{{id: 0}}
	for len(stack) > 0 {
		top := &stack[len(stack)-1]
		if top.next == len(children[top.id]) {
			// Leaving a node restores its parent's text.
			buffer = slices.Replace(buffer, top.top, top.top+top.added, top.removed...)
			stack = stack[:len(stack)-1]
			continue
		}
		id := children[top.id][top.next]
		top.next++

		edit := rand.New(rand.NewPCG(seed, uint64(id)))
		at := edit.IntN(len(buffer) + 1)
		deleted := edit.IntN(min(len(buffer)-at, shape.maxDelete) + 1)
		inserted := make([]string, edit.IntN(shape.maxInsert+1))
		if len(buffer)-deleted+len(inserted) == 0 {
			inserted = make([]string, 1)
		}
		for i := range inserted {
			inserted[i] = sentence(edit, id, i)
		}
		removed := slices.Clone(buffer[at : at+deleted])
		buffer = slices.Replace(buffer, at, at+deleted, inserted...)

		entry := synth.Entry{Top: int32(at), Bottom: int32(at + deleted + 1), Lines: inserted}
		if onPath[id] {
			entry = synth.Entry{Top: int32(at), Bottom: int32(at + len(inserted) + 1), Lines: removed}
		}
		nodes[id] = synth.Node{ID: int32(id), Parent: int32(parent[id]), Time: 1_700_000_000 + int64(id), Entries: []synth.Entry{entry}}

		visit(id)
		stack = append(stack, frame{id: id, top: at, added: len(inserted), removed: removed})
	}

	for id := 0; id <= n; id++ {
		kids := children[id]
		for i, child := range kids {
			if i == 0 && id > 0 {
				nodes[id].Child = int32(child)
			}
			if i > 0 {
				nodes[child].Previous = int32(kids[i-1])
			}
			if i < len(kids)-1 {
				nodes[child].Next = int32(kids[i+1])
			}
		}
	}

	w.file.Nodes = nodes[1:]
	if len(children[0]) > 0 {
		w.file.Oldest = int32(children[0][0])
	}
	w.file.Newest = int32(reference)
	w.file.LastSequence, w.file.TimelineSequence = int32(n), int32(n)
	return w, nil
}

var vocabulary = strings.Fields("retry backoff limit window state request reply error context cancel timer queue value result branch")

// sentence is a line of code-like words, unique to its node and position.
func sentence(rng *rand.Rand, node, line int) string {
	words := make([]string, 3+rng.IntN(6))
	for i := range words {
		words[i] = vocabulary[rng.IntN(len(vocabulary))]
	}
	return fmt.Sprintf("%s // %d.%d", strings.Join(words, " "), node, line)
}

// lineEdits is a linear history of changes that each rewrite one line.
// Every change is on the reference path, so each entry faces undo and holds
// the line as it was.
func lineEdits(seed uint64, changes, lines int) *workload {
	rng := rand.New(rand.NewPCG(seed, 0))
	buffer := make([]string, lines)
	for i := range buffer {
		buffer[i] = sentence(rng, 0, i)
	}

	w := &workload{compare: [2]int32{int32(changes), 0}}
	w.probes = append(w.probes, digest(0, "root", buffer))
	nodes := make([]synth.Node, changes)
	for k := 1; k <= changes; k++ {
		at := rng.IntN(lines)
		old := buffer[at]
		buffer[at] = sentence(rng, k, at)
		nodes[k-1] = synth.Node{
			ID: int32(k), Parent: int32(k - 1), Time: 1_700_000_000 + int64(k),
			Entries: []synth.Entry{{Top: int32(at), Bottom: int32(at + 2), Lines: []string{old}}},
		}
		if k < changes {
			nodes[k-1].Child = int32(k + 1)
		}
		if k == changes/2 {
			w.probes = append(w.probes, digest(int32(k), "halfway", buffer))
		}
	}
	w.probes = append(w.probes, digest(int32(changes), "reference", buffer))

	w.file = synth.File{
		Nodes: nodes, Oldest: 1, Newest: int32(changes),
		LastSequence: int32(changes), TimelineSequence: int32(changes),
		Reference: slices.Clone(buffer),
	}
	return w
}

// manyEntries is one change whose entries each delete the first line of the
// reference state, so reconstructing the root applies all of them.
func manyEntries(entries, lines int) *workload {
	reference := numbered(lines, "line")
	node := synth.Node{ID: 1, Time: 1_700_000_001, Entries: make([]synth.Entry, entries)}
	for i := range node.Entries {
		node.Entries[i] = synth.Entry{Top: 0, Bottom: 2}
	}
	return &workload{
		file: synth.File{
			Nodes: []synth.Node{node}, Oldest: 1, Newest: 1, LastSequence: 1, TimelineSequence: 1,
			Reference: reference,
		},
		probes:  []probe{digest(0, "root", reference[entries:]), digest(1, "reference", reference)},
		compare: [2]int32{1, 0},
	}
}

// emptyLines is one change whose single entry stores n empty lines in place
// of a two-line reference, so the root is n empty lines.
func emptyLines(n int) *workload {
	reference := []string{"first reference line", "second reference line"}
	root := make([]string, n)
	return &workload{
		file: synth.File{
			Nodes: []synth.Node{{ID: 1, Time: 1_700_000_001, Entries: []synth.Entry{
				{Top: 0, Bottom: 0, Lines: root},
			}}},
			Oldest: 1, Newest: 1, LastSequence: 1, TimelineSequence: 1,
			Reference: reference,
		},
		probes:  []probe{digest(0, "root", root), digest(1, "reference", reference)},
		compare: [2]int32{1, 0},
	}
}

// longLines is a reference of three 16 MiB lines whose one change swaps the
// middle line for another of the same length.
func longLines(seed uint64) *workload {
	const size = 16 << 20
	rng := rand.New(rand.NewPCG(seed, 0))
	line := func() string {
		var b strings.Builder
		for b.Len() < size {
			b.WriteString(vocabulary[rng.IntN(len(vocabulary))])
			b.WriteByte(' ')
		}
		return b.String()[:size]
	}
	reference := []string{line(), line(), line()}
	root := []string{reference[0], line(), reference[2]}

	return &workload{
		file: synth.File{
			Nodes: []synth.Node{{ID: 1, Time: 1_700_000_001, Entries: []synth.Entry{
				{Top: 1, Bottom: 3, Lines: []string{root[1]}},
			}}},
			Oldest: 1, Newest: 1, LastSequence: 1, TimelineSequence: 1,
			Reference: reference,
		},
		probes:  []probe{digest(0, "root", root), digest(1, "reference", reference)},
		compare: [2]int32{1, 0},
	}
}
