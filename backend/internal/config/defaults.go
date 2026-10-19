package config

import "github.com/spf13/viper"

func setDefaults(v *viper.Viper) {
	v.SetDefault("env", "development")
	v.SetDefault("log_level", "info")

	v.SetDefault("server.host", "0.0.0.0")
	v.SetDefault("server.port", 8080)
	v.SetDefault("server.public_url", "http://localhost:8080")
	v.SetDefault("server.frontend_url", "http://localhost:3000")

	v.SetDefault("database.url", "postgres://seismo:seismo@localhost:5432/seismo?sslmode=disable")
	v.SetDefault("database.max_conns", 10)

	v.SetDefault("redis.url", "redis://localhost:6379/0")

	v.SetDefault("nats.url", "nats://localhost:4222")

	v.SetDefault("auth.jwt_secret", "")
	v.SetDefault("auth.jwt_ttl", "24h")

	v.SetDefault("github.app_id", 0)
	v.SetDefault("github.app_slug", "")
	v.SetDefault("github.client_id", "")
	v.SetDefault("github.client_secret", "")
	v.SetDefault("github.private_key", "")
	v.SetDefault("github.private_key_path", "")
	v.SetDefault("github.api_url", "https://api.github.com/")
	v.SetDefault("github.web_url", "https://github.com/")
	v.SetDefault("github.min_write_interval", "1s")
	v.SetDefault("github.max_rate_limit_wait", "30s")

	v.SetDefault("limits.test_issues_per_minute", 10)
}
