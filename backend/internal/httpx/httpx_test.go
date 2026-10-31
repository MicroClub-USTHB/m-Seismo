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
	"github.com/MicroClub-USTHB/m-Seismo/backend/internal/config"
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
		{&pgconn.PgError{Code: "23P01"}, http.StatusConflict},
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
	h := NewRouter(routerParams{Routes: []Routes{meRoutes{require: NewAuthMiddleware(fakeVerifier{id: id})}}}, &config.Config{})

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

// ownerRoutes owns "/apps/{appID}" the way the app package does: a nested
// r.Route("/apps", func(r) { r.Route("/{appID}", ...) }).
type ownerRoutes struct{}

func (ownerRoutes) Mount(r chi.Router) {
	r.Route("/apps", func(r chi.Router) {
		r.Route("/{appID}", func(r chi.Router) {
			r.Get("/", func(w http.ResponseWriter, r *http.Request) {
				_, _ = w.Write([]byte("app:" + chi.URLParam(r, "appID")))
			})
			r.Get("/keys", func(w http.ResponseWriter, r *http.Request) {
				_, _ = w.Write([]byte("keys:" + chi.URLParam(r, "appID")))
			})
		})
	})
}

// addOnBadRoutes reproduces the #94 regression: a second package, wanting
// to add a route under the same /apps/{appID} another package already
// owns, calls r.Route on the exact same pattern. chi mounts a second
// sub-router there, which shadows ownerRoutes' own routes entirely.
type addOnBadRoutes struct{}

func (addOnBadRoutes) Mount(r chi.Router) {
	r.Route("/apps/{appID}", func(r chi.Router) {
		r.Delete("/reports/{reportID}", func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte("deleted"))
		})
	})
}

// addOnGoodRoutes is the fix: register full paths directly, so chi adds
// methods to the existing node instead of mounting a competing sub-router.
type addOnGoodRoutes struct{}

func (addOnGoodRoutes) Mount(r chi.Router) {
	r.Delete("/apps/{appID}/reports/{reportID}", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("deleted"))
	})
}

func TestMountingASecondRouteGroupAtAnOwnedPrefixShadowsIt(t *testing.T) {
	r := NewRouter(routerParams{Routes: []Routes{ownerRoutes{}, addOnBadRoutes{}}}, &config.Config{})
	req := httptest.NewRequest(http.MethodGet, "/api/v1/apps/a1/keys", nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("this test documents the #94 regression: GET /apps/{id}/keys status = %d, want 404 (shadowed) — if this now passes, chi's behavior changed and the warning in report.Handler.Mount can be dropped", rec.Code)
	}
}

// TestAppRoutesSurviveAnAddOnPackage guards the fix: another package's
// routes under the same /apps/{appID} prefix must not shadow the owner's.
func TestAppRoutesSurviveAnAddOnPackage(t *testing.T) {
	r := NewRouter(routerParams{Routes: []Routes{ownerRoutes{}, addOnGoodRoutes{}}}, &config.Config{})

	for _, tt := range []struct{ method, path, want string }{
		{http.MethodGet, "/api/v1/apps/a1", "app:a1"},
		{http.MethodGet, "/api/v1/apps/a1/keys", "keys:a1"},
		{http.MethodDelete, "/api/v1/apps/a1/reports/r1", "deleted"},
	} {
		req := httptest.NewRequest(tt.method, tt.path, nil)
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK || rec.Body.String() != tt.want {
			t.Errorf("%s %s: status %d, body %q; want 200 %q", tt.method, tt.path, rec.Code, rec.Body.String(), tt.want)
		}
	}
}
