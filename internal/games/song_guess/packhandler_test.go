package song_guess

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/serediukit/bgex-backend/internal/games/song_guess/packdata"
)

func init() {
	gin.SetMode(gin.TestMode)
}

// packNoopAuth stands in for authMiddleware in these tests: PackHandler
// routing and body handling is what's under test here, not auth itself
// (covered by middleware/admin_test.go and the auth package). Named
// distinctly from ttr's noopAuth since both live in different packages but
// this repo's naming convention keeps handler-test helpers self-descriptive.
func packNoopAuth(c *gin.Context) { c.Next() }

// stubPackReader satisfies packReader for PackHandler tests.
type stubPackReader struct {
	listPublicFn    func(ctx context.Context) ([]PackSummary, error)
	listOwnedFn     func(ctx context.Context, userID uuid.UUID) ([]PackSummary, error)
	getBySlugOrIDFn func(ctx context.Context, ref string) (*PackSummary, error)
	createPackFn    func(ctx context.Context, createdBy uuid.UUID, slug, name, authorLabel string) (*PackSummary, error)
}

func (s *stubPackReader) ListPublic(ctx context.Context) ([]PackSummary, error) {
	if s.listPublicFn == nil {
		return nil, nil
	}
	return s.listPublicFn(ctx)
}

func (s *stubPackReader) ListOwned(ctx context.Context, userID uuid.UUID) ([]PackSummary, error) {
	if s.listOwnedFn == nil {
		return nil, nil
	}
	return s.listOwnedFn(ctx, userID)
}

func (s *stubPackReader) GetBySlugOrID(ctx context.Context, ref string) (*PackSummary, error) {
	return s.getBySlugOrIDFn(ctx, ref)
}

func (s *stubPackReader) CreatePack(ctx context.Context, createdBy uuid.UUID, slug, name, authorLabel string) (*PackSummary, error) {
	return s.createPackFn(ctx, createdBy, slug, name, authorLabel)
}

// newPackTestRouter mounts h under /api/v1, mirroring httpx.NewRouter's
// /api/v1 prefix, without pulling in the full composition root.
func newPackTestRouter(h *PackHandler) *gin.Engine {
	e := gin.New()
	api := e.Group("/api/v1")
	h.Register(packNoopAuth)(api)
	return e
}

// exampleHitsSummary returns the PackSummary a real PackRepo would produce
// for the "example-hits" pack seeded by migration 0012 (8 songs, 2
// languages -- Ukrainian and English -- each split 2/2 across buckets 4 and
// 5). Using the real seeded/embedded document (packdata.ExampleV1) rather
// than a hand-rolled fixture gives the leak-regression test real song
// titles/artists/blocks/video ids to fail on.
func exampleHitsSummary() PackSummary {
	version := int32(packdata.ExampleV1Version)
	return PackSummary{
		ID:                     uuid.MustParse(packdata.ExampleV1ID),
		Slug:                   packdata.ExampleV1Slug,
		Name:                   "Mixed Hits (example)",
		AuthorLabel:            "bgex",
		IsOfficial:             true,
		IsPublic:               true,
		LatestPublishedVersion: &version,
		TotalSongs:             8,
	}
}

// exampleHitsCache returns a PackCache warmed with the embedded
// example-hits document at (packdata.ExampleV1ID, packdata.ExampleV1Version)
// so PackHandler can resolve per-cell counts without a live database.
func exampleHitsCache() *PackCache {
	loader := newFakePackLoader()
	loader.set(packdata.ExampleV1ID, packdata.ExampleV1Version, packdata.ExampleV1())
	return NewPackCache(loader)
}

// exampleHitsForbiddenSubstrings enumerates every song title, artist,
// lyric block and YouTube video/song id that appears in
// packdata/example.v1.json. None of these strings may ever appear in a
// packSummaryView response — see the type's doc comment.
var exampleHitsForbiddenSubstrings = []string{
	// titles
	"Song Title", "Друга пісня", "Третя пісня", "Четверта пісня",
	"Song Four", "Song Four B", "Bohemian Rhapsody", "Song Five B",
	// artists
	"Band", "Гурт", "Artist A", "Artist B", "Artist C", "Queen",
	// lyric blocks
	"First", "second", "third", "fourth",
	"Один", "два", "три", "чотири", "п'ять",
	"Alpha", "Bravo", "Charlie", "Delta",
	"Is this", "the real", "fantasy",
	// song ids
	"ua-song-1", "ua-song-2", "ua-song-3", "ua-song-4",
	"en-song-1", "en-song-2", "en-song-3", "queen-bohemian",
	// YouTube video ids / urls
	"sgua0001xyz", "sgua0002xyz", "sgua0003xyz",
	"sgen0001xyz", "sgen0002xyz", "sgen0003xyz",
	"fJ9rUzIMcZQ", "youtube",
}

func assertNoLeakedSongContent(t *testing.T, path, body string) {
	t.Helper()
	for _, forbidden := range exampleHitsForbiddenSubstrings {
		if strings.Contains(body, forbidden) {
			t.Errorf("%s: response body leaks song content %q\nbody: %s", path, forbidden, body)
		}
	}
}

// TestPackHandler_NoLeakedSongContent is Req 28's named regression: neither
// listPacks nor getPackSummary may ever let a song title, artist, lyric
// block or video id leave the server. It scans the marshalled response
// body rather than checking individual fields, since a field-by-field check
// cannot catch a field someone adds later.
func TestPackHandler_NoLeakedSongContent(t *testing.T) {
	summary := exampleHitsSummary()
	stub := &stubPackReader{
		listPublicFn: func(_ context.Context) ([]PackSummary, error) {
			return []PackSummary{summary}, nil
		},
		getBySlugOrIDFn: func(_ context.Context, ref string) (*PackSummary, error) {
			if ref != packdata.ExampleV1Slug {
				return nil, ErrPackNotFound
			}
			s := summary
			return &s, nil
		},
	}
	h := &PackHandler{repo: stub, cache: exampleHitsCache()}
	router := newPackTestRouter(h)

	paths := []string{
		"/api/v1/games/song-guess/packs",
		"/api/v1/games/song-guess/packs/" + packdata.ExampleV1Slug,
	}
	for _, path := range paths {
		w := httptest.NewRecorder()
		req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, path, nil)
		router.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Fatalf("%s: status = %d, body = %s", path, w.Code, w.Body.String())
		}
		assertNoLeakedSongContent(t, path, w.Body.String())
	}
}

// TestPackHandler_GetPackSummary_Counts asserts getPackSummary reports the
// correct per-(language, bucket) counts and is_even for example-hits: 8
// songs total (even), 2 languages, each with 2 songs at bucket 4 and 2 at
// bucket 5.
func TestPackHandler_GetPackSummary_Counts(t *testing.T) {
	summary := exampleHitsSummary()
	stub := &stubPackReader{
		getBySlugOrIDFn: func(_ context.Context, ref string) (*PackSummary, error) {
			if ref != packdata.ExampleV1Slug {
				return nil, ErrPackNotFound
			}
			s := summary
			return &s, nil
		},
	}
	h := &PackHandler{repo: stub, cache: exampleHitsCache()}
	router := newPackTestRouter(h)

	w := httptest.NewRecorder()
	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/api/v1/games/song-guess/packs/"+packdata.ExampleV1Slug, nil)
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}

	var got packSummaryView
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}

	if got.TotalSongs != 8 {
		t.Errorf("total_songs = %d, want 8", got.TotalSongs)
	}
	if !got.IsEven {
		t.Error("is_even = false, want true for total_songs=8")
	}
	if !got.HasPublished {
		t.Error("has_published = false, want true")
	}

	wantCounts := map[string]map[string]int{
		"Ukrainian": {"4": 2, "5": 2},
		"English":   {"4": 2, "5": 2},
	}
	if len(got.Languages) != len(wantCounts) {
		t.Fatalf("languages = %#v, want entries for %v", got.Languages, wantCounts)
	}
	for _, lang := range got.Languages {
		want, ok := wantCounts[lang.Name]
		if !ok {
			t.Errorf("unexpected language %q in response", lang.Name)
			continue
		}
		if len(lang.Buckets) != len(want) {
			t.Errorf("language %q: buckets = %v, want %v", lang.Name, lang.Buckets, want)
			continue
		}
		for bucket, wantCount := range want {
			if got := lang.Buckets[bucket]; got != wantCount {
				t.Errorf("language %q bucket %q = %d, want %d", lang.Name, bucket, got, wantCount)
			}
		}
	}
}

// TestPackHandler_GetPackSummary_NotFound asserts an unknown ref resolves
// to 404, not a 500 or a silently empty body.
func TestPackHandler_GetPackSummary_NotFound(t *testing.T) {
	stub := &stubPackReader{
		getBySlugOrIDFn: func(_ context.Context, _ string) (*PackSummary, error) {
			return nil, ErrPackNotFound
		},
	}
	h := &PackHandler{repo: stub, cache: exampleHitsCache()}
	router := newPackTestRouter(h)

	w := httptest.NewRecorder()
	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/api/v1/games/song-guess/packs/does-not-exist", nil)
	router.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404; body = %s", w.Code, w.Body.String())
	}
}

// countingPackProvider wraps a PackProvider and counts Get calls, so a test
// can assert an endpoint touched the cache exactly N times.
type countingPackProvider struct {
	inner PackProvider
	calls int
}

func (c *countingPackProvider) Get(ctx context.Context, packID string, version int32) (*Pack, error) {
	c.calls++
	return c.inner.Get(ctx, packID, version)
}

// TestPackHandler_ListPacks_DoesNotTouchCache is the M3(a) regression:
// listPacks previously called buildSummaryView per pack, which loaded and
// parsed that pack's full document via the cache on every miss — an
// in-request fan-out of up to one LoadDoc + ParsePack per published pack,
// for an endpoint with no cap on how many packs exist (Q1: any user may
// publish a pack). buildListView must answer entirely from the
// already-queried PackSummary rows, so the cache is never consulted no
// matter how many packs are listed.
func TestPackHandler_ListPacks_DoesNotTouchCache(t *testing.T) {
	// Three distinct packs (distinct ids) so mergePackSummaries's
	// dedupe-by-id doesn't collapse them — a real ListPublic result would
	// never return the same pack id twice either.
	summaries := make([]PackSummary, 3)
	for i := range summaries {
		s := exampleHitsSummary()
		s.ID = uuid.New()
		summaries[i] = s
	}
	stub := &stubPackReader{
		listPublicFn: func(_ context.Context) ([]PackSummary, error) {
			return summaries, nil
		},
	}
	spy := &countingPackProvider{inner: exampleHitsCache()}
	h := &PackHandler{repo: stub, cache: spy}
	router := newPackTestRouter(h)

	w := httptest.NewRecorder()
	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/api/v1/games/song-guess/packs", nil)
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}
	if spy.calls != 0 {
		t.Errorf("PackProvider.Get called %d times by listPacks, want 0", spy.calls)
	}

	var got []packSummaryView
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}
	if len(got) != len(summaries) {
		t.Fatalf("len(views) = %d, want %d", len(got), len(summaries))
	}
	for _, v := range got {
		if v.Languages != nil {
			t.Errorf("list row carries Languages = %#v, want nil (list rows never load a document)", v.Languages)
		}
	}
}

// TestPackHandler_CreatePack_DuplicateSlug asserts a duplicate-slug
// creation attempt surfaces as 409 conflict, mirroring
// ttr.AdminHandler.createMap's slug-uniqueness handling.
func TestPackHandler_CreatePack_DuplicateSlug(t *testing.T) {
	stub := &stubPackReader{
		createPackFn: func(_ context.Context, _ uuid.UUID, _, _, _ string) (*PackSummary, error) {
			return nil, ErrSlugTaken
		},
	}
	h := &PackHandler{repo: stub, cache: exampleHitsCache()}
	router := newPackTestRouter(h)

	body, err := json.Marshal(map[string]string{"name": "Duplicate Pack", "slug": packdata.ExampleV1Slug})
	if err != nil {
		t.Fatalf("marshal request body: %v", err)
	}

	w := httptest.NewRecorder()
	req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/api/v1/games/song-guess/packs", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(w, req)

	if w.Code != http.StatusConflict {
		t.Fatalf("status = %d, want 409; body = %s", w.Code, w.Body.String())
	}
}
