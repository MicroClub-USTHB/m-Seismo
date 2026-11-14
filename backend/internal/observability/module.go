package observability

import (
	"go.uber.org/fx"
)

// Module provides the observability Service.
var Module = fx.Module("observability",
	fx.Provide(
		NewService,
	),
)
