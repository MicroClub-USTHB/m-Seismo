package httpx

import (
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"math"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/google/uuid"

	"github.com/MicroClub-USTHB/m-Seismo/backend/internal/apperr"
)

// maxBodyBytes caps request bodies on JSON endpoints.
const maxBodyBytes = 1 << 20

// JSON writes v as a JSON response with the given status.
func JSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if v != nil {
		_ = json.NewEncoder(w).Encode(v)
	}
}

// SetRetryAfter sets the Retry-After header to wait, rounded up to whole
// seconds (minimum 1).
func SetRetryAfter(w http.ResponseWriter, wait time.Duration) {
	secs := int(math.Ceil(wait.Seconds()))
	if secs < 1 {
		secs = 1
	}
	w.Header().Set("Retry-After", strconv.Itoa(secs))
}

// NoContent writes a 204.
func NoContent(w http.ResponseWriter) {
	w.WriteHeader(http.StatusNoContent)
}

type errorBody struct {
	Error string `json:"error"`
}

// Error writes err as a JSON error response. Internal errors are logged and
// their details hidden from the client. Rate-limit errors carry a
// Retry-After header (whole seconds, rounded up).
func Error(w http.ResponseWriter, r *http.Request, err error) {
	kind, msg := apperr.Classify(err)
	status := statusFor(kind)
	if wait, ok := apperr.RetryAfterOf(err); ok && kind == apperr.KindRateLimited {
		SetRetryAfter(w, wait)
	}
	if status == http.StatusInternalServerError {
		slog.ErrorContext(r.Context(), "request failed",
			"method", r.Method,
			"path", r.URL.Path,
			"request_id", middleware.GetReqID(r.Context()),
			"err", err,
		)
	}
	JSON(w, status, errorBody{Error: msg})
}

func statusFor(k apperr.Kind) int {
	switch k {
	case apperr.KindNotFound:
		return http.StatusNotFound
	case apperr.KindConflict:
		return http.StatusConflict
	case apperr.KindInvalid:
		return http.StatusUnprocessableEntity
	case apperr.KindUnauthorized:
		return http.StatusUnauthorized
	case apperr.KindRateLimited:
		return http.StatusTooManyRequests
	default:
		return http.StatusInternalServerError
	}
}

// Decode reads a JSON request body into v, rejecting unknown fields and
// trailing data.
func Decode(w http.ResponseWriter, r *http.Request, v any) error {
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxBodyBytes))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		return apperr.Invalid("invalid request body: " + err.Error())
	}
	if err := dec.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return apperr.Invalid("invalid request body: unexpected trailing data")
	}
	return nil
}

// UUIDParam parses the named chi URL parameter as a UUID. A malformed ID
// is reported as not found, the same as an ID that does not exist.
func UUIDParam(r *http.Request, name string) (uuid.UUID, error) {
	id, err := uuid.Parse(chi.URLParam(r, name))
	if err != nil {
		return uuid.Nil, apperr.NotFound("not found")
	}
	return id, nil
}
