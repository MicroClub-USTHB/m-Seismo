package report

import (
	"archive/zip"
	"bytes"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

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
