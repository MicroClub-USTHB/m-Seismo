package httpapi

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/MicroClub-USTHB/m-Seismo/backend/internal/paging"
)

const maxBodyBytes = 1 << 20

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// decodeJSON reads exactly one JSON object into dst, rejecting unknown
// fields and oversized bodies. On failure it writes the problem and returns
// false.
func decodeJSON(w http.ResponseWriter, r *http.Request, dst any) bool {
	if ct := r.Header.Get("Content-Type"); ct != "" {
		mt, _, err := mime.ParseMediaType(ct)
		if err != nil || mt != "application/json" {
			writeProblem(w, r, Problem{
				Type: problemBase + "unsupported-media-type", Status: http.StatusUnsupportedMediaType,
				Detail: "Content-Type must be application/json",
			})
			return false
		}
	}
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxBodyBytes))
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		var maxErr *http.MaxBytesError
		if errors.As(err, &maxErr) {
			writeProblem(w, r, Problem{Status: http.StatusRequestEntityTooLarge})
			return false
		}
		badRequest(w, r, "invalid JSON body: "+err.Error())
		return false
	}
	if dec.Decode(&struct{}{}) != io.EOF {
		badRequest(w, r, "body must contain a single JSON object")
		return false
	}
	return true
}

// pathUUID parses a UUID path parameter, writing a 404 when it is malformed:
// a malformed id cannot name an existing resource.
func pathUUID(w http.ResponseWriter, r *http.Request, name string) (uuid.UUID, bool) {
	id, err := uuid.Parse(chi.URLParam(r, name))
	if err != nil {
		writeProblem(w, r, Problem{
			Type: problemBase + "not-found", Status: http.StatusNotFound, Detail: name + " is not a valid id",
		})
		return uuid.Nil, false
	}
	return id, true
}

func pageParams(w http.ResponseWriter, r *http.Request) (paging.Page, bool) {
	var p paging.Page
	q := r.URL.Query()
	if s := q.Get("limit"); s != "" {
		n, err := strconv.Atoi(s)
		if err != nil || n < 1 || n > paging.MaxLimit {
			badRequest(w, r, fmt.Sprintf("limit must be an integer between 1 and %d", paging.MaxLimit))
			return p, false
		}
		p.Limit = n
	}
	c, err := paging.Decode(q.Get("cursor"))
	if err != nil {
		badRequest(w, r, "cursor is not valid")
		return p, false
	}
	p.After = c
	return p, true
}

type listResponse[T any] struct {
	Items      []T    `json:"items"`
	NextCursor string `json:"next_cursor,omitempty"`
}

func writeList[T any](w http.ResponseWriter, res paging.Result[T]) {
	items := res.Items
	if items == nil {
		items = []T{}
	}
	writeJSON(w, http.StatusOK, listResponse[T]{Items: items, NextCursor: res.NextCursor})
}
