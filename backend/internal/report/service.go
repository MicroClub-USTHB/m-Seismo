// Package report lets an app's owner delete reports, to honour a user's
// deletion request (PRD §8). Attachments cascade; report deliveries and the
// issues they point to are left as they are.
package report

import (
	"archive/zip"
	"context"
	"errors"
	"io"
	"mime"
	"path/filepath"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/MicroClub-USTHB/m-Seismo/backend/internal/apperr"
	"github.com/MicroClub-USTHB/m-Seismo/backend/internal/db"
	"github.com/MicroClub-USTHB/m-Seismo/backend/internal/queue"
)

type Service struct {
	q   db.Querier
	pub queue.Publisher
}

func NewService(q db.Querier, pub queue.Publisher) *Service {
	return &Service{q: q, pub: pub}
}

type ListReportsParams struct {
	Status string
	Cursor *time.Time
	Limit  int32
}

type ListReportsResult struct {
	Reports    []ReportSummary `json:"reports"`
	NextCursor *time.Time      `json:"next_cursor,omitempty"`
}

type ReportSummary struct {
	ReportID       uuid.UUID `json:"report_id"`
	InstallationID uuid.UUID `json:"installation_id"`
	SessionID      string    `json:"session_id"`
	Trigger        string    `json:"trigger"`
	OccurredAt     time.Time `json:"occurred_at"`
	Status         string    `json:"status"`
	IssueURLs      []string  `json:"issue_urls"`
}

type ReportDetail struct {
	ID             uuid.UUID        `json:"id"`
	AppID          uuid.UUID        `json:"app_id"`
	ReportID       uuid.UUID        `json:"report_id"`
	InstallationID uuid.UUID        `json:"installation_id"`
	SessionID      string           `json:"session_id"`
	Trigger        string           `json:"trigger"`
	OccurredAt     time.Time        `json:"occurred_at"`
	ReceivedAt     time.Time        `json:"received_at"`
	ProcessedAt    *time.Time       `json:"processed_at,omitempty"`
	Status         string           `json:"status"`
	Payload        []byte           `json:"payload"`
	Deliveries     []ReportDelivery `json:"deliveries"`
}

type ReportDelivery struct {
	ProjectRepositoryID uuid.UUID `json:"project_repository_id"`
	URL                 string    `json:"url"`
	CreatedAt           time.Time `json:"created_at"`
}

func (s *Service) ListReports(ctx context.Context, ownerID, appID uuid.UUID, p ListReportsParams) (ListReportsResult, error) {
	if _, err := s.getApp(ctx, ownerID, appID); err != nil {
		return ListReportsResult{}, err
	}

	var isProcessed *bool
	switch p.Status {
	case "delivered":
		t := true
		isProcessed = &t
	case "failed":
		f := false
		isProcessed = &f
	default:
		isProcessed = nil
	}

	var cursor pgtype.Timestamptz
	if p.Cursor != nil {
		cursor = pgtype.Timestamptz{Time: *p.Cursor, Valid: true}
	}

	limit := p.Limit
	if limit <= 0 {
		limit = 50
	}

	rows, err := s.q.ListReports(ctx, db.ListReportsParams{
		AppID:       appID,
		Cursor:      cursor,
		IsProcessed: isProcessed,
		Limit:       limit + 1,
	})
	if err != nil {
		return ListReportsResult{}, err
	}

	var nextCursor *time.Time
	if int32(len(rows)) > limit {
		nextCursor = &rows[limit-1].OccurredAt
		rows = rows[:limit]
	}

	reports := make([]ReportSummary, len(rows))
	for i, r := range rows {
		status := "failed"
		if r.ProcessedAt != nil {
			status = "delivered"
		}

		deliveries, err := s.q.ListReportDeliveries(ctx, r.ID)
		if err != nil {
			return ListReportsResult{}, err
		}

		issueURLs := make([]string, 0, len(deliveries))
		for _, d := range deliveries {
			if d.Url != "" {
				issueURLs = append(issueURLs, d.Url)
			}
		}

		reports[i] = ReportSummary{
			ReportID:       r.ReportID,
			InstallationID: r.InstallationID,
			SessionID:      r.SessionID,
			Trigger:        r.Trigger,
			OccurredAt:     r.OccurredAt,
			Status:         status,
			IssueURLs:      issueURLs,
		}
	}

	return ListReportsResult{
		Reports:    reports,
		NextCursor: nextCursor,
	}, nil
}

func (s *Service) GetReport(ctx context.Context, ownerID, appID, reportID uuid.UUID) (ReportDetail, error) {
	if _, err := s.getApp(ctx, ownerID, appID); err != nil {
		return ReportDetail{}, err
	}

	rep, err := s.q.GetReportByReportID(ctx, db.GetReportByReportIDParams{
		AppID:    appID,
		ReportID: reportID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return ReportDetail{}, apperr.NotFound("report not found")
		}
		return ReportDetail{}, err
	}

	deliveries, err := s.q.ListReportDeliveries(ctx, rep.ID)
	if err != nil {
		return ReportDetail{}, err
	}

	delList := make([]ReportDelivery, len(deliveries))
	for i, d := range deliveries {
		delList[i] = ReportDelivery{
			ProjectRepositoryID: d.ProjectRepositoryID,
			URL:                 d.Url,
			CreatedAt:           d.CreatedAt,
		}
	}

	status := "failed"
	if rep.ProcessedAt != nil {
		status = "delivered"
	}

	return ReportDetail{
		ID:             rep.ID,
		AppID:          rep.AppID,
		ReportID:       rep.ReportID,
		InstallationID: rep.InstallationID,
		SessionID:      rep.SessionID,
		Trigger:        rep.Trigger,
		OccurredAt:     rep.OccurredAt,
		ReceivedAt:     rep.ReceivedAt,
		ProcessedAt:    rep.ProcessedAt,
		Status:         status,
		Payload:        rep.Payload,
		Deliveries:     delList,
	}, nil
}

func (s *Service) RetryReport(ctx context.Context, ownerID, appID, reportID uuid.UUID) error {
	if _, err := s.getApp(ctx, ownerID, appID); err != nil {
		return err
	}

	rep, err := s.q.GetReportByReportID(ctx, db.GetReportByReportIDParams{
		AppID:    appID,
		ReportID: reportID,
	})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return apperr.NotFound("report not found")
		}
		return err
	}

	if s.pub != nil {
		if err := s.pub.ReportAccepted(ctx, queue.ReportAccepted{
			ID:    rep.ID,
			AppID: rep.AppID,
		}); err != nil {
			return err
		}
	}

	return nil
}

func (s *Service) Bundle(ctx context.Context, ownerID, appID, reportID uuid.UUID, w io.Writer) error {
	if _, err := s.getApp(ctx, ownerID, appID); err != nil {
		return err
	}

	rep, err := s.q.GetReportByReportID(ctx, db.GetReportByReportIDParams{AppID: appID, ReportID: reportID})
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return apperr.NotFound("report not found")
		}
		return err
	}

	attachments, err := s.q.ListReportAttachmentsWithData(ctx, rep.ID)
	if err != nil {
		return err
	}

	zw := zip.NewWriter(w)

	// 1. Write report.json
	rw, err := zw.Create("report.json")
	if err != nil {
		return err
	}
	if _, err := rw.Write(rep.Payload); err != nil {
		return err
	}

	// 2. Write attachments with extension resolution and path sanitization
	for _, a := range attachments {
		name := filepath.Base(a.Name)
		if filepath.Ext(name) == "" {
			switch a.ContentType {
			case "image/png":
				name += ".png"
			case "image/jpeg":
				name += ".jpg"
			case "text/plain":
				name += ".txt"
			case "application/json":
				name += ".json"
			default:
				if exts, _ := mime.ExtensionsByType(a.ContentType); len(exts) > 0 {
					name += exts[0]
				}
			}
		}

		aw, err := zw.Create(name)
		if err != nil {
			return err
		}
		if _, err := aw.Write(a.Data); err != nil {
			return err
		}
	}

	return zw.Close()
}

// DeleteReport deletes one report and its attachments.
func (s *Service) DeleteReport(ctx context.Context, ownerID, appID, reportID uuid.UUID) error {
	if _, err := s.getApp(ctx, ownerID, appID); err != nil {
		return err
	}
	n, err := s.q.DeleteReportByReportID(ctx, db.DeleteReportByReportIDParams{AppID: appID, ReportID: reportID})
	if err != nil {
		return err
	}
	if n == 0 {
		return apperr.NotFound("report not found")
	}
	return nil
}

// DeleteByInstallation deletes every report and attachment of one
// installation of the app. It returns how many reports were deleted, zero
// when the installation sent none.
func (s *Service) DeleteByInstallation(ctx context.Context, ownerID, appID, installationID uuid.UUID) (int64, error) {
	if _, err := s.getApp(ctx, ownerID, appID); err != nil {
		return 0, err
	}
	n, err := s.q.DeleteReportsByInstallation(ctx, db.DeleteReportsByInstallationParams{AppID: appID, InstallationID: installationID})
	if err != nil {
		return 0, err
	}
	return n, nil
}

func (s *Service) getApp(ctx context.Context, ownerID, id uuid.UUID) (db.App, error) {
	a, err := s.q.GetApp(ctx, db.GetAppParams{ID: id, OwnerID: ownerID})
	if err != nil {
		if kind, _ := apperr.Classify(err); kind == apperr.KindNotFound {
			return db.App{}, apperr.NotFound("app not found")
		}
		return db.App{}, err
	}
	return a, nil
}
