package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	"github.com/serediukit/bgex-backend/cmd/bgex-server/app"
	"github.com/serediukit/bgex-backend/cmd/bgex-server/app/health"
	"github.com/serediukit/bgex-backend/cmd/bgex-server/app/http"
)

func main() {
	runner := app.NewRunner(
		http.New(),
		health.New(),
	)

	instrumentedRunner := app.NewInstrumentedRunner(runner,
		app.WithLogger(),
		app.WithAutoMaxProcs(),
		app.WithTerminate(),
		app.WithViper(app.DefaultConfig()),
		app.WaitForIt(app.WithRedis()),
		app.WaitForIt(app.WithPostgres()),
	)

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	opts := app.DefaultOptions()

	if err := instrumentedRunner.Run(ctx, opts); err != nil {
		os.Exit(1)
	}
}
