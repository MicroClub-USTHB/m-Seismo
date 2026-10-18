package ingest

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/nats-io/nats.go/jetstream"
	"go.uber.org/fx"

	"github.com/MicroClub-USTHB/m-Seismo/backend/internal/app"
	"github.com/MicroClub-USTHB/m-Seismo/backend/internal/config"
	"github.com/MicroClub-USTHB/m-Seismo/backend/internal/httpx"
	"github.com/MicroClub-USTHB/m-Seismo/backend/internal/queue"
	"github.com/MicroClub-USTHB/m-Seismo/backend/internal/ratelimit"
)

// Module provides the ingestion endpoint and what it needs.
var Module = fx.Module("ingest",
	fx.Provide(
		func(s *app.Service) Authenticator { return s },
		func(l *ratelimit.Limiter) Limiter { return l },
		func(pool *pgxpool.Pool) Store { return NewPGStore(pool) },
		newPublisher,
		limitsFromConfig,
		httpx.AsRoutes(NewHandler),
	),
)

// newPublisher creates the reports stream at start-up, so the first report
// doesn't pay for it.
func newPublisher(js jetstream.JetStream) (queue.Publisher, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	return queue.NewJetStream(ctx, js)
}

func limitsFromConfig(cfg *config.Config) Limits {
	c := cfg.Ingest
	return Limits{
		PerApp:          ratelimit.Limit{PerMinute: c.AppPerMinute, Burst: c.AppBurst},
		PerInstallation: ratelimit.Limit{PerMinute: c.InstallationPerMinute, Burst: c.InstallationBurst},
	}
}
