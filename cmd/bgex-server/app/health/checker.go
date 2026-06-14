package health

import (
	"context"

	"github.com/serediukit/bgex-backend/cmd/bgex-server/app"
	"github.com/serediukit/bgex-backend/pkg/health"
	"github.com/serediukit/bgex-backend/pkg/logger"
)

type Plugin struct{}

func New() Plugin {
	return Plugin{}
}

func (Plugin) Name() string {
	return "health-checker"
}

func (Plugin) Run(r *app.Runner, ctx context.Context, _ *app.Options) error {
	log := logger.FromContext(ctx)

	return r.HealthChecker.Run(ctx, health.WithInterceptor(
		health.WithLogger(log),
	))
}
