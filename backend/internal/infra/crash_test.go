package infra

import (
	"crypto/rand"
	"encoding/base64"
	"net/http"
	"testing"

	"go.uber.org/fx/fxtest"

	"github.com/MicroClub-USTHB/m-Seismo/backend/internal/config"
	"github.com/MicroClub-USTHB/m-Seismo/backend/internal/crash/crashlytics"
	"github.com/MicroClub-USTHB/m-Seismo/backend/internal/crash/none"
)

func TestNewCrashSource(t *testing.T) {
	key := make([]byte, 32)
	rand.Read(key)
	secretKey := base64.StdEncoding.EncodeToString(key)
	client := config.CrashlyticsConfig{ClientID: "id", ClientSecret: "secret"}

	tests := []struct {
		name       string
		crash      config.CrashConfig
		secrets    config.SecretsConfig
		want       any
		wantRoutes int
		wantErr    bool
	}{
		{name: "default", want: none.Source{}},
		{name: "none", crash: config.CrashConfig{Source: "none"}, want: none.Source{}},
		{name: "crashlytics", crash: config.CrashConfig{Source: "crashlytics", Crashlytics: client}, secrets: config.SecretsConfig{EncryptionKey: secretKey}, want: &crashlytics.Source{}, wantRoutes: 1},
		{name: "crashlytics without secret key", crash: config.CrashConfig{Source: "crashlytics", Crashlytics: client}, wantErr: true},
		{name: "crashlytics without client", crash: config.CrashConfig{Source: "crashlytics"}, secrets: config.SecretsConfig{EncryptionKey: secretKey}, wantErr: true},
		{name: "unknown", crash: config.CrashConfig{Source: "sentry"}, wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &config.Config{Env: "production", Crash: tt.crash, Secrets: tt.secrets}
			res, err := NewCrashSource(CrashParams{
				Lifecycle: fxtest.NewLifecycle(t),
				Config:    cfg,
				Require:   func(h http.Handler) http.Handler { return h },
			})
			if tt.wantErr {
				if err == nil {
					t.Fatal("want an error")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			switch tt.want.(type) {
			case none.Source:
				if _, ok := res.Source.(none.Source); !ok {
					t.Errorf("got %T, want none.Source", res.Source)
				}
			case *crashlytics.Source:
				if _, ok := res.Source.(*crashlytics.Source); !ok {
					t.Errorf("got %T, want *crashlytics.Source", res.Source)
				}
			}
			if len(res.Routes) != tt.wantRoutes {
				t.Errorf("got %d route sets, want %d", len(res.Routes), tt.wantRoutes)
			}
		})
	}
}

func TestCrashlyticsRedirectDefaultsToPublicURL(t *testing.T) {
	key := make([]byte, 32)
	rand.Read(key)
	cfg := &config.Config{
		Server:  config.ServerConfig{PublicURL: "https://seismo.example.com/"},
		Crash:   config.CrashConfig{Source: "crashlytics", Crashlytics: config.CrashlyticsConfig{ClientID: "id", ClientSecret: "secret"}},
		Secrets: config.SecretsConfig{EncryptionKey: base64.StdEncoding.EncodeToString(key)},
	}
	res, err := NewCrashSource(CrashParams{
		Lifecycle: fxtest.NewLifecycle(t),
		Config:    cfg,
		Require:   func(h http.Handler) http.Handler { return h },
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := res.Routes[0].(*crashlytics.Handler).RedirectURL(); got != "https://seismo.example.com/api/v1/oauth/crashlytics/callback" {
		t.Errorf("redirect URL = %q", got)
	}
}
