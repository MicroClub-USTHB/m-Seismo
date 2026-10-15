// Package ingest accepts bug reports from the SDKs.
package ingest

import (
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"
)

var (
	modes    = []string{"internal", "QA", "production"}
	triggers = []string{"shake", "screenshot", "long_press", "manual", "crash", "non_fatal"}
)

// Report is the envelope from spec/API.md section 4. Unknown fields are
// dropped by encoding/json, which is what the spec wants (version skew).
type Report struct {
	ID             uuid.UUID `json:"id"`
	InstallationID string    `json:"installation_id"`
	Mode           string    `json:"mode"`
	Trigger        string    `json:"trigger"`
	OccurredAt     time.Time `json:"occurred_at"`
}

func (r Report) Validate() error {
	// NOTE: a zero uuid has version 0, so a missing id lands here too.
	if r.ID.Version() != 7 {
		return errors.New("id must be a UUIDv7")
	}
	if n := len(r.InstallationID); n < 1 || n > 128 {
		return errors.New("installation_id must be 1 to 128 characters")
	}
	if !slices.Contains(modes, r.Mode) {
		return errors.New("mode must be internal, QA or production")
	}
	if !slices.Contains(triggers, r.Trigger) {
		return fmt.Errorf("trigger must be one of %s", strings.Join(triggers, ", "))
	}
	if r.OccurredAt.IsZero() {
		return errors.New("occurred_at is required")
	}
	// NOTE: time.Parse only sets UTC for a Z suffix, so +00:00 gets rejected too.
	// Spec says Z, keep it strict until a client needs otherwise.
	if r.OccurredAt.Location() != time.UTC {
		return errors.New("occurred_at must be UTC with a Z suffix")
	}
	return nil
}
