package app

import (
	"context"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/serediukit/bgex-backend/pkg/logger"
)

func WithTerminate() RunInterceptor {
	return func(runner *Runner, ctx context.Context, runnable RunnableFn, opts *Options) error {
		ctx, cancel := context.WithCancel(ctx)

		go func() {
			defer cancel()

			startupTimer := time.NewTimer(opts.StartupTimeout)
			defer startupTimer.Stop()

			quit := make(chan os.Signal, 1)
			signal.Notify(quit, os.Interrupt, syscall.SIGTERM)
			defer signal.Stop(quit)

			for {
				select {
				case <-startupTimer.C:
					if !runner.IsRunning() {
						logger.FromContext(ctx).Printf("Startup timeout, exiting")

						return
					}
				case s := <-quit:
					logger.FromContext(ctx).Printf("Received %s signal", s)

					if runner.IsRunning() && opts.ShutdownDelay > 0 {
						logger.FromContext(ctx).Printf("Wait %s before shutdown", opts.ShutdownDelay)

						time.Sleep(opts.ShutdownDelay)
					}

					return
				}
			}
		}()

		return runnable(ctx, opts)
	}
}
