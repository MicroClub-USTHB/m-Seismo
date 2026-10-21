package config

import (
	"log/slog"

	"go.uber.org/fx"
)

// Module provides a *Config loaded from defaults, config file and env vars,
// and applies its log level to the default slog logger.
var Module = fx.Module("config",
	fx.Provide(New),
	fx.Invoke(applyLogLevel),
)

func applyLogLevel(c *Config) error {
	l, err := c.SlogLevel()
	if err != nil {
		return err
	}
	slog.SetLogLoggerLevel(l)
	return nil
}

// New loads configuration using the default search paths. It is the fx
// constructor for *Config; use Load directly if you need an explicit path.
func New() (*Config, error) {
	return Load("")
}
