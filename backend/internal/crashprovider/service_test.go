package crashprovider

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/MicroClub-USTHB/m-Seismo/backend/internal/apperr"
	"github.com/MicroClub-USTHB/m-Seismo/backend/internal/config"
	"github.com/MicroClub-USTHB/m-Seismo/backend/internal/db"
)

type fakeDB struct {
	db.Querier
	owners      map[uuid.UUID]uuid.UUID
	connections map[uuid.UUID]db.AppCrashProvider
	reports     map[uuid.UUID]db.Report
}

func (q *fakeDB) GetApp(_ context.Context, p db.GetAppParams) (db.App, error) {
	if owner, ok := q.owners[p.ID]; !ok || owner != p.OwnerID {
		return db.App{}, pgx.ErrNoRows
	}
	return db.App{ID: p.ID, OwnerID: p.OwnerID}, nil
}
func (q *fakeDB) GetAppCrashProvider(_ context.Context, id uuid.UUID) (db.AppCrashProvider, error) {
	if row, ok := q.connections[id]; ok {
		return row, nil
	}
	return db.AppCrashProvider{}, pgx.ErrNoRows
}
func (q *fakeDB) SetAppCrashProvider(_ context.Context, p db.SetAppCrashProviderParams) (db.AppCrashProvider, error) {
	row := db.AppCrashProvider{AppID: p.AppID, Provider: p.Provider, Settings: p.Settings, Credentials: p.Credentials, UpdatedAt: time.Now()}
	q.connections[p.AppID] = row
	return row, nil
}
func (q *fakeDB) DeleteAppCrashProvider(_ context.Context, id uuid.UUID) error {
	delete(q.connections, id)
	return nil
}
func (q *fakeDB) GetReportByReportID(_ context.Context, p db.GetReportByReportIDParams) (db.Report, error) {
	if r, ok := q.reports[p.ReportID]; ok && r.AppID == p.AppID {
		return r, nil
	}
	return db.Report{}, pgx.ErrNoRows
}

func setup(t *testing.T, handler http.HandlerFunc) (*Service, *fakeDB, uuid.UUID, uuid.UUID, string) {
	t.Helper()
	server := httptest.NewTLSServer(handler)
	t.Cleanup(server.Close)
	q := &fakeDB{owners: map[uuid.UUID]uuid.UUID{}, connections: map[uuid.UUID]db.AppCrashProvider{}, reports: map[uuid.UUID]db.Report{}}
	owner, app := uuid.New(), uuid.New()
	q.owners[app] = owner
	s, err := NewService(q, &config.Config{CrashProviders: config.CrashProvidersConfig{
		EncryptionKey: base64.StdEncoding.EncodeToString(make([]byte, 32)), SentryOrigins: []string{server.URL},
	}})
	if err != nil {
		t.Fatal(err)
	}
	s.sentry.http.Transport = server.Client().Transport
	return s, q, owner, app, server.URL
}

func sentryInput(origin, token string) Input {
	return Input{Provider: "sentry", Sentry: &SentryInput{SentrySettings: SentrySettings{BaseURL: origin, Organization: "club", Project: "mobile"}, AuthToken: token}}
}

func TestAppsChooseProvidersIndependentlyAndCredentialsStayPrivate(t *testing.T) {
	ctx := context.Background()
	s, q, owner, app, origin := setup(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer secret-token" {
			t.Errorf("unexpected token")
		}
		if r.URL.Path != "/api/0/projects/club/mobile/" {
			t.Errorf("path = %s", r.URL.Path)
		}
		_, _ = w.Write([]byte(`{"id":"42"}`))
	})
	connection, err := s.Set(ctx, owner, app, sentryInput(origin, "secret-token"))
	if err != nil {
		t.Fatal(err)
	}
	encoded, _ := json.Marshal(connection)
	if strings.Contains(string(encoded), "secret-token") || strings.Contains(string(q.connections[app].Credentials), "secret-token") {
		t.Fatal("token exposed")
	}
	if !connection.HasCredentials {
		t.Fatal("missing credential indicator")
	}
	second := uuid.New()
	q.owners[second] = owner
	_, err = s.Set(ctx, owner, second, Input{Provider: "crashlytics", Crashlytics: &CrashlyticsSettings{ProjectID: "firebase-project", AppID: "1:123:ios:abc"}})
	if err != nil {
		t.Fatal(err)
	}
	first, _ := s.Get(ctx, owner, app)
	other, _ := s.Get(ctx, owner, second)
	if first.Provider != "sentry" || other.Provider != "crashlytics" || other.HasCredentials {
		t.Fatal("app settings were mixed")
	}
	if err := s.Delete(ctx, owner, second); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Get(ctx, owner, app); err != nil {
		t.Fatal("deleting one app's connection affected another")
	}
}

func TestOwnerIsolationBeforeExternalCalls(t *testing.T) {
	s, _, _, app, origin := setup(t, func(http.ResponseWriter, *http.Request) { t.Error("upstream called without ownership") })
	ctx, stranger := context.Background(), uuid.New()
	_, err := s.Set(ctx, stranger, app, sentryInput(origin, "secret"))
	if !errors.Is(err, pgx.ErrNoRows) {
		t.Fatal(err)
	}
	_, err = s.Get(ctx, stranger, app)
	if !errors.Is(err, pgx.ErrNoRows) {
		t.Fatal(err)
	}
	if err = s.Delete(ctx, stranger, app); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatal(err)
	}
	_, err = s.ReportCrash(ctx, stranger, app, uuid.New())
	if !errors.Is(err, pgx.ErrNoRows) {
		t.Fatal(err)
	}
}

func TestTokenRotationAndFailedUpdatesPreserveConnection(t *testing.T) {
	s, q, owner, app, origin := setup(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") == "Bearer bad" {
			w.WriteHeader(403)
			_, _ = w.Write([]byte(`private error content`))
			return
		}
		_, _ = w.Write([]byte(`{"id":"42"}`))
	})
	ctx := context.Background()
	if _, err := s.Set(ctx, owner, app, sentryInput(origin, "first")); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Set(ctx, owner, app, sentryInput(origin, "")); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Set(ctx, owner, app, sentryInput(origin, "second")); err != nil {
		t.Fatal(err)
	}
	_, err := s.Set(ctx, owner, app, sentryInput(origin, "bad"))
	if kind, _ := apperr.Classify(err); kind != apperr.KindInvalid {
		t.Fatal(err)
	}
	if strings.Contains(err.Error(), "private") {
		t.Fatal("upstream body leaked")
	}
	token, err := s.vault.open(app, q.connections[app].Credentials)
	if err != nil || token != "second" {
		t.Fatal("failed update replaced valid token")
	}
}

func TestReportResolvesExactEventWithoutRawPersonalData(t *testing.T) {
	id := strings.Repeat("a", 32)
	available := false
	s, q, owner, app, origin := setup(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/0/projects/club/mobile/" {
			_, _ = w.Write([]byte(`{"id":"42"}`))
			return
		}
		if r.URL.Path != "/api/0/projects/club/mobile/events/"+id+"/" {
			t.Errorf("unexpected event path: %s", r.URL.Path)
		}
		if !available {
			w.WriteHeader(404)
			return
		}
		_, _ = w.Write([]byte(`{"eventID":"` + id + `","groupID":"42","title":"TypeError","user":{"email":"private@example.com"},"entries":[{"type":"exception","data":{"values":[{"stacktrace":{"frames":[{"function":"outer","filename":"app.js","lineNo":10},{"function":"inner","filename":"app.js","lineNo":20}]}}]}}]}`))
	})
	ctx := context.Background()
	if _, err := s.Set(ctx, owner, app, sentryInput(origin, "token")); err != nil {
		t.Fatal(err)
	}
	report := uuid.New()
	q.reports[report] = db.Report{AppID: app, Payload: []byte(`{"sentry":{"event_id":"` + strings.ToUpper(id) + `"}}`)}
	link, err := s.ReportCrash(ctx, owner, app, report)
	if err != nil || link.Status != "event_not_found" {
		t.Fatalf("%+v %v", link, err)
	}
	available = true
	link, err = s.ReportCrash(ctx, owner, app, report)
	if err != nil || link.Status != "linked" {
		t.Fatalf("%+v %v", link, err)
	}
	if link.Event.StackTrace[0] != "inner (app.js:20)" || !strings.Contains(link.Event.URL, "/issues/42/events/"+id) {
		t.Fatalf("bad event: %+v", link.Event)
	}
	encoded, _ := json.Marshal(link)
	if strings.Contains(string(encoded), "private@example.com") {
		t.Fatal("identity copied")
	}
	other := uuid.New()
	q.owners[other] = owner
	if _, err := s.ReportCrash(ctx, owner, other, report); !errors.Is(err, pgx.ErrNoRows) {
		t.Fatal("report from another app exposed")
	}
	if err := s.Delete(ctx, owner, app); err != nil {
		t.Fatal(err)
	}
	link, err = s.ReportCrash(ctx, owner, app, report)
	if err != nil || link.Status != "not_configured" {
		t.Fatalf("%+v %v", link, err)
	}
}

func TestVaultBindsCredentialsToAppAndDetectsTampering(t *testing.T) {
	v, err := newVault(base64.StdEncoding.EncodeToString(make([]byte, 32)))
	if err != nil {
		t.Fatal(err)
	}
	app := uuid.New()
	data, err := v.seal(app, "secret")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := v.open(uuid.New(), data); err == nil {
		t.Fatal("accepted another app's ciphertext")
	}
	data[len(data)-1] ^= 1
	if _, err := v.open(app, data); err == nil {
		t.Fatal("accepted corrupted ciphertext")
	}
	if _, err := newVault("not-a-key"); err == nil {
		t.Fatal("accepted invalid key")
	}
}

func TestOriginRestrictionsAndRedirects(t *testing.T) {
	s, _, owner, app, origin := setup(t, func(w http.ResponseWriter, r *http.Request) { http.Redirect(w, r, "https://untrusted.example/", 302) })
	for _, target := range []string{"http://127.0.0.1", "https://untrusted.example", origin + "/path", "https://user:pass@host", origin + "?q=secret", origin + "#fragment"} {
		if _, err := s.Set(context.Background(), owner, app, sentryInput(target, "token")); err == nil {
			t.Errorf("allowed origin %s", target)
		}
	}
	_, err := s.Set(context.Background(), owner, app, sentryInput(origin, "token"))
	if kind, _ := apperr.Classify(err); kind != apperr.KindUnavailable {
		t.Fatalf("redirect was not refused: %v", err)
	}
}

func TestSentryRateLimitsAndUnexpectedEvents(t *testing.T) {
	status := http.StatusTooManyRequests
	s, _, _, _, origin := setup(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Retry-After", "17")
		w.WriteHeader(status)
		_, _ = w.Write([]byte(`{"eventID":"bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"}`))
	})
	_, err := s.sentry.event(context.Background(), sentryInput(origin, "").Sentry.SentrySettings, "token", strings.Repeat("a", 32))
	if wait, ok := apperr.RetryAfterOf(err); !ok || wait != 17*time.Second {
		t.Fatalf("rate limit = %v", err)
	}
	status = http.StatusOK
	_, err = s.sentry.event(context.Background(), sentryInput(origin, "").Sentry.SentrySettings, "token", strings.Repeat("a", 32))
	if kind, _ := apperr.Classify(err); kind != apperr.KindUnavailable {
		t.Fatalf("accepted mismatched event: %v", err)
	}
}

func TestNoProviderAndCrashlyticsDoNotNeedSentryCredentials(t *testing.T) {
	s, q, owner, app, origin := setup(t, func(http.ResponseWriter, *http.Request) {
		t.Error("unexpected Sentry call")
	})
	s.vault, _ = newVault("")
	ctx := context.Background()
	report := uuid.New()
	q.reports[report] = db.Report{AppID: app, Payload: []byte(`{}`)}
	link, err := s.ReportCrash(ctx, owner, app, report)
	if err != nil || link.Status != "not_configured" {
		t.Fatalf("unconfigured app: %+v %v", link, err)
	}
	_, err = s.Set(ctx, owner, app, sentryInput(origin, "token"))
	if kind, _ := apperr.Classify(err); kind != apperr.KindUnavailable {
		t.Fatalf("Sentry setup without encryption: %v", err)
	}
	_, err = s.Set(ctx, owner, app, Input{Provider: "crashlytics", Crashlytics: &CrashlyticsSettings{ProjectID: "firebase", AppID: "1:123:ios:abc"}})
	if err != nil {
		t.Fatal(err)
	}
	link, err = s.ReportCrash(ctx, owner, app, report)
	if err != nil || link.Status != "adapter_pending" || link.Provider != "crashlytics" {
		t.Fatalf("Crashlytics pending: %+v %v", link, err)
	}
}
