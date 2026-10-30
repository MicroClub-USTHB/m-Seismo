package worker

import (
	"github.com/nats-io/nats.go/jetstream"
	"go.uber.org/fx"

	"github.com/MicroClub-USTHB/m-Seismo/backend/internal/attachment"
	"github.com/MicroClub-USTHB/m-Seismo/backend/internal/config"
	"github.com/MicroClub-USTHB/m-Seismo/backend/internal/coordinator"
	"github.com/MicroClub-USTHB/m-Seismo/backend/internal/crashprovider"
	"github.com/MicroClub-USTHB/m-Seismo/backend/internal/db"
	"github.com/MicroClub-USTHB/m-Seismo/backend/internal/issue"
	"github.com/MicroClub-USTHB/m-Seismo/backend/internal/routing"
)

// Module runs the worker in this process, unless worker.enabled is false.
var Module = fx.Module("worker",
	fx.Provide(
		func(q db.Querier) Queries { return q },
		func(s *issue.Service) Issues { return s },
		func(s *crashprovider.Service) CrashResolver { return s },
		func(s *attachment.Service) AttachmentLinker { return s },
		func(c *coordinator.Client) ServiceHealth { return c },
		func(q Queries, is Issues, r *routing.Router, c *coordinator.Client, cr CrashResolver, cfg *config.Config, links AttachmentLinker, health ServiceHealth) *Processor {
			return NewProcessor(q, is, r, c, cfg.Coordinator.Window, cr, links, health)
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
