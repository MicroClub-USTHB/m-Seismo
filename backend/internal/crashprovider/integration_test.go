package crashprovider

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/MicroClub-USTHB/m-Seismo/backend/internal/config"
	"github.com/MicroClub-USTHB/m-Seismo/backend/internal/db"
	"github.com/MicroClub-USTHB/m-Seismo/backend/internal/ingest"
)

// Uses an isolated, migrated PostgreSQL database when explicitly configured.
// External Sentry calls still go to a local TLS server, never a real account.
func TestPostgresReportCrashIntegration(t *testing.T) {
	databaseURL := os.Getenv("SEISMO_TEST_DATABASE_URL")
	if databaseURL == "" {
		t.Skip("set SEISMO_TEST_DATABASE_URL to an isolated migrated test database")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	q := db.New(pool)
	owner, err := q.CreateOwner(ctx, db.CreateOwnerParams{Email: uuid.NewString() + "@test.invalid", PasswordHash: "test-only", Name: "Crash integration test"})
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		cleanup, done := context.WithTimeout(context.Background(), 5*time.Second)
		defer done()
		if _, err := q.DeleteOwner(cleanup, owner.ID); err != nil {
			t.Error(err)
		}
	}()
	app, err := q.CreateApp(ctx, db.CreateAppParams{OwnerID: owner.ID, Name: "Web test", Platform: db.AppPlatformWeb, BundleID: "test.invalid"})
	if err != nil {
		t.Fatal(err)
	}
	eventID := strings.Repeat("a", 32)
	upstream := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/events/") {
			_, _ = w.Write([]byte(`{"eventID":"` + eventID + `","groupID":"123","title":"Test crash"}`))
			return
		}
		_, _ = w.Write([]byte(`{"id":"42"}`))
	}))
	defer upstream.Close()
	s, err := NewService(q, &config.Config{CrashProviders: config.CrashProvidersConfig{
		EncryptionKey: base64.StdEncoding.EncodeToString(make([]byte, 32)), SentryOrigins: []string{upstream.URL},
	}})
	if err != nil {
		t.Fatal(err)
	}
	s.sentry.http.Transport = upstream.Client().Transport
	if _, err := s.Set(ctx, owner.ID, app.ID, sentryInput(upstream.URL, "private-token")); err != nil {
		t.Fatal(err)
	}
	connection, err := q.GetAppCrashProvider(ctx, app.ID)
	if err != nil || strings.Contains(string(connection.Credentials), "private-token") {
		t.Fatalf("stored credentials: %v", err)
	}
	payload, err := os.ReadFile("../../../spec/examples/sentry-web-report.json")
	if err != nil {
		t.Fatal(err)
	}
	report, err := ingest.ParseReport(payload)
	if err != nil {
		t.Fatal(err)
	}
	report.ID = uuid.NewString()
	report.Sentry.EventID = eventID
	if err := report.Validate(); err != nil {
		t.Fatal(err)
	}
	report.Normalize()
	payload, err = json.Marshal(report)
	if err != nil {
		t.Fatal(err)
	}
	store := ingest.NewPGStore(pool)
	saved, err := store.Save(ctx, app.ID, report, payload, nil)
	if err != nil {
		t.Fatal(err)
	}
	duplicate, err := store.Save(ctx, app.ID, report, payload, nil)
	if err != nil || !duplicate.Duplicate || duplicate.ID != saved.ID {
		t.Fatalf("report not idempotent: %+v %v", duplicate, err)
	}
	link, err := s.ReportCrash(ctx, owner.ID, app.ID, uuid.MustParse(report.ID))
	if err != nil || link.Status != "linked" || link.Event.EventID != eventID {
		t.Fatalf("resolution: %+v %v", link, err)
	}
	if _, err := s.ReportCrash(ctx, uuid.New(), app.ID, uuid.MustParse(report.ID)); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("cross-owner report exposed: %v", err)
	}
	if _, err := q.DeleteApp(ctx, db.DeleteAppParams{ID: app.ID, OwnerID: owner.ID}); err != nil {
		t.Fatal(err)
	}
	if _, err := q.GetAppCrashProvider(ctx, app.ID); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatalf("connection did not cascade: %v", err)
	}
}
