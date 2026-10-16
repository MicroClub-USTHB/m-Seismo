// Package config loads m-Seismo's backend configuration from (in increasing
// priority) built-in defaults, a config file, and SEISMO_*-prefixed
// environment variables.
package config

import (
	"fmt"
	"strings"

	"github.com/spf13/viper"
)

func Load(configPath string) (*Config, error) {
	v := viper.New()

	setDefaults(v)

	v.SetEnvPrefix("seismo")
	v.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	v.AutomaticEnv()

	v.SetConfigName("config")
	v.SetConfigType("yaml")
	v.AddConfigPath(".")
	v.AddConfigPath("./configs")
	if configPath != "" {
		v.SetConfigFile(configPath)
	}

	if err := v.ReadInConfig(); err != nil {
		if _, ok := err.(viper.ConfigFileNotFoundError); !ok {
			return nil, fmt.Errorf("config: %w", err)
		}
	}

	cfg := &Config{
		Env:      v.GetString("env"),
		LogLevel: v.GetString("log_level"),
		Server: ServerConfig{
			Host: v.GetString("server.host"),
			Port: v.GetInt("server.port"),
		},
		Database: DatabaseConfig{
			URL:      v.GetString("database.url"),
			MaxConns: int32(v.GetInt("database.max_conns")),
		},
		Redis: RedisConfig{
			URL: v.GetString("redis.url"),
		},
		NATS: NATSConfig{
			URL: v.GetString("nats.url"),
		},
	}

	return cfg, nil
}
