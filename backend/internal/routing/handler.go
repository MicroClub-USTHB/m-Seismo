package routing

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"go.uber.org/fx"

	"github.com/MicroClub-USTHB/m-Seismo/backend/internal/db"
	"github.com/MicroClub-USTHB/m-Seismo/backend/internal/httpx"
)

// Module provides the routing rules API and the Router the worker uses.
var Module = fx.Module("routing",
	fx.Provide(
		NewService,
		func(q db.Querier) *Router { return NewRouter(q) },
		httpx.AsRoutes(NewHandler),
	),
)

type Handler struct {
	svc     *Service
	require httpx.AuthMiddleware
}

func NewHandler(svc *Service, require httpx.AuthMiddleware) *Handler {
	return &Handler{svc: svc, require: require}
}

func (h *Handler) Mount(r chi.Router) {
	r.With(h.require).Route("/projects/{projectID}/routing-rules", func(r chi.Router) {
		r.Get("/", h.list)
		r.Post("/", h.create)
		r.Put("/{ruleID}", h.replace)
		r.Delete("/{ruleID}", h.delete)
	})
}

func (h *Handler) list(w http.ResponseWriter, r *http.Request) {
	projectID, ok := uuidParam(w, r, "projectID")
	if !ok {
		return
	}
	rules, err := h.svc.List(r.Context(), httpx.OwnerID(r.Context()), projectID)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, rules)
}

func (h *Handler) create(w http.ResponseWriter, r *http.Request) {
	projectID, ok := uuidParam(w, r, "projectID")
	if !ok {
		return
	}
	var in RuleInput
	if err := httpx.Decode(w, r, &in); err != nil {
		httpx.Error(w, r, err)
		return
	}
	rule, err := h.svc.Create(r.Context(), httpx.OwnerID(r.Context()), projectID, in)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusCreated, rule)
}

func (h *Handler) replace(w http.ResponseWriter, r *http.Request) {
	projectID, ok := uuidParam(w, r, "projectID")
	if !ok {
		return
	}
	ruleID, ok := uuidParam(w, r, "ruleID")
	if !ok {
		return
	}
	var in RuleInput
	if err := httpx.Decode(w, r, &in); err != nil {
		httpx.Error(w, r, err)
		return
	}
	rule, err := h.svc.Replace(r.Context(), httpx.OwnerID(r.Context()), projectID, ruleID, in)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, rule)
}

func (h *Handler) delete(w http.ResponseWriter, r *http.Request) {
	projectID, ok := uuidParam(w, r, "projectID")
	if !ok {
		return
	}
	ruleID, ok := uuidParam(w, r, "ruleID")
	if !ok {
		return
	}
	if err := h.svc.Delete(r.Context(), httpx.OwnerID(r.Context()), projectID, ruleID); err != nil {
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
