package infra

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
	"go.uber.org/fx"

	"github.com/MicroClub-USTHB/m-Seismo/backend/internal/config"
	"github.com/MicroClub-USTHB/m-Seismo/backend/internal/crash"
	"github.com/MicroClub-USTHB/m-Seismo/backend/internal/crash/crashlytics"
	"github.com/MicroClub-USTHB/m-Seismo/backend/internal/crash/none"
	"github.com/MicroClub-USTHB/m-Seismo/backend/internal/db"
	"github.com/MicroClub-USTHB/m-Seismo/backend/internal/httpx"
	"github.com/MicroClub-USTHB/m-Seismo/backend/internal/secret"
)

// crashCallbackPath is where Google sends the browser back after consent.
const crashCallbackPath = "/api/v1/oauth/crashlytics/callback"

// CrashParams are the dependencies of NewCrashSource.
type CrashParams struct {
	fx.In

	Lifecycle fx.Lifecycle
	Config    *config.Config
	Pool      *pgxpool.Pool
	Redis     *redis.Client
	Require   httpx.AuthMiddleware
}

// CrashResult is the crash source, plus the routes that connect a project
// to it when the source has any.
type CrashResult struct {
	fx.Out

	Source crash.Source
	Routes []httpx.Routes `group:"routes,flatten"`
}

// NewCrashSource builds the crash.Source selected by cfg.Crash.Source. For
// Crashlytics it also provides the endpoints that connect a project
// through OAuth, and an fx start hook that checks every connected project
// can be read.
//
// A failed check only logs a warning: reports must still be processed and
// published without crash details when the crash reporter is unreachable.
func NewCrashSource(p CrashParams) (CrashResult, error) {
	cfg := p.Config

	switch cfg.Crash.Source {
	case "", "none":
		return CrashResult{Source: none.Source{}}, nil
	case "crashlytics":
	default:
		return CrashResult{}, fmt.Errorf("infra: unknown crash source %q", cfg.Crash.Source)
	}

	if cfg.Security.SecretKey == "" {
		return CrashResult{}, errors.New("infra: crash source crashlytics needs SEISMO_SECURITY_SECRET_KEY to encrypt refresh tokens")
	}
	box, err := secret.New(cfg.Security.SecretKey)
	if err != nil {
		return CrashResult{}, fmt.Errorf("infra: %w", err)
	}

	c := cfg.Crash.Crashlytics
	redirect := c.RedirectURL
	if redirect == "" {
		redirect = strings.TrimRight(cfg.Server.PublicURL, "/") + crashCallbackPath
	}
	ccfg := crashlytics.Config{
		ClientID:     c.ClientID,
		ClientSecret: c.ClientSecret,
		RedirectURL:  redirect,
		QuotaProject: c.QuotaProject,
	}
	store := crashlytics.NewPGStore(db.New(p.Pool), box)

	src, err := crashlytics.New(ccfg, store)
	if err != nil {
		return CrashResult{}, fmt.Errorf("infra: create crash source: %w", err)
	}

	p.Lifecycle.Append(fx.Hook{
		OnStart: func(ctx context.Context) error {
			if err := src.Check(ctx); err != nil {
				slog.Warn("crash source cannot be read, reports will be published without crash details",
					"source", cfg.Crash.Source, "err", err)
			}
			return nil
		},
	})

	return CrashResult{
		Source: src,
		Routes: []httpx.Routes{crashlytics.NewHandler(ccfg, store, crashlytics.NewRedisStates(p.Redis), p.Require)},
	}, nil
}
