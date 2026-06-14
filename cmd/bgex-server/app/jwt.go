package app

import (
	"context"

	"github.com/serediukit/bgex-backend/internal/domain/auth"
	"github.com/serediukit/bgex-backend/pkg/redisconn"
)

const jwtviperSubsetKey

func WithJWT() RunInterceptor {
	return func(runner *Runner, ctx context.Context, runnable RunnableFn, opts *Options) error {
		cfg := runner.Viper.Sub(redisconn.ViperSubsetKey)

		runner.JWTIssuer = auth.NewJWTIssuer(cfg.Server.JWT.Secret, cfg.Server.JWT.AccessTTL)
	}
}
