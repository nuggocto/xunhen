package tui

import (
	"container/list"

	"github.com/nuggocto/xunhen/internal/history"
)

// cacheBudget bounds the documents the cache keeps. It holds a compared pair
// of the largest states: two 4,000,000-line documents charge 61 MiB each for
// their line arrays, and the rest covers their column indexes and the
// entries' bookkeeping. TestCacheHoldsTheLargestPair checks that arithmetic.
const cacheBudget = 128 << 20

// entryOverhead is the bookkeeping allowance charged per entry: the map
// slot, the list element, and the entry itself, rounded up.
const entryOverhead = 256

// cache keeps recently prepared documents of one session in LRU order. The
// worker owns it; nothing else touches it. An entry is an output to show
// again, never a replay checkpoint: a missing state is always reconstructed
// from the verified reference.
//
// Only the documents' own storage is charged. The line bytes belong to the
// session, which outlives every entry. Evicting drops the cache's reference
// and nothing else: documents are immutable, so a document the view still
// shows stays intact, and its memory is the view's until the view lets go.
type cache struct {
	budget, used int
	owner        *session
	entries      map[history.NodeRef]*list.Element
	order        list.List // most recently used first
}

type cacheEntry struct {
	node   history.NodeRef
	doc    *document
	charge int
}

func newCache(budget int) *cache {
	return &cache{budget: budget, entries: map[history.NodeRef]*list.Element{}}
}

// reset empties the cache and binds it to a session, so entries of an
// earlier load can never answer for a later one.
func (c *cache) reset(owner *session) {
	c.owner = owner
	c.used = 0
	c.entries = map[history.NodeRef]*list.Element{}
	c.order.Init()
}

// get returns a document of the owning session and marks it recently used.
func (c *cache) get(owner *session, node history.NodeRef) (*document, bool) {
	if owner != c.owner {
		return nil, false
	}

	element, ok := c.entries[node]
	if !ok {
		return nil, false
	}
	c.order.MoveToFront(element)

	return element.Value.(*cacheEntry).doc, true
}

// put adds a document, evicting the least recently used entries until it
// fits. A document of another session first resets the cache. A document
// larger than the whole budget is not kept.
func (c *cache) put(owner *session, node history.NodeRef, doc *document) {
	if owner != c.owner {
		c.reset(owner)
	}
	if _, ok := c.entries[node]; ok {
		return
	}

	charge := doc.charge() + entryOverhead
	if charge > c.budget {
		return
	}

	for c.used+charge > c.budget {
		oldest := c.order.Back()
		entry := oldest.Value.(*cacheEntry)
		c.order.Remove(oldest)
		delete(c.entries, entry.node)
		c.used -= entry.charge
	}

	c.entries[node] = c.order.PushFront(&cacheEntry{node: node, doc: doc, charge: charge})
	c.used += charge
}
