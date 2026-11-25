package crashlytics

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	fc "google.golang.org/api/firebasecrashlytics/v1alpha"
	"google.golang.org/api/option"

	"github.com/MicroClub-USTHB/m-Seismo/backend/internal/crash"

	"github.com/google/uuid"
)

const (
	project = "0d6c7c66-0000-4000-8000-000000000001"
	app     = "projects/123/apps/1:123:android:abc"
)

// testOwner owns project; otherOwner owns nothing.
var (
	testOwner  = uuid.MustParse("0d6c7c66-0000-4000-8000-0000000000aa")
	otherOwner = uuid.MustParse("0d6c7c66-0000-4000-8000-0000000000bb")
)

// memStore is an in-memory Store for tests.
type memStore struct {
	projects map[string]uuid.UUID // project ID → owner
	conns    map[string]Connection
}

func newMemStore(conns ...Connection) *memStore {
	s := &memStore{projects: map[string]uuid.UUID{project: testOwner}, conns: map[string]Connection{}}
	for _, c := range conns {
		s.projects[c.ProjectID] = testOwner
		s.conns[c.ProjectID] = c
	}
	return s
}

func (s *memStore) OwnsProject(_ context.Context, ownerID uuid.UUID, id string) (bool, error) {
	owner, ok := s.projects[id]
	return ok && owner == ownerID, nil
}

func (s *memStore) Save(_ context.Context, id, email, token string) error {
	c := s.conns[id]
	c.ProjectID, c.Email, c.RefreshToken = id, email, token
	s.conns[id] = c
	return nil
}

func (s *memStore) Get(_ context.Context, id string) (Connection, error) {
	c, ok := s.conns[id]
	if !ok {
		return Connection{}, ErrNotConnected
	}
	return c, nil
}

func (s *memStore) List(context.Context) ([]Connection, error) {
	var out []Connection
	for _, c := range s.conns {
		out = append(out, c)
	}
	return out, nil
}

func (s *memStore) SetApps(_ context.Context, id string, apps []string) error {
	c, ok := s.conns[id]
	if !ok {
		return ErrNotConnected
	}
	c.Apps = apps
	s.conns[id] = c
	return nil
}

var testConfig = Config{ClientID: "id", ClientSecret: "secret", RedirectURL: "https://seismo.test/api/v1/oauth/crashlytics/callback"}

// newTestSource returns a Source reading conns, whose API calls go to handler.
func newTestSource(t *testing.T, conns []Connection, handler http.HandlerFunc) *Source {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)

	s, err := New(testConfig, newMemStore(conns...),
		option.WithEndpoint(srv.URL),
		option.WithHTTPClient(srv.Client()),
	)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

// connected is a project connected to Crashlytics with apps.
func connected(apps ...string) []Connection {
	return []Connection{{ProjectID: project, Email: "bot@example.com", RefreshToken: "token", Apps: apps}}
}

func writeEvents(t *testing.T, w http.ResponseWriter, next string, events ...*fc.Event) {
	t.Helper()
	if err := json.NewEncoder(w).Encode(fc.ListEventsResponse{Events: events, NextPageToken: next}); err != nil {
		t.Fatal(err)
	}
}

func writeIssues(t *testing.T, w http.ResponseWriter, ids ...string) {
	t.Helper()
	report := fc.Report{}
	for _, id := range ids {
		report.Groups = append(report.Groups, &fc.ReportGroup{Issue: &fc.Issue{Id: id}})
	}
	if err := json.NewEncoder(w).Encode(report); err != nil {
		t.Fatal(err)
	}
}

func isTopIssues(r *http.Request) bool { return strings.HasSuffix(r.URL.Path, "/reports/topIssues") }

func TestFindPicksClosestEventOfTheInstallation(t *testing.T) {
	at := time.Date(2026, 10, 17, 12, 0, 0, 0, time.UTC)

	var issuesQuery, eventsQuery string
	s := newTestSource(t, connected(app), func(w http.ResponseWriter, r *http.Request) {
		if isTopIssues(r) {
			issuesQuery = r.URL.RawQuery
			writeIssues(t, w, "issue-1")
			return
		}
		eventsQuery = r.URL.RawQuery
		writeEvents(t, w, "",
			&fc.Event{Name: "far", EventTime: at.Add(-90 * time.Second).Format(time.RFC3339)},
			&fc.Event{
				Name:          "close",
				EventTime:     at.Add(10 * time.Second).Format(time.RFC3339),
				IssueTitle:    "MainActivity.kt",
				IssueSubtitle: "boom",
				Issue:         &fc.Issue{Uri: "https://console.firebase.google.com/issue"},
				CustomKeys:    map[string]string{"screen": "home"},
				Breadcrumbs:   []*fc.Breadcrumb{{Title: "screen_view", EventTime: at.Format(time.RFC3339)}},
				Exceptions: []*fc.Exception{{
					Type:             "java.lang.IllegalStateException",
					ExceptionMessage: "boom",
					Frames:           []*fc.Frame{{Symbol: "com.app.Main.onCreate", File: "Main.kt", Line: 42}},
				}},
			},
		)
	})

	c, err := s.Find(context.Background(), crash.Query{ProjectID: project, InstallationID: "inst-1", At: at, Window: 2 * time.Minute})
	if err != nil {
		t.Fatal(err)
	}

	if c.ID != "close" {
		t.Errorf("ID = %q, want %q", c.ID, "close")
	}
	if c.URL != "https://console.firebase.google.com/issue" || c.Title != "MainActivity.kt" || c.Subtitle != "boom" {
		t.Errorf("unexpected crash summary: %+v", c)
	}
	if c.CustomKeys["screen"] != "home" || len(c.Breadcrumbs) != 1 || c.Breadcrumbs[0].Title != "screen_view" {
		t.Errorf("missing custom keys or breadcrumbs: %+v", c)
	}
	if !strings.Contains(c.Trace, "java.lang.IllegalStateException: boom") || !strings.Contains(c.Trace, "at com.app.Main.onCreate (Main.kt:42)") {
		t.Errorf("unexpected trace:\n%s", c.Trace)
	}

	window := []string{"filter.interval.startTime=2026-10-17T11%3A58%3A00Z", "filter.interval.endTime=2026-10-17T12%3A02%3A00Z"}
	for _, want := range window {
		if !strings.Contains(issuesQuery, want) {
			t.Errorf("issues query %q missing %q", issuesQuery, want)
		}
	}
	events, err := url.ParseQuery(eventsQuery)
	if err != nil {
		t.Fatal(err)
	}
	if got := events.Get("filter.issue.id"); got != "issue-1" {
		t.Errorf("filter.issue.id = %q, want issue-1", got)
	}
	if got, want := events.Get("filterExpression"), `custom_keys.seismo_installation_id = "inst-1"`; got != want {
		t.Errorf("filterExpression = %q, want %q", got, want)
	}
	for _, want := range window {
		if !strings.Contains(eventsQuery, want) {
			t.Errorf("events query %q missing %q", eventsQuery, want)
		}
	}
}

func TestFindTakesTitleAndURLFromTheIssue(t *testing.T) {
	at := time.Date(2026, 10, 17, 12, 0, 0, 0, time.UTC)

	s := newTestSource(t, connected(app), func(w http.ResponseWriter, r *http.Request) {
		if isTopIssues(r) {
			issue := &fc.Issue{Id: "issue-1", Title: "Main.kt", Subtitle: "boom", Uri: "https://console.firebase.google.com/issue-1"}
			if err := json.NewEncoder(w).Encode(fc.Report{Groups: []*fc.ReportGroup{{Issue: issue}}}); err != nil {
				t.Fatal(err)
			}
			return
		}
		writeEvents(t, w, "", &fc.Event{Name: "crash", EventTime: at.Format(time.RFC3339)})
	})

	c, err := s.Find(context.Background(), crash.Query{ProjectID: project, InstallationID: "inst-1", At: at, Window: time.Minute})
	if err != nil {
		t.Fatal(err)
	}
	if c.Title != "Main.kt" || c.Subtitle != "boom" || c.URL != "https://console.firebase.google.com/issue-1" {
		t.Errorf("crash = %+v, want the issue's title, subtitle and URL", c)
	}
}

func TestFindSearchesEveryIssue(t *testing.T) {
	at := time.Date(2026, 10, 17, 12, 0, 0, 0, time.UTC)

	s := newTestSource(t, connected(app), func(w http.ResponseWriter, r *http.Request) {
		switch {
		case isTopIssues(r):
			writeIssues(t, w, "issue-1", "issue-2")
		case r.URL.Query().Get("filter.issue.id") == "issue-2":
			writeEvents(t, w, "", &fc.Event{Name: "match", EventTime: at.Format(time.RFC3339)})
		default:
			writeEvents(t, w, "")
		}
	})

	c, err := s.Find(context.Background(), crash.Query{ProjectID: project, InstallationID: "inst-1", At: at, Window: time.Minute})
	if err != nil {
		t.Fatal(err)
	}
	if c.ID != "match" {
		t.Errorf("ID = %q, want %q", c.ID, "match")
	}
}

func TestFindReadsFollowingPages(t *testing.T) {
	at := time.Date(2026, 10, 17, 12, 0, 0, 0, time.UTC)

	s := newTestSource(t, connected(app), func(w http.ResponseWriter, r *http.Request) {
		if isTopIssues(r) {
			writeIssues(t, w, "issue-1")
			return
		}
		if r.URL.Query().Get("pageToken") == "" {
			writeEvents(t, w, "page-2", &fc.Event{Name: "far", EventTime: at.Add(-50 * time.Second).Format(time.RFC3339)})
			return
		}
		writeEvents(t, w, "", &fc.Event{Name: "match", EventTime: at.Format(time.RFC3339)})
	})

	c, err := s.Find(context.Background(), crash.Query{ProjectID: project, InstallationID: "inst-1", At: at, Window: time.Minute})
	if err != nil {
		t.Fatal(err)
	}
	if c.ID != "match" {
		t.Errorf("ID = %q, want %q", c.ID, "match")
	}
}

func TestFindNotFound(t *testing.T) {
	for name, issues := range map[string][]string{"no issue in the window": nil, "no event of the installation": {"issue-1"}} {
		t.Run(name, func(t *testing.T) {
			s := newTestSource(t, connected(app), func(w http.ResponseWriter, r *http.Request) {
				if isTopIssues(r) {
					writeIssues(t, w, issues...)
					return
				}
				writeEvents(t, w, "")
			})

			_, err := s.Find(context.Background(), crash.Query{ProjectID: project, InstallationID: "inst-1", At: time.Now(), Window: time.Minute})
			if !errors.Is(err, crash.ErrNotFound) {
				t.Fatalf("err = %v, want crash.ErrNotFound", err)
			}
		})
	}
}

func TestFindAPIError(t *testing.T) {
	s := newTestSource(t, connected(app), func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, `{"error":{"code":403,"message":"denied"}}`, http.StatusForbidden)
	})

	_, err := s.Find(context.Background(), crash.Query{ProjectID: project, InstallationID: "inst-1", At: time.Now(), Window: time.Minute})
	if err == nil || errors.Is(err, crash.ErrNotFound) {
		t.Fatalf("err = %v, want an API error", err)
	}
}

func TestCheck(t *testing.T) {
	s := newTestSource(t, connected(app, "projects/123/apps/denied"), func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "denied") {
			http.Error(w, `{"error":{"code":403,"message":"denied"}}`, http.StatusForbidden)
			return
		}
		if !isTopIssues(r) {
			t.Errorf("Check called %s, want the top issues report", r.URL.Path)
		}
		writeIssues(t, w)
	})

	err := s.Check(context.Background())
	if err == nil || !strings.Contains(err.Error(), "denied") || strings.Contains(err.Error(), "android") {
		t.Fatalf("err = %v, want an error naming only the denied app", err)
	}
}

func TestCheckProjectWithoutApps(t *testing.T) {
	s := newTestSource(t, connected(), func(w http.ResponseWriter, r *http.Request) {
		t.Error("no API call expected")
	})

	if err := s.Check(context.Background()); err == nil || !strings.Contains(err.Error(), "no Firebase apps") {
		t.Fatalf("err = %v, want a missing apps error", err)
	}
}

func TestCheckNoConnection(t *testing.T) {
	s := newTestSource(t, nil, func(w http.ResponseWriter, r *http.Request) {
		t.Error("no API call expected")
	})

	if err := s.Check(context.Background()); err != nil {
		t.Fatalf("err = %v, want nil", err)
	}
}

func TestFindNotConnected(t *testing.T) {
	s := newTestSource(t, nil, func(w http.ResponseWriter, r *http.Request) {
		t.Error("no API call expected")
	})

	_, err := s.Find(context.Background(), crash.Query{ProjectID: project, InstallationID: "inst-1", At: time.Now(), Window: time.Minute})
	if !errors.Is(err, crash.ErrNotFound) {
		t.Fatalf("err = %v, want crash.ErrNotFound", err)
	}
}

func TestFindSearchesEveryAppOfTheProject(t *testing.T) {
	at := time.Date(2026, 10, 17, 12, 0, 0, 0, time.UTC)
	ios := "projects/123/apps/1:123:ios:def"

	s := newTestSource(t, connected(app, ios), func(w http.ResponseWriter, r *http.Request) {
		switch {
		case isTopIssues(r):
			writeIssues(t, w, "issue-1")
		case strings.Contains(r.URL.Path, "ios"):
			writeEvents(t, w, "", &fc.Event{Name: "ios-crash", EventTime: at.Format(time.RFC3339)})
		default:
			writeEvents(t, w, "")
		}
	})

	c, err := s.Find(context.Background(), crash.Query{ProjectID: project, InstallationID: "inst-1", At: at, Window: time.Minute})
	if err != nil {
		t.Fatal(err)
	}
	if c.ID != "ios-crash" {
		t.Errorf("ID = %q, want %q", c.ID, "ios-crash")
	}
}

func TestNewRequiresClient(t *testing.T) {
	if _, err := New(Config{}, newMemStore()); err == nil {
		t.Fatal("want an error without OAuth client")
	}
}

func TestFormatTraceIOSThread(t *testing.T) {
	trace := formatTrace(&fc.Event{Threads: []*fc.Thread{
		{Name: "background", Frames: []*fc.Frame{{Symbol: "idle"}}},
		{Name: "main", Crashed: true, Signal: "SIGABRT", SignalCode: "ABORT", Frames: []*fc.Frame{{Symbol: "ViewController.tap", Library: "MyApp"}}},
	}})

	want := "Thread main (SIGABRT ABORT)\n    at ViewController.tap (MyApp)\n"
	if trace != want {
		t.Errorf("trace =\n%s\nwant\n%s", trace, want)
	}
}

func TestFormatTraceAndroidSkipsTheCrashedThread(t *testing.T) {
	frames := []*fc.Frame{{Symbol: "MainActivity.onCreate", File: "MainActivity.kt", Line: 12}}
	trace := formatTrace(&fc.Event{
		Exceptions: []*fc.Exception{{Type: "java.lang.IllegalStateException", ExceptionMessage: "boom", Frames: frames}},
		Threads:    []*fc.Thread{{Name: "main", Crashed: true, Frames: frames}},
	})

	want := "java.lang.IllegalStateException: boom\n    at MainActivity.onCreate (MainActivity.kt:12)\n"
	if trace != want {
		t.Errorf("trace =\n%s\nwant\n%s", trace, want)
	}
}
