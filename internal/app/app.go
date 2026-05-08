package app

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/serediukit/bgex-backend/internal/config"
	"github.com/serediukit/bgex-backend/internal/domain/auth"
	"github.com/serediukit/bgex-backend/internal/domain/friends"
	"github.com/serediukit/bgex-backend/internal/domain/user"
	"github.com/serediukit/bgex-backend/internal/httpx"
	"github.com/serediukit/bgex-backend/internal/httpx/middleware"
	"github.com/serediukit/bgex-backend/internal/postgres"
	"github.com/serediukit/bgex-backend/internal/redisx"
)

// Run is the application entrypoint. It blocks until ctx is canceled or the server fails.
func Run(ctx context.Context) error {
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	logger := newLogger(cfg)
	slog.SetDefault(logger)

	if cfg.IsProduction() {
		gin.SetMode(gin.ReleaseMode)
	}

	pool, err := postgres.New(ctx, cfg.Postgres.URL)
	if err != nil {
		return fmt.Errorf("connect postgres: %w", err)
	}
	defer pool.Close()

	redisClient, err := redisx.New(ctx, cfg.Redis.URL)
	if err != nil {
		return fmt.Errorf("connect redis: %w", err)
	}
	defer func() {
		if err := redisClient.Close(); err != nil {
			logger.Error("close redis connection: %s", err)
		}
	}()

	// --- wire domain dependencies ---
	userRepo := user.NewRepository(pool)
	authRepo := auth.NewRepository(pool)
	refreshStore := auth.NewRefreshTokenStore(redisClient)
	jwtIssuer := auth.NewJWTIssuer(cfg.Server.JWT.Secret, cfg.Server.JWT.AccessTTL)

	var googleOAuth *auth.GoogleOAuth
	if cfg.Server.GoogleOAuth.Enabled() {
		googleOAuth = auth.NewGoogleOAuth(cfg.Server.GoogleOAuth.ClientID, cfg.Server.GoogleOAuth.ClientSecret, cfg.Server.GoogleOAuth.RedirectURL)
	} else {
		logger.Warn("google oauth disabled — set GOOGLE_OAUTH_CLIENT_ID/SECRET/REDIRECT_URL to enable")
	}

	oauthStateKey := deriveStateKey(cfg.Server.JWT.Secret)
	authSvc := auth.NewService(userRepo, authRepo, refreshStore, jwtIssuer, googleOAuth, cfg.Server.JWT.RefreshTokenTTL, oauthStateKey)
	authHandler := auth.NewHandler(authSvc, cfg.IsProduction())
	userSvc := user.NewService(userRepo)
	userHandler := user.NewHandler(userRepo, userSvc)

	friendsRepo := friends.NewRepository(pool)
	friendsSvc := friends.NewService(friendsRepo)
	friendsHandler := friends.NewHandler(friendsSvc)

	authMW := middleware.RequireAuth(authSvc)

	router := httpx.NewRouter(httpx.RouterOptions{
		Logger:         logger,
		AllowedOrigins: cfg.Server.CORS.AllowedOrigins,
		ReadyCheck: func() error {
			pingCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			if err := pool.Ping(pingCtx); err != nil {
				return err
			}
			return redisClient.Ping(pingCtx).Err()
		},
		APIRoutes: []httpx.RouteRegistrar{
			authHandler.Register(authMW),
			userHandler.Register(authMW),
			friendsHandler.Register(authMW),
		},
	})

	srv := &http.Server{
		Addr:              ":" + cfg.Server.Port,
		Handler:           router,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      15 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	errCh := make(chan error, 1)
	go func() {
		logger.Info("http server listening", "addr", srv.Addr, "env", cfg.Server.Env)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
			return
		}
		errCh <- nil
	}()

	select {
	case <-ctx.Done():
		logger.Info("shutdown signal received")
	case err := <-errCh:
		if err != nil {
			return fmt.Errorf("http server: %w", err)
		}
		return nil
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		return fmt.Errorf("shutdown: %w", err)
	}
	return nil
}

func newLogger(cfg *config.Config) *slog.Logger {
	level := slog.LevelInfo
	if !cfg.IsProduction() {
		level = slog.LevelDebug
	}
	opts := &slog.HandlerOptions{Level: level}
	if cfg.IsProduction() {
		return slog.New(slog.NewJSONHandler(os.Stdout, opts))
	}
	return slog.New(slog.NewTextHandler(os.Stdout, opts))
}

// deriveStateKey produces a 32-byte key for OAuth state HMAC, derived from the
// JWT secret so operators don't need a separate env var. Distinct domain of use
// from JWT signing thanks to SHA-256 with a domain-separation prefix.
func deriveStateKey(secret string) []byte {
	h := sha256.Sum256([]byte("bgex-oauth-state\x00" + secret))
	return h[:]
}
