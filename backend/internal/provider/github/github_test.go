package github

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/MicroClub-USTHB/m-Seismo/backend/internal/apperr"
	"github.com/MicroClub-USTHB/m-Seismo/backend/internal/issue"
)

// fakeGitHub serves the handful of GitHub endpoints the provider calls,
// for both the web (OAuth) and API base URLs.
type fakeGitHub struct {
	mu          sync.Mutex
	createdBody map[string]any
	comments    []string
}

func (f *fakeGitHub) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	auth := r.Header.Get("Authorization")
	switch {
	case r.Method == http.MethodPost && r.URL.Path == "/login/oauth/access_token":
		_ = r.ParseForm()
		if r.Form.Get("code") != "good-code" || r.Form.Get("client_secret") != "secret" {
			writeJSON(w, http.StatusOK, map[string]any{"error": "bad_verification_code"})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"access_token": "user-token", "token_type": "bearer"})

	case r.Method == http.MethodGet && r.URL.Path == "/user/installations":
		if auth != "Bearer user-token" {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"total_count": 1,
			"installations": []any{
				map[string]any{"id": 42, "account": map[string]any{"login": "octo-org", "type": "Organization"}},
			},
		})

	case r.Method == http.MethodPost && r.URL.Path == "/app/installations/42/access_tokens":
		if !strings.HasPrefix(auth, "Bearer ") {
			http.Error(w, "missing app jwt", http.StatusUnauthorized)
			return
		}
		writeJSON(w, http.StatusCreated, map[string]any{
			"token":      "installation-token",
			"expires_at": time.Now().Add(time.Hour).UTC().Format(time.RFC3339),
		})

	case auth != "token installation-token" && auth != "Bearer installation-token":
		http.Error(w, "unexpected auth "+auth+" for "+r.URL.Path, http.StatusUnauthorized)

	case r.Method == http.MethodGet && r.URL.Path == "/installation/repositories":
		writeJSON(w, http.StatusOK, map[string]any{
			"total_count": 1,
			"repositories": []any{
				map[string]any{"full_name": "octo-org/app", "private": true, "html_url": "https://github.test/octo-org/app"},
			},
		})

	case r.Method == http.MethodGet && r.URL.Path == "/repos/octo-org/app":
		writeJSON(w, http.StatusOK, map[string]any{"full_name": "octo-org/app"})

	case r.Method == http.MethodGet && r.URL.Path == "/repos/octo-org/app/milestones":
		writeJSON(w, http.StatusOK, []any{map[string]any{"number": 3, "title": "v1.0"}})

	case r.Method == http.MethodPost && r.URL.Path == "/repos/octo-org/app/issues":
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		f.mu.Lock()
		f.createdBody = body
		f.mu.Unlock()
		writeJSON(w, http.StatusCreated, map[string]any{"number": 7, "html_url": "https://github.test/octo-org/app/issues/7", "state": "open"})

	case r.Method == http.MethodPost && r.URL.Path == "/repos/octo-org/app/issues/7/comments":
		b, _ := io.ReadAll(r.Body)
		f.mu.Lock()
		f.comments = append(f.comments, string(b))
		f.mu.Unlock()
		writeJSON(w, http.StatusCreated, map[string]any{"id": 1})

	case r.Method == http.MethodGet && r.URL.Path == "/repos/octo-org/app/issues/7":
		writeJSON(w, http.StatusOK, map[string]any{"number": 7, "state": "closed"})

	default:
		writeJSON(w, http.StatusNotFound, map[string]any{"message": "Not Found"})
	}
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func newTestProvider(t *testing.T) (*Provider, *fakeGitHub) {
	t.Helper()
	fake := &fakeGitHub{}
	srv := httptest.NewServer(fake)
	t.Cleanup(srv.Close)

	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)})

	p, err := New(Options{
		AppID:        1,
		AppSlug:      "m-seismo",
		ClientID:     "client",
		ClientSecret: "secret",
		PrivateKey:   keyPEM,
		APIURL:       srv.URL,
		WebURL:       srv.URL,
	})
	if err != nil {
		t.Fatal(err)
	}
	return p, fake
}

func TestInstallURL(t *testing.T) {
	p, _ := newTestProvider(t)
	got := p.InstallURL("a b")
	if !strings.HasSuffix(got, "/apps/m-seismo/installations/new?state=a+b") {
		t.Errorf("InstallURL = %s", got)
	}
}

func TestCompleteInstall(t *testing.T) {
	p, _ := newTestProvider(t)
	ctx := context.Background()

	acct, err := p.CompleteInstall(ctx, url.Values{"installation_id": {"42"}, "code": {"good-code"}, "setup_action": {"install"}})
	if err != nil {
		t.Fatal(err)
	}
	if acct.ExternalID != "42" || acct.Login != "octo-org" || acct.Type != "Organization" {
		t.Errorf("account = %+v", acct)
	}

	rejects := map[string]url.Values{
		"forged installation id": {"installation_id": {"99"}, "code": {"good-code"}},
		"bad code":               {"installation_id": {"42"}, "code": {"bad-code"}},
		"missing code":           {"installation_id": {"42"}},
		"missing installation":   {"code": {"good-code"}},
		"pending request":        {"setup_action": {"request"}},
	}
	for name, q := range rejects {
		t.Run(name, func(t *testing.T) {
			_, err := p.CompleteInstall(ctx, q)
			if kind, _ := apperr.Classify(err); kind != apperr.KindInvalid {
				t.Errorf("err = %v, want an invalid-input error", err)
			}
		})
	}
}

func TestRepositories(t *testing.T) {
	p, _ := newTestProvider(t)
	ctx := context.Background()

	repos, err := p.ListRepositories(ctx, "42")
	if err != nil {
		t.Fatal(err)
	}
	if len(repos) != 1 || repos[0].FullName != "octo-org/app" || !repos[0].Private {
		t.Errorf("repos = %+v", repos)
	}

	if ok, err := p.HasRepository(ctx, "42", "octo-org/app"); err != nil || !ok {
		t.Errorf("HasRepository(octo-org/app) = %v, %v", ok, err)
	}
	if ok, err := p.HasRepository(ctx, "42", "octo-org/other"); err != nil || ok {
		t.Errorf("HasRepository(octo-org/other) = %v, %v", ok, err)
	}
	if _, err := p.HasRepository(ctx, "42", "not-a-repo"); err == nil {
		t.Error("malformed repository accepted")
	}
}

func TestOpener(t *testing.T) {
	p, fake := newTestProvider(t)
	ctx := context.Background()

	op, err := p.Opener(ctx, "42")
	if err != nil {
		t.Fatal(err)
	}

	ref, err := op.Open(ctx, issue.Issue{
		Repository: "octo-org/app",
		Title:      "Crash on login",
		Body:       "details",
		Labels:     []string{"bug", "m-seismo"},
		Milestone:  "v1.0",
	})
	if err != nil {
		t.Fatal(err)
	}
	if ref.ID != "octo-org/app#7" || ref.URL != "https://github.test/octo-org/app/issues/7" {
		t.Errorf("ref = %+v", ref)
	}
	body := fake.createdBody
	if body["title"] != "Crash on login" || body["body"] != "details" || body["milestone"] != float64(3) {
		t.Errorf("create body = %v", body)
	}

	if err := op.Comment(ctx, ref, "seen again"); err != nil {
		t.Fatal(err)
	}
	if len(fake.comments) != 1 || !strings.Contains(fake.comments[0], "seen again") {
		t.Errorf("comments = %v", fake.comments)
	}

	open, err := op.IsOpen(ctx, ref)
	if err != nil || open {
		t.Errorf("IsOpen = %v, %v; want closed", open, err)
	}

	if _, err := op.IsOpen(ctx, issue.Ref{ID: "garbage"}); err == nil {
		t.Error("malformed ref accepted")
	}
}
