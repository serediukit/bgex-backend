package ttr

import (
	"context"
	"fmt"
	"sync"
)

// MapProvider resolves a pinned (map_id, version) to a parsed, validated Map.
// Engine depends on this interface, not on any concrete storage — production
// wires it to a MapCache over a database-backed MapLoader; engine unit tests
// use NewStaticMapProvider instead.
type MapProvider interface {
	Get(ctx context.Context, mapID string, version int32) (*Map, error)
}

// MapLoader fetches a raw published map document by (map_id, version).
// Satisfied by MapRepo.
type MapLoader interface {
	LoadDoc(ctx context.Context, mapID string, version int32) ([]byte, error)
}

// mapCacheKey identifies one immutable (map_id, version) document.
type mapCacheKey struct {
	mapID   string
	version int32
}

// MapCache is a mutex-guarded in-process cache over a MapLoader. A lobby
// pins (map_id, version) at Start and that pair never changes meaning once
// published, so cache entries never need invalidation.
type MapCache struct {
	loader MapLoader

	mu      sync.Mutex
	entries map[mapCacheKey]*Map
}

// NewMapCache returns a MapCache backed by loader.
func NewMapCache(loader MapLoader) *MapCache {
	return &MapCache{loader: loader, entries: make(map[mapCacheKey]*Map)}
}

// Get resolves (mapID, version) to a parsed, validated Map, parsing on first
// access and caching the result thereafter. Safe for concurrent use.
func (c *MapCache) Get(ctx context.Context, mapID string, version int32) (*Map, error) {
	key := mapCacheKey{mapID: mapID, version: version}

	c.mu.Lock()
	m, ok := c.entries[key]
	c.mu.Unlock()
	if ok {
		return m, nil
	}

	doc, err := c.loader.LoadDoc(ctx, mapID, version)
	if err != nil {
		return nil, fmt.Errorf("load map doc %s@%d: %w", mapID, version, err)
	}
	m, err = ParseMap(doc)
	if err != nil {
		return nil, fmt.Errorf("parse map doc %s@%d: %w", mapID, version, err)
	}

	c.mu.Lock()
	// Versions are immutable, so a concurrent winner's entry is equal to
	// ours; keep whichever is already cached to avoid pointer churn.
	if existing, raced := c.entries[key]; raced {
		m = existing
	} else {
		c.entries[key] = m
	}
	c.mu.Unlock()

	return m, nil
}

// staticMapProvider always resolves to a single fixed, pre-parsed Map.
type staticMapProvider struct {
	m       *Map
	mapID   string
	version int32
}

// NewStaticMapProvider returns a MapProvider that resolves exactly (mapID,
// version) to m and rejects everything else. For engine unit tests that
// need no database.
func NewStaticMapProvider(m *Map, mapID string, version int32) MapProvider {
	return &staticMapProvider{m: m, mapID: mapID, version: version}
}

// Get returns p.m if (mapID, version) matches, or ErrMapVersionNotFound.
func (p *staticMapProvider) Get(_ context.Context, mapID string, version int32) (*Map, error) {
	if mapID != p.mapID || version != p.version {
		return nil, fmt.Errorf("%w: %s@%d (static provider only has %s@%d)", ErrMapVersionNotFound, mapID, version, p.mapID, p.version)
	}
	return p.m, nil
}
