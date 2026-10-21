package provider

import (
	"context"
	"errors"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/MicroClub-USTHB/m-Seismo/backend/internal/apperr"
	"github.com/MicroClub-USTHB/m-Seismo/backend/internal/db"
	"github.com/MicroClub-USTHB/m-Seismo/backend/internal/issue"
	"github.com/MicroClub-USTHB/m-Seismo/backend/internal/secrets"
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

// memSecrets is an in-memory SecretStore.
type memSecrets map[uuid.UUID][]byte

func (m memSecrets) Load(_ context.Context, id uuid.UUID) ([]byte, error) {
	v, ok := m[id]
	if !ok {
		return nil, ErrNoSecret
	}
	return v, nil
}

func (m memSecrets) Save(_ context.Context, id uuid.UUID, v []byte) error {
	m[id] = v
	return nil
}

type fakeProvider struct {
	account Account
	listed  []issue.Connection
}

func (*fakeProvider) Tracker() db.Tracker { return db.TrackerGithub }
func (*fakeProvider) InstallURL(state string) string {
	return "https://tracker.test/install?state=" + state
}
func (f *fakeProvider) CompleteInstall(context.Context, url.Values) (Account, error) {
	return f.account, nil
}
func (f *fakeProvider) ListRepositories(_ context.Context, c issue.Connection) ([]Repository, error) {
	f.listed = append(f.listed, c)
	return []Repository{{FullName: "octo/app"}}, nil
}
func (*fakeProvider) HasRepository(context.Context, issue.Connection, string) (bool, error) {
	return true, nil
}
func (*fakeProvider) ValidateRepository(string) error { return nil }
func (*fakeProvider) Covers(c issue.Connection, repo string) bool {
	return strings.HasPrefix(repo, c.AccountLogin+"/")
}
func (*fakeProvider) Opener(context.Context, issue.Connection) (issue.Opener, error) { return nil, nil }

// fakeQuerier keeps apps (app → owner) and connections in memory and
// enforces the rule the database enforces with an exclusion constraint: a
// tracker account belongs to one owner.
type fakeQuerier struct {
	db.Querier
	apps    map[uuid.UUID]uuid.UUID
	conns   map[uuid.UUID]db.ProviderConnection
	secrets memSecrets
}

func newFakeQuerier() *fakeQuerier {
	return &fakeQuerier{apps: map[uuid.UUID]uuid.UUID{}, conns: map[uuid.UUID]db.ProviderConnection{}, secrets: memSecrets{}}
}

func (f *fakeQuerier) addApp(owner uuid.UUID) uuid.UUID {
	id := uuid.New()
	f.apps[id] = owner
	return id
}

func (f *fakeQuerier) addConn(c db.ProviderConnection) db.ProviderConnection {
	c.ID = uuid.New()
	f.conns[c.ID] = c
	return c
}

func (f *fakeQuerier) GetApp(_ context.Context, arg db.GetAppParams) (db.App, error) {
	if owner, ok := f.apps[arg.ID]; ok && owner == arg.OwnerID {
		return db.App{ID: arg.ID, OwnerID: owner}, nil
	}
	return db.App{}, pgx.ErrNoRows
}

func (f *fakeQuerier) UpsertProviderConnection(_ context.Context, arg db.UpsertProviderConnectionParams) (db.ProviderConnection, error) {
	for id, c := range f.conns {
		if c.Tracker != arg.Tracker || c.ExternalID != arg.ExternalID {
			continue
		}
		if c.OwnerID != arg.OwnerID {
			return db.ProviderConnection{}, &pgconn.PgError{Code: "23P01"}
		}
		if c.AppID == arg.AppID {
			c.AccountLogin = arg.AccountLogin
			f.conns[id] = c
			return c, nil
		}
	}
	return f.addConn(db.ProviderConnection{
		OwnerID: arg.OwnerID, AppID: arg.AppID, Tracker: arg.Tracker,
		ExternalID: arg.ExternalID, AccountLogin: arg.AccountLogin, AccountType: arg.AccountType,
	}), nil
}

func (f *fakeQuerier) GetOwnerConnection(_ context.Context, arg db.GetOwnerConnectionParams) (db.ProviderConnection, error) {
	if c, ok := f.conns[arg.ID]; ok && c.OwnerID == arg.OwnerID {
		return c, nil
	}
	return db.ProviderConnection{}, pgx.ErrNoRows
}

func (f *fakeQuerier) GetAppConnection(_ context.Context, arg db.GetAppConnectionParams) (db.ProviderConnection, error) {
	if c, ok := f.conns[arg.ID]; ok && c.AppID == arg.AppID && c.OwnerID == arg.OwnerID {
		return c, nil
	}
	return db.ProviderConnection{}, pgx.ErrNoRows
}

func newTestService(q *fakeQuerier) (*Service, memStates, *fakeProvider) {
	states := memStates{}
	p := &fakeProvider{account: Account{ExternalID: "42", Login: "octo", Type: "User"}}
	reg := NewRegistry(registryParams{Providers: []Provider{p}})
	return NewService(q, reg, states, q.secrets), states, p
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
	q := newFakeQuerier()
	svc, states, _ := newTestService(q)
	ctx := context.Background()
	owner := uuid.New()
	app := q.addApp(owner)

	u, err := svc.StartConnect(ctx, owner, app, db.TrackerGithub)
	if err != nil {
		t.Fatal(err)
	}
	state := stateFrom(t, u)
	if len(state) < 40 || len(states) != 1 {
		t.Fatalf("state %q not stored", state)
	}

	gotApp, conn, err := svc.FinishConnect(ctx, db.TrackerGithub, url.Values{"state": {state}})
	if err != nil {
		t.Fatal(err)
	}
	saved := q.conns[conn.ID]
	if gotApp != app || conn.AppID != app || conn.AccountLogin != "octo" || saved.OwnerID != owner || saved.ExternalID != "42" {
		t.Errorf("app = %s, conn = %+v, saved = %+v", gotApp, conn, saved)
	}

	// A state is single-use.
	if _, _, err := svc.FinishConnect(ctx, db.TrackerGithub, url.Values{"state": {state}}); kindOf(err) != apperr.KindInvalid {
		t.Errorf("replayed state: err = %v", err)
	}
}

func TestStartConnectRequiresOwnApp(t *testing.T) {
	q := newFakeQuerier()
	svc, states, _ := newTestService(q)
	app := q.addApp(uuid.New())

	if _, err := svc.StartConnect(context.Background(), uuid.New(), app, db.TrackerGithub); kindOf(err) != apperr.KindNotFound {
		t.Errorf("another owner's app: err = %v", err)
	}
	if len(states) != 0 {
		t.Error("no state may be stored for an app the owner does not have")
	}
}

func TestConnectRejects(t *testing.T) {
	ctx := context.Background()

	t.Run("unknown tracker", func(t *testing.T) {
		q := newFakeQuerier()
		svc, _, _ := newTestService(q)
		owner := uuid.New()
		if _, err := svc.StartConnect(ctx, owner, q.addApp(owner), db.TrackerGitlab); kindOf(err) != apperr.KindNotFound {
			t.Errorf("err = %v", err)
		}
	})

	t.Run("missing state", func(t *testing.T) {
		svc, _, _ := newTestService(newFakeQuerier())
		if _, _, err := svc.FinishConnect(ctx, db.TrackerGithub, url.Values{}); kindOf(err) != apperr.KindInvalid {
			t.Errorf("err = %v", err)
		}
	})

	t.Run("state for another tracker", func(t *testing.T) {
		svc, states, _ := newTestService(newFakeQuerier())
		states[stateKeyPrefix+"s"] = "gitlab:" + uuid.NewString() + ":" + uuid.NewString()
		if _, _, err := svc.FinishConnect(ctx, db.TrackerGithub, url.Values{"state": {"s"}}); kindOf(err) != apperr.KindInvalid {
			t.Errorf("err = %v", err)
		}
	})

	t.Run("state without an app", func(t *testing.T) {
		svc, states, _ := newTestService(newFakeQuerier())
		states[stateKeyPrefix+"s"] = "github:" + uuid.NewString()
		if _, _, err := svc.FinishConnect(ctx, db.TrackerGithub, url.Values{"state": {"s"}}); kindOf(err) != apperr.KindInvalid {
			t.Errorf("err = %v", err)
		}
	})

	t.Run("account held by another owner", func(t *testing.T) {
		q := newFakeQuerier()
		svc, _, _ := newTestService(q)
		otherOwner := uuid.New()
		q.addConn(db.ProviderConnection{OwnerID: otherOwner, AppID: q.addApp(otherOwner), Tracker: db.TrackerGithub, ExternalID: "42"})

		owner := uuid.New()
		app := q.addApp(owner)
		u, err := svc.StartConnect(ctx, owner, app, db.TrackerGithub)
		if err != nil {
			t.Fatal(err)
		}
		gotApp, _, err := svc.FinishConnect(ctx, db.TrackerGithub, url.Values{"state": {stateFrom(t, u)}})
		if kindOf(err) != apperr.KindConflict {
			t.Errorf("err = %v", err)
		}
		if gotApp != app {
			t.Errorf("app = %s, want %s so the browser returns to it", gotApp, app)
		}
	})
}

// Apps of one owner may share a tracker account; each gets its own row.
func TestSameAccountOnTwoApps(t *testing.T) {
	q := newFakeQuerier()
	svc, _, _ := newTestService(q)
	ctx := context.Background()
	owner := uuid.New()
	appA, appB := q.addApp(owner), q.addApp(owner)

	var ids []uuid.UUID
	for _, app := range []uuid.UUID{appA, appB} {
		u, err := svc.StartConnect(ctx, owner, app, db.TrackerGithub)
		if err != nil {
			t.Fatal(err)
		}
		_, conn, err := svc.FinishConnect(ctx, db.TrackerGithub, url.Values{"state": {stateFrom(t, u)}})
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, conn.ID)
	}
	if ids[0] == ids[1] || q.conns[ids[0]].AppID != appA || q.conns[ids[1]].AppID != appB {
		t.Errorf("connections = %+v", q.conns)
	}
}

func TestAttachConnection(t *testing.T) {
	q := newFakeQuerier()
	svc, _, _ := newTestService(q)
	ctx := context.Background()
	owner, other := uuid.New(), uuid.New()
	appA, appB, otherApp := q.addApp(owner), q.addApp(owner), q.addApp(other)
	now := time.Now()
	src := q.addConn(db.ProviderConnection{OwnerID: owner, AppID: appA, Tracker: db.TrackerGithub, ExternalID: "42", AccountLogin: "octo"})
	suspended := q.addConn(db.ProviderConnection{OwnerID: owner, AppID: appA, Tracker: db.TrackerGithub, ExternalID: "43", SuspendedAt: &now})
	foreign := q.addConn(db.ProviderConnection{OwnerID: other, AppID: otherApp, Tracker: db.TrackerGithub, ExternalID: "44"})

	c, err := svc.AttachConnection(ctx, owner, appB, src.ID)
	if err != nil {
		t.Fatal(err)
	}
	if c.ID == src.ID || c.AppID != appB || q.conns[c.ID].ExternalID != "42" || c.AccountLogin != "octo" {
		t.Errorf("attached = %+v", c)
	}

	if _, err := svc.AttachConnection(ctx, owner, appB, foreign.ID); kindOf(err) != apperr.KindNotFound {
		t.Errorf("another owner's connection: err = %v", err)
	}
	if _, err := svc.AttachConnection(ctx, owner, appB, suspended.ID); kindOf(err) != apperr.KindInvalid {
		t.Errorf("suspended connection: err = %v", err)
	}
	if _, err := svc.AttachConnection(ctx, owner, otherApp, src.ID); kindOf(err) != apperr.KindNotFound {
		t.Errorf("into another owner's app: err = %v", err)
	}
}

func TestListRepositories(t *testing.T) {
	q := newFakeQuerier()
	svc, _, p := newTestService(q)
	ctx := context.Background()
	owner := uuid.New()
	appA, appB := q.addApp(owner), q.addApp(owner)
	now := time.Now()
	conn := q.addConn(db.ProviderConnection{OwnerID: owner, AppID: appA, Tracker: db.TrackerGithub, ExternalID: "42", AccountLogin: "octo"})
	suspended := q.addConn(db.ProviderConnection{OwnerID: owner, AppID: appA, Tracker: db.TrackerGithub, ExternalID: "43", SuspendedAt: &now})

	if _, err := svc.ListRepositories(ctx, owner, appA, conn.ID); err != nil {
		t.Fatal(err)
	}
	if len(p.listed) != 1 || p.listed[0].ID != conn.ID || p.listed[0].ExternalID != "42" {
		t.Errorf("provider got %+v, want the whole connection", p.listed)
	}
	if _, err := svc.ListRepositories(ctx, owner, appB, conn.ID); kindOf(err) != apperr.KindNotFound {
		t.Errorf("another app's connection: err = %v", err)
	}
	if _, err := svc.ListRepositories(ctx, uuid.New(), appA, conn.ID); kindOf(err) != apperr.KindNotFound {
		t.Errorf("another owner: err = %v", err)
	}
	if _, err := svc.ListRepositories(ctx, owner, appA, suspended.ID); err == nil || !strings.Contains(err.Error(), "suspended") {
		t.Errorf("suspended connection: err = %v", err)
	}
}

func TestRegistryCovers(t *testing.T) {
	_, _, p := newTestService(newFakeQuerier())
	reg := NewRegistry(registryParams{Providers: []Provider{p}})
	gh := issue.Connection{Tracker: db.TrackerGithub, AccountLogin: "octo"}

	if !reg.Covers(gh, "octo/app") || reg.Covers(gh, "acme/app") {
		t.Error("registry must delegate Covers to the tracker's provider")
	}
	if reg.Covers(issue.Connection{Tracker: db.TrackerGitlab, AccountLogin: "octo"}, "octo/app") {
		t.Error("a connection on an unconfigured tracker covers nothing")
	}
}

func kindOf(err error) apperr.Kind {
	if err == nil {
		return -1
	}
	k, _ := apperr.Classify(err)
	return k
}

// An OAuth tracker's tokens are stored for the connection when the flow
// completes, and such a connection cannot be attached to another app.
func TestOAuthSecretsArePerApp(t *testing.T) {
	q := newFakeQuerier()
	svc, _, p := newTestService(q)
	p.account.Secret = []byte(`{"access_token":"t"}`)
	ctx := context.Background()
	owner := uuid.New()
	appA, appB := q.addApp(owner), q.addApp(owner)

	u, err := svc.StartConnect(ctx, owner, appA, db.TrackerGithub)
	if err != nil {
		t.Fatal(err)
	}
	_, conn, err := svc.FinishConnect(ctx, db.TrackerGithub, url.Values{"state": {stateFrom(t, u)}})
	if err != nil {
		t.Fatal(err)
	}
	if got := string(q.secrets[conn.ID]); got != `{"access_token":"t"}` {
		t.Errorf("stored secret = %q", got)
	}

	if _, err := svc.AttachConnection(ctx, owner, appB, conn.ID); kindOf(err) != apperr.KindInvalid || !strings.Contains(err.Error(), "cannot be shared") {
		t.Errorf("attaching an OAuth connection: err = %v, want 422 'cannot be shared'", err)
	}
}

// fakeSecretRows implements the two connection_secrets queries.
type fakeSecretRows struct {
	db.Querier
	rows map[uuid.UUID][]byte
}

func (f *fakeSecretRows) UpsertConnectionSecret(_ context.Context, arg db.UpsertConnectionSecretParams) error {
	f.rows[arg.ProviderConnectionID] = arg.Ciphertext
	return nil
}

func (f *fakeSecretRows) GetConnectionSecret(_ context.Context, id uuid.UUID) ([]byte, error) {
	v, ok := f.rows[id]
	if !ok {
		return nil, pgx.ErrNoRows
	}
	return v, nil
}

func TestDBSecretStore(t *testing.T) {
	key := make([]byte, secrets.KeySize)
	key[0] = 1
	c, err := secrets.New(key)
	if err != nil {
		t.Fatal(err)
	}
	rows := &fakeSecretRows{rows: map[uuid.UUID][]byte{}}
	store := NewDBSecretStore(rows, c)
	ctx := context.Background()
	a, b := uuid.New(), uuid.New()

	if _, err := store.Load(ctx, a); !errors.Is(err, ErrNoSecret) {
		t.Errorf("missing secret: err = %v, want ErrNoSecret", err)
	}
	if err := store.Save(ctx, a, []byte("token-a")); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(rows.rows[a]), "token-a") {
		t.Error("the stored row must be encrypted")
	}
	if got, err := store.Load(ctx, a); err != nil || string(got) != "token-a" {
		t.Errorf("Load = %q, %v", got, err)
	}

	rows.rows[b] = rows.rows[a] // a ciphertext copied to another connection
	if _, err := store.Load(ctx, b); err == nil {
		t.Error("a ciphertext must only decrypt for its own connection")
	}
}
