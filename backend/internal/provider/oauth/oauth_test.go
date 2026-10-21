package oauth

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/MicroClub-USTHB/m-Seismo/backend/internal/apperr"
	"github.com/MicroClub-USTHB/m-Seismo/backend/internal/issue"
)

// memStore is an in-memory Store.
type memStore struct {
	mu    sync.Mutex
	m     map[uuid.UUID][]byte
	saves int
}

func newMemStore() *memStore { return &memStore{m: map[uuid.UUID][]byte{}} }

func (s *memStore) Load(_ context.Context, id uuid.UUID) ([]byte, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	v, ok := s.m[id]
	if !ok {
		return nil, errors.New("no secret")
	}
	return v, nil
}

func (s *memStore) Save(_ context.Context, id uuid.UUID, v []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.m[id] = v
	s.saves++
	return nil
}

// mustSave stores tok for id, failing the test on error.
func mustSave(t *testing.T, s *memStore, id uuid.UUID, tok Token) {
	t.Helper()
	if err := s.Save(context.Background(), id, tok.Encode()); err != nil {
		t.Fatal(err)
	}
}

func (s *memStore) token(id uuid.UUID) Token {
	raw, _ := s.Load(context.Background(), id)
	t, _ := DecodeToken(raw)
	return t
}

// tokenServer is an OAuth token endpoint that rotates refresh tokens like
// GitLab: each refresh token works once.
type tokenServer struct {
	*httptest.Server
	basic     bool
	refreshes atomic.Int32
	mu        sync.Mutex
	valid     map[string]bool // refresh tokens still accepted
	gen       int
}

func newTokenServer(t *testing.T, basic bool) *tokenServer {
	ts := &tokenServer{basic: basic, valid: map[string]bool{"r0": true}}
	ts.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		id, secret, hasBasic := r.BasicAuth()
		if !ts.basic {
			id, secret = r.PostForm.Get("client_id"), r.PostForm.Get("client_secret")
			if hasBasic {
				http.Error(w, `{"error":"unexpected basic auth"}`, 400)
				return
			}
		}
		if id != "cid" || secret != "csecret" {
			w.WriteHeader(401)
			_, _ = fmt.Fprint(w, `{"error":"invalid_client"}`)
			return
		}
		ts.mu.Lock()
		defer ts.mu.Unlock()
		switch r.PostForm.Get("grant_type") {
		case "authorization_code":
			if r.PostForm.Get("code") != "good" || r.PostForm.Get("redirect_uri") != "https://seismo.test/cb" {
				w.WriteHeader(400)
				_, _ = fmt.Fprint(w, `{"error":"invalid_grant","error_description":"bad code"}`)
				return
			}
			_, _ = fmt.Fprint(w, `{"access_token":"a0","refresh_token":"r0","expires_in":7200,"token_type":"bearer"}`)
		case "refresh_token":
			rt := r.PostForm.Get("refresh_token")
			if !ts.valid[rt] {
				w.WriteHeader(400)
				_, _ = fmt.Fprint(w, `{"error":"invalid_grant","error_description":"refresh token used or revoked"}`)
				return
			}
			delete(ts.valid, rt)
			ts.gen++
			ts.refreshes.Add(1)
			next := fmt.Sprintf("r%d", ts.gen)
			ts.valid[next] = true
			_, _ = fmt.Fprintf(w, `{"access_token":"a%d","refresh_token":"%s","expires_in":7200}`, ts.gen, next)
		default:
			w.WriteHeader(400)
		}
	}))
	t.Cleanup(ts.Close)
	return ts
}

func (ts *tokenServer) endpoint() Endpoint {
	return Endpoint{
		Tracker: "test", AuthURL: ts.URL + "/authorize", TokenURL: ts.URL + "/token",
		ClientID: "cid", ClientSecret: "csecret", RedirectURL: "https://seismo.test/cb",
		Scopes: []string{"api"}, BasicAuth: ts.basic,
	}
}

func TestAuthCodeURL(t *testing.T) {
	e := Endpoint{AuthURL: "https://gl.test/oauth/authorize", ClientID: "cid", RedirectURL: "https://s/cb", Scopes: []string{"api", "read_user"}}
	u := e.AuthCodeURL("st ate")
	for _, want := range []string{"client_id=cid", "response_type=code", "state=st+ate", "redirect_uri=https%3A%2F%2Fs%2Fcb", "scope=api+read_user"} {
		if !strings.Contains(u, want) {
			t.Errorf("%s lacks %s", u, want)
		}
	}
	if strings.Contains((Endpoint{AuthURL: "x"}).AuthCodeURL("s"), "scope=") {
		t.Error("no scopes → no scope parameter")
	}
}

func TestExchange(t *testing.T) {
	for _, basic := range []bool{false, true} {
		ts := newTokenServer(t, basic)
		tok, err := ts.endpoint().Exchange(context.Background(), "good")
		if err != nil {
			t.Fatalf("basic=%v: %v", basic, err)
		}
		if tok.AccessToken != "a0" || tok.RefreshToken != "r0" || time.Until(tok.Expiry) < time.Hour {
			t.Errorf("basic=%v: token = %+v", basic, tok)
		}

		_, err = ts.endpoint().Exchange(context.Background(), "bad")
		if k, msg := apperr.Classify(err); k != apperr.KindInvalid || !strings.Contains(msg, "rejected the authorization code") {
			t.Errorf("bad code: %v / %s", err, msg)
		}

		e := ts.endpoint()
		e.ClientSecret = "wrong"
		if _, err := e.Exchange(context.Background(), "good"); err == nil {
			t.Error("wrong client secret accepted")
		}
	}
}

func TestTokensRefreshBeforeExpiry(t *testing.T) {
	ts := newTokenServer(t, false)
	store := newMemStore()
	id := uuid.New()
	ctx := context.Background()
	mustSave(t, store, id, Token{AccessToken: "a0", RefreshToken: "r0", Expiry: time.Now().Add(time.Hour)})
	tokens := NewTokens(store, ts.endpoint())

	if got, err := tokens.Access(ctx, id); err != nil || got != "a0" {
		t.Fatalf("fresh token: %q, %v", got, err)
	}
	if ts.refreshes.Load() != 0 {
		t.Error("a token far from expiry must not be refreshed")
	}

	mustSave(t, store, id, Token{AccessToken: "a0", RefreshToken: "r0", Expiry: time.Now().Add(10 * time.Second)})
	got, err := tokens.Access(ctx, id)
	if err != nil || got != "a1" {
		t.Fatalf("expiring token: %q, %v", got, err)
	}
	if saved := store.token(id); saved.AccessToken != "a1" || saved.RefreshToken != "r1" {
		t.Errorf("the rotated token must be saved, got %+v", saved)
	}
	if got, _ := tokens.Access(ctx, id); got != "a1" || ts.refreshes.Load() != 1 {
		t.Errorf("after the refresh: %q, %d refreshes", got, ts.refreshes.Load())
	}
}

// Many requests finding the same expiring token refresh it once: a second
// refresh with the (single-use) old refresh token would fail.
func TestTokensConcurrentRefreshOnce(t *testing.T) {
	ts := newTokenServer(t, false)
	store := newMemStore()
	id := uuid.New()
	ctx := context.Background()
	mustSave(t, store, id, Token{AccessToken: "a0", RefreshToken: "r0", Expiry: time.Now()})
	tokens := NewTokens(store, ts.endpoint())

	var wg sync.WaitGroup
	errs := make(chan error, 20)
	for range 20 {
		wg.Go(func() {
			if got, err := tokens.Access(ctx, id); err != nil || got != "a1" {
				errs <- fmt.Errorf("got %q, %v", got, err)
			}
		})
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Error(err)
	}
	if n := ts.refreshes.Load(); n != 1 {
		t.Errorf("refreshes = %d, want 1", n)
	}
}

func TestTokensRenewAndRevoked(t *testing.T) {
	ts := newTokenServer(t, true)
	store := newMemStore()
	id := uuid.New()
	ctx := context.Background()
	mustSave(t, store, id, Token{AccessToken: "a0", RefreshToken: "r0"}) // no expiry known
	tokens := NewTokens(store, ts.endpoint())

	if got, _ := tokens.Access(ctx, id); got != "a0" {
		t.Fatalf("token without expiry is used as is, got %q", got)
	}
	if got, err := tokens.Renew(ctx, id, "a0"); err != nil || got != "a1" {
		t.Fatalf("renew after 401: %q, %v", got, err)
	}
	if got, _ := tokens.Renew(ctx, id, "a0"); got != "a1" || ts.refreshes.Load() != 1 {
		t.Errorf("renewing an already replaced token must not refresh again: %q, %d", got, ts.refreshes.Load())
	}

	ts.mu.Lock()
	ts.valid = map[string]bool{} // the user revoked the application
	ts.mu.Unlock()
	if _, err := tokens.Renew(ctx, id, "a1"); !errors.Is(err, ErrRevoked) || !strings.Contains(err.Error(), "connect the app again") {
		t.Errorf("revoked: err = %v, want ErrRevoked telling to reconnect", err)
	}
	mustSave(t, store, id, Token{AccessToken: "a1", RefreshToken: "r1", Expiry: time.Now()})
	if _, err := tokens.Access(ctx, id); !errors.Is(err, ErrRevoked) || !strings.Contains(err.Error(), "connect the app again") {
		t.Errorf("revoked, found at expiry: err = %v, want ErrRevoked telling to reconnect", err)
	}

	if _, err := tokens.Access(ctx, uuid.New()); err == nil {
		t.Error("a connection without a stored token must fail")
	}
}

// apiServer answers /thing with the token it was given, after optional
// 401 / 429 answers.
type apiServer struct {
	*httptest.Server
	calls   atomic.Int32
	limited atomic.Int32 // remaining 429 answers
	retry   string
	tokens  []string
	mu      sync.Mutex
}

func newAPIServer(t *testing.T) *apiServer {
	s := &apiServer{}
	s.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.calls.Add(1)
		tok := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		s.mu.Lock()
		s.tokens = append(s.tokens, tok)
		s.mu.Unlock()
		switch {
		case r.URL.Path == "/api/missing":
			w.WriteHeader(404)
			_, _ = fmt.Fprint(w, `{"message":"404 Project Not Found"}`)
		case r.URL.Path == "/api/nested-error":
			w.WriteHeader(404)
			_, _ = fmt.Fprint(w, `{"type":"error","error":{"message":"Repository has no issue tracker."}}`)
		case tok == "stale":
			w.WriteHeader(401)
			_, _ = fmt.Fprint(w, `{"message":"401 Unauthorized"}`)
		case s.limited.Load() > 0:
			s.limited.Add(-1)
			w.Header().Set("Retry-After", s.retry)
			w.WriteHeader(429)
			_, _ = fmt.Fprint(w, `{"message":"slow down"}`)
		default:
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]string{"path": r.URL.Path, "raw": r.URL.EscapedPath()})
		}
	}))
	t.Cleanup(s.Close)
	return s
}

func (s *apiServer) client(maxWait time.Duration) *Client {
	return &Client{Tracker: "test", BaseURL: s.URL + "/api/", MaxRateLimitWait: maxWait}
}

func TestClientRenewsOn401(t *testing.T) {
	ts := newTokenServer(t, false)
	api := newAPIServer(t)
	store := newMemStore()
	id := uuid.New()
	ctx := context.Background()
	mustSave(t, store, id, Token{AccessToken: "stale", RefreshToken: "r0"})

	var out struct{ Path, Raw string }
	_, err := api.client(0).Do(ctx, NewTokens(store, ts.endpoint()).For(id), "GET", "projects/group%2Fapp", nil, &out)
	if err != nil {
		t.Fatal(err)
	}
	if api.tokens[0] != "stale" || api.tokens[1] != "a1" || store.token(id).AccessToken != "a1" {
		t.Errorf("tokens sent = %v, stored = %+v", api.tokens, store.token(id))
	}
	if out.Raw != "/api/projects/group%2Fapp" {
		t.Errorf("escaped path sent as %q; %%2F must be kept", out.Raw)
	}

	if _, err := api.client(0).Do(ctx, StaticToken("stale"), "GET", "x", nil, nil); !strings.Contains(fmt.Sprint(err), "authorization was rejected") {
		t.Errorf("a static token that is rejected: %v", err)
	}
}

func TestClientRateLimits(t *testing.T) {
	api := newAPIServer(t)
	ctx := context.Background()

	api.limited.Store(1)
	api.retry = "1"
	start := time.Now()
	if _, err := api.client(0).Do(ctx, StaticToken("t"), "POST", "issues", map[string]string{"title": "x"}, nil); err != nil {
		t.Fatalf("short 429 must be waited out: %v", err)
	}
	if time.Since(start) < time.Second || api.calls.Load() != 2 {
		t.Errorf("waited %s, %d calls", time.Since(start), api.calls.Load())
	}

	api.calls.Store(0)
	api.limited.Store(1)
	api.retry = "3600"
	start = time.Now()
	_, err := api.client(0).Do(ctx, StaticToken("t"), "POST", "issues", nil, nil)
	var rl *issue.RateLimitError
	if !errors.As(err, &rl) || rl.Wait != time.Hour || rl.Tracker != "test" {
		t.Fatalf("long 429: err = %v, want *issue.RateLimitError of 1h", err)
	}
	if time.Since(start) > 2*time.Second || api.calls.Load() != 1 {
		t.Errorf("a long limit must fail fast with one call: %s, %d calls", time.Since(start), api.calls.Load())
	}

	api.calls.Store(0)
	api.limited.Store(5)
	api.retry = "1"
	if _, err := api.client(-1).Do(ctx, StaticToken("t"), "GET", "x", nil, nil); !errors.As(err, &rl) || api.calls.Load() != 1 {
		t.Errorf("negative max wait never waits: %v, %d calls", err, api.calls.Load())
	}

	api.limited.Store(5)
	short, cancel := context.WithTimeout(ctx, 500*time.Millisecond)
	defer cancel()
	if _, err := api.client(0).Do(short, StaticToken("t"), "GET", "x", nil, nil); !errors.As(err, &rl) {
		t.Errorf("a wait past the deadline must fail fast: %v", err)
	}
}

func TestClientErrors(t *testing.T) {
	api := newAPIServer(t)
	ctx := context.Background()
	c := api.client(0)

	_, err := c.Do(ctx, StaticToken("t"), "GET", "missing", nil, nil)
	if !IsStatus(err, 404) || !strings.Contains(err.Error(), "404 Project Not Found") {
		t.Errorf("gitlab-style error: %v", err)
	}
	_, err = c.Do(ctx, StaticToken("t"), "GET", "nested-error", nil, nil)
	if !strings.Contains(fmt.Sprint(err), "Repository has no issue tracker.") {
		t.Errorf("bitbucket-style error: %v", err)
	}

	if _, err := c.Do(ctx, StaticToken("t"), "GET", "https://evil.test/api/x", nil, nil); err == nil || api.calls.Load() != 2 {
		t.Error("an absolute URL on another host must be refused before sending the token")
	}
	if _, err := c.Do(ctx, StaticToken("t"), "GET", api.URL+"/api/next?page=2", nil, nil); err != nil {
		t.Errorf("an absolute URL on the API host is allowed (pagination): %v", err)
	}
}

func TestRateLimitWait(t *testing.T) {
	h := http.Header{}
	if rateLimitWait(h) != time.Minute {
		t.Error("no hint → a minute")
	}
	h.Set("RateLimit-Reset", fmt.Sprint(time.Now().Add(90*time.Second).Unix()))
	if w := rateLimitWait(h); w < 80*time.Second || w > 91*time.Second {
		t.Errorf("RateLimit-Reset → %s", w)
	}
	h.Set("Retry-After", "0")
	if rateLimitWait(h) != time.Second {
		t.Error("Retry-After wins and is at least a second")
	}
}
