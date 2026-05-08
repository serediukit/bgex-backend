package middleware

import (
	"context"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

const (
	HeaderRequestID = "X-Request-ID"
	ctxKeyRequestID = ctxKey("request_id")
)

type ctxKey string

// RequestID ensures every request has an X-Request-ID header, reusing the
// caller-supplied one if present. The value is stored on the gin.Context and
// the request's context.Context, and echoed in the response header.
func RequestID() gin.HandlerFunc {
	return func(c *gin.Context) {
		id := c.GetHeader(HeaderRequestID)
		if id == "" {
			id = uuid.NewString()
		}
		c.Header(HeaderRequestID, id)
		c.Set(string(ctxKeyRequestID), id)
		c.Request = c.Request.WithContext(context.WithValue(c.Request.Context(), ctxKeyRequestID, id))
		c.Next()
	}
}

// RequestIDFrom extracts the request ID from a context, empty string if missing.
func RequestIDFrom(ctx context.Context) string {
	if v, ok := ctx.Value(ctxKeyRequestID).(string); ok {
		return v
	}
	return ""
}
