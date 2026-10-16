// Package issue opens, updates and closes issues on an external tracker on
// behalf of m-Seismo (PRD section 12).
//
// This package holds only the Opener interface and the types it speaks in.
// Each tracker gets its own subpackage that implements Opener:
//
//	internal/issue/github/     - v1
//	internal/issue/gitlab/
//	internal/issue/bitbucket/
//
// Nothing outside those subpackages should import a vendor's API client
// directly. Domain code (dedup, routing, ...) depends only on Opener, so
// adding or swapping a tracker never touches code outside its own
// subpackage.
package issue

import "context"

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
