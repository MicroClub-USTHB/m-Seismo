package config

import "github.com/spf13/viper"

func setDefaults(v *viper.Viper) {
	// Production unless told otherwise, so a deployment that forgets
	// SEISMO_ENV never falls back to development-only settings.
	v.SetDefault("crash_providers.encryption_key", "")
	v.SetDefault("crash_providers.sentry_origins", []string{"https://sentry.io", "https://us.sentry.io", "https://de.sentry.io"})
	v.SetDefault("env", "production")
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

	v.SetDefault("gitlab.web_url", "https://gitlab.com/")
	v.SetDefault("gitlab.api_url", "https://gitlab.com/api/v4/")
	v.SetDefault("gitlab.redirect_url", "")
	v.SetDefault("gitlab.max_rate_limit_wait", "30s")

	v.SetDefault("bitbucket.web_url", "https://bitbucket.org/")
	v.SetDefault("bitbucket.api_url", "https://api.bitbucket.org/2.0/")
	v.SetDefault("bitbucket.redirect_url", "")
	v.SetDefault("bitbucket.max_rate_limit_wait", "30s")

	v.SetDefault("secrets.encryption_key", "")

	v.SetDefault("limits.test_issues_per_minute", 10)

	v.SetDefault("ingest.app_per_minute", 600)
	v.SetDefault("ingest.app_burst", 600)
	v.SetDefault("ingest.installation_per_minute", 10)
	v.SetDefault("ingest.installation_burst", 5)

	v.SetDefault("crash.source", "none")
	v.SetDefault("crash.window", "2m")
	v.SetDefault("crash_providers.previous_encryption_keys", []string{})
	v.SetDefault("worker.enabled", true)
	v.SetDefault("worker.concurrency", 4)
	v.SetDefault("worker.max_attempts", 10)

	v.SetDefault("coordinator.enabled", false)
	v.SetDefault("coordinator.addr", ":9443")
	v.SetDefault("coordinator.tls_cert_file", "")
	v.SetDefault("coordinator.tls_key_file", "")
	v.SetDefault("coordinator.plaintext", false)
	v.SetDefault("coordinator.window", "2m")
	v.SetDefault("coordinator.timeout", "8s")
	v.SetDefault("coordinator.max_bytes", 256*1024)
	v.SetDefault("coordinator.max_lines_per_workload", 200)
}
