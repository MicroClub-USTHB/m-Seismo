package observability

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/MicroClub-USTHB/m-Seismo/backend/internal/apperr"
	"github.com/MicroClub-USTHB/m-Seismo/backend/internal/httpx"
)

type tokenVerifier struct {
	owner uuid.UUID
}

func (v tokenVerifier) Verify(token string) (uuid.UUID, error) {
	if token != "valid-token" {
		return uuid.Nil, apperr.Unauthorized("invalid token")
	}
	return v.owner, nil
}

func setupTestRouter(t *testing.T) (*fakeDB, *Handler, uuid.UUID, uuid.UUID, chi.Router) {
	t.Helper()
	fdb := newFakeDB()
	svc := NewService(fdb)
	owner := uuid.New()
	appID := uuid.New()
	fdb.apps[appID] = owner

	require := httpx.NewAuthMiddleware(tokenVerifier{owner: owner})
	h := NewHandler(svc, require)

	r := chi.NewRouter()
	r.Route("/api/v1", func(sub chi.Router) {
		h.Mount(sub)
	})

	return fdb, h, owner, appID, r
}

func TestHandler_Auth(t *testing.T) {
	_, _, _, appID, r := setupTestRouter(t)

	url := fmt.Sprintf("/api/v1/apps/%s/observability", appID)

	// No token
	req := httptest.NewRequest(http.MethodGet, url, nil)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("expected 401 Unauthorized, got %d", rec.Code)
	}

	// Invalid token
	req = httptest.NewRequest(http.MethodGet, url, nil)
	req.Header.Set("Authorization", "Bearer invalid-token")
	rec = httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("expected 401 Unauthorized, got %d", rec.Code)
	}

	// Valid token
	req = httptest.NewRequest(http.MethodGet, url, nil)
	req.Header.Set("Authorization", "Bearer valid-token")
	rec = httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Errorf("expected 200 OK, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestHandler_OwnerVerification(t *testing.T) {
	fdb := newFakeDB()
	svc := NewService(fdb)
	owner := uuid.New()
	otherOwner := uuid.New()
	appID := uuid.New()
	fdb.apps[appID] = owner // app belongs to owner, not otherOwner

	require := httpx.NewAuthMiddleware(tokenVerifier{owner: otherOwner})
	h := NewHandler(svc, require)

	r := chi.NewRouter()
	r.Route("/api/v1", func(sub chi.Router) {
		h.Mount(sub)
	})

	url := fmt.Sprintf("/api/v1/apps/%s/observability", appID)
	req := httptest.NewRequest(http.MethodGet, url, nil)
	req.Header.Set("Authorization", "Bearer valid-token")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Errorf("expected 404 Not Found for non-owner, got %d: %s", rec.Code, rec.Body.String())
	}
}

func TestHandler_List(t *testing.T) {
	_, _, _, appID, r := setupTestRouter(t)

	url := fmt.Sprintf("/api/v1/apps/%s/observability", appID)

	// Empty list
	req := httptest.NewRequest(http.MethodGet, url, nil)
	req.Header.Set("Authorization", "Bearer valid-token")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 OK, got %d", rec.Code)
	}
	if strings.TrimSpace(rec.Body.String()) != "[]" {
		t.Errorf("expected empty array '[]', got %q", rec.Body.String())
	}

	// Add provider
	putURL := fmt.Sprintf("/api/v1/apps/%s/observability/grafana", appID)
	putReq := httptest.NewRequest(http.MethodPut, putURL, strings.NewReader(`{"base_url":"https://grafana.example.com"}`))
	putReq.Header.Set("Authorization", "Bearer valid-token")
	putReq.Header.Set("Content-Type", "application/json")
	putRec := httptest.NewRecorder()
	r.ServeHTTP(putRec, putReq)
	if putRec.Code != http.StatusOK && putRec.Code != http.StatusNoContent {
		t.Fatalf("put failed with code %d: %s", putRec.Code, putRec.Body.String())
	}

	// List now contains grafana
	req = httptest.NewRequest(http.MethodGet, url, nil)
	req.Header.Set("Authorization", "Bearer valid-token")
	rec = httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 OK, got %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), `"provider":"grafana"`) {
		t.Errorf("expected grafana in list output: %s", rec.Body.String())
	}
}

func TestHandler_Set_and_Get(t *testing.T) {
	_, _, _, appID, r := setupTestRouter(t)

	// Configure Grafana
	putURL := fmt.Sprintf("/api/v1/apps/%s/observability/grafana", appID)
	putReq := httptest.NewRequest(http.MethodPut, putURL, strings.NewReader(`{"base_url":"https://grafana.example.com","datasource_uid":"prom"}`))
	putReq.Header.Set("Authorization", "Bearer valid-token")
	putReq.Header.Set("Content-Type", "application/json")
	putRec := httptest.NewRecorder()
	r.ServeHTTP(putRec, putReq)

	if putRec.Code != http.StatusOK && putRec.Code != http.StatusNoContent {
		t.Fatalf("expected 200 or 204 on PUT, got %d: %s", putRec.Code, putRec.Body.String())
	}

	// Get configured provider
	getURL := fmt.Sprintf("/api/v1/apps/%s/observability/grafana", appID)
	getReq := httptest.NewRequest(http.MethodGet, getURL, nil)
	getReq.Header.Set("Authorization", "Bearer valid-token")
	getRec := httptest.NewRecorder()
	r.ServeHTTP(getRec, getReq)

	if getRec.Code != http.StatusOK {
		t.Fatalf("expected 200 OK on GET, got %d: %s", getRec.Code, getRec.Body.String())
	}
	if !strings.Contains(getRec.Body.String(), "https://grafana.example.com") {
		t.Errorf("expected base_url in GET response: %s", getRec.Body.String())
	}

	// Get non-configured provider returns 404
	getNotConfigured := fmt.Sprintf("/api/v1/apps/%s/observability/datadog", appID)
	req := httptest.NewRequest(http.MethodGet, getNotConfigured, nil)
	req.Header.Set("Authorization", "Bearer valid-token")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Errorf("expected 404 Not Found for unconfigured provider, got %d", rec.Code)
	}
}

func TestHandler_Set_ValidationErrors(t *testing.T) {
	_, _, _, appID, r := setupTestRouter(t)

	tests := []struct {
		name     string
		provider string
		body     string
	}{
		{
			name:     "malformed json",
			provider: "grafana",
			body:     `{invalid-json`,
		},
		{
			name:     "grafana missing base_url",
			provider: "grafana",
			body:     `{"base_url":""}`,
		},
		{
			name:     "cloudwatch missing log_group",
			provider: "cloudwatch",
			body:     `{"region":"us-east-1"}`,
		},
		{
			name:     "unsupported provider",
			provider: "kibana",
			body:     `{"base_url":"https://kibana.example.com"}`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			url := fmt.Sprintf("/api/v1/apps/%s/observability/%s", appID, tt.provider)
			req := httptest.NewRequest(http.MethodPut, url, strings.NewReader(tt.body))
			req.Header.Set("Authorization", "Bearer valid-token")
			req.Header.Set("Content-Type", "application/json")
			rec := httptest.NewRecorder()
			r.ServeHTTP(rec, req)

			if rec.Code != http.StatusBadRequest && rec.Code != http.StatusUnprocessableEntity {
				t.Fatalf("expected 400 or 422 for invalid request, got %d: %s", rec.Code, rec.Body.String())
			}
		})
	}
}

func TestHandler_Delete_and_DeleteAll(t *testing.T) {
	_, _, _, appID, r := setupTestRouter(t)

	// Setup Grafana and Loki
	for _, prov := range []string{"grafana", "loki"} {
		url := fmt.Sprintf("/api/v1/apps/%s/observability/%s", appID, prov)
		body := `{"base_url":"https://example.com"}`
		req := httptest.NewRequest(http.MethodPut, url, strings.NewReader(body))
		req.Header.Set("Authorization", "Bearer valid-token")
		req.Header.Set("Content-Type", "application/json")
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK && rec.Code != http.StatusNoContent {
			t.Fatalf("setup %s failed: %d", prov, rec.Code)
		}
	}

	// Delete Grafana
	delURL := fmt.Sprintf("/api/v1/apps/%s/observability/grafana", appID)
	delReq := httptest.NewRequest(http.MethodDelete, delURL, nil)
	delReq.Header.Set("Authorization", "Bearer valid-token")
	delRec := httptest.NewRecorder()
	r.ServeHTTP(delRec, delReq)

	if delRec.Code != http.StatusNoContent {
		t.Fatalf("expected 204 No Content on DELETE, got %d", delRec.Code)
	}

	// Verify Grafana is gone
	getReq := httptest.NewRequest(http.MethodGet, delURL, nil)
	getReq.Header.Set("Authorization", "Bearer valid-token")
	getRec := httptest.NewRecorder()
	r.ServeHTTP(getRec, getReq)
	if getRec.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for deleted provider, got %d", getRec.Code)
	}

	// Verify Loki is still present
	lokiURL := fmt.Sprintf("/api/v1/apps/%s/observability/loki", appID)
	lokiReq := httptest.NewRequest(http.MethodGet, lokiURL, nil)
	lokiReq.Header.Set("Authorization", "Bearer valid-token")
	lokiRec := httptest.NewRecorder()
	r.ServeHTTP(lokiRec, lokiReq)
	if lokiRec.Code != http.StatusOK {
		t.Fatalf("expected 200 for remaining loki provider, got %d", lokiRec.Code)
	}

	// DeleteAll
	allURL := fmt.Sprintf("/api/v1/apps/%s/observability", appID)
	allReq := httptest.NewRequest(http.MethodDelete, allURL, nil)
	allReq.Header.Set("Authorization", "Bearer valid-token")
	allRec := httptest.NewRecorder()
	r.ServeHTTP(allRec, allReq)

	if allRec.Code != http.StatusNoContent {
		t.Fatalf("expected 204 No Content on DeleteAll, got %d", allRec.Code)
	}

	// Verify list is now empty
	listReq := httptest.NewRequest(http.MethodGet, allURL, nil)
	listReq.Header.Set("Authorization", "Bearer valid-token")
	listRec := httptest.NewRecorder()
	r.ServeHTTP(listRec, listReq)
	if listRec.Code != http.StatusOK || strings.TrimSpace(listRec.Body.String()) != "[]" {
		t.Fatalf("expected empty list after DeleteAll, got %d: %s", listRec.Code, listRec.Body.String())
	}
}

func TestHandler_InvalidUUID(t *testing.T) {
	_, _, _, _, r := setupTestRouter(t)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/apps/not-a-valid-uuid/observability", nil)
	req.Header.Set("Authorization", "Bearer valid-token")
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)

	if rec.Code != http.StatusNotFound {
		t.Errorf("expected 404 Not Found for invalid UUID, got %d", rec.Code)
	}
}
