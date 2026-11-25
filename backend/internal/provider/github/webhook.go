package github

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

	"github.com/MicroClub-USTHB/m-Seismo/backend/internal/config"
	"github.com/MicroClub-USTHB/m-Seismo/backend/internal/db"
	"github.com/MicroClub-USTHB/m-Seismo/backend/internal/httpx"
)

// maxWebhookBytes caps a webhook body. Issue events are a few kilobytes;
// GitHub caps payloads at 25 MB, mostly for push events we don't take.
const maxWebhookBytes = 1 << 20

// IssueStates records what a tracker says about the state of the issues
// m-Seismo opened. Implemented by db.Querier.
type IssueStates interface {
	SetIssueStateByExternalID(ctx context.Context, arg db.SetIssueStateByExternalIDParams) (int64, error)
}

// WebhookHandler receives the GitHub App's webhooks. GitHub calls it when
// an issue in a repository the App is installed on is closed or reopened,
// and m-Seismo records the new state of its issue: a closed issue means
// the bug of its reports is fixed.
//
// Every delivery is signed with the App's webhook secret
// (X-Hub-Signature-256, HMAC-SHA256 of the body); one without a valid
// signature is refused.
type WebhookHandler struct {
	secret []byte
	states IssueStates
}

func NewWebhookHandler(cfg *config.Config, states IssueStates) *WebhookHandler {
	return &WebhookHandler{secret: []byte(cfg.GitHub.WebhookSecret), states: states}
}

func (h *WebhookHandler) Mount(r chi.Router) {
	r.Post("/webhooks/github", h.receive)
}

// issuesEvent is the part of an "issues" event m-Seismo reads.
type issuesEvent struct {
	Action string `json:"action"`
	Issue  struct {
		Number int `json:"number"`
	} `json:"issue"`
	Repository struct {
		FullName string `json:"full_name"`
	} `json:"repository"`
}

func (h *WebhookHandler) receive(w http.ResponseWriter, r *http.Request) {
	if len(h.secret) == 0 {
		httpx.JSON(w, http.StatusNotFound, map[string]string{"error": "github webhooks are not configured"})
		return
	}
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
	if !validSignature(h.secret, body, r.Header.Get("X-Hub-Signature-256")) {
		httpx.JSON(w, http.StatusUnauthorized, map[string]string{"error": "invalid signature"})
		return
	}

	delivery := r.Header.Get("X-GitHub-Delivery")
	// Every other event is acknowledged and ignored, so GitHub doesn't
	// report failed deliveries for events the App sends anyway (ping,
	// installation, ...).
	if r.Header.Get("X-GitHub-Event") != "issues" {
		httpx.NoContent(w)
		return
	}
	var ev issuesEvent
	if err := json.Unmarshal(body, &ev); err != nil {
		httpx.JSON(w, http.StatusBadRequest, map[string]string{"error": "invalid issues event"})
		return
	}
	state, ok := map[string]string{"closed": "closed", "reopened": "open"}[ev.Action]
	if !ok || ev.Repository.FullName == "" || ev.Issue.Number <= 0 {
		httpx.NoContent(w)
		return
	}

	ref := ev.Repository.FullName + "#" + strconv.Itoa(ev.Issue.Number)
	n, err := h.states.SetIssueStateByExternalID(r.Context(), db.SetIssueStateByExternalIDParams{
		State:      state,
		Tracker:    db.TrackerGithub,
		ExternalID: ref,
	})
	if err != nil {
		// GitHub shows the failed delivery, which can be redelivered.
		slog.ErrorContext(r.Context(), "github webhook: record issue state", "delivery", delivery, "issue", ref, "err", err)
		httpx.JSON(w, http.StatusInternalServerError, map[string]string{"error": "internal error"})
		return
	}
	// n is 0 for an issue m-Seismo didn't open, or already in that state.
	if n > 0 {
		slog.InfoContext(r.Context(), "github issue "+ev.Action, "issue", ref, "delivery", delivery)
	}
	httpx.NoContent(w)
}

// validSignature checks GitHub's "sha256=<hex HMAC-SHA256 of body>" header
// in constant time.
func validSignature(secret, body []byte, header string) bool {
	got, ok := strings.CutPrefix(header, "sha256=")
	if !ok {
		return false
	}
	sig, err := hex.DecodeString(got)
	if err != nil {
		return false
	}
	mac := hmac.New(sha256.New, secret)
	mac.Write(body)
	return hmac.Equal(sig, mac.Sum(nil))
}
