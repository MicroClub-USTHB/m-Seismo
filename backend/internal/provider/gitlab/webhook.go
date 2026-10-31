package gitlab

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"github.com/MicroClub-USTHB/m-Seismo/backend/internal/db"
	"github.com/MicroClub-USTHB/m-Seismo/backend/internal/httpx"
	"github.com/MicroClub-USTHB/m-Seismo/backend/internal/secrets"
)

// maxWebhookBytes caps a webhook body; issue events are a few kilobytes.
const maxWebhookBytes = 1 << 20

// IssueStates records what a tracker says about the state of the issues
// m-Seismo opened. Implemented by db.Querier.
type IssueStates interface {
	SetIssueStateByExternalID(ctx context.Context, arg db.SetIssueStateByExternalIDParams) (int64, error)
}

// WebhookSecrets looks up the webhook secrets of every project repository
// linked to a GitLab project path. Implemented by db.Querier.
type WebhookSecrets interface {
	ListProjectRepositoryWebhookSecrets(ctx context.Context, arg db.ListProjectRepositoryWebhookSecretsParams) ([]db.ListProjectRepositoryWebhookSecretsRow, error)
}

// WebhookHandler receives GitLab's Issue Hook. GitLab calls it when an
// issue is closed or reopened in a project m-Seismo created a webhook on
// (CreateWebhook, FR-56), and m-Seismo records the new state of its issue.
//
// Every delivery carries X-Gitlab-Token, the plaintext secret the webhook
// was created with; one that matches none of the project's webhook
// secrets is refused. A repository can be linked to more than one
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
	r.Post("/webhooks/gitlab", h.receive)
}

// issueHookEvent is the part of GitLab's Issue Hook payload m-Seismo reads.
type issueHookEvent struct {
	ObjectKind string `json:"object_kind"`
	Project    struct {
		PathWithNamespace string `json:"path_with_namespace"`
	} `json:"project"`
	ObjectAttributes struct {
		Action string `json:"action"`
		IID    int    `json:"iid"`
	} `json:"object_attributes"`
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
	var ev issueHookEvent
	if err := json.Unmarshal(body, &ev); err != nil {
		httpx.JSON(w, http.StatusBadRequest, map[string]string{"error": "invalid issue hook event"})
		return
	}
	// Every other event is acknowledged and ignored, so a project with more
	// than Issue Hook enabled doesn't show failed deliveries.
	if ev.ObjectKind != "issue" || ev.Project.PathWithNamespace == "" {
		httpx.NoContent(w)
		return
	}

	candidates, err := h.secrets.ListProjectRepositoryWebhookSecrets(r.Context(), db.ListProjectRepositoryWebhookSecretsParams{
		Tracker: db.TrackerGitlab, Repository: ev.Project.PathWithNamespace,
	})
	if err != nil {
		slog.ErrorContext(r.Context(), "gitlab webhook: load webhook secrets", "repository", ev.Project.PathWithNamespace, "err", err)
		httpx.JSON(w, http.StatusInternalServerError, map[string]string{"error": "internal error"})
		return
	}
	if !h.tokenMatches(candidates, r.Header.Get("X-Gitlab-Token")) {
		httpx.JSON(w, http.StatusUnauthorized, map[string]string{"error": "invalid token"})
		return
	}

	if ev.ObjectAttributes.IID <= 0 {
		httpx.NoContent(w)
		return
	}
	state, ok := map[string]string{"close": "closed", "reopen": "open"}[ev.ObjectAttributes.Action]
	if !ok {
		httpx.NoContent(w)
		return
	}

	ref := ev.Project.PathWithNamespace + "#" + strconv.Itoa(ev.ObjectAttributes.IID)
	n, err := h.states.SetIssueStateByExternalID(r.Context(), db.SetIssueStateByExternalIDParams{
		State:      state,
		Tracker:    db.TrackerGitlab,
		ExternalID: ref,
	})
	if err != nil {
		slog.ErrorContext(r.Context(), "gitlab webhook: record issue state", "issue", ref, "err", err)
		httpx.JSON(w, http.StatusInternalServerError, map[string]string{"error": "internal error"})
		return
	}
	// n is 0 for an issue m-Seismo didn't open, or already in that state.
	if n > 0 {
		slog.InfoContext(r.Context(), "gitlab issue "+ev.ObjectAttributes.Action, "issue", ref)
	}
	httpx.NoContent(w)
}

// tokenMatches reports whether token decrypts to equal any of candidates'
// secrets, in constant time per comparison.
func (h *WebhookHandler) tokenMatches(candidates []db.ListProjectRepositoryWebhookSecretsRow, token string) bool {
	if token == "" {
		return false
	}
	for _, c := range candidates {
		plain, err := h.cipher.Open(c.WebhookSecret, c.ID[:])
		if err != nil {
			continue
		}
		if subtle.ConstantTimeCompare(plain, []byte(token)) == 1 {
			return true
		}
	}
	return false
}
