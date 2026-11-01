package bitbucket

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/MicroClub-USTHB/m-Seismo/backend/internal/db"
	"github.com/MicroClub-USTHB/m-Seismo/backend/internal/httpx"
	"github.com/MicroClub-USTHB/m-Seismo/backend/internal/secrets"
)

// maxWebhookBytes caps a webhook body; issue events are a few kilobytes.
const maxWebhookBytes = 1 << 20

// closedStates are the Bitbucket issue states that mean the bug is no
// longer open; everything else (new, open, on hold) counts as open.
var closedStates = map[string]bool{
	"resolved": true, "closed": true, "invalid": true, "duplicate": true, "wontfix": true,
}

// IssueStates records what a tracker says about the state of the issues
// m-Seismo opened. Implemented by db.Querier.
type IssueStates interface {
	SetIssueStateByExternalID(ctx context.Context, arg db.SetIssueStateByExternalIDParams) (int64, error)
}

// WebhookSecrets looks up the webhook secrets of every project repository
// linked to a Bitbucket repository. Implemented by db.Querier.
type WebhookSecrets interface {
	ListProjectRepositoryWebhookSecrets(ctx context.Context, arg db.ListProjectRepositoryWebhookSecretsParams) ([]db.ListProjectRepositoryWebhookSecretsRow, error)
}

// WebhookHandler receives Bitbucket's issue:updated webhook (CreateWebhook,
// FR-56), and records the new state of the issue m-Seismo opened.
//
// Every delivery is signed with the webhook's own secret
// (X-Hub-Signature, HMAC-SHA256 of the body); one without a valid
// signature is refused. A repository can be linked to more than one
// project, each with its own webhook, so every candidate secret is tried.
type WebhookHandler struct {
	secrets WebhookSecrets
	cipher  *secrets.Cipher
	states  IssueStates
}

func NewWebhookHandler(secrets WebhookSecrets, cipher *secrets.Cipher, states IssueStates) *WebhookHandler {
	return &WebhookHandler{secrets: secrets, cipher: cipher, states: states}
}

func (h *WebhookHandler) Mount(r chi.Router) {
	r.Post("/webhooks/bitbucket", h.receive)
}

// issueUpdatedEvent is the part of Bitbucket's issue:updated payload
// m-Seismo reads.
type issueUpdatedEvent struct {
	Repository struct {
		FullName string `json:"full_name"`
	} `json:"repository"`
	Issue struct {
		ID    int    `json:"id"`
		State string `json:"state"`
	} `json:"issue"`
}

func (h *WebhookHandler) receive(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxWebhookBytes))
	if err != nil {
		var tooBig *http.MaxBytesError
		if errors.As(err, &tooBig) {
			httpx.JSON(w, http.StatusRequestEntityTooLarge, map[string]string{"error": "payload too large"})
			return
		}
		httpx.JSON(w, http.StatusBadRequest, map[string]string{"error": "unreadable body"})
		return
	}
	// Every other event is acknowledged and ignored, so a webhook with more
	// than issue:updated enabled doesn't show failed deliveries.
	if r.Header.Get("X-Event-Key") != "issue:updated" {
		httpx.NoContent(w)
		return
	}
	var ev issueUpdatedEvent
	if err := json.Unmarshal(body, &ev); err != nil {
		httpx.JSON(w, http.StatusBadRequest, map[string]string{"error": "invalid issue event"})
		return
	}
	if ev.Repository.FullName == "" {
		httpx.NoContent(w)
		return
	}

	candidates, err := h.secrets.ListProjectRepositoryWebhookSecrets(r.Context(), db.ListProjectRepositoryWebhookSecretsParams{
		Tracker: db.TrackerBitbucket, Repository: ev.Repository.FullName,
	})
	if err != nil {
		slog.ErrorContext(r.Context(), "bitbucket webhook: load webhook secrets", "repository", ev.Repository.FullName, "err", err)
		httpx.JSON(w, http.StatusInternalServerError, map[string]string{"error": "internal error"})
		return
	}
	if !h.signatureMatches(candidates, body, r.Header.Get("X-Hub-Signature")) {
		httpx.JSON(w, http.StatusUnauthorized, map[string]string{"error": "invalid signature"})
		return
	}

	if ev.Issue.ID <= 0 {
		httpx.NoContent(w)
		return
	}
	state := "open"
	if closedStates[ev.Issue.State] {
		state = "closed"
	}

	ref := ev.Repository.FullName + "#" + strconv.Itoa(ev.Issue.ID)
	n, err := h.states.SetIssueStateByExternalID(r.Context(), db.SetIssueStateByExternalIDParams{
		State:      state,
		Tracker:    db.TrackerBitbucket,
		ExternalID: ref,
	})
	if err != nil {
		slog.ErrorContext(r.Context(), "bitbucket webhook: record issue state", "issue", ref, "err", err)
		httpx.JSON(w, http.StatusInternalServerError, map[string]string{"error": "internal error"})
		return
	}
	// n is 0 for an issue m-Seismo didn't open, or already in that state.
	if n > 0 {
		slog.InfoContext(r.Context(), "bitbucket issue "+ev.Issue.State, "issue", ref)
	}
	httpx.NoContent(w)
}

// signatureMatches reports whether header is "sha256=<hex HMAC-SHA256 of
// body>" for any of candidates' secrets, in constant time per comparison.
func (h *WebhookHandler) signatureMatches(candidates []db.ListProjectRepositoryWebhookSecretsRow, body []byte, header string) bool {
	got, ok := strings.CutPrefix(header, "sha256=")
	if !ok {
		return false
	}
	sig, err := hex.DecodeString(got)
	if err != nil {
		return false
	}
	for _, c := range candidates {
		plain, err := h.cipher.Open(c.WebhookSecret, c.ID[:])
		if err != nil {
			continue
		}
		mac := hmac.New(sha256.New, plain)
		mac.Write(body)
		if hmac.Equal(sig, mac.Sum(nil)) {
			return true
		}
	}
	return false
}
