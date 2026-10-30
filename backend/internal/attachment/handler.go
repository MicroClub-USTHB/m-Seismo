package attachment

import (
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"github.com/MicroClub-USTHB/m-Seismo/backend/internal/apperr"
	"github.com/MicroClub-USTHB/m-Seismo/backend/internal/httpx"
)

// Handler serves GET /api/v1/attachments/{id}?expires=…&sig=…, the private,
// expiring link an issue shows for a report's attachment (FR-48).
type Handler struct {
	svc *Service
}

func NewHandler(svc *Service) *Handler {
	return &Handler{svc: svc}
}

func (h *Handler) Mount(r chi.Router) {
	r.Get("/attachments/{id}", h.get)
}

func (h *Handler) get(w http.ResponseWriter, r *http.Request) {
	id, err := httpx.UUIDParam(r, "id")
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	expires, err := strconv.ParseInt(r.URL.Query().Get("expires"), 10, 64)
	if err != nil {
		httpx.Error(w, r, apperr.Unauthorized("invalid or expired link"))
		return
	}
	a, err := h.svc.Fetch(r.Context(), id, expires, r.URL.Query().Get("sig"))
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	w.Header().Set("Content-Type", a.ContentType)
	w.Header().Set("Cache-Control", "private, max-age=3600")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(a.Data)
}
