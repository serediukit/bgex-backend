package realtime

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/gorilla/websocket"
	"github.com/sirupsen/logrus"

	"github.com/serediukit/bgex-backend/internal/games/engine"
	"github.com/serediukit/bgex-backend/internal/games/lobby"
)

const (
	handOverDelay = 4 * time.Second
	opTimeout     = 5 * time.Second
)

// Handler upgrades WebSocket connections and drives gameplay for a lobby.
type Handler struct {
	auth     AccessVerifier
	lobbies  *lobby.Service
	games    map[string]GameService
	hub      Broadcaster
	log      *logrus.Logger
	upgrader websocket.Upgrader
}

// NewHandler wires the realtime handler.
func NewHandler(auth AccessVerifier, lobbies *lobby.Service, hub Broadcaster, log *logrus.Logger, games ...GameService) *Handler {
	byKey := make(map[string]GameService, len(games))
	for _, g := range games {
		byKey[g.GameKey()] = g
	}
	return &Handler{
		auth:    auth,
		lobbies: lobbies,
		games:   byKey,
		hub:     hub,
		log:     log,
		upgrader: websocket.Upgrader{
			ReadBufferSize:  1024,
			WriteBufferSize: 1024,
			// The access token is carried in the query string and verified below,
			// so any origin may complete the handshake.
			CheckOrigin: func(*http.Request) bool { return true },
		},
	}
}

// Register mounts the WebSocket route.
func (h *Handler) Register(_ gin.HandlerFunc) func(r *gin.RouterGroup) {
	return func(r *gin.RouterGroup) {
		r.GET("/games/lobbies/:id/ws", h.serveWS)
	}
}

func (h *Handler) serveWS(c *gin.Context) {
	lobbyID, err := uuid.Parse(c.Param("id"))
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid lobby id"})
		return
	}
	// WebSocket clients cannot set Authorization headers, so the short-lived
	// access token arrives as a query parameter.
	userID, err := h.auth.VerifyAccessToken(c.Request.Context(), c.Query("token"))
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "invalid or missing token"})
		return
	}

	lob, err := h.lobbies.Get(c.Request.Context(), lobbyID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "lobby not found"})
		return
	}
	if _, ok := h.games[lob.GameKey]; !ok {
		c.JSON(http.StatusBadRequest, gin.H{"error": "unsupported game"})
		return
	}

	conn, err := h.upgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		return // Upgrade already wrote the error.
	}

	client := newClient(conn, userID, lobbyID, lob.GameKey)
	h.hub.Register(lobbyID, client)
	go client.writePump()

	// Send the joining client the current state, and let others see them arrive.
	h.broadcastState(context.Background(), lobbyID)

	client.readPump(func(msg ClientMessage) {
		h.onMessage(client, msg)
	})

	h.hub.Unregister(lobbyID, client)
	h.broadcastState(context.Background(), lobbyID)
}

func (h *Handler) onMessage(c *Client, msg ClientMessage) {
	ctx, cancel := context.WithTimeout(context.Background(), opTimeout)
	defer cancel()

	switch msg.Type {
	case "action":
		gs := h.games[c.gameKey]
		if gs == nil {
			return
		}
		handOver, err := gs.Apply(ctx, c.lobbyID, engine.Action{
			UserID: c.userID, Type: msg.Action, Amount: msg.Amount,
		})
		if err != nil {
			c.sendJSON(ServerMessage{Type: "error", Error: err.Error()})
			return
		}
		h.broadcastState(ctx, c.lobbyID)
		if handOver {
			h.scheduleNextHand(c.lobbyID, c.gameKey)
		}

	case "sit":
		if _, err := h.lobbies.Join(ctx, c.userID, c.lobbyID, msg.SeatIndex); err != nil {
			c.sendJSON(ServerMessage{Type: "error", Error: err.Error()})
			return
		}
		h.broadcastState(ctx, c.lobbyID)

	case "leave":
		if err := h.lobbies.Leave(ctx, c.userID, c.lobbyID); err != nil {
			c.sendJSON(ServerMessage{Type: "error", Error: err.Error()})
			return
		}
		h.broadcastState(ctx, c.lobbyID)

	case "start":
		if _, err := h.lobbies.Start(ctx, c.userID, c.lobbyID); err != nil {
			c.sendJSON(ServerMessage{Type: "error", Error: err.Error()})
			return
		}
		h.broadcastState(ctx, c.lobbyID)

	case "ping":
		// no-op; keep-alive handled by the write pump

	default:
		c.sendJSON(ServerMessage{Type: "error", Error: "unknown message type"})
	}
}

// scheduleNextHand deals the next hand after a short delay so players can see
// the showdown, then broadcasts the fresh state (or finishes the game).
func (h *Handler) scheduleNextHand(lobbyID uuid.UUID, gameKey string) {
	time.AfterFunc(handOverDelay, func() {
		ctx, cancel := context.WithTimeout(context.Background(), opTimeout)
		defer cancel()

		gs := h.games[gameKey]
		if gs == nil {
			return
		}
		finished, err := gs.NextHand(ctx, lobbyID)
		if err != nil {
			h.log.WithError(err).WithField("lobby", lobbyID).Warn("deal next hand failed")
			return
		}
		if finished {
			if err := h.lobbies.Finish(ctx, lobbyID); err != nil {
				h.log.WithError(err).Warn("finish lobby failed")
			}
			h.hub.Broadcast(lobbyID, func(uuid.UUID) any {
				return ServerMessage{Type: "event", Event: &engine.Event{Type: "game_over"}}
			})
		}
		h.broadcastState(ctx, lobbyID)
	})
}

// broadcastState pushes the lobby snapshot plus each viewer's redacted game view
// to everyone watching the lobby.
func (h *Handler) broadcastState(ctx context.Context, lobbyID uuid.UUID) {
	lob, err := h.lobbies.Get(ctx, lobbyID)
	if err != nil {
		if !errors.Is(err, lobby.ErrNotFound) {
			h.log.WithError(err).Warn("broadcast: load lobby failed")
		}
		return
	}
	gs := h.games[lob.GameKey]
	inProgress := lob.Status == "in_progress"

	h.hub.Broadcast(lobbyID, func(userID uuid.UUID) any {
		msg := ServerMessage{Type: "state", Lobby: lob}
		if inProgress && gs != nil {
			if view, err := gs.View(ctx, lobbyID, userID); err == nil {
				msg.Game = view
			}
		}
		return msg
	})
}
