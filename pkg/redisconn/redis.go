package redisconn

import (
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/spf13/viper"
)

const ViperSubsetKey = "redis"

func DefaultConfig() map[string]any {
	return map[string]any{
		"cluster_host": "localhost",
		"cluster_port": 6379,
		"username":     "",
		"password":     "",
		"maxRetries":   0,
		"dialTimeout":  time.Second,
		"readTimeout":  time.Second,
		"writeTimeout": time.Second,
		"poolSize":     0,
		"minIdleConns": 3,
	}
}

func FromViper(v *viper.Viper) *redis.Client {
	return redis.NewClient(optionsFromViper(v))
}

func optionsFromViper(v *viper.Viper) *redis.Options {
	return &redis.Options{
		Addr:         fmt.Sprintf("%s:%d", v.GetString("cluster_host"), v.GetInt("cluster_port")),
		Username:     v.GetString("username"),
		Password:     v.GetString("password"),
		MaxRetries:   v.GetInt("maxRetries"),
		DialTimeout:  v.GetDuration("dialTimeout"),
		ReadTimeout:  v.GetDuration("readTimeout"),
		WriteTimeout: v.GetDuration("writeTimeout"),
		PoolSize:     v.GetInt("poolSize"),
		MinIdleConns: v.GetInt("minIdleConns"),
	}
}
