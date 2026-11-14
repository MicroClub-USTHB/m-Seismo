package release

import (
	"go.uber.org/fx"
)

// Module provides the release Correlator.
var Module = fx.Module("release",
	fx.Provide(
		func() *Correlator {
			return NewCorrelator()
		},
	),
)
