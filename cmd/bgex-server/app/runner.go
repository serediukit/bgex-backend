package app

import (
	"context"
	"sync/atomic"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
	"github.com/serediukit/bgex-backend/pkg/clock"
	"github.com/serediukit/bgex-backend/pkg/health"
	"github.com/serediukit/bgex-backend/pkg/logger"
	"github.com/sirupsen/logrus"
	"github.com/spf13/viper"
	"golang.org/x/sync/errgroup"
)

const (
	Waiting = iota
	Running
)

type Plugin interface {
	Name() string
	Run(r *Runner, ctx context.Context, opts *Options) error
}

type Runner struct {
	startTime     time.Time
	DB            *pgxpool.Pool
	Redis         *redis.Client
	Viper         *viper.Viper
	HealthChecker *health.BackgroundChecker
	plugins       []Plugin
	status        atomic.Int32
}

func NewRunner(plugins ...Plugin) *Runner {
	return &Runner{
		startTime:     clock.Now(),
		plugins:       plugins,
		HealthChecker: health.NewBackgroundChecker(),
	}
}

func (r *Runner) IsRunning() bool {
	return r.status.Load() == Running
}

func (r *Runner) Run(ctx context.Context, opts *Options) error {
	eg, egCtx := errgroup.WithContext(ctx)

	for _, p := range r.plugins {
		plugin := p

		eg.Go(func() error {
			log := logrus.WithField("plugin", plugin.Name())

			log.Infoln("Starting", plugin.Name())
			defer log.Infoln("Stopped", plugin.Name())

			return plugin.Run(r, logger.WithContext(egCtx, log), opts)
		})
	}

	r.status.Store(Running)

	return eg.Wait()
}

type RunnableFn func(ctx context.Context, opts *Options) error

type RunInterceptor func(runner *Runner, ctx context.Context, runnable RunnableFn, opts *Options) error

type InstrumentedRunner struct {
	runner      *Runner
	interceptor RunInterceptor
}

func NewInstrumentedRunner(runner *Runner, interceptors ...RunInterceptor) *InstrumentedRunner {
	return &InstrumentedRunner{
		runner:      runner,
		interceptor: chainInterceptors(interceptors),
	}
}

func (r *InstrumentedRunner) Run(ctx context.Context, opts *Options) error {
	return r.interceptor(r.runner, ctx, r.runner.Run, opts)
}

func chainInterceptors(interceptors []RunInterceptor) RunInterceptor {
	return func(runner *Runner, ctx context.Context, runnable RunnableFn, opts *Options) error {
		var (
			i    int
			next RunnableFn
		)

		next = func(ctx context.Context, opts *Options) error {
			if i == len(interceptors)-1 {
				return interceptors[i](runner, ctx, runnable, opts)
			}
			i++

			return interceptors[i-1](runner, ctx, next, opts)
		}

		return next(ctx, opts)
	}
}
