package report

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/MicroClub-USTHB/m-Seismo/backend/internal/apperr"
	"github.com/MicroClub-USTHB/m-Seismo/backend/internal/db"
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

func TestHandlerBundle(t *testing.T) {
	owner := uuid.New()
	appID := uuid.New()
	reportID := uuid.New()
	internalID := uuid.New()
	payload := []byte(`{"event":"crash"}`)
	screenshot := []byte("fake-png-binary")

	q := &fakeQuerier{
		app: db.App{ID: appID, OwnerID: owner},
		reports: map[uuid.UUID]db.Report{
			reportID: {ID: internalID, AppID: appID, ReportID: reportID, Payload: payload},
		},
		attachments: map[uuid.UUID][]db.ListReportAttachmentsWithDataRow{
			internalID: {
				{Name: "screenshot.png", ContentType: "image/png", Data: screenshot},
			},
		},
	}

	svc := NewService(q, nil)
	require := httpx.NewAuthMiddleware(tokenVerifier{owner: owner})
	h := NewHandler(svc, require)

	r := chi.NewRouter()
	r.Route("/api/v1", func(sub chi.Router) {
		h.Mount(sub)
	})

	t.Run("success returns 200 with zip headers and valid content", func(t *testing.T) {
		url := fmt.Sprintf("/api/v1/apps/%s/reports/%s/bundle", appID, reportID)
		req := httptest.NewRequest(http.MethodGet, url, nil)
		req.Header.Set("Authorization", "Bearer valid-token")

		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200 OK, got %d: %s", rec.Code, rec.Body.String())
		}
		if ct := rec.Header().Get("Content-Type"); ct != "application/zip" {
			t.Errorf("expected Content-Type application/zip, got %q", ct)
		}
		wantDisp := fmt.Sprintf(`attachment; filename="report-%s.zip"`, reportID)
		if cd := rec.Header().Get("Content-Disposition"); cd != wantDisp {
			t.Errorf("expected Content-Disposition %q, got %q", wantDisp, cd)
		}

		// Verify zip content
		zr, err := zip.NewReader(bytes.NewReader(rec.Body.Bytes()), int64(rec.Body.Len()))
		if err != nil {
			t.Fatalf("invalid zip: %v", err)
		}
		if len(zr.File) != 2 {
			t.Fatalf("expected 2 files in zip, got %d", len(zr.File))
		}
	})

	t.Run("unauthenticated returns 401", func(t *testing.T) {
		url := fmt.Sprintf("/api/v1/apps/%s/reports/%s/bundle", appID, reportID)
		req := httptest.NewRequest(http.MethodGet, url, nil)
		// No auth header

		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)

		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("expected 401 Unauthorized, got %d", rec.Code)
		}
	})

	t.Run("missing report returns 404", func(t *testing.T) {
		url := fmt.Sprintf("/api/v1/apps/%s/reports/%s/bundle", appID, uuid.New())
		req := httptest.NewRequest(http.MethodGet, url, nil)
		req.Header.Set("Authorization", "Bearer valid-token")

		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)

		if rec.Code != http.StatusNotFound {
			t.Fatalf("expected 404 Not Found, got %d", rec.Code)
		}
	})
}

func TestHandlerListReports(t *testing.T) {
	owner := uuid.New()
	appID := uuid.New()
	repID1 := uuid.New()
	repID2 := uuid.New()
	intID1 := uuid.New()
	intID2 := uuid.New()
	now := time.Now().UTC().Truncate(time.Millisecond)
	proc1 := now.Add(-1 * time.Minute)

	q := &fakeQuerier{
		app: db.App{ID: appID, OwnerID: owner},
		reports: map[uuid.UUID]db.Report{
			repID1: {ID: intID1, AppID: appID, ReportID: repID1, OccurredAt: now.Add(-1 * time.Minute), ProcessedAt: &proc1, Trigger: "shake"},
			repID2: {ID: intID2, AppID: appID, ReportID: repID2, OccurredAt: now.Add(-2 * time.Minute), ProcessedAt: nil, Trigger: "manual"},
		},
		deliveries: map[uuid.UUID][]db.ReportDelivery{
			intID1: {
				{ReportID: intID1, Url: "https://github.com/org/repo/issues/10"},
			},
		},
	}

	svc := NewService(q, nil)
	require := httpx.NewAuthMiddleware(tokenVerifier{owner: owner})
	h := NewHandler(svc, require)

	r := chi.NewRouter()
	r.Route("/api/v1", func(sub chi.Router) {
		h.Mount(sub)
	})

	t.Run("success returns 200 with reports array", func(t *testing.T) {
		url := fmt.Sprintf("/api/v1/apps/%s/reports", appID)
		req := httptest.NewRequest(http.MethodGet, url, nil)
		req.Header.Set("Authorization", "Bearer valid-token")

		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200 OK, got %d: %s", rec.Code, rec.Body.String())
		}

		var res ListReportsResult
		if err := json.Unmarshal(rec.Body.Bytes(), &res); err != nil {
			t.Fatalf("failed to decode response: %v", err)
		}
		if len(res.Reports) != 2 {
			t.Fatalf("expected 2 reports, got %d", len(res.Reports))
		}
		if res.Reports[0].ReportID != repID1 || res.Reports[0].Status != "delivered" {
			t.Errorf("unexpected report 0: %+v", res.Reports[0])
		}
		if len(res.Reports[0].IssueURLs) != 1 || res.Reports[0].IssueURLs[0] != "https://github.com/org/repo/issues/10" {
			t.Errorf("unexpected issue URLs: %+v", res.Reports[0].IssueURLs)
		}
	})

	t.Run("filter by status delivered", func(t *testing.T) {
		url := fmt.Sprintf("/api/v1/apps/%s/reports?status=delivered", appID)
		req := httptest.NewRequest(http.MethodGet, url, nil)
		req.Header.Set("Authorization", "Bearer valid-token")

		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200 OK, got %d: %s", rec.Code, rec.Body.String())
		}

		var res ListReportsResult
		if err := json.Unmarshal(rec.Body.Bytes(), &res); err != nil {
			t.Fatalf("failed to decode response: %v", err)
		}
		if len(res.Reports) != 1 || res.Reports[0].ReportID != repID1 {
			t.Fatalf("expected 1 delivered report, got %+v", res.Reports)
		}
	})

	t.Run("unauthenticated returns 401", func(t *testing.T) {
		url := fmt.Sprintf("/api/v1/apps/%s/reports", appID)
		req := httptest.NewRequest(http.MethodGet, url, nil)

		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)

		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("expected 401 Unauthorized, got %d", rec.Code)
		}
	})
}

func TestHandlerGetReport(t *testing.T) {
	owner := uuid.New()
	appID := uuid.New()
	reportID := uuid.New()
	internalID := uuid.New()
	now := time.Now().UTC().Truncate(time.Millisecond)
	proc := now.Add(-5 * time.Minute)
	payload := []byte(`{"device":{"model":"Pixel8"},"user_input":{"title":"Crash on checkout"}}`)
	issueURL := "https://github.com/org/repo/issues/42"
	repoID := uuid.New()

	q := &fakeQuerier{
		app: db.App{ID: appID, OwnerID: owner},
		reports: map[uuid.UUID]db.Report{
			reportID: {
				ID:             internalID,
				AppID:          appID,
				ReportID:       reportID,
				InstallationID: uuid.New(),
				SessionID:      "sess-123",
				Trigger:        "shake",
				OccurredAt:     now.Add(-10 * time.Minute),
				ReceivedAt:     now.Add(-9 * time.Minute),
				ProcessedAt:    &proc,
				Payload:        payload,
			},
		},
		deliveries: map[uuid.UUID][]db.ReportDelivery{
			internalID: {
				{
					ReportID:            internalID,
					ProjectRepositoryID: repoID,
					Url:                 issueURL,
					CreatedAt:           proc,
				},
			},
		},
	}

	svc := NewService(q, nil)
	require := httpx.NewAuthMiddleware(tokenVerifier{owner: owner})
	h := NewHandler(svc, require)

	r := chi.NewRouter()
	r.Route("/api/v1", func(sub chi.Router) {
		h.Mount(sub)
	})

	t.Run("success returns 200 with full report", func(t *testing.T) {
		url := fmt.Sprintf("/api/v1/apps/%s/reports/%s", appID, reportID)
		req := httptest.NewRequest(http.MethodGet, url, nil)
		req.Header.Set("Authorization", "Bearer valid-token")

		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)

		if rec.Code != http.StatusOK {
			t.Fatalf("expected 200 OK, got %d: %s", rec.Code, rec.Body.String())
		}

		var detail ReportDetail
		if err := json.Unmarshal(rec.Body.Bytes(), &detail); err != nil {
			t.Fatalf("failed to decode response: %v", err)
		}
		if detail.ReportID != reportID {
			t.Errorf("ReportID = %v, want %v", detail.ReportID, reportID)
		}
		if detail.Trigger != "shake" {
			t.Errorf("Trigger = %q, want %q", detail.Trigger, "shake")
		}
		if detail.Status != "delivered" {
			t.Errorf("Status = %q, want %q", detail.Status, "delivered")
		}
		if len(detail.Deliveries) != 1 || detail.Deliveries[0].URL != issueURL {
			t.Errorf("unexpected deliveries: %+v", detail.Deliveries)
		}
	})

	t.Run("missing report returns 404", func(t *testing.T) {
		url := fmt.Sprintf("/api/v1/apps/%s/reports/%s", appID, uuid.New())
		req := httptest.NewRequest(http.MethodGet, url, nil)
		req.Header.Set("Authorization", "Bearer valid-token")

		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)

		if rec.Code != http.StatusNotFound {
			t.Fatalf("expected 404 Not Found, got %d", rec.Code)
		}
	})

	t.Run("unauthenticated returns 401", func(t *testing.T) {
		url := fmt.Sprintf("/api/v1/apps/%s/reports/%s", appID, reportID)
		req := httptest.NewRequest(http.MethodGet, url, nil)

		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)

		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("expected 401 Unauthorized, got %d", rec.Code)
		}
	})
}

func TestHandlerRetryReport(t *testing.T) {
	owner := uuid.New()
	appID := uuid.New()
	reportID := uuid.New()
	internalID := uuid.New()

	q := &fakeQuerier{
		app: db.App{ID: appID, OwnerID: owner},
		reports: map[uuid.UUID]db.Report{
			reportID: {
				ID:       internalID,
				AppID:    appID,
				ReportID: reportID,
			},
		},
	}
	pub := &fakePublisher{}
	svc := NewService(q, pub)
	require := httpx.NewAuthMiddleware(tokenVerifier{owner: owner})
	h := NewHandler(svc, require)

	r := chi.NewRouter()
	r.Route("/api/v1", func(sub chi.Router) {
		h.Mount(sub)
	})

	t.Run("success returns 202 Accepted", func(t *testing.T) {
		pub.published = nil
		url := fmt.Sprintf("/api/v1/apps/%s/reports/%s/retry", appID, reportID)
		req := httptest.NewRequest(http.MethodPost, url, nil)
		req.Header.Set("Authorization", "Bearer valid-token")

		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)

		if rec.Code != http.StatusAccepted {
			t.Fatalf("expected 202 Accepted, got %d: %s", rec.Code, rec.Body.String())
		}
		if len(pub.published) != 1 {
			t.Fatalf("expected 1 message published, got %d", len(pub.published))
		}
		if pub.published[0].ID != internalID || pub.published[0].AppID != appID {
			t.Errorf("unexpected published message: %+v", pub.published[0])
		}
	})

	t.Run("missing report returns 404", func(t *testing.T) {
		pub.published = nil
		url := fmt.Sprintf("/api/v1/apps/%s/reports/%s/retry", appID, uuid.New())
		req := httptest.NewRequest(http.MethodPost, url, nil)
		req.Header.Set("Authorization", "Bearer valid-token")

		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)

		if rec.Code != http.StatusNotFound {
			t.Fatalf("expected 404 Not Found, got %d", rec.Code)
		}
		if len(pub.published) != 0 {
			t.Errorf("expected 0 published messages, got %d", len(pub.published))
		}
	})

	t.Run("unauthenticated returns 401", func(t *testing.T) {
		url := fmt.Sprintf("/api/v1/apps/%s/reports/%s/retry", appID, reportID)
		req := httptest.NewRequest(http.MethodPost, url, nil)

		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)

		if rec.Code != http.StatusUnauthorized {
			t.Fatalf("expected 401 Unauthorized, got %d", rec.Code)
		}
	})
}
