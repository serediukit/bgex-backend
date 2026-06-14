package http

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/serediukit/bgex-backend/cmd/bgex-server/app"
	"github.com/serediukit/bgex-backend/internal/domain/auth"
	"github.com/serediukit/bgex-backend/internal/domain/friends"
	"github.com/serediukit/bgex-backend/internal/domain/user"
	"github.com/serediukit/bgex-backend/internal/httpx"
	"github.com/serediukit/bgex-backend/internal/httpx/middleware"
	"github.com/serediukit/bgex-backend/pkg/logger"
)

func NewServer(r *app.Runner) (*http.Server, error) {
	serverConfig := serverConfigFromViper(r.Viper)

	// --- wire domain dependencies ---
	userRepo := user.NewRepository(r.DB)
	authRepo := auth.NewRepository(r.DB)
	refreshStore := auth.NewRefreshTokenStore(r.Redis)
	jwtIssuer := auth.NewJWTIssuer(serverConfig.JWT.Secret, serverConfig.JWT.AccessTTL)

	var googleOAuth *auth.GoogleOAuth
	if serverConfig.GoogleOAuth.enabled() {
		googleOAuth = auth.NewGoogleOAuth(serverConfig.GoogleOAuth.ClientID, serverConfig.GoogleOAuth.ClientSecret, serverConfig.GoogleOAuth.RedirectURL)
	} else {
		logger.FromContext(ctx).Warn("google oauth disabled — set GOOGLE_OAUTH_CLIENT_ID/SECRET/REDIRECT_URL to enable")
	}

	oauthStateKey := deriveStateKey(serverConfig.JWT.Secret)
	authSvc := auth.NewService(userRepo, authRepo, refreshStore, jwtIssuer, googleOAuth, serverConfig.JWT.RefreshTokenTTL, oauthStateKey)
	authHandler := auth.NewHandler(authSvc, serverConfig.isProduction())
	userSvc := user.NewService(userRepo)
	userHandler := user.NewHandler(userRepo, userSvc)

	friendsRepo := friends.NewRepository(r.DB)
	friendsSvc := friends.NewService(friendsRepo)
	friendsHandler := friends.NewHandler(friendsSvc)

	authMW := middleware.RequireAuth(authSvc)

	router := httpx.NewRouter(httpx.RouterOptions{
		Logger:         logger.FromContext(ctx),
		AllowedOrigins: serverConfig.CORS.AllowedOrigins,
		ReadyCheck:     r.HealthChecker.CheckStatus(ctx),
		APIRoutes: []httpx.RouteRegistrar{
			authHandler.Register(authMW),
			userHandler.Register(authMW),
			friendsHandler.Register(authMW),
		},
	})

	srv := &http.Server{
		Addr:              ":" + serverConfig.Port,
		Handler:           router,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      15 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	errCh := make(chan error, 1)
	go func() {
		logger.FromContext(ctx).Info("http server listening", "addr", srv.Addr, "env", serverConfig.Env)
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
