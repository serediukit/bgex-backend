package song_guess

import (
	"context"
	"errors"
	"net/http"
	"regexp"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/serediukit/bgex-backend/internal/httpx/middleware"
	"github.com/serediukit/bgex-backend/internal/httpx/response"
)

// packReader is the subset of *PackRepo that PackHandler needs for the
// play-time routes (list/get/create). Depending on this narrow interface
// rather than *PackRepo directly lets packhandler_test.go exercise the
// handlers against an in-memory stub instead of a live Postgres instance,
// mirroring ttr.mapReader.
type packReader interface {
	ListPublic(ctx context.Context) ([]PackSummary, error)
	ListOwned(ctx context.Context, userID uuid.UUID) ([]PackSummary, error)
	GetBySlugOrID(ctx context.Context, ref string) (*PackSummary, error)
	CreatePack(ctx context.Context, createdBy uuid.UUID, slug, name, authorLabel string) (*PackSummary, error)
}

// PackHandler exposes the play-time song_guess pack REST routes: listing,
// single-pack lookup by slug or id, and creation. The owner-gated authoring
// routes (doc/versions/draft/publish/delete) are a separate handler, mounted
// alongside this one but never sharing a route group via group.Use (root
// CLAUDE.md gotcha).
type PackHandler struct {
	repo  packReader
	cache PackProvider
}

// NewPackHandler creates a PackHandler backed by repo/cache.
func NewPackHandler(repo *PackRepo, cache *PackCache) *PackHandler {
	return &PackHandler{repo: repo, cache: cache}
}

// Register mounts the play-time song_guess pack routes under
// /games/song-guess. authMiddleware is applied inline per route (never
// group.Use) per the codebase convention — two groups at the same prefix
// with Use on one silently drops routes from Gin's radix tree.
func (h *PackHandler) Register(authMiddleware gin.HandlerFunc) func(*gin.RouterGroup) {
	return func(r *gin.RouterGroup) {
		g := r.Group("/games/song-guess")
		g.GET("/packs", authMiddleware, h.listPacks)
		g.GET("/packs/:ref", authMiddleware, h.getPackSummary)
		g.POST("/packs", authMiddleware, h.createPack)
	}
}

// languageSummary is one row of a pack's per-cell song counts: never a song
// id, title, artist or block — see packSummaryView's doc comment.
type languageSummary struct {
	Name    string         `json:"name"`
	Buckets map[string]int `json:"buckets"`
}

// packSummaryView is the play-time projection of a pack: metadata plus,
// where the caller asked for a single pack, per-(language, bucket) song
// counts. It must never carry a song id, title, artist, block string or
// video id — those only ever leave the server via the redacted play-time
// view (engine.View), not this browse/lookup surface.
//
// Languages is populated only by buildSummaryView (GET /packs/:ref,
// POST /packs) — never by buildListView (GET /packs), which answers the
// unbounded pack list from already-queried metadata alone and does not load
// or parse any pack document. It is optional here, not lied-about-omitted:
// a list row and a single-pack row are genuinely different shapes on the
// wire, and the frontend's SongGuessPackSummary type mirrors that (see
// bgex-frontend/types/song-guess.ts).
type packSummaryView struct {
	ID           uuid.UUID         `json:"id"`
	Slug         string            `json:"slug"`
	Name         string            `json:"name"`
	AuthorLabel  string            `json:"author_label"`
	IsOfficial   bool              `json:"is_official"`
	TotalSongs   int               `json:"total_songs"`
	IsEven       bool              `json:"is_even"`
	HasPublished bool              `json:"has_published"`
	Languages    []languageSummary `json:"languages,omitempty"`
}

// listPacks returns every public pack plus the caller's own drafts, de-
// duplicated by id (a pack can be both public and owned by the caller).
//
// This endpoint deliberately does NOT call buildSummaryView: that helper
// loads and parses each pack's latest published document via h.cache to
// compute per-(language, bucket) counts, which is fine for one pack
// (getPackSummary, createPack) but was previously called once per pack
// *here*, in a loop, in-request. With an unbounded, unmoderated pack list —
// this feature's entire premise is that any user can author and share packs
// (Q1), unlike TTR's admin-gated maps — that meant every browse issued one
// sequential LoadDoc (up to the 512 KiB doc cap) plus a full ParsePack per
// published pack. buildListView below returns exactly the metadata
// columns already sitting in PackSummary (including the denormalized
// total_songs), with no document load and no Languages field at all — the
// TTR-sanctioned shape (ttr.maphandler.go's listMaps answers from SQL
// alone; only the single-map getMap touches the cache). A client that needs
// per-cell counts fetches them from GET /packs/:ref, which still calls
// buildSummaryView for that one pack.
func (h *PackHandler) listPacks(c *gin.Context) {
	ctx := c.Request.Context()
	userID := middleware.UserIDFrom(ctx)

	public, err := h.repo.ListPublic(ctx)
	if err != nil {
		response.Error(c, http.StatusInternalServerError, response.CodeInternal, "internal error")
		return
	}
	owned, err := h.repo.ListOwned(ctx, userID)
	if err != nil {
		response.Error(c, http.StatusInternalServerError, response.CodeInternal, "internal error")
		return
	}

	merged := mergePackSummaries(public, owned)
	views := make([]packSummaryView, 0, len(merged))
	for i := range merged {
		views = append(views, buildListView(&merged[i]))
	}
	response.OK(c, views)
}

// buildListView projects a PackSummary into its list-time view: metadata
// only, straight from the already-queried row, with no document load and no
// Languages — see listPacks's doc comment for why the per-pack fan-out this
// replaces was a problem.
func buildListView(s *PackSummary) packSummaryView {
	return packSummaryView{
		ID:           s.ID,
		Slug:         s.Slug,
		Name:         s.Name,
		AuthorLabel:  s.AuthorLabel,
		IsOfficial:   s.IsOfficial,
		TotalSongs:   s.TotalSongs,
		IsEven:       s.TotalSongs%2 == 0,
		HasPublished: s.LatestPublishedVersion != nil,
	}
}

// mergePackSummaries unions a and b, keeping the first occurrence of each
// distinct pack id (order: a's entries first, then any of b's not already
// present).
func mergePackSummaries(a, b []PackSummary) []PackSummary {
	seen := make(map[uuid.UUID]bool, len(a)+len(b))
	out := make([]PackSummary, 0, len(a)+len(b))
	for _, s := range a {
		if seen[s.ID] {
			continue
		}
		seen[s.ID] = true
		out = append(out, s)
	}
	for _, s := range b {
		if seen[s.ID] {
			continue
		}
		seen[s.ID] = true
		out = append(out, s)
	}
	return out
}

// getPackSummary resolves :ref (a slug or a UUID) to its packSummaryView.
func (h *PackHandler) getPackSummary(c *gin.Context) {
	ctx := c.Request.Context()
	ref := c.Param("ref")

	s, err := h.repo.GetBySlugOrID(ctx, ref)
	if err != nil {
		writePackError(c, err)
		return
	}
	response.OK(c, h.buildSummaryView(ctx, s))
}

// createPackReq is the request body for POST /games/song-guess/packs. Slug
// and AuthorLabel are optional: an absent slug is derived from Name, an
// absent AuthorLabel is stored as an empty string.
type createPackReq struct {
	Name        string `json:"name"`
	Slug        string `json:"slug"`
	AuthorLabel string `json:"author_label"`
}

// slugFromNamePattern matches every run of characters that are not a
// lowercase letter or digit, so it can be collapsed to a single hyphen when
// deriving a slug from a pack name.
var slugFromNamePattern = regexp.MustCompile(`[^a-z0-9]+`)

// slugify derives a URL-safe slug from a pack name: lowercased, non-
// alphanumeric runs collapsed to a single hyphen, leading/trailing hyphens
// trimmed, and capped at 40 chars (mirroring ttr's slugPattern upper
// bound). Falls back to "pack" if the name has no alphanumeric characters
// at all (e.g. a name that is only emoji or punctuation).
func slugify(name string) string {
	s := slugFromNamePattern.ReplaceAllString(strings.ToLower(strings.TrimSpace(name)), "-")
	s = strings.Trim(s, "-")
	if len(s) > 40 {
		s = strings.Trim(s[:40], "-")
	}
	if s == "" {
		return "pack"
	}
	return s
}

// createPack inserts a new, versionless pack owned by the caller. slug is
// generated from name when absent; a unique-slug violation is reported as
// 409 conflict.
func (h *PackHandler) createPack(c *gin.Context) {
	var req createPackReq
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Error(c, http.StatusBadRequest, response.CodeInvalidRequest, err.Error())
		return
	}

	req.Name = strings.TrimSpace(req.Name)
	if req.Name == "" {
		response.Error(c, http.StatusBadRequest, response.CodeInvalidRequest, "name is required")
		return
	}

	slug := strings.TrimSpace(req.Slug)
	if slug == "" {
		slug = slugify(req.Name)
	}

	userID := middleware.UserIDFrom(c.Request.Context())
	summary, err := h.repo.CreatePack(c.Request.Context(), userID, slug, req.Name, req.AuthorLabel)
	if err != nil {
		writePackError(c, err)
		return
	}
	response.Created(c, h.buildSummaryView(c.Request.Context(), summary))
}

// buildSummaryView projects a PackSummary into its play-time view, filling
// Languages with per-(language, bucket) counts only — never song content —
// by loading and parsing the pack's latest published document. A pack with
// no published version (LatestPublishedVersion == nil) contributes no
// Languages at all, since there is no player-facing content to count yet.
//
// A load/parse failure on an already-published version should not fail the
// whole listing (a single corrupt pack must not take down browse/lookup for
// everyone else), so it degrades to omitting Languages rather than erroring.
func (h *PackHandler) buildSummaryView(ctx context.Context, s *PackSummary) packSummaryView {
	v := packSummaryView{
		ID:           s.ID,
		Slug:         s.Slug,
		Name:         s.Name,
		AuthorLabel:  s.AuthorLabel,
		IsOfficial:   s.IsOfficial,
		TotalSongs:   s.TotalSongs,
		IsEven:       s.TotalSongs%2 == 0,
		HasPublished: s.LatestPublishedVersion != nil,
	}
	if s.LatestPublishedVersion == nil || h.cache == nil {
		return v
	}

	pack, err := h.cache.Get(ctx, s.ID.String(), *s.LatestPublishedVersion)
	if err != nil {
		return v
	}
	v.Languages = languageSummaries(pack)
	return v
}

// languageSummaries builds the counts-only per-language view of pack: one
// entry per language (document order), each carrying only the number of
// songs at every bucket key present anywhere in the pack (0 counts, where a
// language has no songs at a given bucket, are omitted rather than zero-
// filled).
func languageSummaries(pack *Pack) []languageSummary {
	names := pack.LanguageNames()
	keys := pack.BucketKeys()

	out := make([]languageSummary, 0, len(names))
	for _, name := range names {
		buckets := make(map[string]int, len(keys))
		for _, k := range keys {
			if n := pack.CountAt(name, k); n > 0 {
				buckets[strconv.Itoa(k)] = n
			}
		}
		out = append(out, languageSummary{Name: name, Buckets: buckets})
	}
	return out
}

// writePackError maps song_guess pack domain sentinels to the HTTP error
// envelope; anything unrecognized becomes a 500.
func writePackError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, ErrPackNotFound):
		response.Error(c, http.StatusNotFound, response.CodeNotFound, err.Error())
	case errors.Is(err, ErrPackVersionNotFound):
		response.Error(c, http.StatusNotFound, response.CodeNotFound, err.Error())
	case errors.Is(err, ErrSlugTaken):
		response.Error(c, http.StatusConflict, response.CodeConflict, "slug already in use")
	default:
		response.Error(c, http.StatusInternalServerError, response.CodeInternal, "internal error")
	}
}
