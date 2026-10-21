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
	"github.com/MicroClub-USTHB/m-Seismo/backend/internal/db"
	"github.com/MicroClub-USTHB/m-Seismo/backend/internal/issue"
	seismov1 "github.com/MicroClub-USTHB/m-Seismo/backend/internal/pb/seismo/v1"
)

type fakeQueries struct {
	report     *db.Report
	targets    []db.ListIssueTargetsForAppRow
	deliveries []db.ReportDelivery
	processed  int
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

func (f *fakeQueries) ListIssueTargetsForApp(context.Context, uuid.UUID) ([]db.ListIssueTargetsForAppRow, error) {
	return f.targets, nil
}

func (f *fakeQueries) ListReportDeliveries(context.Context, uuid.UUID) ([]db.ReportDelivery, error) {
	return f.deliveries, nil
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

// fakeIssues opens issues, or fails with the error set for a repository.
type fakeIssues struct {
	fail   map[uuid.UUID]error
	opened []uuid.UUID
	drafts []issue.Draft
}

func (f *fakeIssues) OpenInRepository(_ context.Context, repo uuid.UUID, _ *uuid.UUID, d issue.Draft) (issue.Result, error) {
	if err := f.fail[repo]; err != nil {
		return issue.Result{Err: err}, err
	}
	f.opened = append(f.opened, repo)
	f.drafts = append(f.drafts, d)
	return issue.Result{IssueID: uuid.New(), Ref: issue.Ref{ID: "1", URL: "https://github.com/o/r/issues/1"}}, nil
}

func newFixture(t *testing.T, repos int) (*fakeQueries, *fakeIssues, *Processor) {
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
	q := &fakeQueries{report: &db.Report{ID: uuid.New(), AppID: uuid.New(), ReportID: uuid.New(), InstallationID: uuid.New(), Payload: payload}}
	for i := 0; i < repos; i++ {
		q.targets = append(q.targets, db.ListIssueTargetsForAppRow{ProjectRepositoryID: uuid.New(), Repository: "o/r"})
	}
	is := &fakeIssues{fail: map[uuid.UUID]error{}}
	return q, is, NewProcessor(q, is, nil, time.Minute)
}

func TestProcessOpensOneIssuePerRepository(t *testing.T) {
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
	p := NewProcessor(q, is, logs, 2*time.Minute)

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
		p := NewProcessor(q, is, &fakeLogs{err: err}, time.Minute)
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
	failing := q.targets[1].ProjectRepositoryID
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
	is.fail[q.targets[0].ProjectRepositoryID] = &issue.RateLimitError{Tracker: "github", Wait: time.Minute, Err: errors.New("403")}

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
