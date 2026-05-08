package middleware

import (
	"context"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/serediukit/bgex-backend/internal/httpx/response"
)

// AccessTokenVerifier validates a raw access token and returns the authenticated user id.
type AccessTokenVerifier interface {
	VerifyAccessToken(ctx context.Context, raw string) (uuid.UUID, error)
}

const ctxKeyUserID = ctxKey("user_id")

// RequireAuth is a gin middleware that requires a valid bearer access token.
// On success it stores the user id on the request context.
func RequireAuth(v AccessTokenVerifier) gin.HandlerFunc {
	return func(c *gin.Context) {
		raw := extractBearer(c.GetHeader("Authorization"))
		if raw == "" {
			response.Error(c, http.StatusUnauthorized, response.CodeUnauthorized, "missing bearer token")
			return
		}
		userID, err := v.VerifyAccessToken(c.Request.Context(), raw)
		if err != nil {
			response.Error(c, http.StatusUnauthorized, response.CodeTokenInvalid, "invalid or expired token")
			return
		}
		c.Set(string(ctxKeyUserID), userID)
		c.Request = c.Request.WithContext(context.WithValue(c.Request.Context(), ctxKeyUserID, userID))
		c.Next()
	}
}

// UserIDFrom returns the authenticated user id from the context, or the zero
// UUID if the request was not authenticated.
func UserIDFrom(ctx context.Context) uuid.UUID {
	if v, ok := ctx.Value(ctxKeyUserID).(uuid.UUID); ok {
		return v
	}
	return uuid.Nil
}

func extractBearer(header string) string {
	const prefix = "Bearer "
	if !strings.HasPrefix(header, prefix) {
		return ""
	}
	return strings.TrimSpace(strings.TrimPrefix(header, prefix))
}
