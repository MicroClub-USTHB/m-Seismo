package gitlab

import (
	"bytes"
	"context"
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

// sealedSecret encrypts plaintext the same way WebhookService.Ensure does,
// so tests can seed a row ListProjectRepositoryWebhookSecrets would return.
func sealedSecret(t *testing.T, c *secrets.Cipher, id uuid.UUID, plaintext string) []byte {
	t.Helper()
	sealed, err := c.Seal([]byte(plaintext), id[:])
	if err != nil {
		t.Fatal(err)
	}
	return sealed
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

func deliver(t *testing.T, secretsLookup WebhookSecrets, cipher *secrets.Cipher, states IssueStates, body, token string) int {
	t.Helper()
	r := chi.NewRouter()
	NewWebhookHandler(secretsLookup, cipher, states).Mount(r)
	req := httptest.NewRequest(http.MethodPost, "/webhooks/gitlab", strings.NewReader(body))
	req.Header.Set("X-Gitlab-Token", token)
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	return rec.Code
}

const issueHookBody = `{"object_kind":"issue","project":{"path_with_namespace":"group/project"},"object_attributes":{"action":"close","iid":7}}`

func TestWebhookRecordsIssueState(t *testing.T) {
	cipher := testCipher(t)
	id := uuid.New()
	secretsLookup := &fakeSecrets{rows: []db.ListProjectRepositoryWebhookSecretsRow{
		{ID: id, WebhookSecret: sealedSecret(t, cipher, id, "the-token")},
	}}

	for action, want := range map[string]string{"close": "closed", "reopen": "open"} {
		states := &fakeStates{}
		body := strings.Replace(issueHookBody, `"action":"close"`, `"action":"`+action+`"`, 1)
		if code := deliver(t, secretsLookup, cipher, states, body, "the-token"); code != http.StatusNoContent {
			t.Fatalf("%s: status %d", action, code)
		}
		if len(states.calls) != 1 {
			t.Fatalf("%s: %d updates", action, len(states.calls))
		}
		if c := states.calls[0]; c.State != want || c.Tracker != db.TrackerGitlab || c.ExternalID != "group/project#7" {
			t.Errorf("%s: update %+v", action, c)
		}
	}
}

func TestWebhookChecksEveryCandidateSecret(t *testing.T) {
	cipher := testCipher(t)
	other, mine := uuid.New(), uuid.New()
	secretsLookup := &fakeSecrets{rows: []db.ListProjectRepositoryWebhookSecretsRow{
		{ID: other, WebhookSecret: sealedSecret(t, cipher, other, "not-this-one")},
		{ID: mine, WebhookSecret: sealedSecret(t, cipher, mine, "the-token")},
	}}
	states := &fakeStates{}
	if code := deliver(t, secretsLookup, cipher, states, issueHookBody, "the-token"); code != http.StatusNoContent {
		t.Fatalf("status %d", code)
	}
	if len(states.calls) != 1 {
		t.Fatalf("%d updates", len(states.calls))
	}
}

func TestWebhookRefusesBadTokens(t *testing.T) {
	cipher := testCipher(t)
	id := uuid.New()
	secretsLookup := &fakeSecrets{rows: []db.ListProjectRepositoryWebhookSecretsRow{
		{ID: id, WebhookSecret: sealedSecret(t, cipher, id, "the-token")},
	}}
	for name, token := range map[string]string{"missing": "", "wrong": "guess"} {
		states := &fakeStates{}
		if code := deliver(t, secretsLookup, cipher, states, issueHookBody, token); code != http.StatusUnauthorized {
			t.Errorf("%s: status %d, want 401", name, code)
		}
		if len(states.calls) != 0 {
			t.Errorf("%s: recorded %+v", name, states.calls)
		}
	}
}

func TestWebhookIgnoresOtherEventsWithoutCheckingTheToken(t *testing.T) {
	cipher := testCipher(t)
	states := &fakeStates{}
	body := `{"object_kind":"push","project":{"path_with_namespace":"group/project"}}`
	if code := deliver(t, &fakeSecrets{}, cipher, states, body, ""); code != http.StatusNoContent {
		t.Errorf("status %d", code)
	}
}
