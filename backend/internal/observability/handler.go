package observability

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/MicroClub-USTHB/m-Seismo/backend/internal/httpx"
)

// Handler handles HTTP requests for app observability provider configurations.
type Handler struct {
	svc     *Service
	require httpx.AuthMiddleware
}

// NewHandler creates a new Handler with the given Service and auth middleware.
func NewHandler(svc *Service, require httpx.AuthMiddleware) *Handler {
	return &Handler{svc: svc, require: require}
}

// Mount registers observability routes under /apps/{appID}/observability.
func (h *Handler) Mount(r chi.Router) {
	r.With(h.require).Route("/apps/{appID}/observability", func(r chi.Router) {
		r.Get("/", h.list)
		r.Delete("/", h.deleteAll)
		r.Route("/{provider}", func(r chi.Router) {
			r.Get("/", h.get)
			r.Put("/", h.set)
			r.Delete("/", h.delete)
		})
	})
}

func (h *Handler) list(w http.ResponseWriter, r *http.Request) {
	appID, err := httpx.UUIDParam(r, "appID")
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	ownerID := httpx.OwnerID(r.Context())

	out, err := h.svc.List(r.Context(), ownerID, appID)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, out)
}

func (h *Handler) deleteAll(w http.ResponseWriter, r *http.Request) {
	appID, err := httpx.UUIDParam(r, "appID")
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	ownerID := httpx.OwnerID(r.Context())

	if err := h.svc.DeleteAll(r.Context(), ownerID, appID); err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.NoContent(w)
}

func (h *Handler) get(w http.ResponseWriter, r *http.Request) {
	appID, err := httpx.UUIDParam(r, "appID")
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	provider := Provider(chi.URLParam(r, "provider"))
	ownerID := httpx.OwnerID(r.Context())

	out, err := h.svc.Get(r.Context(), ownerID, appID, provider)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, out)
}

func (h *Handler) set(w http.ResponseWriter, r *http.Request) {
	appID, err := httpx.UUIDParam(r, "appID")
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	provider := Provider(chi.URLParam(r, "provider"))

	var settings json.RawMessage
	if err := httpx.Decode(w, r, &settings); err != nil {
		httpx.Error(w, r, err)
		return
	}

	ownerID := httpx.OwnerID(r.Context())
	if err := h.svc.Set(r.Context(), ownerID, appID, provider, settings); err != nil {
		httpx.Error(w, r, err)
		return
	}

	conn, err := h.svc.Get(r.Context(), ownerID, appID, provider)
	if err != nil {
		httpx.NoContent(w)
		return
	}
	httpx.JSON(w, http.StatusOK, conn)
}

func (h *Handler) delete(w http.ResponseWriter, r *http.Request) {
	appID, err := httpx.UUIDParam(r, "appID")
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	provider := Provider(chi.URLParam(r, "provider"))
	ownerID := httpx.OwnerID(r.Context())

	if err := h.svc.Delete(r.Context(), ownerID, appID, provider); err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.NoContent(w)
}
