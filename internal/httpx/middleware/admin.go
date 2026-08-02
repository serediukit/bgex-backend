package middleware

import (
	"context"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/serediukit/bgex-backend/internal/httpx/response"
)

// RoleLookup resolves a user's role (satisfied by user.Repository).
type RoleLookup interface {
	GetRole(ctx context.Context, id uuid.UUID) (string, error)
}

// RequireAdmin must be chained AFTER RequireAuth. It 403s unless the
// authenticated user's role is 'admin'.
func RequireAdmin(l RoleLookup) gin.HandlerFunc {
	return func(c *gin.Context) {
		userID := UserIDFrom(c.Request.Context())
		if userID == uuid.Nil {
			response.Error(c, http.StatusUnauthorized, response.CodeUnauthorized, "missing bearer token")
			return
		}
		role, err := l.GetRole(c.Request.Context(), userID)
		if err != nil {
			response.Error(c, http.StatusInternalServerError, response.CodeInternal, "internal error")
			return
		}
		if role != "admin" {
			response.Error(c, http.StatusForbidden, response.CodeForbidden, "admin access required")
			return
		}
		c.Next()
	}
}
