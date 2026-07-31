package engine

import (
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
	// DefaultBuyIn is the starting chip stack handed to each seat.
	DefaultBuyIn() int64

	// InitState creates the first-hand state for the given seated players.
	InitState(seats []SeatInit) (state []byte, events []Event, err error)
	// NextHand advances a finished hand to a fresh one, carrying stacks over and
	// rotating the button. Returns ErrNotEnoughPlayers if fewer than MinSeats
	// players still have chips.
	NextHand(state []byte) (next []byte, events []Event, err error)

	// Apply validates and applies a player action, returning the new state and
	// any broadcastable events.
	Apply(state []byte, a Action) (next []byte, events []Event, err error)

	// View returns a JSON-serializable, per-player redacted projection of the
	// state (a player must not see opponents' hidden cards).
	View(state []byte, forUser uuid.UUID) (any, error)

	// IsHandOver reports whether the current hand has completed (showdown or
	// everyone-but-one folded), so the caller can archive it and deal the next.
	IsHandOver(state []byte) bool
}
