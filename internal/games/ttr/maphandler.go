package ttr

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/serediukit/bgex-backend/internal/httpx/response"
)

// mapReader is the subset of *MapRepo that MapHandler needs. Depending on
// this narrow interface (rather than *MapRepo directly) lets
// maphandler_test.go exercise the handlers against an in-memory stub instead
// of a live Postgres instance, while production wiring still goes through
// NewMapHandler(*MapRepo, *MapCache) — *MapRepo trivially satisfies it.
type mapReader interface {
	ListPublished(ctx context.Context) ([]MapSummary, error)
	GetBySlugOrID(ctx context.Context, ref string) (*MapSummary, error)
	LatestPublished(ctx context.Context, mapID uuid.UUID) (int32, []byte, error)
	GetVersion(ctx context.Context, mapID uuid.UUID, version int32) (status string, doc []byte, err error)
	GetAsset(ctx context.Context, id uuid.UUID) (mime string, data []byte, err error)
	GetAssetMeta(ctx context.Context, id uuid.UUID) (mime, sha256Hex string, err error)
}

// MapHandler exposes read-only TTR map and asset REST routes to players.
type MapHandler struct {
	repo  mapReader
	cache *MapCache
}

// NewMapHandler creates a MapHandler backed by repo/cache.
func NewMapHandler(repo *MapRepo, cache *MapCache) *MapHandler {
	return &MapHandler{repo: repo, cache: cache}
}

// Register mounts the public TTR map/asset routes under /games/ttr.
// authMiddleware is applied inline per route (never group.Use) per the
// codebase convention. The asset route is deliberately left unauthenticated
// so plain <image> tags work without a bearer token — its content is
// content-addressed and immutable, so there is nothing to protect.
func (h *MapHandler) Register(authMiddleware gin.HandlerFunc) func(*gin.RouterGroup) {
	return func(r *gin.RouterGroup) {
		g := r.Group("/games/ttr")
		g.GET("/maps", authMiddleware, h.listMaps)
		g.GET("/maps/:ref", authMiddleware, h.getMap)
		g.GET("/assets/:id", h.getAsset)
	}
}

// listMaps returns every map that has at least one published version.
func (h *MapHandler) listMaps(c *gin.Context) {
	summaries, err := h.repo.ListPublished(c.Request.Context())
	if err != nil {
		writeMapError(c, err)
		return
	}
	if summaries == nil {
		summaries = []MapSummary{}
	}
	response.OK(c, summaries)
}

// getMap resolves :ref (a slug or a UUID) to its MapSummary plus the
// document for its latest published version, or the version pinned by
// ?version=N — which must itself be published, since this route is
// player-facing and drafts are not meant to be visible outside admin.
func (h *MapHandler) getMap(c *gin.Context) {
	ctx := c.Request.Context()
	ref := c.Param("ref")

	summary, err := h.repo.GetBySlugOrID(ctx, ref)
	if err != nil {
		writeMapError(c, err)
		return
	}

	version, doc, err := h.resolveMapDoc(c, summary.ID)
	if err != nil {
		return // resolveMapDoc already wrote the error response
	}

	// Best-effort warm of the in-process map cache: a lobby is likely to
	// pin exactly this (map_id, version) next, so priming it here saves
	// that lobby's first Start/Apply a cold-cache DB round trip. The doc
	// was already returned to the client above regardless of outcome.
	if h.cache != nil {
		_, _ = h.cache.Get(ctx, summary.ID.String(), version)
	}

	response.OK(c, gin.H{
		"map":     summary,
		"version": version,
		"doc":     json.RawMessage(doc),
	})
}

// resolveMapDoc resolves the document getMap should return: the version
// pinned by ?version=N (which must be published), or the latest published
// version when the query param is absent. On any failure it writes the
// appropriate error response itself and returns a non-nil error as a
// "stop processing" signal to the caller.
func (h *MapHandler) resolveMapDoc(c *gin.Context, mapID uuid.UUID) (version int32, doc []byte, err error) {
	ctx := c.Request.Context()

	raw := c.Query("version")
	if raw == "" {
		version, doc, err = h.repo.LatestPublished(ctx, mapID)
		if err != nil {
			writeMapError(c, err)
			return 0, nil, err
		}
		return version, doc, nil
	}

	n, perr := strconv.ParseInt(raw, 10, 32)
	if perr != nil {
		response.Error(c, http.StatusBadRequest, response.CodeInvalidRequest, "version must be an integer")
		return 0, nil, perr
	}

	status, d, gerr := h.repo.GetVersion(ctx, mapID, int32(n))
	if gerr != nil {
		writeMapError(c, gerr)
		return 0, nil, gerr
	}
	if status != mapVersionStatusPublished {
		response.Error(c, http.StatusNotFound, response.CodeNotFound, ErrMapVersionNotFound.Error())
		return 0, nil, ErrMapVersionNotFound
	}
	return int32(n), d, nil
}

// getAsset serves a background image by id with immutable caching headers,
// answering 304 when the client's If-None-Match already matches. It is
// mounted without authMiddleware — see Register's doc comment.
//
// This is the platform's only unauthenticated route, so an attacker can
// loop requests against it at near-zero cost. The digest used for the ETag
// is fetched via GetAssetMeta (mime, sha256 only) rather than GetAsset
// (mime, bytes): the sha256 was already computed once at upload time
// (InsertAsset) and never changes for a given asset id (content-addressed,
// immutable), so there is nothing to gain by re-reading and re-hashing up
// to 4 MiB on every request. GetAsset — the full bytes read — is only
// called once the ETag comparison has determined this really is a fresh
// 200, never on a path that ends in 304.
func (h *MapHandler) getAsset(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		response.Error(c, http.StatusNotFound, response.CodeNotFound, ErrAssetNotFound.Error())
		return
	}

	mime, sha, err := h.repo.GetAssetMeta(c.Request.Context(), id)
	if err != nil {
		writeMapError(c, err)
		return
	}

	etag := `"` + sha + `"`
	c.Header("Cache-Control", "public, max-age=31536000, immutable")
	c.Header("ETag", etag)
	if c.GetHeader("If-None-Match") == etag {
		c.Status(http.StatusNotModified)
		return
	}

	_, data, err := h.repo.GetAsset(c.Request.Context(), id)
	if err != nil {
		writeMapError(c, err)
		return
	}
	c.Data(http.StatusOK, mime, data)
}

// writeMapError maps ttr map/asset domain sentinels to the HTTP error
// envelope; anything unrecognized becomes a 500.
func writeMapError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, ErrMapNotFound), errors.Is(err, ErrMapVersionNotFound), errors.Is(err, ErrAssetNotFound):
		response.Error(c, http.StatusNotFound, response.CodeNotFound, err.Error())
	default:
		response.Error(c, http.StatusInternalServerError, response.CodeInternal, "internal error")
	}
}
