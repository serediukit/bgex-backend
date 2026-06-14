package pgconn

import (
	"context"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/spf13/viper"
)

const ViperSubsetKey = "postgres"

func DefaultConfig() map[string]interface{} {
	return map[string]interface{}{
		"host":                   "localhost",
		"port":                   5432,
		"username":               "",
		"password":               "",
		"database":               "",
		"connectTimeoutSec":      1,
		"poolMaxConns":           "16",
		"poolMinConns":           "4",
		"poolMaxConnLifetime":    "8h",
		"poolMaxConnIdleTime":    "30m",
		"poolHealthCheckPeriod":  "1m",
		"applicationName":        "",
		"statementCacheCapacity": 100,
	}
}

func FromViper(ctx context.Context, v *viper.Viper) (*pgxpool.Pool, error) {
	cfg, err := pgxpool.ParseConfig(optionsFromViper(v).ToDSN())
	if err != nil {
		return nil, fmt.Errorf("pgconn: failed to parse config: %w", err)
	}

	return pgxpool.NewWithConfig(ctx, cfg)
}

func IsConnectionErr(err error) bool {
	switch {
	case err == nil:
		return false
	case strings.Contains(err.Error(), "dial tcp"):
		return true
	default:
		return false
	}
}

func optionsFromViper(v *viper.Viper) *poolConfig {
	return &poolConfig{
		Host:                   v.GetString("host"),
		Port:                   v.GetInt("port"),
		Username:               v.GetString("username"),
		Password:               v.GetString("password"),
		Database:               v.GetString("database"),
		ConnectTimeoutSec:      v.GetInt("connectTimeoutSec"),
		PoolMaxConns:           v.GetInt("poolMaxConns"),
		PoolMinConns:           v.GetInt("poolMinConns"),
		PoolMaxConnLifetime:    v.GetDuration("poolMaxConnLifetime"),
		PoolMaxConnIdleTime:    v.GetDuration("poolMaxConnIdleTime"),
		PoolHealthCheckPeriod:  v.GetDuration("poolHealthCheckPeriod"),
		ApplicationName:        v.GetString("applicationName"),
		StatementCacheCapacity: v.GetInt("statementCacheCapacity"),
	}
}
