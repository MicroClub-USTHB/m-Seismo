package httpapi

import (
	"net/http"

	"github.com/google/uuid"

	"github.com/MicroClub-USTHB/m-Seismo/backend/internal/apps"
)

type appHandlers struct {
	svc *apps.Service
}

func (h appHandlers) scope(w http.ResponseWriter, r *http.Request) (userID, projectID uuid.UUID, ok bool) {
	if userID, ok = pathUUID(w, r, "user_id"); !ok {
		return
	}
	projectID, ok = pathUUID(w, r, "project_id")
	return
}

func (h appHandlers) ids(w http.ResponseWriter, r *http.Request) (userID, projectID, appID uuid.UUID, ok bool) {
	if userID, projectID, ok = h.scope(w, r); !ok {
		return
	}
	appID, ok = pathUUID(w, r, "app_id")
	return
}

func (h appHandlers) create(w http.ResponseWriter, r *http.Request) {
	userID, projectID, ok := h.scope(w, r)
	if !ok {
		return
	}
	var in apps.CreateInput
	if !decodeJSON(w, r, &in) {
		return
	}
	a, err := h.svc.Create(r.Context(), userID, projectID, in)
	if err != nil {
		writeError(w, r, err)
		return
	}
	w.Header().Set("Location", r.URL.Path+"/"+a.ID.String())
	writeJSON(w, http.StatusCreated, a)
}

func (h appHandlers) list(w http.ResponseWriter, r *http.Request) {
	userID, projectID, ok := h.scope(w, r)
	if !ok {
		return
	}
	page, ok := pageParams(w, r)
	if !ok {
		return
	}
	res, err := h.svc.List(r.Context(), userID, projectID, page)
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeList(w, res)
}

func (h appHandlers) get(w http.ResponseWriter, r *http.Request) {
	userID, projectID, appID, ok := h.ids(w, r)
	if !ok {
		return
	}
	a, err := h.svc.Get(r.Context(), userID, projectID, appID)
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, a)
}

func (h appHandlers) update(w http.ResponseWriter, r *http.Request) {
	userID, projectID, appID, ok := h.ids(w, r)
	if !ok {
		return
	}
	var in apps.UpdateInput
	if !decodeJSON(w, r, &in) {
		return
	}
	a, err := h.svc.Update(r.Context(), userID, projectID, appID, in)
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, a)
}

func (h appHandlers) delete(w http.ResponseWriter, r *http.Request) {
	userID, projectID, appID, ok := h.ids(w, r)
	if !ok {
		return
	}
	if err := h.svc.Delete(r.Context(), userID, projectID, appID); err != nil {
		writeError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
