package httpapi

import (
	"net/http"

	"github.com/MicroClub-USTHB/m-Seismo/backend/internal/users"
)

type userHandlers struct {
	svc *users.Service
}

func (h userHandlers) create(w http.ResponseWriter, r *http.Request) {
	var in users.CreateInput
	if !decodeJSON(w, r, &in) {
		return
	}
	u, err := h.svc.Create(r.Context(), in)
	if err != nil {
		writeError(w, r, err)
		return
	}
	w.Header().Set("Location", "/v1/users/"+u.ID.String())
	writeJSON(w, http.StatusCreated, u)
}

func (h userHandlers) list(w http.ResponseWriter, r *http.Request) {
	page, ok := pageParams(w, r)
	if !ok {
		return
	}
	res, err := h.svc.List(r.Context(), page)
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeList(w, res)
}

func (h userHandlers) get(w http.ResponseWriter, r *http.Request) {
	id, ok := pathUUID(w, r, "user_id")
	if !ok {
		return
	}
	u, err := h.svc.Get(r.Context(), id)
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, u)
}

func (h userHandlers) update(w http.ResponseWriter, r *http.Request) {
	id, ok := pathUUID(w, r, "user_id")
	if !ok {
		return
	}
	var in users.UpdateInput
	if !decodeJSON(w, r, &in) {
		return
	}
	u, err := h.svc.Update(r.Context(), id, in)
	if err != nil {
		writeError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, u)
}

func (h userHandlers) delete(w http.ResponseWriter, r *http.Request) {
	id, ok := pathUUID(w, r, "user_id")
	if !ok {
		return
	}
	if err := h.svc.Delete(r.Context(), id); err != nil {
		writeError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
