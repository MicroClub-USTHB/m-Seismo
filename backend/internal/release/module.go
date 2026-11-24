package release

import (
	"go.uber.org/fx"

	"github.com/MicroClub-USTHB/m-Seismo/backend/internal/config"
)

// Module provides the release Correlator.
var Module = fx.Module("release",
	fx.Provide(
		func(cfg *config.Config) *Correlator {
			return NewCorrelator(WithReleaseClient(NewHTTPClient(cfg)))
		},
	),
)
