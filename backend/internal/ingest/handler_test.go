package ingest

import (
	"bytes"
	"context"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"
)

const testKey = "msk_test_key"

// NOTE: needs TEST_DATABASE_URL, same as store_test.go.
func seedHandler(t *testing.T) http.Handler {
	t.Helper()
	s := testStore(t)
	ctx := context.Background()
	project := uuid.Must(uuid.NewV7())
	if _, err := s.db.Exec(ctx, `INSERT INTO projects (id, name) VALUES ($1, 'demo')`, project); err != nil {
		t.Fatal(err)
	}
	if _, err := s.db.Exec(ctx, `INSERT INTO project_keys (key_hash, project_id) VALUES ($1, $2)`, HashKey(testKey), project); err != nil {
		t.Fatal(err)
	}
	return NewHandler(s)
}

func multipartBody(t *testing.T, report string) (*bytes.Buffer, string) {
	t.Helper()
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	part, err := mw.CreateFormField("report")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := part.Write([]byte(report)); err != nil {
		t.Fatal(err)
	}
	if err := mw.Close(); err != nil {
		t.Fatal(err)
	}
	return &buf, mw.FormDataContentType()
}

func send(h http.Handler, auth string, body *bytes.Buffer, ct string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, "/v1/reports", body)
	req.Header.Set("Content-Type", ct)
	if auth != "" {
		req.Header.Set("Authorization", auth)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestSubmit(t *testing.T) {
	h := seedHandler(t)
	id := uuid.Must(uuid.NewV7()).String()
	good := `{"id":"` + id + `","installation_id":"inst-1","mode":"QA","trigger":"shake","occurred_at":"2026-10-15T19:13:00.123Z"}`
	bearer := "Bearer " + testKey

	t.Run("accepts, and a repeat gets the same answer", func(t *testing.T) {
		for range 2 {
			body, ct := multipartBody(t, good)
			rec := send(h, bearer, body, ct)
			if rec.Code != http.StatusAccepted {
				t.Fatalf("want 202, got %d: %s", rec.Code, rec.Body)
			}
			var got accepted
			if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
				t.Fatal(err)
			}
			if got.ID != id || got.Status != "accepted" {
				t.Fatalf("unexpected body %+v", got)
			}
		}
	})

	cases := map[string]struct {
		auth   string
		report string
		status int
		code   string
	}{
		"no key":    {"", good, http.StatusUnauthorized, "UNAUTHORIZED"},
		"wrong key": {"Bearer nope", good, http.StatusUnauthorized, "UNAUTHORIZED"},
		"bad mode":  {bearer, strings.Replace(good, `"QA"`, `"staging"`, 1), http.StatusBadRequest, "INVALID_PAYLOAD"},
		"not json":  {bearer, `{`, http.StatusBadRequest, "INVALID_PAYLOAD"},
		"too large": {bearer, `{"pad":"` + strings.Repeat("x", 16<<20) + `"}`, http.StatusRequestEntityTooLarge, "PAYLOAD_TOO_LARGE"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			body, ct := multipartBody(t, tc.report)
			rec := send(h, tc.auth, body, ct)
			if rec.Code != tc.status {
				t.Fatalf("want %d, got %d: %s", tc.status, rec.Code, rec.Body)
			}
			var got apiError
			if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
				t.Fatal(err)
			}
			if got.Code != tc.code {
				t.Fatalf("want code %s, got %s", tc.code, got.Code)
			}
		})
	}
}

func TestSubmitRejectsExtraPart(t *testing.T) {
	h := seedHandler(t)
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	part, err := mw.CreateFormField("report")
	if err != nil {
		t.Fatal(err)
	}
	part.Write([]byte(`{}`))
	att, err := mw.CreateFormFile("screenshot", "s.png")
	if err != nil {
		t.Fatal(err)
	}
	att.Write([]byte("png"))
	mw.Close()

	rec := send(h, "Bearer "+testKey, &buf, mw.FormDataContentType())
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("want 400 for an extra part, got %d: %s", rec.Code, rec.Body)
	}
}
