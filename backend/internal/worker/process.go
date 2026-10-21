package worker

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"slices"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/MicroClub-USTHB/m-Seismo/backend/internal/db"
	"github.com/MicroClub-USTHB/m-Seismo/backend/internal/ingest"
	"github.com/MicroClub-USTHB/m-Seismo/backend/internal/issue"
)

// Issues opens issues in project repositories. Implemented by
// issue.Service.
type Issues interface {
	OpenInRepository(ctx context.Context, appID, projectRepositoryID uuid.UUID, d issue.Draft) (issue.Result, error)
}

// Queries is the part of db.Querier the processor uses.
type Queries interface {
	GetReport(ctx context.Context, id uuid.UUID) (db.Report, error)
	ListReportAttachmentNames(ctx context.Context, reportID uuid.UUID) ([]string, error)
	ListIssueTargetsForApp(ctx context.Context, appID uuid.UUID) ([]db.ListIssueTargetsForAppRow, error)
	ListReportDeliveries(ctx context.Context, reportID uuid.UUID) ([]db.ReportDelivery, error)
	CreateReportDelivery(ctx context.Context, arg db.CreateReportDeliveryParams) error
	MarkReportProcessed(ctx context.Context, id uuid.UUID) error
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
}

func NewProcessor(q Queries, issues Issues) *Processor {
	return &Processor{q: q, issues: issues}
}

// Process opens the report's issue in the default repository of every
// project its app is linked to.
//
// The queue delivers a report at least once, so Process can run several
// times for the same report: each repository that already got its issue is
// recorded in report_deliveries and skipped, and a processed report is left
// alone. When some repositories fail, the others are recorded and the next
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

	targets, err := p.q.ListIssueTargetsForApp(ctx, row.AppID)
	if err != nil {
		return fmt.Errorf("list targets of app %s: %w", row.AppID, err)
	}
	if len(targets) == 0 {
		return permanent(fmt.Errorf("report %s: %w", id, issue.ErrNoTargets))
	}

	deliveries, err := p.q.ListReportDeliveries(ctx, id)
	if err != nil {
		return fmt.Errorf("list deliveries of report %s: %w", id, err)
	}
	delivered := make(map[uuid.UUID]bool, len(deliveries))
	for _, d := range deliveries {
		delivered[d.ProjectRepositoryID] = true
	}

	names, err := p.q.ListReportAttachmentNames(ctx, id)
	if err != nil {
		return fmt.Errorf("list attachments of report %s: %w", id, err)
	}
	draft := render(&r, reportInfo{
		ID:             row.ReportID,
		InstallationID: row.InstallationID,
		Screenshot:     slices.Contains(names, ingest.ScreenshotName),
	})

	var errs []error
	for _, t := range targets {
		if delivered[t.ProjectRepositoryID] {
			continue
		}
		res, err := p.issues.OpenInRepository(ctx, row.AppID, t.ProjectRepositoryID, draft)
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
		slog.InfoContext(ctx, "issue opened", "report", id, "repository", t.Repository, "url", res.Ref.URL)
	}
	if len(errs) > 0 {
		return errors.Join(errs...)
	}

	if err := p.q.MarkReportProcessed(ctx, id); err != nil {
		return fmt.Errorf("mark report %s processed: %w", id, err)
	}
	return nil
}
