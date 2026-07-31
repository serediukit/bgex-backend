package poker

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// ErrStateNotFound is returned when no poker state exists for a lobby.
var ErrStateNotFound = errors.New("poker game state not found")

// Querier is satisfied by both *pgxpool.Pool and pgx.Tx, letting repo methods
// run either standalone or inside a caller-managed transaction.
type Querier interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

// StateRepo persists poker hand state to the `poker` schema. Postgres is the
// source of truth: mutations lock game_states FOR UPDATE via GetForUpdate.
type StateRepo struct{}

// NewStateRepo returns a StateRepo.
func NewStateRepo() *StateRepo { return &StateRepo{} }

// Insert creates the initial state row for a lobby.
func (r *StateRepo) Insert(ctx context.Context, q Querier, lobbyID uuid.UUID, state []byte, handNo int32) error {
	_, err := q.Exec(ctx,
		`INSERT INTO poker.game_states (lobby_id, state, hand_no, version)
		 VALUES ($1, $2, $3, 0)`,
		lobbyID, state, handNo,
	)
	if err != nil {
		return fmt.Errorf("insert poker state: %w", err)
	}
	return nil
}

// GetForUpdate reads and row-locks the state for a lobby. Must run inside a tx.
func (r *StateRepo) GetForUpdate(ctx context.Context, tx pgx.Tx, lobbyID uuid.UUID) (state []byte, handNo, version int32, err error) {
	row := tx.QueryRow(ctx,
		`SELECT state, hand_no, version FROM poker.game_states WHERE lobby_id = $1 FOR UPDATE`,
		lobbyID,
	)
	if err = row.Scan(&state, &handNo, &version); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, 0, 0, ErrStateNotFound
		}
		return nil, 0, 0, fmt.Errorf("get poker state for update: %w", err)
	}
	return state, handNo, version, nil
}

// Get reads the current state without locking (for read-only views).
func (r *StateRepo) Get(ctx context.Context, q Querier, lobbyID uuid.UUID) (state []byte, handNo int32, err error) {
	row := q.QueryRow(ctx,
		`SELECT state, hand_no FROM poker.game_states WHERE lobby_id = $1`,
		lobbyID,
	)
	if err = row.Scan(&state, &handNo); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, 0, ErrStateNotFound
		}
		return nil, 0, fmt.Errorf("get poker state: %w", err)
	}
	return state, handNo, nil
}

// Update writes new state, bumping the version optimistically.
func (r *StateRepo) Update(ctx context.Context, tx pgx.Tx, lobbyID uuid.UUID, state []byte, handNo int32) error {
	_, err := tx.Exec(ctx,
		`UPDATE poker.game_states
		 SET state = $2, hand_no = $3, version = version + 1, updated_at = now()
		 WHERE lobby_id = $1`,
		lobbyID, state, handNo,
	)
	if err != nil {
		return fmt.Errorf("update poker state: %w", err)
	}
	return nil
}

// InsertHistory archives a completed hand's final state.
func (r *StateRepo) InsertHistory(ctx context.Context, q Querier, lobbyID uuid.UUID, handNo int32, result []byte) error {
	_, err := q.Exec(ctx,
		`INSERT INTO poker.hand_history (lobby_id, hand_no, result) VALUES ($1, $2, $3)`,
		lobbyID, handNo, result,
	)
	if err != nil {
		return fmt.Errorf("insert poker hand history: %w", err)
	}
	return nil
}
