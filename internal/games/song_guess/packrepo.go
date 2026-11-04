package song_guess

import (
	"context"
	"errors"
	"fmt"
	"time"

	sq "github.com/Masterminds/squirrel"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// packVersionStatusPublished is the song_guess.pack_versions.status value
// meaning a version is immutable and player-facing. Kept as a constant
// (mirroring ttr.mapVersionStatusPublished) because Publish below compares
// a decoded status string against it in Go, unlike the query literals in
// this file, which stay inline SQL.
const packVersionStatusPublished = "published"

// ErrSlugTaken is returned when creating a pack with a slug that already
// exists (mirrors user.ErrEmailTaken's unique-violation handling). Declared
// here rather than errors.go because it is specific to PackRepo's write
// path, not a domain-wide sentinel.
var ErrSlugTaken = errors.New("pack slug already in use")

// ErrVersionPublished is returned when an attempt is made to mutate or
// re-publish a pack version that has already been published (mirrors
// ttr.ErrVersionPublished).
var ErrVersionPublished = errors.New("pack version is already published")

// PackRepo persists song_guess pack definitions (song_guess.packs /
// song_guess.pack_versions) via pgx + squirrel. It satisfies PackLoader
// (LoadDoc), so a PackCache can be built directly over it.
type PackRepo struct {
	pool *pgxpool.Pool
	sb   sq.StatementBuilderType
}

// NewPackRepo returns a PackRepo backed by pool.
func NewPackRepo(pool *pgxpool.Pool) *PackRepo {
	return &PackRepo{pool: pool, sb: sq.StatementBuilder.PlaceholderFormat(sq.Dollar)}
}

// PackSummary is a lightweight projection of a song_guess.packs row for
// listing and lookup, annotated with its latest published version (nil if
// the pack has never been published) and that version's denormalized song
// count (0 if never published).
type PackSummary struct {
	ID                     uuid.UUID  `json:"id"`
	Slug                   string     `json:"slug"`
	Name                   string     `json:"name"`
	AuthorLabel            string     `json:"author_label"`
	CreatedBy              *uuid.UUID `json:"created_by"`
	IsOfficial             bool       `json:"is_official"`
	IsPublic               bool       `json:"is_public"`
	LatestPublishedVersion *int32     `json:"latest_published_version"`
	TotalSongs             int        `json:"total_songs"`
}

// packSummaryColumns is shared by every query that scans a PackSummary; the
// two correlated subqueries find the highest version currently published
// for the pack (or NULL if none is) and that version's denormalized
// total_songs (or 0 if none is published).
const packSummaryColumns = `
	p.id, p.slug, p.name, p.author_label, p.created_by, p.is_official, p.is_public,
	(SELECT MAX(pv.version) FROM song_guess.pack_versions pv
	 WHERE pv.pack_id = p.id AND pv.status = 'published') AS latest_published_version,
	COALESCE((SELECT pv2.total_songs FROM song_guess.pack_versions pv2
	 WHERE pv2.pack_id = p.id AND pv2.status = 'published'
	 ORDER BY pv2.version DESC LIMIT 1), 0) AS total_songs
FROM song_guess.packs p`

func scanPackSummary(row pgx.Row) (*PackSummary, error) {
	var s PackSummary
	if err := row.Scan(
		&s.ID, &s.Slug, &s.Name, &s.AuthorLabel, &s.CreatedBy, &s.IsOfficial, &s.IsPublic,
		&s.LatestPublishedVersion, &s.TotalSongs,
	); err != nil {
		return nil, err
	}
	return &s, nil
}

func scanPackSummaries(rows pgx.Rows) ([]PackSummary, error) {
	var out []PackSummary
	for rows.Next() {
		s, err := scanPackSummary(rows)
		if err != nil {
			return nil, fmt.Errorf("scan pack summary: %w", err)
		}
		out = append(out, *s)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate pack summaries: %w", err)
	}
	return out, nil
}

// ListPublic returns every pack that has at least one published version and
// is marked public, official packs first, then newest first. This is the
// browse-time "pick a pack for my lobby" listing (Q1).
func (r *PackRepo) ListPublic(ctx context.Context) ([]PackSummary, error) {
	rows, err := r.pool.Query(ctx, `SELECT `+packSummaryColumns+`
		WHERE p.is_public AND EXISTS (
			SELECT 1 FROM song_guess.pack_versions pv2 WHERE pv2.pack_id = p.id AND pv2.status = 'published'
		)
		ORDER BY p.is_official DESC, p.created_at DESC`)
	if err != nil {
		return nil, fmt.Errorf("list public packs: %w", err)
	}
	defer rows.Close()
	return scanPackSummaries(rows)
}

// ListOwned returns every pack created by userID, newest first, regardless
// of publication or visibility state — the pack editor's "my packs" list.
func (r *PackRepo) ListOwned(ctx context.Context, userID uuid.UUID) ([]PackSummary, error) {
	rows, err := r.pool.Query(ctx, `SELECT `+packSummaryColumns+`
		WHERE p.created_by = $1
		ORDER BY p.created_at DESC`, userID)
	if err != nil {
		return nil, fmt.Errorf("list owned packs for %s: %w", userID, err)
	}
	defer rows.Close()
	return scanPackSummaries(rows)
}

// GetBySlugOrID resolves ref — a slug or a UUID string — to its
// PackSummary. Returns ErrPackNotFound if ref matches nothing.
func (r *PackRepo) GetBySlugOrID(ctx context.Context, ref string) (*PackSummary, error) {
	var (
		query string
		arg   any
	)
	if id, err := uuid.Parse(ref); err == nil {
		query = `SELECT ` + packSummaryColumns + ` WHERE p.id = $1`
		arg = id
	} else {
		query = `SELECT ` + packSummaryColumns + ` WHERE p.slug = $1`
		arg = ref
	}

	s, err := scanPackSummary(r.pool.QueryRow(ctx, query, arg))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrPackNotFound
		}
		return nil, fmt.Errorf("get pack %q: %w", ref, err)
	}
	return s, nil
}

// LoadDoc fetches the raw document for a published (pack_id, version) pair.
// It satisfies PackLoader for PackCache. packID is a string (rather than
// uuid.UUID) to match the PackLoader interface, which is driven by the
// pinned pack_id string stored in engine state.
//
// The status = 'published' filter matters even though PackCache entries are
// never invalidated: that "never needs invalidation" invariant relies on
// versions being immutable once published (Req 31), which is not true of a
// draft. If a caller ever warmed the cache for a draft's (pack_id, version)
// key, this query would otherwise happily return the draft's current doc
// and the cache would serve that snapshot forever, even after the draft is
// edited again or published with different content.
func (r *PackRepo) LoadDoc(ctx context.Context, packID string, version int32) ([]byte, error) {
	id, err := uuid.Parse(packID)
	if err != nil {
		return nil, fmt.Errorf("%w: %q is not a uuid", ErrPackNotFound, packID)
	}
	var doc []byte
	err = r.pool.QueryRow(ctx,
		`SELECT doc FROM song_guess.pack_versions WHERE pack_id = $1 AND version = $2 AND status = 'published'`,
		id, version,
	).Scan(&doc)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrPackVersionNotFound
		}
		return nil, fmt.Errorf("load pack doc %s@%d: %w", packID, version, err)
	}
	return doc, nil
}

// GetVersionDoc returns the raw stored document and status ("draft" |
// "published") for (packID, version), regardless of publication state.
//
// Unlike LoadDoc above, which filters to status = 'published' because it
// exists solely to feed the play-time PackCache's immutable-once-published
// contract, the owner-gated authoring routes (AuthorHandler, authorhandler.go)
// must be able to read and re-validate a document that is still a draft:
// publish needs to re-parse the draft's own content (Req 31), and the pack
// editor needs to reopen a draft it previously saved. That distinction —
// status-agnostic vs. published-only — is the entire reason this method
// exists alongside LoadDoc instead of replacing it.
func (r *PackRepo) GetVersionDoc(ctx context.Context, packID uuid.UUID, version int32) ([]byte, string, error) {
	var doc []byte
	var status string
	err := r.pool.QueryRow(ctx,
		`SELECT doc, status FROM song_guess.pack_versions WHERE pack_id = $1 AND version = $2`,
		packID, version,
	).Scan(&doc, &status)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, "", ErrPackVersionNotFound
		}
		return nil, "", fmt.Errorf("get pack version doc %s@%d: %w", packID, version, err)
	}
	return doc, status, nil
}

// CreatePack inserts a new, versionless pack row. The first version is
// added separately via UpsertDraft. A unique-violation on slug is mapped to
// ErrSlugTaken (mirrors user.ErrEmailTaken's mapUniqueViolation pattern).
func (r *PackRepo) CreatePack(ctx context.Context, createdBy uuid.UUID, slug, name, authorLabel string) (*PackSummary, error) {
	query, args, err := r.sb.Insert("song_guess.packs").
		Columns("slug", "name", "author_label", "created_by").
		Values(slug, name, authorLabel, createdBy).
		Suffix("RETURNING id, slug, name, author_label, created_by, is_official, is_public").
		ToSql()
	if err != nil {
		return nil, fmt.Errorf("build insert pack: %w", err)
	}

	var s PackSummary
	row := r.pool.QueryRow(ctx, query, args...)
	if err := row.Scan(&s.ID, &s.Slug, &s.Name, &s.AuthorLabel, &s.CreatedBy, &s.IsOfficial, &s.IsPublic); err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" && pgErr.ConstraintName == "packs_slug_key" {
			return nil, ErrSlugTaken
		}
		return nil, fmt.Errorf("create pack %q: %w", slug, err)
	}
	// A freshly created pack has no published version yet.
	s.LatestPublishedVersion = nil
	s.TotalSongs = 0
	return &s, nil
}

// UpsertDraft writes doc (with its precomputed sha256 digest, validation
// state and denormalized song count) to packID's single draft version,
// creating one at max(version)+1 if none exists yet. Per Req 31, published
// versions are immutable: if the highest existing version is published,
// this forks a brand-new draft one version ahead rather than touching it.
//
// The whole read-then-write runs in a single transaction with
// SELECT ... FOR UPDATE locking whichever row this call is about to touch
// (the existing draft row, or the parent song_guess.packs row when forking
// a new one) — mirroring ttr.MapRepo.UpsertDraft's race analysis verbatim:
// without the lock, a save could race a concurrent Publish of the same
// version (the SELECT sees status='draft', Publish commits its flip to
// 'published' in between, and the subsequent UPDATE, still scoped to
// status='draft', silently matches zero rows), or two concurrent forks
// could compute the same max(version)+1 and collide on the primary key.
func (r *PackRepo) UpsertDraft(ctx context.Context, packID uuid.UUID, doc []byte, sha string, validated bool, totalSongs int) (int32, error) {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return 0, fmt.Errorf("begin upsert draft tx: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	var draftVersion int32
	err = tx.QueryRow(ctx,
		`SELECT version FROM song_guess.pack_versions WHERE pack_id = $1 AND status = 'draft' ORDER BY version DESC LIMIT 1 FOR UPDATE`,
		packID,
	).Scan(&draftVersion)
	switch {
	case err == nil:
		tag, uerr := tx.Exec(ctx,
			`UPDATE song_guess.pack_versions SET doc = $3, doc_sha256 = $4, validated = $5, total_songs = $6, updated_at = now()
			 WHERE pack_id = $1 AND version = $2 AND status = 'draft'`,
			packID, draftVersion, doc, sha, validated, totalSongs,
		)
		if uerr != nil {
			return 0, fmt.Errorf("update draft version %s@%d: %w", packID, draftVersion, uerr)
		}
		if tag.RowsAffected() == 0 {
			return 0, ErrVersionPublished
		}
		if cerr := tx.Commit(ctx); cerr != nil {
			return 0, fmt.Errorf("commit upsert draft tx: %w", cerr)
		}
		return draftVersion, nil
	case errors.Is(err, pgx.ErrNoRows):
		if _, lerr := tx.Exec(ctx, `SELECT id FROM song_guess.packs WHERE id = $1 FOR UPDATE`, packID); lerr != nil {
			return 0, fmt.Errorf("lock pack %s: %w", packID, lerr)
		}
		var next int32
		if nerr := tx.QueryRow(ctx,
			`SELECT COALESCE(MAX(version), 0) + 1 FROM song_guess.pack_versions WHERE pack_id = $1`,
			packID,
		).Scan(&next); nerr != nil {
			return 0, fmt.Errorf("compute next version for pack %s: %w", packID, nerr)
		}
		if _, ierr := tx.Exec(ctx,
			`INSERT INTO song_guess.pack_versions (pack_id, version, status, doc, doc_sha256, validated, total_songs)
			 VALUES ($1, $2, 'draft', $3, $4, $5, $6)`,
			packID, next, doc, sha, validated, totalSongs,
		); ierr != nil {
			return 0, fmt.Errorf("insert draft version %s@%d: %w", packID, next, ierr)
		}
		if cerr := tx.Commit(ctx); cerr != nil {
			return 0, fmt.Errorf("commit upsert draft tx: %w", cerr)
		}
		return next, nil
	default:
		return 0, fmt.Errorf("find draft version for pack %s: %w", packID, err)
	}
}

// Publish flips (packID, version) from draft to published, stamping
// published_at. Published versions are immutable (Req 31), so publishing an
// already-published version — the only way this method could otherwise be
// asked to mutate one — is rejected with ErrVersionPublished instead of
// silently no-op'ing.
func (r *PackRepo) Publish(ctx context.Context, packID uuid.UUID, version int32) error {
	tag, err := r.pool.Exec(ctx,
		`UPDATE song_guess.pack_versions SET status = 'published', validated = TRUE, published_at = now(), updated_at = now()
		 WHERE pack_id = $1 AND version = $2 AND status = 'draft'`,
		packID, version,
	)
	if err != nil {
		return fmt.Errorf("publish pack version %s@%d: %w", packID, version, err)
	}
	if tag.RowsAffected() > 0 {
		return nil
	}

	status, err := r.getVersionStatus(ctx, packID, version)
	if err != nil {
		return err // ErrPackVersionNotFound
	}
	if status == packVersionStatusPublished {
		return ErrVersionPublished
	}
	return ErrPackVersionNotFound
}

// getVersionStatus returns the status ("draft" | "published") for
// (packID, version), or ErrPackVersionNotFound if no such row exists.
func (r *PackRepo) getVersionStatus(ctx context.Context, packID uuid.UUID, version int32) (string, error) {
	var status string
	err := r.pool.QueryRow(ctx,
		`SELECT status FROM song_guess.pack_versions WHERE pack_id = $1 AND version = $2`,
		packID, version,
	).Scan(&status)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return "", ErrPackVersionNotFound
		}
		return "", fmt.Errorf("get pack version status %s@%d: %w", packID, version, err)
	}
	return status, nil
}

// PackVersionInfo is a lightweight projection of a song_guess.pack_versions
// row — everything the pack editor's version picker needs, without the
// (potentially large) doc itself.
type PackVersionInfo struct {
	Version     int32      `json:"version"`
	Status      string     `json:"status"`
	Validated   bool       `json:"validated"`
	TotalSongs  int        `json:"total_songs"`
	PublishedAt *time.Time `json:"published_at"`
	CreatedAt   time.Time  `json:"created_at"`
}

// ListVersions returns every version of packID, newest first.
func (r *PackRepo) ListVersions(ctx context.Context, packID uuid.UUID) ([]PackVersionInfo, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT version, status, validated, total_songs, published_at, created_at
		 FROM song_guess.pack_versions WHERE pack_id = $1 ORDER BY version DESC`,
		packID,
	)
	if err != nil {
		return nil, fmt.Errorf("list versions for pack %s: %w", packID, err)
	}
	defer rows.Close()

	var out []PackVersionInfo
	for rows.Next() {
		var v PackVersionInfo
		if err := rows.Scan(&v.Version, &v.Status, &v.Validated, &v.TotalSongs, &v.PublishedAt, &v.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan version info for pack %s: %w", packID, err)
		}
		out = append(out, v)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate version info for pack %s: %w", packID, err)
	}
	return out, nil
}

// DeletePack removes packID and, via ON DELETE CASCADE, every one of its
// versions. It does not remove song_guess.game_states rows that pin one of
// those versions — the composite FK from game_states to pack_versions has
// no ON DELETE clause, so Postgres refuses the delete (a foreign-key
// violation) if any lobby, running or finished, still references it. That
// is deliberate: Req 32's pin must never dangle.
func (r *PackRepo) DeletePack(ctx context.Context, packID uuid.UUID) error {
	tag, err := r.pool.Exec(ctx, `DELETE FROM song_guess.packs WHERE id = $1`, packID)
	if err != nil {
		return fmt.Errorf("delete pack %s: %w", packID, err)
	}
	if tag.RowsAffected() == 0 {
		return ErrPackNotFound
	}
	return nil
}

// OwnerOf returns packID's created_by user, or nil if the owning account
// was deleted (created_by is ON DELETE SET NULL). Used by the write
// authorization check (Req 29): created_by == caller OR caller is admin.
func (r *PackRepo) OwnerOf(ctx context.Context, packID uuid.UUID) (*uuid.UUID, error) {
	var createdBy *uuid.UUID
	err := r.pool.QueryRow(ctx, `SELECT created_by FROM song_guess.packs WHERE id = $1`, packID).Scan(&createdBy)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrPackNotFound
		}
		return nil, fmt.Errorf("get owner of pack %s: %w", packID, err)
	}
	return createdBy, nil
}
