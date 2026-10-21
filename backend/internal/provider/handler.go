package provider

import (
	"log/slog"
	"net/http"
	"net/url"
	"strings"

	"github.com/go-chi/chi/v5"
	"go.uber.org/fx"

	"github.com/MicroClub-USTHB/m-Seismo/backend/internal/apperr"
	"github.com/MicroClub-USTHB/m-Seismo/backend/internal/config"
	"github.com/MicroClub-USTHB/m-Seismo/backend/internal/db"
	"github.com/MicroClub-USTHB/m-Seismo/backend/internal/httpx"
	"github.com/MicroClub-USTHB/m-Seismo/backend/internal/issue"
)

// Module provides the registry (also as issue.OpenerSource), the connect
// service and the provider routes. Tracker subpackages add themselves to
// the "providers" group.
var Module = fx.Module("provider",
	fx.Provide(
		NewRegistry,
		func(r *Registry) issue.OpenerSource { return r },
		NewRedisStateStore,
		NewService,
		httpx.AsRoutes(NewHandler),
	),
)

type Handler struct {
	svc         *Service
	reg         *Registry
	require     httpx.AuthMiddleware
	frontendURL string
}

func NewHandler(svc *Service, reg *Registry, require httpx.AuthMiddleware, cfg *config.Config) *Handler {
	return &Handler{svc: svc, reg: reg, require: require, frontendURL: cfg.Server.FrontendURL}
}

func (h *Handler) Mount(r chi.Router) {
	r.Route("/providers", func(r chi.Router) {
		// The tracker redirects the browser here, so there is no bearer
		// token: the owner is recovered from the one-time state.
		r.Get("/{tracker}/callback", h.callback)

		r.Group(func(r chi.Router) {
			r.Use(h.require)
			r.Get("/", h.list)
			r.Get("/{tracker}/connect", h.connect)
			r.Get("/connections", h.listConnections)
			r.Delete("/connections/{id}", h.disconnect)
			r.Get("/connections/{id}/repositories", h.listRepositories)
		})
	})
}

func (h *Handler) list(w http.ResponseWriter, _ *http.Request) {
	httpx.JSON(w, http.StatusOK, map[string]any{"providers": h.reg.Trackers()})
}

func (h *Handler) connect(w http.ResponseWriter, r *http.Request) {
	tracker := db.Tracker(chi.URLParam(r, "tracker"))
	u, err := h.svc.StartConnect(r.Context(), httpx.OwnerID(r.Context()), tracker)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	// JSON rather than a redirect: an SPA calling this with a bearer token
	// cannot follow a redirect itself, so it navigates to url.
	httpx.JSON(w, http.StatusOK, map[string]string{"url": u})
}

func (h *Handler) callback(w http.ResponseWriter, r *http.Request) {
	tracker := db.Tracker(chi.URLParam(r, "tracker"))
	back := url.Values{"provider": {string(tracker)}}

	conn, err := h.svc.FinishConnect(r.Context(), tracker, r.URL.Query())
	if err != nil {
		kind, msg := apperr.Classify(err)
		if kind == apperr.KindInternal {
			slog.ErrorContext(r.Context(), "provider callback failed", "tracker", tracker, "err", err)
		}
		back.Set("status", "error")
		back.Set("reason", msg)
	} else {
		back.Set("status", "connected")
		back.Set("connection_id", conn.ID.String())
	}

	http.Redirect(w, r, strings.TrimRight(h.frontendURL, "/")+"/settings/providers?"+back.Encode(), http.StatusFound)
}

func (h *Handler) listConnections(w http.ResponseWriter, r *http.Request) {
	conns, err := h.svc.ListConnections(r.Context(), httpx.OwnerID(r.Context()))
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, conns)
}

func (h *Handler) disconnect(w http.ResponseWriter, r *http.Request) {
	id, err := httpx.UUIDParam(r, "id")
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	if err := h.svc.Disconnect(r.Context(), httpx.OwnerID(r.Context()), id); err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.NoContent(w)
}

func (h *Handler) listRepositories(w http.ResponseWriter, r *http.Request) {
	id, err := httpx.UUIDParam(r, "id")
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	repos, err := h.svc.ListRepositories(r.Context(), httpx.OwnerID(r.Context()), id)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, repos)
}
