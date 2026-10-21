// Package config loads m-Seismo's backend configuration from (in increasing
// priority) built-in defaults, a config file, and SEISMO_*-prefixed
// environment variables.
package config

import (
	"fmt"
	"log/slog"
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
		Auth: AuthConfig{
			JWTSecret: v.GetString("auth.jwt_secret"),
			JWTTTL:    v.GetDuration("auth.jwt_ttl"),
		},
		GitHub: GitHubConfig{
			AppID:            v.GetInt64("github.app_id"),
			AppSlug:          v.GetString("github.app_slug"),
			ClientID:         v.GetString("github.client_id"),
			ClientSecret:     v.GetString("github.client_secret"),
			PrivateKey:       v.GetString("github.private_key"),
			PrivateKeyPath:   v.GetString("github.private_key_path"),
			APIURL:           v.GetString("github.api_url"),
			WebURL:           v.GetString("github.web_url"),
			MinWriteInterval: v.GetDuration("github.min_write_interval"),
			MaxRateLimitWait: v.GetDuration("github.max_rate_limit_wait"),
		},
		Limits: LimitsConfig{
			TestIssuesPerMinute: v.GetInt("limits.test_issues_per_minute"),
		},
		Ingest: IngestConfig{
			AppPerMinute:          v.GetFloat64("ingest.app_per_minute"),
			AppBurst:              v.GetInt("ingest.app_burst"),
			InstallationPerMinute: v.GetFloat64("ingest.installation_per_minute"),
			InstallationBurst:     v.GetInt("ingest.installation_burst"),
		},
		Worker: WorkerConfig{
			Enabled:     v.GetBool("worker.enabled"),
			Concurrency: v.GetInt("worker.concurrency"),
			MaxAttempts: v.GetInt("worker.max_attempts"),
		},
		Coordinator: CoordinatorConfig{
			Enabled:             v.GetBool("coordinator.enabled"),
			Addr:                v.GetString("coordinator.addr"),
			TLSCertFile:         v.GetString("coordinator.tls_cert_file"),
			TLSKeyFile:          v.GetString("coordinator.tls_key_file"),
			Plaintext:           v.GetBool("coordinator.plaintext"),
			Window:              v.GetDuration("coordinator.window"),
			Timeout:             v.GetDuration("coordinator.timeout"),
			MaxBytes:            v.GetInt("coordinator.max_bytes"),
			MaxLinesPerWorkload: v.GetInt("coordinator.max_lines_per_workload"),
		},
	}

	cfg.Server.PublicURL = v.GetString("server.public_url")
	cfg.Server.FrontendURL = v.GetString("server.frontend_url")

	if err := cfg.validate(); err != nil {
		return nil, fmt.Errorf("config: %w", err)
	}

	return cfg, nil
}

// devJWTSecret is used only when env is "development" and no secret is set,
// so a fresh checkout runs without extra setup.
const devJWTSecret = "m-seismo-development-only-secret"

func (c *Config) validate() error {
	if c.Auth.JWTSecret == "" {
		if c.Env != "development" {
			return fmt.Errorf("auth.jwt_secret is required when env is %q", c.Env)
		}
		c.Auth.JWTSecret = devJWTSecret
	}
	if c.Auth.JWTTTL <= 0 {
		return fmt.Errorf("auth.jwt_ttl must be positive")
	}
	if _, err := c.SlogLevel(); err != nil {
		return err
	}
	if c.Ingest.AppPerMinute <= 0 || c.Ingest.AppBurst < 1 ||
		c.Ingest.InstallationPerMinute <= 0 || c.Ingest.InstallationBurst < 1 {
		return fmt.Errorf("ingest rate limits must be positive")
	}
	if c.Worker.Concurrency < 1 || c.Worker.MaxAttempts < 1 {
		return fmt.Errorf("worker.concurrency and worker.max_attempts must be at least 1")
	}
	if err := c.Coordinator.validate(c.Env); err != nil {
		return err
	}
	return nil
}

func (c CoordinatorConfig) validate(env string) error {
	if (c.TLSCertFile == "") != (c.TLSKeyFile == "") {
		return fmt.Errorf("coordinator.tls_cert_file and coordinator.tls_key_file go together")
	}
	if c.Enabled && c.TLSCertFile == "" && !c.Plaintext && env != "development" {
		return fmt.Errorf("coordinator needs coordinator.tls_cert_file and tls_key_file, or coordinator.plaintext when TLS ends in front of the backend")
	}
	if c.Window <= 0 || c.Timeout <= 0 || c.MaxBytes < 1024 || c.MaxLinesPerWorkload < 1 {
		return fmt.Errorf("coordinator.window, timeout, max_bytes (at least 1024) and max_lines_per_workload must be positive")
	}
	return nil
}

// SlogLevel parses log_level (debug, info, warn or error).
func (c *Config) SlogLevel() (slog.Level, error) {
	var l slog.Level
	if err := l.UnmarshalText([]byte(c.LogLevel)); err != nil {
		return 0, fmt.Errorf("log_level %q must be debug, info, warn or error", c.LogLevel)
	}
	return l, nil
}
