package lobby

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Index names from migration 0004 — used to distinguish unique violations.
const (
	idxOneActivePerUser = "game_seats_one_active_per_user_idx"
	idxLobbySeat        = "game_seats_lobby_seat_idx"
)

// execer is satisfied by both *pgxpool.Pool and pgx.Tx.
type execer interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

// Repository handles persistence for lobbies and seats (default schema).
type Repository struct {
	pool *pgxpool.Pool
}

// NewRepository creates a Repository over the given pool.
func NewRepository(pool *pgxpool.Pool) *Repository { return &Repository{pool: pool} }

// Pool exposes the underlying pool for callers that need to manage a tx.
func (r *Repository) Pool() *pgxpool.Pool { return r.pool }

// InsertLobby creates a lobby row and returns it (without seats).
func (r *Repository) InsertLobby(ctx context.Context, q execer, gameKey, name string, hostID uuid.UUID, maxSeats int) (*Lobby, error) {
	var l Lobby
	row := q.QueryRow(ctx,
		`INSERT INTO game_lobbies (game_key, name, host_id, max_seats)
		 VALUES ($1, $2, $3, $4)
		 RETURNING id, game_key, name, host_id, status, max_seats, created_at, updated_at`,
		gameKey, name, hostID, maxSeats,
	)
	if err := row.Scan(&l.ID, &l.GameKey, &l.Name, &l.HostID, &l.Status, &l.MaxSeats, &l.CreatedAt, &l.UpdatedAt); err != nil {
		return nil, fmt.Errorf("insert lobby: %w", err)
	}
	return &l, nil
}

// ClaimSeat inserts an active seat. It maps unique-index violations to the
// matching domain error: the platform-wide one-active-seat rule, or a taken
// physical seat.
func (r *Repository) ClaimSeat(ctx context.Context, q execer, lobbyID, userID uuid.UUID, seatIndex int, stack int64) error {
	_, err := q.Exec(ctx,
		`INSERT INTO game_seats (lobby_id, seat_index, user_id, status, stack)
		 VALUES ($1, $2, $3, 'active', $4)`,
		lobbyID, seatIndex, userID, stack,
	)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			switch pgErr.ConstraintName {
			case idxOneActivePerUser:
				return ErrAlreadySeated
			case idxLobbySeat:
				return ErrSeatTaken
			}
		}
		return fmt.Errorf("claim seat: %w", err)
	}
	return nil
}

// LeaveSeat marks a user's active seat in a lobby as left and returns how many
// active seats remain.
func (r *Repository) LeaveSeat(ctx context.Context, lobbyID, userID uuid.UUID) (remaining int, err error) {
	tag, err := r.pool.Exec(ctx,
		`UPDATE game_seats SET status = 'left'
		 WHERE lobby_id = $1 AND user_id = $2 AND status = 'active'`,
		lobbyID, userID,
	)
	if err != nil {
		return 0, fmt.Errorf("leave seat: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return 0, ErrNotSeated
	}
	row := r.pool.QueryRow(ctx,
		`SELECT COUNT(*) FROM game_seats WHERE lobby_id = $1 AND status = 'active'`,
		lobbyID,
	)
	if err := row.Scan(&remaining); err != nil {
		return 0, fmt.Errorf("count remaining seats: %w", err)
	}
	return remaining, nil
}

// UpdateStatus sets a lobby's status.
func (r *Repository) UpdateStatus(ctx context.Context, q execer, lobbyID uuid.UUID, status string) error {
	_, err := q.Exec(ctx,
		`UPDATE game_lobbies SET status = $2, updated_at = now() WHERE id = $1`,
		lobbyID, status,
	)
	if err != nil {
		return fmt.Errorf("update lobby status: %w", err)
	}
	return nil
}

// SetHost reassigns a lobby's host.
func (r *Repository) SetHost(ctx context.Context, lobbyID, hostID uuid.UUID) error {
	_, err := r.pool.Exec(ctx,
		`UPDATE game_lobbies SET host_id = $2, updated_at = now() WHERE id = $1`,
		lobbyID, hostID,
	)
	if err != nil {
		return fmt.Errorf("set host: %w", err)
	}
	return nil
}

// Get loads a lobby with its active seats (enriched with player profiles).
func (r *Repository) Get(ctx context.Context, lobbyID uuid.UUID) (*Lobby, error) {
	var l Lobby
	row := r.pool.QueryRow(ctx,
		`SELECT id, game_key, name, host_id, status, max_seats, created_at, updated_at
		 FROM game_lobbies WHERE id = $1`,
		lobbyID,
	)
	if err := row.Scan(&l.ID, &l.GameKey, &l.Name, &l.HostID, &l.Status, &l.MaxSeats, &l.CreatedAt, &l.UpdatedAt); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("get lobby: %w", err)
	}

	rows, err := r.pool.Query(ctx,
		`SELECT s.seat_index, s.user_id, s.stack, s.status,
		        u.username, u.display_name, u.avatar_url
		 FROM game_seats s
		 JOIN users u ON u.id = s.user_id
		 WHERE s.lobby_id = $1 AND s.status = 'active'
		 ORDER BY s.seat_index`,
		lobbyID,
	)
	if err != nil {
		return nil, fmt.Errorf("get lobby seats: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var (
			s                              Seat
			username, displayName, avatar  *string
		)
		if err := rows.Scan(&s.SeatIndex, &s.UserID, &s.Stack, &s.Status, &username, &displayName, &avatar); err != nil {
			return nil, fmt.Errorf("scan seat: %w", err)
		}
		if username != nil {
			s.Username = *username
		}
		if displayName != nil {
			s.DisplayName = *displayName
		}
		if avatar != nil {
			s.AvatarURL = *avatar
		}
		l.Seats = append(l.Seats, s)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate seats: %w", err)
	}
	if l.Seats == nil {
		l.Seats = []Seat{} // always serialize seats as a JSON array, never null
	}
	return &l, nil
}

// List returns open lobbies for a game (waiting or in progress), newest first.
func (r *Repository) List(ctx context.Context, gameKey string) ([]ListItem, error) {
	rows, err := r.pool.Query(ctx,
		`SELECT l.id, l.game_key, l.name, l.status, l.host_id, l.max_seats, l.created_at,
		        COUNT(s.id) FILTER (WHERE s.status = 'active') AS seat_count
		 FROM game_lobbies l
		 LEFT JOIN game_seats s ON s.lobby_id = l.id
		 WHERE l.game_key = $1 AND l.status IN ('waiting', 'in_progress')
		 GROUP BY l.id
		 ORDER BY l.created_at DESC`,
		gameKey,
	)
	if err != nil {
		return nil, fmt.Errorf("list lobbies: %w", err)
	}
	defer rows.Close()

	var items []ListItem
	for rows.Next() {
		var it ListItem
		if err := rows.Scan(&it.ID, &it.GameKey, &it.Name, &it.Status, &it.HostID, &it.MaxSeats, &it.CreatedAt, &it.SeatCount); err != nil {
			return nil, fmt.Errorf("scan lobby: %w", err)
		}
		items = append(items, it)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate lobbies: %w", err)
	}
	return items, nil
}

// CurrentLobby returns the lobby id of the user's active seat, if any.
func (r *Repository) CurrentLobby(ctx context.Context, userID uuid.UUID) (uuid.UUID, bool, error) {
	var id uuid.UUID
	row := r.pool.QueryRow(ctx,
		`SELECT lobby_id FROM game_seats WHERE user_id = $1 AND status = 'active' LIMIT 1`,
		userID,
	)
	if err := row.Scan(&id); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return uuid.Nil, false, nil
		}
		return uuid.Nil, false, fmt.Errorf("current lobby: %w", err)
	}
	return id, true, nil
}
