package config

import (
	"log/slog"
	"reflect"
	"strings"
	"testing"
)

func TestCrashProviderEnvironmentSettings(t *testing.T) {
	t.Setenv("SEISMO_ENV", "development")
	t.Setenv("SEISMO_CRASH_PROVIDERS_ENCRYPTION_KEY", "operator-key")
	t.Setenv("SEISMO_CRASH_PROVIDERS_SENTRY_ORIGINS", "https://sentry.io https://sentry.example.com")
	cfg, err := Load("")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.CrashProviders.EncryptionKey != "operator-key" || !reflect.DeepEqual(cfg.CrashProviders.SentryOrigins, []string{"https://sentry.io", "https://sentry.example.com"}) {
		t.Fatalf("crash-provider environment settings were not loaded")
	}
}

func TestLoadRequiresJWTSecretOutsideDevelopment(t *testing.T) {
	t.Setenv("SEISMO_ENV", "")
	t.Setenv("SEISMO_AUTH_JWT_SECRET", "")

	_, err := Load("")
	if err == nil || !strings.Contains(err.Error(), "auth.jwt_secret is required") {
		t.Fatalf("Load with no env and no secret: err = %v, want jwt_secret error", err)
	}
}

func TestLoadDevelopmentFallsBackToDevSecret(t *testing.T) {
	t.Setenv("SEISMO_ENV", "development")
	t.Setenv("SEISMO_AUTH_JWT_SECRET", "")

	cfg, err := Load("")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Auth.JWTSecret != devJWTSecret {
		t.Errorf("JWTSecret = %q, want the development secret", cfg.Auth.JWTSecret)
	}
}

func TestLoadLogLevel(t *testing.T) {
	t.Setenv("SEISMO_AUTH_JWT_SECRET", "test-secret")

	t.Setenv("SEISMO_LOG_LEVEL", "debug")
	cfg, err := Load("")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if l, _ := cfg.SlogLevel(); l != slog.LevelDebug {
		t.Errorf("SlogLevel = %v, want debug", l)
	}

	t.Setenv("SEISMO_LOG_LEVEL", "verbose")
	if _, err := Load(""); err == nil {
		t.Error("Load with log_level=verbose: want an error")
	}
}
