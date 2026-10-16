package httpapi

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5/middleware"

	"github.com/MicroClub-USTHB/m-Seismo/backend/internal/apperr"
)

// Problem is an RFC 9457 problem details object.
type Problem struct {
	Type          string              `json:"type"`
	Title         string              `json:"title"`
	Status        int                 `json:"status"`
	Detail        string              `json:"detail,omitempty"`
	Instance      string              `json:"instance,omitempty"`
	RequestID     string              `json:"request_id,omitempty"`
	InvalidParams []apperr.FieldError `json:"invalid-params,omitempty"`
}

const problemBase = "https://m-seismo.dev/problems/"

func writeProblem(w http.ResponseWriter, r *http.Request, p Problem) {
	if p.Type == "" {
		p.Type = "about:blank"
	}
	if p.Title == "" {
		p.Title = http.StatusText(p.Status)
	}
	p.Instance = r.URL.Path
	p.RequestID = middleware.GetReqID(r.Context())
	w.Header().Set("Content-Type", "application/problem+json")
	w.WriteHeader(p.Status)
	_ = json.NewEncoder(w).Encode(p)
}

// writeError maps a service error to a problem. Unknown errors are logged and
// reported as 500 without leaking their text.
func writeError(w http.ResponseWriter, r *http.Request, err error) {
	var verr *apperr.ValidationError
	switch {
	case errors.As(err, &verr):
		writeProblem(w, r, Problem{
			Type: problemBase + "validation", Title: "Your request is not valid.",
			Status: http.StatusUnprocessableEntity, InvalidParams: verr.Fields,
		})
	case errors.Is(err, apperr.ErrNotFound):
		writeProblem(w, r, Problem{
			Type: problemBase + "not-found", Status: http.StatusNotFound, Detail: err.Error(),
		})
	case errors.Is(err, apperr.ErrConflict):
		writeProblem(w, r, Problem{
			Type: problemBase + "conflict", Status: http.StatusConflict, Detail: err.Error(),
		})
	default:
		slog.ErrorContext(r.Context(), "request failed",
			"err", err, "method", r.Method, "path", r.URL.Path, "request_id", middleware.GetReqID(r.Context()))
		writeProblem(w, r, Problem{Status: http.StatusInternalServerError})
	}
}

func badRequest(w http.ResponseWriter, r *http.Request, detail string) {
	writeProblem(w, r, Problem{Type: problemBase + "bad-request", Status: http.StatusBadRequest, Detail: detail})
}
