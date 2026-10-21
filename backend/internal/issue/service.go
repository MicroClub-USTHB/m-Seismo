package issue

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/google/uuid"
	"go.uber.org/fx"

	"github.com/MicroClub-USTHB/m-Seismo/backend/internal/db"
)

var Module = fx.Module("issue",
	fx.Provide(NewService),
)

// OpenerSource hands out Openers for connections and says which repositories
// a connection can reach. Implemented by provider.Registry.
type OpenerSource interface {
	// Opener returns an Opener acting through the connection (e.g. one
	// GitHub App installation).
	Opener(ctx context.Context, c Connection) (Opener, error)
	// Covers reports whether the connection can, in principle, reach
	// repository. It must not call the tracker: it is used to pick one of an
	// app's connections for every target.
	Covers(c Connection, repository string) bool
}

// ErrNoTargets is returned when an app has no project with a default
// repository to open issues in.
var ErrNoTargets = errors.New("issue: app has no linked project with a default repository")

// ErrNoConnection marks a target the app cannot reach: the app has no
// connection on the repository's tracker that covers the repository.
var ErrNoConnection = errors.New("issue: app has no connection for the repository")

// Draft is a tracker-independent issue, before the target repository is
// known.
type Draft struct {
	Title     string
	Body      string
	Labels    []string
	Assignees []string
	Milestone string
	// Fingerprint groups reports of the same bug (FR-54). Optional.
	Fingerprint string
}

// Target is one repository an app's issue goes to, with the app connection
// it will be opened through.
type Target struct {
	ProjectID           uuid.UUID
	ProjectRepositoryID uuid.UUID
	Repository          string
	Tracker             db.Tracker
	// Connection is the app's connection for the repository; nil when Err
	// is set.
	Connection *Connection
	// Err wraps ErrNoConnection when the app cannot reach the repository.
	Err error
}

// Result is the outcome of opening a draft in one project repository.
type Result struct {
	ProjectID           uuid.UUID
	ProjectRepositoryID uuid.UUID
	Repository          string
	Tracker             db.Tracker
	// ConnectionID is the app connection used; zero when no connection
	// covered the repository.
	ConnectionID uuid.UUID
	// IssueID is the m-Seismo issue row; zero when Err is set.
	IssueID uuid.UUID
	Ref     Ref
	Err     error
}

// Service opens issues on the trackers behind each project an app reports
// into, through the app's own connections, and records them.
type Service struct {
	q       db.Querier
	openers OpenerSource
}

func NewService(q db.Querier, openers OpenerSource) *Service {
	return &Service{q: q, openers: openers}
}

// Targets resolves where an issue from the app would go: the default
// repository of every linked project, each paired with the first of the
// app's active connections on the same tracker that covers it. A target
// without one carries an error wrapping ErrNoConnection.
func (s *Service) Targets(ctx context.Context, appID uuid.UUID) ([]Target, error) {
	rows, err := s.q.ListIssueTargetsForApp(ctx, appID)
	if err != nil {
		return nil, fmt.Errorf("issue: list targets for app %s: %w", appID, err)
	}
	if len(rows) == 0 {
		return []Target{}, nil
	}
	conns, err := s.q.ListActiveConnectionsForApp(ctx, appID)
	if err != nil {
		return nil, fmt.Errorf("issue: list connections for app %s: %w", appID, err)
	}

	out := make([]Target, len(rows))
	for i, r := range rows {
		out[i] = s.resolve(Target{
			ProjectID:           r.ProjectID,
			ProjectRepositoryID: r.ProjectRepositoryID,
			Repository:          r.Repository,
			Tracker:             r.Tracker,
		}, conns)
	}
	return out, nil
}

func (s *Service) resolve(t Target, conns []db.ProviderConnection) Target {
	for _, row := range conns {
		if row.Tracker != t.Tracker {
			continue
		}
		c := ConnectionFromRow(row)
		if s.openers.Covers(c, t.Repository) {
			t.Connection = &c
			return t
		}
	}
	t.Err = fmt.Errorf("%w: no %s connection on this app covers %s", ErrNoConnection, t.Tracker, t.Repository)
	return t
}

// OpenForApp opens the draft in the default repository of every project
// linked to the app, each through the app's own connection. Each target is
// tried independently: one failing tracker, or a repository the app has no
// connection for, does not stop the others, and each Result carries its own
// error. The returned error is non-nil when the targets cannot be resolved,
// when there are none (ErrNoTargets), or when every target failed.
func (s *Service) OpenForApp(ctx context.Context, appID uuid.UUID, d Draft) ([]Result, error) {
	if err := d.validate(); err != nil {
		return nil, err
	}

	targets, err := s.Targets(ctx, appID)
	if err != nil {
		return nil, err
	}
	if len(targets) == 0 {
		return nil, ErrNoTargets
	}

	results := make([]Result, 0, len(targets))
	var errs []error
	// A rate limit applies to the whole tracker-side connection (e.g. a
	// GitHub App installation), so once one target hits it, the others on
	// the same connection are skipped with the same error instead of piling
	// on.
	limited := map[string]*RateLimitError{}
	for _, t := range targets {
		var res Result
		switch {
		case t.Err != nil:
			res = t.result()
			res.Err = t.Err
		case limited[t.connKey()] != nil:
			res = t.result()
			res.Err = fmt.Errorf("issue: skipped %s %s: connection is rate limited: %w", t.Tracker, t.Repository, limited[t.connKey()])
		default:
			res = s.open(ctx, t, appID, d)
			var rl *RateLimitError
			if errors.As(res.Err, &rl) {
				limited[t.connKey()] = rl
			}
		}
		if res.Err != nil {
			errs = append(errs, res.Err)
		}
		results = append(results, res)
	}

	if len(errs) == len(results) {
		return results, fmt.Errorf("issue: every target failed: %w", errors.Join(errs...))
	}
	return results, nil
}

// OpenInRepository opens the draft in one project repository through the
// app's connection for it. The app must be linked to the repository's
// project. Routing rules (FR-58) build on this.
func (s *Service) OpenInRepository(ctx context.Context, appID, projectRepositoryID uuid.UUID, d Draft) (Result, error) {
	if err := d.validate(); err != nil {
		return Result{}, err
	}

	row, err := s.q.GetIssueTarget(ctx, db.GetIssueTargetParams{ID: projectRepositoryID, AppID: appID})
	if err != nil {
		return Result{}, fmt.Errorf("issue: resolve repository %s for app %s: %w", projectRepositoryID, appID, err)
	}
	conns, err := s.q.ListActiveConnectionsForApp(ctx, appID)
	if err != nil {
		return Result{}, fmt.Errorf("issue: list connections for app %s: %w", appID, err)
	}

	t := s.resolve(Target{
		ProjectID:           row.ProjectID,
		ProjectRepositoryID: row.ProjectRepositoryID,
		Repository:          row.Repository,
		Tracker:             row.Tracker,
	}, conns)
	if t.Err != nil {
		res := t.result()
		res.Err = t.Err
		return res, t.Err
	}
	res := s.open(ctx, t, appID, d)
	return res, res.Err
}

func (s *Service) open(ctx context.Context, t Target, appID uuid.UUID, d Draft) Result {
	res := t.result()

	opener, err := s.openers.Opener(ctx, *t.Connection)
	if err != nil {
		res.Err = fmt.Errorf("issue: %s opener for %s: %w", t.Tracker, t.Repository, err)
		return res
	}

	ref, err := opener.Open(ctx, Issue{
		Repository: t.Repository,
		Title:      d.Title,
		Body:       d.Body,
		Labels:     d.Labels,
		Assignees:  d.Assignees,
		Milestone:  d.Milestone,
	})
	if err != nil {
		res.Err = fmt.Errorf("issue: open on %s %s: %w", t.Tracker, t.Repository, err)
		return res
	}
	res.Ref = ref

	var fingerprint *string
	if d.Fingerprint != "" {
		fingerprint = &d.Fingerprint
	}
	connectionID := t.Connection.ID
	row, err := s.q.CreateIssue(ctx, db.CreateIssueParams{
		ProjectRepositoryID:  t.ProjectRepositoryID,
		AppID:                &appID,
		ProviderConnectionID: &connectionID,
		ExternalID:           ref.ID,
		Url:                  ref.URL,
		Title:                d.Title,
		Fingerprint:          fingerprint,
	})
	if err != nil {
		// The issue exists on the tracker; losing the record only costs us
		// dedup for it, so report it without failing the result.
		slog.ErrorContext(ctx, "issue opened but not recorded",
			"tracker", t.Tracker, "repository", t.Repository, "ref", ref.ID, "err", err)
		return res
	}
	res.IssueID = row.ID
	return res
}

func (t Target) result() Result {
	res := Result{
		ProjectID:           t.ProjectID,
		ProjectRepositoryID: t.ProjectRepositoryID,
		Repository:          t.Repository,
		Tracker:             t.Tracker,
	}
	if t.Connection != nil {
		res.ConnectionID = t.Connection.ID
	}
	return res
}

// connKey identifies the tracker-side connection, which is what rate limits
// apply to: apps sharing a GitHub installation share its limits.
func (t Target) connKey() string {
	return string(t.Tracker) + "/" + t.Connection.ExternalID
}

func (d Draft) validate() error {
	if d.Title == "" {
		return errors.New("issue: draft title is required")
	}
	return nil
}
