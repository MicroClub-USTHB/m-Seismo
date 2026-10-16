package httpapi

import (
	"net/http"

	"github.com/google/uuid"

	"github.com/MicroClub-USTHB/m-Seismo/backend/internal/projects"
)

type projectHandlers struct {
	svc *projects.Service
}

func (h projectHandlers) ids(w http.ResponseWriter, r *http.Request) (userID, projectID uuid.UUID, ok bool) {
	if userID, ok = pathUUID(w, r, "user_id"); !ok {
		return
	}
	projectID, ok = pathUUID(w, r, "project_id")
	return
}

func (h projectHandlers) create(w http.ResponseWriter, r *http.Request) {
	userID, ok := pathUUID(w, r, "user_id")
	if !ok {
		return
	}
	var in projects.CreateInput
	if !decodeJSON(w, r, &in) {
		return
	}
	p, err := h.svc.Create(r.Context(), userID, in)
	if err != nil {
		writeError(w, r, err)
		return
	}
	w.Header().Set("Location", r.URL.Path+"/"+p.ID.String())
	writeJSON(w, http.StatusCreated, p)
}

func (h projectHandlers) list(w http.ResponseWriter, r *http.Request) {
	userID, ok := pathUUID(w, r, "user_id")
	if !ok {
		return
	}
	page, ok := pageParams(w, r)
	if !ok {
		return
	}
	res, err := h.svc.List(r.Context(), userID, page)
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeList(w, res)
}

func (h projectHandlers) get(w http.ResponseWriter, r *http.Request) {
	userID, projectID, ok := h.ids(w, r)
	if !ok {
		return
	}
	p, err := h.svc.Get(r.Context(), userID, projectID)
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, p)
}

func (h projectHandlers) update(w http.ResponseWriter, r *http.Request) {
	userID, projectID, ok := h.ids(w, r)
	if !ok {
		return
	}
	var in projects.UpdateInput
	if !decodeJSON(w, r, &in) {
		return
	}
	p, err := h.svc.Update(r.Context(), userID, projectID, in)
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, p)
}

func (h projectHandlers) delete(w http.ResponseWriter, r *http.Request) {
	userID, projectID, ok := h.ids(w, r)
	if !ok {
		return
	}
	if err := h.svc.Delete(r.Context(), userID, projectID); err != nil {
		writeError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
