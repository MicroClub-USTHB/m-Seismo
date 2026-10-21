package provider

import (
	"context"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/MicroClub-USTHB/m-Seismo/backend/internal/apperr"
	"github.com/MicroClub-USTHB/m-Seismo/backend/internal/db"
	"github.com/MicroClub-USTHB/m-Seismo/backend/internal/issue"
)

type memStates map[string]string

func (m memStates) Put(_ context.Context, k, v string, _ time.Duration) error {
	m[k] = v
	return nil
}

func (m memStates) Take(_ context.Context, k string) (string, bool, error) {
	v, ok := m[k]
	delete(m, k)
	return v, ok, nil
}

type fakeProvider struct{ account Account }

func (fakeProvider) Tracker() db.Tracker { return db.TrackerGithub }
func (fakeProvider) InstallURL(state string) string {
	return "https://tracker.test/install?state=" + state
}
func (f fakeProvider) CompleteInstall(context.Context, url.Values) (Account, error) {
	return f.account, nil
}
func (fakeProvider) ListRepositories(context.Context, string) ([]Repository, error) { return nil, nil }
func (fakeProvider) HasRepository(_ context.Context, _, repo string) (bool, error) {
	return repo == "octo/app", nil
}
func (fakeProvider) Opener(context.Context, string) (issue.Opener, error) { return nil, nil }

type fakeQuerier struct {
	db.Querier
	// takenBy simulates the installation already belonging to another owner.
	takenBy uuid.UUID
	saved   []db.UpsertProviderConnectionParams
	conns   map[uuid.UUID]db.ProviderConnection
}

func (f *fakeQuerier) UpsertProviderConnection(_ context.Context, arg db.UpsertProviderConnectionParams) (db.ProviderConnection, error) {
	if f.takenBy != uuid.Nil && f.takenBy != arg.OwnerID {
		return db.ProviderConnection{}, pgx.ErrNoRows
	}
	f.saved = append(f.saved, arg)
	return db.ProviderConnection{ID: uuid.New(), OwnerID: arg.OwnerID, Tracker: arg.Tracker, ExternalID: arg.ExternalID, AccountLogin: arg.AccountLogin}, nil
}

func (f *fakeQuerier) GetProviderConnection(_ context.Context, arg db.GetProviderConnectionParams) (db.ProviderConnection, error) {
	c, ok := f.conns[arg.ID]
	if !ok || c.OwnerID != arg.OwnerID {
		return db.ProviderConnection{}, pgx.ErrNoRows
	}
	return c, nil
}

func newTestService(q *fakeQuerier) (*Service, memStates) {
	states := memStates{}
	reg := NewRegistry(registryParams{Providers: []Provider{fakeProvider{account: Account{ExternalID: "42", Login: "octo", Type: "User"}}}})
	return NewService(q, reg, states), states
}

func stateFrom(t *testing.T, installURL string) string {
	t.Helper()
	u, err := url.Parse(installURL)
	if err != nil {
		t.Fatal(err)
	}
	return u.Query().Get("state")
}

func TestConnectFlow(t *testing.T) {
	q := &fakeQuerier{}
	svc, states := newTestService(q)
	ctx := context.Background()
	owner := uuid.New()

	u, err := svc.StartConnect(ctx, owner, db.TrackerGithub)
	if err != nil {
		t.Fatal(err)
	}
	state := stateFrom(t, u)
	if len(state) < 40 || len(states) != 1 {
		t.Fatalf("state %q not stored", state)
	}

	conn, err := svc.FinishConnect(ctx, db.TrackerGithub, url.Values{"state": {state}})
	if err != nil {
		t.Fatal(err)
	}
	if conn.AccountLogin != "octo" || len(q.saved) != 1 || q.saved[0].OwnerID != owner || q.saved[0].ExternalID != "42" {
		t.Errorf("conn = %+v, saved = %+v", conn, q.saved)
	}

	// A state is single-use.
	if _, err := svc.FinishConnect(ctx, db.TrackerGithub, url.Values{"state": {state}}); kindOf(err) != apperr.KindInvalid {
		t.Errorf("replayed state: err = %v", err)
	}
}

func TestConnectRejects(t *testing.T) {
	ctx := context.Background()

	t.Run("unknown tracker", func(t *testing.T) {
		svc, _ := newTestService(&fakeQuerier{})
		if _, err := svc.StartConnect(ctx, uuid.New(), db.TrackerGitlab); kindOf(err) != apperr.KindNotFound {
			t.Errorf("err = %v", err)
		}
	})

	t.Run("missing state", func(t *testing.T) {
		svc, _ := newTestService(&fakeQuerier{})
		if _, err := svc.FinishConnect(ctx, db.TrackerGithub, url.Values{}); kindOf(err) != apperr.KindInvalid {
			t.Errorf("err = %v", err)
		}
	})

	t.Run("state for another tracker", func(t *testing.T) {
		svc, states := newTestService(&fakeQuerier{})
		states[stateKeyPrefix+"s"] = "gitlab:" + uuid.NewString()
		if _, err := svc.FinishConnect(ctx, db.TrackerGithub, url.Values{"state": {"s"}}); kindOf(err) != apperr.KindInvalid {
			t.Errorf("err = %v", err)
		}
	})

	t.Run("installation owned by someone else", func(t *testing.T) {
		svc, _ := newTestService(&fakeQuerier{takenBy: uuid.New()})
		u, err := svc.StartConnect(ctx, uuid.New(), db.TrackerGithub)
		if err != nil {
			t.Fatal(err)
		}
		_, err = svc.FinishConnect(ctx, db.TrackerGithub, url.Values{"state": {stateFrom(t, u)}})
		if kindOf(err) != apperr.KindConflict {
			t.Errorf("err = %v", err)
		}
	})
}

func TestVerifyRepository(t *testing.T) {
	owner, other := uuid.New(), uuid.New()
	connID := uuid.New()
	suspendedID := uuid.New()
	now := time.Now()
	q := &fakeQuerier{conns: map[uuid.UUID]db.ProviderConnection{
		connID:      {ID: connID, OwnerID: owner, Tracker: db.TrackerGithub, ExternalID: "42"},
		suspendedID: {ID: suspendedID, OwnerID: owner, Tracker: db.TrackerGithub, ExternalID: "43", SuspendedAt: &now},
	}}
	svc, _ := newTestService(q)
	ctx := context.Background()

	if _, err := svc.VerifyRepository(ctx, owner, connID, "octo/app"); err != nil {
		t.Errorf("reachable repo: %v", err)
	}
	if _, err := svc.VerifyRepository(ctx, owner, connID, "octo/secret"); kindOf(err) != apperr.KindInvalid {
		t.Errorf("unreachable repo: err = %v", err)
	}
	if _, err := svc.VerifyRepository(ctx, other, connID, "octo/app"); kindOf(err) != apperr.KindNotFound {
		t.Errorf("other owner's connection: err = %v", err)
	}
	if _, err := svc.VerifyRepository(ctx, owner, suspendedID, "octo/app"); err == nil || !strings.Contains(err.Error(), "suspended") {
		t.Errorf("suspended connection: err = %v", err)
	}
}

func kindOf(err error) apperr.Kind {
	if err == nil {
		return -1
	}
	k, _ := apperr.Classify(err)
	return k
}
