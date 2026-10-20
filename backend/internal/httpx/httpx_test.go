package httpx

import (
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/MicroClub-USTHB/m-Seismo/backend/internal/apperr"
)

func TestErrorStatus(t *testing.T) {
	tests := []struct {
		err  error
		want int
	}{
		{apperr.NotFound("x"), http.StatusNotFound},
		{apperr.Conflict("x"), http.StatusConflict},
		{apperr.Invalid("x"), http.StatusUnprocessableEntity},
		{apperr.Unauthorized("x"), http.StatusUnauthorized},
		{fmt.Errorf("wrapped: %w", pgx.ErrNoRows), http.StatusNotFound},
		{&pgconn.PgError{Code: "23505"}, http.StatusConflict},
		{errors.New("boom"), http.StatusInternalServerError},
	}
	for _, tt := range tests {
		rec := httptest.NewRecorder()
		Error(rec, httptest.NewRequest(http.MethodGet, "/", nil), tt.err)
		if rec.Code != tt.want {
			t.Errorf("%v: status %d, want %d", tt.err, rec.Code, tt.want)
		}
	}
}

type fakeVerifier struct{ id uuid.UUID }

func (f fakeVerifier) Verify(token string) (uuid.UUID, error) {
	if token != "good" {
		return uuid.Nil, errors.New("bad token")
	}
	return f.id, nil
}

type meRoutes struct{ require AuthMiddleware }

func (m meRoutes) Mount(r chi.Router) {
	r.With(m.require).Get("/me", func(w http.ResponseWriter, r *http.Request) {
		JSON(w, http.StatusOK, OwnerID(r.Context()).String())
	})
}

func TestAuthMiddleware(t *testing.T) {
	id := uuid.New()
	h := NewRouter(routerParams{Routes: []Routes{meRoutes{require: NewAuthMiddleware(fakeVerifier{id: id})}}})

	tests := []struct {
		name   string
		header string
		want   int
	}{
		{"no header", "", http.StatusUnauthorized},
		{"wrong scheme", "Basic good", http.StatusUnauthorized},
		{"bad token", "Bearer nope", http.StatusUnauthorized},
		{"good token", "Bearer good", http.StatusOK},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/api/v1/me", nil)
			if tt.header != "" {
				req.Header.Set("Authorization", tt.header)
			}
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, req)
			if rec.Code != tt.want {
				t.Fatalf("status %d, want %d (%s)", rec.Code, tt.want, rec.Body)
			}
			if tt.want == http.StatusOK && rec.Body.String() != fmt.Sprintf("%q\n", id) {
				t.Errorf("body %s, want owner id %s", rec.Body, id)
			}
		})
	}
}

func TestRateLimitedSetsRetryAfter(t *testing.T) {
	rec := httptest.NewRecorder()
	Error(rec, httptest.NewRequest(http.MethodGet, "/", nil), apperr.RateLimited("slow down", 2500*time.Millisecond))
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("status %d, want 429", rec.Code)
	}
	if got := rec.Header().Get("Retry-After"); got != "3" {
		t.Errorf("Retry-After = %q, want 3 (rounded up)", got)
	}
}

type waitErr struct{}

func (waitErr) Error() string             { return "tracker says wait" }
func (waitErr) RetryAfter() time.Duration { return 10 * time.Second }

func TestUpstreamRetryAfterIsClassified(t *testing.T) {
	rec := httptest.NewRecorder()
	Error(rec, httptest.NewRequest(http.MethodGet, "/", nil), fmt.Errorf("wrapped: %w", waitErr{}))
	if rec.Code != http.StatusTooManyRequests || rec.Header().Get("Retry-After") != "10" {
		t.Errorf("status %d, Retry-After %q; want 429 and 10", rec.Code, rec.Header().Get("Retry-After"))
	}
}
