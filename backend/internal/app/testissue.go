package app

import (
	"context"
	"errors"
	"math"
	"time"

	"github.com/google/uuid"

	"github.com/MicroClub-USTHB/m-Seismo/backend/internal/apperr"
	"github.com/MicroClub-USTHB/m-Seismo/backend/internal/db"
	"github.com/MicroClub-USTHB/m-Seismo/backend/internal/issue"
	"github.com/MicroClub-USTHB/m-Seismo/backend/internal/ratelimit"
)

// TestIssueInput customises the test issue. Both fields are optional.
type TestIssueInput struct {
	Title string `json:"title"`
	Body  string `json:"body"`
}

// TestIssueResult is the outcome for one target repository.
type TestIssueResult struct {
	ProjectID           uuid.UUID  `json:"project_id"`
	ProjectRepositoryID uuid.UUID  `json:"project_repository_id"`
	Repository          string     `json:"repository"`
	Tracker             db.Tracker `json:"tracker"`
	// ConnectionID is the app connection the issue was opened through;
	// absent when the app has no connection for the repository.
	ConnectionID *uuid.UUID `json:"connection_id,omitempty"`
	IssueID      *uuid.UUID `json:"issue_id,omitempty"`
	Ref          string     `json:"ref,omitempty"`
	URL          string     `json:"url,omitempty"`
	Error        string     `json:"error,omitempty"`
	// RetryAfterSeconds is set when the tracker rate-limited this target.
	RetryAfterSeconds int `json:"retry_after_seconds,omitempty"`
}

// TestIssueOutcome is what SendTestIssue reports.
type TestIssueOutcome struct {
	Results []TestIssueResult
	// Opened is true when at least one issue was opened.
	Opened bool
	// RetryAfter is set when nothing was opened and every target failed
	// because of a tracker rate limit: the shortest wait among them.
	RetryAfter time.Duration
}

// SendTestIssue opens a test issue through issue.Service exactly as a real
// report would: in the default repository of every project linked to the
// app, each through the app's own connection. Owners use it to check that an app is wired up end to end.
//
// It is throttled per app (limits.test_issues_per_minute) so it cannot be
// used to burn the tracker's rate limit. An app with no reachable target
// is an invalid-input error.
func (s *Service) SendTestIssue(ctx context.Context, ownerID, appID uuid.UUID, in TestIssueInput) (TestIssueOutcome, error) {
	a, err := s.get(ctx, ownerID, appID)
	if err != nil {
		return TestIssueOutcome{}, err
	}

	if s.limiter != nil && s.testIssuesPerMinute > 0 {
		n := s.testIssuesPerMinute
		res, err := s.limiter.Allow(ctx, "test_issue:"+appID.String(), ratelimit.Limit{PerMinute: float64(n), Burst: n})
		if err != nil {
			return TestIssueOutcome{}, err
		}
		if !res.Allowed {
			return TestIssueOutcome{}, apperr.RateLimited("too many test issues for this app; retry later", res.RetryAfter)
		}
	}

	draft := issue.Draft{
		Title:  in.Title,
		Body:   in.Body,
		Labels: []string{"m-seismo-test"},
	}
	if draft.Title == "" {
		draft.Title = "m-Seismo test issue from " + a.Name
	}
	if draft.Body == "" {
		draft.Body = "This issue was opened by the m-Seismo \"send test issue\" action at " +
			time.Now().UTC().Format(time.RFC3339) + ". It is safe to close."
	}

	results, err := s.issues.OpenForApp(ctx, appID, draft)
	if errors.Is(err, issue.ErrNoTargets) {
		return TestIssueOutcome{}, apperr.Invalid("app has no linked project with a default repository")
	}
	if results == nil && err != nil {
		return TestIssueOutcome{}, err
	}

	out := TestIssueOutcome{Results: make([]TestIssueResult, len(results))}
	allRateLimited := len(results) > 0
	for i, r := range results {
		res := TestIssueResult{
			ProjectID:           r.ProjectID,
			ProjectRepositoryID: r.ProjectRepositoryID,
			Repository:          r.Repository,
			Tracker:             r.Tracker,
		}
		if r.ConnectionID != uuid.Nil {
			id := r.ConnectionID
			res.ConnectionID = &id
		}
		if r.Err != nil {
			res.Error = r.Err.Error()
			var rl *issue.RateLimitError
			if errors.As(r.Err, &rl) {
				res.RetryAfterSeconds = int(math.Ceil(rl.Wait.Seconds()))
				if out.RetryAfter == 0 || rl.Wait < out.RetryAfter {
					out.RetryAfter = rl.Wait
				}
			} else {
				allRateLimited = false
			}
			out.Results[i] = res
			continue
		}
		out.Opened = true
		res.Ref = r.Ref.ID
		res.URL = r.Ref.URL
		if r.IssueID != uuid.Nil {
			id := r.IssueID
			res.IssueID = &id
		}
		out.Results[i] = res
	}
	if out.Opened || !allRateLimited {
		out.RetryAfter = 0
	}
	return out, nil
}
