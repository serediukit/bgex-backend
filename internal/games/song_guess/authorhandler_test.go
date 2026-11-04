package song_guess

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/serediukit/bgex-backend/internal/httpx/middleware"
)

// --- fixtures ---

// authorEvenPackDoc returns a valid, even-count (2 songs) pack v2 document
// carrying title as its first song's title, so leak-regression tests have
// something concrete to fail on if the ownership check is ever removed.
// Named distinctly from packloader_test.go's validPackDoc (a
// map[string]any fixture) and packcache_test.go's validPackCacheDoc (a
// fixed two-song-id fixture with no caller-supplied title).
func authorEvenPackDoc(title string) []byte {
	return []byte(`{
		"packName": "Author Test Pack",
		"version": 2,
		"languages": [
			{"name": "english", "buckets": {"4": [
				{"id": "author-song-a", "title": "` + title + `", "artist": "Some Artist", "line": ["a","b","c","d"]},
				{"id": "author-song-b", "title": "Song B", "artist": "Artist B", "line": ["e","f","g","h"]}
			]}}
		]
	}`)
}

// authorOddCountPackDoc returns an otherwise well-formed pack document
// (equal-length lines, no duplicate ids) with 3 songs — odd — so ParsePack
// fatally rejects it on the parity rule named at path "totalSongs", while
// still being a candidate for the ?validate=false escape hatch.
func authorOddCountPackDoc() []byte {
	return []byte(`{
		"packName": "Odd Pack",
		"version": 2,
		"languages": [
			{"name": "english", "buckets": {"4": [
				{"id": "odd-song-1", "title": "One", "artist": "A", "line": ["a","b","c","d"]},
				{"id": "odd-song-2", "title": "Two", "artist": "B", "line": ["e","f","g","h"]},
				{"id": "odd-song-3", "title": "Three", "artist": "C", "line": ["i","j","k","l"]}
			]}}
		]
	}`)
}

// --- fake authRepo ---

// fakeVersionRow is one in-memory song_guess.pack_versions row.
type fakeVersionRow struct {
	status      string
	doc         []byte
	validated   bool
	totalSongs  int
	publishedAt *time.Time
	createdAt   time.Time
}

// fakeAuthorPackRepo is an in-memory authRepo stub for AuthorHandler
// tests, mirroring stubPackReader's role for PackHandler tests but backed
// by real maps (rather than per-call function fields) since these tests
// need multi-step read-your-writes behavior (draft fork, publish, delete).
type fakeAuthorPackRepo struct {
	mu               sync.Mutex
	packs            map[uuid.UUID]PackSummary
	slugs            map[string]uuid.UUID
	versions         map[uuid.UUID]map[int32]*fakeVersionRow
	referencedByGame map[uuid.UUID]bool
}

func newFakeAuthorPackRepo() *fakeAuthorPackRepo {
	return &fakeAuthorPackRepo{
		packs:            make(map[uuid.UUID]PackSummary),
		slugs:            make(map[string]uuid.UUID),
		versions:         make(map[uuid.UUID]map[int32]*fakeVersionRow),
		referencedByGame: make(map[uuid.UUID]bool),
	}
}

// addPack registers a pack with no versions yet, owned by createdBy.
func (f *fakeAuthorPackRepo) addPack(id uuid.UUID, slug string, createdBy uuid.UUID) {
	f.mu.Lock()
	defer f.mu.Unlock()
	owner := createdBy
	f.packs[id] = PackSummary{ID: id, Slug: slug, Name: slug, CreatedBy: &owner}
	f.slugs[slug] = id
	f.versions[id] = make(map[int32]*fakeVersionRow)
}

// addVersion seeds a specific version row directly, bypassing UpsertDraft,
// so a test can start from a known state (e.g. an already-published v1).
func (f *fakeAuthorPackRepo) addVersion(id uuid.UUID, version int32, status string, doc []byte, validated bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	now := time.Now()
	row := &fakeVersionRow{status: status, doc: doc, validated: validated, totalSongs: countWireSongs(doc), createdAt: now}
	if status == packVersionStatusPublished {
		row.publishedAt = &now
	}
	f.versions[id][version] = row
}

func (f *fakeAuthorPackRepo) GetBySlugOrID(_ context.Context, ref string) (*PackSummary, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if id, err := uuid.Parse(ref); err == nil {
		if s, ok := f.packs[id]; ok {
			cp := s
			return &cp, nil
		}
		return nil, ErrPackNotFound
	}
	id, ok := f.slugs[ref]
	if !ok {
		return nil, ErrPackNotFound
	}
	s := f.packs[id]
	return &s, nil
}

func (f *fakeAuthorPackRepo) OwnerOf(_ context.Context, packID uuid.UUID) (*uuid.UUID, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	s, ok := f.packs[packID]
	if !ok {
		return nil, ErrPackNotFound
	}
	return s.CreatedBy, nil
}

func (f *fakeAuthorPackRepo) ListVersions(_ context.Context, packID uuid.UUID) ([]PackVersionInfo, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	rows, ok := f.versions[packID]
	if !ok {
		return nil, nil
	}
	keys := make([]int32, 0, len(rows))
	for k := range rows {
		keys = append(keys, k)
	}
	for i := 1; i < len(keys); i++ {
		for j := i; j > 0 && keys[j-1] < keys[j]; j-- {
			keys[j-1], keys[j] = keys[j], keys[j-1]
		}
	}
	out := make([]PackVersionInfo, 0, len(keys))
	for _, v := range keys {
		r := rows[v]
		out = append(out, PackVersionInfo{
			Version: v, Status: r.status, Validated: r.validated,
			TotalSongs: r.totalSongs, PublishedAt: r.publishedAt, CreatedAt: r.createdAt,
		})
	}
	return out, nil
}

func (f *fakeAuthorPackRepo) GetVersionDoc(_ context.Context, packID uuid.UUID, version int32) ([]byte, string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	rows, ok := f.versions[packID]
	if !ok {
		return nil, "", ErrPackVersionNotFound
	}
	r, ok := rows[version]
	if !ok {
		return nil, "", ErrPackVersionNotFound
	}
	return r.doc, r.status, nil
}

func (f *fakeAuthorPackRepo) UpsertDraft(_ context.Context, packID uuid.UUID, doc []byte, _ string, validated bool, totalSongs int) (int32, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	rows, ok := f.versions[packID]
	if !ok {
		return 0, ErrPackNotFound
	}

	var draftVersion int32 = -1
	var maxVersion int32
	for v, r := range rows {
		if v > maxVersion {
			maxVersion = v
		}
		if r.status != packVersionStatusPublished && v > draftVersion {
			draftVersion = v
		}
	}
	if draftVersion >= 0 {
		r := rows[draftVersion]
		r.doc = doc
		r.validated = validated
		r.totalSongs = totalSongs
		return draftVersion, nil
	}

	next := maxVersion + 1
	rows[next] = &fakeVersionRow{status: "draft", doc: doc, validated: validated, totalSongs: totalSongs, createdAt: time.Now()}
	return next, nil
}

func (f *fakeAuthorPackRepo) Publish(_ context.Context, packID uuid.UUID, version int32) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	rows, ok := f.versions[packID]
	if !ok {
		return ErrPackVersionNotFound
	}
	r, ok := rows[version]
	if !ok {
		return ErrPackVersionNotFound
	}
	if r.status == packVersionStatusPublished {
		return ErrVersionPublished
	}
	r.status = packVersionStatusPublished
	r.validated = true
	now := time.Now()
	r.publishedAt = &now
	return nil
}

func (f *fakeAuthorPackRepo) DeletePack(_ context.Context, packID uuid.UUID) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, ok := f.packs[packID]; !ok {
		return ErrPackNotFound
	}
	if f.referencedByGame[packID] {
		// Simulates the composite FK from song_guess.game_states to
		// song_guess.pack_versions refusing the delete.
		return fmt.Errorf("delete pack %s: %w", packID, &pgconn.PgError{Code: "23503"})
	}
	delete(f.packs, packID)
	delete(f.versions, packID)
	return nil
}

// --- fake RoleLookup ---

// fakeRoleLookup maps user ids to a role string; an id with no entry is
// treated as an ordinary "user", matching how a real lookup would resolve
// a non-admin account.
type fakeRoleLookup map[uuid.UUID]string

func (f fakeRoleLookup) GetRole(_ context.Context, id uuid.UUID) (string, error) {
	if role, ok := f[id]; ok {
		return role, nil
	}
	return "user", nil
}

// --- fake bearer auth ---

// authTestVerifier maps opaque bearer token strings to user ids, letting
// these tests drive AuthorHandler through the real middleware.RequireAuth
// (rather than a no-op auth stub, as packhandler_test.go uses) so
// requireOwnerOrAdmin sees a real, distinct caller identity per request.
type authTestVerifier struct {
	tokens map[string]uuid.UUID
}

func (v authTestVerifier) VerifyAccessToken(_ context.Context, raw string) (uuid.UUID, error) {
	id, ok := v.tokens[raw]
	if !ok {
		return uuid.Nil, errors.New("invalid token")
	}
	return id, nil
}

// newAuthorTestRouter mounts h under /api/v1 behind real bearer auth
// (verifier), mirroring newPackTestRouter's /api/v1 prefix.
func newAuthorTestRouter(h *AuthorHandler, verifier middleware.AccessTokenVerifier) *gin.Engine {
	e := gin.New()
	api := e.Group("/api/v1")
	h.Register(middleware.RequireAuth(verifier))(api)
	return e
}

func bearerRequest(t *testing.T, method, path, token string, body []byte) *http.Request {
	t.Helper()
	var reader *bytes.Reader
	if body != nil {
		reader = bytes.NewReader(body)
	} else {
		reader = bytes.NewReader(nil)
	}
	req := httptest.NewRequestWithContext(t.Context(), method, path, reader)
	req.Header.Set("Authorization", "Bearer "+token)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	return req
}

// TestAuthorHandler_GetPackDoc_NonOwnerGets404 is Req 28's named
// regression: a non-owner, non-admin caller requesting a pack's full
// document — even one currently pinned by a live lobby (simulated here via
// referencedByGame, the same FK relationship deletePack must respect) —
// gets 404, never 403, and the response body never contains the song's
// title. See the falsification note in the verification report: dropping
// the ownership check in requireOwnerOrAdmin makes this test fail.
func TestAuthorHandler_GetPackDoc_NonOwnerGets404(t *testing.T) {
	owner := uuid.New()
	other := uuid.New()
	packID := uuid.New()

	repo := newFakeAuthorPackRepo()
	repo.addPack(packID, "pinned-pack", owner)
	repo.addVersion(packID, 1, packVersionStatusPublished, authorEvenPackDoc("Bohemian Rhapsody"), true)
	repo.referencedByGame[packID] = true

	h := &AuthorHandler{repo: repo, roles: fakeRoleLookup{}}
	router := newAuthorTestRouter(h, authTestVerifier{tokens: map[string]uuid.UUID{"other-token": other}})

	w := httptest.NewRecorder()
	req := bearerRequest(t, http.MethodGet, "/api/v1/games/song-guess/packs/pinned-pack/doc", "other-token", nil)
	router.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404; body = %s", w.Code, w.Body.String())
	}
	if strings.Contains(w.Body.String(), "Bohemian Rhapsody") {
		t.Fatalf("response leaks song title: %s", w.Body.String())
	}
}

// TestAuthorHandler_GetPackDoc_OwnerAndAdminGet200 asserts both the pack's
// creator and an admin caller can read someone-else's-owned pack's full
// document.
func TestAuthorHandler_GetPackDoc_OwnerAndAdminGet200(t *testing.T) {
	owner := uuid.New()
	admin := uuid.New()
	packID := uuid.New()

	repo := newFakeAuthorPackRepo()
	repo.addPack(packID, "owned-pack", owner)
	repo.addVersion(packID, 1, packVersionStatusPublished, authorEvenPackDoc("Song B"), true)

	h := &AuthorHandler{repo: repo, roles: fakeRoleLookup{admin: "admin"}}
	router := newAuthorTestRouter(h, authTestVerifier{tokens: map[string]uuid.UUID{
		"owner-token": owner,
		"admin-token": admin,
	}})

	for _, tok := range []string{"owner-token", "admin-token"} {
		w := httptest.NewRecorder()
		req := bearerRequest(t, http.MethodGet, "/api/v1/games/song-guess/packs/owned-pack/doc", tok, nil)
		router.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("token %q: status = %d, want 200; body = %s", tok, w.Code, w.Body.String())
		}
	}
}

// TestAuthorHandler_GetPackDoc_ZeroVersions_NotFound is F3's regression: a
// brand-new pack with zero versions must resolve an explicit ?version=N doc
// request as 404 not_found, not a 500 internal_error. Before the fix,
// GetVersionDoc's ErrPackVersionNotFound sentinel fell through
// writePackError's default case (no matching errors.Is branch) and reported
// 500; confirmed live via curl against GET
// /api/v1/games/song-guess/packs/:ref/doc?version=1.
func TestAuthorHandler_GetPackDoc_ZeroVersions_NotFound(t *testing.T) {
	owner := uuid.New()
	packID := uuid.New()

	repo := newFakeAuthorPackRepo()
	repo.addPack(packID, "brand-new-pack", owner)
	// Deliberately no addVersion call: this pack has zero versions.

	h := &AuthorHandler{repo: repo, roles: fakeRoleLookup{}}
	router := newAuthorTestRouter(h, authTestVerifier{tokens: map[string]uuid.UUID{"owner-token": owner}})

	w := httptest.NewRecorder()
	req := bearerRequest(t, http.MethodGet, "/api/v1/games/song-guess/packs/brand-new-pack/doc?version=1", "owner-token", nil)
	router.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404; body = %s", w.Code, w.Body.String())
	}
	var errBody struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &errBody); err != nil {
		t.Fatalf("unmarshal error body: %v", err)
	}
	if errBody.Error.Code != "not_found" {
		t.Fatalf("error.code = %q, want %q; body = %s", errBody.Error.Code, "not_found", w.Body.String())
	}
}

// TestAuthorHandler_PutDraft_BodyTooLarge asserts a body over the 512 KiB
// cap is rejected cleanly (400 or 413), never a 500.
func TestAuthorHandler_PutDraft_BodyTooLarge(t *testing.T) {
	owner := uuid.New()
	packID := uuid.New()
	repo := newFakeAuthorPackRepo()
	repo.addPack(packID, "big-pack", owner)

	h := &AuthorHandler{repo: repo, roles: fakeRoleLookup{}}
	router := newAuthorTestRouter(h, authTestVerifier{tokens: map[string]uuid.UUID{"owner-token": owner}})

	huge := make([]byte, 0, 600*1024+64)
	huge = append(huge, []byte(`{"packName":"`)...)
	huge = append(huge, bytes.Repeat([]byte("x"), 600*1024)...)
	huge = append(huge, []byte(`","version":2,"languages":[]}`)...)

	w := httptest.NewRecorder()
	req := bearerRequest(t, http.MethodPut, "/api/v1/games/song-guess/packs/big-pack/draft", "owner-token", huge)
	router.ServeHTTP(w, req)

	if w.Code == http.StatusInternalServerError {
		t.Fatalf("status = 500, want 400/413; body = %s", w.Body.String())
	}
	if w.Code != http.StatusBadRequest && w.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status = %d, want 400 or 413; body = %s", w.Code, w.Body.String())
	}
}

// TestAuthorHandler_PutDraft_OddCountFatalUnlessSkipped asserts an odd
// song count is fatally rejected (400, error.details naming the parity
// path) under full validation, but the identical body saves successfully
// with validated=false under ?validate=false.
func TestAuthorHandler_PutDraft_OddCountFatalUnlessSkipped(t *testing.T) {
	owner := uuid.New()
	packID := uuid.New()
	repo := newFakeAuthorPackRepo()
	repo.addPack(packID, "odd-pack", owner)

	h := &AuthorHandler{repo: repo, roles: fakeRoleLookup{}}
	router := newAuthorTestRouter(h, authTestVerifier{tokens: map[string]uuid.UUID{"owner-token": owner}})

	doc := authorOddCountPackDoc()

	w := httptest.NewRecorder()
	req := bearerRequest(t, http.MethodPut, "/api/v1/games/song-guess/packs/odd-pack/draft", "owner-token", doc)
	router.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body = %s", w.Code, w.Body.String())
	}

	var errBody struct {
		Error struct {
			Details []struct {
				Path    string `json:"path"`
				Message string `json:"message"`
			} `json:"details"`
		} `json:"error"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &errBody); err != nil {
		t.Fatalf("unmarshal error body: %v", err)
	}
	found := false
	for _, d := range errBody.Error.Details {
		if d.Path == "totalSongs" {
			found = true
		}
	}
	if !found {
		t.Fatalf("error.details = %+v, want an entry at path \"totalSongs\"", errBody.Error.Details)
	}

	w2 := httptest.NewRecorder()
	req2 := bearerRequest(t, http.MethodPut, "/api/v1/games/song-guess/packs/odd-pack/draft?validate=false", "owner-token", doc)
	router.ServeHTTP(w2, req2)
	if w2.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body = %s", w2.Code, w2.Body.String())
	}
	var okBody struct {
		Validated bool `json:"validated"`
	}
	if err := json.Unmarshal(w2.Body.Bytes(), &okBody); err != nil {
		t.Fatalf("unmarshal ok body: %v", err)
	}
	if okBody.Validated {
		t.Fatal("validated = true, want false for a ?validate=false save")
	}
}

// TestAuthorHandler_Publish_UnvalidatedDraftRejected asserts publish
// re-validates unconditionally: a draft saved via ?validate=false that
// still fails ParsePack cannot be published (400), regardless of what its
// validated column says.
func TestAuthorHandler_Publish_UnvalidatedDraftRejected(t *testing.T) {
	owner := uuid.New()
	packID := uuid.New()
	repo := newFakeAuthorPackRepo()
	repo.addPack(packID, "unvalidated-pack", owner)
	repo.addVersion(packID, 1, "draft", authorOddCountPackDoc(), false)

	h := &AuthorHandler{repo: repo, roles: fakeRoleLookup{}}
	router := newAuthorTestRouter(h, authTestVerifier{tokens: map[string]uuid.UUID{"owner-token": owner}})

	w := httptest.NewRecorder()
	req := bearerRequest(t, http.MethodPost, "/api/v1/games/song-guess/packs/unvalidated-pack/versions/1/publish", "owner-token", nil)
	router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400; body = %s", w.Code, w.Body.String())
	}
}

// TestAuthorHandler_PutDraft_ForksNewDraftOverPublished asserts saving a
// draft over an already-published version forks a new version rather than
// mutating it, and that the published version's document is semantically
// unchanged afterward. JSONB does not preserve byte-for-byte formatting,
// so the comparison is done after json.Unmarshal, never on raw bytes (root
// CLAUDE.md).
func TestAuthorHandler_PutDraft_ForksNewDraftOverPublished(t *testing.T) {
	owner := uuid.New()
	packID := uuid.New()
	repo := newFakeAuthorPackRepo()
	repo.addPack(packID, "forkable-pack", owner)
	publishedDoc := authorEvenPackDoc("Original Title")
	repo.addVersion(packID, 1, packVersionStatusPublished, publishedDoc, true)

	h := &AuthorHandler{repo: repo, roles: fakeRoleLookup{}}
	router := newAuthorTestRouter(h, authTestVerifier{tokens: map[string]uuid.UUID{"owner-token": owner}})

	newDoc := authorEvenPackDoc("Updated Title")
	w := httptest.NewRecorder()
	req := bearerRequest(t, http.MethodPut, "/api/v1/games/song-guess/packs/forkable-pack/draft", "owner-token", newDoc)
	router.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200; body = %s", w.Code, w.Body.String())
	}

	var okBody struct {
		Version int32 `json:"version"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &okBody); err != nil {
		t.Fatalf("unmarshal ok body: %v", err)
	}
	if okBody.Version != 2 {
		t.Fatalf("version = %d, want 2 (a new draft forked over the published v1)", okBody.Version)
	}

	gotDoc, status, err := repo.GetVersionDoc(t.Context(), packID, 1)
	if err != nil {
		t.Fatalf("GetVersionDoc(1): %v", err)
	}
	if status != packVersionStatusPublished {
		t.Fatalf("v1 status = %q, want %q", status, packVersionStatusPublished)
	}

	var want, got map[string]any
	if err := json.Unmarshal(publishedDoc, &want); err != nil {
		t.Fatalf("unmarshal want: %v", err)
	}
	if err := json.Unmarshal(gotDoc, &got); err != nil {
		t.Fatalf("unmarshal got: %v", err)
	}
	if !reflect.DeepEqual(want, got) {
		t.Fatalf("published v1 changed:\nwant %#v\ngot  %#v", want, got)
	}
}
