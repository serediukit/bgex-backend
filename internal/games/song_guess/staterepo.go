package song_guess

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// ErrStateNotFound is returned when no song_guess state exists for a lobby.
var ErrStateNotFound = errors.New("song_guess game state not found")

// Querier is satisfied by both *pgxpool.Pool and pgx.Tx, letting repo methods
// run either standalone or inside a caller-managed transaction. Mirrors
// ttr.Querier exactly.
type Querier interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

// StateRepo persists song_guess hot state to the `song_guess` schema.
// Postgres is the source of truth: mutations lock game_states FOR UPDATE via
// GetForUpdate.
type StateRepo struct{}

// NewStateRepo returns a StateRepo.
func NewStateRepo() *StateRepo { return &StateRepo{} }

// ResultRow is one participant's final scoring breakdown, persisted to
// song_guess.game_results. UserID is nil if the seat's user account was
// deleted (game_results.user_id is ON DELETE SET NULL). Team is the wire form
// ("a" | "b" | "none") — game_results.team is a CHECK-constrained TEXT
// column, not an enum, so the caller (Session) converts pb.Team via
// teamWire before building this row. One row is written per participant,
// judge included (Req 36) — the judge's Team is "none" and Total/Rank are
// left at their pb.ResultEntry zero values.
type ResultRow struct {
	SeatIndex int32
	UserID    *uuid.UUID
	Team      string
	Total     int32
	Rank      int32
	IsJudge   bool
	Breakdown []byte // JSON
}

// Insert creates the initial state row for a lobby, pinning (packID,
// packVersion) — enforced by the song_guess.game_states ->pack_versions
// composite FK, so a state row can never reference an unpublished or
// nonexistent pack version.
func (r *StateRepo) Insert(ctx context.Context, q Querier, lobbyID, packID uuid.UUID, packVersion int32, state []byte) error {
	_, err := q.Exec(ctx,
		`INSERT INTO song_guess.game_states (lobby_id, pack_id, pack_version, state, version)
		 VALUES ($1, $2, $3, $4, 0)`,
		lobbyID, packID, packVersion, state,
	)
	if err != nil {
		return fmt.Errorf("insert song_guess state: %w", err)
	}
	return nil
}

// GetPackPin reads a lobby's pinned (pack_id, pack_version) without locking
// the game_states row. (pack_id, pack_version) is written once by Insert and
// never changes afterward (a lobby pins its pack at Start), so this is safe
// to read outside a transaction. Session.Apply uses this to warm the pack
// cache *before* opening the tx that takes GetForUpdate's row lock, so a cold
// cache never forces a second pool connection acquisition while the first is
// held open on a locked row (the ttr platform review's M1 finding, mirrored
// here from ttr.StateRepo.GetMapPin).
func (r *StateRepo) GetPackPin(ctx context.Context, q Querier, lobbyID uuid.UUID) (packID string, packVersion int32, err error) {
	row := q.QueryRow(ctx, `SELECT pack_id, pack_version FROM song_guess.game_states WHERE lobby_id = $1`, lobbyID)
	if err = row.Scan(&packID, &packVersion); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return "", 0, ErrStateNotFound
		}
		return "", 0, fmt.Errorf("get song_guess pack pin: %w", err)
	}
	return packID, packVersion, nil
}

// GetForUpdate reads and row-locks the state for a lobby. Must run inside a
// tx. packID is returned as a string (matching PackProvider/PackLoader's
// signature) rather than uuid.UUID.
func (r *StateRepo) GetForUpdate(ctx context.Context, tx pgx.Tx, lobbyID uuid.UUID) (state []byte, packID string, packVersion, version int32, err error) {
	row := tx.QueryRow(ctx,
		`SELECT state, pack_id, pack_version, version FROM song_guess.game_states WHERE lobby_id = $1 FOR UPDATE`,
		lobbyID,
	)
	if err = row.Scan(&state, &packID, &packVersion, &version); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, "", 0, 0, ErrStateNotFound
		}
		return nil, "", 0, 0, fmt.Errorf("get song_guess state for update: %w", err)
	}
	return state, packID, packVersion, version, nil
}

// Get reads the current state without locking (for read-only views).
func (r *StateRepo) Get(ctx context.Context, q Querier, lobbyID uuid.UUID) (state []byte, packID string, packVersion int32, err error) {
	row := q.QueryRow(ctx,
		`SELECT state, pack_id, pack_version FROM song_guess.game_states WHERE lobby_id = $1`,
		lobbyID,
	)
	if err = row.Scan(&state, &packID, &packVersion); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, "", 0, ErrStateNotFound
		}
		return nil, "", 0, fmt.Errorf("get song_guess state: %w", err)
	}
	return state, packID, packVersion, nil
}

// Update writes new state, bumping the version optimistically.
func (r *StateRepo) Update(ctx context.Context, tx pgx.Tx, lobbyID uuid.UUID, state []byte) error {
	_, err := tx.Exec(ctx,
		`UPDATE song_guess.game_states SET state = $2, version = version + 1, updated_at = now() WHERE lobby_id = $1`,
		lobbyID, state,
	)
	if err != nil {
		return fmt.Errorf("update song_guess state: %w", err)
	}
	return nil
}

// AppendLog records one applied action for replay/debug. seq must be the
// engine's post-Apply gs.Seq (monotonic per lobby, bumped once per
// successful Apply call) to satisfy the (lobby_id, seq) primary key.
func (r *StateRepo) AppendLog(ctx context.Context, tx pgx.Tx, lobbyID, userID uuid.UUID, seq int64, action []byte) error {
	_, err := tx.Exec(ctx,
		`INSERT INTO song_guess.action_log (lobby_id, seq, user_id, action) VALUES ($1, $2, $3, $4)`,
		lobbyID, seq, userID, action,
	)
	if err != nil {
		return fmt.Errorf("append song_guess action log: %w", err)
	}
	return nil
}

// InsertResults persists the final per-participant scoring breakdown, one row
// per participant (players and the judge, Req 36).
func (r *StateRepo) InsertResults(ctx context.Context, tx pgx.Tx, lobbyID uuid.UUID, rows []ResultRow) error {
	for _, row := range rows {
		_, err := tx.Exec(ctx,
			`INSERT INTO song_guess.game_results (lobby_id, seat_index, user_id, team, total, rank, is_judge, breakdown)
			 VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`,
			lobbyID, row.SeatIndex, row.UserID, row.Team, row.Total, row.Rank, row.IsJudge, row.Breakdown,
		)
		if err != nil {
			return fmt.Errorf("insert song_guess game result seat %d: %w", row.SeatIndex, err)
		}
	}
	return nil
}
