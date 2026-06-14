package http

import (
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

func serverConfigFromViper(v *viper.Viper) *ServerConfig {
	return &ServerConfig{
		Env:  v.GetString("env"),
		Port: v.GetString("port"),
		JWT: JWTConfig{
			Secret:          v.GetString("secret"),
			AccessTTL:       v.GetDuration("access_ttl"),
			RefreshTokenTTL: v.GetDuration("refresh_ttl"),
		},
		GoogleOAuth: GoogleOAuthConfig{
			ClientID:     v.GetString("client_id"),
			ClientSecret: v.GetString("client_secret"),
			RedirectURL:  v.GetString("redirect_url"),
		},
		CORS: CORSConfig{
			AllowedOrigins: v.GetStringSlice("allowed_origins"),
		},
	}
}

func (sc *ServerConfig) isProduction() bool {
	return sc.Env == "production"
}

func (gac GoogleOAuthConfig) enabled() bool {
	return gac.ClientID != "" && gac.ClientSecret != "" && gac.RedirectURL != ""
}
