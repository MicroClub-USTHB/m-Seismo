package worker

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/MicroClub-USTHB/m-Seismo/backend/internal/coordinator"
	"github.com/MicroClub-USTHB/m-Seismo/backend/internal/crashprovider"
	"github.com/MicroClub-USTHB/m-Seismo/backend/internal/db"
	"github.com/MicroClub-USTHB/m-Seismo/backend/internal/ingest"
	"github.com/MicroClub-USTHB/m-Seismo/backend/internal/issue"
	seismov1 "github.com/MicroClub-USTHB/m-Seismo/backend/internal/pb/seismo/v1"
	"github.com/MicroClub-USTHB/m-Seismo/backend/internal/routing"
	"github.com/MicroClub-USTHB/m-Seismo/backend/internal/triage"
)

// Issues opens issues in project repositories. Implemented by
// issue.Service.
type Issues interface {
	OpenInRepository(ctx context.Context, appID, projectRepositoryID uuid.UUID, d issue.Draft) (issue.Result, error)
}

// Queries is the part of db.Querier the processor uses.
type Queries interface {
	GetReport(ctx context.Context, id uuid.UUID) (db.Report, error)
	ListReportAttachments(ctx context.Context, reportID uuid.UUID) ([]db.ListReportAttachmentsRow, error)
	ListReportDeliveryProjects(ctx context.Context, reportID uuid.UUID) ([]uuid.UUID, error)
	CreateReportDelivery(ctx context.Context, arg db.CreateReportDeliveryParams) error
	MarkReportProcessed(ctx context.Context, id uuid.UUID) error
}

// Triager produces suggestions for a report targeting a repository.
// Implemented by *triage.Service.
type Triager interface {
	TriageReport(ctx context.Context, r *ingest.Report, repository string) triage.Suggestion
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

// ServiceHealth asks an app's log coordinator for a snapshot of its
// services' health (error rate, restarts, configured CPU and memory,
// deployed version), for the "Service health" table. Implemented by
// *coordinator.Client.
type ServiceHealth interface {
	QueryMetrics(ctx context.Context, appID uuid.UUID, from, to time.Time) (*seismov1.AggregateMetrics, error)
	QueryStatus(ctx context.Context, appID uuid.UUID) (*seismov1.AggregateStatus, error)
}

// permanentError marks a failure that trying again won't fix.
type permanentError struct{ err error }

func (e *permanentError) Error() string { return e.err.Error() }
func (e *permanentError) Unwrap() error { return e.err }

func permanent(err error) error { return &permanentError{err: err} }

// CrashResolver resolves a persisted report within its app's configured project.
type CrashResolver interface {
	ForReport(context.Context, uuid.UUID, *ingest.Report) (*crashprovider.Event, error)
}

// AttachmentLinker builds the private, expiring link an issue uses to show
// an attachment (FR-48). Implemented by *attachment.Service.
type AttachmentLinker interface {
	SignedURL(id uuid.UUID) string
}

// Processor turns one stored report into issues.
type Processor struct {
	q      Queries
	issues Issues
	router Router
	// logs and health are optional; window is how far around the report
	// they look.
	logs    ServerLogs
	health  ServiceHealth
	window  time.Duration
	crashes CrashResolver
	links   AttachmentLinker
	triager Triager
}

// Option configures optional Processor dependencies.
type Option func(*Processor)

// WithTriager sets the triager used to enrich reports.
func WithTriager(t Triager) Option {
	return func(p *Processor) {
		p.triager = t
	}
}

func NewProcessor(q Queries, issues Issues, router Router, logs ServerLogs, window time.Duration, crashes CrashResolver, links AttachmentLinker, health ServiceHealth, opts ...Option) *Processor {
	p := &Processor{q: q, issues: issues, router: router, logs: logs, window: window, crashes: crashes, links: links, health: health}
	for _, opt := range opts {
		opt(p)
	}
	return p
}

// Process opens the report's issue in every project its app is linked to,
// in the repository and with the labels, assignees and milestone the
// project's routing rules pick.
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

	attachments, err := p.q.ListReportAttachments(ctx, id)
	if err != nil {
		return fmt.Errorf("list attachments of report %s: %w", id, err)
	}
	var crashEvent *crashprovider.Event
	if r.Sentry != nil && p.crashes != nil {
		var resolveErr error
		crashEvent, resolveErr = p.crashes.ForReport(ctx, row.AppID, &r)
		if resolveErr != nil {
			// Sentry delays or outages must not discard or indefinitely delay the issue.
			// The resolver returns a validated link when its configuration is available.
			slog.WarnContext(ctx, "Sentry details unavailable; retaining crash reference", "report", id)
		}
	}
	repo := ""
	if len(todo) > 0 {
		repo = todo[0].Repository
	}
	var suggestion triage.Suggestion
	if p.triager != nil {
		suggestion = p.triager.TriageReport(ctx, &r, repo)
	} else {
		suggestion = triage.Analyze(&r)
	}
	info := reportInfo{
		CrashEvent:     crashEvent,
		ID:             row.ReportID,
		InstallationID: row.InstallationID,
		ScreenshotURL:  p.screenshotURL(attachments),
		ServerLogs:     p.serverLogs(ctx, row),
		Health:         p.serviceHealth(ctx, row),
		Triage:         suggestion,
	}
	base := render(&r, info)

	var errs []error
	for _, t := range todo {
		draft := base
		if t.TitleTemplate != nil && t.BodyTemplate != nil {
			if d, err := renderWithTemplate(*t.TitleTemplate, *t.BodyTemplate, &r, info); err == nil {
				draft = d
			} else {
				slog.WarnContext(ctx, "issue template failed, opening with the default layout", "report", id, "rule", t.RuleID, "err", err)
			}
		}
		draft.Labels, draft.Assignees, draft.Milestone = t.Labels, t.Assignees, t.Milestone
		if len(draft.Labels) == 0 && len(suggestion.Labels) > 0 {
			draft.Labels = slices.Clone(suggestion.Labels)
		}
		if len(draft.Assignees) == 0 && suggestion.Owner != "" {
			rawOwner := strings.TrimPrefix(suggestion.Owner, "@")
			if rawOwner != "" {
				draft.Assignees = []string{rawOwner}
			}
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
	return p.markProcessed(ctx, id)
}

func (p *Processor) markProcessed(ctx context.Context, id uuid.UUID) error {
	if err := p.q.MarkReportProcessed(ctx, id); err != nil {
		return fmt.Errorf("mark report %s processed: %w", id, err)
	}
	return nil
}

// screenshotURL returns the private, expiring link to the report's
// screenshot, or "" when it sent none.
func (p *Processor) screenshotURL(attachments []db.ListReportAttachmentsRow) string {
	for _, a := range attachments {
		if a.Name == ingest.ScreenshotName {
			return p.links.SignedURL(a.ID)
		}
	}
	return ""
}

// serviceHealth asks the app's coordinator for a snapshot of its services'
// health around the report. The issue is opened without it when there is
// none, or the coordinator can't fully answer.
func (p *Processor) serviceHealth(ctx context.Context, row db.Report) []healthRow {
	if p.health == nil {
		return nil
	}
	metrics, err := p.health.QueryMetrics(ctx, row.AppID, row.OccurredAt.Add(-p.window), row.OccurredAt.Add(p.window))
	switch {
	case errors.Is(err, coordinator.ErrNotAttached):
		return nil
	case err != nil:
		slog.WarnContext(ctx, "service metrics unavailable, opening the issue without them", "report", row.ID, "err", err)
	}
	status, err := p.health.QueryStatus(ctx, row.AppID)
	if err != nil && !errors.Is(err, coordinator.ErrNotAttached) {
		slog.WarnContext(ctx, "service status unavailable, opening the issue without it", "report", row.ID, "err", err)
	}
	return mergeHealth(metrics, status)
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
