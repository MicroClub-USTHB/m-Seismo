package report

import (
	"bytes"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/MicroClub-USTHB/m-Seismo/backend/internal/apperr"
	"github.com/MicroClub-USTHB/m-Seismo/backend/internal/httpx"
)

// Handler serves report management and deletion endpoints.
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
	r.With(h.require).Get("/apps/{appID}/reports", h.listReports)
	r.With(h.require).Get("/apps/{appID}/reports/{reportID}", h.getReport)
	r.With(h.require).Post("/apps/{appID}/reports/{reportID}/retry", h.retryReport)
	r.With(h.require).Get("/apps/{appID}/reports/{reportID}/bundle", h.bundle)
	r.With(h.require).Delete("/apps/{appID}/reports/{reportID}", h.deleteReport)
	r.With(h.require).Delete("/apps/{appID}/installations/{installationID}/reports", h.deleteByInstallation)
}

func (h *Handler) listReports(w http.ResponseWriter, r *http.Request) {
	appID, ok := uuidParam(w, r, "appID")
	if !ok {
		return
	}

	q := r.URL.Query()
	status := q.Get("status")

	var cursor *time.Time
	if cursorStr := q.Get("cursor"); cursorStr != "" {
		if t, err := time.Parse(time.RFC3339Nano, cursorStr); err == nil {
			cursor = &t
		} else if t, err := time.Parse(time.RFC3339, cursorStr); err == nil {
			cursor = &t
		} else {
			httpx.Error(w, r, apperr.Invalid("invalid cursor timestamp format"))
			return
		}
	}

	limit := int32(20)
	if limitStr := q.Get("limit"); limitStr != "" {
		if n, err := strconv.Atoi(limitStr); err == nil && n > 0 {
			limit = int32(n)
		}
	}

	params := ListReportsParams{
		Status: status,
		Cursor: cursor,
		Limit:  limit,
	}

	res, err := h.svc.ListReports(r.Context(), httpx.OwnerID(r.Context()), appID, params)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}

	httpx.JSON(w, http.StatusOK, res)
}

func (h *Handler) getReport(w http.ResponseWriter, r *http.Request) {
	appID, ok := uuidParam(w, r, "appID")
	if !ok {
		return
	}
	reportID, ok := uuidParam(w, r, "reportID")
	if !ok {
		return
	}

	res, err := h.svc.GetReport(r.Context(), httpx.OwnerID(r.Context()), appID, reportID)
	if err != nil {
		httpx.Error(w, r, err)
		return
	}

	httpx.JSON(w, http.StatusOK, res)
}

func (h *Handler) retryReport(w http.ResponseWriter, r *http.Request) {
	appID, ok := uuidParam(w, r, "appID")
	if !ok {
		return
	}
	reportID, ok := uuidParam(w, r, "reportID")
	if !ok {
		return
	}

	if err := h.svc.RetryReport(r.Context(), httpx.OwnerID(r.Context()), appID, reportID); err != nil {
		httpx.Error(w, r, err)
		return
	}

	w.WriteHeader(http.StatusAccepted)
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
