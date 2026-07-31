package lobby

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/serediukit/bgex-backend/internal/games/engine"
)

// GameInitializer persists a game's freshly-created initial state inside the
// same transaction that flips the lobby to in_progress. Each game provides an
// implementation (poker writes to its own schema); the lobby layer stays
// game-agnostic and only knows this interface.
type GameInitializer interface {
	InitGameState(ctx context.Context, tx pgx.Tx, lobbyID uuid.UUID, state []byte, handNo int32) error
}

// Service holds the lobby business logic.
type Service struct {
	repo         *Repository
	engines      *engine.Registry
	initializers map[string]GameInitializer
}

// NewService wires the lobby service. initializers is keyed by game key.
func NewService(repo *Repository, engines *engine.Registry, initializers map[string]GameInitializer) *Service {
	return &Service{repo: repo, engines: engines, initializers: initializers}
}

// Create makes a new lobby and seats the host at seat 0, all in one tx. Fails
// with ErrAlreadySeated if the host already occupies a seat elsewhere.
func (s *Service) Create(ctx context.Context, hostID uuid.UUID, gameKey, name string, maxSeats int) (*Lobby, error) {
	eng, ok := s.engines.Get(gameKey)
	if !ok {
		return nil, ErrUnknownGame
	}
	if maxSeats < eng.MinSeats() || maxSeats > eng.MaxSeats() {
		maxSeats = eng.MaxSeats()
	}
	if name == "" {
		name = "New table"
	}

	tx, err := s.repo.Pool().Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	created, err := s.repo.InsertLobby(ctx, tx, gameKey, name, hostID, maxSeats)
	if err != nil {
		return nil, err
	}
	if err := s.repo.ClaimSeat(ctx, tx, created.ID, hostID, 0, eng.DefaultBuyIn()); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit tx: %w", err)
	}
	return s.repo.Get(ctx, created.ID)
}

// List returns open lobbies for a game.
func (s *Service) List(ctx context.Context, gameKey string) ([]ListItem, error) {
	if !s.engines.Has(gameKey) {
		return nil, ErrUnknownGame
	}
	return s.repo.List(ctx, gameKey)
}

// Get returns a lobby with its seats.
func (s *Service) Get(ctx context.Context, lobbyID uuid.UUID) (*Lobby, error) {
	return s.repo.Get(ctx, lobbyID)
}

// Join seats a user in a waiting lobby at the requested seat index.
func (s *Service) Join(ctx context.Context, userID, lobbyID uuid.UUID, seatIndex int) (*Lobby, error) {
	lob, err := s.repo.Get(ctx, lobbyID)
	if err != nil {
		return nil, err
	}
	if lob.Status != "waiting" {
		return nil, ErrNotWaiting
	}
	if seatIndex < 0 || seatIndex >= lob.MaxSeats {
		return nil, ErrInvalidSeat
	}
	if len(lob.Seats) >= lob.MaxSeats {
		return nil, ErrLobbyFull
	}
	eng, ok := s.engines.Get(lob.GameKey)
	if !ok {
		return nil, ErrUnknownGame
	}
	if err := s.repo.ClaimSeat(ctx, s.repo.Pool(), lobbyID, userID, seatIndex, eng.DefaultBuyIn()); err != nil {
		return nil, err
	}
	return s.repo.Get(ctx, lobbyID)
}

// Leave removes a user from a lobby. If the lobby empties it is finished; if the
// host leaves while others remain, the earliest remaining seat becomes host.
func (s *Service) Leave(ctx context.Context, userID, lobbyID uuid.UUID) error {
	lob, err := s.repo.Get(ctx, lobbyID)
	if err != nil {
		return err
	}
	remaining, err := s.repo.LeaveSeat(ctx, lobbyID, userID)
	if err != nil {
		return err
	}
	if remaining == 0 {
		return s.repo.UpdateStatus(ctx, s.repo.Pool(), lobbyID, "finished")
	}
	if lob.HostID == userID {
		fresh, err := s.repo.Get(ctx, lobbyID)
		if err != nil {
			return err
		}
		if len(fresh.Seats) > 0 {
			return s.repo.SetHost(ctx, lobbyID, fresh.Seats[0].UserID)
		}
	}
	return nil
}

// Start begins the game: only the host may start, only from `waiting`, only
// with enough players. It calls the engine to build the initial state and
// persists it alongside the status flip in a single transaction.
func (s *Service) Start(ctx context.Context, userID, lobbyID uuid.UUID) (*Lobby, error) {
	lob, err := s.repo.Get(ctx, lobbyID)
	if err != nil {
		return nil, err
	}
	if lob.HostID != userID {
		return nil, ErrForbidden
	}
	if lob.Status != "waiting" {
		return nil, ErrNotWaiting
	}
	eng, ok := s.engines.Get(lob.GameKey)
	if !ok {
		return nil, ErrUnknownGame
	}
	if len(lob.Seats) < eng.MinSeats() {
		return nil, ErrNotEnoughPlayers
	}
	init, ok := s.initializers[lob.GameKey]
	if !ok {
		return nil, ErrUnknownGame
	}

	seatInits := make([]engine.SeatInit, len(lob.Seats))
	for i, st := range lob.Seats {
		seatInits[i] = engine.SeatInit{Seat: st.SeatIndex, UserID: st.UserID, Stack: st.Stack}
	}
	state, _, err := eng.InitState(seatInits)
	if err != nil {
		return nil, err
	}

	tx, err := s.repo.Pool().Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	if err := s.repo.UpdateStatus(ctx, tx, lobbyID, "in_progress"); err != nil {
		return nil, err
	}
	if err := init.InitGameState(ctx, tx, lobbyID, state, 1); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit tx: %w", err)
	}
	return s.repo.Get(ctx, lobbyID)
}

// CurrentLobby returns the id of the user's active lobby, if any.
func (s *Service) CurrentLobby(ctx context.Context, userID uuid.UUID) (uuid.UUID, bool, error) {
	return s.repo.CurrentLobby(ctx, userID)
}

// Finish marks a lobby as finished (e.g. the game can no longer continue).
func (s *Service) Finish(ctx context.Context, lobbyID uuid.UUID) error {
	return s.repo.UpdateStatus(ctx, s.repo.Pool(), lobbyID, "finished")
}
