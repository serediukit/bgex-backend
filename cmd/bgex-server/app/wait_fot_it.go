package app

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/serediukit/bgex-backend/pkg/logger"
)

type NotReadyError struct {
	serviceName string
	err         error
}

func NewNotReadyError(err error, serviceName string) *NotReadyError {
	return &NotReadyError{err: err, serviceName: serviceName}
}

func (e *NotReadyError) Error() string {
	return fmt.Sprintf("service [%s] not ready: %s", e.serviceName, e.err)
}

func (e *NotReadyError) Unwrap() error { return e.err }

func WaitForIt(next RunInterceptor) RunInterceptor {
	return func(runner *Runner, ctx context.Context, runnable RunnableFn, opts *Options) error {
		return waitForIt(ctx, opts.StartupPeriod, func() error {
			return next(runner, ctx, runnable, opts)
		})
	}
}

func waitForIt(ctx context.Context, period time.Duration, fn func() error) error {
	ticker := time.NewTicker(period)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
			err := fn()
			if !isNotReadyError(err) {
				return err
			}

			logger.FromContext(ctx).Error("Wait for:", err)

			<-ticker.C
		}
	}
}

func isNotReadyError(err error) bool {
	var connectionError *NotReadyError

	return errors.As(err, &connectionError)
}
