package config

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/joho/godotenv"
	"github.com/spf13/viper"
)

type Config struct {
	Server   ServerConfig   `mapstructure:"bgex-server"`
	Postgres PostgresConfig `mapstructure:"postgres"`
	Redis    RedisConfig    `mapstructure:"redis"`
}

type ServerConfig struct {
	Env         string            `mapstructure:"env"`
	Port        string            `mapstructure:"port"`
	JWT         JWTConfig         `mapstructure:"jwt"`
	GoogleOAuth GoogleOAuthConfig `mapstructure:"google_oauth"`
	CORS        CORSConfig        `mapstructure:"cors"`
}

type JWTConfig struct {
	Secret          string        `mapstructure:"secret"`
	AccessTTL       time.Duration `mapstructure:"access_ttl"`
	RefreshTokenTTL time.Duration `mapstructure:"refresh_token_ttl"`
}

type GoogleOAuthConfig struct {
	ClientID     string `mapstructure:"client_id"`
	ClientSecret string `mapstructure:"client_secret"`
	RedirectURL  string `mapstructure:"redirect_url"`
}

type CORSConfig struct {
	AllowedOrigins []string `mapstructure:"allowed_origins"`
}

type PostgresConfig struct {
	URL string `mapstructure:"url"`
}

type RedisConfig struct {
	URL string `mapstructure:"url"`
}

func (c *Config) IsProduction() bool {
	return strings.EqualFold(c.Server.Env, "production")
}

func (g GoogleOAuthConfig) Enabled() bool {
	return g.ClientID != "" && g.ClientSecret != "" && g.RedirectURL != ""
}

// Load reads config from ./config/server.yaml. If a .env file exists in cwd,
// it is loaded first so its vars are visible to viper's BindEnv overrides.
// Env vars override YAML values for secrets and deployment-specific settings.
func Load() (*Config, error) {
	_ = godotenv.Load()

	v := viper.New()
	v.SetConfigName("server")
	v.SetConfigType("yaml")
	v.AddConfigPath("./config")
	v.AddConfigPath("../config")
	v.AddConfigPath("../../config")

	v.SetDefault("bgex-server.env", "development")
	v.SetDefault("bgex-server.port", "8080")
	v.SetDefault("bgex-server.jwt.access_ttl", "15m")
	v.SetDefault("bgex-server.jwt.refresh_token_ttl", "720h")

	bindings := map[string]string{
		"bgex-server.env":                        "APP_ENV",
		"bgex-server.port":                       "APP_PORT",
		"bgex-server.jwt.secret":                 "JWT_SECRET",
		"bgex-server.jwt.access_ttl":             "JWT_ACCESS_TTL",
		"bgex-server.jwt.refresh_token_ttl":      "REFRESH_TOKEN_TTL",
		"bgex-server.google_oauth.client_id":     "GOOGLE_OAUTH_CLIENT_ID",
		"bgex-server.google_oauth.client_secret": "GOOGLE_OAUTH_CLIENT_SECRET",
		"bgex-server.google_oauth.redirect_url":  "GOOGLE_OAUTH_REDIRECT_URL",
		"bgex-server.cors.allowed_origins":       "CORS_ALLOWED_ORIGINS",
		"postgres.url":                           "DATABASE_URL",
		"redis.url":                              "REDIS_URL",
	}
	for key, envVar := range bindings {
		if err := v.BindEnv(key, envVar); err != nil {
			return nil, fmt.Errorf("bind env %s: %w", envVar, err)
		}
	}

	if err := v.ReadInConfig(); err != nil {
		var notFound viper.ConfigFileNotFoundError
		if !errors.As(err, &notFound) {
			return nil, fmt.Errorf("read config: %w", err)
		}
	}

	var cfg Config
	if err := v.Unmarshal(&cfg); err != nil {
		return nil, fmt.Errorf("unmarshal config: %w", err)
	}

	if err := validate(&cfg); err != nil {
		return nil, err
	}

	return &cfg, nil
}

func validate(cfg *Config) error {
	if len(cfg.Server.JWT.Secret) < 32 {
		return fmt.Errorf("JWT_SECRET must be at least 32 bytes")
	}
	if cfg.Postgres.URL == "" {
		return fmt.Errorf("postgres.url (DATABASE_URL) is required")
	}
	if cfg.Redis.URL == "" {
		return fmt.Errorf("redis.url (REDIS_URL) is required")
	}
	return nil
}
