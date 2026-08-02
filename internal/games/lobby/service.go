package lobby

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/serediukit/bgex-backend/internal/games/engine"
)

// maxConfigBytes bounds the marshalled length of a lobby's config JSONB
// blob, regardless of whether the game registers a ConfigValidator. config
// is persisted verbatim and re-served to every browsing client via
// ListItem.Config, so an unbounded blob is a persistent, attacker-supplied
// payload amplifier: a handful of oversized lobbies makes GET
// /api/v1/games/lobbies unusably large for everyone, not just their creator.
const maxConfigBytes = 4096

// GameInitializer persists a game's freshly-created initial state inside the
// same transaction that flips the lobby to in_progress. Each game provides an
// implementation (poker writes to its own schema); the lobby layer stays
// game-agnostic and only knows this interface. config is the lobby's own
// game-specific configuration (e.g. TTR's pinned map_id/map_version).
type GameInitializer interface {
	InitGameState(ctx context.Context, tx pgx.Tx, lobbyID uuid.UUID, state []byte, config map[string]any) error
}

// ConfigValidator lets a game validate and normalize the lobby config at
// creation time (e.g. TTR resolves map_id -> pinned map_version). Games
// without any config to validate (poker) do not implement it.
type ConfigValidator interface {
	ValidateLobbyConfig(ctx context.Context, cfg map[string]any) (normalized map[string]any, err error)
}

// ResignHandler lets a game react to a player leaving an in-progress game
// (e.g. TTR marks the player resigned instead of pulling their pieces off the
// board). Games without mid-game-leave semantics (poker) do not implement it.
type ResignHandler interface {
	Resign(ctx context.Context, lobbyID, userID uuid.UUID) (gameOver bool, err error)
}

// Service holds the lobby business logic.
type Service struct {
	repo         *Repository
	engines      *engine.Registry
	initializers map[string]GameInitializer
	validators   map[string]ConfigValidator
	resigners    map[string]ResignHandler
}

// NewService wires the lobby service. initializers, validators and resigners
// are all keyed by game key; validators and resigners may be nil or omit
// entries for games that don't need them.
func NewService(repo *Repository, engines *engine.Registry, initializers map[string]GameInitializer, validators map[string]ConfigValidator, resigners map[string]ResignHandler) *Service {
	return &Service{repo: repo, engines: engines, initializers: initializers, validators: validators, resigners: resigners}
}

// Create makes a new lobby and seats the host at seat 0, all in one tx. Fails
// with ErrAlreadySeated if the host already occupies a seat elsewhere. If the
// game registers a ConfigValidator, config is validated and normalized before
// being persisted; a rejected config maps to ErrInvalidConfig.
func (s *Service) Create(ctx context.Context, hostID uuid.UUID, gameKey, name string, maxSeats int, config map[string]any) (*Lobby, error) {
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
	config, err := s.validateAndEncodeConfig(ctx, gameKey, config)
	if err != nil {
		return nil, err
	}

	tx, err := s.repo.Pool().Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	created, err := s.repo.InsertLobby(ctx, tx, gameKey, name, hostID, maxSeats, config)
	if err != nil {
		return nil, err
	}
	if err := s.repo.ClaimSeat(ctx, tx, created.ID, hostID, 0, engine.BuyInFor(eng)); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit tx: %w", err)
	}
	return s.repo.Get(ctx, created.ID)
}

// validateAndEncodeConfig resolves the lobby config Create should persist
// for gameKey: default nil to empty, hand it to gameKey's ConfigValidator
// (if one is registered) for normalization, reject a non-empty config for
// games with no validator to sanity-check it, and cap the marshalled size
// regardless of validator outcome. config is persisted verbatim and
// re-served to every browsing client via ListItem.Config, so both checks
// matter even for games that never register a validator.
func (s *Service) validateAndEncodeConfig(ctx context.Context, gameKey string, config map[string]any) (map[string]any, error) {
	if config == nil {
		config = map[string]any{}
	}
	if v, ok := s.validators[gameKey]; ok {
		normalized, err := v.ValidateLobbyConfig(ctx, config)
		if err != nil {
			return nil, fmt.Errorf("%w: %s", ErrInvalidConfig, err.Error())
		}
		config = normalized
	} else if len(config) > 0 {
		// gameKey (e.g. poker) registers no ConfigValidator, so there is no
		// seam to bound or sanity-check an arbitrary client-supplied config
		// blob. Reject it outright rather than persisting and re-serving
		// whatever the client sent to every browser of the lobby list.
		return nil, fmt.Errorf("%w: %s does not accept a lobby config", ErrInvalidConfig, gameKey)
	}

	raw, err := json.Marshal(config)
	if err != nil {
		return nil, fmt.Errorf("marshal lobby config: %w", err)
	}
	if len(raw) > maxConfigBytes {
		return nil, fmt.Errorf("%w: config exceeds %d bytes", ErrInvalidConfig, maxConfigBytes)
	}
	return config, nil
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
	if err := s.repo.ClaimSeat(ctx, s.repo.Pool(), lobbyID, userID, seatIndex, engine.BuyInFor(eng)); err != nil {
		return nil, err
	}
	return s.repo.Get(ctx, lobbyID)
}

// Leave removes a user from a lobby. If the lobby empties it is finished; if the
// host leaves while others remain, the earliest remaining seat becomes host. If
// the lobby's game is in_progress and registers a ResignHandler, the game is
// given a chance to react (e.g. TTR marks the player resigned, leaving their
// routes/stations on the board) before the seat is marked left; if that
// resolves the game, the lobby is finished too.
func (s *Service) Leave(ctx context.Context, userID, lobbyID uuid.UUID) error {
	lob, err := s.repo.Get(ctx, lobbyID)
	if err != nil {
		return err
	}
	if lob.Status == "in_progress" {
		if err := s.resignFromInProgressGame(ctx, lob, userID); err != nil {
			return err
		}
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

// resignFromInProgressGame gives the game's ResignHandler (if registered) a
// chance to react to a player leaving an in-progress game, and finishes the
// lobby if the game reports it's now over.
//
// A resign the engine refuses is treated as a no-op rather than a hard
// failure that blocks the seat release below:
//   - engine.ErrGameOver means the engine's own state has already
//     concluded — most likely because a prior finishGame call updated the
//     game's state but its own lobby.Finish call failed transiently,
//     leaving the lobby stuck at "in_progress" (see realtime.Handler).
//     Every subsequent Resign then hits this same error. This branch also
//     brings the lobby's status in line with the engine's, since the
//     lobby was never actually finished.
//   - engine.ErrNotSeated / engine.ErrIllegalAction cover races like the
//     player already having resigned or left through another path.
//
// Without this, any of the three permanently locks the leaving player out
// of this lobby's seat — and, because the platform enforces one active
// seat per user, out of every other lobby too. Any other error from Resign
// is still treated as a hard failure.
func (s *Service) resignFromInProgressGame(ctx context.Context, lob *Lobby, userID uuid.UUID) error {
	rh, ok := s.resigners[lob.GameKey]
	if !ok {
		return nil
	}
	gameOver, err := rh.Resign(ctx, lob.ID, userID)
	switch {
	case err == nil:
		// proceed to the gameOver check below
	case errors.Is(err, engine.ErrGameOver):
		gameOver = true
	case errors.Is(err, engine.ErrNotSeated), errors.Is(err, engine.ErrIllegalAction):
		return nil
	default:
		return err
	}
	if !gameOver {
		return nil
	}
	return s.repo.UpdateStatus(ctx, s.repo.Pool(), lob.ID, "finished")
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
	state, _, err := eng.InitState(ctx, lob.Config, seatInits)
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
	if err := init.InitGameState(ctx, tx, lobbyID, state, lob.Config); err != nil {
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
