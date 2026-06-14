package middleware

import (
	"net/http"
	"runtime/debug"

	"github.com/gin-gonic/gin"
	"github.com/sirupsen/logrus"

	"github.com/serediukit/bgex-backend/internal/httpx/response"
)

// Recovery logs panics with a stack trace and responds with a 500 error envelope.
func Recovery(logger *logrus.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		defer func() {
			if rec := recover(); rec != nil {
				logger.WithContext(c.Request.Context()).WithFields(logrus.Fields{
					"panic":      rec,
					"path":       c.Request.URL.Path,
					"request_id": RequestIDFrom(c.Request.Context()),
					"stack":      string(debug.Stack()),
				}).Error("panic")
				response.Error(c, http.StatusInternalServerError, response.CodeInternal, "internal error")
			}
		}()
		c.Next()
	}
}
