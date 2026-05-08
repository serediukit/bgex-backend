package auth

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/serediukit/bgex-backend/internal/domain/user"
	"github.com/serediukit/bgex-backend/internal/httpx/middleware"
	"github.com/serediukit/bgex-backend/internal/httpx/response"
)

const (
	oauthStateCookie       = "bgex_oauth_state"
	oauthStateCookieMaxAge = 600 // seconds
)

// Handler wires auth HTTP routes onto a gin RouterGroup.
type Handler struct {
	svc          *Service
	cookieSecure bool
}

func NewHandler(svc *Service, cookieSecure bool) *Handler {
	return &Handler{svc: svc, cookieSecure: cookieSecure}
}

// Register returns a RouteRegistrar mounting /auth/* onto the provided group.
// authMiddleware is applied to endpoints that require a bearer token.
func (h *Handler) Register(authMiddleware gin.HandlerFunc) func(r *gin.RouterGroup) {
	return func(r *gin.RouterGroup) {
		auth := r.Group("/auth")
		auth.POST("/register", h.register)
		auth.POST("/login", h.login)
		auth.POST("/refresh", h.refresh)
		auth.POST("/logout", authMiddleware, h.logout)

		if h.svc.google != nil {
			auth.GET("/google/login", h.googleLogin)
			auth.GET("/google/callback", h.googleCallback)
		}
	}
}

type registerReq struct {
	Email       string `json:"email" binding:"required,email,max=254"`
	Password    string `json:"password" binding:"required,min=12,max=256"`
	DisplayName string `json:"display_name" binding:"omitempty,max=64"`
}

type loginReq struct {
	Email    string `json:"email" binding:"required,email"`
	Password string `json:"password" binding:"required"`
}

type refreshReq struct {
	RefreshToken string `json:"refresh_token" binding:"required"`
}

type authResp struct {
	User   *user.User `json:"user"`
	Tokens *Tokens    `json:"tokens"`
}

func (h *Handler) register(c *gin.Context) {
	var req registerReq
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Error(c, http.StatusBadRequest, response.CodeInvalidRequest, err.Error())
		return
	}
	u, tokens, err := h.svc.Register(c.Request.Context(), req.Email, req.Password, req.DisplayName)
	if err != nil {
		if errors.Is(err, user.ErrEmailTaken) {
			response.Error(c, http.StatusConflict, response.CodeConflict, "email already registered")
			return
		}
		response.Error(c, http.StatusInternalServerError, response.CodeInternal, "failed to register")
		return
	}
	response.Created(c, authResp{User: u, Tokens: tokens})
}

func (h *Handler) login(c *gin.Context) {
	var req loginReq
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Error(c, http.StatusBadRequest, response.CodeInvalidRequest, err.Error())
		return
	}
	u, tokens, err := h.svc.Login(c.Request.Context(), req.Email, req.Password)
	if err != nil {
		switch {
		case errors.Is(err, ErrInvalidCredentials), errors.Is(err, ErrPasswordNotSet):
			response.Error(c, http.StatusUnauthorized, response.CodeInvalidCredential, "invalid email or password")
		default:
			response.Error(c, http.StatusInternalServerError, response.CodeInternal, "failed to login")
		}
		return
	}
	response.OK(c, authResp{User: u, Tokens: tokens})
}

func (h *Handler) refresh(c *gin.Context) {
	var req refreshReq
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Error(c, http.StatusBadRequest, response.CodeInvalidRequest, err.Error())
		return
	}
	tokens, err := h.svc.Refresh(c.Request.Context(), req.RefreshToken)
	if err != nil {
		if errors.Is(err, ErrRefreshTokenInvalid) {
			response.Error(c, http.StatusUnauthorized, response.CodeTokenInvalid, "refresh token invalid or expired")
			return
		}
		response.Error(c, http.StatusInternalServerError, response.CodeInternal, "failed to refresh")
		return
	}
	response.OK(c, tokens)
}

func (h *Handler) logout(c *gin.Context) {
	var req refreshReq
	if err := c.ShouldBindJSON(&req); err != nil {
		response.Error(c, http.StatusBadRequest, response.CodeInvalidRequest, err.Error())
		return
	}
	if err := h.svc.Logout(c.Request.Context(), req.RefreshToken); err != nil {
		response.Error(c, http.StatusInternalServerError, response.CodeInternal, "failed to logout")
		return
	}
	response.NoContent(c)
}

func (h *Handler) googleLogin(c *gin.Context) {
	url, state, err := h.svc.GoogleAuthURL()
	if err != nil {
		response.Error(c, http.StatusInternalServerError, response.CodeInternal, "oauth not available")
		return
	}
	c.SetSameSite(http.SameSiteLaxMode)
	c.SetCookie(oauthStateCookie, state, oauthStateCookieMaxAge, "/", "", h.cookieSecure, true)
	c.Redirect(http.StatusFound, url)
}

func (h *Handler) googleCallback(c *gin.Context) {
	code := c.Query("code")
	state := c.Query("state")
	cookie, _ := c.Cookie(oauthStateCookie)
	// Clear the state cookie regardless of outcome.
	c.SetSameSite(http.SameSiteLaxMode)
	c.SetCookie(oauthStateCookie, "", -1, "/", "", h.cookieSecure, true)

	if code == "" {
		response.Error(c, http.StatusBadRequest, response.CodeInvalidRequest, "missing authorization code")
		return
	}
	u, tokens, err := h.svc.GoogleCallback(c.Request.Context(), code, state, cookie)
	if err != nil {
		switch {
		case errors.Is(err, ErrOAuthState):
			response.Error(c, http.StatusBadRequest, response.CodeInvalidRequest, "invalid oauth state")
		default:
			response.Error(c, http.StatusInternalServerError, response.CodeInternal, "oauth exchange failed")
		}
		return
	}
	response.OK(c, authResp{User: u, Tokens: tokens})
}

// Ensure Service satisfies middleware.AccessTokenVerifier.
var _ middleware.AccessTokenVerifier = (*Service)(nil)
