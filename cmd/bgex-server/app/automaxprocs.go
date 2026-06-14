package app

import (
	"context"
	"fmt"

	"github.com/sirupsen/logrus"
	"go.uber.org/automaxprocs/maxprocs"
)

func WithAutoMaxProcs() RunInterceptor {
	return func(runner *Runner, ctx context.Context, runnable RunnableFn, opts *Options) error {
		if _, err := maxprocs.Set(maxprocs.Logger(logrus.Printf)); err != nil {
			return fmt.Errorf("maxprocs: %w", err)
		}

		return runnable(ctx, opts)
	}
}
