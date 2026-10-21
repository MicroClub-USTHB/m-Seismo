package gitlab

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
	"github.com/MicroClub-USTHB/m-Seismo/backend/internal/provider/gitlab/fakegitlab"
	"github.com/MicroClub-USTHB/m-Seismo/backend/internal/provider/oauth"
)

// memSecrets is an in-memory provider.SecretStore.
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

func (s *memSecrets) token(t *testing.T, id uuid.UUID) oauth.Token {
	t.Helper()
	raw, err := s.Load(context.Background(), id)
	if err != nil {
		t.Fatal(err)
	}
	tok, err := oauth.DecodeToken(raw)
	if err != nil {
		t.Fatal(err)
	}
	return tok
}

type env struct {
	p     *Provider
	fake  *fakegitlab.Server
	store *memSecrets
}

func newEnv(t *testing.T, mod ...func(*Options)) *env {
	t.Helper()
	fake := fakegitlab.New("https://gitlab.test")
	srv := httptest.NewServer(fake.Handler())
	t.Cleanup(srv.Close)

	o := Options{
		ClientID:     fakegitlab.ClientID,
		ClientSecret: fakegitlab.ClientSecret,
		WebURL:       srv.URL,
		RedirectURL:  "https://seismo.test/api/v1/providers/gitlab/callback",
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

// connect runs CompleteInstall with code and stores the token the way
// provider.Service does, returning the connection.
func (e *env) connect(t *testing.T, code string) issue.Connection {
	t.Helper()
	acct, err := e.p.CompleteInstall(context.Background(), url.Values{"code": {code}, "state": {"s"}})
	if err != nil {
		t.Fatalf("CompleteInstall(%s): %v", code, err)
	}
	c := issue.Connection{ID: uuid.New(), Tracker: db.TrackerGitlab, ExternalID: acct.ExternalID, AccountLogin: acct.Login}
	if err := e.store.Save(context.Background(), c.ID, acct.Secret); err != nil {
		t.Fatal(err)
	}
	return c
}

func TestNewRequiresCredentials(t *testing.T) {
	if _, err := New(Options{ClientID: "x"}, &memSecrets{}); err == nil {
		t.Error("missing secret and redirect URL must be rejected")
	}
	p, err := New(Options{ClientID: "x", ClientSecret: "y", RedirectURL: "https://s/cb"}, &memSecrets{})
	if err != nil || p.opts.APIURL != "https://gitlab.com/api/v4/" || p.opts.WebURL != "https://gitlab.com/" {
		t.Errorf("defaults: %+v, %v", p.opts, err)
	}
	if p.Tracker() != db.TrackerGitlab {
		t.Error("tracker must be gitlab")
	}
}

func TestInstallURL(t *testing.T) {
	e := newEnv(t)
	u, err := url.Parse(e.p.InstallURL("st"))
	if err != nil {
		t.Fatal(err)
	}
	q := u.Query()
	if !strings.HasSuffix(u.Path, "/oauth/authorize") || q.Get("client_id") != fakegitlab.ClientID ||
		q.Get("scope") != "api" || q.Get("state") != "st" || q.Get("response_type") != "code" ||
		q.Get("redirect_uri") != "https://seismo.test/api/v1/providers/gitlab/callback" {
		t.Errorf("install URL = %s", u)
	}
}

func TestCompleteInstall(t *testing.T) {
	e := newEnv(t)
	ctx := context.Background()

	acct, err := e.p.CompleteInstall(ctx, url.Values{"code": {"fake-code-7"}})
	if err != nil {
		t.Fatal(err)
	}
	if acct.ExternalID != "7" || acct.Login != "gl-user-7" || acct.Type != "User" {
		t.Errorf("account = %+v", acct)
	}
	tok, err := oauth.DecodeToken(acct.Secret)
	if err != nil || tok.AccessToken != "gl-access-7-1" || tok.RefreshToken != "gl-refresh-7-1" || time.Until(tok.Expiry) < time.Hour {
		t.Errorf("secret = %+v, %v", tok, err)
	}

	for name, q := range map[string]url.Values{
		"denied":       {"error": {"access_denied"}, "error_description": {"The resource owner denied the request"}},
		"missing code": {"state": {"s"}},
		"bad code":     {"code": {"stolen"}},
	} {
		_, err := e.p.CompleteInstall(ctx, q)
		if k, _ := apperr.Classify(err); k != apperr.KindInvalid {
			t.Errorf("%s: err = %v, want 422", name, err)
		}
	}
	_, err = e.p.CompleteInstall(ctx, url.Values{"error": {"access_denied"}, "error_description": {"denied by user"}})
	if !strings.Contains(err.Error(), "denied by user") {
		t.Errorf("the tracker's reason must be kept: %v", err)
	}
}

func TestRepositories(t *testing.T) {
	e := newEnv(t, func(o *Options) { o.PageSize = 2 })
	c := e.connect(t, "fake-code-3")
	ctx := context.Background()

	repos, err := e.p.ListRepositories(ctx, c)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, r := range repos {
		names = append(names, r.FullName)
		if !r.Private || r.URL == "" {
			t.Errorf("repository %+v", r)
		}
	}
	if !slices.Equal(names, fakegitlab.Projects) {
		t.Errorf("listed %v (all pages), want %v", names, fakegitlab.Projects)
	}

	if ok, err := e.p.HasRepository(ctx, c, "seismo-group/sub/nested"); err != nil || !ok {
		t.Errorf("nested project: %v, %v", ok, err)
	}
	if ok, err := e.p.HasRepository(ctx, c, "seismo-group/unknown"); err != nil || ok {
		t.Errorf("unknown project: %v, %v", ok, err)
	}
	if _, err := e.p.HasRepository(ctx, c, "no-slash"); err == nil {
		t.Error("malformed name accepted")
	}
}

func TestValidateAndCovers(t *testing.T) {
	e := newEnv(t)
	for repo, ok := range map[string]bool{
		"group/project":           true,
		"group/sub/deep/project":  true,
		"user_1/my.project-x":     true,
		"no-slash":                false,
		"group/":                  false,
		"/project":                false,
		"group//project":          false,
		"group/-project":          false,
		"group/../project":        false,
		"group/pro ject":          false,
		"":                        false,
		"group/project#1":         false,
		"https://gitlab.com/a/b":  false,
		"group/sub/project/extra": true,
	} {
		if got := e.p.ValidateRepository(repo) == nil; got != ok {
			t.Errorf("ValidateRepository(%q) ok = %v, want %v", repo, got, ok)
		}
		if got := e.p.Covers(issue.Connection{}, repo); got != ok {
			t.Errorf("Covers(%q) = %v, want %v (any valid path)", repo, got, ok)
		}
	}
}

func TestOpener(t *testing.T) {
	e := newEnv(t)
	c := e.connect(t, "fake-code-4")
	ctx := context.Background()
	op, err := e.p.Opener(ctx, c)
	if err != nil {
		t.Fatal(err)
	}

	ref, err := op.Open(ctx, issue.Issue{
		Repository: "seismo-group/sub/nested",
		Title:      "Crash on login",
		Body:       "steps",
		Labels:     []string{"bug", "m-seismo"},
		Milestone:  "v1.0",
		Assignees:  []string{"gl-user-9", "nobody"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if ref.ID != "seismo-group/sub/nested#1" || ref.URL != "https://gitlab.test/seismo-group/sub/nested/-/issues/1" {
		t.Errorf("ref = %+v", ref)
	}
	got := e.fake.Issues()[0]
	if got.Description != "steps" || !slices.Equal(got.Labels, []string{"bug", "m-seismo"}) ||
		got.MilestoneID == nil || *got.MilestoneID != 1 || !slices.Equal(got.AssigneeIDs, []int{9}) || got.AuthorID != 4 {
		t.Errorf("issue on GitLab = %+v", got)
	}

	if _, err := op.Open(ctx, issue.Issue{Repository: "seismo-group/another", Title: "no milestone", Milestone: "v9"}); err != nil {
		t.Errorf("an unknown milestone must not block the issue: %v", err)
	}

	if err := op.Comment(ctx, ref, "seen again"); err != nil {
		t.Fatal(err)
	}
	if notes := e.fake.Issues()[0].Notes; len(notes) != 1 || notes[0] != "seen again" {
		t.Errorf("notes = %v", notes)
	}
	if open, err := op.IsOpen(ctx, ref); err != nil || !open {
		t.Errorf("IsOpen = %v, %v", open, err)
	}
	e.fake.SetState("seismo-group/sub/nested", 1, "closed")
	if open, err := op.IsOpen(ctx, ref); err != nil || open {
		t.Errorf("closed issue: IsOpen = %v, %v", open, err)
	}
	for _, bad := range []string{"garbage", "#3", "group/p#x", "group/p#0"} {
		if _, err := op.IsOpen(ctx, issue.Ref{ID: bad}); err == nil {
			t.Errorf("malformed ref %q accepted", bad)
		}
	}

	_, err = op.Open(ctx, issue.Issue{Repository: fakegitlab.BrokenProject, Title: "x"})
	var rl *issue.RateLimitError
	if err == nil || errors.As(err, &rl) || !strings.Contains(err.Error(), "500") {
		t.Errorf("broken project: %v", err)
	}
}

// A token about to expire is refreshed before use, and the rotated token
// is saved; the old refresh token no longer works afterwards.
func TestTokenRefreshAndRotation(t *testing.T) {
	e := newEnv(t)
	c := e.connect(t, "fake-code-5-short")
	ctx := context.Background()
	before := e.store.token(t, c.ID)

	if _, err := e.p.ListRepositories(ctx, c); err != nil {
		t.Fatalf("the expiring token must be refreshed transparently: %v", err)
	}
	after := e.store.token(t, c.ID)
	if e.fake.Stats().Refreshes != 1 || after.RefreshToken == before.RefreshToken || after.AccessToken != "gl-access-5-2" {
		t.Errorf("refreshes = %d, stored = %+v", e.fake.Stats().Refreshes, after)
	}

	if _, err := e.p.ListRepositories(ctx, c); err != nil || e.fake.Stats().Refreshes != 1 {
		t.Errorf("a fresh token must be reused: %v, %d refreshes", err, e.fake.Stats().Refreshes)
	}
}

func TestRevokedAuthorization(t *testing.T) {
	e := newEnv(t)
	c := e.connect(t, "fake-code-6")
	e.fake.Revoke(6)

	_, err := e.p.ListRepositories(context.Background(), c)
	if err == nil || !strings.Contains(err.Error(), "connect the app again") || !errors.Is(err, oauth.ErrRevoked) {
		t.Errorf("revoked: %v", err)
	}

	if _, err := e.p.ListRepositories(context.Background(), issue.Connection{ID: uuid.New()}); !errors.Is(err, provider.ErrNoSecret) {
		t.Errorf("a connection without a token: %v", err)
	}
}

func TestRateLimits(t *testing.T) {
	e := newEnv(t)
	c := e.connect(t, "fake-code-8")
	ctx := context.Background()
	op, _ := e.p.Opener(ctx, c)

	start := time.Now()
	if _, err := op.Open(ctx, issue.Issue{Repository: fakegitlab.LimitedProject, Title: "limited"}); err != nil {
		t.Fatalf("a short 429 must be waited out: %v", err)
	}
	if time.Since(start) < time.Second || len(e.fake.Issues()) != 1 {
		t.Errorf("waited %s, %d issues", time.Since(start), len(e.fake.Issues()))
	}

	start = time.Now()
	_, err := op.Open(ctx, issue.Issue{Repository: fakegitlab.ExhaustedProject, Title: "exhausted"})
	var rl *issue.RateLimitError
	if !errors.As(err, &rl) || rl.Tracker != "gitlab" || rl.Wait < 59*time.Minute {
		t.Fatalf("exhausted: %v", err)
	}
	if time.Since(start) > 2*time.Second {
		t.Error("a long limit must fail fast")
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
