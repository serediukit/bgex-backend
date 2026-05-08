package auth

import (
	"context"
	"errors"
	"fmt"

	sq "github.com/Masterminds/squirrel"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// ErrRefreshTokenInvalid is returned when a refresh token is unknown, expired, or revoked.
var ErrRefreshTokenInvalid = errors.New("refresh token invalid")

type Repository struct {
	pool *pgxpool.Pool
	sb   sq.StatementBuilderType
}

func NewRepository(pool *pgxpool.Pool) *Repository {
	return &Repository{
		pool: pool,
		sb:   sq.StatementBuilder.PlaceholderFormat(sq.Dollar),
	}
}

// FindOAuthIdentity returns the user_id associated with an external provider identity.
func (r *Repository) FindOAuthIdentity(ctx context.Context, tx pgx.Tx, provider, providerUserID string) (uuid.UUID, error) {
	var userID uuid.UUID
	err := tx.QueryRow(ctx,
		`SELECT user_id FROM oauth_identities WHERE provider = $1 AND provider_user_id = $2`,
		provider, providerUserID,
	).Scan(&userID)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return uuid.Nil, pgx.ErrNoRows
		}
		return uuid.Nil, err
	}
	return userID, nil
}

// LinkOAuthIdentity inserts a new oauth_identity row for the given user.
func (r *Repository) LinkOAuthIdentity(ctx context.Context, tx pgx.Tx, userID uuid.UUID, provider, providerUserID string) error {
	_, err := tx.Exec(ctx,
		`INSERT INTO oauth_identities (user_id, provider, provider_user_id) VALUES ($1, $2, $3)`,
		userID, provider, providerUserID,
	)
	return err
}

// FindUserIDByEmail looks up a user id by email inside a transaction.
func (r *Repository) FindUserIDByEmail(ctx context.Context, tx pgx.Tx, email string) (uuid.UUID, error) {
	var id uuid.UUID
	err := tx.QueryRow(ctx, `SELECT id FROM users WHERE email = $1`, email).Scan(&id)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return uuid.Nil, pgx.ErrNoRows
		}
		return uuid.Nil, err
	}
	return id, nil
}

// WithTx begins a transaction, invokes fn, and commits or rolls back based on fn's return.
func (r *Repository) WithTx(ctx context.Context, fn func(pgx.Tx) error) error {
	tx, err := r.pool.BeginTx(ctx, pgx.TxOptions{})
	if err != nil {
		return fmt.Errorf("begin tx: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if err := fn(tx); err != nil {
		return err
	}
	return tx.Commit(ctx)
}
