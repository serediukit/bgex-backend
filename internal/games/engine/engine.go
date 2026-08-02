package engine

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/google/uuid"
)

// Common engine errors. Games may return these directly so callers can map them
// to HTTP/WS responses without importing each game package.
var (
	// ErrNotYourTurn is returned when a player acts out of turn.
	ErrNotYourTurn = errors.New("not your turn")
	// ErrIllegalAction is returned for an action that is not legal in the
	// current state (e.g. checking when facing a bet).
	ErrIllegalAction = errors.New("illegal action")
	// ErrNotEnoughPlayers is returned when a hand cannot start.
	ErrNotEnoughPlayers = errors.New("not enough players")
	// ErrGameOver is returned when an action is attempted after the game has
	// already ended.
	ErrGameOver = errors.New("game is over")
	// ErrNotSeated is returned when the acting user does not occupy a seat in
	// this game.
	ErrNotSeated = errors.New("you are not seated in this game")
	// ErrWrongPhase is returned when an action is not valid in the state's
	// current phase (e.g. resolving a decision that isn't pending).
	ErrWrongPhase = errors.New("action not valid in the current phase")
)

// SeatInit describes a player taking part in a hand at engine start.
type SeatInit struct {
	Seat   int
	UserID uuid.UUID
	Stack  int64
}

// Action is a game-agnostic player action. Type is interpreted per game
// (poker: "fold" | "check" | "call" | "bet" | "raise"). Amount is used by
// games that need a magnitude (bet/raise size); ignored otherwise.
type Action struct {
	UserID uuid.UUID
	Type   string
	Amount int64
	// Payload is a game-specific action payload; ignored by games that only
	// need Type/Amount.
	Payload json.RawMessage
}

// Event is a discrete, non-secret thing that happened, suitable for broadcast
// to every player at the table (e.g. "flop", "player_folded").
type Event struct {
	Type string         `json:"type"`
	Data map[string]any `json:"data,omitempty"`
}

// Engine is the contract every game implements. State is an opaque, serialized
// blob (protobuf for poker) that the platform persists as the source of truth;
// only the engine interprets it.
type Engine interface {
	// GameKey is the stable identifier used in lobbies (e.g. "poker").
	GameKey() string
	// MinSeats / MaxSeats bound how many players a table supports.
	MinSeats() int
	MaxSeats() int

	// InitState creates the initial state for the given seated players. cfg is
	// the lobby's game-specific configuration (e.g. TTR resolves its pinned
	// map from cfg); poker ignores both ctx and cfg.
	InitState(ctx context.Context, cfg map[string]any, seats []SeatInit) (state []byte, events []Event, err error)

	// Apply validates and applies a player action, returning the new state and
	// any broadcastable events.
	Apply(state []byte, a Action) (next []byte, events []Event, err error)

	// View returns a JSON-serializable, per-player redacted projection of the
	// state (a player must not see opponents' hidden information).
	View(state []byte, forUser uuid.UUID) (any, error)

	// IsOver reports whether the game itself has concluded (as opposed to a
	// single hand, see HandBased.IsHandOver).
	IsOver(state []byte) bool
}

// HandBased is implemented by games played as a series of independent hands
// (poker). Games with a single continuous game (TTR) do not implement it.
type HandBased interface {
	DefaultBuyIn() int64
	NextHand(state []byte) (next []byte, events []Event, err error)
	IsHandOver(state []byte) bool
}

// BuyInFor returns the starting stack for a seat joining e's lobby: the
// hand-based default buy-in when e implements HandBased, or 0 for games (like
// TTR) with no chip stacks.
func BuyInFor(e Engine) int64 {
	if hb, ok := e.(HandBased); ok {
		return hb.DefaultBuyIn()
	}
	return 0
}
