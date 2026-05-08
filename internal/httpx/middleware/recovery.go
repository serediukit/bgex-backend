package middleware

import (
	"log/slog"
	"net/http"
	"runtime/debug"

	"github.com/gin-gonic/gin"

	"github.com/serediukit/bgex-backend/internal/httpx/response"
)

// Recovery logs panics with a stack trace and responds with a 500 error envelope.
func Recovery(logger *slog.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		defer func() {
			if rec := recover(); rec != nil {
				logger.ErrorContext(c.Request.Context(), "panic",
					"panic", rec,
					"path", c.Request.URL.Path,
					"request_id", RequestIDFrom(c.Request.Context()),
					"stack", string(debug.Stack()),
				)
				response.Error(c, http.StatusInternalServerError, response.CodeInternal, "internal error")
			}
		}()
		c.Next()
	}
}
