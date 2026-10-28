// Package crash links a report to the crash it came from on an external crash
// reporter.
//
// This package holds only the Source interface and the types it speaks in.
// Each crash reporter gets its own subpackage that implements Source:
//
//	internal/crash/none/        - no crash reporter configured
//	internal/crash/crashlytics/ - v1, Crashlytics REST API (v1alpha)
//
// Nothing outside those subpackages should import a vendor's API client
// directly. The rest of the pipeline depends only on Source, so a report is
// still processed and published when no crash is found or the source is
// down; only the crash details are missing from the issue.
package crash

import (
	"context"
	"errors"
	"time"
)

// ErrNotFound is returned by Find when no crash matches the query.
var ErrNotFound = errors.New("crash: no matching crash")

// Query describes the crash to look for: same installation, close in time.
type Query struct {
	// ProjectID is the m-Seismo project the report belongs to. Sources that
	// connect per project (e.g. Crashlytics) use it to pick the credentials.
	ProjectID string
	// App is the source-specific app identifier (e.g. the Firebase app ID).
	App string
	// InstallationID identifies the app installation that sent the report.
	InstallationID string
	// At is when the crash happened according to the report.
	At time.Time
	// Window is how far before and after At a crash may be and still match.
	Window time.Duration
}

// Breadcrumb is an event recorded during the session that crashed.
type Breadcrumb struct {
	At     time.Time
	Title  string
	Params map[string]string
}

// Crash is a crash found on the crash reporter, with what the issue needs
// to show.
type Crash struct {
	// ID is the source-native identifier of the crash event.
	ID string
	// URL is the crash reporter's web URL for the crash, if it has one.
	URL string
	// Title and Subtitle summarise the crash (e.g. file or method, and the
	// exception message).
	Title    string
	Subtitle string
	// At is when the crash happened, as recorded on the device.
	At time.Time
	// Trace is the crash trace, already formatted for display.
	Trace       string
	Breadcrumbs []Breadcrumb
	CustomKeys  map[string]string
}

// Source finds crashes on a crash reporter. Implementations live one per
// reporter (none, crashlytics, ...).
type Source interface {
	// Check verifies at start-up that the source can be read, so the
	// project owner can be warned if it cannot.
	Check(ctx context.Context) error

	// Find returns the crash that best matches q, or ErrNotFound.
	Find(ctx context.Context, q Query) (Crash, error)
}
