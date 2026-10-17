package issue

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"

	"github.com/MicroClub-USTHB/m-Seismo/backend/internal/db"
)

// fakeQuerier implements only the queries the service uses; anything else
// panics through the nil embedded interface.
type fakeQuerier struct {
	db.Querier
	targets []db.ListIssueTargetsForAppRow
	created []db.CreateIssueParams
}

func (f *fakeQuerier) ListIssueTargetsForApp(_ context.Context, _ uuid.UUID) ([]db.ListIssueTargetsForAppRow, error) {
	return f.targets, nil
}

func (f *fakeQuerier) GetIssueTarget(_ context.Context, id uuid.UUID) (db.GetIssueTargetRow, error) {
	for _, t := range f.targets {
		if t.ProjectRepositoryID == id {
			return db.GetIssueTargetRow(t), nil
		}
	}
	return db.GetIssueTargetRow{}, errors.New("no rows")
}

func (f *fakeQuerier) CreateIssue(_ context.Context, arg db.CreateIssueParams) (db.Issue, error) {
	f.created = append(f.created, arg)
	return db.Issue{ID: uuid.New()}, nil
}

// fakeOpener fails for repositories listed in fail.
type fakeOpener struct {
	fail   map[string]bool
	opened []Issue
}

func (o *fakeOpener) Open(_ context.Context, in Issue) (Ref, error) {
	if o.fail[in.Repository] {
		return Ref{}, errors.New("tracker down")
	}
	o.opened = append(o.opened, in)
	return Ref{ID: in.Repository + "#1", URL: "https://example.test/" + in.Repository}, nil
}

func (o *fakeOpener) Comment(context.Context, Ref, string) error { return nil }
func (o *fakeOpener) IsOpen(context.Context, Ref) (bool, error)  { return true, nil }

type fakeSource struct{ opener *fakeOpener }

func (s fakeSource) Opener(_ context.Context, tracker db.Tracker, _ string) (Opener, error) {
	if tracker != db.TrackerGithub {
		return nil, errors.New("unsupported tracker")
	}
	return s.opener, nil
}

func newTarget(repo string, tracker db.Tracker) db.ListIssueTargetsForAppRow {
	return db.ListIssueTargetsForAppRow{
		ProjectRepositoryID: uuid.New(),
		ProjectID:           uuid.New(),
		Repository:          repo,
		Tracker:             tracker,
		ExternalID:          "42",
	}
}

func TestOpenForApp(t *testing.T) {
	q := &fakeQuerier{targets: []db.ListIssueTargetsForAppRow{
		newTarget("octo/ok", db.TrackerGithub),
		newTarget("octo/down", db.TrackerGithub),
		newTarget("group/gl", db.TrackerGitlab),
	}}
	op := &fakeOpener{fail: map[string]bool{"octo/down": true}}
	svc := NewService(q, fakeSource{opener: op})
	appID := uuid.New()

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
	if ok.Err != nil || ok.Ref.ID != "octo/ok#1" || ok.IssueID == uuid.Nil {
		t.Errorf("ok result = %+v", ok)
	}
	if down.Err == nil || down.IssueID != uuid.Nil {
		t.Errorf("down result should fail: %+v", down)
	}
	if gl.Err == nil {
		t.Errorf("gitlab result should fail without an opener: %+v", gl)
	}

	if len(q.created) != 1 {
		t.Fatalf("recorded %d issues, want 1", len(q.created))
	}
	rec := q.created[0]
	if rec.AppID == nil || *rec.AppID != appID || rec.Fingerprint == nil || *rec.Fingerprint != "fp-1" || rec.ExternalID != "octo/ok#1" {
		t.Errorf("recorded issue = %+v", rec)
	}
	if got := op.opened[0]; got.Title != "Crash on login" || got.Labels[0] != "bug" {
		t.Errorf("opened issue = %+v", got)
	}
}

func TestOpenForAppAllFail(t *testing.T) {
	q := &fakeQuerier{targets: []db.ListIssueTargetsForAppRow{newTarget("octo/down", db.TrackerGithub)}}
	svc := NewService(q, fakeSource{opener: &fakeOpener{fail: map[string]bool{"octo/down": true}}})

	results, err := svc.OpenForApp(context.Background(), uuid.New(), Draft{Title: "x"})
	if err == nil {
		t.Fatal("want error when every target fails")
	}
	if len(results) != 1 || results[0].Err == nil {
		t.Errorf("results = %+v", results)
	}
}

func TestOpenForAppNoTargets(t *testing.T) {
	svc := NewService(&fakeQuerier{}, fakeSource{opener: &fakeOpener{}})
	if _, err := svc.OpenForApp(context.Background(), uuid.New(), Draft{Title: "x"}); !errors.Is(err, ErrNoTargets) {
		t.Fatalf("err = %v, want ErrNoTargets", err)
	}
}

func TestOpenRequiresTitle(t *testing.T) {
	svc := NewService(&fakeQuerier{}, fakeSource{opener: &fakeOpener{}})
	if _, err := svc.OpenForApp(context.Background(), uuid.New(), Draft{}); err == nil {
		t.Fatal("want error for empty title")
	}
}

func TestOpenInRepository(t *testing.T) {
	tg := newTarget("octo/ok", db.TrackerGithub)
	q := &fakeQuerier{targets: []db.ListIssueTargetsForAppRow{tg}}
	svc := NewService(q, fakeSource{opener: &fakeOpener{}})

	res, err := svc.OpenInRepository(context.Background(), tg.ProjectRepositoryID, nil, Draft{Title: "x"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Ref.ID != "octo/ok#1" || len(q.created) != 1 || q.created[0].AppID != nil {
		t.Errorf("res = %+v, created = %+v", res, q.created)
	}
}
