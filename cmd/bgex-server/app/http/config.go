package http

import (
	"strings"
	"time"

	"github.com/spf13/viper"
)

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

// ViperSubsetKey is the top-level config key holding HTTP server settings.
const ViperSubsetKey = "server"

func serverConfigFromViper(v *viper.Viper) *ServerConfig {
	return &ServerConfig{
		Env:  v.GetString(ViperSubsetKey + ".env"),
		Port: v.GetString(ViperSubsetKey + ".port"),
		JWT: JWTConfig{
			Secret:          v.GetString(ViperSubsetKey + ".jwt.secret"),
			AccessTTL:       v.GetDuration(ViperSubsetKey + ".jwt.access_ttl"),
			RefreshTokenTTL: v.GetDuration(ViperSubsetKey + ".jwt.refresh_token_ttl"),
		},
		GoogleOAuth: GoogleOAuthConfig{
			ClientID:     v.GetString(ViperSubsetKey + ".google_oauth.client_id"),
			ClientSecret: v.GetString(ViperSubsetKey + ".google_oauth.client_secret"),
			RedirectURL:  v.GetString(ViperSubsetKey + ".google_oauth.redirect_url"),
		},
		CORS: CORSConfig{
			AllowedOrigins: splitAndTrim(v.GetString(ViperSubsetKey + ".cors.allowed_origins")),
		},
	}
}

// splitAndTrim splits a comma-separated string into a slice, trimming spaces
// and dropping empty entries. viper's GetStringSlice splits values sourced from
// env vars on whitespace rather than commas, so we parse the raw string here to
// honor the documented comma-separated CORS_ALLOWED_ORIGINS format.
func splitAndTrim(s string) []string {
	if s == "" {
		return nil
	}

	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if trimmed := strings.TrimSpace(p); trimmed != "" {
			out = append(out, trimmed)
		}
	}

	return out
}

func (sc *ServerConfig) isProduction() bool {
	return sc.Env == "production"
}

func (gac GoogleOAuthConfig) enabled() bool {
	return gac.ClientID != "" && gac.ClientSecret != "" && gac.RedirectURL != ""
}
