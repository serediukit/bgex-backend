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
	h.broadcastStateWithTimeout(lobbyID)

	client.readPump(func(msg ClientMessage) {
		h.onMessage(client, msg)
	})

	h.hub.Unregister(lobbyID, client)
	h.broadcastStateWithTimeout(lobbyID)
}

// broadcastStateWithTimeout calls broadcastState with a bounded context, the
// same opTimeout deadline onMessage already applies to every action. Unlike
// onMessage's call sites, serveWS has no request context of its own to
// derive from (the WebSocket's lifetime long outlives any one HTTP request),
// so context.Background() is the right root — but it still needs a
// deadline: for TTR each call does two DB queries per viewer, and an
// undeadlined context.Background() call could hang indefinitely against a
// stalled database.
func (h *Handler) broadcastStateWithTimeout(lobbyID uuid.UUID) {
	ctx, cancel := context.WithTimeout(context.Background(), opTimeout)
	defer cancel()
	h.broadcastState(ctx, lobbyID)
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
		events, over, err := gs.Apply(ctx, c.lobbyID, engine.Action{
			UserID: c.userID, Type: msg.Action, Amount: msg.Amount, Payload: msg.Payload,
		})
		if err != nil {
			c.sendJSON(ServerMessage{Type: "error", Error: err.Error()})
			return
		}
		// Events first, in the engine's own causal order, then the resulting
		// state — so a client's log panel always reads "what happened" before
		// "where things ended up".
		h.broadcastEvents(c.lobbyID, events)
		h.broadcastState(ctx, c.lobbyID)
		if over {
			if hb, ok := gs.(HandBasedGameService); ok {
				h.scheduleNextHand(c.lobbyID, hb)
			} else {
				// Non-hand-based games (e.g. TTR) emit their own "game_over"
				// event as part of Apply's events when the game concludes;
				// finishGame must not also synthesize one in that case.
				h.finishGame(ctx, c.lobbyID, hasGameOverEvent(events))
			}
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
func (h *Handler) scheduleNextHand(lobbyID uuid.UUID, gs HandBasedGameService) {
	time.AfterFunc(handOverDelay, func() {
		ctx, cancel := context.WithTimeout(context.Background(), opTimeout)
		defer cancel()

		events, finished, err := gs.NextHand(ctx, lobbyID)
		if err != nil {
			h.log.WithError(err).WithField("lobby", lobbyID).Warn("deal next hand failed")
			return
		}
		if finished {
			// Hand-based games (poker) have no engine-level "game_over" event
			// of their own — finishGame's synthesized one is authoritative here.
			h.finishGame(ctx, lobbyID, false)
			return
		}
		h.broadcastEvents(lobbyID, events)
		h.broadcastState(ctx, lobbyID)
	})
}

// finishGame marks a lobby finished (no more hands/turns possible), tells
// everyone watching, and pushes the final lobby/game state. alreadyAnnounced
// is true when the caller already broadcast an engine-emitted "game_over"
// event for this same conclusion (TTR's engine emits its own); in that case
// finishGame does not synthesize a second one.
func (h *Handler) finishGame(ctx context.Context, lobbyID uuid.UUID, alreadyAnnounced bool) {
	if err := h.lobbies.Finish(ctx, lobbyID); err != nil {
		// Error, not Warn: a failure here leaves the lobby stuck at
		// "in_progress" while the engine's own state has already
		// concluded. Every player's subsequent DELETE .../seats then hits
		// the game's ResignHandler, which lobby.Service now treats as a
		// no-op (see lobby.Service.resignFromInProgressGame) rather than a
		// hard failure, so players can still leave — but the lobby itself
		// stays visibly "in_progress" to anyone browsing/joining until an
		// operator notices this log line. Silently swallowing it at Warn
		// made this exact failure mode invisible in production.
		h.log.WithError(err).WithField("lobby_id", lobbyID).Error("finish lobby failed: lobby will remain in_progress")
	}
	if !alreadyAnnounced {
		h.hub.Broadcast(lobbyID, func(uuid.UUID) any {
			return ServerMessage{Type: "event", Event: &engine.Event{Type: "game_over"}}
		})
	}
	h.broadcastState(ctx, lobbyID)
}

// broadcastEvents pushes each engine event to every subscriber of lobbyID, in
// the order given, ahead of the resulting state broadcast.
func (h *Handler) broadcastEvents(lobbyID uuid.UUID, events []engine.Event) {
	for i := range events {
		ev := events[i]
		h.hub.Broadcast(lobbyID, func(uuid.UUID) any {
			return ServerMessage{Type: "event", Event: &ev}
		})
	}
}

// hasGameOverEvent reports whether events already includes a "game_over"
// entry emitted by the engine itself.
func hasGameOverEvent(events []engine.Event) bool {
	for _, ev := range events {
		if ev.Type == "game_over" {
			return true
		}
	}
	return false
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
	// A finished lobby's game_states row (and ttr.game_results) still
	// exists and View still works on it — the final scores are only ever
	// readable through this same redacted View. Excluding "finished" here
	// made the results screen unreachable: any client connecting (or any
	// broadcast happening) after the game ended got the lobby only, forever.
	hasGame := lob.Status == "in_progress" || lob.Status == "finished"

	h.hub.Broadcast(lobbyID, func(userID uuid.UUID) any {
		msg := ServerMessage{Type: "state", Lobby: lob}
		if hasGame && gs != nil {
			if view, err := gs.View(ctx, lobbyID, userID); err == nil {
				msg.Game = view
			}
		}
		return msg
	})
}
