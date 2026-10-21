package provider

import (
	"log/slog"
	"net/http"
	"net/url"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"go.uber.org/fx"

	"github.com/MicroClub-USTHB/m-Seismo/backend/internal/apperr"
	"github.com/MicroClub-USTHB/m-Seismo/backend/internal/config"
	"github.com/MicroClub-USTHB/m-Seismo/backend/internal/db"
	"github.com/MicroClub-USTHB/m-Seismo/backend/internal/httpx"
	"github.com/MicroClub-USTHB/m-Seismo/backend/internal/issue"
)

// Module provides the registry (also as issue.OpenerSource), the connect
// service and the provider routes. The per-app connection routes are
// mounted by the app package, under /apps/{appID}. Tracker subpackages add
// themselves to the "providers" group.
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
		// token: the owner and app are recovered from the one-time state.
		r.Get("/{tracker}/callback", h.callback)

		r.Group(func(r chi.Router) {
			r.Use(h.require)
			r.Get("/", h.list)
			r.Get("/connections", h.listOwnerConnections)
		})
	})
}

func (h *Handler) list(w http.ResponseWriter, _ *http.Request) {
	httpx.JSON(w, http.StatusOK, map[string]any{"providers": h.reg.Trackers()})
}

// callback finishes the connect flow and sends the browser back to the
// frontend: to the app's connections page when the state named an app, or
// to the app list when the state was unknown.
func (h *Handler) callback(w http.ResponseWriter, r *http.Request) {
	tracker := db.Tracker(chi.URLParam(r, "tracker"))
	back := url.Values{"provider": {string(tracker)}}

	appID, conn, err := h.svc.FinishConnect(r.Context(), tracker, r.URL.Query())
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

	path := "/apps"
	if appID != uuid.Nil {
		path = "/apps/" + appID.String() + "/connections"
	}
	http.Redirect(w, r, strings.TrimRight(h.frontendURL, "/")+path+"?"+back.Encode(), http.StatusFound)
}

// listOwnerConnections lists the connections of every app of the owner.
func (h *Handler) listOwnerConnections(w http.ResponseWriter, r *http.Request) {
	conns, err := h.svc.ListOwnerConnections(r.Context(), httpx.OwnerID(r.Context()))
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, conns)
}
