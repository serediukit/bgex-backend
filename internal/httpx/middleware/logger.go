package middleware

import (
	"time"

	"github.com/gin-gonic/gin"
	"github.com/sirupsen/logrus"
)

// AccessLog emits a structured logger line per request using the given logrus.Logger.
func AccessLog(logger *logrus.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		path := c.Request.URL.Path
		raw := c.Request.URL.RawQuery

		c.Next()

		status := c.Writer.Status()
		fields := logrus.Fields{
			"method":      c.Request.Method,
			"path":        path,
			"status":      status,
			"duration_ms": time.Since(start).Milliseconds(),
			"client_ip":   c.ClientIP(),
			"request_id":  RequestIDFrom(c.Request.Context()),
		}
		if raw != "" {
			fields["query"] = raw
		}
		if len(c.Errors) > 0 {
			fields["errors"] = c.Errors.String()
		}

		logger.WithContext(c.Request.Context()).WithFields(fields).Log(levelFromStatus(status), "http_request")
	}
}

func levelFromStatus(status int) logrus.Level {
	switch {
	case status >= 500:
		return logrus.ErrorLevel
	case status >= 400:
		return logrus.WarnLevel
	default:
		return logrus.InfoLevel
	}
}
