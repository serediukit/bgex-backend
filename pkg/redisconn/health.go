package redisconn

import (
	"context"
	"fmt"

	"github.com/redis/go-redis/v9"
	"github.com/serediukit/bgex-backend/pkg/health"
)

func HealthChecker(client *redis.Client) health.StatusCheckerFunc {
	return func(ctx context.Context) error {
		if err := client.Ping(ctx).Err(); err != nil {
			return fmt.Errorf("redis not healthy: %w", err)
		}

		return nil
	}
}
