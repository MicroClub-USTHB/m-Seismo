package issuetemplate

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"go.uber.org/fx"

	"github.com/MicroClub-USTHB/m-Seismo/backend/internal/httpx"
)

// Module provides the issue-templates API.
var Module = fx.Module("issuetemplate",
	fx.Provide(
		NewService,
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
	r.With(h.require).Route("/projects/{projectID}/issue-templates", func(r chi.Router) {
		r.Get("/", h.list)
		r.Post("/", h.create)
		r.Put("/{templateID}", h.replace)
		r.Delete("/{templateID}", h.delete)
	})
}

func (h *Handler) list(w http.ResponseWriter, r *http.Request) {
	projectID, ok := uuidParam(w, r, "projectID")
	if !ok {
		return
	}
	templates, err := h.svc.List(r.Context(), httpx.OwnerID(r.Context()), projectID)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, templates)
}

func (h *Handler) create(w http.ResponseWriter, r *http.Request) {
	projectID, ok := uuidParam(w, r, "projectID")
	if !ok {
		return
	}
	var in Input
	if err := httpx.Decode(w, r, &in); err != nil {
		httpx.Error(w, r, err)
		return
	}
	t, err := h.svc.Create(r.Context(), httpx.OwnerID(r.Context()), projectID, in)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusCreated, t)
}

func (h *Handler) replace(w http.ResponseWriter, r *http.Request) {
	projectID, ok := uuidParam(w, r, "projectID")
	if !ok {
		return
	}
	templateID, ok := uuidParam(w, r, "templateID")
	if !ok {
		return
	}
	var in Input
	if err := httpx.Decode(w, r, &in); err != nil {
		httpx.Error(w, r, err)
		return
	}
	t, err := h.svc.Replace(r.Context(), httpx.OwnerID(r.Context()), projectID, templateID, in)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, t)
}

func (h *Handler) delete(w http.ResponseWriter, r *http.Request) {
	projectID, ok := uuidParam(w, r, "projectID")
	if !ok {
		return
	}
	templateID, ok := uuidParam(w, r, "templateID")
	if !ok {
		return
	}
	if err := h.svc.Delete(r.Context(), httpx.OwnerID(r.Context()), projectID, templateID); err != nil {
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
