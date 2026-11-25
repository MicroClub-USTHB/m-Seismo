package provider

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log/slog"

	"github.com/google/uuid"

	"github.com/MicroClub-USTHB/m-Seismo/backend/internal/db"
	"github.com/MicroClub-USTHB/m-Seismo/backend/internal/issue"
	"github.com/MicroClub-USTHB/m-Seismo/backend/internal/secrets"
)

// RandomSecret returns a hex-encoded random secret for a new webhook
// (GitLab's token, Bitbucket's signing secret).
func RandomSecret() (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("provider: generate webhook secret: %w", err)
	}
	return hex.EncodeToString(buf), nil
}

// WebhookService makes sure a project repository has the webhook its
// tracker needs to tell m-Seismo when one of its issues changes state
// (FR-56), creating it the first time an app's connection opens an issue
// there. It implements issue.Webhooks.
//
// Best-effort: Ensure runs after the issue is already open, and a failure
// here is only logged, never returned to the caller. GitHub needs no
// webhook of its own (its App already has one for every installed
// repository), so trackers that don't implement WebhookManager are a
// silent no-op.
type WebhookService struct {
	q      db.Querier
	reg    *Registry
	cipher *secrets.Cipher
}

func NewWebhookService(q db.Querier, reg *Registry, cipher *secrets.Cipher) *WebhookService {
	return &WebhookService{q: q, reg: reg, cipher: cipher}
}

func (s *WebhookService) Ensure(ctx context.Context, c issue.Connection, repository string, projectRepositoryID uuid.UUID, hasWebhook bool) {
	if hasWebhook {
		return
	}
	p, err := s.reg.Get(c.Tracker)
	if err != nil {
		return
	}
	mgr, ok := p.(WebhookManager)
	if !ok {
		return
	}

	hook, err := mgr.CreateWebhook(ctx, c, repository)
	if err != nil {
		slog.WarnContext(ctx, "provider: create webhook failed, issue status from this tracker won't update",
			"tracker", c.Tracker, "repository", repository, "err", err)
		return
	}
	sealed, err := s.cipher.Seal([]byte(hook.Secret), projectRepositoryID[:])
	if err != nil {
		slog.WarnContext(ctx, "provider: seal webhook secret", "repository", repository, "err", err)
		return
	}
	if err := s.q.SetProjectRepositoryWebhook(ctx, db.SetProjectRepositoryWebhookParams{
		ID:                projectRepositoryID,
		WebhookExternalID: &hook.ExternalID,
		WebhookSecret:     sealed,
	}); err != nil {
		slog.WarnContext(ctx, "provider: record webhook", "repository", repository, "err", err)
	}
}

var _ issue.Webhooks = (*WebhookService)(nil)
