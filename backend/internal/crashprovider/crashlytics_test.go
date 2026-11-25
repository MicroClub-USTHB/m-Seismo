package crashprovider

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/MicroClub-USTHB/m-Seismo/backend/internal/crash"
	"github.com/MicroClub-USTHB/m-Seismo/backend/internal/db"
	"github.com/MicroClub-USTHB/m-Seismo/backend/internal/ingest"
)

type fakeSource struct {
	queries []crash.Query
	found   map[string]crash.Crash // by project ID
	err     error
}

func (f *fakeSource) Check(context.Context) error { return nil }
func (f *fakeSource) Find(_ context.Context, q crash.Query) (crash.Crash, error) {
	f.queries = append(f.queries, q)
	if f.err != nil {
		return crash.Crash{}, f.err
	}
	if c, ok := f.found[q.ProjectID]; ok {
		return c, nil
	}
	return crash.Crash{}, crash.ErrNotFound
}

type projectsDB struct {
	*fakeDB
	projects []db.Project
}

func (q *projectsDB) ListAppProjects(context.Context, uuid.UUID) ([]db.Project, error) {
	return q.projects, nil
}

func TestCrashlyticsResolvesThroughTheAppsProjects(t *testing.T) {
	ctx := context.Background()
	s, base, owner, app, _ := setup(t, nil)
	other, connected := uuid.New(), uuid.New()
	q := &projectsDB{fakeDB: base, projects: []db.Project{{ID: other}, {ID: connected}}}
	s.q = q
	src := &fakeSource{found: map[string]crash.Crash{connected.String(): {
		ID:       "projects/club/apps/1:123:android:abc/events/ev42",
		URL:      "https://console.firebase.google.com/issue/1",
		Title:    "MainActivity.kt",
		Subtitle: "NullPointerException",
		Trace:    "at a.b(MainActivity.kt:12)\n\nat c.d(App.kt:3)\n",
	}}}
	s.crashes, s.crashWindow = src, 0

	if _, err := s.Set(ctx, owner, app, Input{Provider: "crashlytics", Crashlytics: &CrashlyticsSettings{ProjectID: "club", AppID: "1:123:android:abc"}}); err != nil {
		t.Fatal(err)
	}
	occurred := time.Date(2026, 11, 25, 10, 0, 0, 0, time.UTC)
	report := &ingest.Report{InstallationID: "0b7c1f6e-2d7a-4c5e-9a43-1d2f3e4a5b6c", OccurredAt: occurred.Format(time.RFC3339Nano)}

	ev, err := s.ForReport(ctx, app, report)
	if err != nil {
		t.Fatal(err)
	}
	want := Event{Provider: "crashlytics", EventID: "ev42", URL: "https://console.firebase.google.com/issue/1", Title: "MainActivity.kt — NullPointerException"}
	if ev == nil || ev.Provider != want.Provider || ev.EventID != want.EventID || ev.URL != want.URL || ev.Title != want.Title || len(ev.StackTrace) != 2 {
		t.Fatalf("event = %+v", ev)
	}
	if len(src.queries) != 2 {
		t.Fatalf("queries = %d, want one per project until found", len(src.queries))
	}
	got := src.queries[1]
	if got.ProjectID != connected.String() || got.App != "projects/club/apps/1:123:android:abc" ||
		got.InstallationID != report.InstallationID || !got.At.Equal(occurred) || got.Window != defaultCrashWindow {
		t.Fatalf("query = %+v", got)
	}
}

func TestCrashlyticsMissingCrashOrOutageDoesNotBlockTheIssue(t *testing.T) {
	ctx := context.Background()
	s, base, owner, app, _ := setup(t, nil)
	s.q = &projectsDB{fakeDB: base, projects: []db.Project{{ID: uuid.New()}}}
	if _, err := s.Set(ctx, owner, app, Input{Provider: "crashlytics", Crashlytics: &CrashlyticsSettings{ProjectID: "club", AppID: "1:123:ios:abc"}}); err != nil {
		t.Fatal(err)
	}
	report := &ingest.Report{InstallationID: "0b7c1f6e-2d7a-4c5e-9a43-1d2f3e4a5b6c", OccurredAt: time.Now().UTC().Format(time.RFC3339Nano)}

	s.crashes = &fakeSource{}
	if ev, err := s.ForReport(ctx, app, report); ev != nil || err != nil {
		t.Fatalf("no crash: %+v %v", ev, err)
	}

	s.crashes = &fakeSource{err: errors.New("crashlytics down")}
	if ev, err := s.ForReport(ctx, app, report); ev != nil || err == nil {
		t.Fatalf("outage must be reported to the caller, which only logs it: %+v %v", ev, err)
	}
}

func TestAppWithoutProviderUsesItsProjectsCrashlytics(t *testing.T) {
	ctx := context.Background()
	s, base, owner, app, _ := setup(t, nil)
	project := uuid.New()
	s.q = &projectsDB{fakeDB: base, projects: []db.Project{{ID: project}}}
	src := &fakeSource{found: map[string]crash.Crash{project.String(): {ID: "events/ev7", Title: "Crash"}}}
	s.crashes = src

	report := &ingest.Report{InstallationID: "0b7c1f6e-2d7a-4c5e-9a43-1d2f3e4a5b6c", OccurredAt: time.Now().UTC().Format(time.RFC3339Nano)}
	ev, err := s.ForReport(ctx, app, report)
	if err != nil || ev == nil || ev.EventID != "ev7" {
		t.Fatalf("event = %+v %v", ev, err)
	}
	if src.queries[0].App != "" {
		t.Fatalf("app = %q, want every Firebase app of the connection", src.queries[0].App)
	}

	id := uuid.New()
	base.reports[id] = db.Report{AppID: app, Payload: []byte(`{"installation_id":"0b7c1f6e-2d7a-4c5e-9a43-1d2f3e4a5b6c","occurred_at":"` + report.OccurredAt + `"}`)}
	link, err := s.ReportCrash(ctx, owner, app, id)
	if err != nil || link.Status != "linked" || link.Provider != "crashlytics" {
		t.Fatalf("link = %+v %v", link, err)
	}
}
