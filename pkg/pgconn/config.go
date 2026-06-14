package pgconn

import (
	"fmt"
	"strings"
	"time"
)

type poolConfig struct {
	Host                   string        `mapstructure:"host"`
	Port                   int           `mapstructure:"port"`
	Username               string        `mapstructure:"username"`
	Password               string        `mapstructure:"password"`
	Database               string        `mapstructure:"database"`
	ConnectTimeoutSec      int           `mapstructure:"connectTimeoutSec"`
	PoolMaxConns           int           `mapstructure:"poolMaxConns"`
	PoolMinConns           int           `mapstructure:"poolMinConns"`
	PoolMaxConnLifetime    time.Duration `mapstructure:"poolMaxConnLifetime"`
	PoolMaxConnIdleTime    time.Duration `mapstructure:"poolMaxConnIdleTime"`
	PoolHealthCheckPeriod  time.Duration `mapstructure:"poolHealthCheckPeriod"`
	ApplicationName        string        `mapstructure:"applicationName"`
	StatementCacheCapacity int           `mapstructure:"statementCacheCapacity"`
}

func (c *poolConfig) ToDSN() string {
	var params []string

	if c.ConnectTimeoutSec > 0 {
		params = append(params, fmt.Sprintf("%s=%d", "connect_timeout", c.ConnectTimeoutSec))
	}

	if c.PoolMaxConns > 0 {
		params = append(params, fmt.Sprintf("%s=%d", "pool_max_conns", c.PoolMaxConns))
	}

	if c.PoolMinConns > 0 {
		params = append(params, fmt.Sprintf("%s=%d", "pool_min_conns", c.PoolMinConns))
	}

	if c.PoolMaxConnLifetime > 0 {
		params = append(params, fmt.Sprintf("%s=%s", "pool_max_conn_lifetime", c.PoolMaxConnLifetime))
	}

	if c.PoolMaxConnIdleTime > 0 {
		params = append(params, fmt.Sprintf("%s=%s", "pool_max_conn_idle_time", c.PoolMaxConnIdleTime))
	}

	if c.PoolHealthCheckPeriod > 0 {
		params = append(params, fmt.Sprintf("%s=%s", "pool_health_check_period", c.PoolHealthCheckPeriod))
	}

	if c.ApplicationName != "" {
		params = append(params, fmt.Sprintf("%s=%s", "application_name", c.ApplicationName))
	}

	if c.StatementCacheCapacity > 0 {
		params = append(params, fmt.Sprintf("%s=%d", "statement_cache_capacity", c.StatementCacheCapacity))
	}

	dsn := fmt.Sprintf("postgres://%s:%s@%s:%d/%s", c.Username, c.Password, c.Host, c.Port, c.Database)

	if len(params) == 0 {
		return dsn
	}

	return dsn + "?" + strings.Join(params, "&")
}
