// Command server runs the m-Seismo backend HTTP API.
//
//	server           run the API (migrates first when MIGRATE_ON_START=true)
//	server migrate   apply pending migrations and exit
package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/MicroClub-USTHB/m-Seismo/backend/internal/apps"
	"github.com/MicroClub-USTHB/m-Seismo/backend/internal/config"
	"github.com/MicroClub-USTHB/m-Seismo/backend/internal/db"
	"github.com/MicroClub-USTHB/m-Seismo/backend/internal/httpapi"
	"github.com/MicroClub-USTHB/m-Seismo/backend/internal/projects"
	"github.com/MicroClub-USTHB/m-Seismo/backend/internal/users"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		slog.Error("fatal", "err", err)
		os.Exit(1)
	}
}

func run(args []string) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	if err := setupLogger(cfg); err != nil {
		return err
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	pool, err := db.Open(ctx, cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer pool.Close()

	if len(args) > 0 {
		switch args[0] {
		case "migrate":
			if err := db.Migrate(ctx, pool); err != nil {
				return err
			}
			slog.Info("migrations applied")
			return nil
		default:
			return fmt.Errorf("unknown command %q", args[0])
		}
	}

	if cfg.MigrateOnStart {
		if err := db.Migrate(ctx, pool); err != nil {
			return err
		}
	}

	srv := &http.Server{
		Addr: cfg.HTTPAddr,
		Handler: httpapi.NewRouter(httpapi.Deps{
			AdminToken:     cfg.AdminAPIToken,
			RequestTimeout: cfg.RequestTimeout,
			Ready:          pool.Ping,
			Users:          users.NewService(pool),
			Projects:       projects.NewService(pool),
			Apps:           apps.NewService(pool),
		}),
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       2 * time.Minute,
	}

	errc := make(chan error, 1)
	go func() {
		slog.Info("http server listening", "addr", cfg.HTTPAddr)
		errc <- srv.ListenAndServe()
	}()

	select {
	case err := <-errc:
		return err
	case <-ctx.Done():
	}

	slog.Info("shutting down")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), cfg.ShutdownTimeout)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		return fmt.Errorf("shutdown: %w", err)
	}
	if err := <-errc; !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

func setupLogger(cfg config.Config) error {
	level, err := cfg.SlogLevel()
	if err != nil {
		return err
	}
	opts := &slog.HandlerOptions{Level: level}
	var h slog.Handler = slog.NewJSONHandler(os.Stdout, opts)
	if cfg.LogFormat == "text" {
		h = slog.NewTextHandler(os.Stdout, opts)
	}
	slog.SetDefault(slog.New(h))
	return nil
}
