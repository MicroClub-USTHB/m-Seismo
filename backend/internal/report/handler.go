package report

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/MicroClub-USTHB/m-Seismo/backend/internal/httpx"
)

// Handler serves the report-deletion endpoints described in issue #61:
// DELETE /api/v1/apps/{appID}/reports/{reportID} and
// DELETE /api/v1/apps/{appID}/installations/{installationID}/reports.
type Handler struct {
	svc     *Service
	require httpx.AuthMiddleware
}

func NewHandler(svc *Service, require httpx.AuthMiddleware) *Handler {
	return &Handler{svc: svc, require: require}
}

func (h *Handler) Mount(r chi.Router) {
	r.With(h.require).Route("/apps/{appID}", func(r chi.Router) {
		r.Delete("/reports/{reportID}", h.deleteReport)
		r.Delete("/installations/{installationID}/reports", h.deleteByInstallation)
	})
}

func (h *Handler) deleteReport(w http.ResponseWriter, r *http.Request) {
	appID, ok := uuidParam(w, r, "appID")
	if !ok {
		return
	}
	reportID, ok := uuidParam(w, r, "reportID")
	if !ok {
		return
	}
	if err := h.svc.DeleteReport(r.Context(), httpx.OwnerID(r.Context()), appID, reportID); err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.NoContent(w)
}

func (h *Handler) deleteByInstallation(w http.ResponseWriter, r *http.Request) {
	appID, ok := uuidParam(w, r, "appID")
	if !ok {
		return
	}
	installationID, ok := uuidParam(w, r, "installationID")
	if !ok {
		return
	}
	n, err := h.svc.DeleteByInstallation(r.Context(), httpx.OwnerID(r.Context()), appID, installationID)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}
	httpx.JSON(w, http.StatusOK, map[string]int64{"deleted": n})
}

func uuidParam(w http.ResponseWriter, r *http.Request, name string) (uuid.UUID, bool) {
	id, err := httpx.UUIDParam(r, name)
	if err != nil {
		httpx.Error(w, r, err)
		return uuid.Nil, false
	}
	return id, true
}
