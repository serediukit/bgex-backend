package http

import (
	"context"
	"fmt"
	"net"

	"github.com/serediukit/bgex-backend/cmd/bgex-server/app"
)

type Plugin struct{}

func New() Plugin {
	return Plugin{}
}

func (Plugin) Name() string {
	return "http-server"
}

func (Plugin) Run(r *app.Runner, ctx context.Context, opts *app.Options) error {
	server, err := NewServer(r)
	if err != nil {
		return fmt.Errorf("creating http server: %w", err)
	}

	listener, err := new(net.ListenConfig).Listen(ctx, "tcp", opts.Address)
	if err != nil {
		return fmt.Errorf("creating http listener: %w", err)
	}

	go func() {
		<-ctx.Done()

		server.GracefulStop()
	}()

	return server.Serve(listener)
}
