package http

import (
	"context"
	"crypto/sha256"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/serediukit/bgex-backend/cmd/bgex-server/app"
	"github.com/serediukit/bgex-backend/internal/domain/auth"
	"github.com/serediukit/bgex-backend/internal/domain/friends"
	"github.com/serediukit/bgex-backend/internal/domain/user"
	"github.com/serediukit/bgex-backend/internal/games/engine"
	"github.com/serediukit/bgex-backend/internal/games/lobby"
	"github.com/serediukit/bgex-backend/internal/games/poker"
	"github.com/serediukit/bgex-backend/internal/games/realtime"
	"github.com/serediukit/bgex-backend/internal/games/ttr"
	"github.com/serediukit/bgex-backend/internal/httpx"
	"github.com/serediukit/bgex-backend/internal/httpx/middleware"
	"github.com/serediukit/bgex-backend/pkg/logger"
)

// NewServer wires the domain dependencies and builds the *http.Server. Serving
// and graceful shutdown are handled by the plugin (see plugin.go).
func NewServer(ctx context.Context, r *app.Runner) (*http.Server, error) {
	log := logger.FromContext(ctx)

	serverConfig := serverConfigFromViper(r.Viper)

	if serverConfig.isProduction() {
		gin.SetMode(gin.ReleaseMode)
	}

	// --- wire domain dependencies ---
	userRepo := user.NewRepository(r.DB)
	authRepo := auth.NewRepository(r.DB)
	refreshStore := auth.NewRefreshTokenStore(r.Redis)
	jwtIssuer := auth.NewJWTIssuer(serverConfig.JWT.Secret, serverConfig.JWT.AccessTTL)

	var googleOAuth *auth.GoogleOAuth
	if serverConfig.GoogleOAuth.enabled() {
		googleOAuth = auth.NewGoogleOAuth(serverConfig.GoogleOAuth.ClientID, serverConfig.GoogleOAuth.ClientSecret, serverConfig.GoogleOAuth.RedirectURL)
	} else {
		log.Warn("google oauth disabled — set GOOGLE_OAUTH_CLIENT_ID/SECRET/REDIRECT_URL to enable")
	}

	oauthStateKey := deriveStateKey(serverConfig.JWT.Secret)
	authSvc := auth.NewService(userRepo, authRepo, refreshStore, jwtIssuer, googleOAuth, serverConfig.JWT.RefreshTokenTTL, oauthStateKey)
	authHandler := auth.NewHandler(authSvc, serverConfig.isProduction())
	userSvc := user.NewService(userRepo)
	userHandler := user.NewHandler(userRepo, userSvc)

	friendsRepo := friends.NewRepository(r.DB)
	friendsSvc := friends.NewService(friendsRepo)
	friendsHandler := friends.NewHandler(friendsSvc)

	// --- games: reusable framework + poker ---
	pokerEngine := poker.New()
	pokerSession := poker.NewSession(r.DB, poker.NewStateRepo(), pokerEngine)

	// --- games: ticket to ride ---
	ttrMapRepo := ttr.NewMapRepo(r.DB)
	ttrMapCache := ttr.NewMapCache(ttrMapRepo)
	ttrEngine := ttr.New(ttrMapCache)
	ttrSession := ttr.NewSession(r.DB, ttr.NewStateRepo(), ttrEngine, ttrMapCache, ttrMapRepo)
	ttrHandler := ttr.NewMapHandler(ttrMapRepo, ttrMapCache)
	ttrAdmin := ttr.NewAdminHandler(ttrMapRepo, ttrMapCache)

	engines := engine.NewRegistry(pokerEngine, ttrEngine)
	lobbyRepo := lobby.NewRepository(r.DB)
	lobbySvc := lobby.NewService(lobbyRepo, engines,
		map[string]lobby.GameInitializer{
			poker.GameKey: pokerSession,
			ttr.GameKey:   ttrSession,
		},
		map[string]lobby.ConfigValidator{
			ttr.GameKey: ttrSession,
		},
		map[string]lobby.ResignHandler{
			ttr.GameKey: ttrSession,
		},
	)
	lobbyHandler := lobby.NewHandler(lobbySvc)

	hub := realtime.NewHub()
	realtimeHandler := realtime.NewHandler(authSvc, lobbySvc, hub, log.Logger, pokerSession, ttrSession)

	authMW := middleware.RequireAuth(authSvc)
	adminMW := middleware.RequireAdmin(userRepo) // userRepo satisfies middleware.RoleLookup

	router := httpx.NewRouter(httpx.RouterOptions{
		Logger:         log.Logger,
		AllowedOrigins: serverConfig.CORS.AllowedOrigins,
		ReadyCheck: func() error {
			checkCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()

			return r.HealthChecker.CheckStatus(checkCtx)
		},
		APIRoutes: []httpx.RouteRegistrar{
			authHandler.Register(authMW),
			userHandler.Register(authMW),
			friendsHandler.Register(authMW),
			lobbyHandler.Register(authMW),
			realtimeHandler.Register(authMW),
			ttrHandler.Register(authMW),
			ttrAdmin.Register(authMW, adminMW),
		},
	})

	srv := &http.Server{
		Handler:           router,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      15 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	return srv, nil
}

// deriveStateKey produces a 32-byte key for OAuth state HMAC, derived from the
// JWT secret so operators don't need a separate env var. Distinct domain of use
// from JWT signing thanks to SHA-256 with a domain-separation prefix.
func deriveStateKey(secret string) []byte {
	h := sha256.Sum256([]byte("bgex-oauth-state\x00" + secret))
	return h[:]
}
