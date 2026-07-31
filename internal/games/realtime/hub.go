package realtime

import (
	"sync"

	"github.com/google/uuid"
)

// Broadcaster fans messages out to the clients watching a lobby. The in-process
// Hub implements it; the interface is the seam where a Redis pub/sub backplane
// would slot in for horizontal scaling.
type Broadcaster interface {
	Register(lobbyID uuid.UUID, c *Client)
	Unregister(lobbyID uuid.UUID, c *Client)
	// Broadcast sends a message to every client in a lobby. build is called once
	// per client with that client's user id, so each recipient can get a
	// per-viewer (redacted) payload.
	Broadcast(lobbyID uuid.UUID, build func(userID uuid.UUID) any)
}

// Hub is an in-process Broadcaster keyed by lobby id.
type Hub struct {
	mu    sync.RWMutex
	rooms map[uuid.UUID]map[*Client]struct{}
}

// NewHub creates an empty Hub.
func NewHub() *Hub {
	return &Hub{rooms: make(map[uuid.UUID]map[*Client]struct{})}
}

// Register adds a client to a lobby's room.
func (h *Hub) Register(lobbyID uuid.UUID, c *Client) {
	h.mu.Lock()
	defer h.mu.Unlock()
	room, ok := h.rooms[lobbyID]
	if !ok {
		room = make(map[*Client]struct{})
		h.rooms[lobbyID] = room
	}
	room[c] = struct{}{}
}

// Unregister removes a client, dropping the room when it empties.
func (h *Hub) Unregister(lobbyID uuid.UUID, c *Client) {
	h.mu.Lock()
	defer h.mu.Unlock()
	room, ok := h.rooms[lobbyID]
	if !ok {
		return
	}
	delete(room, c)
	if len(room) == 0 {
		delete(h.rooms, lobbyID)
	}
}

// Broadcast sends a per-viewer payload to every client in the lobby.
func (h *Hub) Broadcast(lobbyID uuid.UUID, build func(userID uuid.UUID) any) {
	h.mu.RLock()
	clients := make([]*Client, 0, len(h.rooms[lobbyID]))
	for c := range h.rooms[lobbyID] {
		clients = append(clients, c)
	}
	h.mu.RUnlock()

	for _, c := range clients {
		c.sendJSON(build(c.userID))
	}
}
