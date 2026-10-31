package issue

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/MicroClub-USTHB/m-Seismo/backend/internal/db"
)

// fakeQuerier implements only the queries the service uses; anything else
// panics through the nil embedded interface. Every app shares the same
// targets; connections are per app.
type fakeQuerier struct {
	db.Querier
	targets []db.ListIssueTargetsForAppRow
	conns   map[uuid.UUID][]db.ProviderConnection
	created []db.CreateIssueParams
}

func (f *fakeQuerier) ListIssueTargetsForApp(_ context.Context, _ uuid.UUID) ([]db.ListIssueTargetsForAppRow, error) {
	return f.targets, nil
}

func (f *fakeQuerier) ListActiveConnectionsForApp(_ context.Context, appID uuid.UUID) ([]db.ProviderConnection, error) {
	return f.conns[appID], nil
}

func (f *fakeQuerier) GetIssueTarget(_ context.Context, arg db.GetIssueTargetParams) (db.GetIssueTargetRow, error) {
	if _, linked := f.conns[arg.AppID]; !linked {
		return db.GetIssueTargetRow{}, errors.New("no rows")
	}
	for _, t := range f.targets {
		if t.ProjectRepositoryID == arg.ID {
			return db.GetIssueTargetRow(t), nil
		}
	}
	return db.GetIssueTargetRow{}, errors.New("no rows")
}

func (f *fakeQuerier) CreateIssue(_ context.Context, arg db.CreateIssueParams) (db.Issue, error) {
	f.created = append(f.created, arg)
	return db.Issue{ID: uuid.New()}, nil
}

// fakeOpener fails for repositories listed in fail and rate-limits those in
// limited.
type fakeOpener struct {
	fail    map[string]bool
	limited map[string]bool
	calls   []string
	opened  []Issue
}

func (o *fakeOpener) Open(_ context.Context, in Issue) (Ref, error) {
	o.calls = append(o.calls, in.Repository)
	if o.fail[in.Repository] {
		return Ref{}, errors.New("tracker down")
	}
	if o.limited[in.Repository] {
		return Ref{}, &RateLimitError{Tracker: "github", Wait: 42 * time.Second, Err: errors.New("403")}
	}
	o.opened = append(o.opened, in)
	return Ref{ID: in.Repository + "#1", URL: "https://example.test/" + in.Repository}, nil
}

func (o *fakeOpener) Comment(context.Context, Ref, string) error { return nil }
func (o *fakeOpener) IsOpen(context.Context, Ref) (bool, error)  { return true, nil }

// fakeSource opens only on GitHub and covers like GitHub does: a connection
// reaches the repositories of its own account.
type fakeSource struct {
	opener *fakeOpener
	used   []Connection
}

func (s *fakeSource) Opener(_ context.Context, c Connection) (Opener, error) {
	if c.Tracker != db.TrackerGithub {
		return nil, errors.New("unsupported tracker")
	}
	s.used = append(s.used, c)
	return s.opener, nil
}

func (s *fakeSource) Covers(c Connection, repository string) bool {
	owner, _, _ := strings.Cut(repository, "/")
	return strings.EqualFold(owner, c.AccountLogin)
}

func newTarget(repo string, tracker db.Tracker) db.ListIssueTargetsForAppRow {
	return db.ListIssueTargetsForAppRow{
		ProjectRepositoryID: uuid.New(),
		ProjectID:           uuid.New(),
		Repository:          repo,
		Tracker:             tracker,
	}
}

func newConn(appID uuid.UUID, tracker db.Tracker, externalID, account string) db.ProviderConnection {
	return db.ProviderConnection{ID: uuid.New(), AppID: appID, Tracker: tracker, ExternalID: externalID, AccountLogin: account}
}

func TestOpenForApp(t *testing.T) {
	appID := uuid.New()
	gh := newConn(appID, db.TrackerGithub, "42", "octo")
	q := &fakeQuerier{
		targets: []db.ListIssueTargetsForAppRow{
			newTarget("octo/ok", db.TrackerGithub),
			newTarget("octo/down", db.TrackerGithub),
			newTarget("group/gl", db.TrackerGitlab),
		},
		conns: map[uuid.UUID][]db.ProviderConnection{appID: {gh, newConn(appID, db.TrackerGitlab, "9", "group")}},
	}
	op := &fakeOpener{fail: map[string]bool{"octo/down": true}}
	svc := NewService(q, &fakeSource{opener: op}, nil)

	results, err := svc.OpenForApp(context.Background(), appID, Draft{
		Title:       "Crash on login",
		Body:        "steps...",
		Labels:      []string{"bug"},
		Fingerprint: "fp-1",
	})
	if err != nil {
		t.Fatalf("partial failure should not fail the call: %v", err)
	}
	if len(results) != 3 {
		t.Fatalf("got %d results, want 3", len(results))
	}

	ok, down, gl := results[0], results[1], results[2]
	if ok.Err != nil || ok.Ref.ID != "octo/ok#1" || ok.IssueID == uuid.Nil || ok.ConnectionID != gh.ID {
		t.Errorf("ok result = %+v", ok)
	}
	if down.Err == nil || down.IssueID != uuid.Nil {
		t.Errorf("down result should fail: %+v", down)
	}
	if gl.Err == nil || errors.Is(gl.Err, ErrNoConnection) {
		t.Errorf("gitlab result should fail without an opener, not for lack of a connection: %+v", gl)
	}

	if len(q.created) != 1 {
		t.Fatalf("recorded %d issues, want 1", len(q.created))
	}
	rec := q.created[0]
	if rec.AppID == nil || *rec.AppID != appID || rec.Fingerprint == nil || *rec.Fingerprint != "fp-1" || rec.ExternalID != "octo/ok#1" {
		t.Errorf("recorded issue = %+v", rec)
	}
	if rec.ProviderConnectionID == nil || *rec.ProviderConnectionID != gh.ID {
		t.Errorf("recorded connection = %v, want %s", rec.ProviderConnectionID, gh.ID)
	}
	if got := op.opened[0]; got.Title != "Crash on login" || got.Labels[0] != "bug" {
		t.Errorf("opened issue = %+v", got)
	}
}

func TestOpenForAppAllFail(t *testing.T) {
	appID := uuid.New()
	q := &fakeQuerier{
		targets: []db.ListIssueTargetsForAppRow{newTarget("octo/down", db.TrackerGithub)},
		conns:   map[uuid.UUID][]db.ProviderConnection{appID: {newConn(appID, db.TrackerGithub, "42", "octo")}},
	}
	svc := NewService(q, &fakeSource{opener: &fakeOpener{fail: map[string]bool{"octo/down": true}}}, nil)

	results, err := svc.OpenForApp(context.Background(), appID, Draft{Title: "x"})
	if err == nil {
		t.Fatal("want error when every target fails")
	}
	if len(results) != 1 || results[0].Err == nil {
		t.Errorf("results = %+v", results)
	}
}

func TestOpenForAppNoTargets(t *testing.T) {
	svc := NewService(&fakeQuerier{}, &fakeSource{opener: &fakeOpener{}}, nil)
	if _, err := svc.OpenForApp(context.Background(), uuid.New(), Draft{Title: "x"}); !errors.Is(err, ErrNoTargets) {
		t.Fatalf("err = %v, want ErrNoTargets", err)
	}
}

func TestOpenRequiresTitle(t *testing.T) {
	svc := NewService(&fakeQuerier{}, &fakeSource{opener: &fakeOpener{}}, nil)
	if _, err := svc.OpenForApp(context.Background(), uuid.New(), Draft{}); err == nil {
		t.Fatal("want error for empty title")
	}
}

// A repository that none of the app's connections covers fails on its own,
// without calling the tracker, and the other targets still go through.
func TestOpenForAppWithoutConnection(t *testing.T) {
	appID := uuid.New()
	q := &fakeQuerier{
		targets: []db.ListIssueTargetsForAppRow{
			newTarget("octo/ok", db.TrackerGithub),
			newTarget("other-org/tools", db.TrackerGithub),
		},
		conns: map[uuid.UUID][]db.ProviderConnection{appID: {newConn(appID, db.TrackerGithub, "42", "octo")}},
	}
	op := &fakeOpener{}
	svc := NewService(q, &fakeSource{opener: op}, nil)

	results, err := svc.OpenForApp(context.Background(), appID, Draft{Title: "x"})
	if err != nil {
		t.Fatalf("one success should not fail the call: %v", err)
	}
	if results[0].Err != nil {
		t.Errorf("covered target failed: %v", results[0].Err)
	}
	if !errors.Is(results[1].Err, ErrNoConnection) || results[1].ConnectionID != uuid.Nil {
		t.Errorf("uncovered target = %+v, want ErrNoConnection", results[1])
	}
	if len(op.calls) != 1 || op.calls[0] != "octo/ok" {
		t.Errorf("tracker calls = %v; an uncovered repository must not be tried", op.calls)
	}
}

// Two apps reporting into the same project each open through their own
// connection, and each issue records the connection that opened it.
func TestEachAppUsesItsOwnConnection(t *testing.T) {
	appA, appB := uuid.New(), uuid.New()
	connA := newConn(appA, db.TrackerGithub, "42", "octo")
	connB := newConn(appB, db.TrackerGithub, "43", "octo")
	q := &fakeQuerier{
		targets: []db.ListIssueTargetsForAppRow{newTarget("octo/shared", db.TrackerGithub)},
		conns:   map[uuid.UUID][]db.ProviderConnection{appA: {connA}, appB: {connB}},
	}
	src := &fakeSource{opener: &fakeOpener{}}
	svc := NewService(q, src, nil)

	for _, app := range []uuid.UUID{appA, appB} {
		if _, err := svc.OpenForApp(context.Background(), app, Draft{Title: "x"}); err != nil {
			t.Fatal(err)
		}
	}
	if len(src.used) != 2 || src.used[0].ExternalID != "42" || src.used[1].ExternalID != "43" {
		t.Errorf("connections used = %+v, want 42 then 43", src.used)
	}
	if *q.created[0].ProviderConnectionID != connA.ID || *q.created[1].ProviderConnectionID != connB.ID {
		t.Errorf("recorded connections = %v, %v", *q.created[0].ProviderConnectionID, *q.created[1].ProviderConnectionID)
	}
}

func TestOpenInRepository(t *testing.T) {
	appID := uuid.New()
	conn := newConn(appID, db.TrackerGithub, "42", "octo")
	tg := newTarget("octo/ok", db.TrackerGithub)
	q := &fakeQuerier{
		targets: []db.ListIssueTargetsForAppRow{tg},
		conns:   map[uuid.UUID][]db.ProviderConnection{appID: {conn}},
	}
	svc := NewService(q, &fakeSource{opener: &fakeOpener{}}, nil)

	res, err := svc.OpenInRepository(context.Background(), appID, tg.ProjectRepositoryID, Draft{Title: "x"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Ref.ID != "octo/ok#1" || res.ConnectionID != conn.ID || len(q.created) != 1 || *q.created[0].AppID != appID {
		t.Errorf("res = %+v, created = %+v", res, q.created)
	}

	if _, err := svc.OpenInRepository(context.Background(), uuid.New(), tg.ProjectRepositoryID, Draft{Title: "x"}); err == nil {
		t.Error("an app not linked to the project must not open issues in it")
	}
}

func TestOpenInRepositoryWithoutConnection(t *testing.T) {
	appID := uuid.New()
	tg := newTarget("octo/ok", db.TrackerGithub)
	q := &fakeQuerier{
		targets: []db.ListIssueTargetsForAppRow{tg},
		conns:   map[uuid.UUID][]db.ProviderConnection{appID: {}},
	}
	svc := NewService(q, &fakeSource{opener: &fakeOpener{}}, nil)

	if _, err := svc.OpenInRepository(context.Background(), appID, tg.ProjectRepositoryID, Draft{Title: "x"}); !errors.Is(err, ErrNoConnection) {
		t.Fatalf("err = %v, want ErrNoConnection", err)
	}
}

func TestTargets(t *testing.T) {
	appID := uuid.New()
	conn := newConn(appID, db.TrackerGithub, "42", "Octo")
	q := &fakeQuerier{
		targets: []db.ListIssueTargetsForAppRow{
			newTarget("octo/ok", db.TrackerGithub),
			newTarget("octo/gl", db.TrackerGitlab),
		},
		conns: map[uuid.UUID][]db.ProviderConnection{appID: {conn}},
	}
	svc := NewService(q, &fakeSource{opener: &fakeOpener{}}, nil)

	ts, err := svc.Targets(context.Background(), appID)
	if err != nil {
		t.Fatal(err)
	}
	if ts[0].Connection == nil || ts[0].Connection.ID != conn.ID || ts[0].Err != nil {
		t.Errorf("github target = %+v", ts[0])
	}
	// The GitHub connection covers octo/ but is on another tracker.
	if ts[1].Connection != nil || !errors.Is(ts[1].Err, ErrNoConnection) {
		t.Errorf("gitlab target = %+v", ts[1])
	}

	empty, err := NewService(&fakeQuerier{}, &fakeSource{}, nil).Targets(context.Background(), appID)
	if err != nil || empty == nil || len(empty) != 0 {
		t.Errorf("no targets = %v, %v; want an empty slice", empty, err)
	}
}

func TestRateLimitedConnectionIsSkipped(t *testing.T) {
	appID := uuid.New()
	q := &fakeQuerier{
		targets: []db.ListIssueTargetsForAppRow{
			newTarget("octo/limited", db.TrackerGithub),
			newTarget("octo/same-installation", db.TrackerGithub),
			newTarget("acme/other-installation", db.TrackerGithub),
		},
		conns: map[uuid.UUID][]db.ProviderConnection{appID: {
			newConn(appID, db.TrackerGithub, "42", "octo"),
			newConn(appID, db.TrackerGithub, "7", "acme"),
		}},
	}
	op := &fakeOpener{limited: map[string]bool{"octo/limited": true}}
	svc := NewService(q, &fakeSource{opener: op}, nil)

	results, err := svc.OpenForApp(context.Background(), appID, Draft{Title: "x"})
	if err != nil {
		t.Fatalf("one success should not fail the call: %v", err)
	}

	var rl *RateLimitError
	if !errors.As(results[0].Err, &rl) || rl.RetryAfter() != 42*time.Second {
		t.Errorf("limited target: err = %v", results[0].Err)
	}
	if !errors.As(results[1].Err, &rl) {
		t.Errorf("same-connection target should be skipped with the rate-limit error, got %v", results[1].Err)
	}
	if results[2].Err != nil {
		t.Errorf("other connection should still be tried: %v", results[2].Err)
	}
	if len(op.calls) != 2 || op.calls[1] != "acme/other-installation" {
		t.Errorf("tracker calls = %v; the rate-limited connection must not be called again", op.calls)
	}
}
