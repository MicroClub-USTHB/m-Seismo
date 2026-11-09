package report

import (
	"bytes"
	"fmt"
	"net/http"
	"strconv"

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

// Mount registers full paths directly (not r.Route("/apps/{appID}", ...)):
// that would Mount a second sub-router at the same pattern app.Handler
// already owns, which shadows its own routes under /apps/{appID}.
func (h *Handler) Mount(r chi.Router) {
	r.With(h.require).Get("/apps/{appID}/reports/{reportID}/bundle", h.bundle)
	r.With(h.require).Delete("/apps/{appID}/reports/{reportID}", h.deleteReport)
	r.With(h.require).Delete("/apps/{appID}/installations/{installationID}/reports", h.deleteByInstallation)
}

func (h *Handler) bundle(w http.ResponseWriter, r *http.Request) {
	appID, ok := uuidParam(w, r, "appID")
	if !ok {
		return
	}
	reportID, ok := uuidParam(w, r, "reportID")
	if !ok {
		return
	}

	var buf bytes.Buffer
	if err := h.svc.Bundle(r.Context(), httpx.OwnerID(r.Context()), appID, reportID, &buf); err != nil {
		httpx.Error(w, r, err)
		return
	}

	w.Header().Set("Content-Type", "application/zip")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="report-%s.zip"`, reportID))
	w.Header().Set("Content-Length", strconv.Itoa(buf.Len()))
	w.WriteHeader(http.StatusOK)
	_, _ = buf.WriteTo(w)
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
