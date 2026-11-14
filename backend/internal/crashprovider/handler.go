package crashprovider

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"go.uber.org/fx"

	"github.com/MicroClub-USTHB/m-Seismo/backend/internal/httpx"
)

var Module = fx.Module("crashprovider", fx.Provide(NewService, httpx.AsRoutes(NewHandler)))

type Handler struct {
	svc     *Service
	require httpx.AuthMiddleware
}

func NewHandler(svc *Service, require httpx.AuthMiddleware) *Handler {
	return &Handler{svc: svc, require: require}
}

func (h *Handler) Mount(r chi.Router) {
	r.With(h.require).Route("/apps/{appID}/crash-provider", func(r chi.Router) {
		r.Get("/", h.get)
		r.Put("/", h.set)
		r.Delete("/", h.remove)
	})
	r.With(h.require).Get("/apps/{appID}/reports/{reportID}/crash", h.reportCrash)
}

func (h *Handler) get(w http.ResponseWriter, r *http.Request) {
	id, err := httpx.UUIDParam(r, "appID")
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	out, err := h.svc.Get(r.Context(), httpx.OwnerID(r.Context()), id)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, out)
}

func (h *Handler) set(w http.ResponseWriter, r *http.Request) {
	id, err := httpx.UUIDParam(r, "appID")
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	var in Input
	if err := httpx.Decode(w, r, &in); err != nil {
		httpx.Error(w, r, err)
		return
	}
	out, err := h.svc.Set(r.Context(), httpx.OwnerID(r.Context()), id, in)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, out)
}

func (h *Handler) remove(w http.ResponseWriter, r *http.Request) {
	id, err := httpx.UUIDParam(r, "appID")
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	if err := h.svc.Delete(r.Context(), httpx.OwnerID(r.Context()), id); err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.NoContent(w)
}

func (h *Handler) reportCrash(w http.ResponseWriter, r *http.Request) {
	appID, err := httpx.UUIDParam(r, "appID")
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	reportID, err := httpx.UUIDParam(r, "reportID")
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	out, err := h.svc.ReportCrash(r.Context(), httpx.OwnerID(r.Context()), appID, reportID)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, out)
}
