package ttr

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"regexp"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/serediukit/bgex-backend/internal/httpx/middleware"
	"github.com/serediukit/bgex-backend/internal/httpx/response"
)

// mapAdminRepo is the subset of *MapRepo that AdminHandler needs. As with
// mapReader (maphandler.go), this narrow interface lets
// maphandler_test.go stub it without a live Postgres instance; production
// wiring goes through NewAdminHandler(*MapRepo, *MapCache).
type mapAdminRepo interface {
	ListAll(ctx context.Context) ([]MapSummary, error)
	CreateMap(ctx context.Context, slug, name string, createdBy uuid.UUID) (*MapSummary, error)
	GetVersion(ctx context.Context, mapID uuid.UUID, version int32) (status string, doc []byte, err error)
	UpsertDraft(ctx context.Context, mapID uuid.UUID, doc []byte) (int32, error)
	Publish(ctx context.Context, mapID uuid.UUID, version int32) error
	InsertAsset(ctx context.Context, mime string, data []byte, sha string, by uuid.UUID) (uuid.UUID, error)
}

// AdminHandler exposes the TTR map-authoring REST routes under
// /admin/ttr/*.
type AdminHandler struct {
	repo  mapAdminRepo
	cache *MapCache
}

// NewAdminHandler creates an AdminHandler backed by repo/cache.
func NewAdminHandler(repo *MapRepo, cache *MapCache) *AdminHandler {
	return &AdminHandler{repo: repo, cache: cache}
}

// Register mounts the admin TTR map routes. Every route carries both
// authMiddleware and adminMiddleware inline (never group.Use) per the
// codebase convention.
func (h *AdminHandler) Register(authMiddleware, adminMiddleware gin.HandlerFunc) func(*gin.RouterGroup) {
	return func(r *gin.RouterGroup) {
		g := r.Group("/admin/ttr")
		g.GET("/maps", authMiddleware, adminMiddleware, h.listMaps)
		g.POST("/maps", authMiddleware, adminMiddleware, h.createMap)
		g.GET("/maps/:id/versions/:version", authMiddleware, adminMiddleware, h.getVersion)
		g.PUT("/maps/:id/draft", authMiddleware, adminMiddleware, h.putDraft)
		g.POST("/maps/:id/versions/:version/publish", authMiddleware, adminMiddleware, h.publish)
		g.POST("/assets", authMiddleware, adminMiddleware, h.uploadAsset)
	}
}

// listMaps returns every map, including drafts-only ones (admin use).
func (h *AdminHandler) listMaps(c *gin.Context) {
	summaries, err := h.repo.ListAll(c.Request.Context())
	if err != nil {
		response.Error(c, http.StatusInternalServerError, response.CodeInternal, "internal error")
		return
	}
	if summaries == nil {
		summaries = []MapSummary{}
	}
	response.OK(c, summaries)
}

// slugPattern is the allowed shape for a new map's slug.
var slugPattern = regexp.MustCompile(`^[a-z0-9-]{2,40}$`)

type createMapReq struct {
	Slug string `json:"slug"`
	Name string `json:"name"`
}

// createMap inserts a new, versionless map row. slug must be unique and
// match slugPattern; a duplicate slug is reported as 409 conflict.
func (h *AdminHandler) createMap(c *gin.Context) {
	var req createMapReq
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Error(c, http.StatusBadRequest, response.CodeInvalidRequest, err.Error())
		return
	}
	if !slugPattern.MatchString(req.Slug) {
		response.Error(c, http.StatusBadRequest, response.CodeInvalidRequest, "slug must match ^[a-z0-9-]{2,40}$")
		return
	}
	if req.Name == "" {
		response.Error(c, http.StatusBadRequest, response.CodeInvalidRequest, "name is required")
		return
	}

	userID := middleware.UserIDFrom(c.Request.Context())
	summary, err := h.repo.CreateMap(c.Request.Context(), req.Slug, req.Name, userID)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" && pgErr.ConstraintName == "maps_slug_key" {
			response.Error(c, http.StatusConflict, response.CodeConflict, "slug already in use")
			return
		}
		response.Error(c, http.StatusInternalServerError, response.CodeInternal, "internal error")
		return
	}
	response.Created(c, summary)
}

// getVersion returns the status and document for one (map, version) pair,
// draft or published.
func (h *AdminHandler) getVersion(c *gin.Context) {
	mapID, ok := parseIDParam(c, "id")
	if !ok {
		return
	}
	version, ok := parseVersionParam(c)
	if !ok {
		return
	}

	status, doc, err := h.repo.GetVersion(c.Request.Context(), mapID, version)
	if err != nil {
		writeMapError(c, err)
		return
	}
	response.OK(c, gin.H{"status": status, "doc": json.RawMessage(doc)})
}

// maxMapDocBytes caps a PUT draft body, mirroring uploadAsset's
// http.MaxBytesReader use just below. Map documents are JSON (cities,
// routes, tickets, layout) — a few hundred KB even for the full Europe
// board — so this leaves generous headroom while still bounding an
// unauthenticated-adjacent (admin-only, but still client-supplied) body.
const maxMapDocBytes = 2 << 20 // 2 MiB

// putDraft validates the raw request body as a map document and, if it
// passes, writes it to the map's single draft version (creating one if
// needed). On a ValidationErrors failure it returns 400 with a "details"
// array carrying every offending JSON path — the primary authoring
// feedback loop for the admin UI.
func (h *AdminHandler) putDraft(c *gin.Context) {
	mapID, ok := parseIDParam(c, "id")
	if !ok {
		return
	}

	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maxMapDocBytes)
	body, err := io.ReadAll(c.Request.Body)
	if err != nil {
		var maxErr *http.MaxBytesError
		if errors.As(err, &maxErr) {
			response.Error(c, http.StatusBadRequest, response.CodeInvalidRequest, "map document exceeds maximum size")
			return
		}
		response.Error(c, http.StatusBadRequest, response.CodeInvalidRequest, "failed to read request body")
		return
	}

	if _, perr := ParseMap(body); perr != nil {
		writeValidationError(c, perr)
		return
	}

	version, err := h.repo.UpsertDraft(c.Request.Context(), mapID, body)
	if err != nil {
		if errors.Is(err, ErrVersionPublished) {
			response.Error(c, http.StatusConflict, response.CodeConflict, err.Error())
			return
		}
		response.Error(c, http.StatusInternalServerError, response.CodeInternal, "internal error")
		return
	}
	response.OK(c, gin.H{"version": version})
}

// publish re-validates the stored document (defence in depth — the draft
// may have been written before a loader rule change) and, if it still
// passes, flips it to published. An already-published version is reported
// as 409 conflict.
func (h *AdminHandler) publish(c *gin.Context) {
	mapID, ok := parseIDParam(c, "id")
	if !ok {
		return
	}
	version, ok := parseVersionParam(c)
	if !ok {
		return
	}
	ctx := c.Request.Context()

	status, doc, err := h.repo.GetVersion(ctx, mapID, version)
	if err != nil {
		writeMapError(c, err)
		return
	}
	if status == "published" {
		response.Error(c, http.StatusConflict, response.CodeConflict, ErrVersionPublished.Error())
		return
	}

	if _, perr := ParseMap(doc); perr != nil {
		writeValidationError(c, perr)
		return
	}

	if err := h.repo.Publish(ctx, mapID, version); err != nil {
		if errors.Is(err, ErrVersionPublished) {
			response.Error(c, http.StatusConflict, response.CodeConflict, err.Error())
			return
		}
		writeMapError(c, err)
		return
	}

	// Best-effort warm of the in-process cache so the very next lobby
	// pinning this version doesn't pay a cold-cache DB round trip. Publish
	// already succeeded, so a warm failure here is not fatal.
	if h.cache != nil {
		_, _ = h.cache.Get(ctx, mapID.String(), version)
	}

	response.OK(c, gin.H{"version": version, "status": "published"})
}

const (
	// maxAssetBytes is the upload cap enforced both here and by the
	// ttr.map_assets.byte_size CHECK constraint (migration 0006).
	maxAssetBytes = 4 << 20 // 4 MiB
)

// allowedAssetMimes are the only background-image mime types accepted;
// notably, image/svg+xml is never in this set (it can carry script).
var allowedAssetMimes = map[string]bool{
	"image/png":  true,
	"image/jpeg": true,
	"image/webp": true,
}

// uploadAsset stores a background image, sniffing its real content type
// with http.DetectContentType rather than trusting the client-declared
// Content-Type, and rejecting anything outside allowedAssetMimes (SVG
// included) or over maxAssetBytes.
func (h *AdminHandler) uploadAsset(c *gin.Context) {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maxAssetBytes)

	fileHeader, err := c.FormFile("file")
	if err != nil {
		response.Error(c, http.StatusBadRequest, response.CodeInvalidRequest, "missing file field")
		return
	}
	if fileHeader.Size > maxAssetBytes {
		response.Error(c, http.StatusBadRequest, response.CodeInvalidRequest, ErrAssetTooLarge.Error())
		return
	}

	f, err := fileHeader.Open()
	if err != nil {
		response.Error(c, http.StatusBadRequest, response.CodeInvalidRequest, "failed to read uploaded file")
		return
	}
	defer f.Close() //nolint:errcheck

	data, err := io.ReadAll(f)
	if err != nil {
		var maxErr *http.MaxBytesError
		if errors.As(err, &maxErr) {
			response.Error(c, http.StatusBadRequest, response.CodeInvalidRequest, ErrAssetTooLarge.Error())
			return
		}
		response.Error(c, http.StatusBadRequest, response.CodeInvalidRequest, "failed to read uploaded file")
		return
	}
	if len(data) == 0 || len(data) > maxAssetBytes {
		response.Error(c, http.StatusBadRequest, response.CodeInvalidRequest, ErrAssetTooLarge.Error())
		return
	}

	sniffed := http.DetectContentType(data)
	declared := fileHeader.Header.Get("Content-Type")
	if !allowedAssetMimes[sniffed] || (declared != "" && !mimeEqual(sniffed, declared)) {
		response.Error(c, http.StatusBadRequest, response.CodeInvalidRequest, ErrUnsupportedMime.Error())
		return
	}

	sum := sha256.Sum256(data)
	sha := hex.EncodeToString(sum[:])

	userID := middleware.UserIDFrom(c.Request.Context())
	assetID, err := h.repo.InsertAsset(c.Request.Context(), sniffed, data, sha, userID)
	if err != nil {
		response.Error(c, http.StatusInternalServerError, response.CodeInternal, "internal error")
		return
	}

	response.Created(c, gin.H{
		"asset_id":  assetID,
		"mime":      sniffed,
		"byte_size": len(data),
		"sha256":    sha,
	})
}

// mimeEqual compares a sniffed mime type against a client-declared
// Content-Type, ignoring any ";charset=..." parameters on the latter.
func mimeEqual(sniffed, declared string) bool {
	if idx := strings.Index(declared, ";"); idx >= 0 {
		declared = declared[:idx]
	}
	return strings.EqualFold(strings.TrimSpace(declared), sniffed)
}

// writeValidationError reports a ParseMap failure as 400 invalid_request,
// attaching every ValidationError as a {"path","message"} details entry
// when perr is a ValidationErrors. A broken route pair can legitimately
// emit two entries (one per direction) — callers must never assume an
// exact count, only that every offending path is present.
func writeValidationError(c *gin.Context, perr error) {
	var verrs ValidationErrors
	if errors.As(perr, &verrs) {
		details := make([]any, 0, len(verrs))
		for _, e := range verrs {
			details = append(details, gin.H{"path": e.Path, "message": e.Message})
		}
		response.ErrorWithDetails(c, http.StatusBadRequest, response.CodeInvalidRequest, "invalid map document", details)
		return
	}
	response.Error(c, http.StatusBadRequest, response.CodeInvalidRequest, perr.Error())
}

// parseIDParam parses gin path param name as a UUID, writing a 400 and
// returning ok=false on failure.
func parseIDParam(c *gin.Context, name string) (uuid.UUID, bool) {
	id, err := uuid.Parse(c.Param(name))
	if err != nil {
		response.Error(c, http.StatusBadRequest, response.CodeInvalidRequest, "invalid "+name)
		return uuid.Nil, false
	}
	return id, true
}

// parseVersionParam parses the gin path param "version" as an int32,
// writing a 400 and returning ok=false on failure.
func parseVersionParam(c *gin.Context) (int32, bool) {
	n, err := strconv.ParseInt(c.Param("version"), 10, 32)
	if err != nil {
		response.Error(c, http.StatusBadRequest, response.CodeInvalidRequest, "invalid version")
		return 0, false
	}
	return int32(n), true
}
