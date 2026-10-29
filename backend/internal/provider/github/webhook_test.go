package github

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"

	"github.com/MicroClub-USTHB/m-Seismo/backend/internal/config"
	"github.com/MicroClub-USTHB/m-Seismo/backend/internal/db"
)

type fakeStates struct {
	calls []db.SetIssueStateByExternalIDParams
	err   error
}

func (f *fakeStates) SetIssueStateByExternalID(_ context.Context, arg db.SetIssueStateByExternalIDParams) (int64, error) {
	f.calls = append(f.calls, arg)
	return 1, f.err
}

const testSecret = "It's a Secret to Everybody"

func sign(secret, body string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(body))
	return "sha256=" + hex.EncodeToString(mac.Sum(nil))
}

func deliver(t *testing.T, secret string, states IssueStates, event, body, signature string) int {
	t.Helper()
	r := chi.NewRouter()
	NewWebhookHandler(&config.Config{GitHub: config.GitHubConfig{WebhookSecret: secret}}, states).Mount(r)
	req := httptest.NewRequest(http.MethodPost, "/webhooks/github", strings.NewReader(body))
	req.Header.Set("X-GitHub-Event", event)
	req.Header.Set("X-GitHub-Delivery", "72d3162e")
	req.Header.Set("X-Hub-Signature-256", signature)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	return rec.Code
}

func issuesEventBody(action string) string {
	return `{"action":"` + action + `","issue":{"number":42,"state":"closed"},"repository":{"full_name":"Octo-Org/Mobile"},"sender":{"login":"octocat"}}`
}

func TestWebhookRecordsIssueState(t *testing.T) {
	for action, want := range map[string]string{"closed": "closed", "reopened": "open"} {
		states := &fakeStates{}
		body := issuesEventBody(action)
		if code := deliver(t, testSecret, states, "issues", body, sign(testSecret, body)); code != http.StatusNoContent {
			t.Fatalf("%s: status %d", action, code)
		}
		if len(states.calls) != 1 {
			t.Fatalf("%s: %d updates", action, len(states.calls))
		}
		if c := states.calls[0]; c.State != want || c.Tracker != db.TrackerGithub || c.ExternalID != "Octo-Org/Mobile#42" {
			t.Errorf("%s: update %+v", action, c)
		}
	}
}

func TestWebhookIgnoresOtherEvents(t *testing.T) {
	states := &fakeStates{}
	for _, tc := range []struct{ event, body string }{
		{"ping", `{"zen":"Keep it logically awesome."}`},
		{"installation", `{"action":"deleted"}`},
		{"issues", issuesEventBody("labeled")},
		{"issues", `{"action":"closed","issue":{"number":0},"repository":{"full_name":"o/r"}}`},
	} {
		if code := deliver(t, testSecret, states, tc.event, tc.body, sign(testSecret, tc.body)); code != http.StatusNoContent {
			t.Errorf("%s %s: status %d", tc.event, tc.body, code)
		}
	}
	if len(states.calls) != 0 {
		t.Errorf("recorded %+v", states.calls)
	}
}

func TestWebhookRefusesBadSignatures(t *testing.T) {
	body := issuesEventBody("closed")
	for name, sig := range map[string]string{
		"missing":          "",
		"other secret":     sign("guess", body),
		"other body":       sign(testSecret, body+" "),
		"sha1 header form": strings.Replace(sign(testSecret, body), "sha256=", "sha1=", 1),
		"not hex":          "sha256=zz",
	} {
		states := &fakeStates{}
		if code := deliver(t, testSecret, states, "issues", body, sig); code != http.StatusUnauthorized {
			t.Errorf("%s: status %d, want 401", name, code)
		}
		if len(states.calls) != 0 {
			t.Errorf("%s: recorded %+v", name, states.calls)
		}
	}
}

func TestWebhookErrors(t *testing.T) {
	body := issuesEventBody("closed")
	if code := deliver(t, "", &fakeStates{}, "issues", body, sign("", body)); code != http.StatusNotFound {
		t.Errorf("without a secret: status %d, want 404", code)
	}
	big := strings.Repeat("x", maxWebhookBytes+1)
	if code := deliver(t, testSecret, &fakeStates{}, "issues", big, sign(testSecret, big)); code != http.StatusRequestEntityTooLarge {
		t.Errorf("too large: status %d, want 413", code)
	}
	if code := deliver(t, testSecret, &fakeStates{}, "issues", "{", sign(testSecret, "{")); code != http.StatusBadRequest {
		t.Errorf("invalid JSON: status %d, want 400", code)
	}
	failing := &fakeStates{err: errors.New("database down")}
	if code := deliver(t, testSecret, failing, "issues", body, sign(testSecret, body)); code != http.StatusInternalServerError {
		t.Errorf("database down: status %d, want 500 so GitHub shows a failed delivery", code)
	}
}

// GitHub's own example: https://docs.github.com/en/webhooks/using-webhooks/validating-webhook-deliveries
func TestValidSignatureGitHubExample(t *testing.T) {
	sig := "sha256=757107ea0eb2509fc211221cce984b8a37570b6d7586c22c46f4379c8b043e17"
	if !validSignature([]byte(testSecret), []byte("Hello, World!"), sig) {
		t.Error("GitHub's documented example does not validate")
	}
}
