package user

import (
	"context"
	"errors"
	"fmt"

	sq "github.com/Masterminds/squirrel"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

var (
	ErrNotFound      = errors.New("user not found")
	ErrEmailTaken    = errors.New("email already in use")
	ErrUsernameTaken = errors.New("username already in use")
)

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

// userColumns must match scanRow's Scan call exactly.
const userColumns = "id, email, password_hash, username, display_name, avatar_url, bio, country, role, email_verified, created_at, updated_at"

func (r *Repository) GetByID(ctx context.Context, id uuid.UUID) (*User, error) {
	query, args, err := r.sb.Select(userColumns).From("users").Where(sq.Eq{"id": id}).ToSql()
	if err != nil {
		return nil, fmt.Errorf("build select: %w", err)
	}
	return r.scanOne(ctx, query, args)
}

func (r *Repository) GetByEmail(ctx context.Context, email string) (*User, error) {
	query, args, err := r.sb.Select(userColumns).From("users").Where(sq.Eq{"email": email}).ToSql()
	if err != nil {
		return nil, fmt.Errorf("build select: %w", err)
	}
	return r.scanOne(ctx, query, args)
}

func (r *Repository) GetByUsername(ctx context.Context, username string) (*User, error) {
	query, args, err := r.sb.Select(userColumns).From("users").Where(sq.Eq{"username": username}).ToSql()
	if err != nil {
		return nil, fmt.Errorf("build select: %w", err)
	}
	return r.scanOne(ctx, query, args)
}

func (r *Repository) CreateWithPassword(ctx context.Context, creds Credentials) (*User, error) {
	query, args, err := r.sb.
		Insert("users").
		Columns("email", "password_hash", "display_name").
		Values(creds.Email, creds.PasswordHash, nullString(creds.DisplayName)).
		Suffix("RETURNING " + userColumns).
		ToSql()
	if err != nil {
		return nil, fmt.Errorf("build insert: %w", err)
	}
	u, err := r.scanOne(ctx, query, args)
	if err != nil {
		return nil, mapUniqueViolation(err)
	}
	return u, nil
}

func (r *Repository) CreateFromOAuth(ctx context.Context, tx pgx.Tx, p OAuthProfile) (*User, error) {
	query, args, err := r.sb.
		Insert("users").
		Columns("email", "display_name", "avatar_url", "email_verified").
		Values(p.Email, nullString(p.DisplayName), nullString(p.AvatarURL), p.EmailVerified).
		Suffix("RETURNING " + userColumns).
		ToSql()
	if err != nil {
		return nil, fmt.Errorf("build insert: %w", err)
	}
	row := tx.QueryRow(ctx, query, args...)
	u, err := scanRow(row)
	if err != nil {
		return nil, mapUniqueViolation(err)
	}
	return u, nil
}

// Update applies non-nil fields from params and returns the updated user.
func (r *Repository) Update(ctx context.Context, id uuid.UUID, params UpdateParams) (*User, error) {
	q := r.sb.Update("users").Where(sq.Eq{"id": id}).Suffix("RETURNING " + userColumns)

	if params.Username != nil {
		q = q.Set("username", nullString(*params.Username))
	}
	if params.DisplayName != nil {
		q = q.Set("display_name", nullString(*params.DisplayName))
	}
	if params.AvatarURL != nil {
		q = q.Set("avatar_url", nullString(*params.AvatarURL))
	}
	if params.Bio != nil {
		q = q.Set("bio", nullString(*params.Bio))
	}
	if params.Country != nil {
		q = q.Set("country", nullString(*params.Country))
	}
	q = q.Set("updated_at", sq.Expr("now()"))

	query, args, err := q.ToSql()
	if err != nil {
		return nil, fmt.Errorf("build update: %w", err)
	}
	u, err := r.scanOne(ctx, query, args)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return nil, ErrNotFound
		}
		return nil, mapUniqueViolation(err)
	}
	return u, nil
}

func (r *Repository) GetPasswordHash(ctx context.Context, id uuid.UUID) (string, error) {
	var hash *string
	err := r.pool.QueryRow(ctx, `SELECT password_hash FROM users WHERE id = $1`, id).Scan(&hash)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return "", ErrNotFound
		}
		return "", err
	}
	if hash == nil {
		return "", nil
	}
	return *hash, nil
}

// GetRole returns a user's role ('user' or 'admin').
func (r *Repository) GetRole(ctx context.Context, id uuid.UUID) (string, error) {
	var role string
	err := r.pool.QueryRow(ctx, `SELECT role FROM users WHERE id = $1`, id).Scan(&role)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return "", ErrNotFound
		}
		return "", err
	}
	return role, nil
}

func (r *Repository) scanOne(ctx context.Context, query string, args []any) (*User, error) {
	row := r.pool.QueryRow(ctx, query, args...)
	u, err := scanRow(row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return u, nil
}

type rowScanner interface {
	Scan(dest ...any) error
}

func scanRow(row rowScanner) (*User, error) {
	var (
		u            User
		passwordHash *string
		username     *string
		displayName  *string
		avatarURL    *string
		bio          *string
		country      *string
	)
	if err := row.Scan(
		&u.ID, &u.Email, &passwordHash,
		&username, &displayName, &avatarURL, &bio, &country, &u.Role,
		&u.EmailVerified, &u.CreatedAt, &u.UpdatedAt,
	); err != nil {
		return nil, err
	}
	u.HasPassword = passwordHash != nil && *passwordHash != ""
	if username != nil {
		u.Username = *username
	}
	if displayName != nil {
		u.DisplayName = *displayName
	}
	if avatarURL != nil {
		u.AvatarURL = *avatarURL
	}
	if bio != nil {
		u.Bio = *bio
	}
	if country != nil {
		u.Country = *country
	}
	return &u, nil
}

func nullString(s string) any {
	if s == "" {
		return nil
	}
	return s
}
