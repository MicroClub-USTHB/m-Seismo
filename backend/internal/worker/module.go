package worker

import (
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/nats-io/nats.go/jetstream"
	"go.uber.org/fx"

	"github.com/MicroClub-USTHB/m-Seismo/backend/internal/config"
	"github.com/MicroClub-USTHB/m-Seismo/backend/internal/coordinator"
	"github.com/MicroClub-USTHB/m-Seismo/backend/internal/db"
	"github.com/MicroClub-USTHB/m-Seismo/backend/internal/issue"
	"github.com/MicroClub-USTHB/m-Seismo/backend/internal/routing"
)

// Module runs the worker in this process, unless worker.enabled is false.
var Module = fx.Module("worker",
	fx.Provide(
		func(q db.Querier) Queries { return q },
		func(s *issue.Service) Issues { return s },
		func(pool *pgxpool.Pool) Locks { return pgLocks{pool: pool} },
		func(q Queries, is Issues, r *routing.Router, l Locks, c *coordinator.Client, cfg *config.Config) *Processor {
			return NewProcessor(q, is, r, l, c, cfg.Coordinator.Window)
		},
	),
	fx.Invoke(register),
)

func register(lc fx.Lifecycle, cfg *config.Config, js jetstream.JetStream, p *Processor) {
	if !cfg.Worker.Enabled {
		return
	}
	w := New(js, p, cfg.Worker.Concurrency, cfg.Worker.MaxAttempts)
	lc.Append(fx.Hook{OnStart: w.Start, OnStop: w.Stop})
}
