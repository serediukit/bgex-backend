package friends

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/serediukit/bgex-backend/internal/httpx/middleware"
	"github.com/serediukit/bgex-backend/internal/httpx/response"
)

// Handler wires friends HTTP routes onto a gin RouterGroup.
type Handler struct {
	svc *Service
}

// NewHandler creates a new Handler backed by the given Service.
func NewHandler(svc *Service) *Handler {
	return &Handler{svc: svc}
}

// Register returns a RouteRegistrar that mounts all friends routes.
// authMiddleware is applied inline per route following codebase conventions.
func (h *Handler) Register(authMiddleware gin.HandlerFunc) func(r *gin.RouterGroup) {
	return func(r *gin.RouterGroup) {
		g := r.Group("/friends")
		g.POST("/requests", authMiddleware, h.sendRequest)
		g.GET("/requests/incoming", authMiddleware, h.listIncoming)
		g.GET("/requests/outgoing", authMiddleware, h.listOutgoing)
		g.PATCH("/requests/:id", authMiddleware, h.respondToRequest)
		g.DELETE("/requests/:id", authMiddleware, h.cancelRequest)
		g.GET("", authMiddleware, h.listFriends)
		g.DELETE("/:user_id", authMiddleware, h.unfriend)
		g.GET("/status/:user_id", authMiddleware, h.getRelationshipStatus)
	}
}

type sendRequestReq struct {
	UserID uuid.UUID `json:"user_id" binding:"required"`
}

// sendRequest handles POST /friends/requests.
func (h *Handler) sendRequest(c *gin.Context) {
	var req sendRequestReq
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Error(c, http.StatusBadRequest, response.CodeInvalidRequest, err.Error())
		return
	}

	userID := middleware.UserIDFrom(c.Request.Context())
	result, err := h.svc.SendRequest(c.Request.Context(), userID, req.UserID)
	if err != nil {
		h.handleError(c, err)
		return
	}
	response.Created(c, result)
}

// listIncoming handles GET /friends/requests/incoming.
func (h *Handler) listIncoming(c *gin.Context) {
	userID := middleware.UserIDFrom(c.Request.Context())
	list, err := h.svc.ListIncoming(c.Request.Context(), userID)
	if err != nil {
		response.Error(c, http.StatusInternalServerError, response.CodeInternal, "failed to list incoming requests")
		return
	}
	if list == nil {
		list = []RequestWithUser{}
	}
	response.OK(c, list)
}

// listOutgoing handles GET /friends/requests/outgoing.
func (h *Handler) listOutgoing(c *gin.Context) {
	userID := middleware.UserIDFrom(c.Request.Context())
	list, err := h.svc.ListOutgoing(c.Request.Context(), userID)
	if err != nil {
		response.Error(c, http.StatusInternalServerError, response.CodeInternal, "failed to list outgoing requests")
		return
	}
	if list == nil {
		list = []RequestWithUser{}
	}
	response.OK(c, list)
}

type respondReq struct {
	Action string `json:"action" binding:"required,oneof=accept decline"`
}

// respondToRequest handles PATCH /friends/requests/:id.
func (h *Handler) respondToRequest(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		response.Error(c, http.StatusBadRequest, response.CodeInvalidRequest, "invalid request id")
		return
	}

	var req respondReq
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Error(c, http.StatusBadRequest, response.CodeInvalidRequest, err.Error())
		return
	}

	userID := middleware.UserIDFrom(c.Request.Context())
	result, err := h.svc.Respond(c.Request.Context(), userID, id, req.Action == "accept")
	if err != nil {
		h.handleError(c, err)
		return
	}
	response.OK(c, result)
}

// cancelRequest handles DELETE /friends/requests/:id.
func (h *Handler) cancelRequest(c *gin.Context) {
	id, err := uuid.Parse(c.Param("id"))
	if err != nil {
		response.Error(c, http.StatusBadRequest, response.CodeInvalidRequest, "invalid request id")
		return
	}

	userID := middleware.UserIDFrom(c.Request.Context())
	if err := h.svc.CancelRequest(c.Request.Context(), userID, id); err != nil {
		h.handleError(c, err)
		return
	}
	response.NoContent(c)
}

// listFriends handles GET /friends.
func (h *Handler) listFriends(c *gin.Context) {
	userID := middleware.UserIDFrom(c.Request.Context())
	list, err := h.svc.ListFriends(c.Request.Context(), userID)
	if err != nil {
		response.Error(c, http.StatusInternalServerError, response.CodeInternal, "failed to list friends")
		return
	}
	if list == nil {
		list = []Friend{}
	}
	response.OK(c, list)
}

// unfriend handles DELETE /friends/:user_id.
func (h *Handler) unfriend(c *gin.Context) {
	targetID, err := uuid.Parse(c.Param("user_id"))
	if err != nil {
		response.Error(c, http.StatusBadRequest, response.CodeInvalidRequest, "invalid user id")
		return
	}

	userID := middleware.UserIDFrom(c.Request.Context())
	if err := h.svc.Unfriend(c.Request.Context(), userID, targetID); err != nil {
		h.handleError(c, err)
		return
	}
	response.NoContent(c)
}

// getRelationshipStatus handles GET /friends/status/:user_id.
func (h *Handler) getRelationshipStatus(c *gin.Context) {
	targetID, err := uuid.Parse(c.Param("user_id"))
	if err != nil {
		response.Error(c, http.StatusBadRequest, response.CodeInvalidRequest, "invalid user id")
		return
	}

	userID := middleware.UserIDFrom(c.Request.Context())
	rs, err := h.svc.GetRelationshipStatus(c.Request.Context(), userID, targetID)
	if err != nil {
		response.Error(c, http.StatusInternalServerError, response.CodeInternal, "failed to get relationship status")
		return
	}
	response.OK(c, rs)
}

// handleError maps domain errors to HTTP responses.
func (h *Handler) handleError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, ErrSelfFriend):
		response.Error(c, http.StatusBadRequest, response.CodeInvalidRequest, err.Error())
	case errors.Is(err, ErrAlreadyExists):
		response.Error(c, http.StatusConflict, response.CodeConflict, err.Error())
	case errors.Is(err, ErrNotFound):
		response.Error(c, http.StatusNotFound, response.CodeNotFound, err.Error())
	case errors.Is(err, ErrForbidden):
		response.Error(c, http.StatusForbidden, response.CodeForbidden, err.Error())
	default:
		response.Error(c, http.StatusInternalServerError, response.CodeInternal, "internal error")
	}
}
