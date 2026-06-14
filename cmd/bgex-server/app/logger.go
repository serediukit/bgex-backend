package app

import (
	"context"
	"fmt"
	"os"

	"github.com/serediukit/bgex-backend/pkg/logger"
	"github.com/sirupsen/logrus"
)

func WithLogger() RunInterceptor {
	return func(runner *Runner, ctx context.Context, runnable RunnableFn, opts *Options) error {
		level, err := logrus.ParseLevel(opts.LogLevel)
		if err != nil {
			return err
		}

		logOpts := []logger.Option{
			logger.WithLevel(level),
		}

		if opts.LogFile != "" {
			f, err := os.OpenFile(opts.LogFile, os.O_CREATE|os.O_RDWR, 0o666)
			if err != nil {
				return fmt.Errorf("opening log file[%s]: %w", opts.LogFile, err)
			}

			defer f.Close()

			logOpts = append(logOpts, logger.WithOutput(f))
		}

		logger.Configure(logOpts...)

		return runnable(ctx, opts)
	}
}
