package app

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"go.uber.org/fx"

	"github.com/MicroClub-USTHB/m-Seismo/backend/internal/db"
	"github.com/MicroClub-USTHB/m-Seismo/backend/internal/httpx"
	"github.com/MicroClub-USTHB/m-Seismo/backend/internal/provider"
	"github.com/MicroClub-USTHB/m-Seismo/backend/internal/ratelimit"
)

var Module = fx.Module("app",
	fx.Provide(
		func(l *ratelimit.Limiter) Limiter { return l },
		NewService,
		httpx.AsRoutes(NewHandler),
	),
)

type Handler struct {
	svc *Service
	// conns runs the per-app connect flow and manages app connections.
	conns   *provider.Service
	require httpx.AuthMiddleware
}

func NewHandler(svc *Service, conns *provider.Service, require httpx.AuthMiddleware) *Handler {
	return &Handler{svc: svc, conns: conns, require: require}
}

func (h *Handler) Mount(r chi.Router) {
	r.With(h.require).Route("/apps", func(r chi.Router) {
		r.Post("/", h.create)
		r.Get("/", h.list)
		r.Route("/{appID}", func(r chi.Router) {
			r.Get("/", h.get)
			r.Patch("/", h.update)
			r.Delete("/", h.delete)

			r.Get("/keys", h.listKeys(db.AppKeyKindSdk))
			r.Post("/keys", h.createKey(db.AppKeyKindSdk))
			r.Delete("/keys/{keyID}", h.revokeKey(db.AppKeyKindSdk))

			r.Get("/coordinator-keys", h.listKeys(db.AppKeyKindCoordinator))
			r.Post("/coordinator-keys", h.createKey(db.AppKeyKindCoordinator))
			r.Delete("/coordinator-keys/{keyID}", h.revokeKey(db.AppKeyKindCoordinator))

			r.Get("/projects", h.listProjects)
			r.Put("/projects/{projectID}", h.linkProject)
			r.Delete("/projects/{projectID}", h.unlinkProject)

			r.Get("/providers/{tracker}/credentials", h.getTrackerCredentials)
			r.Put("/providers/{tracker}/credentials", h.setTrackerCredentials)

			r.Get("/connect/{tracker}", h.connect)
			r.Get("/connections", h.listConnections)
			r.Post("/connections", h.attachConnection)
			r.Delete("/connections/{connectionID}", h.disconnect)
			r.Get("/connections/{connectionID}/repositories", h.listConnectionRepositories)
			r.Get("/targets", h.targets)

			r.Post("/test-issue", h.sendTestIssue)
		})
	})
}

type testIssueResponse struct {
	Error   string            `json:"error,omitempty"`
	Results []TestIssueResult `json:"results"`
}

// sendTestIssue answers 201 when at least one issue was opened, 429 (with
// Retry-After) when every target was rate-limited by its tracker, and 502
// when every target failed otherwise; all three include per-target results.
func (h *Handler) sendTestIssue(w http.ResponseWriter, r *http.Request) {
	id, ok := uuidParam(w, r, "appID")
	if !ok {
		return
	}
	var in TestIssueInput
	if r.ContentLength != 0 {
		if err := httpx.Decode(w, r, &in); err != nil {
			httpx.Error(w, r, err)
			return
		}
	}
	out, err := h.svc.SendTestIssue(r.Context(), httpx.OwnerID(r.Context()), id, in)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	switch {
	case out.Opened:
		httpx.JSON(w, http.StatusCreated, testIssueResponse{Results: out.Results})
	case out.RetryAfter > 0:
		httpx.SetRetryAfter(w, out.RetryAfter)
		httpx.JSON(w, http.StatusTooManyRequests, testIssueResponse{Error: "rate limited by the issue tracker; retry later", Results: out.Results})
	default:
		httpx.JSON(w, http.StatusBadGateway, testIssueResponse{Error: "no issue could be opened", Results: out.Results})
	}
}

func (h *Handler) create(w http.ResponseWriter, r *http.Request) {
	var in CreateInput
	if err := httpx.Decode(w, r, &in); err != nil {
		httpx.Error(w, r, err)
		return
	}
	a, err := h.svc.Create(r.Context(), httpx.OwnerID(r.Context()), in)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusCreated, a)
}

func (h *Handler) list(w http.ResponseWriter, r *http.Request) {
	as, err := h.svc.List(r.Context(), httpx.OwnerID(r.Context()))
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, as)
}

func (h *Handler) get(w http.ResponseWriter, r *http.Request) {
	id, ok := uuidParam(w, r, "appID")
	if !ok {
		return
	}
	a, err := h.svc.Get(r.Context(), httpx.OwnerID(r.Context()), id)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, a)
}

func (h *Handler) update(w http.ResponseWriter, r *http.Request) {
	id, ok := uuidParam(w, r, "appID")
	if !ok {
		return
	}
	var in UpdateInput
	if err := httpx.Decode(w, r, &in); err != nil {
		httpx.Error(w, r, err)
		return
	}
	a, err := h.svc.Update(r.Context(), httpx.OwnerID(r.Context()), id, in)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, a)
}

func (h *Handler) delete(w http.ResponseWriter, r *http.Request) {
	id, ok := uuidParam(w, r, "appID")
	if !ok {
		return
	}
	if err := h.svc.Delete(r.Context(), httpx.OwnerID(r.Context()), id); err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.NoContent(w)
}

func (h *Handler) listKeys(kind db.AppKeyKind) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, ok := uuidParam(w, r, "appID")
		if !ok {
			return
		}
		keys, err := h.svc.ListKeys(r.Context(), httpx.OwnerID(r.Context()), id, kind)
		if err != nil {
			httpx.Error(w, r, err)
			return
		}
		httpx.JSON(w, http.StatusOK, keys)
	}
}

func (h *Handler) createKey(kind db.AppKeyKind) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, ok := uuidParam(w, r, "appID")
		if !ok {
			return
		}
		k, err := h.svc.CreateKey(r.Context(), httpx.OwnerID(r.Context()), id, kind)
		if err != nil {
			httpx.Error(w, r, err)
			return
		}
		httpx.JSON(w, http.StatusCreated, k)
	}
}

func (h *Handler) revokeKey(kind db.AppKeyKind) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, ok := uuidParam(w, r, "appID")
		if !ok {
			return
		}
		keyID, ok := uuidParam(w, r, "keyID")
		if !ok {
			return
		}
		if err := h.svc.RevokeKey(r.Context(), httpx.OwnerID(r.Context()), id, keyID, kind); err != nil {
			httpx.Error(w, r, err)
			return
		}
		httpx.NoContent(w)
	}
}

func (h *Handler) listProjects(w http.ResponseWriter, r *http.Request) {
	id, ok := uuidParam(w, r, "appID")
	if !ok {
		return
	}
	ps, err := h.svc.ListProjects(r.Context(), httpx.OwnerID(r.Context()), id)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, ps)
}

func (h *Handler) linkProject(w http.ResponseWriter, r *http.Request) {
	id, ok := uuidParam(w, r, "appID")
	if !ok {
		return
	}
	projectID, ok := uuidParam(w, r, "projectID")
	if !ok {
		return
	}
	if err := h.svc.LinkProject(r.Context(), httpx.OwnerID(r.Context()), id, projectID); err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.NoContent(w)
}

func (h *Handler) unlinkProject(w http.ResponseWriter, r *http.Request) {
	id, ok := uuidParam(w, r, "appID")
	if !ok {
		return
	}
	projectID, ok := uuidParam(w, r, "projectID")
	if !ok {
		return
	}
	if err := h.svc.UnlinkProject(r.Context(), httpx.OwnerID(r.Context()), id, projectID); err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.NoContent(w)
}

func uuidParam(w http.ResponseWriter, r *http.Request, name string) (uuid.UUID, bool) {
	id, err := httpx.UUIDParam(r, name)
	if err != nil {
		httpx.Error(w, r, err)
		return uuid.Nil, false
	}
	return id, true
}
