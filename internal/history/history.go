// Package history validates decoded relationships and owns immutable navigation
// indexes. Graph validity alone does not establish that text can be replayed.
package history

import (
	"context"
	"errors"
	"fmt"

	"github.com/nuggocto/xunhen/internal/limits"
	"github.com/nuggocto/xunhen/internal/undofile"
)

// NodeID is a selector within one history. Positive values preserve the
// producer's sequence numbers; zero identifies the synthetic retained root.
type NodeID int32

// NodeRef binds a selector to the history that validated it.
type NodeRef struct {
	owner *History
	index int
}

// SameHistory reports whether both references came from one validated
// history. Zero references belong to no history.
func (r NodeRef) SameHistory(other NodeRef) bool {
	return r.owner != nil && r.owner == other.owner
}

// NodeInfo is a value copy. Root has no event time or save metadata.
// Zero child/sibling IDs mean absent, while Parent == 0 names the retained root.
type NodeInfo struct {
	ID, Parent                   NodeID
	PreferredChild               NodeID
	NextSibling, PreviousSibling NodeID
	HasEvent                     bool
	Time                         int64
	Save                         undofile.SaveNumber
	Flags                        uint16
	TextEntries, Extmarks        int
}

type node struct {
	info        NodeInfo
	recordIndex int
}

// History is immutable after New. A zero value is not a validated history.
type History struct {
	file      *undofile.DecodedFile
	metadata  undofile.Metadata
	nodes     []node
	byID      map[NodeID]int
	order     []int
	reference int
	valid     bool
}

// New checks identities, references, sibling lists, ancestry, connectivity,
// and the persisted reference position in O(nodes) bounded iterative passes.
// The node limit bounds each pass, so cancellation is checked between them.
func New(ctx context.Context, file *undofile.DecodedFile, lim limits.Limits) (*History, error) {
	if err := lim.Validate(); err != nil {
		return nil, err
	}

	meta, ok := file.Metadata()
	if !ok {
		return nil, errors.New("history requires a complete decoded file")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	h := &History{file: file, metadata: meta}
	if meta.HeaderCount > lim.Nodes {
		return nil, h.failure(undofile.Limit, undofile.Record{}, "history nodes", "node limit exceeded")
	}

	h.nodes = make([]node, 1, meta.HeaderCount+1)
	h.nodes[0] = node{
		info:        NodeInfo{ID: 0, PreferredChild: NodeID(meta.OldestRoot)},
		recordIndex: -1,
	}
	h.byID = make(map[NodeID]int, meta.HeaderCount+1)
	h.byID[0] = 0

	for _, pass := range []func() error{h.indexRecords, h.validateLinks, h.walk, h.bindReference} {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if err := pass(); err != nil {
			return nil, err
		}
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	h.valid = true
	return h, nil
}

func (h *History) indexRecords() error {
	for i := range h.metadata.HeaderCount {
		record, ok := h.file.Record(i)
		if !ok {
			return errors.New("decoded record inventory is incomplete")
		}

		info := record.Info()
		id := NodeID(info.Sequence)
		if _, exists := h.byID[id]; exists {
			return h.invalid(record, "sequence", "duplicate identity")
		}
		if info.Sequence > h.metadata.LastSequence {
			return h.invalid(record, "sequence", "identity exceeds last allocated sequence")
		}
		if info.Save.Known && h.metadata.LastSave.Known && info.Save.Value > h.metadata.LastSave.Value {
			return h.invalid(record, "save number", "save exceeds last recorded save")
		}

		h.byID[id] = len(h.nodes)
		h.nodes = append(h.nodes, node{
			recordIndex: i,
			info: NodeInfo{
				ID:              id,
				Parent:          NodeID(info.Parent),
				PreferredChild:  NodeID(info.PreferredChild),
				NextSibling:     NodeID(info.NextSibling),
				PreviousSibling: NodeID(info.PreviousSibling),
				HasEvent:        true,
				Time:            info.Time,
				Save:            info.Save,
				Flags:           info.Flags,
				TextEntries:     record.EntryCount(),
				Extmarks:        record.ExtmarkCount(),
			},
		})
	}

	return nil
}

func (h *History) failure(kind undofile.ErrorKind, r undofile.Record, field, detail string) error {
	info := r.Info()
	offset := info.Offset
	if info.Sequence == 0 {
		offset = -1
	}

	return &undofile.InputError{
		Kind:     kind,
		Source:   h.file.Source(),
		Offset:   offset,
		Sequence: info.Sequence,
		Field:    field,
		Detail:   detail,
	}
}

func (h *History) invalid(r undofile.Record, field, detail string) error {
	return h.failure(undofile.Invalid, r, field, detail)
}

func (h *History) record(n node) undofile.Record {
	// The decoded file owns large cursor/mark and edit metadata. Navigation
	// stores only an index rather than retaining a second copy for every node.
	// New checked each index; the root's -1 intentionally yields a zero record.
	r, _ := h.file.Record(n.recordIndex)
	return r
}

func (h *History) validateLinks() error {
	for i, n := range h.nodes {
		info := n.info
		links := [...]struct {
			name string
			id   NodeID
		}{
			{"parent", info.Parent},
			{"preferred child", info.PreferredChild},
			{"next sibling", info.NextSibling},
			{"previous sibling", info.PreviousSibling},
		}

		for _, link := range links {
			if _, exists := h.byID[link.id]; !exists {
				return h.invalid(h.record(n), link.name, fmt.Sprintf("dangling reference %d", link.id))
			}
		}

		if i > 0 {
			if info.Parent >= info.ID {
				return h.invalid(h.record(n), "parent", "parent must precede child in sequence allocation")
			}

			parent := h.nodeInfo(info.Parent)
			if info.PreviousSibling == 0 && parent.PreferredChild != info.ID {
				return h.invalid(h.record(n), "preferred child", "first sibling is not selected by its parent")
			}
		}

		if info.PreferredChild != 0 {
			child := h.nodeInfo(info.PreferredChild)
			if child.Parent != info.ID || child.PreviousSibling != 0 {
				return h.invalid(h.record(n), "preferred child", "child has another parent or an earlier sibling")
			}
		}

		if info.NextSibling != 0 {
			next := h.nodeInfo(info.NextSibling)
			if next.ID == info.ID || next.Parent != info.Parent || next.PreviousSibling != info.ID {
				return h.invalid(h.record(n), "next sibling", "siblings must share a parent and reciprocal links")
			}
		}

		if info.PreviousSibling != 0 {
			previous := h.nodeInfo(info.PreviousSibling)
			if previous.ID == info.ID || previous.Parent != info.Parent || previous.NextSibling != info.ID {
				return h.invalid(h.record(n), "previous sibling", "siblings must share a parent and reciprocal links")
			}
		}
	}

	return nil
}

func (h *History) walk() error {
	seen := make([]bool, len(h.nodes))
	stack := []int{0}

	for len(stack) != 0 {
		index := stack[len(stack)-1]
		stack = stack[:len(stack)-1]

		n := h.nodes[index]
		if seen[index] {
			return h.invalid(h.record(n), "relationships", "cycle or repeated child in history")
		}

		seen[index] = true
		h.order = append(h.order, index)

		if n.info.NextSibling != 0 {
			stack = append(stack, h.byID[n.info.NextSibling])
		}
		if n.info.PreferredChild != 0 {
			stack = append(stack, h.byID[n.info.PreferredChild])
		}
	}

	if len(h.order) != len(h.nodes) {
		return h.invalid(undofile.Record{}, "relationships", "disconnected retained records")
	}

	return nil
}

func (h *History) bindReference() error {
	meta := h.metadata
	for _, link := range []undofile.Sequence{meta.Newest, meta.NextRedo} {
		if _, exists := h.byID[NodeID(link)]; !exists {
			return h.invalid(undofile.Record{}, "reference position", "dangling navigation marker")
		}
	}

	if meta.TimelineSequence > meta.LastSequence {
		return h.invalid(undofile.Record{}, "timeline sequence", "position exceeds last allocated sequence")
	}

	// With pending redo, Newest may still name a different branch. Validate
	// NextRedo instead and use its parent as the reference state.
	target := NodeID(meta.Newest)
	if meta.NextRedo != 0 {
		target = NodeID(meta.NextRedo)
	} else if h.nodeInfo(target).PreferredChild != 0 {
		return h.invalid(undofile.Record{}, "newest header", "reference without a next redo must be a leaf")
	}

	for id := NodeID(0); ; {
		info := h.nodeInfo(id)
		if id == target {
			h.reference = h.byID[id]
			if meta.NextRedo != 0 {
				h.reference = h.byID[info.Parent]
			}
			return nil
		}
		if info.PreferredChild == 0 {
			break
		}

		id = info.PreferredChild
	}

	if meta.NextRedo != 0 {
		return h.invalid(undofile.Record{}, "next redo", "marker is outside the preferred path")
	}

	return h.invalid(undofile.Record{}, "newest header", "reference is outside the preferred path")
}

// nodeInfo reads a node by an ID that validation has already resolved.
func (h *History) nodeInfo(id NodeID) NodeInfo {
	return h.nodes[h.byID[id]].info
}

// parent returns the node index of a validated node's parent.
func (h *History) parent(index int) int {
	return h.byID[h.nodes[index].info.Parent]
}

func (h *History) ready() error {
	if h == nil || !h.valid {
		return errors.New("uninitialized history")
	}

	return nil
}

// Metadata returns a value copy of the reference metadata.
func (h *History) Metadata() (undofile.Metadata, error) {
	if err := h.ready(); err != nil {
		return undofile.Metadata{}, err
	}

	return h.metadata, nil
}

// Count includes the retained root. Uninitialized histories have no nodes.
func (h *History) Count() int {
	if h == nil || !h.valid {
		return 0
	}

	return len(h.nodes)
}

// Lookup binds a selector to this history. IDs are stable for unchanged input.
func (h *History) Lookup(id NodeID) (NodeRef, error) {
	if err := h.ready(); err != nil {
		return NodeRef{}, err
	}

	index, ok := h.byID[id]
	if !ok {
		return NodeRef{}, fmt.Errorf("unknown node %d", id)
	}

	return NodeRef{owner: h, index: index}, nil
}

// At visits retained states in recorded branch order, with the preferred child
// before its alternate siblings. It does not sort by timestamps or sequences.
func (h *History) At(index int) (NodeRef, error) {
	if err := h.ready(); err != nil {
		return NodeRef{}, err
	}
	if index < 0 || index >= len(h.order) {
		return NodeRef{}, errors.New("node index out of range")
	}

	return NodeRef{owner: h, index: h.order[index]}, nil
}

// Reference identifies the buffer state at undo-file write time, without
// asserting that a matching base was supplied or that replay will succeed.
func (h *History) Reference() (NodeRef, error) {
	if err := h.ready(); err != nil {
		return NodeRef{}, err
	}

	return NodeRef{owner: h, index: h.reference}, nil
}

func (h *History) check(ref NodeRef) error {
	if err := h.ready(); err != nil {
		return err
	}
	if ref.owner != h || ref.index < 0 || ref.index >= len(h.nodes) {
		return errors.New("node reference does not belong to this history")
	}

	return nil
}

// Info rejects zero and foreign-history references.
func (h *History) Info(ref NodeRef) (NodeInfo, error) {
	if err := h.check(ref); err != nil {
		return NodeInfo{}, err
	}

	return h.nodes[ref.index].info, nil
}
