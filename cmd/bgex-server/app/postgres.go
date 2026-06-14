package app

import (
	"context"
	"fmt"

	"github.com/serediukit/bgex-backend/pkg/pgconn"
	"github.com/sirupsen/logrus"
)

func WithPostgres() RunInterceptor {
	return func(runner *Runner, ctx context.Context, runnable RunnableFn, opts *Options) error {
		logrus.Infoln("Setup Postgres connection")

		db, err := pgconn.FromViper(ctx, runner.Viper.Sub(pgconn.ViperSubsetKey))
		if err != nil {
			if pgconn.IsConnectionErr(err) {
				return fmt.Errorf("postgres is not ready: %w", err)
			}

			return fmt.Errorf("starting postgres: %w", err)
		}

		defer db.Close()

		runner.DB = db
		runner.HealthChecker.AddStatusChecker(pgconn.HealthChecker(db))

		return runnable(ctx, opts)
	}
}
