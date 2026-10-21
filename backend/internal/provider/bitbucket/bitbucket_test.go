package bitbucket

import (
	"context"
	"errors"
	"net/http/httptest"
	"net/url"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/MicroClub-USTHB/m-Seismo/backend/internal/apperr"
	"github.com/MicroClub-USTHB/m-Seismo/backend/internal/db"
	"github.com/MicroClub-USTHB/m-Seismo/backend/internal/issue"
	"github.com/MicroClub-USTHB/m-Seismo/backend/internal/provider"
	"github.com/MicroClub-USTHB/m-Seismo/backend/internal/provider/bitbucket/fakebitbucket"
	"github.com/MicroClub-USTHB/m-Seismo/backend/internal/provider/oauth"
)

type memSecrets struct {
	mu sync.Mutex
	m  map[uuid.UUID][]byte
}

func (s *memSecrets) Load(_ context.Context, id uuid.UUID) ([]byte, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	v, ok := s.m[id]
	if !ok {
		return nil, provider.ErrNoSecret
	}
	return v, nil
}

func (s *memSecrets) Save(_ context.Context, id uuid.UUID, v []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.m[id] = v
	return nil
}

type env struct {
	p     *Provider
	fake  *fakebitbucket.Server
	store *memSecrets
}

func newEnv(t *testing.T, mod ...func(*Options)) *env {
	t.Helper()
	fake := fakebitbucket.New("https://bitbucket.test")
	srv := httptest.NewServer(fake.Handler())
	t.Cleanup(srv.Close)
	o := Options{
		ClientID:     fakebitbucket.ClientID,
		ClientSecret: fakebitbucket.ClientSecret,
		WebURL:       srv.URL,
		APIURL:       srv.URL + "/2.0",
		RedirectURL:  "https://seismo.test/api/v1/providers/bitbucket/callback",
	}
	for _, m := range mod {
		m(&o)
	}
	store := &memSecrets{m: map[uuid.UUID][]byte{}}
	p, err := New(o, store)
	if err != nil {
		t.Fatal(err)
	}
	return &env{p: p, fake: fake, store: store}
}

func (e *env) connect(t *testing.T, code string) issue.Connection {
	t.Helper()
	acct, err := e.p.CompleteInstall(context.Background(), url.Values{"code": {code}, "state": {"s"}})
	if err != nil {
		t.Fatalf("CompleteInstall(%s): %v", code, err)
	}
	c := issue.Connection{ID: uuid.New(), Tracker: db.TrackerBitbucket, ExternalID: acct.ExternalID, AccountLogin: acct.Login}
	if err := e.store.Save(context.Background(), c.ID, acct.Secret); err != nil {
		t.Fatal(err)
	}
	return c
}

func TestNew(t *testing.T) {
	if _, err := New(Options{ClientID: "x"}, &memSecrets{}); err == nil {
		t.Error("a missing secret must be rejected")
	}
	p, err := New(Options{ClientID: "x", ClientSecret: "y"}, &memSecrets{})
	if err != nil || p.opts.APIURL != "https://api.bitbucket.org/2.0/" || p.opts.WebURL != "https://bitbucket.org/" || p.opts.PageSize != 100 {
		t.Errorf("defaults: %+v, %v", p.opts, err)
	}
	if p.Tracker() != db.TrackerBitbucket {
		t.Error("tracker must be bitbucket")
	}
}

func TestInstallURL(t *testing.T) {
	e := newEnv(t)
	u, _ := url.Parse(e.p.InstallURL("st"))
	q := u.Query()
	if !strings.HasSuffix(u.Path, "/site/oauth2/authorize") || q.Get("client_id") != fakebitbucket.ClientID ||
		q.Get("state") != "st" || q.Get("response_type") != "code" || q.Has("scope") {
		t.Errorf("install URL = %s (scopes come from the consumer, so none are sent)", u)
	}
}

func TestCompleteInstall(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()

	acct, err := e.p.CompleteInstall(ctx, url.Values{"code": {"fake-code-7"}})
	if err != nil {
		t.Fatal(err)
	}
	if acct.ExternalID != fakebitbucket.UserUUID(7) || acct.Login != "bb-user-7" || acct.Type != "User" {
		t.Errorf("account = %+v", acct)
	}
	if tok, err := oauth.DecodeToken(acct.Secret); err != nil || tok.AccessToken != "bb-access-7-1" || tok.RefreshToken != "bb-refresh-7-1" {
		t.Errorf("secret = %+v, %v", tok, err)
	}

	for name, q := range map[string]url.Values{
		"denied":       {"error": {"access_denied"}},
		"missing code": {},
		"bad code":     {"code": {"stolen"}},
	} {
		if _, err := e.p.CompleteInstall(ctx, q); func() bool { k, _ := apperr.Classify(err); return k != apperr.KindInvalid }() {
			t.Errorf("%s: err = %v, want 422", name, err)
		}
	}

	bad := newEnv(t, func(o *Options) { o.ClientSecret = "wrong" })
	if _, err := bad.p.CompleteInstall(ctx, url.Values{"code": {"fake-code-1"}}); err == nil {
		t.Error("a wrong consumer secret must fail the exchange")
	}
}

func TestRepositories(t *testing.T) {
	e := newEnv(t, func(o *Options) { o.PageSize = 4 })
	c := e.connect(t, "fake-code-3")
	ctx := context.Background()

	repos, err := e.p.ListRepositories(ctx, c)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, r := range repos {
		names = append(names, r.FullName)
	}
	if !slices.Equal(names, fakebitbucket.Repositories) {
		t.Errorf("listed %v (following next links), want %v", names, fakebitbucket.Repositories)
	}
	if !repos[0].Private || repos[0].URL != "https://bitbucket.test/seismo-ws/seismo-test" {
		t.Errorf("repository = %+v", repos[0])
	}

	if ok, err := e.p.HasRepository(ctx, c, "seismo-ws/another"); err != nil || !ok {
		t.Errorf("known: %v, %v", ok, err)
	}
	if ok, err := e.p.HasRepository(ctx, c, "seismo-ws/nope"); err != nil || ok {
		t.Errorf("unknown: %v, %v", ok, err)
	}
}

func TestValidateAndCovers(t *testing.T) {
	e := newEnv(t)
	for repo, ok := range map[string]bool{
		"ws/repo":       true,
		"my-ws/my.repo": true,
		"ws_1/repo-2":   true,
		"ws":            false,
		"ws/":           false,
		"/repo":         false,
		"ws/sub/repo":   false,
		"ws/re po":      false,
		"ws/-repo":      false,
		"":              false,
	} {
		if got := e.p.ValidateRepository(repo) == nil; got != ok {
			t.Errorf("ValidateRepository(%q) = %v, want %v", repo, got, ok)
		}
		if got := e.p.Covers(issue.Connection{}, repo); got != ok {
			t.Errorf("Covers(%q) = %v, want %v", repo, got, ok)
		}
	}
}

func TestOpener(t *testing.T) {
	e := newEnv(t)
	c := e.connect(t, "fake-code-4")
	ctx := context.Background()
	op, _ := e.p.Opener(ctx, c)

	ref, err := op.Open(ctx, issue.Issue{
		Repository: "seismo-ws/seismo-test",
		Title:      "Crash on login",
		Body:       "steps",
		Labels:     []string{"bug", "m-seismo"},
		Milestone:  "v1.0",
		Assignees:  []string{"someone"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if ref.ID != "seismo-ws/seismo-test#1" || ref.URL != "https://bitbucket.test/seismo-ws/seismo-test/issues/1" {
		t.Errorf("ref = %+v", ref)
	}
	got := e.fake.Issues()[0]
	if got.Content != "steps\n\nLabels: bug, m-seismo" || got.Kind != "bug" || got.Milestone != "v1.0" || got.Reporter != fakebitbucket.UserUUID(4) {
		t.Errorf("issue on Bitbucket = %+v", got)
	}

	if _, err := op.Open(ctx, issue.Issue{Repository: "seismo-ws/another", Title: "unknown milestone", Milestone: "v9"}); err != nil {
		t.Errorf("an unknown milestone must not block the issue: %v", err)
	}
	if _, err := op.Open(ctx, issue.Issue{Repository: "seismo-ws/another", Title: "labels only", Labels: []string{"x"}}); err != nil {
		t.Fatal(err)
	}
	if got := e.fake.Issues()[2].Content; got != "Labels: x" {
		t.Errorf("labels without a body: %q", got)
	}

	if err := op.Comment(ctx, ref, "seen again"); err != nil {
		t.Fatal(err)
	}
	if cs := e.fake.Issues()[0].Comments; len(cs) != 1 || cs[0] != "seen again" {
		t.Errorf("comments = %v", cs)
	}
	for state, open := range map[string]bool{"new": true, "open": true, "on hold": true, "resolved": false, "closed": false, "wontfix": false} {
		e.fake.SetState("seismo-ws/seismo-test", 1, state)
		if got, err := op.IsOpen(ctx, ref); err != nil || got != open {
			t.Errorf("state %q: IsOpen = %v, %v", state, got, err)
		}
	}
	for _, bad := range []string{"garbage", "ws/repo#x", "ws#1", "ws/repo#0"} {
		if _, err := op.IsOpen(ctx, issue.Ref{ID: bad}); err == nil {
			t.Errorf("malformed ref %q accepted", bad)
		}
	}

	_, err = op.Open(ctx, issue.Issue{Repository: fakebitbucket.NoIssuesRepo, Title: "x"})
	if err == nil || !strings.Contains(err.Error(), "Repository has no issue tracker.") {
		t.Errorf("a repository without issues must say so: %v", err)
	}
	_, err = op.Open(ctx, issue.Issue{Repository: fakebitbucket.BrokenRepo, Title: "x"})
	var rl *issue.RateLimitError
	if err == nil || errors.As(err, &rl) || !strings.Contains(err.Error(), "500") {
		t.Errorf("broken repository: %v", err)
	}
}

func TestTokenRefresh(t *testing.T) {
	e := newEnv(t)
	c := e.connect(t, "fake-code-5-short")
	ctx := context.Background()

	if _, err := e.p.ListRepositories(ctx, c); err != nil {
		t.Fatalf("the expiring token must be refreshed transparently: %v", err)
	}
	raw, _ := e.store.Load(ctx, c.ID)
	tok, _ := oauth.DecodeToken(raw)
	if e.fake.Stats().Refreshes != 1 || tok.AccessToken != "bb-access-5-2" || tok.RefreshToken != "bb-refresh-5-1" {
		t.Errorf("refreshes = %d, stored = %+v (Bitbucket keeps the refresh token)", e.fake.Stats().Refreshes, tok)
	}
}

func TestRevokedAuthorization(t *testing.T) {
	e := newEnv(t)
	c := e.connect(t, "fake-code-6")
	e.fake.Revoke(6)
	_, err := e.p.ListRepositories(context.Background(), c)
	if !errors.Is(err, oauth.ErrRevoked) || !strings.Contains(err.Error(), "connect the app again") {
		t.Errorf("revoked: %v", err)
	}
}

func TestRateLimits(t *testing.T) {
	e := newEnv(t)
	c := e.connect(t, "fake-code-8")
	ctx := context.Background()
	op, _ := e.p.Opener(ctx, c)

	start := time.Now()
	if _, err := op.Open(ctx, issue.Issue{Repository: fakebitbucket.LimitedRepo, Title: "limited"}); err != nil {
		t.Fatalf("a short 429 must be waited out: %v", err)
	}
	if time.Since(start) < time.Second || len(e.fake.Issues()) != 1 {
		t.Errorf("waited %s, %d issues", time.Since(start), len(e.fake.Issues()))
	}

	start = time.Now()
	_, err := op.Open(ctx, issue.Issue{Repository: fakebitbucket.ExhaustedRepo, Title: "exhausted"})
	var rl *issue.RateLimitError
	if !errors.As(err, &rl) || rl.Tracker != "bitbucket" || rl.Wait != time.Hour || time.Since(start) > 2*time.Second {
		t.Errorf("exhausted: %v after %s", err, time.Since(start))
	}
}

// Two apps authorised by the same user hold separate grants: connecting
// the second app, or refreshing either token, never breaks the other.
func TestTwoAppsSameUser(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()
	appA := e.connect(t, "fake-code-11")
	appB := e.connect(t, "fake-code-11-short")

	for name, c := range map[string]issue.Connection{"A": appA, "B (refreshed)": appB, "A again": appA} {
		if _, err := e.p.ListRepositories(ctx, c); err != nil {
			t.Errorf("app %s: %v", name, err)
		}
	}
	if e.fake.Stats().Refreshes != 1 {
		t.Errorf("refreshes = %d, want 1 (only B's short-lived token)", e.fake.Stats().Refreshes)
	}
}
