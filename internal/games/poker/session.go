package poker

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/serediukit/bgex-backend/internal/games/engine"
)

// Session ties the poker Engine to its Postgres-backed state, implementing the
// realtime GameService seam and the lobby GameInitializer seam. Postgres is the
// source of truth: every mutation locks the state row FOR UPDATE.
type Session struct {
	pool *pgxpool.Pool
	repo *StateRepo
	eng  *Engine
}

// NewSession creates a poker Session.
func NewSession(pool *pgxpool.Pool, repo *StateRepo, eng *Engine) *Session {
	return &Session{pool: pool, repo: repo, eng: eng}
}

// GameKey identifies poker.
func (s *Session) GameKey() string { return GameKey }

// InitGameState persists the freshly-built initial state (lobby.GameInitializer).
func (s *Session) InitGameState(ctx context.Context, tx pgx.Tx, lobbyID uuid.UUID, state []byte, handNo int32) error {
	return s.repo.Insert(ctx, tx, lobbyID, state, handNo)
}

// View returns the redacted table view for a viewer.
func (s *Session) View(ctx context.Context, lobbyID, userID uuid.UUID) (any, error) {
	state, _, err := s.repo.Get(ctx, s.pool, lobbyID)
	if err != nil {
		return nil, err
	}
	return s.eng.View(state, userID)
}

// Apply runs one action atomically against the locked state.
func (s *Session) Apply(ctx context.Context, lobbyID uuid.UUID, action engine.Action) (handOver bool, err error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return false, fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	state, handNo, _, err := s.repo.GetForUpdate(ctx, tx, lobbyID)
	if err != nil {
		return false, err
	}
	next, _, err := s.eng.Apply(state, action)
	if err != nil {
		return false, err // validation error surfaces to the client
	}
	if err := s.repo.Update(ctx, tx, lobbyID, next, handNo); err != nil {
		return false, err
	}
	over := s.eng.IsHandOver(next)
	if over {
		if err := s.repo.InsertHistory(ctx, tx, lobbyID, handNo, next); err != nil {
			return false, err
		}
	}
	if err := tx.Commit(ctx); err != nil {
		return false, fmt.Errorf("commit tx: %w", err)
	}
	return over, nil
}

// NextHand deals the next hand once the current one is over. It returns
// finished=true when the table can no longer field enough players.
func (s *Session) NextHand(ctx context.Context, lobbyID uuid.UUID) (finished bool, err error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return false, fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	state, handNo, _, err := s.repo.GetForUpdate(ctx, tx, lobbyID)
	if err != nil {
		return false, err
	}
	if !s.eng.IsHandOver(state) {
		return false, nil // already advanced by a concurrent trigger
	}
	next, _, err := s.eng.NextHand(state)
	if err != nil {
		if errors.Is(err, engine.ErrNotEnoughPlayers) {
			return true, nil
		}
		return false, err
	}
	if err := s.repo.Update(ctx, tx, lobbyID, next, handNo+1); err != nil {
		return false, err
	}
	if err := tx.Commit(ctx); err != nil {
		return false, fmt.Errorf("commit tx: %w", err)
	}
	return false, nil
}
