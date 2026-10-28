//go:build integration

package main

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/nats-io/nats.go/jetstream"
	"go.uber.org/fx"
	"go.uber.org/fx/fxtest"

	"github.com/MicroClub-USTHB/m-Seismo/backend/internal/queue"
)

// TestCrashProviderHTTPIntegration runs the production dependency graph against
// isolated Postgres, Redis and NATS. Only the external Sentry API is simulated.
// It deliberately uses owner/SDK HTTP APIs rather than calling domain services.
func TestCrashProviderHTTPIntegration(t *testing.T) {
	for setting, target := range map[string]string{
		"SEISMO_TEST_DATABASE_URL": "SEISMO_DATABASE_URL",
		"SEISMO_TEST_REDIS_URL":    "SEISMO_REDIS_URL",
		"SEISMO_TEST_NATS_URL":     "SEISMO_NATS_URL",
	} {
		value := os.Getenv(setting)
		if value == "" {
			t.Skip(setting + " must point to an isolated test service")
		}
		t.Setenv(target, value)
	}
	t.Setenv("SEISMO_ENV", "development")
	t.Setenv("SEISMO_AUTH_JWT_SECRET", "http-integration-test-secret")
	t.Setenv("SEISMO_GITHUB_APP_ID", "0")
	t.Setenv("SEISMO_SERVER_HOST", "127.0.0.1")
	t.Setenv("SEISMO_SERVER_PORT", "0")
	t.Setenv("SEISMO_CRASH_PROVIDERS_ENCRYPTION_KEY", base64.StdEncoding.EncodeToString(make([]byte, 32)))
	// This suite intentionally exceeds the production installation burst while
	// testing validation/retries. Redis limits are exercised by their own suite.
	t.Setenv("SEISMO_INGEST_INSTALLATION_BURST", "100")
	t.Setenv("SEISMO_INGEST_APP_BURST", "100")

	eventID := strings.Repeat("a", 32)
	var upstreamStatus atomic.Int64
	upstreamStatus.Store(http.StatusOK)
	var eventCalls atomic.Int64
	sentry := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer test-sentry-secret" {
			w.WriteHeader(http.StatusForbidden)
			_, _ = io.WriteString(w, `{"detail":"private upstream error"}`)
			return
		}
		if r.URL.Path == "/api/0/projects/club/web/" {
			_, _ = io.WriteString(w, `{"id":"42"}`)
			return
		}
		eventCalls.Add(1)
		if status := int(upstreamStatus.Load()); status != http.StatusOK {
			w.Header().Set("Retry-After", "11")
			w.WriteHeader(status)
			_, _ = io.WriteString(w, `{"detail":"private upstream error"}`)
			return
		}
		if r.URL.Path != "/api/0/projects/club/web/events/"+eventID+"/" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		_, _ = io.WriteString(w, `{"eventID":"`+eventID+`","groupID":"42","title":"Frontend crash","user":{"email":"private@example.com"},"entries":[{"type":"exception","data":{"values":[{"stacktrace":{"frames":[{"function":"render","filename":"app.js","lineNo":12}]}}]}}]}`)
	}))
	defer sentry.Close()
	t.Setenv("SEISMO_CRASH_PROVIDERS_SENTRY_ORIGINS", sentry.URL)
	t.Setenv("SEISMO_WORKER_ENABLED", "false")
	// Trust only this test server in addition to the regular roots. Production
	// TLS verification remains enabled; no insecure transport is added to code.
	transport := http.DefaultTransport.(*http.Transport).Clone()
	roots, err := x509.SystemCertPool()
	if err != nil {
		t.Fatal(err)
	}
	roots.AddCert(sentry.Certificate())
	transport.TLSClientConfig = &tls.Config{RootCAs: roots, MinVersion: tls.VersionTLS12}
	previousTransport := http.DefaultTransport
	http.DefaultTransport = transport
	defer func() { transport.CloseIdleConnections(); http.DefaultTransport = previousTransport }()

	var handler http.Handler
	var pool *pgxpool.Pool
	var js jetstream.JetStream
	application := fxtest.New(t, Options(), fx.Populate(&handler, &pool, &js))
	application.RequireStart()
	defer application.RequireStop()
	server := httptest.NewServer(handler)
	defer server.Close()
	client := &http.Client{Timeout: 10 * time.Second}

	request := func(method, path, token string, payload any, want int) map[string]any {
		t.Helper()
		var body io.Reader
		if payload != nil {
			data, err := json.Marshal(payload)
			if err != nil {
				t.Fatal(err)
			}
			body = bytes.NewReader(data)
		}
		req, err := http.NewRequest(method, server.URL+"/api/v1"+path, body)
		if err != nil {
			t.Fatal(err)
		}
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		req.Header.Set("Content-Type", "application/json")
		response, err := client.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = response.Body.Close() }()
		data, err := io.ReadAll(response.Body)
		if err != nil {
			t.Fatal(err)
		}
		if response.StatusCode != want {
			t.Fatalf("%s %s: got %d, want %d: %s", method, path, response.StatusCode, want, data)
		}
		if want == http.StatusTooManyRequests && response.Header.Get("Retry-After") != "11" {
			t.Fatal("Sentry Retry-After was not preserved by the HTTP endpoint")
		}
		var out map[string]any
		if len(data) > 0 {
			if err := json.Unmarshal(data, &out); err != nil {
				t.Fatal(err)
			}
		}
		if strings.Contains(string(data), "test-sentry-secret") || strings.Contains(string(data), "private@example.com") || strings.Contains(string(data), "private upstream error") {
			t.Fatalf("private upstream fields escaped in %s %s", method, path)
		}
		return out
	}
	newOwner := func() (string, string) {
		t.Helper()
		out := request("POST", "/auth/register", "", map[string]any{"email": uuid.NewString() + "@test.invalid", "password": "integration-password", "name": "Integration"}, 201)
		ownerID := out["owner"].(map[string]any)["id"].(string)
		t.Cleanup(func() {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			// fxtest lifecycle defers run before t.Cleanup. Use a separate
			// short-lived pool so fixtures are removed after shutdown too.
			cleanup, err := pgxpool.New(ctx, os.Getenv("SEISMO_TEST_DATABASE_URL"))
			if err != nil {
				t.Error(err)
				return
			}
			defer cleanup.Close()
			if _, err := cleanup.Exec(ctx, "DELETE FROM owners WHERE id=$1", ownerID); err != nil {
				t.Error(err)
			}
		})
		return ownerID, out["token"].(map[string]any)["access_token"].(string)
	}
	_, token := newOwner()
	_, stranger := newOwner()
	web := request("POST", "/apps", token, map[string]any{"name": "Website", "platform": "web", "bundle_id": "test.invalid"}, 201)["id"].(string)
	mobile := request("POST", "/apps", token, map[string]any{"name": "Mobile", "platform": "ios", "bundle_id": "com.test.app"}, 201)["id"].(string)
	webPath := "/apps/" + web
	mobilePath := "/apps/" + mobile
	key := request("POST", webPath+"/keys", token, nil, 201)
	sdkKey := key["secret"].(string)
	mobileKey := request("POST", mobilePath+"/keys", token, nil, 201)["secret"].(string)
	settings := map[string]any{"provider": "sentry", "sentry": map[string]any{"base_url": sentry.URL, "organization": "club", "project": "web", "auth_token": "test-sentry-secret"}}
	request("PUT", webPath+"/crash-provider", token, settings, 200)
	request("PUT", mobilePath+"/crash-provider", token, map[string]any{"provider": "crashlytics", "crashlytics": map[string]any{"project_id": "firebase", "app_id": "1:123:ios:abc"}}, 200)
	if request("GET", webPath+"/crash-provider", token, nil, 200)["provider"] != "sentry" {
		t.Fatal("app providers were mixed")
	}
	request("GET", webPath+"/crash-provider", sdkKey, nil, 401)
	for _, method := range []string{"GET", "PUT", "DELETE"} {
		request(method, webPath+"/crash-provider", stranger, settings, 404)
	}
	var encrypted []byte
	if err := pool.QueryRow(context.Background(), "SELECT credentials FROM app_crash_providers WHERE app_id=$1", web).Scan(&encrypted); err != nil {
		t.Fatal(err)
	}
	if len(encrypted) == 0 || bytes.Contains(encrypted, []byte("test-sentry-secret")) {
		t.Fatal("Sentry token not encrypted in Postgres")
	}

	payload, err := os.ReadFile("../spec/examples/sentry-web-report.json")
	if err != nil {
		t.Fatal(err)
	}
	var report map[string]any
	if err := json.Unmarshal(payload, &report); err != nil {
		t.Fatal(err)
	}
	reportID := uuid.NewString()
	report["id"] = strings.ToUpper(reportID)
	report["installation_id"] = uuid.NewString()
	report["sentry"] = map[string]any{"event_id": strings.ToUpper(eventID), "auth_token": "discard-this-field"}
	shot, err := base64.StdEncoding.DecodeString("iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAQAAAC1HAwCAAAAC0lEQVR42mP8/x8AAwMCAO+jRZkAAAAASUVORK5CYII=")
	if err != nil {
		t.Fatal(err)
	}
	upload := func(key string, report map[string]any, want int) {
		t.Helper()
		var data bytes.Buffer
		writer := multipart.NewWriter(&data)
		part, err := writer.CreateFormField("report")
		if err != nil {
			t.Fatal(err)
		}
		if err := json.NewEncoder(part).Encode(report); err != nil {
			t.Fatal(err)
		}
		attachment, err := writer.CreateFormFile("screenshot", "screen.png")
		if err != nil {
			t.Fatal(err)
		}
		if _, err := attachment.Write(shot); err != nil {
			t.Fatal(err)
		}
		if err := writer.Close(); err != nil {
			t.Fatal(err)
		}
		req, err := http.NewRequest("POST", server.URL+"/api/v1/reports", &data)
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Authorization", "Bearer "+key)
		req.Header.Set("Content-Type", writer.FormDataContentType())
		response, err := client.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = response.Body.Close() }()
		body, err := io.ReadAll(response.Body)
		if err != nil {
			t.Fatal(err)
		}
		if response.StatusCode != want {
			t.Fatalf("upload got %d, want %d: %s", response.StatusCode, want, body)
		}
	}
	// Sentry can be completely unavailable while the ingestion API succeeds.
	upstreamStatus.Store(http.StatusServiceUnavailable)
	upload(sdkKey, report, 202)
	upload(sdkKey, report, 202)
	if eventCalls.Load() != 0 {
		t.Fatal("ingestion contacted Sentry synchronously")
	}
	var storedID uuid.UUID
	var storedPayload []byte
	var queuedAt *time.Time
	if err := pool.QueryRow(context.Background(), "SELECT id,payload,queued_at FROM reports WHERE app_id=$1 AND report_id=$2", web, reportID).Scan(&storedID, &storedPayload, &queuedAt); err != nil {
		t.Fatal(err)
	}
	if queuedAt == nil || bytes.Contains(storedPayload, []byte("discard-this-field")) || !bytes.Contains(storedPayload, []byte(eventID)) {
		t.Fatal("report not normalized, queued, or scrubbed")
	}
	var count int
	if err := pool.QueryRow(context.Background(), "SELECT count(*) FROM reports WHERE app_id=$1 AND report_id=$2", web, reportID).Scan(&count); err != nil || count != 1 {
		t.Fatalf("duplicate reports: %d %v", count, err)
	}
	var screenshot []byte
	if err := pool.QueryRow(context.Background(), "SELECT data FROM report_attachments WHERE report_id=$1", storedID).Scan(&screenshot); err != nil || !bytes.Equal(screenshot, shot) {
		t.Fatalf("screenshot not stored: %v", err)
	}
	stream, err := js.Stream(context.Background(), queue.StreamReports)
	if err != nil {
		t.Fatal(err)
	}
	message, err := stream.GetLastMsgForSubject(context.Background(), queue.SubjectReportAccepted)
	if err != nil {
		t.Fatal(err)
	}
	var accepted queue.ReportAccepted
	if err := json.Unmarshal(message.Data, &accepted); err != nil || accepted.ID != storedID || accepted.AppID.String() != web {
		t.Fatalf("incorrect queued event: %+v %v", accepted, err)
	}
	crashPath := webPath + "/reports/" + reportID + "/crash"
	request("GET", crashPath, token, nil, 503)
	request("GET", crashPath, stranger, nil, 404)
	request("GET", crashPath, sdkKey, nil, 401)
	request("GET", mobilePath+"/reports/"+reportID+"/crash", token, nil, 404)
	upstreamStatus.Store(http.StatusOK)
	linked := request("GET", crashPath, token, nil, 200)
	if linked["status"] != "linked" || linked["event"].(map[string]any)["event_id"] != eventID {
		t.Fatalf("Sentry event not linked: %+v", linked)
	}
	frames := linked["event"].(map[string]any)["stack_trace"].([]any)
	if len(frames) != 1 || frames[0] != "render (app.js:12)" {
		t.Fatalf("incorrect stack: %+v", frames)
	}
	for _, status := range []int{http.StatusBadRequest, http.StatusUnauthorized, http.StatusForbidden} {
		upstreamStatus.Store(int64(status))
		request("GET", crashPath, token, nil, 422)
	}
	upstreamStatus.Store(http.StatusTooManyRequests)
	request("GET", crashPath, token, nil, 429)
	upstreamStatus.Store(http.StatusOK)
	badSettings := map[string]any{"provider": "sentry", "sentry": map[string]any{"base_url": sentry.URL, "organization": "club", "project": "web", "auth_token": "rejected-token"}}
	request("PUT", webPath+"/crash-provider", token, badSettings, 422)
	if request("GET", crashPath, token, nil, 200)["status"] != "linked" {
		t.Fatal("failed update replaced working connection")
	}
	retainedSettings := map[string]any{"provider": "sentry", "sentry": map[string]any{"base_url": sentry.URL, "organization": "club", "project": "web"}}
	request("PUT", webPath+"/crash-provider", token, retainedSettings, 200)
	// Report IDs are idempotent within an app, not globally across apps.
	report["platform"] = "ios"
	upload(mobileKey, report, 202)
	if request("GET", mobilePath+"/reports/"+reportID+"/crash", token, nil, 200)["status"] != "adapter_pending" {
		t.Fatal("Crashlytics app incorrectly resolved through Sentry")
	}
	report["id"] = uuid.NewString()
	report["sentry"] = map[string]any{"event_id": "../invalid"}
	upload(sdkKey, report, 400)
	report["sentry"] = map[string]any{"event_id": strings.Repeat("b", 32)}
	upload(sdkKey, report, 202)
	if request("GET", webPath+"/reports/"+report["id"].(string)+"/crash", token, nil, 200)["status"] != "event_not_found" {
		t.Fatal("missing event was guessed into a match")
	}
	report["id"] = uuid.NewString()
	report["trigger"] = "manual"
	delete(report, "sentry")
	upload(sdkKey, report, 202)
	if request("GET", webPath+"/reports/"+report["id"].(string)+"/crash", token, nil, 200)["status"] != "no_reference" {
		t.Fatal("a manual report without Sentry should still be accepted")
	}
	t.Run("web_adapter_to_live_backend", func(t *testing.T) {
		node, err := exec.LookPath("node")
		if err != nil {
			t.Skip("Node 20+ is required for the web-adapter integration")
		}
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		// Run the real web adapter with Node's fetch/FormData implementations.
		// Test credentials are passed through the environment, never argv.
		//nolint:gosec // Node is resolved from PATH; the script and arguments are fixed test code.
		command := exec.CommandContext(ctx, node, "--input-type=module", "-e", `
import { createReporter } from '../sdks/web-sentry/index.js';
const reporter = createReporter({
  endpoint: process.env.MSEISMO_TEST_ENDPOINT,
  appKey: process.env.MSEISMO_TEST_KEY,
  installationID: crypto.randomUUID(),
  release: { version: '1.0.0', build: 'integration' },
});
const report = reporter.createReport({ sentryEventID: 'a'.repeat(32), trigger: 'non_fatal', consent: 'given' });
const result = await reporter.sendReport(report);
if (result.status !== 'accepted' || result.id !== report.id) throw new Error('report was not accepted');
console.log(JSON.stringify({ id: report.id }));
`)
		command.Env = append(os.Environ(), "MSEISMO_TEST_ENDPOINT="+server.URL+"/api/v1/reports", "MSEISMO_TEST_KEY="+sdkKey)
		output, err := command.Output()
		if err != nil {
			if exit, ok := err.(*exec.ExitError); ok {
				t.Fatalf("web adapter failed: %v: %s", err, exit.Stderr)
			}
			t.Fatalf("web adapter failed: %v", err)
		}
		var sent struct {
			ID string `json:"id"`
		}
		if err := json.Unmarshal(output, &sent); err != nil || sent.ID == "" {
			t.Fatalf("invalid web adapter result: %v: %s", err, output)
		}
		if request("GET", webPath+"/reports/"+sent.ID+"/crash", token, nil, 200)["status"] != "linked" {
			t.Fatal("web adapter report did not resolve its Sentry event")
		}
	})
	request("DELETE", webPath+"/keys/"+key["id"].(string), token, nil, 204)
	upload(sdkKey, report, 401)
	request("DELETE", webPath+"/crash-provider", token, nil, 204)
	if request("GET", crashPath, token, nil, 200)["status"] != "not_configured" {
		t.Fatal("disconnected provider still used")
	}
	request("PUT", webPath+"/crash-provider", token, retainedSettings, 422)
	request("PUT", webPath+"/crash-provider", token, map[string]any{"provider": "crashlytics", "crashlytics": map[string]any{"project_id": "firebase", "app_id": "1:123:ios:abc"}}, 200)
	request("PUT", webPath+"/crash-provider", token, retainedSettings, 422)
	request("PUT", webPath+"/crash-provider", token, settings, 200)
	if request("GET", crashPath, token, nil, 200)["status"] != "linked" {
		t.Fatal("reconnecting Sentry did not restore resolution")
	}
	t.Log("HTTP registration, per-app providers, encrypted credentials, multipart ingestion, idempotency, screenshot persistence, JetStream publication, Sentry recovery/rate limits, owner/app isolation and key revocation passed")
}
