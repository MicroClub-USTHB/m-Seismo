package httpapi

import (
	"crypto/sha256"
	"crypto/subtle"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5/middleware"

	"github.com/MicroClub-USTHB/m-Seismo/backend/internal/audit"
)

// requireAdminToken is the stand-in for real authentication: every /v1
// request must carry "Authorization: Bearer <ADMIN_API_TOKEN>".
func requireAdminToken(token string) func(http.Handler) http.Handler {
	want := sha256.Sum256([]byte(token))
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			got, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
			sum := sha256.Sum256([]byte(got))
			if !ok || subtle.ConstantTimeCompare(sum[:], want[:]) != 1 {
				w.Header().Set("WWW-Authenticate", `Bearer realm="m-seismo"`)
				writeProblem(w, r, Problem{Status: http.StatusUnauthorized})
				return
			}
			next.ServeHTTP(w, r.WithContext(audit.WithActor(r.Context(), "admin-token")))
		})
	}
}

// requestLogger writes one structured log line per request.
func requestLogger(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ww := middleware.NewWrapResponseWriter(w, r.ProtoMajor)
		start := time.Now()
		next.ServeHTTP(ww, r)
		slog.InfoContext(r.Context(), "http request",
			"method", r.Method,
			"path", r.URL.Path,
			"status", ww.Status(),
			"bytes", ww.BytesWritten(),
			"duration_ms", time.Since(start).Milliseconds(),
			"request_id", middleware.GetReqID(r.Context()),
		)
	})
}

// recoverer turns panics into a logged 500 problem.
func recoverer(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if rec := recover(); rec != nil {
				if rec == http.ErrAbortHandler {
					panic(rec)
				}
				slog.ErrorContext(r.Context(), "panic", "panic", rec, "request_id", middleware.GetReqID(r.Context()))
				writeProblem(w, r, Problem{Status: http.StatusInternalServerError})
			}
		}()
		next.ServeHTTP(w, r)
	})
}
