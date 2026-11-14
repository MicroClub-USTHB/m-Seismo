package bitbucket

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
	p, err := New(Options{APIURL: ts.URL + "/", WebhookURL: "https://seismo.example.com/api/v1/webhooks/bitbucket"}, fakeStore{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	return p, issue.Connection{ID: uuid.New(), AppID: uuid.New()}
}

func TestCreateWebhook(t *testing.T) {
	var gotPath string
	var body struct {
		URL    string   `json:"url"`
		Active bool     `json:"active"`
		Events []string `json:"events"`
		Secret string   `json:"secret"`
	}
	p, c := newTestProvider(t, func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.EscapedPath()
		_ = json.NewDecoder(r.Body).Decode(&body)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"uuid": "{abc-123}"})
	})

	hook, err := p.CreateWebhook(context.Background(), c, "workspace/repo")
	if err != nil {
		t.Fatalf("CreateWebhook() err = %v", err)
	}
	if hook.ExternalID != "{abc-123}" {
		t.Errorf("ExternalID = %q", hook.ExternalID)
	}
	if hook.Secret == "" || hook.Secret != body.Secret {
		t.Errorf("Secret = %q, sent secret = %q", hook.Secret, body.Secret)
	}
	if !body.Active || len(body.Events) != 1 || body.Events[0] != "issue:updated" {
		t.Errorf("active/events = %+v", body)
	}
	if body.URL != "https://seismo.example.com/api/v1/webhooks/bitbucket" {
		t.Errorf("url = %q", body.URL)
	}
	if gotPath != "/repositories/workspace/repo/hooks" {
		t.Errorf("path = %q", gotPath)
	}
}

func TestCreateWebhookInvalidRepository(t *testing.T) {
	p, c := newTestProvider(t, func(http.ResponseWriter, *http.Request) {
		t.Fatal("API called for an invalid repository")
	})
	if _, err := p.CreateWebhook(context.Background(), c, "not-a-repo"); err == nil {
		t.Error("CreateWebhook() with an invalid repository: err = nil")
	}
}

func TestDeleteWebhook(t *testing.T) {
	var gotMethod, gotPath string
	p, c := newTestProvider(t, func(w http.ResponseWriter, r *http.Request) {
		gotMethod, gotPath = r.Method, r.URL.EscapedPath()
		w.WriteHeader(http.StatusNoContent)
	})
	if err := p.DeleteWebhook(context.Background(), c, "workspace/repo", provider.Webhook{ExternalID: "{abc-123}"}); err != nil {
		t.Fatalf("DeleteWebhook() err = %v", err)
	}
	if gotMethod != http.MethodDelete || gotPath != "/repositories/workspace/repo/hooks/%7Babc-123%7D" {
		t.Errorf("method = %q, path = %q", gotMethod, gotPath)
	}
}
