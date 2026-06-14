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
				return NewNotReadyError(err, "postgres")
			}

			return fmt.Errorf("starting postgres: %w", err)
		}

		// pgxpool connects lazily, so ping to confirm the database is actually
		// reachable before declaring postgres ready.
		if err := db.Ping(ctx); err != nil {
			db.Close()

			return NewNotReadyError(err, "postgres")
		}

		defer db.Close()

		runner.DB = db
		runner.HealthChecker.AddStatusChecker(pgconn.HealthChecker(db))

		return runnable(ctx, opts)
	}
}
