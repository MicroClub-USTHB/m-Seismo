// Package issue opens, updates and closes issues on an external tracker on
// behalf of m-Seismo (PRD section 12).
//
// This package holds the Opener interface, the types it speaks in, and the
// Service that opens issues for apps. Each tracker gets its own provider
// subpackage, which implements Opener next to the tracker's connect flow:
//
//	internal/provider/github/     - v1
//	internal/provider/gitlab/
//	internal/provider/bitbucket/
//
// Nothing outside those subpackages should import a vendor's API client
// directly. Domain code (dedup, routing, ...) depends only on Opener and
// OpenerSource, so adding or swapping a tracker never touches code outside
// its own subpackage.
package issue

import (
	"context"
	"fmt"
	"time"
)

// RateLimitError is returned by an Opener when the tracker refused the
// request because of a rate limit and waiting was not possible within the
// configured bounds. The request was not processed, so it is safe to retry
// after Wait. Implements apperr.RetryAfterer.
type RateLimitError struct {
	Tracker string
	// Wait is how long until the tracker is expected to accept requests.
	Wait time.Duration
	// Secondary is true for GitHub-style secondary (abuse) limits, false for
	// the primary hourly quota.
	Secondary bool
	Err       error
}

func (e *RateLimitError) Error() string {
	kind := "primary"
	if e.Secondary {
		kind = "secondary"
	}
	return fmt.Sprintf("%s %s rate limit: retry after %s: %v", e.Tracker, kind, e.Wait.Round(time.Second), e.Err)
}

func (e *RateLimitError) Unwrap() error { return e.Err }

// RetryAfter returns Wait.
func (e *RateLimitError) RetryAfter() time.Duration { return e.Wait }

// Ref identifies an issue that was opened on a tracker.
type Ref struct {
	// ID is the tracker-native identifier (e.g. GitHub issue number).
	ID string
	// URL is the tracker's web URL for the issue.
	URL string
}

// Issue is everything needed to open a new issue, independent of which
// tracker receives it (FR-47).
type Issue struct {
	Repository string
	Title      string
	Body       string
	Labels     []string
	Assignees  []string
	Milestone  string
}

// Opener opens and manages issues on a tracker. Implementations live one
// per tracker (github, gitlab, bitbucket, ...).
type Opener interface {
	// Open creates a new issue and returns its tracker reference.
	Open(ctx context.Context, issue Issue) (Ref, error)

	// Comment adds a comment to an existing issue, used when a new report
	// matches an already-open issue (FR-55).
	Comment(ctx context.Context, ref Ref, body string) error

	// IsOpen reports whether the issue is still open, used for two-way
	// status sync (FR-51) and regression detection (FR-57).
	IsOpen(ctx context.Context, ref Ref) (bool, error)
}
