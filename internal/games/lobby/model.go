package lobby

import (
	"time"

	"github.com/google/uuid"
)

// Lobby is a game room with its seated players. Game-agnostic: the hot game
// state lives in the game's own schema, keyed by the lobby id.
type Lobby struct {
	ID       uuid.UUID `json:"id"`
	GameKey  string    `json:"game_key"`
	Name     string    `json:"name"`
	HostID   uuid.UUID `json:"host_id"`
	Status   string    `json:"status"`
	MaxSeats int       `json:"max_seats"`
	// Config is the game-specific lobby configuration (e.g. TTR's map_id /
	// map_version). Never serialized as null — always at least an empty object.
	Config    map[string]any `json:"config"`
	CreatedAt time.Time      `json:"created_at"`
	UpdatedAt time.Time      `json:"updated_at"`
	Seats     []Seat         `json:"seats"`
}

// Seat is one occupied place at a lobby, enriched with the player's public
// profile so clients can render names/avatars without extra lookups.
type Seat struct {
	SeatIndex   int       `json:"seat_index"`
	UserID      uuid.UUID `json:"user_id"`
	Username    string    `json:"username,omitempty"`
	DisplayName string    `json:"display_name,omitempty"`
	AvatarURL   string    `json:"avatar_url,omitempty"`
	Stack       int64     `json:"stack"`
	Status      string    `json:"status"`
}

// ListItem is a lightweight lobby summary for the browse view.
type ListItem struct {
	ID        uuid.UUID      `json:"id"`
	GameKey   string         `json:"game_key"`
	Name      string         `json:"name"`
	Status    string         `json:"status"`
	HostID    uuid.UUID      `json:"host_id"`
	SeatCount int            `json:"seat_count"`
	MaxSeats  int            `json:"max_seats"`
	Config    map[string]any `json:"config"`
	CreatedAt time.Time      `json:"created_at"`
}
