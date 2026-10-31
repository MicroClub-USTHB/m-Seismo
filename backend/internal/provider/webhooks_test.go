package provider

import (
	"bytes"
	"context"
	"errors"
	"net/url"
	"testing"

	"github.com/google/uuid"

	"github.com/MicroClub-USTHB/m-Seismo/backend/internal/db"
	"github.com/MicroClub-USTHB/m-Seismo/backend/internal/issue"
	"github.com/MicroClub-USTHB/m-Seismo/backend/internal/secrets"
)

// fakeWebhookManager is a tracker (like GitLab or Bitbucket) that needs a
// webhook of its own. GitHub doesn't implement WebhookManager at all,
// which Ensure must treat as a no-op rather than an error.
type fakeWebhookManager struct {
	tracker  db.Tracker
	created  []issue.Connection
	hook     Webhook
	err      error
	deleted  bool
	deleteOn Webhook
}

func (*fakeWebhookManager) Tracker() db.Tracker { return db.TrackerGitlab }
func (*fakeWebhookManager) InstallURL(context.Context, uuid.UUID, string) (string, error) {
	return "", nil
}
func (*fakeWebhookManager) CompleteInstall(context.Context, uuid.UUID, url.Values) (Account, error) {
	return Account{}, nil
}
func (*fakeWebhookManager) ListRepositories(context.Context, issue.Connection) ([]Repository, error) {
	return nil, nil
}
func (*fakeWebhookManager) HasRepository(context.Context, issue.Connection, string) (bool, error) {
	return true, nil
}
func (*fakeWebhookManager) ValidateRepository(string) error      { return nil }
func (*fakeWebhookManager) Covers(issue.Connection, string) bool { return true }
func (*fakeWebhookManager) Opener(context.Context, issue.Connection) (issue.Opener, error) {
	return nil, nil
}

func (f *fakeWebhookManager) CreateWebhook(_ context.Context, c issue.Connection, _ string) (Webhook, error) {
	f.created = append(f.created, c)
	if f.err != nil {
		return Webhook{}, f.err
	}
	return f.hook, nil
}

func (f *fakeWebhookManager) DeleteWebhook(_ context.Context, _ issue.Connection, _ string, hook Webhook) error {
	f.deleted = true
	f.deleteOn = hook
	return nil
}

var _ Provider = (*fakeWebhookManager)(nil)
var _ WebhookManager = (*fakeWebhookManager)(nil)

// fakeNoWebhookProvider is a tracker (like GitHub) that needs no webhook
// of its own: it implements Provider but not WebhookManager.
type fakeNoWebhookProvider struct{}

func (*fakeNoWebhookProvider) Tracker() db.Tracker { return db.TrackerGithub }
func (*fakeNoWebhookProvider) InstallURL(context.Context, uuid.UUID, string) (string, error) {
	return "", nil
}
func (*fakeNoWebhookProvider) CompleteInstall(context.Context, uuid.UUID, url.Values) (Account, error) {
	return Account{}, nil
}
func (*fakeNoWebhookProvider) ListRepositories(context.Context, issue.Connection) ([]Repository, error) {
	return nil, nil
}
func (*fakeNoWebhookProvider) HasRepository(context.Context, issue.Connection, string) (bool, error) {
	return true, nil
}
func (*fakeNoWebhookProvider) ValidateRepository(string) error      { return nil }
func (*fakeNoWebhookProvider) Covers(issue.Connection, string) bool { return true }
func (*fakeNoWebhookProvider) Opener(context.Context, issue.Connection) (issue.Opener, error) {
	return nil, nil
}

var _ Provider = (*fakeNoWebhookProvider)(nil)

type fakeWebhookQuerier struct {
	db.Querier
	sets []db.SetProjectRepositoryWebhookParams
	err  error
}

func (f *fakeWebhookQuerier) SetProjectRepositoryWebhook(_ context.Context, arg db.SetProjectRepositoryWebhookParams) error {
	f.sets = append(f.sets, arg)
	return f.err
}

func testCipher(t *testing.T) *secrets.Cipher {
	t.Helper()
	c, err := secrets.New(bytes.Repeat([]byte{3}, secrets.KeySize))
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func TestWebhookServiceEnsureCreatesOnce(t *testing.T) {
	mgr := &fakeWebhookManager{hook: Webhook{ExternalID: "42", Secret: "shh"}}
	reg := NewRegistry(registryParams{Providers: []Provider{mgr}})
	q := &fakeWebhookQuerier{}
	svc := NewWebhookService(q, reg, testCipher(t))

	conn := issue.Connection{ID: uuid.New(), AppID: uuid.New(), Tracker: db.TrackerGitlab}
	repoID := uuid.New()
	svc.Ensure(context.Background(), conn, "group/project", repoID, false)

	if len(mgr.created) != 1 {
		t.Fatalf("CreateWebhook called %d times", len(mgr.created))
	}
	if len(q.sets) != 1 {
		t.Fatalf("SetProjectRepositoryWebhook called %d times", len(q.sets))
	}
	set := q.sets[0]
	if set.ID != repoID || set.WebhookExternalID == nil || *set.WebhookExternalID != "42" {
		t.Errorf("set = %+v", set)
	}
	plain, err := testCipher(t).Open(set.WebhookSecret, repoID[:])
	if err != nil || string(plain) != "shh" {
		t.Errorf("stored secret = %q, %v", plain, err)
	}
}

func TestWebhookServiceEnsureSkipsWhenAlreadyHasOne(t *testing.T) {
	mgr := &fakeWebhookManager{}
	reg := NewRegistry(registryParams{Providers: []Provider{mgr}})
	svc := NewWebhookService(&fakeWebhookQuerier{}, reg, testCipher(t))

	svc.Ensure(context.Background(), issue.Connection{Tracker: db.TrackerGitlab}, "group/project", uuid.New(), true)

	if len(mgr.created) != 0 {
		t.Errorf("CreateWebhook called for a repository that already has one")
	}
}

func TestWebhookServiceEnsureIsNoopForTrackerWithout(t *testing.T) {
	mgr := &fakeNoWebhookProvider{}
	reg := NewRegistry(registryParams{Providers: []Provider{mgr}})
	q := &fakeWebhookQuerier{}
	svc := NewWebhookService(q, reg, testCipher(t))

	svc.Ensure(context.Background(), issue.Connection{Tracker: db.TrackerGithub}, "o/r", uuid.New(), false)

	if len(q.sets) != 0 {
		t.Errorf("recorded a webhook for a tracker that doesn't implement WebhookManager")
	}
}

func TestWebhookServiceEnsureNeverFailsOnError(t *testing.T) {
	mgr := &fakeWebhookManager{err: errors.New("gitlab is down")}
	reg := NewRegistry(registryParams{Providers: []Provider{mgr}})
	svc := NewWebhookService(&fakeWebhookQuerier{}, reg, testCipher(t))

	// Must not panic and must return normally.
	svc.Ensure(context.Background(), issue.Connection{Tracker: db.TrackerGitlab}, "group/project", uuid.New(), false)
}
