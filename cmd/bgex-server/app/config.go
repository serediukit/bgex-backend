package app

import (
	"context"
	"fmt"
	"strings"

	"github.com/joho/godotenv"
	"github.com/serediukit/bgex-backend/pkg/pgconn"
	"github.com/serediukit/bgex-backend/pkg/redisconn"
	"github.com/spf13/viper"
)

// serverConfigKey is the top-level config key holding HTTP server settings.
// It mirrors http.ViperSubsetKey but is duplicated here to avoid an import
// cycle (the http package depends on this package).
const serverConfigKey = "server"

// envBindings maps dotted config keys to the (unprefixed) env vars documented
// in .env.example, so existing operator conventions keep working alongside the
// EnvPrefix-based AutomaticEnv lookups.
var envBindings = map[string]string{
	serverConfigKey + ".env":                        "APP_ENV",
	serverConfigKey + ".port":                       "APP_PORT",
	serverConfigKey + ".jwt.secret":                 "JWT_SECRET",
	serverConfigKey + ".jwt.access_ttl":             "JWT_ACCESS_TTL",
	serverConfigKey + ".jwt.refresh_token_ttl":      "REFRESH_TOKEN_TTL",
	serverConfigKey + ".google_oauth.client_id":     "GOOGLE_OAUTH_CLIENT_ID",
	serverConfigKey + ".google_oauth.client_secret": "GOOGLE_OAUTH_CLIENT_SECRET",
	serverConfigKey + ".google_oauth.redirect_url":  "GOOGLE_OAUTH_REDIRECT_URL",
	serverConfigKey + ".cors.allowed_origins":       "CORS_ALLOWED_ORIGINS",
}

func DefaultConfig() map[string]any {
	return map[string]any{
		redisconn.ViperSubsetKey: redisconn.DefaultConfig(),
		pgconn.ViperSubsetKey:    pgconn.DefaultConfig(),
	}
}

func WithViper(defaults map[string]any) RunInterceptor {
	return func(runner *Runner, ctx context.Context, runnable RunnableFn, opts *Options) error {
		v, err := resolveConfiguration(opts.ConfigFile, opts.EnvPrefix, defaults)
		if err != nil {
			return err
		}

		runner.Viper = v

		return runnable(ctx, opts)
	}
}

func resolveConfiguration(configFile, envPrefix string, defaults map[string]interface{}) (*viper.Viper, error) {
	// Load .env if present so its vars are visible to the bindings below. Absence
	// is not an error — env vars may be provided by the environment directly.
	_ = godotenv.Load()

	v := viper.NewWithOptions(
		viper.EnvKeyReplacer(strings.NewReplacer(".", "_")),
	)
	v.SetConfigFile(configFile)
	v.SetEnvPrefix(envPrefix)

	for key, val := range defaults {
		v.SetDefault(key, val)
	}

	v.AutomaticEnv()

	for key, envVar := range envBindings {
		if err := v.BindEnv(key, envVar); err != nil {
			return nil, fmt.Errorf("viper bind env %s: %w", envVar, err)
		}
	}

	if err := v.ReadInConfig(); err != nil {
		return nil, fmt.Errorf("viper read in config: %w", err)
	}

	if err := fixNestedDefaultValues(v, defaults); err != nil {
		return nil, err
	}

	return v, nil
}

func fixNestedDefaultValues(v *viper.Viper, defaults map[string]interface{}) error {
	settings := v.AllSettings()

	if err := v.MergeConfigMap(defaults); err != nil {
		return err
	}

	return v.MergeConfigMap(settings)
}
