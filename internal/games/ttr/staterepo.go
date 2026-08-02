package ttr

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// ErrStateNotFound is returned when no TTR state exists for a lobby.
var ErrStateNotFound = errors.New("ttr game state not found")

// Querier is satisfied by both *pgxpool.Pool and pgx.Tx, letting repo methods
// run either standalone or inside a caller-managed transaction.
type Querier interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

// StateRepo persists TTR hot state to the `ttr` schema. Postgres is the
// source of truth: mutations lock game_states FOR UPDATE via GetForUpdate.
type StateRepo struct{}

// NewStateRepo returns a StateRepo.
func NewStateRepo() *StateRepo { return &StateRepo{} }

// ResultRow is one seat's final scoring breakdown, persisted to
// ttr.game_results. UserID is nil if the seat's user account was deleted
// (game_results.user_id is ON DELETE SET NULL).
type ResultRow struct {
	SeatIndex int32
	UserID    *uuid.UUID
	Total     int32
	Rank      int32
	Breakdown []byte // JSON
}

// Insert creates the initial state row for a lobby, pinning (mapID,
// mapVersion) — enforced by the ttr.game_states -> ttr.map_versions
// composite FK, so a state row can never reference an unpublished or
// nonexistent map version.
func (r *StateRepo) Insert(ctx context.Context, q Querier, lobbyID, mapID uuid.UUID, mapVersion int32, state []byte) error {
	_, err := q.Exec(ctx,
		`INSERT INTO ttr.game_states (lobby_id, map_id, map_version, state, version)
		 VALUES ($1, $2, $3, $4, 0)`,
		lobbyID, mapID, mapVersion, state,
	)
	if err != nil {
		return fmt.Errorf("insert ttr state: %w", err)
	}
	return nil
}

// GetMapPin reads a lobby's pinned (map_id, map_version) without locking the
// game_states row. (map_id, map_version) is written once by Insert and never
// changes afterward (a lobby pins its map at Start), so this is safe to read
// outside a transaction. Session.Apply uses this to warm the map cache
// *before* opening the tx that takes GetForUpdate's row lock, so a cold cache
// never forces a second pool connection acquisition while the first is held
// open on a locked row (see the ttr platform review's M1 finding).
func (r *StateRepo) GetMapPin(ctx context.Context, q Querier, lobbyID uuid.UUID) (mapID string, mapVersion int32, err error) {
	row := q.QueryRow(ctx, `SELECT map_id, map_version FROM ttr.game_states WHERE lobby_id = $1`, lobbyID)
	if err = row.Scan(&mapID, &mapVersion); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return "", 0, ErrStateNotFound
		}
		return "", 0, fmt.Errorf("get ttr map pin: %w", err)
	}
	return mapID, mapVersion, nil
}

// GetForUpdate reads and row-locks the state for a lobby. Must run inside a
// tx. mapID is returned as a string (matching MapProvider/MapLoader's
// signature) rather than uuid.UUID.
func (r *StateRepo) GetForUpdate(ctx context.Context, tx pgx.Tx, lobbyID uuid.UUID) (state []byte, mapID string, mapVersion, version int32, err error) {
	row := tx.QueryRow(ctx,
		`SELECT state, map_id, map_version, version FROM ttr.game_states WHERE lobby_id = $1 FOR UPDATE`,
		lobbyID,
	)
	if err = row.Scan(&state, &mapID, &mapVersion, &version); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, "", 0, 0, ErrStateNotFound
		}
		return nil, "", 0, 0, fmt.Errorf("get ttr state for update: %w", err)
	}
	return state, mapID, mapVersion, version, nil
}

// Get reads the current state without locking (for read-only views).
func (r *StateRepo) Get(ctx context.Context, q Querier, lobbyID uuid.UUID) (state []byte, mapID string, mapVersion int32, err error) {
	row := q.QueryRow(ctx,
		`SELECT state, map_id, map_version FROM ttr.game_states WHERE lobby_id = $1`,
		lobbyID,
	)
	if err = row.Scan(&state, &mapID, &mapVersion); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, "", 0, ErrStateNotFound
		}
		return nil, "", 0, fmt.Errorf("get ttr state: %w", err)
	}
	return state, mapID, mapVersion, nil
}

// Update writes new state, bumping the version optimistically.
func (r *StateRepo) Update(ctx context.Context, tx pgx.Tx, lobbyID uuid.UUID, state []byte) error {
	_, err := tx.Exec(ctx,
		`UPDATE ttr.game_states SET state = $2, version = version + 1, updated_at = now() WHERE lobby_id = $1`,
		lobbyID, state,
	)
	if err != nil {
		return fmt.Errorf("update ttr state: %w", err)
	}
	return nil
}

// AppendLog records one applied action for replay/debug. seq must be the
// engine's post-Apply gs.Seq (monotonic per lobby, bumped once per
// successful Apply call) to satisfy the (lobby_id, seq) primary key.
func (r *StateRepo) AppendLog(ctx context.Context, tx pgx.Tx, lobbyID, userID uuid.UUID, seq int64, action []byte) error {
	_, err := tx.Exec(ctx,
		`INSERT INTO ttr.action_log (lobby_id, seq, user_id, action) VALUES ($1, $2, $3, $4)`,
		lobbyID, seq, userID, action,
	)
	if err != nil {
		return fmt.Errorf("append ttr action log: %w", err)
	}
	return nil
}

// InsertResults persists the final per-seat scoring breakdown, one row per
// player.
func (r *StateRepo) InsertResults(ctx context.Context, tx pgx.Tx, lobbyID uuid.UUID, rows []ResultRow) error {
	for _, row := range rows {
		_, err := tx.Exec(ctx,
			`INSERT INTO ttr.game_results (lobby_id, seat_index, user_id, total, rank, breakdown)
			 VALUES ($1, $2, $3, $4, $5, $6)`,
			lobbyID, row.SeatIndex, row.UserID, row.Total, row.Rank, row.Breakdown,
		)
		if err != nil {
			return fmt.Errorf("insert ttr game result seat %d: %w", row.SeatIndex, err)
		}
	}
	return nil
}
