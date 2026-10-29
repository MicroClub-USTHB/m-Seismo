package worker

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
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

type fakeQueries struct {
	report     *db.Report
	router     *fakeRouter
	deliveries []db.ReportDelivery
	// projectOf is the project of each repository.
	projectOf map[uuid.UUID]uuid.UUID
	processed int
	// issues are the issues opened so far, as recorded by issue.Service.
	issues []db.Issue
}

func (f *fakeQueries) GetReport(_ context.Context, id uuid.UUID) (db.Report, error) {
	if f.report == nil || f.report.ID != id {
		return db.Report{}, pgx.ErrNoRows
	}
	return *f.report, nil
}

func (f *fakeQueries) ListReportAttachmentNames(context.Context, uuid.UUID) ([]string, error) {
	return []string{"screenshot"}, nil
}

func (f *fakeQueries) ListReportDeliveryProjects(_ context.Context, reportID uuid.UUID) ([]uuid.UUID, error) {
	var out []uuid.UUID
	for _, d := range f.deliveries {
		if d.ReportID == reportID {
			out = append(out, f.projectOf[d.ProjectRepositoryID])
		}
	}
	return out, nil
}

func (f *fakeQueries) CreateReportDelivery(_ context.Context, arg db.CreateReportDeliveryParams) error {
	f.deliveries = append(f.deliveries, db.ReportDelivery{ReportID: arg.ReportID, ProjectRepositoryID: arg.ProjectRepositoryID, IssueID: arg.IssueID, Url: arg.Url})
	return nil
}

func (f *fakeQueries) GetOpenIssueByFingerprint(_ context.Context, arg db.GetOpenIssueByFingerprintParams) (db.Issue, error) {
	for _, iss := range slices.Backward(f.issues) {
		if iss.ProjectRepositoryID == arg.ProjectRepositoryID && *iss.Fingerprint == *arg.Fingerprint && iss.State == "open" {
			return iss, nil
		}
	}
	return db.Issue{}, pgx.ErrNoRows
}

// GetIssueOccurrences counts the deliveries to the issue, plus the report
// being delivered; every report comes from another installation.
func (f *fakeQueries) GetIssueOccurrences(_ context.Context, arg db.GetIssueOccurrencesParams) (db.GetIssueOccurrencesRow, error) {
	n := int32(1)
	for _, d := range f.deliveries {
		if d.IssueID != nil && *d.IssueID == arg.IssueID {
			n++
		}
	}
	return db.GetIssueOccurrencesRow{Occurrences: n, Installations: n, Versions: []string{"2.3.1"}}, nil
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

// fakeIssues opens issues and records them in q, or fails with the error
// set for a repository. Issues listed in closed were closed on the tracker.
type fakeIssues struct {
	q        *fakeQueries
	fail     map[uuid.UUID]error
	closed   map[uuid.UUID]bool
	opened   []uuid.UUID
	drafts   []issue.Draft
	comments []string
}

func (f *fakeIssues) OpenInRepository(_ context.Context, _, repo uuid.UUID, d issue.Draft) (issue.Result, error) {
	if err := f.fail[repo]; err != nil {
		return issue.Result{Err: err}, err
	}
	f.opened = append(f.opened, repo)
	f.drafts = append(f.drafts, d)
	iss := db.Issue{ID: uuid.New(), ProjectRepositoryID: repo, ExternalID: "1", Url: "https://github.com/o/r/issues/1", State: "open", Fingerprint: &d.Fingerprint}
	f.q.issues = append(f.q.issues, iss)
	return issue.Result{IssueID: iss.ID, Ref: issue.Ref{ID: iss.ExternalID, URL: iss.Url}}, nil
}

func (f *fakeIssues) CommentOn(_ context.Context, _ uuid.UUID, iss db.Issue, body string) (bool, error) {
	if f.closed[iss.ID] {
		return false, nil
	}
	f.comments = append(f.comments, body)
	return true, nil
}

// fakeLocks records the keys locked and checks each is released.
type fakeLocks struct {
	keys []string
	held int
}

func (f *fakeLocks) Lock(_ context.Context, key string) (func(), error) {
	f.keys = append(f.keys, key)
	f.held++
	return func() { f.held-- }, nil
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
	is := &fakeIssues{q: q, fail: map[uuid.UUID]error{}, closed: map[uuid.UUID]bool{}}
	return q, is, NewProcessor(q, is, router, &fakeLocks{}, nil, time.Minute, nil)
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

// crashReport turns the fixture's report into a crash, a new report each
// call, from the same bug.
func crashReport(t *testing.T, q *fakeQueries) {
	t.Helper()
	payload, _ := json.Marshal(map[string]any{
		"platform": "android",
		"trigger":  "crash",
		"release":  map[string]string{"version": "2.3.1", "build": "87"},
		"crash": map[string]any{
			"type":        "java.lang.IllegalStateException",
			"reason":      "Fragment not attached",
			"stack_trace": []string{"com.example.ProfileFragment.onResume(ProfileFragment.kt:88)"},
		},
	})
	q.report = &db.Report{ID: uuid.New(), AppID: q.report.AppID, ReportID: uuid.New(), InstallationID: uuid.New(), Payload: payload}
}

func TestProcessGroupsReportsOfTheSameBug(t *testing.T) {
	q, is, p := newFixture(t, 1)
	locks := p.locks.(*fakeLocks)

	crashReport(t, q)
	if err := p.Process(context.Background(), q.report.ID); err != nil {
		t.Fatal(err)
	}
	first := q.issues[0]
	if *first.Fingerprint == "" || len(is.opened) != 1 {
		t.Fatalf("first crash: opened %d issues with fingerprint %q", len(is.opened), *first.Fingerprint)
	}

	for range 2 {
		crashReport(t, q)
		if err := p.Process(context.Background(), q.report.ID); err != nil {
			t.Fatal(err)
		}
	}
	if len(is.opened) != 1 || len(is.comments) != 2 {
		t.Fatalf("opened %d issues and %d comments, want 1 and 2", len(is.opened), len(is.comments))
	}
	if !strings.Contains(is.comments[1], "**Seen again**: 3 reports, 3 installations · versions 2.3.1") {
		t.Errorf("comment has no counts:\n%s", is.comments[1])
	}
	if d := q.deliveries[2]; d.IssueID == nil || *d.IssueID != first.ID || d.Url != first.Url {
		t.Errorf("delivery = %+v, want the first issue", d)
	}
	if len(locks.keys) != 3 || locks.keys[0] != q.router.routes[0].ProjectRepositoryID.String()+"/"+*first.Fingerprint || locks.held != 0 {
		t.Errorf("locks %v, still held %d", locks.keys, locks.held)
	}
}

func TestProcessOpensAgainWhenTheIssueWasClosed(t *testing.T) {
	q, is, p := newFixture(t, 1)
	crashReport(t, q)
	if err := p.Process(context.Background(), q.report.ID); err != nil {
		t.Fatal(err)
	}
	is.closed[q.issues[0].ID] = true

	crashReport(t, q)
	if err := p.Process(context.Background(), q.report.ID); err != nil {
		t.Fatal(err)
	}
	if len(is.opened) != 2 || len(is.comments) != 0 {
		t.Errorf("opened %d issues and %d comments, want a new issue", len(is.opened), len(is.comments))
	}
}

func TestProcessNeverGroupsReportsWithoutCrash(t *testing.T) {
	q, is, p := newFixture(t, 1)
	for range 2 {
		q.report.ID, q.report.ProcessedAt = uuid.New(), nil
		if err := p.Process(context.Background(), q.report.ID); err != nil {
			t.Fatal(err)
		}
	}
	if len(is.opened) != 2 || len(p.locks.(*fakeLocks).keys) != 0 {
		t.Errorf("opened %d issues, locked %v; want 2 issues and no lock", len(is.opened), p.locks.(*fakeLocks).keys)
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
	p := NewProcessor(q, is, q.router, &fakeLocks{}, logs, 2*time.Minute, nil)

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
		p := NewProcessor(q, is, q.router, &fakeLocks{}, &fakeLogs{err: err}, time.Minute, nil)
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
