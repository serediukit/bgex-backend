package song_guess

import (
	"context"
	"sync"
	"testing"
)

// fakePackLoader is an in-memory PackLoader that counts LoadDoc calls per
// (packID, version) key, so tests can assert exactly when PackCache does
// and does not go back to the loader.
type fakePackLoader struct {
	mu    sync.Mutex
	docs  map[packCacheKey][]byte
	calls map[packCacheKey]int
}

func newFakePackLoader() *fakePackLoader {
	return &fakePackLoader{
		docs:  make(map[packCacheKey][]byte),
		calls: make(map[packCacheKey]int),
	}
}

func (f *fakePackLoader) set(packID string, version int32, doc []byte) {
	f.docs[packCacheKey{packID: packID, version: version}] = doc
}

func (f *fakePackLoader) callCount(packID string, version int32) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.calls[packCacheKey{packID: packID, version: version}]
}

func (f *fakePackLoader) LoadDoc(_ context.Context, packID string, version int32) ([]byte, error) {
	key := packCacheKey{packID: packID, version: version}

	f.mu.Lock()
	f.calls[key]++
	f.mu.Unlock()

	doc, ok := f.docs[key]
	if !ok {
		return nil, ErrPackVersionNotFound
	}
	return doc, nil
}

// validPackCacheDoc builds a minimal, fatal-issue-free pack v2 document
// with two songs sharing a bucket, so ParsePack succeeds cleanly (2 songs
// in the bucket avoids the "only one song" warning; 2 is also even,
// satisfying the total-song-count parity rule). Named distinctly from
// packloader_test.go's validPackDoc (a map[string]any fixture for
// mutate-and-marshal cases), since this one needs raw bytes keyed by
// caller-chosen song ids.
func validPackCacheDoc(songID1, songID2 string) []byte {
	return []byte(`{
		"packName": "Test Pack",
		"version": 2,
		"languages": [
			{
				"name": "en",
				"buckets": {
					"4": [
						{"id": "` + songID1 + `", "title": "T1", "artist": "A1", "line": ["a", "b", "c", "d"]},
						{"id": "` + songID2 + `", "title": "T2", "artist": "A2", "line": ["e", "f", "g", "h"]}
					]
				}
			}
		]
	}`)
}

// invalidPackCacheDoc builds a document that fails ParsePack fatally
// (unsupported version), so ParsePack returns a nil *Pack.
func invalidPackCacheDoc() []byte {
	return []byte(`{"packName": "Bad Pack", "version": 1, "languages": []}`)
}

// mustGetPack calls cache.Get and fails the test on error, returning the
// resolved pack (which may legitimately be nil if the caller expects an
// error instead — use wantGetErr for that case).
func mustGetPack(t *testing.T, cache *PackCache, ctx context.Context, packID string, version int32) *Pack {
	t.Helper()
	p, err := cache.Get(ctx, packID, version)
	if err != nil {
		t.Fatalf("Get %s@%d: unexpected error: %v", packID, version, err)
	}
	return p
}

// wantGetErr calls cache.Get and fails the test unless it returns a nil
// pack alongside a non-nil error.
func wantGetErr(t *testing.T, cache *PackCache, ctx context.Context, packID string, version int32) {
	t.Helper()
	p, err := cache.Get(ctx, packID, version)
	if err == nil {
		t.Fatalf("Get %s@%d: expected an error, got none", packID, version)
	}
	if p != nil {
		t.Fatalf("Get %s@%d: expected a nil pack alongside the error", packID, version)
	}
}

// assertLoaderCalls fails the test unless loader has been called exactly
// want times for (packID, version).
func assertLoaderCalls(t *testing.T, loader *fakePackLoader, packID string, version int32, want int) {
	t.Helper()
	if got := loader.callCount(packID, version); got != want {
		t.Fatalf("loader called %d times for %s@%d, want %d", got, packID, version, want)
	}
}

func TestPackCache_GetCachesOnHit(t *testing.T) {
	loader := newFakePackLoader()
	loader.set("pack-1", 1, validPackCacheDoc("s1", "s2"))
	cache := NewPackCache(loader)
	ctx := context.Background()

	p1 := mustGetPack(t, cache, ctx, "pack-1", 1)
	if p1 == nil {
		t.Fatal("first Get: expected non-nil pack")
	}
	assertLoaderCalls(t, loader, "pack-1", 1, 1)

	p2 := mustGetPack(t, cache, ctx, "pack-1", 1)
	if p2 != p1 {
		t.Fatal("second Get returned a different *Pack than the cached one")
	}
	// The load-bearing assertion: a cache hit must not call the loader
	// again.
	assertLoaderCalls(t, loader, "pack-1", 1, 1)
}

func TestPackCache_ParseFailureNotCached(t *testing.T) {
	loader := newFakePackLoader()
	loader.set("pack-bad", 1, invalidPackCacheDoc())
	cache := NewPackCache(loader)
	ctx := context.Background()

	wantGetErr(t, cache, ctx, "pack-bad", 1)
	assertLoaderCalls(t, loader, "pack-bad", 1, 1)

	// A poisoned cache entry would make this second call resolve without
	// touching the loader again, or would return a bogus non-nil pack. It
	// must instead retry the loader and fail again exactly the same way.
	wantGetErr(t, cache, ctx, "pack-bad", 1)
	assertLoaderCalls(t, loader, "pack-bad", 1, 2)
}

func TestPackCache_DifferentVersionsCachedIndependently(t *testing.T) {
	loader := newFakePackLoader()
	loader.set("pack-multi", 1, validPackCacheDoc("v1-s1", "v1-s2"))
	loader.set("pack-multi", 2, validPackCacheDoc("v2-s1", "v2-s2"))
	cache := NewPackCache(loader)
	ctx := context.Background()

	pv1 := mustGetPack(t, cache, ctx, "pack-multi", 1)
	pv2 := mustGetPack(t, cache, ctx, "pack-multi", 2)
	if pv1 == pv2 {
		t.Fatal("expected distinct *Pack instances for distinct versions")
	}
	assertLoaderCalls(t, loader, "pack-multi", 1, 1)
	assertLoaderCalls(t, loader, "pack-multi", 2, 1)

	assertOwnsSong(t, pv1, "v1-s1")
	assertOwnsSong(t, pv2, "v2-s1")
	assertLacksSong(t, pv1, "v2-s1")
	assertLacksSong(t, pv2, "v1-s1")

	// Re-fetching either version must still be a cache hit.
	mustGetPack(t, cache, ctx, "pack-multi", 1)
	mustGetPack(t, cache, ctx, "pack-multi", 2)
	assertLoaderCalls(t, loader, "pack-multi", 1, 1)
	assertLoaderCalls(t, loader, "pack-multi", 2, 1)
}

// TestPackCache_EvictsAtCapacity is the M3(b) regression: PackCache
// previously retained every parsed pack for the process lifetime with no
// bound at all. A capacity-2 cache asked for three distinct packs must
// evict the least-recently-used one (pack-a, touched before pack-b and
// pack-c) rather than growing to three entries.
func TestPackCache_EvictsAtCapacity(t *testing.T) {
	loader := newFakePackLoader()
	loader.set("pack-a", 1, validPackCacheDoc("a1", "a2"))
	loader.set("pack-b", 1, validPackCacheDoc("b1", "b2"))
	loader.set("pack-c", 1, validPackCacheDoc("c1", "c2"))
	cache := NewPackCacheWithCapacity(loader, 2)
	ctx := context.Background()

	mustGetPack(t, cache, ctx, "pack-a", 1)
	mustGetPack(t, cache, ctx, "pack-b", 1)
	mustGetPack(t, cache, ctx, "pack-c", 1) // over capacity: evicts pack-a (LRU)
	assertLoaderCalls(t, loader, "pack-a", 1, 1)
	assertLoaderCalls(t, loader, "pack-b", 1, 1)
	assertLoaderCalls(t, loader, "pack-c", 1, 1)

	// pack-b and pack-c fit within capacity 2 and must still be hits.
	mustGetPack(t, cache, ctx, "pack-b", 1)
	mustGetPack(t, cache, ctx, "pack-c", 1)
	assertLoaderCalls(t, loader, "pack-b", 1, 1)
	assertLoaderCalls(t, loader, "pack-c", 1, 1)

	// The load-bearing assertion: pack-a was evicted, so this Get must go
	// back to the loader (a genuine cache miss), proving eviction actually
	// happened rather than the cache silently growing past capacity.
	assertLoaderCalls(t, loader, "pack-a", 1, 1)
	_ = mustGetPack(t, cache, ctx, "pack-a", 1)
	assertLoaderCalls(t, loader, "pack-a", 1, 2)
}

// TestPackCache_EvictedEntryReloadsTransparently checks that an evicted
// entry's re-fetch is not merely "doesn't error" but returns the exact same
// content as the original parse — the property that makes eviction safe at
// all: entries are immutable (Req 31), so a re-parse can only ever
// reproduce what was evicted, never diverge from it.
func TestPackCache_EvictedEntryReloadsTransparently(t *testing.T) {
	loader := newFakePackLoader()
	loader.set("pack-a", 1, validPackCacheDoc("a1", "a2"))
	loader.set("pack-b", 1, validPackCacheDoc("b1", "b2"))
	cache := NewPackCacheWithCapacity(loader, 1)
	ctx := context.Background()

	original := mustGetPack(t, cache, ctx, "pack-a", 1)
	mustGetPack(t, cache, ctx, "pack-b", 1) // evicts pack-a (capacity 1)
	assertLoaderCalls(t, loader, "pack-a", 1, 1)

	reloaded := mustGetPack(t, cache, ctx, "pack-a", 1)
	assertLoaderCalls(t, loader, "pack-a", 1, 2)
	if reloaded == original {
		t.Fatal("reloaded pack is the same pointer as the evicted one — eviction did not actually happen")
	}
	assertOwnsSong(t, reloaded, "a1")
	assertOwnsSong(t, reloaded, "a2")
}

func assertOwnsSong(t *testing.T, p *Pack, songID string) {
	t.Helper()
	if p.SongByID(songID) == nil {
		t.Fatalf("expected pack to contain song %q", songID)
	}
}

func assertLacksSong(t *testing.T, p *Pack, songID string) {
	t.Helper()
	if p.SongByID(songID) != nil {
		t.Fatalf("expected pack NOT to contain song %q (versions must not leak each other's songs)", songID)
	}
}
