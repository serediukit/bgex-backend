package pgconn

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/serediukit/bgex-backend/pkg/health"
)

func HealthChecker(db *pgxpool.Pool) health.StatusCheckerFunc {
	return func(ctx context.Context) error {
		if err := db.Ping(ctx); err != nil {
			return fmt.Errorf("postgres not healthy: %w", err)
		}

		return nil
	}
}
