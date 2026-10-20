// Package httpx holds the HTTP server, the router and the helpers every
// handler package shares (JSON responses, error mapping, auth).
package httpx

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"go.uber.org/fx"

	"github.com/MicroClub-USTHB/m-Seismo/backend/internal/config"
)

// Routes is implemented by each handler package. Mount receives the
// /api/v1 router; protected routes wrap themselves in AuthMiddleware.
type Routes interface {
	Mount(r chi.Router)
}

// AsRoutes annotates a constructor so its result joins the "routes" group.
func AsRoutes(f any) any {
	return fx.Annotate(f, fx.As(new(Routes)), fx.ResultTags(`group:"routes"`))
}

type routerParams struct {
	fx.In
	Routes []Routes `group:"routes"`
}

// NewRouter builds the root router with common middleware and mounts every
// registered Routes under /api/v1.
func NewRouter(p routerParams) http.Handler {
	r := chi.NewRouter()
	r.Use(middleware.RequestID)
	r.Use(middleware.RealIP)
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)

	r.Get("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		JSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})

	r.Route("/api/v1", func(api chi.Router) {
		for _, routes := range p.Routes {
			routes.Mount(api)
		}
	})

	return r
}

// NewServer creates the HTTP server and ties it to the fx lifecycle.
func NewServer(lc fx.Lifecycle, cfg *config.Config, h http.Handler) *http.Server {
	srv := &http.Server{
		Addr:              fmt.Sprintf("%s:%d", cfg.Server.Host, cfg.Server.Port),
		Handler:           h,
		ReadHeaderTimeout: 10 * time.Second,
	}

	lc.Append(fx.Hook{
		OnStart: func(ctx context.Context) error {
			ln, err := net.Listen("tcp", srv.Addr)
			if err != nil {
				return fmt.Errorf("httpx: listen %s: %w", srv.Addr, err)
			}
			slog.Info("http server listening", "addr", srv.Addr, "env", cfg.Env)
			go func() {
				if err := srv.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
					slog.Error("http server stopped", "err", err)
				}
			}()
			return nil
		},
		OnStop: func(ctx context.Context) error {
			return srv.Shutdown(ctx)
		},
	})

	return srv
}

// Module provides the router, the server and the auth middleware. A
// TokenVerifier must be provided elsewhere (auth.Module).
var Module = fx.Module("httpx",
	fx.Provide(
		NewRouter,
		NewServer,
		NewAuthMiddleware,
	),
	fx.Invoke(func(*http.Server) {}),
)
