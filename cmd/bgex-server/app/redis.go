package app

import (
	"context"

	"github.com/serediukit/bgex-backend/pkg/logger"
	"github.com/serediukit/bgex-backend/pkg/redisconn"
)

func WithRedis() RunInterceptor {
	return func(runner *Runner, ctx context.Context, runnable RunnableFn, opts *Options) error {
		logger.FromContext(ctx).Infoln("Initialize Redis client")

		runner.Redis = redisconn.FromViper(runner.Viper.Sub(redisconn.ViperSubsetKey))

		if _, err := runner.Redis.Ping(ctx).Result(); err != nil {
			return NewNotReadyError(err, "redis")
		}

		healthChecker := redisconn.HealthChecker(runner.Redis)
		if err := healthChecker(ctx); err != nil {
			return err
		}

		runner.HealthChecker.AddStatusChecker(healthChecker)

		err := runnable(ctx, opts)

		if closeErr := runner.Redis.Close(); closeErr != nil {
			logger.FromContext(ctx).Errorln("Failed to close Redis connection:", closeErr)
		}

		return err
	}
}
