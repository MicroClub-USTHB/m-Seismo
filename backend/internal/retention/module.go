package retention

import (
	"go.uber.org/fx"

	"github.com/MicroClub-USTHB/m-Seismo/backend/internal/config"
	"github.com/MicroClub-USTHB/m-Seismo/backend/internal/db"
)

// Module runs the daily retention sweep in this process.
var Module = fx.Module("retention",
	fx.Provide(
		func(q db.Querier) Deleter { return q },
		func(d Deleter, cfg *config.Config) *Service { return NewService(d, cfg.Retention.DefaultDays) },
		func(svc *Service) *Scheduler { return NewScheduler(svc, SweepInterval) },
	),
	fx.Invoke(register),
)

func register(lc fx.Lifecycle, s *Scheduler) {
	lc.Append(fx.Hook{OnStart: s.Start, OnStop: s.Stop})
}
