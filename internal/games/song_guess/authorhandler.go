package song_guess

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/serediukit/bgex-backend/internal/httpx/middleware"
	"github.com/serediukit/bgex-backend/internal/httpx/response"
)

// RoleLookup resolves a user's role, mirroring middleware.RoleLookup's
// shape without importing the middleware package: AuthorHandler's
// authorization is a per-row owner-or-admin check made inside each
// handler, not a route-level gate, so it has no reason to depend on that
// package. Satisfied by user.Repository — the same concrete dependency
// server.go already wires into middleware.RequireAdmin.
type RoleLookup interface {
	GetRole(ctx context.Context, id uuid.UUID) (string, error)
}

// authRepo is the subset of *PackRepo that AuthorHandler needs for the
// owner-gated authoring routes (doc/versions/draft/publish/delete).
// Depending on this narrow interface, rather than *PackRepo directly, lets
// authorhandler_test.go exercise the handlers against an in-memory stub
// instead of a live Postgres instance, mirroring packReader.
type authRepo interface {
	GetBySlugOrID(ctx context.Context, ref string) (*PackSummary, error)
	ListVersions(ctx context.Context, packID uuid.UUID) ([]PackVersionInfo, error)
	UpsertDraft(ctx context.Context, packID uuid.UUID, doc []byte, sha string, validated bool, totalSongs int) (int32, error)
	Publish(ctx context.Context, packID uuid.UUID, version int32) error
	DeletePack(ctx context.Context, packID uuid.UUID) error
	OwnerOf(ctx context.Context, packID uuid.UUID) (*uuid.UUID, error)
	GetVersionDoc(ctx context.Context, packID uuid.UUID, version int32) ([]byte, string, error)
}

// AuthorHandler exposes the owner-gated song_guess pack authoring routes:
// full document read, version listing, draft save, publish, and delete.
// Mounted alongside PackHandler under the same /games/song-guess prefix —
// per root CLAUDE.md's route-registration gotcha, neither Register uses
// group.Use, so the two route sets coexist without one shadowing the
// other in Gin's radix tree. There is deliberately no admin middleware
// anywhere here: authorization is a per-row created_by-or-admin check,
// via requireOwnerOrAdmin below.
type AuthorHandler struct {
	repo  authRepo
	roles RoleLookup
}

// NewAuthorHandler creates an AuthorHandler backed by repo and roles.
func NewAuthorHandler(repo *PackRepo, roles RoleLookup) *AuthorHandler {
	return &AuthorHandler{repo: repo, roles: roles}
}

// Register mounts the owner-gated song_guess pack authoring routes under
// /games/song-guess. authMiddleware is applied inline per route (never
// group.Use) per the codebase convention.
func (h *AuthorHandler) Register(authMiddleware gin.HandlerFunc) func(*gin.RouterGroup) {
	return func(r *gin.RouterGroup) {
		g := r.Group("/games/song-guess")
		g.GET("/packs/:ref/doc", authMiddleware, h.getPackDoc)
		g.GET("/packs/:ref/versions", authMiddleware, h.listVersions)
		g.PUT("/packs/:ref/draft", authMiddleware, h.putDraft)
		g.POST("/packs/:ref/versions/:version/publish", authMiddleware, h.publish)
		g.DELETE("/packs/:ref", authMiddleware, h.deletePack)
	}
}

// resolvePackRef resolves the :ref path param (slug or UUID) to a
// PackSummary, writing a 404 and returning ok=false if it does not exist.
func (h *AuthorHandler) resolvePackRef(c *gin.Context) (*PackSummary, bool) {
	s, err := h.repo.GetBySlugOrID(c.Request.Context(), c.Param("ref"))
	if err != nil {
		writePackError(c, err)
		return nil, false
	}
	return s, true
}

// requireOwnerOrAdmin confirms the authenticated caller may access packID's
// full document or mutate it: either they created it, or their role is
// admin. It writes the response itself and returns ok=false on any
// failure — including when the caller is simply not the owner, which
// resolves to 404, never 403, so a non-owner can never learn that someone
// else's pack (or draft) exists at all (Req 28). On success it returns the
// caller's own user id, saving every route handler a second
// middleware.UserIDFrom call.
func (h *AuthorHandler) requireOwnerOrAdmin(c *gin.Context, packID uuid.UUID) (uuid.UUID, bool) {
	ctx := c.Request.Context()
	userID := middleware.UserIDFrom(ctx)

	owner, err := h.repo.OwnerOf(ctx, packID)
	if err != nil {
		writePackError(c, err)
		return uuid.Nil, false
	}
	if owner != nil && *owner == userID {
		return userID, true
	}

	// A role-lookup failure fails closed (treated as non-admin) rather than
	// surfacing a 500: this path must never distinguish "pack exists but
	// you're not allowed" from "pack doesn't exist" via a different status
	// code or error shape.
	if role, rerr := h.roles.GetRole(ctx, userID); rerr == nil && role == "admin" {
		return userID, true
	}

	response.Error(c, http.StatusNotFound, response.CodeNotFound, "pack not found")
	return uuid.Nil, false
}

// getPackDoc returns the full, unredacted pack document for the pack's
// current version (or a specific ?version=N) to the pack's owner or an
// admin only. Everyone else gets 404 (Req 28, Req 29).
func (h *AuthorHandler) getPackDoc(c *gin.Context) {
	s, ok := h.resolvePackRef(c)
	if !ok {
		return
	}
	if _, ok := h.requireOwnerOrAdmin(c, s.ID); !ok {
		return
	}

	version, ok := h.resolveDocVersion(c, s.ID)
	if !ok {
		return
	}

	doc, status, err := h.repo.GetVersionDoc(c.Request.Context(), s.ID, version)
	if err != nil {
		writePackError(c, err)
		return
	}

	response.OK(c, gin.H{
		"pack_id": s.ID,
		"version": version,
		"status":  status,
		"doc":     json.RawMessage(doc),
	})
}

// resolveDocVersion resolves the target version for getPackDoc: an explicit
// ?version=N query param if present, else the pack's newest version
// (draft or published) so the editor's default load is always "whatever
// was most recently saved".
func (h *AuthorHandler) resolveDocVersion(c *gin.Context, packID uuid.UUID) (int32, bool) {
	if q := c.Query("version"); q != "" {
		n, err := strconv.ParseInt(q, 10, 32)
		if err != nil {
			response.Error(c, http.StatusBadRequest, response.CodeInvalidRequest, "invalid version")
			return 0, false
		}
		return int32(n), true
	}

	versions, err := h.repo.ListVersions(c.Request.Context(), packID)
	if err != nil {
		response.Error(c, http.StatusInternalServerError, response.CodeInternal, "internal error")
		return 0, false
	}
	if len(versions) == 0 {
		response.Error(c, http.StatusNotFound, response.CodeNotFound, "pack has no versions yet")
		return 0, false
	}
	// ListVersions returns newest first.
	return versions[0].Version, true
}

// listVersions returns every version of a pack, newest first, to the
// pack's owner or an admin only.
func (h *AuthorHandler) listVersions(c *gin.Context) {
	s, ok := h.resolvePackRef(c)
	if !ok {
		return
	}
	if _, ok := h.requireOwnerOrAdmin(c, s.ID); !ok {
		return
	}

	versions, err := h.repo.ListVersions(c.Request.Context(), s.ID)
	if err != nil {
		response.Error(c, http.StatusInternalServerError, response.CodeInternal, "internal error")
		return
	}
	if versions == nil {
		versions = []PackVersionInfo{}
	}
	response.OK(c, versions)
}

// maxPackDocBytes caps a PUT draft body (Req 37, Q8). Applies whether or
// not ?validate=false is set.
const maxPackDocBytes = 512 << 10 // 512 KiB

// putDraft validates the raw request body as a pack document and, if it
// passes, writes it to the pack's draft version (creating one, or forking
// a new one over an already-published version, as UpsertDraft decides). On
// a fatal ParsePack failure it returns 400 with itemized "details".
// Honors ?validate=false: any value other than the literal "false"
// (including an absent query param) means "validate".
func (h *AuthorHandler) putDraft(c *gin.Context) {
	s, ok := h.resolvePackRef(c)
	if !ok {
		return
	}
	if _, ok := h.requireOwnerOrAdmin(c, s.ID); !ok {
		return
	}

	validate := c.Query("validate") != "false"

	body, ok := readDraftBody(c)
	if !ok {
		return
	}

	warnings, ok := validateDraftBody(c, body, validate)
	if !ok {
		return
	}

	version, ok := h.saveDraft(c, s.ID, body, validate)
	if !ok {
		return
	}

	writeDraftSaved(c, version, validate, warnings)
}

// readDraftBody reads the request body under maxPackDocBytes, mapping a
// MaxBytesReader overflow to 400 rather than letting it surface as a raw
// I/O error (or, absent this check, a 500).
func readDraftBody(c *gin.Context) ([]byte, bool) {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maxPackDocBytes)
	body, err := io.ReadAll(c.Request.Body)
	if err != nil {
		var maxErr *http.MaxBytesError
		if errors.As(err, &maxErr) {
			response.Error(c, http.StatusBadRequest, response.CodeInvalidRequest, "pack document exceeds maximum size")
			return nil, false
		}
		response.Error(c, http.StatusBadRequest, response.CodeInvalidRequest, "failed to read request body")
		return nil, false
	}
	return body, true
}

// validateDraftBody runs ParsePack over body when validate is true,
// writing an itemized 400 on any fatal issue and returning its warnings
// (if any) on success. When validate is false it only confirms body is a
// syntactically valid JSON object — Req 37/38's ?validate=false escape
// hatch for a half-authored pack that can never pass full validation yet —
// and returns no warnings, since ParsePack never ran.
func validateDraftBody(c *gin.Context, body []byte, validate bool) ([]ValidationIssue, bool) {
	if !validate {
		// A JSON literal `null` unmarshals into a nil map with a nil error,
		// so the nil check is required alongside the error check — relying
		// on err alone lets a `null` body through as if it were `{}`.
		var obj map[string]json.RawMessage
		if err := json.Unmarshal(body, &obj); err != nil || obj == nil {
			response.Error(c, http.StatusBadRequest, response.CodeInvalidRequest, "pack document must be a JSON object")
			return nil, false
		}
		return nil, true
	}

	pack, perr := ParsePack(body)
	if pack == nil {
		writePackValidationError(c, perr)
		return nil, false
	}

	var verr *ValidationError
	if errors.As(perr, &verr) {
		return verr.Warnings(), true
	}
	return nil, true
}

// saveDraft computes doc's sha256 digest (UpsertDraft takes it as a
// parameter — this package has no hashing helper, see packrepo.go) and a
// best-effort total-song tally, then persists the draft.
func (h *AuthorHandler) saveDraft(c *gin.Context, packID uuid.UUID, body []byte, validate bool) (int32, bool) {
	sum := sha256.Sum256(body)
	sha := hex.EncodeToString(sum[:])
	totalSongs := countWireSongs(body)

	version, err := h.repo.UpsertDraft(c.Request.Context(), packID, body, sha, validate, totalSongs)
	if err != nil {
		if errors.Is(err, ErrVersionPublished) {
			response.Error(c, http.StatusConflict, response.CodeConflict, err.Error())
			return 0, false
		}
		response.Error(c, http.StatusInternalServerError, response.CodeInternal, "internal error")
		return 0, false
	}
	return version, true
}

// writeDraftSaved writes putDraft's success response, attaching a
// "warnings" array (in the same {path, message} shape as error.details)
// when ParsePack found any.
func writeDraftSaved(c *gin.Context, version int32, validated bool, warnings []ValidationIssue) {
	resp := gin.H{"version": version, "validated": validated}
	if len(warnings) > 0 {
		resp["warnings"] = issueDetails(warnings)
	}
	response.OK(c, resp)
}

// countWireSongs performs a best-effort tally of song entries in a pack v2
// document without running ParsePack's fatal validation. This is needed
// because putDraft with ?validate=false must still record an approximate
// total_songs count for a document that, by design, cannot pass full
// validation yet (e.g. an odd song count) — ParsePack itself returns no
// count on a fatal failure, only a *ValidationError. Reuses wireDoc
// (packloader.go, same package) rather than a parallel struct. Never
// errors: a document that doesn't even match the pack's rough JSON shape
// simply counts 0.
func countWireSongs(doc []byte) int {
	var d wireDoc
	if err := json.Unmarshal(doc, &d); err != nil {
		return 0
	}
	total := 0
	for _, l := range d.Languages {
		for _, songs := range l.Buckets {
			total += len(songs)
		}
	}
	return total
}

// publish re-validates the pack's stored document unconditionally via
// ParsePack and, if it still passes, flips it to published. An
// already-published version is reported as 409 conflict. This is the only
// path that can ever set validated = TRUE, so an unvalidated draft can
// never be published or played (Req 31).
func (h *AuthorHandler) publish(c *gin.Context) {
	s, ok := h.resolvePackRef(c)
	if !ok {
		return
	}
	if _, ok := h.requireOwnerOrAdmin(c, s.ID); !ok {
		return
	}
	version, ok := parsePathVersion(c)
	if !ok {
		return
	}
	ctx := c.Request.Context()

	doc, status, err := h.repo.GetVersionDoc(ctx, s.ID, version)
	if err != nil {
		writePackError(c, err)
		return
	}
	if status == packVersionStatusPublished {
		response.Error(c, http.StatusConflict, response.CodeConflict, ErrVersionPublished.Error())
		return
	}

	if pack, perr := ParsePack(doc); pack == nil {
		writePackValidationError(c, perr)
		return
	}

	if err := h.repo.Publish(ctx, s.ID, version); err != nil {
		if errors.Is(err, ErrVersionPublished) {
			response.Error(c, http.StatusConflict, response.CodeConflict, err.Error())
			return
		}
		writePackError(c, err)
		return
	}

	response.OK(c, gin.H{"version": version, "status": "published"})
}

// deletePack removes a pack and every one of its versions. If any
// song_guess.game_states row still pins one of those versions, the
// database's composite FK refuses the delete; that is translated to a
// clean 409 rather than letting it escape as a 500 (Req 32).
func (h *AuthorHandler) deletePack(c *gin.Context) {
	s, ok := h.resolvePackRef(c)
	if !ok {
		return
	}
	if _, ok := h.requireOwnerOrAdmin(c, s.ID); !ok {
		return
	}

	if err := h.repo.DeletePack(c.Request.Context(), s.ID); err != nil {
		if isForeignKeyViolation(err) {
			response.Error(c, http.StatusConflict, response.CodeConflict,
				"pack is referenced by an existing game and cannot be deleted")
			return
		}
		writePackError(c, err)
		return
	}
	response.NoContent(c)
}

// isForeignKeyViolation reports whether err wraps a Postgres foreign-key
// violation (SQLSTATE 23503) — the composite FK from
// song_guess.game_states to song_guess.pack_versions.
func isForeignKeyViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23503"
}

// issueDetails renders a []ValidationIssue as the {"path","message"}
// details shape response.ErrorWithDetails and writeDraftSaved both use.
func issueDetails(issues []ValidationIssue) []any {
	details := make([]any, 0, len(issues))
	for _, iss := range issues {
		details = append(details, gin.H{"path": iss.Path, "message": iss.Message})
	}
	return details
}

// writePackValidationError reports a ParsePack failure as 400
// invalid_request, attaching every fatal ValidationIssue as a
// {"path","message"} details entry when perr is a *ValidationError.
func writePackValidationError(c *gin.Context, perr error) {
	var verr *ValidationError
	if errors.As(perr, &verr) {
		response.ErrorWithDetails(c, http.StatusBadRequest, response.CodeInvalidRequest,
			"invalid pack document", issueDetails(verr.Fatal()))
		return
	}
	response.Error(c, http.StatusBadRequest, response.CodeInvalidRequest, perr.Error())
}

// parsePathVersion parses the gin path param "version" as an int32,
// writing a 400 and returning ok=false on failure.
func parsePathVersion(c *gin.Context) (int32, bool) {
	n, err := strconv.ParseInt(c.Param("version"), 10, 32)
	if err != nil {
		response.Error(c, http.StatusBadRequest, response.CodeInvalidRequest, "invalid version")
		return 0, false
	}
	return int32(n), true
}
