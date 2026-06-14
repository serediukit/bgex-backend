package app

import (
	"context"
	"fmt"
	"strings"

	"github.com/serediukit/bgex-backend/pkg/pgconn"
	"github.com/serediukit/bgex-backend/pkg/redisconn"
	"github.com/spf13/viper"
)

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
	v := viper.NewWithOptions(
		viper.EnvKeyReplacer(strings.NewReplacer(".", "_")),
	)
	v.SetConfigFile(configFile)
	v.SetEnvPrefix(envPrefix)

	for key, val := range defaults {
		v.SetDefault(key, val)
	}

	v.AutomaticEnv()

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
