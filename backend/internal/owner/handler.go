package owner

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"
	"go.uber.org/fx"

	"github.com/MicroClub-USTHB/m-Seismo/backend/internal/auth"
	"github.com/MicroClub-USTHB/m-Seismo/backend/internal/config"
	"github.com/MicroClub-USTHB/m-Seismo/backend/internal/httpx"
)

var Module = fx.Module("owner",
	fx.Provide(
		NewService,
		httpx.AsRoutes(NewHandler),
	),
	fx.Invoke(seedSuperadmin),
)

// seedSuperadmin runs EnsureSuperadmin once the server starts serving, so a
// transient DB hiccup at boot fails the same way any other startup error
// does rather than being swallowed.
func seedSuperadmin(lc fx.Lifecycle, cfg *config.Config, svc *Service) {
	lc.Append(fx.Hook{OnStart: func(ctx context.Context) error {
		sa := cfg.Superadmin
		if sa.Email == "" || sa.Password == "" {
			return nil
		}
		if err := svc.EnsureSuperadmin(ctx, sa.Email, sa.Password, sa.Name); err != nil {
			return fmt.Errorf("owner: seed superadmin: %w", err)
		}
		slog.Info("superadmin ready", "email", sa.Email)
		return nil
	}})
}

type Handler struct {
	svc     *Service
	require httpx.AuthMiddleware
}

func NewHandler(svc *Service, require httpx.AuthMiddleware) *Handler {
	return &Handler{svc: svc, require: require}
}

func (h *Handler) Mount(r chi.Router) {
	r.Post("/auth/register", h.register)
	r.Post("/auth/login", h.login)

	r.With(h.require).Get("/owners", h.listAll)
	r.With(h.require).Route("/owners/me", func(r chi.Router) {
		r.Get("/", h.get)
		r.Patch("/", h.update)
		r.Delete("/", h.delete)
	})
}

type sessionResponse struct {
	Owner Owner      `json:"owner"`
	Token auth.Token `json:"token"`
}

func (h *Handler) register(w http.ResponseWriter, r *http.Request) {
	var in RegisterInput
	if err := httpx.Decode(w, r, &in); err != nil {
		httpx.Error(w, r, err)
		return
	}
	o, tok, err := h.svc.Register(r.Context(), in)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusCreated, sessionResponse{Owner: o, Token: tok})
}

type loginRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

func (h *Handler) login(w http.ResponseWriter, r *http.Request) {
	var in loginRequest
	if err := httpx.Decode(w, r, &in); err != nil {
		httpx.Error(w, r, err)
		return
	}
	o, tok, err := h.svc.Login(r.Context(), in.Email, in.Password)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, sessionResponse{Owner: o, Token: tok})
}

// listAll lists every owner. Only the configured superadmin may call it
// (see Service.ListAll); anyone else gets the same 401 as a bad login.
func (h *Handler) listAll(w http.ResponseWriter, r *http.Request) {
	os, err := h.svc.ListAll(r.Context(), httpx.OwnerID(r.Context()))
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, os)
}

func (h *Handler) get(w http.ResponseWriter, r *http.Request) {
	o, err := h.svc.Get(r.Context(), httpx.OwnerID(r.Context()))
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, o)
}

func (h *Handler) update(w http.ResponseWriter, r *http.Request) {
	var in UpdateInput
	if err := httpx.Decode(w, r, &in); err != nil {
		httpx.Error(w, r, err)
		return
	}
	o, err := h.svc.Update(r.Context(), httpx.OwnerID(r.Context()), in)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, o)
}

func (h *Handler) delete(w http.ResponseWriter, r *http.Request) {
	if err := h.svc.Delete(r.Context(), httpx.OwnerID(r.Context())); err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.NoContent(w)
}
