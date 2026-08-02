// Package realtime provides the game-agnostic WebSocket layer: a per-lobby
// fan-out hub and a connection handler that drives the Postgres-authoritative
// action loop. It depends only on the lobby service and a GameService seam, so
// any game can plug in.
package realtime

import (
	"context"
	"encoding/json"

	"github.com/google/uuid"

	"github.com/serediukit/bgex-backend/internal/games/engine"
)

// ClientMessage is a message sent from the browser over the socket.
type ClientMessage struct {
	Type      string          `json:"type"`              // "action" | "sit" | "leave" | "start" | "ping"
	Action    string          `json:"action,omitempty"`  // for "action": fold|check|call|bet|raise
	Amount    int64           `json:"amount,omitempty"`  // for bet/raise: target commitment
	SeatIndex int             `json:"seat_index"`        // for "sit"
	Payload   json.RawMessage `json:"payload,omitempty"` // game-specific action payload (e.g. TTR)
}

// ServerMessage is a message pushed to the browser.
type ServerMessage struct {
	Type  string        `json:"type"`            // "state" | "event" | "error"
	Lobby any           `json:"lobby,omitempty"` // lobby snapshot (seats, status)
	Game  any           `json:"game,omitempty"`  // per-viewer redacted game view
	Event *engine.Event `json:"event,omitempty"`
	Error string        `json:"error,omitempty"`
}

// AccessVerifier validates a raw access token (satisfied by auth.Service).
type AccessVerifier interface {
	VerifyAccessToken(ctx context.Context, raw string) (uuid.UUID, error)
}

// GameService is the persistence-aware seam between the realtime layer and a
// concrete game. Implementations own their storage (e.g. poker's schema) and
// keep Postgres as the source of truth.
type GameService interface {
	GameKey() string
	// View returns the redacted view of the current state for a viewer.
	View(ctx context.Context, lobbyID, userID uuid.UUID) (any, error)
	// Apply validates and applies an action atomically. events are the
	// engine's own events emitted while applying it, in causal order, for the
	// realtime layer to broadcast before the resulting state. over reports
	// whether this action ended the hand (hand-based games) or the whole game.
	Apply(ctx context.Context, lobbyID uuid.UUID, action engine.Action) (events []engine.Event, over bool, err error)
}

// HandBasedGameService is implemented by hand-based games so the realtime
// layer can deal a following hand instead of finishing the lobby.
type HandBasedGameService interface {
	GameService
	// NextHand deals the following hand once the current one is over,
	// returning the fresh hand's own events (e.g. "hand_start") and reporting
	// whether the table can no longer continue (finished).
	NextHand(ctx context.Context, lobbyID uuid.UUID) (events []engine.Event, finished bool, err error)
}
