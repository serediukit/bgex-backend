package httpx

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/sirupsen/logrus"

	"github.com/serediukit/bgex-backend/internal/httpx/middleware"
)

// RouteRegistrar registers routes onto a gin.RouterGroup. Domain packages
// expose a Register(*gin.RouterGroup) function matching this type.
type RouteRegistrar func(r *gin.RouterGroup)

// RouterOptions bundles the dependencies required to build the gin engine.
type RouterOptions struct {
	Logger         *logrus.Logger
	AllowedOrigins []string
	ReadyCheck     func() error
	APIRoutes      []RouteRegistrar
}

// NewRouter builds a gin.Engine with standard middleware, health endpoints,
// and the provided API route registrars mounted under /api/v1.
func NewRouter(opts RouterOptions) *gin.Engine {
	r := gin.New()
	r.RedirectTrailingSlash = false

	r.Use(middleware.RequestID())
	r.Use(middleware.AccessLog(opts.Logger))
	r.Use(middleware.Recovery(opts.Logger))
	r.Use(middleware.CORS(opts.AllowedOrigins))

	r.GET("/healthz", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	})
	r.GET("/readyz", func(c *gin.Context) {
		if opts.ReadyCheck != nil {
			if err := opts.ReadyCheck(); err != nil {
				c.JSON(http.StatusServiceUnavailable, gin.H{"status": "unavailable", "error": err.Error()})
				return
			}
		}
		c.JSON(http.StatusOK, gin.H{"status": "ready"})
	})

	api := r.Group("/api/v1")
	for _, reg := range opts.APIRoutes {
		reg(api)
	}

	return r
}
