package logger

import (
	"context"

	"github.com/sirupsen/logrus"
)

type contextKey struct{}

func WithContext(ctx context.Context, logger *logrus.Entry) context.Context {
	return context.WithValue(ctx, contextKey{}, logger)
}

func FromContext(ctx context.Context) *logrus.Entry {
	if l := ctx.Value(contextKey{}); l != nil {
		return l.(*logrus.Entry)
	}

	return logrus.NewEntry(logrus.StandardLogger())
}
