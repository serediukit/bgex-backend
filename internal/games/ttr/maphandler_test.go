package ttr

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

func init() {
	gin.SetMode(gin.TestMode)
}

// noopAuth stands in for authMiddleware in these tests: MapHandler/
// AdminHandler routing and body handling is what's under test here, not
// auth itself (covered by middleware/admin_test.go and the auth package).
func noopAuth(c *gin.Context) { c.Next() }

// --- stubMapReader: satisfies mapReader for MapHandler tests -----------

type stubMapReader struct {
	listPublishedFn   func(ctx context.Context) ([]MapSummary, error)
	getBySlugOrIDFn   func(ctx context.Context, ref string) (*MapSummary, error)
	latestPublishedFn func(ctx context.Context, mapID uuid.UUID) (int32, []byte, error)
	getVersionFn      func(ctx context.Context, mapID uuid.UUID, version int32) (string, []byte, error)
	getAssetFn        func(ctx context.Context, id uuid.UUID) (string, []byte, error)
	getAssetMetaFn    func(ctx context.Context, id uuid.UUID) (string, string, error)
}

func (s *stubMapReader) ListPublished(ctx context.Context) ([]MapSummary, error) {
	return s.listPublishedFn(ctx)
}

func (s *stubMapReader) GetBySlugOrID(ctx context.Context, ref string) (*MapSummary, error) {
	return s.getBySlugOrIDFn(ctx, ref)
}

func (s *stubMapReader) LatestPublished(ctx context.Context, mapID uuid.UUID) (int32, []byte, error) {
	return s.latestPublishedFn(ctx, mapID)
}

func (s *stubMapReader) GetVersion(ctx context.Context, mapID uuid.UUID, version int32) (string, []byte, error) {
	return s.getVersionFn(ctx, mapID, version)
}

func (s *stubMapReader) GetAsset(ctx context.Context, id uuid.UUID) (string, []byte, error) {
	return s.getAssetFn(ctx, id)
}

func (s *stubMapReader) GetAssetMeta(ctx context.Context, id uuid.UUID) (string, string, error) {
	return s.getAssetMetaFn(ctx, id)
}

// newMapTestRouter mounts h under /api/v1, mirroring httpx.NewRouter's
// /api/v1 prefix, without pulling in the full composition root.
func newMapTestRouter(h *MapHandler) *gin.Engine {
	e := gin.New()
	api := e.Group("/api/v1")
	h.Register(noopAuth)(api)
	return e
}

func newAdminTestRouter(h *AdminHandler) *gin.Engine {
	e := gin.New()
	api := e.Group("/api/v1")
	h.Register(noopAuth, noopAuth)(api)
	return e
}

func TestMapHandler_GetAsset_NotFound(t *testing.T) {
	stub := &stubMapReader{
		getAssetMetaFn: func(_ context.Context, _ uuid.UUID) (string, string, error) {
			return "", "", ErrAssetNotFound
		},
		getAssetFn: func(_ context.Context, _ uuid.UUID) (string, []byte, error) {
			t.Fatal("GetAsset should not be called when GetAssetMeta already failed")
			return "", nil, ErrAssetNotFound
		},
	}
	router := newMapTestRouter(&MapHandler{repo: stub})

	w := httptest.NewRecorder()
	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/api/v1/games/ttr/assets/"+uuid.New().String(), nil)
	router.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404; body = %s", w.Code, w.Body.String())
	}
	var body map[string]map[string]string
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal body: %v", err)
	}
	if body["error"]["code"] != "not_found" {
		t.Fatalf("error.code = %q, want not_found", body["error"]["code"])
	}
}

func TestMapHandler_GetAsset_OK_Headers(t *testing.T) {
	data := []byte{0x89, 'P', 'N', 'G', 0x0d, 0x0a, 0x1a, 0x0a, 1, 2, 3}
	sum := sha256.Sum256(data)
	sha := hex.EncodeToString(sum[:])
	stub := &stubMapReader{
		getAssetMetaFn: func(_ context.Context, _ uuid.UUID) (string, string, error) {
			return "image/png", sha, nil
		},
		getAssetFn: func(_ context.Context, _ uuid.UUID) (string, []byte, error) {
			return "image/png", data, nil
		},
	}
	router := newMapTestRouter(&MapHandler{repo: stub})

	w := httptest.NewRecorder()
	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/api/v1/games/ttr/assets/"+uuid.New().String(), nil)
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body = %s", w.Code, w.Body.String())
	}
	if ct := w.Header().Get("Content-Type"); ct != "image/png" {
		t.Fatalf("Content-Type = %q, want image/png", ct)
	}
	if cc := w.Header().Get("Cache-Control"); cc != "public, max-age=31536000, immutable" {
		t.Fatalf("Cache-Control = %q, want the immutable directive", cc)
	}
	wantETag := `"` + sha + `"`
	if et := w.Header().Get("ETag"); et != wantETag {
		t.Fatalf("ETag = %q, want %q", et, wantETag)
	}
	if !bytes.Equal(w.Body.Bytes(), data) {
		t.Fatalf("body mismatch")
	}
}

// TestMapHandler_GetAsset_NotModified also asserts GetAsset (the 4 MiB bytes
// read) is never called on a 304 path — the M6 fix: the ETag is built
// entirely from GetAssetMeta's stored sha256, so a matching If-None-Match
// must short-circuit before the handler ever reaches for the asset's bytes.
func TestMapHandler_GetAsset_NotModified(t *testing.T) {
	data := []byte{0x89, 'P', 'N', 'G', 9, 9, 9}
	sum := sha256.Sum256(data)
	sha := hex.EncodeToString(sum[:])
	getAssetCalled := false
	stub := &stubMapReader{
		getAssetMetaFn: func(_ context.Context, _ uuid.UUID) (string, string, error) {
			return "image/png", sha, nil
		},
		getAssetFn: func(_ context.Context, _ uuid.UUID) (string, []byte, error) {
			getAssetCalled = true
			return "image/png", data, nil
		},
	}
	router := newMapTestRouter(&MapHandler{repo: stub})

	etag := `"` + sha + `"`

	w := httptest.NewRecorder()
	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/api/v1/games/ttr/assets/"+uuid.New().String(), nil)
	req.Header.Set("If-None-Match", etag)
	router.ServeHTTP(w, req)

	if w.Code != http.StatusNotModified {
		t.Fatalf("status = %d, want 304; body = %s", w.Code, w.Body.String())
	}
	if w.Body.Len() != 0 {
		t.Fatalf("304 response should have an empty body, got %d bytes", w.Body.Len())
	}
	if getAssetCalled {
		t.Fatal("GetAsset (bytes read) must not be called on a 304 response")
	}
}

func TestMapHandler_ListMaps_Shape(t *testing.T) {
	mapID := uuid.New()
	v := int32(3)
	stub := &stubMapReader{
		listPublishedFn: func(_ context.Context) ([]MapSummary, error) {
			return []MapSummary{{ID: mapID, Slug: "europe", Name: "Europe", IsOfficial: true, LatestPublishedVersion: &v}}, nil
		},
	}
	router := newMapTestRouter(&MapHandler{repo: stub})

	w := httptest.NewRecorder()
	req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/api/v1/games/ttr/maps", nil)
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body = %s", w.Code, w.Body.String())
	}
	var got []map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil {
		t.Fatalf("unmarshal body: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("len(got) = %d, want 1", len(got))
	}
	if got[0]["slug"] != "europe" || got[0]["name"] != "Europe" || got[0]["id"] != mapID.String() {
		t.Fatalf("unexpected summary shape: %+v", got[0])
	}
	if got[0]["latest_published_version"] != float64(3) {
		t.Fatalf("latest_published_version = %v, want 3", got[0]["latest_published_version"])
	}
}

func TestMapHandler_GetMap_VersionPin(t *testing.T) {
	mapID := uuid.New()
	summary := &MapSummary{ID: mapID, Slug: "europe", Name: "Europe"}
	latestDoc := []byte(`{"schema_version":1,"name":"latest"}`)
	pinnedDoc := []byte(`{"schema_version":1,"name":"pinned"}`)

	stub := &stubMapReader{
		getBySlugOrIDFn: func(_ context.Context, ref string) (*MapSummary, error) {
			if ref != "europe" {
				t.Fatalf("ref = %q, want europe", ref)
			}
			return summary, nil
		},
		latestPublishedFn: func(_ context.Context, id uuid.UUID) (int32, []byte, error) {
			return 5, latestDoc, nil
		},
		getVersionFn: func(_ context.Context, id uuid.UUID, version int32) (string, []byte, error) {
			if version != 2 {
				t.Fatalf("version = %d, want 2", version)
			}
			return "published", pinnedDoc, nil
		},
	}
	router := newMapTestRouter(&MapHandler{repo: stub})

	// No ?version= -> latest published.
	w := httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/api/v1/games/ttr/maps/europe", nil))
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body = %s", w.Code, w.Body.String())
	}
	var latest map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &latest); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if latest["version"] != float64(5) {
		t.Fatalf("version = %v, want 5 (latest)", latest["version"])
	}

	// ?version=2 -> pinned.
	w2 := httptest.NewRecorder()
	router.ServeHTTP(w2, httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/api/v1/games/ttr/maps/europe?version=2", nil))
	if w2.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body = %s", w2.Code, w2.Body.String())
	}
	var pinned map[string]any
	if err := json.Unmarshal(w2.Body.Bytes(), &pinned); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if pinned["version"] != float64(2) {
		t.Fatalf("version = %v, want 2 (pinned)", pinned["version"])
	}
	doc, ok := pinned["doc"].(map[string]any)
	if !ok || doc["name"] != "pinned" {
		t.Fatalf("doc = %v, want the pinned document", pinned["doc"])
	}
}

func TestMapHandler_GetMap_VersionPin_RejectsDraft(t *testing.T) {
	mapID := uuid.New()
	summary := &MapSummary{ID: mapID, Slug: "europe", Name: "Europe"}
	stub := &stubMapReader{
		getBySlugOrIDFn: func(_ context.Context, _ string) (*MapSummary, error) { return summary, nil },
		getVersionFn: func(_ context.Context, _ uuid.UUID, _ int32) (string, []byte, error) {
			return "draft", []byte(`{}`), nil
		},
	}
	router := newMapTestRouter(&MapHandler{repo: stub})

	w := httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/api/v1/games/ttr/maps/europe?version=9", nil))
	if w.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404 for a draft version pin; body = %s", w.Code, w.Body.String())
	}
}

// --- stubMapAdminRepo: satisfies mapAdminRepo for AdminHandler tests ----

type stubMapAdminRepo struct {
	listAllFn     func(ctx context.Context) ([]MapSummary, error)
	createMapFn   func(ctx context.Context, slug, name string, createdBy uuid.UUID) (*MapSummary, error)
	getVersionFn  func(ctx context.Context, mapID uuid.UUID, version int32) (string, []byte, error)
	upsertDraftFn func(ctx context.Context, mapID uuid.UUID, doc []byte) (int32, error)
	publishFn     func(ctx context.Context, mapID uuid.UUID, version int32) error
	insertAssetFn func(ctx context.Context, mime string, data []byte, sha string, by uuid.UUID) (uuid.UUID, error)
}

func (s *stubMapAdminRepo) ListAll(ctx context.Context) ([]MapSummary, error) {
	return s.listAllFn(ctx)
}

func (s *stubMapAdminRepo) CreateMap(ctx context.Context, slug, name string, createdBy uuid.UUID) (*MapSummary, error) {
	return s.createMapFn(ctx, slug, name, createdBy)
}

func (s *stubMapAdminRepo) GetVersion(ctx context.Context, mapID uuid.UUID, version int32) (string, []byte, error) {
	return s.getVersionFn(ctx, mapID, version)
}

func (s *stubMapAdminRepo) UpsertDraft(ctx context.Context, mapID uuid.UUID, doc []byte) (int32, error) {
	return s.upsertDraftFn(ctx, mapID, doc)
}

func (s *stubMapAdminRepo) Publish(ctx context.Context, mapID uuid.UUID, version int32) error {
	return s.publishFn(ctx, mapID, version)
}

func (s *stubMapAdminRepo) InsertAsset(ctx context.Context, mime string, data []byte, sha string, by uuid.UUID) (uuid.UUID, error) {
	return s.insertAssetFn(ctx, mime, data, sha, by)
}

// TestAdminHandler_PutDraft_InvalidDoc_Details feeds a structurally broken
// map document (a route referencing an unknown city) to PUT .../draft and
// asserts the 400 response's error.details carries the offending JSON path.
// UpsertDraft must never be called — ParseMap must reject the document
// first (asserted via a repo stub that fails the test if it's invoked).
func TestAdminHandler_PutDraft_InvalidDoc_Details(t *testing.T) {
	doc := validMapDoc()
	routeAt(doc, 0)["a"] = "nonexistent-city"
	body, err := json.Marshal(doc)
	if err != nil {
		t.Fatalf("marshal doc: %v", err)
	}

	stub := &stubMapAdminRepo{
		upsertDraftFn: func(_ context.Context, _ uuid.UUID, _ []byte) (int32, error) {
			t.Fatalf("UpsertDraft must not be called for an invalid document")
			return 0, nil
		},
	}
	router := newAdminTestRouter(&AdminHandler{repo: stub})

	w := httptest.NewRecorder()
	req := httptest.NewRequestWithContext(t.Context(), http.MethodPut, "/api/v1/admin/ttr/maps/"+uuid.New().String()+"/draft", bytes.NewReader(body))
	router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body = %s", w.Code, w.Body.String())
	}

	var resp struct {
		Error struct {
			Code    string            `json:"code"`
			Message string            `json:"message"`
			Details []ValidationError `json:"details"`
		} `json:"error"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal response: %v; body = %s", err, w.Body.String())
	}
	if resp.Error.Code != "invalid_request" {
		t.Fatalf("error.code = %q, want invalid_request", resp.Error.Code)
	}
	if len(resp.Error.Details) == 0 {
		t.Fatalf("expected non-empty error.details")
	}

	const wantPath = "$.rules.routes[0].a"
	found := false
	for _, d := range resp.Error.Details {
		if d.Path == wantPath {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("details %+v does not contain path %q", resp.Error.Details, wantPath)
	}
}

// TestAdminHandler_PutDraft_ValidDoc_UpsertsAndReturnsVersion is the happy
// path counterpart: a valid document reaches UpsertDraft and its returned
// version is what the handler reports back.
func TestAdminHandler_PutDraft_ValidDoc_UpsertsAndReturnsVersion(t *testing.T) {
	doc := validMapDoc()
	body, err := json.Marshal(doc)
	if err != nil {
		t.Fatalf("marshal doc: %v", err)
	}

	var gotMapID uuid.UUID
	mapID := uuid.New()
	stub := &stubMapAdminRepo{
		upsertDraftFn: func(_ context.Context, id uuid.UUID, _ []byte) (int32, error) {
			gotMapID = id
			return 7, nil
		},
	}
	router := newAdminTestRouter(&AdminHandler{repo: stub})

	w := httptest.NewRecorder()
	req := httptest.NewRequestWithContext(t.Context(), http.MethodPut, "/api/v1/admin/ttr/maps/"+mapID.String()+"/draft", bytes.NewReader(body))
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body = %s", w.Code, w.Body.String())
	}
	if gotMapID != mapID {
		t.Fatalf("UpsertDraft called with map id %s, want %s", gotMapID, mapID)
	}
	var resp map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp["version"] != float64(7) {
		t.Fatalf("version = %v, want 7", resp["version"])
	}
}
