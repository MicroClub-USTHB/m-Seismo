package config

import "go.uber.org/fx"

// Module provides a *Config loaded from defaults, config file and env vars.
var Module = fx.Module("config",
	fx.Provide(New),
)

// New loads configuration using the default search paths. It is the fx
// constructor for *Config; use Load directly if you need an explicit path.
func New() (*Config, error) {
	return Load("")
}
