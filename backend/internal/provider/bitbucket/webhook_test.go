package bitbucket

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/MicroClub-USTHB/m-Seismo/backend/internal/db"
	"github.com/MicroClub-USTHB/m-Seismo/backend/internal/secrets"
)

func testCipher(t *testing.T) *secrets.Cipher {
	t.Helper()
	c, err := secrets.New(bytes.Repeat([]byte{7}, secrets.KeySize))
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func sealedSecret(t *testing.T, c *secrets.Cipher, id uuid.UUID, plaintext string) []byte {
	t.Helper()
	sealed, err := c.Seal([]byte(plaintext), id[:])
	if err != nil {
		t.Fatal(err)
	}
	return sealed
}

func sign(secret, body string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(body))
	return "sha256=" + hex.EncodeToString(mac.Sum(nil))
}

type fakeSecrets struct {
	rows []db.ListProjectRepositoryWebhookSecretsRow
}

func (f *fakeSecrets) ListProjectRepositoryWebhookSecrets(context.Context, db.ListProjectRepositoryWebhookSecretsParams) ([]db.ListProjectRepositoryWebhookSecretsRow, error) {
	return f.rows, nil
}

type fakeStates struct {
	calls []db.SetIssueStateByExternalIDParams
}

func (f *fakeStates) SetIssueStateByExternalID(_ context.Context, arg db.SetIssueStateByExternalIDParams) (int64, error) {
	f.calls = append(f.calls, arg)
	return 1, nil
}

func deliver(t *testing.T, secretsLookup WebhookSecrets, cipher *secrets.Cipher, states IssueStates, body, signature string) int {
	t.Helper()
	return deliverEvent(t, secretsLookup, cipher, states, "issue:updated", body, signature)
}

func deliverEvent(t *testing.T, secretsLookup WebhookSecrets, cipher *secrets.Cipher, states IssueStates, event, body, signature string) int {
	t.Helper()
	r := chi.NewRouter()
	NewWebhookHandler(secretsLookup, cipher, states).Mount(r)
	req := httptest.NewRequest(http.MethodPost, "/webhooks/bitbucket", strings.NewReader(body))
	req.Header.Set("X-Event-Key", event)
	req.Header.Set("X-Hub-Signature", signature)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	return rec.Code
}

func issueUpdatedBody(state string) string {
	return `{"repository":{"full_name":"workspace/repo"},"issue":{"id":7,"state":"` + state + `"}}`
}

func TestWebhookRecordsIssueState(t *testing.T) {
	cipher := testCipher(t)
	id := uuid.New()
	secretsLookup := &fakeSecrets{rows: []db.ListProjectRepositoryWebhookSecretsRow{
		{ID: id, WebhookSecret: sealedSecret(t, cipher, id, "the-secret")},
	}}

	for state, want := range map[string]string{"resolved": "closed", "closed": "closed", "open": "open", "new": "open"} {
		states := &fakeStates{}
		body := issueUpdatedBody(state)
		if code := deliver(t, secretsLookup, cipher, states, body, sign("the-secret", body)); code != http.StatusNoContent {
			t.Fatalf("%s: status %d", state, code)
		}
		if len(states.calls) != 1 {
			t.Fatalf("%s: %d updates", state, len(states.calls))
		}
		if c := states.calls[0]; c.State != want || c.Tracker != db.TrackerBitbucket || c.ExternalID != "workspace/repo#7" {
			t.Errorf("%s: update %+v", state, c)
		}
	}
}

func TestWebhookChecksEveryCandidateSecret(t *testing.T) {
	cipher := testCipher(t)
	other, mine := uuid.New(), uuid.New()
	secretsLookup := &fakeSecrets{rows: []db.ListProjectRepositoryWebhookSecretsRow{
		{ID: other, WebhookSecret: sealedSecret(t, cipher, other, "not-this-one")},
		{ID: mine, WebhookSecret: sealedSecret(t, cipher, mine, "the-secret")},
	}}
	states := &fakeStates{}
	body := issueUpdatedBody("closed")
	if code := deliver(t, secretsLookup, cipher, states, body, sign("the-secret", body)); code != http.StatusNoContent {
		t.Fatalf("status %d", code)
	}
	if len(states.calls) != 1 {
		t.Fatalf("%d updates", len(states.calls))
	}
}

func TestWebhookRefusesBadSignatures(t *testing.T) {
	cipher := testCipher(t)
	id := uuid.New()
	secretsLookup := &fakeSecrets{rows: []db.ListProjectRepositoryWebhookSecretsRow{
		{ID: id, WebhookSecret: sealedSecret(t, cipher, id, "the-secret")},
	}}
	body := issueUpdatedBody("closed")
	for name, sig := range map[string]string{
		"missing":      "",
		"other secret": sign("guess", body),
		"other body":   sign("the-secret", body+" "),
		"not hex":      "sha256=zz",
	} {
		states := &fakeStates{}
		if code := deliver(t, secretsLookup, cipher, states, body, sig); code != http.StatusUnauthorized {
			t.Errorf("%s: status %d, want 401", name, code)
		}
		if len(states.calls) != 0 {
			t.Errorf("%s: recorded %+v", name, states.calls)
		}
	}
}

func TestWebhookIgnoresOtherEventsWithoutCheckingTheSignature(t *testing.T) {
	cipher := testCipher(t)
	states := &fakeStates{}
	if code := deliverEvent(t, &fakeSecrets{}, cipher, states, "repo:push", `{"push":{}}`, ""); code != http.StatusNoContent {
		t.Errorf("status %d", code)
	}
}
