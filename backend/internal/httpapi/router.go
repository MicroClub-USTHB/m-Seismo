// Package httpapi exposes the admin REST API: users, projects and apps (and,
// in later phases, git connections and repository links).
package httpapi

import (
	"context"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"github.com/MicroClub-USTHB/m-Seismo/backend/internal/apps"
	"github.com/MicroClub-USTHB/m-Seismo/backend/internal/projects"
	"github.com/MicroClub-USTHB/m-Seismo/backend/internal/users"
)

type Deps struct {
	AdminToken     string
	RequestTimeout time.Duration
	// Ready reports whether the service can take traffic (database reachable).
	Ready func(ctx context.Context) error

	Users    *users.Service
	Projects *projects.Service
	Apps     *apps.Service
}

func NewRouter(d Deps) http.Handler {
	r := chi.NewRouter()
	r.Use(middleware.RequestID, middleware.RealIP, requestLogger, recoverer)

	r.NotFound(func(w http.ResponseWriter, r *http.Request) {
		writeProblem(w, r, Problem{Status: http.StatusNotFound})
	})
	r.MethodNotAllowed(func(w http.ResponseWriter, r *http.Request) {
		writeProblem(w, r, Problem{Status: http.StatusMethodNotAllowed})
	})

	r.Get("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})
	r.Get("/readyz", func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
		defer cancel()
		if err := d.Ready(ctx); err != nil {
			writeProblem(w, r, Problem{Status: http.StatusServiceUnavailable, Detail: "database unreachable"})
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})

	r.Route("/v1", func(r chi.Router) {
		r.Use(middleware.Timeout(d.RequestTimeout), requireAdminToken(d.AdminToken))

		uh := userHandlers{svc: d.Users}
		r.Post("/users", uh.create)
		r.Get("/users", uh.list)
		r.Route("/users/{user_id}", func(r chi.Router) {
			r.Get("/", uh.get)
			r.Patch("/", uh.update)
			r.Delete("/", uh.delete)

			ph := projectHandlers{svc: d.Projects}
			r.Post("/projects", ph.create)
			r.Get("/projects", ph.list)
			r.Route("/projects/{project_id}", func(r chi.Router) {
				r.Get("/", ph.get)
				r.Patch("/", ph.update)
				r.Delete("/", ph.delete)

				ah := appHandlers{svc: d.Apps}
				r.Post("/apps", ah.create)
				r.Get("/apps", ah.list)
				r.Get("/apps/{app_id}", ah.get)
				r.Patch("/apps/{app_id}", ah.update)
				r.Delete("/apps/{app_id}", ah.delete)
			})
		})
	})
	return r
}
