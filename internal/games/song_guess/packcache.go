package song_guess

import (
	"container/list"
	"context"
	"fmt"
	"sync"
)

// PackProvider resolves a pinned (pack_id, version) to a parsed, validated
// Pack. Engine depends on this interface, not on any concrete storage —
// production wires it to a PackCache over a database-backed PackLoader;
// engine unit tests can supply their own fixed-pack implementation instead.
//
// Get must be warmed with a real request ctx before a caller opens any
// FOR UPDATE transaction. engine.Apply/View carry no ctx (platform
// constraint, Req 39), so a Session's Apply path must call Get once,
// outside the transaction, with the real ctx — never rely on Engine's own
// ctx-less path, which can only ever fall back to context.Background().
// Warming inside the transaction would acquire a second pool connection
// while holding song_guess.game_states' row lock: the proven TTR M1
// self-deadlock (see root CLAUDE.md "Hard-won gotchas" and Req 40). Step 12's
// Session.Apply is what actually upholds this ordering; this comment is not
// itself an enforcement mechanism.
type PackProvider interface {
	Get(ctx context.Context, packID string, version int32) (*Pack, error)
}

// PackLoader fetches a raw published pack document by (pack_id, version).
// Satisfied by PackRepo.
type PackLoader interface {
	LoadDoc(ctx context.Context, packID string, version int32) ([]byte, error)
}

// packCacheKey identifies one immutable (pack_id, version) document.
type packCacheKey struct {
	packID  string
	version int32
}

// defaultPackCacheCapacity bounds the number of parsed packs PackCache
// retains at once (see the type doc comment for why a bound is needed at
// all). Packs are capped at 512 KiB on disk (Req 37) and a parsed *Pack is
// the same order of magnitude in memory, so 256 entries bounds this cache's
// worst-case footprint to roughly a couple hundred MB — comfortably within
// a server process's budget — while remaining far larger than the number of
// distinct packs any realistic set of concurrently active lobbies pins at
// once, so normal play essentially never evicts a warm entry.
const defaultPackCacheCapacity = 256

// packCacheEntry is one list.Element's payload: the key alongside the
// parsed Pack, so evicting the list's back element can also delete the
// matching map entry without a second lookup.
type packCacheEntry struct {
	key  packCacheKey
	pack *Pack
}

// PackCache is a mutex-guarded, capacity-bounded in-process LRU cache over a
// PackLoader.
//
// A lobby pins (pack_id, version) at Start and, per Req 31, a published
// version's document can never change afterward — saving over one forks a
// new draft version instead — so a cached entry never needs invalidation:
// evicting one is always safe, since re-fetching it can only ever reproduce
// the exact same parse.
//
// That safety is exactly why a bound is still required rather than letting
// entries accumulate forever, though: this feature's whole premise (Q1) is
// that any user may author and publish a pack, unlike TTR's admin-gated
// maps, so the set of distinct (pack_id, version) pairs a long-running
// process might eventually be asked to resolve is unbounded and outside
// operator control. Without a cap, browsing an ever-growing public pack
// list would retain every pack ever parsed for the lifetime of the process
// — an authenticated slow-memory-growth lever. The LRU bound turns that
// into a bounded footprint at the cost of an occasional re-parse for a pack
// that has fallen out of the active working set, never a correctness
// concern.
type PackCache struct {
	loader   PackLoader
	capacity int

	mu      sync.Mutex
	entries map[packCacheKey]*list.Element // Element.Value is *packCacheEntry
	order   *list.List                     // front = most recently used, back = least
}

// NewPackCache returns a PackCache backed by loader, bounded at
// defaultPackCacheCapacity entries.
func NewPackCache(loader PackLoader) *PackCache {
	return NewPackCacheWithCapacity(loader, defaultPackCacheCapacity)
}

// NewPackCacheWithCapacity returns a PackCache backed by loader, bounded at
// capacity entries (at least 1). Exposed separately from NewPackCache so
// tests can exercise eviction without waiting for defaultPackCacheCapacity
// distinct packs.
func NewPackCacheWithCapacity(loader PackLoader, capacity int) *PackCache {
	if capacity < 1 {
		capacity = 1
	}
	return &PackCache{
		loader:   loader,
		capacity: capacity,
		entries:  make(map[packCacheKey]*list.Element),
		order:    list.New(),
	}
}

// Get resolves (packID, version) to a parsed, validated Pack, parsing on
// first access and caching the result thereafter, up to the cache's
// capacity. Safe for concurrent use.
//
// See the PackProvider doc comment above: callers must supply a real
// request ctx and must call this before opening any FOR UPDATE transaction.
func (c *PackCache) Get(ctx context.Context, packID string, version int32) (*Pack, error) {
	key := packCacheKey{packID: packID, version: version}

	if pack, ok := c.lookup(key); ok {
		return pack, nil
	}

	doc, err := c.loader.LoadDoc(ctx, packID, version)
	if err != nil {
		return nil, fmt.Errorf("load pack doc %s@%d: %w", packID, version, err)
	}

	// ParsePack's contract (see packloader.go) is that callers must check
	// the returned *Pack, not a bare err != nil: a non-nil Pack alongside a
	// non-nil err means parsing succeeded with warnings only. A published
	// pack version is guaranteed fatal-issue-free (Publish re-validates and
	// only ever flips validated=TRUE), but it may still carry warnings, so
	// this cache must not treat that combination as a miss to avoid
	// re-parsing on every single Get.
	pack, perr := ParsePack(doc)
	if pack == nil {
		return nil, fmt.Errorf("parse pack doc %s@%d: %w", packID, version, perr)
	}

	return c.insert(key, pack), nil
}

// lookup returns the cached pack for key, if any, promoting it to
// most-recently-used on a hit.
func (c *PackCache) lookup(key packCacheKey) (*Pack, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()

	el, ok := c.entries[key]
	if !ok {
		return nil, false
	}
	c.order.MoveToFront(el)
	entry, _ := el.Value.(*packCacheEntry)
	return entry.pack, true
}

// insert adds pack under key as the most-recently-used entry, evicting the
// least-recently-used entry if that pushes the cache over capacity. If a
// concurrent Get already inserted the same key first (versions are
// immutable, so the two parses are equal), insert keeps that entry instead
// of pack, to avoid pointer churn.
func (c *PackCache) insert(key packCacheKey, pack *Pack) *Pack {
	c.mu.Lock()
	defer c.mu.Unlock()

	if el, raced := c.entries[key]; raced {
		c.order.MoveToFront(el)
		entry, _ := el.Value.(*packCacheEntry)
		return entry.pack
	}

	el := c.order.PushFront(&packCacheEntry{key: key, pack: pack})
	c.entries[key] = el

	if c.order.Len() > c.capacity {
		c.evictOldestLocked()
	}
	return pack
}

// evictOldestLocked removes the least-recently-used entry. Callers must
// hold c.mu.
func (c *PackCache) evictOldestLocked() {
	oldest := c.order.Back()
	if oldest == nil {
		return
	}
	c.order.Remove(oldest)
	entry, _ := oldest.Value.(*packCacheEntry)
	delete(c.entries, entry.key)
}
