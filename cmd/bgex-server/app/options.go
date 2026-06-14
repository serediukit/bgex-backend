package app

import (
	"time"
)

type Options struct {
	EnvPrefix             string
	ConfigFile            string
	Address               string
	MetricsAddress        string
	LogLevel              string
	LogFile               string
	StartupTimeout        time.Duration
	StartupPeriod         time.Duration
	ShutdownDelay         time.Duration
	CollectGoMetrics      bool
	CollectProcessMetrics bool
	EnablePprof           bool
}

func DefaultOptions() *Options {
	return &Options{
		Address:        ":8080",
		MetricsAddress: ":9100",
		ConfigFile:     "config/server.yaml",
		EnvPrefix:      "BGEX",
		StartupPeriod:  time.Second,
		StartupTimeout: time.Second * 15,
		ShutdownDelay:  time.Second * 10,
		LogLevel:       "info",
	}
}
