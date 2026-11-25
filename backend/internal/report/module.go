package report

import (
	"go.uber.org/fx"

	"github.com/MicroClub-USTHB/m-Seismo/backend/internal/httpx"
)

// Module provides the report-deletion endpoints and what they need.
var Module = fx.Module("report",
	fx.Provide(
		NewService,
		httpx.AsRoutes(NewHandler),
	),
)
