package http

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"

	"github.com/serediukit/bgex-backend/cmd/bgex-server/app"
	"github.com/serediukit/bgex-backend/pkg/logger"
)

type Plugin struct{}

func New() Plugin {
	return Plugin{}
}

func (Plugin) Name() string {
	return "http-server"
}

func (Plugin) Run(r *app.Runner, ctx context.Context, opts *app.Options) error {
	log := logger.FromContext(ctx)

	server, err := NewServer(ctx, r)
	if err != nil {
		return fmt.Errorf("creating http server: %w", err)
	}

	listener, err := new(net.ListenConfig).Listen(ctx, "tcp", opts.Address)
	if err != nil {
		return fmt.Errorf("creating http listener: %w", err)
	}

	go func() {
		<-ctx.Done()

		shutdownCtx, cancel := context.WithTimeout(context.Background(), opts.ShutdownDelay)
		defer cancel()

		if shutdownErr := server.Shutdown(shutdownCtx); shutdownErr != nil {
			log.Errorln("http server graceful shutdown failed:", shutdownErr)
		}
	}()

	log.Infoln("http server listening on", opts.Address)

	if err := server.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return fmt.Errorf("http server: %w", err)
	}

	return nil
}
