package logger

import (
	"io"
	"os"

	"github.com/sirupsen/logrus"
)

type opts struct {
	out       io.Writer
	formatter logrus.Formatter
	level     logrus.Level
}

type Option func(*opts)

func WithOutput(w io.Writer) Option {
	return func(o *opts) {
		o.out = w
	}
}

func WithFormatter(formatter logrus.Formatter) Option {
	return func(o *opts) {
		o.formatter = formatter
	}
}

func WithLevel(l logrus.Level) Option {
	return func(o *opts) {
		o.level = l
	}
}

func Configure(opts ...Option) {
	o := defaultOpts()
	for _, opt := range opts {
		opt(o)
	}

	logrus.SetOutput(o.out)
	logrus.SetLevel(o.level)
	logrus.SetFormatter(o.formatter)
}

func defaultOpts() *opts {
	return &opts{
		out:       os.Stderr,
		formatter: new(logrus.JSONFormatter),
		level:     logrus.InfoLevel,
	}
}
