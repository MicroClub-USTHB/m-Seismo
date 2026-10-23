package worker

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/MicroClub-USTHB/m-Seismo/backend/internal/coordinator"
	"github.com/MicroClub-USTHB/m-Seismo/backend/internal/db"
	"github.com/MicroClub-USTHB/m-Seismo/backend/internal/dedup"
	"github.com/MicroClub-USTHB/m-Seismo/backend/internal/ingest"
	"github.com/MicroClub-USTHB/m-Seismo/backend/internal/issue"
	seismov1 "github.com/MicroClub-USTHB/m-Seismo/backend/internal/pb/seismo/v1"
	"github.com/MicroClub-USTHB/m-Seismo/backend/internal/routing"
)

// Issues opens issues in project repositories. Implemented by
// issue.Service.
type Issues interface {
	OpenInRepository(ctx context.Context, appID, projectRepositoryID uuid.UUID, d issue.Draft) (issue.Result, error)
	CommentOn(ctx context.Context, appID uuid.UUID, iss db.Issue, body string) (bool, error)
}

// Queries is the part of db.Querier the processor uses.
type Queries interface {
	GetReport(ctx context.Context, id uuid.UUID) (db.Report, error)
	ListReportAttachmentNames(ctx context.Context, reportID uuid.UUID) ([]string, error)
	ListReportDeliveryProjects(ctx context.Context, reportID uuid.UUID) ([]uuid.UUID, error)
	CreateReportDelivery(ctx context.Context, arg db.CreateReportDeliveryParams) error
	MarkReportProcessed(ctx context.Context, id uuid.UUID) error
	GetOpenIssueByFingerprint(ctx context.Context, arg db.GetOpenIssueByFingerprintParams) (db.Issue, error)
	GetIssueOccurrences(ctx context.Context, arg db.GetIssueOccurrencesParams) (db.GetIssueOccurrencesRow, error)
}

// Router picks where a report's issues go. Implemented by
// *routing.Router.
type Router interface {
	Routes(ctx context.Context, f routing.Facts) ([]routing.Route, error)
}

// ServerLogs asks an app's log coordinator for server logs. Implemented by
// *coordinator.Client.
type ServerLogs interface {
	QueryLogs(ctx context.Context, appID uuid.UUID, from, to time.Time) (*seismov1.AggregateResult, error)
}

// permanentError marks a failure that trying again won't fix.
type permanentError struct{ err error }

func (e *permanentError) Error() string { return e.err.Error() }
func (e *permanentError) Unwrap() error { return e.err }

func permanent(err error) error { return &permanentError{err: err} }

// Processor turns one stored report into issues.
type Processor struct {
	q      Queries
	issues Issues
	router Router
	locks  Locks
	// logs is optional; window is how far around the report it looks.
	logs   ServerLogs
	window time.Duration
}

func NewProcessor(q Queries, issues Issues, router Router, locks Locks, logs ServerLogs, window time.Duration) *Processor {
	return &Processor{q: q, issues: issues, router: router, locks: locks, logs: logs, window: window}
}

// Process opens the report's issue in every project its app is linked to,
// in the repository and with the labels, assignees and milestone the
// project's routing rules pick.
//
// A report of a bug that already has an open issue in the route's
// repository (same fingerprint, see package dedup) adds a comment to that
// issue instead of opening another one.
//
// The queue delivers a report at least once, so Process can run several
// times for the same report: each project that already got its issue is
// recorded in report_deliveries and skipped, and a processed report is left
// alone. When some projects fail, the others are recorded and the next
// attempt only retries the failed ones.
//
// A returned error is permanent (trying again won't help), an
// *issue.RateLimitError (try again after its wait), or temporary.
func (p *Processor) Process(ctx context.Context, id uuid.UUID) error {
	row, err := p.q.GetReport(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		// Deleted since it was queued, e.g. with its app.
		return permanent(fmt.Errorf("report %s not found", id))
	}
	if err != nil {
		return fmt.Errorf("load report %s: %w", id, err)
	}
	if row.ProcessedAt != nil {
		return nil
	}

	var r ingest.Report
	if err := json.Unmarshal(row.Payload, &r); err != nil {
		return permanent(fmt.Errorf("decode report %s: %w", id, err))
	}

	routes, err := p.router.Routes(ctx, routing.FactsOf(row.AppID, &r))
	if err != nil {
		return err
	}
	if len(routes) == 0 {
		return permanent(fmt.Errorf("report %s: %w", id, issue.ErrNoTargets))
	}

	// One issue per project: a project that got it is skipped, even if
	// its rules changed since and would now pick another repository.
	projects, err := p.q.ListReportDeliveryProjects(ctx, id)
	if err != nil {
		return fmt.Errorf("list deliveries of report %s: %w", id, err)
	}
	var todo []routing.Route
	for _, route := range routes {
		if !slices.Contains(projects, route.ProjectID) {
			todo = append(todo, route)
		}
	}
	if len(todo) == 0 {
		return p.markProcessed(ctx, id)
	}

	names, err := p.q.ListReportAttachmentNames(ctx, id)
	if err != nil {
		return fmt.Errorf("list attachments of report %s: %w", id, err)
	}
	info := reportInfo{
		ID:             row.ReportID,
		InstallationID: row.InstallationID,
		Screenshot:     slices.Contains(names, ingest.ScreenshotName),
		ServerLogs:     p.serverLogs(ctx, row),
	}
	base := render(&r, info)
	base.Fingerprint = dedup.Fingerprint(&r)

	var errs []error
	for _, t := range todo {
		draft := base
		draft.Labels, draft.Assignees, draft.Milestone = t.Labels, t.Assignees, t.Milestone
		res, err := p.deliver(ctx, row, &r, info, t, draft)
		if err != nil {
			var rl *issue.RateLimitError
			if errors.As(err, &rl) {
				// The rest would most likely hit the same limit; they
				// are tried again with this one.
				return err
			}
			errs = append(errs, err)
			continue
		}

		var issueID *uuid.UUID
		if res.IssueID != uuid.Nil {
			issueID = &res.IssueID
		}
		err = p.q.CreateReportDelivery(ctx, db.CreateReportDeliveryParams{
			ReportID:            id,
			ProjectRepositoryID: t.ProjectRepositoryID,
			IssueID:             issueID,
			Url:                 res.Ref.URL,
		})
		if err != nil {
			// The issue exists but the next attempt can't know it and will
			// open it again. Nothing better can be done than to say so.
			slog.ErrorContext(ctx, "issue opened but delivery not recorded, a retry will open it again",
				"report", id, "repository", t.Repository, "url", res.Ref.URL, "err", err)
			errs = append(errs, fmt.Errorf("record delivery to %s: %w", t.Repository, err))
			continue
		}
		if res.grouped {
			slog.InfoContext(ctx, "report added to an open issue", "report", id, "repository", t.Repository, "url", res.Ref.URL)
		} else {
			slog.InfoContext(ctx, "issue opened", "report", id, "repository", t.Repository, "url", res.Ref.URL)
		}
	}
	if len(errs) > 0 {
		return errors.Join(errs...)
	}
	return p.markProcessed(ctx, id)
}

// delivered is where a report went in one repository.
type delivered struct {
	issue.Result
	// grouped is set when the report was added to an issue already open.
	grouped bool
}

// deliver opens the route's issue, or comments on the open issue the
// repository already has for the same bug. The repository and fingerprint
// are locked meanwhile, so two reports of a new bug arriving together open
// one issue between them.
func (p *Processor) deliver(ctx context.Context, row db.Report, r *ingest.Report, info reportInfo, t routing.Route, draft issue.Draft) (delivered, error) {
	if draft.Fingerprint == "" {
		res, err := p.issues.OpenInRepository(ctx, row.AppID, t.ProjectRepositoryID, draft)
		return delivered{Result: res}, err
	}

	unlock, err := p.locks.Lock(ctx, t.ProjectRepositoryID.String()+"/"+draft.Fingerprint)
	if err != nil {
		return delivered{}, err
	}
	defer unlock()

	existing, err := p.q.GetOpenIssueByFingerprint(ctx, db.GetOpenIssueByFingerprintParams{
		ProjectRepositoryID: t.ProjectRepositoryID,
		Fingerprint:         &draft.Fingerprint,
	})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
	case err != nil:
		return delivered{}, fmt.Errorf("find the open issue of report %s: %w", row.ID, err)
	default:
		n, err := p.q.GetIssueOccurrences(ctx, db.GetIssueOccurrencesParams{IssueID: existing.ID, ReportID: row.ID})
		if err != nil {
			return delivered{}, fmt.Errorf("count the reports of issue %s: %w", existing.ID, err)
		}
		body := renderOccurrence(r, info, occurrences{Reports: int(n.Occurrences), Installations: int(n.Installations), Versions: n.Versions})
		posted, err := p.issues.CommentOn(ctx, row.AppID, existing, body)
		if err != nil {
			return delivered{}, err
		}
		if posted {
			res := issue.Result{IssueID: existing.ID, Ref: issue.Ref{ID: existing.ExternalID, URL: existing.Url}}
			return delivered{Result: res, grouped: true}, nil
		}
		// Closed on the tracker since: the bug is back, it gets a new
		// issue.
	}

	res, err := p.issues.OpenInRepository(ctx, row.AppID, t.ProjectRepositoryID, draft)
	return delivered{Result: res}, err
}

func (p *Processor) markProcessed(ctx context.Context, id uuid.UUID) error {
	if err := p.q.MarkReportProcessed(ctx, id); err != nil {
		return fmt.Errorf("mark report %s processed: %w", id, err)
	}
	return nil
}

// serverLogs asks the app's coordinator for the server logs around the
// report. The issue is opened without them when there are none.
func (p *Processor) serverLogs(ctx context.Context, row db.Report) *seismov1.AggregateResult {
	if p.logs == nil {
		return nil
	}
	res, err := p.logs.QueryLogs(ctx, row.AppID, row.OccurredAt.Add(-p.window), row.OccurredAt.Add(p.window))
	switch {
	case errors.Is(err, coordinator.ErrNotAttached):
		slog.DebugContext(ctx, "no coordinator attached, opening the issue without server logs", "report", row.ID)
	case err != nil:
		slog.WarnContext(ctx, "server logs unavailable, opening the issue without them", "report", row.ID, "err", err)
	}
	return res
}
