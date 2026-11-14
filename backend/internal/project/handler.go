package project

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"go.uber.org/fx"

	"github.com/MicroClub-USTHB/m-Seismo/backend/internal/app"
	"github.com/MicroClub-USTHB/m-Seismo/backend/internal/httpx"
)

var Module = fx.Module("project",
	fx.Provide(
		NewService,
		httpx.AsRoutes(NewHandler),
	),
)

type Handler struct {
	svc     *Service
	apps    *app.Service
	require httpx.AuthMiddleware
}

func NewHandler(svc *Service, apps *app.Service, require httpx.AuthMiddleware) *Handler {
	return &Handler{svc: svc, apps: apps, require: require}
}

func (h *Handler) Mount(r chi.Router) {
	r.With(h.require).Route("/projects", func(r chi.Router) {
		r.Post("/", h.create)
		r.Get("/", h.list)
		r.Route("/{projectID}", func(r chi.Router) {
			r.Get("/", h.get)
			r.Patch("/", h.update)
			r.Delete("/", h.delete)

			r.Get("/apps", h.listApps)
			r.Post("/apps", h.createApp)

			r.Get("/repositories", h.listRepositories)
			r.Post("/repositories", h.linkRepository)
			r.Put("/repositories/{repositoryID}/default", h.setDefaultRepository)
			r.Delete("/repositories/{repositoryID}", h.unlinkRepository)
		})
	})
}

func (h *Handler) create(w http.ResponseWriter, r *http.Request) {
	var in CreateInput
	if err := httpx.Decode(w, r, &in); err != nil {
		httpx.Error(w, r, err)
		return
	}
	p, err := h.svc.Create(r.Context(), httpx.OwnerID(r.Context()), in)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusCreated, p)
}

func (h *Handler) list(w http.ResponseWriter, r *http.Request) {
	ps, err := h.svc.List(r.Context(), httpx.OwnerID(r.Context()))
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, ps)
}

func (h *Handler) get(w http.ResponseWriter, r *http.Request) {
	id, ok := projectID(w, r)
	if !ok {
		return
	}
	p, err := h.svc.Get(r.Context(), httpx.OwnerID(r.Context()), id)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, p)
}

func (h *Handler) update(w http.ResponseWriter, r *http.Request) {
	id, ok := projectID(w, r)
	if !ok {
		return
	}
	var in UpdateInput
	if err := httpx.Decode(w, r, &in); err != nil {
		httpx.Error(w, r, err)
		return
	}
	p, err := h.svc.Update(r.Context(), httpx.OwnerID(r.Context()), id, in)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, p)
}

func (h *Handler) delete(w http.ResponseWriter, r *http.Request) {
	id, ok := projectID(w, r)
	if !ok {
		return
	}
	if err := h.svc.Delete(r.Context(), httpx.OwnerID(r.Context()), id); err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.NoContent(w)
}

func (h *Handler) listApps(w http.ResponseWriter, r *http.Request) {
	id, ok := projectID(w, r)
	if !ok {
		return
	}
	apps, err := h.svc.ListApps(r.Context(), httpx.OwnerID(r.Context()), id)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, apps)
}

// createApp adds a sub-app to the project — a web, iOS or Android build
// that reports into the project's repositories.
func (h *Handler) createApp(w http.ResponseWriter, r *http.Request) {
	id, ok := projectID(w, r)
	if !ok {
		return
	}
	var in app.CreateInput
	if err := httpx.Decode(w, r, &in); err != nil {
		httpx.Error(w, r, err)
		return
	}
	a, err := h.apps.CreateForProject(r.Context(), httpx.OwnerID(r.Context()), id, in)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusCreated, a)
}

func (h *Handler) listRepositories(w http.ResponseWriter, r *http.Request) {
	id, ok := projectID(w, r)
	if !ok {
		return
	}
	repos, err := h.svc.ListRepositories(r.Context(), httpx.OwnerID(r.Context()), id)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, repos)
}

func (h *Handler) linkRepository(w http.ResponseWriter, r *http.Request) {
	id, ok := projectID(w, r)
	if !ok {
		return
	}
	var in LinkRepositoryInput
	if err := httpx.Decode(w, r, &in); err != nil {
		httpx.Error(w, r, err)
		return
	}
	repo, err := h.svc.LinkRepository(r.Context(), httpx.OwnerID(r.Context()), id, in)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusCreated, repo)
}

func (h *Handler) setDefaultRepository(w http.ResponseWriter, r *http.Request) {
	id, ok := projectID(w, r)
	if !ok {
		return
	}
	repoID, err := httpx.UUIDParam(r, "repositoryID")
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	if err := h.svc.SetDefaultRepository(r.Context(), httpx.OwnerID(r.Context()), id, repoID); err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.NoContent(w)
}

func (h *Handler) unlinkRepository(w http.ResponseWriter, r *http.Request) {
	id, ok := projectID(w, r)
	if !ok {
		return
	}
	repoID, err := httpx.UUIDParam(r, "repositoryID")
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	if err := h.svc.UnlinkRepository(r.Context(), httpx.OwnerID(r.Context()), id, repoID); err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.NoContent(w)
}

func projectID(w http.ResponseWriter, r *http.Request) (uuid.UUID, bool) {
	id, err := httpx.UUIDParam(r, "projectID")
	if err != nil {
		httpx.Error(w, r, err)
		return uuid.Nil, false
	}
	return id, true
}
