package worker

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/MicroClub-USTHB/m-Seismo/backend/internal/coordinator"
	"github.com/MicroClub-USTHB/m-Seismo/backend/internal/crashprovider"
	"github.com/MicroClub-USTHB/m-Seismo/backend/internal/db"
	"github.com/MicroClub-USTHB/m-Seismo/backend/internal/ingest"
	"github.com/MicroClub-USTHB/m-Seismo/backend/internal/issue"
	seismov1 "github.com/MicroClub-USTHB/m-Seismo/backend/internal/pb/seismo/v1"
	"github.com/MicroClub-USTHB/m-Seismo/backend/internal/routing"
)

// screenshotAttachmentID is the fixed id ListReportAttachments answers
// with, so tests can assert on the link fakeLinker built from it.
var screenshotAttachmentID = uuid.MustParse("00000000-0000-0000-0000-00000000a77a")

// fakeLinker returns a fixed, recognizable URL for any attachment id.
type fakeLinker struct{}

func (fakeLinker) SignedURL(id uuid.UUID) string {
	return "https://seismo.example.com/attachments/" + id.String()
}

type fakeQueries struct {
	report     *db.Report
	router     *fakeRouter
	deliveries []db.ReportDelivery
	// projectOf is the project of each repository.
	projectOf map[uuid.UUID]uuid.UUID
	processed int
}

func (f *fakeQueries) GetReport(_ context.Context, id uuid.UUID) (db.Report, error) {
	if f.report == nil || f.report.ID != id {
		return db.Report{}, pgx.ErrNoRows
	}
	return *f.report, nil
}

func (f *fakeQueries) ListReportAttachments(context.Context, uuid.UUID) ([]db.ListReportAttachmentsRow, error) {
	return []db.ListReportAttachmentsRow{{ID: screenshotAttachmentID, Name: ingest.ScreenshotName}}, nil
}

func (f *fakeQueries) ListReportDeliveryProjects(context.Context, uuid.UUID) ([]uuid.UUID, error) {
	var out []uuid.UUID
	for _, d := range f.deliveries {
		out = append(out, f.projectOf[d.ProjectRepositoryID])
	}
	return out, nil
}

func (f *fakeQueries) CreateReportDelivery(_ context.Context, arg db.CreateReportDeliveryParams) error {
	f.deliveries = append(f.deliveries, db.ReportDelivery{ReportID: arg.ReportID, ProjectRepositoryID: arg.ProjectRepositoryID, IssueID: arg.IssueID, Url: arg.Url})
	return nil
}

func (f *fakeQueries) MarkReportProcessed(context.Context, uuid.UUID) error {
	f.processed++
	now := time.Now()
	f.report.ProcessedAt = &now
	return nil
}

// fakeRouter routes every report the same way and keeps the facts it got.
type fakeRouter struct {
	routes []routing.Route
	facts  []routing.Facts
}

func (f *fakeRouter) Routes(_ context.Context, facts routing.Facts) ([]routing.Route, error) {
	f.facts = append(f.facts, facts)
	return f.routes, nil
}

// fakeIssues opens issues, or fails with the error set for a repository.
type fakeIssues struct {
	fail   map[uuid.UUID]error
	opened []uuid.UUID
	drafts []issue.Draft
}

func (f *fakeIssues) OpenInRepository(_ context.Context, _, repo uuid.UUID, d issue.Draft) (issue.Result, error) {
	if err := f.fail[repo]; err != nil {
		return issue.Result{Err: err}, err
	}
	f.opened = append(f.opened, repo)
	f.drafts = append(f.drafts, d)
	return issue.Result{IssueID: uuid.New(), Ref: issue.Ref{ID: "1", URL: "https://github.com/o/r/issues/1"}}, nil
}

// newFixture routes the report to the default repository of `projects`
// projects.
func newFixture(t *testing.T, projects int) (*fakeQueries, *fakeIssues, *Processor) {
	t.Helper()
	payload, _ := json.Marshal(map[string]any{
		"platform": "ios",
		"trigger":  "shake",
		"release":  map[string]string{"version": "2.3.1", "build": "87"},
		"user_input": map[string]string{
			"title":       "Pay button does nothing",
			"description": "I tapped pay",
		},
	})
	router := &fakeRouter{}
	q := &fakeQueries{router: router, projectOf: map[uuid.UUID]uuid.UUID{}, report: &db.Report{ID: uuid.New(), AppID: uuid.New(), ReportID: uuid.New(), InstallationID: uuid.New(), Payload: payload}}
	for i := 0; i < projects; i++ {
		route := routing.Route{ProjectID: uuid.New(), ProjectRepositoryID: uuid.New(), Repository: "o/r"}
		router.routes = append(router.routes, route)
		q.projectOf[route.ProjectRepositoryID] = route.ProjectID
	}
	is := &fakeIssues{fail: map[uuid.UUID]error{}}
	return q, is, NewProcessor(q, is, router, nil, time.Minute, nil, fakeLinker{})
}

func TestProcessOpensOneIssuePerProject(t *testing.T) {
	q, is, p := newFixture(t, 2)
	if err := p.Process(context.Background(), q.report.ID); err != nil {
		t.Fatal(err)
	}
	if len(is.opened) != 2 || len(q.deliveries) != 2 || q.processed != 1 {
		t.Fatalf("opened %d, recorded %d, processed %d; want 2, 2, 1", len(is.opened), len(q.deliveries), q.processed)
	}
	if is.drafts[0].Title != "Pay button does nothing" {
		t.Errorf("title = %q", is.drafts[0].Title)
	}

	// Delivered again by the queue: nothing new is opened.
	if err := p.Process(context.Background(), q.report.ID); err != nil {
		t.Fatal(err)
	}
	if len(is.opened) != 2 {
		t.Errorf("opened %d issues after a redelivery, want 2", len(is.opened))
	}
}

func TestProcessUsesTheRoute(t *testing.T) {
	q, is, p := newFixture(t, 1)
	route := &q.router.routes[0]
	route.Labels, route.Assignees, route.Milestone = []string{"bug", "ios"}, []string{"octocat"}, "v2.4"

	if err := p.Process(context.Background(), q.report.ID); err != nil {
		t.Fatal(err)
	}
	if f := q.router.facts[0]; f.AppID != q.report.AppID {
		t.Errorf("routed with app %s, want %s", f.AppID, q.report.AppID)
	}
	if len(is.opened) != 1 || is.opened[0] != route.ProjectRepositoryID {
		t.Fatalf("opened %v, want the route's repository", is.opened)
	}
	d := is.drafts[0]
	if strings.Join(d.Labels, ",") != "bug,ios" || strings.Join(d.Assignees, ",") != "octocat" || d.Milestone != "v2.4" {
		t.Errorf("labels %v, assignees %v, milestone %q", d.Labels, d.Assignees, d.Milestone)
	}
}

func TestProcessSkipsProjectsAlreadyDelivered(t *testing.T) {
	q, is, p := newFixture(t, 1)
	if err := p.Process(context.Background(), q.report.ID); err != nil {
		t.Fatal(err)
	}

	// A rule changed since and sends the project's issues to another of
	// its repositories, but the project already has this report's issue.
	q.report.ProcessedAt = nil
	moved := uuid.New()
	q.projectOf[moved] = q.router.routes[0].ProjectID
	q.router.routes[0].ProjectRepositoryID = moved
	if err := p.Process(context.Background(), q.report.ID); err != nil {
		t.Fatal(err)
	}
	if len(is.opened) != 1 {
		t.Errorf("opened %d issues, want 1", len(is.opened))
	}
}

type fakeLogs struct {
	calls    int
	from, to time.Time
	err      error
}

func (f *fakeLogs) QueryLogs(_ context.Context, _ uuid.UUID, from, to time.Time) (*seismov1.AggregateResult, error) {
	f.calls++
	f.from, f.to = from, to
	if f.err != nil {
		return nil, f.err
	}
	return &seismov1.AggregateResult{Agents: []*seismov1.AgentSection{{AgentId: "prod-1"}}}, nil
}

func TestProcessAddsServerLogs(t *testing.T) {
	q, is, _ := newFixture(t, 1)
	q.report.OccurredAt = time.Date(2026, 10, 21, 14, 0, 0, 0, time.UTC)
	logs := &fakeLogs{}
	p := NewProcessor(q, is, q.router, logs, 2*time.Minute, nil, fakeLinker{})

	if err := p.Process(context.Background(), q.report.ID); err != nil {
		t.Fatal(err)
	}
	if !logs.from.Equal(q.report.OccurredAt.Add(-2*time.Minute)) || !logs.to.Equal(q.report.OccurredAt.Add(2*time.Minute)) {
		t.Errorf("window = [%s, %s]", logs.from, logs.to)
	}
	if !strings.Contains(is.drafts[0].Body, "**Agent `prod-1`**") {
		t.Errorf("issue has no server logs:\n%s", is.drafts[0].Body)
	}

	// Nothing left to open: the coordinator is not asked again.
	if err := p.Process(context.Background(), q.report.ID); err != nil {
		t.Fatal(err)
	}
	if logs.calls != 1 {
		t.Errorf("asked for logs %d times, want 1", logs.calls)
	}
}

func TestProcessWithoutServerLogs(t *testing.T) {
	for _, err := range []error{coordinator.ErrNotAttached, errors.New("NATS is down")} {
		q, is, _ := newFixture(t, 1)
		p := NewProcessor(q, is, q.router, &fakeLogs{err: err}, time.Minute, nil, fakeLinker{})
		if err := p.Process(context.Background(), q.report.ID); err != nil {
			t.Fatalf("logs failing with %v failed the report: %v", err, err)
		}
		if len(is.opened) != 1 || strings.Contains(is.drafts[0].Body, "Server logs") {
			t.Errorf("logs failing with %v: opened %d issues", err, len(is.opened))
		}
	}
}

func TestProcessRetriesOnlyFailedRepositories(t *testing.T) {
	q, is, p := newFixture(t, 2)
	failing := q.router.routes[1].ProjectRepositoryID
	is.fail[failing] = errors.New("github is down")

	err := p.Process(context.Background(), q.report.ID)
	var perm *permanentError
	if err == nil || errors.As(err, &perm) {
		t.Fatalf("got %v, want a temporary error", err)
	}
	if len(q.deliveries) != 1 || q.processed != 0 {
		t.Fatalf("recorded %d, processed %d; want 1, 0", len(q.deliveries), q.processed)
	}

	delete(is.fail, failing)
	if err := p.Process(context.Background(), q.report.ID); err != nil {
		t.Fatal(err)
	}
	if len(is.opened) != 2 || is.opened[1] != failing || q.processed != 1 {
		t.Errorf("opened %v, processed %d; want the failed repository opened once more", is.opened, q.processed)
	}
}

func TestProcessStopsOnRateLimit(t *testing.T) {
	q, is, p := newFixture(t, 2)
	is.fail[q.router.routes[0].ProjectRepositoryID] = &issue.RateLimitError{Tracker: "github", Wait: time.Minute, Err: errors.New("403")}

	err := p.Process(context.Background(), q.report.ID)
	var rl *issue.RateLimitError
	if !errors.As(err, &rl) || rl.Wait != time.Minute {
		t.Fatalf("got %v, want the rate limit error", err)
	}
	if len(is.opened) != 0 {
		t.Errorf("opened %d issues after a rate limit, want 0", len(is.opened))
	}
}

func TestProcessPermanentErrors(t *testing.T) {
	t.Run("report not found", func(t *testing.T) {
		_, _, p := newFixture(t, 1)
		assertPermanent(t, p.Process(context.Background(), uuid.New()))
	})
	t.Run("no target repository", func(t *testing.T) {
		q, _, p := newFixture(t, 0)
		err := p.Process(context.Background(), q.report.ID)
		assertPermanent(t, err)
		if !errors.Is(err, issue.ErrNoTargets) {
			t.Errorf("got %v, want ErrNoTargets", err)
		}
	})
	t.Run("unreadable payload", func(t *testing.T) {
		q, _, p := newFixture(t, 1)
		q.report.Payload = []byte("{")
		assertPermanent(t, p.Process(context.Background(), q.report.ID))
	})
}

func assertPermanent(t *testing.T, err error) {
	t.Helper()
	var perm *permanentError
	if !errors.As(err, &perm) {
		t.Fatalf("got %v, want a permanent error", err)
	}
}

type fakeCrashResolver struct {
	event *crashprovider.Event
	err   error
	app   uuid.UUID
}

func (f *fakeCrashResolver) ForReport(_ context.Context, app uuid.UUID, _ *ingest.Report) (*crashprovider.Event, error) {
	f.app = app
	return f.event, f.err
}
func TestProcessSentryOnlyReportIncludesCrashDetailsAndSurvivesProviderFailure(t *testing.T) {
	for _, unavailable := range []bool{false, true} {
		q, is, p := newFixture(t, 1)
		id := strings.Repeat("a", 32)
		q.report.Payload = []byte(`{"platform":"web","trigger":"crash","sentry":{"event_id":"` + id + `"}}`)
		resolver := &fakeCrashResolver{event: &crashprovider.Event{EventID: id, URL: "https://sentry.example/organizations/club/issues/?query=" + id, Title: "Unhandled error", StackTrace: []string{"render (app.js:12)"}}}
		if unavailable {
			resolver.err = errors.New("provider unavailable")
			resolver.event.Title = ""
			resolver.event.StackTrace = nil
		}
		p.crashes = resolver
		if err := p.Process(context.Background(), q.report.ID); err != nil {
			t.Fatal(err)
		}
		if resolver.app != q.report.AppID {
			t.Fatal("resolver used another app")
		}
		for _, text := range []string{"### Crash", id, "[View in Sentry](" + resolver.event.URL + ")"} {
			if !strings.Contains(is.drafts[0].Body, text) {
				t.Fatalf("missing %q in issue", text)
			}
		}
		if !unavailable && !strings.Contains(is.drafts[0].Body, "render (app.js:12)") {
			t.Fatal("resolved stack missing")
		}
		if q.processed != 1 {
			t.Fatal("Sentry failure prevented issue publication")
		}
	}
}
