package ingest

import (
	"crypto/sha256"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"mime"
	"mime/multipart"
	"net/http"
	"strings"
)

// NOTE: the whole request is capped at 15 MB. The 8 MB per-attachment cap comes with attachments.
const maxBodyBytes = 15 << 20

type api struct {
	store *Store
}

// NewHandler serves the ingestion API from spec/API.md.
func NewHandler(store *Store) http.Handler {
	a := &api{store: store}
	mux := http.NewServeMux()
	mux.HandleFunc("POST /v1/reports", a.submit)
	return mux
}

type accepted struct {
	ID     string `json:"id"`
	Status string `json:"status"`
}

type apiError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func (a *api) submit(w http.ResponseWriter, r *http.Request) {
	// NOTE: auth runs before the body is read, so anonymous floods cost one db lookup each.
	key, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
	if !ok || key == "" {
		writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "invalid project key")
		return
	}
	project, err := a.store.ProjectFor(r.Context(), HashKey(key))
	if errors.Is(err, ErrUnauthorized) {
		writeError(w, http.StatusUnauthorized, "UNAUTHORIZED", "invalid project key")
		return
	}
	if err != nil {
		slog.Error("project lookup", "err", err)
		writeError(w, http.StatusServiceUnavailable, "UNAVAILABLE", "try again later")
		return
	}

	body := http.MaxBytesReader(w, r.Body, maxBodyBytes)
	rep, err := readReport(body, r.Header.Get("Content-Type"))
	var tooBig *http.MaxBytesError
	if errors.As(err, &tooBig) {
		writeError(w, http.StatusRequestEntityTooLarge, "PAYLOAD_TOO_LARGE", "request is over 15 MB")
		return
	}
	if err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_PAYLOAD", err.Error())
		return
	}
	if err := rep.Validate(); err != nil {
		writeError(w, http.StatusBadRequest, "INVALID_PAYLOAD", err.Error())
		return
	}
	if err := a.store.Save(r.Context(), project, rep); err != nil {
		slog.Error("save report", "err", err, "id", rep.ID)
		writeError(w, http.StatusServiceUnavailable, "UNAVAILABLE", "try again later")
		return
	}
	writeJSON(w, http.StatusAccepted, accepted{ID: rep.ID.String(), Status: "accepted"})
}

// readReport reads the one "report" part of a multipart body.
// NOTE: other parts are refused for now. Attachments land with their storage slice.
func readReport(body io.Reader, contentType string) (Report, error) {
	mediaType, params, err := mime.ParseMediaType(contentType)
	if err != nil || mediaType != "multipart/form-data" {
		return Report{}, errors.New("body must be multipart/form-data")
	}
	mr := multipart.NewReader(body, params["boundary"])

	var rep Report
	found := false
	for {
		part, err := mr.NextPart()
		if err == io.EOF {
			break
		}
		if err != nil {
			return Report{}, err
		}
		if part.FormName() != "report" || found {
			return Report{}, errors.New(`body must contain exactly one "report" part`)
		}
		raw, err := io.ReadAll(part)
		if err != nil {
			return Report{}, err
		}
		if err := json.Unmarshal(raw, &rep); err != nil {
			return Report{}, err
		}
		found = true
	}
	if !found {
		return Report{}, errors.New(`body must contain a "report" part`)
	}
	return rep, nil
}

// HashKey is the digest stored in project_keys. Keys are random, so no salt.
func HashKey(key string) []byte {
	sum := sha256.Sum256([]byte(key))
	return sum[:]
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	// NOTE: a failed write means the client hung up, nothing left to do.
	_ = json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, code, msg string) {
	writeJSON(w, status, apiError{Code: code, Message: msg})
}
