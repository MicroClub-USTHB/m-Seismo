package config

import "github.com/spf13/viper"

func setDefaults(v *viper.Viper) {
	v.SetDefault("env", "development")
	v.SetDefault("log_level", "info")

	v.SetDefault("server.host", "0.0.0.0")
	v.SetDefault("server.port", 8080)

	v.SetDefault("database.url", "postgres://seismo:seismo@localhost:5432/seismo?sslmode=disable")
	v.SetDefault("database.max_conns", 10)

	v.SetDefault("redis.url", "redis://localhost:6379/0")

	v.SetDefault("nats.url", "nats://localhost:4222")
}
