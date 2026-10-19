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

// OpenerSource hands out an Opener bound to one provider connection
// (e.g. one GitHub App installation). Implemented by provider.Registry.
type OpenerSource interface {
	Opener(ctx context.Context, tracker db.Tracker, externalID string) (Opener, error)
}

// ErrNoTargets is returned when an app has no project with a default
// repository to open issues in.
var ErrNoTargets = errors.New("issue: app has no linked project with a default repository")

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

// Result is the outcome of opening a draft in one project repository.
type Result struct {
	ProjectID           uuid.UUID
	ProjectRepositoryID uuid.UUID
	Repository          string
	Tracker             db.Tracker
	// IssueID is the m-Seismo issue row; zero when Err is set.
	IssueID uuid.UUID
	Ref     Ref
	Err     error
}

// Service opens issues on the trackers behind each project an app reports
// into, and records them.
type Service struct {
	q       db.Querier
	openers OpenerSource
}

func NewService(q db.Querier, openers OpenerSource) *Service {
	return &Service{q: q, openers: openers}
}

// target is one repository an issue goes to.
type target struct {
	projectID           uuid.UUID
	projectRepositoryID uuid.UUID
	repository          string
	tracker             db.Tracker
	externalID          string
}

// OpenForApp opens the draft in the default repository of every project
// linked to the app. Each target is tried independently: one failing
// tracker does not stop the others, and each Result carries its own error.
// The returned error is non-nil when the targets cannot be resolved, when
// there are none (ErrNoTargets), or when every target failed.
func (s *Service) OpenForApp(ctx context.Context, appID uuid.UUID, d Draft) ([]Result, error) {
	if err := d.validate(); err != nil {
		return nil, err
	}

	rows, err := s.q.ListIssueTargetsForApp(ctx, appID)
	if err != nil {
		return nil, fmt.Errorf("issue: list targets for app %s: %w", appID, err)
	}
	if len(rows) == 0 {
		return nil, ErrNoTargets
	}

	results := make([]Result, 0, len(rows))
	var errs []error
	// A rate limit applies to the whole connection (e.g. a GitHub App
	// installation), so once one target hits it, the others on the same
	// connection are skipped with the same error instead of piling on.
	limited := map[string]*RateLimitError{}
	for _, row := range rows {
		t := target{
			projectID:           row.ProjectID,
			projectRepositoryID: row.ProjectRepositoryID,
			repository:          row.Repository,
			tracker:             row.Tracker,
			externalID:          row.ExternalID,
		}
		conn := string(t.tracker) + "/" + t.externalID

		var res Result
		if rl, ok := limited[conn]; ok {
			res = Result{
				ProjectID:           t.projectID,
				ProjectRepositoryID: t.projectRepositoryID,
				Repository:          t.repository,
				Tracker:             t.tracker,
				Err:                 fmt.Errorf("issue: skipped %s %s: connection is rate limited: %w", t.tracker, t.repository, rl),
			}
		} else {
			res = s.open(ctx, t, &appID, d)
			var rl *RateLimitError
			if errors.As(res.Err, &rl) {
				limited[conn] = rl
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

// OpenInRepository opens the draft in one project repository. appID is
// optional and recorded with the issue. Routing rules (FR-58) build on this.
func (s *Service) OpenInRepository(ctx context.Context, projectRepositoryID uuid.UUID, appID *uuid.UUID, d Draft) (Result, error) {
	if err := d.validate(); err != nil {
		return Result{}, err
	}

	row, err := s.q.GetIssueTarget(ctx, projectRepositoryID)
	if err != nil {
		return Result{}, fmt.Errorf("issue: resolve repository %s: %w", projectRepositoryID, err)
	}

	res := s.open(ctx, target{
		projectID:           row.ProjectID,
		projectRepositoryID: row.ProjectRepositoryID,
		repository:          row.Repository,
		tracker:             row.Tracker,
		externalID:          row.ExternalID,
	}, appID, d)
	return res, res.Err
}

func (s *Service) open(ctx context.Context, t target, appID *uuid.UUID, d Draft) Result {
	res := Result{
		ProjectID:           t.projectID,
		ProjectRepositoryID: t.projectRepositoryID,
		Repository:          t.repository,
		Tracker:             t.tracker,
	}

	opener, err := s.openers.Opener(ctx, t.tracker, t.externalID)
	if err != nil {
		res.Err = fmt.Errorf("issue: %s opener for %s: %w", t.tracker, t.repository, err)
		return res
	}

	ref, err := opener.Open(ctx, Issue{
		Repository: t.repository,
		Title:      d.Title,
		Body:       d.Body,
		Labels:     d.Labels,
		Assignees:  d.Assignees,
		Milestone:  d.Milestone,
	})
	if err != nil {
		res.Err = fmt.Errorf("issue: open on %s %s: %w", t.tracker, t.repository, err)
		return res
	}
	res.Ref = ref

	var fingerprint *string
	if d.Fingerprint != "" {
		fingerprint = &d.Fingerprint
	}
	row, err := s.q.CreateIssue(ctx, db.CreateIssueParams{
		ProjectRepositoryID: t.projectRepositoryID,
		AppID:               appID,
		ExternalID:          ref.ID,
		Url:                 ref.URL,
		Title:               d.Title,
		Fingerprint:         fingerprint,
	})
	if err != nil {
		// The issue exists on the tracker; losing the record only costs us
		// dedup for it, so report it without failing the result.
		slog.ErrorContext(ctx, "issue opened but not recorded",
			"tracker", t.tracker, "repository", t.repository, "ref", ref.ID, "err", err)
		return res
	}
	res.IssueID = row.ID
	return res
}

func (d Draft) validate() error {
	if d.Title == "" {
		return errors.New("issue: draft title is required")
	}
	return nil
}
