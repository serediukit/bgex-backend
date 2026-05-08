package user

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/serediukit/bgex-backend/internal/httpx/middleware"
	"github.com/serediukit/bgex-backend/internal/httpx/response"
)

type Handler struct {
	repo *Repository
	svc  *Service
}

func NewHandler(repo *Repository, svc *Service) *Handler {
	return &Handler{repo: repo, svc: svc}
}

func (h *Handler) Register(authMiddleware gin.HandlerFunc) func(r *gin.RouterGroup) {
	return func(r *gin.RouterGroup) {
		users := r.Group("/users")
		users.GET("/me", authMiddleware, h.me)
		users.PATCH("/me", authMiddleware, h.updateMe)
		users.GET("/:id", h.getProfile)
	}
}

// me returns the full authenticated user (includes email).
func (h *Handler) me(c *gin.Context) {
	id := middleware.UserIDFrom(c.Request.Context())
	u, err := h.repo.GetByID(c.Request.Context(), id)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			response.Error(c, http.StatusNotFound, response.CodeNotFound, "user not found")
			return
		}
		response.Error(c, http.StatusInternalServerError, response.CodeInternal, "failed to load user")
		return
	}
	response.OK(c, u)
}

type updateMeReq struct {
	Username    *string `json:"username"    binding:"omitempty,min=3,max=30"`
	DisplayName *string `json:"display_name" binding:"omitempty,max=64"`
	AvatarURL   *string `json:"avatar_url"   binding:"omitempty,url,max=512"`
	Bio         *string `json:"bio"          binding:"omitempty,max=280"`
	Country     *string `json:"country"      binding:"omitempty,len=2"`
}

// updateMe handles PATCH /users/me.
func (h *Handler) updateMe(c *gin.Context) {
	var req updateMeReq
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Error(c, http.StatusBadRequest, response.CodeInvalidRequest, err.Error())
		return
	}

	id := middleware.UserIDFrom(c.Request.Context())
	params := UpdateParams{
		Username:    req.Username,
		DisplayName: req.DisplayName,
		AvatarURL:   req.AvatarURL,
		Bio:         req.Bio,
		Country:     req.Country,
	}

	u, err := h.svc.UpdateProfile(c.Request.Context(), id, params)
	if err != nil {
		switch {
		case errors.Is(err, ErrUsernameTaken):
			response.Error(c, http.StatusConflict, response.CodeConflict, "username already taken")
		case errors.Is(err, ErrInvalidUsername), errors.Is(err, ErrInvalidCountry):
			response.Error(c, http.StatusBadRequest, response.CodeInvalidRequest, err.Error())
		default:
			response.Error(c, http.StatusInternalServerError, response.CodeInternal, "failed to update profile")
		}
		return
	}
	response.OK(c, u)
}

// getProfile returns the public profile of any user by UUID or username.
func (h *Handler) getProfile(c *gin.Context) {
	param := c.Param("id")

	var (
		u   *User
		err error
	)
	if id, parseErr := uuid.Parse(param); parseErr == nil {
		u, err = h.repo.GetByID(c.Request.Context(), id)
	} else {
		u, err = h.repo.GetByUsername(c.Request.Context(), param)
	}

	if err != nil {
		if errors.Is(err, ErrNotFound) {
			response.Error(c, http.StatusNotFound, response.CodeNotFound, "user not found")
			return
		}
		response.Error(c, http.StatusInternalServerError, response.CodeInternal, "failed to load user")
		return
	}
	response.OK(c, u.ToPublic())
}
