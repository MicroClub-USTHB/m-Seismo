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

func TestLoadOAuthTrackers(t *testing.T) {
	t.Setenv("SEISMO_AUTH_JWT_SECRET", "test-secret")
	t.Setenv("SEISMO_SERVER_PUBLIC_URL", "https://seismo.example.com/")
	t.Setenv("SEISMO_BITBUCKET_REDIRECT_URL", "https://cb.example.com/bb")

	t.Setenv("SEISMO_ENV", "production")
	if _, err := Load(""); err == nil || !strings.Contains(err.Error(), "secrets.encryption_key is required") {
		t.Fatalf("production without an encryption key: err = %v", err)
	}

	t.Setenv("SEISMO_SECRETS_ENCRYPTION_KEY", "a2V5")
	cfg, err := Load("")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.GitLab.APIURL != "https://gitlab.com/api/v4/" {
		t.Errorf("gitlab = %+v", cfg.GitLab)
	}
	if cfg.GitLab.RedirectURL != "https://seismo.example.com/api/v1/providers/gitlab/callback" {
		t.Errorf("derived gitlab redirect = %q", cfg.GitLab.RedirectURL)
	}
	if cfg.Bitbucket.RedirectURL != "https://cb.example.com/bb" {
		t.Errorf("explicit bitbucket redirect = %q", cfg.Bitbucket.RedirectURL)
	}
	if cfg.Bitbucket.APIURL != "https://api.bitbucket.org/2.0/" || cfg.Bitbucket.MaxRateLimitWait.Seconds() != 30 {
		t.Errorf("bitbucket = %+v", cfg.Bitbucket)
	}
}

func TestLoadLogLevel(t *testing.T) {
	t.Setenv("SEISMO_AUTH_JWT_SECRET", "test-secret")
	t.Setenv("SEISMO_SECRETS_ENCRYPTION_KEY", "a2V5")

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

func TestPreviousCredentialKeysLoadFromEnvironment(t *testing.T) {
	t.Setenv("SEISMO_AUTH_JWT_SECRET", "test-secret")
	t.Setenv("SEISMO_SECRETS_ENCRYPTION_KEY", "test-key")
	t.Setenv("SEISMO_CRASH_PROVIDERS_PREVIOUS_ENCRYPTION_KEYS", "old-key-a old-key-b")
	cfg, err := Load("")
	if err != nil {
		t.Fatal(err)
	}
	if keys := cfg.CrashProviders.PreviousEncryptionKeys; len(keys) != 2 || keys[0] != "old-key-a" || keys[1] != "old-key-b" {
		t.Fatalf("previous keys not parsed: %v", keys)
	}
}
