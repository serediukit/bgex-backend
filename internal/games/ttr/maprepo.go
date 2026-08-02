package ttr

import (
	"context"
	"errors"
	"fmt"
	"time"

	sq "github.com/Masterminds/squirrel"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// mapVersionStatusPublished is the ttr.map_versions.status value meaning a
// version is immutable and player-facing. Shared as a constant (rather than
// a repeated literal) between this file and the REST handlers that also
// compare against it (maphandler.go, adminhandler.go).
const mapVersionStatusPublished = "published"

// MapRepo persists TTR map definitions (ttr.maps / ttr.map_versions /
// ttr.map_assets) via pgx + squirrel. It satisfies MapLoader (LoadDoc), so a
// MapCache can be built directly over it.
type MapRepo struct {
	pool *pgxpool.Pool
	sb   sq.StatementBuilderType
}

// NewMapRepo returns a MapRepo backed by pool.
func NewMapRepo(pool *pgxpool.Pool) *MapRepo {
	return &MapRepo{pool: pool, sb: sq.StatementBuilder.PlaceholderFormat(sq.Dollar)}
}

// MapSummary is a lightweight projection of a ttr.maps row for listing and
// lookup, annotated with its latest published version (nil if the map has
// never been published).
type MapSummary struct {
	ID                     uuid.UUID `json:"id"`
	Slug                   string    `json:"slug"`
	Name                   string    `json:"name"`
	IsOfficial             bool      `json:"is_official"`
	LatestPublishedVersion *int32    `json:"latest_published_version"`
	CreatedAt              time.Time `json:"created_at"`
	UpdatedAt              time.Time `json:"updated_at"`
}

// mapSummaryColumns is shared by every query that scans a MapSummary; the
// correlated subquery finds the highest version currently published for the
// map, or NULL if none is.
const mapSummaryColumns = `
	m.id, m.slug, m.name, m.is_official, m.created_at, m.updated_at,
	(SELECT MAX(mv.version) FROM ttr.map_versions mv
	 WHERE mv.map_id = m.id AND mv.status = 'published') AS latest_published_version
FROM ttr.maps m`

func scanMapSummary(row pgx.Row) (*MapSummary, error) {
	var s MapSummary
	if err := row.Scan(&s.ID, &s.Slug, &s.Name, &s.IsOfficial, &s.CreatedAt, &s.UpdatedAt, &s.LatestPublishedVersion); err != nil {
		return nil, err
	}
	return &s, nil
}

func scanMapSummaries(rows pgx.Rows) ([]MapSummary, error) {
	var out []MapSummary
	for rows.Next() {
		s, err := scanMapSummary(rows)
		if err != nil {
			return nil, fmt.Errorf("scan map summary: %w", err)
		}
		out = append(out, *s)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate map summaries: %w", err)
	}
	return out, nil
}

// ListPublished returns every map that has at least one published version,
// ordered by name. This is the public "pick a map for my lobby" listing.
func (r *MapRepo) ListPublished(ctx context.Context) ([]MapSummary, error) {
	rows, err := r.pool.Query(ctx, `SELECT `+mapSummaryColumns+`
		WHERE EXISTS (
			SELECT 1 FROM ttr.map_versions mv2 WHERE mv2.map_id = m.id AND mv2.status = 'published'
		)
		ORDER BY m.name`)
	if err != nil {
		return nil, fmt.Errorf("list published maps: %w", err)
	}
	defer rows.Close()
	return scanMapSummaries(rows)
}

// ListAll returns every map regardless of publication state (admin use).
func (r *MapRepo) ListAll(ctx context.Context) ([]MapSummary, error) {
	rows, err := r.pool.Query(ctx, `SELECT `+mapSummaryColumns+` ORDER BY m.name`)
	if err != nil {
		return nil, fmt.Errorf("list all maps: %w", err)
	}
	defer rows.Close()
	return scanMapSummaries(rows)
}

// GetBySlugOrID resolves ref — a slug or a UUID string — to its MapSummary.
// Returns ErrMapNotFound if ref matches nothing.
func (r *MapRepo) GetBySlugOrID(ctx context.Context, ref string) (*MapSummary, error) {
	var (
		query string
		arg   any
	)
	if id, err := uuid.Parse(ref); err == nil {
		query = `SELECT ` + mapSummaryColumns + ` WHERE m.id = $1`
		arg = id
	} else {
		query = `SELECT ` + mapSummaryColumns + ` WHERE m.slug = $1`
		arg = ref
	}

	s, err := scanMapSummary(r.pool.QueryRow(ctx, query, arg))
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrMapNotFound
		}
		return nil, fmt.Errorf("get map %q: %w", ref, err)
	}
	return s, nil
}

// LoadDoc fetches the raw document for a published (map_id, version) pair.
// It satisfies MapLoader for MapCache. mapID is a string (rather than
// uuid.UUID) to match the MapLoader interface, which is driven by the
// pinned map_id string stored in engine state.
//
// The status = 'published' filter matters even though MapCache entries are
// never invalidated: that "never needs invalidation" invariant relies on
// versions being immutable once published, which is not true of a draft. If
// a caller ever warmed the cache for a draft's (map_id, version) key — e.g.
// a future admin-preview feature — this query would otherwise happily
// return the draft's current doc and then the cache would serve that
// snapshot forever, even after the draft is edited again or published with
// different content.
func (r *MapRepo) LoadDoc(ctx context.Context, mapID string, version int32) ([]byte, error) {
	id, err := uuid.Parse(mapID)
	if err != nil {
		return nil, fmt.Errorf("%w: %q is not a uuid", ErrMapNotFound, mapID)
	}
	var doc []byte
	err = r.pool.QueryRow(ctx,
		`SELECT doc FROM ttr.map_versions WHERE map_id = $1 AND version = $2 AND status = 'published'`,
		id, version,
	).Scan(&doc)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrMapVersionNotFound
		}
		return nil, fmt.Errorf("load map doc %s@%d: %w", mapID, version, err)
	}
	return doc, nil
}

// LatestPublished returns the highest published version for mapID and its
// document, or ErrMapVersionNotFound if the map has never been published.
func (r *MapRepo) LatestPublished(ctx context.Context, mapID uuid.UUID) (int32, []byte, error) {
	var (
		version int32
		doc     []byte
	)
	err := r.pool.QueryRow(ctx,
		`SELECT version, doc FROM ttr.map_versions
		 WHERE map_id = $1 AND status = 'published'
		 ORDER BY version DESC LIMIT 1`,
		mapID,
	).Scan(&version, &doc)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return 0, nil, ErrMapVersionNotFound
		}
		return 0, nil, fmt.Errorf("latest published version for map %s: %w", mapID, err)
	}
	return version, doc, nil
}

// CreateMap inserts a new, versionless map row (admin use). The first
// version is added separately via UpsertDraft.
func (r *MapRepo) CreateMap(ctx context.Context, slug, name string, createdBy uuid.UUID) (*MapSummary, error) {
	query, args, err := r.sb.Insert("ttr.maps").
		Columns("slug", "name", "created_by").
		Values(slug, name, createdBy).
		Suffix("RETURNING id, slug, name, is_official, created_at, updated_at").
		ToSql()
	if err != nil {
		return nil, fmt.Errorf("build insert map: %w", err)
	}

	var s MapSummary
	row := r.pool.QueryRow(ctx, query, args...)
	if err := row.Scan(&s.ID, &s.Slug, &s.Name, &s.IsOfficial, &s.CreatedAt, &s.UpdatedAt); err != nil {
		return nil, fmt.Errorf("create map %q: %w", slug, err)
	}
	return &s, nil
}

// UpsertDraft writes doc to mapID's single draft version, creating one at
// max(version)+1 if none exists yet. Published versions are immutable and
// are never targeted by this method — that invariant, plus the immutable
// contract itself, is instead enforced by Publish, which is the only
// operation that can turn a draft into a published version and refuses to
// do so twice (see Publish's doc comment for ErrVersionPublished).
//
// The whole read-then-write runs in a single transaction with SELECT ... FOR
// UPDATE locking whichever row this call is about to touch (the existing
// draft row, or the parent ttr.maps row when forking a new one). Without
// that lock, an admin editing a draft could race a concurrent Publish of the
// same version: the SELECT sees status='draft', Publish commits its flip to
// 'published' in between, and the subsequent UPDATE (still scoped to
// status='draft') silently matches zero rows — the caller gets back a 200
// with the version number as if the save had succeeded, and their edit is
// discarded. Taking the row lock up front makes Publish's own UPDATE (which
// takes the same row's lock implicitly) block until this transaction
// commits or rolls back, so the two operations serialize instead of racing.
// The RowsAffected()==0 check after the UPDATE is kept anyway as defense in
// depth (e.g. a future caller of this method outside of Publish's contract)
// and reported as ErrVersionPublished rather than a lying success.
//
// Forking a new draft (no existing draft row) has an analogous race: two
// concurrent forks could both read the same max(version) and then both
// INSERT the same next version, one of them failing on the primary key with
// a bare, unmapped 500. Locking the parent ttr.maps row first serializes
// concurrent forks against each other the same way, so the max(version)+1
// computed inside this transaction is guaranteed unique when the INSERT
// runs.
func (r *MapRepo) UpsertDraft(ctx context.Context, mapID uuid.UUID, doc []byte) (int32, error) {
	sha := DocSHA256(doc)

	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return 0, fmt.Errorf("begin upsert draft tx: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	var draftVersion int32
	err = tx.QueryRow(ctx,
		`SELECT version FROM ttr.map_versions WHERE map_id = $1 AND status = 'draft' ORDER BY version DESC LIMIT 1 FOR UPDATE`,
		mapID,
	).Scan(&draftVersion)
	switch {
	case err == nil:
		tag, uerr := tx.Exec(ctx,
			`UPDATE ttr.map_versions SET doc = $3, doc_sha256 = $4, updated_at = now()
			 WHERE map_id = $1 AND version = $2 AND status = 'draft'`,
			mapID, draftVersion, doc, sha,
		)
		if uerr != nil {
			return 0, fmt.Errorf("update draft version %s@%d: %w", mapID, draftVersion, uerr)
		}
		if tag.RowsAffected() == 0 {
			return 0, ErrVersionPublished
		}
		if cerr := tx.Commit(ctx); cerr != nil {
			return 0, fmt.Errorf("commit upsert draft tx: %w", cerr)
		}
		return draftVersion, nil
	case errors.Is(err, pgx.ErrNoRows):
		if _, lerr := tx.Exec(ctx, `SELECT id FROM ttr.maps WHERE id = $1 FOR UPDATE`, mapID); lerr != nil {
			return 0, fmt.Errorf("lock map %s: %w", mapID, lerr)
		}
		var next int32
		if nerr := tx.QueryRow(ctx,
			`SELECT COALESCE(MAX(version), 0) + 1 FROM ttr.map_versions WHERE map_id = $1`,
			mapID,
		).Scan(&next); nerr != nil {
			return 0, fmt.Errorf("compute next version for map %s: %w", mapID, nerr)
		}
		if _, ierr := tx.Exec(ctx,
			`INSERT INTO ttr.map_versions (map_id, version, status, doc, doc_sha256)
			 VALUES ($1, $2, 'draft', $3, $4)`,
			mapID, next, doc, sha,
		); ierr != nil {
			return 0, fmt.Errorf("insert draft version %s@%d: %w", mapID, next, ierr)
		}
		if cerr := tx.Commit(ctx); cerr != nil {
			return 0, fmt.Errorf("commit upsert draft tx: %w", cerr)
		}
		return next, nil
	default:
		return 0, fmt.Errorf("find draft version for map %s: %w", mapID, err)
	}
}

// Publish flips (mapID, version) from draft to published, stamping
// published_at. Published versions are immutable, so publishing an
// already-published version — the only way this method could otherwise be
// asked to mutate one — is rejected with ErrVersionPublished instead of
// silently no-op'ing.
func (r *MapRepo) Publish(ctx context.Context, mapID uuid.UUID, version int32) error {
	tag, err := r.pool.Exec(ctx,
		`UPDATE ttr.map_versions SET status = 'published', published_at = now(), updated_at = now()
		 WHERE map_id = $1 AND version = $2 AND status = 'draft'`,
		mapID, version,
	)
	if err != nil {
		return fmt.Errorf("publish map version %s@%d: %w", mapID, version, err)
	}
	if tag.RowsAffected() > 0 {
		return nil
	}

	status, _, err := r.GetVersion(ctx, mapID, version)
	if err != nil {
		return err // ErrMapVersionNotFound
	}
	if status == mapVersionStatusPublished {
		return ErrVersionPublished
	}
	return ErrMapVersionNotFound
}

// GetVersion returns the status ("draft" | "published") and document for
// (mapID, version).
func (r *MapRepo) GetVersion(ctx context.Context, mapID uuid.UUID, version int32) (status string, doc []byte, err error) {
	err = r.pool.QueryRow(ctx,
		`SELECT status, doc FROM ttr.map_versions WHERE map_id = $1 AND version = $2`,
		mapID, version,
	).Scan(&status, &doc)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return "", nil, ErrMapVersionNotFound
		}
		return "", nil, fmt.Errorf("get map version %s@%d: %w", mapID, version, err)
	}
	return status, doc, nil
}

// InsertAsset stores a content-addressed background image and returns its
// id. Because storage is keyed by sha256 (unique index), uploading bytes
// that already exist is idempotent: the ON CONFLICT clause makes the insert
// a no-op write and RETURNING still yields the existing row's id rather than
// erroring or creating a duplicate.
func (r *MapRepo) InsertAsset(ctx context.Context, mime string, data []byte, sha string, by uuid.UUID) (uuid.UUID, error) {
	query, args, err := r.sb.Insert("ttr.map_assets").
		Columns("mime", "byte_size", "sha256", "bytes", "created_by").
		Values(mime, len(data), sha, data, by).
		Suffix("ON CONFLICT (sha256) DO UPDATE SET mime = EXCLUDED.mime RETURNING id").
		ToSql()
	if err != nil {
		return uuid.Nil, fmt.Errorf("build insert asset: %w", err)
	}

	var id uuid.UUID
	if err := r.pool.QueryRow(ctx, query, args...).Scan(&id); err != nil {
		return uuid.Nil, fmt.Errorf("insert map asset: %w", err)
	}
	return id, nil
}

// GetAsset returns a background asset's mime type and bytes.
func (r *MapRepo) GetAsset(ctx context.Context, id uuid.UUID) (mime string, data []byte, err error) {
	err = r.pool.QueryRow(ctx, `SELECT mime, bytes FROM ttr.map_assets WHERE id = $1`, id).Scan(&mime, &data)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return "", nil, ErrAssetNotFound
		}
		return "", nil, fmt.Errorf("get map asset %s: %w", id, err)
	}
	return mime, data, nil
}

// GetAssetMeta returns a background asset's mime type and stored sha256
// digest without reading its bytes column (up to 4 MiB). getAsset uses this
// to build the ETag and answer a 304 without touching bytes at all: the
// digest is computed once at upload time (InsertAsset) and never changes
// afterward (content-addressed, immutable), so recomputing sha256.Sum256 on
// every request — including ones that end in an empty 304 — was pure work
// amplification on the platform's only unauthenticated route.
func (r *MapRepo) GetAssetMeta(ctx context.Context, id uuid.UUID) (mime, sha256Hex string, err error) {
	err = r.pool.QueryRow(ctx, `SELECT mime, sha256 FROM ttr.map_assets WHERE id = $1`, id).Scan(&mime, &sha256Hex)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return "", "", ErrAssetNotFound
		}
		return "", "", fmt.Errorf("get map asset meta %s: %w", id, err)
	}
	return mime, sha256Hex, nil
}
