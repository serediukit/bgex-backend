package health

import "context"

type Status uint8

const (
	StatusUnknown Status = iota
	StatusNoHealthy
	StatusHealthy
)

type StatusChecker interface {
	CheckStatus(ctx context.Context) error
}

type StatusCheckerFunc func(ctx context.Context) error

func (c StatusCheckerFunc) CheckStatus(ctx context.Context) error {
	return c(ctx)
}

type StatusListener interface {
	OnStatusChanged(status Status)
}
