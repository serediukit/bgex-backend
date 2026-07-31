package lobby

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/serediukit/bgex-backend/internal/httpx/middleware"
	"github.com/serediukit/bgex-backend/internal/httpx/response"
)

// Handler exposes lobby REST routes.
type Handler struct {
	svc *Service
}

// NewHandler creates a lobby Handler.
func NewHandler(svc *Service) *Handler { return &Handler{svc: svc} }

// Register mounts the lobby routes under /games/lobbies. authMiddleware is
// applied inline per route per codebase convention.
func (h *Handler) Register(authMiddleware gin.HandlerFunc) func(r *gin.RouterGroup) {
	return func(r *gin.RouterGroup) {
		g := r.Group("/games/lobbies")
		g.POST("", authMiddleware, h.create)
		g.GET("", authMiddleware, h.list)
		g.GET("/current", authMiddleware, h.current)
		g.GET("/:id", authMiddleware, h.get)
		g.POST("/:id/seats", authMiddleware, h.join)
		g.DELETE("/:id/seats", authMiddleware, h.leave)
		g.POST("/:id/start", authMiddleware, h.start)
	}
}

type createReq struct {
	GameKey  string `json:"game_key"`
	Name     string `json:"name"`
	MaxSeats int    `json:"max_seats"`
}

func (h *Handler) create(c *gin.Context) {
	var req createReq
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Error(c, http.StatusBadRequest, response.CodeInvalidRequest, err.Error())
		return
	}
	if req.GameKey == "" {
		req.GameKey = "poker"
	}
	userID := middleware.UserIDFrom(c.Request.Context())
	lob, err := h.svc.Create(c.Request.Context(), userID, req.GameKey, req.Name, req.MaxSeats)
	if err != nil {
		h.handleError(c, err)
		return
	}
	response.Created(c, lob)
}

func (h *Handler) list(c *gin.Context) {
	gameKey := c.DefaultQuery("game", "poker")
	items, err := h.svc.List(c.Request.Context(), gameKey)
	if err != nil {
		h.handleError(c, err)
		return
	}
	if items == nil {
		items = []ListItem{}
	}
	response.OK(c, items)
}

func (h *Handler) current(c *gin.Context) {
	userID := middleware.UserIDFrom(c.Request.Context())
	id, ok, err := h.svc.CurrentLobby(c.Request.Context(), userID)
	if err != nil {
		response.Error(c, http.StatusInternalServerError, response.CodeInternal, "failed to look up current lobby")
		return
	}
	if !ok {
		response.NoContent(c)
		return
	}
	response.OK(c, gin.H{"lobby_id": id})
}

func (h *Handler) get(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		response.Error(c, http.StatusBadRequest, response.CodeInvalidRequest, "invalid lobby id")
		return
	}
	lob, err := h.svc.Get(c.Request.Context(), id)
	if err != nil {
		h.handleError(c, err)
		return
	}
	response.OK(c, lob)
}

type joinReq struct {
	SeatIndex int `json:"seat_index"`
}

func (h *Handler) join(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		response.Error(c, http.StatusBadRequest, response.CodeInvalidRequest, "invalid lobby id")
		return
	}
	var req joinReq
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Error(c, http.StatusBadRequest, response.CodeInvalidRequest, err.Error())
		return
	}
	userID := middleware.UserIDFrom(c.Request.Context())
	lob, err := h.svc.Join(c.Request.Context(), userID, id, req.SeatIndex)
	if err != nil {
		h.handleError(c, err)
		return
	}
	response.OK(c, lob)
}

func (h *Handler) leave(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		response.Error(c, http.StatusBadRequest, response.CodeInvalidRequest, "invalid lobby id")
		return
	}
	userID := middleware.UserIDFrom(c.Request.Context())
	if err := h.svc.Leave(c.Request.Context(), userID, id); err != nil {
		h.handleError(c, err)
		return
	}
	response.NoContent(c)
}

func (h *Handler) start(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		response.Error(c, http.StatusBadRequest, response.CodeInvalidRequest, "invalid lobby id")
		return
	}
	userID := middleware.UserIDFrom(c.Request.Context())
	lob, err := h.svc.Start(c.Request.Context(), userID, id)
	if err != nil {
		h.handleError(c, err)
		return
	}
	response.OK(c, lob)
}

// handleError maps lobby domain errors to HTTP responses.
func (h *Handler) handleError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, ErrNotFound):
		response.Error(c, http.StatusNotFound, response.CodeNotFound, err.Error())
	case errors.Is(err, ErrAlreadySeated), errors.Is(err, ErrSeatTaken), errors.Is(err, ErrLobbyFull), errors.Is(err, ErrNotWaiting):
		response.Error(c, http.StatusConflict, response.CodeConflict, err.Error())
	case errors.Is(err, ErrForbidden):
		response.Error(c, http.StatusForbidden, response.CodeForbidden, err.Error())
	case errors.Is(err, ErrInvalidSeat), errors.Is(err, ErrNotEnoughPlayers), errors.Is(err, ErrUnknownGame), errors.Is(err, ErrNotSeated):
		response.Error(c, http.StatusBadRequest, response.CodeInvalidRequest, err.Error())
	default:
		response.Error(c, http.StatusInternalServerError, response.CodeInternal, "internal error")
	}
}
