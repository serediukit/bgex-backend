package friends

import (
	"context"
	"errors"
	"fmt"

	sq "github.com/Masterminds/squirrel"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/serediukit/bgex-backend/internal/domain/user"
)

// Repository handles all database operations for the friends domain.
type Repository struct {
	pool *pgxpool.Pool
	sb   sq.StatementBuilderType
}

// NewRepository creates a new Repository backed by the given connection pool.
func NewRepository(pool *pgxpool.Pool) *Repository {
	return &Repository{
		pool: pool,
		sb:   sq.StatementBuilder.PlaceholderFormat(sq.Dollar),
	}
}

// Send inserts a new pending friend request from requesterID to addresseeID.
// Returns ErrAlreadyExists if a request in either direction already exists.
func (r *Repository) Send(ctx context.Context, requesterID, addresseeID uuid.UUID) (*Request, error) {
	query, args, err := r.sb.
		Insert("friend_requests").
		Columns("requester_id", "addressee_id").
		Values(requesterID, addresseeID).
		Suffix("RETURNING id, requester_id, addressee_id, status, created_at, updated_at").
		ToSql()
	if err != nil {
		return nil, fmt.Errorf("build insert: %w", err)
	}

	row := r.pool.QueryRow(ctx, query, args...)
	req, err := scanRequest(row)
	if err != nil {
		if isUniqueViolation(err) {
			return nil, ErrAlreadyExists
		}
		return nil, fmt.Errorf("send friend request: %w", err)
	}
	return req, nil
}

// GetByID retrieves a single friend request by its primary key.
// Returns ErrNotFound if no row exists.
func (r *Repository) GetByID(ctx context.Context, id uuid.UUID) (*Request, error) {
	query, args, err := r.sb.
		Select("id, requester_id, addressee_id, status, created_at, updated_at").
		From("friend_requests").
		Where(sq.Eq{"id": id}).
		ToSql()
	if err != nil {
		return nil, fmt.Errorf("build select: %w", err)
	}

	row := r.pool.QueryRow(ctx, query, args...)
	req, err := scanRequest(row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("get friend request by id: %w", err)
	}
	return req, nil
}

// GetBetween retrieves the friend request between userA and userB regardless of direction.
// Returns ErrNotFound if no row exists.
func (r *Repository) GetBetween(ctx context.Context, userA, userB uuid.UUID) (*Request, error) {
	row := r.pool.QueryRow(ctx,
		`SELECT id, requester_id, addressee_id, status, created_at, updated_at
		 FROM friend_requests
		 WHERE (requester_id = $1 AND addressee_id = $2)
		    OR (requester_id = $2 AND addressee_id = $1)
		 LIMIT 1`,
		userA, userB,
	)
	req, err := scanRequest(row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("get friend request between users: %w", err)
	}
	return req, nil
}

// UpdateStatus changes the status of a friend request and returns the updated row.
func (r *Repository) UpdateStatus(ctx context.Context, id uuid.UUID, status Status) (*Request, error) {
	query, args, err := r.sb.
		Update("friend_requests").
		Set("status", status).
		Set("updated_at", sq.Expr("now()")).
		Where(sq.Eq{"id": id}).
		Suffix("RETURNING id, requester_id, addressee_id, status, created_at, updated_at").
		ToSql()
	if err != nil {
		return nil, fmt.Errorf("build update: %w", err)
	}

	row := r.pool.QueryRow(ctx, query, args...)
	req, err := scanRequest(row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("update friend request status: %w", err)
	}
	return req, nil
}

// Delete removes a friend request by ID.
// Returns ErrNotFound if no row was deleted.
func (r *Repository) Delete(ctx context.Context, id uuid.UUID) error {
	query, args, err := r.sb.
		Delete("friend_requests").
		Where(sq.Eq{"id": id}).
		ToSql()
	if err != nil {
		return fmt.Errorf("build delete: %w", err)
	}

	tag, err := r.pool.Exec(ctx, query, args...)
	if err != nil {
		return fmt.Errorf("delete friend request: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// DeleteBetween removes the friend request between userA and userB in either direction.
func (r *Repository) DeleteBetween(ctx context.Context, userA, userB uuid.UUID) error {
	_, err := r.pool.Exec(ctx,
		`DELETE FROM friend_requests
		 WHERE (requester_id = $1 AND addressee_id = $2)
		    OR (requester_id = $2 AND addressee_id = $1)`,
		userA, userB,
	)
	if err != nil {
		return fmt.Errorf("delete friend relationship: %w", err)
	}
	return nil
}

// ListIncoming returns all pending requests where the given user is the addressee,
// enriched with the requester's public profile, newest first.
func (r *Repository) ListIncoming(ctx context.Context, userID uuid.UUID) ([]RequestWithUser, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT fr.id, u.id, u.username, u.display_name, u.avatar_url, u.bio, u.country, u.created_at, fr.created_at
		 FROM friend_requests fr
		 JOIN users u ON u.id = fr.requester_id
		 WHERE fr.addressee_id = $1 AND fr.status = 'pending'
		 ORDER BY fr.created_at DESC`,
		userID,
	)
	if err != nil {
		return nil, fmt.Errorf("list incoming requests: %w", err)
	}
	defer rows.Close()

	return scanRequestsWithUser(rows)
}

// ListOutgoing returns all pending requests where the given user is the requester,
// enriched with the addressee's public profile, newest first.
func (r *Repository) ListOutgoing(ctx context.Context, userID uuid.UUID) ([]RequestWithUser, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT fr.id, u.id, u.username, u.display_name, u.avatar_url, u.bio, u.country, u.created_at, fr.created_at
		 FROM friend_requests fr
		 JOIN users u ON u.id = fr.addressee_id
		 WHERE fr.requester_id = $1 AND fr.status = 'pending'
		 ORDER BY fr.created_at DESC`,
		userID,
	)
	if err != nil {
		return nil, fmt.Errorf("list outgoing requests: %w", err)
	}
	defer rows.Close()

	return scanRequestsWithUser(rows)
}

// ListFriends returns all accepted friendships for the given user, newest first.
func (r *Repository) ListFriends(ctx context.Context, userID uuid.UUID) ([]Friend, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT fr.id, u.id, u.username, u.display_name, u.avatar_url, u.bio, u.country, u.created_at, fr.updated_at
		 FROM friend_requests fr
		 JOIN users u ON (fr.requester_id = $1 AND u.id = fr.addressee_id)
		                  OR (fr.addressee_id = $1 AND u.id = fr.requester_id)
		 WHERE fr.status = 'accepted'
		 ORDER BY fr.updated_at DESC`,
		userID,
	)
	if err != nil {
		return nil, fmt.Errorf("list friends: %w", err)
	}
	defer rows.Close()

	var friends []Friend
	for rows.Next() {
		var (
			f           Friend
			username    *string
			displayName *string
			avatarURL   *string
			bio         *string
			country     *string
		)
		if err := rows.Scan(
			&f.RequestID,
			&f.User.ID,
			&username, &displayName, &avatarURL, &bio, &country,
			&f.User.CreatedAt,
			&f.FriendsSince,
		); err != nil {
			return nil, fmt.Errorf("scan friend: %w", err)
		}
		if username != nil {
			f.User.Username = *username
		}
		if displayName != nil {
			f.User.DisplayName = *displayName
		}
		if avatarURL != nil {
			f.User.AvatarURL = *avatarURL
		}
		if bio != nil {
			f.User.Bio = *bio
		}
		if country != nil {
			f.User.Country = *country
		}
		friends = append(friends, f)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate friends: %w", err)
	}
	return friends, nil
}

// scanRequest scans a single friend request row.
func scanRequest(row interface{ Scan(...any) error }) (*Request, error) {
	var req Request
	if err := row.Scan(
		&req.ID,
		&req.RequesterID,
		&req.AddresseeID,
		&req.Status,
		&req.CreatedAt,
		&req.UpdatedAt,
	); err != nil {
		return nil, err
	}
	return &req, nil
}

// scanRequestsWithUser scans rows from a query that joins friend_requests with users.
// Expected columns: fr.id, u.id, u.username, u.display_name, u.avatar_url, u.bio, u.country, u.created_at, fr.created_at
func scanRequestsWithUser(rows pgx.Rows) ([]RequestWithUser, error) {
	var result []RequestWithUser
	for rows.Next() {
		var (
			rw          RequestWithUser
			username    *string
			displayName *string
			avatarURL   *string
			bio         *string
			country     *string
		)
		if err := rows.Scan(
			&rw.ID,
			&rw.User.ID,
			&username, &displayName, &avatarURL, &bio, &country,
			&rw.User.CreatedAt,
			&rw.CreatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan request with user: %w", err)
		}
		if username != nil {
			rw.User.Username = *username
		}
		if displayName != nil {
			rw.User.DisplayName = *displayName
		}
		if avatarURL != nil {
			rw.User.AvatarURL = *avatarURL
		}
		if bio != nil {
			rw.User.Bio = *bio
		}
		if country != nil {
			rw.User.Country = *country
		}
		// RequestWithUser.Status is always pending for incoming/outgoing lists.
		rw.Status = StatusPending
		result = append(result, rw)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate requests: %w", err)
	}
	return result, nil
}

// isUniqueViolation reports whether err is a PostgreSQL unique constraint violation.
func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}

// Ensure user.PublicProfile fields are used (import guard).
var _ user.PublicProfile
