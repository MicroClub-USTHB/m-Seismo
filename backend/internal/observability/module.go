package observability

import (
	"go.uber.org/fx"

	"github.com/MicroClub-USTHB/m-Seismo/backend/internal/httpx"
)

// Module provides the observability Service and HTTP routes.
var Module = fx.Module("observability",
	fx.Provide(
		NewService,
		httpx.AsRoutes(NewHandler),
	),
)
