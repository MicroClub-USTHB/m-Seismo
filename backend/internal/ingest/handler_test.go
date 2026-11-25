package ingest

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/textproto"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/MicroClub-USTHB/m-Seismo/backend/internal/apperr"
	"github.com/MicroClub-USTHB/m-Seismo/backend/internal/queue"
	"github.com/MicroClub-USTHB/m-Seismo/backend/internal/ratelimit"
)

const testKey = "msk_test"

var testApp = uuid.MustParse("6f1c0e9a-1d2b-4c3d-8e4f-5a6b7c8d9e01")

type fakeAuth struct{ err error }

func (f fakeAuth) Authenticate(_ context.Context, key string) (uuid.UUID, error) {
	if f.err != nil {
		return uuid.Nil, f.err
	}
	if key != testKey {
		return uuid.Nil, apperr.Unauthorized("invalid key")
	}
	return testApp, nil
}

// fakeLimiter refuses the keys listed in deny.
type fakeLimiter struct {
	deny map[string]bool
	err  error
	keys []string
}

func (f *fakeLimiter) Allow(_ context.Context, key string, _ ratelimit.Limit) (ratelimit.Result, error) {
	f.keys = append(f.keys, key)
	if f.err != nil {
		return ratelimit.Result{}, f.err
	}
	for prefix := range f.deny {
		if strings.HasPrefix(key, prefix) {
			return ratelimit.Result{RetryAfter: 2500 * time.Millisecond}, nil
		}
	}
	return ratelimit.Result{Allowed: true}, nil
}

// memStore keeps reports in memory, once per app and report ID.
type memStore struct {
	byReport map[string]*Saved
	payloads map[uuid.UUID][]byte
	files    map[uuid.UUID][]Attachment
	err      error
}

func newMemStore() *memStore {
	return &memStore{byReport: map[string]*Saved{}, payloads: map[uuid.UUID][]byte{}, files: map[uuid.UUID][]Attachment{}}
}

func (s *memStore) Save(_ context.Context, appID uuid.UUID, r *Report, payload []byte, files []Attachment) (Saved, error) {
	if s.err != nil {
		return Saved{}, s.err
	}
	k := appID.String() + "/" + r.ID
	if prev, ok := s.byReport[k]; ok {
		return Saved{ID: prev.ID, Duplicate: true, Queued: prev.Queued}, nil
	}
	saved := &Saved{ID: uuid.New()}
	s.byReport[k] = saved
	s.payloads[saved.ID] = payload
	s.files[saved.ID] = files
	return *saved, nil
}

func (s *memStore) MarkQueued(_ context.Context, id uuid.UUID) error {
	for _, saved := range s.byReport {
		if saved.ID == id {
			saved.Queued = true
		}
	}
	return nil
}

type fakeQueue struct {
	sent []queue.ReportAccepted
	err  error
}

func (q *fakeQueue) ReportAccepted(_ context.Context, msg queue.ReportAccepted) error {
	if q.err != nil {
		return q.err
	}
	q.sent = append(q.sent, msg)
	return nil
}

type handlerTest struct {
	auth    fakeAuth
	limiter *fakeLimiter
	store   *memStore
	queue   *fakeQueue
}

func newHandlerTest() *handlerTest {
	ht := &handlerTest{limiter: &fakeLimiter{deny: map[string]bool{}}, store: newMemStore(), queue: &fakeQueue{}}
	return ht
}

func (ht *handlerTest) do(t *testing.T, req *http.Request) *httptest.ResponseRecorder {
	t.Helper()
	h := NewHandler(ht.auth, ht.limiter, Limits{
		PerApp:          ratelimit.Limit{PerMinute: 600, Burst: 600},
		PerInstallation: ratelimit.Limit{PerMinute: 10, Burst: 5},
	}, ht.store, ht.queue)
	r := chi.NewRouter()
	r.Route("/api/v1", func(r chi.Router) { h.Mount(r) })
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	return rec
}

func exampleReport(t *testing.T) []byte {
	t.Helper()
	data, err := os.ReadFile(specDir + "/examples/shake-report.json")
	if err != nil {
		t.Fatal(err)
	}
	return data
}

var pngBytes = []byte("\x89PNG\r\n\x1a\n\x00\x00\x00\rIHDR\x00\x00\x00\x01\x00\x00\x00\x01\x08\x06\x00\x00\x00")

type part struct {
	name        string
	contentType string
	data        []byte
}

func multipartRequest(t *testing.T, key string, parts ...part) *http.Request {
	t.Helper()
	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	for _, p := range parts {
		hdr := textproto.MIMEHeader{}
		hdr.Set("Content-Disposition", `form-data; name="`+p.name+`"; filename="`+p.name+`"`)
		hdr.Set("Content-Type", p.contentType)
		w, err := mw.CreatePart(hdr)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write(p.data); err != nil {
			t.Fatal(err)
		}
	}
	if err := mw.Close(); err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodPost, "/api/v1/reports", &body)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	if key != "" {
		req.Header.Set("Authorization", "Bearer "+key)
	}
	return req
}

func reportPart(data []byte) part {
	return part{name: "report", contentType: "application/json", data: data}
}

func errorCode(t *testing.T, rec *httptest.ResponseRecorder) string {
	t.Helper()
	var body struct{ Code, Message string }
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("error body is not JSON: %s", rec.Body)
	}
	if body.Message == "" {
		t.Errorf("error %q has no message", body.Code)
	}
	return body.Code
}

func TestAccepted(t *testing.T) {
	ht := newHandlerTest()
	data := exampleReport(t)

	rec := ht.do(t, multipartRequest(t, testKey, reportPart(data), part{name: "screenshot", contentType: "image/png", data: pngBytes}))
	if rec.Code != http.StatusAccepted {
		t.Fatalf("status %d: %s", rec.Code, rec.Body)
	}
	var body map[string]string
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body["id"] != "0192a5f0-7c3e-7d4a-9b1e-3f6a2c8d4e51" || body["status"] != "accepted" {
		t.Errorf("body = %s", rec.Body)
	}

	if len(ht.queue.sent) != 1 || ht.queue.sent[0].AppID != testApp {
		t.Fatalf("queued %+v, want one message for the app", ht.queue.sent)
	}
	id := ht.queue.sent[0].ID
	if files := ht.store.files[id]; len(files) != 1 || files[0].ContentType != "image/png" {
		t.Errorf("stored files = %+v", files)
	}
	if !strings.Contains(strings.Join(ht.limiter.keys, ","), "installation:"+testApp.String()+":5b8f2c1e") {
		t.Errorf("installation limit not checked: %v", ht.limiter.keys)
	}
}

func TestDuplicateIsAcceptedOnce(t *testing.T) {
	ht := newHandlerTest()
	data := exampleReport(t)

	for i := 0; i < 3; i++ {
		rec := ht.do(t, multipartRequest(t, testKey, reportPart(data)))
		if rec.Code != http.StatusAccepted {
			t.Fatalf("attempt %d: status %d: %s", i+1, rec.Code, rec.Body)
		}
	}
	if len(ht.store.byReport) != 1 || len(ht.queue.sent) != 1 {
		t.Errorf("stored %d and queued %d, want 1 and 1", len(ht.store.byReport), len(ht.queue.sent))
	}
}

func TestQueueFailureIsRetried(t *testing.T) {
	ht := newHandlerTest()
	data := exampleReport(t)

	ht.queue.err = errors.New("nats down")
	rec := ht.do(t, multipartRequest(t, testKey, reportPart(data)))
	if rec.Code != http.StatusServiceUnavailable || rec.Header().Get("Retry-After") == "" {
		t.Fatalf("status %d, Retry-After %q", rec.Code, rec.Header().Get("Retry-After"))
	}

	// The SDK retries once NATS is back: the stored report is queued, not
	// stored again.
	ht.queue.err = nil
	rec = ht.do(t, multipartRequest(t, testKey, reportPart(data)))
	if rec.Code != http.StatusAccepted {
		t.Fatalf("retry: status %d: %s", rec.Code, rec.Body)
	}
	if len(ht.store.byReport) != 1 || len(ht.queue.sent) != 1 {
		t.Errorf("stored %d and queued %d, want 1 and 1", len(ht.store.byReport), len(ht.queue.sent))
	}
}

func TestRejected(t *testing.T) {
	valid := exampleReport(t)
	crashNoDetails, _ := os.ReadFile(specDir + "/examples/invalid-crash-without-details.json")
	v2 := bytes.Replace(valid, []byte(`"schema_version": 1`), []byte(`"schema_version": 2`), 1)

	tests := []struct {
		name   string
		setup  func(ht *handlerTest)
		req    func(t *testing.T) *http.Request
		status int
		code   string
	}{
		{
			name:   "no key",
			req:    func(t *testing.T) *http.Request { return multipartRequest(t, "", reportPart(valid)) },
			status: http.StatusUnauthorized, code: "unauthorized",
		},
		{
			name:   "unknown key",
			req:    func(t *testing.T) *http.Request { return multipartRequest(t, "msk_nope", reportPart(valid)) },
			status: http.StatusUnauthorized, code: "unauthorized",
		},
		{
			name:   "key check failing",
			setup:  func(ht *handlerTest) { ht.auth.err = errors.New("db down") },
			req:    func(t *testing.T) *http.Request { return multipartRequest(t, testKey, reportPart(valid)) },
			status: http.StatusServiceUnavailable, code: "unavailable",
		},
		{
			name:   "app over its limit",
			setup:  func(ht *handlerTest) { ht.limiter.deny["app:"] = true },
			req:    func(t *testing.T) *http.Request { return multipartRequest(t, testKey, reportPart(valid)) },
			status: http.StatusTooManyRequests, code: "rate_limited",
		},
		{
			name:   "installation over its limit",
			setup:  func(ht *handlerTest) { ht.limiter.deny["installation:"] = true },
			req:    func(t *testing.T) *http.Request { return multipartRequest(t, testKey, reportPart(valid)) },
			status: http.StatusTooManyRequests, code: "rate_limited",
		},
		{
			name: "not multipart",
			req: func(t *testing.T) *http.Request {
				req := httptest.NewRequest(http.MethodPost, "/api/v1/reports", bytes.NewReader(valid))
				req.Header.Set("Content-Type", "application/json")
				req.Header.Set("Authorization", "Bearer "+testKey)
				return req
			},
			status: http.StatusBadRequest, code: "invalid_request",
		},
		{
			name: "no report part",
			req: func(t *testing.T) *http.Request {
				return multipartRequest(t, testKey, part{name: "screenshot", contentType: "image/png", data: pngBytes})
			},
			status: http.StatusBadRequest, code: "invalid_request",
		},
		{
			name:   "report is not JSON",
			req:    func(t *testing.T) *http.Request { return multipartRequest(t, testKey, reportPart([]byte("{nope"))) },
			status: http.StatusBadRequest, code: "invalid_report",
		},
		{
			name:   "report fails the schema",
			req:    func(t *testing.T) *http.Request { return multipartRequest(t, testKey, reportPart(crashNoDetails)) },
			status: http.StatusBadRequest, code: "invalid_report",
		},
		{
			name:   "unsupported schema version",
			req:    func(t *testing.T) *http.Request { return multipartRequest(t, testKey, reportPart(v2)) },
			status: http.StatusBadRequest, code: "unsupported_version",
		},
		{
			name: "screenshot is not an image",
			req: func(t *testing.T) *http.Request {
				return multipartRequest(t, testKey, reportPart(valid), part{name: "screenshot", contentType: "image/png", data: []byte("<html>")})
			},
			status: http.StatusUnsupportedMediaType, code: "unsupported_media_type",
		},
		{
			name: "report part too large",
			req: func(t *testing.T) *http.Request {
				return multipartRequest(t, testKey, reportPart(bytes.Repeat([]byte(" "), maxReportBytes+1)))
			},
			status: http.StatusRequestEntityTooLarge, code: "too_large",
		},
		{
			name: "screenshot too large",
			req: func(t *testing.T) *http.Request {
				big := append(append([]byte{}, pngBytes...), make([]byte, maxScreenshotBytes)...)
				return multipartRequest(t, testKey, reportPart(valid), part{name: "screenshot", contentType: "image/png", data: big})
			},
			status: http.StatusRequestEntityTooLarge, code: "too_large",
		},
		{
			name: "request too large",
			req: func(t *testing.T) *http.Request {
				return multipartRequest(t, testKey, reportPart(valid), part{name: "extra", contentType: "application/octet-stream", data: make([]byte, maxRequestBytes)})
			},
			status: http.StatusRequestEntityTooLarge, code: "too_large",
		},
		{
			name:   "store failing",
			setup:  func(ht *handlerTest) { ht.store.err = errors.New("db down") },
			req:    func(t *testing.T) *http.Request { return multipartRequest(t, testKey, reportPart(valid)) },
			status: http.StatusServiceUnavailable, code: "unavailable",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ht := newHandlerTest()
			if tt.setup != nil {
				tt.setup(ht)
			}
			rec := ht.do(t, tt.req(t))
			if rec.Code != tt.status {
				t.Fatalf("status %d, want %d: %s", rec.Code, tt.status, rec.Body)
			}
			if got := errorCode(t, rec); got != tt.code {
				t.Errorf("code %q, want %q", got, tt.code)
			}
			if tt.status == http.StatusTooManyRequests && rec.Header().Get("Retry-After") != "3" {
				t.Errorf("Retry-After = %q, want 3", rec.Header().Get("Retry-After"))
			}
			if len(ht.queue.sent) != 0 {
				t.Error("a rejected report was queued")
			}
		})
	}
}

func TestRateLimiterDownLetsReportsThrough(t *testing.T) {
	ht := newHandlerTest()
	ht.limiter.err = errors.New("redis down")
	rec := ht.do(t, multipartRequest(t, testKey, reportPart(exampleReport(t))))
	if rec.Code != http.StatusAccepted {
		t.Fatalf("status %d: %s", rec.Code, rec.Body)
	}
}

func TestBodiesDroppedUnlessCaptured(t *testing.T) {
	ht := newHandlerTest()
	var m map[string]any
	if err := json.Unmarshal(exampleReport(t), &m); err != nil {
		t.Fatal(err)
	}
	m["network_requests"] = []any{map[string]any{
		"timestamp": "2026-10-18T14:03:09.866Z", "url": "https://api.example.com/pay", "method": "POST",
		"duration_ms": 120, "request_body": `{"card":"4111111111111111"}`,
	}}
	data, _ := json.Marshal(m)

	rec := ht.do(t, multipartRequest(t, testKey, reportPart(data)))
	if rec.Code != http.StatusAccepted {
		t.Fatalf("status %d: %s", rec.Code, rec.Body)
	}
	for _, payload := range ht.store.payloads {
		if strings.Contains(string(payload), "4111") {
			t.Error("request body stored although bodies_captured is false")
		}
	}
}
