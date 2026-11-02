//go:build integration

package worker

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"github.com/MicroClub-USTHB/m-Seismo/backend/internal/testenv"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"

	"github.com/MicroClub-USTHB/m-Seismo/backend/internal/config"
	"github.com/MicroClub-USTHB/m-Seismo/backend/internal/crashprovider"
	"github.com/MicroClub-USTHB/m-Seismo/backend/internal/db"
	"github.com/MicroClub-USTHB/m-Seismo/backend/internal/ingest"
	"github.com/MicroClub-USTHB/m-Seismo/backend/internal/issue"
	"github.com/MicroClub-USTHB/m-Seismo/backend/internal/queue"
	"github.com/MicroClub-USTHB/m-Seismo/backend/internal/routing"
)

type capturedIssue struct{ drafts chan issue.Draft }

func (c *capturedIssue) OpenInRepository(_ context.Context, _, _ uuid.UUID, d issue.Draft) (issue.Result, error) {
	c.drafts <- d
	return issue.Result{Ref: issue.Ref{ID: "1", URL: "https://github.com/test/repo/issues/1"}}, nil
}

func (c *capturedIssue) CommentOn(context.Context, uuid.UUID, db.Issue, string) (bool, error) {
	return true, nil
}

// Exercise the real queue consumer, report/connection persistence and processor.
// Only Sentry and the tracker write boundary are simulated.
func TestSentryReportQueueToIssueIntegration(t *testing.T) {
	databaseURL, natsURL := testenv.DatabaseURL(t), testenv.NATSURL(t)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	pool, err := pgxpool.New(ctx, databaseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	q := db.New(pool)
	owner, err := q.CreateOwner(ctx, db.CreateOwnerParams{Email: uuid.NewString() + "@test.invalid", PasswordHash: "test-only", Name: "Queue Sentry test"})
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if _, err := q.DeleteOwner(context.Background(), owner.ID); err != nil {
			t.Error(err)
		}
	}()
	app, err := q.CreateApp(ctx, db.CreateAppParams{OwnerID: owner.ID, Name: "Web", Platform: db.AppPlatformWeb, BundleID: "web.test"})
	if err != nil {
		t.Fatal(err)
	}
	project, err := q.CreateProject(ctx, db.CreateProjectParams{OwnerID: owner.ID, Name: "Test", Slug: uuid.NewString()})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := q.UpsertProviderConnection(ctx, db.UpsertProviderConnectionParams{OwnerID: owner.ID, AppID: app.ID, Tracker: db.TrackerGithub, ExternalID: uuid.NewString(), AccountLogin: "test", AccountType: "Organization"}); err != nil {
		t.Fatal(err)
	}
	if _, err := q.LinkProjectRepository(ctx, db.LinkProjectRepositoryParams{ProjectID: project.ID, Tracker: db.TrackerGithub, Repository: "test/repo", IsDefault: true}); err != nil {
		t.Fatal(err)
	}
	if err := q.LinkAppProject(ctx, db.LinkAppProjectParams{AppID: app.ID, ProjectID: project.ID}); err != nil {
		t.Fatal(err)
	}
	eventID := strings.Repeat("e", 32)
	upstream := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer test-read-token" {
			w.WriteHeader(403)
			return
		}
		if strings.Contains(r.URL.Path, "/events/") {
			_, _ = w.Write([]byte(`{"eventID":"` + eventID + `","groupID":"17","title":"Unhandled browser error","entries":[{"type":"exception","data":{"values":[{"stacktrace":{"frames":[{"function":"render","filename":"app.js","lineNo":7}]}}]}}]}`))
		} else {
			_, _ = w.Write([]byte(`{"id":"42"}`))
		}
	}))
	defer upstream.Close()
	roots := x509.NewCertPool()
	roots.AddCert(upstream.Certificate())
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.TLSClientConfig = &tls.Config{RootCAs: roots, MinVersion: tls.VersionTLS12}
	previous := http.DefaultTransport
	http.DefaultTransport = transport
	defer func() { http.DefaultTransport = previous; transport.CloseIdleConnections() }()
	resolver, err := crashprovider.NewService(q, &config.Config{CrashProviders: config.CrashProvidersConfig{EncryptionKey: base64.StdEncoding.EncodeToString(make([]byte, 32)), SentryOrigins: []string{upstream.URL}}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := resolver.Set(ctx, owner.ID, app.ID, crashprovider.Input{Provider: "sentry", Sentry: &crashprovider.SentryInput{SentrySettings: crashprovider.SentrySettings{BaseURL: upstream.URL, Organization: "club", Project: "web"}, AuthToken: "test-read-token"}}); err != nil {
		t.Fatal(err)
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
	report.Trigger = "crash"
	if err := report.Validate(); err != nil {
		t.Fatal(err)
	}
	payload, err = json.Marshal(report)
	if err != nil {
		t.Fatal(err)
	}
	saved, err := ingest.NewPGStore(pool).Save(ctx, app.ID, report, payload, nil)
	if err != nil {
		t.Fatal(err)
	}
	nc, err := nats.Connect(natsURL)
	if err != nil {
		t.Fatal(err)
	}
	defer nc.Close()
	js, err := jetstream.New(nc)
	if err != nil {
		t.Fatal(err)
	}
	publisher, err := queue.NewJetStream(ctx, js)
	if err != nil {
		t.Fatal(err)
	}
	sink := &capturedIssue{drafts: make(chan issue.Draft, 8)}
	w := New(js, NewProcessor(q, sink, routing.NewRouter(q), pgLocks{pool: pool}, nil, 0, resolver, nil, nil), 1, 3)
	if err := w.Start(ctx); err != nil {
		t.Fatal(err)
	}
	defer func() {
		stop, done := context.WithTimeout(context.Background(), 5*time.Second)
		defer done()
		if err := w.Stop(stop); err != nil {
			t.Error(err)
		}
	}()
	if err := publisher.ReportAccepted(ctx, queue.ReportAccepted{ID: saved.ID, AppID: app.ID}); err != nil {
		t.Fatal(err)
	}
	select {
	case draft := <-sink.drafts:
		for _, want := range []string{"### Crash", eventID, "Unhandled browser error", "render (app.js:7)", upstream.URL + "/organizations/club/issues/17/events/" + eventID + "/"} {
			if !strings.Contains(draft.Body, want) {
				t.Fatalf("issue missing %q", want)
			}
		}
	case <-ctx.Done():
		t.Fatal("worker did not publish Sentry crash issue")
	}
	for {
		row, err := q.GetReport(ctx, saved.ID)
		if err != nil {
			t.Fatal(err)
		}
		if row.ProcessedAt != nil {
			break
		}
		select {
		case <-ctx.Done():
			t.Fatal("report not marked processed")
		case <-time.After(10 * time.Millisecond):
		}
	}
	deliveries, err := q.ListReportDeliveries(ctx, saved.ID)
	if err != nil || len(deliveries) != 1 || deliveries[0].Url != "https://github.com/test/repo/issues/1" {
		t.Fatalf("delivery not recorded: %v", err)
	}
}
