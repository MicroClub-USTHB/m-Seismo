package gitlab

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"

	"github.com/MicroClub-USTHB/m-Seismo/backend/internal/issue"
	"github.com/MicroClub-USTHB/m-Seismo/backend/internal/provider"
	"github.com/MicroClub-USTHB/m-Seismo/backend/internal/provider/oauth"
)

// fakeStore answers Load with a non-expiring access token, so Tokens.For
// never needs a real OAuth endpoint to refresh through.
type fakeStore struct{}

func (fakeStore) Load(context.Context, uuid.UUID) ([]byte, error) {
	return oauth.Token{AccessToken: "tok"}.Encode(), nil
}
func (fakeStore) Save(context.Context, uuid.UUID, []byte) error { return nil }

func newTestProvider(t *testing.T, handler http.HandlerFunc) (*Provider, issue.Connection) {
	t.Helper()
	ts := httptest.NewServer(handler)
	t.Cleanup(ts.Close)
	p, err := New(Options{APIURL: ts.URL + "/", WebhookURL: "https://seismo.example.com/api/v1/webhooks/gitlab"}, fakeStore{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	return p, issue.Connection{ID: uuid.New(), AppID: uuid.New()}
}

func TestCreateWebhook(t *testing.T) {
	var gotPath, gotToken, gotURL string
	p, c := newTestProvider(t, func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.EscapedPath()
		var body struct {
			URL                   string `json:"url"`
			Token                 string `json:"token"`
			IssuesEvents          bool   `json:"issues_events"`
			EnableSSLVerification bool   `json:"enable_ssl_verification"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		gotToken, gotURL = body.Token, body.URL
		if !body.IssuesEvents || !body.EnableSSLVerification {
			t.Errorf("issues_events/enable_ssl_verification = %+v", body)
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]int{"id": 42})
	})

	hook, err := p.CreateWebhook(context.Background(), c, "group/project")
	if err != nil {
		t.Fatalf("CreateWebhook() err = %v", err)
	}
	if hook.ExternalID != "42" {
		t.Errorf("ExternalID = %q", hook.ExternalID)
	}
	if hook.Secret == "" || hook.Secret != gotToken {
		t.Errorf("Secret = %q, sent token = %q", hook.Secret, gotToken)
	}
	if gotPath != "/projects/group%2Fproject/hooks" {
		t.Errorf("path = %q", gotPath)
	}
	if gotURL != "https://seismo.example.com/api/v1/webhooks/gitlab" {
		t.Errorf("url = %q", gotURL)
	}
}

func TestCreateWebhookInvalidRepository(t *testing.T) {
	p, c := newTestProvider(t, func(http.ResponseWriter, *http.Request) {
		t.Fatal("API called for an invalid repository")
	})
	if _, err := p.CreateWebhook(context.Background(), c, "not-a-path"); err == nil {
		t.Error("CreateWebhook() with an invalid repository: err = nil")
	}
}

func TestDeleteWebhook(t *testing.T) {
	var gotMethod, gotPath string
	p, c := newTestProvider(t, func(w http.ResponseWriter, r *http.Request) {
		gotMethod, gotPath = r.Method, r.URL.EscapedPath()
		w.WriteHeader(http.StatusNoContent)
	})
	if err := p.DeleteWebhook(context.Background(), c, "group/project", provider.Webhook{ExternalID: "42"}); err != nil {
		t.Fatalf("DeleteWebhook() err = %v", err)
	}
	if gotMethod != http.MethodDelete || gotPath != "/projects/group%2Fproject/hooks/42" {
		t.Errorf("method = %q, path = %q", gotMethod, gotPath)
	}
}
