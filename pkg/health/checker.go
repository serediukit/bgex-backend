package health

import (
	"context"
	"sync"
	"time"
)

type Interceptor func(next InterceptorFunc) InterceptorFunc

type InterceptorFunc func(ctx context.Context, opts *CheckOptions) error

type CheckOptions struct {
	interceptors     []Interceptor
	initialDelay     time.Duration
	period           time.Duration
	timeout          time.Duration
	successThreshold int
	failureThreshold int
}

func defaultCheckOptions() *CheckOptions {
	return &CheckOptions{
		initialDelay:     time.Second,
		period:           time.Second,
		timeout:          time.Second,
		successThreshold: 2,
		failureThreshold: 2,
	}
}

type CheckOption func(o *CheckOptions)

func WithInterceptor(interceptor ...Interceptor) CheckOption {
	return func(o *CheckOptions) {
		o.interceptors = append(o.interceptors, interceptor...)
	}
}

type BackgroundChecker struct {
	listeners    []StatusListener
	checkers     []StatusChecker
	listenersMux sync.Mutex
	checkersMux  sync.Mutex
}

func NewBackgroundChecker() *BackgroundChecker {
	return &BackgroundChecker{}
}

type Logger interface {
	Errorf(format string, args ...any)
}

func WithLogger(logger Logger) Interceptor {
	return func(next InterceptorFunc) InterceptorFunc {
		return func(ctx context.Context, opts *CheckOptions) error {
			err := next(ctx, opts)
			if err != nil {
				logger.Errorf("Health check failed: %s", err)
			}

			return err
		}
	}
}

func (c *BackgroundChecker) AddStatusChecker(ch StatusChecker) {
	c.checkersMux.Lock()
	c.checkers = append(c.checkers, ch)
	c.checkersMux.Unlock()
}

func (c *BackgroundChecker) AddStatusListener(l StatusListener) {
	c.listenersMux.Lock()
	c.listeners = append(c.listeners, l)
	c.listenersMux.Unlock()
}

func (c *BackgroundChecker) Run(ctx context.Context, opts ...CheckOption) error {
	options := defaultCheckOptions()

	for _, opt := range opts {
		opt(options)
	}

	check := withInterceptors(options.interceptors, func(ctx context.Context, o *CheckOptions) error {
		ctx, cancel := context.WithTimeout(ctx, o.timeout)
		defer cancel()

		done := make(chan error, 1)

		go func() {
			done <- c.CheckStatus(ctx)
			close(done)
		}()

		select {
		case err := <-done:
			return err
		case <-ctx.Done():
			return ctx.Err()
		}
	})

	ticker := time.NewTicker(options.initialDelay)
	defer ticker.Stop()

	var (
		prevStatus = StatusUnknown
		success    = options.successThreshold
		failure    = options.failureThreshold
	)

	calcStatus := func(err error) Status {
		if err != nil {
			success = 0

			if options.failureThreshold < failure {
				failure++

				return prevStatus
			}

			return StatusNoHealthy
		}

		failure = 0

		if options.successThreshold < success {
			success++

			return prevStatus
		}

		return StatusHealthy
	}

	for {
		select {
		case <-ctx.Done():
			c.notifyStatusListeners(StatusNoHealthy)

			return nil
		case <-ticker.C:
			err := check(ctx, options)

			if curStatus := calcStatus(err); curStatus != prevStatus {
				prevStatus = curStatus

				c.notifyStatusListeners(curStatus)
			}

			ticker.Reset(options.period)
		}
	}
}

func (c *BackgroundChecker) CheckStatus(ctx context.Context) error {
	c.checkersMux.Lock()
	defer c.checkersMux.Unlock()

	for _, checker := range c.checkers {
		if err := checker.CheckStatus(ctx); err != nil {
			return err
		}
	}

	return nil
}

func (c *BackgroundChecker) notifyStatusListeners(status Status) {
	c.listenersMux.Lock()
	for _, l := range c.listeners {
		l.OnStatusChanged(status)
	}
	c.listenersMux.Unlock()
}

func withInterceptors(interceptors []Interceptor, target InterceptorFunc) InterceptorFunc {
	chain := target
	for idx := len(interceptors) - 1; idx >= 0; idx-- {
		chain = interceptors[idx](chain)
	}

	return chain
}
