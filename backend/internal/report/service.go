// Package report lets an app's owner delete reports, to honour a user's
// deletion request (PRD §8). Attachments cascade; report deliveries and the
// issues they point to are left as they are.
package report

import (
	"archive/zip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/MicroClub-USTHB/m-Seismo/backend/internal/apperr"
	"github.com/MicroClub-USTHB/m-Seismo/backend/internal/db"
	"github.com/MicroClub-USTHB/m-Seismo/backend/internal/queue"
)

const (
	defaultListLimit = 50
	maxListLimit     = 100
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
	Cursor string
	Limit  int32
}

type ListReportsResult struct {
	Reports    []ReportSummary `json:"reports"`
	NextCursor *string         `json:"next_cursor,omitempty"`
}

type ReportSummary struct {
	ReportID       uuid.UUID `json:"report_id"`
	InstallationID uuid.UUID `json:"installation_id"`
	SessionID      string    `json:"session_id"`
	Trigger        string    `json:"trigger"`
	OccurredAt     time.Time `json:"occurred_at"`
	Status         string    `json:"status"`
	DeliveryError  *string   `json:"delivery_error,omitempty"`
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
	DeliveryError  *string          `json:"delivery_error,omitempty"`
	Payload        json.RawMessage  `json:"payload"`
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

	var statusFilter *string
	switch p.Status {
	case "":
	case "delivered", "failed", "queued":
		v := p.Status
		statusFilter = &v
	default:
		return ListReportsResult{}, apperr.Invalid("invalid status: must be delivered, failed, or queued")
	}

	cursorTime, cursorID, err := parseCursor(p.Cursor)
	if err != nil {
		return ListReportsResult{}, err
	}

	limit := p.Limit
	if limit <= 0 {
		limit = defaultListLimit
	}
	if limit > maxListLimit {
		limit = maxListLimit
	}

	rows, err := s.q.ListReports(ctx, db.ListReportsParams{
		AppID:      appID,
		CursorTime: cursorTime,
		CursorID:   cursorID,
		Status:     statusFilter,
		Limit:      limit + 1,
	})
	if err != nil {
		return ListReportsResult{}, err
	}

	var nextCursor *string
	if len(rows) > int(limit) {
		rows = rows[:limit]
		last := rows[len(rows)-1]
		c := formatCursor(last.OccurredAt, last.ID)
		nextCursor = &c
	}

	urlsByReport, err := s.batchIssueURLs(ctx, rows)
	if err != nil {
		return ListReportsResult{}, err
	}

	reports := make([]ReportSummary, len(rows))
	for i, r := range rows {
		urls := urlsByReport[r.ID]
		if urls == nil {
			urls = []string{}
		}
		reports[i] = ReportSummary{
			ReportID:       r.ReportID,
			InstallationID: r.InstallationID,
			SessionID:      r.SessionID,
			Trigger:        r.Trigger,
			OccurredAt:     r.OccurredAt,
			Status:         reportStatus(r.ProcessedAt, r.DeliveryError),
			DeliveryError:  r.DeliveryError,
			IssueURLs:      urls,
		}
	}

	return ListReportsResult{
		Reports:    reports,
		NextCursor: nextCursor,
	}, nil
}

func (s *Service) batchIssueURLs(ctx context.Context, rows []db.Report) (map[uuid.UUID][]string, error) {
	urlsByReport := make(map[uuid.UUID][]string, len(rows))
	if len(rows) == 0 {
		return urlsByReport, nil
	}
	ids := make([]uuid.UUID, len(rows))
	for i, r := range rows {
		ids[i] = r.ID
	}
	deliveries, err := s.q.ListReportDeliveriesByReportIDs(ctx, ids)
	if err != nil {
		return nil, err
	}
	for _, d := range deliveries {
		if d.Url != "" {
			urlsByReport[d.ReportID] = append(urlsByReport[d.ReportID], d.Url)
		}
	}
	return urlsByReport, nil
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

	payload := json.RawMessage(rep.Payload)
	if len(payload) == 0 {
		payload = json.RawMessage("null")
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
		Status:         reportStatus(rep.ProcessedAt, rep.DeliveryError),
		DeliveryError:  rep.DeliveryError,
		Payload:        payload,
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

	if err := s.q.ClearReportFailure(ctx, rep.ID); err != nil {
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

func reportStatus(processedAt *time.Time, deliveryError *string) string {
	if processedAt != nil {
		return "delivered"
	}
	if deliveryError != nil {
		return "failed"
	}
	return "queued"
}

func formatCursor(occurredAt time.Time, id uuid.UUID) string {
	return fmt.Sprintf("%s_%s", occurredAt.UTC().Format(time.RFC3339Nano), id)
}

// parseCursor accepts a composite "<timestamp>_<uuid>" keyset cursor or a bare
// RFC3339 timestamp (used by date-bounded filters such as "on or before").
func parseCursor(raw string) (pgtype.Timestamptz, *uuid.UUID, error) {
	if raw == "" {
		return pgtype.Timestamptz{}, nil, nil
	}
	tsPart, idPart, hasID := strings.Cut(raw, "_")
	t, err := parseTimestamp(tsPart)
	if err != nil {
		return pgtype.Timestamptz{}, nil, apperr.Invalid("invalid cursor format")
	}
	ts := pgtype.Timestamptz{Time: t, Valid: true}
	if !hasID {
		return ts, nil, nil
	}
	id, err := uuid.Parse(idPart)
	if err != nil {
		return pgtype.Timestamptz{}, nil, apperr.Invalid("invalid cursor format")
	}
	return ts, &id, nil
}

func parseTimestamp(s string) (time.Time, error) {
	if t, err := time.Parse(time.RFC3339Nano, s); err == nil {
		return t, nil
	}
	return time.Parse(time.RFC3339, s)
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
